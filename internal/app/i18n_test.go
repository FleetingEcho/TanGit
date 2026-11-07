package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// English is the default, and an unknown or missing stored language falls
// back to it rather than leaving the interface untranslated.
func TestLanguageDefaultsToEnglish(t *testing.T) {
	if got := defaultSettings().Language; got != "en" {
		t.Fatalf("the default language is %q, want en", got)
	}
	if got := normalizeLanguage(""); got != "en" {
		t.Fatalf("an empty language became %q, want en", got)
	}
	if got := normalizeLanguage("not a language"); got != "en" {
		t.Fatalf("an unknown language became %q, want en", got)
	}
	if got := normalizeLanguage("zh-Hans"); got != "zh-Hans" {
		t.Fatalf("Simplified Chinese became %q", got)
	}
	if got := languageName("zh-Hans"); got != "简体中文" {
		t.Fatalf("the Chinese menu label is %q", got)
	}
}

// Choosing Simplified Chinese in the settings modal translates the interface
// on the next frame, not only after a restart.
func TestLanguageSelectorTranslatesTheInterface(t *testing.T) {
	h := menuTestApp(t)
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()

	if err := h.tester.Click("English"); err != nil {
		t.Fatalf("open the language menu: %v", err)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "简体中文") {
		t.Fatalf("the language menu is missing Simplified Chinese: %q", h.texts())
	}
	if err := h.tester.Click("简体中文"); err != nil {
		t.Fatalf("choose Simplified Chinese: %v", err)
	}
	h.tester.Frame()
	if got := h.app.settings.Language; got != "zh-Hans" {
		t.Fatalf("the language menu chose %q, want zh-Hans", got)
	}
	for _, want := range []string{"设置", "外观", "语言", "主题", "主页背景", "选择图片…"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("the Chinese interface does not show %q: %q", want, h.texts())
		}
	}
	if slices.Contains(h.texts(), "Settings") {
		t.Fatalf("the English settings title survived the switch: %q", h.texts())
	}

	// The sidebar and the pane titles are translated too, not only the
	// modal.
	h.app.closeSettings()
	h.tester.Frame()
	for _, want := range []string{"变更", "历史", "分支", "工作区"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("the Chinese workspace does not show %q: %q", want, h.texts())
		}
	}

	// The choice is stored, so the next run starts in Chinese.
	h.app.pathUserData = t.TempDir()
	h.app.saveSettings()
	loaded := loadAppSettings(h.app.pathUserData)
	if loaded.Language != "zh-Hans" {
		t.Fatalf("the stored language is %q, want zh-Hans", loaded.Language)
	}

	// Leave the process-wide language as the other tests expect it.
	currentLanguage = defaultLanguage
}

// A stored Chinese choice is applied when the settings load.
func TestStoredLanguageAppliesOnLoad(t *testing.T) {
	dir := t.TempDir()
	if err := saveAppSettings(dir, appSettings{
		Theme:    "System",
		Language: "zh-Hans",
	}); err != nil {
		t.Fatal(err)
	}
	a := newApp(fakeOpener{})
	a.pathUserData = dir
	a.loadSettings()
	if got := a.settings.Language; got != "zh-Hans" {
		t.Fatalf("the loaded language is %q, want zh-Hans", got)
	}
	if got := currentLanguage; got != "zh-Hans" {
		t.Fatalf("the frame language is %q, want zh-Hans", got)
	}
	currentLanguage = defaultLanguage
}
