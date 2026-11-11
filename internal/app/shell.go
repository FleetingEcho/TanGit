package app

import (
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The window chrome: one title bar over the tabs' pages. The bar is drawn by
// the app, because the system's is hidden (mygo.WindowOptions.TitleBarStyle),
// so its controls and the tabs share one row.
const (
	// maxRepositoryTabs caps the repositories one window reads at once, so a
	// window stays a place to work rather than a wall of tabs.
	maxRepositoryTabs = 10

	titleBarHeight     float32 = 40
	windowCornerRadius float32 = 12
	tabPillHeight      float32 = 26
	tabStripGap        float32 = 4
	tabMinWidth        float32 = 36
	tabMaxWidth        float32 = 208
	// tabNarrowWidth is where a pill gives up its title so its close control
	// still fits: ten repositories and a picker at the window's smallest
	// width leave about that much room each.
	tabNarrowWidth float32 = 76
	// tabTinyWidth is where a pill has no room for its title or close control.
	tabTinyWidth      float32 = 34
	pinnedTabWidth    float32 = 46
	windowControlSize float32 = 32
	// windowControlsRoom is the room the drawn controls take: the four
	// buttons, their gaps, and the bar's trailing padding.
	windowControlsRoom float32 = 4*windowControlSize + 3*2 + 10
	// titleBarReserved is the room the bar's fixed parts take: its padding,
	// the app mark, the + and update buttons, the window controls, and their
	// gaps.
	titleBarReserved float32 = 286
)

// systemWindowControls reports whether the platform draws the window controls
// itself, on the left as macOS's traffic lights: true there, where MyGo keeps
// them, and false on Linux and Windows, where the bar draws its own on the
// right. A test swaps it to check the macOS layout on any platform.
var systemWindowControls = runtime.GOOS == "darwin"

// tab is one page of the window: a repository, or the picker that opens one.
// It wraps an app of its own, so each page keeps its own reads, forms and
// terminal without touching its neighbours.
type tab struct {
	app    *app
	pinned bool
}

// repository reports whether the tab holds a repository, rather than the
// picker. A failed open falls back to the picker, so the tab that showed its
// error can be used to try another folder.
func (t *tab) repository() bool {
	if t.app == nil {
		return false
	}
	return t.app.state == stateReady || t.app.state == stateLoading
}

// path is the repository the tab reads, or the folder its picker points at:
// the folder lets a failed open keep its name and its way back.
func (t *tab) path() string {
	if t.app == nil {
		return ""
	}
	if t.app.state == stateReady {
		if root := strings.TrimSpace(t.app.snapshot.Root); root != "" {
			return root
		}
	}
	return strings.TrimSpace(t.app.path)
}

// title is the name the tab pill shows: the name the repository was given,
// its folder while it has none, or Home.
func (t *tab) title() string {
	if path := t.path(); path != "" {
		if t.app != nil {
			if name := t.app.recentNameFor(path); name != "" {
				return name
			}
		}
		return repoName(path)
	}
	return tr("Home")
}

// tooltip names the page under the pointer, so a pinned tab, which hides its
// title, still says where it goes.
func (t *tab) tooltip() string {
	if !t.repository() {
		if path := strings.TrimSpace(t.path()); path != "" {
			return path + " · " + tr("not open")
		}
		return tr("Home — open a repository")
	}
	path := t.path()
	if t.pinned {
		path += " · " + tr("Pinned")
	}
	return path
}

// shell is the window around the tabs: it owns the frame, the shared
// preferences, and the strip that switches pages. Closing a tab shuts its app
// down; the settings and the history keep their home in the shell.
type shell struct {
	nativeHelpItem *mygo.MenuItem
	window         *mygo.Window
	opener         repositoryOpener
	update         func(func())
	invalidate     func()

	settings       appSettings
	settingsLoaded bool
	pathUserData   string

	tabs   []*tab
	active int
	// pending are the changes a menu, a shortcut or a click asked for while a
	// frame was reading the tabs; they run once the frame is built.
	pending []func()

	// windowVisible and windowFocused follow the window, so the repository
	// is only read again while the user can see it. Showing it, focusing it
	// or switching pages reads again at once; the timer then keeps it fresh.
	windowVisible bool
	windowFocused bool
	// refreshStop ends the auto-refresh timer when the window closes.
	refreshStop chan struct{}
	// availableUpdate is the version the updater's last automatic check
	// found. checkForUpdates opens its native window; both are seams so the
	// shell's UI tests do not need a running MyGo application.
	availableUpdate string
	lastUpdateCheck time.Time
	updaterEnabled  bool
	checkForUpdates func()
	stopUpdateWatch func()

	// crash is a fault caught while drawing the chrome. The title bar falls
	// back to a safe screen rather than taking the window with it.
	crash string
	title string
}

// newShell builds the window's frame without a window yet: a test builds one
// on its own, and the real app hands it a mygo.Window right after.
func newShell(opener repositoryOpener) *shell {
	return &shell{
		opener:          opener,
		update:          func(fn func()) { fn() },
		invalidate:      func() {},
		checkForUpdates: func() {},
		settings:        defaultSettings(),
		settingsLoaded:  true,
		// The app opens its window: a shell built before one exists is a
		// test's, and a test drives autoRefreshTick itself.
		windowVisible: true,
		windowFocused: true,
	}
}

// loadPreferences reads the settings and resolves the directory the history
// lives in. It runs once the window exists, because MyGo resolves that
// directory from the application.
func (s *shell) loadPreferences() {
	s.settings = defaultSettings()
	if s.window != nil {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			s.pathUserData = dir
		}
	}
	if s.pathUserData != "" {
		s.settings = normalizeSettings(loadAppSettings(s.pathUserData))
	}
	s.settingsLoaded = true
	currentLanguage = normalizeLanguage(s.settings.Language)
}

// start fills the window with its first page: the picker, or the repository
// the app was launched with.
func (s *shell) start(path string) {
	path = strings.TrimSpace(path)
	t := s.newTab(path)
	s.tabs = []*tab{t}
	s.active = 0
	s.pushSettings(t.app)
	if path != "" {
		t.app.beginOpen()
	}
}

// newTab makes a page of its own around the shared preferences. The hooks let
// the app ask the window for another tab, or give itself up, without knowing
// there is a shell at all: an app built by a test keeps them nil.
func (s *shell) newTab(path string) *tab {
	a := newApp(s.opener)
	a.window = s.window
	a.update = s.update
	if s.window != nil {
		// Window.Update always repaints, even when a quiet Git read found the
		// same snapshot. Only changed reads should cost a whole-window frame.
		a.updateChanged = func(fn func() bool) {
			mygo.RunOnMain(func() {
				if fn() {
					s.invalidate()
				}
			})
		}
	}
	a.pathUserData = s.pathUserData
	a.path = path
	a.checkForUpdates = func() { s.checkForUpdates() }
	a.updateInfo = s.updateInfo
	a.openTab = func(path string) {
		s.request(func() { s.openTab(path) })
	}
	a.openHomeTab = func() {
		s.request(s.showHome)
	}
	a.claimOpen = func(path string) bool { return s.claimOpen(a, path) }
	a.closeTab = func() { s.closeTabFor(a) }
	a.onRecentRenamed = func(path, name string) { s.spreadRecentName(a, path, name) }
	a.externalRefresh = func() {
		if s.windowVisible && s.windowFocused && s.activeApp() == a {
			a.requestQuietRefresh()
		}
	}
	a.loadHistory()
	s.pushSettings(a)
	return &tab{app: a}
}

// view draws the window: the title bar, then the active tab's page.
func (s *shell) view(c *ui.Context) {
	// A menu or a shortcut asks for its change while a frame reads the tabs:
	// what landed after the last pass is applied before this one builds, and
	// what lands while it builds is applied once it is done. Either way the
	// frame is never reading a slice that changes under it.
	defer s.recoverView(c)
	defer s.applyPending()
	s.applyPending()
	// CornerRadius only rounds native frameless windows on macOS and Windows.
	// Clear the root instead, then clip the app surface on every platform;
	// keeping the native hidden-title-bar frame preserves its resize edges.
	c.Root().Background(ui.Transparent)
	if s.crash != "" {
		s.crashView(c)
		return
	}

	// The System theme resolves against the window's own light or dark
	// appearance, which a palette set below would hide; keep it from before.
	systemDark := c.Theme().Dark
	s.pullSettings()
	currentLanguage = normalizeLanguage(s.settings.Language)
	c.SetTheme(themeForSettings(s.settings, systemDark))
	s.refreshTitle()
	s.registerShortcuts(c)

	// The page starts below the title bar and its divider, and the
	// notifications start below that, on the page's own top edge.
	chrome := max(float32(titleBarHeight), c.TitleBar().Height) + 1
	ui.Column(c).Fill().Radius(windowCornerRadius).Clip().Background(c.Theme().Background).Children(func() {
		s.titleBarView(c)
		ui.Divider(c)
		ui.Column(c).Grow(1).FillWidth().Children(func() {
			if a := s.activeApp(); a != nil {
				a.frame(c, systemDark, chrome)
				s.refreshNativeHelpEnabled()
			}
		})
	})
	// The page settles its own theme through the shared context. Clear the
	// root last so neither that call nor the shell's replaces the transparent
	// corner cutouts with the palette's window-sized background.
	c.Root().Background(ui.Transparent)
}

// recoverView contains a fault in the chrome, as the app contains one in a
// page: the safe screen draws on the next pass.
func (s *shell) recoverView(c *ui.Context) {
	r := recover()
	if r == nil || s.crash != "" {
		return
	}
	s.crash = describeFault(r)
	if a := s.activeApp(); a != nil && a.commandLog != nil {
		a.commandLog.Note("Unexpected window chrome failure: " + s.crash)
	}
	if c != nil {
		c.Invalidate()
	}
}

// crashView keeps the window useful when its chrome failed: the message, and
// a way to try the frame again.
func (s *shell) crashView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Fill().Center().Gap(14).Padding(28).Radius(windowCornerRadius).Clip().
		Background(c.Theme().Background).Children(func() {
		ui.Box(c).Size(52, 52).Radius(14).Center().
			Background(theme.Danger.Alpha(0.14)).Children(func() {
			ui.Icon(c, conflictIcon).FontSize(26).TextColor(theme.Danger)
		})
		ui.Text(c, tr("This window hit an unexpected problem")).FontSize(21).Bold().
			TextAlign(ui.Center)
		ui.Text(c, s.crash).TextColor(theme.Danger).TextAlign(ui.Center).
			MaxWidth(560).Selectable()
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c, tr("Try again")).Clicked() {
				s.crash = ""
			}
		})
	})
}

// applyPending runs what the last frame asked for, in the order it was asked.
func (s *shell) applyPending() {
	for len(s.pending) > 0 {
		fn := s.pending[0]
		s.pending = s.pending[1:]
		fn()
	}
}

// request queues a change to the window's pages and asks for the frame that
// applies it.
func (s *shell) request(fn func()) {
	s.pending = append(s.pending, fn)
	s.invalidate()
}

// pullSettings takes the preferences the active page edited, so the chrome and
// the next page it switches to share them.
func (s *shell) pullSettings() {
	a := s.activeApp()
	if a == nil {
		return
	}
	s.settings = a.settings
	s.settingsLoaded = a.settingsLoaded
}

// pushSettings gives a page the window's preferences, with the toggles the
// settings modal mirrors into an app.
func (s *shell) pushSettings(a *app) {
	if a == nil {
		return
	}
	a.settings = s.settings
	a.settingsLoaded = s.settingsLoaded
	a.pathUserData = s.pathUserData
	a.diffSplit = s.settings.DiffSplit
	a.diffWrap = s.settings.DiffWrap
	a.ignoreUntracked = s.settings.IgnoreUntracked
	a.ensureVisibleSection()
}

// activeTab is the page on screen, nil while the window has none.
func (s *shell) activeTab() *tab {
	if s.active < 0 || s.active >= len(s.tabs) {
		return nil
	}
	return s.tabs[s.active]
}

// activeApp is the app of the page on screen.
func (s *shell) activeApp() *app {
	if t := s.activeTab(); t != nil {
		return t.app
	}
	return nil
}

// indexOf finds a page after the tabs moved, so a queued action still names
// the page the user chose rather than the place it used to hold.
func (s *shell) indexOf(t *tab) int {
	for i, candidate := range s.tabs {
		if candidate == t {
			return i
		}
	}
	return -1
}

// tabOf finds the page an app belongs to.
func (s *shell) tabOf(a *app) *tab {
	for _, t := range s.tabs {
		if t.app == a {
			return t
		}
	}
	return nil
}

// hasHome reports whether the window already has the picker open, which the
// + button shows instead of adding a second one.
func (s *shell) hasHome() bool {
	for _, t := range s.tabs {
		if !t.repository() {
			return true
		}
	}
	return false
}

// repositoryCount is the number of pages that read a repository, which the cap
// counts; the picker is always allowed on top.
func (s *shell) repositoryCount() int {
	count := 0
	for _, t := range s.tabs {
		if t.repository() {
			count++
		}
	}
	return count
}

// activate switches the window to a page, carrying the preferences across.
func (s *shell) activate(i int) {
	if i < 0 || i >= len(s.tabs) {
		return
	}
	// The page leaving saves the keys typed into its panes: it gets no frame
	// of its own once the switch is made.
	if prev := s.activeApp(); prev != nil {
		prev.flushEdits()
	}
	s.pullSettings()
	s.active = i
	s.pushSettings(s.tabs[i].app)
	s.refreshTitle()
	s.invalidate()
	// The page coming in may have been hidden while its repository moved:
	// read it again so it shows what Git holds now.
	s.tabs[i].app.requestAmbientRefresh()
}

// activateTab is activate for a page that may have moved.
func (s *shell) activateTab(t *tab) {
	if i := s.indexOf(t); i >= 0 {
		s.activate(i)
	}
}

// startAutoRefresh starts the timer that keeps the pages current with their
// repositories. It runs only while the window is visible and focused, so a
// window in the tray costs nothing, and it stops when the window closes.
func (s *shell) startAutoRefresh() {
	if s.window == nil || s.refreshStop != nil {
		return
	}
	stop := make(chan struct{})
	s.refreshStop = stop
	go func() {
		ticker := time.NewTicker(autoRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// A tick starts an asynchronous read; it changes no pixels by
				// itself, so it must not repaint the whole window either.
				mygo.RunOnMain(s.autoRefreshTick)
			}
		}
	}()
}

// autoRefreshTick reads the active page's repository again if the user can
// see the window. It runs on the main thread, where the state lives.
func (s *shell) autoRefreshTick() {
	if !s.windowVisible || !s.windowFocused {
		return
	}
	s.refreshActiveTab()
}

// handleWindowShown records that the window is visible again and reads every page:
// what another app changed while it was in the tray is there when it opens,
// not after an F5.
func (s *shell) handleWindowShown() {
	s.windowVisible = true
	s.refreshAllTabs()
}

// handleWindowFocused records that the window took focus, as switching back to it
// does, and reads the page on screen.
func (s *shell) handleWindowFocused() {
	s.windowVisible = true
	s.windowFocused = true
	s.refreshActiveTab()
}

// handleWindowBlurred records that the window lost focus: its pages keep their
// state, and the timer stands aside until it is back.
func (s *shell) handleWindowBlurred() {
	s.windowFocused = false
}

// handleWindowHidden records that the window went to the tray.
func (s *shell) handleWindowHidden() {
	s.windowVisible = false
	s.windowFocused = false
}

// refreshActiveTab reads the repository of the page on screen again.
func (s *shell) refreshActiveTab() {
	if a := s.activeApp(); a != nil {
		a.requestAmbientRefresh()
	}
}

// refreshAllTabs reads every open repository again, which is what reopening
// the window after it was hidden asks for.
func (s *shell) refreshAllTabs() {
	for _, t := range s.tabs {
		if t.app != nil {
			t.app.requestAmbientRefresh()
		}
	}
}

// cycleTab moves through the pages, wrapping at either end.
func (s *shell) cycleTab(delta int) {
	if len(s.tabs) < 2 {
		return
	}
	i := (s.active + delta) % len(s.tabs)
	if i < 0 {
		i += len(s.tabs)
	}
	s.activate(i)
}

// refreshTitle names the active page in the window's title, so the task bar
// and the window list tell the windows apart.
func (s *shell) refreshTitle() {
	if s.window == nil {
		return
	}
	title := appName
	if t := s.activeTab(); t != nil && t.repository() {
		if name := t.title(); name != "" {
			title = name + " — " + appName
		}
	}
	if title == s.title {
		return
	}
	s.title = title
	s.window.SetTitle(title)
}

// cleanRepoPath resolves a folder the same way each time, so "/work/repo",
// "/work/./repo" and a symbolic link to it are recognised as one repository.
func cleanRepoPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}

// tabForPath finds the page that already reads a repository, leaving out the
// page that is asking (a picker named after the folder it points at).
func (s *shell) tabForPath(path string, except *app) *tab {
	want := cleanRepoPath(path)
	if want == "" {
		return nil
	}
	for _, t := range s.tabs {
		if t.app == except || !t.repository() {
			continue
		}
		if cleanRepoPath(t.path()) == want {
			return t
		}
	}
	return nil
}

// isPicker reports whether a page is the picker: not reading a repository, and
// not on its way to one.
func (s *shell) isPicker(a *app) bool {
	return a != nil && a.state != stateReady && a.state != stateLoading
}

// claimOpen is the hook beginOpen calls first. It reports whether the window
// took the open over: the repository already has a tab, which is switched to
// (and the picker that led there is put away), or the window has reached its
// cap. Otherwise the open runs in the page that asked.
func (s *shell) claimOpen(from *app, path string) bool {
	if existing := s.tabForPath(path, from); existing != nil {
		picker := s.isPicker(from)
		s.request(func() {
			if picker {
				if t := s.tabOf(from); t != nil {
					s.closeTab(t)
				}
			}
			s.activateTab(existing)
		})
		return true
	}
	if s.isPicker(from) && s.repositoryCount() >= maxRepositoryTabs {
		s.notifyTabLimit(from)
		return true
	}
	return false
}

// showHome shows the picker: the tab that already holds it, or a new one when
// the window has room. The + button and Ctrl+T lead here.
func (s *shell) showHome() {
	for i, t := range s.tabs {
		if !t.repository() {
			s.activate(i)
			return
		}
	}
	if s.repositoryCount() >= maxRepositoryTabs {
		s.notifyTabLimit(s.activeApp())
		return
	}
	t := s.newTab("")
	s.tabs = append(s.tabs, t)
	s.activate(s.indexOf(t))
}

// openTab opens a repository in a page of its own, for the picker's "Open in
// new tab". An already open repository is switched to instead of duplicated.
func (s *shell) openTab(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if existing := s.tabForPath(path, nil); existing != nil {
		s.activateTab(existing)
		return
	}
	if s.repositoryCount() >= maxRepositoryTabs {
		s.notifyTabLimit(s.activeApp())
		return
	}
	t := s.newTab(path)
	s.tabs = append(s.tabs, t)
	s.activate(s.indexOf(t))
	t.app.beginOpen()
}

// closeTabFor gives up the page an app belongs to, as the app's crash screen
// and its "Back to repositories" ask.
func (s *shell) closeTabFor(a *app) {
	if t := s.tabOf(a); t != nil {
		s.request(func() { s.closeTab(t) })
		return
	}
	a.shutdown()
}

// canClose reports whether closing a page would leave the window with a page:
// the last picker stays, and a lone repository leaves a fresh picker behind.
func (s *shell) canClose(t *tab) bool {
	return len(s.tabs) > 1 || t.repository()
}

// closeTab closes a page. The last page is never dropped to nothing: a lone
// repository leaves the picker behind, and a lone picker stays.
func (s *shell) closeTab(t *tab) {
	s.confirmCodeTabs([]*tab{t}, func() { s.closeTabConfirmed(t) })
}

// confirmCodeTabs visits each affected repository, showing its unsaved prompt
// before any page is removed. Cancelling leaves every page intact.
func (s *shell) confirmCodeTabs(tabs []*tab, run func()) {
	for index, candidate := range tabs {
		if s.indexOf(candidate) < 0 || len(candidate.app.dirtyCodePaths()) == 0 {
			continue
		}
		s.activate(s.indexOf(candidate))
		candidate.app.confirmCodeChanges(candidate.app.dirtyCodePaths(), func() {
			s.confirmCodeTabs(tabs[index+1:], run)
		})
		s.invalidate()
		return
	}
	run()
}

func (s *shell) closeTabConfirmed(t *tab) {
	i := s.indexOf(t)
	if i < 0 {
		return
	}
	if len(s.tabs) == 1 {
		if !t.repository() {
			return
		}
		t.app.shutdown()
		s.tabs[0] = s.newTab("")
		s.active = 0
		s.pushSettings(s.tabs[0].app)
		s.refreshTitle()
		s.invalidate()
		return
	}
	wasActive := s.activeTab()
	t.app.shutdown()
	s.tabs = append(s.tabs[:i], s.tabs[i+1:]...)
	s.reindex(wasActive)
	s.invalidate()
}

// closeOthers closes every page that can be closed besides t, keeping pinned
// pages as Chrome does.
func (s *shell) closeOthers(t *tab) {
	var closing []*tab
	for _, candidate := range s.tabs {
		if candidate != t && !candidate.pinned {
			closing = append(closing, candidate)
		}
	}
	s.confirmCodeTabs(closing, func() { s.closeOthersConfirmed(t) })
}

func (s *shell) closeOthersConfirmed(t *tab) {
	if s.indexOf(t) < 0 {
		return
	}
	wasActive := s.activeTab()
	kept := make([]*tab, 0, len(s.tabs))
	for _, candidate := range s.tabs {
		if candidate == t || candidate.pinned {
			kept = append(kept, candidate)
			continue
		}
		candidate.app.shutdown()
	}
	s.tabs = kept
	s.reindex(wasActive)
	// The page that led here stays on screen when the one it replaced was
	// the page the user was reading.
	if wasActive != nil && s.indexOf(wasActive) < 0 {
		s.activate(s.indexOf(t))
	}
	s.invalidate()
}

// closeRight closes the pages after t that can be closed, leaving pinned ones.
func (s *shell) closeRight(t *tab) {
	index := s.indexOf(t)
	if index < 0 {
		return
	}
	var closing []*tab
	for _, candidate := range s.tabs[index+1:] {
		if !candidate.pinned {
			closing = append(closing, candidate)
		}
	}
	s.confirmCodeTabs(closing, func() { s.closeRightConfirmed(t) })
}

func (s *shell) closeRightConfirmed(t *tab) {
	i := s.indexOf(t)
	if i < 0 {
		return
	}
	wasActive := s.activeTab()
	kept := make([]*tab, 0, len(s.tabs))
	for j, candidate := range s.tabs {
		if j > i && !candidate.pinned {
			candidate.app.shutdown()
			continue
		}
		kept = append(kept, candidate)
	}
	s.tabs = kept
	s.reindex(wasActive)
	s.invalidate()
}

// closeAll closes every page and leaves the picker in the window.
func (s *shell) closeAll() {
	s.confirmCodeTabs(append([]*tab(nil), s.tabs...), s.closeAllConfirmed)
}

func (s *shell) closeAllConfirmed() {
	for _, t := range s.tabs {
		t.app.shutdown()
	}
	s.tabs = nil
	s.active = 0
	t := s.newTab("")
	s.tabs = []*tab{t}
	s.pushSettings(t.app)
	s.refreshTitle()
	s.invalidate()
}

// reindex points the window at the page that was active before pages went
// away, or at the nearest one that is left.
func (s *shell) reindex(active *tab) {
	if i := s.indexOf(active); i >= 0 {
		s.active = i
	} else if s.active >= len(s.tabs) {
		s.active = len(s.tabs) - 1
	}
	if s.active < 0 {
		s.active = 0
	}
	s.pushSettings(s.activeApp())
	s.refreshTitle()
}

// togglePin pins a page to the front, or lets it back among the others.
func (s *shell) togglePin(t *tab) {
	i := s.indexOf(t)
	if i < 0 {
		return
	}
	s.pullSettings()
	t.pinned = !t.pinned
	pinned := make([]*tab, 0, len(s.tabs))
	for _, candidate := range s.tabs {
		if candidate.pinned {
			pinned = append(pinned, candidate)
		}
	}
	for _, candidate := range s.tabs {
		if !candidate.pinned {
			pinned = append(pinned, candidate)
		}
	}
	s.tabs = pinned
	s.active = s.indexOf(t)
	s.invalidate()
}

// hasOtherClosable reports whether "Close other tabs" would close anything.
func (s *shell) hasOtherClosable(t *tab) bool {
	for _, candidate := range s.tabs {
		if candidate != t && !candidate.pinned {
			return true
		}
	}
	return false
}

// hasClosableRight reports whether "Close tabs to the right" would close
// anything.
func (s *shell) hasClosableRight(t *tab) bool {
	i := s.indexOf(t)
	if i < 0 {
		return false
	}
	for j := i + 1; j < len(s.tabs); j++ {
		if !s.tabs[j].pinned {
			return true
		}
	}
	return false
}

// notifyTabLimit explains why another repository cannot open.
func (s *shell) notifyTabLimit(a *app) {
	if a == nil {
		return
	}
	a.notify("error", "Tab limit reached",
		trf("TanGit keeps up to %d repositories open at once. Close a tab before opening another.",
			maxRepositoryTabs))
	s.invalidate()
}

// registerShortcuts installs the chrome's keys. They stand aside while a
// page's dialog owns the keyboard, and while every shortcut is switched off.
func (s *shell) registerShortcuts(c *ui.Context) {
	if s.settings.DisableShortcuts {
		return
	}
	if a := s.activeApp(); a != nil && a.modalOpen() {
		return
	}
	c.OnShortcut(ui.Cmd, ui.KeyT, func() { s.request(s.showHome) })
	c.OnShortcut(ui.Cmd, ui.KeyW, func() {
		s.request(func() {
			if t := s.activeTab(); t != nil {
				s.closeTab(t)
			}
		})
	})
	c.OnShortcut(ui.Ctrl, ui.KeyTab, func() { s.request(func() { s.cycleTab(1) }) })
	c.OnShortcut(ui.Ctrl|ui.Shift, ui.KeyTab, func() { s.request(func() { s.cycleTab(-1) }) })
}

// titleBarView draws the window's own title bar: the brand, the tabs, +, and
// the window controls. The bar drags the window wherever no control does. On
// macOS the system keeps its traffic lights on the left, so the bar starts
// clear of them and draws no controls of its own.
func (s *shell) titleBarView(c *ui.Context) {
	bar := c.TitleBar()
	height := max(float32(titleBarHeight), bar.Height)
	ui.Row(c).FillWidth().Height(height).AlignItems(ui.Center).
		Padding(0, 10).Gap(8).DragWindow().Children(func() {
		reserved := titleBarReserved
		if systemWindowControls {
			// The bar's 10 of padding is already inside the room the
			// traffic lights take, so the spacer adds what is left.
			ui.Box(c).Width(max(bar.Left-10, 0))
			reserved = titleBarReserved - windowControlsRoom + bar.Left
		}
		s.appMarkView(c)
		width, _ := c.Size()
		s.tabStripView(c, width-reserved)
		s.plusView(c)
		ui.Box(c).Grow(1)
		s.updaterView(c)
		if !systemWindowControls {
			s.windowControlsView(c)
		}
	})
}

// updaterView is always a manual check button. After a background check finds
// a release it gains an accent face and a notification dot, so the update is
// visible without interrupting the user's work; clicking opens the updater's
// release notes and install flow.
func (s *shell) updaterView(c *ui.Context) {
	theme := c.Theme()
	version := strings.TrimSpace(s.availableUpdate)
	label := tr("Check for updates")
	tooltip := label
	if version != "" {
		label = tr("Update available")
		tooltip = trf("Update %s is available — click to install", version)
	}
	button := ui.ButtonBase(c).Height(tabPillHeight).Padding(6).Gap(4).
		Radius(pillRadius(theme)).Label(label).Tooltip(tooltip)
	button.Children(func() {
		color := theme.TextMuted
		if version != "" {
			color = theme.Accent
		}
		ui.Icon(c, updateIcon).FontSize(15).TextColor(color)
		if version != "" {
			ui.Box(c).Size(6, 6).Radius(3).Background(theme.Danger)
		}
	})
	if version != "" {
		button.Background(theme.Accent.Alpha(0.14))
	} else if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	if button.Clicked() {
		s.checkForUpdates()
	}
}

// appMarkView is the brand on the left: a quiet way back to the picker.
func (s *shell) appMarkView(c *ui.Context) {
	theme := c.Theme()
	button := ui.ButtonBase(c).Size(28, tabPillHeight).Radius(pillRadius(theme)).Center()
	button.Label(tr("TanGit home")).Tooltip(tr("TanGit — open the repository picker"))
	hovered := button.Hovered()
	button.Children(func() {
		ui.Icon(c, appIcon).FontSize(16).TextColor(theme.Accent)
	})
	if hovered {
		button.Background(theme.SurfaceHover)
	} else {
		button.Background(theme.Accent.Alpha(0.12))
	}
	if button.Clicked() {
		s.request(s.showHome)
	}
}

// plusView opens another repository: the picker, or the one the window already
// shows. It dims once the cap leaves no room for another page.
func (s *shell) plusView(c *ui.Context) {
	if !s.hasHome() && s.repositoryCount() >= maxRepositoryTabs {
		flatIconButton(c, "New tab", "New tab — up to 10 repositories", plusIcon,
			15, 6, true, nil)
		return
	}
	flatIconButton(c, "New tab", "New tab (Ctrl+T) — choose a repository", plusIcon,
		15, 6, false, func() { s.request(s.showHome) })
}

// tabStripView lays the pills out across the room the bar leaves them. Pinned
// pills keep a fixed width; the others share the rest, so ten tabs still fit.
func (s *shell) tabStripView(c *ui.Context, available float32) {
	widths := s.tabWidths(available)
	ui.Row(c).AlignItems(ui.Center).Gap(tabStripGap).Children(func() {
		for i := range widths {
			if i >= len(s.tabs) {
				break
			}
			s.tabView(c, i, widths[i])
		}
	})
}

// tabWidths spreads the room across the tabs, clamped so a pill is still a
// target at one end and still readable at the other.
func (s *shell) tabWidths(available float32) []float32 {
	widths := make([]float32, len(s.tabs))
	pinned := 0
	for i := range s.tabs {
		if s.tabs[i].pinned {
			widths[i] = pinnedTabWidth
			pinned++
		}
	}
	rest := len(s.tabs) - pinned
	if rest == 0 {
		return widths
	}
	gaps := float32(len(s.tabs)-1) * tabStripGap
	total := available - gaps - float32(pinned)*pinnedTabWidth
	width := total / float32(rest)
	if width < tabMinWidth {
		width = tabMinWidth
	}
	if width > tabMaxWidth {
		width = tabMaxWidth
	}
	for i := range s.tabs {
		if !s.tabs[i].pinned {
			widths[i] = width
		}
	}
	// Pinned pills keep room for their pin, but a strip of them at the
	// smallest window must not push the + and the controls out of the bar:
	// every pill gives up the same share instead.
	used := gaps
	for _, w := range widths {
		used += w
	}
	if available > 0 && used > available {
		scale := available / used
		for i := range widths {
			widths[i] *= scale
		}
	}
	return widths
}

// pillRadius keeps the pills rounded in every theme, sharp only when the
// settings ask for sharp corners.
func pillRadius(theme *ui.Theme) float32 {
	return min(max(theme.Radius+2, 3), 10)
}

// tabView is one pill: its name and the close control on the page the pointer
// is on. A right click opens the tab's menu.
func (s *shell) tabView(c *ui.Context, i int, width float32) {
	theme := c.Theme()
	t := s.tabs[i]
	active := i == s.active
	narrow := width < tabNarrowWidth
	tiny := width < tabTinyWidth
	gap, pad := float32(7), float32(8)
	if narrow {
		gap, pad = 3, 4
	}
	c.Key(t)
	pill := ui.Row(c).Height(tabPillHeight).Width(width).AlignItems(ui.Center).
		Gap(gap).Padding(0, pad).Radius(pillRadius(theme)).
		Role(ui.RoleTab).Checked(active).Label(t.title()).Focusable().FocusRing(true)
	hovered := pill.Hovered()
	switch {
	case active:
		pill.Background(theme.Surface).Border(1, theme.Border)
	case hovered:
		pill.Background(theme.SurfaceHover.Alpha(0.6)).Border(1, ui.Transparent)
	default:
		pill.Background(ui.Transparent).Border(1, ui.Transparent)
	}
	pill.Tooltip(t.tooltip())
	pill.OnShortcut(0, ui.KeyEnter, func() { s.request(func() { s.activateTab(t) }) })
	pill.OnShortcut(0, ui.KeySpace, func() { s.request(func() { s.activateTab(t) }) })
	services := c.Services()
	closeClicked := false
	pill.Children(func() {
		if t.pinned {
			ui.Icon(c, pinIcon).FontSize(11).TextColor(theme.TextMuted)
			return
		}
		showClose := (active || hovered) && s.canClose(t) && !tiny
		if showClose && narrow {
			// The close control takes the room the title would have, so a
			// crowded strip can still put away the page on screen.
			closeClicked = s.tabCloseButton(c, t)
			return
		}
		if tiny {
			// The tooltip and menu still name a page too narrow for either a
			// title or a control.
			return
		}
		color := theme.TextMuted
		if active {
			color = theme.Text
		}
		title := ui.Text(c, t.title()).Grow(1).SingleLine().TextColor(color)
		if active {
			title.Bold()
		}
		if showClose {
			closeClicked = s.tabCloseButton(c, t)
		}
	})
	if pill.Clicked() && !closeClicked {
		s.request(func() { s.activateTab(t) })
	}
	pill.PopoverMenu(func(m *ui.Menu) { s.tabMenu(m, t, services) })
}

// tabCloseButton closes one page from its pill, without also switching to it.
func (s *shell) tabCloseButton(c *ui.Context, t *tab) bool {
	theme := c.Theme()
	button := ui.ButtonBase(c).Size(16, 16).Radius(4).Center()
	button.Label(tr("Close tab") + " " + t.title()).Tooltip(tr("Close tab"))
	hovered := button.Hovered()
	button.Children(func() {
		ui.Icon(c, closeIcon).FontSize(10).TextColor(theme.TextMuted)
	})
	if hovered {
		button.Background(theme.SurfacePressed)
	}
	if !button.Clicked() {
		return false
	}
	// The page goes once the frame is built, so the strip is not changed
	// while it is being drawn.
	s.request(func() { s.closeTab(t) })
	return true
}

// renameTab asks for the name of the page's repository. The page is brought
// forward first, so the modal that takes the name sits on it.
func (s *shell) renameTab(t *tab) {
	if t == nil || t.app == nil || !t.repository() {
		return
	}
	s.activateTab(t)
	t.app.openTabRename(t.path())
}

// spreadRecentName gives a name chosen in one page to every other page, so a
// pill and the picker's card name the repository the same way. The page that
// chose it has already stored the name.
func (s *shell) spreadRecentName(source *app, path, name string) {
	for _, t := range s.tabs {
		if t.app == nil || t.app == source {
			continue
		}
		t.app.setRecentName(path, name)
	}
}

// tabMenu is Chrome's tab menu in the app's own look: pin, close, close the
// neighbours, close all, then the repository's path.
func (s *shell) tabMenu(m *ui.Menu, t *tab, services ui.Services) {
	if t.repository() {
		if m.Item(tr("Rename tab")).Chosen() {
			s.request(func() { s.renameTab(t) })
		}
		m.Separator()
		label := "Pin tab"
		if t.pinned {
			label = "Unpin tab"
		}
		if m.Item(tr(label)).Chosen() {
			s.request(func() { s.togglePin(t) })
		}
		m.Separator()
	}
	closeEnabled := s.canClose(t)
	if m.Item(tr("Close tab")).Disabled(!closeEnabled).Chosen() {
		s.request(func() { s.closeTab(t) })
	}
	if m.Item(tr("Close other tabs")).Disabled(!s.hasOtherClosable(t)).Chosen() {
		s.request(func() { s.closeOthers(t) })
	}
	if m.Item(tr("Close tabs to the right")).Disabled(!s.hasClosableRight(t)).Chosen() {
		s.request(func() { s.closeRight(t) })
	}
	if len(s.tabs) > 1 && m.Item(tr("Close all tabs")).Chosen() {
		s.request(s.closeAll)
	}
	if t.repository() {
		m.Separator()
		if m.Item(tr("Copy repository path")).Chosen() {
			// The confirmation belongs on the page in front, which may not be
			// the tab the menu was opened on.
			toast := s.activeApp()
			if toast == nil {
				toast = t.app
			}
			toast.notifyCopied(services, "repository path", t.path())
		}
	}
}

// windowControlsView draws minimize, maximize or restore, full screen, and
// close, since the system's are hidden.
func (s *shell) windowControlsView(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(2).Children(func() {
		s.windowControlView(c, "Minimize window", "Minimize", minimizeIcon,
			false, func() {
				if s.window != nil {
					s.window.Minimize()
				}
			})
		icon, tip := maximizeIcon, "Maximize"
		if s.isMaximized() {
			icon, tip = restoreIcon, "Restore"
		}
		s.windowControlView(c, "Maximize window", tip, icon, true, func() {
			if s.window != nil {
				s.window.ToggleMaximize()
			}
		})
		icon, tip = fullscreenIcon, "Enter full screen"
		if s.isFullScreen() {
			icon, tip = exitFullscreenIcon, "Exit full screen"
		}
		s.windowControlView(c, "Full screen", tip, icon, false, func() {
			if s.window != nil {
				s.window.ToggleFullScreen()
			}
		})
		s.windowCloseView(c)
	})
}

// windowControlView is one window control, with the maximize one telling
// Windows where its snap layouts belong.
func (s *shell) windowControlView(c *ui.Context, label, tip string, icon *ui.SVG, maximize bool, run func()) {
	theme := c.Theme()
	button := ui.ButtonBase(c).Size(windowControlSize, tabPillHeight).
		Radius(pillRadius(theme)).Center()
	button.Label(tr(label)).Tooltip(tr(tip))
	if maximize && s.window != nil {
		button.MaximizeButton()
	}
	hovered := button.Hovered()
	button.Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(theme.TextMuted)
	})
	if hovered {
		button.Background(theme.SurfaceHover)
	}
	if button.Clicked() {
		run()
	}
}

// windowCloseView is the close control, red on hover as every window's is.
func (s *shell) windowCloseView(c *ui.Context) {
	theme := c.Theme()
	button := ui.ButtonBase(c).Size(windowControlSize, tabPillHeight).
		Radius(pillRadius(theme)).Center()
	tip := "Close"
	if trayReady {
		tip = "Minimize to the tray"
	}
	button.Label(tr("Close window")).Tooltip(tr(tip))
	hovered := button.Hovered()
	button.Children(func() {
		color := theme.TextMuted
		if hovered {
			color = theme.Danger
		}
		ui.Icon(c, closeIcon).FontSize(13).TextColor(color)
	})
	if hovered {
		button.Background(theme.Danger.Alpha(0.16))
	}
	if button.Clicked() && s.window != nil {
		s.window.Close()
	}
}

// isMaximized reports the window's state, false while a test has no window.
func (s *shell) isMaximized() bool {
	return s.window != nil && s.window.IsMaximized()
}

// isFullScreen reports the window's state, false while a test has no window.
func (s *shell) isFullScreen() bool {
	return s.window != nil && s.window.IsFullScreen()
}

// shutdown stops the auto-refresh timer and shuts every page down, releasing
// what each held, as the window closes.
func (s *shell) shutdown() {
	if s.stopUpdateWatch != nil {
		s.stopUpdateWatch()
		s.stopUpdateWatch = nil
	}
	if s.refreshStop != nil {
		close(s.refreshStop)
		s.refreshStop = nil
	}
	for _, t := range s.tabs {
		if t.app != nil {
			t.app.shutdown()
		}
	}
	s.tabs = nil
}
