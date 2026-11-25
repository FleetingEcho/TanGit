package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Picking an untracked file to stash is the request to include it: the stash
// must not fail with "include untracked", and must leave other untracked files.
func TestStashSelectedFilesSavesAnUntrackedFileWithoutTheFlag(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	for name, content := range map[string]string{"picked.txt": "picked\n", "left.txt": "left\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.StashSelectedFiles(ctx, "picked", []File{{Path: "picked.txt", Unstaged: true}}, false); err != nil {
		t.Fatalf("stashing an untracked file failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "picked.txt")); !os.IsNotExist(err) {
		t.Fatalf("the picked untracked file was not stashed: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "left.txt")); err != nil || string(content) != "left\n" {
		t.Fatalf("an unpicked untracked file was touched: %q, %v", content, err)
	}
	if err := session.ApplyStash(ctx, 0, false); err != nil {
		t.Fatalf("applying the stash failed: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "picked.txt")); err != nil || string(content) != "picked\n" {
		t.Fatalf("the stashed untracked file did not come back: %q, %v", content, err)
	}
}

// Other programs write into a checkout while an action takes its recovery
// point. A change that lands between taking the point and checking it must
// retry the point, not refuse an action that has not run yet.
func TestRecoveryPointIsTakenAgainWhenTheWorkspaceChangedUnderIt(t *testing.T) {
	_, session := recoveryRepository(t)
	real := session.(*gitSession)
	ctx := context.Background()
	attempts := 0
	_, record, err := real.captureAndBeginOperation(ctx, func(ctx context.Context) (WorkSnapshot, error) {
		attempts++
		if attempts < 3 {
			return WorkSnapshot{}, errors.New("fatal: confused by unstable object source data for abc")
		}
		return real.captureWorkSnapshot(ctx, RecoverySnapshotKind, "retry test")
	}, OperationDiscard, "retry test", UndoRestoreWork)
	if err != nil || record.ID == "" || attempts != 3 {
		t.Fatalf("recovery point after %d attempts: record %q, err %v", attempts, record.ID, err)
	}

	// An error that is not about a busy workspace is not retried.
	attempts = 0
	boom := errors.New("disk full")
	_, _, err = real.captureAndBeginOperation(ctx, func(context.Context) (WorkSnapshot, error) {
		attempts++
		return WorkSnapshot{}, boom
	}, OperationDiscard, "no retry", UndoRestoreWork)
	if !errors.Is(err, boom) || attempts != 1 {
		t.Fatalf("a real failure was retried: %d attempts, err %v", attempts, err)
	}

	// A workspace that never settles still fails, after a bounded number of tries.
	attempts = 0
	_, _, err = real.captureAndBeginOperation(ctx, func(context.Context) (WorkSnapshot, error) {
		attempts++
		return WorkSnapshot{}, errors.New("workspace changed while creating the recovery point; the action was not run")
	}, OperationDiscard, "gives up", UndoRestoreWork)
	if err == nil || attempts != recoveryAttempts {
		t.Fatalf("a never-settling workspace: %d attempts, err %v", attempts, err)
	}
}

// The remembered hash of a file is reused only while the file is as it was.
func TestFileHashCacheTracksTheFileItDescribes(t *testing.T) {
	_, session := recoveryRepository(t)
	real := session.(*gitSession)
	name := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(name, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(name, old, old); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := real.cachedFileHash(name, info); ok {
		t.Fatal("an unseen file had a remembered hash")
	}
	real.rememberFileHash(name, info, "oid-one")
	if oid, ok := real.cachedFileHash(name, info); !ok || oid != "oid-one" {
		t.Fatalf("an unchanged file lost its hash: %q, %v", oid, ok)
	}
	if err := os.WriteFile(name, []byte("three"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, old, old); err != nil {
		t.Fatal(err)
	}
	changed, err := os.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := real.cachedFileHash(name, changed); ok {
		t.Fatal("a file with a new size reused the old hash")
	}

	// A file written moments ago may be written again within the same
	// timestamp, so its hash is not trusted.
	fresh := filepath.Join(t.TempDir(), "fresh.bin")
	if err := os.WriteFile(fresh, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	freshInfo, err := os.Lstat(fresh)
	if err != nil {
		t.Fatal(err)
	}
	real.rememberFileHash(fresh, freshInfo, "oid-fresh")
	if _, ok := real.cachedFileHash(fresh, freshInfo); ok {
		t.Fatal("a just-written file was trusted")
	}
}
