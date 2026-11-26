package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestThemeJSONValidatesPreviewsSavesAndDeletes(t *testing.T) {
	a := newApp(nil)
	a.pathUserData = t.TempDir()
	original := a.settings.Theme
	a.openThemeEditor()
	tt := ui.NewTester(a.view, 1260, 840)
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	click("JSON")
	draft := &a.settingsUI.themeEditorJSON
	if seed, problem := a.validateThemeJSON(draft.text); problem != "" || len(seed.Colors) != themeColorCount {
		t.Fatalf("default JSON is incomplete or invalid: %s, %s", draft.text, problem)
	}
	paste := func(text string) {
		t.Helper()
		click("Theme JSON")
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type(text)
		tt.Frame()
	}
	paste(`{"name":"Terminal Glow","base":"dark","colors":{"accent":"blue"}}`)
	click("Apply")
	if !tt.HasText(`Color "accent" must use #RRGGBB.`) || draft.preview != nil || len(a.settings.CustomThemes) != 0 {
		t.Fatal("invalid color was accepted or its validation message is missing")
	}
	paste(`{"name":"Terminal Glow","base":"dark","colors":{"accent":"#62A1DC"}}`)
	click("Apply")
	if draft.preview == nil || draft.preview.Colors["accent"] != "#62a1dc" ||
		a.settings.Theme != original || len(a.settings.CustomThemes) != 0 {
		t.Fatal("Apply did not preview the valid JSON independently of saved settings")
	}
	click("Save theme")
	saved := loadAppSettings(a.pathUserData)
	if saved.Theme != customThemeValue("Terminal Glow") ||
		len(saved.CustomThemes) != 1 || saved.CustomThemes[0].Colors["accent"] != "#62a1dc" {
		t.Fatalf("the JSON theme was not saved and activated: theme=%q, themes=%+v", saved.Theme, saved.CustomThemes)
	}
	click("Delete this custom theme")
	if a.confirm == nil {
		t.Fatal("deleting a saved theme did not ask for confirmation")
	}
	click("Delete theme")
	saved = loadAppSettings(a.pathUserData)
	if len(saved.CustomThemes) != 0 || saved.Theme != original {
		t.Fatalf("deleting the theme left theme=%q, themes=%+v", saved.Theme, saved.CustomThemes)
	}
}

func TestThemeJSONCanSelectAndUpdateAnExistingCustomTheme(t *testing.T) {
	a := newApp(nil)
	a.pathUserData = t.TempDir()
	a.settings.CustomThemes = []namedCustomTheme{
		{Name: "Dusk", customThemeSetting: customThemeSetting{Base: "dark", Colors: map[string]string{"accent": "#112233"}}},
		{Name: "Dawn", customThemeSetting: customThemeSetting{Base: "light", Colors: map[string]string{"accent": "#445566"}}},
	}
	// The editor opens on the active custom theme; the picker switches between
	// saved themes without changing which one the window uses.
	a.settings.Theme = customThemeValue("Dusk")
	active := a.settings.Theme
	a.openThemeEditor()
	tt := ui.NewTester(a.view, 1260, 840)
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	click("JSON")
	click("Custom theme")
	click("Dawn")
	if draft, problem := a.validateThemeJSON(a.settingsUI.themeEditorJSON.text); problem != "" ||
		draft.Name != "Dawn" || draft.Base != "light" || draft.Colors["accent"] != "#445566" ||
		a.settings.Theme != active || a.settingsUI.themeEditorIndex != 1 {
		t.Fatalf("selecting a saved theme did not load its JSON for editing: %+v, %s", draft, problem)
	}
	click("Theme JSON")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(`{"name":"Dawn","base":"light","colors":{"accent":"#123456"}}`)
	tt.Frame()
	click("Save theme")
	saved := loadAppSettings(a.pathUserData)
	if len(saved.CustomThemes) != 2 || saved.Theme != customThemeValue("Dawn") ||
		saved.CustomThemes[0].Colors["accent"] != "#112233" || saved.CustomThemes[1].Colors["accent"] != "#123456" {
		t.Fatalf("saving JSON did not update the selected theme in place: %+v", saved.CustomThemes)
	}
	click("New theme")
	if a.settingsUI.themeEditorIndex != -1 || len(a.settings.CustomThemes) != 2 {
		t.Fatal("New theme did not start a separate draft")
	}
	click("Theme name")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Morning")
	tt.Frame()
	click("Create theme")
	saved = loadAppSettings(a.pathUserData)
	if len(saved.CustomThemes) != 3 || saved.CustomThemes[2].Name != "Morning" ||
		saved.Theme != customThemeValue("Morning") {
		t.Fatalf("Create theme did not save and activate the new draft: %+v", saved.CustomThemes)
	}
	click("Custom theme")
	click("Dusk")
	if draft, problem := a.validateThemeJSON(a.settingsUI.themeEditorJSON.text); problem != "" ||
		draft.Name != "Dusk" || draft.Colors["accent"] != "#112233" || a.settingsUI.themeEditorIndex != 0 {
		t.Fatalf("saved theme selector did not load the theme: %+v, %s", draft, problem)
	}
}

func TestThemePickerRenameFocusesNameAndUpdatesExistingTheme(t *testing.T) {
	a := newApp(nil)
	a.pathUserData = t.TempDir()
	a.settings.CustomThemes = []namedCustomTheme{
		{Name: "Dusk", customThemeSetting: customThemeSetting{Base: "dark", Colors: map[string]string{"accent": "#112233"}}},
	}
	a.openThemeEditor()
	tt := ui.NewTester(a.view, 1260, 840)
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if a.settingsUI.themeEditorIndex != -1 {
		t.Fatal("opening the editor did not start a new draft from a built-in theme")
	}
	if !tt.HasText("Save or reset the current new theme before adding another.") {
		t.Fatal("the new-theme draft does not explain how to start another")
	}
	// Renaming applies to a saved theme, which the picker switches to.
	a.editCustomTheme(0)
	tt.Frame()
	click("Custom theme")
	click("Dusk")
	click("Rename theme")
	if !a.settingsUI.themeEditorRenaming || !tt.Focused("Theme name") {
		t.Fatal("Rename did not focus the existing theme's Name field")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Evening")
	tt.Key(0, ui.KeyEnter)
	click("Done")
	saved := loadAppSettings(a.pathUserData)
	if len(saved.CustomThemes) != 1 || saved.CustomThemes[0].Name != "Evening" ||
		saved.CustomThemes[0].Colors["accent"] != "#112233" {
		t.Fatalf("rename did not keep the saved palette in place: %+v", saved.CustomThemes)
	}
}

func TestThemeJSONRejectsUnsupportedFieldsAndNames(t *testing.T) {
	a := newApp(nil)
	a.openThemeEditor()
	for _, test := range []struct{ text, problem string }{
		{`{`, "Enter valid theme JSON"},
		{`{"name":"Sample","base":"dark","colors":{"accent":"#123456"},"typo":true}`, "Enter valid theme JSON"},
		{`{"name":"Sample","base":"dark","colors":{"accent":"#123456"}} {}`, "Enter valid theme JSON"},
		{`{"name":"Sample","base":"blue","colors":{"accent":"#123456"}}`, "Base must be"},
		{`{"name":"Sample","base":"dark","colors":{"accented":"#123456"}}`, "Unknown theme color"},
		{`{"name":"Sample","base":"dark","colors":{}}`, "Provide at least one"},
		{`{"name":"System","base":"dark","colors":{"accent":"#123456"}}`, "built-in theme"},
	} {
		if _, problem := a.validateThemeJSON(test.text); !strings.Contains(problem, test.problem) {
			t.Errorf("validateThemeJSON(%s) = %q, want %q", test.text, problem, test.problem)
		}
	}
}
