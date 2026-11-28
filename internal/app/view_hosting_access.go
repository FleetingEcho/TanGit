package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

const hostingAccessListHeight float32 = 280

// hostingSettingRow keeps the form compact: explanations live in an info
// badge instead of permanently taking several lines beside each control.
func hostingSettingRow(c *ui.Context, label, explanation string, control func()) {
	ui.Row(c).AlignItems(ui.Center).Gap(16).MinHeight(42).Children(func() {
		ui.Box(c).Width(154).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, tr(label)).Bold().Shrink(1).MinWidth(0)
				ui.Box(c).Shrink(0).Children(func() { cacheInfoBadge(c, label, explanation) })
			})
		})
		ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(8).Wrap().Children(control)
	})
}

// hostingAccessModal first presents the saved configurations, then opens a
// separate add/edit layer. Tokens remain in the keychain rather than settings.
func (a *app) hostingAccessModal(c *ui.Context) {
	if !a.hostingAccess.open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(620).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 8).
				Role(ui.RoleDialog).Label(tr("Git hosting settings"))
			panel.Children(func() {
				state := &a.hostingAccess
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					if state.editing {
						iconButton(c, "Back to configurations", "Back to configurations", backIcon, false, a.closeHostingAccessEditor)
					} else {
						ui.Icon(c, settingsIcon).FontSize(19).TextColor(theme.Accent)
					}
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						title := "Git hosting settings"
						if state.editing && state.profileID == "" {
							title = "Add Git host"
						} else if state.editing {
							title = "Edit Git host"
						}
						ui.Text(c, tr(title)).FontSize(18).Bold()
						ui.Text(c, tr("The token is kept in your system keychain, not in settings.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					closeAction := a.closeHostingAccess
					closeTooltip := "Close"
					if state.editing {
						closeAction = a.closeHostingAccessEditor
						closeTooltip = "Close editor and return to configurations"
					}
					iconButton(c, closeTooltip, "Escape", closeIcon, false, closeAction)
				})
				if !state.editing {
					a.hostingAccessList(c)
				} else {
					a.hostingAccessEditor(c)
				}
			})
			if a.confirm == nil && panel.PressedOutside() {
				a.closeHostingAccess()
			} else if a.confirm == nil && back.OverlayShortcut(0, ui.KeyEscape) {
				if a.hostingAccess.editing {
					a.closeHostingAccessEditor()
				} else {
					a.closeHostingAccess()
				}
			}
		})
	})
}

func (a *app) hostingAccessList(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("Saved configurations")).Bold().Grow(1)
		iconTextButton(c, "Add configuration", "Add a Git host", plusIcon, true, a.beginHostingProfile)
	})
	if a.hostingAccess.err != "" {
		ui.Text(c, a.hostingAccess.err).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
	}
	if a.hostingAccess.notice != "" {
		ui.Text(c, a.hostingAccess.notice).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	}
	if a.hostingAccess.repoKey != "" && a.session != nil && len(a.snapshot.Remotes) == 0 &&
		a.hostingAccess.repoKey == hostingBindingKey(a.remoteWebRepositoryKey()) {
		ui.Text(c, tr("This repository has no remote yet. A configuration chosen now applies once you add one.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	}
	if a.hostingAccess.repoKey != "" && len(a.settings.HostingAccounts) > 0 {
		hint := "Click a configuration to switch this repository to it, or add a new one."
		if _, ok := a.hostingBindingForRepository(a.hostingAccess.repoKey); !ok {
			hint = "Select a configuration for this repository by clicking it, or add a new one."
		}
		ui.Text(c, tr(hint)).TextColor(theme.Accent).FontSize(theme.FontSize - 1)
	}
	if len(a.settings.HostingAccounts) == 0 {
		ui.Column(c).FillWidth().Height(hostingAccessListHeight).Gap(7).Padding(28).Center().
			Background(theme.Surface.Alpha(0.55)).Border(1, theme.Border).
			BorderStyle(ui.BorderDashed).Radius(theme.Radius + 5).Children(func() {
			ui.Icon(c, settingsIcon).FontSize(24).TextColor(theme.Accent)
			ui.Text(c, tr("No Git hosts yet")).Bold()
			ui.Text(c, tr("Add a GitHub or GitLab host and keep its token in the system keychain.")).
				TextColor(theme.TextMuted).TextAlign(ui.Center)
		})
		return
	}
	bound, hasBinding := a.hostingBindingForRepository(a.hostingAccess.repoKey)
	var editID, deleteID string
	ui.Scroll(c.Key("hosting-configurations-list")).Height(hostingAccessListHeight).Shrink(0).FillWidth().
		Label(tr("Git hosting list")).Children(func() {
		ui.Column(c).FillWidth().Gap(8).Padding(2).Children(func() {
			for _, account := range a.settings.HostingAccounts {
				account := account
				selected := hasBinding && bound.ProfileID == account.ID
				hostMatch := a.hostingAccountFitsRepository(account)
				selectionTooltip := tr("Click to select this configuration")
				if selected {
					selectionTooltip = tr("Click to deselect this configuration")
				}
				c.Key("hosting-profile:" + account.ID)
				var item ui.Element
				if a.hostingAccess.repoKey != "" {
					item = ui.ButtonBase(c).Role(ui.RoleCheckBox).Checked(selected).
						Label(trf("Use configuration %s", account.Name)).
						Tooltip(selectionTooltip)
				} else {
					item = ui.Row(c)
				}
				item.FillWidth().Gap(0).AlignItems(ui.Stretch).
					Background(theme.Surface.Alpha(0.55)).Border(1, theme.Border).
					Radius(theme.Radius + 3).Clip()
				if a.hostingAccess.repoKey != "" && item.Hovered() {
					item.Background(theme.SurfaceHover)
				}
				item.Children(func() {
					if a.hostingAccess.repoKey != "" {
						barColor := ui.Color{}
						if selected {
							barColor = theme.Success
						}
						ui.Box(c).Width(4).Shrink(0).AlignSelf(ui.Stretch).Background(barColor)
					}
					ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(10).Padding(11, 12).Children(func() {
						ui.Box(c).Size(34, 34).Radius(theme.Radius + 2).Center().
							Background(theme.Accent.Alpha(0.12)).Children(func() {
							ui.Icon(c, hostingProviderIcon(hostingProviderName(account.Provider))).FontSize(17).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
							nameTooltip := account.Name
							summary := hostingProviderName(account.Provider) + " · " + hostingHostname(account.URL)
							summaryTooltip := summary
							if a.hostingAccess.repoKey != "" {
								nameTooltip = selectionTooltip
								summaryTooltip = selectionTooltip
							}
							ui.Text(c, account.Name).Bold().SingleLine().Tooltip(nameTooltip)
							ui.Text(c, summary).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).
								SingleLine().Tooltip(summaryTooltip).MinWidth(0)
							if a.hostingAccess.repoKey != "" {
								// Every card keeps this third line so a warning never makes one
								// taller than the others.
								warning, color := "\u00a0", theme.TextMuted
								if !hostMatch {
									warning, color = tr("Different host from this repository"), theme.Danger
								}
								ui.Text(c, warning).TextColor(color).FontSize(theme.FontSize - 2).SingleLine().MinWidth(0)
							}
						})
						count := a.hostingBindingCount(account.ID)
						usageButton := ui.ButtonBase(c).Size(30, 30).Center().Radius(theme.Radius).
							Label(trf("Repositories using %s", account.Name)).
							Tooltip(trf("Repositories using %s", account.Name))
						if usageButton.Hovered() {
							usageButton.Background(theme.SurfaceHover)
						}
						usageButton.Children(func() {
							ui.Icon(c, selectCommitsIcon).FontSize(14).TextColor(theme.TextMuted)
						})
						if usageButton.Clicked() {
							if a.hostingAccess.usageID == account.ID {
								a.hostingAccess.usageOpen = !a.hostingAccess.usageOpen
							} else {
								a.hostingAccess.usageID = account.ID
								a.hostingAccess.usageOpen = true
							}
						}
						if a.hostingAccess.usageID == account.ID {
							popover := ui.Popover(c, usageButton, &a.hostingAccess.usageOpen, func() {
								a.hostingRepositoryUsagePopover(c, account)
							})
							if popover.Dismissed() {
								a.hostingAccess.usageID = ""
							}
						}
						editLabel := trf("Edit configuration %s", account.Name)
						flatIconButton(c, editLabel, editLabel,
							editIcon, 14, 6, false, func() { editID = account.ID })
						deleteTooltip := trf("Delete configuration %s", account.Name)
						if count > 0 {
							deleteTooltip = trf("Used by %d repositories; change those bindings before deleting it", count)
						}
						flatIconButton(c, trf("Delete configuration %s", account.Name), deleteTooltip,
							trashIcon, 14, 6, count > 0, func() { deleteID = account.ID })
					})
				})
				if a.hostingAccess.repoKey != "" && item.Clicked() {
					a.toggleHostingBinding(account.ID)
				}
			}
		})
	})
	if editID != "" {
		a.selectHostingProfile(editID)
	}
	if deleteID != "" {
		a.confirmDeleteHostingProfile(deleteID)
	}
}

func (a *app) hostingRepositoryUsagePopover(c *ui.Context, account hostingAccount) {
	theme := c.Theme()
	repositories := a.hostingRepositories(account.ID)
	ui.Column(c).Width(310).Gap(8).Padding(5).Children(func() {
		ui.Text(c, trf("Repositories using %s", account.Name)).Bold().Padding(3, 5)
		if len(repositories) == 0 {
			ui.Text(c, tr("No repositories use this configuration.")).
				TextColor(theme.TextMuted).Padding(8, 5)
			return
		}
		ui.Scroll(c.Key("hosting-usage:" + account.ID)).FillWidth().MaxHeight(220).Children(func() {
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				for _, repository := range repositories {
					repository := repository
					c.Key("hosting-repository:" + repository.Path)
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(6, 5).Children(func() {
						ui.Icon(c, folderIcon).FontSize(14).TextColor(theme.Accent).Shrink(0)
						ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
							ui.Text(c, repository.Name).Bold().SingleLine().Tooltip(repository.Name)
							ui.Text(c, repository.Path).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted).
								SingleLine().Tooltip(repository.Path)
						})
					})
				}
			})
		})
	})
}

func (a *app) hostingAccessEditor(c *ui.Context) {
	theme := c.Theme()
	state := &a.hostingAccess
	hostingSettingRow(c, "Provider", "Choose the GitHub or GitLab server type", func() {
		names := []string{"GitLab", "GitHub"}
		chosen := hostingProviderName(state.provider)
		selects := ui.SelectBase(c, &chosen)
		selects.Trigger.Grow(1).Padding(7, 10).Gap(8).
			Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
			Label(tr("Hosting provider")).Children(func() {
			ui.Icon(c, hostingProviderIcon(chosen)).FontSize(15)
			ui.Text(c, chosen).Grow(1)
			ui.Icon(c, chevronDownIcon).FontSize(12).TextColor(theme.TextMuted)
		})
		selects.Popup(func(panel ui.Element) {
			panel.Padding(4).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius)
			for _, name := range names {
				item := selects.Item(name).Padding(7, 10).Gap(8).AlignItems(ui.Center)
				if item.Highlighted() {
					item.Background(theme.SurfaceHover)
				}
				item.Children(func() {
					ui.Icon(c, hostingProviderIcon(name)).FontSize(15)
					ui.Text(c, name).SingleLine()
				})
			}
		})
		if next := map[string]string{"GitLab": hostingProviderGitLab, "GitHub": hostingProviderGitHub}[chosen]; next != "" && next != state.provider {
			state.provider = next
		}
	})
	hostingSettingRow(c, "Configuration name", "A unique, memorable name for this saved account and token", func() {
		placeholder := defaultHostingProfileName(hostingAccount{Provider: state.provider, URL: state.url})
		ui.TextInput(c, &state.name).Label(tr("Configuration name")).Placeholder(placeholder).Grow(1).AutoFocus()
	})
	hostingSettingRow(c, "Server address", "For example https://gitlab.example.com", func() {
		ui.TextInput(c, &state.url).Label(tr("Server address")).
			Placeholder(tr("https://gitlab.example.com")).Grow(1)
	})
	hostingSettingRow(c, "Project", "Optional: enter group/project, or leave empty to use each repository's remote", func() {
		ui.TextInput(c, &state.project).Label(tr("Project path")).
			Placeholder(tr("group/project")).Grow(1)
	})
	hostingSettingRow(c, "Remote host", "Optional: use this when the Git remote has another host name, such as git.example.com", func() {
		ui.TextInput(c, &state.alias).Label(tr("Git remote host")).
			Placeholder(tr("git.example.com")).Grow(1)
	})
	for i := range state.webRemotes {
		label := "Web address"
		if len(state.webRemotes) > 1 {
			label = "Web · " + state.webRemotes[i].name
		}
		c.Key("hosting-web:" + state.webRemotes[i].name)
		hostingSettingRow(c, label, "Optional: the repository page used by Open on remote and Copy web URL. Leave it empty to derive it from the Git remote", func() {
			ui.TextInput(c, &state.webRemotes[i].url).Label(trf("Web address of remote %s", state.webRemotes[i].name)).
				Placeholder(tr("https://git.example.com/group/project")).Grow(1)
		})
	}
	hostingSettingRow(c, "Issue link", "The project issue-list page opened by the Issues toolbar button", func() {
		ui.TextInput(c, &state.issuesURL).Label(tr("Issue link")).
			Placeholder(tr("https://github.example.com/group/project/issues")).Grow(1)
	})
	hostingSettingRow(c, "Description issue link", "Turns #1234 in descriptions into a link; use {id} for the issue number", func() {
		ui.TextInput(c, &state.issueLink).Label(tr("Issue link in descriptions")).
			Placeholder(tr("https://tracker.example.com/view?id={id}")).Grow(1)
	})
	hint := "Optional: paste a personal access token (read_api is enough to browse). Leave it empty to use the login of gh or glab"
	if state.hasToken {
		hint = "A token is saved. It is hidden until you press the eye"
	}
	hostingSettingRow(c, "Access token", hint, func() {
		ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6).Children(func() {
			input := ui.TextInput(c, &state.token).Label(tr("Access token")).
				Placeholder(tr("Optional · not stored in settings")).Grow(1)
			if !state.showToken {
				input.Password()
			}
			eye, tip := eyeIcon, "Show token"
			if state.showToken {
				eye, tip = eyeOffIcon, "Hide token"
			}
			iconButton(c, tip, tip, eye, false, func() { state.showToken = !state.showToken })
			iconButton(c, "Copy token", "Copy the token", copyIcon, strings.TrimSpace(state.token) == "",
				func() { a.notifyCopied(a.services, "access token", strings.TrimSpace(state.token)) })
		})
	})
	if count := a.hostingBindingCount(state.profileID); count > 0 {
		ui.Text(c, trf("Used by %d repositories; changes apply to all of them.", count)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	}
	ui.Text(c, tr("Applies to local repositories. SSH workspaces keep using the remote machine's own login.")).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	if state.err != "" {
		ui.Text(c, state.err).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
	}
	if state.notice != "" {
		noticeColor := theme.TextMuted
		if state.noticeOK {
			noticeColor = theme.Success
		}
		ui.Text(c, state.notice).TextColor(noticeColor).FontSize(theme.FontSize - 1)
	}
	// Checking and removing the token sit on the left, the way out of the
	// dialog on the right.
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Gap(8).Wrap().Children(func() {
		ui.Row(c).Gap(8).Wrap().Children(func() {
			iconTextButtonState(c, "Test connection", "Check the server and token", plugIcon, false, state.busy, a.testHostingAccess)
			if state.hasToken {
				iconTextButtonState(c, "Remove token", "Delete the saved token from the keychain", trashIcon, false, state.busy, a.removeHostingToken)
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			iconTextButton(c, "Cancel", "Return to configurations without saving", closeIcon, false, a.closeHostingAccessEditor)
			saveLabel := "Save"
			if state.repoKey != "" {
				saveLabel = "Save & use"
			}
			iconTextButtonState(c, saveLabel, "Save the configuration and token, using it for this repository when one is selected", commandSuccessIcon, true, state.busy, a.saveHostingAccess)
		})
	})
}
