package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// openCommitRewordForm loads the full message in the background. History rows
// carry only subjects, and using one as the whole message would lose the body.
func (a *app) openCommitRewordForm(commit repository.Commit) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.showCommitTagForm = false
	a.showCommitBranchForm = false
	a.showCommitRewordForm = true
	a.commitRewordCommit = commit
	a.commitRewordSummary = commit.Subject
	a.commitRewordBody = ""
	a.commitRewordError = ""
	a.commitRewordLoading = true
	a.commitRewordToken++
	token := a.commitRewordToken
	session := a.session
	go func() {
		message, err := session.CommitMessage(context.Background(), commit)
		a.update(func() {
			if token != a.commitRewordToken || !a.showCommitRewordForm {
				return
			}
			a.commitRewordLoading = false
			if err != nil {
				a.commitRewordError = safeError(err)
				return
			}
			a.commitRewordSummary = message.Summary
			a.commitRewordBody = message.Description
		})
	}()
}

func (a *app) closeCommitRewordForm() {
	a.showCommitRewordForm = false
	a.commitRewordLoading = false
	a.commitRewordError = ""
	a.commitRewordToken++
}

func (a *app) beginCommitReword() {
	if a.actionBusy || a.session == nil || a.commitRewordLoading || a.commitRewordError != "" {
		return
	}
	summary := strings.TrimSpace(a.commitRewordSummary)
	if summary == "" {
		return
	}
	commit := a.commitRewordCommit
	description := strings.TrimSpace(a.commitRewordBody)
	detail := commit.Subject + "\n\n" + tr("Only this commit's message changes; staged and unstaged file changes are not committed. If it is older than HEAD, later commits will be rewritten with new hashes.")
	a.askConfirm(confirmRequest{
		title:        "Reword Commit",
		message:      trf("Change the message of %s?", commit.ShortHash),
		detail:       detail,
		confirmLabel: "Reword commit",
		run: func() {
			a.beginRepositoryActionMessage(trf("Reworded %s", commit.ShortHash), "",
				func(ctx context.Context, session repository.Session) error {
					return session.RewordCommit(ctx, commit, summary, description)
				}, a.closeCommitRewordForm)
		},
	})
}

func (a *app) rewordSelectedCommit() {
	if commit, ok := a.selectedVisibleCommit(); ok {
		a.openCommitRewordForm(commit)
	}
}

func (a *app) commitRewordModal(c *ui.Context) {
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(520).MaxWidthPercent(92).
				Padding(20).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Children(func() {
				commit := a.commitRewordCommit
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Icon(c, editIcon).FontSize(18).TextColor(theme.Accent)
					ui.Column(c).Gap(1).Grow(1).Children(func() {
						ui.Text(c, tr("Reword Commit")).FontSize(17).Bold()
						ui.Textf(c, tr("%s · written by %s"), commit.ShortHash, commit.AuthorName).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine()
					})
					iconButton(c, "Close the reword form", "Escape", closeIcon, a.actionBusy, a.closeCommitRewordForm)
				})
				ui.TextInput(c, &a.commitRewordSummary).Label(tr("Summary")).Placeholder(tr("Describe the change")).
					Disabled(a.commitRewordLoading || a.actionBusy)
				ui.TextArea(c, &a.commitRewordBody).Label(tr("Description")).Placeholder(tr("Optional details")).Height(150).
					Disabled(a.commitRewordLoading || a.actionBusy)
				if a.commitRewordLoading {
					ui.Text(c, tr("Loading the full commit message…")).TextColor(theme.TextMuted)
				}
				if a.commitRewordError != "" {
					ui.Text(c, a.commitRewordError).TextColor(theme.Danger)
				}
				ui.Text(c, tr("File changes stay untouched. Rewording an older commit rewrites later commit hashes.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
						a.closeCommitRewordForm()
					}
					invalid := strings.TrimSpace(a.commitRewordSummary) == "" || a.commitRewordLoading || a.commitRewordError != ""
					if ui.PrimaryButton(c, tr("Reword commit")).Disabled(a.actionBusy || invalid).Clicked() {
						a.beginCommitReword()
					}
				})
			})
			if !a.actionBusy && (panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape)) {
				a.closeCommitRewordForm()
			}
		})
	})
}
