package app

import (
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/agentwork"
	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/transfer"
)

// TestWorkflowEntryScreenshots is opt-in like the other screenshot harnesses:
//
//	MYGO_SHOTS=/path/to/shots go test ./internal/app -run TestWorkflowEntryScreenshots
//
// Tester images are for visual review; they are not native-window acceptance.
func TestWorkflowEntryScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	makeShell := func(dark bool, theme string, width, height int) (*shell, *ui.Tester) {
		s, tt, _, _ := newShellShot(t, dark, theme)
		s.activeApp().workspaceTab = sectionOverview
		tt.SetSize(width, height)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
		return s, tt
	}
	populateActivity := func(a *app) {
		if a.commandLog == nil {
			a.commandLog = repository.NewCommandLog()
		}
		a.commandLog.Record("git push --dry-run", "remote: authentication failed", true)
		at := time.Now().Add(-10 * time.Minute)
		a.snapshot.Operations = append(a.snapshot.Operations, repository.OperationRecord{
			ID: "visual-operation", Summary: "Apply reviewed patch", Status: repository.OperationInterrupted,
			Partial: true, CreatedAt: at.Unix(), Error: "2 files could not be updated",
		})
		a.agents.tasks = []agentwork.Task{{
			ID: "visual-task", RepositoryID: a.agentRepositoryID(), Title: "Review session lifecycle", Status: agentwork.StatusNeedsInput,
			Activity: []agentwork.Activity{{ID: "visual-agent-activity", Kind: agentwork.ActivityStatus, StatusTo: string(agentwork.StatusNeedsInput), CreatedAt: at}},
		}}
		a.agents.loadedRepo = a.agentRepositoryID()
		a.remoteTransfers.jobs = map[transfer.ID]*remoteTransferJob{
			7: {occurredAt: at, updatedAt: at.Add(time.Minute), kind: "upload", result: transfer.Result{
				ID: 7, Status: transfer.StatusPartial, TotalItems: 12, CompletedItems: 7, FailedItems: 1,
				CompletedBytes: 1024, TotalBytes: 4096,
			}},
		}
		a.recordActivityNotice(activitySeverityInfo, "Workspace restored", "Pinned tabs and panel sizes were restored.", activityTarget{})
	}

	t.Run("command-palette-dark", func(t *testing.T) {
		s, tt := makeShell(true, defaultTheme, 1280, 800)
		s.openCommandPalette(nil)
		s.palette.query = "push"
		saveScreenshot(t, dir, "command-palette-dark", tt)
	})
	t.Run("command-palette-disabled-narrow-light", func(t *testing.T) {
		s, tt := makeShell(false, defaultTheme, 720, 800)
		s.openCommandPalette(nil)
		s.palette.query = "Continue the interrupted"
		saveScreenshot(t, dir, "command-palette-disabled-narrow-light", tt)
	})
	t.Run("overview-dirty-dark", func(t *testing.T) {
		_, tt := makeShell(true, defaultTheme, 1280, 800)
		saveScreenshot(t, dir, "overview-dirty-dark", tt)
	})
	t.Run("overview-operation-agent-narrow-light", func(t *testing.T) {
		s, tt := makeShell(false, defaultTheme, 720, 800)
		a := s.activeApp()
		populateActivity(a)
		a.snapshot.Operation = "merge"
		saveScreenshot(t, dir, "overview-operation-agent-narrow-light", tt)
	})
	t.Run("overview-wide-dark", func(t *testing.T) {
		_, tt := makeShell(true, defaultTheme, 1920, 1080)
		saveScreenshot(t, dir, "overview-wide-dark", tt)
	})
	t.Run("overview-high-contrast-text-scale", func(t *testing.T) {
		s, tt := makeShell(true, "High Contrast", 1280, 900)
		populateActivity(s.activeApp())
		tt.SetPreferences(ui.Preferences{HighContrast: true, ReduceMotion: true, TextScale: 1.5})
		saveScreenshot(t, dir, "overview-high-contrast-text-scale", tt)
	})
}
