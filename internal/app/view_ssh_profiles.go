package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

func (a *app) sshProfileSettings(c *ui.Context) {
	settingsSection(c, func() {
		settingsGroup(c, remotesIcon, "SSH profiles", "Remote Git workspaces, without credentials in settings")
		if a.sshProfileFormOpen {
			a.sshProfileForm(c)
			return
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, tr("Passwords and key passphrases use the system keychain when available.")).
				TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1).Grow(1)
			iconTextButton(c, "Add SSH profile…", "Create an SSH connection profile", plusIcon, false, func() {
				a.openSSHProfileForm("")
			})
		})
		if len(a.settings.SSHProfiles) == 0 {
			ui.Column(c).Gap(7).Padding(24).AlignItems(ui.Center).
				Background(c.Theme().Surface.Alpha(0.55)).Border(1, c.Theme().Border).
				BorderStyle(ui.BorderDashed).Radius(c.Theme().Radius + 5).Children(func() {
				ui.Icon(c, remotesIcon).FontSize(24).TextColor(c.Theme().Accent)
				ui.Text(c, tr("No SSH profiles yet")).Bold()
				ui.Text(c, tr("Add a host to open a Git folder on it from Home.")).
					TextColor(c.Theme().TextMuted).TextAlign(ui.Center)
			})
			return
		}
		var editID, deleteID string
		for _, profile := range a.settings.SSHProfiles {
			profile := profile
			c.Key("ssh-profile:" + profile.ID)
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(11, 12).
				Background(c.Theme().Surface.Alpha(0.55)).Border(1, c.Theme().Border).
				Radius(c.Theme().Radius + 3).Children(func() {
				ui.Box(c).Size(34, 34).Radius(c.Theme().Radius + 2).Center().
					Background(c.Theme().Accent.Alpha(0.12)).Children(func() {
					ui.Icon(c, remotesIcon).FontSize(17).TextColor(c.Theme().Accent)
				})
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, profile.Name).Bold().SingleLine()
					ui.Text(c, sshProfileSummary(profile)).TextColor(c.Theme().TextMuted).
						FontSize(c.Theme().FontSize - 2).SingleLine().Tooltip(sshProfileSummary(profile))
				})
				if ui.Button(c, tr("Edit")).Label(trf("Edit SSH profile %s", profile.Name)).Clicked() {
					editID = profile.ID
				}
				if ui.Button(c, tr("Delete")).Label(trf("Delete SSH profile %s", profile.Name)).Clicked() {
					deleteID = profile.ID
				}
			})
		}
		if editID != "" {
			a.openSSHProfileForm(editID)
		}
		if deleteID != "" {
			a.deleteSSHProfile(deleteID)
			c.Invalidate()
		}
	})
}

// sshProfileForm is shared by Settings and Home's first-profile flow. Sensitive
// values are write-only drafts: editing never loads an existing secret into a
// visible field, and an empty field keeps the stored value.
func (a *app) sshProfileForm(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(10).Padding(14).Background(theme.Surface.Alpha(0.55)).
		Border(1, theme.Border).Radius(theme.Radius + 5).Children(func() {
		title := tr("Add SSH profile")
		if a.sshProfileDraft.ID != "" {
			title = tr("Edit SSH profile")
		}
		ui.Text(c, title).Bold().FontSize(16)
		settingRow(c, "Profile name", "A label for this connection", func() {
			ui.TextInput(c, &a.sshProfileDraft.Name).Label(tr("SSH profile name")).
				Placeholder("Production").Grow(1).AutoFocus()
		})
		settingRow(c, "Host", "DNS name or IP address, without a port", func() {
			ui.TextInput(c, &a.sshProfileDraft.Host).Label(tr("SSH host")).
				Placeholder("server.example.com").Grow(1)
		})
		settingRow(c, "Username", "Remote login name", func() {
			ui.TextInput(c, &a.sshProfileDraft.Username).Label(tr("SSH username")).
				Placeholder("git").Grow(1)
		})
		settingRow(c, "Port", "SSH defaults to 22", func() {
			ui.TextInput(c, &a.sshProfileDraft.Port).Label(tr("SSH port")).
				Placeholder("22").Width(110)
		})
		settingRow(c, "Authentication", "Agent, private key, or password", func() {
			ui.Select(c, &a.sshProfileDraft.AuthMode, []string{sshAuthAgent, sshAuthKey, sshAuthPassword}).
				Label(tr("SSH authentication")).Grow(1)
		})
		switch a.sshProfileDraft.AuthMode {
		case sshAuthKey:
			settingRow(c, "Private key", "A path on this machine", func() {
				ui.TextInput(c, &a.sshProfileDraft.PrivateKeyPath).Label(tr("SSH private-key path")).
					Placeholder("~/.ssh/id_ed25519").Grow(1)
			})
			settingRow(c, "Key passphrase", "Leave blank to keep the saved passphrase", func() {
				ui.TextInput(c, &a.sshProfileDraft.Passphrase).Label(tr("SSH private-key passphrase")).
					Placeholder(tr("Not stored in settings")).Password().Grow(1)
			})
		case sshAuthPassword:
			settingRow(c, "Password", "Leave blank to keep the saved password", func() {
				ui.TextInput(c, &a.sshProfileDraft.Password).Label(tr("SSH password")).
					Placeholder(tr("Not stored in settings")).Password().Grow(1)
			})
		}
		settingRow(c, "Default remote folder", "Absolute, ~, or ~/folder", func() {
			ui.TextInput(c, &a.sshProfileDraft.DefaultRoot).Label(tr("SSH default remote folder")).
				Placeholder("~/src/project").Grow(1)
		})
		if !validSSHRoot(a.sshProfileDraft.DefaultRoot) {
			ui.Text(c, tr("Use an absolute POSIX path, ~, or a path beginning with ~/.")).
				TextColor(theme.Warning).FontSize(theme.FontSize - 1)
		}
		if a.sshProfileFormError != "" {
			ui.Text(c, a.sshProfileFormError).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
		}
		if a.sshProfileFormNotice != "" {
			ui.Text(c, a.sshProfileFormNotice).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
		ui.Row(c).Justify(ui.End).Gap(8).Wrap().Children(func() {
			if ui.Button(c, tr("Test connection")).Disabled(a.sshProfileTestBusy).Clicked() {
				a.testSSHProfile()
			}
			if ui.Button(c, tr("Cancel")).Label(tr("Cancel SSH profile form")).Clicked() {
				a.closeSSHProfileForm()
			}
			if ui.PrimaryButton(c, tr("Save SSH profile")).Clicked() {
				a.submitSSHProfileForm(c)
			}
		})
	})
}

// sshWorkspaceModal is Home's remote launcher. It has no SSH side effects until
// Open, which converts the validated selection to the app's canonical URI.
func (a *app) sshWorkspaceModal(c *ui.Context) {
	if !a.sshWorkspaceOpen {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(640).Height(600).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 8).
				Role(ui.RoleDialog).Label(tr("Open SSH workspace"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(36, 36).Radius(9).Center().
						Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, remotesIcon).FontSize(19).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Open SSH workspace")).FontSize(19).Bold()
						ui.Text(c, tr("Choose a profile and a remote Git folder")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close SSH workspace", "Escape", closeIcon, false, a.closeSSHWorkspacePicker)
				})
				ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(12).Padding(2).Children(func() {
						if a.sshProfileFormOpen {
							a.sshProfileForm(c)
							return
						}
						a.sshWorkspaceProfiles(c)
					})
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeSSHWorkspacePicker()
			}
		})
	})
}

func (a *app) sshWorkspaceProfiles(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("SSH profiles")).Bold().Grow(1)
		iconTextButton(c, "New SSH profile…", "Create a profile without leaving Home", plusIcon, false, func() {
			a.openSSHProfileForm("")
		})
	})
	if len(a.settings.SSHProfiles) == 0 {
		ui.Text(c, tr("No SSH profiles yet. Create one to continue.")).TextColor(theme.TextMuted)
		return
	}
	for _, profile := range a.settings.SSHProfiles {
		profile := profile
		selected := profile.ID == a.sshWorkspaceProfileID
		c.Key("ssh-workspace-profile:" + profile.ID)
		button := ui.ButtonBase(c).FillWidth().Padding(11, 12).Gap(10).AlignItems(ui.Center).
			Role(ui.RoleRadio).Checked(selected).Label(trf("Use SSH profile %s", profile.Name)).
			Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius + 3)
		if selected {
			button.Background(theme.Accent.Alpha(0.13)).Border(1, theme.Accent.Alpha(0.55))
		} else if button.Hovered() {
			button.Background(theme.SurfaceHover)
		}
		button.Children(func() {
			ui.Icon(c, remotesIcon).FontSize(17).TextColor(theme.Accent)
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, profile.Name).Bold().SingleLine()
				ui.Text(c, sshProfileSummary(profile)).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 2).SingleLine()
			})
			if selected {
				ui.Icon(c, commitIcon).FontSize(15).TextColor(theme.Accent)
			}
		})
		if button.Clicked() {
			a.selectSSHWorkspaceProfile(profile.ID)
		}
	}
	ui.Divider(c)
	ui.Column(c).Gap(5).Children(func() {
		ui.Text(c, tr("Remote folder")).Bold()
		input := ui.TextInput(c, &a.sshWorkspaceRoot).Label(tr("SSH workspace remote folder")).
			Placeholder("/home/user/project").Grow(1)
		if input.Submitted() {
			a.openSelectedSSHWorkspace()
		}
		ui.Text(c, tr("Use an absolute POSIX path, ~, or a path beginning with ~/.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	})
	if a.sshWorkspaceResolving {
		ui.Text(c, tr("Resolving the remote folder…")).TextColor(theme.TextMuted)
	}
	if a.sshWorkspaceError != "" {
		ui.Text(c, a.sshWorkspaceError).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
	}
	if selected, ok := a.selectedSSHProfile(); ok {
		ui.Text(c, fmt.Sprintf("%s · %s", selected.Name, selected.AuthMode)).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	}
	ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
		if ui.Button(c, tr("Cancel")).Label(tr("Cancel SSH workspace")).Clicked() {
			a.closeSSHWorkspacePicker()
		}
		if ui.PrimaryButton(c, tr("Open")).Disabled(a.sshWorkspaceResolving).
			Label(tr("Open selected SSH workspace")).Clicked() {
			a.openSelectedSSHWorkspace()
		}
	})
}
