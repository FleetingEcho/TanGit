package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentCheckpointOpener struct {
	session repository.Session
	calls   *callRecorder
}

func (o agentCheckpointOpener) Init(context.Context, string) error { return nil }
func (o agentCheckpointOpener) Open(_ context.Context, path string) (repository.Session, error) {
	o.calls.record("open-checkpoint:" + path)
	return o.session, nil
}

type agentCheckpointSession struct {
	fakeSession
	calls         *callRecorder
	head          string
	checkpointErr error
	restoreErr    error
	checkpoints   int
	closed        int
	restored      []repository.WorkSnapshot
}

func (s *agentCheckpointSession) Snapshot(context.Context) (repository.Snapshot, error) {
	snapshot := s.fakeSession.snapshot
	snapshot.HeadHash = s.head
	return snapshot, nil
}

func (s *agentCheckpointSession) CreateCheckpoint(_ context.Context, reason string) (repository.WorkSnapshot, error) {
	s.calls.record("checkpoint:" + reason)
	if s.checkpointErr != nil {
		return repository.WorkSnapshot{}, s.checkpointErr
	}
	s.checkpoints++
	id := fmt.Sprintf("snapshot-%d", s.checkpoints)
	return repository.WorkSnapshot{
		ID: id, Hash: id, Kind: repository.CheckpointSnapshotKind, Name: reason,
	}, nil
}

func (s *agentCheckpointSession) RestoreWorkSnapshot(_ context.Context, snapshot repository.WorkSnapshot) error {
	s.calls.record("restore-snapshot:" + snapshot.ID)
	s.restored = append(s.restored, snapshot)
	return s.restoreErr
}

func (s *agentCheckpointSession) Close() error {
	s.closed++
	return nil
}

func TestAgentCreateCheckpointUsesExplicitTargetAndActualHead(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{})
	h.app.agents.selectedTaskID = "not-the-checkpoint-target"
	calls := &callRecorder{}
	target := &agentCheckpointSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: task.WorktreePath}},
		calls:       calls, head: "actual-task-head",
	}
	h.app.opener = agentCheckpointOpener{session: target, calls: calls}
	mainCalls := &callRecorder{}
	h.app.session = fakeSession{snapshot: h.app.snapshot, calls: mainCalls}
	h.app.snapshot.Root = "/work/project"
	h.app.snapshot.HeadHash = "main-repository-head"

	h.app.createAgentCheckpoint(task.ID, "Before experiment")
	h.waitFor(t, "the durable task checkpoint", func() bool {
		current, err := store.Get(task.ID)
		return err == nil && len(current.Checkpoints) == 1 && !h.app.agents.checkpointBusy[task.ID]
	})
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := stored.Checkpoints[0]
	if checkpoint.SnapshotID != "snapshot-1" || checkpoint.BaseCommit != task.BaseCommit ||
		checkpoint.HeadCommit != "actual-task-head" || checkpoint.Reason != "Before experiment" ||
		checkpoint.ID == "" || checkpoint.CreatedAt.IsZero() {
		t.Fatalf("captured checkpoint = %#v", checkpoint)
	}
	if got, want := calls.all(), []string{
		"open-checkpoint:" + task.WorktreePath,
		"checkpoint:Before experiment",
	}; !slices.Equal(got, want) {
		t.Fatalf("checkpoint calls = %v, want %v", got, want)
	}
	if len(mainCalls.all()) != 0 || target.closed != 1 {
		t.Fatalf("checkpoint touched the main session or leaked ownership: main=%v closes=%d", mainCalls.all(), target.closed)
	}
	if h.app.snapshot.Root != "/work/project" || h.app.snapshot.HeadHash != "main-repository-head" {
		t.Fatalf("checkpoint switched the current repository: %#v", h.app.snapshot)
	}
}

func TestAgentRestoreCheckpointRecordsSafetyBeforeRestoringWIP(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{})
	withCheckpoint, err := store.RecordCheckpoint(task.ID, agentwork.Checkpoint{
		SnapshotID: "original-snapshot", HeadCommit: "captured-head", Reason: "Before experiment",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.app.agents.tasks[0] = withCheckpoint.Clone()
	original := withCheckpoint.Checkpoints[0]
	calls := &callRecorder{}
	target := &agentCheckpointSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: task.WorktreePath}},
		calls:       calls, head: "captured-head",
	}
	h.app.opener = agentCheckpointOpener{session: target, calls: calls}

	h.app.restoreAgentCheckpoint(task.ID, original.ID)
	h.waitFor(t, "the safety checkpoint and WIP restore", func() bool {
		current, err := store.Get(task.ID)
		return err == nil && len(current.Checkpoints) == 2 && len(target.restored) == 1 &&
			!h.app.agents.checkpointBusy[task.ID]
	})
	safetyReason := "Before restore " + agentShortHash(original.ID)
	if got, want := calls.all(), []string{
		"open-checkpoint:" + task.WorktreePath,
		"checkpoint:" + safetyReason,
		"restore-snapshot:original-snapshot",
	}; !slices.Equal(got, want) {
		t.Fatalf("restore calls = %v, want safety capture and durable write before restore %v", got, want)
	}
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	safety := stored.Checkpoints[1]
	if safety.SnapshotID != "snapshot-1" || safety.HeadCommit != "captured-head" ||
		safety.BaseCommit != task.BaseCommit || safety.Reason != safetyReason {
		t.Fatalf("safety checkpoint = %#v", safety)
	}
	restored := target.restored[0]
	if restored.ID != "original-snapshot" || restored.Hash != "original-snapshot" ||
		restored.Kind != repository.CheckpointSnapshotKind || restored.Name != "Before experiment" {
		t.Fatalf("restored snapshot = %#v", restored)
	}
	if target.closed != 1 {
		t.Fatalf("restore closed its owned target %d times", target.closed)
	}
}

func TestAgentRestoreCheckpointRefusesDivergentHeadBeforeWrites(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{})
	withCheckpoint, err := store.RecordCheckpoint(task.ID, agentwork.Checkpoint{
		SnapshotID: "original-snapshot", HeadCommit: "captured-head", Reason: "Before experiment",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.app.agents.tasks[0] = withCheckpoint.Clone()
	calls := &callRecorder{}
	target := &agentCheckpointSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: task.WorktreePath}},
		calls:       calls, head: "newer-head",
	}
	h.app.opener = agentCheckpointOpener{session: target, calls: calls}

	h.app.restoreAgentCheckpoint(task.ID, withCheckpoint.Checkpoints[0].ID)
	h.waitFor(t, "the newer-work refusal", func() bool {
		return !h.app.agents.checkpointBusy[task.ID]
	})
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Checkpoints) != 1 {
		t.Fatalf("divergent restore wrote checkpoint history: %#v", stored.Checkpoints)
	}
	if got, want := calls.all(), []string{"open-checkpoint:" + task.WorktreePath}; !slices.Equal(got, want) {
		t.Fatalf("divergent restore performed writes: %v", got)
	}
	texts := strings.Join(h.texts(), "\n")
	if !strings.Contains(texts, "Checkpoint not restored") || !strings.Contains(texts, "Newer work") ||
		!strings.Contains(texts, "No changes were made") {
		t.Fatalf("divergent restore notification is unclear: %q", texts)
	}
	if len(target.restored) != 0 || target.closed != 1 {
		t.Fatalf("divergent restore changed work or leaked its target: restored=%v closes=%d", target.restored, target.closed)
	}
}

func TestAgentCreateCheckpointFailureDoesNotRunContinuation(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{})
	calls := &callRecorder{}
	target := &agentCheckpointSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: task.WorktreePath}},
		calls:       calls, head: "task-head", checkpointErr: errors.New("capture refused"),
	}
	h.app.opener = agentCheckpointOpener{session: target, calls: calls}
	continued := false

	h.app.createAgentCheckpoint(task.ID, "Before accept", func(err error) {
		continued = err == nil
	})
	h.waitFor(t, "the failed checkpoint continuation guard", func() bool {
		return !h.app.agents.checkpointBusy[task.ID]
	})
	stored, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if continued || len(stored.Checkpoints) != 0 {
		t.Fatalf("failed capture continued or was recorded: continued=%t checkpoints=%#v", continued, stored.Checkpoints)
	}
}
