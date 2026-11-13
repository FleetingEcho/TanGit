package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func runGitForRecoveryFuzz(f *testing.F, dir string, args ...string) {
	f.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		f.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
}

func fileRecoveryFuzzSession(f *testing.F) (*gitSession, string) {
	f.Helper()
	dir, err := os.MkdirTemp("", "tangit-file-recovery-fuzz-")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = os.RemoveAll(dir) })
	runGitForRecoveryFuzz(f, dir, "init", "--initial-branch=main")
	runGitForRecoveryFuzz(f, dir, "config", "user.email", "desktop@example.com")
	runGitForRecoveryFuzz(f, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644); err != nil {
		f.Fatal(err)
	}
	runGitForRecoveryFuzz(f, dir, "add", "--", "f.txt")
	runGitForRecoveryFuzz(f, dir, "commit", "-m", "base")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		f.Fatal(err)
	}
	return session.(*gitSession), dir
}

func readGoldenForRecoveryFuzz(f *testing.F) []byte {
	f.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "file_recovery_v2_golden.json"))
	if err != nil {
		f.Fatal(err)
	}
	return data
}

func FuzzReadFileRecoveryRecord(f *testing.F) {
	git, dir := fileRecoveryFuzzSession(f)
	manifest, err := git.fileRecoveryManifestPath(fileRecoveryGoldenID)
	if err != nil {
		f.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(manifest), 0o700); err != nil {
		f.Fatal(err)
	}
	sentinel := filepath.Join(filepath.Dir(dir), "file-recovery-fuzz-outside-"+filepath.Base(dir))
	if err := os.WriteFile(sentinel, []byte("outside user work\x00\n"), 0o600); err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { _ = os.Remove(sentinel) })
	golden := readGoldenForRecoveryFuzz(f)
	var identifiedGolden FileRecoveryRecord
	if err := json.Unmarshal(golden, &identifiedGolden); err != nil {
		f.Fatal(err)
	}
	identifiedGolden.Identity = git.fileRecoveryIdentity()
	identifiedData, err := json.MarshalIndent(identifiedGolden, "", "  ")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(append(identifiedData, '\n'))
	f.Add(golden)
	f.Add([]byte("{}"))
	f.Add([]byte{0, 0xff, '{', '}', '\n'})
	f.Add(golden[:len(golden)/2])
	f.Add(bytes.Replace(golden, []byte("\"version\": 2"), []byte("\"version\": 3"), 1))
	f.Add(bytes.Replace(golden, []byte("\"path\": \"dir/f.txt\""), []byte("\"path\": \"../outside\""), 1))
	f.Add(append([]byte("{\"future_v2_field\":true,"), golden[1:]...))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Keep disk use bounded without imposing a production schema limit.
		if len(data) > 4<<20 {
			t.Skip("fuzz input exceeds the per-iteration disk budget")
		}
		if err := os.WriteFile(manifest, data, 0o600); err != nil {
			t.Fatal(err)
		}
		record, readErr := git.readFileRecoveryRecord(fileRecoveryGoldenID, false)
		identified, identifyErr := git.readFileRecoveryRecord(fileRecoveryGoldenID, true)
		if readErr == nil && record.Identity == git.fileRecoveryIdentity() {
			if identifyErr != nil || identified.OperationID != record.OperationID || identified.Status != record.Status || identified.Progress != record.Progress {
				t.Fatalf("matching identity changed a valid record: %#v, %v; metadata: %#v", identified, identifyErr, record)
			}
			listed, listErr := git.ListFileRecovery(context.Background())
			if listErr != nil || len(listed) != 1 || listed[0].OperationID != fileRecoveryGoldenID {
				t.Fatalf("valid identified manifest disappeared from discovery: %#v, %v", listed, listErr)
			}
			if listed[0].Status == FileRecoveryReady && listed[0].Progress != FileRecoveryPending {
				t.Fatalf("discovery exposed an illegal ready record: %#v", listed[0])
			}
		} else {
			if identifyErr == nil {
				t.Fatalf("malformed or cross-repository record passed identity validation: %#v", identified)
			}
			listed, listErr := git.ListFileRecovery(context.Background())
			if listErr == nil {
				t.Fatalf("broken manifest produced discovery metadata: %#v", listed)
			}
		}
		if identifyErr != nil && identified.Status == FileRecoveryReady {
			t.Fatalf("identity validation returned a broken ready record: %#v", identified)
		}
		if readErr == nil {
			if err := validateFileRecoveryRecord(record, fileRecoveryGoldenID); err != nil {
				t.Fatalf("reader returned an invalid record: %v\nrecord: %#v", err, record)
			}
			if record.Version != FileRecoveryManifestVersion || record.OperationID != fileRecoveryGoldenID {
				t.Fatalf("reader changed manifest identity: %#v", record)
			}
			if record.Status == FileRecoveryReady && record.Progress != FileRecoveryPending {
				t.Fatalf("advanced progress was exposed as ready: %#v", record)
			}
			if record.Status == FileRecoveryRestored && record.Progress != FileRecoveryIndexRestored {
				t.Fatalf("incomplete progress was exposed as restored: %#v", record)
			}
			if err := validateFileRecoveryPath(record.Path); err != nil {
				t.Fatalf("reader returned an unsafe path %q: %v", record.Path, err)
			}
			if record.PathDigest != fileRecoveryDigest([]byte(record.Path)) || !fileRecoveryValidDigest(record.PathDigest) {
				t.Fatalf("reader trusted a tampered path digest: %#v", record)
			}
		} else if record.Status == FileRecoveryReady {
			t.Fatalf("broken record was returned as ready: %#v", record)
		}
		stored, err := os.ReadFile(manifest)
		if err != nil || !bytes.Equal(stored, data) {
			t.Fatalf("read silently repaired or rewrote a manifest: %v", err)
		}
		outside, err := os.ReadFile(sentinel)
		if err != nil || !bytes.Equal(outside, []byte("outside user work\x00\n")) {
			t.Fatalf("manifest read escaped its root: %q, %v", outside, err)
		}
	})
}

func FuzzValidateFileRecoveryRecord(f *testing.F) {
	golden := readGoldenForRecoveryFuzz(f)
	var baseline FileRecoveryRecord
	if err := json.Unmarshal(golden, &baseline); err != nil {
		f.Fatal(err)
	}
	digest := baseline.Initial.Worktree.Digest
	f.Add(2, fileRecoveryGoldenID, "dir/f.txt", baseline.PathDigest,
		FileRecoveryReady, FileRecoveryPending, int64(5), digest, true)
	f.Add(2, fileRecoveryGoldenID, "dir/f.txt", baseline.PathDigest,
		FileRecoveryRestored, FileRecoveryIndexRestored, int64(0), "", false)
	f.Add(3, fileRecoveryGoldenID, "../outside", baseline.PathDigest,
		FileRecoveryReady, FileRecoveryIndexRestored, int64(-1), "00", true)

	f.Fuzz(func(t *testing.T, version int, operationID, name, pathDigest, status, progress string,
		size int64, worktreeDigest string, worktreePresent bool) {
		record := baseline
		record.Version = version
		record.OperationID = operationID
		record.Path = name
		record.PathDigest = pathDigest
		record.Status = status
		record.Progress = progress
		record.Initial.Worktree.Present = worktreePresent
		record.Initial.Worktree.Mode = "100644"
		if !worktreePresent {
			record.Initial.Worktree.Mode = ""
		}
		record.Initial.Worktree.Size = size
		record.Initial.Worktree.Digest = worktreeDigest
		if err := validateFileRecoveryRecord(record, fileRecoveryGoldenID); err != nil {
			return
		}
		if record.Version != FileRecoveryManifestVersion || !validOperationID(operationID) {
			t.Fatalf("unsupported manifest identity passed validation: %#v", record)
		}
		if err := validateFileRecoveryPath(record.Path); err != nil {
			t.Fatalf("unsafe path passed validation: %q: %v", record.Path, err)
		}
		if record.PathDigest != fileRecoveryDigest([]byte(record.Path)) || !fileRecoveryValidDigest(record.PathDigest) {
			t.Fatalf("tampered path identity passed validation: %#v", record)
		}
		if record.Status == FileRecoveryReady && record.Progress != FileRecoveryPending {
			t.Fatalf("illegal transition passed as ready: %#v", record)
		}
		if record.Status == FileRecoveryRestored && record.Progress != FileRecoveryIndexRestored {
			t.Fatalf("incomplete restored state passed validation: %#v", record)
		}
		if worktreePresent {
			if size < 0 || !fileRecoveryValidDigest(worktreeDigest) {
				t.Fatalf("bad present worktree metadata passed validation: %#v", record.Initial.Worktree)
			}
		} else if size != 0 || worktreeDigest != "" || record.Initial.Worktree.Mode != "" {
			t.Fatalf("absent entry acquired present-file metadata: %#v", record.Initial.Worktree)
		}
	})
}

func FuzzFileRecoveryPath(f *testing.F) {
	for _, seed := range []string{"f.txt", "dir/f.txt", "../outside", "/absolute", `..\outside`, "a/../../outside", "a//b", "", "\x00"} {
		f.Add(seed)
	}
	root := filepath.Join(string(os.PathSeparator), "file-recovery-fuzz-root")
	f.Fuzz(func(t *testing.T, name string) {
		if err := validateFileRecoveryPath(name); err != nil {
			return
		}
		if name == "" || path.Clean(name) != name || path.IsAbs(name) || strings.ContainsRune(name, '\x00') {
			t.Fatalf("non-canonical path passed validation: %q", name)
		}
		joined := filepath.Join(root, filepath.FromSlash(name))
		relative, err := filepath.Rel(root, joined)
		if err != nil {
			t.Fatalf("compute containment for %q: %v", name, err)
		}
		inside := joined == root || strings.HasPrefix(joined, root+string(os.PathSeparator))
		if !inside || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
			t.Fatalf("validated path %q escapes %q as %q (relative %q)", name, root, joined, relative)
		}
	})
}
