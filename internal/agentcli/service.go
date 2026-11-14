// Package agentcli provides explicit-target, local automation for agent tasks.
package agentcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

const DataDirEnvironment = "TANGIT_AGENT_DATA_DIR"

// ResolveDataDir applies the CLI, environment, then per-user default precedence.
// The desktop app must be configured with this same directory to share tasks.
func ResolveDataDir(explicit string) (string, error) {
	dir := strings.TrimSpace(explicit)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv(DataDirEnvironment))
	}
	if dir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		if strings.TrimSpace(configDir) == "" {
			return "", errors.New("user config directory is empty; set --data-dir or " + DataDirEnvironment)
		}
		dir = filepath.Join(configDir, "TanGit")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve agent data directory: %w", err)
	}
	return filepath.Clean(absolute), nil
}

type repositoryOpener interface {
	Open(context.Context, string) (repository.Session, error)
}

// Service performs operations without consulting any desktop UI state.
type Service struct {
	dataDir string
	opener  repositoryOpener
	now     func() time.Time
}

// NewService resolves dataDir (or its environment/default fallback).
func NewService(dataDir string) (*Service, error) {
	resolved, err := ResolveDataDir(dataDir)
	if err != nil {
		return nil, err
	}
	return &Service{dataDir: resolved, opener: repository.Opener{}, now: time.Now}, nil
}

func (s *Service) forDataDir(dataDir string) (*Service, error) {
	if strings.TrimSpace(dataDir) == "" {
		return s, nil
	}
	resolved, err := ResolveDataDir(dataDir)
	if err != nil {
		return nil, err
	}
	clone := *s
	clone.dataDir = resolved
	return &clone, nil
}

func (s *Service) store() (*agentwork.Store, error) {
	return agentwork.OpenStore(s.dataDir)
}

func absolutePath(value, role string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", role)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", role, err)
	}
	return filepath.Clean(absolute), nil
}

func closeSession(session repository.Session) {
	if closer, ok := session.(io.Closer); ok {
		_ = closer.Close()
	}
}

func (s *Service) open(ctx context.Context, path string) (repository.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.opener.Open(ctx, path)
}

// List returns tasks newest first; repository is an exact opaque ID filter.
func (s *Service) List(ctx context.Context, repositoryID string) ([]agentwork.Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, err := s.store()
	if err != nil {
		return nil, err
	}
	return store.List(strings.TrimSpace(repositoryID))
}

// Show reads one task, including checkpoint history and its latest test result.
func (s *Service) Show(ctx context.Context, taskID string) (agentwork.Task, error) {
	if err := ctx.Err(); err != nil {
		return agentwork.Task{}, err
	}
	store, err := s.store()
	if err != nil {
		return agentwork.Task{}, err
	}
	return store.Get(strings.TrimSpace(taskID))
}

// DiffOptions identifies one explicit task and optionally one final file.
type DiffOptions struct {
	TaskID           string
	File             string
	IgnoreWhitespace bool
	ContextLines     int
}

// DiffResult is the direct base-to-final review, never a cached UI selection.
type DiffResult struct {
	TaskID     string                  `json:"taskId"`
	BaseCommit string                  `json:"baseCommit"`
	Files      []repository.CommitFile `json:"files"`
	File       string                  `json:"file,omitempty"`
	Patch      string                  `json:"patch,omitempty"`
	OldText    string                  `json:"oldText,omitempty"`
	NewText    string                  `json:"newText,omitempty"`
	Kind       string                  `json:"kind,omitempty"`
}

// Diff reads BaseCommit directly against the task worktree's final state.
func (s *Service) Diff(ctx context.Context, options DiffOptions) (DiffResult, error) {
	task, err := s.Show(ctx, options.TaskID)
	if err != nil {
		return DiffResult{}, err
	}
	session, err := s.open(ctx, task.WorktreePath)
	if err != nil {
		return DiffResult{}, fmt.Errorf("open task worktree %s: %w", task.WorktreePath, err)
	}
	defer closeSession(session)
	diffOptions := repository.DiffOptions{
		IgnoreWhitespace: options.IgnoreWhitespace,
		ContextLines:     options.ContextLines,
	}
	files, err := session.FinalFiles(ctx, task.BaseCommit, diffOptions)
	if err != nil {
		return DiffResult{}, err
	}
	if files == nil {
		files = []repository.CommitFile{}
	}
	result := DiffResult{TaskID: task.ID, BaseCommit: task.BaseCommit, Files: files, File: options.File}
	if options.File == "" {
		return result, nil
	}
	for _, file := range files {
		if file.Path != options.File {
			continue
		}
		diff, err := session.FinalFileDiff(ctx, task.BaseCommit, file, diffOptions)
		if err != nil {
			return DiffResult{}, err
		}
		result.Patch, result.OldText, result.NewText, result.Kind = diff.Patch, diff.OldText, diff.NewText, diff.Kind
		return result, nil
	}
	return DiffResult{}, fmt.Errorf("final file %q was not found in agent task %s", options.File, task.ID)
}

// CreateParams contains all Git and profile inputs for a new task.
type CreateParams struct {
	Repository string
	Path       string
	Branch     string
	Base       string
	Title      string
	Request    string
	Profile    agentwork.Profile
	Yes        bool
}

// Create freezes Base before creating a worktree and then persists its task.
func (s *Service) Create(ctx context.Context, params CreateParams) (agentwork.Task, error) {
	if !params.Yes {
		return agentwork.Task{}, errors.New("creating an agent task requires explicit --yes confirmation")
	}
	repositoryPath, err := absolutePath(params.Repository, "repository")
	if err != nil {
		return agentwork.Task{}, err
	}
	worktreePath, err := absolutePath(params.Path, "worktree path")
	if err != nil {
		return agentwork.Task{}, err
	}
	params.Branch = strings.TrimSpace(params.Branch)
	params.Base = strings.TrimSpace(params.Base)
	params.Title = strings.TrimSpace(params.Title)
	if params.Branch == "" || params.Base == "" || params.Title == "" || strings.TrimSpace(params.Request) == "" {
		return agentwork.Task{}, errors.New("branch, base, title, and request are required")
	}
	session, err := s.open(ctx, repositoryPath)
	if err != nil {
		return agentwork.Task{}, err
	}
	defer closeSession(session)
	baseCommit, err := session.ResolveCommit(ctx, params.Base)
	if err != nil {
		return agentwork.Task{}, err
	}
	if err := session.CreateWorktree(ctx, repository.WorktreeCreateOptions{
		Path: worktreePath, Base: baseCommit, Branch: params.Branch,
	}); err != nil {
		return agentwork.Task{}, err
	}
	store, err := s.store()
	if err != nil {
		return agentwork.Task{}, createPersistenceError(worktreePath, params.Branch, err)
	}
	created, err := store.Create(agentwork.Task{
		RepositoryID: repositoryPath,
		Title:        params.Title,
		Request:      params.Request,
		BaseRef:      params.Base,
		BaseCommit:   baseCommit,
		Branch:       params.Branch,
		WorktreePath: worktreePath,
		Profile:      params.Profile,
	})
	if err != nil {
		return agentwork.Task{}, createPersistenceError(worktreePath, params.Branch, err)
	}
	return created, nil
}

func createPersistenceError(path, branch string, err error) error {
	return fmt.Errorf("worktree %s and branch %s were created, but the agent task was not saved; inspect them before manual cleanup (no automatic deletion was attempted): %w", path, branch, err)
}

// ReportParams uses pointers to distinguish omitted fields from empty values.
type ReportParams struct {
	TaskID            string
	ExpectedUpdatedAt string
	SessionID         *string
	Summary           *string
	Status            agentwork.Status
}

// CommentParams appends a delimited block to the durable request text.
type CommentParams struct {
	TaskID            string
	ExpectedUpdatedAt string
	Text              string
}

// TestResultParams records an observed result without changing lifecycle state.
type TestResultParams struct {
	TaskID            string
	ExpectedUpdatedAt string
	Status            agentwork.TestStatus
	Command           string
	ExitCode          int
	StartedAt         time.Time
	FinishedAt        time.Time
}

func expectedRevision(expected string) (time.Time, error) {
	if strings.TrimSpace(expected) == "" {
		return time.Time{}, errors.New("--expected-updated-at is required for existing-task mutations")
	}
	revision, err := time.Parse(time.RFC3339, expected)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse --expected-updated-at as RFC3339: %w", err)
	}
	return revision, nil
}

// ErrStaleTask identifies compare-and-swap failures for automation clients.
var ErrStaleTask = errors.New("agent task changed since the expected revision")

func mutateExpected(store *agentwork.Store, taskID, expected string, mutate func(*agentwork.Task) error) (agentwork.Task, error) {
	revision, err := expectedRevision(expected)
	if err != nil {
		return agentwork.Task{}, err
	}
	return store.Update(strings.TrimSpace(taskID), func(task *agentwork.Task) error {
		if !task.UpdatedAt.Equal(revision) {
			return fmt.Errorf("%w: expected %s, current %s; show the task again", ErrStaleTask, revision.Format(time.RFC3339Nano), task.UpdatedAt.Format(time.RFC3339Nano))
		}
		return mutate(task)
	})
}

// Report updates only supplied report fields and lets Store enforce transitions.
func (s *Service) Report(ctx context.Context, params ReportParams) (agentwork.Task, error) {
	if err := ctx.Err(); err != nil {
		return agentwork.Task{}, err
	}
	if params.SessionID == nil && params.Summary == nil && params.Status == "" {
		return agentwork.Task{}, errors.New("report requires at least one of session ID, summary, or status")
	}
	switch params.Status {
	case "", agentwork.StatusNeedsInput, agentwork.StatusReadyForReview, agentwork.StatusBlocked, agentwork.StatusFailed:
	default:
		return agentwork.Task{}, fmt.Errorf("unsupported report status %q", params.Status)
	}
	store, err := s.store()
	if err != nil {
		return agentwork.Task{}, err
	}
	return mutateExpected(store, params.TaskID, params.ExpectedUpdatedAt, func(task *agentwork.Task) error {
		if params.SessionID != nil {
			task.SessionID = strings.TrimSpace(*params.SessionID)
		}
		if params.Summary != nil {
			task.Summary = strings.TrimSpace(*params.Summary)
		}
		if params.Status != "" {
			task.Status = params.Status
		}
		return nil
	})
}

const (
	agentCommentStart = "<!-- TANGIT AGENT COMMENT START -->"
	agentCommentEnd   = "<!-- TANGIT AGENT COMMENT END -->"
)

// Comment appends feedback without implicitly resuming or changing status.
func (s *Service) Comment(ctx context.Context, params CommentParams) (agentwork.Task, error) {
	if err := ctx.Err(); err != nil {
		return agentwork.Task{}, err
	}
	text := strings.TrimSpace(params.Text)
	if text == "" {
		return agentwork.Task{}, errors.New("comment text is required")
	}
	store, err := s.store()
	if err != nil {
		return agentwork.Task{}, err
	}
	return mutateExpected(store, params.TaskID, params.ExpectedUpdatedAt, func(task *agentwork.Task) error {
		task.Request += "\n\n" + agentCommentStart + "\n\n" + text + "\n\n" + agentCommentEnd + "\n"
		return nil
	})
}

// RecordTestResult persists a completed test observation, never review readiness.
func (s *Service) RecordTestResult(ctx context.Context, params TestResultParams) (agentwork.Task, error) {
	if err := ctx.Err(); err != nil {
		return agentwork.Task{}, err
	}
	if params.Status != agentwork.TestStatusPassed && params.Status != agentwork.TestStatusFailed {
		return agentwork.Task{}, errors.New("test result status must be passed or failed")
	}
	params.Command = strings.TrimSpace(params.Command)
	if params.Command == "" {
		return agentwork.Task{}, errors.New("test result command is required")
	}
	if params.StartedAt.IsZero() && params.FinishedAt.IsZero() {
		params.StartedAt = s.now().UTC()
	}
	if params.StartedAt.IsZero() {
		params.StartedAt = params.FinishedAt
	}
	if params.FinishedAt.IsZero() {
		params.FinishedAt = s.now().UTC()
	}
	params.StartedAt, params.FinishedAt = params.StartedAt.UTC(), params.FinishedAt.UTC()
	if params.FinishedAt.Before(params.StartedAt) {
		return agentwork.Task{}, errors.New("test result finished time precedes its started time")
	}
	store, err := s.store()
	if err != nil {
		return agentwork.Task{}, err
	}
	exitCode := params.ExitCode
	result := agentwork.TestResult{
		Status: params.Status, Command: params.Command, ExitCode: &exitCode,
		StartedAt: params.StartedAt, FinishedAt: params.FinishedAt,
	}
	return mutateExpected(store, params.TaskID, params.ExpectedUpdatedAt, func(task *agentwork.Task) error {
		task.LastTest = &result
		return nil
	})
}

// PatchParams always names both the destination worktree and source patch.
type PatchParams struct {
	Destination string
	Patch       string
	Yes         bool
}

// PatchResult contains only the public, serializable preview information.
type PatchResult struct {
	Destination   string                 `json:"destination"`
	Path          string                 `json:"path"`
	Text          string                 `json:"text"`
	TextTruncated bool                   `json:"textTruncated"`
	Clean         bool                   `json:"clean"`
	Applied       bool                   `json:"applied"`
	Diagnostic    string                 `json:"diagnostic,omitempty"`
	Files         []repository.PatchFile `json:"files"`
	Additions     int                    `json:"additions"`
	Deletions     int                    `json:"deletions"`
}

func publicPatchResult(destination string, preview repository.PatchPreview) PatchResult {
	files := preview.Files
	if files == nil {
		files = []repository.PatchFile{}
	}
	return PatchResult{
		Destination: destination, Path: preview.Path, Text: preview.Text,
		TextTruncated: preview.TextTruncated, Clean: preview.Clean,
		Diagnostic: preview.Diagnostic, Files: files,
		Additions: preview.Additions, Deletions: preview.Deletions,
	}
}

func (s *Service) previewPatch(ctx context.Context, params PatchParams) (repository.Session, repository.PatchPreview, PatchResult, error) {
	destination, err := absolutePath(params.Destination, "destination worktree")
	if err != nil {
		return nil, repository.PatchPreview{}, PatchResult{}, err
	}
	patchPath, err := absolutePath(params.Patch, "patch file")
	if err != nil {
		return nil, repository.PatchPreview{}, PatchResult{}, err
	}
	session, err := s.open(ctx, destination)
	if err != nil {
		return nil, repository.PatchPreview{}, PatchResult{}, err
	}
	preview, err := session.PreviewPatch(ctx, patchPath)
	if err != nil {
		closeSession(session)
		return nil, repository.PatchPreview{}, PatchResult{}, err
	}
	return session, preview, publicPatchResult(destination, preview), nil
}

// PreviewPatch checks a patch and leaves both source and destination unchanged.
func (s *Service) PreviewPatch(ctx context.Context, params PatchParams) (PatchResult, error) {
	session, _, result, err := s.previewPatch(ctx, params)
	if session != nil {
		defer closeSession(session)
	}
	return result, err
}

// ApplyPatch applies the exact clean preview obtained in this same invocation.
func (s *Service) ApplyPatch(ctx context.Context, params PatchParams) (PatchResult, error) {
	if !params.Yes {
		return PatchResult{}, errors.New("applying a patch requires explicit --yes confirmation")
	}
	session, preview, result, err := s.previewPatch(ctx, params)
	if session != nil {
		defer closeSession(session)
	}
	if err != nil {
		return PatchResult{}, err
	}
	if !preview.Clean {
		return result, fmt.Errorf("patch preview is not clean: %s", strings.TrimSpace(preview.Diagnostic))
	}
	if err := session.ApplyPatch(ctx, preview); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}
