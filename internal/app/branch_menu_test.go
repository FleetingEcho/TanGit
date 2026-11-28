package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"
)

type branchMenuSession struct{ fakeSession }

func (s branchMenuSession) Push(context.Context) error {
	s.calls.record("push")
	return nil
}

func (s branchMenuSession) RenameBranch(_ context.Context, oldName, newName string) error {
	s.calls.record("rename-branch:" + oldName + ":" + newName)
	return nil
}

func openCurrentBranchMenu(t *testing.T) (*harness, *callRecorder) {
	t.Helper()
	h, calls := menuTestAppWithCalls(t)
	h.app.session = branchMenuSession{fakeSession: h.app.session.(fakeSession)}
	h.press(t, sectionKeys[sectionBranches])
	h.rightClick(t, "main (local)")
	return h, calls
}

func TestCurrentBranchMenuConfirmedGitActions(t *testing.T) {
	for _, action := range []struct {
		item, title, button, call string
	}{
		{"Push", "Push", "Continue", "push"},
		{"Push with tags", "Push with tags", "Continue", "push-follow-tags"},
		{"Force push with lease", "Force Push with Lease", "Force push with lease", "force-push-with-lease"},
		{"Pull latest", "Pull", "Continue", "pull-branch:main"},
		{"Fast-forward", "Fast-forward", "Continue", "fast-forward:main"},
		{"Unset upstream", "Unset upstream", "Continue", "unset-upstream:main"},
	} {
		t.Run(action.item, func(t *testing.T) {
			h, calls := openCurrentBranchMenu(t)
			h.chooseMenuItem(t, action.item)
			h.waitFor(t, "the confirmation to become ready", func() bool {
				return h.app.confirm != nil && !h.app.confirm.disabled
			})
			if got := h.app.confirm.title; got != action.title {
				t.Fatalf("confirmation title = %q, want %q", got, action.title)
			}
			if h.app.actionBusy || calls.has(action.call) {
				t.Fatalf("%s started before confirmation", action.call)
			}
			h.clickButton(t, action.button)
			h.waitForCall(t, calls, action.call)
		})
	}
}

func TestCurrentBranchMenuPullRequests(t *testing.T) {
	t.Run("Create pull request", func(t *testing.T) {
		h, calls := openCurrentBranchMenu(t)
		h.chooseMenuItem(t, "Create pull request")
		h.waitFor(t, "the pull request form", func() bool { return h.app.showPRForm })
		if h.app.prTitle != "main" || h.app.prBase != "main" {
			t.Fatalf("pull request defaults = %q / %q, want main / main", h.app.prTitle, h.app.prBase)
		}
		h.clickAndSettle(t, "Base branch")
		h.tester.Key(ui.Ctrl, ui.KeyA)
		h.tester.Type("release")
		h.tester.Frame()
		h.clickAndSettle(t, "Create as draft")
		h.clickButton(t, "Create pull request")
		h.waitForCall(t, calls, "create-pr:main||main|release|true")
	})

	t.Run("Open pull request in browser", func(t *testing.T) {
		h, calls := openCurrentBranchMenu(t)
		var opened string
		h.app.openExternalURL = func(url string) { opened = url }
		h.chooseMenuItem(t, "Open pull request in browser")
		h.waitFor(t, "the browser confirmation", func() bool { return h.app.confirm != nil })
		if h.app.confirm.message != "main" || calls.has("open-pr:main") || opened != "" {
			t.Fatal("opening the pull request did not wait for confirmation on main")
		}
		h.confirmAttentionForTest(t)
		h.waitFor(t, "the pull request URL", func() bool { return opened != "" })
		if opened != "https://example.com/pull/42" || !calls.has("open-pr:main") {
			t.Fatalf("opened %q, calls %q", opened, calls.all())
		}
	})

	t.Run("View pull request checks", func(t *testing.T) {
		h, calls := openCurrentBranchMenu(t)
		h.chooseMenuItem(t, "View pull request checks")
		h.waitFor(t, "the checks report", func() bool {
			return h.app.prChecksOpen && !h.app.prChecksLoading
		})
		if !calls.has("pr-checks:main") || !h.tester.HasText("build  passed  42s") {
			t.Fatalf("checks for main were not displayed: calls=%q report=%q", calls.all(), h.app.prChecksText)
		}
	})
}

func TestCurrentBranchMenuBranchFormsAndCopy(t *testing.T) {
	t.Run("Move unpushed commits to new branch", func(t *testing.T) {
		h, calls := openCurrentBranchMenu(t)
		h.chooseMenuItem(t, "Move unpushed commits to new branch")
		if !h.app.showMoveCommitsForm {
			t.Fatal("the move commits form did not open")
		}
		h.tester.Type("feature/moved")
		h.tester.Frame()
		if got := h.app.moveCommitsName; got != "feature/moved" {
			t.Fatalf("move destination = %q, want feature/moved", got)
		}
		h.clickButton(t, "Move commits")
		if h.app.confirm == nil || calls.has("move-unpushed:feature/moved") {
			t.Fatal("moving commits did not wait for confirmation")
		}
		h.clickButton(t, "Move commits")
		h.waitForCall(t, calls, "move-unpushed:feature/moved")
	})

	t.Run("Rename", func(t *testing.T) {
		h, calls := openCurrentBranchMenu(t)
		h.chooseMenuItem(t, "Rename")
		if !h.app.showRenameForm || h.app.renameBranch != "main" {
			t.Fatalf("rename field = %q, want main", h.app.renameBranch)
		}
		h.clickAndSettle(t, "New branch name")
		h.tester.Key(ui.Ctrl, ui.KeyA)
		h.tester.Type("renamed-main")
		h.tester.Frame()
		h.clickButton(t, "Rename")
		h.waitForCall(t, calls, "rename-branch:main:renamed-main")
	})

	t.Run("New branch here", func(t *testing.T) {
		h, calls := openCurrentBranchMenu(t)
		h.chooseMenuItem(t, "New branch here")
		if !h.app.showBranchForm || h.app.newBranchBase != "main" {
			t.Fatalf("new branch base = %q, want main", h.app.newBranchBase)
		}
		h.tester.Type("feature/new")
		h.tester.Frame()
		if got := h.app.newBranchName; got != "feature/new" {
			t.Fatalf("new branch name = %q, want feature/new", got)
		}
		h.clickButton(t, "Create branch")
		h.waitForCall(t, calls, "branch:feature/new:main")
	})

	t.Run("Copy branch name", func(t *testing.T) {
		h, _ := openCurrentBranchMenu(t)
		h.chooseMenuItem(t, "Copy branch name")
		if got := h.tester.Clipboard(); got != "main" {
			t.Fatalf("clipboard = %q, want the Git branch name main", got)
		}
	})
}
