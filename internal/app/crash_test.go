package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// A fault while a frame builds must not close the window: the app falls back
// to its safe screen, shows what happened, and can draw the app again.
func TestViewContainsBuildFault(t *testing.T) {
	a := newApp(fakeOpener{})
	a.state = stateEmpty
	updated := newUpdates()
	a.update = updated.update

	fired := false
	uiFaultHook = func() {
		if !fired {
			fired = true
			panic("boom while building")
		}
	}
	t.Cleanup(func() { uiFaultHook = nil })

	tt := ui.NewTester(a.view, 1280, 800)
	// The first frame hits the fault; the boundary remembers it.
	tt.Frame()
	if a.crash == "" {
		t.Fatal("the build fault was not recorded")
	}
	// The next frame draws the safe screen instead of the app.
	tt.Frame()
	if !tt.HasText("This window hit an unexpected problem") {
		t.Fatalf("the safe screen is missing: %v", tt.Texts())
	}
	if !strings.Contains(strings.Join(tt.Texts(), " "), "boom while building") {
		t.Fatalf("the safe screen does not name the fault: %v", tt.Texts())
	}

	// With the fault cleared, Try again draws the app, not the safe screen.
	if err := tt.Click("Try again"); err != nil {
		t.Fatalf("click Try again: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if a.crash != "" {
		t.Fatalf("Try again left the fault in place: %q", a.crash)
	}
	if !tt.HasText("Open a repository") {
		t.Fatalf("the app did not come back: %v", tt.Texts())
	}
}

// Back to repositories leaves the safe screen and the open repository, so a
// fault in one repository does not trap the window in it.
func TestCrashScreenReturnsToPicker(t *testing.T) {
	a := newApp(fakeOpener{})
	a.state = stateReady
	a.session = fakeSession{}
	a.snapshot = repository.Snapshot{Root: "/work/project", Branch: "main"}
	a.crash = "boom"

	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("Back to repositories"); err != nil {
		t.Fatalf("click Back to repositories: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if a.crash != "" {
		t.Fatalf("the safe screen stayed up: %q", a.crash)
	}
	if a.state != stateEmpty || a.session != nil {
		t.Fatalf("the repository stayed open: state=%v", a.state)
	}
	if !tt.HasText("Open a repository") {
		t.Fatalf("the picker did not come back: %v", tt.Texts())
	}
}

// A panic on a background read becomes a failed read: the pane stops loading,
// shows why, and the window stays up.
func TestBackgroundFaultBecomesReadError(t *testing.T) {
	a := newApp(fakeOpener{})
	updated := newUpdates()
	a.update = updated.update
	a.diffLoading = true

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer a.recoverBackground()
		panic("git exploded")
	}()
	<-done
	updated.waitForUpdate(t)

	if a.diffLoading {
		t.Fatal("the failed read left its pane loading")
	}
	if a.diffError != "git exploded" {
		t.Fatalf("the pane error is %q, want the fault reason", a.diffError)
	}
}

// A stray negative lane wraps into the palette instead of indexing past it.
func TestPaletteIndexWrapsNegative(t *testing.T) {
	for _, lane := range []int{-1, -7, -30, 0, 3, 30} {
		if got := paletteIndex(lane, 7); got < 0 || got >= 7 {
			t.Errorf("paletteIndex(%d, 7) = %d, want a palette index", lane, got)
		}
	}
	if got, want := paletteIndex(-1, 7), 6; got != want {
		t.Errorf("paletteIndex(-1, 7) = %d, want %d", got, want)
	}
	if got := paletteIndex(5, 0); got != 0 {
		t.Errorf("paletteIndex(5, 0) = %d, want 0", got)
	}
}

// The remote-branch list asks for keys before its selection is known to be
// valid; an out-of-range read must report an empty row rather than panic.
func TestRemoteBranchRowsGuardTheirSelection(t *testing.T) {
	a := newApp(fakeOpener{})
	a.snapshot = repository.Snapshot{Remotes: []repository.Remote{{
		Name: "origin",
		Branches: []repository.RemoteBranch{
			{Name: "main"},
			{Name: "feature"},
		},
	}}}

	a.selectedRemote = 0
	if got := a.remoteBranches.Key(1); got != "feature" {
		t.Fatalf("Key(1) = %v, want feature", got)
	}
	if got := a.remoteBranches.Label(0); got != "main" {
		t.Fatalf("Label(0) = %q, want main", got)
	}
	// No remote chosen, or a row outside its branches: empty, no panic.
	a.selectedRemote = -1
	if got := a.remoteBranches.Key(0); got != "" {
		t.Fatalf("Key(0) with no remote = %v, want empty", got)
	}
	a.selectedRemote = 9
	if got := a.remoteBranches.Label(0); got != "" {
		t.Fatalf("Label(0) with a stale remote = %q, want empty", got)
	}
	a.selectedRemote = 0
	if got := a.remoteBranches.Label(2); got != "" {
		t.Fatalf("Label(2) past the branches = %q, want empty", got)
	}
}

// The remotes list reports a change before its selection is known to be
// valid: resetting the branch choice must not index a stale remote.
func TestRemoteListChangeGuardsItsSelection(t *testing.T) {
	a := newApp(fakeOpener{})
	a.snapshot = repository.Snapshot{Remotes: []repository.Remote{{
		Name:     "origin",
		Branches: []repository.RemoteBranch{{Name: "main"}},
	}}}

	a.selectedRemote, a.selectedRemoteBranch = 0, -1
	a.resetRemoteBranchChoice()
	if a.selectedRemoteBranch != 0 {
		t.Fatalf("branch choice = %d, want the remote's first branch", a.selectedRemoteBranch)
	}

	// No remote chosen, or a stale index: no branch, no panic.
	for _, selected := range []int{-1, 9} {
		a.selectedRemote, a.selectedRemoteBranch = selected, 3
		a.resetRemoteBranchChoice()
		if a.selectedRemoteBranch != -1 {
			t.Fatalf("branches[%d] choice = %d, want none", selected, a.selectedRemoteBranch)
		}
	}

	// A remote without branches has nothing to choose.
	a.snapshot.Remotes = []repository.Remote{{Name: "origin"}}
	a.selectedRemote, a.selectedRemoteBranch = 0, 2
	a.resetRemoteBranchChoice()
	if a.selectedRemoteBranch != -1 {
		t.Fatalf("an empty remote chose branch %d", a.selectedRemoteBranch)
	}
}

// A fault in any background read stops the pane it was filling and names the
// reason, so no pane is left loading forever and the way to retry shows.
func TestBackgroundFaultStopsEveryPane(t *testing.T) {
	tests := []struct {
		name    string
		set     func(a *app)
		loading func(a *app) bool
		message func(a *app) string
	}{
		{
			name:    "the commit page",
			set:     func(a *app) { a.commitsLoading = true },
			loading: func(a *app) bool { return a.commitsLoading },
			message: func(a *app) string { return a.commitsError },
		},
		{
			name:    "the author list",
			set:     func(a *app) { a.authorsLoading = true },
			loading: func(a *app) bool { return a.authorsLoading },
			message: func(a *app) string { return a.authorsError },
		},
		{
			name:    "the pane's file editor",
			set:     func(a *app) { a.fileEditLoading = true },
			loading: func(a *app) bool { return a.fileEditLoading },
			message: func(a *app) string { return a.fileEditError },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := newApp(fakeOpener{})
			tc.set(a)
			a.reportBackgroundFault("boom")
			if tc.loading(a) {
				t.Fatal("the pane is still loading after the fault")
			}
			if got := tc.message(a); got != "boom" {
				t.Fatalf("the pane error is %q, want the fault reason", got)
			}
		})
	}
}
