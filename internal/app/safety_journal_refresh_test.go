package app

import (
	"testing"

	"lazygitapp/internal/repository"
)

func TestSnapshotSignaturesTrackJournalAndRecoveryOnlyChanges(t *testing.T) {
	base := repository.Snapshot{Root: "/work/repo", Branch: "main"}
	journalChanged := base
	journalChanged.Operations = []repository.OperationRecord{
		recoverableJournalRecord("operation-1", "Discard tracked edits"),
	}
	if signSnapshot(base).operations == signSnapshot(journalChanged).operations {
		t.Fatal("adding a journal entry did not change the quiet refresh signature")
	}
	if got, want := signSnapshot(journalChanged), signSnapshot(journalChanged); got != want {
		t.Fatalf("unchanged durable journal state is unstable: %#v != %#v", got, want)
	}

	statusChanged := journalChanged
	statusChanged.Operations = append([]repository.OperationRecord(nil), journalChanged.Operations...)
	statusChanged.Operations[0].UndoneAt = 1_700_000_100
	if signSnapshot(journalChanged).operations == signSnapshot(statusChanged).operations {
		t.Fatal("journal-only status change did not change the quiet refresh signature")
	}
	if signSnapshot(base).recovery != signSnapshot(journalChanged).recovery {
		t.Fatal("journal-only change also changed the recovery signature")
	}

	recoveryChanged := base
	recoveryChanged.WorkSnapshots = []repository.WorkSnapshot{{
		ID: "snapshot-1", Hash: "abc123", Kind: repository.RecoverySnapshotKind,
		Name: "Before discard", CreatedAt: 1_700_000_000,
	}}
	if signSnapshot(base).recovery == signSnapshot(recoveryChanged).recovery {
		t.Fatal("adding saved work did not change the quiet refresh signature")
	}
	if got, want := signSnapshot(recoveryChanged), signSnapshot(recoveryChanged); got != want {
		t.Fatalf("unchanged recovery state is unstable: %#v != %#v", got, want)
	}

	nameChanged := recoveryChanged
	nameChanged.WorkSnapshots = append([]repository.WorkSnapshot(nil), recoveryChanged.WorkSnapshots...)
	nameChanged.WorkSnapshots[0].Name = "Before reset"
	if signSnapshot(recoveryChanged).recovery == signSnapshot(nameChanged).recovery {
		t.Fatal("recovery-only metadata change did not change the quiet refresh signature")
	}
	if signSnapshot(base).operations != signSnapshot(recoveryChanged).operations {
		t.Fatal("recovery-only change also changed the journal signature")
	}
}

func TestQuietRefreshAppliesJournalAndRecoveryOnlyChanges(t *testing.T) {
	base := repository.Snapshot{Root: "/work/repo", Branch: "main"}
	opener := &liveOpener{snapshot: base}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, "/work/repo")

	journalChanged := base
	journalChanged.Operations = []repository.OperationRecord{
		recoverableJournalRecord("operation-1", "Discard tracked edits"),
	}
	opener.set(journalChanged)
	a.requestQuietRefresh()
	drainRefresh(t, updated, 1)
	if len(a.snapshot.Operations) != 1 || a.snapshot.Operations[0].ID != "operation-1" {
		t.Fatalf("quiet refresh discarded a journal-only change: %#v", a.snapshot.Operations)
	}

	recoveryChanged := journalChanged
	recoveryChanged.WorkSnapshots = []repository.WorkSnapshot{{
		ID: "snapshot-1", Hash: "abc123", Kind: repository.RecoverySnapshotKind,
		Name: "Before discard", CreatedAt: 1_700_000_000,
	}}
	opener.set(recoveryChanged)
	a.requestQuietRefresh()
	drainRefresh(t, updated, 1)
	if len(a.snapshot.WorkSnapshots) != 1 || a.snapshot.WorkSnapshots[0].ID != "snapshot-1" {
		t.Fatalf("quiet refresh discarded a recovery-only change: %#v", a.snapshot.WorkSnapshots)
	}
}
