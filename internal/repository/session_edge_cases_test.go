package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Paths git quotes, or takes for its own arguments, survive every common
// operation: unicode and spaces, quotes in the name, and a name that
// looks like a flag.
func TestAwkwardPathsStageDiffCommitAndDiscard(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	names := []string{"你好 世界.txt", `a "b".txt`, "-x.txt", filepath.Join("nested dir", "inner é.txt")}
	for _, name := range names {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("one\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	byPath := func() map[string]File {
		snapshot, err := session.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]File{}
		for _, f := range snapshot.Files {
			out[f.Path] = f
		}
		return out
	}
	files := byPath()
	for _, name := range names {
		f, ok := files[name]
		if !ok {
			t.Fatalf("the snapshot missed %q: %#v", name, files)
		}
		diff, err := session.Diff(context.Background(), f, DiffOptions{ContextLines: 3})
		if err != nil {
			t.Fatalf("diff %q: %v", name, err)
		}
		if !strings.Contains(diff.Patch, "+one") {
			t.Fatalf("diff %q: %q", name, diff.Patch)
		}
		if err := session.Stage(context.Background(), f); err != nil {
			t.Fatalf("stage %q: %v", name, err)
		}
	}
	if err := session.Commit(context.Background(), "add awkward paths", "", false); err != nil {
		t.Fatal(err)
	}
	if left := byPath(); len(left) != 0 {
		t.Fatalf("not clean after committing: %#v", left)
	}
	// Each changed again goes back to what the commit holds.
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("two\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files = byPath()
	for _, name := range names {
		f, ok := files[name]
		if !ok {
			t.Fatalf("the snapshot missed the change to %q: %#v", name, files)
		}
		if err := session.Discard(context.Background(), f); err != nil {
			t.Fatalf("discard %q: %v", name, err)
		}
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "one\n" {
			t.Fatalf("discard %q left %q", name, got)
		}
	}
}

// A text of CRLF breaks, and one with no newline at its end, round trip
// through diff, stage and commit as their exact bytes.
func TestCRLFAndNoTrailingNewlineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	dos := "one\r\ntwo\r\n"
	noneol := "alpha\nbeta"
	if err := os.WriteFile(filepath.Join(dir, "dos.txt"), []byte(dos), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "noneol.txt"), []byte(noneol), 0o600); err != nil {
		t.Fatal(err)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range snapshot.Files {
		diff, err := session.Diff(context.Background(), f, DiffOptions{ContextLines: 3})
		if err != nil {
			t.Fatal(err)
		}
		if f.Path == "noneol.txt" && !strings.Contains(diff.Patch, "\\ No newline at end of file") {
			t.Fatalf("the patch of %q says nothing of its missing newline: %q", f.Path, diff.Patch)
		}
		if err := session.Stage(context.Background(), f); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Commit(context.Background(), "texts", "", false); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"dos.txt": dos, "noneol.txt": noneol} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%q kept %q", name, got)
		}
	}
	// A change to each is discarded back to the exact bytes of the
	// commit.
	for _, name := range []string{"dos.txt", "noneol.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("other"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range snapshot.Files {
		if err := session.Discard(context.Background(), f); err != nil {
			t.Fatalf("discard %q: %v", f.Path, err)
		}
	}
	for name, want := range map[string]string{"dos.txt": dos, "noneol.txt": noneol} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("discard %q left %q", name, got)
		}
	}
}

// A rename git knows of shows as one file of its old path, and commits.
func TestRenameShowsItsPreviousPath(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, "old name.txt"), []byte("body\nsecond\nthird\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "old")
	runGit(t, dir, "mv", "old name.txt", "new name.txt")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 1 {
		t.Fatalf("a rename showed as %#v", snapshot.Files)
	}
	f := snapshot.Files[0]
	if f.Path != "new name.txt" {
		t.Fatalf("a rename showed as %#v", f)
	}
	if f.PreviousPath != "old name.txt" {
		t.Fatalf("a rename kept no old path: %#v", f)
	}
	if err := session.Commit(context.Background(), "rename", "", false); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 0 {
		t.Fatalf("not clean after the rename: %#v", snapshot.Files)
	}
}

// The executable bit of a tracked file changes, stages and commits.
func TestModeChangeStagesAndCommits(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "script")
	if err := os.Chmod(filepath.Join(dir, "run.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 1 {
		t.Fatalf("a mode change showed as %#v", snapshot.Files)
	}
	f := snapshot.Files[0]
	diff, err := session.Diff(context.Background(), f, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "new mode") {
		t.Fatalf("the patch says nothing of the mode: %q", diff.Patch)
	}
	if err := session.Stage(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "make it executable", "", false); err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 0 {
		t.Fatalf("not clean after the mode change: %#v", snapshot.Files)
	}
}

// The root commit, which has no parent, amends like any other.
func TestAmendRootCommit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "first")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Commit(context.Background(), "first again", "with b", true); err != nil {
		t.Fatal(err)
	}
	if n := strings.TrimSpace(gitOutput(t, dir, "rev-list", "--count", "HEAD")); n != "1" {
		t.Fatalf("the amended history holds %s commits", n)
	}
	if subject := gitOutput(t, dir, "log", "-1", "--format=%s"); strings.TrimSpace(subject) != "first again" {
		t.Fatalf("the root commit says %q", subject)
	}
	if body := gitOutput(t, dir, "log", "-1", "--format=%b"); strings.TrimSpace(body) != "with b" {
		t.Fatalf("the root commit's body says %q", body)
	}
}

// Deleting a tracked file comes back whole when it is discarded.
func TestDiscardRestoresADeletedFile(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	path := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(path, []byte("still here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "keep it")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Files) != 1 || !snapshot.Files[0].Deleted {
		t.Fatalf("the deletion showed as %#v", snapshot.Files)
	}
	if err := session.Discard(context.Background(), snapshot.Files[0]); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "still here\n" {
		t.Fatalf("the discard left %q", got)
	}
}
