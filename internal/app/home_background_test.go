package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func writeHomeBackgroundPNG(t *testing.T, path string) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 640, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 640; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(35 + x*130/640), G: uint8(65 + y*120/360),
				B: uint8(150 + x*80/640), A: 255,
			})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func backgroundSettingsTester(t *testing.T) (*app, *ui.Tester, *updates) {
	t.Helper()
	a := newApp(fakeOpener{})
	a.pathUserData = t.TempDir()
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	a.openSettings()
	tt.Frame()
	return a, tt, updated
}

func TestHomeBackgroundPickerErrorIsNotATranslationKey(t *testing.T) {
	const payload = "native chooser supplied an arbitrary error"
	stubScopedTranslationCatalog(t, map[string]string{
		"Could not set Home background": "Translated background failure",
		payload:                         "must not be used",
	})
	originalChoose := chooseHomeBackgroundPath
	chooseHomeBackgroundPath = func(mygo.OpenDialogOptions) ([]string, error) {
		return nil, errors.New(payload)
	}
	t.Cleanup(func() { chooseHomeBackgroundPath = originalChoose })
	a := newApp(fakeOpener{})
	updates := newUpdates()
	a.update = updates.update
	a.browseHomeBackground()
	updates.waitForUpdate(t)
	if len(a.pendingToasts) != 1 {
		t.Fatalf("picker error notifications = %#v", a.pendingToasts)
	}
	toast := a.pendingToasts[0]
	if toast.title != "Translated background failure" || toast.description != payload {
		t.Fatalf("picker error notification = %#v", toast)
	}
}

func TestHomeBackgroundErrorDescriptionTranslatesOnlyKnownValidation(t *testing.T) {
	const payload = "filesystem or decoder supplied an arbitrary error"
	known := []string{
		"Choose an image no larger than 10 MiB",
		"Choose a valid PNG, JPEG, GIF, WebP, or BMP image",
		"Choose an image with at most 40 million pixels",
	}
	catalog := map[string]string{payload: "must not be used"}
	for _, message := range known {
		catalog[message] = "Translated " + message
	}
	stubScopedTranslationCatalog(t, catalog)
	for _, message := range known {
		if got, want := homeBackgroundErrorDescription(errors.New(message)), "Translated "+message; got != want {
			t.Errorf("known validation description = %q, want %q", got, want)
		}
	}
	if got := homeBackgroundErrorDescription(errors.New(payload)); got != payload {
		t.Fatalf("external error description = %q, want raw payload", got)
	}
}

func TestHomeBackgroundUploadPreviewsAndPersists(t *testing.T) {
	a, tt, updated := backgroundSettingsTester(t)
	path := filepath.Join(t.TempDir(), "home.png")
	writeHomeBackgroundPNG(t, path)
	restore := chooseHomeBackgroundPath
	var opts mygo.OpenDialogOptions
	chooseHomeBackgroundPath = func(got mygo.OpenDialogOptions) ([]string, error) {
		opts = got
		return []string{path}, nil
	}
	t.Cleanup(func() { chooseHomeBackgroundPath = restore })

	if err := tt.Click("Choose image…"); err != nil {
		t.Fatalf("choose a background: %v", err)
	}
	updated.waitForUpdate(t)
	tt.Frame()
	if a.settings.HomeBackgroundPath != path || a.homeBackgroundPath != path || a.homeBackground == nil {
		t.Fatalf("the picked image was not installed: setting=%q cache=%q bitmap=%v",
			a.settings.HomeBackgroundPath, a.homeBackgroundPath, a.homeBackground != nil)
	}
	if got := loadAppSettings(a.pathUserData).HomeBackgroundPath; got != path {
		t.Fatalf("the stored background is %q, want %q", got, path)
	}
	if _, ok := tt.Find("Home background preview"); !ok {
		t.Fatalf("the selected image has no preview: %q", tt.Texts())
	}
	if !slices.Contains(tt.Texts(), "Home background updated") {
		t.Fatalf("the upload showed no success notification: %q", tt.Texts())
	}
	if opts.Title != "Choose a Home background" || opts.Directory || opts.Multiple {
		t.Fatalf("the native dialog options are wrong: %+v", opts)
	}
	if len(opts.Filters) != 1 || !slices.Equal(opts.Filters[0].Extensions,
		[]string{"png", "jpg", "jpeg", "gif", "webp", "bmp"}) {
		t.Fatalf("the native dialog filters are wrong: %+v", opts.Filters)
	}

	// Close Settings and verify the same cached bitmap is visible on Home,
	// while the ordinary repository controls remain interactive.
	a.closeSettings()
	tt.Frame()
	if _, ok := tt.Find("Home background"); !ok {
		t.Fatalf("Home does not render the selected image: %q", tt.Texts())
	}
	if err := tt.Click("Repository folder"); err != nil {
		t.Fatalf("the background intercepts the repository controls: %v", err)
	}
}

func TestHomeBackgroundOpacityControlPersistsAndChangesRendering(t *testing.T) {
	a, tt, _ := backgroundSettingsTester(t)
	path := filepath.Join(t.TempDir(), "home.png")
	writeHomeBackgroundPNG(t, path)
	bitmap, err := readHomeBackground(path)
	if err != nil {
		t.Fatal(err)
	}
	a.settings.HomeBackgroundPath = path
	a.settings.HomeBackgroundOpacity = 0.1
	a.homeBackgroundPath = path
	a.homeBackground = bitmap
	a.closeSettings()
	tt.Frame()
	before := append([]byte(nil), tt.Image().Pix...)

	a.settings.HomeBackgroundOpacity = 0.9
	tt.Frame()
	if bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("changing opacity did not change the rendered Home image")
	}

	a.openSettings()
	tt.Frame()
	if !tt.HasText("Background opacity") {
		t.Fatalf("Settings does not offer opacity: %q", tt.Texts())
	}
	slider, ok := tt.Find("Home image opacity")
	if !ok {
		t.Fatal("opacity slider has no accessible label")
	}
	tt.ClickAt(slider.X+slider.W*0.75, slider.Y+slider.H/2)
	tt.Frame()
	tt.Frame()
	if got := a.settings.HomeBackgroundOpacity; got < 0.7 || got > 0.8 {
		t.Fatalf("slider set opacity to %v, want about 0.75", got)
	}
	if got := loadAppSettings(a.pathUserData).HomeBackgroundOpacity; got != a.settings.HomeBackgroundOpacity {
		t.Fatalf("stored opacity = %v, running opacity = %v", got, a.settings.HomeBackgroundOpacity)
	}
}

func TestHomeBackgroundCanBeDisabledWithoutRemovingImage(t *testing.T) {
	a, tt, _ := backgroundSettingsTester(t)
	path := filepath.Join(t.TempDir(), "home.png")
	writeHomeBackgroundPNG(t, path)
	bitmap, err := readHomeBackground(path)
	if err != nil {
		t.Fatal(err)
	}
	a.settings.HomeBackgroundPath = path
	a.settings.HomeBackgroundOpacity = 0.37
	a.homeBackgroundPath = path
	a.homeBackground = bitmap
	tt.Frame()

	if err := tt.Click("Enable Home background"); err != nil {
		t.Fatalf("disable the background: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if !a.settings.HomeBackgroundDisabled {
		t.Fatal("the background toggle did not disable the image")
	}
	if a.settings.HomeBackgroundPath != path || a.settings.HomeBackgroundOpacity != 0.37 ||
		a.homeBackgroundPath != path || a.homeBackground != bitmap {
		t.Fatal("disabling the image discarded its path, opacity, or decoded bitmap")
	}
	stored := loadAppSettings(a.pathUserData)
	if !stored.HomeBackgroundDisabled || stored.HomeBackgroundPath != path || stored.HomeBackgroundOpacity != 0.37 {
		t.Fatalf("the disabled image was not stored intact: %+v", stored)
	}
	if _, ok := tt.Find("Home background preview"); !ok {
		t.Fatal("disabling the image removed its Settings preview")
	}
	a.closeSettings()
	tt.Frame()
	if _, ok := tt.Find("Home background"); ok {
		t.Fatal("Home still renders a disabled background")
	}

	a.openSettings()
	tt.Frame()
	if err := tt.Click("Enable Home background"); err != nil {
		t.Fatalf("re-enable the background: %v", err)
	}
	tt.Frame()
	tt.Frame()
	a.closeSettings()
	tt.Frame()
	if a.settings.HomeBackgroundDisabled || loadAppSettings(a.pathUserData).HomeBackgroundDisabled {
		t.Fatal("the background did not stay re-enabled")
	}
	if _, ok := tt.Find("Home background"); !ok {
		t.Fatal("re-enabling did not restore the retained Home image")
	}
	if a.homeBackground != bitmap || a.settings.HomeBackgroundOpacity != 0.37 {
		t.Fatal("re-enabling did not restore the same bitmap and opacity")
	}
}

func TestHomeBackgroundRejectsDisguisedNonImageAndKeepsPrevious(t *testing.T) {
	a, tt, updated := backgroundSettingsTester(t)
	oldPath := filepath.Join(t.TempDir(), "old.png")
	writeHomeBackgroundPNG(t, oldPath)
	oldBitmap, err := readHomeBackground(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	a.settings.HomeBackgroundPath = oldPath
	a.homeBackgroundPath = oldPath
	a.homeBackground = oldBitmap
	invalid := filepath.Join(t.TempDir(), "not-really.png")
	if err := os.WriteFile(invalid, []byte("this is text with an image extension"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := chooseHomeBackgroundPath
	chooseHomeBackgroundPath = func(mygo.OpenDialogOptions) ([]string, error) {
		return []string{invalid}, nil
	}
	t.Cleanup(func() { chooseHomeBackgroundPath = restore })
	tt.Frame()

	if err := tt.Click("Choose image…"); err != nil {
		t.Fatal(err)
	}
	updated.waitForUpdate(t)
	tt.Frame()
	if a.settings.HomeBackgroundPath != oldPath || a.homeBackgroundPath != oldPath || a.homeBackground != oldBitmap {
		t.Fatal("an invalid replacement discarded the previous valid background")
	}
	if !slices.Contains(tt.Texts(), "Could not set Home background") ||
		!slices.Contains(tt.Texts(), "Choose a valid PNG, JPEG, GIF, WebP, or BMP image") {
		t.Fatalf("the invalid image did not explain the failure: %q", tt.Texts())
	}
}

func TestReadHomeBackgroundEnforcesTheExactSizeLimit(t *testing.T) {
	dir := t.TempDir()
	exact := filepath.Join(dir, "exact.png")
	data := writeHomeBackgroundPNG(t, exact)
	if int64(len(data)) > maxHomeBackgroundSize {
		t.Fatal("the PNG fixture is unexpectedly too large")
	}
	exactData := make([]byte, maxHomeBackgroundSize)
	copy(exactData, data)
	if err := os.WriteFile(exact, exactData, 0o644); err != nil {
		t.Fatal(err)
	}
	if bitmap, err := readHomeBackground(exact); err != nil || bitmap == nil {
		t.Fatalf("an exactly-10-MiB valid image was rejected: bitmap=%v err=%v", bitmap != nil, err)
	}

	oversize := filepath.Join(dir, "oversize.png")
	if err := os.WriteFile(oversize, make([]byte, maxHomeBackgroundSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := decodeHomeBackground
	decodes := 0
	decodeHomeBackground = func(data []byte) (*ui.Bitmap, error) {
		decodes++
		return decodeHomeBackgroundForTest(data)
	}
	t.Cleanup(func() { decodeHomeBackground = restore })
	if _, err := readHomeBackground(oversize); err == nil || err.Error() != "Choose an image no larger than 10 MiB" {
		t.Fatalf("an oversized file returned the wrong error: %v", err)
	}
	if decodes != 0 {
		t.Fatalf("the oversized file reached the decoder %d times", decodes)
	}
}

func TestReadHomeBackgroundRejectsHugeDimensionsBeforeDecoding(t *testing.T) {
	// Only a PNG header is needed: DecodeConfig reads the dimensions without
	// allocating the 50-million-pixel image the header describes.
	data := make([]byte, 33)
	copy(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], 10_000)
	binary.BigEndian.PutUint32(data[20:24], 5_000)
	data[24] = 8 // bit depth
	data[25] = 6 // RGBA
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	path := filepath.Join(t.TempDir(), "huge.png")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	restore := decodeHomeBackground
	decodes := 0
	decodeHomeBackground = func(data []byte) (*ui.Bitmap, error) {
		decodes++
		return ui.DecodeBitmap(data)
	}
	t.Cleanup(func() { decodeHomeBackground = restore })
	if _, err := readHomeBackground(path); err == nil || err.Error() != "Choose an image with at most 40 million pixels" {
		t.Fatalf("a decompression bomb returned the wrong error: %v", err)
	}
	if decodes != 0 {
		t.Fatalf("the oversized image reached the pixel decoder %d times", decodes)
	}
}

// decodeHomeBackgroundForTest keeps a stub's delegate outside the mutable seam.
func decodeHomeBackgroundForTest(data []byte) (*ui.Bitmap, error) {
	return ui.DecodeBitmap(data)
}

func TestReadHomeBackgroundReportsMissingFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.png")
	if bitmap, err := readHomeBackground(path); err == nil || bitmap != nil || !os.IsNotExist(err) {
		t.Fatalf("a missing background returned bitmap=%v err=%v", bitmap != nil, err)
	}
}

func TestRemoveHomeBackgroundClearsPreviewRenderingAndStore(t *testing.T) {
	a, tt, _ := backgroundSettingsTester(t)
	path := filepath.Join(t.TempDir(), "home.png")
	writeHomeBackgroundPNG(t, path)
	bitmap, err := readHomeBackground(path)
	if err != nil {
		t.Fatal(err)
	}
	a.settings.HomeBackgroundPath = path
	a.homeBackgroundPath = path
	a.homeBackground = bitmap
	tt.Frame()
	if err := tt.Click("Remove"); err != nil {
		t.Fatalf("remove the background: %v", err)
	}
	tt.Frame()
	if a.settings.HomeBackgroundPath != "" || a.homeBackground != nil || a.homeBackgroundPath != "" {
		t.Fatal("removal left background state behind")
	}
	if got := loadAppSettings(a.pathUserData).HomeBackgroundPath; got != "" {
		t.Fatalf("removal left the stored path %q", got)
	}
	if _, ok := tt.Find("Home background preview"); ok {
		t.Fatal("removal left the Settings preview visible")
	}
	a.closeSettings()
	tt.Frame()
	if _, ok := tt.Find("Home background"); ok {
		t.Fatal("removal left the Home image visible")
	}
}

func TestResetSettingsRemovesHomeBackground(t *testing.T) {
	a, tt, _ := backgroundSettingsTester(t)
	path := filepath.Join(t.TempDir(), "home.png")
	writeHomeBackgroundPNG(t, path)
	bitmap, err := readHomeBackground(path)
	if err != nil {
		t.Fatal(err)
	}
	a.settings.HomeBackgroundPath = path
	a.homeBackgroundPath = path
	a.homeBackground = bitmap
	tt.Frame()
	if err := tt.Click("Reset to defaults"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.HomeBackgroundPath != "" || a.homeBackground != nil {
		t.Fatal("resetting Settings left the Home background behind")
	}
	if got := loadAppSettings(a.pathUserData).HomeBackgroundPath; got != "" {
		t.Fatalf("reset left the stored background %q", got)
	}
}

func TestHomeBackgroundLoadsOnceAndRemovalRejectsStaleRead(t *testing.T) {
	a := newApp(fakeOpener{})
	updated := newUpdates()
	a.update = updated.update
	path := filepath.Join(t.TempDir(), "home.png")
	writeHomeBackgroundPNG(t, path)
	a.settings.HomeBackgroundPath = path

	restore := decodeHomeBackground
	var mu sync.Mutex
	decodes := 0
	started := make(chan struct{})
	release := make(chan struct{})
	decodeHomeBackground = func(data []byte) (*ui.Bitmap, error) {
		mu.Lock()
		decodes++
		mu.Unlock()
		close(started)
		<-release
		return ui.DecodeBitmap(data)
	}
	t.Cleanup(func() { decodeHomeBackground = restore })
	a.syncHomeBackground()
	a.syncHomeBackground()
	<-started
	a.removeHomeBackground()
	close(release)
	updated.waitForUpdate(t)
	a.syncHomeBackground()
	mu.Lock()
	got := decodes
	mu.Unlock()
	if got != 1 {
		t.Fatalf("repeated frames decoded the same file %d times", got)
	}
	if a.homeBackground != nil || a.homeBackgroundPath != "" {
		t.Fatal("a stale read restored the removed background")
	}

	// Restoring the setting after all stale work has finished loads it once.
	a.settings.HomeBackgroundPath = path
	decodeHomeBackground = ui.DecodeBitmap
	a.syncHomeBackground()
	updated.waitForUpdate(t)
	a.syncHomeBackground()
	if a.homeBackground == nil || a.homeBackgroundPath != path {
		t.Fatal("the current background did not load after the stale result was ignored")
	}
}

func TestShutdownRejectsAndRetriesHomeBackgroundLoad(t *testing.T) {
	a := newApp(fakeOpener{})
	updated := newUpdates()
	a.update = updated.update
	path := filepath.Join(t.TempDir(), "home.png")
	writeHomeBackgroundPNG(t, path)
	a.settings.HomeBackgroundPath = path

	restore := decodeHomeBackground
	var mu sync.Mutex
	decodes := 0
	started := make(chan struct{})
	release := make(chan struct{})
	decodeHomeBackground = func(data []byte) (*ui.Bitmap, error) {
		mu.Lock()
		decodes++
		call := decodes
		mu.Unlock()
		if call == 1 {
			close(started)
			<-release
		}
		return ui.DecodeBitmap(data)
	}
	t.Cleanup(func() { decodeHomeBackground = restore })
	a.syncHomeBackground()
	<-started
	a.shutdown()
	close(release)
	updated.waitForUpdate(t)
	if a.homeBackground != nil || a.homeBackgroundPath != "" || len(a.pendingToasts) != 0 {
		t.Fatal("a background load installed its result after shutdown")
	}

	// Closing a repository returns to Home in a standalone app, so the empty
	// pending marker must allow that still-live page to load the image again.
	a.syncHomeBackground()
	updated.waitForUpdate(t)
	if a.homeBackground == nil || a.homeBackgroundPath != path {
		t.Fatal("Home could not retry its background load after shutdown")
	}
}

func TestShutdownRejectsAHomeBackgroundSelection(t *testing.T) {
	a := newApp(fakeOpener{})
	a.pathUserData = t.TempDir()
	updated := newUpdates()
	a.update = updated.update
	oldPath := filepath.Join(t.TempDir(), "old.png")
	newPath := filepath.Join(t.TempDir(), "new.png")
	writeHomeBackgroundPNG(t, oldPath)
	writeHomeBackgroundPNG(t, newPath)
	a.settings.HomeBackgroundPath = oldPath
	a.saveSettings()

	restoreChoice := chooseHomeBackgroundPath
	chooseHomeBackgroundPath = func(mygo.OpenDialogOptions) ([]string, error) {
		return []string{newPath}, nil
	}
	t.Cleanup(func() { chooseHomeBackgroundPath = restoreChoice })
	restoreDecode := decodeHomeBackground
	started := make(chan struct{})
	release := make(chan struct{})
	decodeHomeBackground = func(data []byte) (*ui.Bitmap, error) {
		close(started)
		<-release
		return ui.DecodeBitmap(data)
	}
	t.Cleanup(func() { decodeHomeBackground = restoreDecode })

	a.browseHomeBackground()
	<-started
	a.shutdown()
	close(release)
	updated.waitForUpdate(t)
	if a.settings.HomeBackgroundPath != oldPath || a.homeBackgroundPath == newPath {
		t.Fatal("a completed file selection changed the background after shutdown")
	}
	if len(a.pendingToasts) != 0 {
		t.Fatal("a closed page announced its stale background selection")
	}
	if got := loadAppSettings(a.pathUserData).HomeBackgroundPath; got != oldPath {
		t.Fatalf("a stale selection replaced the stored background with %q", got)
	}
}

func TestResetSettingsRejectsAnInflightHomeBackgroundSelection(t *testing.T) {
	a, tt, updated := backgroundSettingsTester(t)
	path := filepath.Join(t.TempDir(), "new.png")
	writeHomeBackgroundPNG(t, path)
	restoreChoice := chooseHomeBackgroundPath
	chooseHomeBackgroundPath = func(mygo.OpenDialogOptions) ([]string, error) {
		return []string{path}, nil
	}
	t.Cleanup(func() { chooseHomeBackgroundPath = restoreChoice })
	restoreDecode := decodeHomeBackground
	started := make(chan struct{})
	release := make(chan struct{})
	decodeHomeBackground = func(data []byte) (*ui.Bitmap, error) {
		close(started)
		<-release
		return ui.DecodeBitmap(data)
	}
	t.Cleanup(func() { decodeHomeBackground = restoreDecode })

	a.browseHomeBackground()
	<-started
	if err := tt.Click("Reset to defaults"); err != nil {
		t.Fatal(err)
	}
	close(release)
	updated.waitForUpdate(t)
	tt.Frame()
	if a.settings.HomeBackgroundPath != "" || a.homeBackground != nil || a.homeBackgroundPath != "" {
		t.Fatal("an in-flight selection restored a background after Settings was reset")
	}
	if got := loadAppSettings(a.pathUserData).HomeBackgroundPath; got != "" {
		t.Fatalf("an in-flight selection restored the stored background %q", got)
	}
}

func TestHomeBackgroundValidationDescriptionIsTranslated(t *testing.T) {
	previousLanguage := currentLanguage
	currentLanguage = "zh-Hans"
	t.Cleanup(func() { currentLanguage = previousLanguage })
	a, tt, updated := backgroundSettingsTester(t)
	a.settings.Language = "zh-Hans"
	currentLanguage = "zh-Hans"
	tt.Frame()
	invalid := filepath.Join(t.TempDir(), "invalid.png")
	if err := os.WriteFile(invalid, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := chooseHomeBackgroundPath
	chooseHomeBackgroundPath = func(mygo.OpenDialogOptions) ([]string, error) {
		return []string{invalid}, nil
	}
	t.Cleanup(func() { chooseHomeBackgroundPath = restore })

	if err := tt.Click("选择图片…"); err != nil {
		t.Fatalf("choose an invalid background: %v", err)
	}
	updated.waitForUpdate(t)
	tt.Frame()
	for _, want := range []string{"无法设置主页背景", "请选择有效的 PNG、JPEG、GIF、WebP 或 BMP 图片"} {
		if !slices.Contains(tt.Texts(), want) {
			t.Fatalf("the translated validation message does not show %q: %q", want, tt.Texts())
		}
	}
}
