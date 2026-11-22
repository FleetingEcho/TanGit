package app

import (
	"strings"
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

// Output reaches the command log while the command is still running.
func TestPaletteShellRunStreamsOutput(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	h.app.snapshot.Root = t.TempDir()
	h.app.commandLog = repository.NewCommandLog()
	s.palette.open = true
	s.palette.query = "!echo first; sleep 30"
	if result := s.invokeCommand("commands/run-line"); !result.Accepted() {
		t.Fatalf("invoke = %+v", result)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if entry, ok := s.palette.shellRun.entry(); ok && entry.Running && strings.Contains(entry.Output, "first") {
			h.app.cancelCustomCommand()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.app.cancelCustomCommand()
	t.Fatal("running command output was not streamed to the log")
}

func TestShellHistoryRecallAndPersistence(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	h.app.pathUserData = t.TempDir()
	s.rememberShellLine("git status")
	s.rememberShellLine("git log")
	s.palette.query = "!typed"
	s.recallShellLine(1)
	if s.palette.query != "!git log" {
		t.Fatalf("first recall = %q", s.palette.query)
	}
	s.recallShellLine(1)
	if s.palette.query != "!git status" {
		t.Fatalf("second recall = %q", s.palette.query)
	}
	s.recallShellLine(-1)
	s.recallShellLine(-1)
	if s.palette.query != "!typed" {
		t.Fatalf("draft not restored: %q", s.palette.query)
	}
	s.togglePinnedCommand("theme/Tokyo Night")
	saved := loadAppSettings(h.app.pathUserData)
	if len(saved.ShellHistory) != 2 || saved.ShellHistory[0] != "git log" || len(saved.PinnedCommands) != 1 {
		t.Fatalf("palette state not persisted: %+v", saved)
	}
}

func TestRowActionsAndFailureCommands(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	h.app.commandLog = repository.NewCommandLog()
	ctx := commandContext{Shell: s, App: h.app}
	if _, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, "retry the latest", nil), "commands/retry-failed"); ok {
		t.Fatal("retry is offered with no failed command")
	}
	h.app.commandLog.BeginUserCommand("false")(" boom", true)
	if c, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, "retry the latest", nil), "commands/retry-failed"); !ok || c.Label == "" {
		t.Fatalf("retry missing after a failure: %+v", c)
	}
	h.app.snapshot.Files = nil
	s.palette.open = true
	s.palette.query = "#"
	for _, c := range s.catalogFor(ctx).Search(ctx, "#", nil) {
		if len(c.ActionLabels) != 2 {
			t.Fatalf("commit row actions = %v", c.ActionLabels)
		}
	}
}

// While the palette shows a ! command's output, its log listener repaints for
// each burst of output even though the command-log modal is closed.
func TestPaletteShellOutputRepaintsWithoutTheCommandLogOpen(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	log := repository.NewCommandLog()
	h.app.attachRepositoryCommandLog(log)
	id, _ := log.BeginUserCommandEntry("echo hi")
	if h.app.commandLogVisible.Load() {
		t.Fatal("the command log should be closed in this test")
	}
	log.AppendOutput(id, "ignored while nothing shows it\n")
	h.app.paletteOutputVisible.Store(true)
	log.AppendOutput(id, "shown\n")
	h.updates.waitForUpdate(t)
	if h.app.paletteOutputVisible.Load() != true {
		t.Fatal("visibility flag was cleared by the repaint")
	}
}
