package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// The launch card and the repository history are one aligned column at both
// desktop and narrow widths, and the launch card starts near the top.
func TestPickerLaunchAndRecentSectionsAreAligned(t *testing.T) {
	for _, width := range []int{1280, 720} {
		t.Run(fmt.Sprintf("%dpx", width), func(t *testing.T) {
			a := newApp(fakeOpener{})
			a.recentRepositories = []recentRepository{{Path: "/w/project", LastOpened: timeNow()}}
			tt := ui.NewTester(a.view, width, 800)
			tt.Frame()

			hero, found := tt.Find("Open a repository")
			if !found {
				t.Fatal("the launch card is missing")
			}
			recent, found := tt.Find("Recent repositories")
			if !found {
				t.Fatal("the repository history is missing")
			}
			// The heading sits inside the launch card's border and 22-DIP
			// padding; the history heading starts at the shared column's left edge.
			if got := hero.X - recent.X; got != 23 {
				t.Fatalf("the sections have different widths or alignment: launch inset=%v hero=%+v recent=%+v", got, hero, recent)
			}
			if hero.Y >= 180 {
				t.Fatalf("the launch card starts too far down the page: %+v", hero)
			}
		})
	}
}

// While a repository opens, the picker is not shown as a working form: the
// whole page becomes one loading screen, with a way back if it takes too long.
func TestPickerShowsOnlyTheLoadingPageWhileOpening(t *testing.T) {
	release := make(chan struct{})
	a := newApp(fakeOpener{
		release: release,
		result:  openResult{snapshot: repository.Snapshot{Root: "/w/slow", Branch: "main"}},
	})
	a.state = stateEmpty
	a.recentRepositories = []recentRepository{{Path: "/w/slow", LastOpened: timeNow()}}
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	if err := tt.Click("/w/slow"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	texts := strings.Join(tt.Texts(), " ")
	for _, gone := range []string{"Open a repository", "Browse…", "Recent repositories"} {
		if strings.Contains(texts, gone) {
			t.Fatalf("the picker still shows %q while opening: %v", gone, tt.Texts())
		}
	}
	if a.state != stateLoading || !tt.HasText("Opening repository") {
		t.Fatalf("the loading page is missing: state=%v texts=%v", a.state, tt.Texts())
	}

	// Cancel stops the open and puts the picker back.
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.state != stateEmpty {
		t.Fatalf("Cancel left state %v, want the picker", a.state)
	}
	if !strings.Contains(strings.Join(tt.Texts(), " "), "Open a repository") {
		t.Fatalf("the picker did not come back after Cancel: %v", tt.Texts())
	}

	// Escape is the keyboard way back from the loading page.
	a.state = stateLoading
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.state != stateEmpty {
		t.Fatalf("Escape left state %v, want the picker", a.state)
	}
	close(release)
}

// A card's trash button takes that repository out of the history, beside the
// context menu that does the same.
func TestRecentCardTrashRemovesTheEntry(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/w/keep", LastOpened: timeNow()},
		recentRepository{Path: "/w/hide", LastOpened: timeNow()},
	)
	if err := tt.Click("Remove /w/hide from the recent list"); err != nil {
		t.Fatalf("click the trash button: %v", err)
	}
	// The removal is deferred to the next build pass.
	tt.Frame()
	tt.Frame()
	texts := strings.Join(tt.Texts(), " ")
	if strings.Contains(texts, "/w/hide") {
		t.Fatalf("the trash button did not remove the entry: %v", tt.Texts())
	}
	if !strings.Contains(texts, "/w/keep") {
		t.Fatalf("the other entry went with it: %v", tt.Texts())
	}
	if len(a.recentRepositories) != 1 {
		t.Fatalf("history not updated: %+v", a.recentRepositories)
	}
	if a.state != stateEmpty {
		t.Fatalf("removing an entry must not open it: state=%v", a.state)
	}
}

// A folder with no Git repository gets an offer to create one there, and the
// offer works: initialize, then open the repository just made.
func TestPlainFolderOffersInitialization(t *testing.T) {
	opener := &recordedOpener{
		notRepository: true,
		snapshot:      repository.Snapshot{Root: "/w/plain", Branch: "main"},
	}
	a := newApp(opener)
	a.state = stateEmpty
	a.recentRepositories = []recentRepository{{Path: "/w/plain", LastOpened: timeNow()}}
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	if err := tt.Click("/w/plain"); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		if a.state == stateEmpty && a.offerInitPath != "" {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	tt.Frame()
	if a.offerInitPath != "/w/plain" {
		t.Fatalf("a plain folder raised no offer: state=%v offer=%q err=%q",
			a.state, a.offerInitPath, a.errMessage)
	}
	texts := strings.Join(tt.Texts(), " ")
	for _, want := range []string{"This folder is not a Git repository", "/w/plain", "Initialize repository"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the offer does not say %q: %v", want, tt.Texts())
		}
	}

	if err := tt.Click("Initialize repository"); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		if a.state == stateReady && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	if a.state != stateReady {
		t.Fatalf("initializing did not open the repository: state=%v err=%q", a.state, a.errMessage)
	}
	if len(opener.inited) != 1 || opener.inited[0] != "/w/plain" {
		t.Fatalf("initialized %v, want the offered folder", opener.inited)
	}
	if a.path != "/w/plain" {
		t.Fatalf("opened %q, want the initialized folder", a.path)
	}
}

// Not now leaves the folder alone and keeps the picker on screen.
func TestPlainFolderCanBeLeftAlone(t *testing.T) {
	a, tt, _ := recentTester(t, recentRepository{Path: "/w/plain", LastOpened: timeNow()})
	a.offerInitPath = "/w/plain"
	tt.Frame()
	if err := tt.Click("Not now"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.offerInitPath != "" {
		t.Fatalf("the offer survived Not now: %q", a.offerInitPath)
	}
	if a.state != stateEmpty {
		t.Fatalf("Not now changed the state: %v", a.state)
	}
	if !strings.Contains(strings.Join(tt.Texts(), " "), "Open a repository") {
		t.Fatalf("the picker is gone: %v", tt.Texts())
	}
}

// A failed initialization explains itself instead of leaving the page blank.
func TestInitializationFailureIsShown(t *testing.T) {
	opener := &recordedOpener{
		notRepository: true,
		initErr:       errors.New("permission denied"),
	}
	a := newApp(opener)
	a.offerInitPath = "/w/plain"
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	if err := tt.Click("Initialize repository"); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		if a.state == stateError && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	tt.Frame()
	if a.state != stateError || !strings.Contains(a.errMessage, "permission denied") {
		t.Fatalf("init failure state=%v message=%q", a.state, a.errMessage)
	}
	if !strings.Contains(strings.Join(tt.Texts(), " "), "permission denied") {
		t.Fatalf("the failure is not on screen: %v", tt.Texts())
	}
}

// Closing a repository returns this window to the picker, where another one
// can be opened, and clears the old one's snapshot.
func TestClosingRepositoryReturnsToThePicker(t *testing.T) {
	h := menuTestApp(t)
	h.app.closeRepository()
	h.tester.Frame()
	if h.app.state != stateEmpty {
		t.Fatalf("closing left state %v, want the picker", h.app.state)
	}
	if h.app.session != nil || h.app.snapshot.Root != "" {
		t.Fatalf("the closed repository is still held: session=%v root=%q",
			h.app.session != nil, h.app.snapshot.Root)
	}
	if h.app.path != "" {
		t.Fatalf("the closed repository's path survived: %q", h.app.path)
	}
	texts := strings.Join(h.texts(), " ")
	for _, want := range []string{"Open a repository", "/work/project"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the picker does not show %q after closing: %v", want, h.texts())
		}
	}

	// The same window opens another repository afterwards.
	h.app.path = "/work/second"
	openFixture(t, h.tester)
	h.settle(t)
	if h.app.state != stateReady {
		t.Fatalf("the window could not open another repository: state=%v err=%q",
			h.app.state, h.app.errMessage)
	}
}

// A card's menu opens its repository in a tab of its own; the picker no
// longer grows whole windows, which the window's own + and tabs replace.
func TestPickerOffersNewTab(t *testing.T) {
	a, tt, _ := recentTester(t, recentRepository{Path: "/w/one", LastOpened: timeNow()})
	var opened []string
	a.openTab = func(path string) { opened = append(opened, path) }
	tt.Frame()

	if tt.HasText("New window") {
		t.Fatalf("the picker still offers a new window: %v", tt.Texts())
	}
	if err := tt.RightClick("/w/one"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Open in new tab") {
		t.Fatalf("a card's menu offers no new tab: %v", tt.Texts())
	}
	if err := tt.Click("Open in new tab"); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || opened[0] != "/w/one" {
		t.Fatalf("the card opened %v, want its repository in a new tab", opened)
	}
}

// Daily Git and utility actions have direct buttons. Settings stays in the
// workspace header; terminal, shortcuts and the command log share the bottom
// island. The terminal icon is the only entry to its persistent bottom dock.
func TestWorkspaceUtilityButtonsHaveDirectHomes(t *testing.T) {
	h := menuTestApp(t)
	for _, label := range []string{"Fetch", "Pull", "Push", "Refresh", "Theme", "Settings"} {
		if _, found := h.tester.Find(label); !found {
			t.Errorf("the workspace lost the header action %q", label)
		}
	}
	settings, _ := h.tester.Find("Settings")
	if more, found := h.tester.Find("More actions"); found && more.Y == settings.Y {
		t.Error("the workspace header still has the redundant More actions button")
	}
	log, found := h.tester.Find("Command log")
	if !found {
		t.Fatal("the status bar lost the command log")
	}
	terminal, found := h.tester.Find("Toggle terminal")
	if !found {
		t.Error("the status bar lost the docked Terminal button")
	} else if terminal.Y+terminal.H/2 != log.Y+log.H/2 {
		t.Errorf("the Terminal button is not centered in the bottom island: button=%+v log=%+v", terminal, log)
	}
	for _, label := range []string{"Keyboard shortcuts"} {
		button, found := h.tester.Find(label)
		if !found {
			t.Errorf("the status bar lost %q", label)
			continue
		}
		if button.Y+button.H/2 != log.Y+log.H/2 {
			t.Errorf("%q is not centered in the bottom island: button=%+v log=%+v", label, button, log)
		}
	}
}

func TestWorkspaceUtilityButtonsExecute(t *testing.T) {
	h := menuTestApp(t)

	if err := h.tester.Click("Keyboard shortcuts"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.keybindingsOpen {
		t.Fatal("the status-bar shortcut button did not open the reference")
	}
	if err := h.tester.Click("Done"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()

	if err := h.tester.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.settingsOpen {
		t.Fatal("the header settings button did not open settings")
	}
}
