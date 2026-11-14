package app

import (
	"context"
	"errors"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// withPullRequestSetup performs an explicit, user-initiated auth preflight.
// It never runs while building views, and prevents duplicate checks. The
// original action continues on the UI thread once gh or glab is ready.
func (a *app) withPullRequestSetup(onReady func()) {
	if a.prSetupBusy || a.actionBusy || a.session == nil {
		return
	}
	a.prSetupBusy = true
	session := a.session
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		err := session.CheckPullRequestReady(context.Background())
		a.update(func() {
			if generation != a.generation {
				return
			}
			a.prSetupBusy = false
			if err != nil {
				a.notifyError(err)
				return
			}
			onReady()
		})
	}()
}

// openCreatePullRequestForm drafts a request for the checked-out branch.
// gh/glab resolve authentication and repository ownership using the user's
// existing CLI configuration; no hosting credentials enter TanGit settings.
func (a *app) openCreatePullRequestForm() {
	if a.snapshot.Branch == "" {
		return
	}
	a.withPullRequestSetup(func() {
		a.prTitle = a.snapshot.Branch
		a.prBody = ""
		a.prBase = a.defaultPullRequestBase()
		a.prDraft = false
		a.prError = ""
		a.showPRForm = true
	})
}

func (a *app) defaultPullRequestBase() string {
	for _, candidate := range []string{"main", "master"} {
		for _, branch := range a.snapshot.Branches {
			if branch.Name == candidate && !branch.Head {
				return candidate
			}
		}
	}
	for _, branch := range a.snapshot.Branches {
		if branch.Head && branch.UpstreamRemote != "" && branch.UpstreamBranch != "" {
			return branch.UpstreamBranch
		}
	}
	return ""
}

func (a *app) beginCreatePullRequest() {
	title := strings.TrimSpace(a.prTitle)
	body := strings.TrimSpace(a.prBody)
	base := strings.TrimSpace(a.prBase)
	if title == "" || a.actionBusy || a.session == nil {
		a.prError = tr("A pull request title is required.")
		return
	}
	branch := a.snapshot.Branch
	draft := a.prDraft
	a.showPRForm = false
	a.beginRepositoryActionMessage(tr("Created the pull request"), "",
		func(ctx context.Context, session repository.Session) error {
			output, err := session.CreatePullRequest(ctx, title, body, branch, base, draft)
			if output != "" {
				session.CommandLog().Record("Pull request URL", output, false)
			}
			return err
		}, nil)
}

func (a *app) confirmOpenPullRequest() {
	if a.snapshot.Branch == "" {
		return
	}
	a.withPullRequestSetup(a.confirmOpenPullRequestReady)
}

func (a *app) confirmOpenPullRequestReady() {
	if a.actionBusy || a.session == nil || a.snapshot.Branch == "" {
		return
	}
	branch := a.snapshot.Branch
	a.confirmAttention("Open Pull Request", a.snapshot.Branch,
		"This opens the branch's existing pull request in a browser using gh or glab, whichever supports your remote.", func() {
			var url string
			a.beginRepositoryActionMessage(tr("Opened the pull request"), "",
				func(ctx context.Context, session repository.Session) error {
					var err error
					url, err = session.OpenPullRequest(ctx, branch)
					return err
				}, func() {
					if a.openExternalURL != nil && url != "" {
						a.openExternalURL(url)
					}
				})
		})
}

// openPullRequestChecks opens a report for the branch immediately and lets
// its explicit refresh button reload it afterwards.
func (a *app) openPullRequestChecks() {
	if a.snapshot.Branch == "" {
		return
	}
	a.withPullRequestSetup(func() {
		a.prChecksOpen = true
		a.requestPullRequestChecks()
	})
}

func (a *app) requestPullRequestChecks() {
	if a.session == nil || !a.prChecksOpen {
		return
	}
	a.prChecksToken++
	token := a.prChecksToken
	generation := a.generation
	branch := a.snapshot.Branch
	session := a.session
	a.prChecksLoading = true
	a.prChecksText = ""
	a.prChecksError = ""
	go func() {
		defer a.recoverBackground()
		output, err := session.PullRequestChecks(context.Background(), branch)
		a.update(func() {
			if token != a.prChecksToken || generation != a.generation || !a.prChecksOpen {
				return
			}
			if errors.Is(err, repository.ErrPullRequestMissing) {
				a.prChecksOpen = false
				a.prChecksLoading = false
				a.prChecksError = ""
				a.notifyPullRequestMissing()
				return
			}
			a.prChecksLoading = false
			a.prChecksText = output
			if err != nil && output == "" {
				a.prChecksError = safeError(err)
			}
			// A failing pipeline also makes gh return non-zero. Its report is
			// more useful than a generic error, so preserve both.
			if err != nil {
				a.commandLog.Note(safeError(err))
			}
		})
	}()
}

// pullRequestMenuButton puts create, open and checks in one compact header
// action so the operations work from any workspace section.
func (a *app) pullRequestMenuButton(c *ui.Context) {
	theme := c.Theme()
	button := compactIconButton(ui.Button(c, "")).Center().Label(tr("Pull requests")).Tooltip(tr("Pull requests and checks (gh/glab)"))
	button.Children(func() {
		ui.Icon(c, externalLinkIcon).FontSize(15).TextColor(theme.Text)
	})
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(tr("Create pull request…")).Disabled(a.actionBusy || len(a.snapshot.Remotes) == 0 || a.snapshot.Branch == "").Chosen() {
			a.openCreatePullRequestForm()
		}
		if m.Item(tr("Open pull request in browser")).Disabled(a.actionBusy || len(a.snapshot.Remotes) == 0 || a.snapshot.Branch == "").Chosen() {
			a.confirmOpenPullRequest()
		}
		if m.Item(tr("View pull request checks")).Disabled(a.actionBusy || len(a.snapshot.Remotes) == 0 || a.snapshot.Branch == "").Chosen() {
			a.openPullRequestChecks()
		}
		m.Separator()
		if m.Item(tr("Uses gh for GitHub and glab for GitLab")).Chosen() {
			a.notify("info", tr("Hosting integration"), tr("Authenticate the matching CLI to enable pull request actions."))
		}
	})
}

func (a *app) pullRequestFormModal(c *ui.Context) {
	if !a.showPRForm {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(580).MaxWidthPercent(92).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Create pull request")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, externalLinkIcon).FontSize(20).TextColor(theme.Text)
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Create pull request")).Bold().FontSize(17)
						ui.Text(c, trf("%s as head branch", a.snapshot.Branch)).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close pull request form", "Escape", closeIcon, false,
						func() { a.showPRForm = false })
				})
				ui.TextInput(c, &a.prTitle).Label(tr("Pull request title")).
					Placeholder(tr("Describe the change")).AutoFocus()
				ui.TextArea(c, &a.prBody).Label(tr("Pull request description")).
					Placeholder(tr("Motivation, changes and testing")).Height(110)
				ui.TextInput(c, &a.prBase).Label(tr("Base branch")).
					Placeholder(tr("main"))
				ui.Checkbox(c, &a.prDraft, tr("Create as draft")).Label(tr("Create as draft"))
				if a.prError != "" {
					ui.Text(c, a.prError).TextColor(theme.Danger)
				}
				ui.Text(c, tr("Uses gh for GitHub or glab for GitLab with their existing authentication.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Cancel")).Clicked() {
						a.showPRForm = false
					}
					if ui.PrimaryButton(c, tr("Create pull request")).Disabled(a.actionBusy || strings.TrimSpace(a.prTitle) == "").Clicked() {
						a.beginCreatePullRequest()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.showPRForm = false
			}
		})
	})
}

func (a *app) pullRequestChecksModal(c *ui.Context) {
	if !a.prChecksOpen {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(780).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Pull request checks")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, searchIcon).FontSize(20).TextColor(theme.Text)
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Checks and pipeline")).Bold().FontSize(17)
						ui.Text(c, a.snapshot.Branch).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close pull request checks", "Escape", closeIcon, false,
						func() { a.prChecksOpen = false; a.prChecksToken++ })
				})
				if a.prChecksLoading {
					ui.Spinner(c).Label(tr("Loading checks"))
				}
				if a.prChecksText != "" {
					ui.Scroll(c).Grow(1).Children(func() {
						ui.Text(c, a.prChecksText).Selectable()
					})
				} else if !a.prChecksLoading && a.prChecksError == "" {
					ui.Text(c, tr("No check information reported.")).TextColor(theme.TextMuted)
				}
				if a.prChecksError != "" {
					ui.Text(c, a.prChecksError).TextColor(theme.Danger)
				}
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("View command log")).Clicked() {
						a.prChecksOpen = false
						a.openCommandLog()
					}
					if ui.Button(c, tr("Refresh checks")).Disabled(a.prChecksLoading).Clicked() {
						a.requestPullRequestChecks()
					}
					if ui.Button(c, tr("Close")).Clicked() {
						a.prChecksOpen = false
						a.prChecksToken++
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.prChecksOpen = false
				a.prChecksToken++
			}
		})
	})
}
