package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// openBisectStartForm starts a bisect with the selected commit as bad. The
// known-good revision is a separate input because choosing that boundary is
// the step that determines how much history Git must search.
func (a *app) openBisectStartForm(commit repository.Commit) {
	if a.actionBusy || a.session == nil {
		return
	}
	if a.snapshot.Bisect != nil && a.snapshot.Bisect.Active {
		a.confirmBisectMark("bad", commit.Hash)
		return
	}
	a.bisectBadCommit = commit
	a.bisectGoodRef = ""
	a.bisectFormError = ""
	a.showBisectForm = true
}

func (a *app) confirmBisectStart() {
	good := strings.TrimSpace(a.bisectGoodRef)
	bad := a.bisectBadCommit
	if bad.Hash == "" {
		return
	}
	if good == "" {
		a.bisectFormError = "Enter a known-good commit, branch or tag."
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Start Bisect",
		message:      trf("Start bisect with %s as bad and %s as good?", bad.ShortHash, good),
		detail:       tr("Git checks out commits between these revisions one by one. Mark each tested commit good, bad or skip, then reset to return to your original checkout."),
		confirmLabel: "Start bisect",
		run: func() {
			a.beginRepositoryActionMessage(tr("Started bisect"), "",
				func(ctx context.Context, session repository.Session) error {
					return session.BisectStart(ctx, bad.Hash, good)
				}, func() {
					a.showBisectForm = false
					a.bisectGoodRef = ""
				})
		},
	})
}

func (a *app) confirmBisectMark(term, ref string) {
	if a.actionBusy || a.session == nil {
		return
	}
	target := "the commit under test"
	if ref != "" {
		target = ref
		if shortHash := shortHashForBisect(ref); shortHash != "" {
			target = shortHash
		}
	}
	a.askConfirm(confirmRequest{
		title:        "Mark Bisect Commit",
		message:      trf("Mark %s as %s?", target, term),
		detail:       tr("Git advances to the next candidate, or reports the first bad commit when the search ends."),
		confirmLabel: "Mark commit",
		run: func() {
			var done string
			a.beginRepositoryActionMessage(trf("Marked commit %s", term), "",
				func(ctx context.Context, session repository.Session) error {
					var err error
					done, err = session.BisectMark(ctx, term, ref)
					return err
				}, func() {
					if done != "" {
						a.notify("success", "Bisect found the first bad commit", done)
					}
				})
		},
	})
}

func shortHashForBisect(ref string) string {
	// A full rev hash has a more readable short form in dialog labels.
	if len(ref) == 40 {
		return ref[:10]
	}
	return ""
}

func (a *app) confirmBisectReset() {
	if a.snapshot.Bisect == nil || !a.snapshot.Bisect.Active || a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Reset Bisect",
		message:      tr("Reset the current bisect and return to your original checkout?"),
		detail:       tr("The marks are discarded. This does not delete the commits or undo edits you made while testing."),
		confirmLabel: "Reset bisect",
		run: func() {
			a.beginRepositoryActionMessage(tr("Reset bisect"), "",
				func(ctx context.Context, session repository.Session) error {
					return session.BisectReset(ctx)
				}, nil)
		},
	})
}

func (a *app) bisectBanner(c *ui.Context) {
	state := a.snapshot.Bisect
	if state == nil || !state.Active {
		return
	}
	theme := c.Theme()
	ui.Column(c).Gap(5).Padding(10).Background(theme.Accent.Alpha(0.1)).
		Border(1, theme.Accent.Alpha(0.35)).Radius(theme.Radius).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Icon(c, searchIcon).FontSize(15).TextColor(theme.Accent)
			ui.Text(c, tr("Bisect in progress")).Bold()
			current := state.Current
			if len(current) > 10 {
				current = current[:10]
			}
			if current != "" {
				ui.Text(c, trf("Testing %s", current)).TextColor(theme.TextMuted)
			}
			ui.Box(c).Grow(1)
			if ui.Button(c, tr("Bad")).Disabled(a.actionBusy).Clicked() {
				a.confirmBisectMark("bad", "")
			}
			if ui.Button(c, tr("Good")).Disabled(a.actionBusy).Clicked() {
				a.confirmBisectMark("good", "")
			}
			if ui.Button(c, tr("Skip")).Disabled(a.actionBusy).Clicked() {
				a.confirmBisectMark("skip", "")
			}
			if ui.Button(c, tr("Reset")).Disabled(a.actionBusy).Clicked() {
				a.confirmBisectReset()
			}
		})
		text := trf("Marks: %d bad · %d good · %d skipped", len(state.Bad), len(state.Good), len(state.Skipped))
		ui.Text(c, text).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
	})
}

func (a *app) bisectFormModal(c *ui.Context) {
	if !a.showBisectForm {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(540).MaxWidthPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Start Bisect")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, searchIcon).FontSize(20).TextColor(theme.Accent)
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Start Bisect")).FontSize(17).Bold()
						ui.Text(c, trf("Known bad: %s %s", a.bisectBadCommit.ShortHash, a.bisectBadCommit.Subject)).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close bisect form", "Escape", closeIcon, false,
						func() { a.showBisectForm = false })
				})
				ui.TextInput(c, &a.bisectGoodRef).Label(tr("Known good revision")).
					Placeholder(tr("commit, branch or tag")).AutoFocus()
				if a.bisectFormError != "" {
					ui.Text(c, tr(a.bisectFormError)).TextColor(theme.Danger)
				}
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Cancel")).Clicked() {
						a.showBisectForm = false
					}
					if ui.PrimaryButton(c, tr("Start bisect")).Disabled(a.actionBusy || strings.TrimSpace(a.bisectGoodRef) == "").Clicked() {
						a.confirmBisectStart()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.showBisectForm = false
			}
		})
	})
}
