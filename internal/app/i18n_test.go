package app

import (
	"regexp"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// English is the default, and an unknown or missing stored language falls
// back to it rather than leaving the interface untranslated.
func TestLanguageDefaultsToEnglish(t *testing.T) {
	want := []languageChoice{
		{Code: "en", Name: "English"},
		{Code: "zh-Hans", Name: "简体中文"},
		{Code: "zh-Hant", Name: "繁體中文"},
		{Code: "ja", Name: "日本語"},
		{Code: "es", Name: "Español"},
		{Code: "fr", Name: "Français"},
	}
	if !slices.Equal(languageChoices, want) {
		t.Fatalf("language choices = %#v, want %#v", languageChoices, want)
	}
	if got := defaultSettings().Language; got != "en" {
		t.Fatalf("the default language is %q, want en", got)
	}
	for _, code := range []string{"", "not a language", "zh", "en-US"} {
		if got := normalizeLanguage(code); got != "en" {
			t.Fatalf("normalizeLanguage(%q) = %q, want en", code, got)
		}
	}
	for _, choice := range want {
		if got := normalizeLanguage(choice.Code); got != choice.Code {
			t.Errorf("normalizeLanguage(%q) = %q", choice.Code, got)
		}
		if got := languageName(choice.Code); got != choice.Name {
			t.Errorf("languageName(%q) = %q, want %q", choice.Code, got, choice.Name)
		}
	}
	if got := languageName("unknown"); got != "English" {
		t.Fatalf("an unknown language has menu label %q, want English", got)
	}
}

var (
	formatDirective = regexp.MustCompile(`%(\[[0-9]+\])?[+#0 -]*[0-9]*(\.[0-9]+)?[a-zA-Z]`)
	argumentIndex   = regexp.MustCompile(`\[[0-9]+\]`)
)

func sortedFormatDirectives(text string) []string {
	directives := formatDirective.FindAllString(text, -1)
	for i, directive := range directives {
		// An argument index changes the order, not the kind of value required.
		directives[i] = argumentIndex.ReplaceAllString(directive, "")
	}
	slices.Sort(directives)
	return directives
}

// Every catalog covers the same English keys as the established Simplified
// Chinese catalog. Format arguments can be reordered, but none can disappear.
func TestTranslationCatalogsAreComplete(t *testing.T) {
	if len(translations) != len(languageChoices)-1 {
		t.Fatalf("there are %d catalogs for %d languages", len(translations), len(languageChoices))
	}
	for _, choice := range languageChoices[1:] {
		catalog := translations[choice.Code]
		if len(catalog) != len(zhHans) {
			t.Errorf("%s has %d entries, want %d", choice.Code, len(catalog), len(zhHans))
		}
		for english, translation := range catalog {
			if _, ok := zhHans[english]; !ok {
				t.Errorf("%s has obsolete key %q", choice.Code, english)
			}
			if translation == "" {
				t.Errorf("%s translates %q to an empty string", choice.Code, english)
			}
			if got, want := sortedFormatDirectives(translation), sortedFormatDirectives(english); !slices.Equal(got, want) {
				t.Errorf("%s format directives for %q = %q, want %q", choice.Code, english, got, want)
			}
		}
		for english := range zhHans {
			if _, ok := catalog[english]; !ok {
				t.Errorf("%s is missing %q", choice.Code, english)
			}
		}
		for _, english := range []string{"Settings", "Workspace", "Changes", "History", "Search"} {
			if catalog[english] == "" || catalog[english] == english {
				t.Errorf("%s does not translate %q: %q", choice.Code, english, catalog[english])
			}
		}
	}
}

// Choosing any language in the settings modal translates the interface on
// the next frame, not only after a restart, and stores the choice.
func TestLanguageSelectorTranslatesTheInterface(t *testing.T) {
	for _, choice := range languageChoices[1:] {
		t.Run(choice.Code, func(t *testing.T) {
			t.Cleanup(func() { currentLanguage = defaultLanguage })
			h := menuTestApp(t)
			h.tester.Key(ui.Ctrl, ui.KeyComma)
			h.tester.Frame()

			if err := h.tester.Click("English"); err != nil {
				t.Fatalf("open the language menu: %v", err)
			}
			h.tester.Frame()
			for _, available := range languageChoices {
				if !slices.Contains(h.texts(), available.Name) {
					t.Fatalf("the language menu is missing %s: %q", available.Name, h.texts())
				}
			}
			if err := h.tester.Click(choice.Name); err != nil {
				t.Fatalf("choose %s: %v", choice.Name, err)
			}
			h.tester.Frame()
			if got := h.app.settings.Language; got != choice.Code {
				t.Fatalf("the language menu chose %q, want %q", got, choice.Code)
			}
			catalog := translations[choice.Code]
			for _, english := range []string{"Settings", "Appearance", "Language", "Theme", "Home background", "Choose image…"} {
				want := catalog[english]
				if !slices.Contains(h.texts(), want) {
					t.Fatalf("the %s interface does not show %q for %q: %q", choice.Name, want, english, h.texts())
				}
			}
			if slices.Contains(h.texts(), "Settings") {
				t.Fatalf("the English settings title survived the switch: %q", h.texts())
			}

			// The sidebar and pane titles are translated too, not only the modal.
			h.app.closeSettings()
			h.tester.Frame()
			for _, english := range []string{"Changes", "History", "Branches", "Workspace"} {
				want := catalog[english]
				if !slices.Contains(h.texts(), want) {
					t.Fatalf("the %s workspace does not show %q for %q: %q", choice.Name, want, english, h.texts())
				}
			}

			h.app.pathUserData = t.TempDir()
			h.app.saveSettings()
			loaded := loadAppSettings(h.app.pathUserData)
			if loaded.Language != choice.Code {
				t.Fatalf("the stored language is %q, want %q", loaded.Language, choice.Code)
			}
		})
	}
}

func TestDetachedCommitCheckoutLabelIsTranslated(t *testing.T) {
	t.Cleanup(func() { currentLanguage = defaultLanguage })
	const english = "Checkout to this commit (detached)…"
	for _, choice := range languageChoices {
		if choice.Code == defaultLanguage {
			continue
		}
		t.Run(choice.Code, func(t *testing.T) {
			currentLanguage = choice.Code
			if got, want := tr(english), translations[choice.Code][english]; got == english || got != want {
				t.Fatalf("detached checkout label = %q, want translation %q", got, want)
			}
		})
	}
}

// A stored choice in any supported language is applied when settings load.
func TestStoredLanguageAppliesOnLoad(t *testing.T) {
	for _, choice := range languageChoices {
		t.Run(choice.Code, func(t *testing.T) {
			t.Cleanup(func() { currentLanguage = defaultLanguage })
			dir := t.TempDir()
			if err := saveAppSettings(dir, appSettings{
				Theme:    "System",
				Language: choice.Code,
			}); err != nil {
				t.Fatal(err)
			}
			a := newApp(fakeOpener{})
			a.pathUserData = dir
			a.loadSettings()
			if got := a.settings.Language; got != choice.Code {
				t.Fatalf("the loaded language is %q, want %q", got, choice.Code)
			}
			if got := currentLanguage; got != choice.Code {
				t.Fatalf("the frame language is %q, want %q", got, choice.Code)
			}
		})
	}
}
