package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type codeLanguageDialog uint8

const (
	codeLanguageDialogNone codeLanguageDialog = iota
	codeLanguageDialogRename
	codeLanguageDialogActions
	codeLanguageDialogReferences
	codeLanguageDialogSymbols
	codeLanguageDialogPeek
)

type languageServerCodeAction struct {
	Title    string          `json:"title"`
	Kind     string          `json:"kind"`
	Edit     json.RawMessage `json:"edit"`
	Command  json.RawMessage `json:"command"`
	Disabled *struct {
		Reason string `json:"reason"`
	} `json:"disabled"`
}

type languageServerCodeActionParams struct {
	TextDocument languageServerTextDocument `json:"textDocument"`
	Range        languageServerRange        `json:"range"`
	Context      struct {
		Diagnostics []json.RawMessage `json:"diagnostics"`
	} `json:"context"`
}

type languageServerRenameParams struct {
	TextDocument languageServerTextDocument `json:"textDocument"`
	Position     languageServerPosition     `json:"position"`
	NewName      string                     `json:"newName"`
}

type languageServerReferenceParams struct {
	TextDocument languageServerTextDocument `json:"textDocument"`
	Position     languageServerPosition     `json:"position"`
	Context      struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

func codeLanguageOffset(document *codeDocument) (int, bool) {
	if document == nil || document.state == nil || document.state.Buffer == nil {
		return 0, false
	}
	selections := document.state.Selections()
	if len(selections) == 0 {
		return 0, false
	}
	return selections[len(selections)-1].Head, true
}

func languageServerCodeActionDiagnostics(document *codeDocument, text string, start, end int) []json.RawMessage {
	diagnostics := make([]json.RawMessage, 0)
	if document == nil || document.state == nil {
		return diagnostics
	}
	for _, diagnostic := range document.state.Diagnostics() {
		if diagnostic.End < start || diagnostic.Start > end {
			continue
		}
		raw, err := json.Marshal(struct {
			Range    languageServerRange `json:"range"`
			Severity int                 `json:"severity"`
			Message  string              `json:"message"`
			Source   string              `json:"source,omitempty"`
		}{
			Range: languageServerRange{
				Start: languageServerPositionAt(text, diagnostic.Start),
				End:   languageServerPositionAt(text, diagnostic.End),
			},
			Severity: int(diagnostic.Severity) + 1,
			Message:  diagnostic.Message,
			Source:   diagnostic.Source,
		})
		if err == nil {
			diagnostics = append(diagnostics, raw)
		}
	}
	return diagnostics
}

// languageServerActionRequest sends an editor command to the one language
// server that owns the document and drops replies for stale buffer versions.
func (a *app) languageServerActionRequest(document *codeDocument, sequence *uint64, method string,
	params any, result func(json.RawMessage, error) bool) {
	if document == nil || document.state == nil || document.state.Buffer == nil ||
		a.code.documents[document.path] != document {
		return
	}
	version := document.state.Buffer.Version()
	*sequence++
	want := *sequence
	deliver := func(valid func() bool, raw json.RawMessage, err error) {
		a.updateChanged(func() bool {
			if !valid() || a.code.documents[document.path] != document || document.state == nil ||
				document.state.Buffer == nil || document.state.Buffer.Version() != version || *sequence != want {
				if method == "textDocument/definition" && *sequence == want && a.code.languageDialog == codeLanguageDialogPeek &&
					a.code.languageDocument == document {
					a.code.languagePeekLoading = false
					a.code.languageError = tr("No definition found")
				}
				return false
			}
			return result(raw, err)
		})
	}
	if strings.EqualFold(filepath.Ext(document.path), ".go") {
		connection := a.lsp.goServer
		if connection == nil {
			result(nil, errors.New("Go language server is not running"))
			return
		}
		a.syncGoLanguageServerDocument(document.path, document)
		connection.Request(method, params, func(raw json.RawMessage, err error) {
			deliver(func() bool { return a.lsp.goServer == connection }, raw, err)
		})
		return
	}
	servers := a.additionalLanguageServersForDocument(document)
	if len(servers) == 0 {
		result(nil, errors.New("language server is not running"))
		return
	}
	server := servers[0]
	a.syncManagedLanguageServerDocument(server, document.path, document)
	connection := server.connection
	connection.Request(method, params, func(raw json.RawMessage, err error) {
		deliver(func() bool { return server.connection == connection }, raw, err)
	})
}

func (a *app) openLanguageRename(document *codeDocument) {
	offset, ok := codeLanguageOffset(document)
	if !ok || document.readOnly || a.location.IsRemote() {
		return
	}
	text := document.state.Buffer.String()
	start, end := languageServerWordRange(text, offset)
	a.closeCodeLanguageDialog()
	a.code.languageDialog = codeLanguageDialogRename
	a.code.languageDocument = document
	a.code.languageOffset = offset
	a.code.languageRename = text[start:end]
	a.code.languageError = ""
}

func (a *app) submitLanguageRename() {
	document := a.code.languageDocument
	name := strings.TrimSpace(a.code.languageRename)
	if name == "" || document == nil || document.state == nil || document.state.Buffer == nil {
		a.code.languageError = tr("Enter a new symbol name")
		return
	}
	text := document.state.Buffer.String()
	params := languageServerRenameParams{
		TextDocument: languageServerTextDocument{URI: languageServerFileURI(a.workingFilePath(document.path))},
		Position:     languageServerPositionAt(text, a.code.languageOffset), NewName: name,
	}
	a.code.languageError = ""
	a.languageServerActionRequest(document, &document.languageRenameSeq, "textDocument/rename", params,
		func(raw json.RawMessage, err error) bool {
			if err != nil {
				a.code.languageError = safeError(err)
				return true
			}
			a.closeCodeLanguageDialog()
			a.applyLanguageServerWorkspaceEdit(raw, tr("Symbol renamed"), nil)
			return true
		})
}

func (a *app) requestLanguageCodeActions(document *codeDocument) {
	offset, ok := codeLanguageOffset(document)
	if !ok || document.readOnly || a.location.IsRemote() {
		return
	}
	text := document.state.Buffer.String()
	start, end := offset, offset
	if selections := document.state.Selections(); len(selections) > 0 {
		selection := selections[len(selections)-1]
		start, end = min(selection.Anchor, selection.Head), max(selection.Anchor, selection.Head)
	}
	params := languageServerCodeActionParams{
		TextDocument: languageServerTextDocument{URI: languageServerFileURI(a.workingFilePath(document.path))},
		Range:        languageServerRange{Start: languageServerPositionAt(text, start), End: languageServerPositionAt(text, end)},
	}
	params.Context.Diagnostics = languageServerCodeActionDiagnostics(document, text, start, end)
	a.closeCodeLanguageDialog()
	a.code.languageDialog = codeLanguageDialogActions
	a.code.languageDocument = document
	a.code.languageActions = nil
	a.code.languageError = ""
	a.languageServerActionRequest(document, &document.languageActionSeq, "textDocument/codeAction", params,
		func(raw json.RawMessage, err error) bool {
			if err != nil {
				a.code.languageError = safeError(err)
				return true
			}
			var actions []languageServerCodeAction
			if len(raw) != 0 && string(raw) != "null" && json.Unmarshal(raw, &actions) != nil {
				a.code.languageError = tr("Language server returned invalid code actions")
				return true
			}
			a.code.languageActions = actions
			return true
		})
}

func (a *app) applyLanguageCodeAction(action languageServerCodeAction) {
	if action.Disabled != nil {
		return
	}
	if (len(action.Command) != 0 && string(action.Command) != "null") || len(action.Edit) == 0 || string(action.Edit) == "null" {
		a.code.languageError = tr("This code action is a command the editor cannot run yet")
		return
	}
	a.closeCodeLanguageDialog()
	a.applyLanguageServerWorkspaceEdit(action.Edit, action.Title, nil)
}

func (a *app) requestLanguageReferences(document *codeDocument) {
	offset, ok := codeLanguageOffset(document)
	if !ok {
		return
	}
	text := document.state.Buffer.String()
	params := languageServerReferenceParams{
		TextDocument: languageServerTextDocument{URI: languageServerFileURI(a.workingFilePath(document.path))},
		Position:     languageServerPositionAt(text, offset),
	}
	params.Context.IncludeDeclaration = true
	a.closeCodeLanguageDialog()
	a.code.languageDialog = codeLanguageDialogReferences
	a.code.languageDocument = document
	a.code.languageReferences = nil
	a.code.languagePeekPath = ""
	a.code.languagePeekState = nil
	a.code.languagePeekLoading = false
	a.code.languageError = ""
	a.languageServerActionRequest(document, &document.languageReferenceSeq, "textDocument/references", params,
		func(raw json.RawMessage, err error) bool {
			if err != nil {
				a.code.languageError = safeError(err)
				return true
			}
			var locations []languageServerLocation
			if len(raw) != 0 && string(raw) != "null" && json.Unmarshal(raw, &locations) != nil {
				a.code.languageError = tr("Language server returned invalid references")
				return true
			}
			a.code.languageReferences = locations
			return true
		})
}

func (a *app) requestLanguagePeekDefinition(document *codeDocument) {
	offset, ok := codeLanguageOffset(document)
	if !ok {
		return
	}
	text := document.state.Buffer.String()
	params := languageServerTextPosition{
		TextDocument: languageServerTextDocument{URI: languageServerFileURI(a.workingFilePath(document.path))},
		Position:     languageServerPositionAt(text, offset),
	}
	a.closeCodeLanguageDialog()
	a.code.languageDialog = codeLanguageDialogPeek
	a.code.languageDocument = document
	a.code.languagePeekPath = ""
	a.code.languagePeekLoading = true
	a.code.languagePeekState = nil
	a.code.languageError = ""
	a.languageServerActionRequest(document, &document.definitionSeq, "textDocument/definition", params,
		func(raw json.RawMessage, err error) bool {
			a.code.languagePeekLoading = false
			if err != nil {
				a.code.languageError = safeError(err)
				return true
			}
			location, found := firstLanguageServerLocation(raw)
			if !found {
				a.code.languageError = tr("No definition found")
				return true
			}
			uri, position := location.URI, location.Range.Start
			if location.TargetURI != "" {
				uri, position = location.TargetURI, location.TargetSelectionRange.Start
			}
			filename, valid := languageServerFilePath(uri)
			if !valid {
				a.code.languageError = tr("Definition is not a local file")
				return true
			}
			a.code.languageDialog = codeLanguageDialogPeek
			a.code.languagePeekPath = filename
			a.code.languagePeekLine = position.Line
			a.code.languagePeekLoading = true
			a.code.languagePeekState = nil
			a.code.languageError = ""
			definitionSequence := document.definitionSeq
			go func() {
				defer a.recoverBackground()
				content, readErr := readWorkingFile(filename)
				a.updateChanged(func() bool {
					if a.code.languageDialog != codeLanguageDialogPeek || a.code.languageDocument != document ||
						a.code.languagePeekPath != filename || document.definitionSeq != definitionSequence {
						return false
					}
					a.code.languagePeekLoading = false
					if readErr != nil {
						a.code.languageError = safeError(readErr)
						return true
					}
					buffer := editor.NewBuffer(string(content))
					buffer.SetLanguage(editor.LanguageFor(filename))
					state := &editor.State{Buffer: buffer, ReadOnly: true, SoftWrap: true, LineNumbers: editor.LineNumbersOn, Label: filename}
					state.ScrollTo(position.Line)
					a.code.languagePeekState = state
					return true
				})
			}()
			return true
		})
}

func (a *app) openLanguageReference(location languageServerLocation) {
	uri, position := location.URI, location.Range.Start
	if location.TargetURI != "" {
		uri, position = location.TargetURI, location.TargetSelectionRange.Start
	}
	name, ok := languageServerFilePath(uri)
	if !ok {
		return
	}
	a.closeCodeLanguageDialog()
	if path, inside := languageServerPathWithinRepository(a.snapshot.Root, name); inside {
		a.openCodeLanguageServerLocation(path, position)
	} else {
		a.openExternalCodeLanguageServerLocation(name, position)
	}
}

func languageServerLocationLabel(root string, location languageServerLocation) string {
	uri, position := location.URI, location.Range.Start
	if location.TargetURI != "" {
		uri, position = location.TargetURI, location.TargetSelectionRange.Start
	}
	name, ok := languageServerFilePath(uri)
	if !ok {
		return fmt.Sprintf("%s:%d:%d", uri, position.Line+1, position.Character+1)
	}
	if path, inside := languageServerPathWithinRepository(root, name); inside {
		name = path
	}
	return fmt.Sprintf("%s:%d:%d", name, position.Line+1, position.Character+1)
}

func (a *app) closeCodeLanguageDialog() {
	if document := a.code.languageDocument; document != nil {
		document.languageRenameSeq++
		document.languageActionSeq++
		document.languageReferenceSeq++
		document.definitionSeq++
		document.symbolSeq++
		document.symbolsRequested = false
		document.symbolsLoading = false
		document.symbols = nil
	}
	a.code.languageDialog = codeLanguageDialogNone
	a.code.languageDocument = nil
	a.code.languageRename = ""
	a.code.languageError = ""
	// A symbol row closes this dialog while its owning list still has input
	// to finish. Keep that list's frame alive; openLanguageSymbols resets all
	// transient view state when the whole modal can safely be built again.
	a.code.languageActions = nil
	a.code.languageReferences = nil
	a.code.languagePeekPath = ""
	a.code.languagePeekState = nil
	a.code.languagePeekLoading = false
}

func (a *app) openLanguageSymbols(document *codeDocument) {
	if document == nil || document.state == nil {
		return
	}
	a.closeCodeLanguageDialog()
	a.code.languageDialog = codeLanguageDialogSymbols
	a.code.languageDocument = document
	a.code.languageError = ""
	a.code.languageSymbols = languageSymbolsViewState{selected: -1, reveal: true}
	version := document.state.Buffer.Version()
	if !document.symbolsRequested || document.symbolVersion != version {
		document.symbolVersion = version
		document.symbolsRequested = true
		document.symbolsLoading = true
		uri := languageServerFileURI(a.workingFilePath(document.path))
		a.languageServerActionRequest(document, &document.symbolSeq, "textDocument/documentSymbol",
			map[string]any{"textDocument": languageServerTextDocument{URI: uri}}, func(raw json.RawMessage, err error) bool {
				document.symbolsLoading = false
				if err != nil {
					document.symbolsRequested = false
					a.code.languageError = safeError(err)
					return true
				}
				document.symbols = parseLanguageServerDocumentSymbols(raw)
				return true
			})
	}
}

func (a *app) codeLanguageDialogView(c *ui.Context) {
	if a.code.languageDialog == codeLanguageDialogNone {
		return
	}
	if a.code.languageDialog == codeLanguageDialogSymbols {
		a.languageSymbolDialogView(c)
		return
	}
	theme := c.Theme()
	title, subtitle := "Rename Symbol", "Rename across the workspace"
	if a.code.languageDialog == codeLanguageDialogActions {
		title, subtitle = "Quick Fix", "Actions offered by the language server"
	} else if a.code.languageDialog == codeLanguageDialogReferences {
		title, subtitle = "References", "Locations reported by the language server"
	} else if a.code.languageDialog == codeLanguageDialogPeek {
		title, subtitle = "Peek Definition", a.code.languagePeekPath
	}
	ui.Overlay(c, func() {
		c.Key("code-language-dialog")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(560).MaxWidthPercent(92).MaxHeightPercent(82).Padding(22).Gap(14).
				Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr(title))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, editIcon).FontSize(18).TextColor(theme.Accent)
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr(title)).FontSize(17).Bold()
						ui.Text(c, tr(subtitle)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close language action", "Escape", closeIcon, false, a.closeCodeLanguageDialog)
				})
				switch a.code.languageDialog {
				case codeLanguageDialogRename:
					input := ui.TextInput(c, &a.code.languageRename).Label(tr("New symbol name")).AutoFocus()
					if input.Submitted() {
						a.submitLanguageRename()
					}
					ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
						if ui.Button(c, tr("Cancel")).Clicked() {
							a.closeCodeLanguageDialog()
						}
						if ui.PrimaryButton(c, tr("Rename")).Clicked() {
							a.submitLanguageRename()
						}
					})
				case codeLanguageDialogActions:
					if len(a.code.languageActions) == 0 && a.code.languageError == "" {
						ui.Text(c, tr("No code actions available")).TextColor(theme.TextMuted)
					}
					ui.Scroll(c).MaxHeight(420).Children(func() {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							for _, action := range a.code.languageActions {
								action := action
								disabled := action.Disabled != nil
								button := ui.Button(c, action.Title).FillWidth().Disabled(disabled)
								if disabled {
									button.Tooltip(action.Disabled.Reason)
								}
								if button.Clicked() {
									a.applyLanguageCodeAction(action)
								}
							}
						})
					})
				case codeLanguageDialogReferences:
					if len(a.code.languageReferences) == 0 && a.code.languageError == "" {
						ui.Text(c, tr("No references found")).TextColor(theme.TextMuted)
					}
					ui.Scroll(c).MaxHeight(420).Children(func() {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							for _, location := range a.code.languageReferences {
								location := location
								if ui.Button(c, languageServerLocationLabel(a.snapshot.Root, location)).FillWidth().Clicked() {
									a.openLanguageReference(location)
								}
							}
						})
					})
				case codeLanguageDialogPeek:
					if a.code.languagePeekLoading {
						ui.Row(c).Gap(8).Children(func() { ui.Spinner(c); ui.Text(c, tr("Loading definition…")) })
					} else if a.code.languagePeekState != nil {
						a.applyCodeEditorPreferences(a.code.languagePeekState)
						a.code.languagePeekState.ReadOnly = true
						editor.View(c, a.code.languagePeekState).FillWidth().Height(420).ReadOnly(true)
					}
				}
				if a.code.languageError != "" {
					ui.Text(c, a.code.languageError).TextColor(theme.Danger)
				}
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCodeLanguageDialog()
			}
		})
	})
}
