package app

import (
	"github.com/egoist/mygo/ui"
)

func agentBatchStatusLabel(status agentBatchResultStatus) string {
	switch status {
	case agentBatchQueued:
		return "Queued"
	case agentBatchRunning:
		return "Running"
	case agentBatchPassed:
		return "Passed"
	case agentBatchFailed:
		return "Failed"
	case agentBatchSkipped:
		return "Skipped"
	case agentBatchCurrent:
		return "Open"
	case agentBatchVisited:
		return "Visited"
	case agentBatchExcluded:
		return "Unavailable"
	default:
		return "Pending"
	}
}

func (a *app) agentBatchModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.agents.batch.open, 860, "Coordinated tests and reviews",
		"Select explicit tasks. Test results and review visits remain independent and never imply readiness.", commandIcon, func() {
			theme := c.Theme()
			state := &a.agents.batch
			pending := agentBatchHasPendingTests(state.tests)
			ui.Text(c, tr("Nothing is selected by default. Queued tests use available terminal capacity; stopping the queue never kills a running process. Review queue visits are inspection only.")).TextColor(theme.TextMuted).Selectable()
			ui.Scroll(c).MaxHeight(460).FillWidth().Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					ui.Text(c, tr("Explicit task selection")).Bold()
					if len(state.ordered) == 0 {
						ui.Text(c, tr("No active agent tasks are available.")).TextColor(theme.TextMuted)
					}
					for _, task := range state.ordered {
						choice := state.selected[task.ID]
						if choice == nil {
							continue
						}
						c.Key("agent-batch-selection:" + task.ID)
						ui.Column(c).Gap(2).Children(func() {
							ui.Checkbox(c, choice, task.Title+" · "+agentShortHash(task.ID)).Disabled(pending)
							if task.Profile.TestCommand == "" {
								ui.Text(c, tr("No test command configured")).FontSize(theme.FontSize - 1).TextColor(theme.Warning)
							} else {
								ui.Text(c, task.Profile.TestCommand).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine().Tooltip(task.Profile.TestCommand)
							}
						})
					}

					if len(state.tests) > 0 {
						ui.Text(c, tr("Independent test queue results")).Bold().Padding(6, 0, 0, 0)
						for _, result := range state.tests {
							color := theme.TextMuted
							switch result.Status {
							case agentBatchRunning:
								color = theme.Accent
							case agentBatchPassed:
								color = theme.Success
							case agentBatchFailed:
								color = theme.Danger
							case agentBatchSkipped:
								color = theme.Warning
							}
							c.Key("agent-batch-test:" + result.TaskID)
							ui.Column(c).Gap(2).Children(func() {
								ui.Text(c, result.Title+" · "+tr(agentBatchStatusLabel(result.Status))).TextColor(color).Bold()
								if result.Detail != "" {
									ui.Text(c, result.Detail).TextColor(color).Selectable()
								}
							})
						}
					}

					if len(state.reviews) > 0 {
						ui.Text(c, tr("Review queue · visits are not approvals")).Bold().Padding(6, 0, 0, 0)
						for _, result := range state.reviews {
							color := theme.TextMuted
							if result.Status == agentBatchCurrent {
								color = theme.Accent
							} else if result.Status == agentBatchExcluded {
								color = theme.Warning
							}
							c.Key("agent-batch-review:" + result.TaskID)
							ui.Column(c).Gap(2).Children(func() {
								ui.Text(c, result.Title+" · "+tr(agentBatchStatusLabel(result.Status))).TextColor(color).Bold()
								if result.Detail != "" {
									ui.Text(c, result.Detail).TextColor(color).Selectable()
								}
							})
						}
					}
				})
			})
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if pending {
					if ui.Button(c, tr("Stop queued tests")).Clicked() {
						a.stopQueuedAgentTests()
					}
					if ui.Button(c, tr("Try waiting tests")).Clicked() {
						a.pumpAgentTestBatch()
					}
				}
				if ui.Button(c, tr("Start review queue")).Disabled(pending || len(state.ordered) == 0).Clicked() {
					a.startSelectedAgentReviews()
				}
				if ui.PrimaryButton(c, tr("Queue selected tests")).Disabled(pending || len(state.ordered) == 0).Clicked() {
					a.queueSelectedAgentTests()
				}
			})
		})
}
