package app

import "testing"

// Searching tags narrows the visible list but keeps the source tag selected,
// so actions and the details pane still refer to the matching tag.
func TestTagSearchFiltersWithoutChangingTheSelectedTag(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionTags
	h.tester.Frame()

	if err := h.tester.Click("Search repository tags"); err != nil {
		t.Fatalf("focus tag search: %v", err)
	}
	h.tester.Type("interim")
	h.tester.Frame()

	visible := h.app.visibleTags()
	if len(visible) != 1 || visible[0].tag.Name != "build/interim" {
		t.Fatalf("tag search rows = %#v, want only build/interim", visible)
	}
	if h.app.selectedTag != 1 {
		t.Fatalf("filtered selection targets source tag %d, want 1", h.app.selectedTag)
	}
	if !h.tester.HasText("Interim build") {
		t.Fatalf("filtered tag details are missing: %q", h.texts())
	}
}
