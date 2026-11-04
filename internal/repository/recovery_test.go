package repository

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func recoveryRepository(t *testing.T) (string, Session) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "base")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, session
}

func nativeStashList(t *testing.T, dir string) string {
	t.Helper()
	command := exec.Command("git", "stash", "list")
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

// Recovery captures the exact index/worktree split before a batch discard and
// restores it without consuming its entry or manufacturing a native stash.
func TestDiscardRecoveryRestoresStagedUnstagedAndUntrackedWork(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	before, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head := before.Commits[0].Hash
	if err := session.DiscardFiles(ctx, core.Files); err != nil {
		t.Fatal(err)
	}
	if core, err = session.Snapshot(ctx); err != nil || len(core.Files) != 0 {
		t.Fatalf("discard left changes: %#v, %v", core.Files, err)
	}
	snapshots, err := session.WorkSnapshots(ctx)
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("recovery entries = %#v, %v", snapshots, err)
	}
	if snapshots[0].Kind != RecoverySnapshotKind || nativeStashList(t, dir) != "" {
		t.Fatalf("recovery leaked into native stash: %#v %q", snapshots, nativeStashList(t, dir))
	}
	if err := session.RestoreWorkSnapshot(ctx, snapshots[0]); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(content) != "staged\nunstaged\n" {
		t.Fatalf("restored working content = %q, %v", content, err)
	}
	content, err = os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil || string(content) != "new\n" {
		t.Fatalf("restored untracked content = %q, %v", content, err)
	}
	command := exec.Command("git", "show", ":a.txt")
	command.Dir = dir
	indexed, err := command.Output()
	if err != nil || string(indexed) != "staged\n" {
		t.Fatalf("restored index = %q, %v", indexed, err)
	}
	after, err := session.Snapshot(ctx)
	if err != nil || after.Commits[0].Hash != head {
		t.Fatalf("restore moved HEAD: %v, %v", after.Commits, err)
	}
	kept, err := session.WorkSnapshots(ctx)
	if err != nil || len(kept) != 1 {
		t.Fatalf("restore consumed recovery: %#v, %v", kept, err)
	}
	if nativeStashList(t, dir) != "" {
		t.Fatal("restore added an ordinary stash")
	}
}

func TestNamedCheckpointPersistsAndRestoreRejectsOverlap(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("checkpoint\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := session.CreateCheckpoint(ctx, "Before experiment")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Kind != CheckpointSnapshotKind || nativeStashList(t, dir) != "" {
		t.Fatalf("bad checkpoint: %#v, stash %q", checkpoint, nativeStashList(t, dir))
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(content) != "checkpoint\n" {
		t.Fatalf("checkpoint changed the working tree: %q", content)
	}
	core, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.DiscardFiles(ctx, core.Files); err != nil {
		t.Fatal(err)
	}
	if err := session.RestoreWorkSnapshot(ctx, checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("new overlapping work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = session.RestoreWorkSnapshot(ctx, checkpoint)
	var conflict *RestoreConflictError
	if !errors.As(err, &conflict) || len(conflict.Paths) != 1 || conflict.Paths[0] != "a.txt" {
		t.Fatalf("overlap error = %#v", err)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(content) != "new overlapping work\n" {
		t.Fatalf("failed restore clobbered overlap: %q", content)
	}
	if err := session.DeleteWorkSnapshot(ctx, checkpoint); err != nil {
		t.Fatal(err)
	}
	snapshots, err := session.WorkSnapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == checkpoint.ID {
			t.Fatal("deleted checkpoint returned")
		}
	}
}

func TestRecoveryCaptureFailureDoesNotDiscard(t *testing.T) {
	dir, session := recoveryRepository(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Removing the repository's object directory guarantees that capture fails
	// before a destructive command; the working file itself must remain.
	if err := os.RemoveAll(filepath.Join(dir, ".git", "objects")); err != nil {
		t.Fatal(err)
	}
	core, err := session.Snapshot(context.Background())
	if err == nil {
		if err := session.Discard(context.Background(), core.Files[0]); err == nil {
			t.Fatal("discard unexpectedly succeeded after capture failure")
		}
	}
	content, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || !strings.Contains(string(content), "keep") {
		t.Fatalf("failed capture discarded content: %q, %v", content, err)
	}
}
