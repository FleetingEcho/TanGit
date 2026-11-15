package agentwork

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

func TestTaskDependenciesAndPhase3ProfilePersistAndClone(t *testing.T) {
	store := testStore(t)
	dependency, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	input := validTask("local:/repo")
	input.Title = "Dependent work"
	input.DependsOn = []string{dependency.ID}
	input.Profile.RunCommand = "npm run dev -- --port {port}"
	input.Profile.SparsePaths = []string{"cmd", "internal/agentwork"}
	input.Profile.IgnoredFiles = []string{"config/local.json"}
	input.Profile.Environment = map[string]string{"APP_ENV": "development"}
	input.Profile.Resources = Resources{
		DevelopmentPort: 3100,
		DatabaseName:    "tangit_task_2",
		CacheDirectory:  filepath.Join("cache", "task-2"),
	}
	created, err := store.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	input.DependsOn[0] = "changed by caller"
	input.Profile.SparsePaths[0] = "changed by caller"
	input.Profile.IgnoredFiles[0] = ".env"
	input.Profile.Environment["APP_ENV"] = "changed by caller"
	created.DependsOn[0] = "changed returned value"
	created.Profile.SparsePaths[0] = "changed returned value"
	created.Profile.IgnoredFiles[0] = "changed returned value"
	created.Profile.Environment["APP_ENV"] = "changed returned value"
	created.Profile.Resources.DevelopmentPort = 1

	restarted := &Store{dir: store.dir, now: time.Now}
	read, err := restarted.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(read.DependsOn) != 1 || read.DependsOn[0] != dependency.ID {
		t.Fatalf("persisted dependencies = %#v, want %q", read.DependsOn, dependency.ID)
	}
	if read.Profile.RunCommand != "npm run dev -- --port {port}" ||
		!slices.Equal(read.Profile.SparsePaths, []string{"cmd", "internal/agentwork"}) ||
		!slices.Equal(read.Profile.IgnoredFiles, []string{"config/local.json"}) ||
		read.Profile.Environment["APP_ENV"] != "development" ||
		read.Profile.Resources.DevelopmentPort != 3100 ||
		read.Profile.Resources.DatabaseName != "tangit_task_2" ||
		read.Profile.Resources.CacheDirectory != filepath.Join("cache", "task-2") {
		t.Fatalf("persisted Phase-3 profile = %#v", read.Profile)
	}
	read.DependsOn[0] = "changed clone"
	read.Profile.SparsePaths[0] = "changed clone"
	read.Profile.IgnoredFiles[0] = "changed clone"
	read.Profile.Environment["APP_ENV"] = "changed clone"
	again, err := restarted.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.DependsOn[0] != dependency.ID || again.Profile.SparsePaths[0] != "cmd" ||
		again.Profile.IgnoredFiles[0] != "config/local.json" || again.Profile.Environment["APP_ENV"] != "development" {
		t.Fatalf("Get shared Phase-3 fields with its caller: %#v", again)
	}
}

func TestTaskOrderIsDependencyFirstDeterministicAndIncludesArchived(t *testing.T) {
	store := testStore(t)
	first, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	secondInput := validTask("local:/repo")
	secondInput.DependsOn = []string{first.ID}
	second, err := store.Create(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	archivedInput := validTask("local:/repo")
	archivedInput.Status = StatusArchived
	archived, err := store.Create(archivedInput)
	if err != nil {
		t.Fatal(err)
	}

	positions := map[string]int{}
	ordered, err := TaskOrder([]Task{second, archived, first})
	if err != nil {
		t.Fatal(err)
	}
	if len(ordered) != 3 {
		t.Fatalf("ordered task count = %d, want 3", len(ordered))
	}
	for index, task := range ordered {
		positions[task.ID] = index
	}
	if positions[first.ID] >= positions[second.ID] {
		t.Fatalf("dependency order = %#v", ordered)
	}
	if _, ok := positions[archived.ID]; !ok {
		t.Fatal("TaskOrder omitted an archived task")
	}
	reversed, err := TaskOrder([]Task{first, archived, second})
	if err != nil {
		t.Fatal(err)
	}
	for index := range ordered {
		if ordered[index].ID != reversed[index].ID {
			t.Fatalf("order depends on input order: first %#v, second %#v", ordered, reversed)
		}
	}
	ordered[0].DependsOn = append(ordered[0].DependsOn, "changed clone")
	read, err := store.Get(ordered[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(read.DependsOn, ordered[0].DependsOn) {
		t.Fatal("TaskOrder returned tasks sharing their dependencies with the store")
	}
}

func TestTaskStoreRejectsInvalidDependenciesAtomically(t *testing.T) {
	store := testStore(t)
	first, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	secondInput := validTask("local:/repo")
	secondInput.DependsOn = []string{first.ID}
	second, err := store.Create(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.Create(validTask("local:/other"))
	if err != nil {
		t.Fatal(err)
	}
	missing := strings.Repeat("f", 32)
	for missing == first.ID || missing == second.ID || missing == other.ID {
		missing = strings.Repeat("e", 32)
	}

	tests := []struct {
		name         string
		dependencies []string
		want         string
	}{
		{name: "self", dependencies: []string{first.ID}, want: "itself"},
		{name: "duplicate", dependencies: []string{second.ID, second.ID}, want: "duplicate"},
		{name: "missing", dependencies: []string{missing}, want: "missing"},
		{name: "malformed", dependencies: []string{"../escape"}, want: "invalid dependency ID"},
		{name: "cross repository", dependencies: []string{other.ID}, want: "another repository"},
		{name: "cycle", dependencies: []string{second.ID}, want: "cycle"},
	}
	taskPath := filepath.Join(store.dir, "agent-tasks.json")
	before, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	requestPath, _ := store.RequestFile(first.ID)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.Update(first.ID, func(task *Task) error {
				task.DependsOn = test.dependencies
				task.Request = "must not be saved"
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Update error = %v, want text %q", err, test.want)
			}
			after, readErr := os.ReadFile(taskPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(after) != string(before) {
				t.Fatal("invalid dependency update changed the task store")
			}
			request, readErr := os.ReadFile(requestPath)
			if readErr != nil || string(request) != first.Request {
				t.Fatalf("invalid dependency update changed request = %q, %v", request, readErr)
			}
		})
	}

	invalidCreate := validTask("local:/repo")
	invalidCreate.DependsOn = []string{missing}
	if _, err := store.Create(invalidCreate); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Create missing dependency error = %v", err)
	}
	invalidCreate.DependsOn = []string{other.ID}
	if _, err := store.Create(invalidCreate); err == nil || !strings.Contains(err.Error(), "another repository") {
		t.Fatalf("Create cross-repository dependency error = %v", err)
	}
}

func TestTaskOrderReportsMissingAndCycleErrors(t *testing.T) {
	first := validTask("local:/repo")
	first.ID = strings.Repeat("1", 32)
	second := validTask("local:/repo")
	second.ID = strings.Repeat("2", 32)
	second.DependsOn = []string{strings.Repeat("3", 32)}
	if _, err := TaskOrder([]Task{first, second}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing dependency error = %v", err)
	}
	first.DependsOn = []string{second.ID}
	second.DependsOn = []string{first.ID}
	if _, err := TaskOrder([]Task{first, second}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestTaskStoreValidatesPhase3ProfileResourcesAndPaths(t *testing.T) {
	store := testStore(t)
	longPath := strings.Repeat("x", maxProfilePathLength+1)
	tooManyPaths := make([]string, maxProfilePaths+1)
	for index := range tooManyPaths {
		tooManyPaths[index] = "file.txt"
	}
	tests := []struct {
		name   string
		mutate func(*Profile)
	}{
		{name: "empty sparse path", mutate: func(p *Profile) { p.SparsePaths = []string{" "} }},
		{name: "absolute sparse path", mutate: func(p *Profile) { p.SparsePaths = []string{string(filepath.Separator) + "etc"} }},
		{name: "escaping sparse path", mutate: func(p *Profile) { p.SparsePaths = []string{filepath.Join("a", "..", "..", "outside")} }},
		{name: "empty ignored file", mutate: func(p *Profile) { p.IgnoredFiles = []string{""} }},
		{name: "absolute ignored file", mutate: func(p *Profile) { p.IgnoredFiles = []string{string(filepath.Separator) + "secret"} }},
		{name: "escaping ignored file", mutate: func(p *Profile) { p.IgnoredFiles = []string{".."} }},
		{name: "path count", mutate: func(p *Profile) { p.SparsePaths = tooManyPaths }},
		{name: "path size", mutate: func(p *Profile) { p.IgnoredFiles = []string{longPath} }},
		{name: "negative port", mutate: func(p *Profile) { p.Resources.DevelopmentPort = -1 }},
		{name: "large port", mutate: func(p *Profile) { p.Resources.DevelopmentPort = 65536 }},
		{name: "blank database", mutate: func(p *Profile) { p.Resources.DatabaseName = " \t " }},
		{name: "database control", mutate: func(p *Profile) { p.Resources.DatabaseName = "db\x00name" }},
		{name: "database size", mutate: func(p *Profile) { p.Resources.DatabaseName = strings.Repeat("d", maxDatabaseNameLength+1) }},
		{name: "blank cache", mutate: func(p *Profile) { p.Resources.CacheDirectory = "  " }},
		{name: "escaping cache", mutate: func(p *Profile) { p.Resources.CacheDirectory = filepath.Join("..", "cache") }},
	}
	for _, test := range tests {
		t.Run("create "+test.name, func(t *testing.T) {
			task := validTask("local:/repo")
			test.mutate(&task.Profile)
			if _, err := store.Create(task); err == nil {
				t.Fatal("Create accepted an invalid Phase-3 profile")
			}
		})
	}

	created, err := store.Create(validTask("local:/repo"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run("update "+test.name, func(t *testing.T) {
			if _, err := store.Update(created.ID, func(task *Task) error {
				test.mutate(&task.Profile)
				return nil
			}); err == nil {
				t.Fatal("Update accepted an invalid Phase-3 profile")
			}
			after, err := store.Get(created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after.Profile.Resources != before.Profile.Resources ||
				!slices.Equal(after.Profile.SparsePaths, before.Profile.SparsePaths) ||
				!slices.Equal(after.Profile.IgnoredFiles, before.Profile.IgnoredFiles) {
				t.Fatalf("invalid update changed profile: %#v", after.Profile)
			}
		})
	}

	for _, cache := range []string{filepath.Join("relative", "cache"), filepath.Join(string(filepath.Separator), "absolute", "cache")} {
		task := validTask("local:/repo")
		task.Profile.Resources = Resources{DevelopmentPort: 65535, DatabaseName: "development", CacheDirectory: cache}
		task.Profile.SparsePaths = []string{filepath.Join("dir", "..", "allowed")}
		if _, err := store.Create(task); err != nil {
			t.Fatalf("Create rejected valid resources/cache %q: %v", cache, err)
		}
	}
}

func TestLegacyTaskWithoutDependenciesOrPhase3ProfileRemainsReadable(t *testing.T) {
	store := testStore(t)
	id := strings.Repeat("a", 32)
	legacy := `{"version":1,"tasks":[{"id":"` + id + `","repositoryId":"local:/repo","title":"Legacy","baseCommit":"abc","worktreePath":"/tmp/legacy","profile":{"launchCommand":"agent"},"status":"ready","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}]}`
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if read.DependsOn != nil || read.Profile.SparsePaths != nil || read.Profile.IgnoredFiles != nil || read.Profile.Resources != (Resources{}) {
		t.Fatalf("legacy task gained nonzero Phase-3 fields: %#v", read)
	}
}
