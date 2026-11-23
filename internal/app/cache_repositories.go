package app

import (
	"os"
	"path/filepath"
)

// cacheRepositoryRow is one recent history entry and the search-index storage
// tied to its path. Bytes describes actual storage; Protected means another
// recent card or an open tab still owns every one of those bytes.
type cacheRepositoryRowKind uint8

const (
	cachePublishedRecent cacheRepositoryRowKind = iota
	cachePublishedUnlisted
	cacheIndexBuildTemporary
)

type cacheRepositoryRow struct {
	recent    recentRepository
	kind      cacheRepositoryRowKind
	indexKey  string
	root      string
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
	listedKeys := make(map[string]bool)
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
			listedKeys[key] = true
			size := codeSearchIndexDiskBytes(filepath.Join(o.cacheDir, codeSearchCacheDirName, key))
			row.bytes += size
			row.protected = row.protected || size > 0 && keep[key]
		}
		rows = append(rows, row)
	}
	if o.cacheDir == "" {
		return rows
	}

	// Published generations are tenant-owned directories keyed by the root
	// path. Include those no longer represented by a card, or whose checkout
	// path changed, instead of making global Settings report invisible bytes.
	root := filepath.Join(o.cacheDir, codeSearchCacheDirName)
	entries, _ := os.ReadDir(root)
	keep := o.keepSearchKeys()
	var temporary int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()
		directory := filepath.Join(root, key)
		if isCodeSearchIndexStagingName(key) {
			temporary += codeSearchIndexDiskBytes(directory)
			continue
		}
		if listedKeys[key] || !cacheHashHex(key, 16) {
			continue
		}
		size := codeSearchIndexDiskBytes(directory)
		if size == 0 {
			continue
		}
		row := cacheRepositoryRow{kind: cachePublishedUnlisted, indexKey: key, bytes: size, protected: keep[key]}
		if manifest, err := loadCodeSearchIndexManifest(directory); err == nil && filepath.IsAbs(manifest.Root) {
			row.root = filepath.Clean(manifest.Root)
		}
		rows = append(rows, row)
	}
	if temporary > 0 {
		// Older staging directories have no canonical repository metadata.
		// Do not invent an owner: they are resumed neither by a project nor by
		// a query, and are managed as one explicitly scoped category.
		rows = append(rows, cacheRepositoryRow{kind: cacheIndexBuildTemporary, bytes: temporary})
	}
	return rows
}

func (a *app) openRepositoryCaches() {
	a.cacheRepositories.open = true
	a.ensureRepositoryCaches()
}

func (a *app) refreshRepositoryCaches() {
	a.cacheRepositories.loaded = false
	a.ensureRepositoryCaches()
	a.refreshCacheUsage()
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

func cacheRepositoryRowName(row cacheRepositoryRow) string {
	switch row.kind {
	case cacheIndexBuildTemporary:
		return tr("Unfinished index builds")
	case cachePublishedUnlisted:
		if row.root != "" {
			return repoName(row.root)
		}
		return tr("Unlisted search index")
	default:
		return row.recent.displayName()
	}
}

func cacheRepositoryRowPath(row cacheRepositoryRow) string {
	switch row.kind {
	case cacheIndexBuildTemporary:
		return tr("Temporary data from index builds that did not complete")
	case cachePublishedUnlisted:
		if row.root != "" {
			return row.root
		}
		return trf("Cache key: %s", row.indexKey)
	default:
		return row.recent.Path
	}
}

// confirmRepositoryCache keeps cache scopes and recent-card choices separate.
// Clearing cache does not silently discard the recent card. Unfinished builds
// have no reliable repository owner, so they never reuse either recent action.
func (a *app) confirmRepositoryCache(row cacheRepositoryRow, forget bool) {
	if a.cacheCleanupBusy || a.confirm != nil || row.bytes == 0 {
		return
	}
	if row.kind == cacheIndexBuildTemporary {
		a.askConfirm(confirmRequest{
			title:        "Clear unfinished index builds",
			message:      "Stop index builds and clear their unfinished temporary data?",
			detail:       trf("Approximately %s will be reclaimed. Completed project indexes and recent entries are kept; indexing can run again on demand.", formatRemoteTransferBytes(row.bytes)),
			confirmLabel: "Stop builds and clear unfinished indexes",
			destructive:  true,
			run:          a.clearUnfinishedIndexes,
		})
		return
	}
	if row.kind == cachePublishedUnlisted {
		if row.protected {
			return
		}
		a.askConfirm(confirmRequest{
			title:        "Clear unlisted search index",
			message:      trf("Clear the complete search index for %s?", cacheRepositoryRowName(row)),
			detail:       cacheRepositoryRowPath(row) + "\n" + tr("Only this index directory is removed. Recent entries, repositories, editor recovery and Git safety journals are kept."),
			confirmLabel: "Clear this index",
			run:          func() { a.clearRepositoryCache(row, false) },
		})
		return
	}
	target := row.recent
	if target.Path == "" {
		return
	}
	request := confirmRequest{
		title:        "Clear repository cache",
		message:      trf("Clear search cache for %s and remove its Recent repositories entry?", target.displayName()),
		detail:       target.Path + "\n" + tr("The recent entry is removed. Only indexes that are not used by another recent entry or open tab are deleted. Working files, editor recovery and Git safety journals are kept."),
		confirmLabel: "Clear cache and remove from recent",
		destructive:  true,
		run:          func() { a.clearRepositoryCache(row, forget) },
	}
	if !forget {
		if row.protected {
			return
		}
		request.message = trf("Clear search cache for %s?", target.displayName())
		request.detail = target.Path + "\n" + trf("Approximately %s will be reclaimed. Working files, the recent entry, editor recovery and Git safety journals are kept; the index is rebuilt on demand.", formatRemoteTransferBytes(row.bytes))
		request.confirmLabel = "Clear cache"
		request.destructive = false
	}
	a.askConfirm(request)
}

func clearSearchIndexByKey(cacheDir, key string) int64 {
	if cacheDir == "" || !cacheHashHex(key, 16) {
		return 0
	}
	directory := filepath.Join(cacheDir, codeSearchCacheDirName, key)
	before := codeSearchIndexDiskBytes(directory)
	removeCodeSearchIndexDirectory(directory)
	return max(before-codeSearchIndexDiskBytes(directory), 0)
}

func (a *app) clearRepositoryCache(row cacheRepositoryRow, forget bool) {
	if row.kind == cacheIndexBuildTemporary {
		a.clearUnfinishedIndexes()
		return
	}
	target := row.recent
	if a.pathCache == "" || a.cacheCleanupBusy || row.kind == cachePublishedRecent && target.Path == "" {
		return
	}
	ownership := a.repositoryCacheOwnership()
	if row.kind == cachePublishedUnlisted && ownership.keepSearchKeys()[row.indexKey] {
		a.notify("info", "Index cache kept", "This index is still used by a recent entry or open tab.")
		return
	}
	a.cacheCleanupBusy = true
	a.cacheCleanupSerial++
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		freed := clearRepositorySearchIndex(ownership, target)
		if row.kind == cachePublishedUnlisted {
			freed = clearSearchIndexByKey(ownership.cacheDir, row.indexKey)
		}
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
			case row.kind == cachePublishedUnlisted:
				a.notify("success", "Unlisted search index cleared", trf("Freed %s. All recent entries and repositories were kept.", formatRemoteTransferBytes(freed)))
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

func clearUnfinishedIndexCaches(cacheDir string) (freed int64) {
	beginCodeSearchIndexMaintenance()
	withCodeSearchIndexMaintenance(func() {
		root := filepath.Join(cacheDir, codeSearchCacheDirName)
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !entry.IsDir() || !isCodeSearchIndexStagingName(entry.Name()) {
				continue
			}
			directory := filepath.Join(root, entry.Name())
			before := codeSearchIndexDiskBytes(directory)
			_ = os.RemoveAll(directory)
			freed += max(before-codeSearchIndexDiskBytes(directory), 0)
		}
	})
	return freed
}

func (a *app) clearUnfinishedIndexes() {
	if a.pathCache == "" || a.cacheCleanupBusy {
		return
	}
	a.cacheCleanupBusy = true
	a.cacheCleanupSerial++
	cacheDir := a.pathCache
	// Stop builders before staging is deleted, unlike an ordinary published
	// index removal whose per-directory lock is sufficient for its own files.
	a.invalidateSearchIndexBeforeCacheClear()
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		freed := clearUnfinishedIndexCaches(cacheDir)
		a.update(func() {
			a.cacheCleanupBusy = false
			a.invalidateCacheUsage()
			a.cacheRepositories.loaded = false
			a.notify("success", "Unfinished indexes cleared", trf("Freed %s. Completed indexes and recent entries were kept; indexing can run again on demand.", formatRemoteTransferBytes(freed)))
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
