package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func realCommandHarness(t *testing.T) (*app, *ui.Tester, *updates) {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main", root},
		{"-C", root, "config", "user.name", "Log Test"},
		{"-C", root, "config", "user.email", "log@example.com"},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", root, "add", "--", "file.txt").CombinedOutput(); err != nil {
		t.Fatalf("stage seed file: %v %s", err, output)
	}
	if output, err := exec.Command("git", "-C", root, "commit", "-qm", "initial", "--", "file.txt").CombinedOutput(); err != nil {
		t.Fatalf("seed commit: %v %s", err, output)
	}
	// newLiveShell assigns the queued update to every page it opens, so a
	// background process cannot mutate app state while a frame builds.
	s, tester, updated := newLiveShell(t, repository.Opener{})
	a := openLiveRepo(t, s, updated, root)
	for a.repositoryDetailsLoading {
		updated.waitForUpdate(t)
	}
	tester.Frame()
	// These are real sessions, not stateless fake snapshots. Close their
	// watcher, command-log listener and child processes with the test.
	t.Cleanup(s.shutdown)
	return a, tester, updated
}

func waitCommandComplete(t *testing.T, a *app, tester *ui.Tester, updated *updates, command string) {
	t.Helper()
	for range 12 {
		for _, entry := range a.commandLogEntries() {
			if entry.Command == command && !entry.Running {
				return
			}
		}
		updated.waitForUpdate(t)
		tester.Frame()
	}
	t.Fatal("command did not complete")
}

func TestCustomCommandWritesRealSessionLog(t *testing.T) {
	a, tester, updated := realCommandHarness(t)
	root := a.snapshot.Root
	if err := os.WriteFile(filepath.Join(root, "untitled.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.runCustomCommand("git status --short", "status")
	waitCommandComplete(t, a, tester, updated, "git status --short")
	entries := a.commandLogEntries()
	found := false
	for _, entry := range entries {
		if !entry.Running && strings.Contains(entry.Command, "git status --short") && strings.Contains(entry.Output, "untitled.txt") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("custom command or output is missing: %+v", entries)
	}
	a.openCommandLog()
	tester.Frame()
	if !tester.HasText("$ git status --short") {
		t.Fatalf("command log does not show custom command: %q", tester.Texts())
	}
}

// Opening the command log while a command is running must still show it.
// A completed-only entry leaves the visible log looking stale throughout
// long foreground and background commands.
func TestRunningCommandAppearsInOpenLogBeforeCompletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell gate")
	}
	a, tester, updated := realCommandHarness(t)
	root := a.snapshot.Root
	started := filepath.Join(root, "started")
	release := filepath.Join(root, "release")
	command := "touch " + shellQuote(started) + "; while [ ! -f " + shellQuote(release) + " ]; do sleep 0.02; done; printf complete"
	a.openCommandLog()
	tester.Frame()

	a.runCustomCommand(command, "background check")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("gated command did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	defer os.WriteFile(release, []byte("done"), 0o644)
	updated.waitForUpdate(t)
	tester.Frame()
	if !tester.HasText("$ " + command) {
		t.Fatalf("running command is absent from the open log: %q", tester.Texts())
	}
	if !tester.HasText("running") {
		t.Fatalf("command is not visibly marked as running: %q", tester.Texts())
	}
	if err := os.WriteFile(release, []byte("done"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitCommandComplete(t, a, tester, updated, command)
	tester.Frame()
	if tester.HasText("running") {
		t.Fatalf("command is still marked running after completion: %q", tester.Texts())
	}
}
