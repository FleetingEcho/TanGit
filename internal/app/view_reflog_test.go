package app

import (
	"testing"
	"time"

	"lazygitapp/internal/repository"
)

// Reflog can contain several identical reset entries recorded in the same
// second. They must still be separate list rows: sharing a key also shares
// hover and pressed state, which lights every duplicate at once.
func TestDuplicateReflogEntriesHaveDistinctRowIdentity(t *testing.T) {
	a := newApp(nil)
	entry := repository.Commit{
		Hash:          "c330bf9f",
		ShortHash:     "c330bf9f",
		Subject:       "reset: moving to HEAD",
		UnixTimestamp: 1_700_000_000,
	}
	a.snapshot.Reflog = []repository.Commit{entry, entry}

	first := a.reflogList.Key(0)
	second := a.reflogList.Key(1)
	if first == second {
		t.Fatalf("duplicate reflog rows share identity %v, so their hover state is shared", first)
	}
}

func reflogHarness(t *testing.T) (*harness, *callRecorder) {
	t.Helper()
	h, calls := menuTestAppWithCalls(t)
	h.app.snapshot.Reflog = []repository.Commit{
		{Hash: "feedface", ShortHash: "feedfac", Subject: "reset: moving to HEAD", UnixTimestamp: time.Now().Add(-time.Hour).Unix()},
		{Hash: "cafebabe", ShortHash: "cafebab", Subject: "checkout: moving to main", UnixTimestamp: time.Now().Add(-2 * time.Hour).Unix()},
	}
	h.app.selectedReflog = 0
	h.app.workspaceTab = sectionReflog
	h.tester.Frame()
	return h, calls
}

func TestReflogMenuOffersRecoveryActions(t *testing.T) {
	h, _ := reflogHarness(t)
	h.rightClick(t, "reset: moving to HEAD")
	h.requireMenuLabels(t,
		"Copy commit hash",
		"Checkout commit (detached)…",
		"Create a branch here…",
		"Soft reset here…",
		"Mixed reset here…",
		"Hard reset here…",
	)
}

func TestReflogCheckoutConfirmsAndDetaches(t *testing.T) {
	h, calls := reflogHarness(t)
	h.rightClick(t, "reset: moving to HEAD")
	h.chooseMenuItem(t, "Checkout commit (detached)…")
	if h.app.confirm == nil {
		t.Fatal("detached checkout asked no confirmation")
	}
	if err := h.tester.Click("Checkout commit"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the reflog commit to be checked out", func() bool {
		return calls.has("checkout-commit:feedface")
	})
}

func TestReflogCanCreateBranchAtEntry(t *testing.T) {
	h, calls := reflogHarness(t)
	h.rightClick(t, "checkout: moving to main")
	h.chooseMenuItem(t, "Create a branch here…")
	if !h.app.showCommitBranchForm || h.app.commitBranchCommit.Hash != "cafebabe" {
		t.Fatalf("branch form targets %#v, want cafebabe", h.app.commitBranchCommit)
	}
	h.app.commitBranchName = "recovered"
	h.tester.Frame()
	if err := h.tester.Click("Create and checkout"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Create branch"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the recovery branch to be created", func() bool {
		return calls.has("branch:recovered:cafebabe") && calls.has("checkout:recovered")
	})
}

func TestReflogMixedResetUsesSelectedEntry(t *testing.T) {
	h, calls := reflogHarness(t)
	if err := h.tester.Click("Mixed reset to reflog commit"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("mixed reset asked no confirmation")
	}
	if err := h.tester.Click("Mixed reset"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the branch to be reset", func() bool {
		return calls.has("reset-mixed:feedface")
	})
}
