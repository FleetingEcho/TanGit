package app

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The settings dialog carries the switch that turns every shortcut off, and
// with it on a key pressed by mistake does nothing: no discard question, no
// staging, no section change.
func TestSettingsDisableShortcutsSwitchesEveryKeyOff(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Behavior settings"); err != nil {
		t.Fatalf("open Behavior settings: %v", err)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Disable all keyboard shortcuts") {
		t.Fatalf("the settings do not offer the switch: %q", h.texts())
	}
	if err := h.tester.Click("Disable all keyboard shortcuts"); err != nil {
		t.Fatalf("click the disable switch: %v", err)
	}
	h.tester.Frame()
	if !h.app.settings.DisableShortcuts {
		t.Fatal("the switch did not reach the settings")
	}
	if err := h.tester.Click("Done"); err != nil {
		t.Fatalf("close the settings: %v", err)
	}
	h.tester.Frame()
	h.settle(t)

	before := len(calls.all())
	h.tester.Key(0, ui.KeyD)
	h.tester.Frame()
	if h.app.confirm != nil {
		t.Fatal("D still opened the discard question")
	}
	h.tester.Key(0, ui.KeySpace)
	h.tester.Frame()
	h.settle(t)
	if got := len(calls.all()); got != before {
		t.Fatalf("keys still reached the session with shortcuts off: %q", calls.all())
	}
	if h.app.workspaceTab != sectionChanges {
		t.Fatalf("a key switched the section with shortcuts off: tab=%v", h.app.workspaceTab)
	}
}

// Re-enabling shortcuts in Settings makes ? useful immediately: it leaves the
// preferences dialog and shows help for the workspace page underneath it.
func TestQuestionMarkWorksAfterReenablingShortcutsInSettings(t *testing.T) {
	h := menuTestApp(t)
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Behavior settings"); err != nil {
		t.Fatalf("open Behavior settings: %v", err)
	}
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
	if h.app.settingsOpen || !h.app.keybindingsOpen || !h.app.keybindingsCurrentOnly {
		t.Fatalf("? did not leave Settings and open current-page help; settings=%v help=%v current=%v",
			h.app.settingsOpen, h.app.keybindingsOpen, h.app.keybindingsCurrentOnly)
	}
}

func TestSettingsStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := appSettings{
		Theme:                  "Dracula",
		Accent:                 "Teal",
		FontSize:               16,
		Font:                   "Menlo",
		Spacing:                5,
		Radius:                 3,
		DiffSplit:              true,
		DiffWrap:               true,
		IgnoreUntracked:        true,
		DisableShortcuts:       true,
		Language:               "zh-Hans",
		EditorCommand:          "zed {{filename}}",
		HomeBackgroundPath:     "/Pictures/home.png",
		HomeBackgroundDisabled: true,
		HomeBackgroundOpacity:  0.42,
		VisibleSections:        []string{"changes", "graph"},
	}
	if err := saveAppSettings(dir, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := loadAppSettings(dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed the settings: want %+v, got %+v", want, got)
	}
	if !filepath.IsAbs(settingsStorePath(dir)) {
		t.Fatalf("settings path is not absolute: %q", settingsStorePath(dir))
	}
}

func TestSettingsStoreToleratesMissingAndDamagedFiles(t *testing.T) {
	dir := t.TempDir()
	missing := loadAppSettings(dir)
	if missing.Theme != "Tokyo Night" || missing.Font != "System" {
		t.Fatalf("a missing file must give the defaults, got %+v", missing)
	}
	if err := os.WriteFile(settingsStorePath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	damaged := loadAppSettings(dir)
	if !reflect.DeepEqual(damaged, defaultSettings()) {
		t.Fatalf("a damaged file must give the defaults, got %+v", damaged)
	}
	if err := saveAppSettings("", defaultSettings()); err != nil {
		t.Fatalf("saving without a directory: %v", err)
	}
}

func TestNormalizeSettingsRejectsUnknownValues(t *testing.T) {
	got := normalizeSettings(appSettings{
		Theme:    "Not a theme",
		FontSize: 200,
		Font:     " ",
		Spacing:  50,
		Radius:   -3,
	})
	want := defaultSettings()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid settings were not normalized: want %+v, got %+v", want, got)
	}
}

func TestHomeBackgroundOpacityDefaultsAndNormalization(t *testing.T) {
	if got := defaultSettings().HomeBackgroundOpacity; got != defaultHomeBackgroundOpacity {
		t.Fatalf("default opacity = %v, want %v", got, defaultHomeBackgroundOpacity)
	}
	for _, invalid := range []float64{0, -0.1, 0.01, 1.1, math.NaN(), math.Inf(1)} {
		if got := normalizeHomeBackgroundOpacity(invalid); got != defaultHomeBackgroundOpacity {
			t.Errorf("opacity %v normalized to %v, want the default", invalid, got)
		}
	}
	for _, valid := range []float64{0.05, 0.24, 0.6, 1} {
		if got := normalizeHomeBackgroundOpacity(valid); got != valid {
			t.Errorf("valid opacity %v normalized to %v", valid, got)
		}
	}
}

func TestThemeCatalogAppliesMetricsAndFollowsSystem(t *testing.T) {
	system := appSettings{Theme: "System"}
	if !themeForSettings(system, true).Dark {
		t.Fatal("System did not follow a dark desktop")
	}
	if themeForSettings(system, false).Dark {
		t.Fatal("System did not follow a light desktop")
	}
	for _, name := range themeNames {
		t.Run(name, func(t *testing.T) {
			theme := themeForSettings(appSettings{
				Theme:    name,
				FontSize: 15,
				Font:     "Menlo",
				Spacing:  5,
				Radius:   4,
			}, true)
			if theme == nil {
				t.Fatal("no theme built")
			}
			if theme.FontSize != 15 || theme.Spacing != 5 || theme.Radius != 4 {
				t.Fatalf("metrics not applied: %+v", theme)
			}
			if theme.Font != "Menlo" {
				t.Fatalf("font not applied: %q", theme.Font)
			}
		})
	}
	if !themeForSettings(appSettings{Theme: "Dracula"}, false).Dark {
		t.Fatal("a dark palette is not dark")
	}
	if themeForSettings(appSettings{Theme: "GitHub Light"}, true).Dark {
		t.Fatal("a light palette is dark")
	}
}

func TestPopularThemeAdditionsAreCatalogued(t *testing.T) {
	dark := []string{
		"Solarized Dark", "Catppuccin Frappé", "Catppuccin Macchiato",
		"Rosé Pine", "Kanagawa Wave", "Everforest Dark", "Ayu Mirage",
	}
	for _, name := range dark {
		if !slices.Contains(themeNames, name) {
			t.Errorf("the theme menu is missing %q", name)
			continue
		}
		if theme := themeForSettings(appSettings{Theme: name}, false); !theme.Dark {
			t.Errorf("%s was not built as a dark palette", name)
		}
	}
	if !slices.Contains(themeNames, "Rosé Pine Dawn") {
		t.Fatal("the theme menu is missing Rosé Pine Dawn")
	}
	if theme := themeForSettings(appSettings{Theme: "Rosé Pine Dawn"}, true); theme.Dark {
		t.Fatal("Rosé Pine Dawn was not built as a light palette")
	}
}

// The header's sun or moon opens the theme list rather than flipping the
// appearance by itself: a theme is one pick from the whole catalog.
func TestThemeButtonOpensTheThemeList(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settings = defaultSettings()
	a.settings.Theme = "GitHub Light"
	tt := ui.NewTester(a.view, 960, 640)
	tt.SetDark(false)
	tt.Frame()

	if err := tt.Click("Theme"); err != nil {
		t.Fatalf("click the theme icon: %v", err)
	}
	tt.Frame()
	for _, want := range []string{"System", "Dracula", "Catppuccin Mocha", "Kanagawa Wave", "Rosé Pine Dawn"} {
		if !tt.HasText(want) {
			t.Fatalf("the theme list is missing %q: %v", want, tt.Texts())
		}
	}
	if err := tt.Click("Dracula"); err != nil {
		t.Fatalf("choose Dracula: %v", err)
	}
	tt.Frame()
	if a.settings.Theme != "Dracula" {
		t.Fatalf("the theme list chose %q, want Dracula", a.settings.Theme)
	}
}

// The workspace header keeps the theme on the bar: appearance is a repository
// tool in daily use, unlike settings and the shortcut list.
func TestHeaderBarChoosesATheme(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.Theme = "GitHub Light"
	h.tester.Frame()

	if err := h.tester.Click("Theme"); err != nil {
		t.Fatalf("open the theme list: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Dracula"); err != nil {
		t.Fatalf("choose Dracula: %v", err)
	}
	h.tester.Frame()
	if h.app.settings.Theme != "Dracula" {
		t.Fatalf("the header chose %q, want Dracula", h.app.settings.Theme)
	}
}

func TestSettingsLoadAppliesTheToggles(t *testing.T) {
	dir := t.TempDir()
	if err := saveAppSettings(dir, appSettings{
		Theme:           "Gruvbox Dark",
		FontSize:        15,
		Font:            "System",
		Spacing:         4,
		Radius:          6,
		DiffSplit:       true,
		DiffWrap:        true,
		IgnoreUntracked: true,
	}); err != nil {
		t.Fatal(err)
	}
	a := newApp(fakeOpener{})
	a.pathUserData = dir
	a.loadSettings()
	if a.settings.Theme != "Gruvbox Dark" || !a.diffSplit || !a.diffWrap || !a.ignoreUntracked {
		t.Fatalf("stored settings not applied: %+v diff=%v wrap=%v untracked=%v",
			a.settings, a.diffSplit, a.diffWrap, a.ignoreUntracked)
	}
}

func TestSettingsModalOpensAndCloses(t *testing.T) {
	h := menuTestApp(t)
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if !h.app.settingsOpen {
		t.Fatal("Ctrl+, did not open the settings modal")
	}
	for _, want := range []string{"Settings", "Language", "Theme", "Accent color", "Home background", "Done"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("the modal does not show %q: %q", want, h.texts())
		}
	}
	if err := h.tester.Click("Typography settings"); err != nil {
		t.Fatalf("open Typography settings: %v", err)
	}
	h.tester.Frame()
	for _, want := range []string{"Font size", "Font family", "Spacing", "Corner radius"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("Typography settings do not show %q: %q", want, h.texts())
		}
	}
	if slices.Contains(h.texts(), "Accent color") {
		t.Fatal("the Appearance controls stayed on the Typography page")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.settingsOpen {
		t.Fatal("Escape did not close the settings modal")
	}
}

func TestSettingsTogglesReachTheRunningApp(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settings.DiffSplit = true
	a.settings.IgnoreUntracked = true
	a.applySettingsChange()
	if !a.diffSplit || !a.ignoreUntracked {
		t.Fatalf("toggles did not reach the app: diff=%v untracked=%v", a.diffSplit, a.ignoreUntracked)
	}
}

// The accent replaces the palette's own in every place the app draws it:
// the fill of primary buttons, the text on it, and the washes that follow.
func TestAccentColorRepaintsThePalette(t *testing.T) {
	for _, theme := range []string{"Tokyo Night", "GitHub Light"} {
		plain := themeForSettings(appSettings{Theme: theme}, true)
		teal := themeForSettings(appSettings{Theme: theme, Accent: "Teal"}, true)
		want := accentChoices["Teal"]
		if teal.Accent != want {
			t.Fatalf("%s: the accent is %v, want %v", theme, teal.Accent, want)
		}
		if teal.Accent == plain.Accent {
			t.Fatalf("%s: the accent did not change", theme)
		}
		if teal.AccentText != accentText(want) {
			t.Fatalf("%s: text on the accent is %v", theme, teal.AccentText)
		}
		if teal.AccentHover == want || teal.AccentPressed == want {
			t.Fatalf("%s: hover and press keep the accent", theme)
		}
		if teal.Selection == plain.Selection || teal.Focus == plain.Focus {
			t.Fatalf("%s: the washes did not follow the accent", theme)
		}
	}
}

// An accent naming no color falls back to the palette's own; every offered
// name survives, so the row's choice round-trips through the store.
func TestNormalizeSettingsKeepsOnlyKnownAccents(t *testing.T) {
	if got := normalizeSettings(appSettings{Theme: defaultTheme, Accent: "Chartreuse"}); got.Accent != "" {
		t.Fatalf("an unknown accent became %q", got.Accent)
	}
	for _, name := range accentNames {
		got := normalizeSettings(appSettings{Theme: defaultTheme, Accent: name})
		if got.Accent != name {
			t.Fatalf("the accent %q became %q", name, got.Accent)
		}
	}
}

// The row is a pick, not a name to type: clicking a color repaints the window
// on the next frame, and the palette's dot takes it back.
func TestSettingsAccentRowPicksAColor(t *testing.T) {
	h := menuTestApp(t)
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if err := h.tester.Click("Teal"); err != nil {
		t.Fatalf("pick Teal: %v", err)
	}
	h.tester.Frame()
	if h.app.settings.Accent != "Teal" {
		t.Fatalf("the row chose %q, want Teal", h.app.settings.Accent)
	}
	if err := h.tester.Click("Theme color"); err != nil {
		t.Fatalf("go back to the palette's accent: %v", err)
	}
	h.tester.Frame()
	if h.app.settings.Accent != "" {
		t.Fatalf("the palette's dot left the accent %q", h.app.settings.Accent)
	}
}
