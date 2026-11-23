package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func cacheRepositoriesTestApp(t *testing.T, recents ...recentRepository) (*app, *updates) {
	t.Helper()
	syncCacheCleanup(t)
	parent := t.TempDir()
	a := newApp(nil)
	a.pathCache = filepath.Join(parent, "cache")
	a.pathUserData = filepath.Join(parent, "data")
	a.recentRepositories = recents
	updated := newUpdates()
	a.update = updated.update
	a.updateChanged = func(fn func() bool) { updated.update(func() { fn() }) }
	return a, updated
}

func TestRepositoryCacheManagerClearsAndForgetsOnlyItsTarget(t *testing.T) {
	parent := t.TempDir()
	kept := cacheTestRepository(t, parent, "important")
	targetRepo := cacheTestRepository(t, parent, "disposable")
	a, updated := cacheRepositoriesTestApp(t, recentRepository{Path: kept, Name: "Important"}, recentRepository{Path: targetRepo, Name: "Disposable"})
	keptIndex, keptRecovery := writeCacheForRepository(t, a, kept)
	targetIndex, targetRecovery := writeCacheForRepository(t, a, targetRepo)
	journal := filepath.Join(targetRepo, ".git", "tangit", "journal", "operation.json")
	if err := os.MkdirAll(filepath.Dir(journal), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, []byte("undo"), 0o600); err != nil {
		t.Fatal(err)
	}
	avatarDir := filepath.Join(a.pathCache, avatarCacheDirName)
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avatarDir, "author.png"), []byte("avatar"), 0o600); err != nil {
		t.Fatal(err)
	}
	resets := 0
	a.onCachesCleared = func() { resets++ }
	a.openRepositoryCaches()
	updated.runPending()
	if len(a.cacheRepositories.rows) != 2 || a.cacheRepositories.rows[0].bytes == 0 || a.cacheRepositories.rows[1].bytes == 0 {
		t.Fatalf("cache manager did not measure each recent entry independently: %+v", a.cacheRepositories.rows)
	}

	row := a.cacheRepositories.rows[1]
	a.confirmRepositoryCache(row, false)
	if a.confirm == nil || !strings.Contains(a.confirm.message, "Disposable") {
		t.Fatalf("repository clear confirmation is missing: %+v", a.confirm)
	}
	a.cancelConfirm()
	for _, path := range []string{keptIndex, targetIndex, keptRecovery, targetRecovery, journal} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cancel changed %s: %v", path, err)
		}
	}

	a.confirmRepositoryCache(row, false)
	a.acceptConfirm()
	updated.runPending()
	if _, err := os.Stat(targetIndex); !os.IsNotExist(err) {
		t.Fatalf("selected index survived project cleanup: %v", err)
	}
	for _, path := range []string{keptIndex, keptRecovery, targetRecovery, journal, avatarDir} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("project cleanup removed preserved data %s: %v", path, err)
		}
	}
	if len(a.recentRepositories) != 2 {
		t.Fatalf("cache-only cleanup also forgot a recent entry: %+v", a.recentRepositories)
	}
	a.ensureRepositoryCaches()
	updated.runPending()
	if len(a.cacheRepositories.rows) != 2 || a.cacheRepositories.rows[0].bytes == 0 ||
		a.cacheRepositories.rows[1].bytes != 0 || a.cacheRepositories.rows[1].protected {
		t.Fatalf("project sizes did not refresh independently: %+v", a.cacheRepositories.rows)
	}

	a.confirmRepositoryCache(row, true)
	a.acceptConfirm()
	updated.runPending()
	if len(a.recentRepositories) != 1 || a.recentRepositories[0].Path != kept {
		t.Fatalf("wrong recent entry survived: %+v", a.recentRepositories)
	}
	if _, err := os.Stat(keptIndex); err != nil {
		t.Fatalf("important project's index was removed: %v", err)
	}
	for _, path := range []string{targetRecovery, journal, avatarDir} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("forgetting a recent card must not discard recovery or journal %s: %v", path, err)
		}
	}
	if resets != 1 || a.cacheCleanupBusy {
		t.Fatalf("only the actual index deletion should invalidate once: resets = %d, busy = %v", resets, a.cacheCleanupBusy)
	}
}

func TestRepositoryCacheManagerProtectsOtherOwners(t *testing.T) {
	parent := t.TempDir()
	realRoot := cacheTestRepository(t, parent, "real")
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	target := recentRepository{Path: realRoot}
	a, _ := cacheRepositoriesTestApp(t, target, recentRepository{Path: alias})
	indexDir, _ := writeCacheForRepository(t, a, realRoot)
	o := a.repositoryCacheOwnership()
	rows := measureRepositoryCaches(o)
	if len(rows) != 2 || !rows[0].protected || !rows[1].protected {
		t.Fatalf("shared indexes were not marked protected for both projected rows: %+v", rows)
	}
	if freed := clearRepositorySearchIndex(o, target); freed != 0 {
		t.Fatalf("other recent card still owns the shared index; freed = %d", freed)
	}
	if _, err := os.Stat(indexDir); err != nil {
		t.Fatalf("alias-owned cache was deleted: %v", err)
	}

	a.recentRepositories = []recentRepository{target}
	o = a.repositoryCacheOwnership()
	o.roots = []string{realRoot}
	if freed := clearRepositorySearchIndex(o, target); freed != 0 {
		t.Fatalf("open tab still owns the cache; freed = %d", freed)
	}
	if _, err := os.Stat(indexDir); err != nil {
		t.Fatalf("open-tab cache was deleted: %v", err)
	}

	o.roots = nil
	if freed := clearRepositorySearchIndex(o, target); freed == 0 {
		t.Fatal("cache remained after its other owners were gone")
	}
	if _, err := os.Stat(indexDir); !os.IsNotExist(err) {
		t.Fatalf("unowned cache survived: %v", err)
	}
}

func TestRepositoryAvatarsAreGloballyScoped(t *testing.T) {
	parent := t.TempDir()
	root := cacheTestRepository(t, parent, "kept")
	a, updated := cacheRepositoriesTestApp(t, recentRepository{Path: root})
	indexDir, recovery := writeCacheForRepository(t, a, root)
	avatarDir := filepath.Join(a.pathCache, avatarCacheDirName)
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avatarDir, "author.png"), []byte("avatar"), 0o600); err != nil {
		t.Fatal(err)
	}

	a.confirmAvatarCaches()
	updated.runPending()
	if a.confirm == nil {
		t.Fatal("avatar cleanup did not ask for confirmation")
	}
	a.cancelConfirm()
	if _, err := os.Stat(avatarDir); err != nil {
		t.Fatalf("cancel deleted avatars: %v", err)
	}
	a.confirmAvatarCaches()
	updated.runPending()
	a.acceptConfirm()
	updated.runPending()
	if _, err := os.Stat(avatarDir); !os.IsNotExist(err) {
		t.Fatalf("global avatars were not removed: %v", err)
	}
	for _, path := range []string{indexDir, recovery} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("global avatar cleanup deleted project data %s: %v", path, err)
		}
	}
}

func TestRepositoryCacheModalListsProjectsAndConfirmsRemoval(t *testing.T) {
	syncCacheCleanup(t)
	h := menuTestApp(t)
	parent := t.TempDir()
	h.app.pathCache = filepath.Join(parent, "cache")
	h.app.pathUserData = filepath.Join(parent, "data")
	kept := cacheTestRepository(t, parent, "important")
	targetRepo := cacheTestRepository(t, parent, "disposable")
	h.app.recentRepositories = []recentRepository{{Path: kept, Name: "Important"}, {Path: targetRepo, Name: "Disposable"}}
	keptIndex, _ := writeCacheForRepository(t, h.app, kept)
	targetIndex, targetRecovery := writeCacheForRepository(t, h.app, targetRepo)
	h.app.openSettingsSection(settingsPageCache)
	h.app.openRepositoryCaches()
	h.tester.Frame()
	h.updates.runPending()
	h.tester.Frame()
	for _, text := range []string{"Manage repository caches", "Important", "Disposable", "Shared author avatars"} {
		if !h.tester.HasText(text) {
			t.Fatalf("repository cache modal misses %q: %q", text, h.texts())
		}
	}

	if err := h.tester.Click("Clear cache for Disposable"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil {
		t.Fatal("per-project clear did not open the confirmation modal")
	}
	if err := h.tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if _, err := os.Stat(targetIndex); err != nil {
		t.Fatalf("cancel removed the project cache: %v", err)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "settings-repository-caches", h.tester)
	}

	previousLanguage := currentLanguage
	t.Cleanup(func() { currentLanguage = previousLanguage })
	narrow := ui.NewTester(func(c *ui.Context) {
		currentLanguage = "zh-Hans"
		h.app.settingsModal(c)
		h.app.confirmModal(c)
	}, 480, 720)
	if !narrow.HasText("管理仓库缓存") || !narrow.HasText("从最近记录移除") {
		t.Fatalf("translated narrow repository cache controls are missing: %q", narrow.Texts())
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "settings-repository-caches-narrow", narrow)
	}

	if err := narrow.Click("清理缓存并将 Disposable 从最近记录移除"); err != nil {
		t.Fatal(err)
	}
	narrow.Frame()
	if err := narrow.Click("清理缓存并移除最近记录"); err != nil {
		t.Fatal(err)
	}
	h.updates.runPending()
	h.tester.Frame()
	h.waitFor(t, "the retained repository cache row after its sibling is forgotten", func() bool {
		return len(h.app.cacheRepositories.rows) == 1 && h.app.cacheRepositories.rows[0].recent.Path == kept
	})
	if !h.tester.HasText("Important") {
		t.Fatalf("manager did not show the retained repository: %q", h.texts())
	}
	if _, err := os.Stat(targetIndex); !os.IsNotExist(err) {
		t.Fatalf("confirmed selected index survived: %v", err)
	}
	for _, path := range []string{keptIndex, targetRecovery} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("project-specific confirmation removed preserved data %s: %v", path, err)
		}
	}
}

func TestUnlistedIndexCleanupIsScopedAndRevalidatesOwners(t *testing.T) {
	parent := t.TempDir()
	recentRoot := cacheTestRepository(t, parent, "recent")
	unlistedRoot := cacheTestRepository(t, parent, "unlisted")
	a, updated := cacheRepositoriesTestApp(t, recentRepository{Path: recentRoot})
	recentIndex, _ := writeCacheForRepository(t, a, recentRoot)
	unlistedIndex, unlistedRecovery := writeCacheForRepository(t, a, unlistedRoot)
	manifest, err := json.Marshal(codeSearchIndexManifest{Version: codeSearchIndexVersion, Root: unlistedRoot})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unlistedIndex, codeSearchIndexManifestFile), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	a.openRepositoryCaches()
	updated.runPending()
	var row cacheRepositoryRow
	for _, candidate := range a.cacheRepositories.rows {
		if candidate.kind == cachePublishedUnlisted {
			row = candidate
		}
	}
	if row.kind != cachePublishedUnlisted || row.root != unlistedRoot {
		t.Fatalf("unlisted index did not retain its recorded root: %+v", row)
	}

	a.confirmRepositoryCache(row, false)
	if a.confirm == nil || a.confirm.title != "Clear unlisted search index" {
		t.Fatalf("unlisted confirmation has the wrong scope: %+v", a.confirm)
	}
	a.cancelConfirm()
	if _, err := os.Stat(unlistedIndex); err != nil {
		t.Fatalf("unlisted cancel deleted the index: %v", err)
	}
	a.confirmRepositoryCache(row, false)
	a.acceptConfirm()
	updated.runPending()
	if _, err := os.Stat(unlistedIndex); !os.IsNotExist(err) {
		t.Fatalf("unlisted index survived its explicit cleanup: %v", err)
	}
	for _, path := range []string{recentIndex, unlistedRecovery} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unlisted cleanup removed preserved data %s: %v", path, err)
		}
	}
	if len(a.recentRepositories) != 1 || a.recentRepositories[0].Path != recentRoot {
		t.Fatalf("unlisted cleanup changed recent history: %+v", a.recentRepositories)
	}

	// A cached list is a proposal, not a deletion authorization. Revalidate
	// ownership if a new card for the previous unlisted root appears before
	// the old row's confirmed operation actually starts.
	writeCacheForRepository(t, a, unlistedRoot)
	a.recentRepositories = append(a.recentRepositories, recentRepository{Path: unlistedRoot})
	a.clearRepositoryCache(row, false)
	if _, err := os.Stat(unlistedIndex); err != nil {
		t.Fatalf("revalidation deleted an index that just gained an owner: %v", err)
	}
	last := a.pendingToasts[len(a.pendingToasts)-1]
	if last.title != "Index cache kept" {
		t.Fatalf("stale index deletion did not report retained ownership: %+v", last)
	}
}

func TestRepositoryCacheInventoryAccountsForUnlistedAndStaging(t *testing.T) {
	parent := t.TempDir()
	selectedRoot := cacheTestRepository(t, parent, "selected")
	unlistedRoot := cacheTestRepository(t, parent, "unlisted")
	a, _ := cacheRepositoriesTestApp(t, recentRepository{Path: selectedRoot})
	writeCacheForRepository(t, a, selectedRoot)
	writeCacheForRepository(t, a, unlistedRoot)
	for _, name := range []string{".lazygitgo-index-2313360354", ".lazygitgo-index-428520547"} {
		directory := filepath.Join(a.pathCache, codeSearchCacheDirName, name)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "partial.zoekt"), []byte("unfinished"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	rows := measureRepositoryCaches(a.repositoryCacheOwnership())
	if len(rows) != 3 {
		t.Fatalf("cache inventory omits unlisted or groups unfinished indexes incorrectly: %+v", rows)
	}
	var listed int64
	for _, row := range rows {
		listed += row.bytes
	}
	want := codeSearchIndexDiskBytes(filepath.Join(a.pathCache, codeSearchCacheDirName))
	if listed != want {
		t.Fatalf("listed index bytes = %d, disk total = %d; rows = %+v", listed, want, rows)
	}
}

func TestRepositoryCacheModalShowsAndClearsUnfinishedIndexes(t *testing.T) {
	syncCacheCleanup(t)
	h := menuTestApp(t)
	parent := t.TempDir()
	h.app.pathCache = filepath.Join(parent, "cache")
	h.app.pathUserData = filepath.Join(parent, "data")
	recentRoot := cacheTestRepository(t, parent, "recent")
	unlistedRoot := cacheTestRepository(t, parent, "unlisted")
	h.app.recentRepositories = []recentRepository{{Path: recentRoot, Name: "Important"}}
	recentIndex, _ := writeCacheForRepository(t, h.app, recentRoot)
	unlistedIndex, _ := writeCacheForRepository(t, h.app, unlistedRoot)
	staging := filepath.Join(h.app.pathCache, codeSearchCacheDirName, ".lazygitgo-index-428520547")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	partial, err := os.Create(filepath.Join(staging, "partial.zoekt"))
	if err != nil {
		t.Fatal(err)
	}
	// Sparse data reproduces the reported GiB-scale size without allocating
	// several GiB of real disk space in a unit test.
	if err := partial.Truncate(7580 << 20); err != nil {
		partial.Close()
		t.Fatal(err)
	}
	if err := partial.Close(); err != nil {
		t.Fatal(err)
	}
	resets := 0
	h.app.onCachesCleared = func() { resets++ }
	h.app.openSettingsSection(settingsPageCache)
	h.app.openRepositoryCaches()
	h.tester.Frame()
	h.updates.runPending()
	h.tester.Frame()
	for _, text := range []string{"Recent projects", "Other published search indexes", "Unfinished index builds", "7.4 GiB"} {
		if !h.tester.HasText(text) {
			t.Fatalf("cache scope %q is missing: %q", text, h.texts())
		}
	}
	scrollRepositoryCachesToEnd(t, h.tester)
	if err := h.tester.Click("Clear cache for Unfinished index builds"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.confirmLabel != "Stop builds and clear unfinished indexes" {
		t.Fatalf("temporary-index cleanup has no explicit stop-build confirmation: %+v", h.app.confirm)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "repository-cache-unfinished-confirm", h.tester)
	}
	if err := h.tester.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if _, err := os.Stat(staging); err != nil || resets != 0 {
		t.Fatalf("cancelled temporary-index cleanup changed state: err=%v resets=%d", err, resets)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "repository-cache-unfinished", h.tester)
	}

	scrollRepositoryCachesToEnd(t, h.tester)
	if err := h.tester.Click("Clear cache for Unfinished index builds"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Stop builds and clear unfinished indexes"); err != nil {
		t.Fatal(err)
	}
	h.updates.runPending()
	h.tester.Frame()
	h.waitFor(t, "the published indexes after unfinished-cache cleanup", func() bool {
		return len(h.app.cacheRepositories.rows) == 2
	})
	for _, path := range []string{recentIndex, unlistedIndex} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("completed index was removed with staging: %s: %v", path, err)
		}
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("unfinished index survived its own cleanup: %v", err)
	}
	if len(h.app.recentRepositories) != 1 || h.app.recentRepositories[0].Path != recentRoot || resets != 1 {
		t.Fatalf("temporary-index cleanup changed recents or did not invalidate builders: recents=%+v resets=%d", h.app.recentRepositories, resets)
	}
}

func scrollRepositoryCachesToEnd(t *testing.T, tester *ui.Tester) {
	t.Helper()
	viewport, found := tester.Find("Recent repository caches")
	if !found {
		t.Fatal("cache-repository scroll viewport is missing")
	}
	for range 3 {
		tester.Scroll(viewport.X+viewport.W/2, viewport.Y+viewport.H/2, 0, 240)
		tester.Frame()
	}
}

func TestRepositoryCacheModalRejectsStaleScansAfterClose(t *testing.T) {
	a, _ := cacheRepositoriesTestApp(t, recentRepository{Path: t.TempDir()})
	var pending []func()
	cacheCleanupAsync = func(fn func()) { pending = append(pending, fn) }
	a.openRepositoryCaches()
	a.cacheRepositories.close()
	pending[0]()
	if a.cacheRepositories.loaded || len(a.cacheRepositories.rows) != 0 {
		t.Fatalf("a closed cache dialog accepted its former scan: %+v", a.cacheRepositories)
	}
}
