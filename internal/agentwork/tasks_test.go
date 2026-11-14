package agentwork

import (
	"errors"
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
