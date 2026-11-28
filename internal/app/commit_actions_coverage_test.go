package app

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// These tests cover every command a History commit offers: each item of its
// right-click menu and each icon of the details toolbar. Every case clicks the
// real control, answers the form or confirmation it opens the way a user
// would, and checks that the repository got the right call for the right
// commit. A command that only opens a dialog is not counted as working.
//
// The fixture's HEAD is "Second commit" (c2c2c2c2) whose first parent is
// c1c1c1c1, so every command, including the parent comparison and the latest
// commit undo, is available on it.
const (
	coverageCommit = "Second commit"
	coverageHash   = "c2c2c2c2"
	coverageParent = "c1c1c1c1"
)

func (c *callRecorder) hasPrefix(prefix string) bool {
	for _, call := range c.all() {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}
	return false
}

// acceptConfirmation presses the pending confirmation's own button, after
// checking it asked the expected question and had not run anything yet.
func (h *harness) acceptConfirmation(t *testing.T, title string) {
	t.Helper()
	h.waitFor(t, "the "+title+" confirmation", func() bool {
		return h.app.confirm != nil && !h.app.confirm.disabled
	})
	if title != "" && h.app.confirm.title != title {
		t.Fatalf("confirmation title = %q, want %q", h.app.confirm.title, title)
	}
	label := h.app.confirm.confirmLabel
	h.tester.Frame()
	if err := h.tester.Click(label); err != nil {
		t.Fatalf("accept the %q confirmation: %v; texts %q", title, err, h.texts())
	}
	h.tester.Frame()
}

// waitForCall waits for the repository to be asked something, failing with the
// calls it did get.
func (h *harness) waitForCall(t *testing.T, calls *callRecorder, call string) {
	t.Helper()
	h.waitFor(t, "the "+call+" call", func() bool { return calls.has(call) })
	h.settle(t)
}

// clickButton presses the button of that name. Some dialogs title themselves
// with their button's text, which a click by text would find first.
func (h *harness) clickButton(t *testing.T, name string) {
	t.Helper()
	info, ok := h.tester.AccessByRole(ui.RoleButton, name)
	if !ok {
		t.Fatalf("no button named %q; texts %q", name, h.texts())
	}
	h.tester.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	h.tester.Frame()
	h.settle(t)
}

func (h *harness) clickAndSettle(t *testing.T, label string) {
	t.Helper()
	if err := h.tester.Click(label); err != nil {
		t.Fatalf("click %q: %v; texts %q", label, err, h.texts())
	}
	h.tester.Frame()
	h.settle(t)
}

// commitControl is one way to start a command: a menu item or a toolbar icon.
type commitControl struct {
	kind  string // "menu" or "bar"
	label string
}

func (h *harness) useCommitControl(t *testing.T, control commitControl) {
	t.Helper()
	switch control.kind {
	case "menu":
		h.rightClick(t, coverageCommit)
		h.chooseMenuItem(t, control.label)
	case "bar":
		h.clickAndSettle(t, control.label)
	default:
		t.Fatalf("unknown control kind %q", control.kind)
	}
}

// commitCommand is a command with every control that starts it and the proof
// that it worked.
type commitCommand struct {
	name     string
	controls []commitControl
	// prepare adjusts the fixture before the control is used.
	prepare func(h *harness)
	// verify finishes the flow and checks the effect.
	verify func(t *testing.T, h *harness, calls *callRecorder)
}

// confirmed expects a confirmation, accepts it, and wants the call afterwards.
func confirmed(title string, wantCalls ...string) func(*testing.T, *harness, *callRecorder) {
	return func(t *testing.T, h *harness, calls *callRecorder) {
		t.Helper()
		if h.app.confirm == nil {
			// An attention confirmation can arrive a frame later.
			h.waitFor(t, "the "+title+" confirmation", func() bool { return h.app.confirm != nil })
		}
		for _, call := range wantCalls {
			if calls.has(call) {
				t.Fatalf("%q ran before it was confirmed", call)
			}
		}
		h.acceptConfirmation(t, title)
		for _, call := range wantCalls {
			h.waitForCall(t, calls, call)
		}
		if h.app.actionError != "" {
			t.Fatalf("the action reported an error: %s", h.app.actionError)
		}
	}
}

func menuControl(label string) commitControl { return commitControl{"menu", label} }
func barControl(label string) commitControl  { return commitControl{"bar", label} }

func commitCommands() []commitCommand {
	return []commitCommand{
		{
			name:     "edit commit pauses a rebase",
			controls: []commitControl{menuControl("Edit commit (pause rebase)")},
			verify:   confirmed("", "edit-commit:"+coverageHash),
		},
		{
			name:     "reword",
			controls: []commitControl{menuControl("Reword commit"), barControl("Reword this commit")},
			verify: func(t *testing.T, h *harness, calls *callRecorder) {
				h.waitFor(t, "the reword form", func() bool {
					return h.app.showCommitRewordForm && !h.app.commitRewordLoading
				})
				h.app.commitRewordSummary = "A reworded subject"
				h.tester.Frame()
				h.clickAndSettle(t, "Reword commit")
				if h.app.confirm != nil {
					h.acceptConfirmation(t, "")
				}
				h.waitFor(t, "the reword call", func() bool { return calls.hasPrefix("reword:" + coverageHash + ":A reworded subject") })
			},
		},
		{
			name:     "amend with staged changes",
			controls: []commitControl{menuControl("Amend with staged changes")},
			verify:   confirmed("Amend Commit with Staged Changes", "amend-to:"+coverageHash),
		},
		{
			name:     "create fixup commit",
			controls: []commitControl{menuControl("Create fixup commit from staged changes")},
			verify:   confirmed("Create Fixup Commit", "fixup-for:"+coverageHash),
		},
		{
			name:     "apply fixups",
			controls: []commitControl{menuControl("Apply fixups from this commit through HEAD")},
			verify:   confirmed("Apply Fixup Commits", "apply-fixups:"+coverageHash),
		},
		{
			name:     "squash into older commit",
			controls: []commitControl{menuControl("Squash into older commit")},
			verify:   confirmed("Squash Commit", "combine:0:"+coverageHash),
		},
		{
			name:     "fixup into older commit",
			controls: []commitControl{menuControl("Fixup into older commit")},
			verify:   confirmed("Fixup Commit", "combine:1:"+coverageHash),
		},
		{
			name:     "move commit toward HEAD",
			controls: []commitControl{menuControl("Move commit toward HEAD")},
			verify:   confirmed("Move Commit", "move:-1:"+coverageHash),
		},
		{
			name:     "move commit toward older history",
			controls: []commitControl{menuControl("Move commit toward older history")},
			verify:   confirmed("Move Commit", "move:1:"+coverageHash),
		},
		{
			name:     "drop commit",
			controls: []commitControl{menuControl("Drop commit")},
			verify:   confirmed("Drop Commit", "drop:"+coverageHash),
		},
		{
			name:     "checkout detached",
			controls: []commitControl{menuControl("Checkout to this commit (detached)"), barControl("Checkout this commit")},
			verify:   confirmed("Checkout Commit", "checkout-commit:"+coverageHash),
		},
		{
			name:     "undo latest commit keeping changes staged",
			controls: []commitControl{menuControl("Undo latest commit and keep changes staged")},
			verify:   confirmed("", "undo-latest-branch:main", "reset-soft:"+coverageParent),
		},
		{
			name:     "undo latest commit keeping changes unstaged",
			controls: []commitControl{menuControl("Undo latest commit and keep changes unstaged")},
			verify:   confirmed("", "undo-latest-branch:main", "reset-mixed:"+coverageParent),
		},
		{
			name:     "soft reset",
			controls: []commitControl{menuControl("Soft reset to this commit"), barControl("Soft reset to this commit")},
			verify:   confirmed("Soft Reset", "reset-soft:"+coverageHash),
		},
		{
			name:     "mixed reset",
			controls: []commitControl{menuControl("Mixed reset to this commit"), barControl("Mixed reset to this commit")},
			verify:   confirmed("Mixed Reset", "reset-mixed:"+coverageHash),
		},
		{
			name:     "hard reset",
			controls: []commitControl{menuControl("Hard reset to this commit"), barControl("Hard reset to this commit")},
			verify:   confirmed("Hard Reset", "reset-hard:"+coverageHash),
		},
		{
			name:     "export as mailbox patch",
			controls: []commitControl{menuControl("Export as mailbox patch")},
			verify: func(t *testing.T, h *harness, calls *callRecorder) {
				if !h.app.showExportPatchForm {
					t.Fatal("the export form did not open")
				}
				h.clickButton(t, "Export patches")
				if h.app.confirm != nil {
					h.acceptConfirmation(t, "")
				}
				h.waitForCall(t, calls, "export-patches:/work/project/patches")
			},
		},
		{
			name:     "create a tag",
			controls: []commitControl{menuControl("Create a tag here"), barControl("Create a tag here")},
			verify: func(t *testing.T, h *harness, calls *callRecorder) {
				if !h.app.showCommitTagForm {
					t.Fatal("the tag form did not open")
				}
				h.app.commitTagName = "v9.9.9"
				h.tester.Frame()
				h.clickAndSettle(t, "Create tag")
				confirmed("Create Tag", "tag:v9.9.9:"+coverageHash)(t, h, calls)
			},
		},
		{
			name:     "create a branch",
			controls: []commitControl{menuControl("Create a branch here"), barControl("Create a branch here")},
			verify: func(t *testing.T, h *harness, calls *callRecorder) {
				if !h.app.showCommitBranchForm {
					t.Fatal("the branch form did not open")
				}
				h.app.commitBranchName = "topic/from-here"
				h.tester.Frame()
				h.clickAndSettle(t, "Create and checkout")
				confirmed("Create Branch", "branch:topic/from-here:"+coverageHash, "checkout:topic/from-here")(t, h, calls)
			},
		},
		{
			name:     "start bisect",
			controls: []commitControl{menuControl("Start bisect from this commit")},
			verify: func(t *testing.T, h *harness, calls *callRecorder) {
				if !h.app.showBisectForm {
					t.Fatal("the bisect form did not open")
				}
				h.app.bisectGoodRef = coverageParent
				h.tester.Frame()
				h.clickAndSettle(t, "Start bisect")
				confirmed("Start Bisect", "bisect-start:"+coverageHash+":"+coverageParent)(t, h, calls)
			},
		},
		{
			name:     "mark commit good during a bisect",
			controls: []commitControl{menuControl("Mark commit good (bisect)")},
			prepare:  bisectActive,
			verify:   confirmed("Mark Bisect Commit", "bisect-mark:good:"+coverageHash),
		},
		{
			name:     "mark commit bad during a bisect",
			controls: []commitControl{menuControl("Mark commit bad (bisect)")},
			prepare:  bisectActive,
			verify:   confirmed("Mark Bisect Commit", "bisect-mark:bad:"+coverageHash),
		},
		{
			name:     "skip commit during a bisect",
			controls: []commitControl{menuControl("Skip commit (bisect)")},
			prepare:  bisectActive,
			verify:   confirmed("Mark Bisect Commit", "bisect-mark:skip:"+coverageHash),
		},
		{
			name:     "cherry-pick",
			controls: []commitControl{menuControl("Cherry-pick onto current branch"), barControl("Cherry-pick this commit")},
			verify:   confirmed("Cherry-pick Commit", "cherry-pick:"+coverageHash),
		},
		{
			name:     "revert",
			controls: []commitControl{menuControl("Revert commit")},
			verify:   confirmed("Revert Commit", "revert:"+coverageHash),
		},
		{
			name:     "compare with HEAD",
			controls: []commitControl{menuControl("Compare commit with HEAD"), barControl("Compare commit with HEAD")},
			verify: func(t *testing.T, h *harness, calls *callRecorder) {
				h.waitFor(t, "the comparison", func() bool { return calls.hasPrefix("compare-files:" + coverageHash + ":HEAD") })
				if h.app.workspaceTab != sectionCompare {
					t.Fatalf("Compare did not open the Compare page: tab %d", h.app.workspaceTab)
				}
			},
		},
		{
			name:     "compare first parent with commit",
			controls: []commitControl{menuControl("Compare first parent with commit"), barControl("Compare parent with commit")},
			verify: func(t *testing.T, h *harness, calls *callRecorder) {
				h.waitFor(t, "the comparison", func() bool {
					return calls.hasPrefix("compare-files:" + coverageParent + ":" + coverageHash)
				})
				if h.app.workspaceTab != sectionCompare {
					t.Fatalf("Compare did not open the Compare page: tab %d", h.app.workspaceTab)
				}
			},
		},
	}
}

func bisectActive(h *harness) {
	h.app.snapshot.Bisect = &repository.BisectState{Active: true}
	h.tester.Frame()
}

// Every command of a commit works from every control that starts it, on both
// pages that list commits with the same menu and toolbar.
func TestEveryCommitCommandWorksFromEveryControl(t *testing.T) {
	for _, page := range []struct {
		name string
		tab  int
	}{{"History", sectionHistory}, {"Graph", sectionGraph}} {
		for _, command := range commitCommands() {
			for _, control := range command.controls {
				page, command, control := page, command, control
				t.Run(page.name+"/"+command.name+" via "+control.kind, func(t *testing.T) {
					h, calls := historyHarness(t)
					if page.tab != sectionHistory {
						h.app.setWorkspaceTab(page.tab)
						h.tester.Frame()
						h.settle(t)
					}
					h.waitFor(t, "the commit details toolbar", func() bool { return h.tester.HasText(coverageCommit) })
					if command.prepare != nil {
						command.prepare(h)
					}
					h.useCommitControl(t, control)
					command.verify(t, h, calls)
				})
			}
		}
	}
}

// The commands that act on the app's own state, rather than on the repository,
// still do their job: copy, mark a base, select for a batch, and Custom Patch.
func TestCommitMenuCommandsThatActOnAppState(t *testing.T) {
	t.Run("copy commit hash", func(t *testing.T) {
		h, _ := historyHarness(t)
		h.rightClick(t, coverageCommit)
		h.chooseMenuItem(t, "Copy commit hash")
		if got := h.tester.Clipboard(); got != coverageHash {
			t.Fatalf("clipboard = %q, want the full commit hash %q", got, coverageHash)
		}
	})
	t.Run("toggle batch selection", func(t *testing.T) {
		h, _ := historyHarness(t)
		h.rightClick(t, coverageCommit)
		h.chooseMenuItem(t, "Toggle batch selection")
		if !h.app.commitSelection[coverageHash] || !h.app.commitSelectionMode {
			t.Fatalf("the commit was not selected for a batch: %v", h.app.commitSelection)
		}
		h.rightClick(t, coverageCommit)
		h.chooseMenuItem(t, "Toggle batch selection")
		if h.app.commitSelection[coverageHash] {
			t.Fatal("toggling twice left the commit selected")
		}
	})
	t.Run("mark a base, open the rebase todo, and cancel the mark", func(t *testing.T) {
		h, _ := historyHarness(t)
		h.rightClick(t, coverageCommit)
		h.chooseMenuItem(t, "Mark as base commit for rebase")
		if h.app.markedBaseCommit.Hash != coverageHash {
			t.Fatalf("marked base = %q, want %q", h.app.markedBaseCommit.Hash, coverageHash)
		}
		h.rightClick(t, coverageCommit)
		h.requireMenuLabels(t, "Cancel marked base commit", "Open Rebase TODO")
		h.chooseMenuItem(t, "Open Rebase TODO")
		if !h.app.rebaseModalOpen {
			t.Fatal("Open Rebase TODO did not open the Rebase TODO dialog")
		}
		h.app.closeRebaseModal()
		h.tester.Frame()
		h.rightClick(t, coverageCommit)
		h.chooseMenuItem(t, "Cancel marked base commit")
		if h.app.markedBaseCommit.Hash != "" {
			t.Fatalf("the mark survived Cancel: %q", h.app.markedBaseCommit.Hash)
		}
	})
	t.Run("add all files to Custom Patch, then review it", func(t *testing.T) {
		h, calls := historyHarness(t)
		h.rightClick(t, coverageCommit)
		h.chooseMenuItem(t, "Add all files to Custom Patch")
		h.waitFor(t, "the files to be added", func() bool { return len(h.app.customPatch.entries) == 2 })
		for _, call := range []string{"commit-files:" + coverageHash, "commit-file-patch:" + coverageHash + ":one.txt", "commit-file-patch:" + coverageHash + ":two.txt"} {
			if !calls.has(call) {
				t.Fatalf("Custom Patch never asked for %q: %q", call, calls.all())
			}
		}
		h.rightClick(t, coverageCommit)
		h.chooseMenuItem(t, "Review Custom Patch")
		if !h.app.customPatch.open {
			t.Fatal("Review Custom Patch did not open the builder")
		}
	})
}

// A commit that is not at the tip offers no "undo latest commit", and a root
// commit has no parent to compare with: the menu says so by leaving them out
// or disabling them, rather than offering a command that cannot work.
func TestCommitMenuOmitsCommandsThatCannotApply(t *testing.T) {
	h, _ := historyHarness(t)
	h.rightClick(t, "Initial commit")
	for _, label := range []string{
		"Undo latest commit and keep changes staged",
		"Undo latest commit and keep changes unstaged",
	} {
		if h.menuOffers(label) {
			t.Errorf("a commit that is not the branch tip offers %q", label)
		}
	}
	// A root commit has no first parent, so the item is there but cannot be chosen.
	h.requireMenuLabels(t, "Compare first parent with commit")
	if err := chooseMenuPath(h.tester, "Compare first parent with commit"); err == nil {
		t.Fatal("the root commit's parent comparison could be chosen")
	}
	if h.app.workspaceTab == sectionCompare {
		t.Fatal("the root commit compared with a parent it does not have")
	}
}

// The number beside a commit list's title says how far the list has loaded, and
// its tooltip tells the reader how to get further: scroll for the next page, or
// use the filter. When nothing more is waiting it does not promise a page.
func TestCommitCountBadgeTooltipExplainsHowToSeeMore(t *testing.T) {
	for _, test := range []struct {
		name string
		more bool
		want string
		not  string
	}{
		{"another page waits", true, "Scroll down to load more, or use the filter to search the commit history.", ""},
		{"everything is loaded", false, "commits shown.", "Scroll down"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Padding(20).Children(func() { commitCountBadge(c, 50, test.more) })
			}, 700, 120)
			tt.Frame()
			badge, ok := tt.Find("50")
			if !ok {
				t.Fatalf("no badge showing 50: %q", tt.Texts())
			}
			if tt.HasText(test.want) {
				t.Fatal("the tooltip shows before anything is hovered")
			}
			tt.Move(badge.X+badge.W/2, badge.Y+badge.H/2)
			tt.Advance(time.Second)
			tt.Frame()
			if !tt.HasText(test.want) {
				t.Fatalf("hovering the count did not show %q: %q", test.want, tt.Texts())
			}
			if test.not != "" {
				for _, text := range tt.Texts() {
					if strings.Contains(text, test.not) {
						t.Fatalf("the tooltip %q should not mention %q", text, test.not)
					}
				}
			}
		})
	}
}

// The filter button reads as applied: pressed for assistive technology, and
// not pressed while the list is whole.
func TestCommitFilterButtonShowsWhetherAFilterIsApplied(t *testing.T) {
	for _, active := range []bool{false, true} {
		active := active
		tt := ui.NewTester(func(c *ui.Context) {
			commitFilterButton(c, "Search and filter the commit list", active, func() {})
		}, 200, 60)
		tt.Frame()
		info, ok := tt.AccessByRole(ui.RoleButton, "Filter commits")
		if !ok {
			t.Fatalf("active=%v: no Filter commits button", active)
		}
		if info.Checked != active {
			t.Fatalf("active=%v: the button reports pressed=%v", active, info.Checked)
		}
	}
}

// The reflog's count says how far the list has loaded; hovering it says how to
// reach the older entries, and does not promise more once the whole reflog is
// here.
func TestReflogCountTooltipExplainsHowToSeeMore(t *testing.T) {
	for _, test := range []struct {
		name string
		more bool
		want string
		not  string
	}{
		{"older entries wait", true, "Scroll down to load more, or use the filter to search by operation, hash or author.", ""},
		{"the whole reflog is here", false, "This is the whole reflog.", "Scroll down"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApp(fakeOpener{})
			a.reflogMore = test.more
			var query string
			var handle ui.Handle
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Padding(20).Children(func() {
					listSearchFieldTip(c, &handle, &query, "Search reflog", "Filter by operation, hash, or author",
						"Clear reflog search", a.reflogCountTooltip(50), 50, 50, func() {})
				})
			}, 900, 120)
			tt.Frame()
			badge, ok := tt.Find("50")
			if !ok {
				t.Fatalf("no count showing 50: %q", tt.Texts())
			}
			tt.Move(badge.X+badge.W/2, badge.Y+badge.H/2)
			tt.Advance(time.Second)
			tt.Frame()
			if !tt.HasText(test.want) {
				t.Fatalf("hovering the reflog count did not show %q: %q", test.want, tt.Texts())
			}
			if test.not != "" {
				for _, text := range tt.Texts() {
					if strings.Contains(text, test.not) {
						t.Fatalf("the tooltip %q should not mention %q", text, test.not)
					}
				}
			}
		})
	}
}
