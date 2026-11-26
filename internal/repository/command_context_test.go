package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestRepositoryCommandContextHelper(t *testing.T) {
	if os.Getenv("LAZYGITGO_REPOSITORY_CONTEXT_HELPER") != "1" {
		return
	}
	fmt.Fprint(os.Stdout, "started")
	time.Sleep(30 * time.Second)
}

func TestRunWithOutputContextStopsChildProcess(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	concrete := session.(*gitSession)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	command, err := concrete.commandContext(ctx, []string{os.Args[0], "-test.run=^TestRepositoryCommandContextHelper$"})
	if err != nil {
		t.Fatal(err)
	}
	command.AddEnvVars("LAZYGITGO_REPOSITORY_CONTEXT_HELPER=1")
	started := time.Now()
	output, err := concrete.runWithOutputContext(ctx, command)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context command output = %q, error = %v", output, err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("canceled child kept running for %v; captured output %q", elapsed, output)
	}
}
