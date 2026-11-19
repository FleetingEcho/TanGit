package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func editorShortcutSettingsHarness(t *testing.T, filter string) *harness {
	t.Helper()
	h := menuTestApp(t)
	h.app.settings.EditorKeyBindings = nil
	h.app.code.shortcutQuery = filter
	h.app.openSettingsSection(settingsPageCodeEditor)
	h.tester.Frame()
	return h
}

func recordEditorShortcutInSettings(t *testing.T, h *harness, id string, slot int, mods ui.Modifiers, key ui.Key) {
	t.Helper()
	action, ok := editorShortcutActionForID(id)
	if !ok {
		t.Fatalf("no editor action %q", id)
	}
	if err := h.tester.Click(editorShortcutRecordLabel(action, slot)); err != nil {
		t.Fatalf("record %s slot %d: %v", action.label, slot, err)
	}
	h.tester.Frame()
	if h.app.code.shortcutRecording != id || h.app.code.shortcutSlot != slot {
		t.Fatalf("recording = %q slot %d, want %s slot %d", h.app.code.shortcutRecording, h.app.code.shortcutSlot, id, slot)
	}
	if !h.tester.Focused(editorShortcutRecordLabel(action, slot)) {
		t.Fatal("shortcut recorder does not have keyboard focus")
	}
	h.tester.Key(mods, key)
	h.tester.Frame()
}

func TestEditorShortcutSettingsRecordsAlternativesAndClears(t *testing.T) {
	h := editorShortcutSettingsHarness(t, "Move line up")
	recordEditorShortcutInSettings(t, h, editor.MoveLinesUp.String(), 0, ui.Ctrl|ui.Shift, ui.KeyUp)
	up := h.app.settings.EditorKeyBindings[editor.MoveLinesUp.String()]
	if len(up) != 2 || up[0] != "ctrl+shift+up" || up[1] != "alt+up" {
		t.Fatalf("recorded primary and alternative = %v, want Ctrl+Shift+Up and Alt+Up", up)
	}
	if h.app.code.shortcutRecording != "" || !h.tester.HasText("Ctrl+Shift+↑") {
		t.Fatal("accepted primary binding did not return to its compact display")
	}

	h.app.code.shortcutQuery = "Duplicate line down"
	h.tester.Frame()
	recordEditorShortcutInSettings(t, h, editor.DuplicateLinesDown.String(), 1, 0, ui.KeyF9)
	recordEditorShortcutInSettings(t, h, editor.DuplicateLinesDown.String(), 0, ui.Ctrl|ui.Shift, ui.KeyD)
	duplicated := h.app.settings.EditorKeyBindings[editor.DuplicateLinesDown.String()]
	if len(duplicated) != 2 || duplicated[0] != "ctrl+shift+d" || duplicated[1] != "f9" {
		t.Fatalf("primary and secondary = %v, want Ctrl+Shift+D and F9", duplicated)
	}
	recordEditorShortcutInSettings(t, h, editor.DuplicateLinesDown.String(), 0, 0, ui.KeyBackspace)
	duplicated = h.app.settings.EditorKeyBindings[editor.DuplicateLinesDown.String()]
	if len(duplicated) != 1 || duplicated[0] != "f9" {
		t.Fatalf("clearing primary did not retain the secondary binding: %v", duplicated)
	}
	recordEditorShortcutInSettings(t, h, editor.DuplicateLinesDown.String(), 0, 0, ui.KeyBackspace)
	if duplicated, present := h.app.settings.EditorKeyBindings[editor.DuplicateLinesDown.String()]; !present || len(duplicated) != 0 {
		t.Fatalf("clearing all bindings = %v, present=%v, want explicitly unassigned", duplicated, present)
	}

	if err := h.tester.Click("Reset Duplicate line down shortcuts"); err != nil {
		t.Fatalf("restore action default: %v", err)
	}
	h.tester.Frame()
	if _, overridden := h.app.settings.EditorKeyBindings[editor.DuplicateLinesDown.String()]; overridden {
		t.Fatal("per-action reset left a customized override behind")
	}
	if !h.tester.HasText("Alt+Shift+↓") {
		t.Fatal("per-action reset did not show the default duplicate-down shortcut")
	}
}

func TestEditorShortcutSettingsConflictDoesNotSaveAndCanRetry(t *testing.T) {
	h := editorShortcutSettingsHarness(t, "Duplicate line down")
	recordEditorShortcutInSettings(t, h, editor.DuplicateLinesDown.String(), 0, ui.Alt, ui.KeyDown)
	if len(h.app.settings.EditorKeyBindings) != 0 {
		t.Fatalf("conflicting binding was saved: %v", h.app.settings.EditorKeyBindings)
	}
	if !h.tester.HasText("already assigned") || h.app.code.shortcutRecording == "" {
		t.Fatal("conflict was not reported while keeping the recorder ready for a retry")
	}
	recordEditorShortcutInSettings(t, h, editor.DuplicateLinesDown.String(), 0, ui.Ctrl|ui.Shift, ui.KeyD)
	got := h.app.settings.EditorKeyBindings[editor.DuplicateLinesDown.String()]
	if len(got) != 2 || got[0] != "ctrl+shift+d" || got[1] != "alt+shift+down" {
		t.Fatalf("retry after conflict = %v, want Ctrl+Shift+D and Alt+Shift+Down", got)
	}
	if h.app.code.shortcutError != "" {
		t.Fatalf("successful retry left stale conflict: %q", h.app.code.shortcutError)
	}
}

func TestEditorShortcutSettingsCancelAndFilter(t *testing.T) {
	h := editorShortcutSettingsHarness(t, "Open Outline")
	action, _ := editorShortcutActionForID(editorShortcutOutlineID)
	if err := h.tester.Click(editorShortcutRecordLabel(action, 0)); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Key(0, ui.KeyF1)
	h.tester.Frame()
	bindings := h.app.settings.EditorKeyBindings[editorShortcutOutlineID]
	if h.app.keybindingsOpen || len(bindings) != 1 || bindings[0] != "f1" {
		t.Fatal("unmodified function key opened global help instead of being captured")
	}
	if err := h.tester.Click(editorShortcutRecordLabel(action, 0)); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if !h.app.settingsOpen || h.app.code.shortcutRecording != "" {
		t.Fatal("Escape should cancel recording without closing Settings")
	}

	h.app.code.shortcutQuery = "Duplicate line down"
	h.tester.Frame()
	recordEditorShortcutInSettings(t, h, editor.DuplicateLinesDown.String(), 0, ui.Shift, ui.KeyD)
	if len(h.app.settings.EditorKeyBindings) != 1 || !strings.Contains(h.app.code.shortcutError, "modifier") {
		t.Fatal("Shift+letter should be rejected without changing the previously recorded shortcut")
	}
	h.app.stopEditorShortcutRecording()
	h.app.code.shortcutQuery = ""
	h.tester.Frame()

	if err := h.tester.Click("Filter editor shortcuts"); err != nil {
		t.Fatalf("focus shortcut filter: %v", err)
	}
	h.tester.Type("outline")
	h.tester.Frame()
	if h.app.code.shortcutQuery != "outline" || !h.tester.HasText("Open Outline") || h.tester.HasText("Move line up") {
		t.Fatal("settings action filter did not narrow its rows")
	}
	if err := h.tester.Click(editorShortcutRecordLabel(action, 0)); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Filter editor shortcuts"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type(" no match")
	h.tester.Frame()
	if h.app.code.shortcutRecording != "" || !h.tester.HasText("No editor shortcuts match this filter") {
		t.Fatal("changing the filter did not stop recording and show its empty state")
	}
	h.app.openSettingsSection(settingsPageBehavior)
	h.tester.Frame()
	if h.app.code.shortcutRecording != "" {
		t.Fatal("changing Settings category left a recorder active")
	}
	h.app.closeSettings()
	if h.app.code.shortcutRecording != "" {
		t.Fatal("closing Settings left capture state behind")
	}
}

func TestEditorShortcutSettingsRestoreDefaults(t *testing.T) {
	h := editorShortcutSettingsHarness(t, "")
	h.app.settings.EditorKeyBindings = map[string][]string{
		editor.MoveLinesUp.String():        {"ctrl+shift+up"},
		editor.DuplicateLinesDown.String(): {"ctrl+shift+d"},
	}
	h.tester.Frame()
	if err := h.tester.Click("Restore default editor shortcuts"); err != nil {
		t.Fatalf("restore defaults: %v", err)
	}
	h.tester.Frame()
	if len(h.app.settings.EditorKeyBindings) != 0 {
		t.Fatalf("Restore defaults left custom bindings: %v", h.app.settings.EditorKeyBindings)
	}
}
