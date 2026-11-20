package app

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func languageActionTestDocument(h *harness, text string) (*fakeLanguageServerConnection, *codeDocument) {
	connection := newFakeLanguageServerConnection()
	document := &codeDocument{path: "main.go", state: &editor.State{Buffer: editor.NewBuffer(text)}, loaded: true}
	h.app.code.documents = map[string]*codeDocument{document.path: document}
	h.app.lsp.goServer = connection
	h.app.lsp.goDocs = make(map[string]uint64)
	return connection, document
}

func TestCloseCodeLanguageDialogDropsLateRename(t *testing.T) {
	h := menuTestApp(t)
	connection, document := languageActionTestDocument(h, "package main\n")
	var response func(json.RawMessage, error)
	connection.request = func(_ string, _ any, result func(json.RawMessage, error)) {
		response = result
	}
	delivered := false
	h.app.openLanguageRename(document)
	h.app.languageServerActionRequest(document, &document.languageRenameSeq, "textDocument/rename", nil,
		func(json.RawMessage, error) bool {
			delivered = true
			return true
		})
	if response == nil {
		t.Fatal("rename request was not sent")
	}
	h.app.closeCodeLanguageDialog()
	response(json.RawMessage(`{}`), nil)
	h.updates.waitForUpdate(t)
	if delivered {
		t.Fatal("a closed rename dialog accepted its late response")
	}
}

func TestCloseCodeLanguageDialogDoesNotReopenLatePeek(t *testing.T) {
	h := menuTestApp(t)
	h.app.snapshot.Root = t.TempDir()
	connection, document := languageActionTestDocument(h, "package main\n")
	var response func(json.RawMessage, error)
	connection.request = func(_ string, _ any, result func(json.RawMessage, error)) {
		response = result
	}
	h.app.requestLanguagePeekDefinition(document)
	if h.app.code.languageDialog != codeLanguageDialogPeek {
		t.Fatal("peek definition was not cancelable while its request was pending")
	}
	h.app.closeCodeLanguageDialog()
	uri := languageServerFileURI(filepath.Join(h.app.snapshot.Root, document.path))
	response(json.RawMessage(`[{"uri":`+strconv.Quote(uri)+`,"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}]`), nil)
	h.updates.waitForUpdate(t)
	if h.app.code.languageDialog != codeLanguageDialogNone || h.app.code.languagePeekState != nil {
		t.Fatal("a late definition response reopened or populated a closed peek dialog")
	}
}

func TestLanguageCodeActionsSendOverlappingDiagnostics(t *testing.T) {
	h := menuTestApp(t)
	connection, document := languageActionTestDocument(h, "one two three")
	document.state.SetDiagnostics(document.state.Buffer.Version(), []editor.Diagnostic{
		{Start: 0, End: 3, Severity: editor.SeverityError, Message: "first problem", Source: "test"},
		{Start: 8, End: 13, Severity: editor.SeverityHint, Message: "unrelated"},
	})
	connection.request = func(method string, params any, _ func(json.RawMessage, error)) {
		if method != "textDocument/codeAction" {
			t.Fatalf("method = %q", method)
		}
		request := params.(languageServerCodeActionParams)
		if len(request.Context.Diagnostics) != 1 {
			t.Fatalf("diagnostics = %s, want one overlapping entry", request.Context.Diagnostics)
		}
		var diagnostic struct {
			Range    languageServerRange `json:"range"`
			Severity int                 `json:"severity"`
			Message  string              `json:"message"`
			Source   string              `json:"source"`
		}
		if err := json.Unmarshal(request.Context.Diagnostics[0], &diagnostic); err != nil {
			t.Fatal(err)
		}
		if diagnostic.Severity != 1 || diagnostic.Message != "first problem" || diagnostic.Source != "test" ||
			diagnostic.Range.End.Character != 3 {
			t.Fatalf("diagnostic = %+v", diagnostic)
		}
		wire, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Context struct {
				Diagnostics json.RawMessage `json:"diagnostics"`
			} `json:"context"`
		}
		if err := json.Unmarshal(wire, &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Context.Diagnostics) == 0 || string(payload.Context.Diagnostics) == "null" {
			t.Fatalf("diagnostics wire value = %s, want an array", payload.Context.Diagnostics)
		}
	}
	h.app.requestLanguageCodeActions(document)
}

func TestLanguageCodeActionRejectsEditWithUnsupportedCommand(t *testing.T) {
	h := menuTestApp(t)
	h.app.code.languageDialog = codeLanguageDialogActions
	h.app.applyLanguageCodeAction(languageServerCodeAction{
		Title:   "Fix and run",
		Edit:    json.RawMessage(`{"changes":{}}`),
		Command: json.RawMessage(`{"command":"workbench.action.restart"}`),
	})
	if h.app.code.languageDialog != codeLanguageDialogActions || h.app.code.languageError == "" || h.app.code.languageEditRunning {
		t.Fatalf("partial CodeAction was applied: dialog=%v error=%q running=%v",
			h.app.code.languageDialog, h.app.code.languageError, h.app.code.languageEditRunning)
	}
}

func TestReopenLanguageSymbolsRetriesCanceledRequest(t *testing.T) {
	h := menuTestApp(t)
	connection, document := languageActionTestDocument(h, "package main\n")
	requests := 0
	connection.request = func(string, any, func(json.RawMessage, error)) {
		requests++
	}
	h.app.openLanguageSymbols(document)
	h.app.closeCodeLanguageDialog()
	h.app.openLanguageSymbols(document)
	if requests != 2 {
		t.Fatalf("document symbol requests = %d, want a fresh request after cancellation", requests)
	}
}

func TestStalePeekResponseStopsLoading(t *testing.T) {
	h := menuTestApp(t)
	connection, document := languageActionTestDocument(h, "package main\n")
	var response func(json.RawMessage, error)
	connection.request = func(_ string, _ any, result func(json.RawMessage, error)) {
		response = result
	}
	h.app.requestLanguagePeekDefinition(document)
	document.state.Insert("// changed\n")
	response(json.RawMessage(`[]`), nil)
	h.updates.waitForUpdate(t)
	if h.app.code.languagePeekLoading || h.app.code.languageError == "" {
		t.Fatalf("stale peek remains pending: loading=%v error=%q", h.app.code.languagePeekLoading, h.app.code.languageError)
	}
}
