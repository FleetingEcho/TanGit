package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// openFileActions opens the ⋯ menu of the file the detail pane shows.
func (h *harness) openFileActions(t *testing.T) {
	t.Helper()
	if err := h.tester.Click("File actions"); err != nil {
		t.Fatalf("open the file actions menu: %v; texts %q", err, h.texts())
	}
	h.tester.Frame()
}

func (h *harness) menuHasItem(label string) bool {
	for _, text := range h.texts() {
		if text == label {
			return true
		}
	}
	return false
}

// The detail header's ⋯ menu offers Blame on every file that exists, including
// a file added in this change that HEAD does not have yet: its lines are shown
// as new work instead of the item being missing.
func TestFileActionsMenuOffersBlameForANewFile(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "new.go", Status: "A ", Staged: true, Added: true},
	})
	h.waitFor(t, "the file's diff", func() bool { return h.app.diffState != nil })
	h.openFileActions(t)
	h.requireMenuLabels(t, "Blame", "Unstage", "Stash selected file…", "Discard changes…")
	h.chooseMenuItem(t, "Blame")
	h.waitFor(t, "the blame", func() bool { return h.app.blameState != nil })
	if !calls.has("blame-working-tree:new.go") {
		t.Fatalf("a new file was not blamed from the working tree: %q", calls.all())
	}
	if calls.has("blame-file:new.go") {
		t.Fatal("a new file was blamed at HEAD, where it does not exist")
	}
	if !h.tester.HasText("Blame · new file, not committed yet") {
		t.Fatalf("the blame pane does not say the file is new: %q", h.texts())
	}
	h.clickAndSettle(t, "Close blame")
	if h.app.blameState != nil {
		t.Fatal("Close blame left the blame open")
	}
}

// A tracked file is blamed at HEAD, as before.
func TestFileActionsMenuBlamesATrackedFileAtHead(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the file's diff", func() bool { return h.app.diffState != nil })
	h.openFileActions(t)
	h.chooseMenuItem(t, "Blame")
	h.waitFor(t, "the blame", func() bool { return h.app.blameState != nil })
	if !calls.has("blame-file:changed.txt") || calls.has("blame-working-tree:changed.txt") {
		t.Fatalf("a tracked file was not blamed at HEAD: %q", calls.all())
	}
	if !h.tester.HasText("Blame at HEAD") {
		t.Fatalf("the blame pane lost its HEAD title: %q", h.texts())
	}
}

// An untracked file is just as new, and a deleted file has nothing to blame.
func TestBlameIsOfferedOnlyForFilesThatExist(t *testing.T) {
	t.Run("untracked", func(t *testing.T) {
		h, calls := changesTestApp(t, []repository.File{{Path: "scratch.txt", Status: "??", Unstaged: true, Added: true}})
		h.waitFor(t, "the file's diff", func() bool { return h.app.diffState != nil })
		h.openFileActions(t)
		h.chooseMenuItem(t, "Blame")
		h.waitFor(t, "the blame", func() bool { return h.app.blameState != nil })
		if !calls.has("blame-working-tree:scratch.txt") {
			t.Fatalf("an untracked file was not blamed from the working tree: %q", calls.all())
		}
	})
	t.Run("deleted", func(t *testing.T) {
		h, _ := changesTestApp(t, []repository.File{{Path: "gone.txt", Status: " D", Unstaged: true, Tracked: true, Deleted: true}})
		h.tester.Frame()
		h.openFileActions(t)
		if h.menuHasItem("Blame") {
			t.Fatalf("a deleted file offers Blame: %q", h.texts())
		}
	})
}

// The file row's own menu offers the same Blame for a new file.
func TestFileRowMenuOffersBlameForANewFile(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "new.go", Status: "A ", Staged: true, Added: true},
	})
	h.rightClick(t, "new.go")
	h.requireMenuLabels(t, "Blame")
	h.chooseMenuItem(t, "Blame")
	h.waitFor(t, "the blame", func() bool { return calls.has("blame-working-tree:new.go") })
}

// The blame view is a report, not a place to work. With the cursor in it, the
// keys that act on the chosen file (Space to stage, D to discard, I to ignore)
// must do nothing, typing must not change the text, and Escape must close it.
func TestBlameViewIsReadOnlyAndKeysDoNotActOnTheFile(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the file's diff", func() bool { return h.app.diffState != nil })
	h.openFileActions(t)
	h.chooseMenuItem(t, "Blame")
	h.waitFor(t, "the blame", func() bool { return h.app.blameState != nil })
	h.tester.Frame()

	// Put the cursor in the report, as clicking a line does.
	var pane ui.AccessInfo
	for _, node := range h.tester.AccessAll() {
		if (node.Name == "Git blame output" || node.Name == "Blame") && node.Bounds.W*node.Bounds.H > pane.Bounds.W*pane.Bounds.H {
			pane = node
		}
	}
	if pane.Bounds.W == 0 {
		t.Fatalf("the blame pane is not on screen: %q", h.texts())
	}
	h.tester.ClickAt(pane.Bounds.X+pane.Bounds.W/2, pane.Bounds.Y+pane.Bounds.H/4)
	h.tester.Frame()

	before := h.app.blameState.Buffer.String()
	h.press(t, ui.KeySpace)
	h.press(t, ui.KeyD)
	h.press(t, ui.KeyI)
	h.press(t, ui.KeyA)
	h.tester.Type("typed text")
	h.tester.Frame()
	h.settle(t)

	for _, call := range calls.all() {
		for _, prefix := range []string{"stage", "unstage", "discard", "ignore", "exclude", "stash", "commit"} {
			if strings.HasPrefix(call, prefix) {
				t.Fatalf("a key pressed over the blame acted on the file: %q in %q", call, calls.all())
			}
		}
	}
	if h.app.confirm != nil || h.app.showStashForm || h.app.showCommitForm {
		t.Fatalf("a key over the blame opened a dialog: confirm=%v stash=%v commit=%v",
			h.app.confirm != nil, h.app.showStashForm, h.app.showCommitForm)
	}
	if after := h.app.blameState.Buffer.String(); after != before {
		t.Fatalf("the blame text was edited:\nbefore %q\nafter  %q", before, after)
	}
	if !h.app.blameState.ReadOnly {
		t.Fatal("the blame editor is not read-only")
	}

	// Escape closes the report, and the file's keys work again.
	h.press(t, ui.KeyEscape)
	h.settle(t)
	if h.app.blameState != nil {
		t.Fatal("Escape did not close the blame")
	}
	h.clickAndSettle(t, "changed.txt")
	h.press(t, ui.KeySpace)
	h.waitFor(t, "Space to stage the file once the blame is closed", func() bool { return calls.has("stage:changed.txt") })
}

// An open blame follows the selected file: choosing another file reads that
// file's blame in place of the old one, and a file with nothing to blame
// closes it, so the report never describes a file other than the chosen one.
func TestOpenBlameFollowsTheSelectedFile(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "alpha.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "beta.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "gone.txt", Status: " D", Unstaged: true, Tracked: true, Deleted: true},
	})
	h.waitFor(t, "the first file's diff", func() bool { return h.app.diffState != nil })
	h.openFileActions(t)
	h.chooseMenuItem(t, "Blame")
	h.waitFor(t, "alpha's blame", func() bool { return h.app.blameState != nil && h.app.blamePath == "alpha.txt" })
	if !calls.has("blame-file:alpha.txt") {
		t.Fatalf("alpha.txt was not blamed first: %q", calls.all())
	}

	h.clickAndSettle(t, "beta.txt")
	h.waitFor(t, "beta's blame", func() bool { return h.app.blameState != nil && h.app.blamePath == "beta.txt" })
	if !calls.has("blame-file:beta.txt") {
		t.Fatalf("choosing beta.txt did not blame it: %q", calls.all())
	}
	if !h.tester.HasText("Blame at HEAD") {
		t.Fatalf("the blame pane went away when the selection moved: %q", h.texts())
	}

	// Choosing the same file again does not read it again.
	count := 0
	for _, call := range calls.all() {
		if call == "blame-file:beta.txt" {
			count++
		}
	}
	h.clickAndSettle(t, "beta.txt")
	again := 0
	for _, call := range calls.all() {
		if call == "blame-file:beta.txt" {
			again++
		}
	}
	if again != count {
		t.Fatalf("re-choosing the same file blamed it %d more time(s)", again-count)
	}

	// A deleted file has nothing to blame, so the report closes.
	h.clickAndSettle(t, "gone.txt")
	if h.app.blameState != nil || h.app.blameLoading {
		t.Fatal("the blame stayed open on a file that cannot be blamed")
	}
}
