package app

import (
	"testing"
	"time"
)

// The tests assert settled layouts, so panes collapse at once in them; the test
// below turns the motion on.
func init() { paneCollapseDuration = 0 }

// Collapsing a pane slides the details beside it over to the edge in steps. The
// details never jump: the pane turns into its rail as it narrows, so nothing
// appears at the end of the slide.
func TestCollapsingAPaneSlidesWithoutJumping(t *testing.T) {
	paneCollapseDuration = 200 * time.Millisecond
	t.Cleanup(func() { paneCollapseDuration = 0 })

	h, _ := historyHarness(t)
	h.waitFor(t, "the commit's files", func() bool { return len(h.app.commitFiles) > 0 })
	for range 2 {
		h.tester.Frame()
	}
	detailsX := func() float32 {
		r, ok := h.tester.Find("Commit files")
		if !ok {
			t.Fatalf("the details are gone: %q", h.tester.Texts())
		}
		return r.X
	}
	start := detailsX()
	if err := h.tester.Click("Collapse list pane"); err != nil {
		t.Fatal(err)
	}
	var xs []float32
	for range 12 {
		h.tester.Frame()
		xs = append(xs, detailsX())
		h.tester.Advance(20 * time.Millisecond)
	}
	h.tester.Advance(time.Second)
	h.tester.Frame()
	h.tester.Frame()
	end := detailsX()
	xs = append(xs, end)
	if !(end < start-100) {
		t.Fatalf("the details did not move over: start=%.0f end=%.0f", start, end)
	}
	for i := 1; i < len(xs); i++ {
		if xs[i] > xs[i-1]+0.5 {
			t.Fatalf("the details moved back while the pane collapsed: %v", xs)
		}
		if xs[i-1]-xs[i] > 150 {
			t.Fatalf("the details jumped %.0f DIPs in one step: %v", xs[i-1]-xs[i], xs)
		}
	}
	if !h.tester.HasText("Expand list pane") || h.tester.HasText("Collapse list pane") {
		t.Fatalf("only the rail should remain: %q", h.tester.Texts())
	}

	// And back: the details slide out again, without a jump.
	if err := h.tester.Click("Expand list pane"); err != nil {
		t.Fatal(err)
	}
	xs = xs[:0]
	for range 12 {
		h.tester.Frame()
		xs = append(xs, detailsX())
		h.tester.Advance(20 * time.Millisecond)
	}
	h.tester.Advance(time.Second)
	h.tester.Frame()
	xs = append(xs, detailsX())
	for i := 1; i < len(xs); i++ {
		if xs[i] < xs[i-1]-0.5 || xs[i]-xs[i-1] > 150 {
			t.Fatalf("expanding was not a smooth slide: %v", xs)
		}
	}
	if got := detailsX(); got < start-1 || got > start+1 {
		t.Fatalf("the details did not return to where they were: start=%.0f got=%.0f", start, got)
	}
}
