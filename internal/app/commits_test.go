package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// historyHarness opens the menu fixture on the History section.
func historyHarness(t *testing.T) (*harness, *callRecorder) {
	t.Helper()
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionHistory
	h.tester.Frame()
	h.settle(t)
	return h, calls
}

// Each commit row offers the actions of a Git client's history view, named in
// its context menu so the glyphs are never a guess.
func TestCommitRowMenuOffersTheActions(t *testing.T) {
	h, _ := historyHarness(t)
	h.rightClick(t, "Second commit")
	h.requireMenuLabels(t,
		"Copy commit hash",
		"Reword commit…",
		"Amend with staged changes…",
		"Create fixup commit from staged changes…",
		"Apply fixups from this commit through HEAD…",
		"Squash into older commit…",
		"Fixup into older commit…",
		"Move commit toward HEAD",
		"Move commit toward older history",
		"Drop commit…",
		"Checkout this commit (detached)…",
		"Soft reset to this commit…",
		"Mixed reset to this commit…",
		"Hard reset to this commit…",
		"Create a tag here…",
		"Create a branch here…",
		"Cherry-pick onto current branch…",
		"Revert commit…",
	)
}

var safeCommitMenuItems = []string{
	"Copy commit hash",
	"Checkout this commit (detached)…",
	"Create a tag here…",
	"Create a branch here…",
	"Cherry-pick onto current branch…",
	"Revert commit…",
}

var historyOnlyCommitMenuItems = []string{
	"Reword commit…",
	"Amend with staged changes…",
	"Create fixup commit from staged changes…",
	"Apply fixups from this commit through HEAD…",
	"Squash into older commit…",
	"Fixup into older commit…",
	"Move commit toward HEAD",
	"Move commit toward older history",
	"Drop commit…",
	"Soft reset to this commit…",
	"Mixed reset to this commit…",
	"Hard reset to this commit…",
}

// branchCommitHarness opens the Branches page on either the checked-out
// branch's history or another branch, read without checking it out.
func branchCommitHarness(t *testing.T, other bool) (*harness, *callRecorder) {
	t.Helper()
	h, calls := menuTestAppWithCalls(t)
	branch := "main"
	if other {
		h.app.selectedBranch = 1 // feature
		branch = "feature"
	}
	h.press(t, sectionKeys[sectionBranches])
	h.waitFor(t, "the branch commits", func() bool {
		return h.app.branchCommitsBranch == branch && len(h.app.branchCommits) > 0
	})
	return h, calls
}

// Browsing the checked-out branch offers the same History actions on its
// commits; the earlier branch-only menu hid almost all of them for no reason.
func TestCurrentBranchCommitRowMenuOffersHistoryActions(t *testing.T) {
	h, _ := branchCommitHarness(t, false)
	h.rightClick(t, "Second commit")
	h.requireMenuLabels(t, safeCommitMenuItems...)
	h.requireMenuLabels(t, historyOnlyCommitMenuItems...)
}

// Reading another branch never checks it out, so history-rewriting actions do
// not appear there. The actions that work on the chosen commit itself remain.
func TestOtherBranchCommitRowMenuOffersSafeCommitActions(t *testing.T) {
	h, _ := branchCommitHarness(t, true)
	h.rightClick(t, "Initial commit")
	h.requireMenuLabels(t, safeCommitMenuItems...)
	for _, label := range historyOnlyCommitMenuItems {
		if slices.Contains(h.texts(), label) {
			t.Errorf("a non-current branch offers current-branch history edit %q", label)
		}
	}
}

// Every direct Branches menu action asks first, then invokes the repository
// with the clicked commit. Current-branch edits use Second commit; safe
// actions on another branch use Initial commit without checking that branch out.
func TestBranchCommitRowMenuActionsAskAndUseTheClickedCommit(t *testing.T) {
	tests := []struct {
		other    bool
		menu     string
		confirm  string
		wantCall string
	}{
		{menu: "Amend with staged changes…", confirm: "Amend commit", wantCall: "amend-to:c2c2c2c2"},
		{menu: "Create fixup commit from staged changes…", confirm: "Create fixup", wantCall: "fixup-for:c2c2c2c2"},
		{menu: "Apply fixups from this commit through HEAD…", confirm: "Apply fixups", wantCall: "apply-fixups:c2c2c2c2"},
		{menu: "Squash into older commit…", confirm: "Squash commit", wantCall: "combine:0:c2c2c2c2"},
		{menu: "Fixup into older commit…", confirm: "Fixup commit", wantCall: "combine:1:c2c2c2c2"},
		{menu: "Move commit toward HEAD", confirm: "Move commit", wantCall: "move:-1:c2c2c2c2"},
		{menu: "Move commit toward older history", confirm: "Move commit", wantCall: "move:1:c2c2c2c2"},
		{menu: "Drop commit…", confirm: "Drop commit", wantCall: "drop:c2c2c2c2"},
		{menu: "Soft reset to this commit…", confirm: "Soft reset", wantCall: "reset-soft:c2c2c2c2"},
		{menu: "Mixed reset to this commit…", confirm: "Mixed reset", wantCall: "reset-mixed:c2c2c2c2"},
		{menu: "Hard reset to this commit…", confirm: "Hard reset", wantCall: "reset-hard:c2c2c2c2"},
		{other: true, menu: "Checkout this commit (detached)…", confirm: "Checkout commit", wantCall: "checkout-commit:c1c1c1c1"},
		{other: true, menu: "Cherry-pick onto current branch…", confirm: "Cherry-pick", wantCall: "cherry-pick:c1c1c1c1"},
		{other: true, menu: "Revert commit…", confirm: "Revert", wantCall: "revert:c1c1c1c1"},
	}
	for _, test := range tests {
		t.Run(test.menu, func(t *testing.T) {
			h, calls := branchCommitHarness(t, test.other)
			subject := "Second commit"
			if test.other {
				subject = "Initial commit"
			}
			h.rightClick(t, subject)
			h.chooseMenuItem(t, test.menu)
			if h.app.confirm == nil {
				t.Fatal("the action asked for no confirmation")
			}
			if calls.has(test.wantCall) {
				t.Fatal("the action ran before confirmation")
			}
			if err := h.tester.Click(test.confirm); err != nil {
				t.Fatal(err)
			}
			h.waitFor(t, "the confirmed repository action", func() bool {
				return calls.has(test.wantCall)
			})
		})
	}
}

func TestCurrentBranchCommitRowMenuRewordsTheSelectedCommit(t *testing.T) {
	h, calls := branchCommitHarness(t, false)
	h.rightClick(t, "Second commit")
	h.chooseMenuItem(t, "Reword commit…")
	h.waitFor(t, "the full commit message", func() bool {
		return !h.app.commitRewordLoading && h.app.commitRewordBody == "Existing body"
	})
	if h.app.commitRewordCommit.Hash != "c2c2c2c2" {
		t.Fatalf("the reword targets %s, want the clicked commit", h.app.commitRewordCommit.Hash)
	}
	h.app.commitRewordSummary = "Second commit, clarified"
	h.app.commitRewordBody = "A longer explanation"
	h.tester.Frame()
	if err := h.tester.Click("Reword commit"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("rewording asked for no confirmation")
	}
	want := "reword:c2c2c2c2:Second commit, clarified:A longer explanation"
	if calls.has(want) {
		t.Fatal("rewording ran before confirmation")
	}
	if err := h.tester.Click("Reword commit"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the commit to be reworded", func() bool { return calls.has(want) })
}

func TestOtherBranchCommitRowMenuCopiesTheSelectedHash(t *testing.T) {
	h, _ := branchCommitHarness(t, true)
	h.rightClick(t, "Initial commit")
	h.chooseMenuItem(t, "Copy commit hash")
	if got := h.tester.Clipboard(); got != "c1c1c1c1" {
		t.Fatalf("the clipboard holds %q, want the clicked commit hash", got)
	}
	if !containsText(h.texts(), "Copied commit hash") {
		t.Fatalf("copying gave no notification: %q", h.texts())
	}
}

func TestOtherBranchCommitRowMenuCreatesATagAtTheSelectedCommit(t *testing.T) {
	h, calls := branchCommitHarness(t, true)
	h.rightClick(t, "Initial commit")
	h.chooseMenuItem(t, "Create a tag here…")
	if !h.app.showCommitTagForm || h.app.commitTagCommit.Hash != "c1c1c1c1" {
		t.Fatalf("the tag form targets %q, want c1c1c1c1", h.app.commitTagCommit.Hash)
	}
	h.app.commitTagName = "v9.0.0"
	h.tester.Frame()
	if err := h.tester.Click("Create tag"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || calls.has("tag:v9.0.0:c1c1c1c1") {
		t.Fatal("tag creation did not wait for confirmation")
	}
	if err := h.tester.Click("Add tag"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the tag to be created", func() bool {
		return calls.has("tag:v9.0.0:c1c1c1c1")
	})
}

func TestOtherBranchCommitRowMenuCreatesABranchAtTheSelectedCommit(t *testing.T) {
	h, calls := branchCommitHarness(t, true)
	h.rightClick(t, "Initial commit")
	h.chooseMenuItem(t, "Create a branch here…")
	if !h.app.showCommitBranchForm || h.app.commitBranchCommit.Hash != "c1c1c1c1" {
		t.Fatalf("the branch form targets %q, want c1c1c1c1", h.app.commitBranchCommit.Hash)
	}
	h.app.commitBranchName = "from-here"
	h.tester.Frame()
	if err := h.tester.Click("Create and checkout"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || calls.has("branch:from-here:c1c1c1c1") {
		t.Fatal("branch creation did not wait for confirmation")
	}
	if err := h.tester.Click("Create branch"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the branch to be created", func() bool {
		return calls.has("branch:from-here:c1c1c1c1")
	})
	h.waitFor(t, "the branch to be checked out", func() bool {
		return calls.has("checkout:from-here")
	})
}

// The detail icons obey the same non-current-branch restriction as the menu:
// in particular, browsing another branch cannot reset the checked-out branch.
func TestOtherBranchCommitDetailOmitsHistoryEditIcons(t *testing.T) {
	h, _ := branchCommitHarness(t, true)
	h.tester.Frame()
	for _, label := range []string{
		"Reword this commit", "Soft reset to this commit",
		"Mixed reset to this commit", "Hard reset to this commit",
	} {
		if containsText(h.texts(), label) {
			t.Errorf("the other branch's detail pane offers %q", label)
		}
	}
	for _, label := range []string{
		"Copy commit hash", "Checkout this commit", "Create a tag here",
		"Create a branch here", "Cherry-pick this commit",
	} {
		if !containsText(h.texts(), label) {
			t.Errorf("the other branch's detail pane misses %q", label)
		}
	}
}

// Creating a tag from a commit's menu names that commit, not HEAD.
func TestCommitRowMenuRewordsTheSelectedCommit(t *testing.T) {
	h, calls := historyHarness(t)
	h.rightClick(t, "Second commit")
	h.chooseMenuItem(t, "Reword commit…")
	h.waitFor(t, "the full commit message", func() bool {
		return !h.app.commitRewordLoading && h.app.commitRewordBody == "Existing body"
	})
	if h.app.commitRewordCommit.Hash != "c2c2c2c2" || h.app.commitRewordSummary != "Second commit" {
		t.Fatalf("the reword form targets the wrong commit: %#v", h.app.commitRewordCommit)
	}

	h.app.commitRewordSummary = "Second commit, clarified"
	h.app.commitRewordBody = "A longer explanation"
	h.tester.Frame()
	if err := h.tester.Click("Reword commit"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("rewording history asked for no confirmation")
	}
	if calls.has("reword:c2c2c2c2:Second commit, clarified:A longer explanation") {
		t.Fatal("reword ran before confirmation")
	}
	if err := h.tester.Click("Reword commit"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the commit to be reworded", func() bool {
		return calls.has("reword:c2c2c2c2:Second commit, clarified:A longer explanation")
	})
	h.waitFor(t, "the reword form to close", func() bool { return !h.app.showCommitRewordForm })
}

func TestCommitHistoryActionsRequireConfirmation(t *testing.T) {
	tests := []struct {
		menu     string
		confirm  string
		wantCall string
	}{
		{menu: "Amend with staged changes…", confirm: "Amend commit", wantCall: "amend-to:c2c2c2c2"},
		{menu: "Create fixup commit from staged changes…", confirm: "Create fixup", wantCall: "fixup-for:c2c2c2c2"},
		{menu: "Apply fixups from this commit through HEAD…", confirm: "Apply fixups", wantCall: "apply-fixups:c2c2c2c2"},
		{menu: "Squash into older commit…", confirm: "Squash commit", wantCall: "combine:0:c2c2c2c2"},
		{menu: "Fixup into older commit…", confirm: "Fixup commit", wantCall: "combine:1:c2c2c2c2"},
		{menu: "Move commit toward HEAD", confirm: "Move commit", wantCall: "move:-1:c2c2c2c2"},
		{menu: "Move commit toward older history", confirm: "Move commit", wantCall: "move:1:c2c2c2c2"},
		{menu: "Drop commit…", confirm: "Drop commit", wantCall: "drop:c2c2c2c2"},
	}
	for _, test := range tests {
		t.Run(test.menu, func(t *testing.T) {
			h, calls := historyHarness(t)
			h.rightClick(t, "Second commit")
			h.chooseMenuItem(t, test.menu)
			h.tester.Frame()
			if h.app.confirm == nil {
				t.Fatal("history edit asked for no confirmation")
			}
			if calls.has(test.wantCall) {
				t.Fatal("history edit ran before confirmation")
			}
			if err := h.tester.Click(test.confirm); err != nil {
				t.Fatal(err)
			}
			h.waitFor(t, "the history edit", func() bool { return calls.has(test.wantCall) })
		})
	}
}

func TestRewordShortcutLoadsTheMessageAndEscapeCloses(t *testing.T) {
	h, _ := historyHarness(t)
	h.press(t, ui.KeyR)
	h.waitFor(t, "the reword form", func() bool {
		return h.app.showCommitRewordForm && !h.app.commitRewordLoading
	})
	h.press(t, ui.KeyEscape)
	if h.app.showCommitRewordForm {
		t.Fatal("Escape left the reword form open")
	}
}

func TestCommitRowMenuCreatesATagAtTheCommit(t *testing.T) {
	h, calls := historyHarness(t)
	h.rightClick(t, "Second commit")
	h.chooseMenuItem(t, "Create a tag here…")
	h.tester.Frame()
	if !h.app.showCommitTagForm {
		t.Fatal("the tag action opened no form")
	}
	if h.app.commitTagCommit.Hash != "c2c2c2c2" {
		t.Fatalf("the form is for %s, want the chosen commit", h.app.commitTagCommit.Hash)
	}

	h.app.commitTagName = "v9.0.0"
	h.tester.Frame()
	if err := h.tester.Click("Create tag"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("creating a tag asked no confirmation")
	}
	if err := h.tester.Click("Add tag"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the tag to be created", func() bool {
		return calls.has("tag:v9.0.0:c2c2c2c2")
	})
}

// Creating a branch from a commit starts it there and checks it out.
func TestCommitRowMenuCreatesABranchAtTheCommit(t *testing.T) {
	h, calls := historyHarness(t)
	h.rightClick(t, "Initial commit")
	h.chooseMenuItem(t, "Create a branch here…")
	h.tester.Frame()
	if !h.app.showCommitBranchForm {
		t.Fatal("the branch action opened no form")
	}
	if h.app.commitBranchCommit.Hash != "c1c1c1c1" {
		t.Fatalf("the form is for %s, want the chosen commit", h.app.commitBranchCommit.Hash)
	}

	h.app.commitBranchName = "from-here"
	h.tester.Frame()
	if err := h.tester.Click("Create and checkout"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("creating a branch asked no confirmation")
	}
	if err := h.tester.Click("Create branch"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the branch to be created", func() bool {
		return calls.has("branch:from-here:c1c1c1c1")
	})
	h.waitFor(t, "the branch to be checked out", func() bool {
		return calls.has("checkout:from-here")
	})
}

// Hovering a commit keeps its subject stable and leaves its actions in the
// detail pane, without adding a duplicate toolbar to the list row.
func TestHoveringACommitRowKeepsItsHeight(t *testing.T) {
	for _, fontSize := range []float64{9, 14} {
		t.Run(fmt.Sprintf("font %g", fontSize), func(t *testing.T) {
			h, _ := historyHarness(t)
			h.app.settings.FontSize = fontSize
			h.tester.Frame()

			row, ok := h.tester.Find("Initial commit")
			if !ok {
				t.Fatalf("the history list shows no Initial commit: %q", h.texts())
			}
			h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
			h.tester.Frame()

			hovered, _ := h.tester.Find("Initial commit")
			if hovered.H != row.H {
				t.Fatalf("hovering grew the commit row from %v to %v", row.H, hovered.H)
			}
			if hovered.W != row.W {
				t.Fatalf("hovering squeezed the commit subject from %v to %v", row.W, hovered.W)
			}
			count := 0
			for _, text := range h.texts() {
				if text == "Copy commit hash" {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("want only the detail pane's copy action, got %d", count)
			}
		})
	}
}

// The detail pane's icon bar offers every action without hovering a row.
func TestCommitDetailIconBar(t *testing.T) {
	h, _ := historyHarness(t)
	for _, label := range []string{
		"Copy commit hash",
		"Reword this commit",
		"Checkout this commit",
		"Soft reset to this commit",
		"Mixed reset to this commit",
		"Hard reset to this commit",
		"Create a tag here",
		"Create a branch here",
		"Cherry-pick this commit",
	} {
		if !containsText(h.texts(), label) {
			t.Fatalf("the detail bar misses %q: %q", label, h.texts())
		}
	}
}

// Every icon in the detail bar that changes the repository asks before it
// runs, as the same action from a row menu does.
func TestCommitIconActionsAskBeforeRunning(t *testing.T) {
	for _, action := range []struct {
		label string
		call  string
	}{
		{"Soft reset to this commit", "reset-soft:c2c2c2c2"},
		{"Mixed reset to this commit", "reset-mixed:c2c2c2c2"},
		{"Hard reset to this commit", "reset-hard:c2c2c2c2"},
		{"Checkout this commit", "checkout-commit:c2c2c2c2"},
		{"Cherry-pick this commit", "cherry-pick:c2c2c2c2"},
	} {
		h, calls := historyHarness(t)
		if err := h.tester.Click(action.label); err != nil {
			t.Fatalf("%s: %v", action.label, err)
		}
		h.tester.Frame()
		if h.app.confirm == nil {
			t.Fatalf("%s ran without a confirmation", action.label)
		}
		h.press(t, ui.KeyEscape)
		if h.app.confirm != nil {
			t.Fatalf("%s: Escape left the confirmation open", action.label)
		}
		// The pane reads the commit's file diff in the background, so only
		// the action itself says whether the cancel held.
		if calls.has(action.call) {
			t.Fatalf("%s ran after cancelling", action.label)
		}
	}
}

// The expand icon opens the active diff over the whole window, and Escape
// closes it.
func TestDiffExpandsToTheWholeWindow(t *testing.T) {
	h := menuTestApp(t)
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil })
	if err := h.tester.Click("Expand diff"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.diffFullscreen {
		t.Fatal("the expand icon did not open the full-window diff")
	}
	if !containsText(h.texts(), "Restore diff view") {
		t.Fatalf("the full-window diff has no close control: %q", h.texts())
	}

	h.press(t, ui.KeyEscape)
	if h.app.diffFullscreen {
		t.Fatal("Escape left the full-window diff open")
	}
}

func containsText(texts []string, want string) bool {
	for _, text := range texts {
		if text == want {
			return true
		}
	}
	return false
}

// The detail pane opens a file as soon as it knows the files a commit
// changed: the first one, whose diff it then reads.
func TestTheCommitPaneOpensItsFirstFile(t *testing.T) {
	h, _ := historyHarness(t)
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.waitFor(t, "the first file's diff", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	if h.app.selectedCommitFile != 0 {
		t.Fatalf("the pane chose file %d of %d", h.app.selectedCommitFile, len(h.app.commitFiles))
	}
}

// A list holds its choice by the file's path, so switching commits leaves it
// with no choice when the new commit lacks that path. The pane chooses its
// first file again rather than showing the files with none open and no diff.
func TestTheCommitPaneChoosesAFileAgainWhenItsChoiceIsGone(t *testing.T) {
	h, _ := historyHarness(t)
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.waitFor(t, "the first file's diff", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	// What the list does to a choice its rows no longer hold.
	h.app.selectedCommitFile = -1
	h.app.commitDiffState = nil
	h.app.commitDiffHash = ""
	h.app.commitDiffPath = ""
	h.tester.Frame()

	if h.app.selectedCommitFile != 0 {
		t.Fatalf("the pane left file %d chosen, want the first", h.app.selectedCommitFile)
	}
	h.waitFor(t, "the first file's diff again", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
}

// A diff belongs to the commit it was read for. The same path in two commits
// is two reads, so one commit's change is never shown under the other's
// subject while the pane follows the choice down the list.
func TestTheCommitDiffFollowsTheCommitChosen(t *testing.T) {
	h, calls := historyHarness(t)
	h.waitFor(t, "the newest commit's diff", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	if h.app.commitDiffHash != "c2c2c2c2" {
		t.Fatalf("the pane read %q first, want the newest commit", h.app.commitDiffHash)
	}
	// The fixture's two commits changed the same paths, so what moves is the
	// commit, not the file.
	h.app.selectedCommit = 1
	h.tester.Frame()
	h.waitFor(t, "the older commit's diff", func() bool {
		return h.app.commitDiffHash == "c1c1c1c1" && !h.app.commitDiffLoading &&
			h.app.commitDiffState != nil
	})
	if h.app.commitDiffPath != "one.txt" {
		t.Fatalf("the pane is showing %q, want the file both commits changed", h.app.commitDiffPath)
	}
	if !calls.has("commit-file-diff:c1c1c1c1:one.txt") {
		t.Fatalf("the pane never read the older commit's change: %v", calls.all())
	}
}

// The files and the diff share the drawer, with a line between them the user
// drags: the diff is why the pane is open, and the list of files is often one
// row.
func TestTheCommitFilesAndDiffShareTheirHeight(t *testing.T) {
	h, _ := historyHarness(t)
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.waitFor(t, "the first file's diff", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})

	row, ok := h.tester.Find("one.txt")
	if !ok {
		t.Fatalf("the pane lists no file: %q", h.texts())
	}
	// The list's header starts at the top of its pane, and the line is the
	// pane's height below that, including the tree controls above the rows.
	header, ok := h.tester.Find("Commit files")
	if !ok {
		t.Fatalf("the pane has no files header: %q", h.texts())
	}
	lineY := func() float32 { return header.Y + h.app.commitFilesHeight }
	x := row.X + 200

	before := h.app.commitFilesHeight
	dragTo(t, h.tester, x, lineY(), 0, -60)
	if h.app.commitFilesHeight > before-40 {
		t.Fatalf("dragging the line up left the files %v high, was %v",
			h.app.commitFilesHeight, before)
	}
	// Dragging it back down gives the files their room again.
	grown := h.app.commitFilesHeight
	dragTo(t, h.tester, x, lineY(), 0, 60)
	if h.app.commitFilesHeight < grown+40 {
		t.Fatalf("dragging the line down left the files %v high, was %v",
			h.app.commitFilesHeight, grown)
	}
}

// cherryPickConflictSession answers with a clean snapshot until the
// cherry-pick runs, then with the conflict the command left behind, as Git
// does: a cherry-pick that stops on a conflict exits with an error.
type cherryPickConflictSession struct {
	fakeSession
	mu      sync.Mutex
	stopped bool
}

func (s *cherryPickConflictSession) CherryPickCommit(_ context.Context, commit repository.Commit) error {
	s.calls.record("cherry-pick:" + commit.Hash)
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	return errors.New("cherry-pick commit: could not apply " + commit.ShortHash + "... feature")
}

func (s *cherryPickConflictSession) Snapshot(context.Context) (repository.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.fakeSession.snapshot
	if !s.stopped {
		return snapshot, nil
	}
	snapshot.Operation = "cherry-pick"
	snapshot.Files = []repository.File{
		{Path: "f.txt", Status: "UU", Unstaged: true, Tracked: true, HasMergeConflicts: true},
	}
	return snapshot, nil
}

// A cherry-pick that stops on a conflict exits with an error while it leaves
// the conflict in the working tree: the Changes section must show what it left
// without a manual refresh, so switching there after the error finds the merge.
func TestACherryPickStoppedOnAConflictShowsInChanges(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) {
		return []byte("<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\n"), nil
	}

	calls := &callRecorder{}
	session := &cherryPickConflictSession{fakeSession: fakeSession{calls: calls, snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Files: []repository.File{
			{Path: "keep.txt", Status: " M", Unstaged: true, Tracked: true},
		},
		Commits: []repository.Commit{
			{Hash: "c2c2c2c2", ShortHash: "c2c2c2c", Subject: "Second commit", AuthorName: "Ada", AuthorEmail: "ada@example.com", Parents: []string{"c1c1c1c1"}},
			{Hash: "c1c1c1c1", ShortHash: "c1c1c1c", Subject: "Initial commit", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		},
	}}}
	a := newApp(fixedSessionOpener{session: session})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	a.workspaceTab = sectionHistory
	h.tester.Frame()
	h.settle(t)

	if err := h.tester.Click("Cherry-pick this commit"); err != nil {
		t.Fatalf("cherry-pick the commit: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Cherry-pick"); err != nil {
		t.Fatalf("confirm the cherry-pick: %v", err)
	}
	h.waitFor(t, "the stopped cherry-pick", func() bool {
		return a.snapshot.Operation == "cherry-pick" && !a.actionBusy
	})

	if index := changeRowIndex(a.changeRows, "f.txt", false); index < 0 {
		t.Fatalf("the conflict is not among the change rows: %+v", a.changeRows)
	}
	a.setWorkspaceTab(sectionChanges)
	h.tester.Frame()
	h.settle(t)
	if !containsText(h.texts(), "Merge Changes") {
		t.Fatalf("the Changes section shows no merge: %q", h.texts())
	}
}
