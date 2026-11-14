package repository

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentTargetStatusLocalDivergence(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "base.txt", "base", "base")
	runGit(t, dir, "checkout", "-b", "agent")
	writeHistoryCommit(t, dir, "target-one.txt", "one", "target one")
	writeHistoryCommit(t, dir, "target-two.txt", "two", "target two")
	runGit(t, dir, "checkout", "main")
	writeHistoryCommit(t, dir, "base-one.txt", "one", "base one")
	writeHistoryCommit(t, dir, "base-two.txt", "two", "base two")
	writeHistoryCommit(t, dir, "base-three.txt", "three", "base three")
	runGit(t, dir, "checkout", "agent")

	want := AgentTargetStatus{
		HeadSHA: strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")),
		BaseSHA: strings.TrimSpace(gitOutput(t, dir, "rev-parse", "main")),
		Ahead:   2,
		Behind:  3,
	}
	got, err := session.AgentTargetStatus(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("AgentTargetStatus() = %+v, want %+v", got, want)
	}
}

func TestAgentTargetStatusLinkedWorktreeDoesNotSwitchMain(t *testing.T) {
	mainDir, _ := newHistoryEditingSession(t)
	writeHistoryCommit(t, mainDir, "base.txt", "base", "base")
	linkedDir := filepath.Join(t.TempDir(), "agent-worktree")
	runGit(t, mainDir, "worktree", "add", "-b", "agent", linkedDir, "main")
	target, err := (Opener{}).Open(context.Background(), linkedDir)
	if err != nil {
		t.Fatal(err)
	}

	writeHistoryCommit(t, linkedDir, "target.txt", "target", "target")
	writeHistoryCommit(t, mainDir, "main.txt", "main", "main")
	mainHeadBefore := strings.TrimSpace(gitOutput(t, mainDir, "rev-parse", "HEAD"))
	mainBranchBefore := strings.TrimSpace(gitOutput(t, mainDir, "symbolic-ref", "--short", "HEAD"))
	want := AgentTargetStatus{
		HeadSHA: strings.TrimSpace(gitOutput(t, linkedDir, "rev-parse", "HEAD")),
		BaseSHA: mainHeadBefore,
		Ahead:   1,
		Behind:  1,
	}

	got, err := target.AgentTargetStatus(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("AgentTargetStatus() = %+v, want %+v", got, want)
	}
	if branch := strings.TrimSpace(gitOutput(t, mainDir, "symbolic-ref", "--short", "HEAD")); branch != mainBranchBefore {
		t.Fatalf("main worktree branch = %q, want %q", branch, mainBranchBefore)
	}
	if head := strings.TrimSpace(gitOutput(t, mainDir, "rev-parse", "HEAD")); head != mainHeadBefore {
		t.Fatalf("main worktree HEAD = %q, want %q", head, mainHeadBefore)
	}
	if branch := strings.TrimSpace(gitOutput(t, linkedDir, "symbolic-ref", "--short", "HEAD")); branch != "agent" {
		t.Fatalf("linked worktree branch = %q, want agent", branch)
	}
}

func TestAgentTargetStatusRejectsInvalidBase(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "base.txt", "base", "base")
	for _, base := range []string{"", "   ", "refs/heads/missing", "--help"} {
		t.Run(base, func(t *testing.T) {
			got, err := session.AgentTargetStatus(context.Background(), base)
			if err == nil {
				t.Fatalf("AgentTargetStatus(%q) = %+v, want an error", base, got)
			}
			if got != (AgentTargetStatus{}) {
				t.Fatalf("AgentTargetStatus(%q) returned partial status %+v", base, got)
			}
		})
	}
}

func TestAgentTargetStatusHonorsCancelledContext(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "base.txt", "base", "base")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := session.AgentTargetStatus(ctx, "main")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("AgentTargetStatus() error = %v, want context.Canceled", err)
	}
	if got != (AgentTargetStatus{}) {
		t.Fatalf("AgentTargetStatus() returned partial status %+v", got)
	}
}
