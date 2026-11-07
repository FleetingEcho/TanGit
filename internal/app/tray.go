package app

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
)

// The tray keeps TanGit reachable after its window is closed: the close
// button hides the window instead of destroying it, and the icon's menu
// brings it back or quits for good.
var (
	appTray    *mygo.Tray
	mainWindow *mygo.Window
	// trayReady is false when the platform has no tray, as on Linux without
	// libayatana-appindicator3. Closing the window then quits as it always
	// did, so the app never becomes unreachable.
	trayReady bool
	// quitting tells a close made by the quit sequence from the user's, so
	// only the user's hides the window.
	quitting bool
)

// setupTray adds the menu bar or notification area icon. It runs once the
// application is ready, after the window exists.
func setupTray() {
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
	// A Dock icon click on macOS reopens a hidden window.
	mygo.App.OnActivate(func(visible bool) {
		if !visible {
			showMainWindow()
		}
	})

	icon, template, ok := trayIcon()
	if !ok {
		return
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID: "tangit",
		// A template icon is black on transparent, so macOS tints it for
		// the menu bar and Windows for its taskbar.
		Icon:           icon,
		IconIsTemplate: template,
		ToolTip:        appName,
		Menu:           trayMenu(),
	})
	if err != nil {
		return
	}
	appTray = tray
	trayReady = true
}

// trayIcon reads the icon that suits the platform: the template on macOS,
// and the colored mark elsewhere, where the panel draws the icon as it is.
func trayIcon() (icon []byte, template, ok bool) {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return nil, false, false
	}
	if runtime.GOOS == "darwin" {
		if data, err := os.ReadFile(filepath.Join(dir, "tray-template.png")); err == nil {
			return data, true, true
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "tray.png"))
	if err != nil {
		return nil, false, false
	}
	return data, false, true
}

// trayMenu is the icon's menu, rebuilt when the language changes.
func trayMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: tr("Open TanGit"), Click: func(*mygo.MenuItem, *mygo.Window) {
			showMainWindow()
		}},
		mygo.Separator(),
		{Label: tr("Quit"), Click: func(*mygo.MenuItem, *mygo.Window) {
			mygo.App.Quit()
		}},
	})
}

// refreshTrayMenu relabels the menu, as the language may have changed.
func refreshTrayMenu() {
	if appTray == nil {
		return
	}
	appTray.SetMenu(trayMenu())
}

// showMainWindow restores the window from the tray: minimized if the user
// minimized it rather than closed it, visible, and focused.
func showMainWindow() {
	if mainWindow == nil || mainWindow.IsDestroyed() {
		return
	}
	if mainWindow.IsMinimized() {
		mainWindow.Restore()
	}
	mainWindow.Show()
	mainWindow.Focus()
}

// hideOnClose turns the window's close into a hide, so the app keeps running
// in the tray. A quit, or a platform without a tray, still closes it.
func hideOnClose(e *mygo.CloseEvent) {
	if !hideOnCloseEnabled() {
		return
	}
	e.PreventDefault()
	hideWindowForTray(e.Window)
}

// trayCloseWindow is the native window behavior the close-to-tray sequence
// needs. Keeping it behind this small interface lets tests drive AppKit's
// asynchronous fullscreen transition without a macOS host.
type trayCloseWindow interface {
	Hide()
	IsFullScreen() bool
	SetFullScreen(bool)
	OnLeaveFullScreen(func()) (off func())
}

// hideWindowForTray hides a normal window immediately. Native fullscreen is a
// separate AppKit space: hiding its window in place strands a black space, so
// leave it first and hide only once AppKit reports the transition finished.
func hideWindowForTray(window trayCloseWindow) {
	if !window.IsFullScreen() {
		window.Hide()
		return
	}
	var off func()
	off = window.OnLeaveFullScreen(func() {
		off()
		window.Hide()
	})
	window.SetFullScreen(false)
}

// hideOnCloseEnabled reports whether a close should hide the window: only
// while a tray exists to bring it back, and never during a quit.
func hideOnCloseEnabled() bool {
	return trayReady && !quitting
}
