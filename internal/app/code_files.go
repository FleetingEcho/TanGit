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

// listCodeEmptyDirectories supplements the file index with empty, non-ignored
// working-tree directories. Git otherwise has no path entry from which the
// Explorer could synthesize them. Failure only omits this supplementary view;
// listCodeFiles remains authoritative for the index itself.
func listCodeEmptyDirectories(root string) []string {
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--others", "--exclude-standard", "--directory")
	output, err := command.Output()
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var directories []string
	for _, name := range strings.Split(string(output), "\x00") {
		if !strings.HasSuffix(name, "/") {
			continue
		}
		name = strings.TrimSuffix(name, "/")
		if !validCodeRelativePath(name) || seen[name] {
			continue
		}
		filename := filepath.Join(root, filepath.FromSlash(name))
		info, statErr := os.Lstat(filename)
		if statErr != nil || !info.IsDir() {
			continue
		}
		entries, readErr := os.ReadDir(filename)
		if readErr != nil || len(entries) != 0 {
			continue
		}
		seen[name] = true
		directories = append(directories, name)
	}
	sort.Strings(directories)
	return directories
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
	return filterCodePathsFzf(paths, query)
}
