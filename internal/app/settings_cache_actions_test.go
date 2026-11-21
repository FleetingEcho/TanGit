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
	if refresh.X+refresh.W < dialog.Bounds.X+dialog.Bounds.W-48 {
		t.Fatalf("refresh is not aligned with the header's right edge: refresh=%+v dialog=%+v", refresh, dialog.Bounds)
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
