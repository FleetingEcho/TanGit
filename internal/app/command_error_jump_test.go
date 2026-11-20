package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"lazygitapp/internal/repository"
)

func TestClickingStatusErrorOpensAndScrollsToFailedCommand(t *testing.T) {
	h := menuTestApp(t)
	h.app.attachRepositoryCommandLog(&repository.CommandLog{})
	const command = "git push origin main:refs/heads/main --force-with-lease"
	h.app.beginRepositoryAction("", func(context.Context, repository.Session) error {
		done := h.app.commandLog.Begin(command)
		done("example diagnostic output", false)
		return errors.New("fatal: push failed for this test")
	}, nil)
	h.waitFor(t, "the action's failed status", func() bool {
		return h.app.actionError != "" && !h.app.actionBusy
	})
	var expectedID uint64
	for _, entry := range h.app.commandLog.Entries() {
		if entry.Command == command {
			expectedID = entry.ID()
		}
	}
	if expectedID == 0 {
		t.Fatal("the failed command has no stable identity")
	}
	if h.app.actionFailure == nil || h.app.actionFailure.id != expectedID || h.app.actionFailure.log != h.app.commandLog {
		t.Fatalf("failure is not linked to its exact log record: expected=%d target=%+v", expectedID, h.app.actionFailure)
	}
	target := *h.app.actionFailure
	for range 35 { // Close this region only via Scrolling.
		done := h.app.commandLog.Begin(fmt.Sprintf("git status %d", len(h.app.commandLog.Entries())))
		done("", false)
	}
	h.tester.Frame()
	if err := h.tester.Click(statusErrorLine(h.app.actionError)); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.commandLogOpen {
		t.Fatal("clicking the error text did not open the command log")
	}
	if !h.tester.HasText("$ " + command) {
		t.Fatalf("targeted failed command is not visible after navigation: %q", h.texts())
	}
	if h.app.commandLogScroll.Y <= 0 {
		t.Fatalf("failed command remained offscreen: scrollY=%v maxY=%v", h.app.commandLogScroll.Y, h.app.commandLogScroll.MaxY)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "status-error-command-log", h.tester)
	}

	// A new command cannot inherit the failed entry's identity after clearing.
	h.app.commandLog.Clear()
	h.app.commandLog.Begin("git status")("", false)
	h.app.openFailedCommand(&target)
	h.tester.Frame()
	if h.app.commandLogReveal != nil || h.tester.HasText("$ "+command) {
		t.Fatal("an evicted or reused command ID selected a different row")
	}
}
