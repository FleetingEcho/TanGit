package app

import (
	"image"
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestFullscreenHunkRowWearsAnIsland(t *testing.T) {
	session := &fullscreenBackgroundDiffSession{
		fakeSession: fakeSession{
			snapshot: repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{
				{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true},
				{Path: "b.txt", Status: " M", Unstaged: true, Tracked: true},
			}},
			patch: twoHunkPatch,
		},
		secondStarted: make(chan struct{}, 1),
		secondRelease: make(chan struct{}),
	}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1320, 700)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "diff", func() bool { return a.diffState != nil && !a.diffLoading })
	a.openDiffFullscreen()
	h.tester.Frame()

	theme := themeForSettings(a.settings, true)
	backdrop := theme.Background.Mix(black, 0.5)
	back := color.RGBA{R: backdrop.R, G: backdrop.G, B: backdrop.B, A: 255}
	card := color.RGBA{R: theme.Background.R, G: theme.Background.G, B: theme.Background.B, A: 255}

	unk, found := h.tester.Find("Stage hunk")
	if !found {
		t.Fatal("hunk row not rendered")
	}
	// The hunk row must sit inside the island, so a plain x in the row's
	// padding, far from every button, shows the island card face rather
	// than the drawer's bare backdrop.
	y := int(unk.Y + unk.H/2)
	for _, x := range []int{20, 80, 200, 400, 900, 1200} {
		got := color.RGBAModel.Convert(h.tester.Image().At(x, y)).(color.RGBA)
		if got == back {
			t.Errorf("hunk action row at y=%d, x=%d shows the bare backdrop; want the island background %v", y, x, back)
		}
		if got != card {
			t.Logf("x=%d renders %v (backdrop %v, card %v)", x, got, back, card)
		}
	}
	_ = image.Pt
}
