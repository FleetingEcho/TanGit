package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// runGitIn runs git in dir, for the round-trip test against a real repository.
func runGitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

// twoHunkPatch changes one line in each of two hunks, so a test can select a
// line of one and check the other is left alone.
const twoHunkPatch = `diff --git a/app.go b/app.go
index 1111111..2222222 100644
--- a/app.go
+++ b/app.go
@@ -1,4 +1,4 @@
 one
-two
+TWO
 three
-four
+FOUR
@@ -10,3 +10,4 @@
 ten
+ten and a half
 eleven
`

// Selecting an added line of a hunk builds a patch of that hunk alone: the
// other hunk's change stays out of it.
func TestPartialPatchKeepsOnlyTheSelectedHunk(t *testing.T) {
	// New-side lines: 0 one, 1 TWO, 2 three, 3 FOUR, 4 ten,
	// 5 ten and a half, 6 eleven.
	patchText, _, err := partialPatch(twoHunkPatch, repository.File{Path: "app.go"},
		editor.DiffLineRange{Side: editor.DiffNew, Start: 5, End: 5}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--- a/app.go", "+++ b/app.go", "@@ -10,2 +10,3 @@", "+ten and a half", " ten", " eleven"} {
		if !strings.Contains(patchText, want) {
			t.Errorf("the partial patch is missing %q:\n%s", want, patchText)
		}
	}
	for _, unwanted := range []string{"TWO", "FOUR"} {
		if strings.Contains(patchText, unwanted) {
			t.Errorf("the partial patch kept the other hunk's %q:\n%s", unwanted, patchText)
		}
	}
}

// Selecting an added line of a changed line brings the deletion it replaces
// with it, so the index gets the new line in place of the old rather than
// beside it.
func TestPartialPatchReplacesTheChangedLine(t *testing.T) {
	patchText, _, err := partialPatch(twoHunkPatch, repository.File{Path: "app.go"},
		editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@@ -1,4 +1,4 @@", "-two", "+TWO", " one", " three", " four"} {
		if !strings.Contains(patchText, want) {
			t.Errorf("the partial patch is missing %q:\n%s", want, patchText)
		}
	}
	if strings.Contains(patchText, "FOUR") {
		t.Errorf("the partial patch took the other change's addition:\n%s", patchText)
	}
}

// Selecting a deleted line alone stages the deletion: the line goes away and
// the change that replaces it does not come along.
func TestPartialPatchStagesASelectedDeletion(t *testing.T) {
	patchText, _, err := partialPatch(twoHunkPatch, repository.File{Path: "app.go"},
		editor.DiffLineRange{Side: editor.DiffOld, Start: 1, End: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@@ -1,4 +1,3 @@", "-two"} {
		if !strings.Contains(patchText, want) {
			t.Errorf("the partial patch is missing %q:\n%s", want, patchText)
		}
	}
	if strings.Contains(patchText, "+TWO") {
		t.Errorf("the partial patch staged the addition too:\n%s", patchText)
	}
}

// A selection of context lines holds no change, so there is nothing to stage.
func TestPartialPatchIgnoresContextLines(t *testing.T) {
	patchText, _, err := partialPatch(twoHunkPatch, repository.File{Path: "app.go"},
		editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 0}, false)
	if err != nil {
		t.Fatal(err)
	}
	if patchText != "" {
		t.Fatalf("a selection of context built a patch:\n%s", patchText)
	}
}

// Unstaging builds the patch backwards, so the same selection names the same
// change on the staged side.
func TestPartialPatchBuildsTheStagedSideBackwards(t *testing.T) {
	patchText, _, err := partialPatch(twoHunkPatch, repository.File{Path: "app.go"},
		editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-two", "+TWO"} {
		if !strings.Contains(patchText, want) {
			t.Errorf("the reverse patch is missing %q:\n%s", want, patchText)
		}
	}
	if strings.Contains(patchText, "+FOUR") {
		t.Errorf("the reverse patch took the other change:\n%s", patchText)
	}
}

// The whole route, against a real repository: the patch the pane builds from
// a selection stages just that change, and the patch it builds from the
// staged diff takes it back out again.
func TestPartialPatchRoundTripsThroughTheIndex(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Desktop Test")
	runGitIn(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "lines.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "lines.txt")
	runGitIn(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\nFOUR\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	session, err := (repository.Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	file := repository.File{Path: "lines.txt", Tracked: true, Unstaged: true, Status: " M"}
	diff, err := session.Diff(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	patchText, _, err := partialPatch(diff.Patch, file,
		editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ApplyCachedPatch(ctx, patchText, false); err != nil {
		t.Fatal(err)
	}
	staged := runGitIn(t, dir, "diff", "--cached", "--", "lines.txt")
	if !strings.Contains(staged, "-two") || !strings.Contains(staged, "+TWO") {
		t.Fatalf("the index misses the selected change: %q", staged)
	}
	if strings.Contains(staged, "FOUR") {
		t.Fatalf("the index took the other change: %q", staged)
	}

	stagedFile := repository.File{Path: "lines.txt", Tracked: true, Staged: true, Status: "M "}
	// Both changes are in the index, so taking one back out is a partial
	// patch of the staged side rather than the file's whole diff.
	runGitIn(t, dir, "add", "lines.txt")
	stagedDiff, err := session.Diff(ctx, stagedFile)
	if err != nil {
		t.Fatal(err)
	}
	reverse, wholeFile, err := partialPatch(stagedDiff.Patch, stagedFile,
		editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	if wholeFile {
		t.Fatal("a selection of one change covers the whole file's diff")
	}
	if err := session.ApplyCachedPatch(ctx, reverse, true); err != nil {
		t.Fatal(err)
	}
	staged = runGitIn(t, dir, "diff", "--cached", "--", "lines.txt")
	if strings.Contains(staged, "TWO") {
		t.Fatalf("the reverse left the unstaged change staged: %q", staged)
	}
	if !strings.Contains(staged, "+FOUR") {
		t.Fatalf("the reverse took the other change with it: %q", staged)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "one\nTWO\nthree\nFOUR\n" {
		t.Fatalf("the working tree changed: %q %v", content, err)
	}
}

// Selecting every change of the file is selecting the file, and the caller
// stages it whole rather than building a patch of it: the diff of a file
// wholly added or removed describes the file, not a change within it.
func TestPartialPatchReportsAWholeFileSelection(t *testing.T) {
	// New-side lines 0 to 6 are every line of the two hunks.
	_, wholeFile, err := partialPatch(twoHunkPatch, repository.File{Path: "app.go"},
		editor.DiffLineRange{Side: editor.DiffNew, Start: 0, End: 6}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !wholeFile {
		t.Fatal("a selection of every change is not recognised as the whole file")
	}

	_, wholeFile, err = partialPatch(twoHunkPatch, repository.File{Path: "app.go"},
		editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if wholeFile {
		t.Fatal("a selection of one change was taken for the whole file")
	}
}

// A file git does not know yet is shown as the patch that adds it, and a
// selection of its lines builds the patch that adds only those.
func TestPartialPatchStagesLinesOfANewFile(t *testing.T) {
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("first\nsecond\nthird\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	session, err := (repository.Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	file := repository.File{Path: "new.txt", Unstaged: true, Status: "??"}
	diff, err := session.Diff(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Kind != "Untracked" || !strings.Contains(diff.Patch, "+second") {
		t.Fatalf("unexpected untracked diff %#v", diff)
	}
	if !diffSupportsLineStaging(file, diff.Kind) {
		t.Fatal("a new file does not offer line staging")
	}

	patchText, wholeFile, err := partialPatch(diff.Patch, file,
		editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if wholeFile {
		t.Fatal("one line of a new file covers its whole diff")
	}
	for _, want := range []string{"--- a/new.txt", "+++ b/new.txt", "+second"} {
		if !strings.Contains(patchText, want) {
			t.Errorf("the partial patch is missing %q:\n%s", want, patchText)
		}
	}
	if strings.Contains(patchText, "+first") || strings.Contains(patchText, "+third") {
		t.Errorf("the partial patch took the other lines:\n%s", patchText)
	}
	if err := session.ApplyCachedPatch(ctx, patchText, false); err != nil {
		t.Fatal(err)
	}
	staged := runGitIn(t, dir, "diff", "--cached", "--", "new.txt")
	if !strings.Contains(staged, "+second") || !strings.Contains(staged, "new file mode") {
		t.Fatalf("the index misses the selected line: %q", staged)
	}
	if strings.Contains(staged, "+first") || strings.Contains(staged, "+third") {
		t.Fatalf("the index took the other lines: %q", staged)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "new.txt")); err != nil ||
		string(content) != "first\nsecond\nthird\n" {
		t.Fatalf("the working tree changed: %q %v", content, err)
	}
}
