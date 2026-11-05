package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// readWorkingFile and writeWorkingFile are how the pane reaches the working
// tree. They are seams so a test can edit a file it holds in memory, as
// chooseSavePath is for the diff export.
var readWorkingFile = os.ReadFile

var writeWorkingFile = func(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// errBinaryFile reports a file the pane refuses to edit: its bytes include a
// NUL, which the text editor cannot carry back unchanged.
var errBinaryFile = errors.New("binary file")

// workingFilePath turns a path relative to the repository root into the one
// on disk. A path the session already made absolute, or a frame with no
// repository, is left as it is.
func (a *app) workingFilePath(path string) string {
	if a.snapshot.Root == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(a.snapshot.Root, path)
}

// toggleFileEdit turns the pane between the diff of the selected file and an
// editor of the file itself.
func (a *app) toggleFileEdit() {
	if a.fileEditing {
		a.closeFileEdit()
		return
	}
	a.openFileEdit()
}

// openFileEdit shows the working-tree text of the selected file in place of
// its diff. Only a file with unstaged changes has working-tree text the pane
// can edit: a staged file shows the index, and a deleted one has no text. A
// conflicted file is edited whole here too, with its markers in plain sight,
// as well as in the pane that resolves it.
func (a *app) openFileEdit() {
	change, ok := a.selectedChange()
	if a.session == nil || !ok {
		return
	}
	file := change.diffFile()
	if file.Deleted || !file.Unstaged {
		return
	}
	a.fileEditing = true
	a.fileEditKey = file.Path
	a.fileEditState = nil
	a.fileEditLoading = false
	a.fileEditBinary = false
	a.fileEditPreview = nil
	a.fileEditError = ""
	a.fileEditSaveError = ""
	a.fileEditTriedVersion = 0
	a.requestFileEdit()
}

// requestFileEdit reads the selected file's text from the working tree, for the
// editor that replaces its diff. The read is token-guarded so changing the
// selection drops an answer the pane no longer shows.
func (a *app) requestFileEdit() {
	change, ok := a.selectedChange()
	if !ok || !a.fileEditing {
		return
	}
	file := change.diffFile()
	if file.Deleted || !file.Unstaged {
		a.resetFileEdit()
		return
	}
	path := file.Path
	onDisk := a.workingFilePath(path)
	a.fileEditToken++
	token := a.fileEditToken
	a.fileEditKey = path
	a.fileEditLoading = true
	a.fileEditBinary = false
	a.fileEditPreview = nil
	a.fileEditError = ""
	a.fileEditSaveError = ""
	a.fileEditTriedVersion = 0
	a.fileEditState = nil

	go func() {
		defer a.recoverBackground()
		content, err := readWorkingFile(onDisk)
		binary := err == nil && bytes.IndexByte(content, 0) >= 0
		var preview *mediaPreview
		var state *editor.State
		if err == nil {
			if binary || isMediaPath(path) {
				decoded := decodeMediaData(content)
				if decoded.bitmap != nil {
					preview = &decoded
					binary = true
				}
			}
			if !binary {
				buffer := editor.NewBuffer(string(content))
				buffer.SetLanguage(editor.LanguageFor(path))
				state = &editor.State{
					Buffer:      buffer,
					SoftWrap:    true,
					LineNumbers: editor.LineNumbersOn,
					Label:       path,
				}
			}
		}
		a.update(func() {
			if token != a.fileEditToken || !a.fileEditing {
				return
			}
			a.fileEditLoading = false
			if binary {
				a.fileEditBinary = true
				a.fileEditPreview = preview
				return
			}
			if err != nil {
				a.fileEditError = safeError(err)
				return
			}
			a.fileEditState = state
		})
	}()
}

// saveFileEdits writes what was typed into the pane's file editor back to the
// working tree. It runs at the top of every frame that shows the editor, and
// once more as the editor closes, so switching files or closing the app never
// loses the last keys. A failed write is kept on screen and tried again when
// the text changes.
func (a *app) saveFileEdits() {
	state := a.fileEditState
	if state == nil || state.Buffer == nil || !state.Buffer.Modified() {
		return
	}
	version := state.Buffer.Version()
	if a.fileEditSaveError != "" && version == a.fileEditTriedVersion {
		return
	}
	a.fileEditTriedVersion = version

	var content bytes.Buffer
	if _, err := state.Buffer.WriteTo(&content); err != nil {
		a.fileEditSaveError = safeError(err)
		return
	}
	if err := writeWorkingFile(a.workingFilePath(a.fileEditKey), content.Bytes()); err != nil {
		a.fileEditSaveError = safeError(err)
		return
	}
	state.Buffer.MarkSaved()
	a.fileEditSaveError = ""
}

// closeFileEdit saves the typed text, leaves the editor and reads the diff
// again so the pane shows the change just made.
func (a *app) closeFileEdit() {
	if !a.fileEditing {
		return
	}
	a.saveFileEdits()
	a.resetFileEdit()
	a.resetFileDiff()
}

// resetFileEdit drops the pane's file editor and anything reading one.
func (a *app) resetFileEdit() {
	a.fileEditing = false
	a.fileEditKey = ""
	a.fileEditState = nil
	a.fileEditLoading = false
	a.fileEditBinary = false
	a.fileEditPreview = nil
	a.fileEditError = ""
	a.fileEditSaveError = ""
	a.fileEditTriedVersion = 0
	a.fileEditToken++
}

// flushEdits synchronizes the existing Changes/conflict editors and pins dirty
// Editor preview tabs. Code documents are saved only by explicit user actions.
func (a *app) flushEdits() {
	a.saveFileEdits()
	a.pinModifiedCodePreview()
	a.syncConflictEdits()
}

// syncFileEdit leaves the editor when the chosen file changed, or when the
// new choice no longer has working-tree text to edit, writing what was typed
// first. Staging the file while its editor shows counts as such a change: the
// editor's text is no longer what the pane would show.
func (a *app) syncFileEdit(file repository.File) {
	if !a.fileEditing {
		return
	}
	if a.fileEditKey == file.Path && file.Unstaged && !file.Deleted {
		return
	}
	a.saveFileEdits()
	a.resetFileEdit()
}

// fileEditPane draws the selected file as an editor. Its text is written to
// the working tree as the user types, so the diff the pane returns to shows
// the change without a save step.
func (a *app) fileEditPane(c *ui.Context, theme *ui.Theme) {
	if a.fileEditLoading {
		ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Loading file"))
			ui.Text(c, tr("Loading file…")).TextColor(theme.TextMuted)
		})
		return
	}
	if a.fileEditBinary {
		binaryFileView(c, a.fileEditKey, a.fileEditPreview, func() {
			if ui.Button(c, tr("Close")).Clicked() {
				a.closeFileEdit()
			}
		})
		return
	}
	if a.fileEditError != "" {
		ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Text(c, tr("The file could not be read.")).Bold()
			ui.Text(c, a.fileEditError).TextColor(theme.TextMuted).TextAlign(ui.Center)
			if ui.Button(c, tr("Try again")).Clicked() {
				a.requestFileEdit()
			}
		})
		return
	}
	if a.fileEditState == nil {
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("No textual file")).TextColor(theme.TextMuted)
		})
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		if a.fileEditSaveError != "" {
			ui.Text(c, trf("Could not save the file: %s", a.fileEditSaveError)).
				TextColor(theme.Danger).FontSize(theme.FontSize - 1).Grow(1)
		} else {
			ui.Text(c, tr("Changes are saved to the file as you type.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		}
	})
	a.keyboardPane(c, "file-editor", editor.View(c, a.fileEditState).Grow(1))
}
