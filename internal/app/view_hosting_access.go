package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

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

// hostingAccessModal lets the user connect a GitHub or GitLab server from the
// Pull Requests page. The token is a write-only field stored in the keychain.
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
				Role(ui.RoleDialog).Label(tr("Config PR/MR host"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, settingsIcon).FontSize(19).TextColor(theme.Accent)
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Config PR/MR host")).FontSize(18).Bold()
						ui.Text(c, tr("The token is kept in your system keychain, not in settings.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close", "Escape", closeIcon, false, a.closeHostingAccess)
				})
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
						a.loadHostingAccessDraft()
					}
				})
				hostingSettingRow(c, "Server address", "For example https://gitlab.example.com", func() {
					input := ui.TextInput(c, &state.url).Label(tr("Server address")).
						Placeholder(tr("https://gitlab.example.com")).Grow(1).AutoFocus()
					if input.Changed() {
						a.loadHostingAccessDraft()
					}
				})
				hostingSettingRow(c, "Project", "Optional: enter group/project, or leave empty to use each repository's remote", func() {
					ui.TextInput(c, &state.project).Label(tr("Project path")).
						Placeholder(tr("group/project")).Grow(1)
				})
				hostingSettingRow(c, "Remote host", "Optional: use this when the Git remote has another host name, such as git.example.com", func() {
					ui.TextInput(c, &state.alias).Label(tr("Git remote host")).
						Placeholder(tr("git.example.com")).Grow(1)
				})
				hostingSettingRow(c, "Issue link", "The project issue-list page opened by the Issues toolbar button", func() {
					ui.TextInput(c, &state.issuesURL).Label(tr("Issue link")).
						Placeholder(tr("https://github.example.com/group/project/issues")).Grow(1)
				})
				hostingSettingRow(c, "Description issue link", "Turns #1234 in descriptions into a link; use {id} for the issue number", func() {
					ui.TextInput(c, &state.issueLink).Label(tr("Issue link in descriptions")).
						Placeholder(tr("https://tracker.example.com/view?id={id}")).Grow(1)
				})
				hint := "Paste a personal access token (read_api is enough to browse)"
				if state.hasToken {
					hint = "A token is saved. It is hidden until you press the eye"
				}
				hostingSettingRow(c, "Access token", hint, func() {
					ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6).Children(func() {
						input := ui.TextInput(c, &state.token).Label(tr("Access token")).
							Placeholder(tr("Not stored in settings")).Grow(1)
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
				ui.Text(c, tr("Applies to local repositories. SSH workspaces keep using the remote machine's own login.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				if state.err != "" {
					ui.Text(c, state.err).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
				}
				if state.notice != "" {
					ui.Text(c, state.notice).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				}
				// Checking and removing the token sit on the left, the way out of the
				// dialog on the right.
				ui.Row(c).FillWidth().Justify(ui.SpaceBetween).Gap(8).Wrap().Children(func() {
					ui.Row(c).Gap(8).Wrap().Children(func() {
						iconTextButtonState(c, "Test connection", "Check the server and token", plugIcon, false, state.busy, a.testHostingAccess)
						if state.hasToken {
							iconTextButtonState(c, "Remove token", "Delete the saved token from the keychain", trashIcon, false, state.busy, a.removeHostingAccess)
						}
					})
					ui.Row(c).Gap(8).Children(func() {
						iconTextButton(c, "Cancel", "Close without saving", closeIcon, false, a.closeHostingAccess)
						iconTextButtonState(c, "Save", "Save the server and token", commandSuccessIcon, true, state.busy, a.saveHostingAccess)
					})
				})
			})
			if a.confirm == nil && (panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape)) {
				a.closeHostingAccess()
			}
		})
	})
}
