package app

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

// canonicalZoektRepositoryRoot identifies one local workspace when it exists
// and retains the absolute spelling when it has since been removed from disk.
// It is also the identity used to store a disabled Zoekt preference.
func canonicalZoektRepositoryRoot(root string) string {
	cleaned := cleanRepoPath(strings.TrimSpace(root))
	if cleaned == "" {
		return ""
	}
	if canonical, err := resolvedCodeSearchIndexRoot(cleaned); err == nil {
		return canonical
	}
	if absolute, err := filepath.Abs(cleaned); err == nil {
		return absolute
	}
	return filepath.Clean(cleaned)
}

func normalizeZoektDisabledRepositories(repositories []string) []string {
	seen := make(map[string]bool, len(repositories))
	out := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		root := canonicalZoektRepositoryRoot(repository)
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, root)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

func (a *app) currentZoektRepositoryRoot() string {
	if a.location.IsRemote() || a.snapshot.Root == "" {
		return ""
	}
	return canonicalZoektRepositoryRoot(a.snapshot.Root)
}

// zoektCodeSearchEnabled is the per-repository default-on switch. A missing
// entry enables indexing; a disabled root never builds or consults Zoekt and
// therefore falls through to the complete ripgrep/native search backends.
func (a *app) zoektCodeSearchEnabled() bool {
	root := a.currentZoektRepositoryRoot()
	if root == "" {
		return false
	}
	for _, disabled := range a.settings.ZoektDisabledRepositories {
		if canonicalZoektRepositoryRoot(disabled) == root {
			return false
		}
	}
	return true
}

func (a *app) resetZoektBuildersFor(root string) {
	if a.onZoektPreferenceChanged != nil {
		a.onZoektPreferenceChanged(root)
		return
	}
	a.resetCodeSearchIndex()
}

// confirmDisableZoekt asks before changing anything. Cancelling therefore
// leaves both the checkbox and this repository's index exactly as they were.
func (a *app) confirmDisableZoekt(c *ui.Context) {
	if a.currentZoektRepositoryRoot() == "" || !a.zoektCodeSearchEnabled() {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Disable Zoekt for this repository?",
		message:      "Code Search will use ripgrep or the built-in search instead. Searches may be slower, especially on large repositories.",
		detail:       "The current repository's Zoekt index cache will be cleared. Editor recovery and Git history are not affected.",
		confirmLabel: "Disable and clear index",
		destructive:  true,
		run:          func() { a.disableZoekt(c) },
	})
}

func (a *app) disableZoekt(c *ui.Context) {
	root := a.currentZoektRepositoryRoot()
	if root == "" {
		return
	}
	a.settings.ZoektDisabledRepositories = normalizeZoektDisabledRepositories(
		append(a.settings.ZoektDisabledRepositories, root))
	a.settingsChanged(c)
	a.resetZoektBuildersFor(root)
	directory := codeSearchIndexDirectory(a.pathCache, root)
	notify := func() {
		a.notifyTranslated("success", "Zoekt disabled for this repository",
			"Its index cache was cleared. Code Search now uses ripgrep or the built-in search, which may be slower.")
	}
	if directory == "" {
		notify()
		return
	}
	cacheCleanupAsync(func() {
		defer a.recoverBackground()
		// The shell cancelled every builder for this root first. Taking the
		// index lock here also waits for one already publishing its work.
		removeCodeSearchIndexDirectory(directory)
		a.update(notify)
	})
}

func (a *app) enableZoekt(c *ui.Context) {
	root := a.currentZoektRepositoryRoot()
	if root == "" || a.zoektCodeSearchEnabled() {
		return
	}
	kept := make([]string, 0, len(a.settings.ZoektDisabledRepositories))
	for _, disabled := range a.settings.ZoektDisabledRepositories {
		if canonicalZoektRepositoryRoot(disabled) != root {
			kept = append(kept, disabled)
		}
	}
	a.settings.ZoektDisabledRepositories = normalizeZoektDisabledRepositories(kept)
	a.settingsChanged(c)
	a.resetZoektBuildersFor(root)
	// Rebuilding is on demand and still respects the source-size budget.
	a.ensureCodeSearchIndex()
	a.notifyTranslated("success", "Zoekt enabled for this repository",
		"Its index will be rebuilt as needed. Code Search will use Zoekt when the index is ready.")
}
