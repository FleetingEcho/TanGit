package app

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"os"
	"strings"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/workspace"
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

func TestOpenWorktreeInNewTabPreservesRemoteWorkspace(t *testing.T) {
	a := &app{location: workspace.Location{Kind: workspace.SSH, ProfileID: "prod", Root: "/srv/main"}}
	var opened string
	a.openTab = func(path string, _ openPolicy) { opened = path }
	a.snapshot.Worktrees = []repository.Worktree{{Name: "feature", Path: "/srv/feature"}}
	a.selectedWorktree = 0

	a.openSelectedWorktreeInTab()

	if opened != "ssh://prod/srv/feature" {
		t.Fatalf("new-tab worktree = %q, want ssh://prod/srv/feature", opened)
	}
	if a.path != "" {
		t.Fatalf("new-tab action replaced the current page path with %q", a.path)
	}
}

func TestWorktreeOpenPathLeavesLocalPathUnchanged(t *testing.T) {
	a := &app{location: workspace.Location{Kind: workspace.Local, Root: "/work/main"}}
	const worktreePath = "relative/../feature"
	if got := a.worktreeOpenPath(worktreePath); got != worktreePath {
		t.Fatalf("local worktree path = %q, want unchanged %q", got, worktreePath)
	}
}

func TestWorktreeNewTabActionsUseSelectedExplicitTarget(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionWorktrees
	h.app.snapshot.Worktrees = []repository.Worktree{{Name: "feature", Path: "/work/feature", Branch: "feature"}}
	h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
	opened := ""
	h.app.openTab = func(path string, _ openPolicy) { opened = path }
	h.tester.Frame()

	if err := h.tester.Click("Open worktree in new tab"); err != nil {
		t.Fatal(err)
	}
	if opened != "/work/feature" || h.app.path != "/work/project" {
		t.Fatalf("toolbar opened %q and current page is %q", opened, h.app.path)
	}

	h.rightClick(t, "feature")
	h.requireMenuLabels(t, "Open worktree in new tab")
	opened = ""
	h.chooseMenuItem(t, "Open worktree in new tab")
	if opened != "/work/feature" {
		t.Fatalf("row menu opened %q, want the selected worktree", opened)
	}
}

func TestWorktreeMaintenanceActionsReachSession(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionWorktrees
	h.app.snapshot.Worktrees = []repository.Worktree{{Name: "feature", Path: "/work/feature", Branch: "feature"}}
	h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
	h.app.openTab = func(string, openPolicy) {}
	h.tester.Frame()
	for _, label := range []string{
		"New temporary worktree…", "Repair worktree metadata…", "Prune stale worktree records…",
		"Open worktree", "Open worktree in new tab", "Open worktree in editor", "Open worktree terminal", "Compare worktree",
		"Move worktree…", "Lock worktree…", "Remove worktree…",
	} {
		bounds, found := h.tester.Find(label)
		if !found {
			t.Fatalf("worktree icon action %q is missing from %q", label, h.tester.Texts())
		}
		if bounds.W > 40 || bounds.H > 30 {
			t.Fatalf("worktree action %q is not a compact icon button: bounds=%+v", label, bounds)
		}
	}

	h.app.toggleSelectedWorktreeLock()
	if !h.app.showWorktreeLockForm {
		t.Fatal("locking did not open its reason form")
	}
	h.app.beginLockWorktree()
	h.waitFor(t, "worktree lock", func() bool {
		return calls.has("lock-worktree:/work/feature:lazygitGo") && !h.app.actionBusy
	})

	h.app.repairWorktrees()
	if !h.app.showWorktreeRepairForm {
		t.Fatal("repair did not open its path form")
	}
	h.app.beginRepairWorktrees()
	h.waitFor(t, "worktree repair", func() bool { return calls.has("repair-worktrees") && !h.app.actionBusy })

	h.app.confirmPruneWorktrees()
	h.waitFor(t, "worktree prune preview", func() bool { return !h.app.worktreePruneLoading })
	h.app.beginPruneWorktrees()
	if h.app.confirm == nil || !h.app.confirm.destructive || h.app.confirm.confirmLabel != "Prune records" {
		t.Fatal("prune did not require its explicit destructive confirmation")
	}
	if calls.has("prune-worktrees") {
		t.Fatal("prune ran before confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "worktree prune", func() bool { return calls.has("prune-worktrees") && !h.app.actionBusy })

	h.app.snapshot.Worktrees = []repository.Worktree{{Name: "feature", Path: "/work/feature", Branch: "feature"}}
	h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
	h.app.confirmDeleteSelectedWorktree()
	if h.app.confirm == nil || !h.app.confirm.destructive || h.app.confirm.confirmLabel != "Remove" {
		t.Fatal("remove did not require its explicit destructive confirmation")
	}
	h.app.cancelConfirm()
	if calls.has("delete-worktree:/work/feature:false") {
		t.Fatal("canceling worktree removal still deleted the worktree")
	}
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

func TestWorktreeRowsKeepOneLineForLongNamesAndBranches(t *testing.T) {
	for _, width := range []int{900, 1280} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			h, _ := menuTestAppWithCalls(t)
			h.app.workspaceTab = sectionWorktrees
			h.app.filePaneWidth = 300
			h.app.snapshot.Worktrees = []repository.Worktree{
				{Name: "short", Path: "/work/short", Branch: "main"},
				{Name: "dyson-link-mp-2", Path: "/work/dyson-link-mp-2", Branch: "feature/WMP-14622-ship-ticket-with-worktrees-and-many-more-words", Current: true},
				{Name: strings.Repeat("long-worktree-name-", 6), Path: "/work/long-worktree", Branch: strings.Repeat("feature/long-branch-name-", 5)},
				{Name: "locked-worktree-with-a-long-name", Path: "/work/locked-worktree", Branch: "feature/long-branch-with-status", Locked: true, Prunable: true, Modified: 12, Ahead: 3, Behind: 2},
			}
			h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
			h.tester.SetSize(width, 760)
			h.tester.Frame()
			var rowHeight float32
			for _, tree := range h.app.snapshot.Worktrees {
				row, ok := h.tester.AccessByRole(ui.RoleListItem, tree.Name)
				if !ok {
					t.Fatalf("missing worktree row %q", tree.Name)
				}
				if rowHeight == 0 {
					rowHeight = row.Bounds.H
				}
				if row.Bounds.H != rowHeight {
					t.Fatalf("%q wrapped: height=%v, short row=%v", tree.Name, row.Bounds.H, rowHeight)
				}
			}
			target := h.app.snapshot.Worktrees[1]
			row, _ := h.tester.AccessByRole(ui.RoleListItem, target.Name)
			h.tester.Move(row.Bounds.X+5, row.Bounds.Y+row.Bounds.H/2)
			h.tester.Frame()
			hovered, _ := h.tester.AccessByRole(ui.RoleListItem, target.Name)
			if hovered.Bounds.H != rowHeight {
				t.Fatal("hover changed the worktree row height")
			}
			if err := h.tester.Click(target.Name); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			selected, _ := h.tester.AccessByRole(ui.RoleListItem, target.Name)
			if selected.Bounds.H != rowHeight || h.app.selectedWorktree != 1 {
				t.Fatal("selecting a long worktree changed its height or selected the wrong target")
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, fmt.Sprintf("worktree-single-line-%d", width), h.tester)
			}
		})
	}
}
