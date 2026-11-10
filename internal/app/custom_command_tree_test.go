package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestShutdownStopsCustomCommandDescendants(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("verifies process-group and /proc state on Linux")
	}
	a, _, _ := realCommandHarness(t)
	root := a.snapshot.Root
	pidPath := filepath.Join(root, "child.pid")
	// The child's stdout remains attached to the pipeline. Killing only the
	// immediate shell would leave this process visible and holding descriptors.
	command := "sleep 30 & echo $! > " + shellQuote(pidPath) + "; wait"
	log := a.commandLog
	a.runCustomCommand(command, "tree cancellation")
	var childPID int
	deadline := time.Now().Add(3 * time.Second)
	for {
		if content, err := os.ReadFile(pidPath); err == nil {
			value, parseErr := strconv.Atoi(strings.TrimSpace(string(content)))
			if parseErr == nil && value > 0 {
				childPID = value
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("background descendant never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// If a regression regresses process-group cancellation, do not leave the
	// descendant alive after the test finishes. Do not write it on success:
	// the correct group cancellation must handle it without external help.
	t.Cleanup(func() {
		if childPID > 0 {
			if process, err := os.FindProcess(childPID); err == nil {
				_ = process.Kill()
			}
		}
	})
	a.shutdown()
	requireCustomCommandJoined(t, log, "sleep 30")

	deadline = time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(childPID), "stat"))
		if os.IsNotExist(err) {
			return
		}
		if err == nil && strings.Contains(string(data), ") Z") {
			return // Reparented to init and awaiting ordinary reap; already dead.
		}
		if err == nil {
			t.Fatal("custom command descendant survived repository shutdown")
		}
		if time.Now().After(deadline) {
			t.Fatalf("cannot inspect cancelled descendant %d: %v", childPID, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
