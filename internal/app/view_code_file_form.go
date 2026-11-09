package app

import (
	"github.com/egoist/mygo/ui"
)

// codeFileFormModal names a new file/folder or renames an existing entry. It
// stays open on an OS error so the chosen name is not lost.
func (a *app) codeFileFormModal(c *ui.Context) {
	if a.code.fileForm == codeFileFormNone {
		return
	}
	theme := c.Theme()
	title, field, submit, icon := "New File", "File name", "Create", newFileIcon
	if a.code.fileForm == codeFileFormNewFolder {
		title, field, icon = "New Folder", "Folder name", newFolderIcon
	} else if a.code.fileForm == codeFileFormRename {
		title, field, submit, icon = "Rename", "Name", "Rename", editIcon
	}
	location := a.code.fileFormDirectory
	if location == "" {
		location = tr("Repository root")
	}
	ui.Overlay(c, func() {
		c.Key("code-file-form-modal")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(460).MaxWidthPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr(title))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().
						Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, icon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr(title)).FontSize(17).Bold()
						ui.Text(c, location).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().Tooltip(location)
					})
					iconButton(c, "Close file form", "Escape", closeIcon, a.code.fileOperationBusy, a.cancelCodeFileForm)
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, tr(field)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					input := ui.TextInput(c, &a.code.fileFormName).
						Label(tr(field)).Placeholder(tr(field)).AutoFocus()
					if input.Submitted() {
						a.submitCodeFileForm()
					}
				})
				if a.code.fileFormError != "" {
					ui.Text(c, a.code.fileFormError).TextColor(theme.Danger)
				} else {
					ui.Text(c, tr("Use a single name, without / or \\. Existing entries are never overwritten.")).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				}
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					if a.code.fileOperationBusy {
						ui.Spinner(c).Label(tr("Working"))
						ui.Text(c, tr("Working…")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					}
					ui.Spacer(c)
					if ui.Button(c, tr("Cancel")).Disabled(a.code.fileOperationBusy).Clicked() {
						a.cancelCodeFileForm()
					}
					if ui.PrimaryButton(c, tr(submit)).Disabled(a.code.fileOperationBusy).Clicked() {
						a.submitCodeFileForm()
					}
				})
			})
			if !a.code.fileOperationBusy && (panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape)) {
				a.cancelCodeFileForm()
			}
		})
	})
}
