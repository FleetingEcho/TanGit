package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

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
		return h.app.term.err != ""
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

	if h.app.term.loading {
		t.Fatal("the left repository's terminal is still starting")
	}
	if h.app.term.err != "" {
		t.Fatalf("the stale shell's error reached the new state: %q", h.app.term.err)
	}
	if h.app.term.open {
		t.Fatal("the terminal pane stayed open after the repository was left")
	}
}
