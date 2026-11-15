package app

import (
	lslices "slices"
	"testing"
)

// The native guides describe a parent rail and current child connector for
// each level. Root items delegate to normal file icons without extra chrome.
func TestTreeIndentSegmentsDescribeParentRails(t *testing.T) {
	if got := treeIndentSegments(0); len(got) != 0 {
		t.Fatalf("a root item has a guide before its parent exists: %v", got)
	}
	if got := treeIndentSegments(-1); len(got) != 0 {
		t.Fatalf("invalid depth created a guide: %v", got)
	}
	guides := treeIndentSegments(3)
	if len(guides) != 4 {
		t.Fatalf("depth 3 has %d segments, want three vertical parents and current connector", len(guides))
	}
	if guides[0].vertical.X1 == guides[0].vertical.X2 && guides[0].vertical.Y1 == guides[0].vertical.Y2 {
		t.Fatalf("vertical rail has no length: %+v", guides[0].vertical)
	}
	for index, line := range guides[:3] {
		if got, want := line.vertical.X1, float32(12+16*index); got != want {
			t.Errorf("rail %d is outside the 16px indent grid: X1=%.1f, want %.1f", index, got, want)
		}
	}
	for i, line := range guides[:3] {
		if line.vertical.X1 == 0 {
			t.Errorf("rail %d was lost during indentation: %+v", i, line)
		}
		if line.vertical.Y2 < line.vertical.Y1+42 {
			t.Errorf("rail %d does not reach the next visible folder: %+v", i, line)
		}
	}
	if !lslices.IsSortedFunc(guides[:3], func(a, b treeGuideSegment) int {
		return int(a.horizontal.X1 - b.horizontal.X1)
	}) {
		t.Fatal("deeper indentation rails drift outside one consistent 16-DIP grid")
	}
	connector := guides[len(guides)-1].horizontal
	if connector.X1 < 0 || connector.Y1 < 0 || connector.X2 <= connector.X1 {
		t.Fatalf("current child connector switches direction: %+v", connector)
	}
}

func TestTreeGuideColorKeepsTextClearOnSelection(t *testing.T) {
	normal := treeGuideColor(nil, false)
	selected := treeGuideColor(nil, true)
	if selected.A <= normal.A {
		t.Fatalf("selected guides must be at least as legible as normal guides: normal=%v selected=%v", normal.A, selected.A)
	}
}
