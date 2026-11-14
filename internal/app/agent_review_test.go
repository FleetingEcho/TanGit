package app

import (
	"context"
	"sync"
	"testing"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentReviewOpener struct {
	session repository.Session
	calls   *callRecorder
}

func (o agentReviewOpener) Init(context.Context, string) error { return nil }
func (o agentReviewOpener) Open(_ context.Context, path string) (repository.Session, error) {
	o.calls.record("open-review:" + path)
	return o.session, nil
}

type agentReviewSession struct {
	fakeSession
	calls      *callRecorder
	base       string
	head       string
	final      []repository.CommitFile
	commits    []repository.Commit
	commitFile repository.CommitFile
	closeOnce  sync.Once
	closed     int
}

func (s *agentReviewSession) Snapshot(context.Context) (repository.Snapshot, error) {
	snapshot := s.fakeSession.snapshot
	snapshot.HeadHash = s.head
	return snapshot, nil
}
func (s *agentReviewSession) FinalFiles(_ context.Context, base string, _ repository.DiffOptions) ([]repository.CommitFile, error) {
	s.calls.record("final-files:" + base)
	return append([]repository.CommitFile(nil), s.final...), nil
}
func (s *agentReviewSession) CommitRange(_ context.Context, base, head string, _, _ int) ([]repository.Commit, error) {
	s.calls.record("commit-range:" + base + ":" + head)
	return append([]repository.Commit(nil), s.commits...), nil
}
func (s *agentReviewSession) FinalFileDiff(_ context.Context, base string, file repository.CommitFile, _ repository.DiffOptions) (repository.Diff, error) {
	s.calls.record("final-diff:" + base + ":" + file.Path)
	return repository.Diff{Patch: "diff --git a/" + file.Path + " b/" + file.Path + "\n--- a/" + file.Path + "\n+++ b/" + file.Path + "\n@@ -1 +1 @@\n-base\n+agent\n"}, nil
}
func (s *agentReviewSession) CommitFiles(_ context.Context, commit repository.Commit) ([]repository.CommitFile, error) {
	s.calls.record("commit-files:" + commit.Hash)
	return []repository.CommitFile{s.commitFile}, nil
}
func (s *agentReviewSession) CommitFileDiff(_ context.Context, commit repository.Commit, file repository.CommitFile, _ repository.DiffOptions) (repository.Diff, error) {
	s.calls.record("commit-diff:" + commit.Hash + ":" + file.Path)
	return repository.Diff{Patch: "diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-base\n+commit\n"}, nil
}
func (s *agentReviewSession) Close() error {
	s.closeOnce.Do(func() { s.closed++ })
	return nil
}

func readyAgentReviewTask(t *testing.T, h *harness, store *agentwork.Store) agentwork.Task {
	t.Helper()
	task := createAgentTestTask(t, h, store, agentwork.Task{Profile: agentwork.Profile{LaunchCommand: "agent"}})
	var err error
	task, err = store.Update(task.ID, func(task *agentwork.Task) error {
		task.Status = agentwork.StatusRunning
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err = store.Update(task.ID, func(task *agentwork.Task) error {
		task.Status = agentwork.StatusReadyForReview
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h.app.agents.tasks[0] = task.Clone()
	return task
}

func TestAgentReviewUsesExplicitWorktreeForAllThreeViews(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := readyAgentReviewTask(t, h, store)
	calls := &callRecorder{}
	commit := repository.Commit{Hash: "bbbbbbbbbbbb", ShortHash: "bbbb", Subject: "Agent implementation"}
	target := &agentReviewSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{
			Root:  task.WorktreePath,
			Files: []repository.File{{Path: "dirty.txt", Status: " M", Unstaged: true}},
		}},
		calls: calls, base: task.BaseCommit, head: "cccccccccccc",
		final:   []repository.CommitFile{{Path: "committed.txt", Status: "M"}, {Path: "untracked.txt", Status: "??"}},
		commits: []repository.Commit{commit}, commitFile: repository.CommitFile{Path: "committed.txt"},
	}
	h.app.opener = agentReviewOpener{session: target, calls: calls}
	h.app.session = fakeSession{snapshot: repository.Snapshot{Root: "/work/project"}}

	h.app.openAgentReview(task)
	h.waitFor(t, "all review reads from the target worktree", func() bool {
		return !h.app.agents.review.loading && h.app.agents.review.target == target
	})
	if !calls.has("open-review:"+task.WorktreePath) || !calls.has("final-files:"+task.BaseCommit) ||
		!calls.has("commit-range:"+task.BaseCommit+":cccccccccccc") {
		t.Fatalf("review calls do not use fixed task/base/target: %v", calls.all())
	}
	if len(h.app.agents.review.finalFiles) != 2 || h.app.agents.review.finalFiles[1].Status != "??" {
		t.Fatalf("final result omitted untracked output: %#v", h.app.agents.review.finalFiles)
	}
	if h.app.agents.review.head != "cccccccccccc" {
		t.Fatalf("review captured HEAD = %q", h.app.agents.review.head)
	}

	h.app.setAgentReviewMode(agentReviewFinal)
	h.app.requestAgentReviewDiff()
	h.waitFor(t, "the direct final diff", func() bool { return h.app.agents.review.diffPatch != "" })
	if !calls.has("final-diff:" + task.BaseCommit + ":committed.txt") {
		t.Fatalf("final diff was not base-relative: %v", calls.all())
	}

	h.app.setAgentReviewMode(agentReviewCommits)
	h.app.requestAgentReviewDiff()
	h.waitFor(t, "commit files through the target session", func() bool {
		return len(h.app.agents.review.commitFiles) == 1
	})
	h.app.requestAgentReviewDiff()
	h.waitFor(t, "commit file diff through the target session", func() bool {
		return calls.has("commit-diff:" + commit.Hash + ":committed.txt")
	})
	h.app.setAgentReviewMode(agentReviewDirty)
	if len(h.app.agents.review.dirtyFiles) != 1 || h.app.agents.review.dirtyFiles[0].Path != "dirty.txt" {
		t.Fatalf("dirty-only result = %#v", h.app.agents.review.dirtyFiles)
	}

	h.app.closeAgentReview()
	if target.closed != 1 || h.app.agents.review.target != nil {
		t.Fatalf("closing review closed target %d times", target.closed)
	}
}

func TestAgentReviewCourierUsesTheExactReviewedFilePatch(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := readyAgentReviewTask(t, h, store)
	h.app.session = fakeSession{snapshot: h.app.snapshot}
	h.app.agents.review.open = true
	h.app.agents.review.taskID = task.ID
	h.app.agents.review.mode = agentReviewFinal
	h.app.agents.review.finalFiles = []repository.CommitFile{{Path: "result.txt"}}
	h.app.agents.review.selectedFile = 0
	const patch = "diff --git a/result.txt b/result.txt\n--- a/result.txt\n+++ b/result.txt\n@@ -1 +1 @@\n-base\n+agent\n"
	h.app.agents.review.diffPatch = patch

	h.app.sendAgentReviewFile()
	if !h.app.courier.open || h.app.courier.patchText != patch {
		t.Fatalf("courier did not freeze the reviewed patch: open=%v patch=%q", h.app.courier.open, h.app.courier.patchText)
	}
	if h.app.courier.source != "Agent review · result.txt" {
		t.Fatalf("courier source = %q", h.app.courier.source)
	}
	h.app.closeChangeCourier()
}

func TestAgentReviewRejectsLateTargetAfterRepositoryReset(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{})
	release := make(chan struct{})
	started := make(chan struct{})
	opener := &blockingAgentReviewOpener{release: release, started: started}
	h.app.opener = opener
	h.app.openAgentReview(task)
	<-started
	h.app.resetRepositoryState(repository.Snapshot{})
	close(release)

	h.updates.waitForUpdate(t)
	if h.app.agents.review.open || h.app.agents.review.target != nil {
		t.Fatal("repository reset retained the agent review")
	}
}

type blockingAgentReviewOpener struct {
	release <-chan struct{}
	started chan<- struct{}
	once    sync.Once
}

func (o *blockingAgentReviewOpener) Init(context.Context, string) error { return nil }
func (o *blockingAgentReviewOpener) Open(context.Context, string) (repository.Session, error) {
	o.once.Do(func() { close(o.started) })
	<-o.release
	return fakeSession{}, nil
}
