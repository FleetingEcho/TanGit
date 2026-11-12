package app

import (
	"testing"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

func TestOpenWorktreePreservesRemoteWorkspace(t *testing.T) {
	a := &app{location: workspace.Location{Kind: workspace.SSH, ProfileID: "prod", Root: "/srv/main"}}
	var opened string
	a.claimOpen = func(path string) bool {
		opened = path
		return true
	}

	a.openWorktree(repository.Worktree{Path: "/srv/feature"})

	if opened != "ssh://prod/srv/feature" {
		t.Fatalf("opened worktree = %q, want ssh://prod/srv/feature", opened)
	}
}

func TestWorktreeOpenPathLeavesLocalPathUnchanged(t *testing.T) {
	a := &app{location: workspace.Location{Kind: workspace.Local, Root: "/work/main"}}
	const worktreePath = "relative/../feature"
	if got := a.worktreeOpenPath(worktreePath); got != worktreePath {
		t.Fatalf("local worktree path = %q, want unchanged %q", got, worktreePath)
	}
}
