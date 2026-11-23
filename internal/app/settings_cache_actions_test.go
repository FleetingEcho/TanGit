package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCacheRefreshBelongsToGroupHeader(t *testing.T) {
	syncCacheCleanup(t)
	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	h.app.pathUserData = t.TempDir()
	h.app.cacheUsage.loaded = true
	h.app.cacheUsage.indexBytes = 3 << 20
	h.app.cacheUsage.avatarBytes = 1 << 20
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Cache settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()

	refresh, found := h.tester.Find("Refresh cache size")
	if !found {
		t.Fatal("cache refresh button is missing")
	}
	manage, found := h.tester.Find("Manage repositories")
	if !found {
		t.Fatal("cache management actions are missing")
	}
	total, found := h.tester.Find("Total cache")
	if !found {
		t.Fatal("cache total row is missing")
	}
	dialog, found := h.tester.AccessByRole(ui.RoleDialog, "Settings")
	if !found {
		t.Fatal("Settings dialog is missing")
	}
	if refresh.Y+refresh.H > total.Y || manage.Y+manage.H > total.Y {
		t.Fatalf("cache actions are not in the group header: refresh=%+v total=%+v manage=%+v", refresh, total, manage)
	}
	clear, found := h.tester.Find("Clear cache")
	if !found {
		t.Fatal("the Clear cache button is missing from the header")
	}
	if clear.X+clear.W < dialog.Bounds.X+dialog.Bounds.W-48 || clear.Y+clear.H > total.Y {
		t.Fatalf("Clear cache is not at the header's right edge: clear=%+v dialog=%+v", clear, dialog.Bounds)
	}
	if refresh.X+refresh.W > clear.X+1 {
		t.Fatalf("Refresh and Clear cache overlap or are out of order: %v %v", refresh, clear)
	}
	if _, found := h.tester.AccessByRole(ui.RoleText, "Clear cache"); found {
		t.Fatal("Clear cache is visible as text instead of an icon-only button")
	}
	if manage.X+manage.W > refresh.X+1 {
		t.Fatalf("Manage and refresh overlap or are out of order: Manage=%v Refresh=%v", manage, refresh)
	}
	if _, found := h.tester.AccessByRole(ui.RoleText, "Manage repositories"); found {
		t.Fatal("Manage repositories is visible as text instead of an icon-only button")
	}
	if _, found := h.tester.AccessByRole(ui.RoleButton, "Manage repositories"); !found {
		t.Fatal("the repository-management icon button is missing")
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "cache-actions-in-group-header", h.tester)
	}

	shard := filepath.Join(h.app.pathCache, codeSearchCacheDirName, "0123456789abcdef0123456789abcdef", "working.zoekt")
	if err := os.MkdirAll(filepath.Dir(shard), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shard, []byte("newer-index"), 0o600); err != nil {
		t.Fatal(err)
	}
	avatarDir := filepath.Join(h.app.pathCache, avatarCacheDirName)
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avatarDir, "author.png"), make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.tester.Click("Refresh cache size"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the cache sizes after a header refresh", func() bool {
		return h.app.cacheUsage.indexBytes == int64(len("newer-index"))
	})
	if h.app.cacheUsage.avatarBytes != 1<<20 || h.app.cacheUsage.totalBytes() != int64(len("newer-index"))+1<<20 {
		t.Fatalf("refresh changed the wrong cache values: %+v", h.app.cacheUsage)
	}

	h.tester.SetSize(720, 720)
	h.tester.Frame()
	if _, found := h.tester.Find("Refresh cache size"); !found {
		t.Fatal("header refresh is clipped by the narrower settings layout")
	}

	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "cache-actions-in-group-header-narrow", h.tester)
	}
	if err := h.tester.Click("Manage repositories"); err != nil {
		t.Fatal(err)
	}
	if !h.app.cacheRepositories.open {
		t.Fatal("the header icon does not open the repository cache manager")
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.cacheRepositories.open {
		t.Fatal("closing the manager after a header-icon click failed")
	}
}

// The Cache page has a selector for the most source one repository may put into
// its search index; choosing a size applies at once and is saved.
func TestCacheIndexLimitSelector(t *testing.T) {
	previous := codeSearchIndexLimit.Load()
	t.Cleanup(func() { codeSearchIndexLimit.Store(previous) })
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.cacheUsage.loaded = true
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Cache settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if got := codeSearchIndexSourceBytesLimit(); got != 10<<30 {
		t.Fatalf("the default limit is %d, want 10 GiB", got)
	}
	if _, ok := h.tester.Find("Index size limit"); !ok {
		t.Fatal("the index size limit row is missing")
	}
	h.app.applyIndexLimitChoice(nil, "2 GiB")
	if h.app.settings.CodeSearchIndexLimitGiB != 2 || codeSearchIndexSourceBytesLimit() != 2<<30 {
		t.Fatalf("the choice was not applied: setting %d, limit %d", h.app.settings.CodeSearchIndexLimitGiB, codeSearchIndexSourceBytesLimit())
	}
	if saved := loadAppSettings(h.app.pathUserData); saved.CodeSearchIndexLimitGiB != 2 {
		t.Fatalf("the choice was not saved: %d", saved.CodeSearchIndexLimitGiB)
	}
	if normalizeSettings(appSettings{CodeSearchIndexLimitGiB: 7}).CodeSearchIndexLimitGiB != 0 {
		t.Fatal("an unknown size was kept")
	}
}
