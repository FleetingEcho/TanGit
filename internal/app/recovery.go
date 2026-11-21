package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// openActivityRecovery exposes the operation journal through its own status
// entry. Other recovery links keep the tab they selected.
func (a *app) openActivityRecovery() {
	a.savedWorkPanel = 1
	a.showRecoveryModal = true
	a.journalReveal = ""
	a.journalNotice = ""
}

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
			ui.Badge(c, fmt.Sprintf("%d", len(a.snapshot.WorkSnapshots)+len(a.snapshot.FileRecoveries)+len(a.snapshot.Operations)))
		})
		if a.savedWorkPanel == 1 {
			a.safetyJournalBody(c)
			return
		}
		ui.Text(c, tr("Conflict recovery restores the original unresolved file and Git stages. Discard copies and named checkpoints stay separate.")).
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
		a.fileRecoveryList(c)
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

func fileRecoveryStageSummary(record repository.FileRecoveryRecord) string {
	stages := make([]string, 0, 3)
	for stage, entry := range []repository.FileRecoveryIndexEntry{
		record.Initial.Index.Stage1, record.Initial.Index.Stage2, record.Initial.Index.Stage3,
	} {
		if entry.Present {
			stages = append(stages, fmt.Sprint(stage+1))
		}
	}
	if len(stages) == 0 {
		return tr("Saved file and ordinary index entry")
	}
	return trf("Original index stages %s", strings.Join(stages, "/"))
}

func fileRecoveryStatus(record repository.FileRecoveryRecord) (string, string) {
	switch record.Status {
	case repository.FileRecoveryRestored:
		return tr("Restored"), tr("Original unresolved state restored")
	case repository.FileRecoveryInterrupted:
		if record.Progress == repository.FileRecoveryWorktreeRestored {
			return tr("Interrupted · index pending"), tr("The file step finished; retry verifies and resumes the index step")
		}
		return tr("Interrupted"), tr("Retry verifies the after-state before resuming")
	default:
		return tr("Ready"), tr("Ready to restore")
	}
}

// fileRecoveryList renders V2 conflict records above the independent V1
// discard/checkpoint list. Every row comes from a durable manifest discovered
// again on repository open, including interrupted two-phase restores.
func (a *app) fileRecoveryList(c *ui.Context) {
	records := a.snapshot.FileRecoveries
	if len(records) == 0 {
		return
	}
	if a.fileRecoveryFocus != "" {
		ordered := make([]repository.FileRecoveryRecord, 0, len(records))
		for _, record := range records {
			if record.OperationID == a.fileRecoveryFocus {
				ordered = append(ordered, record)
			}
		}
		for _, record := range records {
			if record.OperationID != a.fileRecoveryFocus {
				ordered = append(ordered, record)
			}
		}
		records = ordered
	}
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(7).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Icon(c, conflictIcon).FontSize(14).TextColor(theme.Warning)
			ui.Text(c, tr("Conflict recovery")).Bold().Grow(1)
			ui.Badge(c, fmt.Sprintf("%d", len(records)))
		})
		ui.Text(c, tr("Restores saved marker bytes, deletions, executable modes, and exact unmerged index stages.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		height := float32(min(len(records), 3)) * 76
		list := a.keyboardList(c, &a.fileRecoveryRows, len(records)).Height(height)
		list.Rows(func(row ui.ListRow) {
			record := records[row.Index]
			status, statusTip := fileRecoveryStatus(record)
			statusColor := theme.Accent
			if record.Status == repository.FileRecoveryInterrupted {
				statusColor = theme.Danger
			} else if record.Status == repository.FileRecoveryRestored {
				statusColor = theme.Success
			}
			ui.Column(row.Context).FillWidth().Gap(3).Padding(5, 7).
				Border(1, theme.Border.Alpha(0.65)).Radius(theme.Radius).Children(func() {
				ui.Row(row.Context).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Icon(row.Context, softResetIcon).FontSize(14).TextColor(statusColor)
					ui.Text(row.Context, record.Path).SingleLine().Tooltip(record.Path).Grow(1)
					ui.Badge(row.Context, status).Background(statusColor.Alpha(0.14)).TextColor(statusColor).
						Tooltip(tr(statusTip))
				})
				ui.Row(row.Context).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Column(row.Context).Gap(1).Grow(1).Children(func() {
						ui.Text(row.Context, fileRecoveryStageSummary(record)).SingleLine().
							TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 1)
						ui.Text(row.Context, time.Unix(record.UpdatedAt, 0).Format("2006-01-02 15:04")).
							TextColor(secondaryRowColor(theme, row)).FontSize(theme.FontSize - 2)
					})
					if fileRecoveryCanRestore(record) {
						label := tr("Restore unresolved…")
						if record.Status == repository.FileRecoveryInterrupted {
							label = tr("Retry…")
						}
						button := compactTextButton(ui.Button(row.Context, label)).
							Label(label).Tooltip(tr("Restore this saved state; newer changes to the same file stop the compare-and-swap")).
							Disabled(a.actionBusy)
						if button.Clicked() {
							a.confirmRestoreFileRecovery(record)
						}
					}
				})
				if record.Error != "" && record.Status == repository.FileRecoveryInterrupted {
					ui.Text(row.Context, record.Error).SingleLine().Tooltip(record.Error).
						TextColor(theme.Danger).FontSize(theme.FontSize - 2)
				}
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
