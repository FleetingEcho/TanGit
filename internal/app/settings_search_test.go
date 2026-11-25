package app

import (
	"strings"
	"testing"
)

func TestSettingsSearchFindsAcrossPagesAndOpensOption(t *testing.T) {
	language := currentLanguage
	currentLanguage = defaultLanguage
	t.Cleanup(func() { currentLanguage = language })
	h := menuTestApp(t)
	h.app.openSettings()
	h.tester.Frame()
	search, ok := h.tester.Access("Search settings")
	closeSettings, closeFound := h.tester.Access("Close settings")
	preferences, preferencesFound := h.tester.Find("PREFERENCES")
	if !ok || !closeFound || !preferencesFound || search.Bounds.Y+search.Bounds.H > preferences.Y ||
		search.Bounds.X+search.Bounds.W > closeSettings.Bounds.X {
		t.Fatalf("settings search overlaps the category area: search=%+v close=%+v preferences=%+v",
			search.Bounds, closeSettings.Bounds, preferences)
	}
	if err := h.tester.Click("Search settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("font size")
	h.tester.Frame()
	if !h.tester.HasText("Interface font size") || !h.tester.HasText("Typography") {
		t.Fatalf("cross-page settings results are missing: %q", h.texts())
	}
	if err := h.tester.Click("Interface font size — Typography"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.settingsUI.page != settingsPageTypography || h.app.settingsUI.searchQuery != "" {
		t.Fatalf("choosing a result did not open and restore its page: page=%d query=%q",
			h.app.settingsUI.page, h.app.settingsUI.searchQuery)
	}
	if !h.tester.HasText("Buffer font size") {
		t.Fatal("the selected settings page is not visible after search")
	}

	if err := h.tester.Click("Search settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("no-such-setting-anywhere")
	h.tester.Frame()
	if !h.tester.HasText("No settings match your search") {
		t.Fatal("settings search did not show its empty state")
	}
	if err := h.tester.Click("Clear settings search"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.settingsUI.searchQuery != "" || !h.tester.HasText("Typography and layout") {
		t.Fatal("clearing search did not restore the selected settings page")
	}
}

func TestSettingsSearchMatchesTermsAndCatalogPages(t *testing.T) {
	language := currentLanguage
	currentLanguage = defaultLanguage
	t.Cleanup(func() { currentLanguage = language })
	seen := make(map[int]bool)
	for _, option := range settingsSearchCatalog {
		seen[option.page] = true
		if option.label == "" || settingsPageTitle(option.page) == "Settings" {
			t.Fatalf("invalid settings search entry: %+v", option)
		}
	}
	for _, page := range []int{
		settingsPageAppearance, settingsPageTypography, settingsPageBehavior,
		settingsPageEditor, settingsPageLSP, settingsPageWorkspace, settingsPageCache,
		settingsPageCommands, settingsPageSSHProfiles, settingsPageUpdates,
	} {
		if !seen[page] {
			t.Errorf("settings page %d has no searchable options", page)
		}
	}
	for _, test := range []struct {
		query string
		page  int
		label string
	}{
		{"FONT SIZE", settingsPageTypography, "Interface font size"},
		{"prune", settingsPageBehavior, "Prune stale remote-tracking branches on fetch"},
		{"zoekt", settingsPageCache, "Zoekt search indexes"},
		{"remote login", settingsPageSSHProfiles, "Username"},
	} {
		matches := settingsSearchMatches(test.query)
		found := false
		for _, option := range matches {
			if option.page == test.page && option.label == test.label {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("settings search %q did not find %s: %+v", test.query,
				strings.TrimSpace(test.label), matches)
		}
	}
}
