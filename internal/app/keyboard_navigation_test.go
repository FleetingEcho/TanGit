package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestKeyboardLaunchOpensRecentRepositoryWithoutMouse(t *testing.T) {
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{Root: "/w/project", Branch: "main"}}})
	a.recentRepositories = []recentRepository{{Path: "/w/project", LastOpened: timeNow()}}
	h := &harness{app: a, updates: newUpdates()}
	a.update = h.updates.update
	h.tester = ui.NewTester(a.view, 1280, 800)
	h.tester.Frame()
	if !h.tester.Focused("Repository folder") {
		t.Fatal("launch does not focus repository path")
	}
	for i := 0; i < 20 && !h.tester.Focused("Open /w/project"); i++ {
		h.tester.Key(0, ui.KeyTab)
		h.tester.Frame()
	}
	if !h.tester.Focused("Open /w/project") {
		t.Fatal("Tab never reaches recent repository Open action")
	}
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	h.waitFor(t, "recent repository open", func() bool { return a.state == stateReady })
	if a.snapshot.Root != "/w/project" {
		t.Fatal("keyboard opened the wrong repository")
	}
}

func TestKeyboardF6CyclesVisiblePanelsBothDirections(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "diff", func() bool { return h.app.diffState != nil })
	h.tester.Frame()
	if !h.tester.Focused("Changes") {
		t.Fatal("workspace did not focus its file list")
	}
	h.tester.Key(0, ui.KeyF6)
	h.tester.Frame()
	if !h.tester.Focused("Diff") {
		t.Fatalf("F6 did not move from file list to diff: %v", h.texts())
	}
	h.tester.Key(ui.Shift, ui.KeyF6)
	h.tester.Frame()
	if !h.tester.Focused("Changes") {
		t.Fatal("Shift+F6 did not return to files")
	}
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	if !h.tester.Focused("Diff") {
		t.Fatal("Enter did not focus diff")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if !h.tester.Focused("Changes") {
		t.Fatal("Escape did not return to files")
	}
}

func TestKeyboardPaneOutlineFollowsInputModality(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "diff", func() bool { return h.app.diffState != nil })
	h.tester.Frame()
	if !h.tester.Focused("Changes") || h.app.keyboardFocusVisible {
		t.Fatal("automatic initial focus drew a keyboard pane outline")
	}

	h.tester.Key(0, ui.KeyF6)
	h.tester.Frame()
	if !h.tester.Focused("Diff") || !h.app.keyboardFocusVisible {
		t.Fatal("F6 did not show the outline of its keyboard-focused pane")
	}
	h.tester.Key(ui.Shift, ui.KeyF6)
	h.tester.Frame()
	if !h.tester.Focused("Changes") || !h.app.keyboardFocusVisible {
		t.Fatal("Shift+F6 did not keep the keyboard pane outline visible")
	}

	if err := h.tester.Click("changed.txt"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.tester.Focused("Changes") || h.app.keyboardFocusVisible {
		t.Fatal("pointer selection left a keyboard outline on the file pane")
	}
	h.tester.Key(0, ui.KeyJ)
	h.tester.Frame()
	if !h.tester.Focused("Changes") || !h.app.keyboardFocusVisible {
		t.Fatal("using the list from the keyboard did not restore its outline")
	}
}

func TestKeyboardJKMovesSelectedFileAndUsesItsMenu(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{{Path: "first.txt", Status: " M", Tracked: true, Unstaged: true}, {Path: "second.txt", Status: " M", Tracked: true, Unstaged: true}})
	h.tester.Key(0, ui.KeyJ)
	h.tester.Frame()
	selected, ok := h.app.selectedChange()
	if !ok || selected.file.Path != "second.txt" {
		t.Fatalf("j selected wrong file: %+v", selected)
	}
	h.tester.Key(ui.Shift, ui.KeyF10)
	h.tester.Frame()
	h.requireMenuLabels(t, "Stage", "Discard changes…")
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	h.tester.Key(0, ui.KeySpace)
	h.tester.Frame()
	h.waitFor(t, "stage selected file", func() bool { return calls.has("stage:second.txt") })
}

func TestKeyboardScopeProtectsFilePanelFromCommitDrop(t *testing.T) {
	h, calls := historyHarness(t)
	h.app.selectedCommit = 0
	h.tester.Frame()
	h.waitFor(t, "commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	if !h.tester.Focused("Commit files") {
		t.Fatal("Enter did not focus commit files")
	}
	h.tester.Key(0, ui.KeyD)
	h.tester.Frame()
	if h.app.confirm != nil || calls.has("drop:c2c2c2c2") {
		t.Fatal("d in commit files attempted to drop the commit")
	}
	h.tester.Key(ui.Shift, ui.KeyF10)
	h.tester.Frame()
	h.requireMenuLabels(t, "Copy relative path")
}

func TestKeyboardTextInputKeepsLetters(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.focusActiveSearch()
	h.tester.Frame()
	h.tester.TypeKey(0, ui.KeyP, "p")
	h.tester.TypeKey(0, ui.KeyJ, "j")
	h.tester.Frame()
	if h.app.fileSearch != "pj" || h.app.confirm != nil {
		t.Fatalf("letters triggered operations instead of typing: %q", h.app.fileSearch)
	}
}

func TestKeyboardLazygitPullPushAndCopyPaste(t *testing.T) {
	h, calls := historyHarness(t)
	h.app.selectedCommit = 0
	h.tester.Frame()
	h.tester.Key(0, ui.KeyP)
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.title != "Pull" {
		t.Fatal("p did not ask to pull")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	h.tester.Key(ui.Shift, ui.KeyP)
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.title != "Push" {
		t.Fatal("P did not ask to push")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	h.tester.Key(ui.Shift, ui.KeyC)
	h.tester.Frame()
	if len(h.app.keyboardCopied) != 1 {
		t.Fatal("C did not copy selected commit")
	}
	h.tester.Key(ui.Shift, ui.KeyV)
	h.tester.Frame()
	if h.app.confirm == nil || !strings.Contains(h.app.confirm.message, "Cherry-pick") {
		t.Fatal("V did not ask to cherry-pick copied commits")
	}
	if calls.has("batch-cherry-pick:c2c2c2c2") {
		t.Fatal("cherry-pick ran before confirmation")
	}
}

func TestKeyboardDiffSelectionStagesOnlySelectedLines(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "diff", func() bool { return h.app.diffState != nil })
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	h.tester.Key(0, ui.KeyV)
	h.tester.Frame()
	h.tester.Key(ui.Shift, ui.KeyDown)
	h.tester.Frame()
	selected, ok := h.app.diffState.SelectedLines()
	if !ok || selected.End < 1 {
		t.Fatalf("keyboard did not extend line selection: %+v %v", selected, ok)
	}
	h.tester.Key(0, ui.KeySpace)
	h.tester.Frame()
	h.waitFor(t, "partial stage", func() bool { return findCall(calls, "apply-patch:false:") != "" })
	patch := findCall(calls, "apply-patch:false:")
	if !strings.Contains(patch, "+TWO") || strings.Contains(patch, "FOUR") {
		t.Fatalf("wrong selected patch: %s", patch)
	}
}

func TestKeyboardFullscreenPaneTraversalAndReturn(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "diff", func() bool { return h.app.diffState != nil })
	h.app.openDiffFullscreen()
	h.tester.Frame()
	for i := 0; i < 3 && !h.tester.Focused("Diff"); i++ {
		h.tester.Key(0, ui.KeyF6)
		h.tester.Frame()
	}
	if !h.tester.Focused("Diff") || h.app.crash != "" {
		t.Fatalf("fullscreen diff cannot be focused: %s", h.app.crash)
	}
	h.tester.Key(ui.Shift, ui.KeyF6)
	h.tester.Frame()
	if !h.tester.Focused("Changed files") {
		t.Fatal("fullscreen pane traversal escaped into background sidebar")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.diffFullscreen {
		t.Fatal("Escape did not close fullscreen review")
	}
}

func TestKeyboardComposeAndCommitWithoutMouse(t *testing.T) {
	for _, mods := range []ui.Modifiers{0, ui.Ctrl} {
		t.Run(map[bool]string{true: "CtrlEnter", false: "Enter"}[mods != 0], func(t *testing.T) {
			h, calls := menuTestAppWithCalls(t)
			h.tester.Key(0, ui.KeyC)
			h.tester.Frame()
			if !h.tester.Focused("Summary") {
				t.Fatal("commit form did not focus the summary")
			}
			h.tester.Type("keyboard commit")
			h.tester.Frame()
			h.tester.Key(mods, ui.KeyEnter)
			h.tester.Frame()
			h.waitFor(t, "commit", func() bool { return calls.has("commit:keyboard commit") })
		})
	}
}

func TestKeyboardTextInputKeepsDigitsAndQuestionMark(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.focusActiveSearch()
	h.tester.Frame()
	h.tester.TypeKey(0, ui.Key3, "3")
	h.tester.TypeKey(ui.Shift, ui.KeySlash, "?")
	h.tester.TypeKey(0, ui.KeySlash, "/")
	h.tester.Frame()
	if h.app.workspaceTab != sectionChanges || h.app.keybindingsOpen || h.app.fileSearch != "3?/" {
		t.Fatalf("text keys triggered commands: section=%d help=%v text=%q", h.app.workspaceTab, h.app.keybindingsOpen, h.app.fileSearch)
	}
	if nativeHelpAvailable(h.app) {
		t.Fatal("native ? accelerator would intercept text input")
	}
}

func TestKeyboardCommandReferenceMatchesDispatch(t *testing.T) {
	groups := map[string][]keyBinding{}
	for _, g := range keyBindingGroups() {
		groups[g.title] = g.bindings
	}
	seen := map[string]bool{}
	for _, command := range keyboardCommands() {
		id := command.group + ":" + command.keys
		if seen[id] {
			t.Fatalf("duplicate keyboard command: %s", id)
		}
		seen[id] = true
		found := false
		for _, binding := range groups[command.group] {
			if binding.keys == command.keys && binding.label == command.label {
				found = true
			}
		}
		if !found {
			t.Errorf("shortcut reference omits %s", id)
		}
	}
}

func TestKeyboardCanFocusAndActivateRepositoryTabs(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{})
	home := s.activeTab()
	readyShellTab(s, "/work/keyboard-project")
	s.active = 1
	tt.Frame()
	for i := 0; i < 80 && !tt.Focused("Home"); i++ {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
	}
	if !tt.Focused("Home") {
		t.Fatal("Tab did not reach the Home repository tab")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.activeTab() != home {
		t.Fatal("Enter did not activate the focused repository tab")
	}
}

func TestKeyboardTabLeavesLargeGraphForRightPanels(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	commits := make([]repository.Commit, 200)
	for i := range commits {
		commits[i] = repository.Commit{Hash: fmt.Sprintf("%040x", i+1), ShortHash: fmt.Sprintf("%07x", i+1), Subject: fmt.Sprintf("Commit %d", i), AuthorName: "Ada"}
		if i < len(commits)-1 {
			commits[i].Parents = []string{fmt.Sprintf("%040x", i+2)}
		}
	}
	h.app.snapshot.Commits = commits
	h.app.workspaceTab = sectionGraph
	h.app.graphFilter = commitQuery{}
	h.app.selectedCommit = 1
	h.tester.Frame()
	h.waitFor(t, "graph details", func() bool { return h.app.commitDiffState != nil })
	if !h.tester.Focused("Commits") {
		t.Fatal("graph list is not focused")
	}
	h.tester.Key(0, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Commit files") {
		t.Fatal("Tab stayed in graph rows instead of moving to the right-hand files panel")
	}
	if h.app.selectedCommit != 1 {
		t.Fatal("panel traversal changed the selected commit")
	}
	h.tester.Key(0, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Diff") {
		t.Fatal("Tab did not move from files to the diff")
	}
	h.tester.Key(ui.Shift, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Commit files") {
		t.Fatal("Shift+Tab did not return to files")
	}
	h.tester.Key(ui.Shift, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Commits") {
		t.Fatal("Shift+Tab did not return to graph")
	}
}

func TestKeyboardTabLeavesListRowCheckbox(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.selectedCommit = 0
	h.tester.Frame()
	h.waitFor(t, "history details", func() bool { return len(h.app.commitFiles) > 0 })
	if err := h.tester.Click("Select multiple commits"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Select commit for batch actions"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Key(0, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Commit files") {
		t.Fatalf("Tab was trapped among commit-row controls: commit=%v files=%v toolbar=%v pending=%q group=%q", h.tester.Focused("Commits"), h.tester.Focused("Commit files"), h.tester.Focused("Compare refs"), h.app.keyboardFocus, h.app.keyboardCurrentGroup)
	}
}

func TestKeyboardConfirmationEscapeCancelsAndEnterContinues(t *testing.T) {
	for _, focusProceed := range []bool{false, true} {
		t.Run(map[bool]string{false: "CancelFocused", true: "ProceedFocused"}[focusProceed], func(t *testing.T) {
			h, _ := historyHarness(t)
			runs := 0
			h.app.askConfirm(confirmRequest{
				title:        "Review",
				confirmLabel: "Proceed",
				destructive:  true,
				run:          func() { runs++ },
			})
			h.tester.Frame()
			if !h.tester.Focused("Cancel") {
				t.Fatal("confirmation did not start safely focused on Cancel")
			}
			if focusProceed {
				h.tester.Key(0, ui.KeyTab)
				h.tester.Frame()
				if !h.tester.Focused("Proceed") {
					t.Fatal("could not focus Proceed")
				}
			}

			h.tester.Key(0, ui.KeyEscape)
			h.tester.Frame()
			if h.app.confirm != nil || runs != 0 {
				t.Fatalf("Escape must cancel without running: open=%v runs=%d", h.app.confirm != nil, runs)
			}

			h.app.askConfirm(confirmRequest{
				title:        "Review",
				confirmLabel: "Proceed",
				destructive:  true,
				run:          func() { runs++ },
			})
			h.tester.Frame()
			h.tester.Key(0, ui.KeyEnter)
			h.tester.Frame()
			if h.app.confirm != nil || runs != 1 {
				t.Fatalf("Enter must continue exactly once: open=%v runs=%d", h.app.confirm != nil, runs)
			}
		})
	}
}

func TestKeyboardTabStillTraversesToolbarAndConfirmationControls(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.keyboardFocus = "toolbar"
	h.tester.Frame()
	h.tester.Key(0, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Compare refs") {
		t.Fatal("Tab no longer enters toolbar controls")
	}
	h.tester.Key(0, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Fetch") {
		t.Fatal("Tab skipped toolbar controls")
	}
	h.app.askConfirm(confirmRequest{title: "Review", confirmLabel: "Proceed"})
	h.tester.Frame()
	if !h.tester.Focused("Cancel") {
		t.Fatal("confirmation did not focus Cancel")
	}

	h.tester.Key(0, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Proceed") {
		t.Fatalf("Tab escaped the confirmation instead of focusing Proceed: cancel=%v proceed=%v pending=%q modal=%v", h.tester.Focused("Cancel"), h.tester.Focused("Proceed"), h.app.keyboardFocus, h.app.confirm != nil)
	}
	h.tester.Key(ui.Shift, ui.KeyTab)
	h.tester.Frame()
	if !h.tester.Focused("Cancel") {
		t.Fatal("Shift+Tab escaped the confirmation")
	}
}
