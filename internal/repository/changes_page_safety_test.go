package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// These tests protect the promise the Changes page makes: nothing a user has
// in their working tree is lost by staging, unstaging, discarding, resolving a
// conflict, continuing or aborting, and whatever the page removes can be
// brought back byte for byte with Undo.

// worktreeDigest hashes every working-tree path outside .git with its mode and
// bytes, so two digests are equal only when no file changed at all.
func worktreeDigest(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(dir, path)
		if relative == ".git" {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s %o %x", filepath.ToSlash(relative), info.Mode().Perm(), sha256.Sum256(content)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// fullDigest adds the index and HEAD to the working tree: the whole state a
// user could lose.
func fullDigest(t *testing.T, dir string) string {
	t.Helper()
	return worktreeDigest(t, dir) + "\n--index--\n" + gitOutput(t, dir, "ls-files", "--stage") +
		"--head--\n" + strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
}

func writeSafetyFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func snapshotFiles(t *testing.T, session Session) []File {
	t.Helper()
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.Files
}

func fileByPath(t *testing.T, files []File, path string) File {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("%s is not among the changes: %#v", path, files)
	return File{}
}

// richWorkspace has every kind of change the page lists: a partly staged
// file, a CRLF edit, a deletion, an edit under an awkward path, a staged new
// file and untracked text, empty and binary files.
func richWorkspace(t *testing.T) (string, FullSession) {
	t.Helper()
	dir, session := recoveryRepository(t)
	writeSafetyFile(t, dir, "b.txt", "b base\n")
	writeSafetyFile(t, dir, "del.txt", "delete me\n")
	writeSafetyFile(t, dir, "sp ace/ünï.txt", "awkward\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "tracked files")

	writeSafetyFile(t, dir, "a.txt", "staged\n")
	runGit(t, dir, "add", "a.txt")
	writeSafetyFile(t, dir, "a.txt", "staged\nunstaged\n")
	writeSafetyFile(t, dir, "b.txt", "b changed\r\nno trailing newline")
	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil {
		t.Fatal(err)
	}
	writeSafetyFile(t, dir, "sp ace/ünï.txt", "awkward\nchanged\n")
	writeSafetyFile(t, dir, "staged-new.txt", "brand new\n")
	runGit(t, dir, "add", "staged-new.txt")
	writeSafetyFile(t, dir, "new.txt", "untracked\n")
	writeSafetyFile(t, dir, "empty.txt", "")
	writeSafetyFile(t, dir, "bin.dat", "\x00\x01\x02binary\x00")
	return dir, session
}

func TestStageAndUnstageEveryKindOfChangeKeepsEveryByte(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	before := worktreeDigest(t, dir)

	// One file at a time, as the row buttons do.
	for _, file := range snapshotFiles(t, session) {
		if !file.Unstaged {
			continue
		}
		if err := session.Stage(ctx, file); err != nil {
			t.Fatalf("stage %s: %v", file.Path, err)
		}
		if got := worktreeDigest(t, dir); got != before {
			t.Fatalf("staging %s changed the working tree", file.Path)
		}
	}
	for _, file := range snapshotFiles(t, session) {
		if file.Unstaged {
			t.Fatalf("%s is still unstaged after Stage: %#v", file.Path, file)
		}
	}
	for _, file := range snapshotFiles(t, session) {
		if err := session.Unstage(ctx, file); err != nil {
			t.Fatalf("unstage %s: %v", file.Path, err)
		}
		if got := worktreeDigest(t, dir); got != before {
			t.Fatalf("unstaging %s changed the working tree", file.Path)
		}
	}
	for _, file := range snapshotFiles(t, session) {
		if file.Staged {
			t.Fatalf("%s is still staged after Unstage: %#v", file.Path, file)
		}
	}
	runGit(t, dir, "diff", "--cached", "--quiet")

	// And all at once.
	if err := session.StageAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := session.UnstageAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got := worktreeDigest(t, dir); got != before {
		t.Fatal("StageAll/UnstageAll changed the working tree")
	}
}

func TestStageOneFileLeavesEveryOtherFileAlone(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	before := fullDigest(t, dir)
	indexBefore := gitOutput(t, dir, "ls-files", "--stage")

	if err := session.Stage(ctx, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
		t.Fatal(err)
	}
	indexAfter := gitOutput(t, dir, "ls-files", "--stage")
	added := 0
	for _, line := range strings.Split(strings.TrimSpace(indexAfter), "\n") {
		if !strings.Contains(indexBefore, line) {
			added++
			if !strings.HasSuffix(line, "\tnew.txt") {
				t.Fatalf("staging new.txt changed another index entry: %s", line)
			}
		}
	}
	if added != 1 {
		t.Fatalf("index changed %d entries, want exactly new.txt", added)
	}
	if err := session.Unstage(ctx, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
		t.Fatal(err)
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatal("stage then unstage of one file did not return to the exact starting state")
	}
}

func TestDiscardingEachKindOfChangeIsUndoneExactly(t *testing.T) {
	for _, path := range []string{
		"a.txt", "b.txt", "del.txt", "sp ace/ünï.txt", "staged-new.txt", "new.txt", "empty.txt", "bin.dat",
	} {
		t.Run(path, func(t *testing.T) {
			dir, session := richWorkspace(t)
			ctx := context.Background()
			before := fullDigest(t, dir)
			file := fileByPath(t, snapshotFiles(t, session), path)
			if err := session.Discard(ctx, file); err != nil {
				t.Fatalf("discard %s: %v", path, err)
			}
			if fullDigest(t, dir) == before {
				t.Fatalf("discarding %s changed nothing", path)
			}
			// Everything except the discarded path is exactly as it was.
			for _, other := range []string{"a.txt", "b.txt", "new.txt", "bin.dat"} {
				if other == path {
					continue
				}
				if _, err := os.Stat(filepath.Join(dir, other)); err != nil {
					t.Fatalf("discarding %s removed %s: %v", path, other, err)
				}
			}
			record := onlyOperation(t, session)
			if !record.CanUndo() {
				t.Fatalf("discard of %s is not undoable: %#v", path, record)
			}
			if err := session.UndoOperation(ctx, record.ID); err != nil {
				t.Fatalf("undo discard of %s: %v", path, err)
			}
			if got := fullDigest(t, dir); got != before {
				t.Fatalf("undo of %s did not restore the exact state\n got: %s\nwant: %s", path, got, before)
			}
		})
	}
}

func TestDiscardingEverythingIsUndoneExactlyAndSurvivesRestart(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	before := fullDigest(t, dir)
	if err := session.DiscardFiles(ctx, snapshotFiles(t, session)); err != nil {
		t.Fatal(err)
	}
	if left := snapshotFiles(t, session); len(left) != 0 {
		t.Fatalf("discard left %#v", left)
	}
	record := onlyOperation(t, session)
	reopened, err := openFullRepositorySessionForTest((Opener{}).Open(ctx, dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo after restart: %v", err)
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatalf("undo after restart lost work\n got: %s\nwant: %s", got, before)
	}
}

func TestUndoNeverOverwritesWorkDoneAfterTheDiscard(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	if err := session.Discard(ctx, fileByPath(t, snapshotFiles(t, session), "b.txt")); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	writeSafetyFile(t, dir, "b.txt", "newer work after discard\n")
	other := worktreeDigest(t, dir)
	if err := session.UndoOperation(ctx, record.ID); err == nil {
		t.Fatal("undo overwrote newer work without objecting")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(got) != "newer work after discard\n" {
		t.Fatalf("newer work lost: %q", got)
	}
	if got := worktreeDigest(t, dir); got != other {
		t.Fatal("a refused undo still changed the working tree")
	}
}

func TestDiscardRefusesAConflictedFileAndKeepsItsMarkers(t *testing.T) {
	dir := conflictedRepository(t)
	enableSafetyJournalForTest(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	before := fullDigest(t, dir)
	file := fileNamed(t, mustSnapshot(t, session))
	if err := session.Discard(context.Background(), file); err == nil {
		t.Fatal("discarding a conflicted file must be refused")
	}
	if err := session.DiscardFiles(context.Background(), []File{file}); err == nil {
		t.Fatal("batch discard of a conflicted file must be refused")
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatal("a refused discard changed the repository")
	}
}

func mustSnapshot(t *testing.T, session Session) Snapshot {
	t.Helper()
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func abortMerge(t *testing.T, session Session) {
	t.Helper()
	operations := operationSession(t, session)
	state, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := operations.DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint,
	}); err != nil {
		t.Fatalf("abort: %v", err)
	}
}

// Abort must return to exactly the state before the merge, whichever way the
// conflicts were handled meanwhile, and must not touch the user's untracked
// files.
func TestAbortAfterEveryWayOfResolvingRestoresThePreMergeState(t *testing.T) {
	cases := map[string]func(t *testing.T, session FullSession, file File){
		"nothing resolved": func(*testing.T, FullSession, File) {},
		"use current": func(t *testing.T, session FullSession, file File) {
			if err := session.ResolveConflict(context.Background(), file, true); err != nil {
				t.Fatal(err)
			}
		},
		"use incoming": func(t *testing.T, session FullSession, file File) {
			if err := session.ResolveConflict(context.Background(), file, false); err != nil {
				t.Fatal(err)
			}
		},
		"mark resolved with markers": func(t *testing.T, session FullSession, file File) {
			if err := session.Stage(context.Background(), file); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, resolve := range cases {
		t.Run(name, func(t *testing.T) {
			dir := conflictedRepository(t)
			enableSafetyJournalForTest(t)
			writeSafetyFile(t, dir, "abort-should-keep-untracked.txt", "mine\n")
			writeSafetyFile(t, dir, "sub/other-untracked.txt", "also mine\n")
			session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
			if err != nil {
				t.Fatal(err)
			}
			head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
			resolve(t, session, fileNamed(t, mustSnapshot(t, session)))
			abortMerge(t, session)

			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != head {
				t.Fatalf("abort moved HEAD: %s, want %s", got, head)
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "file.txt")); string(got) != "main\n" {
				t.Fatalf("file.txt after abort = %q, want the pre-merge text", got)
			}
			for path, want := range map[string]string{
				"abort-should-keep-untracked.txt": "mine\n", "sub/other-untracked.txt": "also mine\n",
			} {
				if got, err := os.ReadFile(filepath.Join(dir, path)); err != nil || string(got) != want {
					t.Fatalf("abort lost untracked %s: %q, %v", path, got, err)
				}
			}
			state, err := operationSession(t, session).OperationState(context.Background())
			if err != nil || state.Type != OperationTypeNone {
				t.Fatalf("operation survived abort: %#v, %v", state, err)
			}
			gitOutput(t, dir, "diff", "--cached", "--exit-code")
			files := mustSnapshot(t, session).Files
			for _, file := range files {
				if file.HasMergeConflicts || file.Staged {
					t.Fatalf("abort left %#v", file)
				}
			}
		})
	}
}

func TestContinueKeepsTheChosenSideAndBothParents(t *testing.T) {
	for name, current := range map[string]bool{"current": true, "incoming": false} {
		t.Run(name, func(t *testing.T) {
			dir := conflictedRepository(t)
			session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
			if err != nil {
				t.Fatal(err)
			}
			writeSafetyFile(t, dir, "untracked-through-merge.txt", "mine\n")
			if err := session.ResolveConflict(context.Background(), fileNamed(t, mustSnapshot(t, session)), current); err != nil {
				t.Fatal(err)
			}
			operations := operationSession(t, session)
			state, err := operations.OperationState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := operations.DriveOperation(context.Background(), OperationDriveOptions{
				Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
			}); err != nil {
				t.Fatalf("continue: %v", err)
			}
			want := "feature\n"
			if current {
				want = "main\n"
			}
			if got := gitOutput(t, dir, "show", "HEAD:file.txt"); got != want {
				t.Fatalf("merge commit holds %q, want %q", got, want)
			}
			if parents := strings.Fields(gitOutput(t, dir, "rev-list", "--parents", "-n1", "HEAD")); len(parents) != 3 {
				t.Fatalf("merge commit parents = %v", parents)
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "untracked-through-merge.txt")); string(got) != "mine\n" {
				t.Fatalf("untracked work lost through the merge: %q", got)
			}
		})
	}
}

func TestContinueRefusesWhileAConflictIsUnresolvedAndLosesNothing(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	before := fullDigest(t, dir)
	operations := operationSession(t, session)
	state, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := operations.DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
	}); err == nil {
		t.Fatal("continue with an unresolved conflict must fail")
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatal("a refused continue changed the repository")
	}
}

func TestAbortRefusesAStaleFingerprintAndChangesNothing(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	operations := operationSession(t, session)
	state, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The user resolves a file after the page last looked.
	if err := session.ResolveConflict(context.Background(), fileNamed(t, mustSnapshot(t, session)), true); err != nil {
		t.Fatal(err)
	}
	before := fullDigest(t, dir)
	if err := operations.DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint,
	}); err == nil {
		t.Fatal("abort accepted a fingerprint from before the user's change")
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatal("a refused abort changed the repository")
	}
}

func TestUndoingACommitReturnsStagedAndUnstagedWorkExactly(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	before := fullDigest(t, dir)
	if err := session.Commit(ctx, "commit what is staged", "", false); err != nil {
		t.Fatal(err)
	}
	// Unstaged work and untracked files are not part of that commit.
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "staged\nunstaged\n" {
		t.Fatalf("commit touched unstaged work: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "new.txt")); string(got) != "untracked\n" {
		t.Fatalf("commit touched an untracked file: %q", got)
	}
	record := onlyOperation(t, session)
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo commit: %v", err)
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatalf("undo commit did not restore the exact state\n got: %s\nwant: %s", got, before)
	}
}

func TestStashAllThenRestoreBringsEverythingBack(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	// Git records only the executable bit, so a restored file may be 0644
	// where the test wrote 0600; the contents and paths must match exactly.
	contents := func() string {
		return strings.NewReplacer(" 600 ", " ", " 644 ", " ").Replace(worktreeDigest(t, dir))
	}
	before := contents()
	if err := session.StashChanges(ctx, "safety", true); err != nil {
		t.Fatalf("stash: %v", err)
	}
	if left := snapshotFiles(t, session); len(left) != 0 {
		t.Fatalf("stash left %#v", left)
	}
	gitOutput(t, dir, "stash", "pop", "--index")
	if got := contents(); got != before {
		t.Fatalf("stash round trip changed the working tree\n got: %s\nwant: %s", got, before)
	}
}
