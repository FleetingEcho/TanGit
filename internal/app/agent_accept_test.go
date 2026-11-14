package app

import (
	"errors"
	"slices"
	"testing"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

func TestAgentAcceptCherryPicksCapturedRangeAndMarksTaskDone(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := readyAgentReviewTask(t, h, store)
	calls := &callRecorder{}
	h.app.snapshot.Branch = "main"
	h.app.session = fakeSession{snapshot: h.app.snapshot, calls: calls}
	h.app.opener = agentCheckpointOpener{session: &agentCheckpointSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: task.WorktreePath}},
		head:        "agent-head",
	}}
	h.app.agents.review.open = true
	h.app.agents.review.taskID = task.ID
	h.app.agents.review.commits = []repository.Commit{
		{Hash: "newest"}, {Hash: "oldest"},
	}

	h.app.confirmAcceptAgentCommits()
	if h.app.confirm == nil {
		t.Fatal("accepting commits did not request confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "the captured commits to be applied and the task archived as done", func() bool {
		current, ok := h.app.agentReviewTask()
		return calls.has("batch-cherry-pick:newest") && calls.has("batch-cherry-pick:oldest") &&
			!h.app.actionBusy && ok && current.Status == agentwork.StatusDone
	})
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentwork.StatusDone {
		t.Fatalf("stored task status = %q, want done", stored.Status)
	}
}

func TestAgentAcceptCheckpointFailurePreventsCherryPick(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := readyAgentReviewTask(t, h, store)
	mainCalls := &callRecorder{}
	h.app.snapshot.Branch = "main"
	h.app.session = fakeSession{snapshot: h.app.snapshot, calls: mainCalls}
	checkpointCalls := &callRecorder{}
	h.app.opener = agentCheckpointOpener{calls: checkpointCalls, session: &agentCheckpointSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: task.WorktreePath}},
		calls:       checkpointCalls, head: "agent-head", checkpointErr: errors.New("cannot capture WIP"),
	}}
	h.app.agents.review.open = true
	h.app.agents.review.taskID = task.ID
	h.app.agents.review.commits = []repository.Commit{{Hash: "agent-commit"}}

	h.app.confirmAcceptAgentCommits()
	if h.app.confirm == nil {
		t.Fatal("accepting commits did not preserve user confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "the required pre-accept checkpoint failure", func() bool {
		return !h.app.agents.checkpointBusy[task.ID]
	})
	if mainCalls.has("batch-cherry-pick:agent-commit") || h.app.actionBusy {
		t.Fatalf("checkpoint failure started the main-repository mutation: calls=%v busy=%t", mainCalls.all(), h.app.actionBusy)
	}
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentwork.StatusReadyForReview || len(stored.Checkpoints) != 0 {
		t.Fatalf("checkpoint failure changed the task: %#v", stored)
	}
	if got, want := checkpointCalls.all(), []string{"open-checkpoint:" + task.WorktreePath, "checkpoint:Before accept"}; !slices.Equal(got, want) {
		t.Fatalf("pre-accept calls = %v, want %v", got, want)
	}
}

func TestAgentAcceptMergeUsesTaskBranchAndMarksTaskDone(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := readyAgentReviewTask(t, h, store)
	calls := &callRecorder{}
	h.app.snapshot.Branch = "main"
	h.app.session = fakeSession{snapshot: h.app.snapshot, calls: calls}
	h.app.opener = agentCheckpointOpener{session: &agentCheckpointSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: task.WorktreePath}},
		head:        "agent-head",
	}}
	h.app.agents.review.open = true
	h.app.agents.review.taskID = task.ID
	h.app.agents.review.commits = []repository.Commit{{Hash: "agent-commit"}}

	h.app.confirmMergeAgentTask()
	if h.app.confirm == nil {
		t.Fatal("merging the agent branch did not request confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "the task branch to be merged and the task marked done", func() bool {
		current, ok := h.app.agentReviewTask()
		return calls.has("merge:"+task.Branch) && !h.app.actionBusy && ok && current.Status == agentwork.StatusDone
	})
}

func TestAgentArchivePreservesWorktreeAndBranch(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := readyAgentReviewTask(t, h, store)
	calls := &callRecorder{}
	h.app.session = fakeSession{snapshot: h.app.snapshot, calls: calls}

	h.app.confirmArchiveAgentTask(task)
	if h.app.confirm == nil {
		t.Fatal("archiving did not request confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "the durable task to be archived", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusArchived && !h.app.agents.mutating[task.ID]
	})
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.WorktreePath != task.WorktreePath || stored.Branch != task.Branch || stored.ArchivedAt.IsZero() {
		t.Fatalf("archive changed task identity or omitted its timestamp: %#v", stored)
	}
	if len(calls.all()) != 0 {
		t.Fatalf("archive unexpectedly ran Git operations: %v", calls.all())
	}
}

func TestAgentAcceptRejectsDirtyOrNonReviewTasks(t *testing.T) {
	for _, test := range []struct {
		name   string
		status agentwork.Status
		dirty  []repository.File
	}{
		{name: "uncommitted output", status: agentwork.StatusReadyForReview, dirty: []repository.File{{Path: "untracked.txt", Status: "??", Unstaged: true}}},
		{name: "not ready for review", status: agentwork.StatusRunning},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, store := newAgentTestHarness(t)
			var task agentwork.Task
			if test.status == agentwork.StatusReadyForReview {
				task = readyAgentReviewTask(t, h, store)
			} else {
				task = createAgentTestTask(t, h, store, agentwork.Task{Status: test.status})
			}
			calls := &callRecorder{}
			h.app.session = fakeSession{snapshot: h.app.snapshot, calls: calls}
			h.app.agents.review.open = true
			h.app.agents.review.taskID = task.ID
			h.app.agents.review.commits = []repository.Commit{{Hash: "agent-commit"}}
			h.app.agents.review.dirtyFiles = test.dirty

			if _, ok := h.app.agentAcceptEligibility(); ok {
				t.Fatal("dirty or non-review task was eligible for acceptance")
			}
			h.app.confirmAcceptAgentCommits()
			h.app.confirmMergeAgentTask()
			if h.app.confirm != nil || len(calls.all()) != 0 {
				t.Fatalf("ineligible accept started an operation: confirm=%v calls=%v", h.app.confirm != nil, calls.all())
			}
		})
	}
}
