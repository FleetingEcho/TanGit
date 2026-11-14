package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentUpdateTestSession struct {
	fakeSession
	mu             sync.Mutex
	calls          *callRecorder
	store          *agentwork.Store
	taskID         string
	head           string
	baseSHA        string
	ahead          int
	behind         int
	statusErr      error
	rebaseErr      error
	continueErr    error
	abortErr       error
	operation      string
	checkpoints    int
	rebaseCalls    int
	continueCalls  int
	abortCalls     int
	fetchCalls     int
	rebaseStarted  chan struct{}
	rebaseRelease  chan struct{}
	rebaseFinished chan struct{}
}

func (s *agentUpdateTestSession) Fetch(context.Context) error {
	s.mu.Lock()
	s.fetchCalls++
	s.mu.Unlock()
	s.calls.record("fetch")
	return nil
}

func (s *agentUpdateTestSession) AgentTargetStatus(_ context.Context, base string) (repository.AgentTargetStatus, error) {
	s.calls.record("status:" + base)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.statusErr != nil {
		return repository.AgentTargetStatus{}, s.statusErr
	}
	return repository.AgentTargetStatus{HeadSHA: s.head, BaseSHA: s.baseSHA, Ahead: s.ahead, Behind: s.behind}, nil
}

func (s *agentUpdateTestSession) Snapshot(context.Context) (repository.Snapshot, error) {
	s.calls.record("snapshot")
	s.mu.Lock()
	defer s.mu.Unlock()
	return repository.Snapshot{
		Root: s.fakeSession.snapshot.Root, HeadHash: s.head, Operation: s.operation,
		Files: append([]repository.File(nil), s.fakeSession.snapshot.Files...),
	}, nil
}

func (s *agentUpdateTestSession) CreateCheckpoint(_ context.Context, reason string) (repository.WorkSnapshot, error) {
	s.calls.record("checkpoint:" + reason)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints++
	id := fmt.Sprintf("%s-checkpoint-%d", s.taskID, s.checkpoints)
	return repository.WorkSnapshot{ID: id, Hash: id, Kind: repository.CheckpointSnapshotKind, Name: reason}, nil
}

func (s *agentUpdateTestSession) RebaseOntoBranch(_ context.Context, base string) error {
	s.calls.record("rebase:" + base)
	s.mu.Lock()
	s.rebaseCalls++
	started, release, finished := s.rebaseStarted, s.rebaseRelease, s.rebaseFinished
	rebaseErr, head := s.rebaseErr, s.head
	s.mu.Unlock()

	// The rebase fake refuses to proceed unless RecordCheckpoint is already
	// durable, making the required write ordering observable rather than just
	// checking a call list after the fact.
	stored, err := s.store.Get(s.taskID)
	if err != nil {
		return err
	}
	if len(stored.Checkpoints) != s.rebaseCalls {
		return errors.New("rebase started before the durable task checkpoint")
	}
	checkpoint := stored.Checkpoints[len(stored.Checkpoints)-1]
	if checkpoint.HeadCommit != head || checkpoint.Reason != "Before coordinated rebase onto "+base {
		return errors.New("rebase started with the wrong durable checkpoint")
	}
	if started != nil {
		close(started)
	}
	if release != nil {
		<-release
	}
	if finished != nil {
		defer close(finished)
	}
	if rebaseErr != nil {
		s.mu.Lock()
		s.operation = "rebase"
		s.mu.Unlock()
		return rebaseErr
	}
	return nil
}

func (s *agentUpdateTestSession) ContinueOperation(context.Context) error {
	s.calls.record("continue")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.continueCalls++
	if s.continueErr == nil {
		s.operation = ""
	}
	return s.continueErr
}

func (s *agentUpdateTestSession) AbortOperation(context.Context) error {
	s.calls.record("abort")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.abortCalls++
	if s.abortErr == nil {
		s.operation = ""
	}
	return s.abortErr
}

func (s *agentUpdateTestSession) setHead(head string) {
	s.mu.Lock()
	s.head = head
	s.mu.Unlock()
}

func (s *agentUpdateTestSession) counts() (fetch, checkpoints, rebases, continues, aborts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetchCalls, s.checkpoints, s.rebaseCalls, s.continueCalls, s.abortCalls
}

type agentUpdateTestOpener struct {
	calls   *callRecorder
	targets map[string]repository.Session
}

func (o *agentUpdateTestOpener) Init(context.Context, string) error { return nil }
func (o *agentUpdateTestOpener) Open(_ context.Context, path string) (repository.Session, error) {
	o.calls.record("open:" + path)
	if target := o.targets[path]; target != nil {
		return target, nil
	}
	return nil, errors.New("unknown explicit update target " + path)
}

func createAgentUpdateTask(t *testing.T, h *harness, store *agentwork.Store, title, path, baseRef, baseCommit string, status agentwork.Status) agentwork.Task {
	t.Helper()
	created, err := store.Create(agentwork.Task{
		RepositoryID: h.app.agentRepositoryID(), Title: title, BaseRef: baseRef, BaseCommit: baseCommit,
		Branch: "agent/" + strings.ToLower(title), WorktreePath: path, Status: status,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.app.agents.tasks = append(h.app.agents.tasks, created.Clone())
	return created
}

func prepareAgentUpdateTest(t *testing.T, specs ...struct {
	title, path, baseRef, baseCommit, head, baseSHA string
	status                                          agentwork.Status
}) (*harness, *agentwork.Store, []agentwork.Task, *agentUpdateTestSession, map[string]*agentUpdateTestSession, *callRecorder) {
	t.Helper()
	h, store := newAgentTestHarness(t)
	calls := &callRecorder{}
	main := &agentUpdateTestSession{calls: calls}
	h.app.session = main
	opener := &agentUpdateTestOpener{calls: calls, targets: map[string]repository.Session{}}
	h.app.opener = opener
	tasks := make([]agentwork.Task, 0, len(specs))
	targets := make(map[string]*agentUpdateTestSession, len(specs))
	for _, spec := range specs {
		task := createAgentUpdateTask(t, h, store, spec.title, spec.path, spec.baseRef, spec.baseCommit, spec.status)
		tasks = append(tasks, task)
		target := &agentUpdateTestSession{
			fakeSession: fakeSession{snapshot: repository.Snapshot{Root: spec.path}},
			calls:       calls, store: store, taskID: task.ID, head: spec.head, baseSHA: spec.baseSHA, ahead: 2, behind: 3,
		}
		targets[task.ID] = target
		opener.targets[spec.path] = target
	}
	h.app.beginAgentUpdateCenter()
	return h, store, tasks, main, targets, calls
}

func selectAgentUpdateTasks(h *harness, tasks ...agentwork.Task) {
	for _, task := range tasks {
		choice := h.app.agents.updates.selected[task.ID]
		if choice != nil {
			*choice = true
		}
	}
}

func agentUpdateResultFor(t *testing.T, h *harness, taskID string) agentUpdateResult {
	t.Helper()
	for _, result := range h.app.agents.updates.results {
		if result.TaskID == taskID {
			return result
		}
	}
	t.Fatalf("no update result for task %s", taskID)
	return agentUpdateResult{}
}

func TestAgentUpdateFetchesOnceAndChecksOnlyExplicitTaskBaseRefs(t *testing.T) {
	h, _, tasks, main, targets, calls := prepareAgentUpdateTest(t,
		struct {
			title, path, baseRef, baseCommit, head, baseSHA string
			status                                          agentwork.Status
		}{"Alpha", "/work/alpha", "main", "fixed-alpha", "head-alpha", "resolved-main", agentwork.StatusRunning},
		struct {
			title, path, baseRef, baseCommit, head, baseSHA string
			status                                          agentwork.Status
		}{"Beta", "/work/beta", "origin/release", "fixed-beta", "head-beta", "resolved-release", agentwork.StatusReadyForReview},
		struct {
			title, path, baseRef, baseCommit, head, baseSHA string
			status                                          agentwork.Status
		}{"Unselected", "/work/unselected", "other", "fixed-other", "head-other", "resolved-other", agentwork.StatusReady},
	)
	selectAgentUpdateTasks(h, tasks[0], tasks[1])
	targets[tasks[1].ID].statusErr = errors.New("divergence unavailable")
	h.app.actionBusy = true // Background target work must not borrow this foreground lock.

	h.app.beginAgentUpdateFetchAndCheck()
	h.app.beginAgentUpdateFetchAndCheck() // A duplicate click cannot add another foreground Fetch.
	h.waitFor(t, "the explicit task checks", func() bool { return !h.app.agents.updates.checking })

	if fetch, _, _, _, _ := main.counts(); fetch != 1 {
		t.Fatalf("foreground Fetch calls = %d, want exactly one", fetch)
	}
	if got, want := calls.all(), []string{
		"fetch", "open:/work/alpha", "status:main", "open:/work/beta", "status:origin/release",
	}; !slices.Equal(got, want) {
		t.Fatalf("fetch/status calls = %v, want %v", got, want)
	}
	alpha := agentUpdateResultFor(t, h, tasks[0].ID)
	if !alpha.Checked || alpha.Status != agentUpdatePending || alpha.HeadSHA != "head-alpha" ||
		alpha.BaseSHA != "resolved-main" || alpha.Ahead != 2 || alpha.Behind != 3 || alpha.BaseCommit != "fixed-alpha" {
		t.Fatalf("alpha status capture = %#v", alpha)
	}
	beta := agentUpdateResultFor(t, h, tasks[1].ID)
	if beta.Checked || beta.Status != agentUpdateFailed || !strings.Contains(beta.Error, "divergence unavailable") {
		t.Fatalf("beta independent error = %#v", beta)
	}
	for _, target := range targets {
		if _, _, rebases, _, _ := target.counts(); rebases != 0 {
			t.Fatalf("status check unexpectedly rebased %s", target.taskID)
		}
	}
}

func TestAgentUpdateRebaseWaitsForConfirmationAndPersistsCheckpointFirst(t *testing.T) {
	h, store, tasks, _, targets, calls := prepareAgentUpdateTest(t, struct {
		title, path, baseRef, baseCommit, head, baseSHA string
		status                                          agentwork.Status
	}{"Alpha", "/work/alpha", "main", "fixed-alpha", "head-alpha", "resolved-main", agentwork.StatusRunning})
	task := tasks[0]
	target := targets[task.ID]
	selectAgentUpdateTasks(h, task)
	h.app.beginAgentUpdateFetchAndCheck()
	h.waitFor(t, "the status capture", func() bool { return !h.app.agents.updates.checking })

	h.app.beginAgentUpdateRebase()
	if h.app.confirm == nil {
		t.Fatal("rebase did not request confirmation")
	}
	if _, checkpoints, rebases, _, _ := target.counts(); checkpoints != 0 || rebases != 0 {
		t.Fatalf("writes happened before confirmation: checkpoints=%d rebases=%d", checkpoints, rebases)
	}
	h.app.acceptConfirm()
	h.waitFor(t, "the confirmed rebase", func() bool { return !h.app.agents.updates.running })

	if got, want := calls.all(), []string{
		"fetch", "open:/work/alpha", "status:main", "open:/work/alpha", "snapshot",
		"checkpoint:Before coordinated rebase onto main", "rebase:main",
	}; !slices.Equal(got, want) {
		t.Fatalf("rebase calls = %v, want checkpoint durability before mutation %v", got, want)
	}
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.BaseCommit != "fixed-alpha" || stored.Status != agentwork.StatusRunning || len(stored.Checkpoints) != 1 {
		t.Fatalf("rebase changed fixed task identity/lifecycle: %#v", stored)
	}
	if stored.Checkpoints[0].HeadCommit != "head-alpha" || stored.Checkpoints[0].BaseCommit != "fixed-alpha" {
		t.Fatalf("captured safety checkpoint = %#v", stored.Checkpoints[0])
	}
	result := agentUpdateResultFor(t, h, task.ID)
	if result.Status != agentUpdateSucceeded || result.HeadSHA != "head-alpha" || result.BaseCommit != "fixed-alpha" ||
		result.BaseSHA != "resolved-main" || result.Ahead != 2 || result.Behind != 3 {
		t.Fatalf("successful result lost its fixed capture = %#v", result)
	}
	if h.app.agents.tasks[0].Status != agentwork.StatusRunning || h.app.agents.tasks[0].BaseCommit != "fixed-alpha" {
		t.Fatalf("published task changed readiness/base: %#v", h.app.agents.tasks[0])
	}
}

func TestAgentUpdateRebaseRefusesChangedHeadBeforeAnyWrite(t *testing.T) {
	h, store, tasks, _, targets, _ := prepareAgentUpdateTest(t, struct {
		title, path, baseRef, baseCommit, head, baseSHA string
		status                                          agentwork.Status
	}{"Alpha", "/work/alpha", "main", "fixed-alpha", "captured-head", "resolved-main", agentwork.StatusReadyForReview})
	task := tasks[0]
	target := targets[task.ID]
	selectAgentUpdateTasks(h, task)
	h.app.beginAgentUpdateFetchAndCheck()
	h.waitFor(t, "the original status capture", func() bool { return !h.app.agents.updates.checking })
	target.setHead("newer-head")

	h.app.beginAgentUpdateRebase()
	h.app.acceptConfirm()
	h.waitFor(t, "the expected-head refusal", func() bool { return !h.app.agents.updates.running })

	result := agentUpdateResultFor(t, h, task.ID)
	if result.Status != agentUpdateFailed || !strings.Contains(result.Error, "task HEAD changed") || !strings.Contains(result.Error, "no checkpoint or rebase") {
		t.Fatalf("changed-head result = %#v", result)
	}
	if _, checkpoints, rebases, _, _ := target.counts(); checkpoints != 0 || rebases != 0 {
		t.Fatalf("changed HEAD allowed writes: checkpoints=%d rebases=%d", checkpoints, rebases)
	}
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Checkpoints) != 0 || stored.BaseCommit != "fixed-alpha" || stored.Status != agentwork.StatusReadyForReview {
		t.Fatalf("changed-head refusal mutated task = %#v", stored)
	}
}

func TestAgentUpdateConflictDoesNotStopUnrelatedTask(t *testing.T) {
	h, store, tasks, _, targets, _ := prepareAgentUpdateTest(t,
		struct {
			title, path, baseRef, baseCommit, head, baseSHA string
			status                                          agentwork.Status
		}{"Alpha", "/work/alpha", "main", "fixed-alpha", "head-alpha", "resolved-main", agentwork.StatusRunning},
		struct {
			title, path, baseRef, baseCommit, head, baseSHA string
			status                                          agentwork.Status
		}{"Beta", "/work/beta", "release", "fixed-beta", "head-beta", "resolved-release", agentwork.StatusReadyForReview},
	)
	selectAgentUpdateTasks(h, tasks...)
	targets[tasks[0].ID].rebaseErr = errors.New("rebase stopped at a conflict")
	h.app.beginAgentUpdateFetchAndCheck()
	h.waitFor(t, "both status captures", func() bool { return !h.app.agents.updates.checking })
	h.app.beginAgentUpdateRebase()
	h.app.acceptConfirm()
	h.waitFor(t, "independent rebase results", func() bool { return !h.app.agents.updates.running })

	if got := agentUpdateResultFor(t, h, tasks[0].ID); got.Status != agentUpdateConflicted {
		t.Fatalf("conflicted first task = %#v", got)
	}
	if got := agentUpdateResultFor(t, h, tasks[1].ID); got.Status != agentUpdateSucceeded {
		t.Fatalf("unrelated second task did not continue = %#v", got)
	}
	for index, task := range tasks {
		_, checkpoints, rebases, _, _ := targets[task.ID].counts()
		if checkpoints != 1 || rebases != 1 {
			t.Fatalf("task %d calls: checkpoints=%d rebases=%d", index, checkpoints, rebases)
		}
		stored, err := store.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.BaseCommit != tasks[index].BaseCommit || stored.Status != tasks[index].Status || len(stored.Checkpoints) != 1 {
			t.Fatalf("task %d fixed fields changed = %#v", index, stored)
		}
	}
}

func TestAgentUpdateConflictRecoveryUsesConfirmedExplicitTarget(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(map[bool]string{false: "continue", true: "abort"}[abort], func(t *testing.T) {
			h, _, tasks, _, targets, _ := prepareAgentUpdateTest(t, struct {
				title, path, baseRef, baseCommit, head, baseSHA string
				status                                          agentwork.Status
			}{"Alpha", "/work/alpha", "main", "fixed-alpha", "head-alpha", "resolved-main", agentwork.StatusRunning})
			task, target := tasks[0], targets[tasks[0].ID]
			selectAgentUpdateTasks(h, task)
			target.rebaseErr = errors.New("conflict")
			h.app.beginAgentUpdateFetchAndCheck()
			h.waitFor(t, "the conflict setup capture", func() bool { return !h.app.agents.updates.checking })
			h.app.beginAgentUpdateRebase()
			h.app.acceptConfirm()
			h.waitFor(t, "the conflict setup", func() bool { return !h.app.agents.updates.running })

			h.app.confirmAgentUpdateRecovery(task.ID, abort)
			if h.app.confirm == nil {
				t.Fatal("conflict recovery did not request confirmation")
			}
			if _, _, _, continues, aborts := target.counts(); continues != 0 || aborts != 0 {
				t.Fatalf("recovery ran before confirmation: continues=%d aborts=%d", continues, aborts)
			}
			h.app.acceptConfirm()
			h.waitFor(t, "confirmed conflict recovery", func() bool { return !h.app.agents.updates.running })
			_, _, _, continues, aborts := target.counts()
			if abort && aborts != 1 || !abort && continues != 1 {
				t.Fatalf("explicit recovery calls: continues=%d aborts=%d", continues, aborts)
			}
			if got := agentUpdateResultFor(t, h, task.ID); got.Status != agentUpdateSucceeded {
				t.Fatalf("recovery result = %#v", got)
			}
		})
	}
}

func TestAgentUpdateStaleGenerationCannotPublishResult(t *testing.T) {
	h, store, tasks, _, targets, _ := prepareAgentUpdateTest(t, struct {
		title, path, baseRef, baseCommit, head, baseSHA string
		status                                          agentwork.Status
	}{"Alpha", "/work/alpha", "main", "fixed-alpha", "head-alpha", "resolved-main", agentwork.StatusRunning})
	task, target := tasks[0], targets[tasks[0].ID]
	target.rebaseStarted = make(chan struct{})
	target.rebaseRelease = make(chan struct{})
	target.rebaseFinished = make(chan struct{})
	selectAgentUpdateTasks(h, task)
	h.app.beginAgentUpdateFetchAndCheck()
	h.waitFor(t, "the stale-publication status capture", func() bool { return !h.app.agents.updates.checking })
	h.app.beginAgentUpdateRebase()
	h.app.acceptConfirm()
	select {
	case <-target.rebaseStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("rebase did not reach its controlled blocking point")
	}

	oldGeneration := h.app.generation
	h.app.generation++
	h.app.resetAgentUpdates()
	close(target.rebaseRelease)
	// Running, result, and final completions are all still delivered by the
	// worker, but every one must fail its generation/token/repository guard.
	for range 3 {
		h.updates.waitForUpdate(t)
	}
	if h.app.generation == oldGeneration || h.app.agents.updates.open || len(h.app.agents.updates.results) != 0 {
		t.Fatalf("stale result repopulated reset state: %#v", h.app.agents.updates)
	}
	if len(h.app.agents.tasks[0].Checkpoints) != 0 {
		t.Fatalf("stale durable record was published into replacement UI: %#v", h.app.agents.tasks[0])
	}
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Checkpoints) != 1 || stored.BaseCommit != "fixed-alpha" || stored.Status != agentwork.StatusRunning {
		t.Fatalf("durable task safety/fixed fields = %#v", stored)
	}
}

func TestAgentUpdateStopSkipsSchedulingWithoutFakingCurrentCancellation(t *testing.T) {
	h, _, tasks, _, targets, _ := prepareAgentUpdateTest(t,
		struct {
			title, path, baseRef, baseCommit, head, baseSHA string
			status                                          agentwork.Status
		}{"Alpha", "/work/alpha", "main", "fixed-alpha", "head-alpha", "resolved-main", agentwork.StatusRunning},
		struct {
			title, path, baseRef, baseCommit, head, baseSHA string
			status                                          agentwork.Status
		}{"Beta", "/work/beta", "main", "fixed-beta", "head-beta", "resolved-main", agentwork.StatusRunning},
	)
	first := targets[tasks[0].ID]
	first.rebaseStarted = make(chan struct{})
	first.rebaseRelease = make(chan struct{})
	selectAgentUpdateTasks(h, tasks...)
	h.app.beginAgentUpdateFetchAndCheck()
	h.waitFor(t, "the stop-test status captures", func() bool { return !h.app.agents.updates.checking })
	h.app.beginAgentUpdateRebase()
	h.app.acceptConfirm()
	select {
	case <-first.rebaseStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first rebase did not reach its controlled blocking point")
	}

	h.app.stopAgentUpdateScheduling()
	if !h.app.agents.updates.cancelRequested || !strings.Contains(h.app.agents.updates.err, "will finish") ||
		strings.Contains(strings.ToLower(h.app.agents.updates.err), "canceled") {
		t.Fatalf("stop message overclaims process cancellation: %q", h.app.agents.updates.err)
	}
	close(first.rebaseRelease)
	h.waitFor(t, "the current operation's real outcome", func() bool { return !h.app.agents.updates.running })
	if got := agentUpdateResultFor(t, h, tasks[0].ID); got.Status != agentUpdateSucceeded {
		t.Fatalf("current Git operation did not report its real success: %#v", got)
	}
	if got := agentUpdateResultFor(t, h, tasks[1].ID); got.Status != agentUpdatePending {
		t.Fatalf("unscheduled task did not remain pending: %#v", got)
	}
	if _, _, rebases, _, _ := targets[tasks[1].ID].counts(); rebases != 0 {
		t.Fatalf("stop allowed another task to start: %d rebases", rebases)
	}
}
