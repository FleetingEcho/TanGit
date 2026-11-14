package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"lazygitapp/internal/repository"
)

func TestActionFailureLogFollowsPushNotLaterSnapshotReads(t *testing.T) {
	a, _, updated := realCommandHarness(t)
	root := a.snapshot.Root
	remote := filepath.Join(t.TempDir(), "origin.git")
	initRemote := exec.Command("git", "init", "--bare", remote)
	if output, err := initRemote.CombinedOutput(); err != nil {
		t.Fatalf("create bare origin: %v %s", err, output)
	}
	for _, args := range [][]string{
		{"remote", "add", "origin", remote},
		{"push", "-u", "origin", "main"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	hook := "#!/bin/sh\necho lint-marker-failed >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "hooks", "pre-push"), []byte(hook), 0o700); err != nil {
		t.Fatal(err)
	}

	a.beginRepositoryActionMessage("unexpectedly pushed", "",
		func(ctx context.Context, session repository.Session) error {
			return session.Push(ctx)
		}, nil)
	for i := 0; i < 12; i++ {
		if a.actionError != "" && !a.actionBusy {
			break
		}
		updated.waitForUpdate(t)
	}
	if !strings.Contains(a.actionError, "lint-marker-failed") {
		t.Fatalf("hook diagnostic disappeared: %q", a.actionError)
	}

	failedPush, failedSnapshots := 0, 0
	for _, entry := range a.commandLogEntries() {
		if strings.HasPrefix(entry.Command, "git push ") {
			if !entry.Failed || !strings.Contains(entry.Output, "lint-marker-failed") {
				t.Fatalf("push did not own the failure details: %+v", entry)
			}
			failedPush++
		}
		if strings.Contains(entry.Command, "rev-parse") && entry.Failed {
			failedSnapshots++
		}
	}
	if failedPush != 1 {
		t.Fatalf("got %d failed push entries, want 1", failedPush)
	}
	if failedSnapshots != 0 {
		t.Fatalf("%d post-error snapshot reads were incorrectly marked failed", failedSnapshots)
	}
}
