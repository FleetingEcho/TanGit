package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestGuidedConflictCatalogsCoverEveryLanguage(t *testing.T) {
	before := currentLanguage
	t.Cleanup(func() { currentLanguage = before })
	languages := []string{"en", "zh-Hans", "zh-Hant", "ja", "es", "fr"}
	for _, entry := range guidedConflictTranslations {
		want := []string{entry.english, entry.zhHans, entry.zhHant, entry.japanese, entry.spanish, entry.french}
		for index, language := range languages {
			currentLanguage = language
			if got := tr(entry.english); got != want[index] {
				t.Errorf("%s catalog %q = %q, want %q", language, entry.english, got, want[index])
			}
		}
	}
}

func TestGuidedConflictLocalizedCopyFits240Pane(t *testing.T) {
	before := currentLanguage
	t.Cleanup(func() { currentLanguage = before })
	for _, language := range languageChoices {
		t.Run(language.Code, func(t *testing.T) {
			currentLanguage = language.Code
			checkFits := func(tt *ui.Tester, text string) {
				t.Helper()
				element, found := tt.Find(text)
				if !found || element.X < 0 || element.X+element.W > 240.5 || element.H <= 0 {
					t.Fatalf("localized %q is clipped: %+v, found=%v; texts=%q", text, element, found, tt.Texts())
				}
				for _, item := range tt.AccessAll() {
					if item.Role != ui.RoleButton || item.Offscreen {
						continue
					}
					bounds := item.Bounds
					if bounds.X < 0 || bounds.X+bounds.W > 240.5 || bounds.W <= 0 || bounds.H <= 0 {
						t.Errorf("localized %s button is clipped: %+v", item.Name, bounds)
					}
				}
			}

			a := guidedConflictPaneApp(0)
			stage := ui.NewTester(func(c *ui.Context) { a.conflictPane(c, c.Theme()) }, 240, 900)
			stage.SetDark(true)
			stage.Frame()
			checkFits(stage, tr("Read-only original code from Git stage 1."))

			operation := ui.NewTester(a.operationContextView, 240, 1200)
			operation.SetDark(true)
			operation.Frame()
			checkFits(operation, tr("Made a mistake here? Restore unresolved returns the saved unresolved file and Git stages."))
		})
	}
}

func guidedConflictPaneApp(tab int) *app {
	a := guidedOperationTestApp()
	a.conflictStages = repository.ConflictStages{
		Base:     repository.ConflictStage{Present: true, Content: "base\n"},
		Current:  repository.ConflictStage{Present: true, Content: "current\n"},
		Incoming: repository.ConflictStage{Present: true, Content: "incoming\n"},
	}
	a.conflictStageTab = tab
	a.refreshConflictViewer()
	return a
}

func TestGuidedConflictPaneFitsNarrowSectionsAndThemes(t *testing.T) {
	for _, width := range []int{1200, 960, 720, 320, 240} {
		for _, dark := range []bool{false, true} {
			for _, style := range []string{conflictActionStyleIconsLabels, conflictActionStyleGuided} {
				t.Run(fmt.Sprintf("width=%d/dark=%v/%s", width, dark, style), func(t *testing.T) {
					a := guidedConflictPaneApp(0)
					a.settings.ConflictActionStyle = style
					tt := ui.NewTester(func(c *ui.Context) { a.conflictPane(c, c.Theme()) }, width, 900)
					tt.SetDark(dark)
					tt.Frame()

					if guided := style == conflictActionStyleGuided; guided != tt.HasText("Conflict guidance") {
						t.Fatalf("guided explanation presence = %v, want %v: %q", !guided, guided, tt.Texts())
					}
					body := "Compare the common ancestor with both sides before choosing a resolution."
					if width < 480 {
						body = "Read-only original code from Git stage 1."
					}
					if style == conflictActionStyleGuided {
						element, found := tt.Find(body)
						if !found || element.X < 0 || element.X+element.W > float32(width)+0.5 || element.H <= 0 {
							t.Fatalf("guided explanation is clipped at %d: %+v, found=%v", width, element, found)
						}
					}
					controls := 0
					for _, item := range tt.AccessAll() {
						if item.Role != ui.RoleButton || item.Offscreen {
							continue
						}
						controls++
						bounds := item.Bounds
						if bounds.W <= 0 || bounds.H <= 0 || bounds.X < 0 || bounds.Y < 0 ||
							bounds.X+bounds.W > float32(width)+0.5 || bounds.Y+bounds.H > 900.5 {
							t.Errorf("%s is clipped at %d: %+v", item.Name, width, bounds)
						}
					}
					if controls == 0 {
						t.Fatal("conflict pane has no reachable controls")
					}
				})
			}
		}
	}
}

func TestGuidedConflictExplanationReflowsWithinTheSameWindow(t *testing.T) {
	a := guidedConflictPaneApp(0)
	paneWidth := float32(640)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Width(paneWidth).MinWidth(0).Children(func() { a.conflictPane(c, c.Theme()) })
	}, 1200, 900)
	tt.Frame()
	if !tt.HasText("Compare the common ancestor with both sides before choosing a resolution.") {
		t.Fatal("wide pane did not start with full guidance")
	}

	paneWidth = 320
	a.filePaneWidth = 320 // the real splitter publishes its changed measurement
	tt.Frame()
	if !tt.HasText("Read-only original code from Git stage 1.") ||
		tt.HasText("Compare the common ancestor with both sides before choosing a resolution.") {
		t.Fatalf("the same window did not collapse its narrower pane: %q", tt.Texts())
	}
	paneWidth = 640
	a.filePaneWidth = 640
	tt.Frame()
	if !tt.HasText("Compare the common ancestor with both sides before choosing a resolution.") {
		t.Fatal("restoring pane width did not restore full guidance")
	}
}

func TestGuidedConflictExplanationFollowsStageAndPane(t *testing.T) {
	for _, test := range []struct {
		tab       int
		title     string
		shortText string
		fullText  string
	}{
		{tab: 0, title: "Base: common ancestor", shortText: "Read-only original code from Git stage 1.", fullText: "Compare the common ancestor with both sides before choosing a resolution."},
		{tab: 1, title: "Current: your branch", shortText: "Read-only current-branch content from Git stage 2.", fullText: "Read what your branch changed relative to the common ancestor."},
		{tab: 2, title: "Incoming: other branch", shortText: "Read-only incoming content from Git stage 3.", fullText: "Read what the incoming branch changed relative to the common ancestor."},
		{tab: 3, title: "Result: editable resolution", shortText: "Choose a side or edit the working-tree resolution.", fullText: "Review the Result, then use Mark resolved to stage the file exactly as it is."},
	} {
		t.Run(fmt.Sprintf("tab=%d/wide-pane-in-wide-window", test.tab), func(t *testing.T) {
			a := guidedConflictPaneApp(test.tab)
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Width(640).Children(func() { a.conflictPane(c, c.Theme()) })
			}, 1200, 900)
			tt.Frame()
			for _, want := range []string{"Conflict guidance", test.title, test.fullText, "Hunk 2 of 2"} {
				if !tt.HasText(want) {
					t.Errorf("wide guided pane does not show %q: %q", want, tt.Texts())
				}
			}
		})
		t.Run(fmt.Sprintf("tab=%d/narrow-pane-in-wide-window", test.tab), func(t *testing.T) {
			a := guidedConflictPaneApp(test.tab)
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).Width(320).MinWidth(0).Children(func() { a.conflictPane(c, c.Theme()) })
			}, 1200, 900)
			tt.Frame()
			for _, want := range []string{"Conflict guidance", test.title, test.shortText, "Hunk 2 of 2"} {
				if !tt.HasText(want) {
					t.Errorf("narrow guided pane does not show %q: %q", want, tt.Texts())
				}
			}
			if tt.HasText(test.fullText) {
				t.Error("a narrow section kept the page-length explanation instead of its concise stage help")
			}
		})
	}
}
