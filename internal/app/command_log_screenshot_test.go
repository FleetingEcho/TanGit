package app

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

var commandLogVisualCommands = []string{
	"git worktree list --porcelain",
	"git bisect log",
	"git rev-parse --verify HEAD",
	`git for-each-ref --sort=-refname "--format=%(refname) %(objectname)"`,
	"git rev-parse HEAD",
	"git rev-parse --verify --quiet HEAD",
	"git -c filter.lfs.required=false -c filter.lfs.process= -c filter.lfs.clean= -c filter.lfs.smudge= diff --no-ext-diff --no-textconv --unified=3 HEAD -- internal/app/command_log_scope.go",
	"git ls-files -u -z",
	"git branch --show-current",
	"git diff --cached --stat",
	"git diff --stat",
	"git remote -v",
	"git fetch --dry-run",
	"git status --short",
	"git log --oneline --decorate -20",
	"git reflog show --date=iso -10",
	"git show --stat --oneline HEAD",
	"git stash list --date=local",
	"git tag --sort=-creatordate --list",
	"git submodule status --recursive",
}

func newCommandLogVisualHarness(t *testing.T, dark bool, width, height int) (*app, *ui.Tester) {
	t.Helper()
	theme := defaultTheme
	if !dark {
		theme = "GitHub Light"
	}
	s, tester, _, _ := newShellShot(t, dark, theme)
	a := s.activeApp()
	a.attachRepositoryCommandLog(repository.NewCommandLog())
	tester.SetPreferences(ui.Preferences{ReduceMotion: true})
	tester.SetSize(width, height)
	tester.Frame()
	return a, tester
}

func recordCommandLogVisualCommands(a *app) {
	for _, command := range commandLogVisualCommands {
		a.commandLog.Record(command, "", false)
	}
}

// TestCommandLogNarrowGeometry verifies real row/badge bounds rather than
// inferring responsiveness from a text label or a relaxed layout assertion.
func TestCommandLogNarrowGeometry(t *testing.T) {
	a, tester := newCommandLogVisualHarness(t, false, 720, 780)
	recordCommandLogVisualCommands(a)
	const longCommand = `git for-each-ref --sort=-refname --format="%(refname) %(objectname)" refs/heads/a-very-long-responsive-branch-name`
	a.commandLog.Record(longCommand, "", false)
	a.openCommandLog()
	tester.Frame()
	if a.crash != "" {
		t.Fatal(a.crash)
	}
	dialog, found := tester.AccessByRole(ui.RoleDialog, "Command log")
	if !found {
		t.Fatalf("missing Command Log dialog: %q", tester.Texts())
	}
	// The newest long command is visible and must share its row with the
	// status, time and menu controls without pushing them outside the panel.
	command, found := tester.AccessByRole(ui.RoleText, "$ "+longCommand)
	if !found {
		t.Fatalf("missing unique command geometry: %q", tester.Texts())
	}
	var badge *ui.Rect
	for _, node := range tester.AccessAll() {
		if node.Role == ui.RoleText && node.Name == "Succeeded" && math.Abs(float64(node.Bounds.Y-command.Bounds.Y)) < 8 {
			rect := node.Bounds
			badge = &rect
			break
		}
	}
	if badge == nil {
		t.Fatalf("success badge is missing beside its command: %q", tester.Texts())
	}
	if command.Bounds.X+command.Bounds.W > badge.X || badge.X+badge.W > dialog.Bounds.X+dialog.Bounds.W || badge.H == 0 {
		t.Fatalf("badge falls outside the dialog: badge=%+v dialog=%+v", *badge, dialog.Bounds)
	}
	if !tester.HasText("Status filter") || !tester.HasText("Source filter") || !tester.HasText("Time range filter") {
		t.Fatalf("filter controls are missing: %q", tester.Texts())
	}
}

// TestCommandLogScreenshots is opt-in visual evidence, not native-window
// acceptance. Actual Linux/macOS/Windows focus checks remain separate.
func TestCommandLogScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Run("wide-dark", func(t *testing.T) {
		a, tester := newCommandLogVisualHarness(t, true, 1120, 780)
		recordCommandLogVisualCommands(a)
		a.openCommandLog()
		saveScreenshot(t, dir, "command-log-wide-dark", tester)
	})
	t.Run("narrow-light", func(t *testing.T) {
		a, tester := newCommandLogVisualHarness(t, false, 720, 780)
		recordCommandLogVisualCommands(a)
		a.openCommandLog()
		saveScreenshot(t, dir, "command-log-narrow-light", tester)
	})
	t.Run("failed-light", func(t *testing.T) {
		a, tester := newCommandLogVisualHarness(t, false, 1024, 760)
		for _, command := range commandLogVisualCommands[:5] {
			a.commandLog.Record(command, "", false)
		}
		a.commandLog.Record("git push --force-with-lease", "error: failed to push some refs\nfatal: updating was rejected", true)
		a.openCommandLog()
		tester.Frame()
		commandLogFilterChoice(t, tester, "Status filter", "Failed")
		saveScreenshot(t, dir, "command-log-failed-light", tester)
	})
	t.Run("second-page-dark", func(t *testing.T) {
		a, tester := newCommandLogVisualHarness(t, true, 1120, 780)
		for i := range 55 {
			a.commandLog.Record(fmt.Sprintf("git status --short %02d", i), "", false)
		}
		a.openCommandLog()
		tester.Frame()
		page, found := tester.Find("Command history")
		if !found {
			t.Fatal("virtual list geometry is unavailable")
		}
		tester.Scroll(page.X+page.W/2, page.Y+page.H/2, 0, 10000)
		if a.commandLogLimit != 40 {
			t.Fatalf("user scroll loaded %d records, want 40", a.commandLogLimit)
		}
		saveScreenshot(t, dir, "command-log-second-page-dark", tester)
	})
}
