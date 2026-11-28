package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func undoApp(t *testing.T) *app {
	t.Helper()
	_, session := newAddAddConflict(t)
	a := newApp(fileRecoveryOpener{session: session})
	a.session = session
	a.snapshot.HeadHash = "head-now"
	return a
}

func indexRecord(id, summary, head string) repository.OperationRecord {
	return repository.OperationRecord{
		Version: 1, ID: id, Kind: repository.OperationStage, Summary: summary,
		Status: repository.OperationSucceeded, UndoKind: repository.UndoRestoreIndex,
		HeadAfter: head, IndexBefore: "a", IndexAfter: "b",
	}
}

func lastToastTitle(a *app) string {
	if len(a.pendingToasts) == 0 {
		return ""
	}
	return a.pendingToasts[len(a.pendingToasts)-1].title
}

func TestCtrlZNamesTheExactChangeItWillUndo(t *testing.T) {
	a := undoApp(t)
	a.snapshot.Operations = []repository.OperationRecord{
		indexRecord("00000000000000000000000000000002", "Stage new.txt", "head-now"),
		indexRecord("00000000000000000000000000000001", "Stage old.txt", "head-now"),
	}
	a.offerUndo()
	if a.confirm == nil {
		t.Fatal("Ctrl+Z asked nothing")
	}
	if !strings.Contains(a.confirm.message, "Stage new.txt") || strings.Contains(a.confirm.message, "old.txt") {
		t.Fatalf("Ctrl+Z must name the newest change: %q", a.confirm.message)
	}
	if !strings.Contains(a.confirm.detail, "files are not changed") {
		t.Fatalf("a staging undo must say it leaves files alone: %q", a.confirm.detail)
	}
}

func TestCtrlZSkipsRecordsThatAnotherHeadMadeStale(t *testing.T) {
	a := undoApp(t)
	a.snapshot.Operations = []repository.OperationRecord{
		indexRecord("00000000000000000000000000000002", "Stage after-pull.txt", "head-now"),
		indexRecord("00000000000000000000000000000001", "Stage before-pull.txt", "an-older-head"),
	}
	record, ok := a.latestUndoableRecord()
	if !ok || record.Summary != "Stage after-pull.txt" {
		t.Fatalf("latest = %#v, %v", record, ok)
	}
	a.snapshot.Operations = a.snapshot.Operations[1:]
	if record, ok := a.latestUndoableRecord(); ok {
		t.Fatalf("a record from before the HEAD moved is offered: %#v", record)
	}
}

func TestCtrlZSkipsUndoneAndUnrecoverableRecords(t *testing.T) {
	a := undoApp(t)
	undone := indexRecord("00000000000000000000000000000003", "Stage undone.txt", "head-now")
	undone.UndoneAt = 5
	failed := indexRecord("00000000000000000000000000000002", "Stage failed.txt", "head-now")
	failed.Status = repository.OperationFailed
	good := indexRecord("00000000000000000000000000000001", "Stage good.txt", "head-now")
	a.snapshot.Operations = []repository.OperationRecord{undone, failed, good}
	if record, ok := a.latestUndoableRecord(); !ok || record.Summary != "Stage good.txt" {
		t.Fatalf("latest = %#v, %v", record, ok)
	}
}

func TestCtrlZWithNothingToUndoSaysSoInsteadOfAskingAVagueQuestion(t *testing.T) {
	a := undoApp(t)
	enabled := repository.SafetyJournalEnabled()
	repository.SetSafetyJournalEnabled(true)
	defer repository.SetSafetyJournalEnabled(enabled)
	a.offerUndo()
	if a.confirm != nil {
		t.Fatalf("Ctrl+Z opened a confirmation with nothing to undo: %#v", a.confirm)
	}
	if got := lastToastTitle(a); got != tr("Nothing to undo") {
		t.Fatalf("toast = %q, want Nothing to undo", got)
	}
}

func TestCtrlZExplainsWhyWhenTheSafetyJournalIsOff(t *testing.T) {
	a := undoApp(t)
	enabled := repository.SafetyJournalEnabled()
	repository.SetSafetyJournalEnabled(false)
	defer repository.SetSafetyJournalEnabled(enabled)
	a.offerUndo()
	if len(a.pendingToasts) == 0 || !strings.Contains(a.pendingToasts[len(a.pendingToasts)-1].description, tr("The safety journal is off, so staging and discarding are not recorded. Turn it on in Settings to use Undo for them.")[:20]) {
		t.Fatalf("toast = %#v", a.pendingToasts)
	}
}

func newStagingRepo(t *testing.T) (string, repository.Session) {
	t.Helper()
	previous := repository.SafetyJournalEnabled()
	repository.SetSafetyJournalEnabled(true)
	t.Cleanup(func() { repository.SetSafetyJournalEnabled(previous) })
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Desktop Test")
	runGitIn(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "a.txt")
	runGitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session, err := (repository.Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, session
}

// The whole path a user takes: click Stage on a file, press Ctrl+Z, read what
// the dialog says it will undo, accept, and find the file unstaged again with
// its content untouched.
func TestRealAppCtrlZUndoesAStageFromTheKeyboard(t *testing.T) {
	for _, path := range []string{"a.txt", "new.txt"} {
		t.Run(path, func(t *testing.T) {
			dir, session := newStagingRepo(t)
			before := runGitIn(t, dir, "status", "--porcelain=v1")
			h := newFileRecoveryApp(t, dir, session)
			h.app.confirm = nil
			if path != "a.txt" {
				// Row buttons show on the chosen row, so choose the file first.
				if err := h.tester.Click(path); err != nil {
					t.Fatalf("choose %s: %v", path, err)
				}
				h.settle(t)
				h.tester.Frame()
				h.waitFor(t, path+" chosen", func() bool {
					change, ok := h.app.selectedChange()
					return ok && change.file.Path == path
				})
				h.tester.Frame()
			}
			if err := h.tester.Click("Stage " + path); err != nil {
				t.Fatalf("Stage: %v; texts=%q", err, h.texts())
			}
			h.waitFor(t, "file staged", func() bool {
				if h.app.actionBusy {
					return false
				}
				for _, file := range h.app.snapshot.Files {
					if file.Path == path && file.Staged {
						return true
					}
				}
				return false
			})
			content, _ := os.ReadFile(filepath.Join(dir, path))

			h.tester.Key(ui.Ctrl, ui.KeyZ)
			h.waitFor(t, "Ctrl+Z dialog", func() bool { return h.app.confirm != nil })
			if h.app.confirm == nil || h.app.confirm.message != trf("Undo %s?", "Stage "+path) {
				t.Fatalf("Ctrl+Z dialog = %#v, want it to name %q", h.app.confirm, "Stage "+path)
			}
			h.tester.Frame()
			if err := h.tester.Click("Undo"); err != nil {
				t.Fatalf("confirm Undo: %v; texts=%q", err, h.texts())
			}
			h.waitFor(t, "the file is unstaged again", func() bool {
				if h.app.actionBusy {
					return false
				}
				for _, file := range h.app.snapshot.Files {
					if file.Path == path && file.Staged {
						return false
					}
				}
				return true
			})
			if got := runGitIn(t, dir, "status", "--porcelain=v1"); got != before {
				t.Fatalf("status after Ctrl+Z = %q, want %q", got, before)
			}
			if got, _ := os.ReadFile(filepath.Join(dir, path)); !bytes.Equal(got, content) {
				t.Fatalf("Ctrl+Z changed the file: %q", got)
			}
			// A second Ctrl+Z must not offer the same change again.
			h.tester.Frame()
			if record, ok := h.app.latestUndoableRecord(); ok && record.Summary == "Stage "+path {
				t.Fatalf("the undone stage is still offered: %#v", record)
			}
		})
	}
}

func TestCtrlZPrefersTheNewerOfAJournalEntryAndAConflictResolution(t *testing.T) {
	a := undoApp(t)
	a.snapshot.OperationState = &repository.OperationState{Type: repository.OperationTypeMerge, Fingerprint: "m"}
	recovery := repository.FileRecoveryRecord{
		OperationID: "00000000000000000000000000000009", Path: "conflict.txt", CreatedAt: 200,
		Status: repository.FileRecoveryReady, Progress: repository.FileRecoveryPending,
		Initial: repository.FileRecoveryState{Index: repository.FileRecoveryIndex{Stage1: repository.FileRecoveryIndexEntry{Present: true}}},
	}
	a.snapshot.FileRecoveries = []repository.FileRecoveryRecord{recovery}
	older := indexRecord("00000000000000000000000000000001", "Stage old.txt", "head-now")
	older.CreatedAt = 100
	a.snapshot.Operations = []repository.OperationRecord{older}

	a.offerUndo()
	if a.confirm == nil || a.confirm.message != "conflict.txt" || !strings.Contains(a.confirm.title, "Conflict") {
		t.Fatalf("a newer conflict resolution should be undone first: %#v", a.confirm)
	}

	a.confirm = nil
	newer := indexRecord("00000000000000000000000000000002", "Stage newer.txt", "head-now")
	newer.CreatedAt = 300
	a.snapshot.Operations = []repository.OperationRecord{newer, older}
	a.offerUndo()
	if a.confirm == nil || !strings.Contains(a.confirm.message, "Stage newer.txt") {
		t.Fatalf("a newer stage should be undone first: %#v", a.confirm)
	}

	a.confirm = nil
	a.snapshot.Operations = nil
	a.snapshot.OperationState = nil
	a.offerUndo()
	if a.confirm != nil && a.confirm.message == "conflict.txt" {
		t.Fatal("a conflict resolution is offered after its operation ended")
	}
}

func TestRealAppCtrlZPutsAResolvedConflictBack(t *testing.T) {
	dir, session := newAddAddConflict(t)
	path := "conflict.txt"
	originalFile := readRecoveryFile(t, dir, path)
	originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path)
	h := newFileRecoveryApp(t, dir, session)
	h.app.confirm = nil
	if err := h.tester.Click("Use current " + path); err != nil {
		t.Fatal(err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "resolved", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && !hasConflictedFiles(h.app.snapshot.Files) && len(h.app.snapshot.FileRecoveries) == 1
	})

	h.tester.Key(ui.Ctrl, ui.KeyZ)
	h.waitFor(t, "Ctrl+Z dialog", func() bool { return h.app.confirm != nil })
	if h.app.confirm == nil || h.app.confirm.message != path {
		t.Fatalf("Ctrl+Z after Use current = %#v, want the file's restore dialog", h.app.confirm)
	}
	h.tester.Frame()
	if err := h.tester.Click("Restore unresolved"); err != nil {
		t.Fatalf("confirm: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "conflict restored", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && hasConflictedFiles(h.app.snapshot.Files)
	})
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("restored bytes = %q", got)
	}
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path); got != originalIndex {
		t.Fatalf("restored index = %q", got)
	}
}

// Discarding is the change a user most fears losing: Ctrl+Z must bring the
// file back exactly, tracked edit or untracked file alike.
func TestRealAppCtrlZBringsBackADiscardedFile(t *testing.T) {
	for _, path := range []string{"a.txt", "new.txt"} {
		t.Run(path, func(t *testing.T) {
			dir, session := newStagingRepo(t)
			before := runGitIn(t, dir, "status", "--porcelain=v1")
			content, _ := os.ReadFile(filepath.Join(dir, path))
			h := newFileRecoveryApp(t, dir, session)
			h.app.confirm = nil
			if path != "a.txt" {
				if err := h.tester.Click(path); err != nil {
					t.Fatal(err)
				}
				h.settle(t)
				h.tester.Frame()
				h.waitFor(t, path+" chosen", func() bool {
					change, ok := h.app.selectedChange()
					return ok && change.file.Path == path
				})
				h.tester.Frame()
			}
			if err := h.tester.Click("Discard " + path); err != nil {
				t.Fatalf("Discard: %v; texts=%q", err, h.texts())
			}
			h.waitFor(t, "discard confirmation", func() bool { return h.app.confirm != nil })
			h.tester.Frame()
			if err := h.tester.Click("Discard"); err != nil {
				t.Fatalf("confirm Discard: %v", err)
			}
			h.waitFor(t, "file discarded", func() bool {
				if h.app.actionBusy {
					return false
				}
				for _, file := range h.app.snapshot.Files {
					if file.Path == path {
						return false
					}
				}
				return true
			})
			if _, err := os.Stat(filepath.Join(dir, path)); path == "new.txt" && err == nil {
				t.Fatal("untracked file was not discarded")
			}

			h.tester.Key(ui.Ctrl, ui.KeyZ)
			h.waitFor(t, "Ctrl+Z dialog", func() bool { return h.app.confirm != nil })
			want := trf("Undo %s?", "Discard "+path)
			if h.app.confirm == nil || h.app.confirm.message != want {
				t.Fatalf("Ctrl+Z dialog = %#v, want %q", h.app.confirm, want)
			}
			h.tester.Frame()
			if err := h.tester.Click("Undo"); err != nil {
				t.Fatalf("confirm Undo: %v", err)
			}
			h.waitFor(t, "file back", func() bool {
				if h.app.actionBusy {
					return false
				}
				got, err := os.ReadFile(filepath.Join(dir, path))
				return err == nil && bytes.Equal(got, content)
			})
			if got := runGitIn(t, dir, "status", "--porcelain=v1"); got != before {
				t.Fatalf("status after Ctrl+Z = %q, want %q", got, before)
			}
		})
	}
}

func TestCtrlZNamesEverySmallKindOfChange(t *testing.T) {
	for _, test := range []struct {
		kind, undo, summary, detail string
	}{
		{repository.OperationStage, repository.UndoRestoreIndex, "Stage selected changes", "files are not changed"},
		{repository.OperationUnstage, repository.UndoRestoreIndex, "Unstage selected changes", "files are not changed"},
		{repository.OperationIgnore, repository.UndoRestoreFile, "Ignore new.txt", "Only this one change"},
		{repository.OperationExclude, repository.UndoRestoreFile, "Exclude new.txt", "Only this one change"},
		{repository.OperationTagCreate, repository.UndoRestoreRef, "Create tag v1", "Only this one change"},
		{repository.OperationTagDelete, repository.UndoRestoreRef, "Delete tag v1", "Only this one change"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			a := undoApp(t)
			record := indexRecord("00000000000000000000000000000001", test.summary, "head-now")
			record.Kind, record.UndoKind = test.kind, test.undo
			a.snapshot.Operations = []repository.OperationRecord{record}
			a.offerUndo()
			if a.confirm == nil || a.confirm.message != trf("Undo %s?", test.summary) {
				t.Fatalf("dialog = %#v, want it to name %q", a.confirm, test.summary)
			}
			if !strings.Contains(a.confirm.detail, test.detail) {
				t.Fatalf("detail %q does not explain the scope (%q)", a.confirm.detail, test.detail)
			}
		})
	}
}

// Staging says so, and the notice carries an Undo button that opens the same
// dialog as Ctrl+Z, so the shortcut does not have to be discovered.
func TestStageNoticeOffersUndoThatOpensTheCtrlZDialog(t *testing.T) {
	dir, session := newStagingRepo(t)
	h := newFileRecoveryApp(t, dir, session)
	h.app.confirm = nil
	if err := h.tester.Click("Stage a.txt"); err != nil {
		t.Fatal(err)
	}
	label := tr("Undo") + " (Ctrl+Z)"
	h.waitFor(t, "the Staged notice with its Undo button", func() bool {
		h.tester.Frame()
		return slices.Contains(h.texts(), trf("Staged %s", "a.txt")) && slices.Contains(h.texts(), label)
	})
	if err := h.tester.Click(label); err != nil {
		t.Fatalf("notice Undo: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "the undo dialog", func() bool { return h.app.confirm != nil })
	if want := trf("Undo %s?", "Stage a.txt"); h.app.confirm.message != want {
		t.Fatalf("dialog = %q, want %q", h.app.confirm.message, want)
	}
}

func TestNoUndoNoticeWhileTheSafetyJournalIsOff(t *testing.T) {
	a := undoApp(t)
	enabled := repository.SafetyJournalEnabled()
	repository.SetSafetyJournalEnabled(false)
	defer repository.SetSafetyJournalEnabled(enabled)
	a.notifyUndoable("Staged x")
	if len(a.pendingToasts) != 0 {
		t.Fatalf("a notice offered Undo for a change that was not recorded: %#v", a.pendingToasts)
	}
}
