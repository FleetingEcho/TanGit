package repository

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceSnapshotPreservesRawFilteredPayloads(t *testing.T) {
	dir, session := recoveryRepository(t)
	concrete := session.(*gitSession)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("payload.bin filter=testfilter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".gitattributes")
	runGit(t, dir, "commit", "-m", "configure binary filter")
	runGit(t, dir, "config", "filter.testfilter.clean", "tr A-Z a-z")
	runGit(t, dir, "config", "filter.testfilter.smudge", "tr a-z A-Z")
	original := []byte("ORIGINAL\x00\xffPAYLOAD\n")
	modified := []byte("MODIFIED\x00\xffBYTES\n")
	payload := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(payload, original, 0o711); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(payload, 0o711); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "payload.bin")
	if err := os.WriteFile(payload, modified, 0o711); err != nil {
		t.Fatal(err)
	}
	storedIndex := gitOutput(t, dir, "show", ":payload.bin")
	if !bytes.Equal([]byte(storedIndex), []byte("original\x00\xffpayload\n")) {
		t.Fatalf("fixture index did not apply the clean filter: %v", []byte(storedIndex))
	}
	snapshot, err := concrete.captureWorkSnapshot(ctx, CheckpointSnapshotKind, "Raw filtered binary")
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "gc", "--prune=now")
	runGit(t, dir, "reset", "--hard", "HEAD")
	runGit(t, dir, "clean", "-fd")
	if _, err := os.Stat(payload); !os.IsNotExist(err) {
		t.Fatalf("fixture did not delete the raw filtered payload: %v", err)
	}
	reopened, err := openFullRepositorySessionForTest((Opener{}).Open(ctx, dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.(*gitSession).restoreJournalWorkSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(payload); err != nil || !bytes.Equal(content, modified) {
		t.Fatalf("raw filtered worktree = %v, want %v (%v)", content, modified, err)
	}
	info, err := os.Stat(payload)
	if err != nil || info.Mode().Perm() != 0o711 {
		t.Fatalf("raw filtered permissions = %v, %v", info, err)
	}
	if got := gitOutput(t, dir, "show", ":payload.bin"); got != storedIndex {
		t.Fatalf("filtered index content changed: %v, want %v", []byte(got), []byte(storedIndex))
	}
}

func TestWorkspaceSnapshotPreservesConflictIndexAndOperationMetadata(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	concrete := session.(*gitSession)
	base := []byte("one\ntwo\nthree\nfour\n")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), base, 0o751); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "merge base")
	runGit(t, dir, "checkout", "-b", "incoming")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("incoming\ntwo\nthree\nfour\n"), 0o751); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "incoming edit")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("mine\ntwo\nthree\nfour\n"), 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "a.txt"), 0o751); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "main edit")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("private.env\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "private.env"), []byte("preserve-conflict-recovery\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	clean, err := concrete.captureWorkSnapshot(ctx, RecoverySnapshotKind, "Before conflicted merge")
	if err != nil {
		t.Fatal(err)
	}
	cleanIndex := gitOutput(t, dir, "ls-files", "--stage")
	command := exec.Command("git", "-C", dir, "merge", "--no-ff", "incoming")
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("fixture merge was expected to conflict: %s", output)
	}
	state, err := concrete.OperationState(ctx)
	if err != nil || state.Type != OperationTypeMerge {
		t.Fatalf("fixture merge state = %q, %v", state.Type, err)
	}
	conflicted, err := concrete.captureWorkSnapshot(ctx, CheckpointSnapshotKind, "Conflicted merge")
	if err != nil {
		t.Fatalf("capture conflicted operation: %v", err)
	}
	conflictedIndex := gitOutput(t, dir, "ls-files", "--stage")
	mergeHead, err := os.ReadFile(filepath.Join(dir, ".git", "MERGE_HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	mergeMessage, err := os.ReadFile(filepath.Join(dir, ".git", "MERGE_MSG"))
	if err != nil {
		t.Fatal(err)
	}
	autoMerge, autoMergeErr := os.ReadFile(filepath.Join(dir, ".git", "AUTO_MERGE"))
	// Prune anything not reachable through the two recovery refs. This
	// includes index-only blobs, raw conflict payloads and operation trees.
	runGit(t, dir, "gc", "--prune=now")
	if autoMergeErr == nil && strings.TrimSpace(string(autoMerge)) != "" {
		if _, err := concrete.gitText("rev-parse", "--verify", strings.TrimSpace(string(autoMerge))+"^{tree}"); err != nil {
			t.Fatalf("AUTO_MERGE tree was not pinned: %v", err)
		}
	}
	if err := concrete.restoreJournalWorkSnapshot(conflicted); err != nil {
		t.Fatalf("restore conflicted operation: %v", err)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage"); got != conflictedIndex {
		t.Fatalf("restored conflict index =\n%s\nwant\n%s", got, conflictedIndex)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".git", "MERGE_HEAD")); string(got) != string(mergeHead) {
		t.Fatalf("restored MERGE_HEAD = %q, want %q", got, mergeHead)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".git", "MERGE_MSG")); string(got) != string(mergeMessage) {
		t.Fatalf("restored MERGE_MSG = %q, want %q", got, mergeMessage)
	}
	if autoMergeErr == nil && (strings.TrimSpace(string(autoMerge)) != "") {
		if got, err := os.ReadFile(filepath.Join(dir, ".git", "AUTO_MERGE")); err != nil || string(got) != string(autoMerge) {
			t.Fatalf("restored AUTO_MERGE = %q, want %q (%v)", got, autoMerge, err)
		}
	}
	reopened, err := openFullRepositorySessionForTest((Opener{}).Open(ctx, dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.(*gitSession).restoreJournalWorkSnapshot(clean); err != nil {
		t.Fatalf("restore original merge state after restart: %v", err)
	}
	if state, err := reopened.(*gitSession).OperationState(ctx); err != nil || state.Type != OperationTypeNone {
		t.Fatalf("operation after restoring clean snapshot = %q, %v", state.Type, err)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage"); got != cleanIndex {
		t.Fatalf("clean staged index =\n%s\nwant\n%s", got, cleanIndex)
	}
	info, err := os.Stat(filepath.Join(dir, "a.txt"))
	if err != nil || info.Mode().Perm() != 0o751 {
		t.Fatalf("clean conflict-file permissions = %v, %v", info.Mode(), err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "private.env")); string(got) != "preserve-conflict-recovery\n" {
		t.Fatalf("ignored file changed while restoring operation state: %q", got)
	}
}
