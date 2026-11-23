package app

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Searching and indexing spend most of their time on folders nobody searches:
// other people's code copied into the repository, and generated output. Some of
// them have well-known names. Others do not, so they are also recognized by what
// they contain. The result is a list of folder paths, added to the user's own
// exclusions while the repository is searched and indexed.

// smartSkipFolderNames are folders of dependencies, caches and generated files
// that no repository wants searched, wherever they are.
var smartSkipFolderNames = []string{
	"third_party", "thirdparty", "third-party", "3rdparty", "3rd_party",
	"Pods", "Carthage", "DerivedData", ".venv", "venv", "site-packages", "__pycache__",
	".tox", ".mypy_cache", ".pytest_cache", ".ruff_cache", ".terraform", ".yarn", ".pnpm-store",
	"jspm_packages", ".idea", ".vscode-test", "generated", "__generated__", ".generated", "autogen",
}

// vendoredProjectMarkers say that a folder is a whole upstream project dropped
// into the repository: it carries its own license and its own configure or
// Kconfig step, which a folder of the repository's own code seldom does.
var (
	vendoredLicenseFiles  = []string{"COPYING", "COPYRIGHT", "LICENSE", "LICENSE.txt", "LICENSE.md", "CREDITS", "MAINTAINERS", "NOTICE"}
	vendoredBuildMarkers  = []string{"Kconfig", "configure", "configure.ac", "configure.in", "autogen.sh", "Makefile.in", "config.h.in"}
	vendoredMinimumFiles  = 200
	smartSkipMaximumItems = 40
)

// smartSkippedFolders lists the folders, as paths from the repository root,
// that are probably not worth searching. root locates a .gitattributes file; the
// paths are the repository's files. Nothing is read but that one file.
func smartSkippedFolders(root string, paths []string) []string {
	directories := map[string]*folderSummary{}
	for _, file := range paths {
		dir := path.Dir(file)
		base := path.Base(file)
		for current := dir; ; current = path.Dir(current) {
			summary := directories[current]
			if summary == nil {
				summary = &folderSummary{}
				directories[current] = summary
			}
			summary.files++
			if current == dir {
				summary.names = append(summary.names, base)
			}
			if current == "." || current == "/" || current == "" {
				break
			}
		}
	}
	chosen := map[string]string{}
	// By name.
	skipNames := map[string]bool{}
	for _, name := range smartSkipFolderNames {
		skipNames[name] = true
	}
	for dir := range directories {
		if dir == "." {
			continue
		}
		if skipNames[path.Base(dir)] {
			chosen[dir] = "name"
		}
	}
	// By .gitattributes: linguist-vendored and linguist-generated, which are how
	// a repository already says which folders are not its own.
	for _, dir := range gitattributesSkippedFolders(root) {
		if directories[dir] != nil {
			chosen[dir] = "gitattributes"
		}
	}
	// By content: a whole upstream project.
	for dir, summary := range directories {
		if dir == "." || summary.files < vendoredMinimumFiles {
			continue
		}
		if hasAny(summary.names, vendoredLicenseFiles) && hasAny(summary.names, vendoredBuildMarkers) {
			chosen[dir] = "project"
		}
	}
	// Keep only the outermost folders.
	result := make([]string, 0, len(chosen))
	for dir := range chosen {
		covered := false
		for parent := path.Dir(dir); parent != "." && parent != "/" && parent != ""; parent = path.Dir(parent) {
			if _, ok := chosen[parent]; ok {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, dir)
		}
	}
	sort.Strings(result)
	if len(result) > smartSkipMaximumItems {
		result = result[:smartSkipMaximumItems]
	}
	return result
}

type folderSummary struct {
	files int
	names []string
}

func hasAny(names, wanted []string) bool {
	for _, name := range names {
		for _, want := range wanted {
			if name == want {
				return true
			}
		}
	}
	return false
}

// gitattributesSkippedFolders reads the root .gitattributes for folders marked
// linguist-vendored or linguist-generated. Only plain folder patterns count:
// "third_party/**", "vendor/", "docs/gen". Anything with other wildcards is left
// alone.
func gitattributesSkippedFolders(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		return nil
	}
	var folders []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		marked := false
		for _, attribute := range fields[1:] {
			if attribute == "linguist-vendored" || attribute == "linguist-vendored=true" ||
				attribute == "linguist-generated" || attribute == "linguist-generated=true" {
				marked = true
			}
		}
		if !marked {
			continue
		}
		pattern := strings.TrimPrefix(fields[0], "/")
		for _, suffix := range []string{"/**", "/*", "/"} {
			pattern = strings.TrimSuffix(pattern, suffix)
		}
		if pattern == "" || strings.ContainsAny(pattern, "*?[!\\") {
			continue
		}
		folders = append(folders, path.Clean(pattern))
	}
	return folders
}

// searchExclusions are the folders to leave out of search and indexing: the
// user's own list, and the smart ones unless that is switched off.
func (a *app) searchExclusions() []string {
	excluded := append([]string{}, a.settings.SearchExcludedDirectories...)
	if !a.settings.SmartSearchSkipDisabled {
		excluded = append(excluded, a.code.smartSkips...)
	}
	return normalizeWorkspaceSearchExcludedDirectories(excluded)
}

// refreshSmartSkips finds the folders to skip once a repository's file list is
// known, and is a no-op for remote workspaces, which do not index.
func (a *app) refreshSmartSkips() {
	a.code.smartSkips = nil
	if a.location.IsRemote() || a.snapshot.Root == "" || a.settings.SmartSearchSkipDisabled {
		return
	}
	user := normalizeWorkspaceSearchExcludedDirectories(a.settings.SearchExcludedDirectories)
	for _, folder := range smartSkippedFolders(a.snapshot.Root, a.code.files) {
		// A folder the user's own list already leaves out needs no mention.
		if !workspaceSearchDirectoryExcluded(folder+"/x", user) {
			a.code.smartSkips = append(a.code.smartSkips, folder)
		}
	}
}

// searchExclusionsChanged applies an edit of the excluded folders at once: the
// automatic folders are worked out again, the repository's search index, which
// was built for the old list, is rebuilt in the background (search uses ripgrep
// until it is), and a search that is showing results runs again so that they
// follow the new list.
func (a *app) searchExclusionsChanged(c *ui.Context) {
	a.refreshSmartSkips()
	a.settingsChanged(c)
	if a.snapshot.Root == "" {
		return
	}
	if !a.location.IsRemote() {
		if a.code.searchIndex.status == codeSearchIndexUnavailable {
			a.code.searchIndex.status = codeSearchIndexMissing
		}
		a.invalidateCodeSearchIndex()
	}
	if a.code.searchSubmitted != "" && !a.code.searchReplaceRunning {
		a.beginCodeSearch()
	}
}
