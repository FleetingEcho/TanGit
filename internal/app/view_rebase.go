package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

const (
	historyWorkspaceAuto = iota
	historyWorkspaceCommits
	historyWorkspaceRebase
)

// resetRebaseWorkspace drops repository-bound plans, marks and pending reads
// before another repository (or the picker) becomes visible. Tokens invalidate
// any editor read which was already in flight for the old session.
func (a *app) resetRebaseWorkspace() {
	a.historyWorkspace = historyWorkspaceAuto
	a.markedBaseCommit = repository.Commit{}
	a.selectedRebaseStep = -1
	a.rebaseTargetRef = ""
	a.rebaseDraft = nil
	a.rebasePlanToken++
	a.rebasePlan = nil
	a.rebasePlanText = ""
	a.rebasePlanEditorOpen = false
	a.rebasePlanLoading = false
	a.rebasePlanError = ""
}

func (a *app) activeRebaseState() *repository.OperationState {
	state := a.snapshot.OperationState
	if state == nil || (state.Type != repository.OperationTypeRebase && state.Type != repository.OperationTypePullRebase) {
		return nil
	}
	return state
}

func (a *app) rebaseWorkspaceVisible() bool {
	if a.workspaceTab != sectionHistory {
		return false
	}
	if a.historyWorkspace == historyWorkspaceRebase {
		return true
	}
	return a.historyWorkspace == historyWorkspaceAuto && a.activeRebaseState() != nil
}

func (a *app) openRebaseWorkspace() {
	a.workspaceTab = sectionHistory
	a.historyWorkspace = historyWorkspaceRebase
	a.syncNavSelection()
}

func (a *app) openCommitWorkspace() {
	a.workspaceTab = sectionHistory
	a.historyWorkspace = historyWorkspaceCommits
	a.syncNavSelection()
}

func (a *app) visibleRebaseSteps() []repository.OperationStep {
	if state := a.activeRebaseState(); state != nil {
		return state.Steps
	}
	if a.rebaseDraft != nil {
		return a.rebaseDraft.Steps
	}
	return nil
}

func rebaseStepLabel(step repository.OperationStep) string {
	description := step.Subject
	if step.Action == "update-ref" {
		description = step.UpdateRef
	} else if description == "" {
		description = strings.TrimSpace(step.Raw)
	}
	return fmt.Sprintf("%s · %s · %s", step.Status, step.Action, description)
}

func (a *app) markRebaseBase(commit repository.Commit) {
	if a.markedBaseCommit.Hash == commit.Hash {
		a.markedBaseCommit = repository.Commit{}
		return
	}
	// History loaders carry Git's full object ID. Keeping that value instead
	// of ShortHash makes the later --onto boundary immutable and unambiguous.
	a.markedBaseCommit = commit
	a.markedBaseCommit.ShortHash = ""
}

func (a *app) rebasePlans(session repository.Session) (repository.RebasePlanSession, error) {
	plans, ok := session.(repository.RebasePlanSession)
	if !ok {
		return nil, fmt.Errorf("%w: this session cannot read and compare Git's live rebase todo", repository.ErrOperationUnsupported)
	}
	return plans, nil
}

// rebaseOntoBranch centralizes marked-base behavior for local and remote
// branch menus. The legacy no-mark path remains exactly RebaseOntoBranch.
func (a *app) rebaseOntoBranch(ctx context.Context, session repository.Session, name string) error {
	if a.markedBaseCommit.Hash == "" {
		return requireBranchRemoteSession(session).RebaseOntoBranch(ctx, name)
	}
	plans, err := a.rebasePlans(session)
	if err != nil {
		return err
	}
	return plans.RebaseOntoBranchFromBase(ctx, name, a.markedBaseCommit.Hash)
}

func (a *app) clearMarkedRebaseBase() {
	a.markedBaseCommit = repository.Commit{}
}

func rebaseStepStatusColor(theme *ui.Theme, status repository.OperationStepStatus) ui.Color {
	switch status {
	case repository.OperationStepCurrent:
		return theme.Warning
	case repository.OperationStepDone:
		return theme.Success
	default:
		return theme.TextMuted
	}
}

func rebaseStepDescription(step repository.OperationStep) string {
	if step.Action == "update-ref" {
		return step.UpdateRef
	}
	if step.Subject != "" {
		return step.Subject
	}
	return strings.TrimSpace(step.Raw)
}

// rebaseStepRow renders the operation model, never a Commit row. In
// particular, update-ref has no fabricated commit hash and every full SHA is
// available as selectable text rather than only a short display hash.
func (a *app) rebaseStepRow(c *ui.Context, row ui.ListRow, step repository.OperationStep) {
	theme := c.Theme()
	muted := secondaryRowColor(theme, row)
	status := string(step.Status)
	if status == "" {
		status = "draft"
	}
	width, _ := c.Size()
	narrow := width < 900 || (len(step.Hash) > 40 && width < 1200)
	badge := func() {
		ui.Badge(c, strings.ToUpper(step.Action)).Tooltip(step.Raw)
		statusColor := rowStatusColor(theme, row, rebaseStepStatusColor(theme, step.Status))
		ui.Badge(c, tr(status)).TextColor(statusColor)
	}
	hash := func() {
		if step.Hash != "" {
			text := ui.Text(c, step.Hash).TextColor(muted).FontSize(theme.FontSize - 1).
				MinWidth(0).Tooltip(step.Hash).Selectable()
			if narrow {
				text.Grow(1)
			} else {
				text.SingleLine().Shrink(1)
			}
		}
	}
	description := rebaseStepDescription(step)
	if narrow {
		ui.Column(c).Gap(3).Padding(7, 8).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Children(func() {
				badge()
				if step.Action == "update-ref" {
					ui.Text(c, tr("updates")).TextColor(muted).FontSize(theme.FontSize - 1)
				}
			})
			hash()
			ui.Text(c, description).TextColor(rowStatusColor(theme, row, theme.Text)).MinWidth(0).Tooltip(step.Raw)
		})
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(7, 8).Children(func() {
		badge()
		if step.Hash != "" {
			// Leave enough room for every hexadecimal character at the current
			// font size. Long SHA-256 IDs switch to the wrapping row above.
			hashWidth := float32(len(step.Hash))*theme.FontSize*0.65 + 8
			ui.Box(c).Width(hashWidth).Children(hash)
		}
		ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, description).SingleLine().MinWidth(0).Tooltip(step.Raw)
			if step.Action == "update-ref" {
				ui.Text(c, tr("Git updates this ref when the rebase finishes")).TextColor(muted).
					FontSize(theme.FontSize - 2).SingleLine()
			}
		})
	})
}

func (a *app) rebaseOperationButtons(c *ui.Context) {
	if a.activeRebaseState() == nil {
		return
	}
	conflicts := conflictedFileCount(a.snapshot.Files)
	state := a.activeRebaseState()
	planReadable := state == nil || state.StepsError == ""
	if conflicts > 0 {
		iconTextButton(c, "Resolve conflicts", "Open the existing conflict resolver in Changes", conflictIcon, false, func() {
			a.setWorkspaceTab(sectionChanges)
		})
	}
	if a.operationCan(repository.OperationActionContinue) {
		tooltip := "Continue through preflight and the Operation driver"
		if a.location.IsRemote() {
			tooltip = "Continue checks use fully trusted local commands and are disabled for SSH workspaces"
		}
		iconTextButtonState(c, "Continue", tooltip, continueIcon, false,
			a.actionBusy || conflicts > 0 || !planReadable || a.location.IsRemote(), a.confirmContinueOperation)
	}
	if a.operationCan(repository.OperationActionSkip) {
		iconTextButtonState(c, "Skip…", "Skip the current Git todo step", skipIcon, false, a.actionBusy || !planReadable, a.confirmSkipOperation)
	}
	if a.operationCan(repository.OperationActionAbort) {
		iconTextButton(c, "Abort…", "Abort through the Operation driver", abortIcon, a.actionBusy, a.confirmAbortOperation)
	}
}

func (a *app) rebaseHistoryView(c *ui.Context) {
	theme := c.Theme()
	state := a.activeRebaseState()
	islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(12)).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			paneTitle(c, rebaseIcon, "Rebase TODO")
			if state != nil {
				ui.Badge(c, operationProgress(*state))
			}
			ui.Spacer(c).Grow(1)
			if ui.Button(c, tr("Commits")).Label(tr("Return to commit history")).Clicked() {
				a.openCommitWorkspace()
			}
			if state != nil {
				iconTextButton(c, "Edit TODO", "Compare-and-swap edit Git's complete remaining todo", editIcon,
					a.actionBusy, a.openActiveRebasePlan)
			} else {
				iconTextButton(c, "New plan", "Generate a real Git rebase plan", stageIcon, a.actionBusy, func() {
					a.rebaseDraft = nil
					a.rebasePlanError = ""
				})
			}
		})
		if state != nil {
			if state.StepsError != "" {
				ui.Text(c, state.StepsError).TextColor(theme.Danger).Tooltip(state.StepsError)
			}
			ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, tr("Current / pending / done steps come from Git's done and git-rebase-todo files.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1).MinWidth(160)
				a.rebaseOperationButtons(c)
			})
			if state.OntoSHA != "" {
				onto := ui.Text(c, tr("onto ")+state.OntoSHA).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).
					MinWidth(0).Tooltip(state.OntoSHA).Selectable()
				if width, _ := c.Size(); width >= 900 {
					onto.SingleLine()
				}
			}
		} else {
			a.rebaseDraftControls(c)
		}
		if a.rebasePlanError != "" {
			ui.Text(c, a.rebasePlanError).TextColor(theme.Danger).Tooltip(a.rebasePlanError)
		}
		steps := a.visibleRebaseSteps()
		if len(steps) == 0 {
			ui.Column(c).Grow(1).Center().Gap(8).Padding(20).Children(func() {
				ui.Icon(c, rebaseIcon).FontSize(28).TextColor(theme.TextMuted)
				ui.Text(c, tr("No rebase todo to display")).Bold()
				ui.Text(c, tr("Choose a target ref to generate Git's actual plan, or start a rebase from a branch.")).
					TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(520)
			})
			return
		}
		list := a.keyboardList(c, &a.rebaseStepList, len(steps)).Grow(1).Rows(func(row ui.ListRow) {
			a.rebaseStepRow(row.Context, row, steps[row.Index])
		})
		a.keyboardListChanged(&a.rebaseStepList, list)
		if state != nil && a.selectedRebaseStep >= 0 && a.selectedRebaseStep < len(steps) {
			a.rebaseStepActions(c, steps[a.selectedRebaseStep])
		}
	})
}

func (a *app) rebaseDraftControls(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Gap(7).Padding(9, 10).Background(theme.Surface).Radius(theme.Radius).Children(func() {
		if a.rebaseDraft == nil {
			ui.Row(c).FillWidth().AlignItems(ui.End).Gap(8).Wrap().Children(func() {
				ui.TextInput(c, &a.rebaseTargetRef).Label(tr("Rebase target ref")).Placeholder(tr("target-branch")).
					Grow(1).MinWidth(220).Disabled(a.actionBusy)
				if ui.PrimaryButton(c, tr("Prepare plan")).Disabled(a.actionBusy || strings.TrimSpace(a.rebaseTargetRef) == "").Clicked() {
					a.prepareRebasePlan()
				}
			})
		} else {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
				ui.Badge(c, tr("Draft generated by Git"))
				ui.Text(c, a.rebaseDraft.TargetRef).Bold()
				ui.Spacer(c).Grow(1)
				if ui.PrimaryButton(c, tr("Edit & start")).Disabled(a.actionBusy).Clicked() {
					a.openDraftRebasePlan()
				}
			})
		}
		markedBase := a.markedBaseCommit.Hash
		if a.rebaseDraft != nil {
			// A prepared draft owns its resolved boundary. Clearing a later UI
			// mark cannot turn this draft into an ordinary rebase.
			markedBase = a.rebaseDraft.MarkedBaseSHA
		}
		if markedBase != "" {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Wrap().Children(func() {
				ui.Badge(c, tr("Marked base"))
				ui.Text(c, markedBase).FontSize(theme.FontSize - 1).
					MinWidth(0).Tooltip(markedBase).Selectable().Grow(1)
				if ui.Button(c, tr("Clear mark")).Disabled(a.rebaseDraft != nil).Clicked() {
					a.clearMarkedRebaseBase()
				}
			})
			ui.Text(c, tr("The draft will use git rebase --onto <target> <full-base-SHA>.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		} else {
			ui.Text(c, tr("No marked base: an ordinary rebase will not pass --onto.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
	})
}

func (a *app) prepareRebasePlan() {
	if a.actionBusy || a.session == nil {
		return
	}
	target := strings.TrimSpace(a.rebaseTargetRef)
	marked := a.markedBaseCommit.Hash
	a.rebasePlanError = ""
	var prepared repository.RebasePlan
	a.beginRepositoryActionMessage("", "", func(ctx context.Context, session repository.Session) error {
		plans, err := a.rebasePlans(session)
		if err != nil {
			return err
		}
		prepared, err = plans.PrepareRebasePlan(ctx, repository.RebasePlanOptions{TargetRef: target, MarkedBase: marked})
		return err
	}, func() {
		a.rebaseDraft = &prepared
		a.openDraftRebasePlan()
	})
}

func (a *app) openActiveRebasePlan() {
	if a.session == nil || a.actionBusy || a.activeRebaseState() == nil {
		return
	}
	a.rebasePlanToken++
	token := a.rebasePlanToken
	session := a.session
	ctx := a.repositoryWork.Context()
	a.rebasePlanEditorOpen = true
	a.rebasePlanLoading = true
	a.rebasePlanError = ""
	a.rebasePlan = nil
	a.rebasePlanText = ""
	go func() {
		defer a.recoverBackground()
		plans, err := a.rebasePlans(session)
		var plan repository.RebasePlan
		if err == nil {
			plan, err = plans.ReadRebasePlan(ctx)
		}
		a.update(func() {
			if token != a.rebasePlanToken || !a.rebasePlanEditorOpen {
				return
			}
			a.rebasePlanLoading = false
			if err != nil {
				a.rebasePlanError = safeError(err)
				return
			}
			a.rebasePlan = &plan
			a.rebasePlanText = plan.Text
		})
	}()
}

func (a *app) openDraftRebasePlan() {
	if a.rebaseDraft == nil {
		return
	}
	plan := *a.rebaseDraft
	a.rebasePlan = &plan
	a.rebasePlanText = plan.Text
	a.rebasePlanError = ""
	a.rebasePlanLoading = false
	a.rebasePlanEditorOpen = true
	a.rebasePlanToken++
}

func (a *app) closeRebasePlanEditor() {
	a.rebasePlanEditorOpen = false
	a.rebasePlanLoading = false
	a.rebasePlan = nil
	a.rebasePlanText = ""
	a.rebasePlanToken++
}

func (a *app) saveRebasePlan() {
	plan := a.rebasePlan
	if plan == nil || a.actionBusy {
		return
	}
	edited := a.rebasePlanText
	active := plan.Active
	message := tr("Updated the active rebase todo")
	if !active {
		message = tr("Started the rebase plan")
	}
	a.beginRepositoryActionMessage(message, "", func(ctx context.Context, session repository.Session) error {
		plans, err := a.rebasePlans(session)
		if err != nil {
			return err
		}
		if active {
			return plans.EditRebasePlan(ctx, *plan, edited)
		}
		return plans.StartRebasePlan(ctx, *plan, edited)
	}, func() {
		if active {
			a.closeRebasePlanEditor()
		} else {
			a.rebaseDraft = nil
			a.clearMarkedRebaseBase()
			a.closeRebasePlanEditor()
			a.historyWorkspace = historyWorkspaceAuto
		}
	})
}

// replaceRebaseRawLine replaces exactly one complete executable line. Counting
// exact lines, rather than substrings, prevents a duplicate or a comment from
// being changed accidentally.
func replaceRebaseRawLine(text, oldLine, newLine string) (string, error) {
	lines := strings.Split(text, "\n")
	found := 0
	for index, line := range lines {
		if line == oldLine {
			lines[index] = newLine
			found++
		}
	}
	if found != 1 {
		return "", fmt.Errorf("rebase step line occurs %d times; refresh and select it again", found)
	}
	return strings.Join(lines, "\n"), nil
}

func rebaseActionLine(raw, action string) string {
	_, rest := splitRebaseInstructionForView(raw)
	return action + " " + rest
}

func splitRebaseInstructionForView(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if index := strings.IndexAny(raw, " \t"); index >= 0 {
		return raw[:index], strings.TrimSpace(raw[index+1:])
	}
	return raw, ""
}

func (a *app) changeActiveRebaseStep(step repository.OperationStep, action string) {
	if a.actionBusy || a.session == nil || step.Status != repository.OperationStepPending || step.Hash == "" {
		return
	}
	run := func() {
		a.beginRepositoryActionMessage(trf("Changed %s to %s", step.Hash, action), "", func(ctx context.Context, session repository.Session) error {
			plans, err := a.rebasePlans(session)
			if err != nil {
				return err
			}
			plan, err := plans.ReadRebasePlan(ctx)
			if err != nil {
				return err
			}
			matched := false
			edited := plan.Text
			for _, candidate := range plan.Steps {
				if candidate.Hash != step.Hash || candidate.Raw != step.Raw {
					continue
				}
				if matched {
					return fmt.Errorf("rebase step %s is duplicated; edit the whole todo instead", step.Hash)
				}
				matched = true
				edited, err = replaceRebaseRawLine(plan.Text, candidate.Raw, rebaseActionLine(candidate.Raw, action))
				if err != nil {
					return err
				}
			}
			if !matched {
				return fmt.Errorf("%w: selected step is no longer pending", repository.ErrRebasePlanChanged)
			}
			return plans.EditRebasePlan(ctx, plan, edited)
		}, nil)
	}
	if action != "drop" {
		run()
		return
	}
	a.askConfirm(confirmRequest{
		title: "Drop Rebase Step", message: trf("Change %s to drop?", step.Hash),
		detail:       tr("The line remains in Git's todo as an explicit drop; it is not deleted."),
		confirmLabel: "Drop step", destructive: true, run: run,
	})
}

func (a *app) rebaseStepActions(c *ui.Context, step repository.OperationStep) {
	if step.Status != repository.OperationStepPending || step.Hash == "" {
		return
	}
	ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Wrap().Children(func() {
		ui.Text(c, tr("Selected pending commit:")).TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1)
		for _, action := range []string{"pick", "reword", "edit", "squash", "fixup", "drop"} {
			action := action
			button := ui.Button(c, tr(action)).Disabled(a.actionBusy || step.Action == action)
			if button.Clicked() {
				a.changeActiveRebaseStep(step, action)
			}
		}
	})
}

func (a *app) rebasePlanModal(c *ui.Context) {
	if !a.rebasePlanEditorOpen || a.confirm != nil {
		return
	}
	theme := c.Theme()
	_, windowHeight := c.Size()
	editorHeight := min(max(windowHeight-300, 100), 440)
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(860).MaxWidthPercent(94).MaxHeightPercent(94).Padding(20).Gap(12).
				Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius + 4).Clip().
				Role(ui.RoleDialog).Label(tr("Rebase Plan")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Icon(c, rebaseIcon).FontSize(19).TextColor(theme.Accent)
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Rebase TODO")).FontSize(18).Bold()
						ui.Text(c, tr("The complete plan is compared before anything is replaced.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close rebase plan", "Escape", closeIcon, a.actionBusy, a.closeRebasePlanEditor)
				})
				// The body can shrink and scroll while the header and action row
				// stay committed inside the panel on compact-height windows.
				ui.Scroll(c).Grow(1).Children(func() {
					ui.Column(c).Grow(1).Gap(10).Children(func() {
						if a.rebasePlanLoading {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Spinner(c).Label(tr("Loading Git's rebase todo"))
								ui.Text(c, tr("Loading Git's rebase todo…")).TextColor(theme.TextMuted)
							})
						} else if a.rebasePlan != nil {
							if a.rebasePlan.Active {
								ui.Badge(c, tr("Active todo · whole-file CAS"))
							} else {
								ui.Badge(c, tr("Draft · captured from Git"))
							}
							ui.TextArea(c, &a.rebasePlanText).Label(tr("Complete Git rebase todo")).
								Height(editorHeight).Disabled(a.actionBusy)
							ui.Text(c, tr("Change pick/reword/edit/squash/fixup/drop or reorder lines. Do not delete or duplicate lines; exec, update-ref, reset, label and merge directives are preserved.")).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						}
						if a.rebasePlanError != "" {
							ui.Text(c, a.rebasePlanError).TextColor(theme.Danger)
						}
					})
				})
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
						a.closeRebasePlanEditor()
					}
					label := tr("Start rebase")
					if a.rebasePlan != nil && a.rebasePlan.Active {
						label = tr("Save TODO")
					}
					if ui.PrimaryButton(c, label).Disabled(a.actionBusy || a.rebasePlanLoading || a.rebasePlan == nil).Clicked() {
						a.saveRebasePlan()
					}
				})
			})
			if !a.actionBusy && (panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape)) {
				a.closeRebasePlanEditor()
			}
		})
	})
}
