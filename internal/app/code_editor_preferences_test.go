package app

import (
	"strings"
	"testing"
)

func TestBuiltInEditorPreferencesUseUnifiedEditorSettings(t *testing.T) {
	h := menuTestApp(t)
	h.app.openSettingsSection(settingsPageEditor)
	h.tester.Frame()
	if h.app.settingsUI.page != settingsPageEditor || !h.tester.HasText("Built-in editor preferences") {
		t.Fatalf("Editor page does not include built-in preferences: page=%d texts=%q", h.app.settingsUI.page, h.texts())
	}
	for _, label := range []string{
		"Line numbers", "Whitespace", "Wrap long lines", "Inlay hints",
		"Semantic highlighting", "Minimap", "Sticky symbol header",
	} {
		if !h.tester.HasText(label) {
			t.Errorf("Editor settings are missing %q", label)
		}
	}

	if !h.app.settings.EditorMinimap {
		t.Fatal("Minimap must be enabled by default")
	}
	// All Editor preferences share one scrolling page, with Keyboard shortcuts
	// last. Reach Minimap through the same scroll path before toggling it.
	h.tester.Scroll(700, 480, 0, 600)
	h.tester.Frame()
	minimapBounds, found := h.tester.Find("Minimap")
	if !found || minimapBounds.Y < 0 || minimapBounds.Y+minimapBounds.H > 800 {
		t.Fatalf("Minimap was not reachable after scrolling: %+v", minimapBounds)
	}
	if err := h.tester.Click("Minimap"); err != nil {
		t.Fatalf("toggle minimap: %v", err)
	}
	h.tester.Frame()
	if h.app.settings.EditorMinimap || !h.app.settings.EditorPreferencesConfigured {
		t.Fatalf("explicit minimap opt-out was not preserved and configured: %+v", h.app.settings)
	}
}

func TestStickySymbolHeaderTracksVisibleScopeAndPreference(t *testing.T) {
	stubCodeWorkspace(t, map[string]string{"main.go": strings.Repeat("// filler\n", 100)})
	h := menuTestApp(t)
	h.app.settings.EditorPreferencesConfigured = true
	h.app.settings.EditorStickyScroll = true
	h.app.settings.EditorMinimap = false
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("main.go", 0)
	document := waitForCodeDocument(t, h, "main.go")
	document.symbols = []languageServerDocumentSymbol{
		{Name: "first", Range: languageServerRange{End: languageServerPosition{Line: 10}}},
		{Name: "second", Range: languageServerRange{Start: languageServerPosition{Line: 40}, End: languageServerPosition{Line: 60}}},
	}
	document.state.ScrollTo(45)
	h.tester.Frame()
	h.tester.Frame()
	if first, _ := document.state.VisibleLines(); first <= 0 {
		t.Fatalf("test did not scroll the editor: first visible line = %d", first)
	}
	if !h.tester.HasText("second") {
		t.Fatalf("sticky header did not show the visible symbol: %q", h.texts())
	}
	h.app.settings.EditorStickyScroll = false
	h.tester.Frame()
	if h.tester.HasText("second") {
		t.Fatal("sticky symbol remained visible after the preference was disabled")
	}
}

func TestBuiltInEditorFeaturesAreEnabledByDefault(t *testing.T) {
	settings := defaultSettings()
	if settings.EditorLineNumbers != "on" || settings.EditorWhitespace != "selection" {
		t.Fatalf("unexpected default editor choices: lineNumbers=%q whitespace=%q", settings.EditorLineNumbers, settings.EditorWhitespace)
	}
	features := map[string]bool{
		"wrap long lines":       settings.EditorWordWrap,
		"auto-close":            settings.EditorAutoClose,
		"inlay hints":           settings.EditorInlayHints,
		"semantic highlighting": settings.EditorSemanticHighlighting,
		"minimap":               settings.EditorMinimap,
		"sticky symbol header":  settings.EditorStickyScroll,
	}
	for name, enabled := range features {
		if !enabled {
			t.Errorf("%s is not enabled by default", name)
		}
	}
	settings.EditorMinimap = false
	if got := normalizeSettings(settings); got.EditorMinimap {
		t.Fatal("normalization overrode an explicit Minimap opt-out")
	}
}

func TestMinimapKeepsCodeEditorVisible(t *testing.T) {
	stubCodeWorkspace(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	h := menuTestApp(t)
	h.app.settings.EditorPreferencesConfigured = true
	h.app.settings.EditorMinimap = true
	h.app.settings.EditorStickyScroll = false
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("main.go", 0)
	waitForCodeDocument(t, h, "main.go")
	h.tester.Frame()

	bounds, found := h.tester.Find("Code editor")
	minimap, minimapFound := h.tester.Find("Minimap")
	if !found || bounds.W <= 0 || bounds.H <= 0 {
		t.Fatalf("Minimap left the code editor without visible geometry: found=%v bounds=%+v minimapFound=%v minimap=%+v", found, bounds, minimapFound, minimap)
	}
	if !h.tester.HasText("Go to line 1") || !h.tester.HasText("Go to line 3") {
		t.Fatalf("Minimap hid the document's editor lines: %q", h.texts())
	}
}
