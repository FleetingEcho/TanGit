package ui_test

import (
	"fmt"
	"math"
	"testing"
)

// A placeholder can use a different font and baseline from an empty content
// layout. Compare real colored glyph bounds across theme/raster scales rather
// than assuming every font has the same ascent as ASCII text.
func TestPlaceholderInkIsCenteredVertically(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, scale := range []float32{1, 2, 3} {
			t.Run(fmt.Sprintf("dark=%v/scale=%v", dark, scale), func(t *testing.T) {
				state := &placeholderColorState{
					hint: "搜索提交哈希、提交说明或作者",
					tint: placeholderColorTheme().TextMuted,
				}
				tt := newPlaceholderColorTester(state)
				tt.SetDark(dark)
				tt.SetScale(scale)
				tt.Frame()
				input := placeholderColorBounds(t, tt, "target input")
				expectedCenter := (input.Y + input.H/2) * scale
				minY, maxY := (input.Y+input.H)*scale, input.Y*scale
				image, tint, ink := tt.Image(), placeholderColorTheme().TextMuted, 0
				different := func(a, b uint8) bool { return math.Abs(float64(int(a)-int(b))) > 12 }
				for y := int(input.Y * scale); y < int((input.Y+input.H)*scale); y++ {
					for x := int(input.X * scale); x < int((input.X+input.W)*scale); x++ {
						pixel := image.RGBAAt(x, y)
						if different(pixel.R, tint.R) || different(pixel.G, tint.G) || different(pixel.B, tint.B) || pixel.A == 0 {
							continue
						}
						ink++
						minY = min(float32(y), minY)
						maxY = max(float32(y), maxY)
					}
				}
				if ink == 0 {
					t.Fatal("no rendered placeholder ink to measure")
				}
				gotCenter := (minY + maxY + 1) / 2
				if difference := float64(gotCenter - expectedCenter); math.Abs(difference) > 1.5 {
					t.Fatalf("placeholder ink center = %.2f, input center = %.2f (offset %.2f px; ink Y %.1f..%.1f, box Y %.1f..%.1f)",
						gotCenter, expectedCenter, gotCenter-expectedCenter, minY, maxY, input.Y*scale, (input.Y+input.H)*scale)
				}
			})
		}
	}
}
