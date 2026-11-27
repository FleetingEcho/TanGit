package app

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"time"

	"github.com/egoist/mygo"
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
			detail.folders, detail.foldersOthers = overviewFolderSummary(files)
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

func (a *app) overviewCommitPreview(c *ui.Context, o *overviewState, detail *overviewCommitDetail, made time.Time) {
	theme := c.Theme()
	row := detail.row
	byline := made.Format("2006-01-02 15:04")
	if author := authorLabel(row.Commit.AuthorName, row.Commit.AuthorEmail); author != "" {
		byline += " · " + author
	}
	ui.Column(c).FillWidth().Padding(12, 16).Gap(6).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Text(c, row.Commit.Subject).Bold()
		services, hash, author := c.Services(), row.Commit.Hash, row.Commit.AuthorName
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, byline).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine()
			if author != "" {
				flatIconButton(c, "Show this author's commits", trf("Show only commits by %s", author), filterIcon, 13, 3, false, func() {
					o.query = overviewSetAuthor(o.query, author)
					a.overviewSearchChanged(o)
				})
			}
		})
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, hash).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine()
			flatIconButton(c, "Copy commit hash", "Copy the full commit hash", copyIcon, 13, 3, false,
				func() { a.notifyCopied(services, "commit hash", hash) })
		})
		if detail.loading {
			ui.Text(c, tr("Loading commit details…")).TextColor(theme.TextMuted)
		}
		if detail.err != "" {
			ui.Text(c, detail.err).TextColor(theme.Danger)
		}
		if detail.body != "" {
			// The whole description is readable; a long one scrolls in its own
			// bounded box instead of stretching the row.
			ui.Scroll(c.Key("overview-detail-body")).FillWidth().MaxHeight(160).
				Label(tr("Commit description")).Children(func() {
				ui.Text(c, detail.body).FillWidth().MinWidth(0).
					Padding(0, scrollbarGutter(theme)+4, 0, 0)
			})
		}
		if len(detail.files) > 0 {
			overviewPreviewFiles(c, detail)
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Padding(2, 0, 0).Children(func() {
			quickAction := func(label, tooltip string, run func()) {
				ui.ButtonBase(c).Padding(4, 8).Radius(theme.Radius).Label(label).Tooltip(tooltip).
					Background(theme.Background).Border(1, theme.Border).OnClick(run).
					Children(func() { ui.Text(c, label).FontSize(theme.FontSize - 1) })
			}
			if row.MR != "" {
				quickAction(tr("Same MR"), trf("Search every followed repository for %s", row.MR), func() {
					o.query = row.MR
					a.overviewSearchChanged(o)
				})
			}
			flatIconButton(c, "Download patch", "Save this commit as a .patch file…", downloadIcon, 16, 5, false,
				func() { a.downloadOverviewPatch(row) })
			if row.Issue != "" {
				quickAction(tr("Same issue"), trf("Search every followed repository for %s", row.Issue), func() {
					o.query = row.Issue
					a.overviewSearchChanged(o)
				})
			}
			ui.Box(c).Grow(1)
			if a.openOverviewCommitTab != nil {
				flatIconButton(c, "Open commit details", "Open this commit's full details in a tab", externalLinkIcon, 16, 5, false,
					func() { a.openOverviewCommitTab(row.Repo, row.Commit) })
			}
		})
	})
}

func overviewPreviewFiles(c *ui.Context, detail *overviewCommitDetail) {
	theme := c.Theme()
	files := detail.files
	const rowHeight = 28
	const visibleRows = 5
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, tr("Changed files")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, strconvCount(len(files))).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
		})
		if len(detail.folders) > 0 {
			ui.Row(c).FillWidth().Wrap().Gap(6).Children(func() {
				for _, folder := range detail.folders {
					c.Key("overview-folder:" + folder.Dir)
					ui.Row(c).Padding(1, 6).Radius(theme.Radius).Background(theme.Surface).Border(1, theme.Border).
						Tooltip(folder.Dir).Children(func() {
						ui.Text(c, overviewCount(path.Base(folder.Dir), folder.Files)).FontSize(theme.FontSize - 2).
							TextColor(theme.TextMuted).SingleLine().MaxWidth(220)
					})
				}
				if detail.foldersOthers > 0 {
					ui.Text(c, trf("+%d more files", detail.foldersOthers)).FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
				}
			})
		}
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

type overviewPatchSession interface {
	CommitPatch(context.Context, repository.Commit) (string, error)
}

// downloadOverviewPatch asks where to save first, so cancelling costs nothing,
// then has Git format the commit and writes it. Both steps run off the UI
// goroutine: a huge commit can take a while and must not freeze the page.
func (a *app) downloadOverviewPatch(row overviewCommit) {
	opener := a.opener
	if opener == nil {
		return
	}
	name := sanitizeOverviewFileName(row.Commit.ShortHash + "-" + row.Commit.Subject)
	opts := mygo.SaveDialogOptions{
		Parent:      a.window,
		Title:       tr("Save patch"),
		DefaultPath: name + ".patch",
		ButtonLabel: tr("Save"),
		Filters:     []mygo.FileFilter{{Name: tr("Patch files"), Extensions: []string{"patch", "diff"}}},
	}
	go func() {
		defer a.recoverBackground()
		target, err := chooseSavePath(opts)
		if err != nil {
			a.update(func() { a.notify("error", "Could not save the patch", safeError(err)) })
			return
		}
		if target == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		session, err := opener.Open(ctx, row.Repo)
		var patch string
		if err == nil {
			defer closeRepositorySession(session)
			reader, ok := session.(overviewPatchSession)
			if !ok {
				err = errors.New("this repository cannot export patches")
			} else {
				patch, err = reader.CommitPatch(ctx, row.Commit)
			}
		}
		if err == nil {
			err = os.WriteFile(target, []byte(patch), 0o644)
		}
		a.update(func() {
			if err != nil {
				a.notify("error", "Could not save the patch", safeError(err))
				return
			}
			a.notify("success", "Saved the patch", target)
		})
	}()
}

// sanitizeOverviewFileName keeps a commit subject usable as a file name.
func sanitizeOverviewFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' || r < 32:
			return '-'
		case r == ' ':
			return '-'
		}
		return r
	}, name)
	if runes := []rune(name); len(runes) > 60 {
		name = string(runes[:60])
	}
	return strings.Trim(name, "-.")
}

// exportOverview saves the commits loaded for the current search or filter.
// Only loaded rows are written: the export never reads more history.
func (a *app) exportOverview(o *overviewState) {
	rows := append([]overviewCommit(nil), o.visibleCommits()...)
	if len(rows) == 0 {
		return
	}
	names := make(map[string]string, len(o.followed()))
	for _, repo := range o.followed() {
		names[repo.Path] = repo.displayName()
	}
	opts := mygo.SaveDialogOptions{
		Parent:      a.window,
		Title:       tr("Export commits"),
		DefaultPath: "overview.md",
		ButtonLabel: tr("Save"),
		Filters: []mygo.FileFilter{
			{Name: "Markdown", Extensions: []string{"md"}},
			{Name: "CSV", Extensions: []string{"csv"}},
		},
	}
	go func() {
		defer a.recoverBackground()
		target, err := chooseSavePath(opts)
		if err == nil && target != "" {
			err = os.WriteFile(target, overviewExport(target, rows, names), 0o644)
		}
		a.update(func() {
			switch {
			case err != nil:
				a.notify("error", "Could not export the commits", safeError(err))
			case target != "":
				a.notify("success", trf("Exported %d commits", len(rows)), target)
			}
		})
	}()
}
