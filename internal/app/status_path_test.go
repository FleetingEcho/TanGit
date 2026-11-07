package app

import (
	"testing"
)

// Double-clicking the repository path in the status bar copies it, in place,
// so the path can go to a terminal without a menu.
func TestDoubleClickingTheRepositoryPathCopiesIt(t *testing.T) {
	h := menuTestApp(t)

	path, ok := h.tester.Find("/work/project")
	if !ok {
		t.Fatalf("the status bar shows no repository path: %q", h.texts())
	}
	h.tester.ClickAt(path.X+4, path.Y+path.H/2)
	h.tester.ClickAt(path.X+4, path.Y+path.H/2)
	h.tester.Frame()

	if got := h.tester.Clipboard(); got != "/work/project" {
		t.Fatalf("the double click copied %q", got)
	}
	if _, ok := h.tester.Find("Copied"); !ok {
		t.Fatalf("the bar does not say the path was copied: %q", h.texts())
	}

	// The copy is a moment, not a mode: after two seconds the path is back.
	h.tester.Advance(pathCopiedFor)
	h.tester.Frame()
	if _, ok := h.tester.Find("Copied"); ok {
		t.Fatalf("the bar still says \"Copied\" after %v", pathCopiedFor)
	}
	if _, ok := h.tester.Find("/work/project"); !ok {
		t.Fatalf("the path did not come back: %q", h.texts())
	}
}

// A single click on the path copies it too: the path is one piece of text,
// so a click and a double click are the same act.
func TestClickingTheRepositoryPathCopiesIt(t *testing.T) {
	h := menuTestApp(t)

	path, ok := h.tester.Find("/work/project")
	if !ok {
		t.Fatalf("the status bar shows no repository path: %q", h.texts())
	}
	h.tester.ClickAt(path.X+4, path.Y+path.H/2)
	h.tester.Frame()

	if got := h.tester.Clipboard(); got != "/work/project" {
		t.Fatalf("the click copied %q", got)
	}
	if _, ok := h.tester.Find("Copied"); !ok {
		t.Fatalf("the bar does not say the path was copied: %q", h.texts())
	}
}

// The copy icon after the path copies it as the path does, so the action does
// not depend on finding the text and the reading order stays path, action.
func TestTheRepositoryPathCopyIconCopiesIt(t *testing.T) {
	h := menuTestApp(t)

	path, ok := h.tester.Find("/work/project")
	if !ok {
		t.Fatalf("the status bar shows no repository path: %q", h.texts())
	}
	button, ok := h.tester.Find("Copy repository path")
	if !ok {
		t.Fatalf("the bar shows no copy icon: %q", h.texts())
	}
	if button.X < path.X+path.W {
		t.Fatalf("the copy icon is at x=%v, before the path's right edge %v", button.X, path.X+path.W)
	}
	h.tester.ClickAt(button.X+button.W/2, button.Y+button.H/2)
	h.tester.Frame()

	if got := h.tester.Clipboard(); got != "/work/project" {
		t.Fatalf("the copy icon copied %q", got)
	}
	if _, ok := h.tester.Find("Copied"); !ok {
		t.Fatalf("the bar does not say the path was copied: %q", h.texts())
	}
}
