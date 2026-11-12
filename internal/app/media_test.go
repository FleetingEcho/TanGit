package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

const testBinaryImagePatch = "diff --git a/picture.png b/picture.png\n" +
	"index 1111111..2222222 100644\n" +
	"Binary files a/picture.png and b/picture.png differ\n"

func testPNG(t *testing.T, width, height int, fill color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestBinaryImageChangeShowsBeforeAfterAndFullscreen(t *testing.T) {
	oldData := testPNG(t, 2, 3, color.RGBA{R: 255, A: 255})
	newData := testPNG(t, 4, 5, color.RGBA{B: 255, A: 255})
	a := newApp(fakeOpener{
		result: openResult{snapshot: repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{{
			Path: "picture.png", Status: " M", Tracked: true, Unstaged: true,
		}}}},
		patch: testBinaryImagePatch,
		mediaPair: repository.MediaPair{
			Old: repository.MediaBlob{Present: true, Data: oldData},
			New: repository.MediaBlob{Present: true, Data: newData},
		},
	})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1100, 760)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the old and new image previews", func() bool { return a.diffMedia != nil && !a.diffLoading })
	if a.diffState != nil || a.diffError != "" {
		t.Fatalf("binary preview state = diff %v, error %q", a.diffState != nil, a.diffError)
	}
	for _, want := range []string{"Before", "After", "2 × 3", "4 × 5"} {
		if !h.tester.HasText(want) {
			t.Errorf("media review has no %q: %q", want, h.texts())
		}
	}
	if err := h.tester.Click("Expand diff"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !a.diffFullscreen || !h.tester.HasText("Before") || !h.tester.HasText("After") {
		t.Fatal("fullscreen did not retain the media comparison")
	}
}

func TestMediaReviewExplainsAbsentAndLocalLFSSides(t *testing.T) {
	oldPreview := decodeMediaPreview(repository.MediaBlob{})
	if oldPreview.present {
		t.Fatal("an absent addition side became an empty image")
	}
	newPreview := decodeMediaPreview(repository.MediaBlob{Present: true, Unavailable: "Git LFS object is not available locally"})
	if newPreview.unavailable == "" || newPreview.bitmap != nil {
		t.Fatalf("missing LFS preview = %#v", newPreview)
	}
	media := &mediaReview{old: oldPreview, new: newPreview}
	tester := ui.NewTester(func(c *ui.Context) {
		mediaReviewView(c, media, "added.png")
	}, 900, 500)
	tester.Frame()
	for _, want := range []string{"No file on this side", "Preview unavailable", "Git LFS object is not available locally"} {
		if !tester.HasText(want) {
			t.Errorf("missing/LFS explanation has no %q", want)
		}
	}
}

func TestSameMediaReviewComparesContentNotDecodedBitmapIdentity(t *testing.T) {
	data := testPNG(t, 2, 3, color.RGBA{R: 255, A: 255})
	first := &mediaReview{old: decodeMediaData(data), new: decodeMediaPreview(repository.MediaBlob{})}
	second := &mediaReview{old: decodeMediaData(append([]byte(nil), data...)), new: decodeMediaPreview(repository.MediaBlob{})}
	if !sameMediaReview(first, second) {
		t.Fatal("identical encoded images decoded into different bitmaps compared as changed")
	}
	second.old = decodeMediaData(testPNG(t, 2, 3, color.RGBA{B: 255, A: 255}))
	if sameMediaReview(first, second) {
		t.Fatal("different image content compared as unchanged")
	}
	if !sameMediaReview(nil, nil) || sameMediaReview(first, nil) {
		t.Fatal("nil media reviews did not compare correctly")
	}
}

func TestMediaDecodeRejectsInvalidAndOversizedImages(t *testing.T) {
	invalid := decodeMediaPreview(repository.MediaBlob{Present: true, Data: []byte("not an image")})
	if invalid.problem == "" || invalid.bitmap != nil {
		t.Fatalf("invalid media preview = %#v", invalid)
	}
	// The encoded-size bound is enforced before this decoder by repository.Media.
	// Keep the decoder's independent pixel bound covered without allocating a
	// hostile image by declaring enormous dimensions in an otherwise tiny GIF.
	gifHeader := []byte("GIF89a\xff\xff\xff\xff\x00\x00\x00")
	oversized := decodeMediaPreview(repository.MediaBlob{Present: true, Data: gifHeader})
	if oversized.problem == "" {
		t.Fatal("oversized image reached the bitmap decoder")
	}
}
