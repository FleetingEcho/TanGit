package app

import (
	"testing"
	"time"

	"lazygitapp/internal/repository"
)

// A ! command keeps the palette open, shows its log record, survives closing
// the palette, and stops when cancelled.
func TestPaletteShellRunStaysOpenAndCancels(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	h.app.snapshot.Root = t.TempDir()
	h.app.commandLog = repository.NewCommandLog()
	s.palette.open = true
	s.palette.query = "!sleep 30"

	result := s.invokeCommand("commands/run-line")
	if !result.Accepted() || !result.KeepOpen {
		t.Fatalf("invoke = %+v", result)
	}
	if !s.palette.open {
		t.Fatal("palette closed after starting the command")
	}
	entry, ok := s.palette.shellRun.entry()
	if !ok || !entry.Running {
		t.Fatalf("run entry = %+v ok=%v", entry, ok)
	}

	s.closeCommandPalette(true)
	if entry, _ := s.palette.shellRun.entry(); !entry.Running {
		t.Fatal("closing the palette stopped the command")
	}

	s.palette.shellRun.app.cancelCustomCommand()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if entry, _ := s.palette.shellRun.entry(); !entry.Running {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("cancelled command never completed")
}
