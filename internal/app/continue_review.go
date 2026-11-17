package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func continueReviewCheckView(c *ui.Context, key, name string, check continueCheck) {
	theme := c.Theme()
	statusColor := theme.TextMuted
	switch check.status {
	case continueCheckPassed, continueCheckSkipped:
		statusColor = theme.Success
	case continueCheckFailed, continueCheckCancelled:
		statusColor = theme.Danger
	case continueCheckRunning:
		statusColor = theme.Accent
	case continueCheckNotConfigured:
		statusColor = theme.Warning
	}
	c.Key(key)
	ui.Column(c).FillWidth().Gap(5).Padding(10).Background(theme.Surface.Alpha(0.58)).
		Border(1, theme.Border).Radius(theme.Radius + 2).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, trf("%s: %s", tr(name), tr(string(check.status)))).Bold().TextColor(statusColor)
			if check.command != "" {
				ui.Text(c, check.command).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
					SingleLine().MinWidth(0).Tooltip(check.command)
			}
		})
		if check.status == continueCheckNotConfigured {
			ui.Text(c, tr("Required check is not configured. Continue cannot run until a fully trusted shell command is configured.")).
				TextColor(theme.Warning)
		}
		if check.err != "" {
			ui.Text(c, check.err).TextColor(statusColor).FontSize(theme.FontSize - 1).Selectable()
		}
		if check.output != "" {
			ui.Text(c, tr("Output")).FontSize(theme.FontSize - 1).Bold()
			ui.Text(c, check.output).Selectable().MinWidth(0)
		}
	})
}

func continueReviewNeedsRetry(review *continueReview) bool {
	return review.complete && (review.stale || review.sourceErr != "" ||
		review.required.status == continueCheckFailed || review.required.status == continueCheckCancelled ||
		review.optional.status == continueCheckFailed || review.optional.status == continueCheckCancelled ||
		review.checkpointStatus == continueCheckpointFailed)
}

func (a *app) configureContinueCommands() {
	a.closeContinueReview()
	a.openSettingsSection(settingsPageBehavior)
}

// continueReviewModal shows the checks, their real output, canonical changed
// paths and checkpoint outcome before the final driver-backed action. Disabled
// Continue always has a concrete gate reason; it is never a status-formality.
func (a *app) continueReviewModal(c *ui.Context) {
	review := a.continuePreflight.review
	if review == nil {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		c.Key("continue-review-modal")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.4)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(760).Height(700).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(20).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 6).Clip().
				Role(ui.RoleDialog).Label(tr("Continue preflight")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(36, 36).Radius(theme.Radius + 2).Center().Background(theme.Accent.Alpha(0.13)).Children(func() {
						ui.Icon(c, continueIcon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Continue preflight")).FontSize(19).Bold()
						ui.Text(c, tr("Run trusted checks, review their changes, then use the operation driver.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close Continue review", "Cancel checks and close", closeIcon, false, a.closeContinueReview)
				})
				ui.Text(c, tr("Commands are fully trusted local shell commands, not sandboxed. They may use the network and write files. Required and optional checks have different gates.")).
					TextColor(theme.Warning).FontSize(theme.FontSize - 1)
				ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(10).Padding(1, 3, 4, 1).Children(func() {
						continueReviewCheckView(c, "continue-required-check", "Required", review.required)
						continueReviewCheckView(c, "continue-optional-check", "Optional", review.optional)
						ui.Column(c).FillWidth().Gap(5).Padding(10).Background(theme.Surface.Alpha(0.45)).
							Border(1, theme.Border).Radius(theme.Radius + 2).Children(func() {
							ui.Text(c, tr("Source review")).Bold()
							if review.capturePending {
								ui.Text(c, tr("Capturing the operation, canonical index, and working-tree content…")).
									TextColor(theme.TextMuted)
							}
							if review.sourceErr != "" {
								if review.stale {
									ui.Text(c, tr("Operation changed during checks")).Bold().TextColor(theme.Danger)
								}
								ui.Text(c, review.sourceErr).TextColor(theme.Danger).Selectable()
							}
							if review.hasReviewed {
								delta := repository.CompareContinueSources(review.source, review.reviewed)
								ui.Text(c, tr("Changes made by checks")).Bold()
								switch {
								case !delta.ContentChanged():
									ui.Text(c, tr("No changes")).TextColor(theme.TextMuted)
								case len(review.changedFiles) == 0:
									ui.Text(c, tr("Working-tree or index content changed")).TextColor(theme.Warning)
								default:
									ui.Text(c, strings.Join(review.changedFiles, "\n")).Selectable()
								}
							}
						})
						ui.Column(c).FillWidth().Gap(4).Padding(10).Background(theme.Surface.Alpha(0.45)).
							Border(1, theme.Border).Radius(theme.Radius + 2).Children(func() {
							switch review.checkpointStatus {
							case continueCheckpointCreated:
								ui.Text(c, tr("Checkpoint: created")).Bold().TextColor(theme.Success)
								ui.Text(c, review.checkpointID).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Selectable()
							case continueCheckpointFailed:
								ui.Text(c, tr("Checkpoint failed")).Bold().TextColor(theme.Danger)
								ui.Text(c, review.checkpointErr).TextColor(theme.Danger).Selectable()
							case continueCheckpointSkipped:
								ui.Text(c, tr("Checkpoint: skipped")).Bold()
								ui.Text(c, tr("No checkpoint is needed when checks make no changes; cancellation also cannot continue.")).
									TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
							default:
								ui.Text(c, tr("Checkpoint: pending")).TextColor(theme.TextMuted)
							}
						})
					})
				})
				override := review.optional.status == continueCheckFailed
				allowed, reason := a.continueGate(override)
				if !override {
					allowed, reason = a.continueGate(false)
				}
				if reason != "" {
					ui.Text(c, reason).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Tooltip(reason)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					if ui.Button(c, tr("Close review")).Clicked() {
						a.closeContinueReview()
					}
					if review.required.status == continueCheckNotConfigured && ui.Button(c, tr("Configure commands")).Clicked() {
						a.configureContinueCommands()
					}
					if !review.complete {
						if ui.Button(c, tr("Cancel checks")).Disabled(review.cancelRequested).Clicked() {
							a.cancelContinueChecks()
						}
					} else if continueReviewNeedsRetry(review) {
						if ui.Button(c, tr("Retry checks")).Disabled(a.actionBusy).Clicked() {
							a.retryContinueReview()
						}
					}
					ui.Spacer(c).Grow(1)
					label := tr("Continue operation")
					if override {
						label = tr("Override optional failure and continue")
					}
					if ui.PrimaryButton(c, label).Disabled(!allowed || a.actionBusy).Tooltip(reason).Clicked() {
						a.finishContinueReview(override)
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeContinueReview()
			}
		})
	})
}

// continuePreflightSettings is kept next to the behavior controls while the
// broader Guided setting can be integrated independently. Required blank is a
// hard stop; optional blank is an explicit skipped check.
func (a *app) continuePreflightSettings(c *ui.Context) {
	theme := c.Theme()
	settingsGroup(c, continueIcon, "Continue preflight", "Required safety checks and an optional quality gate")
	ui.Column(c).Gap(12).Padding(15).Background(theme.Surface.Alpha(0.55)).
		Border(1, theme.Border).Radius(theme.Radius + 5).Children(func() {
		ui.Text(c, tr("These are fully trusted local shell commands, not sandboxed. They may use the network and write files. SSH workspaces disable Continue checks.")).
			TextColor(theme.Warning).FontSize(theme.FontSize - 1).MaxWidth(560)
		settingRow(c, "Required check command", "Blank disables Continue until configured", func() {
			if ui.TextInput(c, &a.settings.ContinuePreflight.RequiredCommand).
				Label(tr("Required Continue check command")).Placeholder(tr("go test ./...")).Grow(1).Changed() {
				a.settingsChanged(c)
			}
		})
		settingRow(c, "Optional check command", "Blank is skipped; failure needs explicit override", func() {
			if ui.TextInput(c, &a.settings.ContinuePreflight.OptionalCommand).
				Label(tr("Optional Continue check command")).Placeholder(tr("golangci-lint run")).Grow(1).Changed() {
				a.settingsChanged(c)
			}
		})
		ui.Text(c, tr("After checks, TanGit compares canonical index and working-tree hashes, shows changed files and output, and checkpoints any changes. Continue still passes through the operation driver's expected-fingerprint CAS.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(560)
	})
}
