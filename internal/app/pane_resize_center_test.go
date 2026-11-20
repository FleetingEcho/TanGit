package app

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func glyphInkCenter(tester *ui.Tester, bounds ui.Rect, want ui.Color) (float32, float32, bool) {
	want = want.SRGB()
	minX, minY, maxX, maxY := bounds.X+bounds.W, bounds.Y+bounds.H, float32(-1), float32(-1)
	image := tester.Image()
	count := 0
	for y := max(int(bounds.Y), 0); y < min(int(bounds.Y+bounds.H), image.Bounds().Dy()); y++ {
		for x := max(int(bounds.X), 0); x < min(int(bounds.X+bounds.W), image.Bounds().Dx()); x++ {
			r, g, b, _ := image.At(x, y).RGBA()
			if colorChannelDelta(uint8(r>>8), want.R) <= 16 &&
				colorChannelDelta(uint8(g>>8), want.G) <= 16 &&
				colorChannelDelta(uint8(b>>8), want.B) <= 16 {
				count++
				px, py := float32(x)+0.5, float32(y)+0.5
				if px < minX {
					minX = px
				}
				if px > maxX {
					maxX = px
				}
				if py < minY {
					minY = py
				}
				if py > maxY {
					maxY = py
				}
			}
		}
	}
	if count == 0 {
		return 0, 0, false
	}
	// Dots occupy a full glyph box. Comparing the ink's bounding rectangle is
	// valid for both the horizontal and rotated ellipsis.
	return (minX + maxX) / 2, (minY + maxY) / 2, true
}

func TestPaneSplitCentersEllipsisWithinGrip(t *testing.T) {
	for _, test := range []struct {
		name     string
		vertical bool
	}{
		{name: "top/bottom", vertical: true},
		{name: "left/right", vertical: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApp(fakeOpener{})
			theme := themeForSettings(a.settings, false)
			height := float32(72)
			tester := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme)
				paneSplitWithGrip(c, &height, test.vertical, false,
					func() { ui.Box(c).Fill().Label("Commit message") },
					func() { ui.Box(c).Fill().Label("Changed files") },
				).Fill()
			}, 360, 240)
			tester.Frame()

			var grip ui.Rect
			for _, node := range tester.AccessAll() {
				if node.Name == "Drag to resize" {
					grip = node.Bounds
					break
				}
			}
			if grip.W == 0 || grip.H == 0 {
				t.Fatal("pane grip is missing")
			}
			x, y, found := glyphInkCenter(tester, grip, theme.TextMuted)
			if !found {
				t.Fatalf("no ellipsis pixels inside grip: %+v", grip)
			}
			dx := math.Abs(float64(x - (grip.X + grip.W/2)))
			dy := math.Abs(float64(y - (grip.Y + grip.H/2)))
			if dx > 0.75 || dy > 0.75 {
				t.Fatalf("ellipsis ink is not centered: center=(%.2f,%.2f) grip=%+v dx=%.2f dy=%.2f", x, y, grip, dx, dy)
			}
		})
	}
}
