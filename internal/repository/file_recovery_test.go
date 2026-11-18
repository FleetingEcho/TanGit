package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fileRecoveryRepository(t *testing.T, name string, base []byte) (string, Session, FileRecoverySession) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	if err := os.WriteFile(filepath.Join(dir, name), base, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--", name)
	runGit(t, dir, "commit", "-m", "base")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	recovery, ok := session.(FileRecoverySession)
	if !ok {
		t.Fatal("real session does not implement FileRecoverySession")
	}
	return dir, session, recovery
}

func fileRecoveryGitBytes(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %q", strings.Join(args, " "), err, output)
	}
	return output
}

// fileRecoveryAfterHEAD describes what restore --source=HEAD --staged
// --worktree produces for one ordinary file, using Git itself for mode/OID data.
func fileRecoveryAfterHEAD(t *testing.T, dir, name string) FileRecoveryState {
	t.Helper()
	listing := fileRecoveryGitBytes(t, dir, "--literal-pathspecs", "ls-tree", "-z", "HEAD", "--", name)
	after := FileRecoveryState{}
	if len(listing) == 0 {
		return after
	}
	metadata, path, ok := strings.Cut(strings.TrimSuffix(string(listing), "\x00"), "\t")
	if !ok || path != name {
		t.Fatalf("unexpected HEAD tree entry %q", listing)
	}
	var mode, kind, oid string
	if _, err := fmt.Sscanf(metadata, "%s %s %s", &mode, &kind, &oid); err != nil || kind != "blob" {
		t.Fatalf("unexpected HEAD metadata %q: %v", metadata, err)
	}
	entry := FileRecoveryIndexEntry{Present: true, Mode: mode, OID: oid}
	after.Index.Stage0 = entry
	after.Worktree = FileRecoveryWorktree{
		Present: entry.Present, Mode: mode,
		Content: fileRecoveryGitBytes(t, dir, "show", "HEAD:"+name),
	}
	return after
}

func fileRecoveryRestoreHEAD(t *testing.T, dir, name string) {
	t.Helper()
	runGit(t, dir, "restore", "--source=HEAD", "--staged", "--worktree", "--", name)
}

func fileRecoveryIndexBytes(t *testing.T, dir, name string) []byte {
	t.Helper()
	return fileRecoveryGitBytes(t, dir, "--literal-pathspecs", "ls-files", "--stage", "-z", "--", name)
}

func fileRecoveryReadFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestFileRecoveryRestoresStagedAndUnstagedExecutableFile(t *testing.T) {
	for _, unstaged := range []bool{false, true} {
		name := "staged"
		if unstaged {
			name = "unstaged"
		}
		t.Run(name, func(t *testing.T) {
			dir, session, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
			fileName := filepath.Join(dir, "f.txt")
			staged := []byte("staged\x00binary\n")
			working := staged
			if unstaged {
				working = []byte("working\x00bytes\nmore")
			}
			if err := os.WriteFile(fileName, staged, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(fileName, 0o755); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", "--", "f.txt")
			if err := os.WriteFile(fileName, working, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(fileName, 0o755); err != nil {
				t.Fatal(err)
			}
			beforeIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
			record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
				Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if record.Version != FileRecoveryManifestVersion || !validOperationID(record.OperationID) {
				t.Fatalf("invalid V2 identity: %#v", record)
			}
			if record.Initial.Worktree.Digest != fileRecoveryDigest(working) ||
				record.Initial.Worktree.Mode != "100755" || record.Initial.Index.Stage0.Mode != "100755" {
				t.Fatalf("initial bytes/mode were not captured: %#v", record.Initial)
			}
			payload := filepath.Join(dir, ".git", "tangit", "file-recovery", "v2", "payload", record.Initial.Worktree.Digest+".bin")
			if content, err := os.ReadFile(payload); err != nil || !bytes.Equal(content, working) {
				t.Fatalf("raw payload = %q, %v; want %q", content, err, working)
			}
			manifest := filepath.Join(dir, ".git", "tangit", "file-recovery", "v2", "manifest", record.OperationID+".json")
			if _, err := os.Stat(manifest); err != nil {
				t.Fatal(err)
			}
			if snapshots, err := session.WorkSnapshots(context.Background()); err != nil || len(snapshots) != 0 {
				t.Fatalf("V2 polluted V1 snapshots: %#v, %v", snapshots, err)
			}
			if operations, err := session.Operations(context.Background()); err != nil || len(operations) != 0 {
				t.Fatalf("V2 polluted V1 operations: %#v, %v", operations, err)
			}

			fileRecoveryRestoreHEAD(t, dir, "f.txt")
			if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("new independent work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := recovery.RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
				t.Fatal(err)
			}
			if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, working) {
				t.Fatalf("restored worktree = %q, want %q", got, working)
			}
			if got := fileRecoveryIndexBytes(t, dir, "f.txt"); !bytes.Equal(got, beforeIndex) {
				t.Fatalf("restored index = %q, want %q", got, beforeIndex)
			}
			if info, err := os.Stat(fileName); err != nil || info.Mode().Perm()&0o111 == 0 {
				t.Fatalf("executable bit was not restored: %v, %v", info, err)
			}
			if got := string(fileRecoveryReadFile(t, dir, "unrelated.txt")); got != "new independent work\n" {
				t.Fatalf("unrelated path changed: %q", got)
			}
			stored, err := recovery.ReadFileRecovery(context.Background(), record.OperationID)
			if err != nil || stored.Status != FileRecoveryRestored || stored.Progress != FileRecoveryIndexRestored {
				t.Fatalf("completed journal = %#v, %v", stored, err)
			}
		})
	}
}

func fileRecoveryUURepository(t *testing.T, binary bool) string {
	t.Helper()
	base := []byte("value=base\n")
	feature := []byte("value=feature\n")
	main := []byte("value=main\n")
	if binary {
		base = []byte{0, 1, 2, 3, '\n', 0}
		feature = []byte{0, 1, 2, 4, '\n', 0}
		main = []byte{0, 9, 2, 3, '\n', 0}
	}
	name := "f.txt"
	if binary {
		name = "f.bin"
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	fileName := filepath.Join(dir, name)
	write := func(content []byte) {
		t.Helper()
		if err := os.WriteFile(fileName, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(base)
	runGit(t, dir, "add", "--", name)
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "checkout", "-b", "feature")
	write(feature)
	runGit(t, dir, "commit", "-am", "feature")
	runGit(t, dir, "checkout", "main")
	write(main)
	runGit(t, dir, "commit", "-am", "main")
	runGitExpectFailure(t, dir, "merge", "feature", "-m", "merge")
	if got := bytes.Count(fileRecoveryIndexBytes(t, dir, name), []byte{0}); got != 3 {
		t.Fatalf("UU setup has %d stages, want 3", got)
	}
	return dir
}

func TestFileRecoveryRestoresUUTextAndBinaryWithPreciseStages(t *testing.T) {
	for _, binary := range []bool{false, true} {
		kind := "text"
		if binary {
			kind = "binary"
		}
		for _, unstaged := range []bool{false, true} {
			name := kind + "-staged"
			if unstaged {
				name = kind + "-unstaged"
			}
			t.Run(name, func(t *testing.T) {
				dir := fileRecoveryUURepository(t, binary)
				session, err := (Opener{}).Open(context.Background(), dir)
				if err != nil {
					t.Fatal(err)
				}
				recovery := session.(FileRecoverySession)
				path := "f.txt"
				if binary {
					path = "f.bin"
				}
				// A staged UU text file contains Git's conflict markers, not simply
				// ours. Whatever Git wrote is part of the exact initial bytes.
				working := fileRecoveryReadFile(t, dir, path)
				if unstaged {
					working = []byte("unmerged staged + unstaged text\x00\n")
					if binary {
						working = []byte{0, 8, 8, 0, 0xff, '\n'}
					}
					if err := os.WriteFile(filepath.Join(dir, path), working, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				beforeIndex := fileRecoveryIndexBytes(t, dir, path)
				record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
					Path: path, After: fileRecoveryAfterHEAD(t, dir, path),
				})
				if err != nil {
					t.Fatal(err)
				}
				entries := record.Initial.Index.entries()
				for stage := 1; stage <= 3; stage++ {
					entry := entries[stage]
					if !entry.Present || entry.OID == "" || entry.Digest == "" || entry.Size == 0 {
						t.Fatalf("stage %d was not fully archived: %#v", stage, entry)
					}
				}
				fileRecoveryRestoreHEAD(t, dir, path)
				if got := fileRecoveryIndexBytes(t, dir, path); bytes.Count(got, []byte{0}) != 1 {
					t.Fatalf("after-state still has unmerged stages: %q", got)
				}
				if err := recovery.RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
					t.Fatal(err)
				}
				if got := fileRecoveryIndexBytes(t, dir, path); !bytes.Equal(got, beforeIndex) {
					t.Fatalf("unmerged index changed:\n got %q\nwant %q", got, beforeIndex)
				}
				if got := fileRecoveryReadFile(t, dir, path); !bytes.Equal(got, working) {
					t.Fatalf("worktree bytes changed: got %q want %q", got, working)
				}
			})
		}
	}
}

func TestFileRecoveryDistinguishesDeletionAndEmptyFile(t *testing.T) {
	t.Run("deleted worktree with base index", func(t *testing.T) {
		dir, _, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
		if err := os.Remove(filepath.Join(dir, "f.txt")); err != nil {
			t.Fatal(err)
		}
		beforeIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
		record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{Path: "f.txt"})
		if err != nil {
			t.Fatal(err)
		}
		if record.Initial.Worktree.Present {
			t.Fatal("deletion was archived as an empty file")
		}
		// The zero after-state is the state left by git rm for this path.
		runGit(t, dir, "rm", "-f", "--", "f.txt")
		if err := recovery.RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "f.txt")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("deleted worktree file returned: %v", err)
		}
		if got := fileRecoveryIndexBytes(t, dir, "f.txt"); !bytes.Equal(got, beforeIndex) {
			t.Fatalf("deleted file's stage 0 changed: %q", got)
		}
	})

	t.Run("empty present worktree", func(t *testing.T) {
		dir, _, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
			Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !record.Initial.Worktree.Present || record.Initial.Worktree.Size != 0 {
			t.Fatalf("empty file was archived as absent: %#v", record.Initial.Worktree)
		}
		if len(fileRecoveryReadFile(t, dir, "f.txt")) != 0 {
			t.Fatal("test setup did not create an empty file")
		}
		fileRecoveryRestoreHEAD(t, dir, "f.txt")
		if err := recovery.RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(dir, "f.txt"))
		if err != nil || info.Size() != 0 {
			t.Fatalf("empty file was not restored: %#v, %v", info, err)
		}
	})
}

func TestFileRecoveryExplicitRetryAfterReopen(t *testing.T) {
	dir, _, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("before restart\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
		Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	fileRecoveryRestoreHEAD(t, dir, "f.txt")

	reopened, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	reopenedRecovery := reopened.(FileRecoverySession)
	stored, err := reopenedRecovery.ReadFileRecovery(context.Background(), record.OperationID)
	if err != nil || stored.Status != FileRecoveryReady {
		t.Fatalf("record did not survive restart: %#v, %v", stored, err)
	}
	if err := reopenedRecovery.RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
		t.Fatal(err)
	}
	if got := string(fileRecoveryReadFile(t, dir, "f.txt")); got != "before restart\n" {
		t.Fatalf("restart restore = %q", got)
	}
}

func TestFileRecoveryRejectsChangedAfterStateWithoutMutation(t *testing.T) {
	t.Run("worktree", func(t *testing.T) {
		dir, _, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("initial\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
			Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
		})
		if err != nil {
			t.Fatal(err)
		}
		fileRecoveryRestoreHEAD(t, dir, "f.txt")
		afterIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("newer user work\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		err = recovery.RestoreFileRecovery(context.Background(), record.OperationID)
		var conflict *FileRecoveryConflictError
		if !errors.Is(err, ErrFileRecoveryAfterStateChanged) || !errors.As(err, &conflict) || conflict.Component != "worktree" {
			t.Fatalf("CAS error = %#v", err)
		}
		if got := string(fileRecoveryReadFile(t, dir, "f.txt")); got != "newer user work\n" {
			t.Fatalf("CAS failure overwrote new work: %q", got)
		}
		if got := fileRecoveryIndexBytes(t, dir, "f.txt"); !bytes.Equal(got, afterIndex) {
			t.Fatal("CAS failure changed the index")
		}
	})

	t.Run("index", func(t *testing.T) {
		dir, _, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("initial\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
			Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
		})
		if err != nil {
			t.Fatal(err)
		}
		fileRecoveryRestoreHEAD(t, dir, "f.txt")
		runGit(t, dir, "update-index", "--chmod=+x", "--", "f.txt")
		afterWorktree := fileRecoveryReadFile(t, dir, "f.txt")
		afterIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
		err = recovery.RestoreFileRecovery(context.Background(), record.OperationID)
		var conflict *FileRecoveryConflictError
		if !errors.As(err, &conflict) || conflict.Component != "index" {
			t.Fatalf("index CAS error = %#v", err)
		}
		if !bytes.Equal(fileRecoveryReadFile(t, dir, "f.txt"), afterWorktree) ||
			!bytes.Equal(fileRecoveryIndexBytes(t, dir, "f.txt"), afterIndex) {
			t.Fatal("index CAS failure mutated recovery state")
		}
	})
}

func TestFileRecoveryRetriesPartialFileAndIndexInterruptions(t *testing.T) {
	for _, step := range []string{"before-index", "after-index-before-journal"} {
		t.Run(step, func(t *testing.T) {
			dir, session, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
			staged := []byte("staged\n")
			working := []byte("staged\nunstaged\n")
			if err := os.WriteFile(filepath.Join(dir, "f.txt"), staged, 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", "--", "f.txt")
			if err := os.WriteFile(filepath.Join(dir, "f.txt"), working, 0o644); err != nil {
				t.Fatal(err)
			}
			beforeIndex := fileRecoveryIndexBytes(t, dir, "f.txt")
			record, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{
				Path: "f.txt", After: fileRecoveryAfterHEAD(t, dir, "f.txt"),
			})
			if err != nil {
				t.Fatal(err)
			}
			fileRecoveryRestoreHEAD(t, dir, "f.txt")
			crash := errors.New("simulated process interruption")
			hooks := &fileRecoveryRestoreHooks{}
			if step == "before-index" {
				hooks.beforeIndex = func() error { return crash }
			} else {
				hooks.afterIndex = func() error { return crash }
			}
			_, err = session.(*gitSession).restoreFileRecovery(context.Background(), record.OperationID, hooks)
			if !errors.Is(err, crash) {
				t.Fatalf("interruption error = %v", err)
			}
			stored, err := recovery.ReadFileRecovery(context.Background(), record.OperationID)
			if err != nil || stored.Status != FileRecoveryInterrupted || stored.Progress != FileRecoveryWorktreeRestored {
				t.Fatalf("partial journal = %#v, %v", stored, err)
			}
			if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, working) {
				t.Fatalf("file phase did not complete before interruption: %q", got)
			}
			if step == "before-index" && bytes.Equal(fileRecoveryIndexBytes(t, dir, "f.txt"), beforeIndex) {
				t.Fatal("index unexpectedly changed before its phase")
			}
			if step != "before-index" && !bytes.Equal(fileRecoveryIndexBytes(t, dir, "f.txt"), beforeIndex) {
				t.Fatal("index phase did not actually complete before its journal interruption")
			}

			// Also model a crash that left the on-disk status at restoring. A read
			// must expose interrupted without auto-retrying or rewriting the manifest.
			stored.Status = FileRecoveryRestoring
			stored.Error = ""
			if err := session.(*gitSession).writeFileRecoveryRecord(stored); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(dir, ".git", "tangit", "file-recovery", "v2", "manifest", record.OperationID+".json")
			beforeRead, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := (Opener{}).Open(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			interrupted, err := reopened.(FileRecoverySession).ReadFileRecovery(context.Background(), record.OperationID)
			if err != nil || interrupted.Status != FileRecoveryInterrupted {
				t.Fatalf("stale restoring record = %#v, %v", interrupted, err)
			}
			afterRead, err := os.ReadFile(manifest)
			if err != nil || !bytes.Equal(beforeRead, afterRead) {
				t.Fatal("reading an interrupted record mutated its journal")
			}
			if err := reopened.(FileRecoverySession).RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
				t.Fatal(err)
			}
			if got := fileRecoveryReadFile(t, dir, "f.txt"); !bytes.Equal(got, working) {
				t.Fatal("retry changed restored file bytes")
			}
			if got := fileRecoveryIndexBytes(t, dir, "f.txt"); !bytes.Equal(got, beforeIndex) {
				t.Fatal("retry did not restore the exact index")
			}
			completed, err := reopened.(FileRecoverySession).ReadFileRecovery(context.Background(), record.OperationID)
			if err != nil || completed.Status != FileRecoveryRestored {
				t.Fatalf("retry journal = %#v, %v", completed, err)
			}
			if step != "before-index" && completed.Journal[len(completed.Journal)-1].Event != fileRecoveryEventIndexVerified {
				t.Fatalf("retry blindly rewrote an already restored index: %#v", completed.Journal)
			}
		})
	}
}

func TestFileRecoveryRejectsGitlinkAndTraversal(t *testing.T) {
	dir, _, recovery := fileRecoveryRepository(t, "f.txt", []byte("base\n"))
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+head+",sub")
	_, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{Path: "sub"})
	if !errors.Is(err, ErrFileRecoveryUnsupported) {
		t.Fatalf("gitlink archive error = %v", err)
	}
	outside := filepath.Join(filepath.Dir(dir), "outside")
	for _, name := range []string{"../outside", "/absolute", "a/../../outside", `..\outside`} {
		if _, err := recovery.ArchiveFileRecovery(context.Background(), FileRecoveryArchive{Path: name}); err == nil {
			t.Fatalf("unsafe path %q was archived", name)
		}
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path traversal created an outside file: %v", err)
	}
}
