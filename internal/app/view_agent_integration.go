package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *app) agentIntegrationSelectionCount() int {
	count := 0
	for _, task := range a.agentIntegration.ordered {
		if selected := a.agentIntegration.selected[task.ID]; selected != nil && *selected {
			count++
		}
	}
	return count
}

func (a *app) agentIntegrationModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.agentIntegration.open, 900, "Agent integration dry run",
		"Explicit captured-SHA merges in a detached worktree. Integration success never means task readiness.", worktreesIcon, func() {
			theme := c.Theme()
			state := &a.agentIntegration
			ui.Text(c, tr("DRY RUN · No push, foreground checkout, task worktree, task status, or durable BaseCommit is changed. Dependencies must be selected explicitly; archived tasks are not offered.")).TextColor(theme.Warning).Selectable()
			if state.running {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					ui.Text(c, tr("Capturing exact task HEADs and merging only those immutable SHAs…")).TextColor(theme.TextMuted)
				})
			}
			if state.cleaning {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					ui.Text(c, tr("Removing only the confirmed isolated worktree…")).TextColor(theme.TextMuted)
				})
			}

			ui.Scroll(c).MaxHeight(480).FillWidth().Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					if !state.completed {
						ui.Text(c, tr("Selected active tasks · dependency-first order")).Bold()
						for index, task := range state.ordered {
							selected := state.selected[task.ID]
							if selected == nil {
								continue
							}
							ui.Column(c).Gap(2).Children(func() {
								ui.Checkbox(c, selected, fmt.Sprintf("%d. %s · %s", index+1, task.Title, agentShortHash(task.ID))).Disabled(state.running || state.cleaning)
								ui.Text(c, tr("Worktree: ")+task.WorktreePath).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Selectable()
								ui.Text(c, tr("Recorded BaseCommit: ")+task.BaseCommit).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Selectable()
							})
						}
					}

					if state.baseCommit != "" {
						ui.Text(c, tr("Immutable integration BASE SHA: ")+state.baseCommit).Bold().Selectable()
					}
					if len(state.captured) > 0 {
						ui.Text(c, tr("Exact task captures · branch names are never merged")).Bold()
						for _, captured := range state.captured {
							color := theme.TextMuted
							if captured.DirtySummary != "" {
								color = theme.Warning
							}
							ui.Column(c).Gap(2).Children(func() {
								ui.Text(c, captured.Task.Title+" · "+agentShortHash(captured.Task.ID)).Bold().Selectable()
								ui.Text(c, "CAPTURED_HEAD "+captured.CapturedHead).TextColor(theme.Accent).Selectable()
								ui.Text(c, "BaseCommit "+captured.Task.BaseCommit).TextColor(color).Selectable()
								ui.Text(c, tr("Worktree: ")+captured.Task.WorktreePath).TextColor(color).Selectable()
								if captured.DirtySummary != "" {
									ui.Text(c, tr("EXCLUDED · ")+captured.DirtySummary).TextColor(theme.Warning).Selectable()
								}
							})
						}
					}
					if len(state.exclusions) > 0 {
						ui.Text(c, tr("NON-CLEAN SELECTION · excluded or unavailable tasks")).TextColor(theme.Warning).Bold()
						for _, exclusion := range state.exclusions {
							ui.Text(c, exclusion.Title+" · "+exclusion.Reason).TextColor(theme.Warning).Selectable()
						}
					}
					if len(state.steps) > 0 {
						ui.Text(c, tr("Per-step integration results")).Bold()
						for index, step := range state.steps {
							label := tr("Merged")
							color := theme.Success
							switch step.Status {
							case agentIntegrationStopped:
								label, color = tr("STOPPED · conflict or error"), theme.Danger
							case agentIntegrationNotRun:
								label, color = tr("Not run"), theme.TextMuted
							}
							ui.Column(c).Gap(2).Children(func() {
								ui.Textf(c, "%d. %s · %s", index+1, step.Title, label).TextColor(color).Bold().Selectable()
								ui.Text(c, "CAPTURED_HEAD "+step.CapturedHead).TextColor(color).Selectable()
								if step.Detail != "" {
									ui.Text(c, step.Detail).TextColor(color).Selectable()
								}
							})
						}
					}
				})
			})

			if state.err != "" {
				color := theme.Warning
				if state.stopped {
					color = theme.Danger
				}
				ui.Text(c, state.err).TextColor(color).Selectable()
			}
			if state.completed && state.succeeded {
				color := theme.Success
				message := tr("Dry-run integration completed. This is only an integration result; tasks are not marked ready or done.")
				if len(state.exclusions) > 0 {
					color = theme.Warning
					message = tr("Dry-run integration completed with NON-CLEAN exclusions. Review them; task readiness is unchanged.")
				}
				ui.Text(c, message).TextColor(color).Selectable()
			}
			if state.completed && state.stopped {
				ui.Text(c, tr("Dry run stopped at the first merge conflict/error. Later tasks were not attempted; the isolated worktree is retained for inspection.")).TextColor(theme.Danger).Selectable()
			}
			if state.worktreePath != "" {
				if state.worktreeCreated {
					ui.Text(c, tr("Detached integration worktree · KEPT FOR INSPECTION")).Bold()
				} else if state.cleanupDone {
					ui.Text(c, tr("Detached integration worktree · explicitly removed")).TextColor(theme.TextMuted)
				} else {
					ui.Text(c, tr("Requested detached integration worktree path")).TextColor(theme.TextMuted)
				}
				ui.Text(c, state.worktreePath).TextColor(theme.Accent).Selectable()
			}
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if !state.completed {
					selected := a.agentIntegrationSelectionCount()
					if ui.PrimaryButton(c, tr("Review dry-run confirmation…")).Disabled(state.running || state.cleaning || selected == 0).Clicked() {
						a.beginAgentIntegrationDryRun()
					}
				}
				if state.worktreeCreated {
					if ui.Button(c, tr("Force remove isolated worktree…")).Disabled(state.running || state.cleaning).Clicked() {
						a.confirmAgentIntegrationCleanup()
					}
				}
			})
		})
}
