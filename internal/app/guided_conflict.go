package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type guidedStageExplanation struct {
	title, short, full string
}

func guidedConflictStageExplanation(tab int) guidedStageExplanation {
	switch tab {
	case 0:
		return guidedStageExplanation{
			title: "Base: common ancestor",
			short: "Read-only original code from Git stage 1.",
			full:  "Compare the common ancestor with both sides before choosing a resolution.",
		}
	case 1:
		return guidedStageExplanation{
			title: "Current: your branch",
			short: "Read-only current-branch content from Git stage 2.",
			full:  "Read what your branch changed relative to the common ancestor.",
		}
	case 2:
		return guidedStageExplanation{
			title: "Incoming: other branch",
			short: "Read-only incoming content from Git stage 3.",
			full:  "Read what the incoming branch changed relative to the common ancestor.",
		}
	default:
		return guidedStageExplanation{
			title: "Result: editable resolution",
			short: "Choose a side or edit the working-tree resolution.",
			full:  "Review the Result, then use Mark resolved to stage the file exactly as it is.",
		}
	}
}

// guidedConflictStageGuide is scoped to the section the reader actually sees.
// Its own measured pane, not the window, selects page-length or concise help.
func (a *app) guidedConflictStageGuide(c *ui.Context) {
	theme := c.Theme()
	card := ui.Column(c).FillWidth().MinWidth(0).MaxWidthPercent(100).Gap(4).Padding(7, 9).
		Background(theme.Accent.Alpha(0.06)).Border(1, theme.Accent.Alpha(0.22)).Radius(theme.Radius).
		Label(tr("Conflict guidance"))
	// Zero is the first, uncommitted measurement. Concise is the safe initial
	// layout; measuredPaneWidth schedules the settled frame immediately.
	paneWidth := a.measuredPaneWidth(c, card)
	compact := paneWidth <= 0 || paneWidth < 480
	explanation := guidedConflictStageExplanation(a.conflictStageTab)
	card.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(7).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Icon(c, historyIcon).FontSize(14).TextColor(theme.Accent)
			ui.Text(c, tr(explanation.title)).Bold().Grow(1).MinWidth(0)
			if a.conflictState != nil && len(a.conflictState.regions) > 0 {
				ui.Badge(c, trf("Hunk %d of %d", a.conflictState.selected+1, len(a.conflictState.regions)))
			}
		})
		body := explanation.full
		if compact {
			body = explanation.short
		}
		ui.Text(c, tr(body)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
			MinWidth(0).MaxWidthPercent(100)
		if !compact && a.conflictStageTab != 3 {
			ui.Text(c, tr("This stage is read-only; Result remains editable in its own view.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).MinWidth(0).MaxWidthPercent(100)
		}
	})
}

// guidedOperationStep is a position in the resolution workflow, not saved
// progress. The current position is derived from the live operation, selected
// file and Result on every frame.
type guidedOperationStep int

const (
	guidedReadConflict guidedOperationStep = iota
	guidedChooseResult
	guidedMarkResolved
	guidedReviewStaged
	guidedContinue
)

func (a *app) guidedOperationSelection() (repository.File, bool) {
	if change, ok := a.selectedChange(); ok {
		return change.file, true
	}
	if a.conflictState != nil {
		return repository.File{Path: a.conflictState.path, HasMergeConflicts: true}, true
	}
	if a.conflictSides != nil {
		return a.conflictSides.file, true
	}
	return repository.File{}, false
}

func (a *app) guidedOperationCurrentStep() guidedOperationStep {
	if conflictedFileCount(a.snapshot.Files) > 0 {
		file, selected := a.guidedOperationSelection()
		if !selected || !file.HasMergeConflicts || a.conflictStageTab != 3 {
			return guidedReadConflict
		}
		if a.conflictState != nil && a.conflictState.path == file.Path && len(a.conflictState.regions) == 0 {
			return guidedMarkResolved
		}
		return guidedChooseResult
	}
	if hasStagedFiles(a.snapshot.Files) {
		return guidedReviewStaged
	}
	return guidedContinue
}

func guidedOperationStepStatus(step, current guidedOperationStep, continueAvailable bool) string {
	if step == current {
		return tr("Current")
	}
	if step < current {
		return tr("Earlier")
	}
	if step == guidedContinue && continueAvailable {
		return tr("Available")
	}
	return tr("Next")
}

// guidedOperationGuide teaches the existing workflow from real repository
// state. Earlier/Current/Next describe position without claiming that a step
// completed; in particular, an unmerged file never gets a false resolved tick.
func (a *app) guidedOperationGuide(c *ui.Context) {
	theme := c.Theme()
	if a.guidedOperationGuideHidden {
		if compactTextButton(ui.Button(c, tr("Show guide"))).Label(tr("Show guided conflict instructions")).Clicked() {
			a.guidedOperationGuideHidden = false
			c.Invalidate()
		}
		return
	}

	panel := ui.Column(c).FillWidth().MinWidth(0).Gap(8).Padding(10).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
		Label(tr("Guided conflict resolution"))
	panel.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			ui.Icon(c, conflictIcon).FontSize(15).TextColor(theme.Accent)
			ui.Text(c, tr("Guided conflict resolution")).Bold().Grow(1).MinWidth(0)
			if compactTextButton(ui.Button(c, tr("Hide guide"))).Label(tr("Hide guided conflict instructions")).Clicked() {
				a.guidedOperationGuideHidden = true
				c.Invalidate()
			}
		})

		ui.Row(c).FillWidth().MinWidth(0).Gap(6).Wrap().Children(func() {
			if a.snapshot.OperationState != nil {
				if progress := operationProgress(*a.snapshot.OperationState); progress != "" {
					ui.Badge(c, progress)
				}
			}
			conflicts := conflictedFileCount(a.snapshot.Files)
			if conflicts > 0 {
				ui.Textf(c, tr("%d conflict%s to resolve"), conflicts, plural(conflicts)).
					Bold().TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			} else {
				ui.Text(c, tr("No conflicts left to resolve")).
					TextColor(theme.Success).FontSize(theme.FontSize - 1)
			}
		})
		if file, selected := a.guidedOperationSelection(); selected && file.Path != "" {
			ui.Textf(c, tr("File: %s"), file.Path).MinWidth(0).MaxWidthPercent(100)
			if a.conflictState != nil && a.conflictState.path == file.Path && len(a.conflictState.regions) > 0 {
				ui.Textf(c, tr("Hunk %d of %d"), a.conflictState.selected+1, len(a.conflictState.regions)).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		}

		current := a.guidedOperationCurrentStep()
		continueAvailable := conflictedFileCount(a.snapshot.Files) == 0 && a.operationCan(repository.OperationActionContinue)
		ui.Column(c).FillWidth().MinWidth(0).Gap(4).Label(tr("Conflict resolution checklist")).Children(func() {
			for step, label := range []string{"Read conflict", "Choose or edit Result", "Mark resolved", "Review staged", "Continue"} {
				step := guidedOperationStep(step)
				status := guidedOperationStepStatus(step, current, continueAvailable)
				statusColor := theme.TextMuted
				if step == current {
					statusColor = theme.Accent
				} else if step == guidedContinue && continueAvailable {
					statusColor = theme.Success
				}
				ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(7).
					Label(tr(label) + ": " + status).Children(func() {
					ui.Text(c, tr(label)).Grow(1).MinWidth(0)
					ui.Badge(c, status).FontSize(theme.FontSize - 2).TextColor(statusColor)
				})
			}
		})

		ui.Text(c, tr("Made a mistake here? Restore unresolved returns the saved unresolved file and Git stages.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MinWidth(0).MaxWidthPercent(100)
		path := ""
		if file, selected := a.guidedOperationSelection(); selected {
			path = file.Path
		}
		record, available := a.latestFileRecovery(path, true)
		if !available {
			ui.Text(c, tr("No recovery is available for the selected file.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MinWidth(0).MaxWidthPercent(100)
			if a.hasRestorableFileRecovery() {
				if compactTextButton(ui.Button(c, tr("Show recovery and checkpoints"))).Label(tr("Show recovery and checkpoints")).Clicked() {
					a.savedWorkPanel = 0
					a.showRecoveryModal = true
				}
			}
			return
		}

		status, statusTip := fileRecoveryStatus(record)
		statusColor := theme.Accent
		if record.Status == repository.FileRecoveryInterrupted {
			statusColor = theme.Danger
		}
		ui.Row(c).FillWidth().MinWidth(0).Gap(7).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Badge(c, status).Background(statusColor.Alpha(0.14)).TextColor(statusColor).Tooltip(tr(statusTip))
			actionLabel := tr("Restore unresolved")
			if record.Status == repository.FileRecoveryInterrupted {
				actionLabel = tr("Retry")
			}
			button := compactTextButton(ui.Button(c, actionLabel)).Label(actionLabel).
				Tooltip(tr("Restore this saved state; newer changes to the same file stop the compare-and-swap")).
				Disabled(a.actionBusy)
			if button.Clicked() {
				a.confirmRestoreFileRecovery(record)
			}
		})
		if record.Status == repository.FileRecoveryInterrupted && record.Error != "" {
			ui.Text(c, record.Error).TextColor(theme.Danger).FontSize(theme.FontSize - 2).
				MinWidth(0).MaxWidthPercent(100)
		}
	})
}
