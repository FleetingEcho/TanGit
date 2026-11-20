package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// repositoryCachesModal replaces the current Settings panel with its child
// manager, so only one primary modal owns input. Confirmation remains a
// separate child overlay in the established app-wide modal layer.
func (a *app) cacheRepositoriesModal(c *ui.Context) {
	a.ensureRepositoryCaches()
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(760).Height(600).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius+10).
				Shadow(0, 24, 64, 0, ui.RGBA(0, 0, 0, 0.3)).Clip().
				Role(ui.RoleDialog).Label(tr("Repository caches")).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
						ui.Box(c).Size(38, 38).Radius(theme.Radius + 4).Center().
							Background(theme.Accent.Alpha(0.12)).Children(func() {
							ui.Icon(c, searchIcon).FontSize(19).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, tr("Manage repository caches")).FontSize(21).Bold()
							ui.Text(c, tr("Free search-index space one project at a time")).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Refresh repository caches", "Measure current published and unfinished index sizes", refreshIcon,
							a.confirm != nil || a.cacheCleanupBusy || a.cacheRepositories.loading, a.refreshRepositoryCaches)
						iconButton(c, "Close repository cache manager", "Back to cache settings", closeIcon,
							a.confirm != nil, a.cacheRepositories.close)
					})
					ui.Text(c, tr("Clearing a search index keeps the repository and its recent entry. To forget a project too, use its separate Remove from recent action.")).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					switch {
					case a.cacheCleanupBusy:
						ui.Row(c).AlignItems(ui.Center).Gap(8).Grow(1).Justify(ui.Center).Children(func() {
							ui.Spinner(c).Label(tr("Clearing cache…"))
							ui.Text(c, tr("Clearing cache…")).TextColor(theme.TextMuted)
						})
					case a.cacheRepositories.loading || !a.cacheRepositories.loaded:
						ui.Row(c).AlignItems(ui.Center).Gap(8).Grow(1).Justify(ui.Center).Children(func() {
							ui.Spinner(c).Label(tr("Measuring repository caches…"))
							ui.Text(c, tr("Measuring repository caches…")).TextColor(theme.TextMuted)
						})
					case len(a.cacheRepositories.rows) == 0:
						ui.Column(c).Gap(6).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, folderIcon).FontSize(28).TextColor(theme.TextMuted)
							ui.Text(c, tr("No recent repositories or folders")).Bold()
							ui.Text(c, tr("Recent projects will appear here when you open them.")).TextColor(theme.TextMuted)
						})
					default:
						ui.Scroll(c).Grow(1).FillWidth().Label(tr("Recent repository caches")).Children(func() {
							ui.Column(c).FillWidth().Gap(8).Children(func() {
								previousKind := cachePublishedRecent
								for i, row := range a.cacheRepositories.rows {
									if i == 0 || row.kind != previousKind {
										label := tr("Recent projects")
										switch row.kind {
										case cachePublishedUnlisted:
											label = tr("Other published search indexes")
										case cacheIndexBuildTemporary:
											label = tr("Unfinished index builds")
										}
										ui.Text(c, label).Bold().TextColor(c.Theme().TextMuted).
											FontSize(c.Theme().FontSize - 1)
									}
									previousKind = row.kind
									a.cacheRepositoryRow(c, row)
								}
							})
						})
					}
					ui.Divider(c)
					ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
						ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
							ui.Text(c, tr("Shared author avatars")).Bold()
							ui.Text(c, tr("Avatars are shared across projects and are not part of a repository's search index.")).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						if a.cacheUsage.loaded {
							ui.Text(c, formatRemoteTransferBytes(a.cacheUsage.avatarBytes)).TextColor(theme.TextMuted)
						}
						iconTextButtonState(c, "Clear avatars", "Clear author-image downloads shared by every repository",
							fileImageIcon, a.confirm != nil || !a.cacheUsage.loaded,
							a.cacheCleanupBusy, a.confirmAvatarCaches)
					})
				})
			if a.confirm == nil && (panel.PressedOutside() || a.onOverlayShortcut(back, 0, ui.KeyEscape,
				shortcut("settings/repository-caches-close", "Escape", tr("Back to Cache")), nil)) {
				a.cacheRepositories.close()
			}
		})
	})
}

func (a *app) cacheRepositoryRow(c *ui.Context, row cacheRepositoryRow) {
	theme := c.Theme()
	project := row.recent
	c.Key(fmt.Sprintf("repository-cache:%d/%t:%s", row.kind, project.EditorOnly, row.indexKey+cacheRepositoryRowPath(row)))
	size := formatRemoteTransferBytes(row.bytes)
	if row.bytes == 0 && row.kind == cachePublishedRecent {
		size = tr("No cached index")
	}
	ui.Column(c).FillWidth().Padding(12).Gap(7).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius + 2).
		Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				icon := worktreesIcon
				switch {
				case row.kind == cacheIndexBuildTemporary:
					icon = refreshIcon
				case row.kind == cachePublishedUnlisted:
					icon = searchIcon
				case project.EditorOnly:
					icon = folderIcon
				case project.Worktree:
					icon = worktreesIcon
				}
				ui.Icon(c, icon).FontSize(16).TextColor(theme.Accent)
				ui.Text(c, cacheRepositoryRowName(row)).Bold().Grow(1).MinWidth(0).SingleLine().Tooltip(cacheRepositoryRowPath(row))
				if row.protected {
					ui.Text(c, tr("Shared or in use")).FontSize(theme.FontSize - 1).TextColor(theme.Warning)
				}
				ui.Text(c, size).Bold().TextColor(theme.Accent)
			})
			ui.Text(c, cacheRepositoryRowPath(row)).SingleLine().Grow(1).MinWidth(0).
				FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Tooltip(cacheRepositoryRowPath(row))
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				if row.kind == cachePublishedRecent && project.Worktree {
					ui.Text(c, tr("Worktree")).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
				}
				if row.kind == cachePublishedRecent && project.EditorOnly {
					ui.Text(c, tr("Editor folder")).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
				}
				ui.Box(c).Grow(1)
				label, action := tr("Clear cache"), tr("Delete this project's search index, keeping its recent entry")
				if row.kind == cachePublishedUnlisted {
					label, action = tr("Clear this index"), tr("Delete this unlisted search index without changing recent entries")
				} else if row.kind == cacheIndexBuildTemporary {
					label, action = tr("Clear unfinished indexes"), tr("Stop index builds and delete only unfinished temporary indexes")
				}
				clear := ui.Button(c, label).
					Disabled(a.cacheCleanupBusy || a.confirm != nil || row.bytes == 0 || row.protected).
					Tooltip(action).
					Label(trf("Clear cache for %s", cacheRepositoryRowName(row)))
				if clear.Clicked() {
					a.confirmRepositoryCache(row, false)
				}
				if row.kind != cachePublishedRecent {
					return
				}
				remove := ui.Button(c, tr("Remove from recent")).
					Disabled(a.cacheCleanupBusy || a.confirm != nil).
					Tooltip(tr("Clear this project's available search cache and remove its recent entry")).
					Label(trf("Clear cache and remove %s from recent", project.displayName()))
				if remove.Clicked() {
					a.confirmRepositoryCache(row, true)
				}
			})
		})
}
