package app

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestFormatDocumentContextMenuAppliesOneUndoableEdit(t *testing.T) {
	const original = "package main\nfunc main(){println(\"😀\")}\n"
	const formatted = "package main\n\nfunc main() {\n\tprintln(\"😀\")\n}\n"
	paramsSent := make(chan []byte, 1)
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{"main.go": original},
		func(method string, params any, result func(json.RawMessage, error)) {
			if method != "textDocument/formatting" {
				return
			}
			payload, _ := json.Marshal(params)
			paramsSent <- payload
			response, _ := json.Marshal([]map[string]any{{
				"range": map[string]any{
					"start": map[string]int{"line": 0, "character": 0},
					"end":   map[string]int{"line": 2, "character": 0},
				},
				"newText": formatted,
			}})
			result(response, nil)
		})
	if document.state.ContextMenu == nil {
		t.Fatal("the language-aware editor has no context menu")
	}
	bounds, found := h.tester.Find("Code editor")
	if !found {
		t.Fatal("the code editor is not visible")
	}
	h.tester.RightClickAt(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
	if menu := h.tester.Menu(); !slices.Contains(menu, "Format Document") {
		t.Fatalf("the editor context menu lacks Format Document: %q", menu)
	}
	if err := h.tester.ChooseMenuItem("Format Document"); err != nil {
		t.Fatal(err)
	}
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	var params struct {
		TextDocument languageServerTextDocument `json:"textDocument"`
		Options      struct {
			TabSize      int  `json:"tabSize"`
			InsertSpaces bool `json:"insertSpaces"`
		} `json:"options"`
	}
	if err := json.Unmarshal(<-paramsSent, &params); err != nil {
		t.Fatal(err)
	}
	if params.TextDocument.URI != "file:///work/project/main.go" || params.Options.TabSize != 4 || params.Options.InsertSpaces {
		t.Fatalf("formatting request = %#v", params)
	}
	if got := document.state.Buffer.String(); got != formatted {
		t.Fatalf("formatted buffer = %q, want %q", got, formatted)
	}
	if document.formatting {
		t.Fatal("the formatter remained busy after its response")
	}
	if !document.state.Buffer.Undo() || document.state.Buffer.String() != original {
		t.Fatalf("one undo did not restore the original document: %q", document.state.Buffer.String())
	}
	if !document.state.Buffer.Redo() || document.state.Buffer.String() != formatted {
		t.Fatalf("redo did not restore formatting: %q", document.state.Buffer.String())
	}
}

func TestFormatSelectionUsesLanguageServerRangeFormattingUTF16(t *testing.T) {
	const original = "package main\nvar 界=\"😀\"\nvar other=2\n"
	const selected = "var 界=\"😀\""
	const formattedSelection = "var 界 = \"😀\""
	requests := make(chan languageServerRangeFormattingParams, 1)
	h, connection, document := openGoDocumentWithLanguageServer(t, map[string]string{"main.go": original},
		func(method string, params any, result func(json.RawMessage, error)) {
			if method != "textDocument/rangeFormatting" {
				return
			}
			rangeParams := params.(languageServerRangeFormattingParams)
			requests <- rangeParams
			response, _ := json.Marshal([]map[string]any{{
				"range": rangeParams.Range, "newText": formattedSelection,
			}})
			result(response, nil)
		})
	start := strings.Index(original, selected)
	document.state.Select(start, start+len(selected))
	h.app.formatCodeSelection(document)
	h.updates.waitForUpdate(t)
	params := <-requests
	if params.Range.Start.Line != 1 || params.Range.Start.Character != 0 ||
		params.Range.End.Line != 1 || params.Range.End.Character != 10 {
		t.Fatalf("UTF-16 selection range = %#v", params.Range)
	}
	if !connection.DocumentRangeFormatting() {
		t.Fatal("the test server did not advertise range formatting")
	}
	formatted := strings.Replace(original, selected, formattedSelection, 1)
	if got := document.state.Buffer.String(); got != formatted {
		t.Fatalf("range-formatted buffer = %q, want %q", got, formatted)
	}
	if !document.state.Buffer.Undo() || document.state.Buffer.String() != original ||
		!document.state.Buffer.Redo() || document.state.Buffer.String() != formatted {
		t.Fatal("range formatting was not one undo/redo step")
	}
}

func TestFormatSelectionRequiresAdvertisedRangeFormatting(t *testing.T) {
	const original = "package main\nvar x=1\n"
	h, connection, document := openGoDocumentWithLanguageServer(t, map[string]string{"main.go": original}, nil)
	connection.rangeFormatting = false
	connection.request = func(method string, _ any, _ func(json.RawMessage, error)) {
		if strings.Contains(method, "formatting") {
			t.Errorf("non-range-formatting server received %s", method)
		}
	}
	document.state.Select(len("package main\n"), len(original))
	h.app.formatCodeSelection(document)
	if document.state.Buffer.String() != original || document.formatting {
		t.Fatalf("unsupported range formatting changed state: %q busy=%v", document.state.Buffer.String(), document.formatting)
	}
	if len(h.app.pendingToasts) == 0 || !strings.Contains(h.app.pendingToasts[len(h.app.pendingToasts)-1].description, "range formatting") {
		t.Fatalf("missing range-formatting guidance: %#v", h.app.pendingToasts)
	}
}

func TestFormatDocumentDiscardsResponseAfterFurtherTyping(t *testing.T) {
	var answer func(json.RawMessage, error)
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\nvar value=1\n",
	}, func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/formatting" {
			answer = result
		}
	})
	h.app.formatCodeDocument(document)
	if answer == nil || !document.formatting {
		t.Fatal("formatting did not start")
	}
	document.state.Insert("// keep this edit\n")
	want := document.state.Buffer.String()
	answer(json.RawMessage(`[{"range":{"start":{"line":0,"character":0},"end":{"line":2,"character":0}},"newText":"package main\n\nvar value = 1\n"}]`), nil)
	h.updates.waitForUpdate(t)
	if got := document.state.Buffer.String(); got != want {
		t.Fatalf("a stale formatting response replaced new typing: %q, want %q", got, want)
	}
	if document.formatting {
		t.Fatal("a stale response did not clear the busy state")
	}
}

func TestFormatDocumentUsesOnlyServerAdvertisingFormatting(t *testing.T) {
	ty, ruff := newFakeLanguageServerConnection(), newFakeLanguageServerConnection()
	ty.formatting = false
	ty.request = func(method string, _ any, _ func(json.RawMessage, error)) {
		if method == "textDocument/formatting" {
			t.Error("the non-formatting server received a formatting request")
		}
	}
	ruff.request = func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/formatting" {
			result(json.RawMessage(`[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":5}},"newText":"x = 1"}]`), nil)
		}
	}
	stubLanguageServerConnection(t, func(launch languageServerLaunch) (languageServerConnection, error) {
		switch launch.Program {
		case "ty":
			return ty, nil
		case "ruff":
			return ruff, nil
		default:
			t.Fatalf("unexpected language server %q", launch.Program)
			return nil, nil
		}
	})
	stubCodeWorkspace(t, map[string]string{"main.py": "x=1\n"})
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Disabled: true}, "ty": {}, "ruff": {}}
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("main.py", 0)
	document := waitForCodeDocument(t, h, "main.py")
	h.waitFor(t, "both Python language servers", func() bool {
		return len(ty.snapshot()) > 0 && len(ruff.snapshot()) > 0
	})
	h.app.formatCodeDocument(document)
	h.updates.waitForUpdate(t)
	if got := document.state.Buffer.String(); got != "x = 1\n" {
		t.Fatalf("Ruff formatting result = %q", got)
	}
}

func TestLanguageServerFormattingCapabilitiesAndEdits(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want bool
	}{
		{raw: `{"capabilities":{"documentFormattingProvider":true}}`, want: true},
		{raw: `{"capabilities":{"documentFormattingProvider":{}}}`, want: true},
		{raw: `{"capabilities":{"documentFormattingProvider":false}}`},
		{raw: `{"capabilities":{}}`},
		{raw: `null`},
	} {
		if got := languageServerDocumentFormattingSupported(json.RawMessage(test.raw)); got != test.want {
			t.Errorf("formatting support for %s = %v, want %v", test.raw, got, test.want)
		}
	}
	text := "first\nA😀界Z\n"
	start := languageServerPositionAt(text, strings.Index(text, "😀"))
	end := languageServerPositionAt(text, strings.Index(text, "界")+len("界"))
	raw, _ := json.Marshal([]languageServerTextEdit{{Range: languageServerRange{Start: start, End: end}, NewText: "formatted"}})
	edits, err := parseLanguageServerFormattingEdits(raw, text)
	if err != nil {
		t.Fatal(err)
	}
	buffer := editor.NewBuffer(text)
	buffer.Edit(edits...)
	if got, want := buffer.String(), "first\nAformattedZ\n"; got != want {
		t.Fatalf("UTF-16 edit result = %q, want %q", got, want)
	}
	overlap := json.RawMessage(`[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":5}},"newText":"a"},{"range":{"start":{"line":0,"character":2},"end":{"line":0,"character":7}},"newText":"b"}]`)
	if _, err := parseLanguageServerFormattingEdits(overlap, "abcdefgh"); err == nil {
		t.Fatal("overlapping formatting edits were accepted")
	}
}
