package app

import (
	"io/fs"
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

// listPlainCodeFiles indexes a folder without consulting Git. WalkDir never
// follows symbolic links; .git is excluded so a nested administrative folder
// cannot become editable through the plain-folder workspace. Empty directories
// are returned separately because the Explorer synthesizes them beside files.
var listPlainCodeFiles = func(root string) ([]string, []string, error) {
	var paths, directories []string
	nonEmpty := make(map[string]bool)
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == root {
			return nil
		}
		relative, err := filepath.Rel(root, filename)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if !validCodeRelativePath(name) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		parent := filepath.ToSlash(filepath.Dir(relative))
		if parent != "." {
			nonEmpty[parent] = true
		}
		if strings.EqualFold(entry.Name(), ".git") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			directories = append(directories, name)
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			paths = append(paths, name)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	emptyDirectories := make([]string, 0, len(directories))
	for _, directory := range directories {
		if !nonEmpty[directory] {
			emptyDirectories = append(emptyDirectories, directory)
		}
	}
	sort.Strings(paths)
	sort.Strings(emptyDirectories)
	return paths, emptyDirectories, nil
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
