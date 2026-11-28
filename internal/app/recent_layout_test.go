package app

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestHomeLayoutSwitchKeepsRecentRowsAndFilter(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/work/first", Branch: "main"},
		recentRepository{Path: "/work/second", Branch: "topic"},
	)
	path, pathOK := tt.Find("Repository path")
	first, firstOK := tt.Find("Open /work/first")
	second, secondOK := tt.Find("Open /work/second")
	if !pathOK || !firstOK || !secondOK || path.X >= first.X ||
		first.X != second.X || first.H != recentSplitRowHeight || first.Y+first.H+recentRowGap != second.Y {
		t.Fatalf("side-by-side layout must have an adjacent list with aligned rows: path=%+v first=%+v second=%+v", path, first, second)
	}

	if err := tt.Click("Search recent repositories"); err != nil {
		t.Fatal(err)
	}
	tt.Type("second")
	tt.Frame()
	if err := tt.Click("Stacked layout"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	browse, browseOK := tt.Find("Browse files")
	second, secondOK = tt.Find("Open /work/second")
	if !a.pickerStacked || !browseOK || !secondOK || second.Y <= browse.Y+browse.H ||
		tt.HasText("/work/first") || second.H != recentRowHeight {
		t.Fatalf("stacked layout must put the filtered list beneath the launcher: browse=%+v second=%+v texts=%q", browse, second, tt.Texts())
	}
	if err := tt.Click("Side-by-side layout"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.pickerStacked || tt.HasText("/work/first") {
		t.Fatalf("switching back lost the layout or filter: stacked=%v filter=%q", a.pickerStacked, a.recentFilter)
	}

	tt.SetSize(720, 800)
	tt.Frame()
	browse, browseOK = tt.Find("Browse files")
	second, secondOK = tt.Find("Open /work/second")
	if !browseOK || !secondOK || second.Y <= browse.Y+browse.H || a.pickerStacked {
		t.Fatalf("narrow window must stack without changing the layout preference: browse=%+v second=%+v", browse, second)
	}
	tt.SetSize(1280, 800)
	tt.Frame()
	second, secondOK = tt.Find("Open /work/second")
	if !secondOK || second.H != recentSplitRowHeight {
		t.Fatalf("widening must restore the preferred layout: second=%+v", second)
	}
}

func TestHomeRecentSelectionUsesThemeText(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%t", dark), func(t *testing.T) {
			a, tt, _ := recentTester(t, recentRepository{Path: "/work/project", Name: "Sample project"})
			if !dark {
				a.settings.Theme = "System"
			}
			tt.SetDark(dark)
			tt.Frame()
			if !rectHasColor(tt, "Sample project", themeForSettings(a.settings, dark).Text) {
				t.Fatal("selected project title is missing the theme's foreground color")
			}
		})
	}
}

func TestHomeCentersLauncherWhenCurrentModeHasNoHistory(t *testing.T) {
	a, tt, _ := recentTester(t, recentRepository{Path: "/work/project", Branch: "main"})
	if err := tt.Click("Search recent repositories"); err != nil {
		t.Fatal(err)
	}
	tt.Type("no match")
	tt.Frame()
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	hero, heroOK := tt.Find("Open a workspace.")
	browse, browseOK := tt.Find("Browse files")
	if !heroOK || !browseOK || hero.Y < 230 || browse.Y+browse.H/2 < 400 || a.recentFilter != "" ||
		tt.HasText("Recent repositories") || tt.HasText("Recent folders") {
		t.Fatalf("empty Editor mode must center the launcher: hero=%+v browse=%+v texts=%q", hero, browse, tt.Texts())
	}
	if err := tt.Click("Git repository"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("/work/project") {
		t.Fatal("returning to Git mode did not restore its history")
	}
}

func TestHomeRecentPanelFillsRemainingSpaceWithAnyResultCount(t *testing.T) {
	for _, stacked := range []bool{false, true} {
		t.Run(fmt.Sprintf("stacked=%t", stacked), func(t *testing.T) {
			a, tt, _ := recentTester(t,
				recentRepository{Path: "/work/first", EditorOnly: true},
				recentRepository{Path: "/work/second", EditorOnly: true},
			)
			a.pickerStacked = stacked
			a.pickerOpenMode = int(openAllowPlainEditor)
			tt.SetSize(1024, 940)
			tt.Frame()

			// The list's footer reports how many cards it shows.
			footer, ok := tt.Find("2 folders")
			pageFooter, pageOK := tt.Find("lazygitGo <path>")
			if !ok || !pageOK || pageFooter.Y-footer.Y > 80 || pageFooter.Y-footer.Y < 30 {
				t.Fatalf("list footer should be near the bottom of the page: list=%+v page=%+v", footer, pageFooter)
			}

			for _, step := range []struct{ query, count string }{{"second", "1 folder"}, {"no matches", "0 folders"}} {
				query := step.query
				a.recentFilter = query
				tt.Frame()
				filtered, ok := tt.Find(step.count)
				if !ok || filtered.Y != footer.Y {
					t.Fatalf("filtering by %q moved the list footer: before=%+v after=%+v", query, footer, filtered)
				}
			}
		})
	}
}

func TestHomeRecentScrollLeavesTableHeadersAndFooterInPlace(t *testing.T) {
	repositories := make([]recentRepository, 12)
	for i := range repositories {
		repositories[i] = recentRepository{Path: fmt.Sprintf("/work/repository-%02d", i+1)}
	}
	for _, stacked := range []bool{false, true} {
		t.Run(fmt.Sprintf("stacked=%t", stacked), func(t *testing.T) {
			a, tt, _ := recentTester(t, repositories...)
			a.pickerStacked = stacked
			tt.SetSize(1024, 940)
			tt.Frame()
			row, rowOK := tt.Find("Open /work/repository-01")
			header, headerOK := tt.Find("Repository / branch")
			footer, footerOK := tt.Find("12 repositories")
			if !rowOK || !headerOK || !footerOK || header.Y >= row.Y || row.Y >= footer.Y {
				t.Fatalf("missing table header, first row or footer: header=%+v row=%+v footer=%+v", header, row, footer)
			}
			tt.Wheel(row.X+row.W/2, row.Y+row.H/2, 0, 1000)
			tt.Advance(time.Second)
			tt.Frame()
			last, lastOK := tt.Find("Open /work/repository-12")
			headerAfter, _ := tt.Find("Repository / branch")
			footerAfter, _ := tt.Find("12 repositories")
			if !lastOK || last.Y < header.Y || last.Y >= footer.Y ||
				header.Y != headerAfter.Y || footer.Y != footerAfter.Y {
				t.Fatalf("scroll should reveal the last row inside the table: last=%+v header before/after=%+v/%+v footer before/after=%+v/%+v", last, header, headerAfter, footer, footerAfter)
			}
		})
	}
}

func TestHomeRecentArrowKeysRevealSelection(t *testing.T) {
	repositories := make([]recentRepository, 12)
	for i := range repositories {
		repositories[i].Path = fmt.Sprintf("/work/repository-%02d", i+1)
	}
	for _, stacked := range []bool{false, true} {
		t.Run(fmt.Sprintf("stacked=%t", stacked), func(t *testing.T) {
			a, tt, _ := recentTester(t, repositories...)
			a.pickerStacked = stacked
			tt.Frame()
			if !tt.Focused("Repository path") {
				t.Fatal("the path field should still receive typing by default")
			}
			for range len(repositories) {
				tt.Key(0, ui.KeyDown)
				tt.Frame()
			}
			last, ok := tt.Find("Open /work/repository-12")
			footer, _ := tt.Find("12 repositories")
			if a.selectedRecent != 11 || !ok || last.H == 0 || last.Y+last.H > footer.Y {
				t.Fatalf("Down did not reveal the last repository: selected=%d last=%+v footer=%+v", a.selectedRecent, last, footer)
			}
			for range len(repositories) {
				tt.Key(0, ui.KeyUp)
				tt.Frame()
			}
			first, ok := tt.Find("Open /work/repository-01")
			header, _ := tt.Find("Repository / branch")
			if a.selectedRecent != 0 || !ok || first.H == 0 || first.Y < header.Y+header.H {
				t.Fatalf("Up did not reveal the first repository: selected=%d first=%+v header=%+v", a.selectedRecent, first, header)
			}
			tt.Key(0, ui.KeyDown)
			tt.Key(0, ui.KeyEnter)
			tt.Frame()
			if a.path != repositories[1].Path {
				t.Fatalf("Enter opened %q, want %q", a.path, repositories[1].Path)
			}
		})
	}
}

func TestHomeRecentArrowKeysUseFilteredEditorFolders(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/work/notes-repo"},
		recentRepository{Path: "/work/notes-1", EditorOnly: true},
		recentRepository{Path: "/work/other", EditorOnly: true},
		recentRepository{Path: "/work/notes-2", EditorOnly: true},
	)
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Search recent folders"); err != nil {
		t.Fatal(err)
	}
	tt.Type("notes")
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.selectedRecent != 3 || a.recentFilter != "notes" {
		t.Fatalf("Down should skip nonmatching rows: selected=%d filter=%q", a.selectedRecent, a.recentFilter)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.path != "/work/notes-2" {
		t.Fatalf("Enter from the filter opened %q, want /work/notes-2", a.path)
	}
}

func TestHomeLayoutsScreenshot(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to capture the home layouts")
	}
	repositories := make([]recentRepository, 12)
	for i, name := range []string{"core-platform", "lazygitApp", "lazygitgo", "lazygit",
		"design-system", "api-gateway", "desktop-client", "build-tools", "docs", "web-console", "server", "metrics"} {
		repositories[i] = recentRepository{Path: "/work/" + name, Branch: "main", LastOpened: timeNow() - int64(300*(i+1))}
	}
	repositories = append(repositories, recentRepository{Path: "/home", EditorOnly: true, LastOpened: timeNow() - 86400})
	cases := []struct {
		name    string
		stacked bool
		editor  bool
		empty   bool
	}{
		{name: "home-stacked", stacked: true},
		{name: "home-side-by-side"},
		{name: "home-editor", editor: true},
		{name: "home-empty", empty: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, tt, _ := recentTester(t, repositories...)
			a.openOverviewTab = func(string, overviewIntent) {}
			a.pickerStacked = tc.stacked
			if tc.editor {
				a.pickerOpenMode = int(openAllowPlainEditor)
			}
			if tc.empty {
				a.recentRepositories = nil
			}
			tt.SetSize(1024, 940)
			tt.SetDark(true)
			tt.Frame()
			saveScreenshot(t, dir, tc.name, tt)
		})
	}
}
