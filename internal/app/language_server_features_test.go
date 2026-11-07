package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
)

func openGoDocumentWithLanguageServer(t *testing.T, files map[string]string,
	request func(string, any, func(json.RawMessage, error))) (*harness, *fakeLanguageServerConnection, *codeDocument) {
	t.Helper()
	connection := newFakeLanguageServerConnection()
	connection.request = request
	stubLanguageServerConnection(t, func(languageServerLaunch) (languageServerConnection, error) {
		return connection, nil
	})
	stubCodeWorkspace(t, files)
	h := menuTestApp(t)
	openCodePageForTest(t, h, len(files))
	h.app.openCodeFile("main.go", 0)
	document := waitForCodeDocument(t, h, "main.go")
	h.waitFor(t, "the language server", func() bool { return h.app.goLanguageServer == connection })
	return h, connection, document
}

func TestCustomOnlyExtensionInstallsEditorLanguageCallbacks(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"custom-example": {
			Disabled: true, Path: "example-ls",
			Custom: &customLanguageServerDefinition{
				Name: "Example", Extensions: map[string]string{".example": "example"},
			},
		},
	}
	document := &codeDocument{path: "main.example", state: &editor.State{Buffer: editor.NewBuffer("example\n")}}
	h.app.configureGoLanguageDocument(document)
	if document.state.OnHover == nil || document.state.OnDefinition == nil || document.state.OnCompletion == nil || document.state.RenderHover == nil {
		t.Fatal("a custom-only extension did not install the editor language callbacks")
	}
}

func TestLanguageServerUTF16PositionsRoundTrip(t *testing.T) {
	text := "first\nA😀界Z\n"
	for _, offset := range []int{0, 3, 6, 7, 11, 14, 15, len(text)} {
		position := languageServerPositionAt(text, offset)
		if got := languageServerOffsetAt(text, position); got != offset {
			t.Errorf("offset %d -> %+v -> %d", offset, position, got)
		}
	}
	if got := languageServerPositionAt(text, strings.Index(text, "界")); got.Line != 1 || got.Character != 3 {
		t.Fatalf("position before 界 = %+v, want line 1 UTF-16 character 3", got)
	}
}

func TestGoLanguageServerHoverAndCompletionsReachTheEditor(t *testing.T) {
	requests := make(chan string, 4)
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\nfunc main() { fm }\n",
	}, func(method string, _ any, result func(json.RawMessage, error)) {
		requests <- method
		switch method {
		case "textDocument/hover":
			result(json.RawMessage(`{"contents":{"kind":"markdown","value":"func **main**()"},"range":{"start":{"line":0,"character":8},"end":{"line":0,"character":12}}}`), nil)
		case "textDocument/completion":
			result(json.RawMessage(`{"items":[{"label":"fmt","detail":"package fmt","kind":9,"insertText":"fmt"},{"label":"func","kind":15,"insertText":"func ${1:name}() {$0}","insertTextFormat":2}]}`), nil)
		}
	})

	document.state.Select(9, 9)
	if !document.state.Run(editor.ShowHover) {
		t.Fatal("the editor did not request hover information")
	}
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if got := <-requests; got != "textDocument/hover" {
		t.Fatalf("hover sent %q", got)
	}
	if document.hoverState == nil || document.hoverState.Buffer.String() != "func **main**()" {
		t.Fatalf("hover response = %#v", document.hoverState)
	}

	offset := strings.Index(document.state.Buffer.String(), "fm") + 2
	document.state.Select(offset, offset)
	if !document.state.Run(editor.ShowCompletions) {
		t.Fatal("the editor did not request completions")
	}
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if got := <-requests; got != "textDocument/completion" {
		t.Fatalf("completion sent %q", got)
	}
	if !h.tester.HasText("package fmt") {
		t.Fatalf("completion response is not visible: %q", h.texts())
	}
}

func TestLanguageServerHoverUnwrapsSingleCodeBlock(t *testing.T) {
	code := "(property) DnDContextType.abortControllerRef: React.MutableRefObject<AbortControllerMap<string>>"
	response, _ := json.Marshal(map[string]any{
		"contents": map[string]string{"kind": "markdown", "value": "```typescript\n" + code + "\n```"},
	})
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\nvar abortControllerRef int\n",
	}, func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/hover" {
			result(response, nil)
		}
	})
	offset := strings.Index(document.state.Buffer.String(), "abortControllerRef")
	document.state.Select(offset, offset)
	document.state.Run(editor.ShowHover)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if document.hoverState == nil {
		t.Fatal("the hover response was not adopted")
	}
	if got := document.hoverState.Buffer.String(); got != code {
		t.Fatalf("hover still contains its Markdown fence: %q", got)
	}
	if language := document.hoverState.Buffer.Language(); language == nil || language.Name != "TypeScript" {
		t.Fatalf("unwrapped hover language = %#v, want TypeScript", language)
	}
}

func TestGoLanguageServerHoverScrollsAndHighlightsCode(t *testing.T) {
	code := strings.Repeat("var result int\n", 80)
	response, _ := json.Marshal(map[string]any{
		"contents": map[string]string{"kind": "markdown", "value": "```go\n" + code + "```"},
	})
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\nvar result int\n",
	}, func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/hover" {
			result(response, nil)
		}
	})

	offset := strings.Index(document.state.Buffer.String(), "result")
	document.state.Select(offset, offset)
	if !document.state.Run(editor.ShowHover) {
		t.Fatal("the editor did not request the long hover")
	}
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	h.tester.Frame()
	if document.hoverState == nil || document.hoverState.Buffer.Language() == nil ||
		document.hoverState.Buffer.Language().Name != "Go" {
		t.Fatal("the unwrapped hover code is not using the Go syntax highlighter")
	}
	if strings.Contains(document.hoverState.Buffer.String(), "```") {
		t.Fatalf("the long hover still contains Markdown fences: %.80q", document.hoverState.Buffer.String())
	}
	if document.hoverScroll.MaxY <= 0 {
		t.Fatalf("long hover is not scrollable: max Y = %v", document.hoverScroll.MaxY)
	}
	bounds, found := h.tester.Find("Hover details")
	if !found {
		t.Fatal("the hover details editor is not visible")
	}
	h.tester.Scroll(bounds.X+bounds.W/2, bounds.Y+bounds.H/2, 0, 120)
	h.tester.Frame()
	if document.hoverScroll.Y <= 0 {
		t.Fatalf("the hover ignored scrolling: Y = %v", document.hoverScroll.Y)
	}
}

func TestLanguageServerHoverRemovesFencesFromMixedMarkdown(t *testing.T) {
	content := "```xml\n<class 'EMSBaseOperator'>\n```\n\n---\nEMS Base Operator\n\n```python\noperator.run()\n```"
	response, _ := json.Marshal(map[string]any{
		"contents": map[string]string{"kind": "markdown", "value": content},
	})
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\nvar operator int\n",
	}, func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/hover" {
			result(response, nil)
		}
	})
	offset := strings.Index(document.state.Buffer.String(), "operator")
	document.state.Select(offset, offset)
	document.state.Run(editor.ShowHover)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if document.hoverState == nil {
		t.Fatal("the mixed Markdown hover was not adopted")
	}
	got := document.hoverState.Buffer.String()
	if strings.Contains(got, "```") || !strings.Contains(got, "<class 'EMSBaseOperator'>") ||
		!strings.Contains(got, "EMS Base Operator") || !strings.Contains(got, "operator.run()") {
		t.Fatalf("mixed hover was not cleaned without dropping content: %q", got)
	}
}

func TestLanguageServerMarkedCodeKeepsItsLanguage(t *testing.T) {
	raw := json.RawMessage(`{"language":"go","value":"func helper() int"}`)
	if got := languageServerMarkupText(raw); got != "```go\nfunc helper() int\n```" {
		t.Fatalf("marked code = %q", got)
	}
}

func TestGoLanguageServerRejectsAStaleHover(t *testing.T) {
	var answer func(json.RawMessage, error)
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\n",
	}, func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/hover" {
			answer = result
		}
	})
	document.state.Select(9, 9)
	document.state.Run(editor.ShowHover)
	if answer == nil {
		t.Fatal("hover request was not sent")
	}
	document.state.Insert("x")
	repaint := true
	h.app.updateChanged = func(fn func() bool) {
		h.app.update(func() { repaint = fn() })
	}
	answer(json.RawMessage(`{"contents":"stale hover"}`), nil)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if h.tester.HasText("stale hover") {
		t.Fatal("a response for the previous buffer version became visible")
	}
	if repaint {
		t.Fatal("a stale language-server response repainted the window")
	}
}

func TestGoLanguageServerEmptyHoverDoesNotRepaint(t *testing.T) {
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\n",
	}, func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/hover" {
			result(json.RawMessage(`{"contents":""}`), nil)
		}
	})
	repaint := true
	h.app.updateChanged = func(fn func() bool) {
		h.app.update(func() { repaint = fn() })
	}
	document.state.Select(9, 9)
	document.state.Run(editor.ShowHover)
	h.updates.waitForUpdate(t)
	if repaint {
		t.Fatal("an empty language-server response repainted the window")
	}
}

func TestAdditionalLanguageServerEmptyHoverDoesNotRepaint(t *testing.T) {
	h := menuTestApp(t)
	document := &codeDocument{path: "main.tsx", state: &editor.State{Buffer: editor.NewBuffer("const value = 1\n")}}
	h.app.code.documents = map[string]*codeDocument{document.path: document}
	connection := newFakeLanguageServerConnection()
	var answer func(json.RawMessage, error)
	connection.request = func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/hover" {
			answer = result
		}
	}
	profile, _ := languageServerProfileByID("typescript")
	h.app.settings.LanguageServers = map[string]languageServerSettings{profile.ID: {}}
	server := &managedLanguageServer{
		profile: profile, connection: connection, documents: make(map[string]uint64),
	}
	h.app.additionalLanguageServers = map[string]*managedLanguageServer{profile.ID: server}
	repaint := true
	h.app.updateChanged = func(fn func() bool) {
		h.app.update(func() { repaint = fn() })
	}

	h.app.requestAdditionalLanguageHover(document, editor.HoverRequest{Offset: 6})
	if answer == nil {
		t.Fatal("the additional language-server request was not sent")
	}
	answer(json.RawMessage(`{"contents":""}`), nil)
	h.updates.waitForUpdate(t)
	if repaint {
		t.Fatal("an empty additional language-server response repainted the window")
	}
}

func TestGoLanguageServerDefinitionJumpsAndNavigatesBack(t *testing.T) {
	var requestedPosition languageServerPosition
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go":   "package main\nfunc main() { Thing() }\n",
		"target.go": "package target\nvar 😀Thing = 1\n",
	}, func(method string, params any, result func(json.RawMessage, error)) {
		if method != "textDocument/definition" {
			return
		}
		data, _ := json.Marshal(params)
		var position languageServerTextPosition
		_ = json.Unmarshal(data, &position)
		requestedPosition = position.Position
		result(json.RawMessage(`{"uri":"file:///work/project/target.go","range":{"start":{"line":1,"character":6},"end":{"line":1,"character":11}}}`), nil)
	})

	offset := strings.Index(document.state.Buffer.String(), "Thing")
	document.state.Select(offset, offset)
	if !document.state.Run(editor.GoToDefinition) {
		t.Fatal("F12 definition action was not handled")
	}
	h.updates.waitForUpdate(t)
	target := waitForCodeDocument(t, h, "target.go")
	if requestedPosition.Line != 1 || requestedPosition.Character != 14 {
		t.Fatalf("definition request position = %+v", requestedPosition)
	}
	if got := target.state.CursorPos(); got.Line != 1 || got.Col != 8 {
		t.Fatalf("UTF-16 definition target opened at %+v, want line 1 byte column 8", got)
	}
	if len(h.app.code.back) != 1 {
		t.Fatalf("definition jump left history %#v", h.app.code.back)
	}
	h.app.navigateCodeHistory(true)
	waitForCodeDocument(t, h, "main.go")
	if h.app.code.active != "main.go" || len(h.app.code.forward) != 1 {
		t.Fatalf("Back navigation active=%q forward=%#v", h.app.code.active, h.app.code.forward)
	}
}

func TestGoLanguageServerPublishesDiagnostics(t *testing.T) {
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\nvar 😀bad = missing\n",
	}, nil)
	version := document.state.Buffer.Version()
	params, _ := json.Marshal(map[string]any{
		"uri":     "file:///work/project/main.go",
		"version": version,
		"diagnostics": []any{map[string]any{
			"range": map[string]any{
				"start": map[string]int{"line": 1, "character": 6},
				"end":   map[string]int{"line": 1, "character": 9},
			},
			"severity": 1, "message": "undefined: bad", "source": "compiler",
		}},
	})
	h.app.handleGoLanguageServerNotification(h.app.goLanguageServerSeq, "textDocument/publishDiagnostics", params)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	diagnostics := document.state.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Start != strings.Index(document.state.Buffer.String(), "bad") ||
		diagnostics[0].Message != "undefined: bad" || diagnostics[0].Severity != editor.SeverityError {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}

	for _, stale := range []bool{false, true} {
		repaint := true
		h.app.updateChanged = func(fn func() bool) {
			h.app.update(func() { repaint = fn() })
		}
		sequence := h.app.goLanguageServerSeq
		if stale {
			sequence--
		}
		h.app.handleGoLanguageServerNotification(sequence, "textDocument/publishDiagnostics", params)
		h.updates.waitForUpdate(t)
		if repaint {
			t.Fatalf("duplicate diagnostics (stale=%v) repainted the window", stale)
		}
	}

	repaint := false
	h.app.updateChanged = func(fn func() bool) {
		h.app.update(func() { repaint = fn() })
	}
	cleared, _ := json.Marshal(map[string]any{
		"uri": "file:///work/project/main.go", "version": version, "diagnostics": []any{},
	})
	h.app.handleGoLanguageServerNotification(h.app.goLanguageServerSeq, "textDocument/publishDiagnostics", cleared)
	h.updates.waitForUpdate(t)
	if !repaint || len(document.state.Diagnostics()) != 0 {
		t.Fatalf("clearing diagnostics repaint = %v, diagnostics = %#v", repaint, document.state.Diagnostics())
	}

	// Diagnostics remain current while another workspace is visible, but must
	// not repaint a window that does not render this editor.
	h.app.workspaceTab = sectionChanges
	repaint = true
	h.app.handleGoLanguageServerNotification(h.app.goLanguageServerSeq, "textDocument/publishDiagnostics", params)
	h.updates.waitForUpdate(t)
	if repaint || len(document.state.Diagnostics()) != 1 {
		t.Fatalf("hidden diagnostics repaint = %v, diagnostics = %#v", repaint, document.state.Diagnostics())
	}

	h.app.workspaceTab = sectionCode
	h.app.code.active = ""
	document.languageDiagnostics[goLanguageServerID] = nil
	document.state.SetDiagnostics(version, nil)
	repaint = true
	h.app.handleGoLanguageServerNotification(h.app.goLanguageServerSeq, "textDocument/publishDiagnostics", params)
	h.updates.waitForUpdate(t)
	if repaint || len(document.state.Diagnostics()) != 1 {
		t.Fatalf("background-document diagnostics repaint = %v, diagnostics = %#v", repaint, document.state.Diagnostics())
	}
}

func TestGoLanguageServerReportsWorkspaceErrors(t *testing.T) {
	h, _, _ := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\n",
	}, nil)
	params, _ := json.Marshal(map[string]any{
		"type":    4,
		"message": "go.mod:3: invalid go version '1.27.3': must match format 1.23",
	})
	h.app.handleGoLanguageServerNotification(h.app.goLanguageServerSeq, "window/showMessage", params)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if !strings.Contains(h.app.goLanguageServerError, "Update gopls") {
		t.Fatalf("workspace error = %q", h.app.goLanguageServerError)
	}
	if h.tester.HasText("gopls issue") || !h.tester.HasText("gopls cannot analyze this workspace") {
		t.Fatalf("workspace error notification changed: %q", h.texts())
	}
	button, ok := h.tester.Find("Language servers")
	if !ok {
		t.Fatal("the workspace error has no language-server status icon")
	}
	h.tester.Move(button.X+button.W/2, button.Y+button.H/2)
	h.tester.Frame()
	if !h.tester.HasText("Go (gopls)") || !h.tester.HasText("Error") {
		t.Fatalf("the status panel does not identify the failed server: %q", h.texts())
	}
	row, ok := h.tester.Find("Language servers: Go (gopls)")
	if !ok {
		t.Fatal("the failed server row is missing")
	}
	h.tester.Move(row.X+row.W/2, row.Y+row.H/2)
	h.tester.Advance(700 * time.Millisecond)
	h.tester.Frame()
	if !h.tester.HasText("Update gopls") {
		t.Fatalf("the status panel does not expose the workspace error detail: %q", h.texts())
	}
}

func TestGoLanguageServerOpensExternalDefinitionsReadOnly(t *testing.T) {
	external := filepath.Join(t.TempDir(), "fmt", "print.go")
	if err := os.MkdirAll(filepath.Dir(external), 0o755); err != nil {
		t.Fatal(err)
	}
	const externalText = "package fmt\nfunc Println() {}\n"
	if err := os.WriteFile(external, []byte(externalText), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := languageServerFileURI(external)
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\nfunc main() { Println() }\n",
	}, func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/definition" {
			response, _ := json.Marshal(languageServerLocation{
				URI: uri, Range: languageServerRange{Start: languageServerPosition{Line: 1, Character: 5}},
			})
			result(response, nil)
		}
	})
	originalRead := readWorkingFile
	readWorkingFile = func(name string) ([]byte, error) {
		if filepath.Clean(name) == filepath.Clean(external) {
			return os.ReadFile(name)
		}
		return originalRead(name)
	}

	offset := strings.Index(document.state.Buffer.String(), "Println")
	document.state.Select(offset, offset)
	document.state.Run(editor.GoToDefinition)
	h.updates.waitForUpdate(t)
	target := waitForCodeDocument(t, h, filepath.Clean(external))
	if !target.readOnly || target.diskPath != filepath.Clean(external) || target.state.Buffer.String() != externalText {
		t.Fatalf("external definition = readOnly %v, disk %q, text %q", target.readOnly, target.diskPath, target.state.Buffer.String())
	}
	if got := target.state.CursorPos(); got.Line != 1 || got.Col != 5 {
		t.Fatalf("external definition cursor = %+v", got)
	}
}

func TestRealGoplsHoverAndDefinition(t *testing.T) {
	if os.Getenv("TANGIT_TEST_REAL_GOPLS") != "1" {
		t.Skip("set TANGIT_TEST_REAL_GOPLS=1 to exercise the installed gopls")
	}
	gopls := realGoplsPath(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/hovercheck\n\ngo 1.27.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const source = "package main\n\nfunc helper(value int) int { return value + 1 }\nfunc main() { result := helper(1); _ = result }\n"
	name := filepath.Join(root, "main.go")
	if err := os.WriteFile(name, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	connection, err := connectStdioLanguageServer(languageServerLaunch{Program: gopls, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	uri := languageServerFileURI(name)
	connection.DidOpen(uri, "go", source, 1)

	request := func(method string, position languageServerPosition) json.RawMessage {
		t.Helper()
		answer := make(chan languageServerResponse, 1)
		connection.Request(method, languageServerTextPosition{
			TextDocument: languageServerTextDocument{URI: uri}, Position: position,
		}, func(raw json.RawMessage, err error) { answer <- languageServerResponse{result: raw, err: err} })
		select {
		case response := <-answer:
			if response.err != nil {
				t.Fatalf("%s: %v", method, response.err)
			}
			return response.result
		case <-time.After(15 * time.Second):
			t.Fatalf("%s timed out", method)
			return nil
		}
	}

	hoverOffset := strings.LastIndex(source, "result")
	hoverRaw := request("textDocument/hover", languageServerPositionAt(source, hoverOffset))
	hover, _, ok := parseLanguageServerHover(hoverRaw)
	if !ok || !strings.Contains(hover, "result") {
		t.Fatalf("real gopls hover = %s", hoverRaw)
	}
	definitionOffset := strings.Index(source, "helper(1)")
	definitionRaw := request("textDocument/definition", languageServerPositionAt(source, definitionOffset))
	location, ok := firstLanguageServerLocation(definitionRaw)
	if !ok || location.URI != uri || location.Range.Start.Line != 2 {
		t.Fatalf("real gopls definition = %s", definitionRaw)
	}
}

func TestRealGoplsEditorHoverAndDefinition(t *testing.T) {
	if os.Getenv("TANGIT_TEST_REAL_GOPLS") != "1" {
		t.Skip("set TANGIT_TEST_REAL_GOPLS=1 to exercise the installed gopls")
	}
	gopls := realGoplsPath(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/editorcheck\n\ngo 1.27.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const source = "package main\n\nfunc helper(value int) int { return value + 1 }\nfunc main() { result := helper(1); _ = result }\n"
	name := filepath.Join(root, "main.go")
	if err := os.WriteFile(name, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	originalList, originalRead, originalWrite := listCodeFiles, readWorkingFile, writeWorkingFile
	t.Cleanup(func() { listCodeFiles, readWorkingFile, writeWorkingFile = originalList, originalRead, originalWrite })
	listCodeFiles = func(string) ([]string, error) { return []string{"main.go"}, nil }
	readWorkingFile = os.ReadFile
	writeWorkingFile = func(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }

	h := menuTestApp(t)
	h.app.snapshot.Root = root
	h.app.path = root
	h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Path: gopls}}
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("main.go", 0)
	document := waitForCodeDocument(t, h, "main.go")
	h.waitFor(t, "real gopls to open the editor document", func() bool {
		_, opened := h.app.goLanguageServerDocs["main.go"]
		return h.app.goLanguageServer != nil && opened
	})
	t.Cleanup(func() {
		if h.app.goLanguageServer != nil {
			h.app.goLanguageServer.Close()
		}
	})

	hoverOffset := strings.LastIndex(source, "result")
	document.state.Select(hoverOffset, hoverOffset)
	if !document.state.Run(editor.ShowHover) {
		t.Fatal("the real editor did not dispatch hover")
	}
	h.waitFor(t, "real hover content", func() bool {
		return document.hoverState != nil && strings.Contains(document.hoverState.Buffer.String(), "var result int")
	})

	definitionOffset := strings.Index(source, "helper(1)")
	document.state.Select(definitionOffset, definitionOffset)
	if !document.state.Run(editor.GoToDefinition) {
		t.Fatal("the real editor did not dispatch go to definition")
	}
	h.waitFor(t, "real definition jump", func() bool {
		position := document.state.CursorPos()
		return position.Line == 2 && position.Col == 5
	})
}

func realGoplsPath(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("TANGIT_TEST_GOPLS_PATH"); path != "" {
		return path
	}
	path, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("gopls is not installed")
	}
	return path
}
