package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// openExportPatchForm prepares one mailbox patch per selected commit. It
// writes normal files to a local folder rather than inventing a second patch
// transport for SSH workspaces.
func (a *app) openExportPatchForm(commits []repository.Commit) {
	if a.actionBusy || a.session == nil || len(commits) == 0 {
		return
	}
	if a.location.IsRemote() {
		a.notify("warning", tr("Export to a local folder"), tr("Mailbox patch export is available for local repositories; SSH workspaces can apply patches."))
		return
	}
	a.exportPatchCommits = append([]repository.Commit(nil), commits...)
	a.exportPatchDir = filepath.Join(a.snapshot.Root, "patches")
	a.showExportPatchForm = true
}

func (a *app) chooseExportPatchFolder() {
	options := mygo.OpenDialogOptions{
		Parent:      a.window,
		Title:       tr("Choose patch export folder"),
		DefaultPath: a.exportPatchDir,
		Directory:   true,
		ButtonLabel: tr("Choose"),
	}
	go func() {
		defer a.recoverBackground()
		paths, err := chooseRepositoryFolder(options)
		a.update(func() {
			if err != nil || len(paths) == 0 || !a.showExportPatchForm {
				return
			}
			a.exportPatchDir = paths[0]
		})
	}()
}

func (a *app) beginExportPatches() {
	directory := strings.TrimSpace(a.exportPatchDir)
	commits := append([]repository.Commit(nil), a.exportPatchCommits...)
	if directory == "" || a.actionBusy || a.session == nil {
		return
	}
	a.showExportPatchForm = false
	var paths []string
	a.beginRepositoryActionMessage(tr("Exported mailbox patches"), "",
		func(ctx context.Context, session repository.Session) error {
			var err error
			paths, err = session.ExportPatches(ctx, commits, directory)
			return err
		}, func() {
			if len(paths) > 0 {
				a.notify("success", trf("Exported %d patch%s", len(paths), pluralS(len(paths))), paths[0])
			}
			a.exportPatchCommits = nil
		})
}

func (a *app) exportPatchFormModal(c *ui.Context) {
	if !a.showExportPatchForm {
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
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Export patches")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, downloadIcon).FontSize(20).TextColor(theme.Accent)
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Export mailbox patches")).FontSize(17).Bold()
						ui.Text(c, trf("%d commit%s, oldest first", len(a.exportPatchCommits), pluralS(len(a.exportPatchCommits)))).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close patch export", "Escape", closeIcon, false,
						func() { a.showExportPatchForm = false })
				})
				ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
					ui.TextInput(c, &a.exportPatchDir).Label(tr("Target folder")).
						Placeholder(tr("Local output folder")).Grow(1)
					if ui.Button(c, tr("Browse…")).Clicked() {
						a.chooseExportPatchFolder()
					}
				})
				ui.Text(c, tr("Creates a format-patch .patch file per commit and leaves existing files untouched.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Cancel")).Clicked() {
						a.showExportPatchForm = false
						a.exportPatchCommits = nil
					}
					if ui.PrimaryButton(c, tr("Export patches")).Disabled(a.actionBusy || strings.TrimSpace(a.exportPatchDir) == "").Clicked() {
						a.beginExportPatches()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.showExportPatchForm = false
			}
		})
	})
}

// beginMailboxPatchImport picks a mailbox-format series produced by tools
// such as git format-patch. Plain one-file diffs without mail headers belong
// to the existing .patch/.diff preview workflow.
func (a *app) beginMailboxPatchImport() {
	if a.actionBusy || a.session == nil {
		return
	}
	options := mygo.OpenDialogOptions{
		Parent:      a.window,
		Title:       tr("Apply mailbox patch"),
		DefaultPath: a.patchDialogDefaultPath(""),
		ButtonLabel: tr("Apply"),
		Filters: []mygo.FileFilter{
			{Name: tr("Mailbox patches"), Extensions: []string{"patch", "mbox"}},
		},
	}
	go func() {
		defer a.recoverBackground()
		paths, err := choosePatchPath(options)
		if err != nil || len(paths) == 0 {
			a.update(func() {
				if err != nil {
					a.notifyError(err)
				}
			})
			return
		}
		content, err := os.ReadFile(paths[0])
		name := filepath.Base(paths[0])
		a.update(func() {
			if err != nil {
				a.notifyError(err)
				return
			}
			a.confirmMailboxPatchImport(name, content)
		})
	}()
}

func (a *app) confirmMailboxPatchImport(name string, content []byte) {
	if a.actionBusy || a.session == nil || len(content) == 0 {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Apply Mailbox Patch",
		message:      trf("Apply %s to the current branch?", name),
		detail:       tr("Creates commits with original authors and messages, like git am --3way. A three-way fallback handles context drift; conflicts can be resolved and continued."),
		confirmLabel: "Apply patch",
		run: func() {
			a.beginRepositoryActionMessage(trf("Applied %s", name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.AmPatch(ctx, name, content)
				}, nil)
		},
	})
}

// mailboxPatchToolbarButton exposes mailbox import from Changes, next to
// the ordinary .patch/.diff workflow.
func (a *app) mailboxPatchToolbarButton(c *ui.Context) {
	flatIconButton(c, "Apply mailbox patch…", "Apply mailbox patches with a three-way fallback", patchImportIcon, 15, 5,
		a.actionBusy, a.beginMailboxPatchImport)
}
