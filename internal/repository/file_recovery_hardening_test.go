package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const fileRecoveryGoldenID = "000102030405060708090a0b0c0d0e0f"

func fileRecoveryGolden(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "file_recovery_v2_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fileRecoveryGoldenRecord(t *testing.T) FileRecoveryRecord {
	t.Helper()
	var record FileRecoveryRecord
	if err := json.Unmarshal(fileRecoveryGolden(t), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func installFileRecoveryManifest(t *testing.T, session Session, id string, data []byte) string {
	t.Helper()
	name, err := session.(*gitSession).fileRecoveryManifestPath(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestFileRecoveryV2GoldenAndUnknownFieldContract(t *testing.T) {
	_, session, _ := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
	name := installFileRecoveryManifest(t, session, fileRecoveryGoldenID, fileRecoveryGolden(t))
	record, err := session.(*gitSession).readFileRecoveryRecord(fileRecoveryGoldenID, false)
	if err != nil {
		t.Fatalf("fixed V2 manifest no longer decodes: %v", err)
	}
	if record.Version != 2 || record.Path != "dir/f.txt" || record.Status != FileRecoveryReady ||
		record.Progress != FileRecoveryPending || record.Initial.Worktree.Mode != "100644" ||
		record.After.Worktree.Mode != "100755" || record.Initial.Index.Stage0.OID != "df967b96a579e45a18b8251732d16804b2e56a55" ||
		len(record.Journal) != 1 || record.Journal[0].Event != fileRecoveryEventArchived {
		t.Fatalf("V2 golden changed meaning: %#v", record)
	}
	if record.Initial.Worktree.Content != nil || record.After.Index.Stage0.Content != nil {
		t.Fatal("metadata read unexpectedly embedded payload bytes")
	}
	legacyRepository := "local\x00/old/repo/.git"
	legacyWorktree := legacyRepository + "\x00local\x00/old/repo"
	if record.Identity.RepositoryDigest != fileRecoveryDigest([]byte(legacyRepository)) ||
		record.Identity.WorktreeDigest != fileRecoveryDigest([]byte(legacyWorktree)) {
		t.Fatalf("legacy identity digest no longer matches its fixed paths: %#v", record.Identity)
	}

	unknown := map[string]string{
		"root":        "future_v2_field",
		"worktree":    "future_worktree_field",
		"index entry": "future_index_field",
		"identity":    "future_identity_field",
	}
	for field, message := range unknown {
		t.Run("unknown "+field, func(t *testing.T) {
			var replacement string
			switch field {
			case "root":
				replacement = "{\"" + message + "\":true,"
			case "worktree":
				replacement = "{\"" + message + "\":true,"
			case "index entry":
				replacement = "{\"" + message + "\":true,"
			case "identity":
				replacement = "{\"" + message + "\":true,"
			}
			var data []byte
			switch field {
			case "root":
				data = append([]byte(replacement), fileRecoveryGolden(t)[1:]...)
			case "worktree":
				data = bytes.Replace(fileRecoveryGolden(t), []byte("\"worktree\": {"), []byte("\"worktree\": "+replacement), 1)
			case "index entry":
				data = bytes.Replace(fileRecoveryGolden(t), []byte("\"stage0\": {"), []byte("\"stage0\": "+replacement), 1)
			case "identity":
				data = bytes.Replace(fileRecoveryGolden(t), []byte("\"identity\": {"), []byte("\"identity\": "+replacement), 1)
			}
			installFileRecoveryManifest(t, session, fileRecoveryGoldenID, data)
			broken, readErr := session.(*gitSession).readFileRecoveryRecord(fileRecoveryGoldenID, false)
			if readErr == nil || !strings.Contains(readErr.Error(), "unknown field") || broken.Status == FileRecoveryReady {
				t.Fatalf("unknown %s field was trusted: %#v, %v", field, broken, readErr)
			}
		})
	}

	t.Run("unknown fields never rewrite the old manifest", func(t *testing.T) {
		data := append([]byte("{\"future_v2_field\":true,"), fileRecoveryGolden(t)[1:]...)
		installFileRecoveryManifest(t, session, fileRecoveryGoldenID, data)
		_, _ = session.(*gitSession).readFileRecoveryRecord(fileRecoveryGoldenID, false)
		stored, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(stored, data) {
			t.Fatalf("read attempted schema repair: %v", err)
		}
	})
}

func TestReadFileRecoveryRecordRejectsMalformedInput(t *testing.T) {
	_, session, _ := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
	git := session.(*gitSession)
	golden := fileRecoveryGolden(t)
	cases := map[string][]byte{
		"empty":                   nil,
		"binary header":           append([]byte{0, 0xff, '{', '\n'}, golden...),
		"NUL after header":        append([]byte("{\x00"), golden[1:]...),
		"truncated first token":   golden[:1],
		"truncated middle":        golden[:len(golden)/2],
		"truncated final newline": golden[:len(golden)-2],
		"trailing object":         append(append([]byte(nil), golden...), []byte("{}\n")...),
		"trailing binary":         append(append([]byte(nil), golden...), 0, 0xff),
		"wrong version":           bytes.Replace(golden, []byte("\"version\": 2"), []byte("\"version\": 3"), 1),
		"mismatched operation":    bytes.Replace(golden, []byte(fileRecoveryGoldenID), []byte("100102030405060708090a0b0c0d0e0f"), 1),
		"negative size":           bytes.Replace(golden, []byte("\"size\": 5"), []byte("\"size\": -1"), 1),
		"short digest":            bytes.Replace(golden, []byte("\"size\": 5,\n      \"digest\": \"f34848ca92665c342abd5816c9e3eda0e82180671195362bcd0080544a3bc2ac\""), []byte("\"size\": 5,\n      \"digest\": \"00\""), 1),
		"wrong path digest":       bytes.Replace(golden, []byte("1f5bc7159a5c53e57e7238e3eba9295e8572363192acdd0a704b2bea37912b38"), []byte("0f5bc7159a5c53e57e7238e3eba9295e8572363192acdd0a704b2bea37912b38"), 1),
		"absent with size":        bytes.Replace(golden, []byte("\"present\": false,\n        \"size\": 0"), []byte("\"present\": false,\n        \"size\": 1"), 1),
		"malformed JSON string":   bytes.Replace(golden, []byte("\"path\": \"dir/f.txt\""), []byte("\"path\": \"unterminated"), 1),
		"wrong JSON field type":   bytes.Replace(golden, []byte("\"size\": 5"), []byte("\"size\": \"5\""), 1),
		"duplicate trailing JSON": append(append([]byte(nil), golden...), []byte("\nnull")...),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			installFileRecoveryManifest(t, session, fileRecoveryGoldenID, data)
			record, err := git.readFileRecoveryRecord(fileRecoveryGoldenID, false)
			if err == nil {
				t.Fatalf("malformed manifest became %q/%q: %#v", record.Status, record.Progress, record)
			}
			if record.Status == FileRecoveryReady {
				t.Fatal("malformed manifest was returned as ready")
			}
		})
	}
}

func emptyFileRecoveryStateForTest() FileRecoveryState {
	var state FileRecoveryState
	state.Index.Stage1.Stage = 1
	state.Index.Stage2.Stage = 2
	state.Index.Stage3.Stage = 3
	return state
}

func TestValidateFileRecoveryStatusProgressMatrix(t *testing.T) {
	statuses := []string{"", FileRecoveryReady, FileRecoveryRestoring, FileRecoveryInterrupted, FileRecoveryRestored, "future"}
	progresses := []string{"", FileRecoveryPending, FileRecoveryWorktreeRestored, FileRecoveryIndexRestored, "future"}
	for _, status := range statuses {
		for _, progress := range progresses {
			t.Run(status+"/"+progress, func(t *testing.T) {
				record := fileRecoveryGoldenRecord(t)
				// In particular, an absent after-state must not turn an advanced
				// progress value into an implicit Ready record.
				record.Initial = emptyFileRecoveryStateForTest()
				record.After = emptyFileRecoveryStateForTest()
				record.Status = status
				record.Progress = progress
				err := validateFileRecoveryRecord(record, fileRecoveryGoldenID)
				valid := (status == FileRecoveryReady && progress == FileRecoveryPending) ||
					((status == FileRecoveryRestoring || status == FileRecoveryInterrupted) &&
						(progress == FileRecoveryPending || progress == FileRecoveryWorktreeRestored || progress == FileRecoveryIndexRestored)) ||
					(status == FileRecoveryRestored && progress == FileRecoveryIndexRestored)
				if (err == nil) != valid {
					t.Fatalf("validate(%q, %q) error = %v, want valid = %v", status, progress, err, valid)
				}
				if err == nil && record.Status == FileRecoveryReady && record.Progress != FileRecoveryPending {
					t.Fatal("advanced absent state was trusted as ready")
				}
			})
		}
	}
}

func TestValidateFileRecoveryRecordRejectsBadFields(t *testing.T) {
	pathCases := []string{"", ".", "..", "../outside", "a/../../outside", "/absolute", `..\outside`, `a\b`, "a//b", "a/./b", "a/../b", "a/", "\x00", "bad\x00name"}
	for _, name := range pathCases {
		t.Run("path "+name, func(t *testing.T) {
			record := fileRecoveryGoldenRecord(t)
			record.Path = name
			record.PathDigest = fileRecoveryDigest([]byte(name))
			if err := validateFileRecoveryRecord(record, fileRecoveryGoldenID); err == nil {
				t.Fatalf("unsafe path %q validated as ready", name)
			}
		})
	}

	cases := map[string]func(*FileRecoveryRecord){
		"operation mismatch": func(r *FileRecoveryRecord) { r.OperationID = strings.Repeat("0", 32) },
		"invalid operation":  func(r *FileRecoveryRecord) { r.OperationID = "not-an-id" },
		"wrong version":      func(r *FileRecoveryRecord) { r.Version = 1 },
		"negative worktree size": func(r *FileRecoveryRecord) {
			r.Initial.Worktree.Size = -1
		},
		"invalid worktree digest": func(r *FileRecoveryRecord) {
			r.Initial.Worktree.Digest = strings.ToUpper(r.Initial.Worktree.Digest)
		},
		"absent worktree metadata": func(r *FileRecoveryRecord) {
			r.Initial.Worktree.Present = false
		},
		"unsupported worktree mode": func(r *FileRecoveryRecord) {
			r.Initial.Worktree.Mode = "120000"
		},
		"wrong index slot": func(r *FileRecoveryRecord) { r.Initial.Index.Stage1.Stage = 3 },
		"negative index size": func(r *FileRecoveryRecord) {
			r.Initial.Index.Stage0.Size = -1
		},
		"invalid index digest": func(r *FileRecoveryRecord) {
			r.Initial.Index.Stage0.Digest = "xyz"
		},
		"invalid index OID": func(r *FileRecoveryRecord) {
			r.Initial.Index.Stage0.OID = strings.ToUpper(r.Initial.Index.Stage0.OID)
		},
		"stage zero and conflict": func(r *FileRecoveryRecord) {
			r.Initial.Index.Stage1 = r.Initial.Index.Stage0
			r.Initial.Index.Stage1.Stage = 1
		},
		"absent index metadata": func(r *FileRecoveryRecord) {
			r.Initial.Index.Stage1 = r.Initial.Index.Stage0
			r.Initial.Index.Stage1.Stage = 1
			r.Initial.Index.Stage1.Present = false
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			record := fileRecoveryGoldenRecord(t)
			mutate(&record)
			if err := validateFileRecoveryRecord(record, fileRecoveryGoldenID); err == nil {
				t.Fatalf("broken record validated as %q: %#v", record.Status, record)
			}
		})
	}
}

func assertFileRecoveryJournalMonotonic(t *testing.T, record FileRecoveryRecord) {
	t.Helper()
	if len(record.Journal) == 0 {
		t.Fatal("operation has no durable journal")
	}
	if record.Attempts < 0 {
		t.Fatalf("negative attempt count: %#v", record)
	}
	var previous FileRecoveryJournalEntry
	for index, entry := range record.Journal {
		if entry.Event == "" || entry.Attempt < 0 || entry.Attempt > record.Attempts {
			t.Fatalf("illegal journal entry %d: %#v", index, entry)
		}
		if index > 0 && (entry.Time < previous.Time || entry.Attempt < previous.Attempt) {
			t.Fatalf("non-monotonic journal entries %#v then %#v", previous, entry)
		}
		previous = entry
	}
	if previous.Attempt != record.Attempts {
		t.Fatalf("journal attempt %d does not reach record attempt %d", previous.Attempt, record.Attempts)
	}
}

func TestFileRecoveryPayloadCorruptionFailsBeforeCASAndRetries(t *testing.T) {
	for _, corruption := range []string{"payload digest", "payload truncation", "manifest buffer digest"} {
		t.Run(corruption, func(t *testing.T) {
			dir, session, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
			workflow := recovery.(FileRecoveryWorkflowSession)
			ctx := context.Background()
			fileName := filepath.Join(dir, "f.txt")
			if err := os.WriteFile(fileName, []byte("original index\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", "--", "f.txt")
			if err := os.WriteFile(fileName, []byte("original worktree\x00bytes\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			initial, err := workflow.CaptureFileRecoveryState(ctx, "f.txt")
			if err != nil {
				t.Fatal(err)
			}
			record, err := recovery.ArchiveFileRecovery(ctx, FileRecoveryArchive{Path: "f.txt", After: initial})
			if err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(fileName, []byte("latest index\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", "--", "f.txt")
			latestWork := []byte("latest user work\x00keep me\n")
			if err := os.WriteFile(fileName, latestWork, 0o644); err != nil {
				t.Fatal(err)
			}
			after, err := workflow.CaptureFileRecoveryState(ctx, "f.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := workflow.RecordFileRecoveryAfter(ctx, record.OperationID, after); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(dir, "unrelated-latest.txt")
			if err := os.WriteFile(sentinel, []byte("unrelated latest user work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			beforeIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
			payloadName, err := session.(*gitSession).fileRecoveryPayloadPath(record.Initial.Worktree.Digest)
			if err != nil {
				t.Fatal(err)
			}
			var stored FileRecoveryRecord
			switch corruption {
			case "payload digest":
				tampered := bytes.Repeat([]byte{0x5a}, len(initial.Worktree.Content))
				if err := os.WriteFile(payloadName, tampered, 0o600); err != nil {
					t.Fatal(err)
				}
			case "payload truncation":
				if err := os.WriteFile(payloadName, []byte{0}, 0o600); err != nil {
					t.Fatal(err)
				}
			case "manifest buffer digest":
				stored, err = recovery.ReadFileRecovery(ctx, record.OperationID)
				if err != nil {
					t.Fatal(err)
				}
				stored.Initial.Worktree.Digest = fileRecoveryDigest([]byte("different buffer"))
				if err := session.(*gitSession).writeFileRecoveryRecord(stored); err != nil {
					t.Fatal(err)
				}
			}

			err = recovery.RestoreFileRecovery(ctx, record.OperationID)
			if err == nil {
				t.Fatal("corrupt recovery payload restored successfully")
			}
			if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, latestWork) {
				t.Fatalf("failed payload validation overwrote latest work: %q", got)
			}
			if got := fileRecoveryIndexBytes(t, dir, "f.txt"); !bytes.Equal(got, beforeIndex) {
				t.Fatal("failed payload validation changed the index")
			}
			interrupted, readErr := recovery.ReadFileRecovery(ctx, record.OperationID)
			if readErr != nil || interrupted.Status != FileRecoveryInterrupted || interrupted.Progress != FileRecoveryPending {
				t.Fatalf("corrupt payload state = %#v, %v", interrupted, readErr)
			}
			assertFileRecoveryJournalMonotonic(t, interrupted)

			if corruption == "manifest buffer digest" {
				interrupted.Initial.Worktree.Digest = record.Initial.Worktree.Digest
				if err := session.(*gitSession).writeFileRecoveryRecord(interrupted); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(payloadName, initial.Worktree.Content, 0o600); err != nil {
				t.Fatal(err)
			}
			// No timer or reopen automatically retries: only this explicit call
			// may consume the repaired content-addressed payload.
			if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, latestWork) {
				t.Fatal("repairing a payload mutated the worktree before explicit retry")
			}
			if err := recovery.RestoreFileRecovery(ctx, record.OperationID); err != nil {
				t.Fatalf("explicit retry after payload repair: %v", err)
			}
			if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, initial.Worktree.Content) {
				t.Fatalf("restored worktree = %q, want %q", got, initial.Worktree.Content)
			}
			for _, entry := range initial.Index.entries() {
				if !entry.Present {
					continue
				}
				expected := fileRecoveryGitBytes(t, dir, "cat-file", "blob", entry.OID)
				if !bytes.Equal(expected, entry.Content) {
					t.Fatalf("restored stage %d blob does not match initial payload", entry.Stage)
				}
			}
			captured, err := workflow.CaptureFileRecoveryState(ctx, "f.txt")
			if err != nil || !fileRecoveryStateEqual(captured, initial) {
				t.Fatalf("final state differs from initial: %#v, %v", captured, err)
			}
			if got := fileRecoveryReadFile(t, dir, "unrelated-latest.txt"); string(got) != "unrelated latest user work\n" {
				t.Fatalf("unrelated user work changed: %q", got)
			}
			completed, err := recovery.ReadFileRecovery(ctx, record.OperationID)
			if err != nil || completed.Status != FileRecoveryRestored || completed.Progress != FileRecoveryIndexRestored {
				t.Fatalf("retry did not complete: %#v, %v", completed, err)
			}
			assertFileRecoveryJournalMonotonic(t, completed)
		})
	}
}

func TestFileRecoveryCancelAndIndexCrashRequireExplicitRetry(t *testing.T) {
	for _, failure := range []string{"cancel", "index process killed"} {
		t.Run(failure, func(t *testing.T) {
			dir, session, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
			ctx := context.Background()
			if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("initial index\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", "--", "f.txt")
			initialWork := []byte("initial staged and unstaged\x00\n")
			if err := os.WriteFile(filepath.Join(dir, "f.txt"), initialWork, 0o644); err != nil {
				t.Fatal(err)
			}
			initial, err := session.(FileRecoveryWorkflowSession).CaptureFileRecoveryState(ctx, "f.txt")
			if err != nil {
				t.Fatal(err)
			}
			record, err := recovery.ArchiveFileRecovery(ctx, FileRecoveryArchive{
				Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
			})
			if err != nil {
				t.Fatal(err)
			}
			fileRecoveryRestoreHEAD(t, dir, "f.txt")

			operationCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			crash := errors.New("simulated index process kill")
			hooks := &fileRecoveryRestoreHooks{}
			if failure == "cancel" {
				hooks.beforeIndex = func() error {
					cancel()
					return operationCtx.Err()
				}
			} else {
				hooks.afterIndex = func() error { return crash }
			}
			_, err = session.(*gitSession).restoreFileRecovery(operationCtx, record.OperationID, hooks)
			if failure == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error = %v", err)
			}
			if failure != "cancel" && !errors.Is(err, crash) {
				t.Fatalf("crash error = %v", err)
			}
			stored, err := recovery.ReadFileRecovery(ctx, record.OperationID)
			if err != nil || stored.Status != FileRecoveryInterrupted || stored.Progress != FileRecoveryWorktreeRestored {
				t.Fatalf("interrupted progress = %#v, %v", stored, err)
			}
			assertFileRecoveryJournalMonotonic(t, stored)
			if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, initialWork) {
				t.Fatalf("durable file step was not exact: %q", got)
			}

			// Model the manifest left by a real process death after update-index.
			// A reopen exposes Interrupted but must not advance it without a call.
			stored.Status = FileRecoveryRestoring
			stored.Error = ""
			if last := len(stored.Journal) - 1; last >= 0 && stored.Journal[last].Event == fileRecoveryEventInterrupted {
				stored.Journal = stored.Journal[:last]
			}
			if err := session.(*gitSession).writeFileRecoveryRecord(stored); err != nil {
				t.Fatal(err)
			}
			reopened, err := openFullRepositorySessionForTest((Opener{}).Open(ctx, dir))
			if err != nil {
				t.Fatal(err)
			}
			beforeRetry := fileRecoveryIndexBytes(t, dir, "f.txt")
			passive, err := reopened.(FileRecoverySession).ReadFileRecovery(ctx, record.OperationID)
			if err != nil || passive.Status != FileRecoveryInterrupted {
				t.Fatalf("stale restoring = %#v, %v", passive, err)
			}
			if afterRead := fileRecoveryIndexBytes(t, dir, "f.txt"); !bytes.Equal(afterRead, beforeRetry) {
				t.Fatal("reopen passively replayed the index step")
			}
			if err := reopened.(FileRecoverySession).RestoreFileRecovery(ctx, record.OperationID); err != nil {
				t.Fatal(err)
			}
			captured, err := reopened.(FileRecoveryWorkflowSession).CaptureFileRecoveryState(ctx, "f.txt")
			if err != nil || !fileRecoveryStateEqual(captured, initial) {
				t.Fatalf("retry final state differs from initial: %#v, %v", captured, err)
			}
			completed, err := reopened.(FileRecoverySession).ReadFileRecovery(ctx, record.OperationID)
			if err != nil || completed.Status != FileRecoveryRestored || completed.Progress != FileRecoveryIndexRestored {
				t.Fatalf("completed retry = %#v, %v", completed, err)
			}
			if failure != "cancel" && completed.Journal[len(completed.Journal)-1].Event != fileRecoveryEventIndexVerified {
				t.Fatalf("index crash retry repeated a completed Git write: %#v", completed.Journal)
			}
			assertFileRecoveryJournalMonotonic(t, completed)
		})
	}
}

func TestFileRecoveryPathTamperAndSymlinkParentsCannotEscape(t *testing.T) {
	for _, unsafePath := range []string{"../outside", "/absolute", `..\outside`} {
		t.Run("manifest "+unsafePath, func(t *testing.T) {
			dir, session, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
			record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
				Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
			})
			if err != nil {
				t.Fatal(err)
			}
			fileRecoveryRestoreHEAD(t, dir, "f.txt")
			beforeWork := fileRecoveryReadFile(t, dir, "f.txt")
			beforeIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
			outside := filepath.Join(filepath.Dir(dir), "outside")
			if err := os.WriteFile(outside, []byte("outside user work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			stored, err := recovery.ReadFileRecovery(context.Background(), record.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			stored.Path = unsafePath
			stored.PathDigest = fileRecoveryDigest([]byte(unsafePath))
			if err := session.(*gitSession).writeFileRecoveryRecord(stored); err != nil {
				t.Fatal(err)
			}
			if err := recovery.RestoreFileRecovery(context.Background(), record.OperationID); err == nil {
				t.Fatalf("unsafe manifest path %q restored", unsafePath)
			}
			if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, beforeWork) ||
				!bytes.Equal(fileRecoveryIndexBytes(t, dir, "f.txt"), beforeIndex) {
				t.Fatal("path rejection mutated the saved CAS state")
			}
			if got, err := os.ReadFile(outside); err != nil || string(got) != "outside user work\n" {
				t.Fatalf("outside sentinel changed: %q, %v", got, err)
			}
		})
	}

	t.Run("existing symlink parent", func(t *testing.T) {
		dir, _, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
		outside := t.TempDir()
		target := filepath.Join(outside, "victim.txt")
		if err := os.WriteFile(target, []byte("outside initial\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep outside\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{Path: "escape/victim.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		if err := recovery.RestoreFileRecovery(context.Background(), record.OperationID); !errors.Is(err, ErrFileRecoveryUnsupported) {
			t.Fatalf("symlink parent restore error = %v", err)
		}
		if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("restore wrote through symlink parent: %v", err)
		}
		if got, err := os.ReadFile(filepath.Join(outside, "keep.txt")); err != nil || string(got) != "keep outside\n" {
			t.Fatalf("outside sibling changed: %q, %v", got, err)
		}
	})
}

func applyFileRecoveryStateForTest(t *testing.T, session Session, relativePath string, state FileRecoveryState) {
	t.Helper()
	git := session.(*gitSession)
	if err := git.ensureFileRecoveryIndexBlobs(state.Index); err != nil {
		t.Fatal(err)
	}
	if err := git.recoverFileRecoveryIndex(relativePath, state.Index); err != nil {
		t.Fatal(err)
	}
	if err := git.writeFileRecoveryWorktree(relativePath, state.Worktree); err != nil {
		t.Fatal(err)
	}
}

func openFileRecoveryWorkflowForTest(t *testing.T, dir string) (FullSession, FileRecoveryWorkflowSession) {
	t.Helper()
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	return session, session.(FileRecoveryWorkflowSession)
}

func TestFileRecoveryConcurrentSessionsPreserveUpdatesAndRestoreCAS(t *testing.T) {
	dir, first, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
	ctx := context.Background()
	firstWorkflow := recovery.(FileRecoveryWorkflowSession)
	fileName := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(fileName, []byte("original index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", "f.txt")
	originalWork := []byte("original worktree\x00\n")
	if err := os.WriteFile(fileName, originalWork, 0o644); err != nil {
		t.Fatal(err)
	}
	initial, err := firstWorkflow.CaptureFileRecoveryState(ctx, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	record, err := recovery.ArchiveFileRecovery(ctx, FileRecoveryArchive{Path: "f.txt", After: initial})
	if err != nil {
		t.Fatal(err)
	}
	originalIndex := fileRecoveryIndexBytes(t, dir, "f.txt")

	const updateCount = 12
	candidates := make([]FileRecoveryState, updateCount)
	for index := range candidates {
		if err := os.WriteFile(fileName, []byte(fmt.Sprintf("resolution %02d index\n", index)), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "--", "f.txt")
		if err := os.WriteFile(fileName, []byte(fmt.Sprintf("resolution %02d worktree\x00\n", index)), 0o644); err != nil {
			t.Fatal(err)
		}
		candidate, err := firstWorkflow.CaptureFileRecoveryState(ctx, "f.txt")
		if err != nil {
			t.Fatal(err)
		}
		candidates[index] = candidate
	}
	latestWork := fileRecoveryReadFile(t, dir, "f.txt")
	latestIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
	_, second := openFileRecoveryWorkflowForTest(t, dir)
	_, third := openFileRecoveryWorkflowForTest(t, dir)

	type updateResult struct {
		record FileRecoveryRecord
		err    error
	}
	results := make([]updateResult, updateCount)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range candidates {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			workflow := []FileRecoveryWorkflowSession{firstWorkflow, second, third}[index%3]
			results[index].record, results[index].err = workflow.RecordFileRecoveryAfter(ctx, record.OperationID, candidates[index])
		}()
	}
	close(start)
	wait.Wait()
	for index, result := range results {
		if result.err != nil {
			t.Fatalf("concurrent update %d: %v", index, result.err)
		}
		if !fileRecoveryStateEqual(result.record.After, candidates[index]) || result.record.Status != FileRecoveryReady ||
			result.record.Progress != FileRecoveryPending || len(result.record.Journal) < 2 {
			t.Fatalf("update %d returned another operation's state: %#v", index, result.record)
		}
		assertFileRecoveryJournalMonotonic(t, result.record)
	}
	if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, latestWork) ||
		!bytes.Equal(fileRecoveryIndexBytes(t, dir, "f.txt"), latestIndex) {
		t.Fatal("RecordFileRecoveryAfter mutated the user's current worktree or index")
	}

	updated, err := recovery.ReadFileRecovery(ctx, record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Journal) != updateCount+1 {
		t.Fatalf("concurrent updates lost journal entries: got %d, want %d", len(updated.Journal), updateCount+1)
	}
	for _, entry := range updated.Journal[1:] {
		if entry.Event != fileRecoveryEventAfterRecorded || entry.Attempt != 0 {
			t.Fatalf("unexpected update journal entry: %#v", entry)
		}
	}
	assertFileRecoveryJournalMonotonic(t, updated)
	selected := -1
	for index := range candidates {
		if fileRecoveryStateEqual(updated.After, candidates[index]) {
			selected = index
			break
		}
	}
	if selected < 0 {
		t.Fatal("last CAS record contains a mixture of different updates")
	}
	applyFileRecoveryStateForTest(t, first, "f.txt", candidates[selected])
	actualAfter, err := firstWorkflow.CaptureFileRecoveryState(ctx, "f.txt")
	if err != nil || !fileRecoveryStateEqual(actualAfter, updated.After) {
		t.Fatalf("could not apply the last linearized after-state: %v", err)
	}

	_, secondRestore := openFileRecoveryWorkflowForTest(t, dir)
	_, thirdRestore := openFileRecoveryWorkflowForTest(t, dir)
	restoreSessions := []FileRecoverySession{recovery, secondRestore, thirdRestore}
	restoreErrors := make([]error, len(restoreSessions))
	start = make(chan struct{})
	for index, restoreSession := range restoreSessions {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			restoreErrors[index] = restoreSession.RestoreFileRecovery(ctx, record.OperationID)
		}()
	}
	close(start)
	wait.Wait()
	for index, err := range restoreErrors {
		if err != nil {
			t.Fatalf("concurrent restore %d: %v", index, err)
		}
	}
	if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, originalWork) ||
		!bytes.Equal(fileRecoveryIndexBytes(t, dir, "f.txt"), originalIndex) {
		t.Fatal("concurrent restores did not converge on exact Initial state")
	}
	completed, err := recovery.ReadFileRecovery(ctx, record.OperationID)
	if err != nil || completed.Attempts != 1 || completed.Status != FileRecoveryRestored || completed.Progress != FileRecoveryIndexRestored {
		t.Fatalf("concurrent restore repeated or lost progress: %#v, %v", completed, err)
	}
	starts := 0
	for _, entry := range completed.Journal {
		if entry.Event == fileRecoveryEventRestoreStarted || entry.Event == fileRecoveryEventRetryStarted {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("three sessions produced %d restore starts, want one", starts)
	}
	assertFileRecoveryJournalMonotonic(t, completed)
}

func TestFileRecoveryConcurrentRecordAfterCannotCrossRestoreCAS(t *testing.T) {
	dir, first, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
	ctx := context.Background()
	workflow := recovery.(FileRecoveryWorkflowSession)
	fileName := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(fileName, []byte("original index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", "f.txt")
	if err := os.WriteFile(fileName, []byte("original worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initial, err := workflow.CaptureFileRecoveryState(ctx, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	record, err := recovery.ArchiveFileRecovery(ctx, FileRecoveryArchive{Path: "f.txt", After: initial})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileName, []byte("recorded after index\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", "f.txt")
	if err := os.WriteFile(fileName, []byte("recorded after worktree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := workflow.CaptureFileRecoveryState(ctx, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.RecordFileRecoveryAfter(ctx, record.OperationID, after); err != nil {
		t.Fatal(err)
	}
	latestWork := []byte("newest concurrent user work\x00\n")
	if err := os.WriteFile(fileName, latestWork, 0o644); err != nil {
		t.Fatal(err)
	}
	latest, err := workflow.CaptureFileRecoveryState(ctx, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	applyFileRecoveryStateForTest(t, first, "f.txt", after)
	beforeIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
	_, second := openFileRecoveryWorkflowForTest(t, dir)

	entered := make(chan struct{})
	release := make(chan struct{})
	hooks := &fileRecoveryRestoreHooks{beforeIndex: func() error {
		if err := os.WriteFile(fileName, latestWork, 0o644); err != nil {
			return fmt.Errorf("inject latest user work: %w", err)
		}
		close(entered)
		<-release
		return nil
	}}
	restoreResult := make(chan error, 1)
	go func() {
		_, restoreErr := first.(*gitSession).restoreFileRecovery(ctx, record.OperationID, hooks)
		restoreResult <- restoreErr
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("restore did not reach its real index fault point")
	}
	updateStarted := make(chan struct{})
	updateResult := make(chan error, 1)
	go func() {
		close(updateStarted)
		_, updateErr := second.RecordFileRecoveryAfter(ctx, record.OperationID, latest)
		updateResult <- updateErr
	}()
	<-updateStarted
	close(release)
	if err := <-restoreResult; !errors.Is(err, ErrFileRecoveryAfterStateChanged) {
		t.Fatalf("restore did not refuse newer concurrent work: %v", err)
	}
	if err := <-updateResult; err == nil {
		t.Fatal("RecordAfter crossed an in-progress restore")
	}
	if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, latestWork) ||
		!bytes.Equal(fileRecoveryIndexBytes(t, dir, "f.txt"), beforeIndex) {
		t.Fatal("concurrent CAS overwrote newest work or changed its index")
	}
	stored, err := recovery.ReadFileRecovery(ctx, record.OperationID)
	if err != nil || stored.Status != FileRecoveryInterrupted || stored.Progress != FileRecoveryWorktreeRestored {
		t.Fatalf("concurrent CAS journal state = %#v, %v", stored, err)
	}
	afterRecords := 0
	for _, entry := range stored.Journal {
		if entry.Event == fileRecoveryEventAfterRecorded {
			afterRecords++
		}
	}
	if afterRecords != 1 {
		t.Fatalf("an update was lost into or injected across restore: %#v", stored.Journal)
	}
	assertFileRecoveryJournalMonotonic(t, stored)
}

func TestDetailsSnapshotLogsDegradedFileRecoveryMetadata(t *testing.T) {
	dir, session, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
	record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
		Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	progressive := session.(ProgressiveSession)
	details, err := progressive.DetailsSnapshot(context.Background())
	if err != nil || len(details.FileRecoveries) != 1 {
		t.Fatalf("valid recovery metadata = %#v, %v", details.FileRecoveries, err)
	}

	manifest, err := session.(*gitSession).fileRecoveryManifestPath(record.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	// Unknown fields are deliberately not part of the V2 compatibility
	// contract. Neither this record nor a stale partial list may become a
	// restore/retry action when discovery fails.
	data = append([]byte("{\"future_v2_field\":true,"), data[1:]...)
	if err := os.WriteFile(manifest, data, 0o600); err != nil {
		t.Fatal(err)
	}
	session.CommandLog().Clear()
	details, err = progressive.DetailsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("degraded optional recovery metadata failed the snapshot: %v", err)
	}
	if len(details.FileRecoveries) != 0 {
		t.Fatalf("broken recovery discovery published actions: %#v", details.FileRecoveries)
	}
	found := false
	for _, entry := range session.CommandLog().Entries() {
		if entry.Failed && strings.Contains(strings.ToLower(entry.Command), "file recovery") &&
			strings.Contains(entry.Output, record.OperationID) && strings.Contains(entry.Output, "future_v2_field") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("recovery metadata error was not logged: %#v", session.CommandLog().Entries())
	}
}
