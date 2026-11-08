// Package agentwork owns durable coding-agent tasks independently of Git and UI.
package agentwork

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	storeVersion          = 1
	checkpointHistoryKeep = 24
	activityHistoryKeep   = 100
)

// Status describes what TanGit knows, not what it infers from terminal text.
// In particular, a zero exit is Exited, not ReadyForReview: only an adapter or
// a person can say that an agent considers its task complete.
type Status string

const (
	StatusReady          Status = "ready"
	StatusRunning        Status = "running"
	StatusNeedsInput     Status = "needs_input"
	StatusTesting        Status = "testing"
	StatusExited         Status = "exited"
	StatusReadyForReview Status = "ready_for_review"
	StatusBlocked        Status = "blocked"
	StatusFailed         Status = "failed"
	StatusDone           Status = "done"
	StatusArchived       Status = "archived"
)

func (s Status) valid() bool {
	switch s {
	case StatusReady, StatusRunning, StatusNeedsInput, StatusTesting, StatusExited,
		StatusReadyForReview, StatusBlocked, StatusFailed, StatusDone, StatusArchived:
		return true
	default:
		return false
	}
}

// Profile is a user-trusted command template. Placeholders are expanded as
// quoted shell arguments by the UI's platform-aware command runner.
type Profile struct {
	Name          string            `json:"name,omitempty"`
	LaunchCommand string            `json:"launchCommand,omitempty"`
	ResumeCommand string            `json:"resumeCommand,omitempty"`
	SetupCommand  string            `json:"setupCommand,omitempty"`
	TestCommand   string            `json:"testCommand,omitempty"`
	Environment   map[string]string `json:"environment,omitempty"`
}

// TestStatus is the recorded outcome of the most recent test command. It is
// deliberately independent of the task lifecycle: passing tests do not imply
// that a task is ready for review.
type TestStatus string

const (
	TestStatusRunning TestStatus = "running"
	TestStatusPassed  TestStatus = "passed"
	TestStatusFailed  TestStatus = "failed"
)

// TestResult captures one invocation of the profile's test command. ExitCode
// and FinishedAt stay unset while the command is running.
type TestResult struct {
	Status     TestStatus `json:"status"`
	Command    string     `json:"command"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt time.Time  `json:"finishedAt,omitempty"`
}

// Clone prevents callers from sharing the optional exit-code pointer.
func (r TestResult) Clone() TestResult {
	clone := r
	if r.ExitCode != nil {
		code := *r.ExitCode
		clone.ExitCode = &code
	}
	return clone
}

// Checkpoint records a task-owned snapshot without changing task readiness.
// The store assigns its identity and base commit; SnapshotID is the immutable
// repository snapshot returned when HeadCommit and the work in progress were
// captured together.
type Checkpoint struct {
	ID         string    `json:"id"`
	SnapshotID string    `json:"snapshotId"`
	BaseCommit string    `json:"baseCommit"`
	HeadCommit string    `json:"headCommit"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"createdAt"`
}

// ActivityKind identifies a durable task event without deriving lifecycle or
// review readiness from process output.
type ActivityKind string

const (
	ActivityStatus     ActivityKind = "status"
	ActivityTest       ActivityKind = "test"
	ActivityCheckpoint ActivityKind = "checkpoint"
	ActivityLaunch     ActivityKind = "launch"
	ActivityResume     ActivityKind = "resume"
)

func (k ActivityKind) valid() bool {
	switch k {
	case ActivityStatus, ActivityTest, ActivityCheckpoint, ActivityLaunch, ActivityResume:
		return true
	default:
		return false
	}
}

// Activity is one append-only task event. StatusFrom and StatusTo contain task
// lifecycle values for status events and TestStatus values for test events.
// Reference optionally identifies a terminal or other artifact; checkpoints
// also retain their stable checkpoint and repository snapshot IDs.
type Activity struct {
	ID           string       `json:"id"`
	Kind         ActivityKind `json:"kind"`
	CreatedAt    time.Time    `json:"createdAt"`
	StatusFrom   string       `json:"statusFrom,omitempty"`
	StatusTo     string       `json:"statusTo,omitempty"`
	Command      string       `json:"command,omitempty"`
	SessionID    string       `json:"sessionId,omitempty"`
	Reference    string       `json:"reference,omitempty"`
	CheckpointID string       `json:"checkpointId,omitempty"`
	SnapshotID   string       `json:"snapshotId,omitempty"`
}

// Task is the durable identity shared by worktrees, runs, reviews and later
// checkpoints. RepositoryID is an opaque workspace identity, not a branch or
// currently selected tab. Paths remain native paths of their local/SSH host.
type Task struct {
	ID           string       `json:"id"`
	RepositoryID string       `json:"repositoryId"`
	Title        string       `json:"title"`
	Request      string       `json:"request"`
	BaseRef      string       `json:"baseRef"`
	BaseCommit   string       `json:"baseCommit"`
	Branch       string       `json:"branch"`
	WorktreePath string       `json:"worktreePath"`
	Profile      Profile      `json:"profile"`
	Status       Status       `json:"status"`
	SessionID    string       `json:"sessionId,omitempty"`
	Summary      string       `json:"summary,omitempty"`
	ExitCode     *int         `json:"exitCode,omitempty"`
	LastTest     *TestResult  `json:"lastTest,omitempty"`
	Checkpoints  []Checkpoint `json:"checkpoints,omitempty"`
	Activity     []Activity   `json:"activity,omitempty"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	LastActiveAt time.Time    `json:"lastActiveAt,omitempty"`
	ArchivedAt   time.Time    `json:"archivedAt,omitempty"`
}

func canTransition(from, to Status) bool {
	if from == to {
		return true
	}
	allowed := map[Status][]Status{
		StatusReady:          {StatusRunning, StatusBlocked, StatusArchived},
		StatusRunning:        {StatusNeedsInput, StatusTesting, StatusExited, StatusReadyForReview, StatusBlocked, StatusFailed, StatusArchived},
		StatusNeedsInput:     {StatusRunning, StatusTesting, StatusReadyForReview, StatusBlocked, StatusFailed, StatusArchived},
		StatusTesting:        {StatusRunning, StatusExited, StatusReadyForReview, StatusBlocked, StatusFailed, StatusArchived},
		StatusExited:         {StatusRunning, StatusReadyForReview, StatusBlocked, StatusDone, StatusArchived},
		StatusReadyForReview: {StatusRunning, StatusTesting, StatusDone, StatusBlocked, StatusArchived},
		StatusBlocked:        {StatusReady, StatusRunning, StatusReadyForReview, StatusFailed, StatusArchived},
		StatusFailed:         {StatusReady, StatusRunning, StatusTesting, StatusReadyForReview, StatusArchived},
		StatusDone:           {StatusRunning, StatusArchived},
	}
	return slices.Contains(allowed[from], to)
}

func (t Task) validate() error {
	if strings.TrimSpace(t.RepositoryID) == "" {
		return errors.New("agent task repository is required")
	}
	if strings.TrimSpace(t.WorktreePath) == "" {
		return errors.New("agent task worktree is required")
	}
	if strings.TrimSpace(t.Title) == "" {
		return errors.New("agent task title is required")
	}
	if strings.TrimSpace(t.BaseCommit) == "" {
		return errors.New("agent task base commit is required")
	}
	if !t.Status.valid() {
		return fmt.Errorf("invalid agent task status %q", t.Status)
	}
	return nil
}

// Clone prevents callers from sharing mutable maps or nested result pointers.
func (t Task) Clone() Task {
	clone := t
	if t.Profile.Environment != nil {
		clone.Profile.Environment = make(map[string]string, len(t.Profile.Environment))
		for key, value := range t.Profile.Environment {
			clone.Profile.Environment[key] = value
		}
	}
	if t.ExitCode != nil {
		code := *t.ExitCode
		clone.ExitCode = &code
	}
	if t.LastTest != nil {
		result := t.LastTest.Clone()
		clone.LastTest = &result
	}
	clone.Checkpoints = append([]Checkpoint(nil), t.Checkpoints...)
	clone.Activity = append([]Activity(nil), t.Activity...)
	return clone
}

type diskStore struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// Store serializes updates from every repository tab sharing a user-data
// directory. Mutations reload while holding the same lock, so independent tabs
// cannot silently overwrite each other's tasks with stale in-memory slices.
type Store struct {
	dir string
	mu  sync.Mutex
	now func() time.Time
}

var (
	storesMu sync.Mutex
	stores   = map[string]*Store{}
)

// OpenStore returns the process-wide store for dir. Tasks and their request
// files stay in TanGit's user-data directory, never inside repository worktrees.
func OpenStore(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("agent task data directory is required")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve agent task data directory: %w", err)
	}
	key := filepath.Clean(absolute)
	storesMu.Lock()
	defer storesMu.Unlock()
	if store := stores[key]; store != nil {
		return store, nil
	}
	store := &Store{dir: key, now: time.Now}
	stores[key] = store
	return store, nil
}

func (s *Store) path() string             { return filepath.Join(s.dir, "agent-tasks.json") }
func (s *Store) taskDir(id string) string { return filepath.Join(s.dir, "agent-tasks", id) }

// RequestFile returns the durable prompt-file path used by command profiles.
func (s *Store) RequestFile(taskID string) (string, error) {
	if !validTaskID(taskID) {
		return "", errors.New("invalid agent task ID")
	}
	return filepath.Join(s.taskDir(taskID), "request.md"), nil
}

func validTaskID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func newTaskID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create agent task ID: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

// List returns independent task values, newest activity first. An exact
// repository ID selects one family; an empty ID powers the global dashboard by
// returning every repository without requiring any of them to be open.
func (s *Store) List(repositoryID string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	disk, err := s.read()
	if err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(disk.Tasks))
	for _, task := range disk.Tasks {
		if repositoryID == "" || task.RepositoryID == repositoryID {
			tasks = append(tasks, task.Clone())
		}
	}
	slices.SortStableFunc(tasks, func(left, right Task) int {
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			if left.UpdatedAt.After(right.UpdatedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(left.ID, right.ID)
	})
	return tasks, nil
}

// Get resolves one explicit target and returns an independent value.
func (s *Store) Get(taskID string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	disk, err := s.read()
	if err != nil {
		return Task{}, err
	}
	_, task, err := findTask(disk.Tasks, taskID)
	return task, err
}

// Create validates and persists a complete new task and its request atomically
// with respect to other Store mutations. Neither a worktree nor a Git ref is
// created here; the app performs those operations before calling Create.
func (s *Store) Create(task Task) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task.ID != "" {
		return Task{}, errors.New("new agent task must not have an ID")
	}
	if task.Status == "" {
		task.Status = StatusReady
	}
	if err := task.validate(); err != nil {
		return Task{}, err
	}
	id, err := newTaskID()
	if err != nil {
		return Task{}, err
	}
	now := s.now().UTC()
	task.ID, task.CreatedAt, task.UpdatedAt = id, now, now
	task.LastActiveAt = now
	// Activity identity and timestamps belong to subsequent store operations.
	task.Activity = nil
	task = task.Clone()

	disk, err := s.read()
	if err != nil {
		return Task{}, err
	}
	disk.Tasks = append(disk.Tasks, task)
	requestPath, _ := s.RequestFile(id)
	if err := os.MkdirAll(filepath.Dir(requestPath), 0o700); err != nil {
		return Task{}, fmt.Errorf("create agent task directory: %w", err)
	}
	if err := writeFileAtomic(requestPath, []byte(task.Request), 0o600); err != nil {
		return Task{}, fmt.Errorf("save agent task request: %w", err)
	}
	if err := s.write(disk); err != nil {
		return Task{}, err
	}
	return task.Clone(), nil
}

func newActivity(kind ActivityKind, createdAt time.Time) (Activity, error) {
	id, err := newTaskID()
	if err != nil {
		return Activity{}, fmt.Errorf("create agent activity ID: %w", err)
	}
	return Activity{ID: id, Kind: kind, CreatedAt: createdAt}, nil
}

func appendActivity(history []Activity, activity Activity) []Activity {
	history = append(history, activity)
	if len(history) > activityHistoryKeep {
		history = history[len(history)-activityHistoryKeep:]
	}
	return append([]Activity(nil), history...)
}

func testActivityStatus(result *TestResult) string {
	if result == nil {
		return ""
	}
	return string(result.Status)
}

func automaticActivities(previous, updated Task, now time.Time) ([]Activity, error) {
	var activities []Activity
	if previous.Status != updated.Status {
		activity, err := newActivity(ActivityStatus, now)
		if err != nil {
			return nil, err
		}
		activity.StatusFrom = string(previous.Status)
		activity.StatusTo = string(updated.Status)
		activities = append(activities, activity)
	}
	if testActivityStatus(previous.LastTest) != testActivityStatus(updated.LastTest) {
		activity, err := newActivity(ActivityTest, now)
		if err != nil {
			return nil, err
		}
		activity.StatusFrom = testActivityStatus(previous.LastTest)
		activity.StatusTo = testActivityStatus(updated.LastTest)
		if updated.LastTest != nil {
			activity.Command = updated.LastTest.Command
		} else if previous.LastTest != nil {
			activity.Command = previous.LastTest.Command
		}
		activities = append(activities, activity)
	}
	return activities, nil
}

// Update applies an explicit-target mutation as one durable read/modify/write.
// Returning an error from mutate leaves both the task and request unchanged.
func (s *Store) Update(taskID string, mutate func(*Task) error) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	disk, err := s.read()
	if err != nil {
		return Task{}, err
	}
	index, previous, err := findTask(disk.Tasks, taskID)
	if err != nil {
		return Task{}, err
	}
	updated := previous.Clone()
	if mutate != nil {
		if err := mutate(&updated); err != nil {
			return Task{}, err
		}
	}
	updated.ID = previous.ID
	updated.RepositoryID = previous.RepositoryID
	updated.BaseRef = previous.BaseRef
	updated.BaseCommit = previous.BaseCommit
	updated.CreatedAt = previous.CreatedAt
	// Checkpoint and activity identity and history have dedicated append-only
	// operations; general mutations cannot rewrite or inject past events.
	updated.Checkpoints = append([]Checkpoint(nil), previous.Checkpoints...)
	updated.Activity = append([]Activity(nil), previous.Activity...)
	if !canTransition(previous.Status, updated.Status) {
		return Task{}, fmt.Errorf("agent task cannot transition from %s to %s", previous.Status, updated.Status)
	}
	if err := updated.validate(); err != nil {
		return Task{}, err
	}
	now := s.now().UTC()
	updated.UpdatedAt = now
	updated.LastActiveAt = now
	if updated.Status == StatusArchived && previous.ArchivedAt.IsZero() && updated.ArchivedAt.IsZero() {
		updated.ArchivedAt = now
	}
	if updated.Status != StatusArchived {
		updated.ArchivedAt = time.Time{}
	}
	activities, err := automaticActivities(previous, updated, now)
	if err != nil {
		return Task{}, err
	}
	for _, activity := range activities {
		updated.Activity = appendActivity(updated.Activity, activity)
	}

	oldRequestPath, _ := s.RequestFile(taskID)
	requestChanged := updated.Request != previous.Request
	if requestChanged {
		if err := writeFileAtomic(oldRequestPath, []byte(updated.Request), 0o600); err != nil {
			return Task{}, fmt.Errorf("save agent task request: %w", err)
		}
	}
	disk.Tasks[index] = updated
	if err := s.write(disk); err != nil {
		if requestChanged {
			_ = writeFileAtomic(oldRequestPath, []byte(previous.Request), 0o600)
		}
		return Task{}, err
	}
	return updated.Clone(), nil
}

// RecordActivity appends an explicit command event without changing lifecycle
// or test state. In particular, launch and resume history never recursively
// synthesizes additional status or test transitions.
func (s *Store) RecordActivity(taskID string, activity Activity) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !activity.Kind.valid() {
		return Task{}, fmt.Errorf("invalid agent activity kind %q", activity.Kind)
	}
	disk, err := s.read()
	if err != nil {
		return Task{}, err
	}
	index, previous, err := findTask(disk.Tasks, taskID)
	if err != nil {
		return Task{}, err
	}
	now := s.now().UTC()
	id, err := newTaskID()
	if err != nil {
		return Task{}, fmt.Errorf("create agent activity ID: %w", err)
	}
	activity.ID = id
	activity.CreatedAt = now
	activity.SessionID = strings.TrimSpace(activity.SessionID)
	activity.Reference = strings.TrimSpace(activity.Reference)
	updated := previous.Clone()
	updated.Activity = appendActivity(updated.Activity, activity)
	updated.UpdatedAt = now
	updated.LastActiveAt = now
	disk.Tasks[index] = updated
	if err := s.write(disk); err != nil {
		return Task{}, err
	}
	return updated.Clone(), nil
}

// RecordCheckpoint durably appends one repository snapshot to an explicit
// task. Task identity, base and timestamps come from the store, not its caller,
// and general task updates cannot rewrite existing checkpoint records.
func (s *Store) RecordCheckpoint(taskID string, checkpoint Checkpoint) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	checkpoint.SnapshotID = strings.TrimSpace(checkpoint.SnapshotID)
	checkpoint.HeadCommit = strings.TrimSpace(checkpoint.HeadCommit)
	checkpoint.Reason = strings.TrimSpace(checkpoint.Reason)
	if checkpoint.SnapshotID == "" {
		return Task{}, errors.New("agent checkpoint snapshot ID is required")
	}
	if checkpoint.HeadCommit == "" {
		return Task{}, errors.New("agent checkpoint HEAD commit is required")
	}
	if checkpoint.Reason == "" {
		return Task{}, errors.New("agent checkpoint reason is required")
	}
	id, err := newTaskID()
	if err != nil {
		return Task{}, fmt.Errorf("create agent checkpoint ID: %w", err)
	}
	disk, err := s.read()
	if err != nil {
		return Task{}, err
	}
	index, previous, err := findTask(disk.Tasks, taskID)
	if err != nil {
		return Task{}, err
	}
	checkpoint.ID = id
	checkpoint.BaseCommit = previous.BaseCommit
	now := s.now().UTC()
	checkpoint.CreatedAt = now
	activity, err := newActivity(ActivityCheckpoint, now)
	if err != nil {
		return Task{}, err
	}
	activity.CheckpointID = checkpoint.ID
	activity.SnapshotID = checkpoint.SnapshotID
	updated := previous.Clone()
	updated.Checkpoints = append(updated.Checkpoints, checkpoint)
	updated.Activity = appendActivity(updated.Activity, activity)
	if len(updated.Checkpoints) > checkpointHistoryKeep {
		updated.Checkpoints = append([]Checkpoint(nil), updated.Checkpoints[len(updated.Checkpoints)-checkpointHistoryKeep:]...)
	}
	disk.Tasks[index] = updated
	if err := s.write(disk); err != nil {
		return Task{}, err
	}
	return updated.Clone(), nil
}

func findTask(tasks []Task, taskID string) (int, Task, error) {
	if !validTaskID(taskID) {
		return -1, Task{}, errors.New("invalid agent task ID")
	}
	for index, task := range tasks {
		if task.ID == taskID {
			return index, task.Clone(), nil
		}
	}
	return -1, Task{}, fmt.Errorf("agent task %s was not found", taskID)
}

func (s *Store) read() (diskStore, error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return diskStore{Version: storeVersion}, nil
	}
	if err != nil {
		return diskStore{}, fmt.Errorf("read agent tasks: %w", err)
	}
	var disk diskStore
	if err := json.Unmarshal(data, &disk); err != nil {
		return diskStore{}, fmt.Errorf("read agent tasks: %w", err)
	}
	if disk.Version != storeVersion {
		return diskStore{}, fmt.Errorf("unsupported agent task store version %d", disk.Version)
	}
	seen := map[string]bool{}
	for _, task := range disk.Tasks {
		if !validTaskID(task.ID) {
			return diskStore{}, errors.New("agent task store contains an invalid ID")
		}
		if seen[task.ID] {
			return diskStore{}, fmt.Errorf("agent task store contains duplicate ID %s", task.ID)
		}
		seen[task.ID] = true
		if err := task.validate(); err != nil {
			return diskStore{}, fmt.Errorf("agent task %s: %w", task.ID, err)
		}
		if len(task.Activity) > activityHistoryKeep {
			return diskStore{}, fmt.Errorf("agent task %s contains too much activity", task.ID)
		}
		activities := map[string]bool{}
		for _, activity := range task.Activity {
			if !validTaskID(activity.ID) {
				return diskStore{}, fmt.Errorf("agent task %s contains an invalid activity ID", task.ID)
			}
			if activities[activity.ID] {
				return diskStore{}, fmt.Errorf("agent task %s contains duplicate activity ID %s", task.ID, activity.ID)
			}
			activities[activity.ID] = true
			if !activity.Kind.valid() || activity.CreatedAt.IsZero() {
				return diskStore{}, fmt.Errorf("agent task %s contains an incomplete activity %s", task.ID, activity.ID)
			}
		}
		checkpoints := map[string]bool{}
		for _, checkpoint := range task.Checkpoints {
			if !validTaskID(checkpoint.ID) {
				return diskStore{}, fmt.Errorf("agent task %s contains an invalid checkpoint ID", task.ID)
			}
			if checkpoints[checkpoint.ID] {
				return diskStore{}, fmt.Errorf("agent task %s contains duplicate checkpoint ID %s", task.ID, checkpoint.ID)
			}
			checkpoints[checkpoint.ID] = true
			if checkpoint.SnapshotID == "" || checkpoint.HeadCommit == "" || checkpoint.Reason == "" ||
				checkpoint.BaseCommit != task.BaseCommit || checkpoint.CreatedAt.IsZero() {
				return diskStore{}, fmt.Errorf("agent task %s contains an incomplete checkpoint %s", task.ID, checkpoint.ID)
			}
		}
	}
	return disk, nil
}

func (s *Store) write(disk diskStore) error {
	disk.Version = storeVersion
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agent tasks: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create agent task store: %w", err)
	}
	if err := writeFileAtomic(s.path(), data, 0o600); err != nil {
		return fmt.Errorf("save agent tasks: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agent-task-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
