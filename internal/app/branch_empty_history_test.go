package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"lazygitapp/internal/repository"
)

type branchHistoryTestSession struct {
	repository.Session
	err error
}

func (s branchHistoryTestSession) BranchCommits(context.Context, string, int) ([]repository.Commit, error) {
	return nil, s.err
}
func (s branchHistoryTestSession) SearchCommits(context.Context, repository.CommitFilter) ([]repository.Commit, error) {
	return nil, s.err
}

func TestBranchEmptyHistoryStaysUsable(t *testing.T) {
	h := branchPageHarness(t)
	h.app.session = branchHistoryTestSession{Session: h.app.session}
	h.app.resetBranchCommits()
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "empty branch history", func() bool { return !h.app.branchCommitsLoading })
	for i := 0; i < 3; i++ {
		h.tester.Frame()
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "branch-empty-history", h.tester)
	}
	if h.app.crash != "" || h.app.branchCommitsError != "" || !h.tester.HasText("No commits on this branch yet.") {
		t.Fatalf("empty branch did not render safely: crash=%q err=%q text=%v", h.app.crash, h.app.branchCommitsError, h.texts())
	}
}

func TestBranchHistoryFailureNotifiesOnce(t *testing.T) {
	h := branchPageHarness(t)
	h.app.session = branchHistoryTestSession{Session: h.app.session, err: errors.New("cannot read branch history")}
	h.app.resetBranchCommits()
	before := h.app.toastSeq
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "failed branch history", func() bool { return !h.app.branchCommitsLoading })
	for i := 0; i < 3; i++ {
		h.tester.Frame()
	}
	if h.app.crash != "" || !h.tester.HasText("Could not load branch commits") || h.app.toastSeq != before+1 {
		t.Fatalf("history failure did not notify once: crash=%q toasts=%d text=%v", h.app.crash, h.app.toastSeq-before, h.texts())
	}
	if !h.tester.HasText("Load again") {
		t.Fatal("retry action missing")
	}
}
