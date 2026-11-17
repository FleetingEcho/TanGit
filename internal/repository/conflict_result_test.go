package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reviewedConflictResultRequest(t *testing.T, session Session, dir, path string) ConflictResultRequest {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}
	stages, err := session.ConflictStages(context.Background(), File{Path: path, HasMergeConflicts: true})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := session.(OperationSession).OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return ConflictResultRequest{
		Path: path, WorktreeDigest: ConflictWorktreeDigest(content),
		IndexDigest: ConflictStagesDigest(stages), OperationFingerprint: operation.Fingerprint,
		Result: []byte("value=main\n"),
	}
}

func TestWriteConflictResultOnlyChangesResultAndSavesV2Recovery(t *testing.T) {
	dir := fileRecoveryUURepository(t, false)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	writer := session.(ConflictResultSession)
	request := reviewedConflictResultRequest(t, session, dir, "f.txt")
	beforeResult := fileRecoveryReadFile(t, dir, "f.txt")
	beforeIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
	beforeMergeHead := strings.TrimSpace(string(fileRecoveryGitBytes(t, dir, "rev-parse", "MERGE_HEAD")))

	record, err := writer.WriteConflictResult(context.Background(), request)
	if err != nil {
		t.Fatalf("WriteConflictResult: %v", err)
	}
	if got := string(fileRecoveryReadFile(t, dir, "f.txt")); got != "value=main\n" {
		t.Fatalf("Result = %q, want only the reviewed replacement", got)
	}
	if afterIndex := fileRecoveryIndexBytes(t, dir, "f.txt"); !bytes.Equal(afterIndex, beforeIndex) {
		t.Fatalf("accept staged or otherwise changed the index:\n got %q\nwant %q", afterIndex, beforeIndex)
	}
	if afterMergeHead := strings.TrimSpace(string(fileRecoveryGitBytes(t, dir, "rev-parse", "MERGE_HEAD"))); afterMergeHead != beforeMergeHead {
		t.Fatalf("accept continued the operation: MERGE_HEAD %q -> %q", beforeMergeHead, afterMergeHead)
	}
	if record.Version != FileRecoveryManifestVersion || record.OperationID == "" {
		t.Fatalf("missing V2 recovery record: %#v", record)
	}
	if !fileRecoveryIndexEqual(record.Initial.Index, record.After.Index) {
		t.Fatal("V2 after-state does not preserve the original unmerged index")
	}
	if got := string(request.Result); record.After.Worktree.Digest != fileRecoveryDigest([]byte(got)) {
		t.Fatal("V2 after-state does not contain the accepted Result")
	}
	if snapshots, err := session.WorkSnapshots(context.Background()); err != nil || len(snapshots) != 0 {
		t.Fatalf("V2 Result recovery leaked into V1 Saved work: %#v, %v", snapshots, err)
	}

	recovery := session.(FileRecoverySession)
	if err := recovery.RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
		t.Fatalf("restore accepted Result recovery: %v", err)
	}
	if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, beforeResult) {
		t.Fatalf("restored conflict source = %q, want original %q", got, beforeResult)
	}
	if restored := fileRecoveryIndexBytes(t, dir, "f.txt"); !bytes.Equal(restored, beforeIndex) {
		t.Fatalf("restore did not recreate the exact index: %q", restored)
	}
}

func TestWriteConflictResultRejectsEveryChangedSourceWithoutWriting(t *testing.T) {
	t.Run("raw hash mismatch", func(t *testing.T) {
		dir := fileRecoveryUURepository(t, false)
		session, _ := (Opener{}).Open(context.Background(), dir)
		request := reviewedConflictResultRequest(t, session, dir, "f.txt")
		before := fileRecoveryReadFile(t, dir, "f.txt")
		request.WorktreeDigest = "sha256:" + strings.Repeat("0", 64)
		_, err := session.(ConflictResultSession).WriteConflictResult(context.Background(), request)
		if !errors.Is(err, ErrConflictResultChanged) {
			t.Fatalf("error = %v, want ErrConflictResultChanged", err)
		}
		if after := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(after, before) {
			t.Fatal("raw hash mismatch wrote Result")
		}
	})

	t.Run("actual index changed", func(t *testing.T) {
		dir := fileRecoveryUURepository(t, false)
		session, _ := (Opener{}).Open(context.Background(), dir)
		request := reviewedConflictResultRequest(t, session, dir, "f.txt")
		before := fileRecoveryReadFile(t, dir, "f.txt")
		runGit(t, dir, "add", "--", "f.txt")
		_, err := session.(ConflictResultSession).WriteConflictResult(context.Background(), request)
		if !errors.Is(err, ErrConflictResultChanged) {
			t.Fatalf("error = %v, want ErrConflictResultChanged", err)
		}
		if after := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(after, before) {
			t.Fatal("index mismatch wrote Result")
		}
	})

	t.Run("operation fingerprint changed", func(t *testing.T) {
		dir := fileRecoveryUURepository(t, false)
		session, _ := (Opener{}).Open(context.Background(), dir)
		request := reviewedConflictResultRequest(t, session, dir, "f.txt")
		before := fileRecoveryReadFile(t, dir, "f.txt")
		message := filepath.Join(dir, ".git", "MERGE_MSG")
		content, err := os.ReadFile(message)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(message, append(content, []byte("\nexternal operation edit\n")...), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = session.(ConflictResultSession).WriteConflictResult(context.Background(), request)
		if !errors.Is(err, ErrConflictResultChanged) {
			t.Fatalf("error = %v, want ErrConflictResultChanged", err)
		}
		if after := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(after, before) {
			t.Fatal("operation mismatch wrote Result")
		}
	})
}

func TestWriteConflictResultRejectsBinarySourceAndResult(t *testing.T) {
	dir := fileRecoveryUURepository(t, true)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	request := reviewedConflictResultRequest(t, session, dir, "f.bin")
	before := fileRecoveryReadFile(t, dir, "f.bin")
	_, err = session.(ConflictResultSession).WriteConflictResult(context.Background(), request)
	if !errors.Is(err, ErrConflictResultBinary) {
		t.Fatalf("binary source error = %v, want ErrConflictResultBinary", err)
	}
	if after := fileRecoveryReadFile(t, dir, "f.bin"); !bytes.Equal(after, before) {
		t.Fatal("binary conflict was modified")
	}

	textDir := fileRecoveryUURepository(t, false)
	textSession, _ := (Opener{}).Open(context.Background(), textDir)
	textRequest := reviewedConflictResultRequest(t, textSession, textDir, "f.txt")
	textRequest.Result = []byte{'x', 0, 'y'}
	textBefore := fileRecoveryReadFile(t, textDir, "f.txt")
	_, err = textSession.(ConflictResultSession).WriteConflictResult(context.Background(), textRequest)
	if !errors.Is(err, ErrConflictResultBinary) {
		t.Fatalf("binary Result error = %v, want ErrConflictResultBinary", err)
	}
	if after := fileRecoveryReadFile(t, textDir, "f.txt"); !bytes.Equal(after, textBefore) {
		t.Fatal("binary Result was written")
	}
}
