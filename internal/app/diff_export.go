package app

import (
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
		DefaultPath: filepath.Join(a.snapshot.Root, diffFileName(shown)),
		ButtonLabel: tr("Save"),
		Filters: []mygo.FileFilter{
			{Name: tr("Patch files"), Extensions: []string{"patch", "diff"}},
		},
	}
	patch := shown.patch
	go func() {
		defer a.recoverBackground()
		path, err := chooseSavePath(opts)
		if err != nil {
			a.update(func() {
				a.notify("error", "Could not save the diff", safeError(err))
			})
			return
		}
		if path == "" {
			// Canceled: no file, and nothing to say about it.
			return
		}
		if err := os.WriteFile(path, []byte(patch), 0o644); err != nil {
			a.update(func() {
				a.notify("error", "Could not save the diff", safeError(err))
			})
			return
		}
		a.update(func() {
			a.notify("success", "Saved the diff", path)
		})
	}()
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
