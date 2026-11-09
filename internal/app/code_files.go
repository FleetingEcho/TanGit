package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// listCodeFiles returns tracked files and untracked files that Git does not
// ignore. Deleted index entries and non-file entries, such as submodules, are
// left out because the Code page edits the working tree.
var listCodeFiles = func(root string) ([]string, error) {
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	paths := make([]string, 0, bytesCount(output, 0))
	for _, name := range strings.Split(string(output), "\x00") {
		if name == "" || !validCodeRelativePath(name) || seen[name] {
			continue
		}
		info, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if statErr != nil || !info.Mode().IsRegular() {
			continue
		}
		seen[name] = true
		paths = append(paths, name)
	}
	sort.Strings(paths)
	return paths, nil
}

func bytesCount(data []byte, value byte) int {
	count := 0
	for _, item := range data {
		if item == value {
			count++
		}
	}
	return count
}

func validCodeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func filterCodePaths(paths []string, query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]string(nil), paths...)
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.Contains(strings.ToLower(path), query) {
			filtered = append(filtered, path)
		}
	}
	return filtered
}
