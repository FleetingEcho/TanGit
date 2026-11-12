package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type languageServerDocumentEvent struct {
	kind, uri, language, text string
	version                   uint64
}

type fakeLanguageServerConnection struct {
	mu      sync.Mutex
	events  []languageServerDocumentEvent
	done    chan error
	once    sync.Once
	closed  bool
	request func(method string, params any, result func(json.RawMessage, error))
}

func newFakeLanguageServerConnection() *fakeLanguageServerConnection {
	return &fakeLanguageServerConnection{done: make(chan error, 1)}
}

func (s *fakeLanguageServerConnection) DidOpen(uri, language, text string, version uint64) {
	s.record(languageServerDocumentEvent{kind: "open", uri: uri, language: language, text: text, version: version})
}

func (s *fakeLanguageServerConnection) DidChange(uri, text string, version uint64) {
	s.record(languageServerDocumentEvent{kind: "change", uri: uri, text: text, version: version})
}

func (s *fakeLanguageServerConnection) DidSave(uri string) {
	s.record(languageServerDocumentEvent{kind: "save", uri: uri})
}

func (s *fakeLanguageServerConnection) DidClose(uri string) {
	s.record(languageServerDocumentEvent{kind: "close", uri: uri})
}

func (s *fakeLanguageServerConnection) Request(method string, params any, result func(json.RawMessage, error)) {
	if s.request != nil {
		s.request(method, params, result)
	}
}

func (s *fakeLanguageServerConnection) record(event languageServerDocumentEvent) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
}

func (s *fakeLanguageServerConnection) snapshot() []languageServerDocumentEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]languageServerDocumentEvent(nil), s.events...)
}

func (s *fakeLanguageServerConnection) Done() <-chan error { return s.done }

func (s *fakeLanguageServerConnection) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		s.done <- nil
		close(s.done)
	})
}

func (s *fakeLanguageServerConnection) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func stubLanguageServerConnection(t *testing.T, connect func(languageServerLaunch) (languageServerConnection, error)) {
	t.Helper()
	original := connectLanguageServer
	originalLookPath := languageServerLookPath
	connectLanguageServer = connect
	languageServerLookPath = func(program string) (string, error) { return program, nil }
	t.Cleanup(func() {
		connectLanguageServer = original
		languageServerLookPath = originalLookPath
	})
}

func TestGoLanguageServerSynchronizesOpenChangeAndClose(t *testing.T) {
	connection := newFakeLanguageServerConnection()
	launches := make(chan languageServerLaunch, 1)
	stubLanguageServerConnection(t, func(launch languageServerLaunch) (languageServerConnection, error) {
		launches <- launch
		return connection, nil
	})
	stubCodeWorkspace(t, map[string]string{"main.go": "package main\n"})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("main.go", 0)
	document := waitForCodeDocument(t, h, "main.go")
	h.waitFor(t, "the gopls connection and didOpen", func() bool {
		return h.app.goLanguageServer == connection && len(connection.snapshot()) >= 1
	})
	launch := <-launches
	if launch.Program != "gopls" || launch.Root != "/work/project" || len(launch.Arguments) != 0 {
		t.Fatalf("default gopls launch = %#v", launch)
	}
	events := connection.snapshot()
	if got := events[0]; got.kind != "open" || got.language != "go" || got.text != "package main\n" ||
		got.uri != "file:///work/project/main.go" {
		t.Fatalf("didOpen = %#v", got)
	}

	document.state.Insert("// changed\n")
	h.tester.Frame()
	h.tester.Frame()
	events = connection.snapshot()
	if len(events) != 2 {
		t.Fatalf("editing emitted a save before the user saved: %#v", events)
	}
	h.app.saveCodeDocument(document)
	events = connection.snapshot()
	if len(events) < 3 || events[1].kind != "change" || !strings.Contains(events[1].text, "// changed") ||
		events[1].version <= events[0].version {
		t.Fatalf("didChange events = %#v", events)
	}
	if events[2].kind != "save" {
		t.Fatalf("didSave events = %#v", events)
	}

	h.app.closeCodeFile("main.go")
	h.tester.Frame()
	h.waitFor(t, "didClose and server shutdown", func() bool {
		events = connection.snapshot()
		return len(events) >= 4 && connection.isClosed()
	})
	if events[3].kind != "close" || events[3].uri != events[0].uri {
		t.Fatalf("final language-server event = %#v", events[3])
	}
}

// Go has its own server lifecycle, but preview replacement must preserve the
// same warm workspace as the shared additional-server path.
func TestGoLanguageServerSurvivesPreviewReplacement(t *testing.T) {
	connection := newFakeLanguageServerConnection()
	launches := make(chan languageServerLaunch, 2)
	stubLanguageServerConnection(t, func(launch languageServerLaunch) (languageServerConnection, error) {
		launches <- launch
		return connection, nil
	})
	stubCodeWorkspace(t, map[string]string{
		"first.go":  "package first\n",
		"second.go": "package second\n",
	})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 2)
	h.app.openCodeFile("first.go", 0)
	waitForCodeDocument(t, h, "first.go")
	h.waitFor(t, "the first Go document", func() bool {
		return len(connection.snapshot()) == 1
	})

	h.app.openCodeFile("second.go", 0)
	waitForCodeDocument(t, h, "second.go")
	h.tester.Frame()
	events := connection.snapshot()
	if len(events) != 3 {
		t.Fatalf("Go preview replacement emitted %#v, want two opens and one close", events)
	}
	if events[1].kind != "open" || !strings.HasSuffix(events[1].uri, "/second.go") ||
		events[2].kind != "close" || !strings.HasSuffix(events[2].uri, "/first.go") {
		t.Fatalf("Go preview replacement events = %#v, want open second before close first", events)
	}
	if got := len(launches); got != 1 {
		t.Fatalf("gopls launches = %d, want one server retained across preview replacement", got)
	}
}

func TestGoLanguageServerUsesConfiguredProgramAndRejectsStaleStartup(t *testing.T) {
	connection := newFakeLanguageServerConnection()
	started := make(chan languageServerLaunch, 1)
	release := make(chan struct{})
	stubLanguageServerConnection(t, func(launch languageServerLaunch) (languageServerConnection, error) {
		started <- launch
		<-release
		return connection, nil
	})
	stubCodeWorkspace(t, map[string]string{"main.go": "package main\n"})
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"go": {Path: "C:/tools/gopls.exe", Arguments: []string{"-remote=auto"}},
	}
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("main.go", 0)
	waitForCodeDocument(t, h, "main.go")
	var launch languageServerLaunch
	select {
	case launch = <-started:
	default:
		h.tester.Frame()
		launch = <-started
	}
	if launch.Program != "C:/tools/gopls.exe" || !reflect.DeepEqual(launch.Arguments, []string{"-remote=auto"}) {
		t.Fatalf("configured launch = %#v", launch)
	}
	h.updates = newUpdates()
	h.app.update = h.updates.update
	repaint := true
	h.app.updateChanged = func(fn func() bool) {
		h.app.update(func() { repaint = fn() })
	}
	h.app.resetCode()
	close(release)
	h.updates.waitForUpdate(t)
	if repaint {
		t.Fatal("a stale language-server startup repainted the window")
	}
	h.tester.Frame()
	h.waitFor(t, "the stale connection shutdown", connection.isClosed)
	if h.app.goLanguageServer != nil {
		t.Fatal("a connection from before reset became active")
	}
}

func TestGoLanguageServerFallsBackToNextConfiguredProgram(t *testing.T) {
	connection := newFakeLanguageServerConnection()
	launches := make(chan string, 2)
	stubLanguageServerConnection(t, func(launch languageServerLaunch) (languageServerConnection, error) {
		launches <- launch.Program
		if launch.Program == "/missing/gopls" {
			return nil, errors.New("not installed")
		}
		return connection, nil
	})
	stubCodeWorkspace(t, map[string]string{"main.go": "package main\n"})
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"go": {Path: "/missing/gopls, /zed/gopls"},
	}
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("main.go", 0)
	waitForCodeDocument(t, h, "main.go")
	h.waitFor(t, "the fallback gopls connection", func() bool { return h.app.goLanguageServer == connection })

	if first, second := <-launches, <-launches; first != "/missing/gopls" || second != "/zed/gopls" {
		t.Fatalf("launch order = %q then %q, want the configured fallback order", first, second)
	}
}

func TestLanguageServerFileURIAndMessageFraming(t *testing.T) {
	if got := languageServerFileURI(`C:\work tree\repo\main.go`); got != "file:///C:/work%20tree/repo/main.go" {
		t.Fatalf("Windows file URI = %q", got)
	}
	reader := bufio.NewReader(strings.NewReader("Content-Type: application/vscode-jsonrpc; charset=utf-8\r\nContent-Length: 7\r\n\r\n{\"x\":1}"))
	message, err := readLanguageServerMessage(reader)
	if err != nil || string(message) != `{"x":1}` {
		t.Fatalf("framed message = %q, %v", message, err)
	}
}

func TestStdioLanguageServerInitializesAndShutsDown(t *testing.T) {
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	environment := []string{"TANGIT_LANGUAGE_SERVER_HELPER=1"}
	if runtime.GOOS == "darwin" {
		// Model a server that needs another tool beside its executable, while
		// launched from Finder with only system directories in PATH.
		bin := t.TempDir()
		link := filepath.Join(bin, "helper-server")
		if err := os.Symlink(executable, link); err != nil {
			t.Fatal(err)
		}
		executable = link
		tool := filepath.Join(bin, "tangit-helper-tool")
		if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		environment = append(environment, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "TANGIT_LANGUAGE_SERVER_EXPECTED_TOOL="+tool)
	}
	connection, err := connectStdioLanguageServer(languageServerLaunch{
		Program:               executable,
		Arguments:             []string{"-test.run=^TestLanguageServerHelperProcess$"},
		Root:                  t.TempDir(),
		Env:                   environment,
		InitializationOptions: map[string]any{"test": map[string]any{"value": "enabled"}},
	})
	if err != nil {
		t.Fatalf("connect helper language server: %v", err)
	}
	connection.DidOpen("file:///workspace/main.go", "go", "package main\n", 1)
	connection.DidChange("file:///workspace/main.go", "package main\n// changed\n", 2)
	connection.DidSave("file:///workspace/main.go")
	connection.DidClose("file:///workspace/main.go")
	hover := make(chan json.RawMessage, 1)
	connection.Request("textDocument/hover", map[string]any{}, func(raw json.RawMessage, err error) {
		if err != nil {
			t.Errorf("hover request: %v", err)
		}
		hover <- raw
	})
	response := <-hover
	if !strings.Contains(string(response), "helper hover") {
		t.Fatalf("hover response = %s", response)
	}
	requestError := make(chan error, 1)
	connection.Request("test/contentModified", nil, func(_ json.RawMessage, err error) {
		requestError <- err
	})
	if err := <-requestError; !isLanguageServerContentModified(err) {
		t.Fatalf("content-modified response lost its error code: %v", err)
	}
	connection.Close()
	select {
	case <-connection.Done():
	default:
		t.Fatal("shutdown returned before the helper process stopped")
	}
}

func TestLanguageServerHelperProcess(t *testing.T) {
	if os.Getenv("TANGIT_LANGUAGE_SERVER_HELPER") != "1" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	for {
		message, err := readLanguageServerMessage(reader)
		if err != nil {
			os.Exit(2)
		}
		var envelope languageServerEnvelope
		if json.Unmarshal(message, &envelope) != nil {
			os.Exit(3)
		}
		switch envelope.Method {
		case "initialize":
			if expected := os.Getenv("TANGIT_LANGUAGE_SERVER_EXPECTED_TOOL"); expected != "" {
				path, err := exec.LookPath(filepath.Base(expected))
				if err != nil || path != expected {
					os.Exit(5)
				}
			}
			var params struct {
				Capabilities struct {
					Window struct {
						WorkDoneProgress bool `json:"workDoneProgress"`
					} `json:"window"`
					Experimental struct {
						ServerStatusNotification bool `json:"serverStatusNotification"`
					} `json:"experimental"`
				} `json:"capabilities"`
				InitializationOptions struct {
					Test struct {
						Value string `json:"value"`
					} `json:"test"`
				} `json:"initializationOptions"`
			}
			if json.Unmarshal(envelope.Params, &params) != nil ||
				params.InitializationOptions.Test.Value != "enabled" ||
				!params.Capabilities.Window.WorkDoneProgress ||
				!params.Capabilities.Experimental.ServerStatusNotification {
				os.Exit(4)
			}
			writeLanguageServerTestResponse(writer, envelope.ID, map[string]any{"capabilities": map[string]any{}})
		case "shutdown":
			writeLanguageServerTestResponse(writer, envelope.ID, nil)
		case "textDocument/hover":
			writeLanguageServerTestResponse(writer, envelope.ID, map[string]any{"contents": "helper hover"})
		case "test/contentModified":
			message, _ := json.Marshal(languageServerEnvelope{
				JSONRPC: "2.0", ID: envelope.ID,
				Error: &languageServerRPCError{Code: -32801, Message: "content modified"},
			})
			_, _ = writer.WriteString("Content-Length: " + strconv.Itoa(len(message)) + "\r\n\r\n")
			_, _ = writer.Write(message)
			_ = writer.Flush()
		case "exit":
			_ = writer.Flush()
			os.Exit(0)
		}
	}
}

func writeLanguageServerTestResponse(writer *bufio.Writer, id json.RawMessage, result any) {
	message, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_, _ = writer.WriteString("Content-Length: " + strconv.Itoa(len(message)) + "\r\n\r\n")
	_, _ = writer.Write(message)
	_ = writer.Flush()
}
