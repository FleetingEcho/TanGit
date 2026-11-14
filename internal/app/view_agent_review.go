package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/agentwork"
)

func (a *app) agentReviewView(c *ui.Context) {
	theme := c.Theme()
	review := &a.agents.review
	ui.Column(c).Grow(1).Gap(10).Padding(12).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			if ui.Button(c, tr("Back to tasks")).Clicked() {
				a.closeAgentReview()
				return
			}
			ui.Text(c, review.title).FontSize(18).Bold().Grow(1).SingleLine().Tooltip(review.title)
			if current, total := agentBatchReviewCounts(a.agents.batch, review.taskID); current > 0 {
				ui.Badge(c, trf("Review %d of %d", current, total))
				flatIconButton(c, "Next review", "Finish inspecting this task and open the next selected review; no readiness is inferred", continueIcon, 14, 4, false, a.advanceAgentBatchReview)
			}
			_, canAccept := a.agentAcceptEligibility()
			if ui.Button(c, tr("Accept commits")).Disabled(!canAccept).Clicked() {
				a.confirmAcceptAgentCommits()
			}
			if ui.Button(c, tr("Merge branch")).Disabled(!canAccept).Clicked() {
				a.confirmMergeAgentTask()
			}
			if ui.Button(c, tr("Refresh review")).Disabled(review.loading).Clicked() {
				a.reloadAgentReview()
			}
		})
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Textf(c, tr("Base: %s"), agentShortHash(review.base)).TextColor(theme.TextMuted).Selectable()
			if review.head != "" {
				ui.Textf(c, tr("Captured HEAD: %s"), agentShortHash(review.head)).TextColor(theme.TextMuted).Selectable()
			}
			ui.Text(c, review.worktree).TextColor(theme.TextMuted).SingleLine().Tooltip(review.worktree).Grow(1)
		})
		ui.Row(c).Gap(6).Children(func() {
			for _, choice := range []struct {
				mode  agentReviewMode
				label string
			}{
				{agentReviewFinal, "Final files"},
				{agentReviewCommits, "Commit range"},
				{agentReviewDirty, "Dirty only"},
			} {
				button := ui.Button(c, tr(choice.label))
				if review.mode == choice.mode {
					button = ui.PrimaryButton(c, tr(choice.label))
				}
				if button.Disabled(review.loading).Clicked() {
					a.setAgentReviewMode(choice.mode)
				}
			}
		})
		ui.Text(c, tr("Final files compares the fixed base directly with commits, staged and unstaged edits, and untracked files as one net result.")).TextColor(theme.TextMuted)
		if len(review.dirtyFiles) > 0 {
			ui.Text(c, tr("Accept is available after the agent commits its remaining staged, unstaged, and untracked output.")).TextColor(theme.Warning)
		}
		a.agentReviewExecution(c)
		a.agentCheckpointControls(c)
		switch {
		case review.loading:
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading agent review"))
				ui.Text(c, tr("Loading agent review…")).TextColor(theme.TextMuted)
			})
			return
		case review.err != "":
			ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Text(c, review.err).TextColor(theme.Danger).Selectable()
				if ui.Button(c, tr("Try again")).Clicked() {
					a.reloadAgentReview()
				}
			})
			return
		}
		a.collapsibleContentPaneSplit(c, &review.listWidth, &a.workspacePaneCollapsed[sectionAgents],
			"Expand review list", func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(7).Padding(10)).Children(func() {
					a.agentReviewList(c)
				})
			}, func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
					a.agentReviewDiff(c)
				})
			})
	})
}

func (a *app) agentReviewExecution(c *ui.Context) {
	theme := c.Theme()
	review := &a.agents.review
	task, ok := a.agentReviewTask()
	if !ok {
		return
	}
	busy := a.agents.mutating[task.ID]
	testRunning := task.LastTest != nil && task.LastTest.Status == agentwork.TestStatusRunning
	ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
		ui.TextArea(c, &review.feedback).Label(tr("Reviewer feedback")).Lines(2, 3).
			Placeholder(tr("Describe what the agent should change")).Grow(1)
		ui.Column(c).Gap(8).Children(func() {
			if ui.Button(c, tr("Run tests")).Disabled(busy || testRunning || task.Profile.TestCommand == "").Clicked() {
				a.runAgentTests(task.ID)
			}
			canRevise := task.Status == agentwork.StatusReadyForReview && strings.TrimSpace(review.feedback) != "" && task.Profile.ResumeCommand != ""
			if ui.Button(c, tr("Request changes")).Disabled(busy || !canRevise).Clicked() {
				a.requestAgentChanges(review.feedback)
			}
		})
	})
	if task.LastTest != nil {
		result := task.LastTest
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Textf(c, tr("Last test: %s"), tr(agentTestStatusLabel(result.Status))).Bold()
			if result.ExitCode != nil {
				ui.Textf(c, tr("Exit code: %d"), *result.ExitCode).TextColor(theme.TextMuted)
			}
			ui.Text(c, result.Command).TextColor(theme.TextMuted).SingleLine().Tooltip(result.Command).Grow(1)
		})
	}
}

func agentTestStatusLabel(status agentwork.TestStatus) string {
	switch status {
	case agentwork.TestStatusRunning:
		return "Running"
	case agentwork.TestStatusPassed:
		return "Passed"
	case agentwork.TestStatusFailed:
		return "Failed"
	default:
		return string(status)
	}
}

func (a *app) agentCheckpointControls(c *ui.Context) {
	theme := c.Theme()
	review := &a.agents.review
	task, ok := a.agentReviewTask()
	if !ok {
		return
	}
	busy := a.agents.checkpointBusy[task.ID]
	ids := make([]string, len(task.Checkpoints))
	for index, checkpoint := range task.Checkpoints {
		ids[index] = checkpoint.ID
	}
	if _, selected := a.selectedAgentCheckpoint(task); !selected && len(ids) > 0 {
		review.checkpointID = ids[len(ids)-1]
	}
	ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
		if len(ids) > 0 {
			ui.Select(c, &review.checkpointID, ids).Label(tr("Task checkpoint")).Grow(1)
		} else {
			ui.Text(c, tr("No task checkpoints yet.")).TextColor(theme.TextMuted).Grow(1)
		}
		if ui.Button(c, tr("Create checkpoint")).Disabled(busy).Clicked() {
			a.createAgentCheckpoint(task.ID, "Manual review checkpoint", func(err error) {
				if err != nil || !review.open || review.taskID != task.ID {
					return
				}
				if current, found := a.agentReviewTask(); found && len(current.Checkpoints) > 0 {
					review.checkpointID = current.Checkpoints[len(current.Checkpoints)-1].ID
				}
			})
		}
		_, canRestore := a.selectedAgentCheckpoint(task)
		if ui.Button(c, tr("Restore checkpoint")).Disabled(busy || !canRestore).Clicked() {
			a.confirmRestoreAgentCheckpoint(task, review.checkpointID)
		}
	})
	if checkpoint, found := a.selectedAgentCheckpoint(task); found {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, checkpoint.Reason).Bold().Grow(1).SingleLine().Tooltip(checkpoint.Reason)
			ui.Textf(c, tr("Captured HEAD: %s"), agentShortHash(checkpoint.HeadCommit)).TextColor(theme.TextMuted)
			ui.Text(c, checkpoint.CreatedAt.Local().Format("2006-01-02 15:04")).TextColor(theme.TextMuted)
		})
	}
	ui.Text(c, tr("Checkpoints preserve staged, unstaged, and untracked WIP. Restore requires the captured HEAD and saves the current WIP first.")).TextColor(theme.TextMuted)
}

func (a *app) confirmRestoreAgentCheckpoint(task agentwork.Task, checkpointID string) {
	checkpoint, ok := a.selectedAgentCheckpoint(task)
	if !ok || checkpoint.ID != checkpointID || a.agents.checkpointBusy[task.ID] {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Restore agent checkpoint",
		message:      trf("Restore %s?", checkpoint.Reason),
		detail:       tr("TanGit saves the current WIP first, then restores the checkpoint's staged, unstaged, and untracked files. Newer commits prevent restoration."),
		confirmLabel: "Restore checkpoint",
		run: func() {
			a.restoreAgentCheckpoint(task.ID, checkpoint.ID)
		},
	})
}

func (a *app) agentReviewList(c *ui.Context) {
	theme := c.Theme()
	review := &a.agents.review
	title := tr("Final files")
	count := len(review.finalFiles)
	if review.mode == agentReviewCommits {
		title = tr("Agent commits")
		count = len(review.commits)
	} else if review.mode == agentReviewDirty {
		title = tr("Uncommitted files")
		count = len(review.dirtyFiles)
	}
	ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
		ui.Text(c, title).Bold().Grow(1)
		ui.Badge(c, strconv.Itoa(count))
		paneCollapseButton(c, &a.workspacePaneCollapsed[sectionAgents], "Collapse review list")
	})
	if count == 0 {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("Nothing to show in this view")).TextColor(theme.TextMuted)
		})
		return
	}
	review.list.Selected = &review.selectedRow
	review.list.Key = func(index int) any {
		if review.mode == agentReviewCommits {
			return review.commits[index].Hash
		}
		if review.mode == agentReviewDirty {
			file := review.dirtyFiles[index]
			return fmt.Sprintf("%s:%t:%t", file.Path, file.Staged, file.Unstaged)
		}
		file := review.finalFiles[index]
		return file.Path + "\x00" + file.PreviousPath
	}
	list := a.keyboardList(c, &review.list, count).Grow(1).Rows(func(row ui.ListRow) {
		if review.mode == agentReviewCommits {
			commit := review.commits[row.Index]
			ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(7, 8).Children(func() {
				ui.Text(row.Context, commit.Subject).Grow(1).SingleLine().Tooltip(commit.Subject)
				ui.Text(row.Context, commit.ShortHash).TextColor(theme.TextMuted)
			}).OnClick(func() { a.selectAgentReviewRow(row.Index) })
			return
		}
		var path, status string
		var added, deleted int
		if review.mode == agentReviewDirty {
			file := review.dirtyFiles[row.Index]
			path, status = file.Path, file.Status
			added, deleted = file.LinesAdded, file.LinesDeleted
		} else {
			file := review.finalFiles[row.Index]
			path, status = file.Path, file.Status
			added, deleted = file.LinesAdded, file.LinesDeleted
		}
		ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(7, 8).Children(func() {
			fileTypeMark(row.Context, path, 13, row.Selected())
			ui.Text(row.Context, path).Grow(1).SingleLine().Tooltip(path)
			fileDiffStatCounts(row.Context, path, added, deleted)
			statusSlot(row.Context, statusLetter(status), rowStatusColor(theme, row, theme.Warning))
		}).OnClick(func() { a.selectAgentReviewRow(row.Index) })
	})
	if a.keyboardListChanged(&review.list, list) {
		a.selectAgentReviewRow(review.selectedRow)
	}
}

func (a *app) agentReviewDiff(c *ui.Context) {
	theme := c.Theme()
	review := &a.agents.review
	if review.mode == agentReviewCommits {
		commit, ok := a.selectedAgentReviewCommit()
		if !ok {
			ui.Column(c).Grow(1).Center().Children(func() {
				ui.Text(c, tr("Select a commit to inspect its files")).TextColor(theme.TextMuted)
			})
			return
		}
		ui.Text(c, commit.Subject).Bold().Selectable()
		ui.Text(c, commit.Hash).TextColor(theme.TextMuted).Selectable()
		if review.commitFiles == nil {
			a.requestAgentReviewCommitFiles()
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Label(tr("Loading commit files"))
				ui.Text(c, tr("Loading commit files…")).TextColor(theme.TextMuted)
			})
			return
		}
		if len(review.commitFiles) == 0 {
			ui.Text(c, tr("This commit has no changed files")).TextColor(theme.TextMuted)
			return
		}
		paths := make([]string, len(review.commitFiles))
		for index, file := range review.commitFiles {
			paths[index] = file.Path
		}
		before := review.selectedCommitFile
		ui.Select(c, &review.selectedCommitFile, paths).Label(tr("Commit file"))
		if before != review.selectedCommitFile {
			a.clearAgentReviewDiff()
		}
	}
	path := a.agentReviewFileLabel()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		if path == "" {
			ui.Text(c, tr("Select a file to see its diff.")).TextColor(theme.TextMuted).Grow(1)
		} else {
			ui.Text(c, path).Bold().Grow(1).SingleLine().Tooltip(path)
		}
		if ui.Button(c, tr("Send file to worktree")).Disabled(review.diffPatch == "" || review.diffLoading || a.actionBusy).Clicked() {
			a.sendAgentReviewFile()
		}
		a.diffViewOptions(c)
	})
	if path == "" {
		return
	}
	a.requestAgentReviewDiff()
	switch {
	case review.diffLoading:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Label(tr("Loading diff"))
			ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted)
		})
	case review.diffError != "":
		ui.Text(c, review.diffError).TextColor(theme.Danger).Selectable()
	case review.diffState != nil:
		a.keyboardDiffViewer(c, review.diffState)
	default:
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("No textual diff")).TextColor(theme.TextMuted)
		})
	}
}
