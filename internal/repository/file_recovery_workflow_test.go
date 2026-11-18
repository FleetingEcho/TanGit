package repository

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileRecoveryWorkflowDiscoversAndAdvancesOriginalRecord(t *testing.T) {
	dir, _, session := fileRecoveryRepository(t, "conflict.txt", []byte("base\n"))
	workflow := session.(FileRecoveryWorkflowSession)
	ctx := context.Background()
	path := filepath.Join(dir, "conflict.txt")
	original := []byte("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\n")
	if err := os.WriteFile(path, original, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	initial, err := workflow.CaptureFileRecoveryState(ctx, "conflict.txt")
	if err != nil {
		t.Fatal(err)
	}
	record, err := workflow.ArchiveFileRecovery(ctx, FileRecoveryArchive{Path: "conflict.txt", After: initial})
	if err != nil {
		t.Fatal(err)
	}
	originalIndex := fileRecoveryIndexBytes(t, dir, "conflict.txt")

	resolved := []byte("resolved\x00bytes\n")
	if err := os.WriteFile(path, resolved, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", "conflict.txt")
	after, err := workflow.CaptureFileRecoveryState(ctx, "conflict.txt")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := workflow.RecordFileRecoveryAfter(ctx, record.OperationID, after)
	if err != nil {
		t.Fatal(err)
	}
	if !fileRecoveryStateEqual(updated.Initial, record.Initial) {
		t.Fatal("recording the after-state replaced the original initial state")
	}
	if !fileRecoveryStateEqual(updated.After, after) {
		t.Fatal("recorded after-state does not match the captured bytes and stages")
	}
	if other := activeIDForRecoveryTest(t, workflow, "unrelated.txt"); other != "" {
		t.Fatalf("unrelated path unexpectedly acquired recovery %q", other)
	}

	reopened, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	reopenedWorkflow := reopened.(FileRecoveryWorkflowSession)
	records, err := reopenedWorkflow.ListFileRecovery(ctx)
	if err != nil || len(records) != 1 {
		t.Fatalf("reopened records = %#v, %v", records, err)
	}
	stored := records[0]
	if stored.OperationID != record.OperationID || !fileRecoveryStateEqual(stored.Initial, record.Initial) ||
		!fileRecoveryStateEqual(stored.After, after) {
		t.Fatalf("durable record changed across reopen: %#v", stored)
	}
	if err := reopenedWorkflow.RestoreFileRecovery(ctx, stored.OperationID); err != nil {
		t.Fatal(err)
	}
	if got := fileRecoveryReadFile(t, dir, "conflict.txt"); !bytes.Equal(got, original) {
		t.Fatalf("restored original = %q, want %q", got, original)
	}
	if got := fileRecoveryIndexBytes(t, dir, "conflict.txt"); !bytes.Equal(got, originalIndex) {
		t.Fatalf("restored index = %q, want %q", got, originalIndex)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("original executable mode missing after workflow restore: %v, %v", info, err)
	}
}

func activeIDForRecoveryTest(t *testing.T, workflow FileRecoveryWorkflowSession, path string) string {
	t.Helper()
	for _, record := range mustListFileRecovery(t, workflow) {
		if record.Path == path && record.Status == FileRecoveryReady {
			return record.OperationID
		}
	}
	return ""
}

func mustListFileRecovery(t *testing.T, workflow FileRecoveryWorkflowSession) []FileRecoveryRecord {
	t.Helper()
	records, err := workflow.ListFileRecovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return records
}
