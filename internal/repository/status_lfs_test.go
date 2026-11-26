package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// lfsOff turns the repository's filter off for the child Git commands the test
// uses to set it up, as an environment without git-lfs would see it.
var lfsOff = []string{
	"-c", "filter.lfs.required=false", "-c", "filter.lfs.process=",
	"-c", "filter.lfs.clean=", "-c", "filter.lfs.smudge=",
}

// newLFSRepositoryWithoutLFS makes a repository whose .gitattributes route
// *.bin through an LFS filter that cannot run, as when the app is started with
// a PATH that lacks git-lfs, and then makes both files' stat data stale the way
// a merge that rewrote the working tree does.
func newLFSRepositoryWithoutLFS(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "filter.lfs.required", "true")
	// The same command lines git-lfs installs, under a name that does not exist.
	runGit(t, dir, "config", "filter.lfs.process", "git-lfs-missing filter-process")
	runGit(t, dir, "config", "filter.lfs.clean", "git-lfs-missing clean -- %f")
	runGit(t, dir, "config", "filter.lfs.smudge", "git-lfs-missing smudge -- %f")
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitattributes", "*.bin filter=lfs\n")
	write("asset.bin", "pointer\n")
	write("notes.txt", "one\n")
	runGit(t, dir, append(append([]string{}, lfsOff...), "add", "-A")...)
	runGit(t, dir, append(append([]string{"-c", "user.name=T", "-c", "user.email=t@t"}, lfsOff...), "commit", "-m", "init")...)

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "asset.bin"), later, later); err != nil {
		t.Fatal(err)
	}
	write("notes.txt", "two\n")
	return dir
}

func coreSnapshotForTest(t *testing.T, dir string) Snapshot {
	t.Helper()
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	progressive, ok := session.(ProgressiveSession)
	if !ok {
		t.Fatal("session is not progressive")
	}
	snapshot, err := progressive.CoreSnapshot(context.Background())
	if err != nil {
		t.Fatalf("CoreSnapshot: %v", err)
	}
	return snapshot
}

// A status that fails because a Git LFS filter cannot run used to come back
// from Lazygit's loader as an empty list, so a working tree full of changes
// was shown as clean. It must list the changes and say LFS was unavailable.
func TestCoreSnapshotListsChangesWhenGitLFSCannotRun(t *testing.T) {
	dir := newLFSRepositoryWithoutLFS(t)

	// The setup really is the failing case the bug needs.
	runGitExpectFailure(t, dir, "status", "--porcelain")

	snapshot := coreSnapshotForTest(t, dir)
	var paths []string
	for _, file := range snapshot.Files {
		paths = append(paths, file.Path)
	}
	if !contains(paths, "notes.txt") {
		t.Fatalf("a modified file was hidden while git-lfs could not run: files = %v", paths)
	}
	warned := false
	for _, warning := range snapshot.Warnings {
		warned = warned || strings.Contains(warning, "Git LFS")
	}
	if !warned {
		t.Fatalf("the degraded status was not reported: warnings = %v", snapshot.Warnings)
	}
}

// The extra check only runs for an empty list, and a clean tree stays clean,
// without a warning.
func TestCoreSnapshotCleanTreeStaysCleanWithoutLFSWarning(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "-c", "user.name=T", "-c", "user.email=t@t", "commit", "-m", "init")

	snapshot := coreSnapshotForTest(t, dir)
	if len(snapshot.Files) != 0 {
		t.Fatalf("a clean tree listed files: %v", snapshot.Files)
	}
	for _, warning := range snapshot.Warnings {
		if strings.Contains(warning, "Git LFS") {
			t.Fatalf("a clean tree warned about Git LFS: %q", warning)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
