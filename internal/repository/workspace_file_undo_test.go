package repository

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUndoRestoreHistoricalFileRecoversWorkingBytesIndexAndPermissions(t *testing.T) {
	dir, session := recoveryRepository(t)
	binary := filepath.Join(dir, "payload.bin")
	older := []byte{0, 1, 0xff}
	newer := []byte{2, 0, 0xfe}
	unsaved := []byte{3, 0xff, 1}
	if err := os.WriteFile(binary, older, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "payload.bin")
	runGit(t, dir, "commit", "-m", "older binary")
	olderSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(binary, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "newer binary")
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(binary, unsaved, 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binary, 0o751); err != nil {
		t.Fatal(err)
	}
	index := gitOutput(t, dir, "ls-files", "--stage")
	ctx := context.Background()
	if err := session.RestoreCommitFile(ctx, Commit{Hash: olderSHA, ShortHash: olderSHA[:7]}, CommitFile{Path: "payload.bin"}); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(binary); err != nil || !bytes.Equal(got, older) {
		t.Fatalf("historical restore did not read its source: %v, %v", got, err)
	}
	record := onlyOperation(t, session)
	if record.Kind != OperationFileRestore || !record.CanUndo() {
		t.Fatalf("historical file restore has no safe inverse: %+v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo historical file restore: %v", err)
	}
	if got, err := os.ReadFile(binary); err != nil || !bytes.Equal(got, unsaved) {
		t.Fatalf("Undo replaced newer working bytes: %v, %v", got, err)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage"); got != index {
		t.Fatalf("Undo changed the original staged index: %q, want %q", got, index)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != head {
		t.Fatalf("file-only restore moved HEAD to %s, want %s", got, head)
	}
	info, err := os.Stat(binary)
	if err != nil || info.Mode().Perm() != 0o751 {
		t.Fatalf("Undo lost raw permission bits: %v, %v", info, err)
	}
}
