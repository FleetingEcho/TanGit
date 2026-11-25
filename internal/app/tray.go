package app

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo"

	"github.com/FleetingEcho/lazygitGo/internal/agentwork"
)

// The tray keeps lazygitGo reachable after its window is closed: the close
// button hides the window instead of destroying it, and the icon's menu
// brings it back or quits for good.
var (
	appTray    *mygo.Tray
	mainWindow *mygo.Window
	// appShell is the window's shell, which the tray reads for its status,
	// recent repositories and the page Settings opens on.
	appShell *shell
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
		ID: "lazygitgo",
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

// trayRecentLimit is how many recent repositories the tray lists.
const trayRecentLimit = 8

// trayRecent is one recent repository offered by the tray.
type trayRecent struct{ path, label string }

// trayState is what the tray menu shows of the running app. The menu is
// rebuilt only when it changes, so a frame that changes nothing never touches
// the native menu.
type trayState struct {
	status    string
	attention int
	recent    []trayRecent
}

func (t trayState) signature() string {
	var b strings.Builder
	b.WriteString(currentLanguage + "|" + t.status + "|" + strconv.Itoa(t.attention))
	for _, r := range t.recent {
		b.WriteString("|" + r.path + "=" + r.label)
	}
	return b.String()
}

// trayState reads the status line, the number of agent tasks waiting for the
// user and the recent repositories. It runs on the main thread, where the
// pages' state lives.
func (s *shell) trayState() trayState {
	var state trayState
	if t := s.activeTab(); t != nil && t.repository() && t.app != nil {
		snapshot := t.app.snapshot
		parts := []string{t.title()}
		if snapshot.Branch != "" {
			parts = append(parts, snapshot.Branch)
		}
		if n := len(snapshot.Files); n > 0 {
			parts = append(parts, trf("%d changed", n))
		}
		state.status = strings.Join(parts, " · ")
	}
	var recents []recentRepository
	for _, t := range s.tabs {
		if t.app == nil {
			continue
		}
		for _, task := range t.app.agents.tasks {
			switch task.Status {
			case agentwork.StatusNeedsInput, agentwork.StatusReadyForReview, agentwork.StatusFailed:
				state.attention++
			}
		}
		if len(recents) == 0 {
			recents = t.app.recentRepositories
		}
	}
	recents = slices.Clone(recents)
	sort.SliceStable(recents, func(i, j int) bool { return recents[i].LastOpened > recents[j].LastOpened })
	for _, r := range recents {
		if len(state.recent) == trayRecentLimit {
			break
		}
		state.recent = append(state.recent, trayRecent{path: r.Path, label: r.displayName()})
	}
	return state
}

// syncTrayMenu rebuilds the icon's menu when what it shows has changed (or
// when force is set, as for a new language).
func (s *shell) syncTrayMenu(force bool) {
	if appTray == nil {
		return
	}
	signature := s.trayState().signature()
	if !force && signature == s.traySignature {
		return
	}
	s.traySignature = signature
	appTray.SetMenu(trayMenu())
}

// trayMenu is the icon's menu: what the app is doing, a way into its recent
// repositories, Settings and updates, and quitting. Before the window exists
// it is only Open and Quit.
func trayMenu() *mygo.Menu {
	open := &mygo.MenuItem{Label: tr("Open lazygitGo"), Click: func(*mygo.MenuItem, *mygo.Window) {
		showMainWindow()
	}}
	quit := &mygo.MenuItem{Label: tr("Quit"), Click: func(*mygo.MenuItem, *mygo.Window) {
		mygo.App.Quit()
	}}
	s := appShell
	if s == nil {
		return mygo.NewMenu([]*mygo.MenuItem{open, mygo.Separator(), quit})
	}
	state := s.trayState()
	var items []*mygo.MenuItem
	if state.status != "" {
		items = append(items, &mygo.MenuItem{Label: state.status, Disabled: true})
	}
	if state.attention > 0 {
		items = append(items, &mygo.MenuItem{Label: trf("%d agent tasks need attention", state.attention), Disabled: true})
	}
	if len(items) > 0 {
		items = append(items, mygo.Separator())
	}
	items = append(items, open)
	if len(state.recent) > 0 {
		recent := make([]*mygo.MenuItem, 0, len(state.recent))
		for _, r := range state.recent {
			r := r
			recent = append(recent, &mygo.MenuItem{Label: r.label, ToolTip: r.path, Click: func(*mygo.MenuItem, *mygo.Window) {
				s.request(func() { s.openTabWithPolicy(r.path, openAllowPlainEditor) })
				showMainWindow()
			}})
		}
		items = append(items, &mygo.MenuItem{Label: tr("Recent repositories"), Submenu: recent})
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: tr("Settings…"), Click: func(*mygo.MenuItem, *mygo.Window) {
			// Show the window first, then open the dialog on the page now in
			// front, so Settings appears over what the user returns to.
			showMainWindow()
			s.request(func() {
				if a := s.activeApp(); a != nil {
					a.openSettings()
				}
			})
		}},
		&mygo.MenuItem{Label: tr("Check for updates…"), Click: func(*mygo.MenuItem, *mygo.Window) {
			showMainWindow()
			s.checkForUpdates()
		}},
		mygo.Separator(),
		quit,
	)
	return mygo.NewMenu(items)
}

// refreshTrayMenu relabels the menu, as the language may have changed.
func refreshTrayMenu() {
	if appTray == nil {
		return
	}
	if appShell != nil {
		appShell.syncTrayMenu(true)
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
