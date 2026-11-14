package agentwork

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func validTask(repositoryID string) Task {
	return Task{
		RepositoryID: repositoryID,
		Title:        "Implement search",
		Request:      "Add search and keep the existing behavior.",
		BaseRef:      "main",
		BaseCommit:   "0123456789abcdef",
		Branch:       "agent/search",
		WorktreePath: "/work/search",
		Profile: Profile{
			Name:          "Test agent",
			LaunchCommand: "agent {worktree}",
			Environment:   map[string]string{"PORT": "3001"},
		},
	}
}

func TestTaskStorePersistsTaskAndRequest(t *testing.T) {
	store := testStore(t)
	store.now = func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.FixedZone("test", -7*3600)) }
	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	if !validTaskID(created.ID) || created.Status != StatusReady {
		t.Fatalf("created task has invalid identity/state: %#v", created)
	}
	if got := created.CreatedAt.Location(); got != time.UTC {
		t.Fatalf("created timestamp location = %v, want UTC", got)
	}
	requestPath, err := store.RequestFile(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	request, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(request) != created.Request {
		t.Fatalf("request = %q, want %q", request, created.Request)
	}

	tasks, err := store.List("local:/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != created.ID || tasks[0].BaseCommit != validTask("").BaseCommit {
		t.Fatalf("persisted tasks = %#v", tasks)
	}
	if other, err := store.List("local:/repo-other"); err != nil || len(other) != 0 {
		t.Fatalf("repository filter leaked tasks: %#v, %v", other, err)
	}
	read, err := store.Get(created.ID)
	if err != nil || read.Title != created.Title {
		t.Fatalf("Get = %#v, %v", read, err)
	}
}

func TestTaskStoreMutationIsDurableAndReturnsIndependentValues(t *testing.T) {
	store := testStore(t)
	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(created.ID, func(task *Task) error {
		task.Status = StatusRunning
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	code := 0
	updated, err := store.Update(created.ID, func(task *Task) error {
		task.Status = StatusExited
		task.ExitCode = &code
		task.Profile.Environment["PORT"] = "4001"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	code = 9
	updated.Profile.Environment["PORT"] = "5001"
	read, err := store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.ExitCode == nil || *read.ExitCode != 0 {
		t.Fatalf("persisted exit code = %v", read.ExitCode)
	}
	if got := read.Profile.Environment["PORT"]; got != "4001" {
		t.Fatalf("persisted environment = %q, want original mutation", got)
	}
	if !read.UpdatedAt.After(read.CreatedAt) && !read.UpdatedAt.Equal(read.CreatedAt) {
		t.Fatalf("invalid updated timestamp: created %v updated %v", read.CreatedAt, read.UpdatedAt)
	}
}

func TestTaskStorePersistsLastTestAcrossRestartAndClonesIt(t *testing.T) {
	store := testStore(t)
	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 13, 19, 0, 0, 0, time.UTC)
	running, err := store.Update(created.ID, func(task *Task) error {
		task.LastTest = &TestResult{
			Status: TestStatusRunning, Command: "go test ./...", StartedAt: started,
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != created.Status || running.LastTest == nil || running.LastTest.Status != TestStatusRunning {
		t.Fatalf("test start changed task state or was not saved: %#v", running)
	}

	// Returned values own their nested result and exit-code pointers.
	running.LastTest.Status = TestStatusFailed
	running.LastTest.Command = "changed by caller"
	finished := started.Add(12 * time.Second)
	code := 0
	passed, err := store.Update(created.ID, func(task *Task) error {
		task.LastTest.Status = TestStatusPassed
		task.LastTest.ExitCode = &code
		task.LastTest.FinishedAt = finished
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	code = 9
	passed.LastTest.ExitCode = &code
	passed.LastTest.Status = TestStatusFailed

	// Use a distinct Store instance to model reopening the durable data.
	restarted := &Store{dir: store.dir, now: time.Now}
	read, err := restarted.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.LastTest == nil || read.LastTest.Status != TestStatusPassed ||
		read.LastTest.Command != "go test ./..." || read.LastTest.ExitCode == nil ||
		*read.LastTest.ExitCode != 0 || !read.LastTest.StartedAt.Equal(started) ||
		!read.LastTest.FinishedAt.Equal(finished) {
		t.Fatalf("persisted test result = %#v", read.LastTest)
	}
	read.LastTest.Status = TestStatusRunning
	*read.LastTest.ExitCode = 23
	again, err := restarted.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.LastTest.Status != TestStatusPassed || *again.LastTest.ExitCode != 0 {
		t.Fatalf("Get shared its test result with the caller: %#v", again.LastTest)
	}
}

func TestTaskCheckpointStorePersistsAcrossRestartAndClonesHistory(t *testing.T) {
	store := testStore(t)
	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	capturedAt := time.Date(2026, 11, 13, 20, 0, 0, 0, time.FixedZone("test", -7*3600))
	store.now = func() time.Time { return capturedAt }
	updated, err := store.RecordCheckpoint(created.ID, Checkpoint{
		ID: "caller-cannot-choose-this", BaseCommit: "caller-cannot-choose-this-either",
		SnapshotID: "snapshot-one", HeadCommit: "head-one", Reason: "  Before accept  ",
		CreatedAt: capturedAt.Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Checkpoints) != 1 {
		t.Fatalf("checkpoints = %#v", updated.Checkpoints)
	}
	checkpoint := updated.Checkpoints[0]
	if !validTaskID(checkpoint.ID) || checkpoint.SnapshotID != "snapshot-one" ||
		checkpoint.BaseCommit != created.BaseCommit || checkpoint.HeadCommit != "head-one" ||
		checkpoint.Reason != "Before accept" || !checkpoint.CreatedAt.Equal(capturedAt) ||
		checkpoint.CreatedAt.Location() != time.UTC {
		t.Fatalf("recorded checkpoint = %#v", checkpoint)
	}
	if len(updated.Activity) != 1 || updated.Activity[0].Kind != ActivityCheckpoint ||
		!validTaskID(updated.Activity[0].ID) || updated.Activity[0].CheckpointID != checkpoint.ID ||
		updated.Activity[0].SnapshotID != checkpoint.SnapshotID || !updated.Activity[0].CreatedAt.Equal(capturedAt) {
		t.Fatalf("checkpoint activity = %#v", updated.Activity)
	}

	updated.Checkpoints[0].SnapshotID = "changed by caller"
	if _, err := store.Update(created.ID, func(task *Task) error {
		task.Checkpoints[0].HeadCommit = "rewritten by task update"
		task.Activity[0].SnapshotID = "rewritten by task update"
		task.Status = StatusRunning
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	restarted := &Store{dir: store.dir, now: time.Now}
	read, err := restarted.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Checkpoints) != 1 || read.Checkpoints[0].ID != checkpoint.ID ||
		read.Checkpoints[0].SnapshotID != "snapshot-one" || read.Checkpoints[0].HeadCommit != "head-one" {
		t.Fatalf("restarted checkpoint history = %#v", read.Checkpoints)
	}
	if len(read.Activity) != 2 || read.Activity[0].SnapshotID != "snapshot-one" ||
		read.Activity[1].Kind != ActivityStatus {
		t.Fatalf("restarted activity history = %#v", read.Activity)
	}
	read.Checkpoints[0].Reason = "changed clone"
	again, err := restarted.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Checkpoints[0].Reason != "Before accept" {
		t.Fatalf("Get shared checkpoint history with its caller: %#v", again.Checkpoints)
	}
}

func TestTaskCheckpointStoreRetainsNewest24Records(t *testing.T) {
	store := testStore(t)
	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	for index := range 30 {
		store.now = func() time.Time { return time.Date(2026, 11, 13, 20, 0, index, 0, time.UTC) }
		if _, err := store.RecordCheckpoint(created.ID, Checkpoint{
			SnapshotID: fmt.Sprintf("snapshot-%02d", index),
			HeadCommit: fmt.Sprintf("head-%02d", index),
			Reason:     fmt.Sprintf("Checkpoint %02d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	read, err := (&Store{dir: store.dir, now: time.Now}).Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Checkpoints) != checkpointHistoryKeep {
		t.Fatalf("checkpoint count = %d, want %d", len(read.Checkpoints), checkpointHistoryKeep)
	}
	ids := map[string]bool{}
	for index, checkpoint := range read.Checkpoints {
		generation := index + 6
		if checkpoint.SnapshotID != fmt.Sprintf("snapshot-%02d", generation) ||
			checkpoint.HeadCommit != fmt.Sprintf("head-%02d", generation) {
			t.Fatalf("checkpoint %d is not the expected retained record: %#v", index, checkpoint)
		}
		if ids[checkpoint.ID] {
			t.Fatalf("duplicate checkpoint identity %q", checkpoint.ID)
		}
		ids[checkpoint.ID] = true
	}
}

func TestTaskActivityStorePersistsClonesAndBoundsHistory(t *testing.T) {
	store := testStore(t)
	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 13, 21, 0, 0, 0, time.FixedZone("test", -7*3600))
	store.now = func() time.Time { return base }
	recorded, err := store.RecordActivity(created.ID, Activity{
		ID: "caller-cannot-choose-this", Kind: ActivityLaunch, CreatedAt: base.Add(-time.Hour),
		Command: "agent --prompt request.md", SessionID: " session-one ", Reference: " terminal:1 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.Activity) != 1 || !validTaskID(recorded.Activity[0].ID) ||
		!recorded.Activity[0].CreatedAt.Equal(base) || recorded.Activity[0].CreatedAt.Location() != time.UTC ||
		recorded.Activity[0].SessionID != "session-one" || recorded.Activity[0].Reference != "terminal:1" {
		t.Fatalf("recorded activity = %#v", recorded.Activity)
	}
	recorded.Activity[0].Command = "changed by caller"
	unchanged, err := store.Get(created.ID)
	if err != nil || unchanged.Activity[0].Command != "agent --prompt request.md" {
		t.Fatalf("activity result shared its history: %#v, %v", unchanged.Activity, err)
	}

	for index := 1; index <= 104; index++ {
		store.now = func() time.Time { return base.Add(time.Duration(index) * time.Minute) }
		if _, err := store.RecordActivity(created.ID, Activity{
			Kind: ActivityLaunch, Command: fmt.Sprintf("agent-%03d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	restarted := &Store{dir: store.dir, now: time.Now}
	read, err := restarted.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.Activity) != activityHistoryKeep || read.Activity[0].Command != "agent-005" ||
		read.Activity[len(read.Activity)-1].Command != "agent-104" {
		t.Fatalf("bounded restarted history = len %d, first %#v, last %#v", len(read.Activity), read.Activity[0], read.Activity[len(read.Activity)-1])
	}
	ids := map[string]bool{}
	for _, activity := range read.Activity {
		if ids[activity.ID] {
			t.Fatalf("duplicate activity identity %q", activity.ID)
		}
		ids[activity.ID] = true
	}

	if _, err := restarted.Update(created.ID, func(task *Task) error {
		task.Activity[0].Command = "rewritten by update"
		task.Activity = task.Activity[:1]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	read.Activity[0].Command = "changed clone"
	again, err := restarted.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Activity) != activityHistoryKeep || again.Activity[0].Command != "agent-005" {
		t.Fatalf("general update rewrote activity history: %#v", again.Activity[:1])
	}
}

func TestTaskStoreRecordsActualLifecycleAndTestTransitions(t *testing.T) {
	store := testStore(t)
	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 13, 22, 0, 0, 0, time.UTC)
	now := started
	store.now = func() time.Time { return now }
	if _, err := store.Update(created.ID, func(task *Task) error {
		task.Status = StatusRunning
		task.Activity = []Activity{{ID: "injected", Kind: ActivityResume}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	withoutChanges, err := store.Update(created.ID, func(task *Task) error {
		task.Summary = "No lifecycle or test transition."
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutChanges.Activity) != 1 || withoutChanges.Activity[0].Kind != ActivityStatus ||
		withoutChanges.Activity[0].StatusFrom != string(StatusReady) ||
		withoutChanges.Activity[0].StatusTo != string(StatusRunning) ||
		!withoutChanges.Activity[0].CreatedAt.Equal(started) {
		t.Fatalf("lifecycle activity = %#v", withoutChanges.Activity)
	}

	now = now.Add(time.Minute)
	if _, err := store.Update(created.ID, func(task *Task) error {
		task.LastTest = &TestResult{Status: TestStatusRunning, Command: "go test ./...", StartedAt: now}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	code := 0
	passed, err := store.Update(created.ID, func(task *Task) error {
		task.LastTest.Status = TestStatusPassed
		task.LastTest.ExitCode = &code
		task.LastTest.FinishedAt = now
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if passed.Status != StatusRunning || passed.LastTest.Status != TestStatusPassed {
		t.Fatalf("test history changed existing task/test outcomes: %#v", passed)
	}
	if len(passed.Activity) != 3 || passed.Activity[1].Kind != ActivityTest || passed.Activity[2].Kind != ActivityTest {
		t.Fatalf("automatic activity = %#v", passed.Activity)
	}
	startEvent, passEvent := passed.Activity[1], passed.Activity[2]
	if startEvent.StatusFrom != "" || startEvent.StatusTo != string(TestStatusRunning) ||
		passEvent.StatusFrom != string(TestStatusRunning) || passEvent.StatusTo != string(TestStatusPassed) ||
		startEvent.Command != "go test ./..." || passEvent.Command != "go test ./..." ||
		!startEvent.CreatedAt.Equal(started.Add(2*time.Minute)) || !passEvent.CreatedAt.Equal(now) {
		t.Fatalf("test transition activity = start %#v, pass %#v", startEvent, passEvent)
	}
}

func TestTaskStoreRejectsFailedMutationWithoutChangingRequest(t *testing.T) {
	store := testStore(t)
	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("stop")
	if _, err := store.Update(created.ID, func(task *Task) error {
		task.Request = "discard this"
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("Update error = %v, want sentinel", err)
	}
	read, err := store.Get(created.ID)
	if err != nil || read.Request != created.Request {
		t.Fatalf("failed mutation changed task: %#v, %v", read, err)
	}
	requestPath, _ := store.RequestFile(created.ID)
	request, err := os.ReadFile(requestPath)
	if err != nil || string(request) != created.Request {
		t.Fatalf("failed mutation changed request file: %q, %v", request, err)
	}
}

func TestTaskStoreSerializesIndependentCreators(t *testing.T) {
	store := testStore(t)
	const count = 12
	var wait sync.WaitGroup
	errorsSeen := make(chan error, count)
	ids := make(chan string, count)
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			created, err := store.Create(validTask("local:/repo"))
			if err != nil {
				errorsSeen <- err
				return
			}
			ids <- created.ID
		}()
	}
	wait.Wait()
	close(errorsSeen)
	close(ids)
	for err := range errorsSeen {
		t.Error(err)
	}
	tasks, err := store.List("local:/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != count || len(ids) != count {
		t.Fatalf("stored %d tasks and returned %d IDs, want %d", len(tasks), len(ids), count)
	}
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != count {
		t.Fatalf("received duplicate task IDs: %#v", unique)
	}
}

func TestTaskStoreDoesNotClobberDamagedData(t *testing.T) {
	store := testStore(t)
	path := filepath.Join(store.dir, "agent-tasks.json")
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"tasks":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(validTask("local:/repo")); err == nil || !strings.Contains(err.Error(), "read agent tasks") {
		t.Fatalf("Create error = %v, want malformed-store error", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"version":1,"tasks":[` {
		t.Fatalf("damaged source was overwritten: %q", data)
	}
}

func TestTaskStoreRejectsUnsafeOrImplicitTargets(t *testing.T) {
	store := testStore(t)
	invalid := validTask("local:/repo")
	invalid.BaseCommit = ""
	if _, err := store.Create(invalid); err == nil {
		t.Fatal("Create accepted a missing base commit")
	}
	if _, err := store.Get("../escape"); err == nil {
		t.Fatal("Get accepted an unsafe task ID")
	}
	if _, err := store.RequestFile("../escape"); err == nil {
		t.Fatal("RequestFile accepted an unsafe task ID")
	}
}
