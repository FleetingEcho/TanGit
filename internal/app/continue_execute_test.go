package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"lazygitapp/internal/repository"
)

func TestContinueTrustedShellHelperExecutesRealLocalCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	root := t.TempDir()
	t.Setenv("CONTINUE_TRUST_TEST", "shell-expanded")
	output, err := runTrustedShellCommand(context.Background(), root,
		"printf '%s' \"$CONTINUE_TRUST_TEST\" > trusted-output.txt; printf 'stdout-visible'")
	if err != nil {
		t.Fatal(err)
	}
	if output != "stdout-visible" {
		t.Fatalf("combined trusted command output = %q", output)
	}
	content, err := os.ReadFile(filepath.Join(root, "trusted-output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "shell-expanded" {
		t.Fatalf("trusted command did not run through the user's shell in the repository root: %q", content)
	}

	output, err = runTrustedShellCommand(context.Background(), root, "printf '__IT_IS_SAFE__'; exit 7")
	if err == nil || output != "__IT_IS_SAFE__" {
		t.Fatalf("trusted helper hid exit status or reinterpreted output as approval: output=%q err=%v", output, err)
	}
}

func TestRealGitContinueDetectsLengthInvariantChangesBeforeDriver(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture")
	}
	a, tester, updated := realCommandHarness(t)
	h := &harness{app: a, tester: tester, updates: updated}
	root := a.snapshot.Root
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("checkout", "-b", "feature")
	write("feature\n")
	git("add", "file.txt")
	git("commit", "-qm", "feature change")
	git("checkout", "main")
	write("main\n")
	git("add", "file.txt")
	git("commit", "-qm", "main change")
	merge := exec.Command("git", "-C", root, "merge", "feature", "-m", "merge feature")
	if output, err := merge.CombinedOutput(); err == nil {
		t.Fatalf("fixture merge did not conflict: %s", output)
	}
	write("alpha\n")
	git("add", "file.txt")

	snapshot, err := a.session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a.snapshot = snapshot
	a.settings.ContinuePreflight.RequiredCommand = "printf 'bravo\\n' > file.txt; printf 'length invariant output'"
	// This deliberately uses the production runner: the command is a fully
	// trusted local shell command, not a sandbox or a simulated Git diff.
	a.continueTestRunner = nil
	a.confirmContinueOperation()
	waitContinueComplete(t, h)
	for _, want := range []string{"file.txt", "length invariant output", "Checkpoint: created"} {
		if !h.tester.HasText(want) {
			t.Errorf("real Continue review does not show %q: %q", want, h.texts())
		}
	}

	// Both edits preserve byte length and line count. The second happens after
	// the checkpoint and output review, so only the final source CAS can stop it.
	write("omega\n")
	a.finishContinueReview(false)
	h.waitFor(t, "the refused stale Continue", func() bool { return !a.actionBusy && a.actionError != "" })
	operations, ok := a.session.(repository.OperationSession)
	if !ok {
		t.Fatal("real repository session lacks OperationSession")
	}
	state, err := operations.OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Type != repository.OperationTypeMerge {
		t.Fatalf("same-length post-review edit reached the real Continue driver: %+v", state)
	}
	if !strings.Contains(a.actionError, "changed") {
		t.Fatalf("stale real Continue has no source-change error: %q", a.actionError)
	}
}
