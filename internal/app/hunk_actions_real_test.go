package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

const realHunkBase = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\ntwelve\nthirteen\nfourteen\nfifteen\nsixteen\nseventeen\neighteen\nnineteen\ntwenty\n"

func newRealHunkRepository(t *testing.T, edited string) (string, repository.Session) {
	t.Helper()
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Desktop Test")
	runGitIn(t, dir, "config", "user.email", "desktop@example.com")
	writeRealHunkFile(t, dir, realHunkBase)
	runGitIn(t, dir, "add", "lines.txt")
	runGitIn(t, dir, "commit", "-m", "initial")
	writeRealHunkFile(t, dir, edited)
	session, err := (repository.Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, session
}

func writeRealHunkFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "lines.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func realCurrentHunkPatch(t *testing.T, session repository.Session, file repository.File, staged bool, run int, discard bool) string {
	t.Helper()
	options := repository.DiffOptions{ContextLines: 3}
	diff, err := session.Diff(context.Background(), file, options)
	if err != nil {
		t.Fatal(err)
	}
	state, err := buildDiffState(editor.DiffUnified, false, file, diff, options)
	if err != nil || state == nil {
		t.Fatalf("build diff = %v, %v", state, err)
	}
	state.GoToHunk(run)
	a := &app{
		diffState: state, diffPatch: diff.Patch, diffKind: diff.Kind,
		changeRows: []changeRow{{file: file, staged: staged}}, selectedFile: 0,
	}
	_, patchText, wholeFile, ok := a.currentHunkPatch(discard)
	if !ok || patchText == "" || wholeFile {
		t.Fatalf("current run %d did not produce an ordinary partial patch: text=%q whole=%v ok=%v", run, patchText, wholeFile, ok)
	}
	return patchText
}

func applyRealHunkPreview(t *testing.T, session repository.Session, patchText string, target repository.PatchTarget, reverse bool) {
	t.Helper()
	if err := previewAndApplyPatch(context.Background(), session, repository.PatchRequest{
		Text: patchText, Target: target, Reverse: reverse,
	}); err != nil {
		t.Fatalf("apply current hunk:\n%s\nerror: %v", patchText, err)
	}
}

func readRealHunkFile(t *testing.T, dir string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, "lines.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestCurrentHunkPatchRequestsAgainstRealGit(t *testing.T) {
	t.Run("stage and reverse only one of two hunks", func(t *testing.T) {
		edited := strings.NewReplacer("two\n", "TWO\n", "sixteen\n", "sixteen\nsixteen and a half\n").Replace(realHunkBase)
		dir, session := newRealHunkRepository(t, edited)
		file := repository.File{Path: "lines.txt", Status: " M", Unstaged: true, Tracked: true}
		first := realCurrentHunkPatch(t, session, file, false, 0, false)
		applyRealHunkPreview(t, session, first, repository.PatchIndex, false)

		indexWithFirst := strings.Replace(realHunkBase, "two\n", "TWO\n", 1)
		if got := runGitIn(t, dir, "show", ":lines.txt"); got != indexWithFirst {
			t.Fatalf("index after staging first hunk:\n%s\nwant:\n%s", got, indexWithFirst)
		}
		if got := readRealHunkFile(t, dir); got != edited {
			t.Fatal("staging the first hunk changed the worktree")
		}

		// Put both changes in the index, then unstage only the second. This
		// exercises the actual staged-side reverse request, not a fake flag.
		runGitIn(t, dir, "add", "lines.txt")
		stagedFile := repository.File{Path: "lines.txt", Status: "M ", Staged: true, Tracked: true}
		second := realCurrentHunkPatch(t, session, stagedFile, true, 1, false)
		applyRealHunkPreview(t, session, second, repository.PatchIndex, true)
		if got := runGitIn(t, dir, "show", ":lines.txt"); got != indexWithFirst {
			t.Fatalf("index after reverse unstaging second hunk:\n%s\nwant:\n%s", got, indexWithFirst)
		}
		if got := readRealHunkFile(t, dir); got != edited {
			t.Fatal("reverse unstage changed the worktree")
		}
	})

	t.Run("mixed file discard preserves index and captures recovery", func(t *testing.T) {
		edited := strings.NewReplacer(
			"two\n", "TWO\n",
			"sixteen\n", "sixteen\nsixteen and a half\n",
			"twenty\n", "TWENTY\n",
		).Replace(realHunkBase)
		dir, session := newRealHunkRepository(t, edited)
		file := repository.File{Path: "lines.txt", Status: " M", Unstaged: true, Tracked: true}
		first := realCurrentHunkPatch(t, session, file, false, 0, false)
		applyRealHunkPreview(t, session, first, repository.PatchIndex, false)
		indexBefore := runGitIn(t, dir, "show", ":lines.txt")

		// The unstaged side now has two hunks while TWO remains staged.
		// Discard only the insertion; TWENTY and the index must survive.
		mixedFile := repository.File{Path: "lines.txt", Status: "MM", Staged: true, Unstaged: true, Tracked: true}
		insertion := realCurrentHunkPatch(t, session, mixedFile, false, 0, true)
		applyRealHunkPreview(t, session, insertion, repository.PatchWorktree, true)
		wantWorktree := strings.NewReplacer("two\n", "TWO\n", "twenty\n", "TWENTY\n").Replace(realHunkBase)
		if got := readRealHunkFile(t, dir); got != wantWorktree {
			t.Fatalf("worktree after mixed discard:\n%s\nwant:\n%s", got, wantWorktree)
		}
		if got := runGitIn(t, dir, "show", ":lines.txt"); got != indexBefore {
			t.Fatal("working-tree hunk discard changed the mixed file's index")
		}
		if staged := runGitIn(t, dir, "diff", "--cached", "--", "lines.txt"); !strings.Contains(staged, "+TWO") || strings.Contains(staged, "TWENTY") {
			t.Fatalf("wrong staged content survived mixed discard:\n%s", staged)
		}
		recovery := runGitIn(t, dir, "for-each-ref", "--format=%(refname)", "refs/tangit/")
		if !strings.Contains(recovery, "refs/tangit/") {
			t.Fatal("discard did not retain a recovery ref")
		}
	})
}
