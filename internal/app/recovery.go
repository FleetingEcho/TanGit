package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func (a *app) recoveryModal(c *ui.Context) {
	if !a.showRecoveryModal || a.confirm != nil {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(640).Height(520).MaxWidthPercent(92).MaxHeightPercent(85).
				Padding(20).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Recovery & checkpoints"))
			panel.Children(func() {
				ui.Scroll(c).FillWidth().Grow(1).Shrink(1).MinHeight(0).Children(func() {
					a.recoveryPanel(c)
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.showRecoveryModal = false
			}
		})
	})
}

// recoveryPanel owns automatic discard recovery and named checkpoints.
// These refs are separate from ordinary stashes.
func (a *app) recoveryPanel(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(12).
		Role(ui.RoleRegion).Label(tr("Recovery & checkpoints")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Icon(c, softResetIcon).FontSize(15).TextColor(theme.Accent)
			ui.Text(c, tr("Recovery & checkpoints")).FontSize(17).Bold().Grow(1)
			iconButton(c, "Close", "Escape", closeIcon, false, func() { a.showRecoveryModal = false })
		})
		ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			recoveryClicked := false
			if a.savedWorkPanel == 0 {
				recoveryClicked = compactTextButton(ui.PrimaryButton(c, tr("Recovery"))).Label(tr("Show recovery and checkpoints")).Clicked()
			} else {
				recoveryClicked = compactTextButton(ui.Button(c, tr("Recovery"))).Label(tr("Show recovery and checkpoints")).Clicked()
			}
			if recoveryClicked {
				a.savedWorkPanel = 0
			}
			journalClicked := false
			if a.savedWorkPanel == 1 {
				journalClicked = compactTextButton(ui.PrimaryButton(c, tr("Safety Journal"))).Label(tr("Show safety journal")).Clicked()
			} else {
				journalClicked = compactTextButton(ui.Button(c, tr("Safety Journal"))).Label(tr("Show safety journal")).Clicked()
			}
			if journalClicked {
				a.savedWorkPanel = 1
			}
			ui.Badge(c, fmt.Sprintf("%d", len(a.snapshot.WorkSnapshots)+len(a.snapshot.Operations)))
		})
		if a.savedWorkPanel == 1 {
			a.safetyJournalBody(c)
			return
		}
		ui.Text(c, tr("Recovery copies appear here after a discard. Named checkpoints stay until deleted.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		ui.Text(c, tr("Save all current work without changing it")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
			input := ui.TextInput(c, &a.checkpointName).Label(tr("Checkpoint name")).
				Placeholder(tr("Name a checkpoint")).Grow(1)
			create := func() {
				name := strings.TrimSpace(a.checkpointName)
				if a.actionBusy || name == "" || a.session == nil {
					return
				}
				a.beginRepositoryActionMessage(trf("Created checkpoint %s", name), "",
					func(ctx context.Context, session repository.Session) error {
						_, err := session.CreateCheckpoint(ctx, name)
						return err
					}, func() { a.checkpointName = "" })
			}
			if input.Submitted() {
				create()
			}
			if compactTextButton(ui.PrimaryButton(c, tr("Create checkpoint"))).Label(tr("Create checkpoint")).
				Tooltip(tr("Save all current work without changing it")).
				Disabled(a.actionBusy || strings.TrimSpace(a.checkpointName) == "").Clicked() {
				create()
			}
		})
		if len(a.snapshot.WorkSnapshots) == 0 {
			return
		}
		height := float32(min(len(a.snapshot.WorkSnapshots), 3)) * 48
		list := a.keyboardList(c, &a.workSnapshotList, len(a.snapshot.WorkSnapshots)).Height(height)
		list.Rows(func(row ui.ListRow) {
			snapshot := a.snapshot.WorkSnapshots[row.Index]
			label := tr("Recovery")
			if snapshot.Kind == repository.CheckpointSnapshotKind {
				label = tr("Checkpoint")
			}
			ui.Row(row.Context).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Badge(row.Context, label)
				ui.Column(row.Context).Gap(1).Grow(1).Children(func() {
					ui.Text(row.Context, snapshot.Name).SingleLine().Tooltip(snapshot.Name)
					ui.Text(row.Context, time.Unix(snapshot.CreatedAt, 0).Format("2006-01-02 15:04")).
						TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 1)
				})
				if compactTextButton(ui.Button(row.Context, tr("Restore…"))).Label(tr("Restore") + " " + snapshot.ID).
					Tooltip(tr("Restore saved work while keeping this entry")).
					Disabled(a.actionBusy).Clicked() {
					a.confirmRestoreWorkSnapshot(snapshot)
				}
				rowIconButton(row.Context, "Delete "+snapshot.ID, "Delete this recovery entry…", revertIcon, func() {
					a.confirmDeleteWorkSnapshot(snapshot)
				})
			})
		})
	})
}

func (a *app) confirmRestoreWorkSnapshot(snapshot repository.WorkSnapshot) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Restore Saved Work",
		message:      trf("Restore %s?", snapshot.Name),
		detail:       "New changes to the same paths stop the restore before anything is overwritten. The recovery entry is kept.",
		confirmLabel: "Restore",
		run: func() {
			a.beginRepositoryActionMessage(trf("Restored %s", snapshot.Name), "", func(ctx context.Context, session repository.Session) error {
				return session.RestoreWorkSnapshot(ctx, snapshot)
			}, nil)
		},
	})
}

func (a *app) confirmDeleteWorkSnapshot(snapshot repository.WorkSnapshot) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Delete Saved Work",
		message:      trf("Delete %s?", snapshot.Name),
		detail:       "The entry will no longer be discoverable in TanGit. This cannot be undone from the app.",
		confirmLabel: "Delete",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Deleted %s", snapshot.Name), "", func(ctx context.Context, session repository.Session) error {
				return session.DeleteWorkSnapshot(ctx, snapshot)
			}, nil)
		},
	})
}
