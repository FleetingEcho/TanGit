package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/workspace"
)

type blockingWorktreeOpener struct {
	started chan<- string
	release <-chan struct{}
}

func (blockingWorktreeOpener) Init(context.Context, string) error { return nil }

func (o blockingWorktreeOpener) Open(ctx context.Context, path string) (repository.Session, error) {
	o.started <- path
	select {
	case <-o.release:
		return pathOpener{}.Open(ctx, path)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// The worktree action targets the new app, never the main tab: no terminal is
// constructed before installation supplies the selected root, and revisiting
// the action reuses both the repository tab and its existing shell.
func TestWorktreeTerminalWaitsForTargetTabAndKeepsIsolation(t *testing.T) {
	s, tester, updated := newShellTester(t, repository.Snapshot{
		Root:      "/work/project",
		Worktrees: []repository.Worktree{{Name: "feature", Path: "/work/feature", Branch: "feature"}},
	})
	t.Cleanup(s.shutdown)
	openShellRepo(t, s, tester, updated, "/work/main")
	mainApp := s.activeApp()
	mainApp.selectedWorktreeRow, mainApp.selectedWorktree = 0, 0

	started := make(chan string, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	s.opener = blockingWorktreeOpener{started: started, release: release}

	originalTerminal, originalLaunch := newTerminal, launchWorktreeTerminal
	t.Cleanup(func() {
		newTerminal = originalTerminal
		launchWorktreeTerminal = originalLaunch
	})
	terminalCalls := make(chan terminal.Options, 2)
	peers := make(chan monitoredTerminalPeer, 1)
	newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
		terminalCalls <- options
		return newMonitoredTerminal(options, peers)
	}
	systemCalls := 0
	launchWorktreeTerminal = func(string) error {
		systemCalls++
		return nil
	}

	mainApp.openSelectedWorktreeTerminal()
	s.applyPending()
	select {
	case path := <-started:
		if path != "/work/feature" {
			t.Fatalf("opener received %q, want the selected worktree", path)
		}
	case <-time.After(time.Second):
		t.Fatal("selected worktree did not start opening")
	}
	if len(s.tabs) != 2 {
		t.Fatalf("worktree terminal created %d repository tabs, want 2", len(s.tabs))
	}
	target := s.activeApp()
	if target == mainApp || target.state != stateLoading || !target.openTerminalOnReady {
		t.Fatalf("target tab is not waiting for its repository: state=%v pending=%v", target.state, target.openTerminalOnReady)
	}
	if len(target.term.sessions) != 0 || len(terminalCalls) != 0 {
		t.Fatal("terminal started before the selected repository was ready")
	}
	if mainApp.term.panelOpen || len(mainApp.term.sessions) != 0 {
		t.Fatal("worktree action opened a terminal in the main tab")
	}

	unblock()
	for range 12 {
		if target.state == stateReady && target.activeTerminal() != nil && target.activeTerminal().pane != nil {
			break
		}
		updated.waitForUpdate(t)
	}
	if target.state != stateReady || !target.term.panelOpen || len(target.term.sessions) != 1 || target.activeTerminal().pane == nil {
		t.Fatalf("worktree terminal did not start after ready: state=%v term=%+v", target.state, target.term)
	}
	if target.openTerminalOnReady {
		t.Fatal("successful terminal startup retained its pending request")
	}
	options := <-terminalCalls
	if options.Dir != "/work/feature" {
		t.Fatalf("terminal directory = %q, want selected worktree", options.Dir)
	}
	if len(options.Command) != 0 {
		t.Fatalf("opening a worktree terminal executed an unexpected command: %v", options.Command)
	}
	peer := <-peers
	t.Cleanup(func() { _ = peer.Close() })
	if mainApp.term.panelOpen || len(mainApp.term.sessions) != 0 || mainApp.snapshot.Root != "/work/main" {
		t.Fatal("opening the worktree terminal changed the main tab")
	}
	if systemCalls != 0 {
		t.Fatal("integrated worktree action unexpectedly opened the system terminal")
	}

	s.activate(0)
	mainApp.openSelectedWorktreeTerminal()
	s.applyPending()
	if s.activeApp() != target || len(s.tabs) != 2 || len(target.term.sessions) != 1 || len(terminalCalls) != 0 {
		t.Fatalf("second action did not reuse the worktree tab and shell: tabs=%d sessions=%d constructors=%d",
			len(s.tabs), len(target.term.sessions), len(terminalCalls))
	}
}

func TestWorktreeTerminalPreservesRemoteWorkspace(t *testing.T) {
	a := &app{
		location: workspace.Location{Kind: workspace.SSH, ProfileID: "prod", Root: "/srv/main"},
		path:     "ssh://prod/srv/main",
		snapshot: repository.Snapshot{Root: "/srv/main", Worktrees: []repository.Worktree{
			{Name: "feature", Path: "/srv/feature", Branch: "feature"},
		}},
	}
	var opened string
	a.openTerminalTab = func(path string) { opened = path }

	a.openSelectedWorktreeTerminal()

	if opened != "ssh://prod/srv/feature" {
		t.Fatalf("terminal worktree identity = %q, want remote target", opened)
	}
	if a.path != "ssh://prod/srv/main" || a.location.Root != "/srv/main" || a.term.panelOpen {
		t.Fatal("requesting a remote worktree terminal replaced the current workspace")
	}
}

func TestWorktreeTerminalButtonsUseSelection(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionWorktrees
	h.app.snapshot.Worktrees = []repository.Worktree{{Name: "feature", Path: "/work/feature", Branch: "feature"}}
	h.app.selectedWorktreeRow, h.app.selectedWorktree = 0, 0
	integratedCalls := 0
	h.app.openTerminalTab = func(path string) {
		integratedCalls++
		if path != "/work/feature" {
			t.Errorf("integrated terminal path = %q", path)
		}
	}
	systemCalls := 0
	original := launchWorktreeTerminal
	t.Cleanup(func() { launchWorktreeTerminal = original })
	launchWorktreeTerminal = func(path string) error {
		systemCalls++
		if path != "/work/feature" {
			t.Errorf("system terminal path = %q", path)
		}
		return nil
	}

	h.tester.Frame()
	if err := h.tester.Click("Open worktree terminal"); err != nil {
		t.Fatal(err)
	}
	if integratedCalls != 1 || systemCalls != 0 {
		t.Fatalf("detail action made %d integrated and %d system calls", integratedCalls, systemCalls)
	}
	h.rightClick(t, "feature")
	h.requireMenuLabels(t, "Open worktree terminal", "Open system terminal here")
	h.chooseMenuItem(t, "Open system terminal here")
	if integratedCalls != 1 || systemCalls != 1 {
		t.Fatalf("row action made %d integrated and %d system calls", integratedCalls, systemCalls)
	}
}

func TestFailedOpenCancelsPendingWorktreeTerminal(t *testing.T) {
	a := newApp(fakeOpener{})
	a.state = stateLoading
	a.openTerminalWhenReady()
	if !a.openTerminalOnReady {
		t.Fatal("loading repository did not retain its terminal request")
	}

	a.setOpenError("/work/feature", a.generation, errors.New("open failed"))

	if a.openTerminalOnReady || a.term.panelOpen || len(a.term.sessions) != 0 {
		t.Fatal("failed repository open left a terminal request behind")
	}
}

func TestWorktreeSystemTerminalRemainsAvailable(t *testing.T) {
	a := &app{
		location: workspace.Location{Kind: workspace.Local, Root: "/work/main"},
		snapshot: repository.Snapshot{Worktrees: []repository.Worktree{{Path: "/work/feature"}}},
	}
	var launched string
	original := launchWorktreeTerminal
	t.Cleanup(func() { launchWorktreeTerminal = original })
	launchWorktreeTerminal = func(path string) error {
		launched = path
		return nil
	}

	a.openSelectedWorktreeSystemTerminal()

	if launched != "/work/feature" {
		t.Fatalf("system terminal directory = %q, want selected worktree", launched)
	}
}
