package app

// Screenshot harness: runs only when MYGO_SHOTS is set. Renders representative
// app states with the fake session and writes PNGs for visual review:
//
//	MYGO_SHOTS=/tmp/lazygit-shots go test ./internal/app -run TestScreenshot
//
// The files land under $MYGO_SHOTS/<name>.png.

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

const screenshotPatch = `diff --git a/one.txt b/one.txt
index 3b18e51..9de77ba 100644
--- a/one.txt
+++ b/one.txt
@@ -1,3 +1,4 @@
 first line
-second line
+second line, changed
 third line
+fourth line
`

// screenshotConflictFile is a Go file mid-merge: three conflicts, the middle
// one with the ancestor the diff3 style writes.
const screenshotConflictFile = `package repository

func (s *gitSession) Diff(ctx context.Context, file File) (Diff, error) {
	if err := ctx.Err(); err != nil {
		return Diff{}, err
	}
<<<<<<< HEAD
	patch, err := s.git.Diff.GetDiff(file.Staged)
	if err != nil {
		return Diff{}, fmt.Errorf("read diff: %w", err)
	}
||||||| base
	patch := s.git.Diff.WorktreeFileDiff(file.Path)
=======
	patch := s.git.Diff.GetDiff(false, file.Path)
>>>>>>> feature/session

	return Diff{Patch: patch, Kind: "Staged"}, nil
}

func (s *gitSession) Stage(ctx context.Context, file File) error {
<<<<<<< HEAD
	if err := s.git.WorkingTree.StageFile(file.Path); err != nil {
		return fmt.Errorf("stage %s: %w", file.Path, err)
	}
	return nil
=======
	return s.git.WorkingTree.StageFiles([]string{file.Path})
>>>>>>> feature/session
}
`

// screenshotEditableFile is a new file shown by the retained in-pane editor,
// entered directly by this fixture after its redundant header button was removed.
const screenshotEditableFile = `{
    "data_source": [
        "data_timestamp",
        "data_version"
    ],
    "Event": [
        "event_action",
        "event_assignee",
        "event_cat",
        "event_count",
        "event_creation_time"
    ],
    "Incident": [
        "incident_id",
        "incident_name",
        "incident_status"
    ]
}
`

// screenshotNewFilePatch is what git shows for a file it does not know: the
// patch that adds it.
const screenshotNewFilePatch = `diff --git a/docs/notes.md b/docs/notes.md
new file mode 100644
index 0000000..1b2c3d4
--- /dev/null
+++ b/docs/notes.md
@@ -0,0 +1,6 @@
+# Notes
+
+What the window does, and why.
+
+The diff of a new file is its content, all of it added.
+Each line of it can be staged on its own.
`

func screenshotSnapshot() repository.Snapshot {
	return repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Files: []repository.File{
			{Path: "internal/repository/session.go", Status: "M ", Staged: true, Tracked: true, LinesAdded: 12, LinesDeleted: 3},
			{Path: "main.go", Status: "MM", Staged: true, Unstaged: true, Tracked: true, LinesAdded: 40, LinesDeleted: 9},
			{Path: "ui/appevents.go", Status: " M", Unstaged: true, Tracked: true, LinesAdded: 5},
			{Path: "docs/notes.md", Status: "??", Unstaged: true, Added: true},
			{Path: "old.go", PreviousPath: "new.go", Status: "R ", Staged: true, Tracked: true},
		},
		Commits: []repository.Commit{
			{Hash: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", ShortHash: "a1b2c3d", Subject: "Add native file diffs and staging", AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com", UnixTimestamp: 1_700_000_000, Tags: []string{"v0.2.0"}, Parents: []string{"b2c3d4e5f60718293a4b5c6d7e8f901234567890", "d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3"}},
			{Hash: "b2c3d4e5f60718293a4b5c6d7e8f901234567890", ShortHash: "b2c3d4e", Subject: "Build the working tree on Lazygit commands", AuthorName: "Alan Turing", AuthorEmail: "alan@example.com", UnixTimestamp: 1_699_999_000, Parents: []string{"c3d4e5f60718293a4b5c6d7e8f90123456789012"}},
			{Hash: "d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3", ShortHash: "d4e5f6a", Subject: "Draw the graph in native lanes", AuthorName: "Grace Hopper", AuthorEmail: "grace@example.com", UnixTimestamp: 1_699_998_500, Parents: []string{"c3d4e5f60718293a4b5c6d7e8f90123456789012"}},
			{Hash: "c3d4e5f60718293a4b5c6d7e8f90123456789012", ShortHash: "c3d4e5f", Subject: "Establish the native desktop foundation", AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com", UnixTimestamp: 1_699_998_000},
		},
		Branches: []repository.Branch{
			{Name: "main", DisplayName: "main", Head: true, Subject: "Add native file diffs and staging", UpstreamRemote: "origin", UpstreamBranch: "main", Ahead: "2", Behind: "0"},
			{Name: "feature/menus", DisplayName: "feature/menus", Subject: "Context menus"},
			{Name: "fix/stale-diff", DisplayName: "fix/stale-diff", Subject: "Cancel stale diff loads"},
		},
		Remotes: []repository.Remote{
			{Name: "origin", URLs: []string{"git@example.com:team/project.git"}, Branches: []repository.RemoteBranch{{Name: "main", RemoteName: "origin"}, {Name: "feature/menus", RemoteName: "origin"}, {Name: "fix/stale-diff", RemoteName: "origin"}}},
			{Name: "upstream", URLs: []string{"git@example.com:org/project.git"}},
		},
		Stashes: []repository.Stash{
			{Index: 0, Name: "WIP: native stash", Hash: "d4e5f607", Recency: "2m"},
			{Index: 1, Name: "On main: experiment", Hash: "e5f60718", Recency: "3h"},
		},
		Tags: []repository.Tag{
			{Name: "v0.2.0", Message: "Native file diffs"},
			{Name: "v0.1.0", Message: "First native release"},
		},
		Worktrees: []repository.Worktree{
			{Name: "project", Path: "/work/project", Branch: "main", Current: true, Main: true},
			{Name: "menus", Path: "/work/menus", Branch: "feature/menus"},
		},
		Submodules: []repository.Submodule{
			{Name: "library", Path: "vendor/library", URL: "git@example.com:team/library.git"},
		},
		Reflog: []repository.Commit{
			{Hash: "a1b2c3d4", ShortHash: "a1b2c3d", Subject: "checkout: moving from feature/menus to main", UnixTimestamp: 1_700_000_000},
			{Hash: "b2c3d4e5", ShortHash: "b2c3d4e", Subject: "commit: Native history", UnixTimestamp: 1_699_999_000},
		},
	}
}

type screenshotSession struct {
	snapshot repository.Snapshot
}

func (s screenshotSession) Snapshot(context.Context) (repository.Snapshot, error) {
	return s.snapshot, nil
}

func (s screenshotSession) ReviewedFiles(context.Context, []repository.File) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (s screenshotSession) SetFileReviewed(context.Context, repository.File, bool) error {
	return nil
}

func (s screenshotSession) ConflictStages(context.Context, repository.File) (repository.ConflictStages, error) {
	return repository.ConflictStages{}, nil
}

func (s screenshotSession) Media(context.Context, repository.MediaRequest) (repository.MediaPair, error) {
	return repository.MediaPair{
		Old: repository.MediaBlob{Present: true, Data: screenshotMediaPNG(240, 140, color.RGBA{R: 44, G: 105, B: 170, A: 255})},
		New: repository.MediaBlob{Present: true, Data: screenshotMediaPNG(240, 140, color.RGBA{R: 76, G: 160, B: 112, A: 255})},
	}, nil
}

func screenshotMediaPNG(width, height int, fill color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		panic(err)
	}
	return data.Bytes()
}

func (s screenshotSession) Diff(_ context.Context, file repository.File, options repository.DiffOptions) (repository.Diff, error) {
	if file.Path == "picture.png" {
		return repository.Diff{Patch: testBinaryImagePatch, Kind: "Unstaged"}, nil
	}
	if !file.Tracked {
		// A file git does not know is shown as the patch that adds it.
		return repository.Diff{Patch: screenshotNewFilePatch, Kind: "Untracked"}, nil
	}
	return repository.Diff{Patch: screenshotPatch, Kind: "Staged"}, nil
}

func (s screenshotSession) CompareFiles(context.Context, string, string, repository.DiffOptions) ([]repository.CommitFile, error) {
	return []repository.CommitFile{{Path: "changed.txt", Status: "M", LinesAdded: 2, LinesDeleted: 1}}, nil
}

func (s screenshotSession) CompareDiff(context.Context, string, string, repository.DiffOptions) (repository.Diff, error) {
	return repository.Diff{Patch: screenshotPatch, Kind: "Compare"}, nil
}

func (s screenshotSession) CompareFileDiff(context.Context, string, string, repository.CommitFile, repository.DiffOptions) (repository.Diff, error) {
	return repository.Diff{Patch: screenshotPatch, Kind: "Compare"}, nil
}

func (s screenshotSession) Stage(context.Context, repository.File) error         { return nil }
func (s screenshotSession) Unstage(context.Context, repository.File) error       { return nil }
func (s screenshotSession) ApplyCachedPatch(context.Context, string, bool) error { return nil }
func (s screenshotSession) ApplyWorkingTreePatch(context.Context, string, bool) error {
	return nil
}
func (s screenshotSession) PreviewPatch(_ context.Context, path string) (repository.PatchPreview, error) {
	return repository.PatchPreview{Path: path, Clean: true}, nil
}
func (s screenshotSession) ApplyPatch(context.Context, repository.PatchPreview) error { return nil }
func (s screenshotSession) DiscardFiles(context.Context, []repository.File) error     { return nil }
func (s screenshotSession) WorkSnapshots(context.Context) ([]repository.WorkSnapshot, error) {
	return nil, nil
}
func (s screenshotSession) CreateCheckpoint(context.Context, string) (repository.WorkSnapshot, error) {
	return repository.WorkSnapshot{}, nil
}
func (s screenshotSession) RestoreWorkSnapshot(context.Context, repository.WorkSnapshot) error {
	return nil
}
func (s screenshotSession) DeleteWorkSnapshot(context.Context, repository.WorkSnapshot) error {
	return nil
}
func (s screenshotSession) StageAll(context.Context) error                     { return nil }
func (s screenshotSession) UnstageAll(context.Context) error                   { return nil }
func (s screenshotSession) Commit(context.Context, string, string, bool) error { return nil }
func (s screenshotSession) Undo(context.Context) error                         { return nil }
func (s screenshotSession) Redo(context.Context) error                         { return nil }
func (s screenshotSession) CommitMessage(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
	return repository.CommitMessage{Summary: commit.Subject}, nil
}
func (s screenshotSession) RewordCommit(context.Context, repository.Commit, string, string) error {
	return nil
}
func (s screenshotSession) DropCommit(context.Context, repository.Commit) error { return nil }
func (s screenshotSession) MoveCommit(context.Context, repository.Commit, repository.CommitMove) error {
	return nil
}
func (s screenshotSession) CombineCommit(context.Context, repository.Commit, repository.CombineMode) error {
	return nil
}
func (s screenshotSession) AmendCommit(context.Context, repository.Commit) error { return nil }
func (s screenshotSession) CreateFixupCommit(context.Context, repository.Commit) error {
	return nil
}
func (s screenshotSession) ApplyFixupCommits(context.Context, repository.Commit) error {
	return nil
}
func (s screenshotSession) Discard(context.Context, repository.File) error     { return nil }
func (s screenshotSession) IgnoreFile(context.Context, repository.File) error  { return nil }
func (s screenshotSession) ExcludeFile(context.Context, repository.File) error { return nil }
func (s screenshotSession) CheckoutBranch(context.Context, string) error       { return nil }
func (s screenshotSession) CreateBranch(context.Context, string, string) error { return nil }
func (s screenshotSession) DeleteBranch(context.Context, string, bool) error   { return nil }
func (s screenshotSession) RenameBranch(context.Context, string, string) error { return nil }
func (s screenshotSession) MergeBranch(context.Context, string) error          { return nil }
func (s screenshotSession) Fetch(context.Context) error                        { return nil }
func (s screenshotSession) FetchRemote(context.Context, string) error          { return nil }
func (s screenshotSession) Pull(context.Context) error                         { return nil }
func (s screenshotSession) Push(context.Context) error                         { return nil }
func (s screenshotSession) PullBranch(context.Context, string) error           { return nil }
func (s screenshotSession) FastForwardBranch(context.Context, string) error    { return nil }
func (s screenshotSession) RebaseOntoBranch(context.Context, string) error     { return nil }
func (s screenshotSession) ForceCheckoutBranch(context.Context, string) error  { return nil }
func (s screenshotSession) SetUpstream(context.Context, string, string, string) error {
	return nil
}
func (s screenshotSession) UnsetUpstream(context.Context, string) error     { return nil }
func (s screenshotSession) CommandLog() *repository.CommandLog              { return nil }
func (s screenshotSession) AddRemote(context.Context, string, string) error { return nil }
func (s screenshotSession) UpdateRemote(context.Context, string, string, string) error {
	return nil
}
func (s screenshotSession) RemoveRemote(context.Context, string) error { return nil }
func (s screenshotSession) CheckoutRemoteBranch(context.Context, string, string, string) error {
	return nil
}
func (s screenshotSession) DeleteRemoteBranch(context.Context, string, string) error {
	return nil
}
func (s screenshotSession) StashChanges(context.Context, string, bool) error { return nil }
func (s screenshotSession) StashSelectedFiles(context.Context, string, []repository.File, bool) error {
	return nil
}
func (s screenshotSession) StashPatch(context.Context, string, string, string) error { return nil }
func (s screenshotSession) StashStagedChanges(context.Context, string) error {
	return nil
}
func (s screenshotSession) ApplyStash(context.Context, int, bool) error    { return nil }
func (s screenshotSession) RenameStash(context.Context, int, string) error { return nil }
func (s screenshotSession) DropStash(context.Context, int) error           { return nil }
func (s screenshotSession) StashFiles(context.Context, int) ([]repository.CommitFile, error) {
	return []repository.CommitFile{
		{Path: "internal/repository/session.go", Status: "M"},
		{Path: "docs/saved-work.md", Status: "A"},
	}, nil
}
func (s screenshotSession) StashFileDiff(context.Context, int, repository.CommitFile, repository.DiffOptions) (repository.Diff, error) {
	return repository.Diff{Patch: screenshotPatch, Kind: "Stash"}, nil
}
func (s screenshotSession) ApplyStashFile(context.Context, int, repository.CommitFile) error {
	return nil
}
func (s screenshotSession) DiscardStashFile(context.Context, int, repository.CommitFile, string) error {
	return nil
}
func (s screenshotSession) CreateTag(context.Context, string, string, string, bool) error {
	return nil
}
func (s screenshotSession) DeleteTag(context.Context, string) error       { return nil }
func (s screenshotSession) PushTag(context.Context, string, string) error { return nil }
func (s screenshotSession) CherryPickCommit(context.Context, repository.Commit) error {
	return nil
}
func (s screenshotSession) RevertCommit(context.Context, repository.Commit) error   { return nil }
func (s screenshotSession) CheckoutCommit(context.Context, repository.Commit) error { return nil }
func (s screenshotSession) ResetToCommit(context.Context, repository.Commit, repository.ResetMode) error {
	return nil
}
func (s screenshotSession) ResolveConflict(context.Context, repository.File, bool) error {
	return nil
}
func (s screenshotSession) ContinueOperation(context.Context) error { return nil }
func (s screenshotSession) AbortOperation(context.Context) error    { return nil }
func (s screenshotSession) CreateWorktree(context.Context, string, string, string, bool) error {
	return nil
}
func (s screenshotSession) DeleteWorktree(context.Context, string, bool) error { return nil }
func (s screenshotSession) AddSubmodule(context.Context, string, string, string) error {
	return nil
}
func (s screenshotSession) UpdateSubmodule(context.Context, string) error { return nil }
func (s screenshotSession) DeleteSubmodule(context.Context, string) error { return nil }
func (s screenshotSession) BlameFile(context.Context, repository.File) ([]repository.BlameLine, error) {
	return []repository.BlameLine{
		{Hash: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Author: "Ada Lovelace", Date: "2026-01-01", Text: "package main"},
		{Hash: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Author: "Ada Lovelace", Date: "2026-01-01", Text: ""},
		{Hash: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Author: "Ada Lovelace", Date: "2026-01-01", Text: "import \"fmt\""},
		{Hash: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", Author: "Ada Lovelace", Date: "2026-01-01", Text: ""},
		{Hash: "b2c3d4e5f60718293a4b5c6d7e8f901234567890", Author: "Alan Turing", Date: "2025-11-02", Text: "func main() {"},
		{Hash: "b2c3d4e5f60718293a4b5c6d7e8f901234567890", Author: "Alan Turing", Date: "2025-11-02", Text: "\tfmt.Println(\"Hello, TanGit\")"},
		{Hash: "b2c3d4e5f60718293a4b5c6d7e8f901234567890", Author: "Alan Turing", Date: "2025-11-02", Text: "}"},
	}, nil
}
func (s screenshotSession) CommitFiles(context.Context, repository.Commit) ([]repository.CommitFile, error) {
	return []repository.CommitFile{
		{Path: "internal/repository/session.go", Status: "M", LinesAdded: 100, LinesDeleted: 10},
		{Path: "main.go", Status: "M", LinesAdded: 8, LinesDeleted: 30},
		{Path: "docs/notes.md", Status: "A", LinesAdded: 24},
	}, nil
}
func (s screenshotSession) RestoreCommitFile(context.Context, repository.Commit, repository.CommitFile) error {
	return nil
}

func (s screenshotSession) CommitFileDiff(context.Context, repository.Commit, repository.CommitFile, repository.DiffOptions) (repository.Diff, error) {
	return repository.Diff{Patch: screenshotPatch, Kind: "Commit"}, nil
}
func (s screenshotSession) CommitFilePatch(context.Context, repository.Commit, repository.CommitFile) (string, error) {
	return screenshotPatch, nil
}
func (s screenshotSession) PreviewPatchText(_ context.Context, text string) (repository.PatchPreview, error) {
	return repository.PatchPreview{Text: text, Clean: true}, nil
}

func (s screenshotSession) BranchCommits(context.Context, string, int) ([]repository.Commit, error) {
	return s.snapshot.Commits, nil
}

func (s screenshotSession) SearchCommits(_ context.Context, filter repository.CommitFilter) ([]repository.Commit, error) {
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	author := strings.ToLower(strings.TrimSpace(filter.Author))
	commits := make([]repository.Commit, 0, len(s.snapshot.Commits))
	for _, commit := range s.snapshot.Commits {
		if query != "" &&
			!strings.Contains(strings.ToLower(commit.Subject), query) &&
			!strings.Contains(strings.ToLower(commit.Hash), query) &&
			!strings.Contains(strings.ToLower(commit.ShortHash), query) {
			continue
		}
		if author != "" && !strings.Contains(strings.ToLower(commit.AuthorName), author) {
			continue
		}
		commits = append(commits, commit)
		if filter.Limit > 0 && len(commits) == filter.Limit {
			break
		}
	}
	return commits, nil
}

func (s screenshotSession) Authors(context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	authors := make([]string, 0, 4)
	for _, commit := range s.snapshot.Commits {
		if commit.AuthorName == "" {
			continue
		}
		if _, ok := seen[commit.AuthorName]; ok {
			continue
		}
		seen[commit.AuthorName] = struct{}{}
		authors = append(authors, commit.AuthorName)
	}
	sort.Strings(authors)
	return authors, nil
}

func (s screenshotSession) FileCommits(_ context.Context, path string, limit, skip int) ([]repository.Commit, error) {
	commits := s.snapshot.Commits
	if skip >= len(commits) {
		return nil, nil
	}
	commits = commits[skip:]
	if limit > 0 && limit < len(commits) {
		commits = commits[:limit]
	}
	return commits, nil
}

func (s screenshotSession) RecentCommits(_ context.Context, limit, skip int) ([]repository.Commit, error) {
	commits := s.snapshot.Commits
	if skip >= len(commits) {
		return nil, nil
	}
	commits = commits[skip:]
	if limit > 0 && limit < len(commits) {
		commits = commits[:limit]
	}
	return commits, nil
}

type screenshotOpener struct {
	snapshot repository.Snapshot
}

func (o screenshotOpener) Open(context.Context, string) (repository.Session, error) {
	return screenshotSession{snapshot: o.snapshot}, nil
}

func (screenshotOpener) Init(context.Context, string) error { return nil }

func newScreenshotHarness(t *testing.T, dark bool) (*app, *ui.Tester, *updates) {
	t.Helper()
	return newScreenshotHarnessFor(t, dark, screenshotSnapshot())
}

// newScreenshotHarnessFor opens a repository with the given snapshot, for the
// states the default snapshot does not cover, such as a stopped merge.
func newScreenshotHarnessFor(t *testing.T, dark bool, snapshot repository.Snapshot) (*app, *ui.Tester, *updates) {
	t.Helper()
	a := newApp(screenshotOpener{snapshot: snapshot})
	// The app now ships on Tokyo Night; the light and dark shots still come
	// from System, so the pair shows both appearances.
	a.settings.Theme = "System"
	a.workspaceTab = sectionChanges
	a.path = "/work/project"
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetDark(dark)
	// The updates of the app run on the test goroutine, as MyGo runs them on
	// the UI loop: building a frame from the goroutine that asked for the
	// update races with the frame the test builds.
	updated := newUpdates()
	a.update = updated.update
	openFixture(t, tt)
	// Wait for the open to land before the states the subtests set up.
	for range 8 {
		if a.state == stateReady && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	// The first frame of the Changes section asks for the chosen file's
	// diff, or reads a conflicted file instead, so wait for that read to
	// land too.
	settleShot(t, tt, updated, func() bool {
		return a.diffState != nil || a.diffMedia != nil || a.diffError != "" || a.conflictState != nil
	})
	return a, tt, updated
}

func newScreenshotApp(t *testing.T, dark bool) (*app, *ui.Tester) {
	t.Helper()
	a, tt, _ := newScreenshotHarness(t, dark)
	return a, tt
}

// settleShot runs the reads a screenshot state starts until they land, so the
// shot shows the detail they fill in rather than a spinner.
func settleShot(t *testing.T, tt *ui.Tester, updated *updates, cond func() bool) {
	t.Helper()
	for range 10 {
		tt.Frame()
		if cond() && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
	}
	tt.Frame()
}

func saveScreenshot(t *testing.T, dir, name string, tt *ui.Tester) {
	t.Helper()
	tt.Frame()
	path := filepath.Join(dir, name+".png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestScreenshot(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("batch-commits-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionHistory
		a.commitSelectionMode = true
		a.commitSelection = map[string]bool{}
		for i, commit := range a.visibleCommits() {
			if i < 2 {
				a.commitSelection[commit.Hash] = true
			}
		}
		tt.Frame()
		settleShot(t, tt, updated, func() bool { return !a.commitFilesLoading && !a.diffLoading })
		saveScreenshot(t, dir, "batch-commits-light", tt)
	})

	t.Run("keyboard-diff-review-dark", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, true)
		a.workspaceTab = sectionHistory
		a.selectedCommit = 0
		tt.Frame()
		settleShot(t, tt, updated, func() bool { return a.commitDiffState != nil })
		tt.Key(0, ui.KeyF6)
		tt.Frame()
		tt.Key(0, ui.KeyF6)
		tt.Frame()
		saveScreenshot(t, dir, "keyboard-diff-review-dark", tt)
	})

	t.Run("empty-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.settings.Theme = "System"
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		tt.Frame()
		saveScreenshot(t, dir, "01-empty-light", tt)
	})

	t.Run("changes-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.reviewedFiles["ui/appevents.go"] = true
		tt.Frame()
		saveScreenshot(t, dir, "02-changes-light", tt)
	})

	t.Run("changes-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.reviewedFiles["ui/appevents.go"] = true
		tt.Frame()
		saveScreenshot(t, dir, "03-changes-dark", tt)
	})

	t.Run("blame-dark", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, true)
		a.beginBlame()
		settleShot(t, tt, updated, func() bool { return a.blameState != nil })
		saveScreenshot(t, dir, "blame-dark", tt)
	})

	for _, appearance := range []struct {
		name string
		dark bool
	}{{"remote-web-light", false}, {"remote-web-dark", true}} {
		t.Run(appearance.name, func(t *testing.T) {
			a, tt, _ := newScreenshotHarness(t, appearance.dark)
			a.workspaceTab = sectionRemotes
			a.selectedRemote, a.selectedRemoteRow = 0, 0
			a.openEditRemoteWebURL(a.snapshot.Remotes[0])
			tt.Frame()
			saveScreenshot(t, dir, appearance.name, tt)
		})
	}

	for _, appearance := range []struct {
		name string
		dark bool
	}{{"remote-edit-light", false}, {"remote-edit-dark", true}} {
		t.Run(appearance.name, func(t *testing.T) {
			a, tt, _ := newScreenshotHarness(t, appearance.dark)
			a.workspaceTab = sectionRemotes
			a.selectedRemote, a.selectedRemoteRow = 0, 0
			a.openEditRemote(a.snapshot.Remotes[0])
			tt.Frame()
			saveScreenshot(t, dir, appearance.name, tt)
		})
	}

	for _, appearance := range []struct {
		name string
		dark bool
	}{{"media-review-light", false}, {"media-review-dark", true}} {
		t.Run(appearance.name, func(t *testing.T) {
			snapshot := screenshotSnapshot()
			snapshot.Files = []repository.File{{Path: "picture.png", Status: " M", Tracked: true, Unstaged: true}}
			a, tt, updated := newScreenshotHarnessFor(t, appearance.dark, snapshot)
			settleShot(t, tt, updated, func() bool { return a.diffMedia != nil && !a.diffLoading })
			saveScreenshot(t, dir, appearance.name, tt)
		})
	}

	t.Run("panes-collapsed-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.navPaneCollapsed = true
		a.filePaneCollapsed = true
		tt.Frame()
		saveScreenshot(t, dir, "55-panes-collapsed-dark", tt)
	})

	// Double-clicking the repository path copies it and the bar says so in
	// its place for a moment, without a toast over the work.
	t.Run("status-path-copied-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		path, ok := tt.Find(a.snapshot.Root)
		if !ok {
			t.Fatalf("the status bar shows no repository path: %v", tt.Texts())
		}
		tt.ClickAt(path.X+4, path.Y+path.H/2)
		tt.ClickAt(path.X+4, path.Y+path.H/2)
		tt.Frame()
		saveScreenshot(t, dir, "52-status-path-copied-light", tt)
	})

	// The palette a fresh install starts on.
	t.Run("changes-tokyo-night", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settings.Theme = defaultTheme
		tt.Frame()
		saveScreenshot(t, dir, "02b-changes-tokyo-night", tt)
	})

	t.Run("directory-selection-tokyo-night", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settings.Theme = defaultTheme
		selected := false
		for i, row := range a.changeRows {
			if row.isDirectory() && row.directory == "internal/repository" {
				a.selectedFile = i
				selected = true
				break
			}
		}
		if !selected {
			t.Fatal("the grouped fixture has no internal/repository directory")
		}
		tt.Frame()
		saveScreenshot(t, dir, "02c-directory-selection-tokyo-night", tt)
	})

	t.Run("code-workspace-dark", func(t *testing.T) {
		stubCodeWorkspace(t, map[string]string{
			"README.md":                 "# TanGit\n\nPrecise commits. Visual history.\n",
			"internal/app/code.go":      "package app\n\nimport \"fmt\"\n\n// codeDocument owns one persistent editor buffer.\ntype codeDocument struct {\n\tpath  string\n\ttitle string\n}\n\nfunc (d *codeDocument) Label() string {\n\treturn fmt.Sprintf(\"%s — %s\", d.title, d.path)\n}\n",
			"internal/app/view_code.go": "package app\n\nfunc (a *app) codeView() {\n\t// Render the repository explorer and editor tabs.\n}\n",
		})
		a, tt, updated := newScreenshotHarness(t, true)
		a.setWorkspaceTab(sectionCode)
		settleShot(t, tt, updated, func() bool { return a.code.filesLoaded && !a.code.filesLoading })
		a.openCodeFile("internal/app/code.go", 0)
		settleShot(t, tt, updated, func() bool {
			document := a.code.documents["internal/app/code.go"]
			return document != nil && document.state != nil
		})
		a.openCodeFile("README.md", 0)
		settleShot(t, tt, updated, func() bool {
			document := a.code.documents["README.md"]
			return document != nil && document.state != nil
		})
		a.openCodeFile("internal/app/code.go", 8)
		settleShot(t, tt, updated, func() bool {
			document := a.code.documents["internal/app/code.go"]
			return document != nil && document.state != nil
		})
		a.setAllFileTreeDirectories(codeFileTreeScope, a.code.files, false)
		a.syncCodeTree()
		tt.Frame()
		saveScreenshot(t, dir, "67-code-workspace-dark", tt)

		a.settings.ExternalEditors = []editorPreset{{Name: "Fleet", Command: "fleet --project {{filename}}"}}
		if err := tt.Click("Open repository in external editor"); err != nil {
			t.Fatal(err)
		}
		saveScreenshot(t, dir, "68b-code-external-editor-dark", tt)
		tt.Key(0, ui.KeyEscape)
		tt.Frame()

		if err := tt.RightClick("Editor tab internal/app/code.go"); err != nil {
			t.Fatal(err)
		}
		saveScreenshot(t, dir, "69-code-tab-menu-dark", tt)
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if err := tt.RightClick("Open internal/app/code.go"); err != nil {
			t.Fatal(err)
		}
		saveScreenshot(t, dir, "70-code-explorer-menu-dark", tt)
		tt.Key(0, ui.KeyEscape)
		tt.Frame()

		a.openCodeFileForm(codeFileFormNewFile, "internal/app", "")
		a.code.fileFormName = "new_file.go"
		saveScreenshot(t, dir, "71-code-new-file-dark", tt)
		a.closeCodeFileForm()
		tt.Frame()

		source, ok := tt.Find("Open README.md")
		if !ok {
			t.Fatal("the drag fixture has no README source")
		}
		target, ok := tt.Find("Collapse directory internal/app")
		if !ok {
			t.Fatal("the drag fixture has no destination folder")
		}
		tt.Press(source.X+source.W/2, source.Y+source.H/2)
		tt.Move(target.X+target.W/2, target.Y+target.H/2)
		saveScreenshot(t, dir, "72-code-drag-dark", tt)
		tt.Key(0, ui.KeyEscape)
		tt.Release(target.X+target.W/2, target.Y+target.H/2)
		tt.Frame()

		a.openCodeWorkspaceFolder("internal/app")
		tt.Frame()
		saveScreenshot(t, dir, "83-code-nested-workspace-dark", tt)

		a.code.sidebar = codeSidebarSearch
		a.code.searchQuery, a.code.searchSubmitted = "code", "code"
		a.code.searchFolder = "internal/app"
		a.code.searchFolderDraft = "internal/app"
		a.code.searchSubmittedOptions = workspaceSearchOptions{Query: "code", Folder: "internal/app"}
		a.code.searchInclude = "**/*.go"
		a.code.searchExclude = "**/*_test.go"
		a.code.searchBackend = workspaceSearchBackendRipgrep
		a.code.searchResults = []workspaceSearchMatch{
			{Path: "internal/app/code.go", Line: 5, Column: 4, Text: "// codeDocument owns one persistent editor buffer."},
			{Path: "internal/app/code.go", Line: 6, Column: 6, Text: "type codeDocument struct {"},
			{Path: "internal/app/view_code.go", Line: 3, Column: 14, Text: "func (a *app) codeView() {"},
			{Path: "internal/app/view_code.go", Line: 4, Column: 15, Text: "// Render the code explorer and editor tabs."},
		}
		a.code.searchResults = rankWorkspaceSearchMatches(a.code.searchResults, a.code.searchSubmittedOptions)
		a.code.searchSelected = 0
		a.rebuildCodeSearchRows()
		saveScreenshot(t, dir, "68-code-search-dark", tt)

		a.stopGoLanguageServer()
		a.goLanguageServer = newFakeLanguageServerConnection()
		a.goLanguageServerDocs = make(map[string]uint64)
		tt.Frame()
		status, ok := tt.Find("Language servers")
		if !ok {
			t.Fatal("the running language server has no status icon")
		}
		tt.Move(status.X+status.W/2, status.Y+status.H/2)
		tt.Frame()
		saveScreenshot(t, dir, "74-code-language-servers-dark", tt)
		tt.Key(0, ui.KeyEscape)
		tt.Frame()

		a.code.sidebar = codeSidebarFiles
		a.code.filter = "vcgo"
		a.syncCodeTree()
		tt.Frame()
		saveScreenshot(t, dir, "76-code-quick-open-fzf-dark", tt)

		a.code.filter = "README.md"
		a.syncCodeTree()
		a.code.filter = ""
		a.syncCodeTree()
		a.sectionSearch[sectionCode].Focus()
		tt.Frame()
		saveScreenshot(t, dir, "75-code-clear-filter-dark", tt)
	})

	t.Run("terminal-dock-light", func(t *testing.T) {
		original := newTerminal
		peers := make(chan monitoredTerminalPeer, 3)
		newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
			return newMonitoredTerminal(options, peers)
		}
		defer func() { newTerminal = original }()
		a, tt, updated := newScreenshotHarness(t, false)
		a.openTerminal()
		tt.Frame()
		settleShot(t, tt, updated, func() bool { return a.term.sessions[0].pane != nil })
		first := <-peers
		defer first.Close()
		tt.Advance(250 * time.Millisecond)
		tt.Frame()
		saveScreenshot(t, dir, "66-terminal-dock-light", tt)
		if err := tt.Click("New terminal tab"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		settleShot(t, tt, updated, func() bool { return a.activeTerminal().pane != nil })
		second := <-peers
		defer second.Close()
		tt.Frame()
		saveScreenshot(t, dir, "73-terminal-tabs-light", tt)
		if err := tt.Click("Maximize terminal"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(250 * time.Millisecond)
		tt.Frame()
		saveScreenshot(t, dir, "74-terminal-maximized-light", tt)
		if err := tt.Click("Restore terminal size"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(250 * time.Millisecond)
		tt.Frame()
		saveScreenshot(t, dir, "75-terminal-restored-light", tt)
		if err := tt.Click("Split terminal right"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		settleShot(t, tt, updated, func() bool {
			return len(a.term.sessions) == 3 && a.term.sessions[2].pane != nil
		})
		third := <-peers
		defer third.Close()
		tt.Frame()
		tab, found := tt.Find("Terminal tab 2")
		if !found {
			t.Fatal("the second terminal tab is missing")
		}
		tt.RightClickAt(tab.X+tab.W/2, tab.Y+tab.H/2)
		saveScreenshot(t, dir, "76-terminal-tab-menu-light", tt)
		if !tt.HasText("Rename tab") || !tt.HasText("Close terminal tab") {
			t.Fatalf("the terminal tab menu did not open: %q", tt.Texts())
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
		if err := tt.Click("Close terminal tab 2"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "77-terminal-close-tab-confirmation-light", tt)
		if err := tt.Click("Cancel"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	})

	t.Run("terminal-split-dark", func(t *testing.T) {
		original := newTerminal
		peers := make(chan monitoredTerminalPeer, terminalSessionLimit)
		newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
			return newMonitoredTerminal(options, peers)
		}
		defer func() { newTerminal = original }()
		a, tt, updated := newScreenshotHarness(t, true)
		a.openTerminal()
		tt.Frame()
		settleShot(t, tt, updated, func() bool { return a.term.sessions[0].pane != nil })
		first := <-peers
		defer first.Close()
		tt.Advance(250 * time.Millisecond)
		tt.Frame()
		if err := tt.Click("Split terminal right"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		settleShot(t, tt, updated, func() bool { return a.term.sessions[1].pane != nil })
		second := <-peers
		defer second.Close()
		saveScreenshot(t, dir, "56-terminal-split-dark", tt)
		if _, err := second.Write([]byte("build finished: needle in scrollback\r\n")); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click("Search terminal"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Type("needle")
		settleShot(t, tt, updated, func() bool { return a.term.sessions[1].searchStatus.Total == 1 })
		saveScreenshot(t, dir, "64-terminal-search-dark", tt)
		a.closeActiveTerminalSearch()
		tt.Frame()
		grip, found := tt.Find("Resize terminal split 1")
		if !found {
			t.Fatal("the terminal divider grip is missing")
		}
		tt.Move(grip.X+grip.W/2, grip.Y+grip.H/2)
		tt.Frame()
		saveScreenshot(t, dir, "60-terminal-divider-hover-dark", tt)
		tt.Move(0, 0)
		tt.Frame()
		if err := tt.Click("Rename terminal 2"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "61-terminal-rename-dark", tt)
		tt.Type("Build output")
		if err := tt.Click("Save name"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "67-terminal-independent-names-dark", tt)
		a.term.layout.below = true
		tt.Frame()
		saveScreenshot(t, dir, "59-terminal-split-below-dark", tt)

		firstTab, _ := tt.Find("Select terminal 1")
		secondTab, _ := tt.Find("Select terminal 2")
		tt.Press(firstTab.X+30, firstTab.Y+firstTab.H/2)
		tt.Move(secondTab.X+30, secondTab.Y+secondTab.H/2)
		tt.Frame()
		saveScreenshot(t, dir, "57-terminal-drag-placeholder-dark", tt)
		tt.Release(secondTab.X+30, secondTab.Y+secondTab.H/2)
		tt.Frame()
		if err := tt.Click("Close all terminals"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "58-terminal-close-all-confirmation-dark", tt)
		if err := tt.Click("Cancel"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		for index := 2; index < terminalSessionLimit; index++ {
			if err := tt.Click("Split terminal right"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			settleShot(t, tt, updated, func() bool {
				return len(a.term.sessions) > index && a.term.sessions[index].pane != nil
			})
			peer := <-peers
			defer peer.Close()
		}
		saveScreenshot(t, dir, "62-terminal-six-cards-dark", tt)
		a.resetTerminal()
	})

	t.Run("terminal-mixed-splits-dark", func(t *testing.T) {
		original := newTerminal
		peers := make(chan monitoredTerminalPeer, terminalSessionLimit)
		newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
			return newMonitoredTerminal(options, peers)
		}
		defer func() { newTerminal = original }()
		a, tt, updated := newScreenshotHarness(t, true)
		var opened []monitoredTerminalPeer
		defer func() {
			for _, peer := range opened {
				_ = peer.Close()
			}
		}()
		a.openTerminal()
		tt.Frame()
		settleShot(t, tt, updated, func() bool { return a.term.sessions[0].pane != nil })
		opened = append(opened, <-peers)
		tt.Advance(250 * time.Millisecond)
		tt.Frame()
		split := func(label string) {
			t.Helper()
			count := a.terminalSessionCount()
			if err := tt.Click(label); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			settleShot(t, tt, updated, func() bool {
				return a.terminalSessionCount() == count+1 && a.activeTerminal().pane != nil
			})
			opened = append(opened, <-peers)
			tt.Frame()
		}
		split("Split terminal right")
		if err := tt.Click("Focus terminal 1"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		split("Split terminal below")
		if err := tt.Click("Focus terminal 2"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		split("Split terminal right")
		saveScreenshot(t, dir, "63-terminal-mixed-splits-dark", tt)
		a.resetTerminal()
	})

	// Selected lines in the diff turn its caption into the action that moves
	// just them, leaving the rest of the file where it is.
	t.Run("line-selection-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		settleShot(t, tt, updated, func() bool {
			return a.diffState != nil && a.diffPatch != "" && !a.diffLoading
		})
		a.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 2}, true)
		tt.Frame()
		saveScreenshot(t, dir, "36-line-selection-light", tt)
	})

	// A file git does not know is shown as the patch that adds it, and its
	// lines are picked like any other file's.
	t.Run("new-file-line-selection-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		for i, row := range a.changeRows {
			if !row.isHeader() && row.file.Path == "docs/notes.md" {
				a.selectedFile = i
			}
		}
		settleShot(t, tt, updated, func() bool {
			return a.diffState != nil && a.diffPatch != "" && !a.diffLoading
		})
		a.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 4, End: 5}, true)
		tt.Frame()
		saveScreenshot(t, dir, "37-new-file-lines-light", tt)
	})

	// Independent clicks mark only their own changes; the context line between
	// them stays outside both the selection and its patch.
	t.Run("line-selection-mode-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		settleShot(t, tt, updated, func() bool {
			return a.diffState != nil && a.diffPatch != "" && !a.diffLoading
		})
		if err := tt.Click("Select multiple lines"); err != nil {
			t.Fatalf("enter the multi-line mode: %v", err)
		}
		tt.Frame()
		for _, line := range []editor.DiffLineRange{
			{Side: editor.DiffNew, Start: 1, End: 1},
			{Side: editor.DiffNew, Start: 3, End: 3},
		} {
			// The editor sets its native range before reporting each gutter click.
			a.diffState.SetSelectedLines(line, true)
			a.extendLineSelection(a.diffState, line)
			tt.Frame()
		}
		saveScreenshot(t, dir, "40-lines-mode-light", tt)
	})

	t.Run("history-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.workspaceTab = sectionHistory
		saveScreenshot(t, dir, "04-history-light", tt)
	})

	t.Run("graph-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionGraph
		settleShot(t, tt, updated, func() bool {
			return !a.commitDiffLoading && a.commitDiffState != nil
		})
		saveScreenshot(t, dir, "05-graph-light", tt)
	})

	t.Run("graph-dark", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, true)
		a.workspaceTab = sectionGraph
		settleShot(t, tt, updated, func() bool {
			return !a.commitDiffLoading && a.commitDiffState != nil
		})
		saveScreenshot(t, dir, "06-graph-dark", tt)
	})

	t.Run("branches-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.selectedBranch = 1
		a.workspaceTab = sectionBranches
		tt.Frame()
		settleShot(t, tt, updated, func() bool {
			return !a.branchCommitsLoading && a.branchCommitDiffState != nil
		})
		saveScreenshot(t, dir, "07-branches-light", tt)
	})

	t.Run("branches-chinese-light", func(t *testing.T) {
		t.Cleanup(func() { currentLanguage = defaultLanguage })
		a, tt, updated := newScreenshotHarness(t, false)
		a.settings.Language = "zh-Hans"
		a.selectedBranch = 1
		a.workspaceTab = sectionBranches
		tt.Frame()
		settleShot(t, tt, updated, func() bool {
			return !a.branchCommitsLoading && a.branchCommitDiffState != nil
		})
		saveScreenshot(t, dir, "62-branches-chinese-light", tt)
	})

	// The plus beside Local branches opens one modal naming the new branch
	// and the branch it starts from.
	t.Run("branch-create-modal-light", func(t *testing.T) {
		a, tt, _ := newScreenshotHarness(t, false)
		a.workspaceTab = sectionBranches
		a.openBranchForm("main")
		a.newBranchName = "feature/login"
		tt.Frame()
		saveScreenshot(t, dir, "33-branch-create-modal-light", tt)
	})

	t.Run("remote-create-modal-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
		a.workspaceTab = sectionRemotes
		a.showRemoteForm = true
		tt.Frame()
		saveScreenshot(t, dir, "53-remote-create-modal-light", tt)
	})

	t.Run("remote-edit-modal-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
		a.workspaceTab = sectionRemotes
		a.openEditRemote(a.snapshot.Remotes[0])
		tt.Frame()
		saveScreenshot(t, dir, "53b-remote-edit-modal-light", tt)
	})

	t.Run("worktree-create-modal-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
		a.workspaceTab = sectionWorktrees
		a.beginNewWorktree()
		tt.Frame()
		saveScreenshot(t, dir, "54-worktree-create-modal-light", tt)
	})

	t.Run("stash-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionStash
		tt.Frame()
		settleShot(t, tt, updated, func() bool {
			return !a.stashFilesLoading && a.stashDiffState != nil
		})
		saveScreenshot(t, dir, "08-stash-light", tt)
	})

	// The same page in the palette the app ships with.
	t.Run("stash-tokyo-night", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, true)
		a.settings.Theme = defaultTheme
		a.workspaceTab = sectionStash
		tt.Frame()
		settleShot(t, tt, updated, func() bool {
			return !a.stashFilesLoading && a.stashDiffState != nil
		})
		saveScreenshot(t, dir, "35-stash-tokyo-night", tt)
	})

	// The plus beside Saved work opens one modal naming the stash and
	// choosing whether untracked files come with it; an empty name falls
	// back to a timestamped one.
	t.Run("stash-changes-modal-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.workspaceTab = sectionStash
		a.openStashForm()
		tt.Frame()
		saveScreenshot(t, dir, "34-stash-changes-modal-light", tt)
	})

	t.Run("tags-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
		a.workspaceTab = sectionTags
		saveScreenshot(t, dir, "09-tags-light", tt)
	})

	t.Run("worktrees-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
		a.workspaceTab = sectionWorktrees
		saveScreenshot(t, dir, "10-worktrees-light", tt)
	})

	t.Run("menu-file-row", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		file, ok := tt.Find("main.go")
		if !ok {
			t.Fatal("the fixture has no main.go row")
		}
		// Aim at the visible name, not the label's center, which is beneath
		// the change statistics drawn over the remaining row width.
		tt.RightClickAt(file.X+8, file.Y+file.H/2)
		tt.Frame()
		// The menu selects the row it acts on, whose diff then lands.
		settleShot(t, tt, updated, func() bool {
			return a.diffState != nil && !a.diffLoading
		})
		saveScreenshot(t, dir, "11-menu-file-row", tt)
		tt.CloseMenu()
	})

	t.Run("file-history-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionHistory
		settleShot(t, tt, updated, func() bool {
			return len(a.commitFiles) > 0
		})
		a.openFileHistory("internal/repository/session.go")
		settleShot(t, tt, updated, func() bool {
			return len(a.fileHistoryCommits) > 0 && len(a.fileHistoryFiles) > 0 && a.fileHistoryDiffState != nil
		})
		saveScreenshot(t, dir, "14-file-history-light", tt)
		tt.SetSize(720, 800)
		tt.Frame()
		saveScreenshot(t, dir, "14b-file-history-narrow-light", tt)
	})

	t.Run("file-history-dark", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, true)
		a.workspaceTab = sectionHistory
		settleShot(t, tt, updated, func() bool {
			return len(a.commitFiles) > 0
		})
		a.openFileHistory("main.go")
		settleShot(t, tt, updated, func() bool {
			return len(a.fileHistoryCommits) > 0 && len(a.fileHistoryFiles) > 0 && a.fileHistoryDiffState != nil
		})
		saveScreenshot(t, dir, "16-file-history-dark", tt)
		if err := tt.Click("Collapse file history commits"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if err := tt.Click("Collapse changed files"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "16b-file-history-collapsed-dark", tt)
	})

	t.Run("settings-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.openSettings()
		tt.Frame()
		saveScreenshot(t, dir, "17-settings-light", tt)
	})

	t.Run("settings-narrow-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.openSettings()
		tt.SetSize(720, 800)
		tt.Frame()
		saveScreenshot(t, dir, "17b-settings-narrow-light", tt)
	})

	t.Run("settings-language-menu-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.openSettings()
		tt.Frame()
		if err := tt.Click("English"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "60-settings-language-menu-light", tt)
	})

	t.Run("settings-french-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.settings.Language = "fr"
		a.openSettings()
		tt.Frame()
		saveScreenshot(t, dir, "61-settings-french-light", tt)
	})

	t.Run("settings-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settings.Theme = "Tokyo Night"
		a.openSettings()
		tt.Frame()
		saveScreenshot(t, dir, "18-settings-dark", tt)
	})

	t.Run("settings-search-exclusions-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settings.SearchExcludedDirectories = []string{"cooked", "dist", "node_modules"}
		a.openSettings()
		tt.Frame()
		if err := tt.Click("Editor settings"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "77-settings-search-exclusions-dark", tt)
	})

	t.Run("settings-custom-editor-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settingsPage = settingsPageEditor
		a.openSettings()
		tt.Frame()
		if err := tt.Click("Add editor…"); err != nil {
			t.Fatal(err)
		}
		a.customEditorFormName = "Fleet"
		a.customEditorFormCommand = "fleet --project {{filename}}"
		tt.Frame()
		saveScreenshot(t, dir, "78-settings-custom-editor-dark", tt)
	})

	t.Run("settings-language-server-catalog-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settingsPage = settingsPageEditor
		a.openSettings()
		tt.Frame()
		tt.Scroll(700, 480, 0, 1800)
		tt.Frame()
		saveScreenshot(t, dir, "79-settings-language-server-catalog-dark", tt)
	})

	t.Run("settings-go-language-server-catalog-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settingsPage = settingsPageEditor
		a.openSettings()
		a.languageServerCatalogQuery = "gopls"
		a.languageServerCatalogSelected = goLanguageServerID
		tt.Frame()
		tt.Scroll(700, 480, 0, 1800)
		tt.Frame()
		tt.Scroll(700, 480, 0, 300)
		tt.Frame()
		saveScreenshot(t, dir, "80-settings-go-language-server-catalog-dark", tt)
	})

	t.Run("settings-custom-language-server-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settingsPage = settingsPageEditor
		a.languageServerCatalogQuery = "ty"
		a.openSettings()
		a.customLanguageServerForm = customLanguageServerForm{
			Open: true, Name: "ty", Description: "Astral Python language server",
			Path: "ty, /opt/python/ty", Arguments: "server", Probe: "--version",
			Extensions: ".py=python, .pyi=python", DownloadURL: "https://pypi.org/project/ty/", Enabled: true,
		}
		tt.Frame()
		tt.Scroll(700, 480, 0, 1800)
		tt.Frame()
		tt.Scroll(700, 480, 0, -300)
		tt.Frame()
		saveScreenshot(t, dir, "81-settings-custom-language-server-form-dark", tt)

		a.customLanguageServerForm.Open = false
		a.settings.LanguageServers = map[string]languageServerSettings{
			"custom-ty": {
				Path: "ty, /opt/python/ty", Arguments: []string{"server"}, ProbeArguments: []string{"--version"},
				Custom: &customLanguageServerDefinition{
					Name: "ty", Description: "Astral Python language server", DownloadURL: "https://pypi.org/project/ty/",
					Extensions: map[string]string{".py": "python", ".pyi": "python"},
				},
			},
		}
		a.loadGoLanguageServerDraft()
		a.languageServerCatalogQuery = "ty"
		tt.Frame()
		tt.Scroll(700, 480, 0, 1800)
		tt.Frame()
		saveScreenshot(t, dir, "82-settings-custom-language-server-card-dark", tt)
	})

	t.Run("settings-font-menu-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.openSettings()
		tt.Frame()
		if err := tt.Click("Typography settings"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "65-settings-typography-groups-dark", tt)
		tt.Scroll(700, 400, 0, 240)
		tt.Frame()
		if err := tt.Click("Search buffer fonts"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type("mono")
		tt.Frame()
		saveScreenshot(t, dir, "65-settings-font-menu-dark", tt)
	})

	// The accent row is a pick, not a name: the chosen color repaints the
	// dialog itself, so the choice is visible before it is left.
	t.Run("settings-accent-tokyo-night", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settings.Theme = defaultTheme
		a.settings.Accent = "#7c3aed"
		a.openSettings()
		tt.Frame()
		saveScreenshot(t, dir, "49-settings-accent-tokyo-night", tt)
		if err := tt.Click("Custom accent color"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "49b-settings-custom-accent-tokyo-night", tt)
	})

	// Notifications sit at the window's top right, under the tab bar, with
	// the newest highest: the place the eye goes for what just happened.
	t.Run("notifications-top-right-tokyo-night", func(t *testing.T) {
		s, tt, _, home := newShellShot(t, true, defaultTheme)
		s.activate(s.indexOf(home))
		home.app.notify("success", "Copied commit hash", "9f2c1ab4e7d8c3f0")
		home.app.notify("error", "Git action failed",
			"fatal: Could not read from remote repository.")
		tt.Frame()
		saveScreenshot(t, dir, "51-notifications-top-right-tokyo-night", tt)
	})

	// The command log is text the pointer can select, so a failure can be
	// pasted elsewhere without copying the whole record.
	t.Run("command-log-selection-tokyo-night", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.settings.Theme = defaultTheme
		output := "remote: OTP verification is required to access the repository.\n" +
			"remote: Use: ssh git@git.example.com verify_device\n" +
			"remote:\n" +
			"fatal: Could not read from remote repository.\n\n" +
			"Please make sure you have the correct access rights\n" +
			"and the repository exists."
		log := repository.NewCommandLog()
		log.Note(output)
		a.commandLog = log
		a.openCommandLog()
		tt.Frame()
		box, ok := tt.Find(output)
		if !ok {
			t.Fatalf("the log does not show its output: %v", tt.Texts())
		}
		tt.Press(box.X+box.W*0.05, box.Y+box.H*0.2)
		tt.Move(box.X+box.W*0.62, box.Y+box.H*0.6)
		tt.Release(box.X+box.W*0.62, box.Y+box.H*0.6)
		saveScreenshot(t, dir, "50-command-log-selection-tokyo-night", tt)
	})

	// Page chips show enabled and disabled sections directly.
	t.Run("settings-sections-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.openSettings()
		tt.Frame()
		if err := tt.Click("Workspace settings"); err != nil {
			t.Fatalf("open Workspace settings: %v", err)
		}
		tt.Frame()
		if err := tt.Click("Show Tags"); err != nil {
			t.Fatalf("toggle the Tags chip: %v", err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "32-settings-sections-light", tt)
		tt.SetDark(true)
		tt.Frame()
		saveScreenshot(t, dir, "32-settings-sections-dark", tt)
		a.moveWorkspaceSection("tags", "code")
		tt.Frame()
		saveScreenshot(t, dir, "32-settings-sections-reordered", tt)
		tt.SetSize(1000, 760)
		tt.Frame()
		saveScreenshot(t, dir, "32-settings-sections-narrow", tt)
	})

	// The behavior block, where the switch that turns every shortcut off
	// sits beside the diff and untracked-file preferences.
	t.Run("settings-behavior-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.openSettings()
		tt.Frame()
		if err := tt.Click("Behavior settings"); err != nil {
			t.Fatalf("open Behavior settings: %v", err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "55-settings-behavior-light", tt)
	})

	// The version row and the button that opens the releases page; nothing is
	// checked or compared.
	t.Run("settings-updates-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.openSettings()
		tt.Frame()
		if err := tt.Click("Updates settings"); err != nil {
			t.Fatalf("open Updates settings: %v", err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "57-settings-updates-light", tt)
	})

	// The shortcut reference is one list for every gesture, filterable and
	// switchable off.
	t.Run("keybindings-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.openKeybindings()
		tt.Frame()
		saveScreenshot(t, dir, "26-keybindings-light", tt)
	})

	t.Run("current-keybindings-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
		a.workspaceTab = sectionRemotes
		a.openCurrentKeybindings()
		tt.Frame()
		saveScreenshot(t, dir, "26b-current-keybindings-light", tt)
	})

	t.Run("changes-split-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.toggleDiffLayout()
		tt.Frame()
		saveScreenshot(t, dir, "15-changes-split-light", tt)
	})

	// The commit action bar shows on the row the pointer or the choice is on;
	// choosing a commit in the history puts it there.
	t.Run("history-actions-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionHistory
		tt.Frame()
		if err := tt.Click("Add native file diffs and staging"); err != nil {
			t.Fatal(err)
		}
		settleShot(t, tt, updated, func() bool {
			return a.commitDiffState != nil && !a.commitDiffLoading
		})
		saveScreenshot(t, dir, "20-history-actions-light", tt)
	})

	// A stopped merge: the banner above the two sections, and the resolution
	// toolbar of the conflicted file in the pane.
	t.Run("conflict-light", func(t *testing.T) {
		originalRead := readWorkingFile
		t.Cleanup(func() { readWorkingFile = originalRead })
		readWorkingFile = func(string) ([]byte, error) { return []byte(screenshotConflictFile), nil }

		snapshot := screenshotSnapshot()
		snapshot.Operation = "merge"
		snapshot.Files = []repository.File{
			{Path: "internal/repository/session.go", Status: "UU", Unstaged: true, Tracked: true, HasMergeConflicts: true},
			{Path: "main.go", Status: " M", Unstaged: true, Tracked: true, LinesAdded: 2},
			{Path: "docs/notes.md", Status: "??", Unstaged: true, Added: true},
		}
		a, tt, updated := newScreenshotHarnessFor(t, false, snapshot)
		settleShot(t, tt, updated, func() bool {
			return a.conflictState != nil && len(updated.done) == 0
		})
		saveScreenshot(t, dir, "21-conflict-light", tt)
	})

	// The same conflicted file reviewed rather than edited: the diff of the
	// current side against the incoming one, with the toggle back to the
	// markers.
	t.Run("conflict-review-light", func(t *testing.T) {
		originalRead := readWorkingFile
		t.Cleanup(func() { readWorkingFile = originalRead })
		readWorkingFile = func(string) ([]byte, error) { return []byte(screenshotConflictFile), nil }

		snapshot := screenshotSnapshot()
		snapshot.Operation = "merge"
		snapshot.Files = []repository.File{
			{Path: "internal/repository/session.go", Status: "UU", Unstaged: true, Tracked: true, HasMergeConflicts: true},
			{Path: "main.go", Status: " M", Unstaged: true, Tracked: true, LinesAdded: 2},
			{Path: "docs/notes.md", Status: "??", Unstaged: true, Added: true},
		}
		a, tt, updated := newScreenshotHarnessFor(t, false, snapshot)
		settleShot(t, tt, updated, func() bool {
			return a.conflictState != nil && len(updated.done) == 0
		})
		if err := tt.Click("Review both sides"); err != nil {
			t.Fatal(err)
		}
		settleShot(t, tt, updated, func() bool {
			return a.conflictReview && a.conflictDiffState != nil && len(updated.done) == 0
		})
		saveScreenshot(t, dir, "22-conflict-review-light", tt)
	})

	// The file pane's editor: an untracked file shown as text that is saved
	// as it is typed, with the diff it replaces a Done click away.
	t.Run("file-edit-light", func(t *testing.T) {
		originalRead, originalWrite := readWorkingFile, writeWorkingFile
		t.Cleanup(func() {
			readWorkingFile, writeWorkingFile = originalRead, originalWrite
		})
		readWorkingFile = func(string) ([]byte, error) { return []byte(screenshotEditableFile), nil }
		writeWorkingFile = func(string, []byte) error { return nil }

		snapshot := screenshotSnapshot()
		snapshot.Files = []repository.File{
			{Path: "examples/server/analytics/core/data_mapping.json", Status: "??", Unstaged: true, Added: true},
		}
		a, tt, updated := newScreenshotHarnessFor(t, false, snapshot)
		a.openFileEdit()
		settleShot(t, tt, updated, func() bool {
			return !a.fileEditLoading && a.fileEditState != nil
		})
		if done, ok := tt.Find("Done"); ok {
			tt.ClickAt(done.X+done.W/2, done.Y+done.H+120)
			tt.Frame()
		}
		saveScreenshot(t, dir, "54-file-edit-light", tt)
	})

	// The full-window diff, with the shrink icon at its top right.
	t.Run("diff-fullscreen-light", func(t *testing.T) {
		a, tt := newScreenshotApp(t, false)
		a.openDiffFullscreen()
		tt.Frame()
		saveScreenshot(t, dir, "22-diff-fullscreen-light", tt)
	})

	t.Run("diff-fullscreen-dark", func(t *testing.T) {
		a, tt := newScreenshotApp(t, true)
		a.openDiffFullscreen()
		tt.Frame()
		saveScreenshot(t, dir, "23-diff-fullscreen-dark", tt)
	})

	// The whole-window diff lists the files of the pane it reads, so another
	// one is a click away, and a line between them is dragged.
	t.Run("diff-fullscreen-files-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionHistory
		settleShot(t, tt, updated, func() bool {
			return a.commitDiffState != nil && !a.commitDiffLoading
		})
		a.openDiffFullscreen()
		tt.Frame()
		saveScreenshot(t, dir, "41-diff-files-light", tt)
		if err := tt.Click("Collapse changed files"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "41b-diff-files-collapsed-light", tt)
	})

	t.Run("diff-fullscreen-files-tokyo-night", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, true)
		a.settings.Theme = defaultTheme
		a.workspaceTab = sectionChanges
		settleShot(t, tt, updated, func() bool {
			return a.diffState != nil && a.diffPatch != "" && !a.diffLoading
		})
		a.openDiffFullscreen()
		tt.Frame()
		saveScreenshot(t, dir, "42-diff-files-tokyo-night", tt)
	})

	t.Run("picker-light", func(t *testing.T) {
		tt := newPickerShot(t, false, "")
		saveScreenshot(t, dir, "12-picker-light", tt)
	})

	t.Run("picker-dark", func(t *testing.T) {
		tt := newPickerShot(t, true, "")
		saveScreenshot(t, dir, "19-picker-dark", tt)
	})

	// A chosen photograph is soft enough to keep the launcher's text and
	// controls readable, and its Settings preview reuses the same bitmap.
	t.Run("picker-background-dark", func(t *testing.T) {
		a, tt := newBackgroundPickerShot(t)
		saveScreenshot(t, dir, "58-picker-background-dark", tt)
		a.openSettings()
		tt.Frame()
		saveScreenshot(t, dir, "59-settings-background-dark", tt)
	})

	t.Run("picker-empty-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.settings.Theme = "System"
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		tt.Frame()
		saveScreenshot(t, dir, "12b-picker-empty-light", tt)
	})

	// Narrow windows retain the complete path field, Browse action, search
	// field, recent cards, and configured-editor shortcuts.
	t.Run("picker-narrow-dark", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.recentRepositories = []recentRepository{
			{Path: "/work/lazygitApp", LastOpened: timeNow()},
			{Path: "/work/project", LastOpened: timeNow() - 3*3600},
		}
		tt := ui.NewTester(a.view, 720, 900)
		tt.SetDark(true)
		tt.Frame()
		saveScreenshot(t, dir, "19b-picker-narrow-dark", tt)
	})

	t.Run("picker-no-search-results-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.recentRepositories = []recentRepository{{Path: "/work/project", LastOpened: timeNow()}}
		a.recentFilter = "missing"
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		tt.Frame()
		saveScreenshot(t, dir, "38b-picker-no-search-results-light", tt)
	})

	// Two folders of the same name are told apart by the name each card was
	// given: the first shows its own, the second its folder's.
	t.Run("picker-names-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.settings.Theme = "System"
		a.recentRepositories = []recentRepository{
			{Path: "/work/client/project", LastOpened: timeNow(), Name: "Client site"},
			{Path: "/work/other/project", LastOpened: timeNow() - 3*3600},
		}
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		saveScreenshot(t, dir, "38-picker-names-light", tt)
	})

	// The pencil on a card opens one modal that names the card alone; the
	// folder's own name is offered as the placeholder.
	t.Run("picker-rename-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.settings.Theme = "System"
		a.recentRepositories = []recentRepository{
			{Path: "/work/client/project", LastOpened: timeNow()},
			{Path: "/work/other/project", LastOpened: timeNow() - 3*3600},
		}
		a.openRecentRename("/work/client/project")
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		saveScreenshot(t, dir, "39-picker-rename-light", tt)
	})

	// While a repository opens the picker steps aside for one loading page.
	t.Run("loading-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: screenshotSnapshot()})
		a.settings.Theme = "System"
		a.state = stateLoading
		a.path = "/tmp/lazygit_repo"
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		saveScreenshot(t, dir, "24-loading-light", tt)
	})

	// A folder with no Git repository is offered adoption instead of an
	// error, and can still be left alone.
	t.Run("init-prompt-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: screenshotSnapshot()})
		a.settings.Theme = "System"
		a.recentRepositories = []recentRepository{
			{Path: "/work/design-docs", LastOpened: timeNow()},
		}
		a.offerInitPath = "/work/design-docs"
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		saveScreenshot(t, dir, "25-init-prompt-light", tt)
	})

	// A wrong path must not hide the repositories the user can pick instead.
	t.Run("picker-error-light", func(t *testing.T) {
		tt := newPickerShot(t, false, "not a Git repository")
		saveScreenshot(t, dir, "13-picker-error-light", tt)
	})

	// The commit filter, over the History list it narrows.
	t.Run("history-filter-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionHistory
		a.openCommitFilter(sectionHistory)
		settleShot(t, tt, updated, func() bool {
			return !a.authorsLoading && len(a.authors) > 0
		})
		saveScreenshot(t, dir, "27-history-filter-light", tt)
	})

	t.Run("history-filter-dark", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, true)
		a.workspaceTab = sectionHistory
		a.openCommitFilter(sectionHistory)
		settleShot(t, tt, updated, func() bool {
			return !a.authorsLoading && len(a.authors) > 0
		})
		saveScreenshot(t, dir, "28-history-filter-dark", tt)
	})

	// A narrowed History list, with the filter's chip and match count.
	t.Run("history-filtered-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionHistory
		a.historyFilter.applied = repository.CommitFilter{Query: "native"}
		a.historyFilter.draft = a.historyFilter.applied
		a.historyFilter.limit = repository.SnapshotCommitLimit
		a.runCommitFilter(sectionHistory, "")
		settleShot(t, tt, updated, func() bool {
			return !a.historyFilter.loading && len(a.historyFilter.matches) > 0
		})
		saveScreenshot(t, dir, "29-history-filtered-light", tt)
	})

	// The Graph has the same filter, kept apart from History's: its lanes
	// narrow while History stays whole.
	t.Run("graph-filtered-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionGraph
		a.graphFilter.applied = repository.CommitFilter{Query: "graph"}
		a.graphFilter.draft = a.graphFilter.applied
		a.graphFilter.limit = repository.SnapshotCommitLimit
		a.runCommitFilter(sectionGraph, "")
		settleShot(t, tt, updated, func() bool {
			return !a.graphFilter.loading && len(a.graphFilter.matches) > 0
		})
		saveScreenshot(t, dir, "53-graph-filtered-light", tt)
	})

	// The branch commit list's filter has no branch choice.
	t.Run("branch-filter-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.selectedBranch = 1
		a.workspaceTab = sectionBranches
		a.openCommitFilter(sectionBranches)
		settleShot(t, tt, updated, func() bool {
			return !a.authorsLoading && len(a.authors) > 0 && !a.branchCommitsLoading
		})
		saveScreenshot(t, dir, "30-branch-filter-light", tt)
	})

	// The author popup of a repository with many authors must scroll
	// rather than run off the window.
	t.Run("history-filter-authors-light", func(t *testing.T) {
		a, tt, updated := newScreenshotHarness(t, false)
		a.workspaceTab = sectionHistory
		a.openCommitFilter(sectionHistory)
		settleShot(t, tt, updated, func() bool { return !a.authorsLoading })
		authors := make([]string, 0, 40)
		for i := range 40 {
			authors = append(authors, fmt.Sprintf("Contributor %02d", i))
		}
		a.authors = authors
		a.authorsLoading = false
		tt.Frame()
		if err := tt.Click("Filter by author"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "31-history-filter-authors-light", tt)
	})

	// The window's own chrome: one title bar with the tabs it reads at once,
	// the + that opens a picker, and the window controls.
	t.Run("shell-changes-light", func(t *testing.T) {
		_, tt, _, _ := newShellShot(t, false, "System")
		saveScreenshot(t, dir, "43-shell-light", tt)
	})

	t.Run("shell-changes-narrow-light", func(t *testing.T) {
		_, tt, _, _ := newShellShot(t, false, "System")
		tt.SetSize(960, 640)
		tt.Frame()
		saveScreenshot(t, dir, "43b-shell-narrow-light", tt)
	})

	t.Run("shell-changes-tokyo-night", func(t *testing.T) {
		_, tt, _, _ := newShellShot(t, true, defaultTheme)
		saveScreenshot(t, dir, "44-shell-tokyo-night", tt)
	})

	t.Run("shell-update-available-tokyo-night", func(t *testing.T) {
		s, tt, _, _ := newShellShot(t, true, defaultTheme)
		s.availableUpdate = "1.1.0"
		tt.Frame()
		saveScreenshot(t, dir, "63-shell-update-available-tokyo-night", tt)
	})

	t.Run("shell-kanagawa-wave", func(t *testing.T) {
		_, tt, _, _ := newShellShot(t, true, "Kanagawa Wave")
		saveScreenshot(t, dir, "44a-shell-kanagawa-wave", tt)
	})

	t.Run("shell-rose-pine-dawn", func(t *testing.T) {
		_, tt, _, _ := newShellShot(t, false, "Rosé Pine Dawn")
		saveScreenshot(t, dir, "44aa-shell-rose-pine-dawn", tt)
	})

	t.Run("shell-changes-collapsed-tokyo-night", func(t *testing.T) {
		s, tt, _, _ := newShellShot(t, true, defaultTheme)
		a := s.activeApp()
		a.navPaneCollapsed = true
		a.workspacePaneCollapsed[sectionHistory] = true
		tt.Frame()
		saveScreenshot(t, dir, "44b-shell-collapsed-tokyo-night", tt)
	})

	t.Run("shell-history-collapsed-tokyo-night", func(t *testing.T) {
		s, tt, _, _ := newShellShot(t, true, defaultTheme)
		a := s.activeApp()
		a.workspaceTab = sectionHistory
		a.filePaneCollapsed = true
		tt.Frame()
		saveScreenshot(t, dir, "44c-shell-history-collapsed-tokyo-night", tt)
	})

	t.Run("shell-picker-tokyo-night", func(t *testing.T) {
		s, tt, _, home := newShellShot(t, true, defaultTheme)
		home.app.recentRepositories = []recentRepository{
			{Path: "/tmp/lazygit_repo", LastOpened: timeNow()},
			{Path: "/work/project", LastOpened: timeNow() - 3*3600},
			{Path: "/work/design-docs", LastOpened: timeNow() - 40*86400},
		}
		s.activate(s.indexOf(home))
		saveScreenshot(t, dir, "45-shell-picker-tokyo-night", tt)
	})

	// The tab's own menu, as Chrome opens it: rename, pin, close, close the
	// neighbours, and the repository's path.
	t.Run("shell-tab-menu-tokyo-night", func(t *testing.T) {
		_, tt, _, _ := newShellShot(t, true, defaultTheme)
		if err := tt.RightClick("project"); err != nil {
			t.Fatal(err)
		}
		saveScreenshot(t, dir, "46-shell-tab-menu-tokyo-night", tt)
	})

	// Naming a tab: the same modal the picker's cards use, saying the tab
	// and the card share the name.
	t.Run("shell-tab-rename-tokyo-night", func(t *testing.T) {
		_, tt, _, _ := newShellShot(t, true, defaultTheme)
		if err := tt.RightClick("project"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if err := tt.Click("Rename tab"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		saveScreenshot(t, dir, "48-shell-tab-rename-tokyo-night", tt)
	})

	// A page's dialog draws over the page, under the tabs that stay reachable
	// to say where the dialog belongs.
	t.Run("shell-settings-tokyo-night", func(t *testing.T) {
		s, tt, _, _ := newShellShot(t, true, defaultTheme)
		s.activeApp().openSettings()
		saveScreenshot(t, dir, "47-shell-settings-tokyo-night", tt)
	})
}

// newShellShot builds the tabbed window on the fixture repository, with more
// pages beside it so the strip shows the shapes it has: a pinned page, the
// picker, and the tasks that are open.
func newShellShot(t *testing.T, dark bool, theme string) (*shell, *ui.Tester, *updates, *tab) {
	t.Helper()
	s := newShell(screenshotOpener{snapshot: screenshotSnapshot()})
	s.settings = defaultSettings()
	s.settings.Theme = theme
	s.pathUserData = t.TempDir()
	updated := newUpdates()
	s.update = updated.update
	s.invalidate = func() {}
	tt := ui.NewTester(s.view, 1280, 800)
	tt.SetDark(dark)
	s.start("/work/project")
	s.activeApp().workspaceTab = sectionChanges
	settleShot(t, tt, updated, func() bool {
		a := s.activeApp()
		return a != nil && a.state == stateReady &&
			(a.diffState != nil || a.diffError != "")
	})
	project := s.tabs[0]

	home := s.newTab("")
	pinned := s.newTab("/work/design-docs")
	pinned.pinned = true
	pinned.app.state = stateReady
	pinned.app.snapshot = repository.Snapshot{Root: "/work/design-docs", Branch: "main"}
	api := s.newTab("/work/api-server")
	api.app.state = stateReady
	api.app.path = "/work/api-server"
	api.app.snapshot = repository.Snapshot{
		Root: "/work/api-server", Branch: "release/2.4",
		Files: []repository.File{{Path: "main.go", Status: " M", Unstaged: true}},
	}
	// Pinned pages come first, as togglePin leaves them.
	s.tabs = []*tab{pinned, home, project, api}
	s.activate(s.indexOf(project))
	tt.Frame()
	return s, tt, updated, home
}

// newBackgroundPickerShot builds Home with a decoded, persisted image already
// installed, so the fixture measures rendering rather than asynchronous I/O.
func newBackgroundPickerShot(t *testing.T) (*app, *ui.Tester) {
	t.Helper()
	a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
	a.settings.Theme = "System"
	a.recentRepositories = []recentRepository{
		{Path: "/tmp/lazygit_repo", LastOpened: timeNow()},
		{Path: "/work/project", LastOpened: timeNow() - 3*3600},
		{Path: "/work/design-docs", LastOpened: timeNow() - 40*86400},
	}
	path := filepath.Join(t.TempDir(), "home-background.png")
	writeHomeBackgroundPNG(t, path)
	bitmap, err := readHomeBackground(path)
	if err != nil {
		t.Fatal(err)
	}
	a.settings.HomeBackgroundPath = path
	a.homeBackgroundPath = path
	a.homeBackground = bitmap
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetDark(true)
	tt.Frame()
	return a, tt
}

// newPickerShot renders the homepage picker on a short history, with the
// optional message of a failed open above it.
func newPickerShot(t *testing.T, dark bool, errMessage string) *ui.Tester {
	t.Helper()
	a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
	a.settings.Theme = "System"
	a.recentRepositories = []recentRepository{
		{Path: "/tmp/lazygit_repo", LastOpened: timeNow()},
		{Path: "/work/project", LastOpened: timeNow() - 3*3600},
		{Path: "/work/design-docs", LastOpened: timeNow() - 40*86400},
	}
	if errMessage != "" {
		a.state = stateError
		a.errMessage = errMessage
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetDark(dark)
	tt.Frame()
	return tt
}

func (s screenshotSession) ForcePushWithLease(context.Context) error                     { return nil }
func (s screenshotSession) CherryPickCommits(context.Context, []repository.Commit) error { return nil }
func (s screenshotSession) EditCommitRange(context.Context, []repository.Commit, repository.CommitRangeAction) error {
	return nil
}
