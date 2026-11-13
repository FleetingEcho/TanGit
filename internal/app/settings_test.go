package app

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
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
	if !slices.Contains(h.texts(), "Enable keyboard shortcuts") {
		t.Fatalf("the settings do not offer the switch: %q", h.texts())
	}
	if err := h.tester.Click("Enable keyboard shortcuts"); err != nil {
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
		if err := h.tester.Click("Enable keyboard shortcuts"); err != nil {
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

func TestFontChoicesListEveryAvailableFamily(t *testing.T) {
	system := ui.FontFamilies()
	choices := fontChoices("")
	if len(system) == 0 {
		t.Skip("the headless text stack exposes no system font families")
	}
	if choices[0] != "System" {
		t.Fatalf("System must be the first choice: %q", choices)
	}
	for _, family := range system {
		if !slices.Contains(choices, family.Name) {
			t.Errorf("font choices omit available family %q", family.Name)
		}
	}
}

func TestFontChoicesPreserveSelectedUnavailableFamily(t *testing.T) {
	families := []ui.FontFamily{{Name: "DejaVu Sans"}, {Name: "Noto Sans"}}
	got := fontChoicesFrom(families, "Uninstalled Local Font")
	want := []string{"System", "DejaVu Sans", "Noto Sans", "Uninstalled Local Font"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("font choices = %q, want %q", got, want)
	}
	if got := fontChoicesFrom(families, "System"); !reflect.DeepEqual(got, []string{"System", "DejaVu Sans", "Noto Sans"}) {
		t.Fatalf("System was duplicated or local family omitted: %q", got)
	}
}

func TestFontChoicesSortRegisteredAndSystemFamilies(t *testing.T) {
	got := fontChoicesFrom([]ui.FontFamily{{Name: "zeta"}, {Name: "Alpha"}, {Name: "beta"}}, "")
	if !reflect.DeepEqual(got, []string{"System", "Alpha", "beta", "zeta"}) {
		t.Fatalf("font choices are not sorted case-insensitively: %q", got)
	}
}

func TestSettingsSearchableFontsChooseIndependently(t *testing.T) {
	families := ui.FontFamilies()
	if len(families) == 0 {
		t.Skip("the headless text stack exposes no system font families")
	}
	want := families[0].Name
	h := menuTestApp(t)
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Typography settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()

	// The interface picker still supports browsing and choosing with a click.
	if err := h.tester.Click("Search interface fonts"); err != nil {
		t.Fatalf("open interface font choices: %v", err)
	}
	h.tester.Frame()
	if !h.tester.HasText(want) {
		t.Fatalf("font menu does not offer system family %q: %q", want, h.texts())
	}
	if err := h.tester.Click(want); err != nil {
		t.Fatalf("choose system family %q: %v", want, err)
	}
	h.tester.Frame()
	if h.app.settings.Font != want || h.app.settings.BufferFont != "System" {
		t.Fatalf("interface choice changed the wrong settings: %+v", h.app.settings)
	}

	// Typing replaces the selected text and filters the buffer families;
	// Enter takes the keyboard-highlighted match without any scrolling.
	if err := h.tester.Click("Search buffer fonts"); err != nil {
		t.Fatalf("focus buffer font search: %v", err)
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type(want)
	h.tester.Frame()
	if err := h.tester.Click(want); err != nil {
		// A click is also a valid choice in the filtered list, and avoids an
		// exact full-family match leaving no highlighted suggestion for Enter.
		t.Fatalf("choose filtered buffer family %q: %v", want, err)
	}
	h.tester.Frame()
	if h.app.settings.BufferFont != want || h.app.settings.Font != want {
		t.Fatalf("buffer search did not preserve both independent choices: %+v", h.app.settings)
	}
}

func TestSettingsBufferFontKeyboardSelection(t *testing.T) {
	families := ui.FontFamilies()
	if len(families) == 0 {
		t.Skip("the headless text stack exposes no system font families")
	}
	want := families[0].Name
	h := menuTestApp(t)
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Typography settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Scroll(700, 400, 0, 240)
	h.tester.Frame()
	if err := h.tester.Click("Search buffer fonts"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type(want)
	h.tester.Frame()
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	if h.app.settings.BufferFont != want {
		t.Fatalf("typing %q and Enter selected %q, want %q", want, h.app.settings.BufferFont, want)
	}
	if h.app.settings.Font != "System" {
		t.Fatalf("buffer keyboard selection changed the interface font to %q", h.app.settings.Font)
	}
}

func TestSettingsBufferTypographyControlsAreIndependent(t *testing.T) {
	h := menuTestApp(t)
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Typography settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	uiFontSize := h.app.settings.FontSize
	stepUp := func(label string) {
		t.Helper()
		bounds, found := h.tester.Find(label)
		if !found {
			t.Fatalf("no number input named %q", label)
		}
		if bounds.W <= 0 || bounds.H <= 0 {
			h.tester.Scroll(700, 400, 0, 240)
			h.tester.Frame()
			bounds, found = h.tester.Find(label)
			if !found || bounds.W <= 0 || bounds.H <= 0 {
				t.Fatalf("number input %q is not visible after scrolling", label)
			}
		}
		h.tester.ClickAt(bounds.X+20, bounds.Y+bounds.H/2)
		h.tester.Key(0, ui.KeyUp)
		h.tester.Frame()
	}
	stepUp("Buffer font size settings")
	if h.app.settings.BufferFontSize != defaultBufferFontSize+1 || h.app.settings.FontSize != uiFontSize {
		t.Fatalf("buffer size step changed the wrong settings: buffer %v, interface %v",
			h.app.settings.BufferFontSize, h.app.settings.FontSize)
	}
	stepUp("Buffer line spacing settings")
	if h.app.settings.BufferLineHeight != defaultBufferLineHeight+0.1 {
		t.Fatalf("buffer line spacing = %v, want %v", h.app.settings.BufferLineHeight, defaultBufferLineHeight+0.1)
	}

	bounds, found := h.tester.Find("Buffer font size settings")
	if !found {
		t.Fatal("buffer font size input disappeared")
	}
	h.tester.ClickAt(bounds.X+20, bounds.Y+bounds.H/2)
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("16")
	h.tester.Frame()
	if h.app.settings.BufferFontSize != 16 || h.app.settings.FontSize != uiFontSize {
		t.Fatalf("typed buffer size = %v, interface size = %v", h.app.settings.BufferFontSize, h.app.settings.FontSize)
	}
}

func TestBufferFontUpdatesCodeViewsWithoutChangingTheInterface(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settings = defaultSettings()
	a.settings.Font = "Interface Sans"
	a.settings.FontSize = 17
	a.settings.Spacing = 8
	a.settings.BufferFont = "Buffer Mono"
	a.settings.BufferFontSize = 15
	a.settings.BufferLineHeight = 1.8
	a.diffState = &editor.DiffState{}
	a.fileEditState = &editor.State{}
	a.conflictView = &editor.State{}
	a.conflictStageViews[1] = &editor.State{}
	a.blameState = &editor.State{}
	a.conflictSides = newConflictSides(repository.File{Path: "code.go"}, "package main\n")
	a.conflictSides.viewState()

	a.applyBufferFonts()
	states := map[string]*editor.State{
		"file editor":    a.fileEditState,
		"conflict":       a.conflictView,
		"conflict stage": a.conflictStageViews[1],
		"conflict side":  a.conflictSides.view,
		"blame":          a.blameState,
	}
	for name, state := range states {
		if state.Font.Family != "Buffer Mono" || state.Font.Size != 15 || state.LineHeight != 1.8 {
			t.Errorf("%s typography = font %+v, line height %v; want Buffer Mono at 15 and 1.8", name, state.Font, state.LineHeight)
		}
	}
	if a.diffState.Font.Family != "Buffer Mono" || a.diffState.Font.Size != 15 || a.diffState.LineHeight != 1.8 {
		t.Errorf("diff typography = font %+v, line height %v; want Buffer Mono at 15 and 1.8", a.diffState.Font, a.diffState.LineHeight)
	}
	theme := themeForSettings(a.settings, false)
	if theme.Font != "Interface Sans" || theme.FontSize != 17 || theme.Spacing != 8 {
		t.Errorf("buffer typography changed interface theme: %+v", theme)
	}

	a.settings.BufferFont = "System"
	a.applyBufferFonts()
	if a.diffState.Font.Family != "" || a.fileEditState.Font.Family != "" {
		t.Fatal("System must restore MyGo's default monospaced buffer family")
	}
}

func TestSettingsStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := appSettings{
		Theme:                     "Dracula",
		Accent:                    "Teal",
		FontSize:                  16,
		Font:                      "Menlo",
		BufferFont:                "JetBrains Mono",
		BufferFontSize:            15,
		BufferLineHeight:          1.8,
		Spacing:                   5,
		Radius:                    3,
		DiffSplit:                 true,
		DiffWrap:                  true,
		IgnoreUntracked:           true,
		DisableShortcuts:          true,
		Language:                  "zh-Hans",
		EditorCommand:             "zed {{filename}}",
		ExternalEditors:           []editorPreset{{Name: "Fleet", Command: "fleet {{filename}}"}},
		SearchExcludedDirectories: []string{"generated", "node_modules"},
		LanguageServers: map[string]languageServerSettings{
			"go": {Path: "/tools/gopls", Arguments: []string{"-remote=auto"}},
		},
		HomeBackgroundPath:     "/Pictures/home.png",
		HomeBackgroundDisabled: true,
		HomeBackgroundOpacity:  0.42,
		VisibleSections:        []string{"changes", "graph"},
		SectionOrder:           normalizeSectionOrder([]string{"graph", "changes"}),
		RemoteWebURLs: map[string]map[string]string{
			"/work/project": {"origin": "https://example.com/team/project"},
		},
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

func TestSettingsSearchExclusionsDefaultNormalizeAndPersistEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(settingsStorePath(dir), []byte(`{"theme":"Tokyo Night"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := loadAppSettings(dir)
	if !slices.Contains(legacy.SearchExcludedDirectories, "node_modules") ||
		slices.Contains(legacy.SearchExcludedDirectories, "vendor") {
		t.Fatalf("legacy exclusions = %v, want common generated folders but not vendor", legacy.SearchExcludedDirectories)
	}

	custom := defaultSettings()
	custom.SearchExcludedDirectories = []string{" generated ", "", ".", "..", "nested/path", "node_modules", "generated", ".nx"}
	custom = normalizeSettings(custom)
	if got, want := custom.SearchExcludedDirectories, []string{".nx", "generated", "node_modules"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized exclusions = %v, want %v", got, want)
	}

	custom.SearchExcludedDirectories = []string{}
	if err := saveAppSettings(dir, custom); err != nil {
		t.Fatal(err)
	}
	loaded := loadAppSettings(dir)
	if loaded.SearchExcludedDirectories == nil || len(loaded.SearchExcludedDirectories) != 0 {
		t.Fatalf("saved empty exclusions became %v, want a persistent empty list", loaded.SearchExcludedDirectories)
	}
	data, err := os.ReadFile(settingsStorePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(strings.Split(string(data), "\n"), `  "searchExcludedDirectories": [],`) {
		t.Fatalf("settings did not serialize an explicit empty list:\n%s", data)
	}
}

func TestSettingsStoreToleratesMissingAndDamagedFiles(t *testing.T) {
	dir := t.TempDir()
	missing := loadAppSettings(dir)
	if missing.Theme != "Tokyo Night" || missing.Font != "System" || missing.BufferFont != "System" ||
		missing.BufferFontSize != defaultBufferFontSize || missing.BufferLineHeight != defaultBufferLineHeight {
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
		Theme:            "Not a theme",
		FontSize:         200,
		Font:             " ",
		BufferFont:       " ",
		BufferFontSize:   200,
		BufferLineHeight: 0.1,
		Spacing:          50,
		Radius:           -3,
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

func TestThemeCatalogRunsFromLightToDark(t *testing.T) {
	if len(themeNames) == 0 || themeNames[0] != "System" {
		t.Fatalf("System must stay first in the theme list: %q", themeNames)
	}
	if len(themeNames) != len(themeChoices) {
		t.Fatalf("theme list has %d entries but the catalog has %d", len(themeNames), len(themeChoices))
	}
	seen := make(map[string]bool, len(themeNames))
	previous := math.Inf(1)
	for i, name := range themeNames {
		if seen[name] {
			t.Errorf("duplicate theme %q", name)
		}
		seen[name] = true
		if _, ok := themeChoices[name]; !ok {
			t.Errorf("theme %q has no palette", name)
			continue
		}
		if i == 0 {
			continue
		}
		luminance := colorRelativeLuminance(themeForSettings(appSettings{Theme: name}, false).Background)
		if luminance > previous+1e-12 {
			t.Errorf("%q (luminance %.6f) is lighter than %q (luminance %.6f)",
				name, luminance, themeNames[i-1], previous)
		}
		if luminance == previous && themeNames[i-1] > name {
			t.Errorf("equal-luminance themes %q and %q are not alphabetical", themeNames[i-1], name)
		}
		previous = luminance
	}
}

func TestHighContrastMatchesVSCodeColorRoles(t *testing.T) {
	theme := themeForSettings(appSettings{Theme: "High Contrast"}, false)
	for name, check := range map[string]struct {
		got, want ui.Color
	}{
		"background": {theme.Background, ui.Hex("#000000")},
		"surface":    {theme.Surface, ui.Hex("#000000")},
		"text":       {theme.Text, ui.Hex("#ffffff")},
		"border":     {theme.Border, ui.Hex("#6fc3df")},
		"accent":     {theme.Accent, ui.Hex("#21a6ff")},
		"focus":      {theme.Focus, ui.RGBA(111, 195, 223, 1)},
	} {
		if check.got != check.want {
			t.Errorf("%s = %v, want VS Code High Contrast %v", name, check.got, check.want)
		}
	}
	if !theme.Dark {
		t.Fatal("High Contrast was not built as a dark palette")
	}
	for name, foreground := range map[string]ui.Color{
		"text": theme.Text, "muted text": theme.TextMuted, "border": theme.Border,
	} {
		if ratio := colorContrastRatio(foreground, theme.Background); ratio < 7 {
			t.Errorf("%s has only %.2f:1 contrast against black", name, ratio)
		}
	}
}

func TestHighContrastLightMatchesVSCodeColorRoles(t *testing.T) {
	theme := themeForSettings(appSettings{Theme: "High Contrast Light"}, true)
	for name, check := range map[string]struct {
		got, want ui.Color
	}{
		"background": {theme.Background, ui.Hex("#ffffff")},
		"surface":    {theme.Surface, ui.Hex("#ffffff")},
		"text":       {theme.Text, ui.Hex("#292929")},
		"border":     {theme.Border, ui.Hex("#0f4a85")},
		"accent":     {theme.Accent, ui.Hex("#0f4a85")},
		"focus":      {theme.Focus, ui.RGBA(0, 107, 189, 1)},
	} {
		if check.got != check.want {
			t.Errorf("%s = %v, want VS Code Light High Contrast %v", name, check.got, check.want)
		}
	}
	if theme.Dark {
		t.Fatal("High Contrast Light was not built as a light palette")
	}
	for name, foreground := range map[string]ui.Color{
		"text": theme.Text, "muted text": theme.TextMuted, "border": theme.Border,
	} {
		if ratio := colorContrastRatio(foreground, theme.Background); ratio < 7 {
			t.Errorf("%s has only %.2f:1 contrast against white", name, ratio)
		}
	}
}

func TestAyuLightIsALightReadablePalette(t *testing.T) {
	if !slices.Contains(themeNames, "Ayu Light") {
		t.Fatal("the theme menu is missing Ayu Light")
	}
	theme := themeForSettings(appSettings{Theme: "Ayu Light"}, true)
	if theme.Dark {
		t.Fatal("Ayu Light was not built as a light palette")
	}
	if theme.Background != ui.Hex("#fafafa") || theme.Accent != ui.Hex("#ffaa33") {
		t.Fatalf("Ayu Light does not use the Ayu palette: background=%v accent=%v", theme.Background, theme.Accent)
	}
	if ratio := colorContrastRatio(theme.Text, theme.Background); ratio < 4.5 {
		t.Fatalf("Ayu Light text has only %.2f:1 contrast", ratio)
	}
}

func colorRelativeLuminance(color ui.Color) float64 {
	channel := func(value uint8) float64 {
		srgb := float64(value) / 255
		if srgb <= 0.04045 {
			return srgb / 12.92
		}
		return math.Pow((srgb+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(color.R) + 0.7152*channel(color.G) + 0.0722*channel(color.B)
}

func colorContrastRatio(first, second ui.Color) float64 {
	lighter := colorRelativeLuminance(first)
	darker := colorRelativeLuminance(second)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
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
	for _, want := range []string{
		"Interface font size", "Interface font", "Interface spacing",
		"Buffer font size", "Buffer font", "Buffer line spacing", "Corner radius",
	} {
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

func TestSettingsEditsSearchExcludedFolderList(t *testing.T) {
	h := menuTestApp(t)
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Editor settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.tester.HasText("Search exclusions") || !h.tester.HasText("Remove excluded folder node_modules") {
		t.Fatalf("Editor settings do not expose the exclusion editor: %q", h.texts())
	}
	if err := h.tester.Click("New excluded folder"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("cooked")
	h.tester.Frame()
	if err := h.tester.Click("Add"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !slices.Contains(h.app.settings.SearchExcludedDirectories, "cooked") || h.app.searchExcludedDirectoryDraft != "" {
		t.Fatalf("adding cooked produced %q with draft %q", h.app.settings.SearchExcludedDirectories, h.app.searchExcludedDirectoryDraft)
	}

	if err := h.tester.Click("Remove excluded folder node_modules"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if slices.Contains(h.app.settings.SearchExcludedDirectories, "node_modules") {
		t.Fatal("a default exclusion could not be removed")
	}
	h.tester.Scroll(700, 400, 0, 500)
	h.tester.Frame()
	if err := h.tester.Click("Clear list"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.settings.SearchExcludedDirectories == nil || len(h.app.settings.SearchExcludedDirectories) != 0 || !h.tester.HasText("No excluded folders") {
		t.Fatalf("Clear list left exclusions %v", h.app.settings.SearchExcludedDirectories)
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
	if got := normalizeSettings(appSettings{Theme: defaultTheme, Accent: "#12AbEF"}); got.Accent != "#12abef" {
		t.Fatalf("a custom accent became %q, want canonical hex", got.Accent)
	}
	for _, invalid := range []string{"#12ab", "#11223344", "112233", "#gg0000"} {
		if got := normalizeSettings(appSettings{Theme: defaultTheme, Accent: invalid}); got.Accent != "" {
			t.Fatalf("invalid custom accent %q became %q", invalid, got.Accent)
		}
	}
}

// The row is a pick, not a name to type: clicking a color repaints the window
// on the next frame, and the palette's dot takes it back.
func TestCustomAccentColorAppliesAnOpaqueReadableTheme(t *testing.T) {
	for _, color := range []ui.Color{ui.RGB(0, 0, 0), ui.RGB(255, 255, 255), ui.RGB(239, 68, 68)} {
		settings := appSettings{Theme: defaultTheme, Accent: formatCustomAccent(color)}
		theme := themeForSettings(settings, true)
		if theme.Accent != color || theme.Accent.A != 255 {
			t.Fatalf("custom accent %v became %v", color, theme.Accent)
		}
		if theme.AccentText != accentText(color) {
			t.Fatalf("custom accent %v has unreadable text %v", color, theme.AccentText)
		}
	}
}

func TestSettingsAccentColorWellPicksACustomColor(t *testing.T) {
	h := menuTestApp(t)
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if err := h.tester.Click("Custom accent color"); err != nil {
		t.Fatalf("open the custom color picker: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if err := h.tester.Click("Black"); err != nil {
		t.Fatalf("pick the color well's black: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if got, want := h.app.settings.Accent, "#000000"; got != want {
		t.Fatalf("the color well stored %q, want %q", got, want)
	}
	if got := themeForSettings(h.app.settings, true).Accent; got != ui.Hex("#000000") {
		t.Fatalf("the picked color did not reach the theme: %v", got)
	}
}

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
