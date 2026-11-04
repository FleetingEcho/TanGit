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
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
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

func TestTerminalCloseAllRequiresConfirmationButTabCloseDoesNot(t *testing.T) {
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
		t.Fatal("a per-tab close unexpectedly asked to close every terminal")
	}
	requireTerminalClosed(t, second)
	requireTerminalRunning(t, first)
	if !h.app.term.open || h.app.term.active != 0 || h.app.terminalSessionCount() != 1 {
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
	if h.app.term.open || h.app.terminalSessionCount() != 0 || h.app.confirm != nil {
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
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
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
	if h.app.term.open {
		t.Fatal("Hide did not hide the terminal panel")
	}
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
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
	if !h.app.term.open || h.app.term.active != 0 || h.app.terminalSessionCount() != 1 || h.app.term.sessions[0].pane != firstPane {
		t.Fatalf("closing the split disturbed the remaining shell: %+v", h.app.term)
	}

	if err := h.tester.Click("Close terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	requireTerminalClosed(t, first)
	if h.app.term.open || h.app.terminalSessionCount() != 0 {
		t.Fatalf("closing the last shell left the empty panel open: %+v", h.app.term)
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
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
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

func TestTerminalButtonOpensAndHidesThePane(t *testing.T) {
	original := newTerminal
	newTerminal = func(terminal.Options) (*terminal.Terminal, error) {
		return nil, errors.New("no shell here")
	}
	defer func() { newTerminal = original }()

	h := menuTestApp(t)
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "the terminal to report its error", func() bool {
		return h.app.term.sessions[0].err != ""
	})
	if !h.app.term.open {
		t.Fatal("the button did not open the terminal pane")
	}
	// The panel slides up over 200ms; let the animation finish.
	h.tester.Advance(250 * time.Millisecond)
	if !slices.Contains(h.texts(), "no shell here") {
		t.Fatalf("the pane shows no error: %q", h.texts())
	}
	if title, ok := h.tester.Find("Terminal"); !ok || title.Y < 400 {
		t.Fatalf("the terminal pane sits at y=%v, want the bottom of the window", title.Y)
	}

	// Ctrl+` toggles the pane too, and hides it without ending the shell.
	h.tester.Key(ui.Ctrl, ui.KeyBackquote)
	h.tester.Frame()
	if h.app.term.open {
		t.Fatal("Ctrl+` did not hide the terminal pane")
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
	if err := h.tester.Click("Toggle terminal"); err != nil {
		t.Fatal(err)
	}
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
	if h.app.term.open {
		t.Fatal("the terminal pane stayed open after the repository was left")
	}
}
