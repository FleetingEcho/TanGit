package app

import (
	"context"
	"fmt"
	"strings"
)

// A Replace All that changes closed files on disk can be taken back. Each
// changed file keeps only what was replaced: the spans of its new text and the
// original text they stood for, plus a hash of the new file. That is small even
// for a huge replacement, costs one hash per changed file while writing, and
// needs no extra reads or copies of the files themselves.

type codeSearchReplaceUndoEdit struct {
	start, end int
	text       string
}

type codeSearchReplaceUndoFile struct {
	path  string
	stamp localFileStamp
	edits []codeSearchReplaceUndoEdit
}

type codeSearchReplaceUndo struct {
	root  string
	files []codeSearchReplaceUndoFile
	bytes int
}

// codeSearchReplaceUndoLimit bounds what one replacement keeps. Past it the
// replacement still runs at full speed and simply offers no undo, rather than
// holding a partial one, which would restore some files and not others.
const codeSearchReplaceUndoLimit = 32 << 20

func newCodeSearchReplaceUndoFile(path, original string, changes workspaceSearchReplacement) (codeSearchReplaceUndoFile, int) {
	file := codeSearchReplaceUndoFile{
		path: path, stamp: stampLocalFile([]byte(changes.text)),
		edits: make([]codeSearchReplaceUndoEdit, 0, len(changes.edits)),
	}
	size, delta := 0, 0
	for _, edit := range changes.edits {
		start := edit.Start + delta
		file.edits = append(file.edits, codeSearchReplaceUndoEdit{
			start: start, end: start + len(edit.Text), text: strings.Clone(original[edit.Start:edit.End]),
		})
		size += edit.End - edit.Start + 32
		delta += len(edit.Text) - (edit.End - edit.Start)
	}
	return file, size
}

// restore puts the original text back, or says why the file was left alone.
func (file codeSearchReplaceUndoFile) restore(current string) (string, bool) {
	if stampLocalFile([]byte(current)) != file.stamp {
		return "", false
	}
	var out strings.Builder
	out.Grow(len(current))
	previous := 0
	for _, edit := range file.edits {
		if edit.start < previous || edit.end > len(current) {
			return "", false
		}
		out.WriteString(current[previous:edit.start])
		out.WriteString(edit.text)
		previous = edit.end
	}
	out.WriteString(current[previous:])
	return out.String(), true
}

// undoCodeSearchReplace restores the files the last Replace All wrote. A file
// changed since, or open with unsaved edits, is left as it is and reported.
func (a *app) undoCodeSearchReplace() {
	undo := a.code.searchReplaceUndo
	if len(undo.files) == 0 || a.code.searchReplaceRunning || undo.root != a.snapshot.Root || a.location.IsRemote() {
		return
	}
	dirty := make(map[string]bool)
	for _, name := range a.dirtyCodePaths() {
		dirty[name] = true
	}
	root, generation := undo.root, a.generation
	a.code.searchReplaceToken++
	token := a.code.searchReplaceToken
	a.code.searchReplaceRunning = true
	a.code.searchReplaceStatus = tr("Restoring files…")
	a.code.searchReplaceError = ""
	a.code.searchReplaceUndo = codeSearchReplaceUndo{}
	ctx, cancel := context.WithCancel(a.repositoryWork.Context())
	a.code.searchReplaceCancel = cancel
	go func() {
		defer a.recoverBackground()
		var restored []string
		var failures []string
		resolvedRoot, err := resolvedCodeSearchIndexRoot(root)
		for _, file := range undo.files {
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				failures = append(failures, fmt.Sprintf("%s: %s", file.path, safeError(err)))
				continue
			}
			if dirty[file.path] {
				failures = append(failures, fmt.Sprintf("%s: %s", file.path, tr("it has unsaved changes")))
				continue
			}
			filename, ok := resolveWorkspaceSearchPath(resolvedRoot, file.path)
			if !ok {
				continue
			}
			data, readErr := readWorkingFile(filename)
			if readErr != nil {
				failures = append(failures, fmt.Sprintf("%s: %s", file.path, safeError(readErr)))
				continue
			}
			original, ok := file.restore(string(data))
			if !ok {
				failures = append(failures, fmt.Sprintf("%s: %s", file.path, tr("it changed after the replacement")))
				continue
			}
			if writeErr := writeWorkingFile(filename, []byte(original)); writeErr != nil {
				failures = append(failures, fmt.Sprintf("%s: %s", file.path, safeError(writeErr)))
				continue
			}
			restored = append(restored, file.path)
		}
		a.updateChanged(func() bool {
			if token != a.code.searchReplaceToken || generation != a.generation || root != a.snapshot.Root {
				return false
			}
			for _, name := range restored {
				document := a.code.documents[name]
				if document == nil || document.loading || document.state == nil || codeDocumentModified(document) {
					continue
				}
				a.requestCodeDocument(document)
			}
			a.code.searchReplaceRunning = false
			a.code.searchReplaceCancel = nil
			a.code.searchReplaceStatus = trf("Restored %d files", len(restored))
			a.code.searchReplaceError = strings.Join(failures, "; ")
			a.invalidateCodeSearchIndex()
			if a.code.searchSubmitted != "" {
				a.beginCodeSearch()
			}
			if len(restored) > 0 {
				a.requestQuietRefresh()
			}
			return true
		})
	}()
}
