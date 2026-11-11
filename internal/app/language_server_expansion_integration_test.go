package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClangdRealProtocolLifecycle(t *testing.T) {
	program, err := languageServerLookPath("clangd")
	if err != nil {
		t.Skip("clangd is not installed")
	}
	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.cpp")
	text := "int tangit_answer = 42;\nint main() { return tangit_answer; }\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	notifications := make(chan languageServerEnvelope, 32)
	connection, err := connectStdioLanguageServer(languageServerLaunch{
		Program: program,
		Root:    workspace,
		Notify: func(method string, params json.RawMessage) {
			if method == "textDocument/publishDiagnostics" {
				select {
				case notifications <- languageServerEnvelope{Method: method, Params: params}:
				default:
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := connection.(*stdioLanguageServer)
	defer server.Close()
	uri := languageServerFileURI(path)
	server.DidOpen(uri, "cpp", text, 1)
	position := func(offset int) languageServerTextPosition {
		return languageServerTextPosition{
			TextDocument: languageServerTextDocument{URI: uri},
			Position:     languageServerPositionAt(text, offset),
		}
	}
	hover, err := server.request("textDocument/hover", position(strings.LastIndex(text, "tangit_answer")), 15*time.Second)
	if err != nil {
		t.Fatalf("clangd hover: %v", err)
	}
	content, _, ok := parseLanguageServerHover(hover)
	if !ok || !strings.Contains(content, "tangit_answer") {
		t.Fatalf("clangd hover = %s", hover)
	}
	completionOffset := strings.Index(text, "tangit_answer;") + len("tan")
	completions, err := server.request("textDocument/completion", position(completionOffset), 15*time.Second)
	if err != nil {
		t.Fatalf("clangd completion: %v", err)
	}
	items := parseLanguageServerCompletions(completions)
	foundCompletion := false
	for _, item := range items {
		if strings.TrimSpace(item.Label) == "tangit_answer" {
			foundCompletion = true
			break
		}
	}
	if !foundCompletion {
		t.Fatalf("clangd completion does not contain tangit_answer: %s", completions)
	}
	definition, err := server.request("textDocument/definition", position(strings.LastIndex(text, "tangit_answer")), 15*time.Second)
	if err != nil {
		t.Fatalf("clangd definition: %v", err)
	}
	location, ok := firstLanguageServerLocation(definition)
	if !ok || location.URI != uri || location.Range.Start.Line != 0 {
		t.Fatalf("clangd definition = %s", definition)
	}

	changed := strings.Replace(text, "tangit_answer; }", "missing_name; }", 1)
	server.DidChange(uri, changed, 2)
	for {
		select {
		case notification := <-notifications:
			var params struct {
				URI         string `json:"uri"`
				Diagnostics []struct {
					Message string `json:"message"`
				} `json:"diagnostics"`
			}
			if json.Unmarshal(notification.Params, &params) != nil || params.URI != uri {
				continue
			}
			for _, diagnostic := range params.Diagnostics {
				if strings.Contains(diagnostic.Message, "missing_name") {
					return
				}
			}
		case <-time.After(15 * time.Second):
			t.Fatal("clangd did not publish diagnostics for the changed document")
		}
	}
}
