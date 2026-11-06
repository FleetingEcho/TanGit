package app

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

type openResult struct {
	snapshot repository.Snapshot
	err      error
}

type fakeOpener struct {
	result         openResult
	release        <-chan struct{}
	calls          *callRecorder
	patch          string
	conflictStages repository.ConflictStages
	mediaPair      repository.MediaPair
	// initErr makes the picker's offer to initialize a folder fail, as a
	// folder the user may not write to does.
	initErr error
}

func (f fakeOpener) Init(_ context.Context, path string) error {
	f.calls.record("init:" + path)
	return f.initErr
}

func (f fakeOpener) Open(context.Context, string) (repository.Session, error) {
	if f.release != nil {
		<-f.release
	}
	if f.result.err != nil {
		return nil, f.result.err
	}
	return fakeSession{snapshot: f.result.snapshot, calls: f.calls, patch: f.patch, conflictStages: f.conflictStages, mediaPair: f.mediaPair}, nil
}

// openFixture submits the picker's path field with Enter, which is how a
// typed path opens now that the picker has no separate Open button.
func openFixture(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click("Repository folder"); err != nil {
		t.Fatalf("focus the repository field: %v", err)
	}
	tt.Key(0, ui.KeyEnter)
}

type fakeSession struct {
	snapshot       repository.Snapshot
	patch          string
	conflictStages repository.ConflictStages
	mediaPair      repository.MediaPair
	mediaErr       error
	reviewed       map[string]bool
	reviewErr      error
	// calls records what the fake was asked to do, for tests that click an
	// action and assert it reached the session. Nil records nothing.
	calls *callRecorder
	// commandLog is what the command log modal reads, nil when a test does
	// not open it.
	commandLog *repository.CommandLog
}

// callRecorder is the calls a fakeSession recorded, safe to read from the
// test's goroutine while the app's actions write to it.
type callRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *callRecorder) record(call string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}

func (r *callRecorder) all() []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func (r *callRecorder) has(call string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, got := range r.calls {
		if got == call {
			return true
		}
	}
	return false
}

func (f fakeSession) Snapshot(context.Context) (repository.Snapshot, error) {
	return f.snapshot, nil
}

func (f fakeSession) ReviewedFiles(context.Context, []repository.File) (map[string]bool, error) {
	if f.reviewErr != nil {
		return nil, f.reviewErr
	}
	reviewed := make(map[string]bool, len(f.reviewed))
	for path, marked := range f.reviewed {
		reviewed[path] = marked
	}
	return reviewed, nil
}

func (f fakeSession) SetFileReviewed(_ context.Context, file repository.File, reviewed bool) error {
	f.calls.record("review:" + file.Path + ":" + strconv.FormatBool(reviewed))
	return f.reviewErr
}

func (f fakeSession) ConflictStages(context.Context, repository.File) (repository.ConflictStages, error) {
	return f.conflictStages, nil
}

func (f fakeSession) Media(context.Context, repository.MediaRequest) (repository.MediaPair, error) {
	return f.mediaPair, f.mediaErr
}

func (f fakeSession) Diff(_ context.Context, file repository.File, options repository.DiffOptions) (repository.Diff, error) {
	side := "unstaged"
	if file.Staged && !file.Unstaged {
		side = "staged"
	}
	f.calls.record("diff:" + file.Path + ":" + side)
	if !file.Tracked {
		return repository.Diff{Patch: fakeNewFilePatch, Kind: "Untracked"}, nil
	}
	if f.patch != "" {
		kind := "Unstaged"
		if side == "staged" {
			kind = "Staged"
		}
		return repository.Diff{Patch: f.patch, Kind: kind}, nil
	}
	kind := "Unstaged"
	if side == "staged" {
		kind = "Staged"
	}
	return repository.Diff{Patch: fakeWorkingTreePatch, Kind: kind}, nil
}

// fakeWorkingTreePatch is the diff the fake session shows for a tracked file:
// two changes in one hunk, so a test can select one and leave the other.
const fakeWorkingTreePatch = `diff --git a/changed.txt b/changed.txt
index 1111111..2222222 100644
--- a/changed.txt
+++ b/changed.txt
@@ -1,4 +1,4 @@
 one
-two
+TWO
 three
-four
+FOUR
`

// fakeNewFilePatch is the diff git shows for a file it does not know: the
// patch that adds it, so a test can select one of its lines.
const fakeNewFilePatch = `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+first
+second
`

func (f fakeSession) Stage(_ context.Context, file repository.File) error {
	f.calls.record("stage:" + file.Path)
	return nil
}

func (f fakeSession) Unstage(_ context.Context, file repository.File) error {
	f.calls.record("unstage:" + file.Path)
	return nil
}

func (f fakeSession) ApplyCachedPatch(_ context.Context, patchText string, reverse bool) error {
	f.calls.record("apply-patch:" + strconv.FormatBool(reverse) + ":" + patchText)
	return nil
}

func (f fakeSession) ApplyWorkingTreePatch(_ context.Context, patchText string, reverse bool) error {
	f.calls.record("apply-worktree:" + strconv.FormatBool(reverse) + ":" + patchText)
	return nil
}

func (f fakeSession) PreviewPatch(_ context.Context, path string) (repository.PatchPreview, error) {
	f.calls.record("preview-patch:" + path)
	return repository.PatchPreview{Path: path, Clean: true}, nil
}

func (f fakeSession) ApplyPatch(_ context.Context, preview repository.PatchPreview) error {
	f.calls.record("apply-patch-file:" + preview.Path)
	return nil
}

func (f fakeSession) StageAll(context.Context) error {
	f.calls.record("stage-all")
	return nil
}

func (f fakeSession) UnstageAll(context.Context) error {
	f.calls.record("unstage-all")
	return nil
}

func (f fakeSession) Commit(_ context.Context, summary, _ string, amend bool) error {
	f.calls.record("commit:" + summary)
	if amend {
		f.calls.record("amend:" + summary)
	}
	return nil
}

func (f fakeSession) Undo(context.Context) error {
	f.calls.record("undo")
	return nil
}

func (f fakeSession) Redo(context.Context) error {
	f.calls.record("redo")
	return nil
}

func (f fakeSession) CommitMessage(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
	return repository.CommitMessage{Summary: commit.Subject, Description: "Existing body"}, nil
}

func (f fakeSession) RewordCommit(_ context.Context, commit repository.Commit, summary, description string) error {
	f.calls.record("reword:" + commit.Hash + ":" + summary + ":" + description)
	return nil
}

func (f fakeSession) DropCommit(_ context.Context, commit repository.Commit) error {
	f.calls.record("drop:" + commit.Hash)
	return nil
}

func (f fakeSession) MoveCommit(_ context.Context, commit repository.Commit, direction repository.CommitMove) error {
	f.calls.record("move:" + strconv.Itoa(int(direction)) + ":" + commit.Hash)
	return nil
}

func (f fakeSession) CombineCommit(_ context.Context, commit repository.Commit, mode repository.CombineMode) error {
	f.calls.record("combine:" + strconv.Itoa(int(mode)) + ":" + commit.Hash)
	return nil
}

func (f fakeSession) AmendCommit(_ context.Context, commit repository.Commit) error {
	f.calls.record("amend-to:" + commit.Hash)
	return nil
}

func (f fakeSession) CreateFixupCommit(_ context.Context, commit repository.Commit) error {
	f.calls.record("fixup-for:" + commit.Hash)
	return nil
}

func (f fakeSession) ApplyFixupCommits(_ context.Context, commit repository.Commit) error {
	f.calls.record("apply-fixups:" + commit.Hash)
	return nil
}

func (f fakeSession) DiscardFiles(ctx context.Context, files []repository.File) error {
	for _, file := range files {
		if err := f.Discard(ctx, file); err != nil {
			return err
		}
	}
	return nil
}

func (f fakeSession) WorkSnapshots(context.Context) ([]repository.WorkSnapshot, error) {
	return f.snapshot.WorkSnapshots, nil
}
func (f fakeSession) CreateCheckpoint(_ context.Context, name string) (repository.WorkSnapshot, error) {
	f.calls.record("checkpoint:" + name)
	return repository.WorkSnapshot{ID: "checkpoint", Kind: repository.CheckpointSnapshotKind, Name: name}, nil
}
func (f fakeSession) RestoreWorkSnapshot(_ context.Context, snapshot repository.WorkSnapshot) error {
	f.calls.record("restore-snapshot:" + snapshot.ID)
	return nil
}
func (f fakeSession) DeleteWorkSnapshot(_ context.Context, snapshot repository.WorkSnapshot) error {
	f.calls.record("delete-snapshot:" + snapshot.ID)
	return nil
}
func (f fakeSession) Operations(context.Context) ([]repository.OperationRecord, error) {
	return f.snapshot.Operations, nil
}
func (f fakeSession) UndoOperation(_ context.Context, id string) error {
	f.calls.record("undo-operation:" + id)
	return nil
}

func (f fakeSession) Discard(_ context.Context, file repository.File) error {
	f.calls.record("discard:" + file.Path)
	return nil
}

func (f fakeSession) IgnoreFile(_ context.Context, file repository.File) error {
	f.calls.record("ignore:" + file.Path)
	return nil
}

func (f fakeSession) ExcludeFile(_ context.Context, file repository.File) error {
	f.calls.record("exclude:" + file.Path)
	return nil
}

func (f fakeSession) CheckoutBranch(_ context.Context, name string) error {
	f.calls.record("checkout:" + name)
	return nil
}

func (f fakeSession) CheckoutCommit(_ context.Context, commit repository.Commit) error {
	f.calls.record("checkout-commit:" + commit.Hash)
	return nil
}

func (f fakeSession) CreateBranch(_ context.Context, name, base string) error {
	f.calls.record("branch:" + name + ":" + base)
	return nil
}

func (f fakeSession) DeleteBranch(context.Context, string, bool) error {
	return nil
}

func (f fakeSession) RenameBranch(context.Context, string, string) error {
	return nil
}

func (f fakeSession) MergeBranch(_ context.Context, name string) error {
	f.calls.record("merge:" + name)
	return nil
}

func (f fakeSession) Fetch(context.Context) error {
	return nil
}

func (f fakeSession) FetchRemote(_ context.Context, name string) error {
	f.calls.record("fetch-remote:" + name)
	return nil
}

func (f fakeSession) Pull(context.Context) error {
	return nil
}

func (f fakeSession) Push(context.Context) error {
	return nil
}

func (f fakeSession) PullBranch(_ context.Context, name string) error {
	f.calls.record("pull-branch:" + name)
	return nil
}

func (f fakeSession) FastForwardBranch(_ context.Context, name string) error {
	f.calls.record("fast-forward:" + name)
	return nil
}

func (f fakeSession) RebaseOntoBranch(_ context.Context, name string) error {
	f.calls.record("rebase-onto:" + name)
	return nil
}

func (f fakeSession) ForceCheckoutBranch(_ context.Context, name string) error {
	f.calls.record("force-checkout:" + name)
	return nil
}

func (f fakeSession) SetUpstream(_ context.Context, remote, remoteBranch, localBranch string) error {
	f.calls.record("set-upstream:" + remote + ":" + remoteBranch + ":" + localBranch)
	return nil
}

func (f fakeSession) UnsetUpstream(_ context.Context, localBranch string) error {
	f.calls.record("unset-upstream:" + localBranch)
	return nil
}

func (f fakeSession) CommandLog() *repository.CommandLog {
	return f.commandLog
}

func (f fakeSession) AddRemote(context.Context, string, string) error {
	return nil
}

func (f fakeSession) UpdateRemote(_ context.Context, oldName, newName, url string) error {
	f.calls.record("update-remote:" + oldName + ":" + newName + ":" + url)
	return nil
}

func (f fakeSession) RemoveRemote(context.Context, string) error {
	return nil
}

func (f fakeSession) CheckoutRemoteBranch(context.Context, string, string, string) error {
	return nil
}

func (f fakeSession) DeleteRemoteBranch(_ context.Context, remote, branch string) error {
	f.calls.record("delete-remote-branch:" + remote + ":" + branch)
	return nil
}

func (f fakeSession) StashSelectedFiles(_ context.Context, message string, files []repository.File, includeUntracked bool) error {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	f.calls.record("stash-files:" + message + ":" + strings.Join(paths, ",") + ":" + strconv.FormatBool(includeUntracked))
	return nil
}

func (f fakeSession) StashPatch(_ context.Context, message, patchText, _ string) error {
	f.calls.record("stash-patch:" + message + ":" + patchText)
	return nil
}

func (f fakeSession) StashChanges(_ context.Context, message string, includeUntracked bool) error {
	f.calls.record("stash:" + message)
	if includeUntracked {
		f.calls.record("stash-untracked")
	}
	return nil
}

func (f fakeSession) StashStagedChanges(_ context.Context, message string) error {
	f.calls.record("stash-staged:" + message)
	return nil
}

func (f fakeSession) ApplyStash(_ context.Context, index int, pop bool) error {
	f.calls.record("stash-apply:" + strconv.Itoa(index) + ":" + strconv.FormatBool(pop))
	return nil
}

func (f fakeSession) RenameStash(context.Context, int, string) error {
	return nil
}

func (f fakeSession) DropStash(_ context.Context, index int) error {
	f.calls.record("stash-drop:" + strconv.Itoa(index))
	return nil
}

func (f fakeSession) ApplyStashFile(_ context.Context, index int, file repository.CommitFile) error {
	f.calls.record("stash-apply-file:" + strconv.Itoa(index) + ":" + file.Path)
	return nil
}

func (f fakeSession) DiscardStashFile(_ context.Context, index int, file repository.CommitFile, name string) error {
	f.calls.record("stash-discard-file:" + strconv.Itoa(index) + ":" + file.Path + ":" + name)
	return nil
}

func (f fakeSession) StashFiles(_ context.Context, index int) ([]repository.CommitFile, error) {
	f.calls.record("stash-files:" + strconv.Itoa(index))
	return []repository.CommitFile{
		{Path: "one.txt", Status: "M"},
		{Path: "two.txt", Status: "A"},
	}, nil
}

func (f fakeSession) StashFileDiff(_ context.Context, index int, file repository.CommitFile, options repository.DiffOptions) (repository.Diff, error) {
	f.calls.record("stash-file-diff:" + strconv.Itoa(index) + ":" + file.Path)
	return repository.Diff{Patch: "diff --git a/" + file.Path + " b/" + file.Path +
		"\n--- a/" + file.Path + "\n+++ b/" + file.Path + "\n@@ -1 +1 @@\n-old\n+new\n",
		Kind: "Stash"}, nil
}

func (f fakeSession) CreateTag(_ context.Context, name, ref, _ string, _ bool) error {
	f.calls.record("tag:" + name + ":" + ref)
	return nil
}

func (f fakeSession) DeleteTag(context.Context, string) error {
	return nil
}

func (f fakeSession) PushTag(context.Context, string, string) error {
	return nil
}

func (f fakeSession) CherryPickCommit(_ context.Context, commit repository.Commit) error {
	f.calls.record("cherry-pick:" + commit.Hash)
	return nil
}

func (f fakeSession) RevertCommit(_ context.Context, commit repository.Commit) error {
	f.calls.record("revert:" + commit.Hash)
	return nil
}

func (f fakeSession) ResetToCommit(ctx context.Context, commit repository.Commit, mode repository.ResetMode) error {
	f.calls.record("reset-" + string(mode) + ":" + commit.Hash)
	return nil
}

func (f fakeSession) ResolveConflict(_ context.Context, file repository.File, current bool) error {
	side := "incoming"
	if current {
		side = "current"
	}
	f.calls.record("resolve:" + file.Path + ":" + side)
	return nil
}

func (f fakeSession) ContinueOperation(context.Context) error {
	f.calls.record("continue")
	return nil
}

func (f fakeSession) AbortOperation(context.Context) error {
	return nil
}

func (f fakeSession) CreateWorktree(context.Context, string, string, string, bool) error {
	return nil
}

func (f fakeSession) DeleteWorktree(context.Context, string, bool) error {
	return nil
}

func (f fakeSession) AddSubmodule(context.Context, string, string, string) error {
	return nil
}

func (f fakeSession) UpdateSubmodule(context.Context, string) error {
	return nil
}

func (f fakeSession) DeleteSubmodule(context.Context, string) error {
	return nil
}

func (f fakeSession) BlameFile(context.Context, repository.File) ([]repository.BlameLine, error) {
	return []repository.BlameLine{{Hash: "a1b2c3d4e5f60718", Author: "Ada", Date: "2026-01-01", Text: "changed"}}, nil
}

func (f fakeSession) BlameWorkingTreeFile(_ context.Context, path string) ([]repository.BlameLine, error) {
	f.calls.record("blame-working-tree:" + path)
	return []repository.BlameLine{{Hash: "a1b2c3d4e5f60718", Author: "Ada", Date: "2026-01-01", Text: "changed"}}, nil
}

func (f fakeSession) CommitFiles(_ context.Context, commit repository.Commit) ([]repository.CommitFile, error) {
	f.calls.record("commit-files:" + commit.Hash)
	// Two files, so a test can choose the second and see the diff move.
	return []repository.CommitFile{
		{Path: "one.txt", Status: "M", LinesAdded: 8, LinesDeleted: 2},
		{Path: "two.txt", Status: "M", LinesAdded: 1, LinesDeleted: 9},
	}, nil
}

func (f fakeSession) CommitFileDiff(_ context.Context, commit repository.Commit, file repository.CommitFile, options repository.DiffOptions) (repository.Diff, error) {
	f.calls.record("commit-file-diff:" + commit.Hash + ":" + file.Path)
	return repository.Diff{NewText: "changed\n", Kind: "Commit"}, nil
}

func (f fakeSession) CommitFilePatch(_ context.Context, commit repository.Commit, file repository.CommitFile) (string, error) {
	f.calls.record("commit-file-patch:" + commit.Hash + ":" + file.Path)
	return fakeWorkingTreePatch, nil
}

func (f fakeSession) WorkingTreeFilePatch(_ context.Context, file repository.File) (string, error) {
	f.calls.record("working-tree-file-patch:" + file.Path)
	return fakeWorkingTreePatch, nil
}

func (f fakeSession) PreviewPatchText(context.Context, string) (repository.PatchPreview, error) {
	return repository.PatchPreview{}, errors.New("in-memory patch preview is not configured")
}

func (f fakeSession) RestoreCommitFile(_ context.Context, commit repository.Commit, file repository.CommitFile) error {
	f.calls.record("restore:" + commit.Hash + ":" + file.Path)
	return nil
}

func (f fakeSession) CompareFiles(_ context.Context, from, to string, options repository.DiffOptions) ([]repository.CommitFile, error) {
	f.calls.record("compare-files:" + from + ":" + to + ":" +
		strconv.FormatBool(options.IgnoreWhitespace) + ":" + strconv.Itoa(options.ContextLines))
	return []repository.CommitFile{
		{Path: "compare-one.txt", Status: "M", LinesAdded: 2, LinesDeleted: 1},
		{Path: "compare-two.txt", Status: "M", LinesAdded: 1, LinesDeleted: 2},
	}, nil
}

func (f fakeSession) CompareDiff(_ context.Context, from, to string, options repository.DiffOptions) (repository.Diff, error) {
	f.calls.record("compare-diff:" + from + ":" + to + ":" +
		strconv.FormatBool(options.IgnoreWhitespace) + ":" + strconv.Itoa(options.ContextLines))
	return repository.Diff{Patch: fakeWorkingTreePatch, Kind: "Compare"}, nil
}

func (f fakeSession) CompareFileDiff(_ context.Context, from, to string, file repository.CommitFile, options repository.DiffOptions) (repository.Diff, error) {
	f.calls.record("compare-file-diff:" + from + ":" + to + ":" + file.Path + ":" +
		strconv.FormatBool(options.IgnoreWhitespace) + ":" + strconv.Itoa(options.ContextLines))
	return repository.Diff{Patch: fakeWorkingTreePatch, Kind: "Compare"}, nil
}

// BranchCommits answers with the snapshot's commits: a test fixture describes
// the branch it browses, not a second history.
func (f fakeSession) BranchCommits(context.Context, string, int) ([]repository.Commit, error) {
	return f.snapshot.Commits, nil
}

// SearchCommits narrows the snapshot's commits as a repository would, so a
// test can drive the filter modal without a second fixture.
func (f fakeSession) SearchCommits(_ context.Context, filter repository.CommitFilter) ([]repository.Commit, error) {
	f.calls.record("search-commits")
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	author := strings.ToLower(strings.TrimSpace(filter.Author))
	commits := make([]repository.Commit, 0, len(f.snapshot.Commits))
	for _, commit := range f.snapshot.Commits {
		if query != "" &&
			!strings.Contains(strings.ToLower(commit.Subject), query) &&
			!strings.Contains(strings.ToLower(commit.Hash), query) &&
			!strings.Contains(strings.ToLower(commit.ShortHash), query) {
			continue
		}
		if author != "" &&
			!strings.Contains(strings.ToLower(commit.AuthorName), author) &&
			!strings.Contains(strings.ToLower(commit.AuthorEmail), author) {
			continue
		}
		commits = append(commits, commit)
		if filter.Limit > 0 && len(commits) == filter.Limit {
			break
		}
	}
	return commits, nil
}

// Authors lists the snapshot's distinct author names, sorted as Git's log
// supplies them to the filter.
func (f fakeSession) Authors(context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	authors := make([]string, 0, 4)
	for _, commit := range f.snapshot.Commits {
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

// FileCommits answers with the snapshot's commits, as BranchCommits does: a
// fixture does not describe a file's own history. It honors the page asked
// for, as a repository does.
func (f fakeSession) FileCommits(_ context.Context, path string, limit, skip int) ([]repository.Commit, error) {
	f.calls.record("file-commits:" + path)
	commits := f.snapshot.Commits
	if skip >= len(commits) {
		return nil, nil
	}
	commits = commits[skip:]
	if limit > 0 && limit < len(commits) {
		commits = commits[:limit]
	}
	return commits, nil
}

// RecentCommits pages the snapshot's commits the way a repository does, so a
// test can describe a history longer than the first page.
func (f fakeSession) RecentCommits(_ context.Context, limit, skip int) ([]repository.Commit, error) {
	f.calls.record("recent-commits")
	commits := f.snapshot.Commits
	if skip >= len(commits) {
		return nil, nil
	}
	commits = commits[skip:]
	if limit > 0 && limit < len(commits) {
		commits = commits[:limit]
	}
	return commits, nil
}

func TestViewOpensRepository(t *testing.T) {
	updated := newUpdates()
	release := make(chan struct{})
	a := newApp(fakeOpener{
		release: release,
		result: openResult{snapshot: repository.Snapshot{
			Root:   "/work/project",
			Branch: "main",
			Head:   "a1b2c3d",
			Files: []repository.File{
				{Path: "one.txt", Status: "M ", Staged: true, Tracked: true},
				{Path: "two.txt", Status: "??", Unstaged: true, Added: true},
			},
			Commits: []repository.Commit{
				{Hash: "a1b2c3d4", ShortHash: "a1b2c3d", Subject: "Native history", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
			},
			Branches: []repository.Branch{
				{Name: "main", DisplayName: "main", Head: true, Subject: "Native history"},
				{Name: "feature", DisplayName: "feature", Subject: "Desktop work"},
			},
			Remotes: []repository.Remote{
				{Name: "origin", URLs: []string{"git@example.com:team/project.git"}, Branches: []repository.RemoteBranch{{Name: "main", RemoteName: "origin"}, {Name: "feature", RemoteName: "origin"}}},
			},
			Stashes: []repository.Stash{
				{Index: 0, Name: "WIP: native stash", Hash: "stashhash", Recency: "2m"},
			},
			Tags: []repository.Tag{
				{Name: "v1.0.0", Message: "First native release"},
			},
			Worktrees: []repository.Worktree{
				{Name: "project", Path: "/work/project", Branch: "main", Current: true, Main: true},
				{Name: "feature-tree", Path: "/work/feature-tree", Branch: "feature"},
			},
			Submodules: []repository.Submodule{
				{Name: "library", Path: "vendor/library", URL: "git@example.com:team/library.git"},
			},
			Reflog: []repository.Commit{
				{Hash: "a1b2c3d4", ShortHash: "a1b2c3d", Subject: "checkout: moving from feature to main", UnixTimestamp: 1_700_000_000},
			},
		}},
	})
	// This walk covers every section, including the specialist tabs the
	// default set leaves out.
	a.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
	a.workspaceTab = sectionChanges
	a.update = updated.update
	a.path = "/work/project"
	tt := ui.NewTester(a.view, 960, 640)

	openFixture(t, tt)
	close(release)
	updated.waitForUpdate(t)
	// The Changes section chooses its first file and reads its diff as it
	// builds, so the diff arrives one frame after the repository opens.
	tt.Frame()
	updated.waitForUpdate(t)
	tt.Frame()

	for _, text := range []string{"main", "/work/project", "one.txt", "two.txt", "Staged", "File actions"} {
		if !tt.HasText(text) {
			t.Errorf("missing %q in texts %q", text, tt.Texts())
		}
	}
	// The staged file's status is its own letter at the end of the row.
	if _, ok := tt.Find("M"); !ok {
		t.Errorf("the staged row shows no status letter: %q", tt.Texts())
	}
	// The full commit form is one item of the panel's menu now: the inline
	// box commits staged changes.
	if err := tt.Click("⋯"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Commit with a description…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Create commit") || !tt.HasText("Amend the latest commit") {
		t.Fatalf("commit form not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Native history") || !tt.HasText("Ada <ada@example.com>") {
		t.Fatalf("history not shown in texts %q", tt.Texts())
	}
	if !tt.HasText("Cherry-pick this commit") || !tt.HasText("Soft reset to this commit") {
		t.Fatalf("history actions not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("Branches"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("feature") || !tt.HasText("Current") {
		t.Fatalf("branches not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("feature"); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"Checkout", "Merge into current…", "Rename…", "Delete…"} {
		if !tt.HasText(action) {
			t.Fatalf("branch action %q not shown in texts %q", action, tt.Texts())
		}
	}
	if err := tt.Click("Rename…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("New branch name") || !tt.HasText("Cancel rename") {
		t.Fatalf("branch actions not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("New branch…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Branch name") || !tt.HasText("From branch") || !tt.HasText("Create branch") {
		t.Fatalf("branch form not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Remotes"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"origin", "git@example.com:team/project.git", "Remote branches", "Checkout as local…"} {
		if !tt.HasText(text) {
			t.Fatalf("remote text %q not shown in texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("Add remote…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Remote name") || !tt.HasText("Remote URL") {
		t.Fatalf("remote form not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Stash"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Saved work", "WIP: native stash", "Apply", "Pop…", "Drop…"} {
		if !tt.HasText(text) {
			t.Fatalf("stash text %q not shown in texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("Stash changes…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Stash message") || !tt.HasText("Include untracked") {
		t.Fatalf("stash form not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Tags"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Repository tags", "v1.0.0", "First native release", "Push tag…", "Delete tag…"} {
		if !tt.HasText(text) {
			t.Fatalf("tag text %q not shown in texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("Create tag…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Tag name") || !tt.HasText("Target ref") || !tt.HasText("Annotated") {
		t.Fatalf("tag form not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Worktrees"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Linked worktrees") || !tt.HasText("feature-tree") || !tt.HasText("Main worktree") {
		t.Fatalf("worktrees not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("feature-tree"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Open worktree") || !tt.HasText("Remove…") {
		t.Fatalf("worktree actions not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("New worktree…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Worktree path") || !tt.HasText("Base ref") || !tt.HasText("Detached HEAD") {
		t.Fatalf("worktree form not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Submodules"); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Configured submodules", "library", "vendor/library", "Initialize / update", "Delete…"} {
		if !tt.HasText(text) {
			t.Fatalf("submodule text %q not shown in texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("Add submodule…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Submodule name") || !tt.HasText("Checkout path") || !tt.HasText("Repository URL") {
		t.Fatalf("submodule form not shown in texts %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Reflog"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Recent reference movements") || !tt.HasText("checkout: moving from feature to main") {
		t.Fatalf("reflog not shown in texts %q", tt.Texts())
	}
}

func TestBuildDiffStateForUntrackedFile(t *testing.T) {
	state, err := buildDiffState(
		editor.DiffUnified,
		false,
		repository.File{Path: "new.go"},
		repository.Diff{NewText: "package new\n", Kind: "Untracked"},
		repository.DiffOptions{ContextLines: 3},
	)
	if err != nil {
		t.Fatal(err)
	}
	if state.Old.String() != "" || state.New.String() != "package new\n" {
		t.Fatalf("unexpected diff texts %q and %q", state.Old.String(), state.New.String())
	}
}

func TestViewShowsOpenError(t *testing.T) {
	updated := newUpdates()
	release := make(chan struct{})
	a := newApp(fakeOpener{
		release: release,
		result:  openResult{err: errors.New("not a Git repository")},
	})
	a.update = updated.update
	a.path = "/work/not-a-repository"
	tt := ui.NewTester(a.view, 960, 640)

	openFixture(t, tt)
	close(release)
	updated.waitForUpdate(t)
	tt.Frame()

	if !tt.HasText("Could not open repository") || !tt.HasText("not a Git repository") {
		t.Fatalf("unexpected texts %q", tt.Texts())
	}
}

func TestViewRequiresRepositoryPath(t *testing.T) {
	a := newApp(fakeOpener{})
	tt := ui.NewTester(a.view, 960, 640)

	openFixture(t, tt)

	if !tt.HasText("Choose a repository folder first.") {
		t.Fatalf("unexpected texts %q", tt.Texts())
	}
}

func TestViewShowsConflictRecoveryControls(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) {
		return []byte("top\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nbottom\n"), nil
	}

	updated := newUpdates()
	release := make(chan struct{})
	a := newApp(fakeOpener{release: release, result: openResult{snapshot: repository.Snapshot{
		Root:      "/work/conflict",
		Branch:    "main",
		Operation: "merge",
		Files: []repository.File{
			{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
		},
	}}})
	a.workspaceTab = sectionChanges
	a.update = updated.update
	a.path = "/work/conflict"
	tt := ui.NewTester(a.view, 960, 640)

	openFixture(t, tt)
	close(release)
	updated.waitForUpdate(t)
	tt.Frame()
	updated.waitForUpdate(t)
	tt.Frame()

	for _, text := range []string{
		"MERGE", "Continue", "Abort…", "Conflict", "Conflict 1 of 1",
		"Use current", "Use incoming", "Use both", "Mark resolved", "Open file in editor",
	} {
		if !tt.HasText(text) {
			t.Fatalf("conflict control %q not shown in texts %q", text, tt.Texts())
		}
	}
}

// updates collects the updates an app asks for, for the test to run on its
// own goroutine: MyGo's Window.Update runs them on the UI loop, and running
// them where they come from races with the frame building the view.
type updates struct {
	mu   sync.Mutex
	fns  []func()
	done chan struct{}
}

func newUpdates() *updates {
	return &updates{done: make(chan struct{}, 8)}
}

func (u *updates) update(fn func()) {
	u.mu.Lock()
	u.fns = append(u.fns, fn)
	u.mu.Unlock()
	// Updates coalesce a redraw wakeup in the real UI loop. Keep every
	// function above, but never let repeated background reads block one
	// another merely because the test has not returned to its frame loop.
	select {
	case u.done <- struct{}{}:
	default:
	}
}

// waitForUpdate waits for one update and runs it, as the UI loop would, before
// the next frame.
func (u *updates) waitForUpdate(t *testing.T) {
	t.Helper()
	select {
	case <-u.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an update")
	}
	u.mu.Lock()
	fns := u.fns
	u.fns = nil
	u.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// The command-line path travels through the same shell startup as the real
// app. Exercise actual Git repositories, including `tangit ./` from inside
// one and a path containing whitespace, without opening a native window.
func TestLaunchPathOpensRealRepository(t *testing.T) {
	for _, test := range []struct {
		name     string
		folder   string
		argument string
		inside   bool
	}{
		{name: "current directory", folder: "current-repo", argument: "./", inside: true},
		{name: "path with spaces", folder: "repository with spaces", argument: "./repository with spaces"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			runGitIn(t, parent, "init", "--initial-branch=main", test.folder)
			root := filepath.Join(parent, test.folder)
			if test.inside {
				t.Chdir(root)
			} else {
				t.Chdir(parent)
			}

			s := newShell(repository.Opener{})
			updated := newUpdates()
			s.update = updated.update
			s.invalidate = func() {}
			t.Cleanup(s.shutdown)
			s.start(launchPath([]string{test.argument}))
			tester := ui.NewTester(s.view, 1000, 700)
			a := s.activeApp()
			for range 20 {
				tester.Frame()
				if a.state == stateReady && !a.repositoryDetailsLoading && len(updated.done) == 0 {
					break
				}
				updated.waitForUpdate(t)
			}
			tester.Frame()
			if a.state != stateReady || !s.activeTab().repository() {
				t.Fatalf("%s did not open directly: state=%v error=%q", test.argument, a.state, a.errMessage)
			}
			want, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if a.path != want {
				t.Fatalf("opened root = %q, want %q", a.path, want)
			}
		})
	}
}

// A launch from the terminal opens the path it names; flags, such as the
// -psn_… a macOS bundle passes, are not paths and are skipped.
func TestLaunchPath(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	absolute := filepath.Join(cwd, "absolute repository")
	cases := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{absolute}, absolute},
		{[]string{"./"}, cwd},
		{[]string{"./work/repo"}, filepath.Join(cwd, "work", "repo")},
		{[]string{"./repository with spaces"}, filepath.Join(cwd, "repository with spaces")},
		{[]string{"-psn_0_12345"}, ""},
		{[]string{"-psn_0_12345", absolute}, absolute},
		{[]string{"", absolute}, absolute},
	}
	for _, tt := range cases {
		if got := launchPath(tt.args); got != tt.want {
			t.Errorf("launchPath(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func (f fakeSession) ForcePushWithLease(context.Context) error {
	f.calls.record("force-push-with-lease")
	return nil
}
func (f fakeSession) CherryPickCommits(_ context.Context, commits []repository.Commit) error {
	for _, c := range commits {
		f.calls.record("batch-cherry-pick:" + c.Hash)
	}
	return nil
}
func (f fakeSession) EditCommitRange(_ context.Context, commits []repository.Commit, action repository.CommitRangeAction) error {
	f.calls.record("batch-edit:" + string(action))
	return nil
}
