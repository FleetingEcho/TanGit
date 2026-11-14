package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/repository"
)

func TestRepositoryShutdownCancelsCustomCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX command loop")
	}
	a, _, _ := realCommandHarness(t)
	root := a.snapshot.Root
	started := filepath.Join(root, "job-started")
	release := filepath.Join(root, "job-release")
	command := "touch " + shellQuote(started) + "; while [ ! -f " + shellQuote(release) + " ]; do sleep 0.02; done"
	a.openCommandLog()
	log := a.commandLog
	a.runCustomCommand(command, "long-running check")
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("custom process never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Even if cancellation were broken, this would release the process in a
	// future test failure. We never write it: the kill is what must finish.
	t.Cleanup(func() { _ = os.WriteFile(release, []byte("done"), 0o644) })
	a.shutdown()

	requireCustomCommandJoined(t, log, "touch ")
}

// requireCustomCommandJoined deliberately does not poll: shutdown must have
// joined the command before returning, not merely posted its cancellation.
func requireCustomCommandJoined(t *testing.T, log *repository.CommandLog, prefix string) {
	t.Helper()
	for _, entry := range log.Entries() {
		if !strings.HasPrefix(entry.Command, prefix) {
			continue
		}
		if entry.Running {
			t.Fatalf("shutdown returned with running command: %+v", entry)
		}
		return
	}
	t.Fatal("shutdown returned before recording the completed custom command")
}
