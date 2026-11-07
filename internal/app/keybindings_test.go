package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The header's keyboard icon opens a reference of every shortcut, grouped by
// where it acts, and its filter narrows the list to matching actions.
func TestKeybindingsModalListsAndFiltersShortcuts(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settings = defaultSettings()
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	if err := tt.Click("Keyboard shortcuts"); err != nil {
		t.Fatalf("the picker offers no shortcut list: %v", err)
	}
	tt.Frame()
	if !a.keybindingsOpen {
		t.Fatal("the shortcut list did not open")
	}
	texts := strings.Join(tt.Texts(), " ")
	for _, want := range []string{
		"Keyboard shortcuts",
		"Everywhere",
		"Commit the staged changes",
		"Check out the chosen branch",
		"Ctrl+Enter",
		"Disable all keyboard shortcuts",
		"Done",
	} {
		if !strings.Contains(texts, want) {
			t.Errorf("the shortcut list does not show %q: %v", want, tt.Texts())
		}
	}

	if err := tt.Click("Filter shortcuts"); err != nil {
		t.Fatalf("focus the filter: %v", err)
	}
	tt.Type("branch")
	tt.Frame()
	texts = strings.Join(tt.Texts(), " ")
	if !strings.Contains(texts, "Check out the chosen branch") {
		t.Fatalf("the filter dropped the branch shortcuts: %v", tt.Texts())
	}
	if strings.Contains(texts, "Commit the staged changes") {
		t.Fatalf("the filter kept a shortcut that does not match: %v", tt.Texts())
	}

	// Done closes the list and clears the filter for the next visit.
	if err := tt.Click("Done"); err != nil {
		t.Fatalf("close the shortcut list: %v", err)
	}
	tt.Frame()
	if a.keybindingsOpen {
		t.Fatal("Done did not close the shortcut list")
	}
	if a.keybindingFilter != "" {
		t.Fatalf("the filter survived closing: %q", a.keybindingFilter)
	}
}

// Question mark opens the shortcuts that apply to the active page, while F1
// remains the complete reference for every page.
func TestQuestionMarkOpensCurrentPageShortcuts(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionBranches
	h.tester.Frame()
	h.tester.Key(ui.Shift, ui.KeySlash)
	h.tester.Frame()

	texts := strings.Join(h.tester.Texts(), " ")
	for _, want := range []string{"Everywhere", "Workspace", "Branches", "Check out the chosen branch"} {
		if !strings.Contains(texts, want) {
			t.Errorf("page shortcuts do not show %q: %v", want, h.tester.Texts())
		}
	}
	for _, unwanted := range []string{"Commit the staged changes", "Reword the chosen commit", "Drop the chosen stash"} {
		if strings.Contains(texts, unwanted) {
			t.Errorf("page shortcuts include an unrelated action %q: %v", unwanted, h.tester.Texts())
		}
	}
}

// Re-enabling shortcuts inside the reference immediately restores ? and F1;
// closing and reopening the dialog must not be part of enabling the keyboard.
func TestQuestionMarkWorksAfterReenablingShortcutsInReference(t *testing.T) {
	h := menuTestApp(t)
	h.tester.Key(0, ui.KeyF1)
	h.tester.Frame()
	for i := 0; i < 2; i++ {
		if err := h.tester.Click("Disable all keyboard shortcuts"); err != nil {
			t.Fatalf("toggle the shortcut switch: %v", err)
		}
		h.tester.Frame()
	}
	if h.app.settings.DisableShortcuts {
		t.Fatal("the second toggle did not re-enable shortcuts")
	}

	h.tester.Key(ui.Shift, ui.KeySlash)
	h.tester.Frame()
	if !h.app.keybindingsOpen || !h.app.keybindingsCurrentOnly {
		t.Fatalf("? did not switch the open reference to current-page shortcuts after re-enabling; filter=%q texts=%q", h.app.keybindingFilter, h.tester.Texts())
	}
	h.tester.Key(0, ui.KeyF1)
	h.tester.Frame()
	if !h.app.keybindingsOpen || h.app.keybindingsCurrentOnly {
		t.Fatal("F1 did not restore the complete reference")
	}
}

// The switch in the shortcut list turns the keyboard off: section digits, F5
// and the list's own F1 stop firing, while the header icons still work.
func TestDisablingAllShortcutsStopsTheKeyboard(t *testing.T) {
	h := menuTestApp(t)
	h.tester.Key(0, ui.Key3)
	h.tester.Frame()
	if h.app.workspaceTab != sectionGraph {
		t.Fatalf("Key3 did not switch to the graph: tab=%v", h.app.workspaceTab)
	}

	if err := h.tester.Click("Keyboard shortcuts"); err != nil {
		t.Fatalf("open the shortcut list: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Disable all keyboard shortcuts"); err != nil {
		t.Fatalf("click the disable switch: %v", err)
	}
	h.tester.Frame()
	if !h.app.settings.DisableShortcuts {
		t.Fatal("the switch did not reach the settings")
	}
	if err := h.tester.Click("Done"); err != nil {
		t.Fatalf("close the shortcut list: %v", err)
	}
	h.tester.Frame()

	h.tester.Key(0, ui.Key1)
	h.tester.Frame()
	if h.app.workspaceTab != sectionGraph {
		t.Fatalf("a section digit still fired with shortcuts off: tab=%v", h.app.workspaceTab)
	}
	h.tester.Key(0, ui.KeyF1)
	h.tester.Frame()
	if h.app.keybindingsOpen {
		t.Fatal("F1 still opened the shortcut list with shortcuts off")
	}
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if h.app.settingsOpen {
		t.Fatal("Ctrl+, still opened the settings with shortcuts off")
	}

	if err := h.tester.Click("Keyboard shortcuts"); err != nil {
		t.Fatalf("the status bar must keep the list reachable: %v", err)
	}
}
