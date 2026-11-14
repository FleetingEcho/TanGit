package agentcli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

func gitCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", dir}, args...)
	command := exec.Command("git", commandArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitCommand(t, root, "init", "-b", "main")
	gitCommand(t, root, "config", "user.name", "Agent CLI Test")
	gitCommand(t, root, "config", "user.email", "agentcli@example.invalid")
	writeTestFile(t, root, "tracked.txt", "base\n")
	gitCommand(t, root, "add", "tracked.txt")
	gitCommand(t, root, "commit", "-m", "base")
	return root
}

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dataDir := t.TempDir()
	service, err := NewService(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return service, dataDir
}

func createStoredTask(t *testing.T, service *Service, root string, mutate func(*agentwork.Task)) agentwork.Task {
	t.Helper()
	base := gitCommand(t, root, "rev-parse", "HEAD")
	store, err := service.store()
	if err != nil {
		t.Fatal(err)
	}
	task := agentwork.Task{
		RepositoryID: root, Title: "Automation task", Request: "Initial request",
		BaseRef: "main", BaseCommit: base, Branch: "agent/test", WorktreePath: root,
		Status: agentwork.StatusRunning,
	}
	if mutate != nil {
		mutate(&task)
	}
	created, err := store.Create(task)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func revision(task agentwork.Task) string {
	return task.UpdatedAt.Format(time.RFC3339Nano)
}

func TestResolveDataDirPrecedence(t *testing.T) {
	t.Setenv(DataDirEnvironment, filepath.Join(t.TempDir(), "environment"))
	explicit := filepath.Join(t.TempDir(), "explicit")
	resolved, err := ResolveDataDir(explicit)
	if err != nil || resolved != explicit {
		t.Fatalf("explicit data dir = %q, %v; want %q", resolved, err, explicit)
	}
	resolved, err = ResolveDataDir("")
	if err != nil || resolved != os.Getenv(DataDirEnvironment) {
		t.Fatalf("environment data dir = %q, %v", resolved, err)
	}
}

func TestSafeReadsAndDirectBaseFinalDiff(t *testing.T) {
	root := newGitRepo(t)
	service, _ := newTestService(t)
	base := gitCommand(t, root, "rev-parse", "HEAD")
	created := createStoredTask(t, service, root, nil)
	if created.BaseCommit != base {
		t.Fatalf("stored base = %s, want %s", created.BaseCommit, base)
	}
	// Include committed, uncommitted and untracked work in the final result.
	writeTestFile(t, root, "tracked.txt", "committed\n")
	gitCommand(t, root, "add", "tracked.txt")
	gitCommand(t, root, "commit", "-m", "agent commit")
	writeTestFile(t, root, "tracked.txt", "committed\nworking tree\n")
	writeTestFile(t, root, "untracked.txt", "untracked line\n")

	tasks, err := service.List(context.Background(), root)
	if err != nil || len(tasks) != 1 || tasks[0].ID != created.ID {
		t.Fatalf("List = %#v, %v", tasks, err)
	}
	filtered, err := service.List(context.Background(), "/another/repository")
	if err != nil || len(filtered) != 0 {
		t.Fatalf("repository filter = %#v, %v", filtered, err)
	}
	shown, err := service.Show(context.Background(), created.ID)
	if err != nil || shown.ID != created.ID {
		t.Fatalf("Show = %#v, %v", shown, err)
	}
	result, err := service.Diff(context.Background(), DiffOptions{TaskID: created.ID, ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]repository.CommitFile{}
	for _, file := range result.Files {
		byPath[file.Path] = file
	}
	tracked, found := byPath["tracked.txt"]
	if !found || tracked.LinesAdded != 2 || tracked.LinesDeleted != 1 {
		t.Fatalf("tracked final diff = %#v; all files %#v", tracked, result.Files)
	}
	untracked, found := byPath["untracked.txt"]
	if !found || untracked.Status != "??" || untracked.LinesAdded != 1 {
		t.Fatalf("untracked final diff = %#v", untracked)
	}
	fileResult, err := service.Diff(context.Background(), DiffOptions{
		TaskID: created.ID, File: "untracked.txt", ContextLines: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fileResult.Patch, "+untracked line") {
		t.Fatalf("untracked patch = %q / new text %q", fileResult.Patch, fileResult.NewText)
	}
	if _, err := service.Diff(context.Background(), DiffOptions{TaskID: created.ID, File: "missing.txt"}); err == nil {
		t.Fatal("diff selected a file absent from the final review")
	}
}

func TestShowIncludesCheckpointsAndLastTest(t *testing.T) {
	root := newGitRepo(t)
	service, _ := newTestService(t)
	created := createStoredTask(t, service, root, nil)
	store, _ := service.store()
	withCheckpoint, err := store.RecordCheckpoint(created.ID, agentwork.Checkpoint{
		SnapshotID: "snapshot", HeadCommit: created.BaseCommit, Reason: "before review",
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	code := 0
	if _, err := store.Update(created.ID, func(task *agentwork.Task) error {
		task.LastTest = &agentwork.TestResult{
			Status: agentwork.TestStatusPassed, Command: "go test ./...", ExitCode: &code,
			StartedAt: started, FinishedAt: started.Add(time.Second),
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	shown, err := service.Show(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shown.Checkpoints) != 1 || shown.Checkpoints[0].SnapshotID != "snapshot" ||
		shown.LastTest == nil || shown.LastTest.Command != "go test ./..." {
		t.Fatalf("Show omitted checkpoint/test data: %#v", shown)
	}
	if withCheckpoint.ID != created.ID {
		t.Fatal("checkpoint updated a different task")
	}
}

func TestExpectedRevisionRejectsStaleMutation(t *testing.T) {
	root := newGitRepo(t)
	service, _ := newTestService(t)
	created := createStoredTask(t, service, root, nil)
	summary := "newer write"
	moved, err := service.Report(context.Background(), ReportParams{
		TaskID: created.ID, ExpectedUpdatedAt: revision(created), Summary: &summary,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Report(context.Background(), ReportParams{
		TaskID: created.ID, ExpectedUpdatedAt: revision(created), Status: agentwork.StatusBlocked,
	})
	if !errors.Is(err, ErrStaleTask) {
		t.Fatalf("stale report error = %v, want ErrStaleTask", err)
	}
	stored, err := service.Show(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.UpdatedAt.Equal(moved.UpdatedAt) || stored.Status != created.Status || stored.Summary != summary {
		t.Fatalf("stale report changed task: %#v", stored)
	}
}

func TestReportCommentAndTestResultPersistWithoutAutomaticReview(t *testing.T) {
	root := newGitRepo(t)
	service, _ := newTestService(t)
	created := createStoredTask(t, service, root, nil)
	summary := "Implemented and checked"
	sessionID := "session-123"
	reported, err := service.Report(context.Background(), ReportParams{
		TaskID: created.ID, ExpectedUpdatedAt: revision(created),
		SessionID: &sessionID, Summary: &summary, Status: agentwork.StatusReadyForReview,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reported.SessionID != sessionID || reported.Summary != summary || reported.Status != agentwork.StatusReadyForReview {
		t.Fatalf("reported task = %#v", reported)
	}
	commented, err := service.Comment(context.Background(), CommentParams{
		TaskID: created.ID, ExpectedUpdatedAt: revision(reported), Text: "Please keep the public API stable.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(commented.Request, agentCommentStart) || !strings.Contains(commented.Request, "Please keep") ||
		!strings.HasPrefix(commented.Request, created.Request) || commented.Status != agentwork.StatusReadyForReview {
		t.Fatalf("comment was not durably delimited: %#v", commented)
	}
	started := time.Date(2026, 11, 13, 11, 0, 0, 0, time.UTC)
	finished := started.Add(2500 * time.Millisecond)
	tested, err := service.RecordTestResult(context.Background(), TestResultParams{
		TaskID: created.ID, ExpectedUpdatedAt: revision(commented), Status: agentwork.TestStatusPassed,
		Command: "go test ./...", ExitCode: 0, StartedAt: started, FinishedAt: finished,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tested.Status != agentwork.StatusReadyForReview || tested.SessionID != sessionID || tested.Summary != summary ||
		tested.LastTest == nil || tested.LastTest.ExitCode == nil || *tested.LastTest.ExitCode != 0 ||
		!tested.LastTest.StartedAt.Equal(started) || !tested.LastTest.FinishedAt.Equal(finished) {
		t.Fatalf("test result persistence/lifecycle = %#v", tested)
	}
}

func TestCreateRequiresYesAndPersistsFrozenBase(t *testing.T) {
	root := newGitRepo(t)
	service, _ := newTestService(t)
	base := gitCommand(t, root, "rev-parse", "HEAD")
	params := CreateParams{
		Repository: root, Path: filepath.Join(t.TempDir(), "agent-worktree"),
		Branch: "agent/from-cli", Base: "main", Title: "CLI created task", Request: "Implement the request",
		Profile: agentwork.Profile{
			Name: "local", LaunchCommand: "agent {worktree}", ResumeCommand: "agent resume {sessionId}",
			SetupCommand: "make setup", TestCommand: "go test ./...", Environment: map[string]string{"PORT": "4100"},
		},
	}
	if _, err := service.Create(context.Background(), params); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("unconfirmed Create error = %v", err)
	}
	if _, err := os.Stat(params.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unconfirmed create made a worktree: %v", err)
	}
	if output := gitCommand(t, root, "branch", "--list", params.Branch); output != "" {
		t.Fatalf("unconfirmed create made branch %q", output)
	}
	params.Yes = true
	created, err := service.Create(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if created.BaseCommit != base || created.BaseRef != "main" || created.RepositoryID != root ||
		created.Profile.LaunchCommand != params.Profile.LaunchCommand || created.Profile.Environment["PORT"] != "4100" {
		t.Fatalf("created task = %#v", created)
	}
	if head := gitCommand(t, params.Path, "rev-parse", "HEAD"); head != base {
		t.Fatalf("worktree HEAD = %s, want frozen base %s", head, base)
	}
	if branch := gitCommand(t, params.Path, "branch", "--show-current"); branch != params.Branch {
		t.Fatalf("worktree branch = %q", branch)
	}
}

func TestPatchPreviewAndApplyUseExplicitDestination(t *testing.T) {
	source := newGitRepo(t)
	destination := newGitRepo(t)
	writeTestFile(t, source, "tracked.txt", "base\npatched\n")
	patchText := gitCommand(t, source, "diff", "--no-color", "--no-ext-diff", "--", "tracked.txt")
	patchPath := filepath.Join(t.TempDir(), "delivery.patch")
	if err := os.WriteFile(patchPath, []byte(patchText+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _ := newTestService(t)
	params := PatchParams{Destination: destination, Patch: patchPath}
	preview, err := service.PreviewPatch(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Clean || preview.Applied || len(preview.Files) != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	if got := gitCommand(t, destination, "status", "--porcelain"); got != "" {
		t.Fatalf("preview changed destination: %q", got)
	}
	if _, err := service.ApplyPatch(context.Background(), params); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("unconfirmed ApplyPatch error = %v", err)
	}
	params.Yes = true
	applied, err := service.ApplyPatch(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || !applied.Clean {
		t.Fatalf("applied result = %#v", applied)
	}
	content, err := os.ReadFile(filepath.Join(destination, "tracked.txt"))
	if err != nil || string(content) != "base\npatched\n" {
		t.Fatalf("patched content = %q, %v", content, err)
	}
	if status := gitCommand(t, destination, "status", "--porcelain"); !strings.Contains(status, "tracked.txt") {
		t.Fatalf("destination lacks applied work: %q", status)
	}
	if sourceStatus := gitCommand(t, source, "status", "--porcelain"); !strings.Contains(sourceStatus, "tracked.txt") {
		t.Fatalf("source unexpectedly changed: %q", sourceStatus)
	}
}

func TestExecuteJSONAndErrorsUseSuppliedStreams(t *testing.T) {
	root := newGitRepo(t)
	service, dataDir := newTestService(t)
	created := createStoredTask(t, service, root, nil)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"show", "--data-dir", dataDir, "--id", created.ID, "--json"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), `"id": "`+created.ID+`"`) {
		t.Fatalf("Execute show code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Execute(context.Background(), []string{"show", "--data-dir", dataDir, "--id", "bad"}, strings.NewReader(""), &stdout, &stderr)
	if code != ExitError || stdout.Len() != 0 || !strings.Contains(stderr.String(), "invalid agent task ID") {
		t.Fatalf("Execute error code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
