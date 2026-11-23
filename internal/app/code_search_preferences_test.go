package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestZoektPreferenceDefaultsOnAndNormalizesDisabledRoots(t *testing.T) {
	root := cacheTestRepository(t, t.TempDir(), "repo")
	a := newApp(nil)
	a.snapshot.Root = root
	if !a.zoektCodeSearchEnabled() {
		t.Fatal("a repository with no stored preference must enable Zoekt by default")
	}
	disabled := normalizeZoektDisabledRepositories([]string{root, root, ""})
	if len(disabled) != 1 || disabled[0] != root {
		t.Fatalf("normalized disabled roots = %v, want only %q", disabled, root)
	}
	a.settings.ZoektDisabledRepositories = disabled
	if a.zoektCodeSearchEnabled() {
		t.Fatal("disabled repository still enables Zoekt")
	}
	other := cacheTestRepository(t, t.TempDir(), "other")
	a.snapshot.Root = other
	if !a.zoektCodeSearchEnabled() {
		t.Fatal("one repository's opt-out changed another repository's default")
	}
}

func TestDisableZoektConfirmationLifecycle(t *testing.T) {
	syncCacheCleanup(t)
	parent := t.TempDir()
	a := newApp(nil)
	root := cacheTestRepository(t, parent, "current")
	other := cacheTestRepository(t, parent, "other")
	if err := os.WriteFile(filepath.Join(root, "search.txt"), []byte("needle in native search\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.snapshot.Root = root
	a.pathCache = filepath.Join(parent, "cache")
	a.pathUserData = filepath.Join(parent, "data")
	a.settings = normalizeSettings(defaultSettings())
	updated := newUpdates()
	a.update = updated.update
	currentIndex, currentRecovery := writeCacheForRepository(t, a, root)
	otherIndex, otherRecovery := writeCacheForRepository(t, a, other)
	tt := ui.NewTester(func(c *ui.Context) {
		a.zoektSearchSettings(c)
		a.confirmModal(c)
	}, 1100, 700)
	tt.Frame()

	if err := tt.Click("Use Zoekt code search"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.confirm == nil || !tt.HasText("Disable Zoekt for this repository?") ||
		!tt.HasText("Code Search will use ripgrep or the built-in search instead. Searches may be slower, especially on large repositories.") {
		t.Fatalf("disable confirmation lacks its search/cost explanation: %v", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.confirm != nil || len(a.settings.ZoektDisabledRepositories) != 0 || !a.zoektCodeSearchEnabled() {
		t.Fatalf("Cancel changed the preference: confirm=%+v disabled=%v", a.confirm, a.settings.ZoektDisabledRepositories)
	}
	if _, err := os.Stat(currentIndex); err != nil {
		t.Fatalf("Cancel deleted the index: %v", err)
	}

	if err := tt.Click("Use Zoekt code search"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Disable and clear index"); err != nil {
		t.Fatal(err)
	}
	updated.runPending()
	tt.Frame()
	if a.zoektCodeSearchEnabled() {
		t.Fatal("accepted disable left Zoekt enabled")
	}
	if _, err := os.Stat(currentIndex); !os.IsNotExist(err) {
		t.Fatalf("current repository index was not cleared: %v", err)
	}
	for _, path := range []string{currentRecovery, otherIndex, otherRecovery} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated data %s was touched: %v", path, err)
		}
	}
	reopened := newApp(nil)
	reopened.snapshot.Root = root
	reopened.settings = normalizeSettings(loadAppSettings(a.pathUserData))
	if reopened.zoektCodeSearchEnabled() {
		t.Fatal("reopening settings forgot this repository's Zoekt opt-out")
	}
	if a.codeSearchIndexDirectory() != "" {
		t.Fatal("disabled repository still exposes a Zoekt search backend")
	}

	// Turning Zoekt back on needs no confirmation and must not rebuild eagerly
	// in this fixture (no file list is loaded yet).
	if err := tt.Click("Use Zoekt code search"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.confirm != nil {
		t.Fatalf("re-enabling unexpectedly opened a confirmation: %+v", a.confirm)
	}
	if !a.zoektCodeSearchEnabled() || len(a.settings.ZoektDisabledRepositories) != 0 {
		t.Fatalf("re-enable did not restore the preference: %+v", a.settings.ZoektDisabledRepositories)
	}
	if reopenedAgain := normalizeSettings(loadAppSettings(a.pathUserData)); len(reopenedAgain.ZoektDisabledRepositories) != 0 {
		t.Fatalf("re-enable was not persisted: %v", reopenedAgain.ZoektDisabledRepositories)
	}
}

func TestZoektPreferenceCancelsSameRepositoryBuildersAcrossTabs(t *testing.T) {
	s, _, _ := newShellTester(t, repository.Snapshot{})
	const shared = "/work/shared-zoekt-repo"
	first := readyShellTab(s, shared)
	second := readyShellTab(s, shared)
	unrelated := readyShellTab(s, "/work/unrelated-zoekt-repo")
	root := first.app.currentZoektRepositoryRoot()
	s.activeApp().settings.ZoektDisabledRepositories = []string{root}

	firstCancelled, secondCancelled, unrelatedCancelled := 0, 0, 0
	first.app.code.searchIndex.cancel = func() { firstCancelled++ }
	second.app.code.searchIndex.cancel = func() { secondCancelled++ }
	unrelated.app.code.searchIndex.cancel = func() { unrelatedCancelled++ }

	s.activeApp().onZoektPreferenceChanged(root)
	if firstCancelled != 1 || secondCancelled != 1 || unrelatedCancelled != 0 {
		t.Fatalf("builder cancellation across tabs = same(%d,%d), unrelated %d", firstCancelled, secondCancelled, unrelatedCancelled)
	}
	for _, tab := range []*tab{first, second} {
		if len(tab.app.settings.ZoektDisabledRepositories) != 1 || tab.app.settings.ZoektDisabledRepositories[0] != root {
			t.Errorf("same-repository tab did not receive disabled preference: %+v", tab.app.settings.ZoektDisabledRepositories)
		}
	}
}
