package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type codeFileOperationKind uint8

const (
	codeFileCreateFile codeFileOperationKind = iota
	codeFileCreateFolder
	codeFileRename
	codeFileMove
	codeFileDelete
)

// cleanCodeEntryName validates a single path component supplied by the Editor.
func cleanCodeEntryName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.EqualFold(name, ".git") {
		return "", false
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return "", false
	}
	return name, true
}

// validCodeManagedPath keeps filesystem operations inside the working tree and
// out of Git's administrative directory.
func validCodeManagedPath(name string) bool {
	if !validCodeRelativePath(name) {
		return false
	}
	first, _, _ := strings.Cut(name, "/")
	return !strings.EqualFold(first, ".git")
}

func joinCodePath(directory, name string) string {
	if directory == "" {
		return name
	}
	return directory + "/" + name
}

func codePathWithin(path, directory string) bool {
	if !validCodeManagedPath(path) {
		return false
	}
	if directory == "" {
		return true
	}
	if !validCodeManagedPath(directory) {
		return false
	}
	return path == directory || strings.HasPrefix(path, directory+"/")
}

// performCodeFileOperation applies one validated filesystem change. The caller
// supplies slash-separated repository-relative paths, never absolute paths.
func performCodeFileOperation(root string, kind codeFileOperationKind, source, destination string) error {
	if !validCodeManagedPath(destination) {
		return fmt.Errorf("invalid destination path %q", destination)
	}

	switch kind {
	case codeFileCreateFile, codeFileCreateFolder:
		if source != "" {
			return fmt.Errorf("create operation has source path %q", source)
		}
	case codeFileRename, codeFileMove, codeFileDelete:
		if !validCodeManagedPath(source) {
			return fmt.Errorf("invalid source path %q", source)
		}
	default:
		return fmt.Errorf("unknown code file operation %d", kind)
	}

	sourcePath := filepath.Join(root, filepath.FromSlash(source))
	destinationPath := filepath.Join(root, filepath.FromSlash(destination))
	if source != "" {
		if err := checkCodeParentDirectories(root, source); err != nil {
			return err
		}
	}
	if err := checkCodeParentDirectories(root, destination); err != nil {
		return err
	}

	switch kind {
	case codeFileCreateFile:
		file, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		return file.Close()
	case codeFileCreateFolder:
		return os.Mkdir(destinationPath, 0o755)
	case codeFileRename, codeFileMove:
		info, err := lstatCodeFileOrDirectory(sourcePath)
		if err != nil {
			return err
		}
		destinationInfo, destinationErr := os.Lstat(destinationPath)
		caseOnlyRename := kind == codeFileRename && source != destination && strings.EqualFold(source, destination) &&
			destinationErr == nil && os.SameFile(info, destinationInfo)
		if destinationErr == nil && !caseOnlyRename {
			return fmt.Errorf("destination %q already exists", destination)
		}
		if destinationErr != nil && !errors.Is(destinationErr, fs.ErrNotExist) {
			return destinationErr
		}
		if kind == codeFileMove && info.IsDir() && codePathWithin(destination, source) {
			return fmt.Errorf("cannot move directory %q into itself", source)
		}
		return os.Rename(sourcePath, destinationPath)
	case codeFileDelete:
		info, err := lstatCodeFileOrDirectory(sourcePath)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return os.RemoveAll(sourcePath)
		}
		return os.Remove(sourcePath)
	default:
		panic("unreachable code file operation")
	}
}

// checkCodeParentDirectories keeps lexical containment meaningful: a symlinked
// parent could otherwise redirect an apparently repository-relative operation
// to any directory on the host. The repository root itself is already trusted.
func checkCodeParentDirectories(root, name string) error {
	parts := strings.Split(name, "/")
	current := root
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%q is not a real directory inside the repository", current)
		}
	}
	return nil
}

// lstatCodeFileOrDirectory refuses links and special files, including when a
// link points to an otherwise acceptable regular file or directory.
func lstatCodeFileOrDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return nil, fmt.Errorf("%q is not a regular file or directory", path)
	}
	return info, nil
}
