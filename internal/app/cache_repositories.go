package app

import (
	"os"
	"path/filepath"
)

// cacheRepositoryRow is one recent history entry and the search-index storage
// tied to its path. Bytes describes actual storage; Protected means another
// recent card or an open tab still owns every one of those bytes.
type cacheRepositoryRow struct {
	recent    recentRepository
	bytes     int64
	protected bool
}

// cacheRepositoriesState keeps directory scans and modal controls away from
// MyGo's UI thread. serial rejects a result after the dialog has been closed.
type cacheRepositoriesState struct {
	open     bool
	loading  bool
	loaded   bool
	serial   uint64
	revision uint64
	rows     []cacheRepositoryRow
}

func (s *cacheRepositoriesState) close() {
	s.open = false
	s.loading = false
	s.loaded = false
	s.serial++
	s.rows = nil
}

// excludeRecent affects only a cleanup calculation. The actual recent history
// is removed separately after a confirmed operation has completed.
func (o repositoryCacheOwnership) excludeRecent(target recentRepository) repositoryCacheOwnership {
	recents := o.history()
	kept := make([]recentRepository, 0, len(recents))
	for _, candidate := range recents {
		if candidate.Path == target.Path && candidate.EditorOnly == target.EditorOnly {
			continue
		}
		kept = append(kept, candidate)
	}
	o.recents = kept
	return o
}

// clearRepositorySearchIndex removes only this repository's unowned search
// indexes. In particular, editor recovery is never part of this measurement
// or deletion. A card and an Editor folder are independent recent identities;
// a symlink alias, sibling card or open tab still protects their shared index.
func clearRepositorySearchIndex(o repositoryCacheOwnership, target recentRepository) int64 {
	if o.cacheDir == "" {
		return 0
	}
	keep := o.excludeRecent(target).keepSearchKeys()
	var freed int64
	for _, key := range codeSearchIndexKeysFor(target.Path) {
		if keep[key] {
			continue
		}
		directory := filepath.Join(o.cacheDir, codeSearchCacheDirName, key)
		before := codeSearchIndexDiskBytes(directory)
		if before == 0 {
			continue
		}
		removeCodeSearchIndexDirectory(directory)
		freed += max(before-codeSearchIndexDiskBytes(directory), 0)
	}
	return freed
}

func measureRepositoryCaches(o repositoryCacheOwnership) []cacheRepositoryRow {
	repositories := o.history()
	rows := make([]cacheRepositoryRow, 0, len(repositories))
	for _, recent := range repositories {
		if recent.Path == "" {
			continue
		}
		row := cacheRepositoryRow{recent: recent}
		if o.cacheDir == "" {
			rows = append(rows, row)
			continue
		}
		keep := o.excludeRecent(recent).keepSearchKeys()
		for _, key := range codeSearchIndexKeysFor(recent.Path) {
			size := codeSearchIndexDiskBytes(filepath.Join(o.cacheDir, codeSearchCacheDirName, key))
			row.bytes += size
			row.protected = row.protected || size > 0 && keep[key]
		}
		rows = append(rows, row)
	}
	return rows
}

func (a *app) openRepositoryCaches() {
	a.cacheRepositories.open = true
	a.ensureRepositoryCaches()
}

func (a *app) ensureRepositoryCaches() {
	s := &a.cacheRepositories
	if !s.open || a.cacheCleanupBusy || s.loading || s.loaded && s.revision == a.historyRevision {
		return
	}
	s.loading = true
	s.loaded = false
	s.serial++
	serial, revision := s.serial, a.historyRevision
	ownership := a.repositoryCacheOwnership()
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		rows := measureRepositoryCaches(ownership)
		a.updateChanged(func() bool {
			if serial != s.serial || !s.open {
				return false
			}
			s.rows = rows
			s.revision = revision
			s.loading = false
			s.loaded = true
			return a.settingsUI.open && a.settingsUI.page == settingsPageCache
		})
	})
}

// confirmRepositoryCache keeps the cache and recent-card choices separate.
// Clearing cache does not silently discard the card; forgetting a card does
// not imply anything about editor recovery or the repository on disk.
func (a *app) confirmRepositoryCache(row cacheRepositoryRow, forget bool) {
	if a.cacheCleanupBusy || row.recent.Path == "" {
		return
	}
	target := row.recent
	request := confirmRequest{
		title:        "Clear repository cache",
		message:      trf("Clear search cache for %s and remove its Recent repositories entry?", target.displayName()),
		detail:       target.Path + "\n" + tr("The recent entry is removed. Only indexes that are not used by another recent entry or open tab are deleted. Working files, editor recovery and Git safety journals are kept."),
		confirmLabel: "Clear cache and remove from recent",
		destructive:  true,
		run:          func() { a.clearRepositoryCache(target, forget) },
	}
	if !forget {
		if row.bytes == 0 || row.protected {
			return
		}
		request.message = trf("Clear search cache for %s?", target.displayName())
		request.detail = target.Path + "\n" + trf("Approximately %s will be reclaimed. Working files, the recent entry, editor recovery and Git safety journals are kept; the index is rebuilt on demand.", formatRemoteTransferBytes(row.bytes))
		request.confirmLabel = "Clear cache"
		request.destructive = false
	}
	a.askConfirm(request)
}

func (a *app) clearRepositoryCache(target recentRepository, forget bool) {
	if a.pathCache == "" || a.cacheCleanupBusy || target.Path == "" {
		return
	}
	a.cacheCleanupBusy = true
	a.cacheCleanupSerial++
	ownership := a.repositoryCacheOwnership()
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		freed := clearRepositorySearchIndex(ownership, target)
		a.update(func() {
			a.cacheCleanupBusy = false
			if forget {
				a.forgetRecentKeepingRecovery(target.Path, target.EditorOnly)
			}
			if freed > 0 && a.onCachesCleared != nil {
				a.onCachesCleared()
			}
			a.invalidateCacheUsage()
			a.cacheRepositories.loaded = false
			switch {
			case forget && freed > 0:
				a.notify("success", "Repository cache cleared and recent entry removed", trf("Freed %s for %s. Other project data was kept.", formatRemoteTransferBytes(freed), target.displayName()))
			case forget:
				a.notify("success", "Recent entry removed", trf("The cache for %s was kept because it is shared, in use, or already empty.", target.displayName()))
			default:
				a.notify("success", "Repository cache cleared", trf("Freed %s for %s. Its recent entry and repository were kept.", formatRemoteTransferBytes(freed), target.displayName()))
			}
		})
	})
}

// clearAvatarCaches is deliberately not a repository action: its entries are
// shared by author identity across all projects.
func clearAvatarCaches(cacheDir string) int64 {
	if cacheDir == "" {
		return 0
	}
	directory := filepath.Join(cacheDir, avatarCacheDirName)
	before := codeSearchIndexDiskBytes(directory)
	_ = os.RemoveAll(directory)
	return max(before-codeSearchIndexDiskBytes(directory), 0)
}

func (a *app) confirmAvatarCaches() {
	if a.pathCache == "" || a.cacheCleanupBusy {
		return
	}
	cacheDir := a.pathCache
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		size := codeSearchIndexDiskBytes(filepath.Join(cacheDir, avatarCacheDirName))
		a.update(func() {
			if a.cacheCleanupBusy {
				return
			}
			if size == 0 {
				a.notify("info", "Cached avatars are already empty", "")
				return
			}
			a.askConfirm(confirmRequest{
				title:        "Clear cached avatars",
				message:      "Clear author avatars shared by all repositories?",
				detail:       trf("Approximately %s will be reclaimed. No repository index or recent entry is affected.", formatRemoteTransferBytes(size)),
				confirmLabel: "Clear avatars",
				run:          a.clearCachedAvatars,
			})
		})
	})
}

func (a *app) clearCachedAvatars() {
	if a.pathCache == "" || a.cacheCleanupBusy {
		return
	}
	a.cacheCleanupBusy = true
	a.cacheCleanupSerial++
	cacheDir := a.pathCache
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		freed := clearAvatarCaches(cacheDir)
		a.update(func() {
			a.cacheCleanupBusy = false
			a.invalidateCacheUsage()
			a.notify("success", "Cached avatars cleared", trf("Freed %s. Repository indexes and recent entries were kept.", formatRemoteTransferBytes(freed)))
		})
	})
}
