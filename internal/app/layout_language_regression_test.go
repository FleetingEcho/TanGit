package app

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The diff helper owns the spacing between its controls. Leaving it to each
// pane made tightly packed headers collide, especially with wider translations.
func TestDiffViewOptionsKeepSpaceBetweenControls(t *testing.T) {
	before := currentLanguage
	currentLanguage = "zh-Hans"
	t.Cleanup(func() { currentLanguage = before })

	a := newApp(fakeOpener{})
	tester := ui.NewTester(func(c *ui.Context) { a.diffViewOptions(c) }, 800, 100)
	tester.Frame()
	labels := []string{
		"Toggle whitespace-only diff changes",
		"Change diff context lines",
		"Toggle diff layout",
		"Toggle diff line wrapping",
		"Save diff",
		"Expand diff",
	}
	previousRight := float32(0)
	for i, label := range labels {
		rect, ok := tester.Find(tr(label))
		if !ok {
			t.Fatalf("missing diff control %q: %q", label, tester.Texts())
		}
		if i > 0 {
			gap := rect.X - previousRight
			if gap < 4 {
				t.Errorf("gap before %q is %.2f DIP, want at least 4", label, gap)
			}
		}
		previousRight = rect.X + rect.W
	}
}

// A translated placeholder must not change the quick-search field's rectangle.
func TestBranchSearchGeometryDoesNotDependOnLanguage(t *testing.T) {
	type geometry struct{ width, height float32 }
	geometries := make(map[string]geometry)
	for _, language := range []string{"en", "zh-Hans"} {
		t.Run(language, func(t *testing.T) {
			t.Cleanup(func() { currentLanguage = defaultLanguage })
			h := menuTestApp(t)
			h.app.settings.Language = language
			h.app.workspaceTab = sectionBranches
			h.tester.Frame()
			field, ok := h.tester.Find(tr("Search local branches"))
			if !ok {
				t.Fatalf("the branch search field is missing: %q", h.texts())
			}
			geometries[language] = geometry{width: field.W, height: field.H}
			t.Logf("search field = %.2fx%.2f DIP", field.W, field.H)
		})
	}
	english, chinese := geometries["en"], geometries["zh-Hans"]
	if math.Abs(float64(english.width-chinese.width)) > 0.01 ||
		math.Abs(float64(english.height-chinese.height)) > 0.01 {
		t.Fatalf("English field = %+v, Chinese field = %+v; geometry must not depend on the placeholder", english, chinese)
	}
}
