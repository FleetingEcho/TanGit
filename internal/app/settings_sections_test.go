package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestNormalizeVisibleSections(t *testing.T) {
	got := normalizeVisibleSections([]string{"reflog", "not a section", "tags", "tags"})
	want := []string{"tags", "reflog"}
	if !slices.Equal(got, want) {
		t.Fatalf("unknown ids must go and known ones keep tab order: got %q, want %q", got, want)
	}
	if got := normalizeVisibleSections(nil); !slices.Equal(got, defaultVisibleSections) {
		t.Fatalf("a file without the choice must start from the defaults: got %q", got)
	}
	if got := normalizeVisibleSections([]string{"not a section"}); !slices.Equal(got, defaultVisibleSections) {
		t.Fatalf("a file naming no known section must start from the defaults: got %q", got)
	}
	if got := normalizeVisibleSections([]string{"reflog"}); !slices.Equal(got, []string{"reflog"}) {
		t.Fatalf("one remaining section must survive: got %q", got)
	}
}

func TestNormalizeSectionOrder(t *testing.T) {
	got := normalizeSectionOrder([]string{"tags", "unknown", "tags", "code"})
	want := append([]string{"tags", "code"}, slices.DeleteFunc(slices.Clone(workspaceSectionIDs), func(id string) bool {
		return id == "tags" || id == "code"
	})...)
	if !slices.Equal(got, want) {
		t.Fatalf("normalized order = %v, want %v", got, want)
	}
	if got := normalizeSectionOrder(nil); !slices.Equal(got, workspaceSectionIDs) {
		t.Fatalf("default order = %v", got)
	}
}

func TestWorkspaceSectionDragReordersSidebar(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.settingsPage = settingsPageWorkspace
	h.app.openSettings()
	h.tester.Frame()
	for _, name := range workspaceSections {
		if chip, ok := h.tester.AccessByRole(ui.RoleCheckBox, "Show "+name); !ok || !chip.Checked {
			t.Fatalf("%s should be enabled by default", name)
		}
	}
	drag := func(source, target string) {
		t.Helper()
		from, fromOK := h.tester.Find("Show " + source)
		to, toOK := h.tester.Find("Show " + target)
		if !fromOK || !toOK {
			t.Fatalf("missing drag chips: %s, %s", source, target)
		}
		x, y := from.X+from.W/2, from.Y+from.H/2
		tx, ty := to.X+to.W/2, to.Y+to.H/2
		h.tester.Press(x, y)
		h.tester.Move(x+10, y)
		h.tester.Move(tx, ty)
		h.tester.Release(tx, ty)
		h.tester.Frame()
	}

	// Move across wrapped rows; a drag must never toggle the dragged page.
	drag("Tags", "Editor")
	if h.app.visibleSections()[0] != sectionTags || !slices.Equal(h.app.settings.VisibleSections, workspaceSectionIDs) {
		t.Fatalf("drag changed visibility or missed the first position: %+v", h.app.settings)
	}
	if saved := loadAppSettings(h.app.pathUserData); !slices.Equal(saved.SectionOrder, h.app.settings.SectionOrder) {
		t.Fatalf("order did not persist: %v", saved.SectionOrder)
	}
	h.app.closeSettings()
	h.tester.Frame()
	h.press(t, ui.Key1)
	if h.app.workspaceTab != sectionTags || h.app.navSelected != 0 {
		t.Fatalf("digit shortcut ignored order: page=%d, row=%d", h.app.workspaceTab, h.app.navSelected)
	}

	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Show Tags"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	drag("Tags", "Graph")
	if h.app.sectionVisible(sectionTags) || h.app.settings.SectionOrder[4] != "tags" {
		t.Fatalf("moving a hidden page changed its visibility or missed its target: %v", h.app.settings.SectionOrder)
	}
	if err := h.tester.Click("Show Tags"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.visibleSections()[4] != sectionTags {
		t.Fatal("enabling the page lost its chosen position")
	}

	// The sole enabled page can still be dragged without being disabled.
	h.app.settings.VisibleSections = []string{"tags"}
	h.tester.Frame()
	drag("Tags", "Editor")
	if h.app.settings.SectionOrder[0] != "tags" || !slices.Equal(h.app.settings.VisibleSections, []string{"tags"}) {
		t.Fatal("dragging the sole enabled page changed visibility or failed")
	}
}

// A fresh workspace lists every section, using the editor's new display name
// while retaining its stable "code" settings id.
func TestDefaultSectionsShowEveryWorkspaceTab(t *testing.T) {
	h := menuTestApp(t)
	if !slices.Equal(h.app.settings.VisibleSections, workspaceSectionIDs) {
		t.Fatalf("default sections = %q, want %q", h.app.settings.VisibleSections, workspaceSectionIDs)
	}
	for _, shown := range workspaceSections {
		if !slices.Contains(h.texts(), shown) {
			t.Errorf("the default sidebar is missing %q: %q", shown, h.texts())
		}
	}
}

// Hiding a section takes its row out of the sidebar, and a repository whose
// first sections are hidden still opens on a section that shows.
func TestHiddenSectionLeavesTheSidebar(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.VisibleSections = []string{
		"graph", "branches", "remotes", "tags", "worktrees", "submodules", "reflog",
	}
	h.app.applySettingsChange()
	h.tester.Frame()

	if h.app.workspaceTab == sectionChanges || h.app.workspaceTab == sectionHistory {
		t.Fatalf("the workspace stayed on a hidden section: %d", h.app.workspaceTab)
	}
	if h.app.workspaceTab != sectionGraph {
		t.Fatalf("the workspace did not fall back to the first visible section: %d", h.app.workspaceTab)
	}
	for _, gone := range []string{"Changes", "History"} {
		if slices.Contains(h.texts(), gone) {
			t.Fatalf("the sidebar still shows %q: %q", gone, h.texts())
		}
	}
	if !slices.Contains(h.texts(), "Graph") || !slices.Contains(h.texts(), "Reflog") {
		t.Fatalf("the sidebar lost its visible sections: %q", h.texts())
	}
}

// A click on a sidebar row switches to the section that row shows, not to the
// one whose index the row would have had before sections were hidden.
func TestSidebarClickMapsVisibleRows(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.VisibleSections = []string{"graph", "branches", "remotes"}
	h.app.applySettingsChange()
	h.tester.Frame()

	if err := h.tester.Click("Branches"); err != nil {
		t.Fatalf("click the Branches row: %v", err)
	}
	h.tester.Frame()
	if h.app.workspaceTab != sectionBranches {
		t.Fatalf("the sidebar row opened section %d, want Branches", h.app.workspaceTab)
	}
	if h.app.navSelected != 1 {
		t.Fatalf("the Branches row is row 1, got %d", h.app.navSelected)
	}
}

// The digit shortcuts number the sections that show: with two hidden, 2 is
// Branches rather than the original second section.
func TestDigitShortcutsFollowVisibleSections(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.VisibleSections = []string{"graph", "branches", "remotes"}
	h.app.applySettingsChange()
	h.tester.Frame()

	h.press(t, ui.Key2)
	if h.app.workspaceTab != sectionBranches {
		t.Fatalf("2 opened section %d, want Branches", h.app.workspaceTab)
	}
	h.press(t, ui.Key1)
	if h.app.workspaceTab != sectionGraph {
		t.Fatalf("1 opened section %d, want Graph", h.app.workspaceTab)
	}
}

// Hiding the section being read switches to one that shows, and the last
// visible section cannot be hidden.
func TestHidingSectionsKeepsOnePane(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
	h.app.applySettingsChange()
	h.tester.Frame()
	h.app.setWorkspaceTab(sectionTags)
	h.tester.Frame()

	h.app.setSectionVisible(sectionTags, false)
	h.tester.Frame()
	if h.app.sectionVisible(sectionTags) {
		t.Fatal("the section stayed visible after being hidden")
	}
	if h.app.workspaceTab == sectionTags {
		t.Fatal("the workspace stayed on the section it just hid")
	}
	if !h.app.sectionVisible(h.app.workspaceTab) {
		t.Fatalf("the workspace fell back to another hidden section: %d", h.app.workspaceTab)
	}

	h.app.settings.VisibleSections = []string{"changes"}
	h.app.applySettingsChange()
	h.tester.Frame()
	h.app.setSectionVisible(sectionChanges, false)
	if !h.app.sectionVisible(sectionChanges) {
		t.Fatal("the last visible section was hidden")
	}
	if visible := h.app.visibleSections(); len(visible) != 1 {
		t.Fatalf("the workspace should keep one section, has %v", visible)
	}
}

// Every section can be toggled directly from a chip without opening a popup.
func TestSettingsSectionPickerTogglesTabs(t *testing.T) {
	h := menuTestApp(t)
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if !h.app.settingsOpen {
		t.Fatal("the settings modal did not open")
	}

	if err := h.tester.Click("Workspace settings"); err != nil {
		t.Fatalf("open Workspace settings: %v", err)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Show Tags") {
		t.Fatalf("the picker does not offer every section: %q", h.texts())
	}

	// Every section starts visible: hide Tags, then show it again.
	if err := h.tester.Click("Show Tags"); err != nil {
		t.Fatalf("hide Tags: %v", err)
	}
	h.tester.Frame()
	if h.app.sectionVisible(sectionTags) {
		t.Fatal("the picker did not hide Tags")
	}
	if slices.Contains(h.app.settings.VisibleSections, "tags") {
		t.Fatalf("the hidden section was still stored: %q", h.app.settings.VisibleSections)
	}
	if chip, ok := h.tester.AccessByRole(ui.RoleCheckBox, "Show Tags"); !ok || chip.Checked {
		t.Fatal("the Tags chip did not expose its disabled state")
	}

	if err := h.tester.Click("Show Tags"); err != nil {
		t.Fatalf("show Tags again: %v", err)
	}
	h.tester.Frame()
	if !h.app.sectionVisible(sectionTags) {
		t.Fatal("the picker did not show Tags again")
	}

	if chip, ok := h.tester.AccessByRole(ui.RoleCheckBox, "Show Tags"); !ok || !chip.Checked {
		t.Fatal("the Tags chip did not expose its enabled state")
	}
	for section := range workspaceSectionIDs {
		if section == sectionTags {
			continue
		}
		if err := h.tester.Click("Show " + workspaceSections[section]); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
	}
	if err := h.tester.Click("Show Tags"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !slices.Equal(h.app.settings.VisibleSections, []string{"tags"}) {
		t.Fatalf("the last enabled chip was disabled: %v", h.app.settings.VisibleSections)
	}

	h.app.closeSettings()
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Tags") {
		t.Fatalf("the sidebar does not show the restored Tags section: %q", h.texts())
	}
}
