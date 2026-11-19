package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Repository-bound caches live outside every working tree, keyed by a digest
// of the repository path: Zoekt shards under code-search-zoekt/<32 hex> and
// editor recovery under editor-recovery/<64 hex>.json. Forgetting a card or
// letting the history cap evict one therefore has to remove those files
// explicitly; otherwise every opened repository caches forever.
//
// Deletion is deliberately conservative:
//   - Anything still referenced by a history card or by an open tab stays.
//     A main worktree, its linked worktrees and its submodules hash
//     independently, so removing one card never touches the others' data.
//   - Only directories and files whose names are exactly the cache's own hash
//     keys are touched. Foreign content in the cache root is never removed.
//   - The Git safety journal is not here at all: it lives inside the
//     repository's own .git directory and is never garbage collected by the
//     app, because losing it would destroy the user's undo history.
//   - Startup GC additionally ages orphans out instead of deleting them on
//     sight, so two app instances with different histories cannot race one
//     another into deleting actively used shard files.
const (
	codeSearchCacheDirName = "code-search-zoekt"
	editorRecoveryDirName  = "editor-recovery"

	// codeSearchOrphanAge keeps orphaned indexes around for a while. Reopening
	// a repository whose card has just aged out of the history should not
	// force a full reindex on the next start.
	codeSearchOrphanAge = 30 * 24 * time.Hour
	// editorRecoveryOrphanAge is much longer than the index window: recovery
	// files hold unsaved edits, which are cheap to keep but unrecoverable
	// once deleted.
	editorRecoveryOrphanAge = 180 * 24 * time.Hour
	// codeSearchCacheBytesLimit bounds the total shard storage. Only data not
	// referenced by history or open tabs is reclaimed, oldest first.
	codeSearchCacheBytesLimit = 4 << 30 // 4 GiB
)

// cacheCleanupAsync runs repository cache reclamation off the UI thread.
// Tests replace it with a synchronous runner to assert the effects.
var cacheCleanupAsync = func(fn func()) { go fn() }

// repositoryCacheOwnership is a UI-thread snapshot of everything cache
// reclamation must preserve: the history cards still present, and the
// repositories any tab currently has open. recents == nil asks startup GC to
// read the stored history itself, off the UI thread.
type repositoryCacheOwnership struct {
	cacheDir string
	dataDir  string
	recents  []recentRepository
	roots    []string
}

// repositoryCacheOwnership captures what this page must preserve. The shell
// extends roots with every other tab's open repository through cacheOpenRoots.
func (a *app) repositoryCacheOwnership() repositoryCacheOwnership {
	o := repositoryCacheOwnership{
		cacheDir: a.pathCache,
		dataDir:  a.pathUserData,
		recents:  append([]recentRepository(nil), a.recentRepositories...),
	}
	if a.snapshot.Root != "" {
		o.roots = append(o.roots, a.snapshot.Root)
	}
	if a.cacheOpenRoots != nil {
		o.roots = append(o.roots, a.cacheOpenRoots()...)
	}
	return o
}

// scheduleRepositoryCacheCleanup reclaims the caches owned by paths that are
// no longer referenced. It returns immediately; filesystem work happens in
// the background and is strictly best-effort.
func (a *app) scheduleRepositoryCacheCleanup(paths ...string) {
	if len(paths) == 0 || (a.pathCache == "" && a.pathUserData == "") {
		return
	}
	ownership := a.repositoryCacheOwnership()
	removed := append([]string(nil), paths...)
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		cleanupForgottenRepositories(ownership, removed)
	})
}

// cleanupEvictedRecents reclaims caches for cards the history cap dropped.
// evicted recents still holding a path stay protected through ownership.
func (a *app) cleanupEvictedRecents(before []recentRepository) {
	stillPresent := make(map[recentHistoryKey]struct{}, len(a.recentRepositories))
	for _, repo := range a.recentRepositories {
		stillPresent[recentKey(repo.Path, repo.EditorOnly)] = struct{}{}
	}
	var evicted []string
	for _, repo := range before {
		if _, ok := stillPresent[recentKey(repo.Path, repo.EditorOnly)]; !ok {
			evicted = append(evicted, repo.Path)
		}
	}
	a.scheduleRepositoryCacheCleanup(evicted...)
}

// cacheHashHex reports whether name is exactly the lowercase hexadecimal form
// of a size-byte digest. It keeps the cleanup away from anything the app did
// not name itself, including staging directories and operator files.
func cacheHashHex(name string, size int) bool {
	if len(name) != size*2 {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// codeSearchIndexKeysFor returns possible shard directory names for path.
// Indexed repositories resolved through EvalSymlinks; a repository deleted
// from disk can no longer do that, so the absolute form is included too.
func codeSearchIndexKeysFor(path string) []string {
	cleaned := cleanRepoPath(path)
	if cleaned == "" {
		return nil
	}
	seen := map[string]bool{}
	keys := make([]string, 0, 2)
	add := func(root string) {
		if root == "" || seen[root] {
			return
		}
		seen[root] = true
		digest := sha256.Sum256([]byte(root))
		keys = append(keys, hex.EncodeToString(digest[:16]))
	}
	if canonical, err := resolvedCodeSearchIndexRoot(cleaned); err == nil {
		add(canonical)
	}
	if absolute, err := filepath.Abs(cleaned); err == nil {
		add(absolute)
	}
	return keys
}

// editorRecoveryKeysFor returns the recovery file name (without extension)
// for path. Recovery hashes filepath.Clean(root), not the resolved path.
func editorRecoveryKeysFor(path string) []string {
	cleaned := cleanRepoPath(path)
	if cleaned == "" {
		return nil
	}
	digest := sha256.Sum256([]byte(filepath.Clean(cleaned)))
	return []string{hex.EncodeToString(digest[:])}
}

func (o repositoryCacheOwnership) history() []recentRepository {
	if o.recents != nil {
		return o.recents
	}
	return loadRecentRepositories(o.dataDir)
}

func (o repositoryCacheOwnership) keepSearchKeys() map[string]bool {
	keep := make(map[string]bool)
	for _, repo := range o.history() {
		for _, key := range codeSearchIndexKeysFor(repo.Path) {
			keep[key] = true
		}
	}
	for _, root := range o.roots {
		for _, key := range codeSearchIndexKeysFor(root) {
			keep[key] = true
		}
	}
	return keep
}

func (o repositoryCacheOwnership) keepRecoveryKeys() map[string]bool {
	keep := make(map[string]bool)
	for _, repo := range o.history() {
		for _, key := range editorRecoveryKeysFor(repo.Path) {
			keep[key] = true
		}
	}
	for _, root := range o.roots {
		for _, key := range editorRecoveryKeysFor(root) {
			keep[key] = true
		}
	}
	return keep
}

// removeCodeSearchIndexDirectory removes one shard directory while holding
// the same per-index lock builders use, so a replacement generation is never
// deleted mid-publish. Active searchers map the shard files: on the indexing
// platforms (Unix), unlinking under an open mapping is safe; on other systems
// a failed removal is simply left for the next pass.
func removeCodeSearchIndexDirectory(directory string) {
	if directory == "" {
		return
	}
	unlock := lockCodeSearchIndex(directory)
	defer unlock()
	_ = os.RemoveAll(directory)
}

func removeEditorRecoveryFile(dataDir, key string) {
	if dataDir == "" || !cacheHashHex(key, sha256.Size) {
		return
	}
	_ = os.Remove(filepath.Join(dataDir, editorRecoveryDirName, key+".json"))
}

// cleanupForgottenRepositories removes caches whose only owner was one of the
// removed paths. A repository still kept by another card, a symlink alias, or
// an open tab keeps its cache.
func cleanupForgottenRepositories(o repositoryCacheOwnership, removed []string) {
	if len(removed) == 0 {
		return
	}
	keepSearch := o.keepSearchKeys()
	keepRecovery := o.keepRecoveryKeys()
	for _, path := range removed {
		if o.cacheDir != "" {
			for _, key := range codeSearchIndexKeysFor(path) {
				if keepSearch[key] {
					continue
				}
				removeCodeSearchIndexDirectory(filepath.Join(o.cacheDir, codeSearchCacheDirName, key))
			}
		}
		if o.dataDir != "" {
			for _, key := range editorRecoveryKeysFor(path) {
				if !keepRecovery[key] {
					removeEditorRecoveryFile(o.dataDir, key)
				}
			}
		}
	}
}

// gcRepositoryCaches reclaims orphaned caches at startup and bounds the total
// shard storage. History and open-tab data is never deleted; orphaned indexes
// must additionally sit unmodified for codeSearchOrphanAge, and orphaned
// recovery files for editorRecoveryOrphanAge.
func gcRepositoryCaches(o repositoryCacheOwnership, now time.Time) {
	if o.cacheDir != "" {
		gcCodeSearchCache(o, now)
	}
	if o.dataDir != "" {
		gcEditorRecovery(o, now)
	}
}

func gcCodeSearchCache(o repositoryCacheOwnership, now time.Time) {
	keep := o.keepSearchKeys()
	root := filepath.Join(o.cacheDir, codeSearchCacheDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type shard struct {
		name    string
		size    int64
		modTime time.Time
	}
	var orphans []shard
	var total int64
	for _, entry := range entries {
		if !entry.IsDir() || !cacheHashHex(entry.Name(), 16) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		size := codeSearchIndexDiskBytes(directory)
		total += size
		if !keep[entry.Name()] {
			orphans = append(orphans, shard{name: entry.Name(), size: size, modTime: info.ModTime()})
		}
	}
	reclaim := func(s shard) {
		removeCodeSearchIndexDirectory(filepath.Join(root, s.name))
		total -= s.size
	}
	remaining := orphans[:0]
	for _, s := range orphans {
		if now.Sub(s.modTime) >= codeSearchOrphanAge {
			reclaim(s)
			continue
		}
		remaining = append(remaining, s)
	}
	orphans = remaining
	if total <= codeSearchCacheBytesLimit {
		return
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].modTime.Before(orphans[j].modTime) })
	for _, s := range orphans {
		if total <= codeSearchCacheBytesLimit {
			break
		}
		reclaim(s)
	}
}

func gcEditorRecovery(o repositoryCacheOwnership, now time.Time) {
	keep := o.keepRecoveryKeys()
	root := filepath.Join(o.dataDir, editorRecoveryDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		key := strings.TrimSuffix(name, ".json")
		if key == name || !cacheHashHex(key, sha256.Size) || keep[key] {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < editorRecoveryOrphanAge {
			continue
		}
		removeEditorRecoveryFile(o.dataDir, key)
	}
}

// avatarCacheDirName is the other application-owned, fully regenerable cache.
// Editor recovery is deliberately not listed: it contains unsaved user work.
const avatarCacheDirName = "avatars"

// cacheUsageState keeps the Settings cache page's disk reads off rendering.
// requested prevents an unseen page from starting another scan after cleanup;
// serial discards measurements superseded by a new one or by a cache clear.
type cacheUsageState struct {
	requested bool
	loaded    bool
	loading   bool
	serial    uint64
	// notifyOnCompletion distinguishes a manual Refresh from the page's
	// initial/background scans; only the former acknowledges with a toast.
	notifyOnCompletion bool
	indexBytes         int64
	avatarBytes        int64
}

func (s cacheUsageState) totalBytes() int64 { return s.indexBytes + s.avatarBytes }

// ensureCacheUsage starts at most one background measurement. Every Settings
// frame may call it; only the cache page sets requested and causes disk work.
func (a *app) ensureCacheUsage() {
	a.cacheUsage.requested = true
	if a.pathCache == "" || a.cacheCleanupBusy || a.cacheUsage.loaded || a.cacheUsage.loading {
		return
	}
	a.measureCacheUsage(false)
}

// measureCacheUsage scans sizes off the UI thread. It never clears loaded
// values: a manual refresh keeps the old digits on screen until the new ones
// arrive, rather than replacing the entire page with a measuring placeholder.
func (a *app) measureCacheUsage(notifyOnCompletion bool) {
	a.cacheUsage.requested = true
	if a.pathCache == "" || a.cacheCleanupBusy || a.cacheUsage.loading {
		return
	}
	a.cacheUsage.loading = true
	a.cacheUsage.notifyOnCompletion = notifyOnCompletion
	a.cacheUsage.serial++
	serial, cacheDir := a.cacheUsage.serial, a.pathCache
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		indexBytes := codeSearchIndexDiskBytes(filepath.Join(cacheDir, codeSearchCacheDirName))
		avatarBytes := codeSearchIndexDiskBytes(filepath.Join(cacheDir, avatarCacheDirName))
		// Only the Cache page needs a repaint for this background result.
		// Every other Settings page or workspace keeps its current frame.
		a.updateChanged(func() bool {
			if serial != a.cacheUsage.serial {
				return false
			}
			a.cacheUsage.indexBytes = indexBytes
			a.cacheUsage.avatarBytes = avatarBytes
			a.cacheUsage.loading = false
			a.cacheUsage.loaded = true
			notify := a.cacheUsage.notifyOnCompletion
			a.cacheUsage.notifyOnCompletion = false
			if notify {
				a.notify("success", "Cache size refreshed", trf("Total cache: %s", formatRemoteTransferBytes(a.cacheUsage.totalBytes())))
			}
			return a.settingsOpen && a.settingsPage == settingsPageCache
		})
	})
}

// invalidateCacheUsage discards stale sizes and refreshes them only when the
// Settings cache page has actually requested them before.
func (a *app) invalidateCacheUsage() {
	a.cacheUsage.serial++
	a.cacheUsage.loaded = false
	a.cacheUsage.loading = false
	a.cacheUsage.notifyOnCompletion = false
	if a.cacheUsage.requested {
		a.ensureCacheUsage()
	}
}

// refreshCacheUsage explicitly starts a new measurement from the Settings
// cache page, even when the currently displayed sizes are already loaded.
func (a *app) refreshCacheUsage() {
	if a.pathCache == "" || a.cacheCleanupBusy || a.cacheUsage.loading {
		return
	}
	// Keep loaded=true: only the digits change when the measurement finishes.
	a.measureCacheUsage(true)
}

// regenerableCacheBytes measures only data the user can safely regenerate.
func regenerableCacheBytes(cacheDir string) int64 {
	if cacheDir == "" {
		return 0
	}
	return codeSearchIndexDiskBytes(filepath.Join(cacheDir, codeSearchCacheDirName)) +
		codeSearchIndexDiskBytes(filepath.Join(cacheDir, avatarCacheDirName))
}

// clearRegenerableCaches removes every search index, including those used by
// open tabs, plus cached avatars. Unlike GC this is an explicit user action;
// the shell resets every tab's index state afterwards so active workspaces
// validate and rebuild on demand. It returns the measured number of bytes
// actually released, excluding anything that could not be removed.
func clearRegenerableCaches(cacheDir string) int64 {
	before := regenerableCacheBytes(cacheDir)
	root := filepath.Join(cacheDir, codeSearchCacheDirName)
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if entry.IsDir() && cacheHashHex(entry.Name(), 16) {
			removeCodeSearchIndexDirectory(filepath.Join(root, entry.Name()))
		}
	}
	if cacheDir != "" {
		_ = os.RemoveAll(filepath.Join(cacheDir, avatarCacheDirName))
	}
	return max(before-regenerableCacheBytes(cacheDir), 0)
}

// openCacheCleaner measures before asking, so the confirmation can tell the
// user how much storage this action frees without blocking Home's rendering.
func (a *app) openCacheCleaner() {
	if a.pathCache == "" || a.cacheCleanupBusy {
		return
	}
	a.cacheCleanupSerial++
	serial, cacheDir := a.cacheCleanupSerial, a.pathCache
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		size := regenerableCacheBytes(cacheDir)
		a.update(func() {
			if serial != a.cacheCleanupSerial || a.cacheCleanupBusy {
				return
			}
			if size == 0 {
				a.notify("info", "Cache is already empty", "")
				return
			}
			a.askConfirm(confirmRequest{
				title:        "Clear cache",
				message:      "Clear all Zoekt search indexes and cached avatars?",
				detail:       trf("Approximately %s can be reclaimed. Unsaved editor recovery and Git safety journals are kept.", formatRemoteTransferBytes(size)),
				confirmLabel: "Clear cache",
				run:          a.clearApplicationCache,
			})
		})
	})
}

// clearApplicationCache performs the confirmed cleanup off the UI thread and
// then lets the shell revalidate any workspace whose index was just removed.
func (a *app) clearApplicationCache() {
	if a.pathCache == "" || a.cacheCleanupBusy {
		return
	}
	a.cacheCleanupBusy = true
	a.cacheCleanupSerial++
	a.notify("info", "Clearing cache…", "")
	cacheDir := a.pathCache
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		freed := clearRegenerableCaches(cacheDir)
		a.update(func() {
			a.cacheCleanupBusy = false
			a.invalidateCacheUsage()
			if a.onCachesCleared != nil {
				a.onCachesCleared()
			}
			a.notify("success", "Cache cleared",
				trf("Freed %s. Zoekt indexes and avatars will be recreated as needed.", formatRemoteTransferBytes(freed)))
		})
	})
}
