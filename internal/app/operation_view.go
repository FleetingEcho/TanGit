package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func shortOperationSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func operationProgress(state repository.OperationState) string {
	if state.Step > 0 && state.Total > 0 {
		return trf("Step %d of %d", state.Step, state.Total)
	}
	return ""
}

func operationTooltip(state repository.OperationState) string {
	var lines []string
	if len(state.FullRefs) > 0 {
		lines = append(lines, strings.Join(state.FullRefs, "\n"))
	}
	for _, item := range []struct {
		label string
		sha   string
	}{{"onto", state.OntoSHA}, {"orig", state.OrigSHA}, {"incoming", state.IncomingSHA}, {"stopped", state.StoppedSHA}} {
		if item.sha != "" {
			lines = append(lines, item.label+": "+item.sha)
		}
	}
	if state.HistoryPatch != nil {
		lines = append(lines,
			"history kind: "+string(state.HistoryPatch.Kind),
			"history step: "+string(state.HistoryPatch.Step),
			"source: "+state.HistoryPatch.SourceSHA,
			"destination: "+state.HistoryPatch.DestinationSHA,
			"patch digest: "+state.HistoryPatch.PatchDigest,
			"compensation digest: "+state.HistoryPatch.CompensationSHA)
	}
	return strings.Join(lines, "\n")
}

// operationControls keeps recovery actions with the operation context in the
// Changes pane. Continue and Abort retain the same preflight, fingerprint and
// confirmation paths they used in the repository header.
func (a *app) operationControls(c *ui.Context) {
	conflicts := conflictedFileCount(a.snapshot.Files)
	continueTooltip := tr("Continue the interrupted operation")
	if conflicts > 0 {
		continueTooltip = trf("Resolve conflicts before continuing (%d remaining)", conflicts)
	}
	ui.Row(c).FillWidth().MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Gap(6).Wrap().
		Padding(0, 10, 8, 10).Label(tr("Operation context")).Children(func() {
		if a.operationCan(repository.OperationActionContinue) {
			if a.location.IsRemote() {
				continueTooltip = tr("Continue checks use fully trusted local commands and are disabled for SSH workspaces")
			}
			iconTextButtonState(c, "Continue", continueTooltip, continueIcon, false,
				a.actionBusy || conflicts > 0 || a.location.IsRemote(), func() {
					if a.actionBusy || hasConflictedFiles(a.snapshot.Files) {
						return
					}
					a.confirmContinueOperation()
				})
		}
		if a.operationCan(repository.OperationActionSkip) {
			iconTextButtonState(c, "Skip", "Skip the stopped step and continue the remaining operation", skipIcon, false,
				a.actionBusy, a.confirmSkipOperation)
		}
		if a.operationCan(repository.OperationActionAbort) {
			iconTextButton(c, "Abort", "Abort the interrupted operation", abortIcon, a.actionBusy,
				a.confirmAbortOperation)
		}
	})
}

// operationContextView is the compact recovery card above Changes. Every ref,
// SHA and count comes from Git's live operation metadata; no commit list or
// short branch name is used to fill in missing facts.
func (a *app) operationContextView(c *ui.Context) {
	state := a.snapshot.OperationState
	if state == nil {
		return
	}
	theme := c.Theme()
	tooltip := operationTooltip(*state)
	card := ui.Column(c).FillWidth().Gap(6).Label(tr("Operation context"))
	card.Children(func() {
		// Adjust on the Changes pane's own width rather than the window's.
		// Compact space is spent on the operation and its resolution.
		paneWidth := a.measuredPaneWidth(c, card)
		compact := paneWidth <= 0 || paneWidth < 360
		guidedDetail := normalizeConflictActionStyle(a.settings.ConflictActionStyle) == conflictActionStyleGuided &&
			!a.guidedOperationGuideHidden
		panel := ui.Column(c).FillWidth().MinWidth(0).
			Background(theme.Accent.Alpha(0.09)).Border(1, theme.Accent.Alpha(0.28)).Radius(theme.Radius).
			Tooltip(tooltip)
		panel.Children(func() {
			ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(10).Padding(8, 10).Children(func() {
				ui.Icon(c, branchIcon).FontSize(16).TextColor(theme.Accent)
				context := strings.Join(state.FullRefs, " · ")
				if compact {
					context = strings.TrimPrefix(context, "refs/heads/")
					context = strings.TrimPrefix(context, "refs/remotes/")
				}
				if context == "" {
					context = shortOperationSHA(state.StoppedSHA)
					if context == "" {
						context = shortOperationSHA(state.OntoSHA)
					}
				}
				ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(7).Wrap().Children(func() {
						ui.Text(c, strings.ToUpper(string(state.Type))).Bold().TextColor(theme.Accent)
						if progress := operationProgress(*state); progress != "" && !guidedDetail {
							ui.Badge(c, progress)
						}
						if state.HistoryPatch != nil {
							ui.Badge(c, tr("Custom Patch")+" · "+string(state.HistoryPatch.Step))
						}
						if state.Done > 0 && !compact {
							ui.Text(c, trf("%d completed", state.Done)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						}
					})
					if context != "" {
						contextLine := ui.Text(c, context).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Tooltip(tooltip)
						if guidedDetail {
							contextLine.MinWidth(0).MaxWidthPercent(100)
						} else {
							contextLine.SingleLine()
						}
					}
					if conflicts := conflictedFileCount(a.snapshot.Files); conflicts > 0 && !guidedDetail {
						ui.Textf(c, tr("%d conflict%s to resolve"), conflicts, plural(conflicts)).
							Bold().TextColor(theme.Danger).FontSize(theme.FontSize - 1)
						// Keep the guidance on its own line instead of squeezing it
						// into the conflict count when space is tight.
						ui.Text(c, tr("Pick a side, then Continue.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					}
				})
				if state.Type == repository.OperationTypeRebase || state.Type == repository.OperationTypePullRebase {
					after := ui.ButtonBase(c).Size(30, 26).Center().Label(tr("Back to Rebase TODO")).
						Tooltip(tr("Return to the real rebase steps after resolving conflicts"))
					after.Children(func() { ui.Icon(c, rebaseIcon).FontSize(15).TextColor(theme.Accent) })
					if after.Clicked() {
						a.openRebaseWorkspace()
					}
				}
				if a.hasRestorableFileRecovery() {
					rowIconButton(c, tr("Conflict recovery"), tr("Restore an original unresolved file and its index stages"), softResetIcon, func() {
						a.savedWorkPanel = 0
						a.showRecoveryModal = true
					})
				}
			})
			a.operationControls(c)
		})
		if normalizeConflictActionStyle(a.settings.ConflictActionStyle) == conflictActionStyleGuided {
			a.guidedOperationGuide(c)
		}
	})
}
