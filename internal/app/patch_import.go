package app

import (
	"context"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// choosePatchPath is the native, single-file patch picker. Keeping the dialog
// behind a variable lets tests answer it without driving the desktop.
var choosePatchPath = func(opts mygo.OpenDialogOptions) ([]string, error) {
	return mygo.Dialog.Open(opts)
}

// beginPatchImport picks and validates a patch away from the UI goroutine.
// Validation is read-only; only beginPatchApply is allowed to change Git.
func (a *app) beginPatchImport() {
	if a.actionBusy || a.session == nil {
		return
	}
	a.patchPreviewToken++
	token := a.patchPreviewToken
	generation := a.generation
	remote := a.location.IsRemote()
	session := a.session
	parent := a.window
	options := mygo.OpenDialogOptions{
		Parent:      parent,
		Title:       tr("Open patch"),
		DefaultPath: a.patchDialogDefaultPath(""),
		ButtonLabel: tr("Preview patch"),
		Filters: []mygo.FileFilter{
			{Name: tr("Patch files"), Extensions: []string{"patch", "diff"}},
		},
	}
	a.patchPreviewOpen = true
	a.patchPreviewLoading = true
	a.patchPreview = nil
	a.patchPreviewError = ""

	go func() {
		defer a.recoverBackground()
		paths, err := choosePatchPath(options)
		if err != nil || len(paths) == 0 {
			a.update(func() {
				if token != a.patchPreviewToken || generation != a.generation {
					return
				}
				a.patchPreviewLoading = false
				if err != nil {
					a.patchPreviewError = safeError(err)
					return
				}
				a.closePatchPreview()
			})
			return
		}

		var preview repository.PatchPreview
		var previewErr error
		if remote {
			var data []byte
			data, previewErr = os.ReadFile(paths[0])
			if previewErr == nil {
				preview, previewErr = session.PreviewPatchText(context.Background(), string(data))
			}
		} else {
			preview, previewErr = session.PreviewPatch(context.Background(), paths[0])
		}
		a.update(func() {
			if token != a.patchPreviewToken || generation != a.generation {
				return
			}
			a.patchPreviewLoading = false
			if previewErr != nil {
				a.patchPreviewError = safeError(previewErr)
				return
			}
			a.patchPreview = &preview
		})
	}()
}

// closePatchPreview discards only the in-memory preview. The picked file is
// external input and is never removed or rewritten by this workflow.
func (a *app) closePatchPreview() {
	a.patchPreviewToken++
	a.patchPreviewOpen = false
	a.patchPreviewLoading = false
	a.patchPreview = nil
	a.patchPreviewError = ""
}

func (a *app) discardPatchPreview() {
	// Do not let Escape race an Apply click in the following rebuild. Closing
	// during Apply is harmless to Git, but keeping the dialog makes its busy
	// state and eventual result unambiguous.
	if a.actionBusy {
		return
	}
	a.closePatchPreview()
}

// beginPatchApply passes back the exact preview, including its private Git
// validation token. The repository checks the file and worktree again before
// applying anything, so repository changes while this dialog is open stop it.
func (a *app) beginPatchApply() {
	if a.actionBusy || a.session == nil || a.patchPreview == nil || !a.patchPreview.Clean {
		return
	}
	preview := *a.patchPreview
	a.beginRepositoryActionMessage(tr("Applied patch to the working tree"), "",
		func(ctx context.Context, session repository.Session) error {
			return session.ApplyPatch(ctx, preview)
		}, a.closePatchPreview)
}

func patchFileLabel(file repository.PatchFile) string {
	if file.OldPath != "" && file.OldPath != file.Path {
		return file.OldPath + " → " + file.Path
	}
	return file.Path
}

// patchPreviewModal gives the user the complete decision before mutation:
// affected files, diagnostics and patch text in a scrollable body, with only
// safe dismissal available when Git reports a conflict or an invalid patch.
func (a *app) patchPreviewModal(c *ui.Context) {
	if !a.patchPreviewOpen {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		c.Key("patch-preview-modal")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(880).Height(680).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Apply Patch/Diff")).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(theme.Accent.Alpha(0.14)).Children(func() {
							ui.Icon(c, patchImportIcon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, tr("Apply Patch/Diff")).FontSize(18).Bold()
							ui.Text(c, tr("Review every change before applying it to the working tree.")).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close patch preview", "Escape", closeIcon, a.actionBusy, a.discardPatchPreview)
					})
					ui.Divider(c)
					ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Label(tr("Patch preview")).Children(func() {
						a.patchPreviewBody(c)
					})
					ui.Divider(c)
					a.patchPreviewFooter(c)
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.discardPatchPreview()
			}
		})
	})
}

func (a *app) patchPreviewBody(c *ui.Context) {
	theme := c.Theme()
	if a.patchPreviewLoading {
		ui.Column(c).Grow(1).Center().Gap(8).Padding(30).Children(func() {
			ui.Text(c, tr("Checking patch without changing the repository…")).Bold()
			ui.Text(c, tr("Git is validating the files and hunks. Your index and working tree stay untouched.")).
				TextColor(theme.TextMuted).TextAlign(ui.Center)
		})
		return
	}
	if a.patchPreviewError != "" {
		ui.Column(c).Gap(8).Padding(12).Children(func() {
			ui.Text(c, tr("Could not preview patch")).Bold().TextColor(theme.Danger)
			ui.Text(c, a.patchPreviewError).Selectable()
		})
		return
	}
	preview := a.patchPreview
	if preview == nil {
		return
	}

	ui.Column(c).FillWidth().Gap(12).Children(func() {
		ui.Text(c, preview.Path).SingleLine().Tooltip(preview.Path)
		statusColor := theme.Success
		status := tr("Patch applies cleanly")
		detail := tr("Apply changes the working tree only; nothing will be staged.")
		if !preview.Clean {
			statusColor = theme.Danger
			status = tr("Patch cannot be applied")
			detail = tr("Resolve the reported conflicts or edit the source files, then preview the patch again. Nothing was changed.")
		}
		ui.Column(c).Gap(5).Padding(10).Background(statusColor.Alpha(0.1)).
			Border(1, statusColor.Alpha(0.45)).Radius(theme.Radius).Children(func() {
			ui.Text(c, status).Bold().TextColor(statusColor)
			ui.Text(c, detail).FontSize(theme.FontSize - 1)
			ui.Text(c, trf("%d files · +%d · −%d", len(preview.Files), preview.Additions, preview.Deletions)).
				FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
		})
		if preview.Diagnostic != "" {
			ui.Column(c).Gap(5).Children(func() {
				ui.Text(c, tr("Git diagnostics")).Bold()
				ui.Text(c, preview.Diagnostic).Selectable().TextColor(theme.Danger)
			})
		}
		if len(preview.Files) > 0 {
			ui.Column(c).Gap(5).Children(func() {
				ui.Text(c, tr("Affected files")).Bold()
				for _, file := range preview.Files {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(6, 8).
						Background(theme.Surface).Radius(theme.Radius).Children(func() {
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, patchFileLabel(file)).Tooltip(patchFileLabel(file))
							if file.Summary != "" {
								ui.Text(c, file.Summary).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
							}
						})
						switch {
						case file.Binary:
							ui.Badge(c, tr("Binary"))
						case file.Additions != 0 || file.Deletions != 0:
							fileDiffStatCounts(c, file.Path, file.Additions, file.Deletions)
						}
					})
				}
			})
		}
		if preview.Text != "" {
			ui.Column(c).Gap(5).Children(func() {
				ui.Text(c, tr("Patch text")).Bold()
				ui.Text(c, preview.Text).Selectable().FontSize(theme.FontSize - 1)
				if preview.TextTruncated {
					ui.Text(c, tr("Patch text is truncated here; Apply still validates and uses the complete file.")).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				}
			})
		}
	})
}

func (a *app) patchPreviewFooter(c *ui.Context) {
	ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
		if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
			a.discardPatchPreview()
		}
		if ui.Button(c, tr("Discard Patch")).Tooltip(tr("Close this preview without changing or deleting the patch file")).
			Disabled(a.actionBusy).Clicked() {
			a.discardPatchPreview()
		}
		if a.patchPreview != nil && a.patchPreview.Clean {
			if ui.PrimaryButton(c, tr("Apply Patch")).Tooltip(tr("Apply all changes atomically to the working tree, leaving them unstaged")).
				Disabled(a.actionBusy || a.patchPreviewLoading).Clicked() {
				a.beginPatchApply()
			}
		}
	})
}
