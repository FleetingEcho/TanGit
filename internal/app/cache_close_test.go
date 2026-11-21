package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRepositoryCacheCloseReturnsToSettings(t *testing.T) {
	syncCacheCleanup(t)
	h := menuTestApp(t)
	parent := t.TempDir()
	h.app.pathCache = filepath.Join(parent, "cache")
	h.app.pathUserData = filepath.Join(parent, "data")
	root := cacheTestRepository(t, parent, "important")
	h.app.recentRepositories = []recentRepository{{Path: root, Name: "Important"}}
	writeCacheForRepository(t, h.app, root)
	h.app.openSettingsSection(settingsPageCache)
	h.app.openRepositoryCaches()
	h.tester.Frame()
	h.updates.runPending()
	h.tester.Frame()

	if _, found := h.tester.AccessByRole(ui.RoleButton, "Back to Cache"); found {
		t.Fatal("cache manager duplicates the header close action with a footer button")
	}
	if _, found := h.tester.AccessByRole(ui.RoleButton, "Close repository cache manager"); !found {
		t.Fatal("cache manager is missing its header close button")
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "cache-manager-close-only", h.tester)
	}
	if err := h.tester.Click("Close repository cache manager"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.cacheRepositories.open || !h.app.settingsUI.open || h.app.settingsUI.page != settingsPageCache {
		t.Fatalf("header close also closed Settings: manager=%v settings=%v page=%d",
			h.app.cacheRepositories.open, h.app.settingsUI.open, h.app.settingsUI.page)
	}
	if !h.tester.HasText("Manage repositories") {
		t.Fatalf("Cache settings were not restored behind the closed manager: %q", h.texts())
	}

	h.app.openRepositoryCaches()
	h.tester.Frame()
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.cacheRepositories.open || !h.app.settingsUI.open || h.app.settingsUI.page != settingsPageCache {
		t.Fatalf("Escape did not close only the cache manager: manager=%v settings=%v page=%d",
			h.app.cacheRepositories.open, h.app.settingsUI.open, h.app.settingsUI.page)
	}
}
