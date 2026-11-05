package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
)

func TestEveryManagedLanguageServerHasAVerifiableDownloadSource(t *testing.T) {
	if len(languageServerDownloadURLs) != len(languageServerProfiles) {
		t.Fatalf("download sources = %d, profiles = %d", len(languageServerDownloadURLs), len(languageServerProfiles))
	}
	for _, profile := range languageServerProfiles {
		source := languageServerDownloadURLs[profile.ID]
		if !strings.HasPrefix(source, "https://") || strings.ContainsAny(source, " \t\r\n") {
			t.Errorf("%s download source = %q, want a valid HTTPS URL", profile.ID, source)
		}
	}
}

func TestLanguageServerExecutableCandidatesSplitTrimAndFallBack(t *testing.T) {
	got := languageServerExecutableCandidates(" /missing/gopls, , /zed/gopls ", "gopls")
	want := []string{"/missing/gopls", "/zed/gopls"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if got := languageServerExecutableCandidates(" , ", "gopls"); !reflect.DeepEqual(got, []string{"gopls"}) {
		t.Fatalf("blank candidates = %v, want the PATH default", got)
	}
}

func TestLanguageServerProfilesMatchLanguagesAndDefaults(t *testing.T) {
	settings := map[string]languageServerSettings{
		"typescript": {}, "tailwind": {}, "oxlint": {}, "rust": {},
	}
	tests := []struct {
		path string
		ids  []string
	}{
		{"main.go", []string{"go"}},
		{"main.rs", []string{"rust"}},
		{"main.tsx", []string{"typescript", "tailwind", "oxlint"}},
		{"index.html", []string{"tailwind"}},
	}
	for _, test := range tests {
		profiles := configuredLanguageServerProfiles(settings, test.path)
		ids := make([]string, len(profiles))
		for index, profile := range profiles {
			ids[index] = profile.ID
		}
		if !reflect.DeepEqual(ids, test.ids) {
			t.Errorf("profiles for %s = %v, want %v", test.path, ids, test.ids)
		}
	}
	typescript, _ := languageServerProfileByID("typescript")
	program, arguments := languageServerProgram(settings, typescript)
	if program != "typescript-language-server" || !reflect.DeepEqual(arguments, []string{"--stdio"}) {
		t.Fatalf("TypeScript launch = %q %v", program, arguments)
	}
	oxc, _ := languageServerProfileByID("oxlint")
	program, arguments = languageServerProgram(settings, oxc)
	if program != "oxlint" || !reflect.DeepEqual(arguments, []string{"--lsp"}) {
		t.Fatalf("Oxlint launch = %q %v", program, arguments)
	}
}

func TestMultipleLanguageServersShareATypeScriptDocument(t *testing.T) {
	dataDir := t.TempDir()
	tsserverPath := filepath.Join(dataDir, "tools", "node", "node_modules", "typescript", "lib", "tsserver.js")
	if err := os.MkdirAll(filepath.Dir(tsserverPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsserverPath, []byte("// managed TypeScript"), 0o644); err != nil {
		t.Fatal(err)
	}

	typescript := newFakeLanguageServerConnection()
	tailwind := newFakeLanguageServerConnection()
	typescript.request = func(method string, _ any, result func(json.RawMessage, error)) {
		switch method {
		case "textDocument/hover":
			result(json.RawMessage("null"), nil)
		case "textDocument/definition":
			result(json.RawMessage(`{"uri":"file:///work/project/target.ts","range":{"start":{"line":1,"character":6},"end":{"line":1,"character":12}}}`), nil)
		}
	}
	tailwind.request = func(method string, _ any, result func(json.RawMessage, error)) {
		if method == "textDocument/hover" {
			response, _ := json.Marshal(map[string]any{
				"contents": map[string]string{"kind": "markdown", "value": "```css\n.flex { display: flex; }\n```"},
			})
			result(response, nil)
		}
	}
	stubLanguageServerConnection(t, func(launch languageServerLaunch) (languageServerConnection, error) {
		switch launch.Program {
		case "typescript-language-server":
			want := map[string]any{"tsserver": map[string]any{"fallbackPath": tsserverPath}}
			if !reflect.DeepEqual(launch.InitializationOptions, want) {
				t.Errorf("TypeScript initialization = %#v, want %#v", launch.InitializationOptions, want)
			}
			return typescript, nil
		case "tailwindcss-language-server":
			if launch.InitializationOptions != nil {
				t.Errorf("TypeScript fallback leaked into Tailwind: %#v", launch.InitializationOptions)
			}
			return tailwind, nil
		default:
			t.Fatalf("unexpected language server %q", launch.Program)
			return nil, nil
		}
	})
	stubCodeWorkspace(t, map[string]string{
		"main.ts":   "const value = 'flex'\n",
		"target.ts": "export const value = 1\nexport const target = value\n",
	})
	h := menuTestApp(t)
	h.app.pathUserData = dataDir
	h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Disabled: true}, "typescript": {}, "tailwind": {}}
	openCodePageForTest(t, h, 2)
	h.app.openCodeFile("main.ts", 0)
	document := waitForCodeDocument(t, h, "main.ts")
	h.waitFor(t, "both TypeScript language servers", func() bool {
		return len(typescript.snapshot()) > 0 && len(tailwind.snapshot()) > 0
	})
	if typescript.snapshot()[0].language != "typescript" || tailwind.snapshot()[0].language != "typescript" {
		t.Fatalf("didOpen language IDs = %q and %q", typescript.snapshot()[0].language, tailwind.snapshot()[0].language)
	}
	document.state.Insert("// changed\n")
	h.tester.Frame()
	h.tester.Frame()
	h.app.saveCodeDocument(document)
	for name, events := range map[string][]languageServerDocumentEvent{
		"TypeScript": typescript.snapshot(), "Tailwind": tailwind.snapshot(),
	} {
		if len(events) < 3 || events[1].kind != "change" || events[2].kind != "save" {
			t.Fatalf("%s synchronization = %#v", name, events)
		}
	}

	offset := strings.Index(document.state.Buffer.String(), "flex")
	document.state.Select(offset, offset)
	if !document.state.Run(editor.ShowHover) {
		t.Fatal("TypeScript hover was not dispatched")
	}
	h.waitFor(t, "Tailwind hover after the TypeScript server returned null", func() bool {
		return document.hoverState != nil && strings.Contains(document.hoverState.Buffer.String(), "display: flex")
	})

	offset = strings.Index(document.state.Buffer.String(), "value")
	document.state.Select(offset, offset)
	if !document.state.Run(editor.GoToDefinition) {
		t.Fatal("TypeScript definition was not dispatched")
	}
	h.waitFor(t, "TypeScript definition", func() bool { return h.app.code.active == "target.ts" })
}

func TestCustomTyLanguageServerLaunchesAndPowersPythonFeatures(t *testing.T) {
	connection := newFakeLanguageServerConnection()
	connection.request = func(method string, _ any, result func(json.RawMessage, error)) {
		switch method {
		case "textDocument/hover":
			result(json.RawMessage(`{"contents":{"kind":"markdown","value":"ty hover"}}`), nil)
		case "textDocument/definition":
			result(json.RawMessage(`{"uri":"file:///work/project/target.py","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":8}}}`), nil)
		case "textDocument/completion":
			result(json.RawMessage(`{"items":[{"label":"ty_completion","kind":6}]}`), nil)
		}
	}
	launches := make(chan languageServerLaunch, 1)
	stubLanguageServerConnection(t, func(launch languageServerLaunch) (languageServerConnection, error) {
		launches <- launch
		return connection, nil
	})
	stubCodeWorkspace(t, map[string]string{
		"main.py":   "operator = 1\nprint(operator)\n",
		"target.py": "operator = 42\n",
	})
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"go": {Disabled: true},
		"custom-ty": {
			Path: "ty", Arguments: []string{"server"}, ProbeArguments: []string{"--version"},
			Custom: &customLanguageServerDefinition{
				Name: "ty", Extensions: map[string]string{".py": "python", ".pyi": "python"},
			},
		},
	}
	openCodePageForTest(t, h, 2)
	h.app.openPinnedCodeFile("main.py", 0)
	document := waitForCodeDocument(t, h, "main.py")
	h.waitFor(t, "the custom ty connection and Python didOpen", func() bool {
		return len(connection.snapshot()) > 0
	})
	launch := <-launches
	if launch.Program != "ty" || !reflect.DeepEqual(launch.Arguments, []string{"server"}) {
		t.Fatalf("custom ty launch = %#v", launch)
	}
	opened := connection.snapshot()[0]
	if opened.kind != "open" || opened.language != "python" || opened.uri != "file:///work/project/main.py" {
		t.Fatalf("custom ty didOpen = %#v", opened)
	}
	statuses := h.app.codeLanguageServerStatuses()
	if len(statuses) != 1 || statuses[0].name != "ty" || !statuses[0].running {
		t.Fatalf("custom ty editor status = %#v", statuses)
	}

	offset := strings.Index(document.state.Buffer.String(), "operator")
	document.state.Select(offset, offset)
	if !document.state.Run(editor.ShowHover) {
		t.Fatal("custom ty hover was not dispatched")
	}
	h.waitFor(t, "the custom ty hover", func() bool {
		return document.hoverState != nil && document.hoverState.Buffer.String() == "ty hover"
	})

	if !document.state.Run(editor.ShowCompletions) {
		t.Fatal("custom ty completion was not dispatched")
	}
	h.waitFor(t, "the custom ty completion", func() bool {
		return h.tester.HasText("ty_completion")
	})

	offset = strings.LastIndex(document.state.Buffer.String(), "operator")
	document.state.Select(offset, offset)
	if !document.state.Run(editor.GoToDefinition) {
		t.Fatal("custom ty definition was not dispatched")
	}
	h.waitFor(t, "the custom ty definition", func() bool { return h.app.code.active == "target.py" })

	params, _ := json.Marshal(map[string]any{
		"uri": "file:///work/project/main.py",
		"diagnostics": []any{map[string]any{
			"range": map[string]any{
				"start": map[string]int{"line": 0, "character": 0},
				"end":   map[string]int{"line": 0, "character": 8},
			},
			"severity": 1, "message": "ty diagnostic", "source": "ty",
		}},
	})
	launch.Notify("textDocument/publishDiagnostics", params)
	h.waitFor(t, "the custom ty diagnostic", func() bool {
		return strings.Contains(fmt.Sprint(document.state.Diagnostics()), "ty diagnostic")
	})
}

func TestDiagnosticsFromMultipleServersAreCombined(t *testing.T) {
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{
		"main.go": "package main\nvar bad = missing\n",
	}, nil)
	makeNotification := func(message, source string, character int) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{
			"uri": "file:///work/project/main.go",
			"diagnostics": []any{map[string]any{
				"range":    map[string]any{"start": map[string]int{"line": 1, "character": character}, "end": map[string]int{"line": 1, "character": character + 3}},
				"severity": 1, "message": message, "source": source,
			}},
		})
		return raw
	}
	h.app.handleLanguageServerDiagnostics("typescript", makeNotification("first", "typescript", 4), func() bool { return true })
	h.updates.waitForUpdate(t)
	h.app.handleLanguageServerDiagnostics("oxlint", makeNotification("second", "oxlint", 10), func() bool { return true })
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if diagnostics := document.state.Diagnostics(); len(diagnostics) != 2 {
		t.Fatalf("combined diagnostics = %#v", diagnostics)
	}
}

func TestRealRustAnalyzerHoverAndDefinition(t *testing.T) {
	if os.Getenv("TANGIT_TEST_REAL_RUST_ANALYZER") != "1" {
		t.Skip("set TANGIT_TEST_REAL_RUST_ANALYZER=1 to exercise rust-analyzer")
	}
	program, err := exec.LookPath("rust-analyzer")
	if err != nil {
		t.Skip("rust-analyzer is not installed")
	}
	for _, mode := range []string{"root", "nested", "standalone"} {
		t.Run(mode, func(t *testing.T) {
			workspace := t.TempDir()
			workspace, err = filepath.EvalSymlinks(workspace)
			if err != nil {
				t.Fatal(err)
			}
			root := workspace
			if mode == "nested" {
				root = filepath.Join(workspace, "apps", "native")
			}
			if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
				t.Fatal(err)
			}
			if mode != "standalone" {
				if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\nname = \"editor-check\"\nversion = \"0.1.0\"\nedition = \"2024\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			const source = "fn helper(value: i32) -> i32 { value + 1 }\nfn main() { let result = helper(1); println!(\"{result}\"); }\n"
			name := filepath.Join(root, "src", "main.rs")
			if err := os.WriteFile(name, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			connection, err := connectStdioLanguageServer(languageServerLaunch{Program: program, Root: workspace, InitializationOptions: rustLanguageServerInitializationOptions(workspace, nil, []string{filepath.ToSlash(mustRelForTest(t, workspace, name))})})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				connection.Close()
				time.Sleep(300 * time.Millisecond)
			})
			uri := languageServerFileURI(name)
			connection.DidOpen(uri, "rust", source, 1)
			request := func(method string, offset int) json.RawMessage {
				t.Helper()
				answer := make(chan languageServerResponse, 1)
				connection.Request(method, languageServerTextPosition{
					TextDocument: languageServerTextDocument{URI: uri}, Position: languageServerPositionAt(source, offset),
				}, func(raw json.RawMessage, err error) { answer <- languageServerResponse{result: raw, err: err} })
				select {
				case response := <-answer:
					if response.err != nil {
						return nil
					}
					return response.result
				case <-time.After(15 * time.Second):
					t.Fatalf("%s timed out", method)
					return nil
				}
			}
			var hoverRaw json.RawMessage
			var hover string
			var ok bool
			for range 20 {
				hoverRaw = request("textDocument/hover", strings.LastIndex(source, "result"))
				hover, _, ok = parseLanguageServerHover(hoverRaw)
				if ok && strings.Contains(hover, "result") {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !ok || !strings.Contains(hover, "result") {
				t.Fatalf("rust-analyzer hover = %s", hoverRaw)
			}
			var definitionRaw json.RawMessage
			var location languageServerLocation
			for range 20 {
				definitionRaw = request("textDocument/definition", strings.Index(source, "helper(1)"))
				location, ok = firstLanguageServerLocation(definitionRaw)
				if ok {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			definitionPath, pathOK := languageServerFilePath(location.URI)
			if !ok || !pathOK || !strings.EqualFold(filepath.Clean(definitionPath), filepath.Clean(name)) || location.Range.Start.Line != 0 {
				t.Fatalf("rust-analyzer definition = %s", definitionRaw)
			}
		})
	}
}

func TestTypeScriptInitializationWithoutManagedInstallation(t *testing.T) {
	for _, dataDir := range []string{"", t.TempDir()} {
		if options := languageServerInitializationOptions("typescript", dataDir); options != nil {
			t.Fatalf("missing managed installation supplied options: %#v", options)
		}
	}
}

func mustRelForTest(t *testing.T, root, path string) string {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return relative
}
