package app

import "github.com/egoist/mygo/ui"

// codeWorkspaceFolderModal keeps draft path edits out of the sidebar header.
// Invalid paths stay in the dialog; a valid path updates both Explorer and Search.
func (a *app) codeWorkspaceFolderModal(c *ui.Context) {
	if !a.code.workspaceFormOpen {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		c.Key("code-workspace-folder-modal")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(460).MaxWidthPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Change workspace folder"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().
						Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, folderOpenIcon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Change workspace folder")).FontSize(17).Bold()
						ui.Text(c, a.snapshot.Root).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
							SingleLine().Ellipsis("…").Tooltip(a.snapshot.Root)
					})
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, tr("Workspace folder")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					input := ui.TextInput(c, &a.code.searchFolderDraft).
						Label(tr("Workspace folder")).Placeholder(tr("Repository root")).AutoFocus()
					if input.Submitted() {
						a.submitCodeWorkspaceFolderForm()
					}
				})
				if a.code.workspaceFormError != "" {
					ui.Text(c, a.code.workspaceFormError).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
				} else {
					ui.Text(c, tr("Enter a repository-relative folder, or leave it empty to use the repository root.")).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				}
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					if ui.Button(c, tr("Browse")).Clicked() {
						a.browseCodeSearchFolder()
					}
					ui.Spacer(c)
					if ui.Button(c, tr("Cancel")).Clicked() {
						a.cancelCodeWorkspaceFolderForm()
					}
					if ui.PrimaryButton(c, tr("Open folder")).Clicked() {
						a.submitCodeWorkspaceFolderForm()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.cancelCodeWorkspaceFolderForm()
			}
		})
	})
}
