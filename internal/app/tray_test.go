package app

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo"
)

// The tray menu is the only way back once the window is hidden, so it always
// offers opening the window and quitting for good.
func TestTrayMenuOffersOpenAndQuit(t *testing.T) {
	items := trayMenu().Items()
	if len(items) != 3 {
		t.Fatalf("the tray menu has %d items, want 3", len(items))
	}
	if items[0].Label != "Open TanGit" {
		t.Errorf("the first item is %q, want Open TanGit", items[0].Label)
	}
	if items[1].Type != mygo.MenuItemSeparator {
		t.Errorf("the second item is %q, want a separator", items[1].Type)
	}
	if items[2].Label != "Quit" {
		t.Errorf("the last item is %q, want Quit", items[2].Label)
	}
}

// The tray reads both icons from the resources directory the build ships, so
// a missing file must fail here rather than leave the tray without an image.
func TestTrayIconsAreShipped(t *testing.T) {
	for _, name := range []string{"../../resources/tray.png", "../../resources/tray-template.png"} {
		f, err := os.Open(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
			t.Errorf("%s is %dx%d, want 64x64", name, b.Dx(), b.Dy())
		}
	}
}

// The menu follows the interface language, like the window's own controls.
func TestTrayMenuFollowsTheLanguage(t *testing.T) {
	before := currentLanguage
	defer func() { currentLanguage = before }()
	currentLanguage = "zh-Hans"

	items := trayMenu().Items()
	if items[0].Label != "打开 TanGit" || items[2].Label != "退出" {
		t.Fatalf("the menu stayed English: %q, %q", items[0].Label, items[2].Label)
	}
}

// Closing hides only while a tray exists to reopen the window, and never
// while the app is quitting: otherwise the quit would leave it hidden.
func TestCloseHidesOnlyWithATrayAndOutsideAQuit(t *testing.T) {
	ready, wasQuitting := trayReady, quitting
	defer func() { trayReady, quitting = ready, wasQuitting }()

	trayReady, quitting = false, false
	if hideOnCloseEnabled() {
		t.Error("a close hides without a tray, so the window could not return")
	}

	trayReady = true
	if !hideOnCloseEnabled() {
		t.Error("a close does not hide while the tray is ready")
	}

	quitting = true
	if hideOnCloseEnabled() {
		t.Error("a quit hid the window instead of closing it")
	}
}

// trayCloseTestWindow records the native calls and lets a test finish the
// asynchronous fullscreen exit after checking that nothing hid it too early.
type trayCloseTestWindow struct {
	full      bool
	exitCalls int
	hides     int
	offs      int
	onLeave   []func()
}

func (w *trayCloseTestWindow) Hide()              { w.hides++ }
func (w *trayCloseTestWindow) IsFullScreen() bool { return w.full }
func (w *trayCloseTestWindow) SetFullScreen(full bool) {
	if !full {
		w.exitCalls++
	}
}
func (w *trayCloseTestWindow) OnLeaveFullScreen(fn func()) func() {
	w.onLeave = append(w.onLeave, fn)
	return func() { w.offs++ }
}
func (w *trayCloseTestWindow) leaveFullScreen() {
	w.full = false
	for _, fn := range w.onLeave {
		fn()
	}
}

// Native fullscreen must be left before orderOut hides a window to the tray:
// doing both at once leaves AppKit's fullscreen space showing a black screen.
func TestCloseToTrayLeavesFullScreenBeforeHiding(t *testing.T) {
	normal := &trayCloseTestWindow{}
	hideWindowForTray(normal)
	if normal.hides != 1 || normal.exitCalls != 0 {
		t.Fatalf("an ordinary close did %+v, want one immediate hide and no fullscreen exit", normal)
	}

	full := &trayCloseTestWindow{full: true}
	hideWindowForTray(full)
	if full.hides != 0 {
		t.Fatal("the fullscreen window hid before leaving its AppKit fullscreen space")
	}
	if full.exitCalls != 1 || len(full.onLeave) != 1 {
		t.Fatalf("the fullscreen close did not request one observed exit: %+v", full)
	}
	full.leaveFullScreen()
	if full.hides != 1 || full.offs != 1 {
		t.Fatalf("leaving fullscreen did not finish one close-to-tray hide: %+v", full)
	}
}
