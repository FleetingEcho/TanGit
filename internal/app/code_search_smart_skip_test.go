package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func manyFiles(dir string, count int, names ...string) []string {
	files := append([]string{}, func() []string {
		var named []string
		for _, name := range names {
			named = append(named, dir+"/"+name)
		}
		return named
	}()...)
	for i := 0; i < count; i++ {
		files = append(files, fmt.Sprintf("%s/src/f%d.c", dir, i))
	}
	return files
}

// Folders nobody searches are found by name, by .gitattributes and by what they
// contain, so ones with unfamiliar names are covered as well.
func TestSmartSkippedFolders(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"),
		[]byte("# vendored\nlegacy/blob/** linguist-vendored\nsrc/*.go linguist-generated\ndocs/api/ linguist-generated=true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var paths []string
	paths = append(paths, "main.go", "src/app/app.go", "src/app/third_party/lib.go", "third_party/zlib/zlib.c")
	paths = append(paths, "legacy/blob/data.txt", "docs/api/index.html", "docs/guide.md")
	// An upstream project with an unfamiliar name: license plus a Kconfig step.
	paths = append(paths, manyFiles("platform/oskernel", 300, "COPYING", "Kconfig", "MAINTAINERS")...)
	// A folder of the repository's own code: a license alone is not enough.
	paths = append(paths, manyFiles("services/billing", 300, "LICENSE")...)
	// Too small to be a copied project.
	paths = append(paths, manyFiles("tools/tiny", 20, "COPYING", "configure")...)

	got := smartSkippedFolders(root, paths)
	want := []string{"docs/api", "legacy/blob", "platform/oskernel", "src/app/third_party", "third_party"}
	if !slices.Equal(got, want) {
		t.Fatalf("skipped folders = %v, want %v", got, want)
	}
}

// A folder inside a skipped one is not listed again.
func TestSmartSkippedFoldersKeepsOnlyTheOutermost(t *testing.T) {
	paths := []string{"third_party/a/third_party/x.go", "third_party/b.go", "app.go"}
	if got := smartSkippedFolders(t.TempDir(), paths); !slices.Equal(got, []string{"third_party"}) {
		t.Fatalf("skipped folders = %v", got)
	}
}

// A path entry excludes one folder from the repository root, a name any folder
// of that name.
func TestSearchExclusionsAcceptFolderPaths(t *testing.T) {
	excluded := normalizeWorkspaceSearchExcludedDirectories([]string{"node_modules", "docs/archive/", "../escape", `back\slash`, " "})
	if !slices.Equal(excluded, []string{"docs/archive", "node_modules"}) {
		t.Fatalf("normalized = %v", excluded)
	}
	for path, want := range map[string]bool{
		"docs/archive/a.md":     true,
		"docs/current/a.md":     false,
		"x/docs/archive/a.md":   false,
		"web/node_modules/p.js": true,
		"docs/archive":          false,
		"docs/archiveold/a.md":  false,
	} {
		if got := workspaceSearchDirectoryExcluded(path, excluded); got != want {
			t.Errorf("%s excluded = %v, want %v", path, got, want)
		}
	}
}

// The smart folders join the user's own list while searching and indexing, and
// the switch turns them off.
func TestSearchExclusionsIncludeSmartSkips(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.SearchExcludedDirectories = []string{"node_modules"}
	h.app.code.smartSkips = []string{"third_party", "platform/oskernel"}
	got := h.app.searchExclusions()
	if !slices.Contains(got, "platform/oskernel") || !slices.Contains(got, "node_modules") {
		t.Fatalf("exclusions = %v", got)
	}
	h.app.settings.SmartSearchSkipDisabled = true
	if slices.Contains(h.app.searchExclusions(), "platform/oskernel") {
		t.Fatal("the smart skips stayed on after the switch was turned off")
	}
}

// Editing the excluded folders applies at once: a search that is showing
// results runs again with the new list, and the index built for the old list is
// rebuilt in the background.
func TestChangingExcludedFoldersRerunsSearchAndRebuildsTheIndex(t *testing.T) {
	var rebuilds []func()
	previous := scheduleCodeSearchIndexRebuild
	scheduleCodeSearchIndexRebuild = func(_ time.Duration, run func()) { rebuilds = append(rebuilds, run) }
	t.Cleanup(func() { scheduleCodeSearchIndexRebuild = previous })

	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.snapshot.Root = t.TempDir()
	h.app.code.filesLoaded = true
	h.app.code.files = []string{"a.txt", "docs/b.txt"}
	h.app.code.searchQuery, h.app.code.searchSubmitted = "needle", "needle"
	h.app.code.searchIndex.status = codeSearchIndexReady

	h.app.settings.SearchExcludedDirectories = append(h.app.settings.SearchExcludedDirectories, "docs")
	h.app.searchExclusionsChanged(nil)

	if !slices.Contains(h.app.code.searchSubmittedOptions.ExcludedDirectories, "docs") {
		t.Fatalf("the open search still uses the old exclusions: %v", h.app.code.searchSubmittedOptions.ExcludedDirectories)
	}
	if h.app.code.searchIndex.status != codeSearchIndexStale || len(rebuilds) != 1 {
		t.Fatalf("the index was not marked stale and queued for a rebuild: status %v, %d rebuilds", h.app.code.searchIndex.status, len(rebuilds))
	}
	if saved := loadAppSettings(h.app.pathUserData); !slices.Contains(saved.SearchExcludedDirectories, "docs") {
		t.Fatal("the new exclusion was not saved")
	}
}
