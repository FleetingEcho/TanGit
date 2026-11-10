package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *app) agentUpdateSelectionCount() int {
	count := 0
	for _, task := range a.agents.updates.ordered {
		choice := a.agents.updates.selected[task.ID]
		if choice != nil && *choice {
			count++
		}
	}
	return count
}

func (a *app) agentUpdateRebaseReadyCount() int {
	count := 0
	for _, result := range a.agents.updates.results {
		choice := a.agents.updates.selected[result.TaskID]
		if choice != nil && *choice && result.Checked && result.Status == agentUpdatePending && result.HeadSHA != "" {
			count++
		}
	}
	return count
}

func agentUpdateResultLabel(status agentUpdateResultStatus) string {
	switch status {
	case agentUpdateRunning:
		return "Running"
	case agentUpdateSucceeded:
		return "Succeeded"
	case agentUpdateConflicted:
		return "Conflicted"
	case agentUpdateFailed:
		return "Failed"
	default:
		return "Pending"
	}
}

func (a *app) agentUpdateModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.agents.updates.open, 920, "Coordinated agent updates",
		"Fetch once, check exact task worktrees, then explicitly rebase selected tasks.", worktreesIcon, func() {
			theme := c.Theme()
			state := &a.agents.updates
			busy := state.checking || state.running
			ui.Text(c, tr("No push, readiness inference, task lifecycle change, or durable BaseCommit rewrite. Ahead/behind describes commits only. A durable safety checkpoint is required before every rebase.")).TextColor(theme.Warning).Selectable()
			if busy {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					message := tr("Fetching once and checking explicit task targets…")
					if state.running {
						message = tr("Processing explicit task targets sequentially…")
					}
					ui.Text(c, message).TextColor(theme.TextMuted)
				})
			}

			ui.Scroll(c).MaxHeight(440).FillWidth().Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					ui.Text(c, tr("Explicit task selection · nothing is selected by default")).Bold()
					for _, task := range state.ordered {
						choice := state.selected[task.ID]
						if choice == nil {
							continue
						}
						c.Key("agent-update-selection:" + task.ID)
						ui.Column(c).Gap(2).Children(func() {
							ui.Checkbox(c, choice, task.Title+" · "+agentShortHash(task.ID)).Disabled(busy)
							ui.Text(c, tr("BaseRef: ")+task.BaseRef).FontSize(theme.FontSize - 1).TextColor(theme.Accent).Selectable()
							ui.Text(c, tr("Fixed BaseCommit: ")+task.BaseCommit).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Selectable()
							ui.Text(c, tr("Worktree: ")+task.WorktreePath).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Selectable()
						})
					}

					if len(state.results) > 0 {
						ui.Text(c, tr("Per-task status captures and update results")).Bold().Padding(6, 0, 0, 0)
						for _, result := range state.results {
							color := theme.TextMuted
							switch result.Status {
							case agentUpdateSucceeded:
								color = theme.Success
							case agentUpdateFailed:
								color = theme.Danger
							case agentUpdateConflicted:
								color = theme.Warning
							case agentUpdateRunning:
								color = theme.Accent
							}
							c.Key("agent-update-result:" + result.TaskID)
							ui.Column(c).Gap(3).Padding(6, 0).Children(func() {
								ui.Text(c, result.Title+" · "+tr(agentUpdateResultLabel(result.Status))).TextColor(color).Bold().Selectable()
								ui.Text(c, fmt.Sprintf("BaseRef %s · ahead %d · behind %d", result.BaseRef, result.Ahead, result.Behind)).TextColor(color).Selectable()
								if result.HeadSHA != "" {
									ui.Text(c, "CAPTURED_HEAD "+result.HeadSHA).TextColor(color).Selectable()
								}
								if result.BaseSHA != "" {
									ui.Text(c, "RESOLVED_BASE "+result.BaseSHA).TextColor(color).Selectable()
								}
								ui.Text(c, "FIXED_BASE_COMMIT "+result.BaseCommit).TextColor(theme.TextMuted).Selectable()
								if result.Error != "" {
									ui.Text(c, result.Error).TextColor(theme.Danger).Selectable()
								}
								if result.Detail != "" {
									ui.Text(c, result.Detail).TextColor(color).Selectable()
								}
								if result.Status == agentUpdateConflicted {
									ui.Row(c).Gap(8).Children(func() {
										if ui.Button(c, tr("Continue rebase…")).Disabled(busy).Clicked() {
											a.confirmAgentUpdateRecovery(result.TaskID, false)
										}
										if ui.Button(c, tr("Abort rebase…")).Disabled(busy).Clicked() {
											a.confirmAgentUpdateRecovery(result.TaskID, true)
										}
									})
								}
							})
						}
					}
				})
			})

			if state.err != "" {
				ui.Text(c, state.err).TextColor(theme.Warning).Selectable()
			}
			if state.cancelRequested && busy {
				ui.Text(c, tr("No new task will be scheduled. The current Git operation still reports its actual outcome.")).TextColor(theme.TextMuted)
			}
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if busy {
					if ui.Button(c, tr("Stop after current task")).Disabled(state.cancelRequested).Clicked() {
						a.stopAgentUpdateScheduling()
					}
				}
				if ui.Button(c, tr("Fetch and check")).Disabled(busy || a.session == nil || a.agentUpdateSelectionCount() == 0).Clicked() {
					a.beginAgentUpdateFetchAndCheck()
				}
				if ui.PrimaryButton(c, tr("Rebase selected")).Disabled(busy || a.agentUpdateRebaseReadyCount() == 0).Clicked() {
					a.beginAgentUpdateRebase()
				}
			})
		})
}
