package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// syncCacheCleanup runs reclamation synchronously for deterministic tests.
func syncCacheCleanup(t *testing.T) {
	t.Helper()
	previous := cacheCleanupAsync
	cacheCleanupAsync = func(fn func()) { fn() }
	t.Cleanup(func() { cacheCleanupAsync = previous })
}

func cacheTestRepository(t *testing.T, parent, name string) string {
	t.Helper()
	root := filepath.Join(parent, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// writeCacheForRepository creates exactly the files production creates, using
// production's own path helpers so the tests cannot drift from the key format.
func writeCacheForRepository(t *testing.T, a *app, root string) (string, string) {
	t.Helper()
	indexDir := codeSearchIndexDirectory(a.pathCache, root)
	if indexDir == "" {
		t.Fatalf("no index directory for %q", root)
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "working_00001.zoekt"), []byte("shards"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovery := codeRecoveryPath(a.pathUserData, root)
	if err := os.MkdirAll(filepath.Dir(recovery), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recovery, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return indexDir, recovery
}

func assertCachePaths(t *testing.T, indexDir, recovery string, want bool) {
	t.Helper()
	_, indexErr := os.Stat(indexDir)
	_, recoveryErr := os.Stat(recovery)
	if want && (indexErr != nil || recoveryErr != nil) {
		t.Fatalf("cache should survive: index err=%v recovery err=%v", indexErr, recoveryErr)
	}
	if !want && (!os.IsNotExist(indexErr) || !os.IsNotExist(recoveryErr)) {
		t.Fatalf("cache should be reclaimed: index err=%v recovery err=%v", indexErr, recoveryErr)
	}
}

func TestForgetRecentReclaimsRepositoryCaches(t *testing.T) {
	syncCacheCleanup(t)
	parent := t.TempDir()
	a := newApp(nil)
	a.pathCache = filepath.Join(parent, "cache")
	a.pathUserData = filepath.Join(parent, "data")
	removed := cacheTestRepository(t, parent, "removed")
	kept := cacheTestRepository(t, parent, "kept")
	a.recentRepositories = []recentRepository{
		{Path: removed}, {Path: kept},
	}
	removedIndex, removedRecovery := writeCacheForRepository(t, a, removed)
	keptIndex, keptRecovery := writeCacheForRepository(t, a, kept)

	a.forgetRecent(removed, false)

	if len(a.recentRepositories) != 1 || a.recentRepositories[0].Path != kept {
		t.Fatalf("wrong history after removal: %+v", a.recentRepositories)
	}
	assertCachePaths(t, removedIndex, removedRecovery, false)
	assertCachePaths(t, keptIndex, keptRecovery, true)
}

func TestForgetRecentLeavesSiblingWorktreeAndSubmoduleCaches(t *testing.T) {
	syncCacheCleanup(t)
	parent := t.TempDir()
	a := newApp(nil)
	a.pathCache = filepath.Join(parent, "cache")
	a.pathUserData = filepath.Join(parent, "data")
	mainRoot := cacheTestRepository(t, parent, "main")
	worktree := cacheTestRepository(t, parent, "feature-worktree")
	submodule := cacheTestRepository(t, parent, "submodule")
	a.recentRepositories = []recentRepository{
		{Path: mainRoot}, {Path: worktree, Worktree: true, RepositoryPath: mainRoot}, {Path: submodule},
	}
	mainIndex, mainRecovery := writeCacheForRepository(t, a, mainRoot)
	worktreeIndex, worktreeRecovery := writeCacheForRepository(t, a, worktree)
	submoduleIndex, submoduleRecovery := writeCacheForRepository(t, a, submodule)

	a.forgetRecent(mainRoot, false)

	assertCachePaths(t, mainIndex, mainRecovery, false)
	assertCachePaths(t, worktreeIndex, worktreeRecovery, true)
	assertCachePaths(t, submoduleIndex, submoduleRecovery, true)
}

func TestForgetRecentKeepsCacheUsedByAnOpenTab(t *testing.T) {
	syncCacheCleanup(t)
	parent := t.TempDir()
	a := newApp(nil)
	a.pathCache = filepath.Join(parent, "cache")
	a.pathUserData = filepath.Join(parent, "data")
	root := cacheTestRepository(t, parent, "open-in-another-tab")
	a.recentRepositories = []recentRepository{{Path: root}}
	a.cacheOpenRoots = func() []string { return []string{root} }
	indexDir, recovery := writeCacheForRepository(t, a, root)

	a.forgetRecent(root, false)

	assertCachePaths(t, indexDir, recovery, true)
	if len(a.recentRepositories) != 0 {
		t.Fatalf("card itself must still be removed: %+v", a.recentRepositories)
	}
}

func TestForgetRecentKeepsCacheReferencedThroughSymlinkAlias(t *testing.T) {
	syncCacheCleanup(t)
	parent := t.TempDir()
	a := newApp(nil)
	a.pathCache = filepath.Join(parent, "cache")
	a.pathUserData = filepath.Join(parent, "data")
	realRoot := cacheTestRepository(t, parent, "real")
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	a.recentRepositories = []recentRepository{{Path: realRoot}, {Path: alias}}
	indexDir, recovery := writeCacheForRepository(t, a, realRoot)

	a.forgetRecent(alias, false)

	// The canonical index is shared by both spellings of the same checkout,
	// and the remaining card still owns it.
	if _, err := os.Stat(indexDir); err != nil {
		t.Fatalf("shared index was removed: %v", err)
	}
	if _, err := os.Stat(recovery); err != nil {
		t.Fatalf("remaining card's recovery was removed: %v", err)
	}
}

func TestRememberOpenedReclaimsEvictedHistoryCaches(t *testing.T) {
	syncCacheCleanup(t)
	parent := t.TempDir()
	a := newApp(nil)
	a.pathCache = filepath.Join(parent, "cache")
	a.pathUserData = filepath.Join(parent, "data")
	type cachePaths struct{ index, recovery string }
	evicted := cacheTestRepository(t, parent, "evicted")
	for i := 0; i < maxRecentRepositories; i++ {
		root := evicted
		if i < maxRecentRepositories-1 {
			root = cacheTestRepository(t, parent, fmt.Sprintf("kept-%02d", i))
		}
		a.recentRepositories = append(a.recentRepositories, recentRepository{Path: root, LastOpened: int64(maxRecentRepositories - i)})
	}
	evictedIndex, evictedRecovery := writeCacheForRepository(t, a, evicted)
	newRoot := cacheTestRepository(t, parent, "new")
	before := append([]recentRepository(nil), a.recentRepositories...)

	a.recentRepositories = rememberRecent(a.recentRepositories, newRoot, 100)
	a.cleanupEvictedRecents(before)

	assertCachePaths(t, evictedIndex, evictedRecovery, false)
	for _, repo := range a.recentRepositories {
		if cleanRepoPath(repo.Path) == cleanRepoPath(evicted) {
			t.Fatalf("evicted card is still in history: %+v", repo)
		}
	}
	if got := len(a.recentRepositories); got != maxRecentRepositories {
		t.Fatalf("history cap changed: got %d", got)
	}
}

func TestStartupGCAgesOrphansAndCapsShardStorage(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "data")
	cacheDir := filepath.Join(parent, "cache")
	keptRoot := cacheTestRepository(t, parent, "kept")
	ownership := repositoryCacheOwnership{
		cacheDir: cacheDir,
		dataDir:  dataDir,
		recents:  []recentRepository{{Path: keptRoot}},
	}
	now := time.Now()

	makeShard := func(name string, size int64, age time.Duration) string {
		t.Helper()
		dir := filepath.Join(cacheDir, codeSearchCacheDirName, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(dir, "working_00001.zoekt"))
		if err != nil {
			t.Fatal(err)
		}
		// A sparse file exercises the size accounting without using real disk.
		if err := file.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-age)
		if err := os.Chtimes(dir, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	keptShard := makeShard(codeSearchIndexKeysFor(keptRoot)[0], 1<<20, 90*24*time.Hour)
	oldOrphan := makeShard("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 1<<20, 40*24*time.Hour)
	olderLarge := makeShard("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 3<<30, 5*24*time.Hour)
	newerLarge := makeShard("cccccccccccccccccccccccccccccccc", 3<<30, 24*time.Hour)
	foreign := filepath.Join(cacheDir, codeSearchCacheDirName, "operator-files")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}

	gcRepositoryCaches(ownership, now)

	if _, err := os.Stat(oldOrphan); !os.IsNotExist(err) {
		t.Fatalf("old orphan survived: %v", err)
	}
	if _, err := os.Stat(olderLarge); !os.IsNotExist(err) {
		t.Fatalf("oldest large orphan should be reclaimed under the cap: %v", err)
	}
	for _, path := range []string{keptShard, newerLarge, foreign} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s should survive GC: %v", path, err)
		}
	}
}

func TestStartupGCKeepsYoungOrphansAndAgesOnlyUnsavedRecovery(t *testing.T) {
	parent := t.TempDir()
	keptRoot := cacheTestRepository(t, parent, "kept")
	ownership := repositoryCacheOwnership{
		cacheDir: filepath.Join(parent, "cache"),
		dataDir:  filepath.Join(parent, "data"),
		recents:  []recentRepository{{Path: keptRoot}},
	}
	now := time.Now()
	youngIndex := filepath.Join(ownership.cacheDir, codeSearchCacheDirName, "dddddddddddddddddddddddddddddddd")
	if err := os.MkdirAll(youngIndex, 0o755); err != nil {
		t.Fatal(err)
	}

	recoveryDir := filepath.Join(ownership.dataDir, editorRecoveryDirName)
	if err := os.MkdirAll(recoveryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keptRecovery := codeRecoveryPath(ownership.dataDir, keptRoot)
	oldOrphan := filepath.Join(recoveryDir, "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee.json")
	youngOrphan := filepath.Join(recoveryDir, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff.json")
	foreign := filepath.Join(recoveryDir, "notes.txt")
	for _, path := range []string{keptRecovery, oldOrphan, youngOrphan, foreign} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldStamp := now.Add(-(editorRecoveryOrphanAge + 24*time.Hour))
	for _, path := range []string{keptRecovery, oldOrphan} {
		if err := os.Chtimes(path, oldStamp, oldStamp); err != nil {
			t.Fatal(err)
		}
	}

	gcRepositoryCaches(ownership, now)

	if _, err := os.Stat(oldOrphan); !os.IsNotExist(err) {
		t.Fatalf("old orphaned recovery survived: %v", err)
	}
	for _, path := range []string{keptRecovery, youngOrphan, youngIndex, foreign} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s should survive GC: %v", path, err)
		}
	}
}

func TestShellStartCacheGCLoadsStoredHistoryBeforeReclaiming(t *testing.T) {
	syncCacheCleanup(t)
	parent := t.TempDir()
	s := newShell(nil)
	s.pathCache = filepath.Join(parent, "cache")
	s.pathUserData = filepath.Join(parent, "data")
	keptRoot := cacheTestRepository(t, parent, "stored-card")
	if err := saveRecentRepositories(s.pathUserData, []recentRepository{{Path: keptRoot}}); err != nil {
		t.Fatal(err)
	}
	a := newApp(nil)
	a.pathCache = s.pathCache
	a.pathUserData = s.pathUserData
	s.tabs = []*tab{{app: a}}
	s.active = 0
	keptIndex, _ := writeCacheForRepository(t, a, keptRoot)
	oldOrphan := filepath.Join(s.pathCache, codeSearchCacheDirName, "99999999999999999999999999999999")
	if err := os.MkdirAll(oldOrphan, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-2 * codeSearchOrphanAge)
	for _, path := range []string{keptIndex, oldOrphan} {
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	s.startCacheGC()

	if _, err := os.Stat(keptIndex); err != nil {
		t.Fatalf("stored card's index was collected before history loaded: %v", err)
	}
	if _, err := os.Stat(oldOrphan); !os.IsNotExist(err) {
		t.Fatalf("old orphaned index survived startup GC: %v", err)
	}
}

func TestClearApplicationCacheReclaimsOnlyRegenerableData(t *testing.T) {
	syncCacheCleanup(t)
	parent := t.TempDir()
	a := newApp(nil)
	a.pathCache = filepath.Join(parent, "cache")
	a.pathUserData = filepath.Join(parent, "data")
	updated := newUpdates()
	a.update = updated.update
	root := cacheTestRepository(t, parent, "repo")
	indexDir, recovery := writeCacheForRepository(t, a, root)
	avatarDir := filepath.Join(a.pathCache, avatarCacheDirName)
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avatarDir, "author.png"), []byte("avatar"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(root, ".git", "tangit", "journal", "operation.json")
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte("undo data"), 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(a.pathCache, "operator-owned")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	resetCalls := 0
	a.onCachesCleared = func() { resetCalls++ }

	a.openCacheCleaner()
	updated.runPending()
	if a.confirm == nil || a.confirm.title != "Clear cache" {
		t.Fatalf("cache confirmation missing: %+v", a.confirm)
	}
	if !strings.Contains(a.confirm.detail, "B can be reclaimed") {
		t.Fatalf("confirmation does not report measured cache size: %q", a.confirm.detail)
	}
	a.acceptConfirm()
	updated.runPending()

	if _, err := os.Stat(indexDir); !os.IsNotExist(err) {
		t.Fatalf("search index survived explicit cleanup: %v", err)
	}
	if _, err := os.Stat(avatarDir); !os.IsNotExist(err) {
		t.Fatalf("avatar cache survived explicit cleanup: %v", err)
	}
	for _, path := range []string{recovery, journal, foreign} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("non-cache data %s was removed: %v", path, err)
		}
	}
	if resetCalls != 1 {
		t.Fatalf("active index reset calls = %d, want 1", resetCalls)
	}
	if a.cacheCleanupBusy {
		t.Fatal("cache cleanup remained busy after completion")
	}
	last := a.pendingToasts[len(a.pendingToasts)-1]
	if last.kind != "success" || last.title != "Cache cleared" || !strings.Contains(last.description, "Freed ") {
		t.Fatalf("cache result notification = %+v", last)
	}
}

func TestOpenCacheCleanerOnEmptyCacheDoesNotConfirm(t *testing.T) {
	syncCacheCleanup(t)
	a := newApp(nil)
	a.pathCache = t.TempDir()
	updated := newUpdates()
	a.update = updated.update

	a.openCacheCleaner()
	updated.runPending()

	if a.confirm != nil {
		t.Fatalf("empty cache opened a confirmation: %+v", a.confirm)
	}
	if len(a.pendingToasts) != 1 || a.pendingToasts[0].title != "Cache is already empty" {
		t.Fatalf("empty-cache feedback = %+v", a.pendingToasts)
	}
}

func TestHomepageHasNoCacheCleanupEntry(t *testing.T) {
	_, tt, _ := recentTester(t)
	if tt.HasText("Clear cache") {
		t.Fatalf("Home exposes a second cache-cleanup entry: %v", tt.Texts())
	}
}

func TestCacheConfirmationCancelKeepsSettingsOpen(t *testing.T) {
	syncCacheCleanup(t)
	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	h.app.pathUserData = t.TempDir()
	indexDir := filepath.Join(h.app.pathCache, codeSearchCacheDirName, "0123456789abcdef0123456789abcdef")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(indexDir, "working.zoekt"), []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.app.openSettingsSection(settingsPageCache)
	h.tester.Frame()
	h.updates.runPending()
	h.tester.Frame()
	if err := h.tester.Click("Clear cache"); err != nil {
		t.Fatal(err)
	}
	h.updates.runPending()
	h.tester.Frame()
	if h.app.confirm == nil || !h.app.settingsOpen {
		t.Fatalf("cache confirmation was not opened above Settings: confirm=%+v settings=%v", h.app.confirm, h.app.settingsOpen)
	}

	if err := h.tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm != nil {
		t.Fatalf("Cancel left the cache confirmation open: %+v", h.app.confirm)
	}
	if !h.app.settingsOpen {
		t.Fatal("cancelling cache cleanup also closed the Settings modal")
	}

	if err := h.tester.Click("Clear cache"); err != nil {
		t.Fatal(err)
	}
	h.updates.runPending()
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("could not reopen the cache confirmation for Escape testing")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.confirm != nil {
		t.Fatalf("Escape left the cache confirmation open: %+v", h.app.confirm)
	}
	if !h.app.settingsOpen {
		t.Fatal("Escape in the cache confirmation also closed the Settings modal")
	}
}

func TestCacheMeasurementDoesNotRepaintOtherSettingsPages(t *testing.T) {
	syncCacheCleanup(t)
	a := newApp(nil)
	a.pathCache = t.TempDir()
	a.settingsOpen = true
	a.settingsPage = settingsPageAppearance
	paint := make(chan bool, 1)
	a.updateChanged = func(fn func() bool) { paint <- fn() }

	a.ensureCacheUsage()
	select {
	case repaint := <-paint:
		if repaint {
			t.Fatal("cache measurement requested a repaint on another Settings page")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cache measurement did not complete")
	}
}

func TestSettingsCachePageShowsBreakdownAndRefreshesAfterClear(t *testing.T) {
	syncCacheCleanup(t)
	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	h.app.pathUserData = t.TempDir()
	writeSized := func(path string, size int64) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	writeSized(filepath.Join(h.app.pathCache, codeSearchCacheDirName, "0123456789abcdef0123456789abcdef", "working.zoekt"), 3<<20)
	writeSized(filepath.Join(h.app.pathCache, avatarCacheDirName, "author.png"), 1<<20)

	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Cache settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.updates.runPending()
	h.tester.Frame()
	if h.app.settingsPage != settingsPageCache {
		t.Fatalf("Cache rail selected settings page %d", h.app.settingsPage)
	}
	for _, text := range []string{
		"Total cache", "Zoekt search indexes", "Cached avatars", "4.0 MiB", "3.0 MiB", "1.0 MiB",
		"Only Zoekt indexes and downloaded avatars are included. Unsaved editor recovery and Git safety journals are never cleared.",
	} {
		if !h.tester.HasText(text) {
			t.Errorf("Settings cache page misses %q; texts=%v", text, h.tester.Texts())
		}
	}
	for _, label := range []string{"Total cache information", "Zoekt search indexes information", "Cached avatars information"} {
		if _, found := h.tester.Access(label); !found {
			t.Errorf("cache row %q is missing its hover-info badge", label)
		}
	}
	for _, description := range []string{
		"Indexes built and queried by Zoekt. They speed up full-text Code Search across local repository files without rescanning every file for every query.",
		"Commit-author images shown next to commits. Caching avoids repeated downloads from Git hosting services.",
	} {
		if h.tester.HasText(description) {
			t.Errorf("long tooltip description leaked into the cache page body: %q", description)
		}
	}

	writeSized(filepath.Join(h.app.pathCache, codeSearchCacheDirName, "0123456789abcdef0123456789abcdef", "working.zoekt"), 5<<20)
	if err := h.tester.Click("Refresh cache size"); err != nil {
		t.Fatal(err)
	}
	if !h.app.cacheUsage.loaded {
		t.Fatal("manual refresh cleared the displayed measurements instead of keeping the old digits")
	}
	h.updates.runPending()
	h.tester.Frame()
	for _, text := range []string{"6.0 MiB", "5.0 MiB", "1.0 MiB"} {
		if !h.tester.HasText(text) {
			t.Errorf("refreshed cache page misses %q; texts=%v", text, h.tester.Texts())
		}
	}
	for _, text := range []string{"Cache size refreshed", "Total cache: 6.0 MiB"} {
		if !h.tester.HasText(text) {
			t.Errorf("cache refresh completion did not show %q; texts=%v", text, h.tester.Texts())
		}
	}

	if err := h.tester.Click("Clear cache"); err != nil {
		t.Fatal(err)
	}
	h.updates.runPending()
	if h.app.confirm == nil {
		t.Fatal("Settings clear-cache action did not ask for confirmation")
	}
	h.app.acceptConfirm()
	h.updates.runPending() // clear completion; it starts a fresh usage scan
	h.updates.runPending() // refreshed usage result
	h.tester.Frame()
	if !h.tester.HasText("Total cache") || !h.tester.HasText("0 B") {
		t.Fatalf("cleared cache page did not refresh its sizes: %v", h.tester.Texts())
	}
	if bytes := regenerableCacheBytes(h.app.pathCache); bytes != 0 {
		t.Fatalf("cache bytes after Settings cleanup = %d", bytes)
	}
}

func TestCacheHashHexRejectsForeignNames(t *testing.T) {
	if !cacheHashHex("0123456789abcdef0123456789abcdef", 16) {
		t.Fatal("valid shard key rejected")
	}
	for _, name := range []string{
		".tangit-index-staging", "0123456789ABCDEF0123456789ABCDEF",
		"../../../etc", "0123456789abcdef", "short", "gggggggggggggggggggggggggggggggg",
	} {
		if cacheHashHex(name, 16) {
			t.Fatalf("foreign name accepted: %q", name)
		}
	}
}
