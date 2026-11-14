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

const storeVersion = 1

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

// Task is the durable identity shared by worktrees, runs, reviews and later
// checkpoints. RepositoryID is an opaque workspace identity, not a branch or
// currently selected tab. Paths remain native paths of their local/SSH host.
type Task struct {
	ID           string      `json:"id"`
	RepositoryID string      `json:"repositoryId"`
	Title        string      `json:"title"`
	Request      string      `json:"request"`
	BaseRef      string      `json:"baseRef"`
	BaseCommit   string      `json:"baseCommit"`
	Branch       string      `json:"branch"`
	WorktreePath string      `json:"worktreePath"`
	Profile      Profile     `json:"profile"`
	Status       Status      `json:"status"`
	SessionID    string      `json:"sessionId,omitempty"`
	Summary      string      `json:"summary,omitempty"`
	ExitCode     *int        `json:"exitCode,omitempty"`
	LastTest     *TestResult `json:"lastTest,omitempty"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
	LastActiveAt time.Time   `json:"lastActiveAt,omitempty"`
	ArchivedAt   time.Time   `json:"archivedAt,omitempty"`
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
