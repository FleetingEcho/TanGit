package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestRealAppUndoCommitThroughSafetyJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the extra recovery assertions use POSIX permissions and symbolic links")
	}
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Undo UI Test")
	runGitIn(t, dir, "config", "user.email", "undo-ui@example.com")
	runGitIn(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "a.txt")
	runGitIn(t, dir, "commit", "-m", "base for journal Undo")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD"))
	binary := []byte{0, 1, 0xff, 2}
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("ignored.bin\nignored-link\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.bin"), binary, 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "ignored.bin"), 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ignored.bin", filepath.Join(dir, "ignored-link")); err != nil {
		t.Fatal(err)
	}
	session, err := (repository.Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	h := newFileRecoveryApp(t, dir, session)
	h.app.pathUserData = t.TempDir()
	previousJournal := repository.SafetyJournalEnabled()
	t.Cleanup(func() { repository.SetSafetyJournalEnabled(previousJournal) })
	h.app.setSafetyJournal(true)
	ctx := context.Background()
	waitFor := func(description string, ready func() bool) {
		t.Helper()
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for {
			h.updates.flushPending()
			h.tester.Frame()
			if ready() {
				return
			}
			select {
			case <-h.updates.done:
			case <-deadline.C:
				records, err := session.Operations(ctx)
				t.Fatalf("waiting for %s: action error=%q, journal=%+v (%v)", description, h.app.actionError, records, err)
			}
		}
	}
	waitFor("the initial repository details", func() bool { return !h.app.repositoryDetailsLoading })

	// Use the actual commit shortcut and real form submission. The baseline
	// contains separate staged bytes and an unstaged working-file suffix.
	h.press(t, ui.KeyC)
	if !h.app.showCommitForm {
		t.Fatalf("C did not open the real commit form: %q", h.texts())
	}
	h.tester.Type("Undo from the safety journal")
	h.tester.Frame()
	if err := h.tester.Click("Commit"); err != nil {
		t.Fatalf("submit the real commit: %v; texts=%q", err, h.texts())
	}
	waitFor("a recoverable commit in the safety journal", func() bool {
		for _, record := range h.app.snapshot.Operations {
			if record.Kind == repository.OperationCommit && record.CanUndo() {
				return true
			}
		}
		return false
	})
	var record repository.OperationRecord
	for _, current := range h.app.snapshot.Operations {
		if current.Kind == repository.OperationCommit {
			record = current
			break
		}
	}
	after := strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD"))
	if after == before {
		t.Fatal("the real commit did not move HEAD")
	}

	entries := h.app.commandLogEntries()
	provenance := 0
	for _, entry := range entries {
		if entry.Shortcut != nil && entry.Shortcut.ID == "git/Changes/Write a commit message" {
			provenance++
			if entry.Shortcut.Chord != "c" || entry.Shortcut.Label != "Write a commit message" {
				t.Fatalf("commit shortcut provenance = %+v", entry.Shortcut)
			}
		}
		if entry.Shortcut != nil && entry.Shortcut.Chord == "Enter" {
			t.Fatalf("typing in the form or mouse approval was mislabeled as a shortcut: %+v", entry.Shortcut)
		}
	}
	if provenance != 1 {
		t.Fatalf("commit keyboard provenance records = %d, want exactly 1", provenance)
	}

	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	openRecoveryForTest(t, h)
	if err := h.tester.Click("Safety Journal"); err != nil {
		t.Fatalf("open Safety Journal: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if err := h.tester.Click("Undo " + record.ID); err != nil {
		t.Fatalf("choose journal Undo: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.message != "Undo "+record.Summary+"?" {
		t.Fatalf("Undo did not open its exact confirmation: %#v; texts=%q", h.app.confirm, h.texts())
	}
	if got := strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD")); got != after {
		t.Fatal("Undo ran before the confirmation was accepted")
	}
	if err := h.tester.Click("Undo"); err != nil {
		t.Fatalf("accept Undo: %v; texts=%q", err, h.texts())
	}
	waitFor("the confirmed Undo to restore the parent", func() bool {
		if strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD")) != before {
			return false
		}
		records, err := session.Operations(ctx)
		if err != nil {
			return false
		}
		for _, current := range records {
			if current.ID == record.ID {
				return current.UndoneAt != 0
			}
		}
		return false
	})
	if got := strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD")); got != before {
		t.Fatalf("confirmed Undo moved HEAD to %s, want %s", got, before)
	}
	if got := runGitIn(t, dir, "show", ":a.txt"); got != "staged\n" {
		t.Fatalf("Undo restored the wrong staged payload: %q", got)
	}
	if got := readRecoveryFile(t, dir, "a.txt"); string(got) != "staged\nunstaged\n" {
		t.Fatalf("Undo restored the wrong staged/unstaged split: %q", got)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "ignored.bin")); err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("Undo lost ignored binary data: %v, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dir, "ignored.bin"))
	if err != nil || info.Mode().Perm() != 0o751 {
		t.Fatalf("Undo restored the wrong ignored permissions: %v, %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "ignored-link")); err != nil || target != "ignored.bin" {
		t.Fatalf("Undo restored the wrong symbolic link: %q, %v", target, err)
	}

}

func TestRealAppCtrlZAfterAbortRestoresConflictWithSafetyJournalOff(t *testing.T) {
	previousJournal := repository.SafetyJournalEnabled()
	repository.SetSafetyJournalEnabled(false)
	t.Cleanup(func() { repository.SetSafetyJournalEnabled(previousJournal) })

	dir, session := newFileRecoveryConflict(t, conflictVersions{
		name: "conflict.txt",
		base: []byte("base\n"), current: []byte("current\n"), incoming: []byte("incoming\n"),
	})
	originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", "conflict.txt")
	h := newFileRecoveryApp(t, dir, session)
	h.waitFor(t, "active merge operation", func() bool {
		return h.app.snapshot.OperationState != nil && h.app.snapshot.OperationState.Type == repository.OperationTypeMerge
	})
	h.tester.Frame()
	if err := h.tester.Click("Abort"); err != nil {
		t.Fatalf("open Abort confirmation: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if err := h.tester.Click("Abort"); err != nil {
		t.Fatalf("confirm Abort: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "merge abort", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && h.app.snapshot.OperationState == nil && !hasConflictedFiles(h.app.snapshot.Files)
	})

	var abortRecord repository.OperationRecord
	h.waitFor(t, "published Abort recovery", func() bool {
		for _, record := range h.app.snapshot.Operations {
			if record.Kind == repository.OperationTransition && record.Summary == "Abort merge" && record.CanUndo() {
				abortRecord = record
				return true
			}
		}
		return false
	})
	if abortRecord.ID == "" {
		t.Fatalf("Abort did not retain an undoable operation transition with the safety journal off: %+v", h.app.snapshot.Operations)
	}

	h.tester.Key(ui.Ctrl, ui.KeyZ)
	h.waitFor(t, "Ctrl+Z confirmation", func() bool { return h.app.confirm != nil })
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.message != "Undo Abort merge?" {
		t.Fatalf("Ctrl+Z selected reflog replay instead of Abort recovery: %#v", h.app.confirm)
	}
	if err := h.tester.Click("Undo"); err != nil {
		t.Fatalf("confirm Abort Undo: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "restored unresolved merge", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && h.app.snapshot.OperationState != nil &&
			h.app.snapshot.OperationState.Type == repository.OperationTypeMerge && hasConflictedFiles(h.app.snapshot.Files)
	})
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", "conflict.txt"); got != originalIndex {
		t.Fatalf("Ctrl+Z restored index = %q, want original conflict stages %q", got, originalIndex)
	}
}

// After an Abort the Changes list is empty and holds nothing to focus, yet the
// empty state must name the Ctrl+Z that takes the Abort back, and Ctrl+Z must
// still work from there. The hint is only for the first minute.
func TestRealAppEmptyChangesHintsCtrlZAfterAbortAndExpires(t *testing.T) {
	previous := repository.SafetyJournalEnabled()
	repository.SetSafetyJournalEnabled(true)
	t.Cleanup(func() { repository.SetSafetyJournalEnabled(previous) })

	dir, session := newFileRecoveryConflict(t, conflictVersions{
		name: "conflict.txt",
		base: []byte("base\n"), current: []byte("current\n"), incoming: []byte("incoming\n"),
	})
	h := newFileRecoveryApp(t, dir, session)
	h.waitFor(t, "active merge operation", func() bool {
		return h.app.snapshot.OperationState != nil
	})
	h.tester.Frame()
	if err := h.tester.Click("Abort"); err != nil {
		t.Fatalf("open Abort confirmation: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if err := h.tester.Click("Abort"); err != nil {
		t.Fatalf("confirm Abort: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "the Abort hint", func() bool {
		if h.app.actionBusy || h.app.snapshot.OperationState != nil || len(h.app.snapshot.Files) != 0 {
			return false
		}
		h.tester.Frame()
		return slices.Contains(h.texts(), "Undo Abort merge") && slices.Contains(h.texts(), "Ctrl+Z")
	})

	h.tester.Key(ui.Ctrl, ui.KeyZ)
	h.waitFor(t, "Ctrl+Z confirmation from the empty list", func() bool { return h.app.confirm != nil })
	if h.app.confirm.message != "Undo Abort merge?" {
		t.Fatalf("Ctrl+Z offered %q", h.app.confirm.message)
	}
	h.app.confirm = nil

	// Past the window the hint goes; Ctrl+Z itself does not.
	for i, record := range h.app.snapshot.Operations {
		h.app.snapshot.Operations[i].FinishedAt = record.FinishedAt - int64(undoHintWindow.Seconds()) - 5
	}
	h.tester.Frame()
	h.tester.Frame()
	if slices.Contains(h.texts(), "Undo Abort merge") {
		t.Fatalf("the hint outlived its minute: %q", h.texts())
	}
	if _, ok := h.app.latestUndoableRecord(); !ok {
		t.Fatal("Abort stopped being undoable when its hint expired")
	}
}
