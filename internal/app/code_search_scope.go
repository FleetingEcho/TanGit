package app

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// normalizedCodeSearchFolder keeps the editable scope repository-relative.
// Search still receives the repository's complete indexed path list; this
// value only narrows that list before either backend reads a file.
func normalizedCodeSearchFolder(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == "/" {
		return "", true
	}
	if strings.ContainsRune(value, '\x00') {
		return "", false
	}
	cleaned := path.Clean(filepath.ToSlash(value))
	if cleaned == "." {
		return "", true
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) || filepath.IsAbs(value) {
		return "", false
	}
	return cleaned, true
}

func codeSearchFolderFromDisk(root, selected string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolvedSelected, err := filepath.EvalSymlinks(selected)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolvedSelected)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedSelected)
	if err != nil {
		return "", err
	}
	folder, ok := normalizedCodeSearchFolder(filepath.ToSlash(relative))
	if !ok {
		return "", fmt.Errorf("folder is outside the repository")
	}
	return folder, nil
}

// openCodeWorkspaceFolder commits a validated repository-relative folder as
// the root shared by the Explorer and content search. Text edits remain drafts
// until this method is called, so they cannot silently change either view.
func (a *app) openCodeWorkspaceFolder(value string) bool {
	folder, ok := normalizedCodeSearchFolder(value)
	if !ok {
		a.code.searchError = tr("Workspace folder must stay inside the repository")
		return false
	}
	a.code.searchFolderSelectionSeq++
	a.code.searchFolder = folder
	a.code.searchFolderDraft = folder
	a.code.searchError = ""
	a.syncCodeTree()
	return true
}

var chooseCodeSearchFolder = func(opts mygo.OpenDialogOptions) ([]string, error) {
	return mygo.Dialog.Open(opts)
}

// browseCodeSearchFolder uses the native directory picker without blocking the
// UI. Its generation and captured root prevent a late answer from changing a
// newer scope or a different repository.
func (a *app) browseCodeSearchFolder() {
	if a.snapshot.Root == "" {
		return
	}
	if a.location.IsRemote() {
		// Native picker results are local paths and cannot be resolved against a
		// remote root. The editable repository-relative scope remains available.
		a.code.searchError = tr("Enter a repository-relative folder for an SSH workspace")
		return
	}
	a.code.searchFolderSelectionSeq++
	selection := a.code.searchFolderSelectionSeq
	root := a.snapshot.Root
	defaultPath := root
	if folder, ok := normalizedCodeSearchFolder(a.code.searchFolder); ok && folder != "" {
		defaultPath = filepath.Join(root, filepath.FromSlash(folder))
	}
	opts := mygo.OpenDialogOptions{
		Parent:      a.window,
		Title:       tr("Choose workspace folder"),
		ButtonLabel: tr("Open folder"),
		DefaultPath: defaultPath,
		Directory:   true,
	}
	go func() {
		defer a.recoverBackground()
		paths, err := chooseCodeSearchFolder(opts)
		if err == nil && len(paths) > 0 && paths[0] != "" {
			var folder string
			folder, err = codeSearchFolderFromDisk(root, paths[0])
			if err == nil {
				a.update(func() {
					if selection != a.code.searchFolderSelectionSeq || root != a.snapshot.Root {
						return
					}
					if a.openCodeWorkspaceFolder(folder) && strings.TrimSpace(a.code.searchQuery) != "" {
						a.beginCodeSearch()
					}
				})
				return
			}
		}
		if err != nil {
			a.update(func() {
				if selection == a.code.searchFolderSelectionSeq && root == a.snapshot.Root {
					a.code.searchError = safeError(err)
				}
			})
		}
	}()
}
