package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// safetyJournalSession lets an action add records to the snapshot it returns,
// as the real session does after writing its immutable journal entry.
type safetyJournalSession struct {
	fakeSession
	addedOperations []repository.OperationRecord
}

func (s *safetyJournalSession) Snapshot(context.Context) (repository.Snapshot, error) {
	snapshot := s.fakeSession.snapshot
	snapshot.Operations = append([]repository.OperationRecord(nil), snapshot.Operations...)
	snapshot.Operations = append(snapshot.Operations, s.addedOperations...)
	return snapshot, nil
}

func safetyJournalTestApp(t *testing.T, existing ...repository.OperationRecord) (*harness, *callRecorder, *safetyJournalSession) {
	t.Helper()
	calls := &callRecorder{}
	session := &safetyJournalSession{fakeSession: fakeSession{
		snapshot: repository.Snapshot{
			Root:       "/work/project",
			Branch:     "main",
			Operations: existing,
		},
		calls: calls,
	}}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, calls, session
}

func recoverableJournalRecord(id, summary string) repository.OperationRecord {
	return repository.OperationRecord{
		ID:        id,
		Summary:   summary,
		Status:    repository.OperationSucceeded,
		CreatedAt: 1_700_000_000,
		UndoKind:  repository.UndoRestoreWork,
	}
}

// waitForSafetyJournalToast drains only app updates, without building a frame
// that would move the pending notification into MyGo's toast viewport.
func waitForSafetyJournalToast(t *testing.T, h *harness) {
	t.Helper()
	for range 8 {
		if len(h.app.pendingToasts) > 0 {
			return
		}
		h.updates.waitForUpdate(t)
	}
	if len(h.app.pendingToasts) == 0 {
		t.Fatal("timed out waiting for the action notification")
	}
}

func TestRepositorySuccessUndoUsesNewSafetyJournalID(t *testing.T) {
	oldRecord := recoverableJournalRecord("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "Older discard")
	newRecord := recoverableJournalRecord("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "Discard two.txt")
	h, calls, session := safetyJournalTestApp(t, oldRecord)

	h.app.beginRepositoryActionMessage("Discarded two.txt", "", func(context.Context, repository.Session) error {
		session.addedOperations = append(session.addedOperations,
			newRecord,
			repository.OperationRecord{
				ID:      "cccccccccccccccccccccccccccccccc",
				Summary: "Completed without recovery",
				Status:  repository.OperationSucceeded,
			},
		)
		return nil
	}, nil)
	waitForSafetyJournalToast(t, h)

	if len(h.app.pendingToasts) != 1 {
		t.Fatalf("success notifications = %d, want 1", len(h.app.pendingToasts))
	}
	toast := h.app.pendingToasts[0]
	if toast.kind != "success" || toast.title != "Discarded two.txt" || toast.action != "Undo" || toast.onAction == nil {
		t.Fatalf("new journal record did not produce an Undo notification: %#v", toast)
	}

	// Exercise the actual toast action. It must ask first, then pass the new
	// immutable ID through unchanged rather than replaying the latest reflog.
	h.tester.Frame()
	if err := h.tester.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.message != "Undo Discard two.txt?" {
		t.Fatalf("Undo did not confirm the new journal record: %#v", h.app.confirm)
	}
	if calls.has("undo-operation:"+oldRecord.ID) || calls.has("undo-operation:"+newRecord.ID) {
		t.Fatal("Undo called the repository before confirmation")
	}
	if err := h.tester.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "Undo of the exact journal record", func() bool {
		return calls.has("undo-operation:" + newRecord.ID)
	})
	if calls.has("undo-operation:" + oldRecord.ID) {
		t.Fatal("Undo used the older record instead of the newly journaled ID")
	}
}

func TestRepositorySuccessWithoutNewJournalRecordHasNoUndo(t *testing.T) {
	h, _, _ := safetyJournalTestApp(t)

	h.app.beginRepositoryActionMessage("Pushed main", "", func(context.Context, repository.Session) error {
		return nil
	}, nil)
	waitForSafetyJournalToast(t, h)

	if len(h.app.pendingToasts) != 1 {
		t.Fatalf("success notifications = %d, want 1", len(h.app.pendingToasts))
	}
	toast := h.app.pendingToasts[0]
	if toast.kind != "success" || toast.title != "Pushed main" || toast.description != "" {
		t.Fatalf("ordinary action did not produce its normal success notification: %#v", toast)
	}
	if toast.action != "" || toast.onAction != nil {
		t.Fatalf("success without a new journal record offered Undo: %#v", toast)
	}
}

func TestSafetyJournalRendersRecordsAndLimitsUndoToRecoverableRows(t *testing.T) {
	h := menuTestApp(t)
	records := []repository.OperationRecord{
		recoverableJournalRecord("recoverable", "Discard tracked edits"),
		{
			ID:        "completed",
			Summary:   "Create a checkpoint",
			Status:    repository.OperationSucceeded,
			CreatedAt: 1_700_000_100,
		},
		{
			ID:        "failed",
			Summary:   "Apply the selected patch",
			Status:    repository.OperationFailed,
			CreatedAt: 1_700_000_200,
			UndoKind:  repository.UndoReversePatch,
			Error:     "patch does not apply",
		},
		{
			ID:        "undone",
			Summary:   "Reset main",
			Status:    repository.OperationSucceeded,
			CreatedAt: 1_700_000_300,
			UndoneAt:  1_700_000_400,
			UndoKind:  repository.UndoResetHead,
		},
	}
	h.app.snapshot.Operations = records
	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	if err := h.tester.Click("Show safety journal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()

	for _, want := range []string{
		"Discard tracked edits", "Create a checkpoint", "Apply the selected patch", "Reset main",
		"Recoverable", "Completed", "Failed", "Undone", "patch does not apply", "4 journal entries",
	} {
		if !h.tester.HasText(want) {
			t.Errorf("Safety Journal is missing %q: %q", want, h.texts())
		}
	}
	undoButtons := 0
	for _, text := range h.texts() {
		if text == "Undo…" {
			undoButtons++
		}
	}
	if undoButtons != 1 {
		t.Fatalf("journal Undo buttons = %d, want only the recoverable row: %q", undoButtons, h.texts())
	}
	for _, id := range []string{"completed", "failed", "undone"} {
		if err := h.tester.Click("Undo " + id); err == nil {
			t.Errorf("non-recoverable record %q unexpectedly offers Undo", id)
		}
	}
	if err := h.tester.Click("Undo recoverable"); err != nil {
		t.Fatalf("recoverable record offers no Undo: %v", err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.message != "Undo Discard tracked edits?" {
		t.Fatalf("journal Undo did not confirm its own row: %#v", h.app.confirm)
	}
}

func TestSafetyJournalScrollsToOlderRecoverableRecords(t *testing.T) {
	h := menuTestApp(t)
	records := make([]repository.OperationRecord, 0, 7)
	for i := range 6 {
		records = append(records, recoverableJournalRecord(fmt.Sprintf("recent-%d", i), fmt.Sprintf("Recent operation %d", i)))
	}
	records = append(records, recoverableJournalRecord("oldest", "Oldest recoverable operation"))
	h.app.snapshot.Operations = records
	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	if err := h.tester.Click("Show safety journal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()

	if !h.tester.HasText("Oldest recoverable operation") || !h.tester.HasText("7 journal entries") {
		t.Fatalf("journal omitted its older durable records: %q", h.texts())
	}
	first, ok := h.tester.Find("Recent operation 0")
	if !ok {
		t.Fatalf("journal does not show its newest row: %q", h.texts())
	}
	x, y := first.X+first.W/2, first.Y+first.H/2
	h.tester.Move(x, y)
	h.tester.Frame()
	h.tester.Scroll(x, y, 0, 600)
	h.tester.Frame()
	scrolled, ok := h.tester.Find("Recent operation 0")
	if !ok || scrolled.Y >= first.Y {
		t.Fatalf("journal did not scroll toward older records: before=%v after=%v", first, scrolled)
	}
	if err := h.tester.Click("Undo oldest"); err != nil {
		t.Fatalf("older recoverable record offers no accessible Undo: %v", err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.message != "Undo Oldest recoverable operation?" {
		t.Fatalf("older journal Undo did not confirm its own row: %#v", h.app.confirm)
	}
}

func TestUndoOperationNotificationWithStaleIDDoesNotCallRepository(t *testing.T) {
	record := recoverableJournalRecord("dddddddddddddddddddddddddddddddd", "Discard stale.txt")
	h, calls, session := safetyJournalTestApp(t)
	h.app.beginRepositoryActionMessage("Discarded stale.txt", "", func(context.Context, repository.Session) error {
		session.addedOperations = append(session.addedOperations, record)
		return nil
	}, nil)
	waitForSafetyJournalToast(t, h)
	h.tester.Frame()

	// The notification retains the immutable ID, but a refresh can remove the
	// record before the user clicks. That stale action must not call Undo.
	h.app.snapshot.Operations = nil
	if err := h.tester.Click("Undo"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm != nil {
		t.Fatal("stale journal ID opened an Undo confirmation")
	}
	if !h.tester.HasText("Undo is unavailable") {
		t.Fatalf("stale journal ID did not explain that Undo is unavailable: %q", h.texts())
	}
	if calls.has("undo-operation:" + record.ID) {
		t.Fatal("stale journal ID still called the repository")
	}
}
