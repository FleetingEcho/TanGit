package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type fileRecoveryOpener struct{ session repository.Session }

func (o fileRecoveryOpener) Open(context.Context, string) (repository.Session, error) {
	return o.session, nil
}
func (fileRecoveryOpener) Init(context.Context, string) error { return nil }

func newFileRecoveryApp(t *testing.T, dir string, session repository.Session) *harness {
	t.Helper()
	a := newApp(fileRecoveryOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1440, 900)
	a.update = h.updates.update
	a.path = dir
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "real repository recovery details", func() bool {
		return h.app.state == stateReady && h.app.snapshot.Root == dir
	})
	h.tester.Frame()
	return h
}

type conflictVersions struct {
	name                    string
	base, current, incoming []byte
	baseMode                os.FileMode
	currentMode             os.FileMode
	incomingMode            os.FileMode
	deleteCurrent           bool
	deleteIncoming          bool
}

func newFileRecoveryConflict(t *testing.T, versions conflictVersions) (string, repository.Session) {
	t.Helper()
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Desktop Test")
	runGitIn(t, dir, "config", "user.email", "desktop@example.com")
	writeVersion := func(content []byte, mode os.FileMode, remove bool) {
		t.Helper()
		name := filepath.Join(dir, versions.name)
		if remove {
			if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			return
		}
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(name, content, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(name, mode); err != nil {
			t.Fatal(err)
		}
	}
	writeVersion(versions.base, versions.baseMode, false)
	runGitIn(t, dir, "add", "--", versions.name)
	runGitIn(t, dir, "commit", "-m", "base")
	runGitIn(t, dir, "checkout", "-b", "feature")
	writeVersion(versions.incoming, versions.incomingMode, versions.deleteIncoming)
	runGitIn(t, dir, "add", "-A", "--", versions.name)
	runGitIn(t, dir, "commit", "-m", "feature")
	runGitIn(t, dir, "checkout", "main")
	writeVersion(versions.current, versions.currentMode, versions.deleteCurrent)
	runGitIn(t, dir, "add", "-A", "--", versions.name)
	runGitIn(t, dir, "commit", "-m", "main")
	command := exec.Command("git", "-C", dir, "merge", "feature", "-m", "merge")
	if output, err := command.CombinedOutput(); err == nil {
		t.Fatalf("merge unexpectedly had no conflict: %s", output)
	}
	session, err := (repository.Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, session
}

func recoveryConflictFile(t *testing.T, session repository.Session, path string) repository.File {
	t.Helper()
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range snapshot.Files {
		if file.Path == path && file.HasMergeConflicts {
			return file
		}
	}
	t.Fatalf("no real conflict for %s: %#v", path, snapshot.Files)
	return repository.File{}
}

func readRecoveryFile(t *testing.T, dir, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func onlyRecoveryRecord(t *testing.T, session repository.Session) repository.FileRecoveryRecord {
	t.Helper()
	records, err := session.(repository.FileRecoveryWorkflowSession).ListFileRecovery(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("recovery records = %#v, want one", records)
	}
	return records[0]
}

func TestRealAppConflictUseCurrentRestartRestoreUnresolved(t *testing.T) {
	dir, session := newFileRecoveryConflict(t, conflictVersions{
		name: "conflict.txt",
		base: []byte("before\nbase\nafter\n"), current: []byte("before\ncurrent\nafter\n"), incoming: []byte("before\nincoming\nafter\n"),
	})
	path := "conflict.txt"
	originalFile := readRecoveryFile(t, dir, path)
	if !bytes.Contains(originalFile, []byte("<<<<<<<")) {
		t.Fatalf("real merge left no markers: %q", originalFile)
	}
	originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path)
	if got := strings.Count(originalIndex, "\x00"); got != 3 {
		t.Fatalf("original index has %d stages, want 3: %q", got, originalIndex)
	}
	h := newFileRecoveryApp(t, dir, session)
	h.app.confirm = nil
	if err := h.tester.Click("Use current " + path); err != nil {
		t.Fatalf("whole-file Use current entry: %v; texts=%q", err, h.texts())
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "Use current and durable after-state", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && !hasConflictedFiles(h.app.snapshot.Files) && len(h.app.snapshot.FileRecoveries) == 1
	})
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, []byte("before\ncurrent\nafter\n")) {
		t.Fatalf("Use current wrote %q", got)
	}
	record := h.app.snapshot.FileRecoveries[0]
	if !fileRecoveryHasUnresolved(record) || !record.After.Index.Stage0.Present || record.After.Index.Stage1.Present {
		t.Fatalf("whole-file stage did not preserve Initial/After separation: %#v", record)
	}

	// Unrelated staged and unstaged work must neither block nor be reverted by
	// the recovered path's exact worktree/index CAS.
	other := filepath.Join(dir, "unrelated.txt")
	if err := os.WriteFile(other, []byte("staged other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "--", "unrelated.txt")
	otherIndex := runGitIn(t, dir, "show", ":unrelated.txt")
	if err := os.WriteFile(other, []byte("staged other\nnew unstaged work\x00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherWorktree := readRecoveryFile(t, dir, "unrelated.txt")
	if closer, ok := session.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}

	reopened, err := (repository.Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	restarted := newFileRecoveryApp(t, dir, reopened)
	restarted.waitFor(t, "reopened durable recovery", func() bool {
		return len(restarted.app.snapshot.FileRecoveries) == 1
	})
	restarted.tester.Frame()
	// Use current can equal HEAD, in which case Git correctly removes the path
	// from Staged Changes. The Operation-context recovery entry remains.
	if err := restarted.tester.Click("Conflict recovery"); err != nil {
		t.Fatalf("Operation recovery entry missing after restart: %v; texts=%q", err, restarted.texts())
	}
	restarted.tester.Frame()
	if err := restarted.tester.Click("Restore unresolved…"); err != nil {
		t.Fatalf("recovery-panel restore entry: %v; texts=%q", err, restarted.texts())
	}
	restarted.tester.Frame()
	if restarted.app.confirm == nil {
		t.Fatal("restoring unresolved work did not ask for destructive confirmation")
	}
	if err := restarted.tester.Click("Restore unresolved"); err != nil {
		t.Fatalf("confirm Restore unresolved: %v; texts=%q", err, restarted.texts())
	}
	restarted.waitFor(t, "Restore Unresolved after restart", func() bool {
		return !restarted.app.actionBusy && restarted.app.actionError == "" && hasConflictedFiles(restarted.app.snapshot.Files)
	})
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("restored marker bytes = %q, want %q", got, originalFile)
	}
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path); got != originalIndex {
		t.Fatalf("restored unmerged index = %q, want %q", got, originalIndex)
	}
	if got := readRecoveryFile(t, dir, "unrelated.txt"); !bytes.Equal(got, otherWorktree) {
		t.Fatalf("unrelated worktree changed: %q", got)
	}
	if got := runGitIn(t, dir, "show", ":unrelated.txt"); got != otherIndex {
		t.Fatalf("unrelated index changed: %q, want %q", got, otherIndex)
	}
	stored := onlyRecoveryRecord(t, reopened)
	if stored.Status != repository.FileRecoveryRestored || stored.Progress != repository.FileRecoveryIndexRestored {
		t.Fatalf("restore did not finish its durable journal: %#v", stored)
	}
}

func TestRealAppConflictHunksThenStagedRestoreOriginalUnresolved(t *testing.T) {
	stable := "stable 1\nstable 2\nstable 3\nstable 4\nstable 5\nstable 6\nstable 7\nstable 8\n"
	dir, session := newFileRecoveryConflict(t, conflictVersions{
		name:     "conflict.txt",
		base:     []byte("before\nbase one\n" + stable + "base two\nafter\n"),
		current:  []byte("before\ncurrent one\n" + stable + "current two\nafter\n"),
		incoming: []byte("before\nincoming one\n" + stable + "incoming two\nafter\n"),
	})
	path := "conflict.txt"
	originalFile := readRecoveryFile(t, dir, path)
	originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path)
	h := newFileRecoveryApp(t, dir, session)
	h.waitFor(t, "real two-hunk conflict editor", func() bool {
		return h.app.conflictState != nil && len(h.app.conflictState.regions) == 2
	})
	h.tester.Frame()
	if err := h.tester.Click("Use current"); err != nil {
		t.Fatalf("first conflict hunk: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "initial hunk recovery", func() bool {
		return len(h.app.snapshot.FileRecoveries) == 1
	})
	first := onlyRecoveryRecord(t, session)
	if first.After.Index.Stage0.Present || first.After.Index.Stage1.Present == false {
		t.Fatalf("hunk selection unexpectedly changed the index: %#v", first.After.Index)
	}
	h.tester.Frame()
	if err := h.tester.Click("Use incoming"); err != nil {
		t.Fatalf("second conflict hunk: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "updated hunk after-state", func() bool {
		record := onlyRecoveryRecord(t, session)
		return len(record.Journal) >= 3 && record.UpdatedAt >= first.UpdatedAt
	})
	second := onlyRecoveryRecord(t, session)
	if second.OperationID != first.OperationID {
		t.Fatal("successive hunks created a new initial recovery instead of advancing the original")
	}
	wantMixed := []byte("before\ncurrent one\n" + stable + "incoming two\nafter\n")
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, wantMixed) {
		t.Fatalf("mixed hunk result = %q, want %q", got, wantMixed)
	}
	h.tester.Frame()
	if err := h.tester.Click("Mark resolved"); err != nil {
		t.Fatalf("mark mixed resolution staged: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "staged mixed resolution", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && !hasConflictedFiles(h.app.snapshot.Files)
	})
	staged := onlyRecoveryRecord(t, session)
	if staged.OperationID != first.OperationID || !staged.After.Index.Stage0.Present || !fileRecoveryHasUnresolved(staged) {
		t.Fatalf("staging replaced the original unresolved record: %#v", staged)
	}
	h.tester.Frame()
	if err := h.tester.Click("Restore unresolved " + path); err != nil {
		t.Fatalf("staged-row recovery entry: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if err := h.tester.Click("Restore unresolved"); err != nil {
		t.Fatalf("confirm staged-row restore: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "restore original multi-hunk conflict", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && hasConflictedFiles(h.app.snapshot.Files)
	})
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("restored original markers = %q, want %q", got, originalFile)
	}
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path); got != originalIndex {
		t.Fatalf("restored original stages = %q, want %q", got, originalIndex)
	}
}

func TestRealAppUnstageResolvedConflictRestoresOriginalUnresolvedState(t *testing.T) {
	dir, session := newFileRecoveryConflict(t, conflictVersions{
		name: "conflict.txt",
		base: []byte("base\n"), current: []byte("current\n"), incoming: []byte("incoming\n"),
	})
	path := "conflict.txt"
	originalFile := readRecoveryFile(t, dir, path)
	originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path)
	h := newFileRecoveryApp(t, dir, session)
	h.app.confirm = nil
	if err := h.tester.Click("Use incoming " + path); err != nil {
		t.Fatalf("whole-file Use incoming entry: %v; texts=%q", err, h.texts())
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "resolved conflict staged with recovery", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && !hasConflictedFiles(h.app.snapshot.Files) && len(h.app.snapshot.FileRecoveries) == 1
	})
	h.tester.Frame()
	if err := h.tester.Click("Unstage " + path); err != nil {
		t.Fatalf("unstage resolved conflict: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("unstaging a resolved conflict did not offer to restore its unresolved state")
	}
	if err := h.tester.Click("Restore unresolved"); err != nil {
		t.Fatalf("confirm Restore unresolved: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "unstage restores original conflict", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && hasConflictedFiles(h.app.snapshot.Files)
	})
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("restored markers = %q, want %q", got, originalFile)
	}
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path); got != originalIndex {
		t.Fatalf("restored unmerged index = %q, want %q", got, originalIndex)
	}
}

func TestRealAppFileRecoveryAfterStateChangedPreservesNewWork(t *testing.T) {
	dir, session := newFileRecoveryConflict(t, conflictVersions{
		name: "conflict.txt", base: []byte("base\n"), current: []byte("current\n"), incoming: []byte("incoming\n"),
	})
	file := recoveryConflictFile(t, session, "conflict.txt")
	if err := runFileRecoveryMutation(context.Background(), session, file.Path, func() error {
		return requireChangesSession(session).ResolveConflict(context.Background(), file, true)
	}); err != nil {
		t.Fatal(err)
	}
	record := onlyRecoveryRecord(t, session)
	afterIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", file.Path)
	newer := []byte("newer user work\x00\n")
	if err := os.WriteFile(filepath.Join(dir, file.Path), newer, 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(other, []byte("independent work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := session.(repository.FileRecoverySession).RestoreFileRecovery(context.Background(), record.OperationID)
	var conflict *repository.FileRecoveryConflictError
	if !errors.Is(err, repository.ErrFileRecoveryAfterStateChanged) || !errors.As(err, &conflict) || conflict.Component != "worktree" {
		t.Fatalf("after-CAS error = %#v", err)
	}
	if got := readRecoveryFile(t, dir, file.Path); !bytes.Equal(got, newer) {
		t.Fatalf("CAS overwrote new work: %q", got)
	}
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", file.Path); got != afterIndex {
		t.Fatal("CAS failure changed the recovered path's index")
	}
	if got := readRecoveryFile(t, dir, "other.txt"); string(got) != "independent work\n" {
		t.Fatalf("unrelated work changed: %q", got)
	}
	stored, err := session.(repository.FileRecoverySession).ReadFileRecovery(context.Background(), record.OperationID)
	if err != nil || stored.Status != repository.FileRecoveryInterrupted || stored.Progress != repository.FileRecoveryPending {
		t.Fatalf("failed CAS did not leave Retry state: %#v, %v", stored, err)
	}
	if stored.After.Worktree.Digest != record.After.Worktree.Digest || stored.After.Index.Stage0.OID != record.After.Index.Stage0.OID {
		t.Fatal("failed CAS replaced the saved after-state")
	}

	a := newApp(fileRecoveryOpener{session: session})
	a.snapshot.FileRecoveries = []repository.FileRecoveryRecord{stored}
	a.session = session
	a.showRecoveryModal = true
	tester := ui.NewTester(a.view, 1000, 800)
	tester.Frame()
	if _, found := tester.Find("Retry…"); !found {
		t.Fatalf("interrupted recovery has no Retry UI: %q", tester.Texts())
	}
}

func TestRealAppInterruptedIndexRecoveryRetriesFromPanel(t *testing.T) {
	dir, session := newFileRecoveryConflict(t, conflictVersions{
		name: "conflict.txt", base: []byte("base\n"), current: []byte("current\n"), incoming: []byte("incoming\n"),
	})
	path := "conflict.txt"
	originalFile := readRecoveryFile(t, dir, path)
	originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path)
	file := recoveryConflictFile(t, session, path)
	if err := runFileRecoveryMutation(context.Background(), session, path, func() error {
		return requireChangesSession(session).ResolveConflict(context.Background(), file, true)
	}); err != nil {
		t.Fatal(err)
	}
	record := onlyRecoveryRecord(t, session)
	// Model a crash after the real file phase and its durable transition, but
	// before update-index. No public production hook is added for this test.
	if err := os.WriteFile(filepath.Join(dir, path), originalFile, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, ".git", "tangit", "file-recovery", "v2", "manifest", record.OperationID+".json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var interrupted repository.FileRecoveryRecord
	if err := json.Unmarshal(data, &interrupted); err != nil {
		t.Fatal(err)
	}
	interrupted.Status = repository.FileRecoveryRestoring
	interrupted.Progress = repository.FileRecoveryWorktreeRestored
	interrupted.Attempts = 1
	interrupted.Error = ""
	interrupted.Journal = append(interrupted.Journal, repository.FileRecoveryJournalEntry{
		Time: time.Now().UnixNano(), Attempt: 1, Event: "worktree-restored",
	})
	data, err = json.MarshalIndent(interrupted, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := (repository.Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	h := newFileRecoveryApp(t, dir, reopened)
	h.waitFor(t, "interrupted recovery discovery", func() bool {
		return len(h.app.snapshot.FileRecoveries) == 1 && h.app.snapshot.FileRecoveries[0].Status == repository.FileRecoveryInterrupted
	})
	h.tester.Frame()
	if err := h.tester.Click("Conflict recovery"); err != nil {
		t.Fatalf("open interrupted recovery: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if !h.tester.HasText("Interrupted · index pending") {
		t.Fatalf("atomic index interruption is not explained: %q", h.texts())
	}
	if err := h.tester.Click("Retry…"); err != nil {
		t.Fatalf("Retry button: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if err := h.tester.Click("Retry restore"); err != nil {
		t.Fatalf("confirm Retry: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "Retry completes the index phase", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && hasConflictedFiles(h.app.snapshot.Files)
	})
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("Retry rewrote the already restored worktree: %q", got)
	}
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path); got != originalIndex {
		t.Fatalf("Retry did not restore the original index: %q", got)
	}
	stored := onlyRecoveryRecord(t, reopened)
	if stored.Status != repository.FileRecoveryRestored || len(stored.Journal) == 0 || stored.Journal[len(stored.Journal)-1].Event != "index-restored" {
		t.Fatalf("Retry journal = %#v", stored)
	}
}

func TestRealAppRecoverySupportsBinaryExecutableAndDeletion(t *testing.T) {
	tests := []struct {
		name     string
		versions conflictVersions
		current  bool
		deleted  bool
	}{
		{name: "binary raw bytes", current: true, versions: conflictVersions{
			name: "binary.dat", base: []byte{0, 1, 2, '\n'}, current: []byte{0, 8, 0xff, '\n'}, incoming: []byte{0, 9, 0xfe, '\n'},
		}},
		{name: "executable mode", current: true, versions: conflictVersions{
			name: "run.sh", base: []byte("base\n"), current: []byte("current\n"), incoming: []byte("incoming\n"),
			baseMode: 0o644, currentMode: 0o755, incomingMode: 0o644,
		}},
		{name: "incoming deletion", current: false, deleted: true, versions: conflictVersions{
			name: "deleted.txt", base: []byte("base\n"), current: []byte("current\n"), deleteIncoming: true,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir, session := newFileRecoveryConflict(t, test.versions)
			path := test.versions.name
			originalFile := readRecoveryFile(t, dir, path)
			originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path)
			file := recoveryConflictFile(t, session, path)
			if err := runFileRecoveryMutation(context.Background(), session, path, func() error {
				return requireChangesSession(session).ResolveConflict(context.Background(), file, test.current)
			}); err != nil {
				t.Fatal(err)
			}
			record := onlyRecoveryRecord(t, session)
			if test.deleted {
				if record.After.Worktree.Present || record.After.Index.Stage0.Present {
					t.Fatalf("deletion after-state was not recorded as absent: %#v", record.After)
				}
				if _, err := os.Lstat(filepath.Join(dir, path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("resolved deletion still exists: %v", err)
				}
			} else {
				if !record.After.Worktree.Present || !record.After.Index.Stage0.Present {
					t.Fatalf("resolved file was not captured: %#v", record.After)
				}
				if test.name == "binary raw bytes" && !bytes.Equal(readRecoveryFile(t, dir, path), test.versions.current) {
					t.Fatal("whole-file binary resolution changed raw bytes")
				}
				if test.name == "executable mode" {
					info, err := os.Stat(filepath.Join(dir, path))
					if err != nil || info.Mode().Perm()&0o111 == 0 || record.After.Index.Stage0.Mode != "100755" {
						t.Fatalf("executable resolution lost its mode: %v, %v, record=%#v", info, err, record.After)
					}
				}
			}
			if err := session.(repository.FileRecoverySession).RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
				t.Fatal(err)
			}
			if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
				t.Fatalf("restored original bytes = %q, want %q", got, originalFile)
			}
			if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path); got != originalIndex {
				t.Fatalf("restored original index = %q, want %q", got, originalIndex)
			}
			if test.name == "executable mode" {
				info, err := os.Stat(filepath.Join(dir, path))
				if err != nil || info.Mode().Perm()&0o111 == 0 {
					t.Fatalf("executable original was not restored: %v, %v", info, err)
				}
			}
		})
	}
}

func TestRealAppUnsupportedConflictDoesNotMutateWithoutRecovery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional Windows privileges")
	}
	dir := t.TempDir()
	name := "linked"
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Desktop Test")
	runGitIn(t, dir, "config", "user.email", "desktop@example.com")
	writeLink := func(target string) {
		t.Helper()
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	writeLink("base")
	runGitIn(t, dir, "add", "--", name)
	runGitIn(t, dir, "commit", "-m", "base")
	runGitIn(t, dir, "checkout", "-b", "feature")
	writeLink("incoming")
	runGitIn(t, dir, "add", "--", name)
	runGitIn(t, dir, "commit", "-m", "feature")
	runGitIn(t, dir, "checkout", "main")
	writeLink("current")
	runGitIn(t, dir, "add", "--", name)
	runGitIn(t, dir, "commit", "-m", "main")
	if output, err := exec.Command("git", "-C", dir, "merge", "feature", "-m", "merge").CombinedOutput(); err == nil {
		t.Fatalf("symlink merge unexpectedly had no conflict: %s", output)
	}
	session, err := (repository.Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	file := recoveryConflictFile(t, session, name)
	called := false
	err = runFileRecoveryMutation(context.Background(), session, name, func() error {
		called = true
		return requireChangesSession(session).ResolveConflict(context.Background(), file, true)
	})
	if !errors.Is(err, repository.ErrFileRecoveryUnsupported) || !strings.Contains(err.Error(), "symbolic") {
		t.Fatalf("unsupported recovery error = %v", err)
	}
	if called {
		t.Fatal("unsupported symlink was resolved without a durable recovery")
	}
	if target, readErr := os.Readlink(filepath.Join(dir, name)); readErr != nil || target != "current" {
		t.Fatalf("unsupported symlink changed: target=%q err=%v", target, readErr)
	}
}
