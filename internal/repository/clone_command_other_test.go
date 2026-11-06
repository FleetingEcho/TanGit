//go:build !windows

package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCloneCancellationKillsProcessGroupAndReleasesOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	marker := filepath.Join(dir, "survived")
	command := exec.CommandContext(ctx, "sh", "-c", `(sleep 1; echo survived > "$2") & echo $! > "$1"; wait`, "sh", pidFile, marker)
	configureCloneCommand(command)
	command.WaitDelay = 250 * time.Millisecond
	// A non-file writer makes os/exec copy through an internal pipe, matching
	// the clone diagnostic path where an inherited descriptor can block Wait.
	command.Stderr = &boundedDiagnostic{limit: 1024}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled process group returned success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation remained blocked on a descendant's output pipe")
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a clone descendant continued writing after cancellation: %v", err)
	}
}
