package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestLanguageServerContentModifiedDoesNotPoisonServerStatus(t *testing.T) {
	for _, language := range []string{"go", "rust"} {
		t.Run(language, func(t *testing.T) {
			connection := newFakeLanguageServerConnection()
			var answer func(json.RawMessage, error)
			connection.request = func(_ string, _ any, result func(json.RawMessage, error)) {
				answer = result
			}
			stubLanguageServerConnection(t, func(languageServerLaunch) (languageServerConnection, error) {
				return connection, nil
			})
			path, text := "main.go", "package main\n"
			if language == "rust" {
				path, text = "main.rs", "fn main() {}\n"
			}
			stubCodeWorkspace(t, map[string]string{path: text})
			h := menuTestApp(t)
			h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Disabled: language != "go"}, "rust": {}}
			openCodePageForTest(t, h, 1)
			h.app.openCodeFile(path, 0)
			document := waitForCodeDocument(t, h, path)
			h.waitFor(t, "the language server", func() bool { return len(connection.snapshot()) > 0 })
			h.tester.Frame()
			document.state.Select(9, 9)

			serverError := func() string {
				if language == "go" {
					return h.app.goLanguageServerError
				}
				return h.app.additionalLanguageServers[language].error
			}
			request := func() {
				t.Helper()
				answer = nil
				document.state.OnHover(editor.HoverRequest{Offset: 9, Version: document.state.Buffer.Version()})
				if answer == nil {
					t.Fatal("hover was not dispatched")
				}
			}
			request()
			toasts := len(h.app.pendingToasts)
			answer(nil, &languageServerRPCError{Code: -32801, Message: "content modified"})
			h.updates.waitForUpdate(t)
			if serverError() != "" || len(h.app.pendingToasts) != toasts {
				t.Fatalf("content modified reported a server failure: %q", serverError())
			}

			request()
			answer(json.RawMessage(`{"contents":"recovered hover"}`), nil)
			h.updates.waitForUpdate(t)
			if document.hoverState == nil || document.hoverState.Buffer.String() != "recovered hover" {
				t.Fatal("the next hover did not recover")
			}

			request()
			toasts = len(h.app.pendingToasts)
			answer(nil, errLanguageServerRequestTimedOut)
			h.updates.waitForUpdate(t)
			if serverError() != "" || len(h.app.pendingToasts) != toasts {
				t.Fatalf("a transient request timeout poisoned the running server: %q", serverError())
			}

			request()
			answer(nil, &languageServerRPCError{Code: -32603, Message: "internal error"})
			h.updates.waitForUpdate(t)
			if serverError() == "" || len(h.app.pendingToasts) <= toasts {
				t.Fatal("a real server error was suppressed")
			}
		})
	}
}
