package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type languageServerFormattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

type languageServerFormattingParams struct {
	TextDocument languageServerTextDocument      `json:"textDocument"`
	Options      languageServerFormattingOptions `json:"options"`
}

type languageServerRangeFormattingParams struct {
	TextDocument languageServerTextDocument      `json:"textDocument"`
	Range        languageServerRange             `json:"range"`
	Options      languageServerFormattingOptions `json:"options"`
}

type languageServerTextEdit struct {
	Range   languageServerRange `json:"range"`
	NewText string              `json:"newText"`
}

type languageServerFormattingTarget struct {
	name       string
	connection languageServerConnection
	sync       func()
	valid      func() bool
}

func (a *app) codeDocumentContextMenu(menu *ui.Menu, document *codeDocument) {
	disabled := document == nil || document.state == nil || document.state.Buffer == nil ||
		document.readOnly || document.state.ReadOnly || document.formatting
	if menu.Item(tr("Quick Fix")).Disabled(disabled).Chosen() {
		a.requestLanguageCodeActions(document)
	}
	if menu.Item(tr("Rename Symbol")).Shortcut(0, ui.KeyF2).Disabled(disabled || a.location.IsRemote()).Chosen() {
		a.openLanguageRename(document)
	}
	if menu.Item(tr("Find References")).Disabled(document == nil || document.state == nil).Chosen() {
		a.requestLanguageReferences(document)
	}
	if menu.Item(tr("Peek Definition")).Shortcut(ui.Alt, ui.KeyF12).Disabled(document == nil || document.state == nil).Chosen() {
		a.requestLanguagePeekDefinition(document)
	}
	menu.Separator()
	if menu.Item(tr("Format Document")).Disabled(disabled).Chosen() {
		a.formatCodeDocument(document)
	}
	selectionDisabled := disabled
	if !disabled {
		selections := document.state.Selections()
		selectionDisabled = len(selections) != 1 || selections[0].Empty()
	}
	if menu.Item(tr("Format Selection")).Disabled(selectionDisabled).Chosen() {
		a.formatCodeSelection(document)
	}
	if a.historyChangesAvailable() && document != nil && document.state != nil && document.state.Buffer != nil {
		selection := document.state.Selection()
		if menu.Item(tr(historyChangesLabel)).Disabled(selection.Empty()).Chosen() {
			selected := document.state.SelectedText()
			a.openHistoryChanges(&repository.HistoryChangeQuery{
				Text: selected, Path: document.path, Occurrences: strings.Contains(selected, "\n"),
			})
		}
	}
}

type codeFormattingRequest struct {
	document  *codeDocument
	selection bool
	start     int
	end       int
	text      string
	filename  string
	root      string
	version   uint64
	sequence  uint64
}

func (a *app) prepareCodeFormatting(document *codeDocument, selection bool) (codeFormattingRequest, bool) {
	if document == nil || document.state == nil || document.state.Buffer == nil || document.formatting ||
		document.readOnly || document.state.ReadOnly || a.code.documents[document.path] != document {
		return codeFormattingRequest{}, false
	}
	request := codeFormattingRequest{
		document: document, selection: selection, text: document.state.Buffer.String(),
		filename: a.workingFilePath(document.path), root: a.snapshot.Root,
		version: document.state.Buffer.Version(), end: document.state.Buffer.Len(),
	}
	if selection {
		selections := document.state.Selections()
		if len(selections) != 1 || selections[0].Empty() {
			return codeFormattingRequest{}, false
		}
		request.start, request.end = selections[0].Start(), selections[0].End()
	}
	return request, true
}

func (a *app) formatCodeDocument(document *codeDocument) {
	request, ok := a.prepareCodeFormatting(document, false)
	if ok {
		a.formatCodeRange(request)
	}
}

func (a *app) formatCodeSelection(document *codeDocument) {
	request, ok := a.prepareCodeFormatting(document, true)
	if ok {
		a.formatCodeRange(request)
	}
}

// formatCodeRange gives an explicitly configured external formatter priority.
// Otherwise it chooses one server advertising the required capability. Unlike
// hover and completion, formatting must never fan out to every server: combining
// independently produced edits would corrupt the buffer.
func (a *app) formatCodeRange(request codeFormattingRequest) {
	document := request.document
	if formatter, ok := a.externalCodeFormatter(document); ok {
		document.formatSeq++
		request.sequence = document.formatSeq
		document.formatting = true
		go func() {
			defer a.recoverBackground()
			input := request.text[request.start:request.end]
			output, err := runCodeFormatter(formatter.command, request.filename, request.root, input)
			var edits []editor.Edit
			if err == nil && input != "" && output == "" {
				err = errCodeFormatterEmptyOutput
			}
			if err == nil && output != input {
				edits = []editor.Edit{{Start: request.start, End: request.end, Text: output}}
			}
			a.completeCodeFormatting(request, formatter.name, nil, edits, err)
		}()
		return
	}
	target, ok := a.languageServerFormattingTarget(document, request.selection)
	if !ok {
		if request.selection {
			a.notify("info", tr("No formatter available"),
				trf("Configure a formatter or enable a language server with range formatting support for %s in Settings.", filepath.Base(document.path)))
		} else {
			a.notify("info", tr("No formatter available"),
				trf("Enable a language server with formatting support for %s in Settings.", filepath.Base(document.path)))
		}
		return
	}
	target.sync()
	document.formatSeq++
	request.sequence = document.formatSeq
	document.formatting = true
	method := "textDocument/formatting"
	var params any = languageServerFormattingParams{
		TextDocument: languageServerTextDocument{URI: languageServerFileURI(a.workingFilePath(document.path))},
		Options:      languageServerDocumentFormattingOptions(document),
	}
	if request.selection {
		method = "textDocument/rangeFormatting"
		params = languageServerRangeFormattingParams{
			TextDocument: languageServerTextDocument{URI: languageServerFileURI(a.workingFilePath(document.path))},
			Range: languageServerRange{
				Start: languageServerPositionAt(request.text, request.start),
				End:   languageServerPositionAt(request.text, request.end),
			},
			Options: languageServerDocumentFormattingOptions(document),
		}
	}
	target.connection.Request(method, params, func(raw json.RawMessage, err error) {
		var edits []editor.Edit
		if err == nil {
			edits, err = parseLanguageServerFormattingEdits(raw, request.text)
		}
		a.completeCodeFormatting(request, target.name, target.valid, edits, err)
	})
}

func (a *app) completeCodeFormatting(request codeFormattingRequest, name string, valid func() bool, edits []editor.Edit, err error) {
	document := request.document
	a.updateChanged(func() bool {
		if a.code.documents[document.path] != document || document.formatSeq != request.sequence {
			return false
		}
		document.formatting = false
		if document.state == nil || document.state.Buffer == nil || valid != nil && !valid() {
			return true
		}
		// Typing while a formatter works retires its result. Applying output
		// calculated for the old buffer could overwrite the user's new text.
		if document.state.Buffer.Version() != request.version {
			return true
		}
		if err != nil {
			a.notify("error", trf("%s formatting failed", name), safeError(err))
			return true
		}
		if len(edits) > 0 {
			document.state.Buffer.Edit(edits...)
			if a.code.preview == document.path {
				a.code.preview = ""
			}
		}
		return true
	})
}

func (a *app) languageServerFormattingTarget(document *codeDocument, selection bool) (languageServerFormattingTarget, bool) {
	supported := func(connection languageServerConnection) bool {
		if selection {
			return connection.DocumentRangeFormatting()
		}
		return connection.DocumentFormatting()
	}
	if strings.EqualFold(filepath.Ext(document.path), ".go") {
		connection := a.lsp.goServer
		if connection == nil || !supported(connection) {
			return languageServerFormattingTarget{}, false
		}
		return languageServerFormattingTarget{
			name: "gopls", connection: connection,
			sync:  func() { a.syncGoLanguageServerDocument(document.path, document) },
			valid: func() bool { return a.lsp.goServer == connection },
		}, true
	}
	for _, server := range a.additionalLanguageServersForDocument(document) {
		connection := server.connection
		if connection == nil || !supported(connection) {
			continue
		}
		return languageServerFormattingTarget{
			name: server.profile.Name, connection: connection,
			sync: func() { a.syncManagedLanguageServerDocument(server, document.path, document) },
			valid: func() bool {
				return a.lsp.additional[server.profile.ID] == server && server.connection == connection
			},
		}, true
	}
	return languageServerFormattingTarget{}, false
}

func languageServerDocumentFormattingOptions(document *codeDocument) languageServerFormattingOptions {
	options := languageServerFormattingOptions{TabSize: document.state.TabSize}
	language := document.state.Buffer.Language()
	if options.TabSize <= 0 {
		options.TabSize = 4
		if language != nil && language.TabSize > 0 {
			options.TabSize = language.TabSize
		}
	}
	switch document.state.Indent {
	case editor.IndentTabs:
		options.InsertSpaces = false
	case editor.IndentSpaces:
		options.InsertSpaces = true
	default:
		options.InsertSpaces = language == nil || !language.HardTabs
	}
	return options
}

func parseLanguageServerFormattingEdits(raw json.RawMessage, text string) ([]editor.Edit, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var replacements []languageServerTextEdit
	if err := json.Unmarshal(raw, &replacements); err != nil {
		return nil, fmt.Errorf("invalid formatting response: %w", err)
	}
	edits := make([]editor.Edit, 0, len(replacements))
	for _, replacement := range replacements {
		start := languageServerOffsetAt(text, replacement.Range.Start)
		end := languageServerOffsetAt(text, replacement.Range.End)
		if end < start {
			return nil, errors.New("invalid formatting response: edit range is reversed")
		}
		edits = append(edits, editor.Edit{Start: start, End: end, Text: replacement.NewText})
	}
	slices.SortStableFunc(edits, func(left, right editor.Edit) int { return left.Start - right.Start })
	previousEnd := -1
	for _, edit := range edits {
		if edit.Start < previousEnd {
			return nil, errors.New("invalid formatting response: edits overlap")
		}
		previousEnd = edit.End
	}
	return edits, nil
}
