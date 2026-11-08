package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Review state belongs to the content between HEAD and the working tree. Moving
// that same content into or out of the index is not a new review, while an edit
// is—even when another process makes the edit and the session is reopened.
func TestReviewedFilesFollowContentAndPersist(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\nreviewed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	session, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range snapshot.Files {
		if err := session.SetFileReviewed(ctx, file, true); err != nil {
			t.Fatal(err)
		}
	}
	assertReviewed := func(wantA, wantNew bool) {
		t.Helper()
		snapshot, err = session.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		reviewed, err := session.ReviewedFiles(ctx, snapshot.Files)
		if err != nil {
			t.Fatal(err)
		}
		if reviewed["a.txt"] != wantA || reviewed["new.txt"] != wantNew {
			t.Fatalf("reviewed = %#v, want a=%v new=%v", reviewed, wantA, wantNew)
		}
	}
	assertReviewed(true, true)

	// Staging changes status, not the content the reviewer approved.
	runGit(t, dir, "add", "a.txt")
	assertReviewed(true, true)

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\nreviewed\nnew edit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertReviewed(false, true)

	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertReviewed(false, false)

	// Nothing is stored in the working tree and a new session sees the same
	// content identities. The old files are now edited, so neither is current.
	reopened, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := reopened.ReviewedFiles(ctx, snapshot.Files)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed["a.txt"] || reviewed["new.txt"] {
		t.Fatalf("edited files stayed reviewed after reopening: %#v", reviewed)
	}
	if _, err := os.Stat(filepath.Join(dir, "tangit")); !os.IsNotExist(err) {
		t.Fatalf("review state leaked into the working tree: %v", err)
	}
}

func TestReviewedFileCanBeUnmarked(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	session, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	file := snapshot.Files[0]
	if err := session.SetFileReviewed(ctx, file, true); err != nil {
		t.Fatal(err)
	}
	if err := session.SetFileReviewed(ctx, file, false); err != nil {
		t.Fatal(err)
	}
	reviewed, err := session.ReviewedFiles(ctx, []File{file})
	if err != nil {
		t.Fatal(err)
	}
	if reviewed[file.Path] {
		t.Fatal("an explicitly unreviewed empty file stayed reviewed")
	}
}
