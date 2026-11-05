package app

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type monitoredTerminalConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *monitoredTerminalConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

type monitoredTerminalPeer struct {
	net.Conn
	closed chan struct{}
}

// newMonitoredTerminal returns a real terminal over an in-memory connection.
// Closing the terminal must close the monitored side; the peer stays test-owned.
func newMonitoredTerminal(options terminal.Options, peers chan<- monitoredTerminalPeer) (*terminal.Terminal, error) {
	terminalSide, peerSide := net.Pipe()
	monitored := &monitoredTerminalConn{Conn: terminalSide, closed: make(chan struct{})}
	options.Conn = monitored
	options.NoBlink = true
	pane, err := terminal.New(options)
	if err != nil {
		_ = monitored.Close()
		_ = peerSide.Close()
		return nil, err
	}
	peers <- monitoredTerminalPeer{Conn: peerSide, closed: monitored.closed}
	return pane, nil
}

func requireTerminalClosed(t *testing.T, peer monitoredTerminalPeer) {
	t.Helper()
	select {
	case <-peer.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the terminal did not close its connection")
	}
}

func requireTerminalRunning(t *testing.T, peer monitoredTerminalPeer) {
	t.Helper()
	select {
	case <-peer.closed:
		t.Fatal("hiding or closing a sibling terminated this terminal")
	default:
	}
}

func monitoredTerminalTestApp(t *testing.T) (*harness, chan monitoredTerminalPeer, monitoredTerminalPeer) {
	t.Helper()
	original := newTerminal
	peers := make(chan monitoredTerminalPeer, terminalSessionLimit)
	newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
		return newMonitoredTerminal(options, peers)
	}
	t.Cleanup(func() { newTerminal = original })
	h := menuTestApp(t)
	h.app.openTerminal()
	h.tester.Frame()
	h.waitFor(t, "the first terminal", func() bool {
		return len(h.app.term.sessions) > 0 && h.app.term.sessions[0].pane != nil
	})
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	first := <-peers
	t.Cleanup(func() { _ = first.Close() })
	return h, peers, first
}

func splitMonitoredTerminal(t *testing.T, h *harness, peers chan monitoredTerminalPeer, below bool) monitoredTerminalPeer {
	t.Helper()
	count := h.app.terminalSessionCount()
	label := "Split terminal right"
	if below {
		label = "Split terminal below"
	}
	if err := h.tester.Click(label); err != nil {
		t.Fatalf("%v; terminal=%+v; texts=%q", err, h.app.term, h.texts())
	}
	h.tester.Frame()
	if h.app.terminalSessionCount() != count+1 {
		t.Fatalf("split did not add one terminal: %+v", h.app.term)
	}
	index := h.app.term.active
	h.waitFor(t, "the split terminal", func() bool {
		return h.app.term.sessions[index].pane != nil
	})
	h.tester.Frame()
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })
	return second
}

func TestTerminalPlusCreatesIndependentTabsAndRetainsTheirSplits(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	firstPane := h.app.term.sessions[0].pane

	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the second terminal tab", func() bool {
		return h.app.terminalSessionCount() == 2 && h.app.activeTerminal().pane != nil
	})
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })
	secondPane := h.app.activeTerminal().pane
	if secondPane == firstPane || h.app.activeTerminalGroup() != 2 {
		t.Fatalf("Plus did not create an independent second tab: %+v", h.app.term)
	}
	h.tester.Frame()
	if _, found := h.tester.Find("Select terminal 1"); found {
		t.Fatal("Plus split the first terminal instead of giving it a separate tab")
	}
	if _, found := h.tester.Find("Select terminal 2"); !found {
		t.Fatal("the new terminal tab is not selected")
	}

	third := splitMonitoredTerminal(t, h, peers, false)
	thirdPane := h.app.activeTerminal().pane
	secondGroup := h.app.activeTerminalGroup()
	secondLayout := h.app.term.layout
	if h.app.activeTerminal().groupID != secondGroup || h.app.terminalGroupSize(secondGroup) != 2 {
		t.Fatalf("the split escaped its top-level tab: %+v", h.app.term.sessions)
	}
	if err := h.tester.Click("Terminal tab 1"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Frame()
	if h.app.activeTerminal().pane != firstPane || h.app.activeTerminalGroup() != 1 {
		t.Fatalf("the first tab did not restore its shell: %+v", h.app.term)
	}
	if _, found := h.tester.Find("Select terminal 2"); found {
		t.Fatal("switching tabs left an inactive terminal bound to a view")
	}
	if err := h.tester.Click("Terminal tab 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Frame()
	if h.app.term.layout != secondLayout || h.app.activeTerminal().pane != thirdPane {
		t.Fatal("returning to a tab replaced its split layout or active shell")
	}
	if h.app.term.sessions[1].pane != secondPane {
		t.Fatal("switching tabs replaced the second shell")
	}
	for _, peer := range []monitoredTerminalPeer{first, second, third} {
		requireTerminalRunning(t, peer)
	}
	if h.tester.HasText("Handle bound to two controls in one build") {
		t.Fatal("a terminal was rendered in two tabs at once")
	}
}

func TestTerminalClosingLastActiveSplitStaysInItsTopLevelTab(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)

	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the middle terminal tab", func() bool {
		return h.app.terminalSessionCount() == 2 && h.app.activeTerminal().pane != nil
	})
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })
	secondPane := h.app.activeTerminal().pane

	third := splitMonitoredTerminal(t, h, peers, false)
	thirdPane := h.app.activeTerminal().pane
	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the last terminal tab", func() bool {
		return h.app.terminalSessionCount() == 4 && h.app.activeTerminal().pane != nil
	})
	fourth := <-peers
	t.Cleanup(func() { _ = fourth.Close() })

	if err := h.tester.Click("Terminal tab 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Frame()
	if h.app.activeTerminal().pane != thirdPane {
		t.Fatal("the middle tab did not restore its active split")
	}
	if err := h.tester.Click("Close terminal 3"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()

	active := h.app.activeTerminal()
	if active == nil || active.groupID != 2 || active.pane != secondPane {
		t.Fatalf("closing the last split switched top-level tabs: active=%+v state=%+v", active, h.app.term)
	}
	requireTerminalClosed(t, third)
	for _, peer := range []monitoredTerminalPeer{first, second, fourth} {
		requireTerminalRunning(t, peer)
	}
}

func TestTerminalDockStaysSharedAcrossWorkspacePages(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	firstPane := h.app.term.sessions[0].pane

	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the docked terminal tab", func() bool {
		return h.app.terminalSessionCount() == 2 && h.app.activeTerminal().pane != nil
	})
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })
	secondPane := h.app.activeTerminal().pane

	h.app.setWorkspaceTab(sectionHistory)
	h.tester.Frame()
	if !h.app.term.panelOpen || h.app.workspaceTab != sectionHistory ||
		h.app.term.sessions[0].pane != firstPane || h.app.activeTerminal().pane != secondPane {
		t.Fatalf("changing workspace pages replaced the docked sessions: %+v", h.app.term)
	}
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	if h.app.term.panelOpen || h.app.workspaceTab != sectionHistory || h.app.terminalSessionCount() != 2 {
		t.Fatal("the status icon did not hide the dock without closing its shells")
	}

	h.app.setWorkspaceTab(sectionCode)
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	if !h.app.term.panelOpen || h.app.terminalSessionCount() != 2 ||
		h.app.term.sessions[0].pane != firstPane || h.app.activeTerminal().pane != secondPane {
		t.Fatal("restoring the dock created different terminal sessions")
	}
	if err := h.tester.Click("Hide terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	if h.app.term.panelOpen || h.app.terminalSessionCount() != 2 {
		t.Fatal("hiding the dock closed its shared sessions")
	}
	requireTerminalRunning(t, first)
}

func TestTerminalTopLevelTabsShareHeaderAndCloseWithConfirmation(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the second top-level tab", func() bool {
		return h.app.terminalSessionCount() == 2 && h.app.activeTerminal().pane != nil
	})
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })
	third := splitMonitoredTerminal(t, h, peers, false)
	h.tester.Frame()

	header, headerFound := h.tester.Find("Terminal actions")
	tabOne, oneFound := h.tester.Find("Terminal tab 1")
	tabTwo, twoFound := h.tester.Find("Terminal tab 2")
	if !headerFound || !oneFound || !twoFound ||
		tabOne.Y+tabOne.H/2 != header.Y+header.H/2 || tabTwo.Y+tabTwo.H/2 != header.Y+header.H/2 {
		t.Fatalf("top-level tabs are not in the terminal header: header=%+v one=%+v two=%+v", header, tabOne, tabTwo)
	}
	for _, group := range []int{1, 2} {
		if _, found := h.tester.Find(fmt.Sprintf("Rename tab %d", group)); !found {
			t.Fatalf("top-level tab %d has no rename button", group)
		}
	}
	if err := h.tester.Click("Close terminal tab 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.title != tr("Close terminal tab") || !h.app.confirm.destructive {
		t.Fatalf("closing a multi-shell tab did not ask for confirmation: %+v", h.app.confirm)
	}
	if want := trf("%d terminals are still running.", 2); h.app.confirm.message != want {
		t.Fatalf("tab confirmation counted shells in other tabs: got %q, want %q", h.app.confirm.message, want)
	}
	for _, peer := range []monitoredTerminalPeer{first, second, third} {
		requireTerminalRunning(t, peer)
	}
	if err := h.tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Close terminal tab 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Close tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.terminalSessionCount() != 1 || h.app.activeTerminalGroup() != 1 || !h.app.term.panelOpen {
		t.Fatalf("confirmed tab close disturbed the other top-level tab: %+v", h.app.term)
	}
	requireTerminalRunning(t, first)
	requireTerminalClosed(t, second)
	requireTerminalClosed(t, third)
}

func TestTerminalSingleShellTabClosesWithoutConfirmation(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the second single-shell tab", func() bool {
		return h.app.terminalSessionCount() == 2 && h.app.activeTerminal().pane != nil
	})
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })

	if err := h.tester.Click("Close terminal tab 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm != nil {
		t.Fatalf("closing a single-shell tab unexpectedly asked for confirmation: %+v", h.app.confirm)
	}
	if h.app.terminalSessionCount() != 1 || h.app.activeTerminalGroup() != 1 {
		t.Fatalf("single-shell tab close disturbed the other tab: %+v", h.app.term)
	}
	requireTerminalRunning(t, first)
	requireTerminalClosed(t, second)
}

func TestTerminalTopLevelTabRenameAndContextMenu(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	firstID := h.app.term.sessions[0].id
	if label := terminalSessionLabel(&h.app.term.sessions[0]); label == "" || label == "Terminal 1" {
		t.Fatalf("the shell has a numeric tab label: %q", label)
	}
	if detail := terminalSessionDetail(&h.app.term.sessions[0]); detail == "" {
		t.Fatal("the top-level shell has no working-directory context")
	}
	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the second tab for renaming", func() bool {
		return h.app.terminalSessionCount() == 2 && h.app.activeTerminal().pane != nil
	})
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })

	// The pencil opens the same stable-ID form directly.
	if err := h.tester.Click("Rename tab 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.anyFormOpen() {
		t.Fatal("the top-level rename modal did not take ownership of text input")
	}
	if h.app.term.renameGroup != h.app.activeTerminalGroup() || h.app.term.renameID != 0 {
		t.Fatalf("rename targeted group %d/shell %d, want group %d", h.app.term.renameGroup, h.app.term.renameID, h.app.activeTerminalGroup())
	}
	h.app.closeTerminalRename()
	h.tester.Frame()

	// Right-click offers both actions and does not redirect them to tab one.
	h.rightClick(t, "Terminal tab 2")
	h.chooseMenuItem(t, "Rename tab")
	h.tester.Type("Database shell")
	h.tester.Frame()
	if err := h.tester.Click("Save name"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.term.sessions[0].name != "" || h.app.activeTerminal().name != "" ||
		h.app.term.groupNames[2] != "Database shell" || !h.tester.HasText("Database shell") {
		t.Fatalf("the context-menu rename missed its tab: groups=%+v sessions=%+v", h.app.term.groupNames, h.app.term.sessions)
	}

	h.rightClick(t, "Terminal tab 2")
	h.chooseMenuItem(t, "Close terminal tab")
	if h.app.confirm != nil {
		t.Fatalf("context-menu close confirmed a single-shell tab: %+v", h.app.confirm)
	}
	h.tester.Frame()
	if h.app.terminalSessionCount() != 1 || h.app.activeTerminal().id != firstID || h.app.term.sessions[0].name != "" {
		t.Fatalf("context-menu close disturbed the remaining tab: %+v", h.app.term)
	}
	requireTerminalRunning(t, first)
	requireTerminalClosed(t, second)
}

func TestTerminalHideAndRestoreKeepShellDimensionsStable(t *testing.T) {
	h, _, first := monitoredTerminalTestApp(t)
	pane := h.app.term.sessions[0].pane
	wantCols, wantRows := pane.Size()
	if wantCols <= 1 || wantRows <= 1 {
		t.Fatalf("the initial terminal has no useful grid: %dx%d", wantCols, wantRows)
	}
	requireSize := func(stage string) {
		t.Helper()
		if cols, rows := pane.Size(); cols != wantCols || rows != wantRows {
			t.Fatalf("%s resized the hidden shell from %dx%d to %dx%d", stage, wantCols, wantRows, cols, rows)
		}
	}

	for cycle := 1; cycle <= 3; cycle++ {
		if err := h.tester.Click("Hide terminal"); err != nil {
			t.Fatal(err)
		}
		for step := 0; step < 14; step++ {
			h.tester.Advance(20 * time.Millisecond)
			h.tester.Frame()
			requireSize(fmt.Sprintf("hide cycle %d frame %d", cycle, step))
		}
		requireTerminalRunning(t, first)
		h.app.openTerminal()
		for step := 0; step < 14; step++ {
			h.tester.Advance(20 * time.Millisecond)
			h.tester.Frame()
			requireSize(fmt.Sprintf("restore cycle %d frame %d", cycle, step))
		}
		requireTerminalRunning(t, first)
	}
}

func TestTerminalMaximizeRestoresPanelHeightAndSessions(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the maximizable terminal tab", func() bool {
		return h.app.terminalSessionCount() == 2 && h.app.activeTerminal().pane != nil
	})
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })
	secondPane := h.app.activeTerminal().pane
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	resizing, found := h.tester.Find("Terminal pane")
	if !found {
		t.Fatal("the dock is missing before resizing")
	}
	defaultPercent := h.app.term.percent
	dragTo(t, h.tester, resizing.X+100, resizing.Y-islandGap/2, 0, -60)
	if h.app.term.percent <= defaultPercent {
		t.Fatalf("the dock divider did not resize the panel: %v%% -> %v%%", defaultPercent, h.app.term.percent)
	}
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	restorePercent := h.app.term.percent
	before, found := h.tester.Find("Terminal pane")
	if !found {
		t.Fatal("the dock is missing before maximizing")
	}
	if _, found := h.tester.Find("Select terminal 2"); !found {
		t.Fatal("the per-shell title bar is missing before maximizing")
	}

	if err := h.tester.Click("Maximize terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	maximized, found := h.tester.Find("Terminal pane")
	if !found || !h.app.term.panelMaximized || maximized.H <= before.H*2 {
		t.Fatalf("maximize did not give the terminal the content area: before=%+v after=%+v state=%+v", before, maximized, h.app.term)
	}
	if h.app.activeTerminal().pane != secondPane || h.app.term.percent != restorePercent {
		t.Fatal("maximizing replaced the active shell or overwrote its restored height")
	}
	if _, found := h.tester.Find("Select terminal 2"); found {
		t.Fatal("maximizing kept the redundant per-shell title bar")
	}
	if _, found := h.tester.Find("Terminal actions"); !found {
		t.Fatal("maximizing hid the toolbar containing the restore control")
	}
	if err := h.tester.Click("Restore terminal size"); err != nil {
		t.Fatal(err)
	}
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	restored, found := h.tester.Find("Terminal pane")
	if !found || h.app.term.panelMaximized || restored.H != before.H || h.app.term.percent != restorePercent {
		t.Fatalf("restore did not recover the original dock height: before=%+v after=%+v state=%+v", before, restored, h.app.term)
	}
	if _, found := h.tester.Find("Select terminal 2"); !found {
		t.Fatal("restoring the dock did not restore its per-shell title bar")
	}
	for _, peer := range []monitoredTerminalPeer{first, second} {
		requireTerminalRunning(t, peer)
	}
}

func TestTerminalIndividualCloseNeverRequiresConfirmation(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	if err := h.tester.Click("New terminal tab"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the second individual terminal", func() bool {
		return h.app.terminalSessionCount() == 2 && h.app.activeTerminal().pane != nil
	})
	second := <-peers
	t.Cleanup(func() { _ = second.Close() })

	if err := h.tester.Click("Close terminal 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm != nil {
		t.Fatalf("closing one exact terminal unexpectedly asked for confirmation: %+v", h.app.confirm)
	}
	if h.app.terminalSessionCount() != 1 || h.app.activeTerminalGroup() != 1 {
		t.Fatalf("individual close disturbed the other tab: %+v", h.app.term)
	}
	requireTerminalRunning(t, first)
	requireTerminalClosed(t, second)

	if err := h.tester.Click("Close terminal 1"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm != nil || h.app.term.panelOpen || h.app.terminalSessionCount() != 0 {
		t.Fatalf("closing the last terminal was not immediate: confirm=%+v terminal=%+v", h.app.confirm, h.app.term)
	}
	requireTerminalClosed(t, first)
}

func TestTerminalCloseAllRequiresConfirmationButIndividualSplitCloseDoesNot(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, false)

	if err := h.tester.Click("Close all terminals"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.title != tr("Close all terminals") || !h.app.confirm.destructive {
		t.Fatalf("close all did not open its destructive confirmation: %+v", h.app.confirm)
	}
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)
	if err := h.tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)

	if err := h.tester.Click("Close terminal 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm != nil {
		t.Fatalf("closing one exact split unexpectedly asked for confirmation: %+v", h.app.confirm)
	}
	requireTerminalClosed(t, second)
	requireTerminalRunning(t, first)
	if !h.app.term.panelOpen || h.app.term.active != 0 || h.app.terminalSessionCount() != 1 {
		t.Fatalf("per-tab close disturbed the remaining shell: %+v", h.app.term)
	}
}

func TestTerminalCloseAllConfirmationExitsEveryShell(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, false)
	if err := h.tester.Click("Close all terminals"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Close all"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	requireTerminalClosed(t, first)
	requireTerminalClosed(t, second)
	if h.app.term.panelOpen || h.app.terminalSessionCount() != 0 || h.app.confirm != nil {
		t.Fatalf("confirmed close all left terminal state behind: %+v", h.app.term)
	}
}

func TestTerminalSplitsSubdivideOnlyActiveCardInMixedDirections(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, false)
	secondBefore, _ := h.tester.Find("Select terminal 2")

	if err := h.tester.Click("Focus terminal 1"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	third := splitMonitoredTerminal(t, h, peers, true)
	firstAfter, _ := h.tester.Find("Select terminal 1")
	secondAfter, _ := h.tester.Find("Select terminal 2")
	thirdAfter, found := h.tester.Find("Select terminal 3")
	if !found {
		t.Fatal("the split-below terminal is missing")
	}
	if thirdAfter.Y <= firstAfter.Y+firstAfter.H || thirdAfter.X != firstAfter.X || thirdAfter.W != firstAfter.W {
		t.Fatalf("terminal 3 is not below the active terminal: first=%+v third=%+v", firstAfter, thirdAfter)
	}
	if secondAfter != secondBefore {
		t.Fatalf("splitting terminal 1 disturbed terminal 2: before=%+v after=%+v", secondBefore, secondAfter)
	}

	if err := h.tester.Click("Focus terminal 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	fourth := splitMonitoredTerminal(t, h, peers, false)
	firstFinal, _ := h.tester.Find("Select terminal 1")
	secondFinal, _ := h.tester.Find("Select terminal 2")
	thirdFinal, _ := h.tester.Find("Select terminal 3")
	fourthFinal, found := h.tester.Find("Select terminal 4")
	if !found {
		t.Fatal("the split-right terminal is missing")
	}
	if fourthFinal.X <= secondFinal.X+secondFinal.W || fourthFinal.Y != secondFinal.Y || fourthFinal.H != secondFinal.H {
		t.Fatalf("terminal 4 is not beside the active terminal: second=%+v fourth=%+v", secondFinal, fourthFinal)
	}
	if firstFinal != firstAfter || thirdFinal != thirdAfter {
		t.Fatalf("splitting terminal 2 disturbed the left column: first=%+v/%+v third=%+v/%+v", firstAfter, firstFinal, thirdAfter, thirdFinal)
	}
	for _, peer := range []monitoredTerminalPeer{first, second, third, fourth} {
		requireTerminalRunning(t, peer)
	}

	// Reordering changes only leaf ownership, and closing a leaf collapses
	// only its parent; both operations retain every other local split.
	root := h.app.term.layout
	right := root.second
	thirdTab, _ := h.tester.Find("Select terminal 3")
	firstTab, _ := h.tester.Find("Select terminal 1")
	h.tester.Press(thirdTab.X+30, thirdTab.Y+thirdTab.H/2)
	h.tester.Move(firstTab.X+30, firstTab.Y+firstTab.H/2)
	h.tester.Frame()
	h.tester.Release(firstTab.X+30, firstTab.Y+firstTab.H/2)
	h.tester.Frame()
	if h.app.term.layout != root || root.second != right || root.first.first.sessionID != 3 || root.first.second.sessionID != 1 {
		t.Fatalf("drag replaced the mixed split structure: %+v", h.app.term.layout)
	}
	if err := h.tester.Click("Close terminal 3"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	requireTerminalClosed(t, third)
	for _, peer := range []monitoredTerminalPeer{first, second, fourth} {
		requireTerminalRunning(t, peer)
	}
	if h.app.term.layout != root || root.second != right || root.first.sessionID != 1 || root.first.first != nil {
		t.Fatalf("close did not collapse only terminal 3's parent: %+v", h.app.term.layout)
	}
	if h.tester.HasText("Handle bound to two controls in one build") || h.tester.HasText("This window hit an unexpected problem") {
		t.Fatalf("mixed-layout reorder or close failed: %q", h.texts())
	}
}

func TestTerminalSplitBelowAndDragReorderShowsTargetPlaceholder(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, true)
	firstPane := h.app.term.sessions[0].pane
	secondPane := h.app.term.sessions[1].pane
	firstTab, ok := h.tester.Find("Select terminal 1")
	if !ok {
		t.Fatal("the first terminal has no draggable tab")
	}
	secondTab, ok := h.tester.Find("Select terminal 2")
	if !ok {
		t.Fatal("the second terminal has no draggable tab")
	}
	if secondTab.Y <= firstTab.Y+firstTab.H {
		t.Fatalf("split-below tabs are not vertically separated: first=%+v second=%+v", firstTab, secondTab)
	}

	h.tester.Press(firstTab.X+30, firstTab.Y+firstTab.H/2)
	h.tester.Move(secondTab.X+30, secondTab.Y+secondTab.H/2)
	h.tester.Frame()
	if !h.tester.HasText("Move terminal here") {
		t.Fatalf("the drag target has no insertion placeholder: %q", h.texts())
	}
	h.tester.Release(secondTab.X+30, secondTab.Y+secondTab.H/2)
	h.tester.Frame()
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)
	if h.app.term.sessions[0].pane != secondPane || h.app.term.sessions[1].pane != firstPane || h.app.term.active != 1 {
		t.Fatalf("drop did not move the dragged shell into the target: %+v", h.app.term)
	}
	if h.app.term.sessions[0].id != 2 || h.app.term.sessions[1].id != 1 {
		t.Fatalf("terminal names did not retain their identities after reorder: %+v", h.app.term.sessions)
	}
}

func TestTerminalDragBackDoesNotBindAShellTwice(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, false)
	firstTab, ok := h.tester.Find("Select terminal 1")
	if !ok {
		t.Fatal("the first terminal has no draggable tab")
	}
	secondTab, ok := h.tester.Find("Select terminal 2")
	if !ok {
		t.Fatal("the second terminal has no draggable tab")
	}

	h.tester.Press(secondTab.X+30, secondTab.Y+secondTab.H/2)
	h.tester.Move(firstTab.X+30, firstTab.Y+firstTab.H/2)
	h.tester.Frame()
	h.tester.Release(firstTab.X+30, firstTab.Y+firstTab.H/2)
	h.tester.Frame()
	if h.tester.HasText("Handle bound to two controls in one build") || h.tester.HasText("This window hit an unexpected problem") {
		t.Fatalf("reverse drag bound a terminal handle twice: %q", h.texts())
	}

	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)
	if h.app.term.sessions[0].id != 2 || h.app.term.sessions[1].id != 1 {
		t.Fatalf("reverse drag did not reorder the terminals: %+v", h.app.term.sessions)
	}
}

func TestTerminalTabsRenameAndContainTheirActions(t *testing.T) {
	h, _, _ := monitoredTerminalTestApp(t)
	tab, ok := h.tester.Find("Select terminal 1")
	if !ok {
		t.Fatal("the terminal tab is missing")
	}
	for _, label := range []string{"Rename terminal 1", "Close terminal 1"} {
		action, found := h.tester.Find(label)
		if !found {
			t.Fatalf("%q is missing", label)
		}
		if action.X < tab.X || action.Y < tab.Y || action.X+action.W > tab.X+tab.W || action.Y+action.H > tab.Y+tab.H {
			t.Fatalf("%q lies outside the tab: tab=%+v action=%+v", label, tab, action)
		}
	}

	if err := h.tester.Click("Rename terminal 1"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Type("Build shell")
	h.tester.Frame()
	if err := h.tester.Click("Save name"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.tester.HasText("Build shell") {
		t.Fatalf("the renamed terminal is not shown: %q", h.texts())
	}
	h.rightClick(t, "Focus terminal 1")
	h.chooseMenuItem(t, "Rename terminal")
	if h.app.term.renameID != h.app.term.sessions[0].id {
		t.Fatal("the split context menu targeted another shell")
	}
	if err := h.tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.rightClick(t, "Focus terminal 1")
	h.chooseMenuItem(t, "Close terminal")
	if h.app.terminalSessionCount() != 0 || h.app.term.panelOpen {
		t.Fatalf("the split context menu did not close its shell: %+v", h.app.term)
	}
}

func TestTerminalTopLevelAndSplitNamesAreIndependent(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, true)
	countLabel := func(label string) int {
		count := 0
		for _, text := range h.texts() {
			if text == label {
				count++
			}
		}
		return count
	}

	if err := h.tester.Click("Rename terminal 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Type("Build output")
	h.tester.Frame()
	if err := h.tester.Click("Save name"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if got := countLabel("Build output"); got != 2 {
		t.Fatalf("renaming a split changed its top-level tab too (%d copies): %q", got, h.texts())
	}
	if h.app.term.sessions[1].name != "Build output" {
		t.Fatalf("the split did not retain its custom name: %+v", h.app.term.sessions)
	}

	if err := h.tester.Click("Rename tab 1"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Type("Primary tab")
	h.tester.Frame()
	if err := h.tester.Click("Save name"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if got := countLabel("Primary tab"); got != 1 {
		t.Fatalf("renaming the top-level tab changed its split too (%d copies): %q", got, h.texts())
	}
	if h.app.term.sessions[1].name != "Build output" || !h.tester.HasText("Build output") {
		t.Fatalf("top-level rename replaced the split name: %+v", h.app.term.sessions)
	}
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)
}

func TestTerminalLimitIsSixAndAnotherSplitReportsAnError(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	opened := []monitoredTerminalPeer{first}
	for i := 1; i < terminalSessionLimit; i++ {
		opened = append(opened, splitMonitoredTerminal(t, h, peers, i%2 == 0))
	}
	if got := h.app.terminalSessionCount(); got != 6 {
		t.Fatalf("terminal count = %d, want 6", got)
	}
	for number := 1; number <= terminalSessionLimit; number++ {
		tab, found := h.tester.Find(fmt.Sprintf("Select terminal %d", number))
		if !found || tab.W <= 0 || tab.H <= 0 {
			t.Fatalf("terminal %d has no visible card", number)
		}
	}
	if err := h.tester.Click("Split terminal right"); err != nil {
		t.Fatalf("the limit must explain itself rather than silently disabling the action: %v", err)
	}
	h.tester.Frame()
	if !h.tester.HasText("Terminal limit reached") {
		t.Fatalf("the extra split did not report its limit: %q", h.texts())
	}
	if got := h.app.terminalSessionCount(); got != 6 {
		t.Fatalf("the rejected split changed the terminal count to %d", got)
	}
	for _, peer := range opened {
		requireTerminalRunning(t, peer)
	}
}

func TestTerminalMiddleGripResizesAndThreeCardDragKeepsShellsAlive(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, false)
	third := splitMonitoredTerminal(t, h, peers, false)
	grip, ok := h.tester.Find("Resize terminal split 1")
	if !ok {
		t.Fatal("the middle split has no grip")
	}
	before := h.app.term.layout.percent
	dragTo(t, h.tester, grip.X+grip.W/2, grip.Y+grip.H/2, 35, 0)
	if h.app.term.layout.percent == before {
		t.Fatalf("the ellipsis grip did not resize the terminal split: before=%v after=%v", before, h.app.term.layout.percent)
	}

	thirdTab, ok := h.tester.Find("Select terminal 3")
	if !ok {
		t.Fatal("the third terminal has no draggable tab")
	}
	firstTab, ok := h.tester.Find("Select terminal 1")
	if !ok {
		t.Fatal("the first terminal has no draggable tab")
	}
	h.tester.Press(thirdTab.X+30, thirdTab.Y+thirdTab.H/2)
	h.tester.Move(firstTab.X+30, firstTab.Y+firstTab.H/2)
	h.tester.Frame()
	h.tester.Release(firstTab.X+30, firstTab.Y+firstTab.H/2)
	h.tester.Frame()
	if h.tester.HasText("Handle bound to two controls in one build") || h.tester.HasText("This window hit an unexpected problem") {
		t.Fatalf("three-card drag bound a terminal handle twice: %q", h.texts())
	}
	for i, id := range []uint64{3, 1, 2} {
		if h.app.term.sessions[i].id != id {
			t.Fatalf("drop did not insert the third card at the target: %+v", h.app.term.sessions)
		}
	}
	for _, peer := range []monitoredTerminalPeer{first, second, third} {
		requireTerminalRunning(t, peer)
	}
}

func TestTerminalSplitHideAndCloseHaveIndependentLifetimes(t *testing.T) {
	original := newTerminal
	peers := make(chan monitoredTerminalPeer, terminalSessionLimit)
	newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
		return newMonitoredTerminal(options, peers)
	}
	defer func() { newTerminal = original }()

	h := menuTestApp(t)
	h.app.openTerminal()
	h.tester.Frame()
	h.waitFor(t, "the first terminal", func() bool {
		return len(h.app.term.sessions) > 0 && h.app.term.sessions[0].pane != nil
	})
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	first := <-peers
	defer first.Close()
	firstPane := h.app.term.sessions[0].pane

	if err := h.tester.Click("Split terminal right"); err != nil {
		t.Fatalf("%v; terminal=%+v; texts=%q", err, h.app.term, h.texts())
	}
	h.tester.Frame()
	h.waitFor(t, "the split terminal", func() bool { return h.app.term.sessions[1].pane != nil })
	h.tester.Frame()
	second := <-peers
	defer second.Close()
	secondPane := h.app.term.sessions[1].pane
	if firstPane == secondPane || h.app.term.active != 1 {
		t.Fatalf("split did not make a second independent active session: active=%d", h.app.term.active)
	}

	if err := h.tester.Click("Hide terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.term.panelOpen {
		t.Fatal("Hide did not hide the terminal panel")
	}
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)
	h.app.openTerminal()
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	if h.app.term.sessions[0].pane != firstPane || h.app.term.sessions[1].pane != secondPane {
		t.Fatal("reopening replaced the hidden terminals")
	}

	// Close applies to the selected session, not the other split or the panel.
	if err := h.tester.Click("Close terminal 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	requireTerminalClosed(t, second)
	requireTerminalRunning(t, first)
	if !h.app.term.panelOpen || h.app.term.active != 0 || h.app.terminalSessionCount() != 1 || h.app.term.sessions[0].pane != firstPane {
		t.Fatalf("closing the split disturbed the remaining shell: %+v", h.app.term)
	}

	if err := h.tester.Click("Close terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	requireTerminalClosed(t, first)
	if h.app.term.panelOpen || h.app.terminalSessionCount() != 0 {
		t.Fatalf("closing the last shell left the empty panel open: %+v", h.app.term)
	}
	if h.app.term.layouts != nil || h.app.term.activeSessions != nil {
		t.Fatalf("closing the last shell retained tab state: layouts=%v active=%v", h.app.term.layouts, h.app.term.activeSessions)
	}
}

func TestTerminalWorkspaceSwitchPreservesSessionsSplitsAndSearch(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, false)
	layout := h.app.term.layout
	firstPane := h.app.term.sessions[0].pane
	secondPane := h.app.term.sessions[1].pane
	active := h.app.activeTerminal()
	active.searchOpen = true
	active.searchText = "needle"

	h.app.setWorkspaceTab(sectionHistory)
	h.tester.Frame()
	if !h.app.term.panelOpen || h.app.workspaceTab != sectionHistory {
		t.Fatalf("switching workspace pages hid the shared dock: tab=%d open=%v", h.app.workspaceTab, h.app.term.panelOpen)
	}
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)

	h.app.hideTerminalView()
	h.tester.Frame()
	h.app.openTerminal()
	h.tester.Frame()
	if !h.app.term.panelOpen || h.app.term.active != 1 || h.app.term.layout != layout ||
		h.app.term.sessions[0].pane != firstPane || h.app.term.sessions[1].pane != secondPane {
		t.Fatalf("restoring the dock replaced terminal state: %+v", h.app.term)
	}
	if active = h.app.activeTerminal(); !active.searchOpen || active.searchText != "needle" {
		t.Fatalf("restoring the dock lost the active terminal search: %+v", active)
	}
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)
}

func TestTerminalHasNoWorkspacePageAndShortcutReopensDock(t *testing.T) {
	original := newTerminal
	newTerminal = func(terminal.Options) (*terminal.Terminal, error) {
		return nil, errors.New("no shell here")
	}
	defer func() { newTerminal = original }()

	h := menuTestApp(t)
	for _, section := range workspaceSections {
		if section == "Terminal" {
			t.Fatal("Terminal still has a standalone workspace page")
		}
	}
	h.app.settings.VisibleSections = normalizeVisibleSections([]string{"terminal"})
	h.app.applySettingsChange()
	h.tester.Frame()
	if slices.Contains(h.app.settings.VisibleSections, "terminal") {
		t.Fatal("the removed Terminal page was retained in workspace settings")
	}
	h.app.setWorkspaceTab(sectionHistory)
	h.tester.Key(ui.Ctrl, ui.KeyBackquote)
	h.tester.Frame()
	h.waitFor(t, "the docked terminal to report its error", func() bool {
		return len(h.app.term.sessions) == 1 && h.app.term.sessions[0].err != ""
	})
	if h.app.workspaceTab != sectionHistory || !h.app.term.panelOpen || h.app.terminalSessionCount() != 1 {
		t.Fatalf("the shortcut did not open the dock over History: tab=%d state=%+v", h.app.workspaceTab, h.app.term)
	}

	h.tester.Key(ui.Ctrl, ui.KeyBackquote)
	h.tester.Frame()
	if h.app.term.panelOpen || h.app.workspaceTab != sectionHistory || h.app.terminalSessionCount() != 1 {
		t.Fatalf("the shortcut did not hide the dock over its current page: tab=%d state=%+v", h.app.workspaceTab, h.app.term)
	}
	h.tester.Key(ui.Ctrl, ui.KeyBackquote)
	h.tester.Frame()
	if h.app.workspaceTab != sectionHistory || !h.app.term.panelOpen || h.app.terminalSessionCount() != 1 {
		t.Fatalf("the shortcut did not reopen the same dock: tab=%d state=%+v", h.app.workspaceTab, h.app.term)
	}
}

func TestClosingAStartingSplitDiscardsItsLateShell(t *testing.T) {
	original := newTerminal
	peers := make(chan monitoredTerminalPeer, terminalSessionLimit)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
		if calls.Add(1) == 2 {
			close(started)
			<-release
		}
		return newMonitoredTerminal(options, peers)
	}
	defer func() { newTerminal = original }()

	h := menuTestApp(t)
	h.app.openTerminal()
	h.tester.Frame()
	h.waitFor(t, "the first terminal", func() bool {
		return len(h.app.term.sessions) > 0 && h.app.term.sessions[0].pane != nil
	})
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	first := <-peers
	defer first.Close()
	if err := h.tester.Click("Split terminal right"); err != nil {
		t.Fatalf("%v; terminal=%+v; texts=%q", err, h.app.term, h.texts())
	}
	<-started
	h.tester.Frame()
	if err := h.tester.Click("Close terminal 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	close(release)
	second := <-peers
	defer second.Close()
	h.updates.waitForUpdate(t)
	requireTerminalClosed(t, second)
	h.tester.Frame()
	requireTerminalRunning(t, first)
	if len(h.app.term.sessions) != 1 || h.app.term.sessions[0].pane == nil || h.app.term.active != 0 {
		t.Fatalf("the late split shell returned after Close: %+v", h.app.term)
	}
}

func TestTerminalShortcutIsGlobalAcrossFormsAndShortcutPreference(t *testing.T) {
	original := newTerminal
	newTerminal = func(terminal.Options) (*terminal.Terminal, error) {
		return nil, errors.New("no shell here")
	}
	t.Cleanup(func() { newTerminal = original })

	for _, test := range []struct {
		name      string
		open      func(*app)
		stillOpen func(*app) bool
	}{
		{name: "workspace"},
		{name: "commit form", open: func(a *app) { a.showCommitForm = true }, stillOpen: func(a *app) bool { return a.showCommitForm }},
		{name: "confirmation", open: func(a *app) {
			a.askConfirm(confirmRequest{title: "Discard changes", run: func() {}})
		}, stillOpen: func(a *app) bool { return a.confirm != nil }},
		{name: "settings", open: func(a *app) { a.openSettings() }, stillOpen: func(a *app) bool { return a.settingsOpen }},
		{name: "other shortcuts disabled", open: func(a *app) { a.settings.DisableShortcuts = true }, stillOpen: func(a *app) bool { return a.settings.DisableShortcuts }},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := menuTestApp(t)
			h.app.setWorkspaceTab(sectionHistory)
			if test.open != nil {
				test.open(h.app)
			}
			h.tester.Frame()

			// Shift often remains held for the physical tilde key. Accept that
			// form as well as the literal Ctrl+backquote binding, in both forms.
			h.tester.Key(ui.Ctrl|ui.Shift, ui.KeyBackquote)
			h.tester.Frame()
			h.waitFor(t, "the terminal behind the focused mode", func() bool {
				return len(h.app.term.sessions) == 1 && h.app.term.sessions[0].err != ""
			})
			if !h.app.term.panelOpen || h.app.workspaceTab != sectionHistory || h.app.terminalSessionCount() != 1 {
				t.Fatalf("Ctrl+~ did not open the global dock: %+v", h.app.term)
			}
			if test.stillOpen != nil && !test.stillOpen(h.app) {
				t.Fatal("opening the terminal dismissed the focused mode")
			}

			h.tester.Key(ui.Ctrl, ui.KeyBackquote)
			h.tester.Frame()
			if h.app.term.panelOpen || h.app.workspaceTab != sectionHistory || h.app.terminalSessionCount() != 1 {
				t.Fatal("Ctrl+` did not hide the global dock without ending its shell")
			}
			if test.stillOpen != nil && !test.stillOpen(h.app) {
				t.Fatal("hiding the terminal dismissed the focused mode")
			}

			h.tester.Key(ui.Ctrl, ui.KeyBackquote)
			h.tester.Frame()
			if !h.app.term.panelOpen || h.app.workspaceTab != sectionHistory || h.app.terminalSessionCount() != 1 {
				t.Fatal("Ctrl+` did not reopen the same global dock")
			}
			if test.stillOpen != nil && !test.stillOpen(h.app) {
				t.Fatal("reopening the terminal dismissed the focused mode")
			}
		})
	}
}

func TestShellTerminalShortcutTogglesRepeatedlyAfterClickingEmptyEditor(t *testing.T) {
	original := newTerminal
	peers := make(chan monitoredTerminalPeer, 1)
	newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
		return newMonitoredTerminal(options, peers)
	}
	t.Cleanup(func() { newTerminal = original })

	s, tt, updated := newShellTester(t, repository.Snapshot{})
	tab := readyShellTab(s, "/work/project")
	tab.app.code.filesLoaded = true
	s.active = len(s.tabs) - 1
	a := tab.app
	a.setWorkspaceTab(sectionCode)
	tt.Frame()
	empty, found := tt.Find("Open a file from the explorer")
	if !found {
		t.Fatalf("the shell's empty Editor is missing: %q", tt.Texts())
	}
	tt.ClickAt(empty.X+empty.W/2, empty.Y+empty.H/2)
	tt.Frame()

	var first monitoredTerminalPeer
	for press := 1; press <= 20; press++ {
		wantOpen := !a.term.panelOpen
		tt.Key(ui.Ctrl|ui.Shift, ui.KeyBackquote)
		tt.Frame()
		if press == 1 && !a.term.panelOpen {
			t.Fatalf("shell Ctrl+~ did not open the terminal after the empty-Editor click: %+v", a.term)
		}
		if press == 1 {
			for range 8 {
				if len(a.term.sessions) == 1 && a.term.sessions[0].pane != nil {
					break
				}
				updated.waitForUpdate(t)
				tt.Frame()
			}
			first = <-peers
			t.Cleanup(func() { _ = first.Close() })
			tt.Advance(250 * time.Millisecond)
			tt.Frame()
		}
		if a.term.panelOpen != wantOpen {
			t.Fatalf("shell Ctrl+~ press %d left terminal open = %v, want %v", press, a.term.panelOpen, wantOpen)
		}
		if a.workspaceTab != sectionCode {
			t.Fatalf("shell Ctrl+~ press %d changed the workspace to %d", press, a.workspaceTab)
		}
	}
}

func TestTerminalButtonTogglesDockWithoutChangingWorkspace(t *testing.T) {
	original := newTerminal
	newTerminal = func(terminal.Options) (*terminal.Terminal, error) {
		return nil, errors.New("no shell here")
	}
	defer func() { newTerminal = original }()

	h := menuTestApp(t)
	h.app.setWorkspaceTab(sectionHistory)
	h.app.openTerminal()
	h.tester.Frame()
	h.waitFor(t, "the terminal to report its error", func() bool {
		return h.app.term.sessions[0].err != ""
	})
	h.tester.Advance(250 * time.Millisecond)
	h.tester.Frame()
	if !h.app.term.panelOpen || h.app.workspaceTab != sectionHistory {
		t.Fatalf("the button changed the workspace instead of opening the dock: tab=%d open=%v", h.app.workspaceTab, h.app.term.panelOpen)
	}
	if !h.tester.HasText("no shell here") {
		t.Fatalf("the dock shows no error: %q", h.texts())
	}
	if pane, ok := h.tester.Find("Terminal pane"); !ok || pane.H <= 0 {
		t.Fatalf("the terminal dock is not visible: %+v", pane)
	}

	// Ctrl+backquote hides the dock over History without ending its session.
	h.tester.Key(ui.Ctrl, ui.KeyBackquote)
	h.tester.Frame()
	if h.app.term.panelOpen || h.app.workspaceTab != sectionHistory || h.app.terminalSessionCount() != 1 {
		t.Fatalf("Ctrl+` did not hide the dock over History: tab=%d open=%v sessions=%d", h.app.workspaceTab, h.app.term.panelOpen, h.app.terminalSessionCount())
	}
}

// A shell that is still starting when the repository is left is ended, not
// installed: its error says nothing about the repository that replaced it, and
// the next repository can start a shell of its own.
func TestLeavingARepositoryDropsAStartingTerminal(t *testing.T) {
	original := newTerminal
	started := make(chan struct{})
	release := make(chan struct{})
	newTerminal = func(terminal.Options) (*terminal.Terminal, error) {
		close(started)
		<-release
		return nil, errors.New("late shell")
	}
	defer func() { newTerminal = original }()

	h := menuTestApp(t)
	h.tester.Frame()
	h.app.openTerminal()
	<-started

	// Leave the repository while the shell is still starting, then let it
	// answer: the answer belongs to the repository that was left.
	h.app.shutdown()
	close(release)
	h.updates.waitForUpdate(t)
	h.tester.Frame()

	if len(h.app.term.sessions) != 0 {
		t.Fatalf("the left repository retained terminal state: %+v", h.app.term.sessions)
	}
	if h.app.term.panelOpen {
		t.Fatalf("the terminal dock stayed open after the repository was left: tab=%d", h.app.workspaceTab)
	}
}
