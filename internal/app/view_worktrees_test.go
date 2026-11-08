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

func TestWorktreeMaintenanceActionsReachSession(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionWorktrees
	h.app.snapshot.Worktrees = []repository.Worktree{{Name: "feature", Path: "/work/feature", Branch: "feature"}}
	h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
	h.tester.Frame()
	for _, label := range []string{"Repair metadata", "Prune stale records…", "Lock", "Remove…"} {
		if !h.tester.HasText(label) {
			t.Fatalf("worktree maintenance action %q is missing from %q", label, h.tester.Texts())
		}
	}

	h.app.toggleSelectedWorktreeLock()
	h.waitFor(t, "worktree lock", func() bool {
		return calls.has("lock-worktree:/work/feature:TanGit") && !h.app.actionBusy
	})

	h.app.repairWorktrees()
	h.waitFor(t, "worktree repair", func() bool { return calls.has("repair-worktrees") && !h.app.actionBusy })

	h.app.confirmPruneWorktrees()
	if h.app.confirm == nil || h.app.confirm.confirmLabel != "Prune" {
		t.Fatal("prune did not require its explicit confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "worktree prune", func() bool { return calls.has("prune-worktrees") && !h.app.actionBusy })

	h.app.snapshot.Worktrees = []repository.Worktree{{Name: "feature", Path: "/work/feature", Branch: "feature"}}
	h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
	h.app.confirmForceDeleteSelectedWorktree()
	if h.app.confirm == nil || h.app.confirm.confirmLabel != "Force remove" {
		t.Fatal("force remove did not require its explicit confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "forced worktree removal", func() bool {
		return calls.has("delete-worktree:/work/feature:true") && !h.app.actionBusy
	})
}

func TestLockedAndPrunableWorktreeStateIsVisible(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionWorktrees
	h.app.snapshot.Worktrees = []repository.Worktree{{
		Name: "archive", Path: "/media/archive", Branch: "archive",
		Locked: true, LockReason: "External drive", Prunable: true, PruneReason: "gitdir is missing",
	}}
	h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
	h.tester.Frame()
	for _, label := range []string{"Locked", "Stale", "Locked: External drive", "Prunable: gitdir is missing", "Unlock"} {
		if !h.tester.HasText(label) {
			t.Fatalf("worktree state %q is missing from %q", label, h.tester.Texts())
		}
	}

	h.app.toggleSelectedWorktreeLock()
	h.waitFor(t, "worktree unlock", func() bool {
		return calls.has("unlock-worktree:/media/archive") && !h.app.actionBusy
	})
}
