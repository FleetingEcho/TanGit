package app

import (
	"errors"
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

// plainFileLimit caps how many files the plain-folder explorer indexes. It is
// generous for a source tree and keeps a workspace opened on a giant system
// image from flooding the interface, memory or the interactivity of the page.
const plainFileLimit = 25000

type plainFileList struct {
	// Paths are the indexed files, in lexical order.
	Paths []string
	// EmptyDirectories holds the directories with no indexed content, which
	// the Explorer synthesizes alongside the files.
	EmptyDirectories []string
	// Skipped counts entries that could not be read or were skipped as
	// mount points.
	Skipped int
	// Truncated reports that the root holds more regular files than the
	// limit; the listing covers the lexical first portion only.
	Truncated bool
}

// listPlainCodeFiles indexes a folder without consulting Git. WalkDir never
// follows symbolic links; .git is excluded so a nested administrative folder
// cannot become editable through the plain-folder workspace. Mount roots are
// skipped wholesale, and the walk stops once plainFileLimit regular files are
// found, in lexical order. A readable root can hold unreadable subtrees, such
// as procfs/sysfs mounts or permission-denied directories: those are skipped
// and counted, never fatal for the whole listing. The root itself must be
// readable.
var errPlainFileLimit = errors.New("plain folder file limit reached")

var listPlainCodeFiles = func(root string) (plainFileList, error) {
	found := plainFileList{}
	var directories []string
	nonEmpty := make(map[string]bool)
	truncated := false
	err := filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if filename == root {
				// The root itself must be readable; there is nothing to show.
				return walkErr
			}
			// An unreadable subtree (procfs/sysfs mount, permission denied):
			// skip it and keep going instead of failing the listing.
			found.Skipped++
			if entry == nil || entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if filename == root {
			return nil
		}
		if info, err := entry.Info(); err == nil && entry.IsDir() && isPlainFolderMountPoint(info) {
			found.Skipped++
			return filepath.SkipDir
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
			// A file that cannot be stat'ed is unreadable for our purposes;
			// it is skipped, not fatal for the whole listing.
			found.Skipped++
			return nil
		}
		if info.Mode().IsRegular() {
			found.Paths = append(found.Paths, name)
			if len(found.Paths) >= plainFileLimit {
				truncated = true
				return errPlainFileLimit
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errPlainFileLimit) {
		return plainFileList{}, err
	}
	if truncated {
		// A truncated listing is already full; the empty-directory view is
		// dropped rather than half-built against unknown remaining content.
		found.Truncated = true
		return found, nil
	}
	for _, directory := range directories {
		if !nonEmpty[directory] {
			found.EmptyDirectories = append(found.EmptyDirectories, directory)
		}
	}
	sort.Strings(found.Paths)
	sort.Strings(found.EmptyDirectories)
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
