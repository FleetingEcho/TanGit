package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// chooseSavePath asks the desktop where to save a file, with the folder, the
// name and the ask before overwriting a file the desktop's own. It is a seam
// so a test can name a path without opening a dialog.
var chooseSavePath = func(opts mygo.SaveDialogOptions) (string, error) {
	return mygo.Dialog.Save(opts)
}

// patchDialogDefaultPath is always local. A remote POSIX repository root must
// never be handed to the desktop's native file chooser.
func (a *app) patchDialogDefaultPath(name string) string {
	if a.location.IsRemote() {
		return name
	}
	if name == "" {
		return a.snapshot.Root
	}
	return filepath.Join(a.snapshot.Root, name)
}

// exportDiffButton saves the diff on screen to a file: the patch as Git
// printed it, so it can be kept, sent, or applied elsewhere. It is disabled
// while the pane shows no diff.
func (a *app) exportDiffButton(c *ui.Context) {
	shown, ok := a.activeDiff()
	iconButton(c, "Save diff", "Save this diff to a file…", downloadIcon,
		!ok || shown.patch == "", a.exportDiff)
}

// exportDiff asks where to save the diff the active pane shows and writes it
// there. The dialog is the desktop's own, so the folder, the name, and the
// overwrite are its business; a cancel leaves everything as it was.
func (a *app) exportDiff() {
	shown, ok := a.activeDiff()
	if !ok || shown.patch == "" {
		return
	}
	opts := mygo.SaveDialogOptions{
		Parent:      a.window,
		Title:       tr("Save diff"),
		DefaultPath: a.patchDialogDefaultPath(diffFileName(shown)),
		ButtonLabel: tr("Save"),
		Filters: []mygo.FileFilter{
			{Name: tr("Patch files"), Extensions: []string{"patch", "diff"}},
		},
	}
	go a.writePatch(opts, shown.patch)
}

// exportCompareDiffButton downloads Git's complete comparison, not a patch
// assembled from the file viewer. It stays independent of the selected file.
func (a *app) exportCompareDiffButton(c *ui.Context) {
	disabled := a.compare.appliedFrom == "" || len(a.compare.files) == 0 ||
		a.compare.filesLoading || a.compare.wholeDiffLoading
	iconButton(c, "Save comparison diff", "Download every change between these refs as a patch…",
		downloadIcon, disabled, a.exportCompareDiff)
}

func (a *app) exportCompareDiff() {
	if a.session == nil || a.compare.appliedFrom == "" || len(a.compare.files) == 0 ||
		a.compare.filesLoading || a.compare.wholeDiffLoading {
		return
	}
	a.compare.wholeDiffLoading = true
	session := a.session
	from, to, options := a.compare.appliedFrom, a.compare.appliedTo, a.compare.appliedOptions
	generation, token := a.generation, a.compare.token
	opts := mygo.SaveDialogOptions{
		Parent:      a.window,
		Title:       tr("Save comparison diff"),
		DefaultPath: a.patchDialogDefaultPath("comparison.patch"),
		ButtonLabel: tr("Save"),
		Filters: []mygo.FileFilter{
			{Name: tr("Patch files"), Extensions: []string{"patch", "diff"}},
		},
	}
	go func() {
		defer a.recoverBackground()
		diff, err := session.CompareDiff(context.Background(), from, to, options)
		a.update(func() {
			if generation == a.generation && token == a.compare.token {
				a.compare.wholeDiffLoading = false
			}
			if err != nil {
				a.notify("error", "Could not save the diff", safeError(err))
			}
		})
		if err == nil {
			a.writePatch(opts, diff.Patch)
		}
	}()
}

// writePatch runs off the UI goroutine: both the desktop dialog and file write
// may block. Canceling the dialog leaves no file and raises no notification.
func (a *app) writePatch(opts mygo.SaveDialogOptions, patch string) {
	a.writePatchBytes(opts, []byte(patch))
}

func (a *app) writePatchBytes(opts mygo.SaveDialogOptions, patch []byte) {
	defer a.recoverBackground()
	path, err := chooseSavePath(opts)
	if err != nil {
		a.update(func() {
			a.notify("error", "Could not save the diff", safeError(err))
		})
		return
	}
	if path == "" {
		return
	}
	if err := os.WriteFile(path, patch, 0o644); err != nil {
		a.update(func() {
			a.notify("error", "Could not save the diff", safeError(err))
		})
		return
	}
	a.update(func() {
		a.notify("success", "Saved the diff", path)
	})
}

// diffFileName names the file a saved diff starts as: the changed file's own
// name with a .patch extension, so a folder can hold several of them without
// one silently replacing another.
func diffFileName(shown shownDiff) string {
	name := filepath.Base(shown.path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "diff"
	}
	return name + ".patch"
}
