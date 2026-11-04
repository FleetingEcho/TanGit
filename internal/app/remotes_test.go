package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestFetchRemoteUsesTheFilteredMenuRow(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.snapshot.Remotes = append(h.app.snapshot.Remotes, repository.Remote{
		Name: "upstream", URLs: []string{"https://example.com/upstream.git"},
	})
	h.app.workspaceTab = sectionRemotes
	h.tester.Frame()
	if err := h.tester.Click("Search remotes"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("upstream")
	h.tester.Frame()
	h.rightClick(t, "upstream")
	h.requireMenuLabels(t, "Fetch remote")
	h.chooseMenuItem(t, "Fetch remote")
	h.waitFor(t, "the selected remote fetch", func() bool {
		return calls.has("fetch-remote:upstream")
	})
}

func TestRemoteKeyboardShortcuts(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionRemotes
	h.tester.Frame()

	h.press(t, ui.KeyF)
	h.waitFor(t, "the keyboard remote fetch", func() bool { return calls.has("fetch-remote:origin") })
	h.press(t, ui.KeyE)
	if !h.app.showRemoteForm || h.app.remoteOriginalName != "origin" {
		t.Fatal("E did not open the selected remote's edit form")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	h.tester.Key(ui.Shift, ui.KeySpace)
	if h.app.confirm == nil || !strings.Contains(h.app.confirm.message, "origin/main") {
		t.Fatalf("Shift+Space did not ask to check out the selected remote branch detached: remotes=%v branches=%v group=%s pending=%s", h.tester.Focused("Remotes"), h.tester.Focused("Remote branches"), h.app.keyboardCurrentGroup, h.app.keyboardFocus)
	}
	if err := h.tester.Click("Checkout commit"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the keyboard detached checkout", func() bool {
		return calls.has("checkout-commit:origin/main")
	})
	h.settle(t)
	h.press(t, ui.KeyU)
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the keyboard upstream update", func() bool {
		return calls.has("set-upstream:origin:main:main")
	})
}

func TestEditRemoteUsesTheFilteredMenuRow(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.snapshot.Remotes = append(h.app.snapshot.Remotes, repository.Remote{
		Name: "upstream", URLs: []string{"https://example.com/upstream.git"},
	})
	h.app.workspaceTab = sectionRemotes
	h.tester.Frame()
	if err := h.tester.Click("Search remotes"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("upstream")
	h.tester.Frame()
	h.rightClick(t, "upstream")
	h.chooseMenuItem(t, "Edit remote…")
	h.tester.Frame()

	if h.app.remoteName != "upstream" || h.app.remoteURL != "https://example.com/upstream.git" {
		t.Fatalf("edit form = %q / %q", h.app.remoteName, h.app.remoteURL)
	}
	for label, value := range map[string]string{
		"Remote name": "backup",
		"Remote URL":  "https://example.com/backup.git",
	} {
		if err := h.tester.Click(label); err != nil {
			t.Fatalf("focus %s: %v", label, err)
		}
		h.tester.Key(ui.Ctrl, ui.KeyA)
		h.tester.Type(value)
		h.tester.Frame()
	}
	if err := h.tester.Click("Save changes"); err != nil {
		t.Fatalf("save edited remote: %v", err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the edited remote", func() bool {
		return calls.has("update-remote:upstream:backup:https://example.com/backup.git")
	})
}

func TestUnsetUpstreamFromLocalBranchMenu(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionBranches
	h.tester.Frame()
	h.rightClick(t, "main (local)")
	h.chooseMenuItem(t, "Unset upstream")
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the upstream removal", func() bool {
		return calls.has("unset-upstream:main")
	})
}

func TestRemoteBranchHasNoUpstreamActionWhileDetached(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionRemotes
	h.app.snapshot.Branch = "c2c2c2c"
	h.app.snapshot.Head = "c2c2c2c2"
	h.app.snapshot.Branches[0].DetachedHead = true
	h.tester.Frame()

	h.rightClick(t, "feature/menus")
	h.requireMenuLabels(t, "Checkout detached…", "Merge into current…")
	if strings.Contains(strings.Join(h.tester.Texts(), " "), "Set as upstream") {
		t.Fatal("detached HEAD offers an upstream for a local branch that does not exist")
	}
}

func TestRemoteBranchSetsUpstreamForCurrentBranch(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionRemotes
	h.tester.Frame()
	if err := h.tester.Click("Search remote branches"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("feature")
	h.tester.Frame()
	h.rightClick(t, "feature/menus")
	h.chooseMenuItem(t, "Set as upstream of main")
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the upstream update", func() bool {
		return calls.has("set-upstream:origin:feature/menus:main")
	})
}

func TestRemoteBranchActionsUseTheFilteredRow(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionRemotes
	h.tester.Frame()
	if err := h.tester.Click("Search remote branches"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("feature")
	h.tester.Frame()

	h.rightClick(t, "feature/menus")
	h.chooseMenuItem(t, "Copy full branch name")
	if got := h.tester.Clipboard(); got != "origin/feature/menus" {
		t.Fatalf("copied remote branch = %q", got)
	}

	h.rightClick(t, "feature/menus")
	h.chooseMenuItem(t, "Checkout detached…")
	if h.app.confirm == nil || !strings.Contains(h.app.confirm.message, "origin/feature/menus") {
		t.Fatal("detached checkout did not confirm the filtered remote branch")
	}
	if err := h.tester.Click("Checkout commit"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the detached remote checkout", func() bool {
		return calls.has("checkout-commit:origin/feature/menus")
	})

	h.rightClick(t, "feature/menus")
	h.chooseMenuItem(t, "Merge into current…")
	if h.app.confirm == nil {
		t.Fatal("merge asked no confirmation")
	}
	if err := h.tester.Click("Merge"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the remote branch merge", func() bool { return calls.has("merge:origin/feature/menus") })

	h.rightClick(t, "feature/menus")
	h.chooseMenuItem(t, "Rebase current onto this…")
	if h.app.confirm == nil {
		t.Fatal("rebase asked no confirmation")
	}
	if err := h.tester.Click("Rebase"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the remote branch rebase", func() bool { return calls.has("rebase-onto:origin/feature/menus") })

	h.rightClick(t, "feature/menus")
	h.chooseMenuItem(t, "Delete remote branch…")
	if h.app.confirm == nil || !h.app.confirm.destructive {
		t.Fatal("remote deletion did not ask for destructive confirmation")
	}
	if !strings.Contains(h.app.confirm.detail, "Local branch feature/menus is kept") {
		t.Fatalf("remote deletion does not explain local history is retained: %q", h.app.confirm.detail)
	}
	if err := h.tester.Click("Delete remote branch"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the remote branch deletion", func() bool {
		return calls.has("delete-remote-branch:origin:feature/menus")
	})
}

// Filtering remote branches narrows the visible rows while preserving the
// source index used by checkout actions.
func TestRemoteBranchSearchFiltersWithoutChangingCheckoutTarget(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionRemotes
	h.tester.Frame()

	if err := h.tester.Click("Search remote branches"); err != nil {
		t.Fatalf("focus remote branch search: %v", err)
	}
	h.tester.Type("feature")
	h.tester.Frame()

	visible := h.app.visibleRemoteBranches()
	if len(visible) != 1 || visible[0].branch.Name != "feature/menus" {
		t.Fatalf("remote search rows = %#v, want only feature/menus", visible)
	}
	if !h.tester.HasText("feature/menus") {
		t.Fatalf("remote search hid its match: %q", h.texts())
	}
	if h.app.selectedRemoteBranch != 1 {
		t.Fatalf("filtered selection targets source branch %d, want 1", h.app.selectedRemoteBranch)
	}
	if err := h.tester.Click("Checkout as local…"); err != nil {
		t.Fatalf("open checkout form: %v", err)
	}
	if h.app.remoteLocalBranch != "feature/menus" {
		t.Fatalf("checkout default = %q, want filtered branch", h.app.remoteLocalBranch)
	}
}
