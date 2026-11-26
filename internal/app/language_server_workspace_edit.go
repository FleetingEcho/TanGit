package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

var (
	errLanguageServerWorkspaceResourceOperation = errors.New("language server requested an unsupported file operation")
	errLanguageServerWorkspaceEditOverlap       = errors.New("language server returned overlapping edits")
)

type languageServerWorkspaceDocumentEdit struct {
	URI     string
	Version *int64
	Edits   []languageServerTextEdit
}

type languageServerWorkspaceEdit struct {
	Documents []languageServerWorkspaceDocumentEdit
}

type languageServerWorkspaceTarget struct {
	path, filename   string
	original, result string
	stamp            localFileStamp
	document         *codeDocument
	state            *editor.State
	version          uint64
	readOnly         bool
	edits            []editor.Edit
}

// parseLanguageServerWorkspaceEdit accepts both WorkspaceEdit.changes and
// text-document entries of WorkspaceEdit.documentChanges. File create, rename
// and delete operations are rejected: symbol rename and quick fixes must never
// mutate the workspace through a path the Editor has not validated.
func parseLanguageServerWorkspaceEdit(raw json.RawMessage) (languageServerWorkspaceEdit, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return languageServerWorkspaceEdit{}, nil
	}
	var value struct {
		Changes         map[string][]languageServerTextEdit `json:"changes"`
		DocumentChanges []json.RawMessage                   `json:"documentChanges"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return languageServerWorkspaceEdit{}, err
	}
	byURI := make(map[string]*languageServerWorkspaceDocumentEdit)
	add := func(uri string, version *int64, edits []languageServerTextEdit) error {
		if strings.TrimSpace(uri) == "" {
			return errors.New("language server returned an edit without a document URI")
		}
		entry := byURI[uri]
		if entry == nil {
			entry = &languageServerWorkspaceDocumentEdit{URI: uri, Version: version}
			byURI[uri] = entry
		} else if version != nil {
			if entry.Version != nil && *entry.Version != *version {
				return fmt.Errorf("language server returned conflicting versions for %s", uri)
			}
			entry.Version = version
		}
		entry.Edits = append(entry.Edits, edits...)
		return nil
	}
	for uri, edits := range value.Changes {
		if err := add(uri, nil, edits); err != nil {
			return languageServerWorkspaceEdit{}, err
		}
	}
	for _, rawChange := range value.DocumentChanges {
		var kind struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(rawChange, &kind) != nil {
			return languageServerWorkspaceEdit{}, errors.New("language server returned an invalid document change")
		}
		if kind.Kind != "" {
			return languageServerWorkspaceEdit{}, fmt.Errorf("%w: %s", errLanguageServerWorkspaceResourceOperation, kind.Kind)
		}
		var change struct {
			TextDocument struct {
				URI     string `json:"uri"`
				Version *int64 `json:"version"`
			} `json:"textDocument"`
			Edits []languageServerTextEdit `json:"edits"`
		}
		if err := json.Unmarshal(rawChange, &change); err != nil {
			return languageServerWorkspaceEdit{}, err
		}
		if err := add(change.TextDocument.URI, change.TextDocument.Version, change.Edits); err != nil {
			return languageServerWorkspaceEdit{}, err
		}
	}
	result := languageServerWorkspaceEdit{Documents: make([]languageServerWorkspaceDocumentEdit, 0, len(byURI))}
	for _, document := range byURI {
		result.Documents = append(result.Documents, *document)
	}
	sort.Slice(result.Documents, func(i, j int) bool { return result.Documents[i].URI < result.Documents[j].URI })
	return result, nil
}

// planLanguageServerTextEdits converts the protocol's UTF-16 positions to the
// editor's byte offsets against one immutable source. Buffer.Edit applies the
// returned edits as one undo step.
func planLanguageServerTextEdits(text string, edits []languageServerTextEdit) ([]editor.Edit, error) {
	planned := make([]editor.Edit, 0, len(edits))
	for _, edit := range edits {
		start, startOK := languageServerExactOffsetAt(text, edit.Range.Start)
		end, endOK := languageServerExactOffsetAt(text, edit.Range.End)
		if !startOK || !endOK || end < start {
			return nil, errors.New("language server returned an invalid edit range")
		}
		planned = append(planned, editor.Edit{Start: start, End: end, Text: edit.NewText})
	}
	slices.SortStableFunc(planned, func(a, b editor.Edit) int {
		if a.Start != b.Start {
			return a.Start - b.Start
		}
		return a.End - b.End
	})
	for i := 1; i < len(planned); i++ {
		if planned[i].Start < planned[i-1].End {
			return nil, errLanguageServerWorkspaceEditOverlap
		}
	}
	return planned, nil
}

func applyLanguageServerTextEdits(text string, edits []editor.Edit) string {
	if len(edits) == 0 {
		return text
	}
	var out strings.Builder
	at := 0
	for _, edit := range edits {
		out.WriteString(text[at:edit.Start])
		out.WriteString(edit.Text)
		at = edit.End
	}
	out.WriteString(text[at:])
	return out.String()
}

func validateLanguageServerWorkspaceTarget(root, path, filename string) error {
	if !validCodeManagedPath(path) {
		return fmt.Errorf("language server edit is not a managed workspace file: %s", path)
	}
	if err := checkCodeParentDirectories(root, path); err != nil {
		return err
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("language server edit is not a regular file: %s", path)
	}
	return nil
}

// applyLanguageServerWorkspaceEdit is the seam shared by rename and code
// actions. Protocol parsing, UTF-16 conversion, stale-buffer validation,
// background I/O and rollback all stay behind this one call.
func (a *app) applyLanguageServerWorkspaceEdit(raw json.RawMessage, label string, done func(bool)) {
	finish := func(ok bool) {
		if done != nil {
			done(ok)
		}
	}
	if a.location.IsRemote() || a.snapshot.Root == "" || a.code.languageEditRunning {
		finish(false)
		return
	}
	workspaceEdit, err := parseLanguageServerWorkspaceEdit(raw)
	if err != nil {
		a.notify("error", tr("Could not apply language server edit"), safeError(err))
		finish(false)
		return
	}
	if len(workspaceEdit.Documents) == 0 {
		finish(true)
		return
	}
	a.code.languageEditSeq++
	sequence := a.code.languageEditSeq
	generation, root := a.generation, a.snapshot.Root
	a.code.languageEditRunning = true
	targets := make([]languageServerWorkspaceTarget, 0, len(workspaceEdit.Documents))
	for _, change := range workspaceEdit.Documents {
		path, ok := languageServerRepositoryPath(root, change.URI)
		if !ok {
			err = fmt.Errorf("language server edit is outside this workspace: %s", change.URI)
			break
		}
		target := languageServerWorkspaceTarget{path: path, filename: a.workingFilePath(path)}
		if err = validateLanguageServerWorkspaceTarget(root, path, target.filename); err != nil {
			break
		}
		if document := a.code.documents[path]; document != nil && document.state != nil && document.state.Buffer != nil {
			if document.readOnly {
				err = fmt.Errorf("%s is read-only", path)
				break
			}
			target.document, target.state = document, document.state
			target.version = document.state.Buffer.Version()
			if change.Version != nil && (*change.Version < 0 || uint64(*change.Version) != target.version) {
				err = fmt.Errorf("%s changed before the language server edit could be applied", path)
				break
			}
			target.original = document.state.Buffer.String()
			target.edits, err = planLanguageServerTextEdits(target.original, change.Edits)
		}
		if err != nil {
			break
		}
		targets = append(targets, target)
	}
	if err != nil {
		a.code.languageEditRunning = false
		a.notify("error", tr("Could not apply language server edit"), safeError(err))
		finish(false)
		return
	}
	go func() {
		defer a.recoverBackground()
		var workerErr error
		for i, change := range workspaceEdit.Documents {
			target := &targets[i]
			if target.state != nil {
				target.result = applyLanguageServerTextEdits(target.original, target.edits)
				continue
			}
			if change.Version != nil {
				workerErr = fmt.Errorf("%s has a version but is not open", target.path)
				break
			}
			if workerErr = validateLanguageServerWorkspaceTarget(root, target.path, target.filename); workerErr != nil {
				break
			}
			content, readErr := readWorkingFile(target.filename)
			if readErr != nil {
				workerErr = readErr
				break
			}
			target.original = string(content)
			target.stamp = stampLocalFile(content)
			target.edits, workerErr = planLanguageServerTextEdits(target.original, change.Edits)
			if workerErr != nil {
				break
			}
			target.result = applyLanguageServerTextEdits(target.original, target.edits)
		}
		a.updateChanged(func() bool {
			if sequence != a.code.languageEditSeq || generation != a.generation || root != a.snapshot.Root {
				if sequence == a.code.languageEditSeq {
					a.code.languageEditRunning = false
				}
				finish(false)
				return false
			}
			if workerErr != nil {
				a.code.languageEditRunning = false
				a.notify("error", tr("Could not apply language server edit"), safeError(workerErr))
				finish(false)
				return true
			}
			for i := range targets {
				target := &targets[i]
				if target.state == nil {
					continue
				}
				if a.code.documents[target.path] != target.document || target.document.state != target.state ||
					target.state.Buffer == nil || target.state.Buffer.Version() != target.version ||
					target.state.Buffer.String() != target.original {
					a.code.languageEditRunning = false
					a.notify("error", tr("Could not apply language server edit"), trf("%s changed before the edit could be applied", target.path))
					finish(false)
					return true
				}
				target.readOnly = target.state.ReadOnly
				target.state.ReadOnly = true
			}
			go a.commitLanguageServerWorkspaceEdit(sequence, generation, root, label, targets, finish)
			return true
		})
	}()
}

func (a *app) commitLanguageServerWorkspaceEdit(sequence, generation uint64, root, label string,
	targets []languageServerWorkspaceTarget, done func(bool)) {
	defer a.recoverBackground()
	written := make([]int, 0, len(targets))
	var commitErr error
	for i := range targets {
		target := &targets[i]
		if err := validateLanguageServerWorkspaceTarget(root, target.path, target.filename); err != nil {
			commitErr = fmt.Errorf("%s: %w", target.path, err)
			break
		}
		if target.state != nil || target.result == target.original {
			continue
		}
		if err := writeWorkingFileChecked(target.filename, []byte(target.result), target.stamp, true); err != nil {
			commitErr = fmt.Errorf("%s: %w", target.path, err)
			break
		}
		written = append(written, i)
	}
	if commitErr != nil {
		for i := len(written) - 1; i >= 0; i-- {
			target := targets[written[i]]
			if err := validateLanguageServerWorkspaceTarget(root, target.path, target.filename); err != nil {
				commitErr = errors.Join(commitErr, fmt.Errorf("restore %s: %w", target.path, err))
				continue
			}
			resultStamp := stampLocalFile([]byte(target.result))
			if err := writeWorkingFileChecked(target.filename, []byte(target.original), resultStamp, true); err != nil {
				commitErr = errors.Join(commitErr, fmt.Errorf("restore %s: %w", target.path, err))
			}
		}
	}
	a.updateChanged(func() bool {
		valid := sequence == a.code.languageEditSeq && generation == a.generation && root == a.snapshot.Root
		for i := range targets {
			if targets[i].state != nil {
				targets[i].state.ReadOnly = targets[i].readOnly
			}
		}
		a.code.languageEditRunning = false
		if !valid {
			done(false)
			return false
		}
		if commitErr != nil {
			a.notify("error", tr("Could not apply language server edit"), safeError(commitErr))
			done(false)
			return true
		}
		for i := range targets {
			target := &targets[i]
			if target.state != nil && target.state.Buffer != nil {
				target.state.Buffer.Edit(target.edits...)
				continue
			}
			if document := a.code.documents[target.path]; document != nil {
				if codeDocumentModified(document) {
					document.saveError = tr("The file changed while a language server edit was being applied")
					continue
				}
				a.reloadCodeDocument(document)
			}
		}
		a.invalidateCodeSearchIndex()
		a.requestQuietRefresh()
		if strings.TrimSpace(label) != "" {
			a.notify("success", label, trf("Updated %d files", len(targets)))
		}
		done(true)
		return true
	})
}

func languageServerExactOffsetAt(text string, position languageServerPosition) (int, bool) {
	if position.Line < 0 || position.Character < 0 {
		return 0, false
	}
	lineStart := 0
	for range position.Line {
		next := strings.IndexByte(text[lineStart:], '\n')
		if next < 0 {
			return 0, false
		}
		lineStart += next + 1
	}
	lineEnd := strings.IndexByte(text[lineStart:], '\n')
	if lineEnd < 0 {
		lineEnd = len(text) - lineStart
	}
	line := text[lineStart : lineStart+lineEnd]
	offset := languageServerUTF16ColumnToByte(line, position.Character)
	if languageServerPositionAt(line, offset).Character != position.Character {
		return 0, false
	}
	return lineStart + offset, true
}

// languageServerLineOffsets shares one line-start index across a complete LSP
// response. Re-running languageServerExactOffsetAt for every semantic token
// would repeatedly scan the document prefix and make large responses quadratic.
type languageServerLineOffsets struct {
	text   string
	starts []int
}

func indexLanguageServerLines(text string) languageServerLineOffsets {
	index := languageServerLineOffsets{text: text, starts: make([]int, 1, strings.Count(text, "\n")+1)}
	for offset, char := range []byte(text) {
		if char == '\n' {
			index.starts = append(index.starts, offset+1)
		}
	}
	return index
}

func (i languageServerLineOffsets) exactOffsetAt(position languageServerPosition) (int, bool) {
	if position.Line < 0 || position.Character < 0 || position.Line >= len(i.starts) {
		return 0, false
	}
	lineStart := i.starts[position.Line]
	lineEnd := len(i.text) - lineStart
	if next := strings.IndexByte(i.text[lineStart:], '\n'); next >= 0 {
		lineEnd = next
	}
	line := i.text[lineStart : lineStart+lineEnd]
	offset := languageServerUTF16ColumnToByte(line, position.Character)
	if languageServerPositionAt(line, offset).Character != position.Character {
		return 0, false
	}
	return lineStart + offset, true
}
