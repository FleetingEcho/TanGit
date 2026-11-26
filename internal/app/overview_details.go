package app

import (
	"context"
	"path"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func (a *app) toggleOverviewCommit(row overviewCommit) {
	o := a.overview
	if previous := o.detail; previous != nil {
		if previous.cancel != nil {
			previous.cancel()
		}
		o.detail = nil
		if previous.row.Repo == row.Repo && previous.row.Commit.Hash == row.Commit.Hash {
			return
		}
	}
	detail := &overviewCommitDetail{row: row}
	o.detail = detail
	if o.ctx == nil || a.opener == nil {
		return
	}
	ctx, cancel := context.WithTimeout(o.ctx, 15*time.Second)
	detail.cancel, detail.loading = cancel, true
	opener := a.opener
	go func() {
		defer a.recoverBackground()
		defer cancel()
		body, files, err := readOverviewCommitDetail(ctx, opener, row)
		a.update(func() {
			if a.overview != o || o.detail != detail {
				return
			}
			detail.loading, detail.cancel = false, nil
			detail.body, detail.files = body, files
			if err != nil {
				detail.err = safeError(err)
			}
		})
	}()
}

type overviewCommitDetailSession interface {
	CommitMessage(context.Context, repository.Commit) (repository.CommitMessage, error)
	CommitFilesWithoutStats(context.Context, repository.Commit) ([]repository.CommitFile, error)
}

func readOverviewCommitDetail(ctx context.Context, opener repositoryOpener, row overviewCommit) (string, []repository.CommitFile, error) {
	session, err := opener.Open(ctx, row.Repo)
	if err != nil {
		return "", nil, err
	}
	defer closeRepositorySession(session)
	reader, ok := session.(overviewCommitDetailSession)
	if !ok {
		return "", nil, nil
	}
	message, messageErr := reader.CommitMessage(ctx, row.Commit)
	files, err := reader.CommitFilesWithoutStats(ctx, row.Commit)
	if err == nil {
		err = messageErr
	}
	return message.Description, files, err
}

func (a *app) overviewCommitPreview(c *ui.Context, detail *overviewCommitDetail, made time.Time) {
	theme := c.Theme()
	row := detail.row
	byline := made.Format("2006-01-02 15:04")
	if author := authorLabel(row.Commit.AuthorName, row.Commit.AuthorEmail); author != "" {
		byline += " · " + author
	}
	ui.Column(c).FillWidth().Padding(12, 16).Gap(6).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Text(c, row.Commit.Subject).Bold()
		ui.Text(c, byline).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine()
		ui.Text(c, row.Commit.Hash).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine()
		if detail.loading {
			ui.Text(c, tr("Loading commit details…")).TextColor(theme.TextMuted)
		}
		if detail.err != "" {
			ui.Text(c, detail.err).TextColor(theme.Danger)
		}
		if detail.body != "" {
			ui.Text(c, detail.body).FillWidth().MaxLines(8)
		}
		if len(detail.files) > 0 {
			overviewPreviewFiles(c, detail.files)
		}
		if a.openOverviewCommitTab != nil {
			ui.Row(c).FillWidth().Justify(ui.End).Padding(2, 0, 0).Children(func() {
				ui.ButtonBase(c).Padding(4, 6).Label(tr("Details")).
					OnClick(func() { a.openOverviewCommitTab(row.Repo, row.Commit) }).
					Children(func() { ui.Text(c, tr("Details")).TextColor(theme.Accent).Underline() })
			})
		}
	})
}

func overviewPreviewFiles(c *ui.Context, files []repository.CommitFile) {
	theme := c.Theme()
	const rowHeight = 28
	const visibleRows = 5
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, tr("Changed files")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, strconvCount(len(files))).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
		})
		ui.Scroll(c).FillWidth().Height(float32(min(len(files), visibleRows)*rowHeight+2)).
			Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius).
			KeepScrollbars(true).Label(tr("Changed files")).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for i, file := range files {
					directory, name := path.Split(file.Path)
					tip := file.Path
					if file.PreviousPath != "" {
						tip = file.PreviousPath + " → " + file.Path
					}
					row := ui.Row(c).FillWidth().Height(rowHeight).Shrink(0).AlignItems(ui.Center).
						Padding(0, 8+scrollbarGutter(theme), 0, 8).Gap(8).Tooltip(tip)
					if i%2 != 0 {
						row.Background(theme.Surface)
					}
					row.Children(func() {
						tone := statusColor(theme, repository.File{
							Added: file.Status == "A", Deleted: file.Status == "D",
						})
						ui.Box(c).Size(22, 18).Shrink(0).Center().Radius(theme.Radius).
							Background(tone.Alpha(0.13)).Children(func() {
							ui.Text(c, file.Status).TextColor(tone).FontSize(theme.FontSize - 2).SingleLine()
						})
						fileTypeMark(c, file.Path, 14, false)
						ui.Text(c, name).FontSize(theme.FontSize - 1).SingleLine().MinWidth(0)
						ui.Text(c, directory).FontSize(theme.FontSize - 2).SingleLine().
							TextColor(theme.TextMuted).Grow(1).MinWidth(0)
					})
				}
			})
		})
	})
}

func (a *app) openOverviewCommitInHistory(commit repository.Commit) {
	a.closeCommitFilter()
	if a.fileHistoryOpen {
		a.closeFileHistory()
	}
	a.closeDiffFullscreen()
	a.cancelCommitSelection()
	query := &a.historyFilter
	query.reset()
	query.applied = repository.CommitFilter{Query: commit.Hash}
	query.draft = query.applied
	query.limit = repository.SnapshotCommitLimit
	query.matches = []repository.Commit{commit}
	a.setWorkspaceTab(sectionHistory)
	a.commitList.Choose(0)
	a.commitList.ScrollTo(0, ui.Start)
}
