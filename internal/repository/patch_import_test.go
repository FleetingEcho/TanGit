package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func patchImportRepository(t *testing.T) (string, Session) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	writePatchImportFile(t, dir, "a.txt", []byte("one\ntwo\nthree\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, session
}

func writePatchImportFile(t *testing.T, root, name string, content []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// makeImportPatch builds a real Git binary patch, then restores the repository
// to its committed state. The patch itself always lives outside the worktree.
func makeImportPatch(t *testing.T, dir string, mutate func()) string {
	t.Helper()
	mutate()
	runGit(t, dir, "add", "-A")
	patch := gitOutput(t, dir, "diff", "--cached", "--binary", "--full-index", "--find-renames", "--no-ext-diff", "--no-textconv")
	if patch == "" {
		t.Fatal("test mutation produced an empty patch")
	}
	runGit(t, dir, "reset", "--hard", "HEAD")
	if status := gitOutput(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("patch fixture did not reset cleanly: %s", status)
	}
	path := filepath.Join(t.TempDir(), "selected.patch")
	if err := os.WriteFile(path, []byte(patch), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readPatchImportFile(t *testing.T, root, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func importRecoveryRefs(t *testing.T, dir string) string {
	t.Helper()
	return gitOutput(t, dir, "for-each-ref", "--format=%(refname)%00%(objectname)", "refs/tangit/")
}

func importRepositoryFingerprint(t *testing.T, dir string) string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == filepath.Join(dir, ".git") {
			return filepath.SkipDir
		}
		if path == dir {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			entries = append(entries, fmt.Sprintf("dir %s %04o", relative, info.Mode().Perm()))
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entries = append(entries, fmt.Sprintf("link %s %s", relative, target))
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, fmt.Sprintf("file %s %04o %x", relative, info.Mode().Perm(), sha256.Sum256(content)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	index, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%s\nindex=%x\nhead=%s\nrefs=%s\ncached=%s", strings.Join(entries, "\n"),
		sha256.Sum256(index), gitOutput(t, dir, "rev-parse", "HEAD"), importRecoveryRefs(t, dir),
		gitOutput(t, dir, "diff", "--cached", "--binary", "--no-ext-diff", "--no-textconv"))
}

func TestPatchImportCleanTextApplyIsUnstagedAndNeedsNoRecovery(t *testing.T) {
	dir, session := patchImportRepository(t)
	ctx := context.Background()
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nTWO\nthree\nfour\n"))
	})
	patchBytes, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	indexBefore := gitOutput(t, dir, "diff", "--cached", "--binary")

	preview, err := session.PreviewPatch(ctx, patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Clean || preview.Diagnostic != "" {
		t.Fatalf("patch preview is not clean: %#v", preview)
	}
	if preview.Path != patchPath || preview.Text != string(patchBytes) || preview.TextTruncated {
		t.Fatalf("unexpected textual preview: path=%q truncated=%v text length=%d", preview.Path, preview.TextTruncated, len(preview.Text))
	}
	exported, err := preview.Bytes()
	if err != nil || !reflect.DeepEqual(exported, patchBytes) {
		t.Fatalf("complete file-backed bytes: equal=%v err=%v", reflect.DeepEqual(exported, patchBytes), err)
	}
	if preview.Additions != 2 || preview.Deletions != 1 || len(preview.Files) != 1 {
		t.Fatalf("unexpected patch statistics: %#v", preview)
	}
	if file := preview.Files[0]; file.Path != "a.txt" || file.Additions != 2 || file.Deletions != 1 || file.Binary {
		t.Fatalf("unexpected file preview: %#v", file)
	}
	if refs := importRecoveryRefs(t, dir); refs != "" {
		t.Fatalf("preview created recovery refs: %q", refs)
	}

	if err := session.ApplyPatch(ctx, preview); err != nil {
		t.Fatal(err)
	}
	if got := string(readPatchImportFile(t, dir, "a.txt")); got != "one\nTWO\nthree\nfour\n" {
		t.Fatalf("applied content = %q", got)
	}
	if cached := gitOutput(t, dir, "diff", "--cached", "--binary"); cached != indexBefore {
		t.Fatalf("patch application changed the index:\n%s", cached)
	}
	if status := gitOutput(t, dir, "status", "--porcelain"); status != " M a.txt\n" {
		t.Fatalf("patch changes were not left unstaged: %q", status)
	}
	if refs := importRecoveryRefs(t, dir); refs != "" {
		t.Fatalf("clean application created recovery refs: %q", refs)
	}
	if after, err := os.ReadFile(patchPath); err != nil || !reflect.DeepEqual(after, patchBytes) {
		t.Fatalf("source patch changed: equal=%v err=%v", reflect.DeepEqual(after, patchBytes), err)
	}
}

func TestPatchImportKeepsApplyWarningsOutOfNumstat(t *testing.T) {
	dir, session := patchImportRepository(t)
	runGit(t, dir, "config", "apply.whitespace", "warn")
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "whitespace.txt", []byte("trailing space \ntrailing tab\t\n"))
	})
	preview, err := session.PreviewPatch(context.Background(), patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Clean || len(preview.Files) != 1 || preview.Files[0].Additions != 2 {
		t.Fatalf("warnings corrupted the patch preview: %#v", preview)
	}
}

func TestPatchImportPreviewsAndAppliesAddDeleteRenameBinaryAndUnicode(t *testing.T) {
	dir, session := patchImportRepository(t)
	writePatchImportFile(t, dir, "delete.txt", []byte("delete me\n"))
	writePatchImportFile(t, dir, "old name.txt", []byte("same\nrename\ncontents\n"))
	writePatchImportFile(t, dir, "binary.dat", []byte{0, 1, 2, 3})
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "fixture")
	binaryAfter := []byte{0, 1, 255, 3, 4}
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "new file.txt", []byte("new\nfile\n"))
		if err := os.Remove(filepath.Join(dir, "delete.txt")); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "mv", "old name.txt", "unicodé renamed.txt")
		writePatchImportFile(t, dir, "binary.dat", binaryAfter)
	})

	preview, err := session.PreviewPatch(context.Background(), patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Clean {
		t.Fatalf("combined patch is not clean: %s", preview.Diagnostic)
	}
	files := make(map[string]PatchFile, len(preview.Files))
	for _, file := range preview.Files {
		files[file.Path] = file
	}
	if len(files) != 4 {
		t.Fatalf("affected files = %#v", preview.Files)
	}
	if file := files["new file.txt"]; file.Additions != 2 || !strings.Contains(file.Summary, "create mode") {
		t.Fatalf("new-file preview = %#v", file)
	}
	if file := files["delete.txt"]; file.Deletions != 1 || !strings.Contains(file.Summary, "delete mode") {
		t.Fatalf("deletion preview = %#v", file)
	}
	if file := files["unicodé renamed.txt"]; file.OldPath != "old name.txt" || !strings.Contains(file.Summary, "rename") {
		t.Fatalf("Unicode rename preview = %#v", file)
	}
	if file := files["binary.dat"]; !file.Binary || file.Additions != 0 || file.Deletions != 0 {
		t.Fatalf("binary preview = %#v", file)
	}

	if err := session.ApplyPatch(context.Background(), preview); err != nil {
		t.Fatal(err)
	}
	if got := string(readPatchImportFile(t, dir, "new file.txt")); got != "new\nfile\n" {
		t.Fatalf("new file = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "delete.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted path still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old name.txt")); !os.IsNotExist(err) {
		t.Fatalf("old rename path still exists: %v", err)
	}
	if got := string(readPatchImportFile(t, dir, "unicodé renamed.txt")); got != "same\nrename\ncontents\n" {
		t.Fatalf("renamed content = %q", got)
	}
	if got := readPatchImportFile(t, dir, "binary.dat"); !reflect.DeepEqual(got, binaryAfter) {
		t.Fatalf("binary content = %v, want %v", got, binaryAfter)
	}
	if cached := gitOutput(t, dir, "diff", "--cached", "--binary"); cached != "" {
		t.Fatalf("combined application staged changes:\n%s", cached)
	}
}

func TestPatchImportPreservesPreexistingStagedAndUnstagedWork(t *testing.T) {
	dir, session := patchImportRepository(t)
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "imported.txt", []byte("imported\n"))
	})
	// Create the dirty state that must survive import and recovery capture.
	writePatchImportFile(t, dir, "a.txt", []byte("staged\n"))
	runGit(t, dir, "add", "a.txt")
	writePatchImportFile(t, dir, "a.txt", []byte("staged\nunstaged\n"))
	writePatchImportFile(t, dir, "untracked.txt", []byte("keep me\n"))
	cachedBefore := gitOutput(t, dir, "diff", "--cached", "--binary")
	preview, err := session.PreviewPatch(context.Background(), patchPath)
	if err != nil || !preview.Clean {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	if err := session.ApplyPatch(context.Background(), preview); err != nil {
		t.Fatal(err)
	}
	if got := string(readPatchImportFile(t, dir, "a.txt")); got != "staged\nunstaged\n" {
		t.Fatalf("preexisting worktree content = %q", got)
	}
	if got := string(readPatchImportFile(t, dir, "untracked.txt")); got != "keep me\n" {
		t.Fatalf("untracked content = %q", got)
	}
	if cached := gitOutput(t, dir, "diff", "--cached", "--binary"); cached != cachedBefore {
		t.Fatalf("preexisting index changed:\nbefore:\n%s\nafter:\n%s", cachedBefore, cached)
	}
	if got := gitOutput(t, dir, "show", ":a.txt"); got != "staged\n" {
		t.Fatalf("preexisting staged content = %q", got)
	}
	if got := string(readPatchImportFile(t, dir, "imported.txt")); got != "imported\n" {
		t.Fatalf("imported content = %q", got)
	}
	if cached := gitOutput(t, dir, "diff", "--cached", "--", "imported.txt"); cached != "" {
		t.Fatalf("imported file was staged: %s", cached)
	}
	snapshots, err := session.WorkSnapshots(context.Background())
	if err != nil || len(snapshots) != 1 || snapshots[0].Kind != RecoverySnapshotKind {
		t.Fatalf("dirty application recovery = %#v, %v", snapshots, err)
	}
}

func TestPatchImportConflictPreviewAndApplyLeaveEverythingUntouched(t *testing.T) {
	dir, session := patchImportRepository(t)
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nPATCH\nthree\n"))
	})
	writePatchImportFile(t, dir, "a.txt", []byte("one\nLOCAL\nthree\n"))
	before := importRepositoryFingerprint(t, dir)

	preview, err := session.PreviewPatch(context.Background(), patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Clean || preview.Diagnostic == "" {
		t.Fatalf("conflict preview = %#v", preview)
	}
	if after := importRepositoryFingerprint(t, dir); after != before {
		t.Fatalf("read-only conflict preview changed repository\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if err := session.ApplyPatch(context.Background(), preview); err == nil {
		t.Fatal("conflicting preview unexpectedly applied")
	}
	if after := importRepositoryFingerprint(t, dir); after != before {
		t.Fatalf("rejected conflict changed repository\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestPatchImportRejectsChangedPatchAndRepositoryChange(t *testing.T) {
	t.Run("changed patch", func(t *testing.T) {
		dir, session := patchImportRepository(t)
		patchPath := makeImportPatch(t, dir, func() {
			writePatchImportFile(t, dir, "a.txt", []byte("one\nPATCH\nthree\n"))
		})
		preview, err := session.PreviewPatch(context.Background(), patchPath)
		if err != nil || !preview.Clean {
			t.Fatalf("preview = %#v, %v", preview, err)
		}
		replacement := strings.Replace(string(readPatchImportFile(t, filepath.Dir(patchPath), filepath.Base(patchPath))), "+PATCH", "+REPLACED", 1)
		if err := os.WriteFile(patchPath, []byte(replacement), 0o600); err != nil {
			t.Fatal(err)
		}
		before := importRepositoryFingerprint(t, dir)
		err = session.ApplyPatch(context.Background(), preview)
		if err == nil || !strings.Contains(err.Error(), "patch file changed since preview") {
			t.Fatalf("changed-patch error = %v", err)
		}
		if after := importRepositoryFingerprint(t, dir); after != before {
			t.Fatal("changed patch altered repository or recovery refs")
		}
	})

	t.Run("repository content changed", func(t *testing.T) {
		dir, session := patchImportRepository(t)
		patchPath := makeImportPatch(t, dir, func() {
			writePatchImportFile(t, dir, "a.txt", []byte("one\nPATCH\nthree\n"))
		})
		preview, err := session.PreviewPatch(context.Background(), patchPath)
		if err != nil || !preview.Clean {
			t.Fatalf("preview = %#v, %v", preview, err)
		}
		writePatchImportFile(t, dir, "a.txt", []byte("one\nEXTERNAL\nthree\n"))
		before := importRepositoryFingerprint(t, dir)
		err = session.ApplyPatch(context.Background(), preview)
		if err == nil || !strings.Contains(err.Error(), "repository changed since patch preview") {
			t.Fatalf("repository-change error = %v", err)
		}
		if after := importRepositoryFingerprint(t, dir); after != before {
			t.Fatal("external repository state was altered by rejected apply")
		}
	})

	t.Run("unrelated repository status changed", func(t *testing.T) {
		dir, session := patchImportRepository(t)
		patchPath := makeImportPatch(t, dir, func() {
			writePatchImportFile(t, dir, "a.txt", []byte("one\nPATCH\nthree\n"))
		})
		preview, err := session.PreviewPatch(context.Background(), patchPath)
		if err != nil || !preview.Clean {
			t.Fatalf("preview = %#v, %v", preview, err)
		}
		writePatchImportFile(t, dir, "unrelated.txt", []byte("external\n"))
		err = session.ApplyPatch(context.Background(), preview)
		if err == nil || !strings.Contains(err.Error(), "repository changed since patch preview") {
			t.Fatalf("status-change error = %v", err)
		}
		if refs := importRecoveryRefs(t, dir); refs != "" {
			t.Fatalf("rejected status change created recovery: %q", refs)
		}
	})
}

func TestPatchImportInvalidPatchAndTruncatedTextPreview(t *testing.T) {
	t.Run("invalid", func(t *testing.T) {
		dir, session := patchImportRepository(t)
		before := importRepositoryFingerprint(t, dir)
		path := filepath.Join(t.TempDir(), "invalid.diff")
		if err := os.WriteFile(path, []byte("this is not a patch\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		preview, err := session.PreviewPatch(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if preview.Clean || preview.Diagnostic == "" || len(preview.Files) != 0 {
			t.Fatalf("invalid preview = %#v", preview)
		}
		if err := session.ApplyPatch(context.Background(), preview); err == nil {
			t.Fatal("invalid patch unexpectedly applied")
		}
		if after := importRepositoryFingerprint(t, dir); after != before {
			t.Fatal("invalid patch changed repository")
		}
	})

	t.Run("bounded text", func(t *testing.T) {
		dir, session := patchImportRepository(t)
		path := makeImportPatch(t, dir, func() {
			writePatchImportFile(t, dir, "large.txt", []byte(strings.Repeat("a repeated line\n", 30000)))
		})
		patchInfo, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if patchInfo.Size() <= patchPreviewTextLimit {
			t.Fatalf("test patch is only %d bytes", patchInfo.Size())
		}
		preview, err := session.PreviewPatch(context.Background(), path)
		if err != nil || !preview.Clean {
			t.Fatalf("large preview = %#v, %v", preview, err)
		}
		if !preview.TextTruncated || len(preview.Text) > patchPreviewTextLimit || preview.Additions != 30000 {
			t.Fatalf("bounded preview: truncated=%v len=%d additions=%d", preview.TextTruncated, len(preview.Text), preview.Additions)
		}
		if refs := importRecoveryRefs(t, dir); refs != "" {
			t.Fatalf("large preview created recovery: %q", refs)
		}
	})
}

func TestPatchImportFailureCannotPartiallyApply(t *testing.T) {
	dir, session := patchImportRepository(t)
	writePatchImportFile(t, dir, "b.txt", []byte("bravo\n"))
	runGit(t, dir, "add", "b.txt")
	runGit(t, dir, "commit", "-m", "second file")
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("ALPHA\n"))
		writePatchImportFile(t, dir, "b.txt", []byte("BRAVO\n"))
		writePatchImportFile(t, dir, "new.txt", []byte("would otherwise be added\n"))
	})
	patch := string(readPatchImportFile(t, filepath.Dir(patchPath), filepath.Base(patchPath)))
	patch = strings.Replace(patch, "-bravo", "-not-the-current-content", 1)
	if err := os.WriteFile(patchPath, []byte(patch), 0o600); err != nil {
		t.Fatal(err)
	}
	before := importRepositoryFingerprint(t, dir)
	preview, err := session.PreviewPatch(context.Background(), patchPath)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Clean || preview.Diagnostic == "" {
		t.Fatalf("partially invalid preview = %#v", preview)
	}
	if err := session.ApplyPatch(context.Background(), preview); err == nil {
		t.Fatal("partially invalid patch unexpectedly applied")
	}
	if after := importRepositoryFingerprint(t, dir); after != before {
		t.Fatalf("one valid file or recovery ref changed during rejected patch\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestPatchImportRecoveryCaptureFailureDoesNotApply(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a small POSIX Git wrapper")
	}
	dir, session := patchImportRepository(t)
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "imported.txt", []byte("must not appear\n"))
	})
	writePatchImportFile(t, dir, "untracked.txt", []byte("protect me\n"))
	preview, err := session.PreviewPatch(context.Background(), patchPath)
	if err != nil || !preview.Clean {
		t.Fatalf("preview = %#v, %v", preview, err)
	}

	// Fail only the recovery write, leaving every read-only preflight Git
	// command functional so this exercises the capture-failure guarantee.
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapperDir := t.TempDir()
	wrapper := "#!/bin/sh\nif [ \"$1\" = stash ] && [ \"$2\" = create ]; then\n  echo 'injected recovery failure' >&2\n  exit 1\nfi\nexec \"$TANGIT_TEST_REAL_GIT\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrapperDir, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANGIT_TEST_REAL_GIT", realGit)
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err = session.ApplyPatch(context.Background(), preview)
	if err == nil {
		t.Fatal("patch applied even though recovery capture failed")
	}
	if !strings.Contains(err.Error(), "capture recovery") || !strings.Contains(err.Error(), "injected recovery failure") {
		t.Fatalf("failure happened outside the required recovery capture: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "imported.txt")); !os.IsNotExist(err) {
		t.Fatalf("failed recovery capture allowed patch mutation: %v", err)
	}
	if got := string(readPatchImportFile(t, dir, "untracked.txt")); got != "protect me\n" {
		t.Fatalf("failed recovery capture changed existing work: %q", got)
	}
	if refs := importRecoveryRefs(t, dir); refs != "" {
		t.Fatalf("failed recovery capture published refs: %q", refs)
	}
}

func TestPatchImportBlocksAnActiveMerge(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "new.patch")
	patch := "diff --git a/new.txt b/new.txt\nnew file mode 100644\n--- /dev/null\n+++ b/new.txt\n@@ -0,0 +1 @@\n+new\n"
	if err := os.WriteFile(path, []byte(patch), 0o600); err != nil {
		t.Fatal(err)
	}
	before := importRepositoryFingerprint(t, dir)
	preview, err := session.PreviewPatch(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Clean || !strings.Contains(preview.Diagnostic, "in progress") {
		t.Fatalf("active-merge preview = %#v", preview)
	}
	if err := session.ApplyPatch(context.Background(), preview); err == nil {
		t.Fatal("patch applied during active merge")
	}
	if after := importRepositoryFingerprint(t, dir); after != before {
		t.Fatal("active-merge rejection changed repository")
	}
}
