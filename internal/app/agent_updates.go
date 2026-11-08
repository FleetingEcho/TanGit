package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentUpdateResultStatus string

const (
	agentUpdatePending    agentUpdateResultStatus = "pending"
	agentUpdateRunning    agentUpdateResultStatus = "running"
	agentUpdateSucceeded  agentUpdateResultStatus = "succeeded"
	agentUpdateConflicted agentUpdateResultStatus = "conflicted"
	agentUpdateFailed     agentUpdateResultStatus = "failed"
)

// agentUpdateResult keeps the immutable status capture beside the mutable
// operation result. HeadSHA is always the HEAD captured by Fetch and check;
// it is never silently replaced after a rebase or recovery attempt.
type agentUpdateResult struct {
	TaskID       string
	Title        string
	OpenPath     string
	WorktreePath string
	BaseRef      string
	BaseCommit   string
	HeadSHA      string
	BaseSHA      string
	Ahead        int
	Behind       int
	Checked      bool
	Status       agentUpdateResultStatus
	Error        string
	Detail       string
}

type agentUpdateTarget struct {
	TaskID       string
	Title        string
	OpenPath     string
	BaseRef      string
	ExpectedHead string
}

// agentUpdateUIState owns only coordinated-update jobs. In particular, it
// never borrows actionBusy for background target sessions. The stop channel
// prevents scheduling another task; cancel is reserved for repository/reset
// invalidation, so Stop never claims that the current Git process was canceled.
type agentUpdateUIState struct {
	open            bool
	checking        bool
	running         bool
	checked         bool
	cancelRequested bool
	token           uint64
	cancel          context.CancelFunc
	stop            chan struct{}
	repositoryID    string
	ordered         []agentwork.Task
	selected        map[string]*bool
	results         []agentUpdateResult
	err             string
}

func (a *app) resetAgentUpdates() {
	state := &a.agents.updates
	state.token++
	if state.cancel != nil {
		state.cancel()
	}
	if state.stop != nil && !state.cancelRequested {
		close(state.stop)
	}
	a.agents.updates = agentUpdateUIState{token: state.token}
}

// beginAgentUpdateCenter opens a selection containing cloned, explicit active
// tasks. Nothing is selected implicitly and no repository work starts here.
func (a *app) beginAgentUpdateCenter() {
	state := &a.agents.updates
	if state.checking || state.running {
		return
	}
	repositoryID := a.agentRepositoryID()
	if repositoryID == "" {
		return
	}
	if state.open && state.repositoryID == repositoryID {
		return
	}
	tasks := make([]agentwork.Task, 0, len(a.agents.tasks))
	for _, task := range a.agents.tasks {
		if agentTaskActive(task) {
			tasks = append(tasks, task.Clone())
		}
	}
	if len(tasks) == 0 {
		a.notify("warning", "No agent tasks to update", "Create an active agent task before opening coordinated updates.")
		return
	}
	state.token++
	if state.cancel != nil {
		state.cancel()
	}
	if state.stop != nil && !state.cancelRequested {
		close(state.stop)
	}
	selected := make(map[string]*bool, len(tasks))
	for _, task := range tasks {
		choice := false
		selected[task.ID] = &choice
	}
	a.agents.updates = agentUpdateUIState{
		open: true, token: state.token, repositoryID: repositoryID,
		ordered: tasks, selected: selected,
	}
}

func (a *app) agentUpdateTargets() []agentUpdateTarget {
	state := &a.agents.updates
	targets := make([]agentUpdateTarget, 0, len(state.ordered))
	for _, task := range state.ordered {
		choice := state.selected[task.ID]
		if choice == nil || !*choice {
			continue
		}
		targets = append(targets, agentUpdateTarget{
			TaskID: task.ID, Title: task.Title,
			OpenPath: a.worktreeOpenPath(task.WorktreePath), BaseRef: task.BaseRef,
		})
	}
	return targets
}

func newAgentUpdateResult(target agentUpdateTarget, task agentwork.Task) agentUpdateResult {
	return agentUpdateResult{
		TaskID: target.TaskID, Title: target.Title, OpenPath: target.OpenPath,
		WorktreePath: task.WorktreePath, BaseRef: target.BaseRef, BaseCommit: task.BaseCommit,
		Status: agentUpdatePending,
	}
}

func (a *app) publishAgentUpdate(token uint64, generation uint64, repositoryID string, publish func(*agentUpdateUIState)) {
	a.update(func() {
		state := &a.agents.updates
		if generation != a.generation || token != state.token || repositoryID != a.agentRepositoryID() {
			return
		}
		publish(state)
	})
}

func setAgentUpdateResult(state *agentUpdateUIState, result agentUpdateResult) {
	for index := range state.results {
		if state.results[index].TaskID == result.TaskID {
			state.results[index] = result
			return
		}
	}
}

func mergeAgentUpdateCheckpointTask(a *app, updated agentwork.Task) {
	if updated.ID == "" {
		return
	}
	for index := range a.agents.tasks {
		current := a.agents.tasks[index]
		if current.ID == updated.ID && current.WorktreePath == updated.WorktreePath && current.BaseCommit == updated.BaseCommit {
			a.agents.tasks[index] = updated.Clone()
			return
		}
	}
}

// beginAgentUpdateFetchAndCheck performs exactly one foreground Fetch before
// opening any selected task. Every target then has its own status and error;
// no Snapshot or task readiness inference participates in this check.
func (a *app) beginAgentUpdateFetchAndCheck() {
	state := &a.agents.updates
	if !state.open || state.checking || state.running || a.session == nil {
		return
	}
	targets := a.agentUpdateTargets()
	if len(targets) == 0 {
		a.notify("warning", "No agent tasks selected", "Select at least one task explicitly before fetching and checking.")
		return
	}
	byID := make(map[string]agentwork.Task, len(state.ordered))
	for _, task := range state.ordered {
		byID[task.ID] = task.Clone()
	}
	results := make([]agentUpdateResult, 0, len(targets))
	for _, target := range targets {
		results = append(results, newAgentUpdateResult(target, byID[target.TaskID]))
	}

	state.token++
	if state.cancel != nil {
		state.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.stop = make(chan struct{})
	state.cancelRequested = false
	state.checking = true
	state.checked = false
	state.running = false
	state.results = results
	state.err = ""
	token, generation, repositoryID := state.token, a.generation, state.repositoryID
	foreground, opener, stop := a.session, a.opener, state.stop

	go func() {
		defer a.recoverBackground()
		defer cancel()
		if err := foreground.Fetch(ctx); err != nil {
			a.publishAgentUpdate(token, generation, repositoryID, func(state *agentUpdateUIState) {
				state.checking = false
				state.cancel = nil
				state.err = "Fetch failed; no task worktree was checked: " + safeError(err)
			})
			return
		}

		for _, target := range targets {
			select {
			case <-stop:
				a.finishAgentUpdateCheck(token, generation, repositoryID)
				return
			default:
			}
			result := newAgentUpdateResult(target, byID[target.TaskID])
			result.Status = agentUpdateRunning
			runningResult := result
			a.publishAgentUpdate(token, generation, repositoryID, func(state *agentUpdateUIState) {
				setAgentUpdateResult(state, runningResult)
			})

			session, err := opener.Open(ctx, target.OpenPath)
			if err == nil {
				var status repository.AgentTargetStatus
				status, err = session.AgentTargetStatus(ctx, target.BaseRef)
				closeRepositorySession(session)
				if err == nil {
					result.HeadSHA = status.HeadSHA
					result.BaseSHA = status.BaseSHA
					result.Ahead = status.Ahead
					result.Behind = status.Behind
					if strings.TrimSpace(status.HeadSHA) == "" {
						err = errors.New("task status returned an empty HEAD commit")
					}
				}
			}
			if err != nil {
				result.Status = agentUpdateFailed
				result.Error = safeError(err)
			} else {
				result.Checked = true
				result.Status = agentUpdatePending
			}
			a.publishAgentUpdate(token, generation, repositoryID, func(state *agentUpdateUIState) {
				setAgentUpdateResult(state, result)
			})
		}
		a.finishAgentUpdateCheck(token, generation, repositoryID)
	}()
}

func (a *app) finishAgentUpdateCheck(token uint64, generation uint64, repositoryID string) {
	a.publishAgentUpdate(token, generation, repositoryID, func(state *agentUpdateUIState) {
		state.checking = false
		state.checked = true
		state.cancel = nil
	})
}

// stopAgentUpdateScheduling leaves the active fetch/status/rebase/recovery to
// return its real outcome. Only not-yet-started targets remain pending.
func (a *app) stopAgentUpdateScheduling() {
	state := &a.agents.updates
	if (!state.checking && !state.running) || state.cancelRequested {
		return
	}
	state.cancelRequested = true
	if state.stop != nil {
		close(state.stop)
	}
	state.err = "Stop requested. The current Git operation will finish; no additional task will start."
}

// beginAgentUpdateRebase freezes IDs, open paths, BaseRefs and captured HEADs
// before showing confirmation. Its callback runs only the frozen selection.
func (a *app) beginAgentUpdateRebase() {
	state := &a.agents.updates
	if !state.open || state.checking || state.running {
		return
	}
	selected := make(map[string]bool)
	for id, choice := range state.selected {
		selected[id] = choice != nil && *choice
	}
	targets := make([]agentUpdateTarget, 0, len(state.results))
	for _, result := range state.results {
		if !selected[result.TaskID] || !result.Checked || result.Status != agentUpdatePending || result.HeadSHA == "" {
			continue
		}
		targets = append(targets, agentUpdateTarget{
			TaskID: result.TaskID, Title: result.Title, OpenPath: result.OpenPath,
			BaseRef: result.BaseRef, ExpectedHead: result.HeadSHA,
		})
	}
	if len(targets) == 0 {
		a.notify("warning", "No checked agent tasks selected", "Run Fetch and check, then select tasks with a successful status capture.")
		return
	}
	repositoryID, token, generation := state.repositoryID, state.token, a.generation
	a.askConfirm(confirmRequest{
		title:        "Rebase selected agent tasks",
		message:      trf("Rebase %d explicitly selected task worktrees onto their recorded BaseRefs?", len(targets)),
		detail:       tr("Each task is processed sequentially against its frozen path, BaseRef and captured HEAD. TanGit verifies that HEAD and records a durable safety checkpoint before every rebase. Conflicts and errors do not stop unrelated tasks. Task BaseCommit and lifecycle are unchanged; nothing is pushed."),
		confirmLabel: "Rebase selected",
		destructive:  true,
		run: func() {
			if token != a.agents.updates.token || generation != a.generation || repositoryID != a.agentRepositoryID() {
				return
			}
			a.startAgentUpdateRebase(repositoryID, targets)
		},
	})
}

func (a *app) startAgentUpdateRebase(repositoryID string, targets []agentUpdateTarget) {
	state := &a.agents.updates
	if state.running || state.checking || state.repositoryID != repositoryID || len(targets) == 0 {
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		state.err = "Could not open the agent task store: " + safeError(err)
		return
	}
	state.token++
	if state.cancel != nil {
		state.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.stop = make(chan struct{})
	state.cancelRequested = false
	state.running = true
	state.err = ""
	for _, target := range targets {
		for index := range state.results {
			if state.results[index].TaskID == target.TaskID {
				state.results[index].Status = agentUpdatePending
				state.results[index].Error = ""
				state.results[index].Detail = ""
			}
		}
	}
	token, generation := state.token, a.generation
	opener, stop := a.opener, state.stop
	captures := make(map[string]agentUpdateResult, len(targets))
	for _, target := range targets {
		for _, result := range state.results {
			if result.TaskID == target.TaskID {
				captures[target.TaskID] = result
				break
			}
		}
	}

	go func() {
		defer a.recoverBackground()
		defer cancel()
		for _, target := range targets {
			select {
			case <-stop:
				a.finishAgentUpdateRun(token, generation, repositoryID)
				return
			default:
			}
			a.publishAgentUpdate(token, generation, repositoryID, func(state *agentUpdateUIState) {
				for index := range state.results {
					if state.results[index].TaskID == target.TaskID {
						state.results[index].Status = agentUpdateRunning
						state.results[index].Error = ""
						state.results[index].Detail = ""
					}
				}
			})
			result, updated := executeAgentUpdateRebase(ctx, opener, store, target, captures[target.TaskID])
			a.publishAgentUpdate(token, generation, repositoryID, func(state *agentUpdateUIState) {
				mergeAgentUpdateCheckpointTask(a, updated)
				setAgentUpdateResult(state, result)
			})
		}
		a.finishAgentUpdateRun(token, generation, repositoryID)
	}()
}

func (a *app) finishAgentUpdateRun(token uint64, generation uint64, repositoryID string) {
	a.publishAgentUpdate(token, generation, repositoryID, func(state *agentUpdateUIState) {
		state.running = false
		state.cancel = nil
	})
}

func executeAgentUpdateRebase(ctx context.Context, opener repositoryOpener, store *agentwork.Store, target agentUpdateTarget, capturedResult agentUpdateResult) (agentUpdateResult, agentwork.Task) {
	result := capturedResult
	result.TaskID = target.TaskID
	result.Title = target.Title
	result.OpenPath = target.OpenPath
	result.BaseRef = target.BaseRef
	result.HeadSHA = target.ExpectedHead
	result.Checked = true
	result.Status = agentUpdateFailed
	result.Error = ""
	result.Detail = ""
	session, err := opener.Open(ctx, target.OpenPath)
	if err != nil {
		result.Error = safeError(err)
		return result, agentwork.Task{}
	}
	defer closeRepositorySession(session)

	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		result.Error = safeError(err)
		return result, agentwork.Task{}
	}
	if snapshot.HeadHash == "" || snapshot.HeadHash != target.ExpectedHead {
		result.Error = fmt.Sprintf("task HEAD changed since Fetch and check (expected %s, current %s); no checkpoint or rebase was attempted",
			agentShortHash(target.ExpectedHead), agentShortHash(snapshot.HeadHash))
		return result, agentwork.Task{}
	}

	reason := "Before coordinated rebase onto " + target.BaseRef
	captured, err := session.CreateCheckpoint(ctx, reason)
	if err != nil {
		result.Error = safeError(err)
		return result, agentwork.Task{}
	}
	updated, err := store.RecordCheckpoint(target.TaskID, agentwork.Checkpoint{
		SnapshotID: captured.ID, HeadCommit: snapshot.HeadHash, Reason: reason,
	})
	if err != nil {
		result.Error = "Safety snapshot was created, but its durable task record failed; rebase was not attempted: " + safeError(err)
		return result, agentwork.Task{}
	}

	if err := session.RebaseOntoBranch(ctx, target.BaseRef); err != nil {
		result.Error = safeError(err)
		if agentUpdateTargetConflicted(ctx, session) {
			result.Status = agentUpdateConflicted
			result.Detail = "Rebase paused with conflicts. Resolve them in the explicit task worktree, then Continue, or Abort this rebase."
		}
		return result, updated
	}
	result.Status = agentUpdateSucceeded
	result.Detail = "Rebase completed on the explicit task worktree. Task BaseCommit and lifecycle were not changed."
	return result, updated
}

func agentUpdateTargetConflicted(ctx context.Context, session repository.Session) bool {
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		return false
	}
	return snapshot.Operation != "" || conflictedFileCount(snapshot.Files) > 0
}

func (a *app) confirmAgentUpdateRecovery(taskID string, abort bool) {
	state := &a.agents.updates
	if state.running || state.checking {
		return
	}
	var result agentUpdateResult
	found := false
	for _, candidate := range state.results {
		if candidate.TaskID == taskID {
			result = candidate
			found = true
			break
		}
	}
	if !found || result.Status != agentUpdateConflicted {
		return
	}
	repositoryID, token, generation := state.repositoryID, state.token, a.generation
	title, label, operation := "Continue agent task rebase", "Continue rebase", "continue"
	detail := "ContinueOperation runs only in this task's frozen worktree after you resolve and stage its conflicts. Another conflict is reported without affecting other tasks."
	if abort {
		title, label, operation = "Abort agent task rebase", "Abort rebase", "abort"
		detail = "AbortOperation runs only in this task's frozen worktree. Durable pre-rebase checkpoints and the task's BaseCommit/lifecycle remain unchanged."
	}
	a.askConfirm(confirmRequest{
		title: title, message: trf("%s rebase for %s?", strings.ToUpper(operation[:1])+operation[1:], result.Title),
		detail: detail, confirmLabel: label, destructive: abort,
		run: func() {
			if token != a.agents.updates.token || generation != a.generation || repositoryID != a.agentRepositoryID() {
				return
			}
			a.startAgentUpdateRecovery(repositoryID, result, abort)
		},
	})
}

func (a *app) startAgentUpdateRecovery(repositoryID string, result agentUpdateResult, abort bool) {
	state := &a.agents.updates
	if state.running || state.checking || state.repositoryID != repositoryID {
		return
	}
	state.token++
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	state.stop = make(chan struct{})
	state.cancelRequested = false
	state.running = true
	state.err = ""
	result.Status = agentUpdateRunning
	result.Error = ""
	if abort {
		result.Detail = "Aborting the rebase in the explicit task worktree."
	} else {
		result.Detail = "Continuing the rebase in the explicit task worktree."
	}
	setAgentUpdateResult(state, result)
	token, generation := state.token, a.generation
	opener := a.opener
	go func() {
		defer a.recoverBackground()
		defer cancel()
		result := executeAgentUpdateRecovery(ctx, opener, result, abort)
		a.publishAgentUpdate(token, generation, repositoryID, func(state *agentUpdateUIState) {
			state.running = false
			state.cancel = nil
			setAgentUpdateResult(state, result)
		})
	}()
}

func executeAgentUpdateRecovery(ctx context.Context, opener repositoryOpener, result agentUpdateResult, abort bool) agentUpdateResult {
	result.Status = agentUpdateFailed
	result.Error = ""
	result.Detail = ""
	session, err := opener.Open(ctx, result.OpenPath)
	if err != nil {
		result.Error = safeError(err)
		return result
	}
	defer closeRepositorySession(session)
	if abort {
		err = session.AbortOperation(ctx)
	} else {
		err = session.ContinueOperation(ctx)
	}
	if err != nil {
		result.Error = safeError(err)
		if agentUpdateTargetConflicted(ctx, session) {
			result.Status = agentUpdateConflicted
			result.Detail = "The operation is still paused. Resolve remaining conflicts before continuing, or abort it."
		}
		return result
	}
	result.Status = agentUpdateSucceeded
	if abort {
		result.Detail = "Rebase aborted in the explicit task worktree. Durable checkpoints and task BaseCommit/lifecycle are unchanged."
	} else {
		result.Detail = "Rebase continued successfully in the explicit task worktree. Task BaseCommit/lifecycle are unchanged."
	}
	return result
}
