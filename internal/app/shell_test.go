package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// pathOpener opens a repository rooted at the folder it was asked for, so a
// window with several pages can name each of them.
type pathOpener struct{ snapshot repository.Snapshot }

func (pathOpener) Init(context.Context, string) error { return nil }

func (o pathOpener) Open(_ context.Context, path string) (repository.Session, error) {
	snapshot := o.snapshot
	snapshot.Root = path
	return fakeSession{snapshot: snapshot}, nil
}

// newShellTester builds a tabbed window on the picker, with its updates run on
// the test goroutine as MyGo runs them on the UI loop.
func newShellTester(t *testing.T, snapshot repository.Snapshot) (*shell, *ui.Tester, *updates) {
	t.Helper()
	s := newShell(pathOpener{snapshot: snapshot})
	s.pathUserData = t.TempDir()
	updated := newUpdates()
	s.update = updated.update
	s.invalidate = func() {}
	s.start("")
	tt := ui.NewTester(s.view, 1280, 800)
	tt.Frame()
	return s, tt, updated
}

// readyShellTab adds a page already reading a repository, without a read on a
// goroutine: a test that is not about opening starts from a settled window.
func readyShellTab(s *shell, root string) *tab {
	t := s.newTab(root)
	t.app.state = stateReady
	t.app.path = root
	t.app.snapshot = repository.Snapshot{Root: root, Branch: "main"}
	s.tabs = append(s.tabs, t)
	return t
}

// openShellRepo opens path from the active page and waits for the read to
// land, as a click on a card would.
func openShellRepo(t *testing.T, s *shell, tt *ui.Tester, updated *updates, path string) {
	t.Helper()
	a := s.activeApp()
	a.path = path
	a.beginOpen()
	for range 12 {
		if a.state == stateReady && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	tt.Frame()
	if a.state != stateReady {
		t.Fatalf("the repository did not open: state=%v err=%q", a.state, a.errMessage)
	}
}

// A fresh window opens on the picker: one page, the tab chrome, and the
// window's own controls, since the system's are hidden.
func TestShellStartsOnThePicker(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})

	if len(s.tabs) != 1 || s.tabs[0].repository() {
		t.Fatalf("the window did not start on one picker page: %d tabs", len(s.tabs))
	}
	if s.activeTab() != s.tabs[0] {
		t.Fatal("the picker is not the page on screen")
	}
	for _, want := range []string{
		"Home", "Open a repository", "New tab", "Check for updates",
		"Minimize window", "Maximize window", "Full screen", "Close window",
	} {
		if _, found := tt.Find(want); !found {
			t.Errorf("the window chrome lost %q: %v", want, tt.Texts())
		}
	}
	// The lone picker cannot be closed, so it carries no close control.
	if tt.HasText("Close tab") {
		t.Errorf("the lone picker offers a close control: %v", tt.Texts())
	}
}

func TestShellUpdateButtonAdvertisesAndOpensAnAvailableUpdate(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	checks := 0
	s.checkForUpdates = func() { checks++ }
	s.availableUpdate = "1.1.0"
	tt.Frame()

	updater, found := tt.Find("Update available")
	if !found {
		t.Fatalf("the title bar did not advertise the update: %v", tt.Texts())
	}
	plus, found := tt.Find("New tab")
	if !found {
		t.Fatal("the title bar lost the new-tab button")
	}
	if updater.X <= plus.X || updater.X < float32(tt.Image().Bounds().Dx())/2 {
		t.Fatalf("the updater is not in the title bar's right area: updater=%+v plus=%+v", updater, plus)
	}
	if err := tt.Click("Update available"); err != nil {
		t.Fatal(err)
	}
	if checks != 1 {
		t.Fatalf("clicking the update icon checked %d times, want 1", checks)
	}
}

// Clicking a pill brings its page to the front, as the strip's tabs do in a
// browser.
func TestShellClickingATabSwitchesPages(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	first := readyShellTab(s, "/work/first")
	second := readyShellTab(s, "/work/second")
	s.activate(s.indexOf(first))
	tt.Frame()

	if err := tt.Click("second"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.activeTab() != second {
		t.Fatalf("clicking the tab did not switch the page: %s", s.tabTitles())
	}
	if !tt.HasText("second") {
		t.Fatalf("the second page did not draw: %v", tt.Texts())
	}
}

// The close control on the page's own pill puts that page away and brings the
// page beside it to the front, as the × on a browser tab does.
func TestShellTabCloseControlClosesThePage(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	first := readyShellTab(s, "/work/first")
	second := readyShellTab(s, "/work/second")
	s.activate(s.indexOf(second))
	tt.Frame()

	if err := tt.Click("Close tab second"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.indexOf(second) >= 0 {
		t.Fatalf("the close control left %v", s.tabTitles())
	}
	if s.activeTab() != first {
		t.Fatalf("the window did not move to the neighbour: %v", s.tabTitles())
	}

	// The same control puts the last repository away, leaving the picker.
	if err := tt.Click("Close tab first"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.tabs) != 1 || s.tabs[0].repository() {
		t.Fatalf("closing the last page from its pill left %v", s.tabTitles())
	}
	if !tt.HasText("Open a repository") {
		t.Fatalf("the picker is not on screen after the close: %v", tt.Texts())
	}
}

// The + button opens the picker, and opening a repository that already has a
// page jumps to that page instead of adding a duplicate.
func TestShellPlusOpensThePickerAndClaimsAnOpenRepository(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	openShellRepo(t, s, tt, updated, "/work/project")
	if len(s.tabs) != 1 || !s.tabs[0].repository() {
		t.Fatalf("the first repository did not take the picker's page: %d tabs", len(s.tabs))
	}

	if err := tt.Click("New tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.tabs) != 2 || s.activeTab().repository() {
		t.Fatalf("+ did not open a picker page: %d tabs", len(s.tabs))
	}

	// The same folder, opened from the new picker, is already on screen: the
	// window switches to its page and puts the spare picker away.
	s.activeApp().path = "/work/project"
	s.activeApp().beginOpen()
	tt.Frame()
	if len(s.tabs) != 1 {
		t.Fatalf("the duplicate open left %d tabs, want the repository's one", len(s.tabs))
	}
	if !s.activeTab().repository() || s.activeTab().path() != "/work/project" {
		t.Fatalf("the window did not switch to the open repository: %+v", s.activeTab())
	}

	// A folder that is not open yet becomes the picker's own page.
	if err := tt.Click("New tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	openShellRepo(t, s, tt, updated, "/work/another")
	if len(s.tabs) != 2 || s.activeTab().path() != "/work/another" {
		t.Fatalf("a new repository did not take the picker's page: %s", s.tabTitles())
	}
}

// A page is recognised by the folder it resolves to, so a path written another
// way is still the same repository.
func TestShellClaimsAReformattedPath(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	openShellRepo(t, s, tt, updated, "/work/project")

	if err := tt.Click("New tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	s.activeApp().path = "/work/./project"
	s.activeApp().beginOpen()
	tt.Frame()
	if len(s.tabs) != 1 || !s.activeTab().repository() {
		t.Fatalf("a reformatted path opened a second page: %d tabs", len(s.tabs))
	}
}

// The window keeps at most ten repositories; the picker may sit on top, and
// the + button dims when there is no room and no picker to show.
func TestShellKeepsTenRepositoryPages(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	for i := range maxRepositoryTabs {
		readyShellTab(s, fmt.Sprintf("/work/repo-%02d", i))
	}
	s.activate(0)
	tt.Frame()
	if s.repositoryCount() != maxRepositoryTabs {
		t.Fatalf("the window holds %d repositories, want %d", s.repositoryCount(), maxRepositoryTabs)
	}

	// With the picker present, + shows it rather than adding an eleventh page.
	s.showHome()
	tt.Frame()
	if len(s.tabs) != maxRepositoryTabs+1 || s.activeTab().repository() {
		// showHome activates the picker, which is not a repository.
		t.Fatalf("+ did not show the existing picker: %d tabs", len(s.tabs))
	}

	// Closing the picker leaves the cap with no room for another.
	s.closeTab(s.tabs[0])
	tt.Frame()
	if s.hasHome() || len(s.tabs) != maxRepositoryTabs {
		t.Fatalf("the picker did not close: %d tabs", len(s.tabs))
	}
	s.showHome()
	if len(s.tabs) != maxRepositoryTabs {
		t.Fatalf("a page was added past the cap: %d tabs", len(s.tabs))
	}
	if a := s.activeApp(); a == nil || len(a.pendingToasts) == 0 {
		t.Fatal("the cap was refused without telling the user")
	}

	// With no picker to show and no room for another repository, + does
	// nothing rather than opening an eleventh page.
	tt.Frame()
	if err := tt.Click("New tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.tabs) != maxRepositoryTabs || s.hasHome() {
		t.Fatalf("the disabled + still opened a page: %d tabs", len(s.tabs))
	}
}

// A tab's menu names its repository; the pill and the picker's card take the
// one name, and it is written down for the next start.
func TestShellTabMenuRenamesTheRepository(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	openShellRepo(t, s, tt, updated, "/work/project")
	repo := s.activeTab()
	if err := tt.Click("New tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	picker := s.activeTab()

	if err := tt.RightClick("project"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Rename tab") {
		t.Fatalf("the tab menu lost Rename tab: %v", tt.Texts())
	}
	if err := tt.Click("Rename tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if s.activeTab() != repo {
		t.Fatalf("renaming did not bring the page forward: %s", s.tabTitles())
	}
	if !repo.app.showRenameRecent || !repo.app.renameRecentFromTab {
		t.Fatal("Rename tab opened no tab rename modal")
	}
	for _, want := range []string{"Rename tab", "Tab name", "Save name"} {
		if !tt.HasText(want) {
			t.Fatalf("the rename modal is missing %q: %v", want, tt.Texts())
		}
	}

	tt.Type("Client site")
	tt.Frame()
	if err := tt.Click("Save name"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()

	if got := repo.title(); got != "Client site" {
		t.Fatalf("the pill still shows %q", got)
	}
	if got := picker.app.recentNameFor("/work/project"); got != "Client site" {
		t.Fatalf("the picker's copy kept %q, want Client site", got)
	}
	if err := tt.Click("Home"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Client site") {
		t.Fatalf("the card did not take the tab's name: %v", tt.Texts())
	}
	stored := loadRecentRepositories(s.pathUserData)
	if len(stored) == 0 || stored[0].Name != "Client site" || stored[0].Path != "/work/project" {
		t.Fatalf("the name did not reach the store: %+v", stored)
	}
}

// Naming the card in the picker names the open page too: the one repository
// has one label wherever it shows.
func TestShellCardRenameReachesTheTab(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	openShellRepo(t, s, tt, updated, "/work/project")
	repo := s.activeTab()
	if err := tt.Click("New tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if err := tt.RightClick("/work/project"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Rename…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("Client site")
	tt.Frame()
	if err := tt.Click("Save name"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()

	if got := repo.title(); got != "Client site" {
		t.Fatalf("the tab kept %q, want the card's name", got)
	}
}

// A tab's right-click menu pins it, closes the others, and closes the pages to
// its right, as Chrome's does.
func TestShellTabMenuPinsAndCloses(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	first := readyShellTab(s, "/work/first")
	second := readyShellTab(s, "/work/second")
	s.activate(s.indexOf(second))
	tt.Frame()

	if err := tt.RightClick("second"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{
		"Pin tab", "Close tab", "Close other tabs", "Close tabs to the right",
		"Close all tabs", "Copy repository path",
	} {
		if !tt.HasText(label) {
			t.Errorf("the tab menu lost %q: %v", label, tt.Texts())
		}
	}

	if err := tt.Click("Pin tab"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !second.pinned || s.tabs[0] != second {
		t.Fatalf("pinning did not move the tab to the front: %v", s.tabTitles())
	}
	if s.activeTab() != second {
		t.Fatal("pinning changed the page on screen")
	}

	// Close other tabs from the unpinned page: the pinned one stays.
	if err := tt.RightClick("first"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Close other tabs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.tabs) != 2 || s.hasHome() || s.tabs[0] != second || s.tabs[1] != first {
		t.Fatalf("close other tabs kept %v", s.tabTitles())
	}

	// Close tabs to the right from the pinned page.
	if err := tt.RightClick("second"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Close tabs to the right"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.tabs) != 1 || s.tabs[0] != second || s.activeTab() != second {
		t.Fatalf("close tabs to the right left %v", s.tabTitles())
	}
}

// Closing the last repository leaves the picker in its place, so a window
// never ends up with no page.
func TestShellClosingTheLastPageLeavesThePicker(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	repo := readyShellTab(s, "/work/first")
	s.closeTab(s.tabs[0]) // the picker, with another page left
	tt.Frame()
	if len(s.tabs) != 1 || s.activeTab() != repo {
		t.Fatalf("closing the picker left %v", s.tabTitles())
	}

	s.closeTab(repo)
	tt.Frame()
	if len(s.tabs) != 1 || s.tabs[0].repository() {
		t.Fatalf("closing the last repository did not leave the picker: %v", s.tabTitles())
	}
	if !tt.HasText("Open a repository") {
		t.Fatalf("the picker is not on screen after closing: %v", tt.Texts())
	}
}

// The tab keys add a picker, move through the pages, and close the active one.
func TestShellTabShortcuts(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	openShellRepo(t, s, tt, updated, "/work/project")

	// There is no picker beside the repository, so Ctrl+T adds one.
	tt.Key(ui.Cmd, ui.KeyT)
	tt.Frame()
	if len(s.tabs) != 2 || s.activeTab().repository() {
		t.Fatalf("Ctrl+T did not open the picker: %v", s.tabTitles())
	}

	// Ctrl+Tab moves to the next page and wraps.
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Frame()
	if !s.activeTab().repository() {
		t.Fatalf("Ctrl+Tab did not move to the repository: %v", s.tabTitles())
	}
	tt.Key(ui.Ctrl, ui.KeyTab)
	tt.Frame()
	if s.activeTab().repository() {
		t.Fatalf("Ctrl+Tab did not wrap to the picker: %v", s.tabTitles())
	}

	// Ctrl+W closes the picker, leaving the repository.
	tt.Key(ui.Cmd, ui.KeyW)
	tt.Frame()
	if len(s.tabs) != 1 || !s.activeTab().repository() {
		t.Fatalf("Ctrl+W did not close the picker: %v", s.tabTitles())
	}
	// The last repository gives way to a fresh picker.
	tt.Key(ui.Cmd, ui.KeyW)
	tt.Frame()
	if len(s.tabs) != 1 || s.tabs[0].repository() {
		t.Fatalf("Ctrl+W did not leave the picker: %v", s.tabTitles())
	}
}

// The window shares its preferences: a theme chosen in one page reaches every
// page the window switches to.
func TestShellSharesSettingsAcrossPages(t *testing.T) {
	t.Cleanup(func() { currentLanguage = defaultLanguage })
	s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	first := readyShellTab(s, "/work/first")
	second := readyShellTab(s, "/work/second")
	s.activate(s.indexOf(first))
	tt.Frame()

	first.app.settings.Theme = "Dracula"
	first.app.settings.Language = "zh-Hans"
	s.activate(s.indexOf(second))
	tt.Frame()
	if second.app.settings.Theme != "Dracula" {
		t.Fatalf("the second page kept theme %q", second.app.settings.Theme)
	}
	if s.settings.Theme != "Dracula" {
		t.Fatalf("the window kept theme %q", s.settings.Theme)
	}
	if !tt.HasText("主页") {
		t.Fatalf("the chrome did not take the chosen language: %v", tt.Texts())
	}
}

// The window's own controls are labels first, and clicking them without a
// window (as a test builds one) must be harmless.
func TestShellWindowControlsAreDrawn(t *testing.T) {
	_, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	for _, label := range []string{"Minimize window", "Maximize window", "Full screen", "Close window"} {
		if _, found := tt.Find(label); !found {
			t.Errorf("the window lost the %q control", label)
		}
		if err := tt.Click(label); err != nil {
			t.Fatalf("click %q: %v", label, err)
		}
	}
}

// On macOS the system keeps its traffic lights on the left, so the bar leaves
// them their room and draws no controls of its own.
func TestShellMakesRoomForMacOSWindowControls(t *testing.T) {
	was := systemWindowControls
	systemWindowControls = true
	defer func() { systemWindowControls = was }()

	_, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	tt.SetTitleBar(ui.TitleBar{Height: 28, Left: 72})
	for _, label := range []string{"Minimize window", "Maximize window", "Full screen", "Close window"} {
		if _, found := tt.Find(label); found {
			t.Errorf("the macOS bar still draws the %q control", label)
		}
	}
	mark, found := tt.Find("TanGit home")
	if !found {
		t.Fatal("the macOS bar lost the app mark")
	}
	if mark.X < 72 {
		t.Errorf("the app mark starts at %v, inside the traffic lights' room of 72", mark.X)
	}
	if plus, found := tt.Find("New tab"); found {
		if width := float32(tt.Image().Bounds().Dx()); plus.X+plus.W > width {
			t.Errorf("the + runs past the window: %v+%v > %v", plus.X, plus.W, width)
		}
	}
}

// Window-relative overlays leave the title bar and its divider alone on macOS,
// where AppKit draws the traffic lights above them. Other platforms keep the
// overlays at the window's top edge, as before.
func TestShellOverlaysRespectNativeWindowControls(t *testing.T) {
	was := systemWindowControls
	defer func() { systemWindowControls = was }()

	for _, test := range []struct {
		name   string
		native bool
	}{
		{name: "macOS", native: true},
		{name: "Linux and Windows", native: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			systemWindowControls = test.native
			s, tt, _ := newShellTester(t, repository.Snapshot{Root: "/work/project"})
			tab := readyShellTab(s, "/work/project")
			s.activate(s.indexOf(tab))
			a := tab.app
			tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 72})
			const chromeBottom = float32(53)

			assertClear := func(label string) {
				t.Helper()
				control, found := tt.Find(label)
				if !found {
					t.Fatalf("the overlay has no %q control", label)
				}
				if test.native && control.Y < chromeBottom {
					t.Errorf("%q starts at y=%v, inside the native title bar ending at %v", label, control.Y, chromeBottom)
				}
				if !test.native && control.Y >= chromeBottom {
					t.Errorf("%q starts at y=%v, retaining the macOS-only top inset", label, control.Y)
				}
			}

			a.fileHistoryOpen = true
			a.fileHistoryPath = "one.txt"
			tt.Frame()
			assertClear("Close file history")

			a.closeFileHistory()
			a.diffFullscreen = true
			tt.Frame()
			assertClear("Close full-window diff")
		})
	}
}

// The window asks the platform for its controls: macOS keeps its traffic
// lights, and Linux and Windows leave them to the bar.
func TestWindowOptionsFollowThePlatformControls(t *testing.T) {
	was := systemWindowControls
	defer func() { systemWindowControls = was }()

	systemWindowControls = true
	mac := appWindowOptions(nil)
	if mac.HideWindowButtons {
		t.Error("macOS asked for its traffic lights to be hidden")
	}
	if mac.TrafficLightPosition == nil {
		t.Fatal("macOS did not place its traffic lights in the bar")
	}
	if y := mac.TrafficLightPosition.Y; y < 0 || float32(y)+macOSWindowButtonHeight > titleBarHeight {
		t.Errorf("the traffic lights' Y %d falls outside the %v-tall bar", y, titleBarHeight)
	}

	systemWindowControls = false
	other := appWindowOptions(nil)
	if !other.HideWindowButtons {
		t.Error("the app's own controls did not replace the system's")
	}
	if other.TrafficLightPosition != nil {
		t.Error("a platform without traffic lights was given a position for them")
	}
}

// tabTitles names the pages for a failure message.
func (s *shell) tabTitles() string {
	titles := make([]string, 0, len(s.tabs))
	for _, t := range s.tabs {
		title := t.title()
		if t.pinned {
			title += "*"
		}
		titles = append(titles, title)
	}
	return strings.Join(titles, ", ")
}
