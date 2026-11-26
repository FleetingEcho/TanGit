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

// plainFileLimit caps how many files the plain-folder explorer lists. It is
// generous for a source tree and keeps a workspace opened on a giant system
// image from flooding the interface, memory or the interactivity of the page.
const plainFileLimit = 25000

type plainFileList struct {
	// Paths are the listed files, in lexical order.
	Paths []string
	// ExtraDirectories are known directories the Explorer should show
	// alongside the files, even if none of their contents were listed.
	ExtraDirectories []string
	// Skipped counts entries that could not be read or were skipped as
	// mount points.
	Skipped int
	// Truncated reports that the root holds more regular files than the
	// limit. The listing favors entries closer to the root.
	Truncated bool
}

var listPlainCodeFiles = func(root string) (plainFileList, error) {
	return scanPlainCodeFolder(root, plainFileLimit)
}

// scanPlainCodeFolder lists shallow entries before descending, so a large
// subtree cannot crowd its siblings out of the initial Explorer view.
func scanPlainCodeFolder(root string, limit int) (plainFileList, error) {
	found := plainFileList{}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() {
		return found, err
	}
	directories := []string{""}
	for next := 0; next < len(directories); next++ {
		directory := directories[next]
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(directory)))
		if err != nil {
			if directory == "" {
				return plainFileList{}, err
			}
			found.Skipped++
			found.ExtraDirectories = append(found.ExtraDirectories, directory)
			continue
		}
		hasEntries := false
		for _, entry := range entries {
			name := filepath.ToSlash(filepath.Join(directory, entry.Name()))
			if !validCodeRelativePath(name) {
				continue
			}
			if strings.EqualFold(entry.Name(), ".git") || entry.Type()&os.ModeSymlink != 0 {
				hasEntries = true
				continue
			}
			if found.Truncated && !entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil || (entry.IsDir() && isPlainFolderMountPoint(info)) {
				found.Skipped++
				continue
			}
			hasEntries = true
			if entry.IsDir() {
				directories = append(directories, name)
			} else if info.Mode().IsRegular() {
				if len(found.Paths) == limit {
					found.Truncated = true
					// Finish this directory so its sibling entries are discovered.
					continue
				}
				found.Paths = append(found.Paths, name)
			}
		}
		if !hasEntries && directory != "" {
			found.ExtraDirectories = append(found.ExtraDirectories, directory)
		}
		if found.Truncated {
			found.ExtraDirectories = append(found.ExtraDirectories, directories[next+1:]...)
			break
		}
	}
	sort.Strings(found.Paths)
	sort.Strings(found.ExtraDirectories)
	return found, nil
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
