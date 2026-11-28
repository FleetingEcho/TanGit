package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func changesHunkApp(t *testing.T, files []repository.File, patchText string) (*harness, *callRecorder) {
	t.Helper()
	calls := &callRecorder{}
	a := newApp(fakeOpener{
		result: openResult{snapshot: repository.Snapshot{Root: "/work/project", Branch: "main", Files: files}},
		calls:  calls,
		patch:  patchText,
	})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, calls
}

func twoHunkChangesApp(t *testing.T, files []repository.File) (*harness, *callRecorder) {
	t.Helper()
	h, calls := changesHunkApp(t, files, twoHunkPatch)
	h.waitFor(t, "the two-hunk diff", func() bool { return h.app.diffState != nil })
	h.app.hunkMode = true
	h.tester.Frame()
	return h, calls
}

func TestChangesOfferHunkAndSelectedLineActionsTogether(t *testing.T) {
	h, _ := twoHunkChangesApp(t, unstagedTrackedFile())
	for _, action := range []string{"Stage hunk", "Discard hunk", "Previous hunk", "Next hunk"} {
		if !h.tester.HasText(action) {
			t.Errorf("hunk action %q is missing", action)
		}
	}
	if h.tester.HasText("Unstage hunk") || h.tester.HasText("Stash hunk") {
		t.Fatal("the working-tree side offers an action for the wrong target")
	}
	clickDiffLine(t, h, editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
	for _, action := range []string{"Stage selected lines", "Stash selected lines", "Stage hunk"} {
		if !h.tester.HasText(action) {
			t.Errorf("action %q disappeared after selecting lines", action)
		}
	}
}

func TestCurrentHunkButtonsNavigateAndStageOnlyOneHunk(t *testing.T) {
	h, calls := twoHunkChangesApp(t, []repository.File{{Path: "app.go", Status: " M", Unstaged: true, Tracked: true}})
	if !h.tester.HasText("Hunk 1/3") {
		t.Fatalf("initial hunk position is missing: %q", h.texts())
	}
	if hunk, _ := h.app.diffState.CurrentHunk(); hunk != 0 {
		t.Fatalf("initial hunk = %d", hunk)
	}
	if err := h.tester.Click("Next hunk"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if hunk, _ := h.app.diffState.CurrentHunk(); hunk != 1 || !h.tester.HasText("Hunk 2/3") {
		t.Fatalf("next did not select hunk 2/3: %d, %q", hunk, h.texts())
	}
	if err := h.tester.Click("Previous hunk"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if hunk, _ := h.app.diffState.CurrentHunk(); hunk != 0 {
		t.Fatalf("previous selected hunk %d", hunk)
	}
	if err := h.tester.Click("Next hunk"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Stage hunk"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the current hunk patch", func() bool { return findCall(calls, "apply-patch:false:") != "" })
	sent := findCall(calls, "apply-patch:false:")
	if !strings.Contains(sent, "+FOUR") || strings.Contains(sent, "+TWO") || strings.Contains(sent, "ten and a half") {
		t.Fatalf("stage changed a different hunk:\n%s", sent)
	}
	if findCall(calls, "apply-worktree:") != "" || calls.has("stage:app.go") {
		t.Fatalf("hunk stage took the worktree or whole-file route: %v", calls.all())
	}
}

func TestUnstageHunkUsesReverseIndexPatchOnly(t *testing.T) {
	h, calls := twoHunkChangesApp(t, []repository.File{{Path: "app.go", Status: "M ", Staged: true, Tracked: true}})
	if !h.tester.HasText("Unstage hunk") || h.tester.HasText("Discard hunk") {
		t.Fatalf("staged hunk actions are wrong: %q", h.texts())
	}
	if err := h.tester.Click("Unstage hunk"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "reverse cached patch", func() bool { return findCall(calls, "apply-patch:true:") != "" })
	sent := findCall(calls, "apply-patch:true:")
	if !strings.Contains(sent, "-two") || !strings.Contains(sent, "+TWO") || strings.Contains(sent, "+FOUR") || strings.Contains(sent, "ten and a half") {
		t.Fatalf("unstage sent the wrong hunk or direction:\n%s", sent)
	}
	if findCall(calls, "apply-worktree:") != "" {
		t.Fatal("unstaging touched the working tree")
	}
}

func TestDiscardHunkConfirmsThenReversesOnlyTheWorktreeHunk(t *testing.T) {
	h, calls := twoHunkChangesApp(t, []repository.File{{Path: "app.go", Status: " M", Unstaged: true, Tracked: true}})
	if err := h.tester.Click("Discard hunk"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.title != "Discard Hunk" {
		t.Fatalf("discard did not ask first: %#v", h.app.confirm)
	}
	if !strings.Contains(h.app.confirm.detail, "index") || !strings.Contains(h.app.confirm.detail, "Saved work") {
		t.Fatalf("confirmation does not explain index safety and recovery: %q", h.app.confirm.detail)
	}
	if _, found := h.tester.AccessByRole(ui.RoleDialog, "Discard Hunk"); !found {
		t.Fatal("discard confirmation dialog is not accessible")
	}
	if findCall(calls, "apply-worktree:") != "" {
		t.Fatal("discard ran before confirmation")
	}
	h.app.acceptConfirm()
	h.tester.Frame()
	h.waitFor(t, "worktree reverse patch", func() bool { return findCall(calls, "apply-worktree:true:") != "" })
	sent := findCall(calls, "apply-worktree:true:")
	if !strings.Contains(sent, "\n-two\n") || !strings.Contains(sent, "\n+TWO\n") ||
		strings.Contains(sent, "\n+FOUR\n") || strings.Contains(sent, "\n+ten and a half\n") {
		t.Fatalf("discard built the wrong reverse hunk:\n%s", sent)
	}
	if findCall(calls, "apply-patch:") != "" {
		t.Fatal("discard used the cached/index route")
	}
}

func TestMixedStagedAndUnstagedHunksUseTheirOwnSide(t *testing.T) {
	files := []repository.File{{
		Path: "app.go", Status: "MM", Staged: true, Unstaged: true, Tracked: true,
		LinesAdded: 4, LinesDeleted: 2, StagedLinesAdded: 2, StagedLinesDeleted: 1,
		UnstagedLinesAdded: 2, UnstagedLinesDeleted: 1,
	}}
	t.Run("staged row", func(t *testing.T) {
		h, calls := twoHunkChangesApp(t, files)
		h.waitFor(t, "staged side", func() bool { return h.app.diffKind == "Staged" })
		if err := h.tester.Click("Unstage hunk"); err != nil {
			t.Fatal(err)
		}
		h.waitFor(t, "unstage", func() bool { return findCall(calls, "apply-patch:true:") != "" })
		if findCall(calls, "apply-worktree:") != "" || findCall(calls, "apply-patch:false:") != "" {
			t.Fatalf("staged side mutated another target: %v", calls.all())
		}
	})
	t.Run("unstaged row", func(t *testing.T) {
		h, calls := twoHunkChangesApp(t, files)
		h.app.selectedFile = changeRowIndex(h.app.changeRows, "app.go", false)
		h.app.resetFileDiff()
		h.tester.Frame()
		h.waitFor(t, "unstaged side", func() bool { return h.app.diffKind == "Unstaged" })
		if err := h.tester.Click("Stage hunk"); err != nil {
			t.Fatal(err)
		}
		h.waitFor(t, "stage", func() bool { return findCall(calls, "apply-patch:false:") != "" })
		if findCall(calls, "apply-worktree:") != "" || findCall(calls, "apply-patch:true:") != "" {
			t.Fatalf("unstaged side mutated another target: %v", calls.all())
		}
	})
}

func TestFocusedDiffHunkShortcutsAndHelp(t *testing.T) {
	h, calls := twoHunkChangesApp(t, []repository.File{{Path: "app.go", Status: " M", Unstaged: true, Tracked: true}})
	if err := h.tester.Click("Diff"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.keyboardDiffCanMutateHunks {
		t.Fatal("the focused Changes diff did not acquire its hunk scope")
	}
	// Mouse focus lands wherever the tester clicked; start at a known hunk
	// without changing the focused-pane scope.
	h.app.diffState.GoToHunk(0)
	h.tester.Key(0, ui.KeyBracketRight)
	h.tester.Frame()
	if hunk, _ := h.app.diffState.CurrentHunk(); hunk != 1 {
		t.Fatalf("] selected hunk %d", hunk)
	}
	h.tester.Key(0, ui.KeyS)
	h.waitFor(t, "hunk shortcut", func() bool { return findCall(calls, "apply-patch:false:") != "" })
	if sent := findCall(calls, "apply-patch:false:"); !strings.Contains(sent, "+FOUR") || strings.Contains(sent, "+TWO") || strings.Contains(sent, "ten and a half") {
		t.Fatalf("s acted on the wrong hunk: %s", sent)
	}

	h, _ = twoHunkChangesApp(t, []repository.File{{Path: "app.go", Status: " M", Unstaged: true, Tracked: true}})
	if err := h.tester.Click("Diff"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Key(0, ui.KeyD)
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.title != "Discard Hunk" {
		t.Fatalf("d did not open hunk-discard confirmation: hunkScope=%v confirm=%#v", h.app.keyboardDiffCanMutateHunks, h.app.confirm)
	}
	h.app.cancelConfirm()
	h.tester.Frame()
	h.tester.Key(ui.Shift, ui.KeySlash)
	h.tester.Frame()
	for _, help := range []string{"Stage or unstage the current diff hunk", "Discard the current working-tree diff hunk after confirmation"} {
		if !h.tester.HasText(help) {
			t.Errorf("focused help is missing %q: %q", help, h.texts())
		}
	}
}

func TestUnsupportedHunkActionsStayVisibleAndExplainWhy(t *testing.T) {
	binaryPatch := "diff --git a/binary.dat b/binary.dat\nindex 1111111..2222222 100644\nBinary files a/binary.dat and b/binary.dat differ\n"
	modePatch := "diff --git a/mode.sh b/mode.sh\nold mode 100644\nnew mode 100755\n"
	for _, test := range []struct {
		name   string
		files  []repository.File
		patch  string
		reason string
	}{
		{"binary", []repository.File{{Path: "binary.dat", Status: " M", Unstaged: true, Tracked: true}}, binaryPatch, "Binary changes cannot be divided into hunks"},
		{"mode only", []repository.File{{Path: "mode.sh", Status: " M", Unstaged: true, Tracked: true}}, modePatch, "Mode-only changes cannot be divided into hunks"},
		{"combined conflict", []repository.File{{Path: "conflict.txt", Status: "UU", Unstaged: true, Tracked: true, HasMergeConflicts: true}}, twoHunkPatch, "Combined conflict diffs cannot be staged or discarded by hunk"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, _ := changesHunkApp(t, test.files, test.patch)
			h.app.hunkMode = true
			h.tester.Frame()
			if !test.files[0].HasMergeConflicts {
				h.waitFor(t, "unsupported diff", func() bool { return h.app.diffPatch == test.patch })
				h.tester.Frame()
			}
			if !h.tester.HasText("Stage hunk") || !h.tester.HasText(test.reason) {
				t.Fatalf("unsupported action or reason is not visible: %q", h.texts())
			}
			button, ok := h.tester.Access("Stage hunk")
			if !ok || !button.Disabled || button.Bounds.W <= 0 || button.Bounds.H <= 0 {
				t.Fatalf("unsupported Stage hunk is not visibly disabled: %+v, found=%v", button, ok)
			}
			target, _ := h.tester.Find("Stage hunk")
			h.tester.Move(target.X+target.W/2, target.Y+target.H/2)
			h.tester.Advance(time.Second)
			h.tester.Frame()
			if !h.tester.HasText(test.reason) {
				t.Fatal("disabled hunk action did not retain its explanation on hover")
			}
		})
	}
}

func TestChangedFileNavigationButtonsAndShortcuts(t *testing.T) {
	h, _ := twoHunkChangesApp(t, []repository.File{
		{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true},
		{Path: "b.txt", Status: " M", Unstaged: true, Tracked: true},
	})
	h.tester.Key(ui.Alt, ui.KeyK)
	h.tester.Frame()
	if change, _ := h.app.selectedChange(); change.file.Path != "b.txt" {
		t.Fatalf("previous shortcut selected %q", change.file.Path)
	}
	if err := h.tester.Click("Next changed file"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if change, _ := h.app.selectedChange(); change.file.Path != "a.txt" {
		t.Fatalf("next selected %q", change.file.Path)
	}
	if err := h.tester.Click("Previous changed file"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if change, _ := h.app.selectedChange(); change.file.Path != "b.txt" {
		t.Fatalf("previous button did not wrap to %q", change.file.Path)
	}
}

// The hunk actions stay out of the way until hunk mode is entered from the
// toolbar's single icon button, and leaving it hides them again.
func TestHunkActionsAreHiddenUntilHunkMode(t *testing.T) {
	h, _ := twoHunkChangesApp(t, unstagedTrackedFile())
	h.app.hunkMode = false
	h.app.stopLineSelection()
	h.tester.Frame()
	for _, action := range []string{"Stage hunk", "Discard hunk", "Next hunk"} {
		if h.tester.HasText(action) {
			t.Fatalf("%q is shown outside hunk mode", action)
		}
	}
	if err := h.tester.Click("Select changes"); err != nil {
		t.Fatalf("enter hunk mode: %v", err)
	}
	h.tester.Frame()
	if !h.app.hunkMode || !h.tester.HasText("Hunk 1/3") {
		t.Fatalf("hunk mode did not show the hunk actions: %q", h.texts())
	}
	if err := h.tester.Click("Stop selecting changes"); err != nil {
		t.Fatalf("leave hunk mode: %v", err)
	}
	h.tester.Frame()
	if h.app.hunkMode || h.tester.HasText("Next hunk") {
		t.Fatal("hunk actions remain after leaving hunk mode")
	}
}

type failingApplySession struct{ fakeSession }

func (failingApplySession) ApplyPatch(context.Context, repository.PatchPreview) error {
	return errors.New("apply patch: error: patch failed: internal/app/i18n/i18n_palette_modes.go:3\nerror: internal/app/i18n/i18n_palette_modes.go: patch does not apply")
}

// A hunk patch that Git refuses (the file changed after its diff loaded) is
// reported, the diff is read again, and the Changes page keeps working: a later
// attempt against the fresh state goes through.
func TestRejectedHunkPatchLeavesTheChangesPageUsable(t *testing.T) {
	h, calls := twoHunkChangesApp(t, []repository.File{{Path: "app.go", Status: " M", Unstaged: true, Tracked: true}})
	good := h.app.session.(fakeSession)
	h.app.session = failingApplySession{fakeSession: good}
	diffReads := func() int {
		count := 0
		for _, call := range calls.all() {
			if strings.HasPrefix(call, "diff:") {
				count++
			}
		}
		return count
	}
	before := diffReads()
	if err := h.tester.Click("Stage hunk"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the rejected action to finish", func() bool { return !h.app.actionBusy && h.app.actionError != "" })
	if !strings.Contains(h.app.actionError, "patch does not apply") {
		t.Fatalf("error = %q", h.app.actionError)
	}
	h.waitFor(t, "the diff to be read again", func() bool { return diffReads() > before && h.app.diffState != nil && !h.app.diffLoading })
	h.tester.Frame()
	if _, ok := h.tester.Find("Stage hunk"); !ok {
		t.Fatalf("the hunk toolbar is gone after the rejection: %q", h.texts())
	}
	h.app.session = good
	if err := h.tester.Click("Stage hunk"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "a later attempt", func() bool { return findCall(calls, "apply-patch:false:") != "" })
}
