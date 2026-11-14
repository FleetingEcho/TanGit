package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/agentwork"
)

func (a *app) agentTaskFormModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.agents.formOpen, 760, "New Agent Task",
		"Create a durable task and an isolated worktree for a coding agent.", fileBracesIcon, func() {
			ui.TextInput(c, &a.agents.title).Label(tr("Task title")).Placeholder(tr("Add repository search")).AutoFocus()
			ui.TextArea(c, &a.agents.request).Label(tr("Agent request")).Lines(4, 7).
				Placeholder(tr("Describe the desired result and its acceptance criteria"))
			ui.Select(c, &a.agents.base, a.worktreeRefOptions()).Label(tr("Base ref"))
			ui.TextInput(c, &a.agents.branch).Label(tr("New branch")).Placeholder(tr("agent/repository-search"))
			ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
				ui.TextInput(c, &a.agents.path).Label(tr("Worktree path")).Placeholder(tr("/path/to/agent-worktree")).Grow(1)
				if ui.Button(c, tr("Use suggested path")).Clicked() {
					a.agents.path = a.suggestedAgentTaskPath()
				}
			})
			ui.Text(c, tr("Agent command templates may use {worktree}, {task}, {request}, {promptFile}, {base}, {branch}, and {sessionId}. Values are shell-quoted.")).TextColor(c.Theme().TextMuted)
			ui.TextInput(c, &a.agents.launchCommand).Label(tr("Launch command")).Placeholder(tr("agent --prompt-file {promptFile}"))
			ui.TextInput(c, &a.agents.resumeCommand).Label(tr("Resume command (optional)")).Placeholder(tr("agent resume {sessionId}"))
			ui.Row(c).Gap(10).Children(func() {
				ui.TextInput(c, &a.agents.setupCommand).Label(tr("Setup command (optional)")).Grow(1)
				ui.TextInput(c, &a.agents.testCommand).Label(tr("Test command (optional)")).Grow(1)
			})
			ui.Checkbox(c, &a.agents.launchAfterAdd, tr("Launch after creating the worktree"))
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
					a.agents.formOpen = false
				}
				invalid := strings.TrimSpace(a.agents.title) == "" ||
					strings.TrimSpace(a.agents.base) == "" ||
					strings.TrimSpace(a.agents.branch) == "" || strings.TrimSpace(a.agents.branch) == "agent/" ||
					strings.TrimSpace(a.agents.path) == "" ||
					(a.agents.launchAfterAdd && strings.TrimSpace(a.agents.launchCommand) == "")
				if ui.PrimaryButton(c, tr("Create agent task")).Disabled(a.actionBusy || invalid).Clicked() {
					a.beginCreateAgentTask()
				}
			})
		})
}

func (a *app) agentsView(c *ui.Context) {
	if a.agents.review.open {
		a.agentReviewView(c)
		return
	}
	theme := c.Theme()
	ui.Column(c).Grow(1).Children(func() {
		a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionAgents],
			"Expand task list", func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
					paneTitleAction(c, fileBracesIcon, "Agent tasks", "New Agent Task…",
						"Isolated coding-agent work with a durable task identity", a.actionBusy, a.beginNewAgentTask, func() {
							paneCollapseButton(c, &a.workspacePaneCollapsed[sectionAgents], "Collapse list pane")
						})
					ui.Row(c).Gap(8).Children(func() {
						if ui.Button(c, tr("Refresh tasks")).Clicked() {
							a.refreshAgentTasks()
						}
						if a.agents.loading {
							ui.Text(c, tr("Loading…")).TextColor(theme.TextMuted)
						}
					})
					if a.agents.err != "" {
						ui.Text(c, a.agents.err).TextColor(theme.Danger).Selectable()
						return
					}
					if len(a.agents.tasks) == 0 {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("No agent tasks in this repository")).TextColor(theme.TextMuted)
							ui.Text(c, tr("Create a task to give an agent its own worktree.")).TextColor(theme.TextMuted)
						})
						return
					}
					a.agents.list.Selected = &a.agents.selectedRow
					a.agents.list.Key = func(index int) any { return a.agents.tasks[index].ID }
					list := a.keyboardList(c, &a.agents.list, len(a.agents.tasks)).Grow(1).Rows(func(row ui.ListRow) {
						task := a.agents.tasks[row.Index]
						ui.Row(row.Context).Gap(8).Padding(7, 8).Children(func() {
							ui.Text(row.Context, task.Title).Grow(1)
							ui.Badge(row.Context, tr(agentTaskStatusLabel(task.Status)))
							if task.Status == agentwork.StatusRunning {
								ui.Text(row.Context, tr("●")).TextColor(theme.Success)
							}
						}).OnClick(func() {
							a.agents.selectedTaskID = task.ID
						})
					})
					if a.keyboardListChanged(&a.agents.list, list) {
						index := min(max(0, a.agents.selectedRow), len(a.agents.tasks)-1)
						if index >= 0 && index < len(a.agents.tasks) {
							a.agents.selectedTaskID = a.agents.tasks[index].ID
						}
					}
				})
			}, func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
					task, ok := a.selectedAgentTask()
					if !ok {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("Select an agent task")).TextColor(theme.TextMuted)
						})
						return
					}
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						ui.Text(c, task.Title).FontSize(18).Bold().Grow(1)
						ui.Badge(c, tr(agentTaskStatusLabel(task.Status)))
					})
					ui.Text(c, task.WorktreePath).TextColor(theme.TextMuted).Selectable()
					ui.Textf(c, tr("Branch: %s"), task.Branch).Selectable()
					ui.Textf(c, tr("Base: %s (%s)"), task.BaseRef, agentShortHash(task.BaseCommit)).Selectable()
					for _, worktree := range a.snapshot.Worktrees {
						if worktree.Path != task.WorktreePath {
							continue
						}
						ui.Textf(c, tr("Worktree changes: %d staged, %d modified, %d untracked"), worktree.Staged, worktree.Modified, worktree.Untracked).Selectable()
						ui.Textf(c, tr("Branch divergence: %d ahead, %d behind"), worktree.Ahead, worktree.Behind).Selectable()
						break
					}
					if task.LastTest != nil {
						ui.Textf(c, tr("Last test: %s"), tr(agentTestStatusLabel(task.LastTest.Status))).Selectable()
					}
					if len(task.Activity) > 0 {
						ui.Text(c, tr("Recent activity")).Bold()
						for index := max(0, len(task.Activity)-3); index < len(task.Activity); index++ {
							activity := task.Activity[index]
							row := ui.Row(c).Gap(8).AlignItems(ui.Center)
							if tooltip := agentActivityTooltip(activity); tooltip != "" {
								row.Tooltip(tooltip)
							}
							row.Children(func() {
								ui.Text(c, agentActivityTimestamp(activity)).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
								ui.Text(c, agentActivitySummary(activity)).FontSize(theme.FontSize - 1).Grow(1).SingleLine().Ellipsis("…")
							})
						}
					}
					if task.Request != "" {
						ui.Text(c, tr("Request")).Bold()
						ui.Text(c, task.Request).Selectable()
					}
					if task.Summary != "" {
						ui.Text(c, tr("Latest summary")).Bold()
						ui.Text(c, task.Summary).Selectable()
					}
					if task.ExitCode != nil {
						ui.Textf(c, tr("Last process exit code: %d"), *task.ExitCode).TextColor(theme.TextMuted)
					}
					if task.UpdatedAt.IsZero() == false {
						ui.Text(c, tr("Updated: ")+agentTaskUpdated(task)).TextColor(theme.TextMuted)
					}
					busy := a.agents.mutating[task.ID] || a.actionBusy
					ui.Row(c).Gap(8).Children(func() {
						launchLabel := "Launch agent"
						if task.Status != agentwork.StatusReady {
							launchLabel = "Resume agent"
						}
						canLaunch := task.Status != agentwork.StatusRunning && task.Status != agentwork.StatusArchived && task.Profile.LaunchCommand != ""
						if ui.PrimaryButton(c, tr(launchLabel)).Disabled(busy || !canLaunch).Clicked() {
							a.launchAgentTask(task.ID, task.Status != agentwork.StatusReady)
						}
						if ui.Button(c, tr("Open worktree")).Disabled(a.actionBusy).Clicked() {
							a.openSelectedAgentWorktree()
						}
						if ui.Button(c, tr("Review changes")).Disabled(a.actionBusy || task.Status == agentwork.StatusArchived).Clicked() {
							a.openAgentReview(task)
						}
						testRunning := task.LastTest != nil && task.LastTest.Status == agentwork.TestStatusRunning
						if ui.Button(c, tr("Run tests")).Disabled(busy || testRunning || task.Profile.TestCommand == "").Clicked() {
							a.runAgentTests(task.ID)
						}
					})
					ui.Row(c).Gap(8).Children(func() {
						canReview := task.Status == agentwork.StatusExited || task.Status == agentwork.StatusFailed
						if ui.Button(c, tr("Mark ready for review")).Disabled(busy || !canReview).Clicked() {
							a.setAgentTaskStatus(task, agentwork.StatusReadyForReview)
						}
						if ui.Button(c, tr("Mark done")).Disabled(busy || task.Status != agentwork.StatusReadyForReview).Clicked() {
							a.setAgentTaskStatus(task, agentwork.StatusDone)
						}
						if ui.Button(c, tr("Archive task")).Disabled(busy || task.Status == agentwork.StatusArchived || task.Status == agentwork.StatusRunning).Clicked() {
							a.confirmArchiveAgentTask(task)
						}
					})
					if task.Profile.LaunchCommand == "" {
						ui.Text(c, tr("This task has no launch command. Create future tasks with launch, setup, and test commands to run agents directly.")).TextColor(theme.Warning)
					}
				})
			})
	})
}

// Archiving only hides the durable task from the active workflow. It never
// removes the agent's worktree, branch, commits, or uncommitted output.
func (a *app) confirmArchiveAgentTask(task agentwork.Task) {
	if task.Status == agentwork.StatusArchived || task.Status == agentwork.StatusRunning {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Archive agent task",
		message:      trf("Archive %s?", task.Title),
		detail:       tr("The worktree, branch, commits, and uncommitted changes are preserved. Remove them separately only after reviewing the result."),
		confirmLabel: "Archive task",
		run: func() {
			a.setAgentTaskStatus(task, agentwork.StatusArchived)
		},
	})
}
