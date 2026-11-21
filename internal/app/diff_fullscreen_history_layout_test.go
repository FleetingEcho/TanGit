package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFullscreenDetailsVerticalSplitUsesCompactSpacing(t *testing.T) {
	height := float32(50)
	tester := ui.NewTester(func(c *ui.Context) {
		paneSplitVerticalReserve(c, &height, 0, 2,
			func() { ui.Box(c).Fill().Label("First details pane") },
			func() { ui.Box(c).Fill().Label("Second details pane") }).Fill()
	}, 240, 240)
	tester.Frame()
	first, firstOK := tester.Find("First details pane")
	second, secondOK := tester.Find("Second details pane")
	if !firstOK || !secondOK {
		t.Fatalf("split panes are missing: first=%+v second=%+v", first, second)
	}
	gap := second.Y - (first.Y + first.H)
	if gap > 5 { // 2 DIP above + the 1 DIP divider + 2 DIP below.
		t.Fatalf("vertical details split has %.1f DIP around its 1 DIP divider; want 2 DIP on each side", gap)
	}
}

// A short expansion of the full-window commit details must keep the metadata
// readable rather than clipping the hash and everything below it.
func TestWholeWindowDiffExpandedDetailsKeepMetadataReachable(t *testing.T) {
	h, _ := historyHarness(t)
	h.tester.SetSize(895, 768)
	h.waitFor(t, "the commit diff", func() bool {
		return h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	h.app.openDiffFullscreen()
	h.tester.Frame()
	dragVerticalPaneGrip(t, h.tester, ui.Rect{W: 895}, 0, 65)
	h.tester.Frame()
	h.tester.Frame()
	details, found := h.tester.Find("Commit details")
	if !found || details.H <= 0 {
		t.Fatalf("expanded commit details are missing: %+v", details)
	}
	hash, found := h.tester.Access("Copy commit hash")
	if !found {
		t.Fatal("expanded commit details lost the commit hash action")
	}
	author, found := h.tester.Access("Ada <ada@example.com>")
	if !found {
		t.Fatal("expanded commit details lost the author metadata")
	}
	fits := func(info ui.AccessInfo) bool {
		return !info.Offscreen && info.Bounds.Y >= details.Y && info.Bounds.Y+info.Bounds.H <= details.Y+details.H
	}
	if !fits(author) {
		if h.app.diffFullscreenDetailsScroll.MaxY <= 0 {
			t.Fatalf("commit metadata is clipped with no way to scroll the details: hash=%+v author=%+v details=%+v", hash, author, details)
		}
		// Scroll the metadata container itself, not the nested Markdown
		// body, just far enough to reveal the complete author row.
		chosenHeight := h.app.diffFullscreenDetailsHeight
		delta := max(hash.Bounds.Y+hash.Bounds.H, author.Bounds.Y+author.Bounds.H) - (details.Y + details.H) + 4
		h.tester.Scroll(details.X+details.W/2, details.Y+details.H/2, 0, delta)
		h.tester.Frame()
		h.tester.Frame()
		details, _ = h.tester.Find("Commit details")
		author, _ = h.tester.Access("Ada <ada@example.com>")
		if h.app.diffFullscreenDetailsScroll.Y <= 0 {
			t.Fatalf("the details scroll did not move: state=%+v", h.app.diffFullscreenDetailsScroll)
		}
		if !fits(author) {
			t.Fatalf("scrolling did not reveal the complete author row: author=%+v details=%+v", author, details)
		}
		if h.app.diffFullscreenDetailsHeight != chosenHeight {
			t.Fatalf("scrolling metadata changed the user's splitter height from %v to %v", chosenHeight, h.app.diffFullscreenDetailsHeight)
		}
	}
}
