package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestEditorShortcutDefaultsIncludePreferredAlternatives(t *testing.T) {
	resolution := resolveEditorShortcutOverrides(nil)
	if resolution.issue != nil {
		t.Fatal(resolution.issue)
	}
	for _, binding := range []editor.Binding{
		{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyUp, Command: editor.MoveLinesUp},
		{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyDown, Command: editor.MoveLinesDown},
		{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyD, Command: editor.DuplicateLinesDown},
	} {
		if countEditorBindings(resolution.native, binding.Command, binding.Mods, binding.Key) != 1 {
			t.Errorf("preferred default %+v is absent or duplicated: %+v", binding, resolution.native)
		}
	}
	moving := resolution.byID[editor.MoveLinesUp.String()]
	if len(moving) == 0 || moving[0].Key != ui.KeyUp || moving[0].Mods != ui.Alt {
		t.Fatalf("move-line default = %v, want Alt+Up", editorShortcutDisplayList(moving))
	}
	outline := resolution.byID[editorShortcutOutlineID]
	if len(outline) != 1 || outline[0].Mods != ui.Ctrl|ui.Shift || outline[0].Key != ui.KeyO {
		t.Fatalf("Outline default = %v, want Ctrl+Shift+O", editorShortcutDisplayList(outline))
	}
	a := &app{}
	if len(a.outlineShortcutBindings()) != 1 {
		t.Fatal("default Outline binding is not available")
	}
	a.settings.DisableShortcuts = true
	if len(a.outlineShortcutBindings()) != 0 {
		t.Fatal("disabled shortcuts still open Outline")
	}
}

func TestEditorShortcutOverridesRunBeforeSuppressedDefaults(t *testing.T) {
	resolution := resolveEditorShortcutOverrides(map[string][]string{
		editor.MoveLinesUp.String():        {"ctrl+shift+up", "f8"},
		editor.DuplicateLinesDown.String(): {"alt+shift+down"},
	})
	if resolution.issue != nil {
		t.Fatal(resolution.issue)
	}
	moving := resolution.byID[editor.MoveLinesUp.String()]
	if got := editorShortcutDisplayList(moving); strings.Join(got, " / ") != "Ctrl+Shift+↑ / F8" {
		t.Fatalf("custom move up bindings = %v, want Ctrl+Shift+Up and F8", got)
	}
	firstNativeBinding := resolution.native[0]
	if firstNativeBinding.Command != editor.MoveLinesUp || firstNativeBinding.Mods != ui.Ctrl|ui.Shift || firstNativeBinding.Key != ui.KeyUp {
		t.Fatalf("first binding = %+v, want customized move up before suppressions", firstNativeBinding)
	}
	if countEditorBindings(resolution.native, editor.NoCommand, ui.Alt, ui.KeyUp) != 1 ||
		countEditorBindings(resolution.native, editor.NoCommand, ui.Alt|ui.Shift, ui.KeyDown) != 1 {
		t.Errorf("old move/duplicate-arrow defaults are not suppressed: %+v", resolution.native)
	}
	for _, binding := range resolution.native {
		if binding.Command == editor.MoveLinesUp && binding.Mods == ui.Alt && binding.Key == ui.KeyUp {
			t.Fatal("the old Alt+Up binding should not execute after remapping")
		}
	}
}

func TestEditorShortcutOverridesCanClearAndRestoreOneAction(t *testing.T) {
	id := editor.MoveLinesUp.String()
	unassigned := resolveEditorShortcutOverrides(map[string][]string{id: {}})
	if unassigned.issue != nil || len(unassigned.byID[id]) != 0 {
		t.Fatalf("explicitly cleared binding = %+v, want no assignment", unassigned)
	}
	if countEditorBindings(unassigned.native, editor.NoCommand, ui.Alt, ui.KeyUp) != 1 ||
		countEditorBindings(unassigned.native, editor.MoveLinesUp, ui.Ctrl|ui.Shift, ui.KeyUp) != 0 {
		t.Fatalf("old Move up bindings were not suppressed: %+v", unassigned.native)
	}
	if bindings, ok := unassigned.byID[editor.MoveLinesDown.String()]; !ok || len(bindings) == 0 {
		t.Fatal("clearing one action changed another action's defaults")
	}

	unchanged := resolveEditorShortcutOverrides(map[string][]string{id: {"alt+up"}})
	if unchanged.issue != nil {
		t.Fatal(unchanged.issue)
	}
	if unchanged.native[0].Command != editor.MoveLinesUp ||
		countEditorBindings(unchanged.native, editor.NoCommand, ui.Alt, ui.KeyUp) != 1 ||
		countEditorBindings(unchanged.native, editor.NoCommand, ui.Ctrl|ui.Shift, ui.KeyUp) != 1 {
		t.Fatalf("explicitly saving an unchanged chord stopped working: %+v", unchanged.native)
	}
}

func TestEditorShortcutValidationRejectsConflictsAndDuplicates(t *testing.T) {
	prefix := ""
	for _, binding := range editor.DefaultBindings() {
		if binding.ThenKey != 0 {
			prefix = editorShortcutChord(binding)
			break
		}
	}
	tests := []struct {
		name    string
		value   map[string][]string
		message string
	}{
		{name: "other editable command", value: map[string][]string{editor.MoveLinesUp.String(): {"alt+down"}}, message: "Move line down"},
		{name: "fixed native command", value: map[string][]string{editor.DuplicateLinesDown.String(): {"ctrl+l"}}, message: "already assigned"},
		{name: "chord prefix", value: map[string][]string{editor.DuplicateLinesDown.String(): {prefix}}, message: "chord"},
		{name: "plain typing key", value: map[string][]string{editor.MoveLinesUp.String(): {"d"}}, message: "modifier"},
		{name: "repeated alternative", value: map[string][]string{editor.MoveLinesUp.String(): {"ctrl+shift+up", "ctrl+shift+up"}}, message: "assigned twice"},
		{name: "more than two alternatives", value: map[string][]string{editor.MoveLinesUp.String(): {"f6", "f7", "f8"}}, message: "two shortcuts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolution := resolveEditorShortcutOverrides(test.value)
			if resolution.issue == nil || !strings.Contains(resolution.issue.Error(), test.message) {
				t.Fatalf("issue = %v, want %q", resolution.issue, test.message)
			}
			if len(resolution.native) != 0 {
				t.Fatalf("an invalid draft would replace native bindings: %+v", resolution.native)
			}
			if len(resolution.byID[editorShortcutOutlineID]) == 0 {
				t.Fatal("an invalid draft did not fall back to defaults")
			}
		})
	}
}

func TestEditorShortcutOverridesAllowSwappingDefaults(t *testing.T) {
	resolution := resolveEditorShortcutOverrides(map[string][]string{
		editor.MoveLinesUp.String():   {"alt+down"},
		editor.MoveLinesDown.String(): {"alt+up"},
	})
	if resolution.issue != nil {
		t.Fatal(resolution.issue)
	}
	if resolution.byID[editor.MoveLinesUp.String()][0].Key != ui.KeyDown || resolution.byID[editor.MoveLinesDown.String()][0].Key != ui.KeyUp {
		t.Fatal("replacement bindings could not exchange two default chords")
	}
}

func TestEditorShortcutChordParsingFormattingAndPersistence(t *testing.T) {
	chord := editor.Binding{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyD}
	serialized := editorShortcutChord(chord)
	if serialized != "ctrl+shift+d" {
		t.Fatalf("serialized shortcut = %q", serialized)
	}
	parsed, err := parseEditorShortcut("CTRL + Shift + D")
	if err != nil || parsed.Mods != chord.Mods || parsed.Key != chord.Key {
		t.Fatalf("parsed shortcut = %+v, %v", parsed, err)
	}
	if label := editorShortcutDisplay(chord); label != "Ctrl+Shift+D" {
		t.Fatalf("display shortcut = %q", label)
	}
	if !slices.Equal(editorShortcutDisplayList([]editor.Binding{chord}), []string{"Ctrl+Shift+D"}) {
		t.Fatal("shortcut display is not suitable for the settings table")
	}

	settings := defaultSettings()
	settings.EditorKeyBindings = map[string][]string{
		editor.MoveLinesUp.String():        {"ctrl+shift+up"},
		editor.DuplicateLinesDown.String(): {},
	}
	directory := t.TempDir()
	if err := saveAppSettings(directory, settings); err != nil {
		t.Fatal(err)
	}
	loaded := loadAppSettings(directory)
	up, present := loaded.EditorKeyBindings[editor.MoveLinesUp.String()]
	if !present || len(up) != 1 || up[0] != "ctrl+shift+up" {
		t.Fatalf("persisted binding = %v, present=%v", up, present)
	}
	duplicated, present := loaded.EditorKeyBindings[editor.DuplicateLinesDown.String()]
	if !present || len(duplicated) != 0 {
		t.Fatalf("explicit unassignment was not preserved: %v, present=%v", duplicated, present)
	}
	cloned := cloneEditorShortcutOverrides(settings.EditorKeyBindings)
	cloned[editor.MoveLinesUp.String()][0] = "f9"
	if settings.EditorKeyBindings[editor.MoveLinesUp.String()][0] != "ctrl+shift+up" {
		t.Fatal("editing a draft mutated the stored settings")
	}
}

func countEditorBindings(bindings []editor.Binding, command editor.Command, mods ui.Modifiers, key ui.Key) int {
	count := 0
	for _, binding := range bindings {
		if binding.Command == command && binding.Mods == mods && binding.Key == key {
			count++
		}
	}
	return count
}

func TestCustomEditorShortcutsWorkThroughNativeEditorInput(t *testing.T) {
	a := &app{}
	a.settings.EditorKeyBindings = map[string][]string{
		editor.MoveLinesDown.String():      {"ctrl+shift+down"},
		editor.DuplicateLinesDown.String(): {"ctrl+shift+d"},
		editor.DuplicateLinesUp.String():   {"ctrl+shift+up"},
	}
	state := &editor.State{Buffer: editor.NewBuffer("one\ntwo\nthree\n")}
	a.applyCodeEditorPreferences(state)
	tester := ui.NewTester(func(c *ui.Context) {
		editor.View(c, state).Fill().AutoFocus()
	}, 600, 400)
	tester.Frame()
	state.Select(0, 0)
	tester.Frame()
	tester.Key(ui.Ctrl|ui.Shift, ui.KeyDown)
	tester.Frame()
	if got := state.Buffer.String(); got != "two\none\nthree\n" {
		t.Fatalf("Ctrl+Shift+Down buffer = %q, want the active line moved down", got)
	}
	tester.Key(ui.Ctrl|ui.Shift, ui.KeyD)
	tester.Frame()
	// Move preserves the active line's position: the caret is now on one.
	if got := state.Buffer.String(); got != "two\none\none\nthree\n" {
		t.Fatalf("Ctrl+Shift+D buffer = %q, want the active line duplicated", got)
	}
	if state.Run(editor.Undo) != true || state.Buffer.String() != "two\none\nthree\n" {
		t.Fatalf("duplicating a line was not one undoable operation: %q", state.Buffer.String())
	}
	beforeOldDefault := state.Buffer.String()
	tester.Key(ui.Alt, ui.KeyDown)
	if state.Buffer.String() != beforeOldDefault {
		t.Fatalf("cleared Alt+Down still moved the line to %q", state.Buffer.String())
	}
	version := state.Buffer.Version()
	selection := state.Selections()
	pointer := state.Buffer
	a.applyCodeEditorPreferences(state)
	if state.Buffer != pointer || state.Buffer.Version() != version || !slices.Equal(state.Selections(), selection) {
		t.Fatal("applying custom shortcuts changed the document or selection")
	}
}

func TestCustomEditorShortcutsLeaveUnmodifiedNativeCommandsAlone(t *testing.T) {
	a := &app{}
	a.settings.EditorKeyBindings = map[string][]string{editor.MoveLinesUp.String(): {"ctrl+shift+up"}}
	buffer := editor.NewBuffer("var x = 1\nvar y = 2\n")
	buffer.SetLanguage(editor.LanguageFor("main.go"))
	state := &editor.State{Buffer: buffer}
	a.applyCodeEditorPreferences(state)
	tester := ui.NewTester(func(c *ui.Context) {
		editor.View(c, state).Fill().AutoFocus()
	}, 600, 400)
	tester.Frame()
	state.Select(0, 0)
	tester.Key(ui.Cmd, ui.KeySlash)
	if got := state.Buffer.String(); !strings.HasPrefix(got, "//") || !strings.Contains(got, "var y = 2") {
		t.Fatalf("custom Move up broke the native Toggle comment binding: %q", got)
	}
}

func TestPreferredEditorBindingsWorkWithoutConfiguration(t *testing.T) {
	for _, test := range []struct {
		id     string
		key    ui.Key
		start  int
		expect string
	}{
		{id: editor.MoveLinesDown.String(), key: ui.KeyDown, start: 0, expect: "two\none\nthree\n"},
		{id: editor.MoveLinesUp.String(), key: ui.KeyUp, start: len("one\n"), expect: "two\none\nthree\n"},
		{id: editor.DuplicateLinesDown.String(), key: ui.KeyD, start: 0, expect: "one\none\ntwo\nthree\n"},
	} {
		t.Run(test.id, func(t *testing.T) {
			action, ok := editorShortcutActionForID(test.id)
			if !ok {
				t.Fatalf("missing action %s", test.id)
			}
			if len(editorShortcutPreferredDefault(action, nil)) == 0 {
				t.Skip("preferred chord belongs to an existing native editor command on this platform")
			}
			a := &app{}
			state := &editor.State{Buffer: editor.NewBuffer("one\ntwo\nthree\n")}
			a.applyCodeEditorPreferences(state)
			tester := ui.NewTester(func(c *ui.Context) {
				editor.View(c, state).Fill().AutoFocus()
			}, 600, 400)
			tester.Frame()
			state.Select(test.start, test.start)
			tester.Frame()
			tester.Key(ui.Ctrl|ui.Shift, test.key)
			if got := state.Buffer.String(); got != test.expect {
				t.Fatalf("default Ctrl+Shift binding produced %q, want %q", got, test.expect)
			}
		})
	}
}

func TestOutlineShortcutUsesCurrentEditorPaneAndOverrides(t *testing.T) {
	h, _ := languageSymbolsHarness(t)
	h.app.closeCodeLanguageDialog()
	h.app.workspaceTab = sectionCode
	h.app.code.files = []string{"main.go"}
	h.app.code.visiblePaths = []string{"main.go"}
	h.app.code.filesLoaded = true
	h.app.settings.EditorKeyBindings = map[string][]string{editorShortcutOutlineID: {"ctrl+shift+d"}}
	h.tester.Frame()
	if err := h.tester.Click("Code editor"); err != nil {
		t.Fatalf("focus code editor: %v", err)
	}
	h.tester.Key(ui.Ctrl|ui.Shift, ui.KeyD)
	h.tester.Frame()
	if h.app.code.languageDialog != codeLanguageDialogSymbols {
		t.Fatal("custom Ctrl+Shift+D did not open Outline in the editor pane")
	}
	h.app.closeCodeLanguageDialog()
	h.app.settings.EditorKeyBindings = map[string][]string{editorShortcutOutlineID: {}}
	h.tester.Frame()
	h.tester.Key(ui.Ctrl|ui.Shift, ui.KeyD)
	h.tester.Frame()
	if h.app.code.languageDialog != codeLanguageDialogNone {
		t.Fatal("cleared Outline shortcut remained active")
	}
	h.app.settings.EditorKeyBindings = nil
	h.tester.Frame()
	h.tester.Key(ui.Ctrl|ui.Shift, ui.KeyO)
	h.tester.Frame()
	if h.app.code.languageDialog != codeLanguageDialogSymbols {
		t.Fatal("restored Outline shortcut did not work")
	}
	h.app.closeCodeLanguageDialog()
	h.app.settings.DisableShortcuts = true
	h.tester.Frame()
	h.tester.Key(ui.Ctrl|ui.Shift, ui.KeyO)
	h.tester.Frame()
	if h.app.code.languageDialog != codeLanguageDialogNone {
		t.Fatal("Outline shortcut ignored Disable shortcuts")
	}
}

func TestCodeEditorShortcutHelpShowsEffectiveBindings(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionCode
	h.app.settings.EditorKeyBindings = map[string][]string{editor.DuplicateLinesDown.String(): {"ctrl+shift+d"}}
	h.tester.Frame()
	h.app.openCurrentKeybindings()
	h.tester.Frame()
	for _, text := range []string{"Code editor", "Duplicate line down", "Ctrl+Shift+D", "Open Outline"} {
		if !h.tester.HasText(text) {
			t.Errorf("editor shortcut help is missing %q", text)
		}
	}
}
