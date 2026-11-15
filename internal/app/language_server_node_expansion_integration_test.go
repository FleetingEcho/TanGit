package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntelephenseRealProtocolLifecycle(t *testing.T) {
	if os.Getenv("TANGIT_TEST_REAL_LSP_INSTALL") != "1" {
		t.Skip("set TANGIT_TEST_REAL_LSP_INSTALL=1 to download and exercise Intelephense")
	}
	profile := mustLanguageServerProfile(t, "intelephense")
	installation, dataDir := installRealExpandedLanguageServer(t, profile)
	workspace := t.TempDir()
	name := filepath.Join(workspace, "index.php")
	text := "<?php\n$tangit_answer = 42;\necho $tan;\necho $tangit_answer;\n"
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	server, diagnostics := openRealExpandedLanguageServer(t, installation, dataDir, workspace, name, profile.ID, "php", text)
	defer server.Close()
	uri := languageServerFileURI(name)
	position := realExpandedLanguageServerPosition(uri, text, strings.LastIndex(text, "tangit_answer")+1)
	hover := requestRealExpandedLanguageServer(t, server, "textDocument/hover", position)
	content, _, ok := parseLanguageServerHover(hover)
	if !ok || strings.TrimSpace(content) == "" {
		t.Fatalf("Intelephense hover = %s", hover)
	}
	completions := requestRealExpandedLanguageServer(t, server, "textDocument/completion", realExpandedLanguageServerPosition(uri, text, strings.Index(text, "$tan;")+len("$tan")))
	assertRealExpandedCompletion(t, completions, "tangit_answer")
	definition := requestRealExpandedLanguageServer(t, server, "textDocument/definition", position)
	location, ok := firstLanguageServerLocation(definition)
	if !ok || location.URI != uri || location.Range.Start.Line != 1 {
		t.Fatalf("Intelephense definition = %s", definition)
	}
	changed := strings.Replace(text, "echo $tan;", "echo ;", 1)
	server.DidChange(uri, changed, 2)
	waitForRealExpandedDiagnostic(t, diagnostics, uri, "")
}

func TestPrismaRealProtocolLifecycle(t *testing.T) {
	if os.Getenv("TANGIT_TEST_REAL_LSP_INSTALL") != "1" {
		t.Skip("set TANGIT_TEST_REAL_LSP_INSTALL=1 to download and exercise Prisma Language Server")
	}
	profile := mustLanguageServerProfile(t, "prisma")
	installation, dataDir := installRealExpandedLanguageServer(t, profile)
	workspace := t.TempDir()
	name := filepath.Join(workspace, "schema.prisma")
	text := "generator client {\n  provider = \"prisma-client-js\"\n}\n\ndatasource db {\n  provider = \"sqlite\"\n  url      = \"file:dev.db\"\n}\n\nmodel Tangit {\n  id       Int    @id @default(autoincrement())\n  answer   Int    @def\n  relation TangitRelation?\n}\n\nmodel TangitRelation {\n  id       Int    @id\n  tangit   Tangit @relation(fields: [tangitId], references: [id])\n  tangitId Int\n}\n"
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	server, diagnostics := openRealExpandedLanguageServer(t, installation, dataDir, workspace, name, profile.ID, "prisma", text)
	defer server.Close()
	uri := languageServerFileURI(name)
	position := realExpandedLanguageServerPosition(uri, text, strings.Index(text, "Tangit @relation"))
	hover := requestRealExpandedLanguageServer(t, server, "textDocument/hover", position)
	if content, _, ok := parseLanguageServerHover(hover); !ok || strings.TrimSpace(content) == "" {
		t.Fatalf("Prisma hover = %s", hover)
	}
	completions := requestRealExpandedLanguageServer(t, server, "textDocument/completion", realExpandedLanguageServerPosition(uri, text, strings.Index(text, "@def")+len("@def")))
	assertRealExpandedCompletion(t, completions, "relation")
	definition := requestRealExpandedLanguageServer(t, server, "textDocument/definition", position)
	location, ok := firstLanguageServerLocation(definition)
	if !ok || location.URI != uri || location.Range.Start.Line != 9 {
		t.Fatalf("Prisma definition = %s", definition)
	}
	changed := strings.Replace(text, "@def", "@unknown", 1)
	server.DidChange(uri, changed, 2)
	waitForRealExpandedDiagnostic(t, diagnostics, uri, "unknown")
}

func installRealExpandedLanguageServer(t *testing.T, profile languageServerProfile) (languageServerInstallation, string) {
	t.Helper()
	dataDir := t.TempDir()
	installation, err := installLanguageServerTool(profile, dataDir)
	if err != nil {
		t.Fatalf("install %s: %v", profile.Name, err)
	}
	installation, version, err := prepareLanguageServerInstallation(profile, dataDir, installation)
	if err != nil {
		t.Fatalf("probe %s: %v", profile.Name, err)
	}
	if version == "" {
		t.Fatalf("%s returned an empty version", profile.Name)
	}
	return installation, dataDir
}

func openRealExpandedLanguageServer(t *testing.T, installation languageServerInstallation, dataDir, workspace, name, id, language, text string) (*stdioLanguageServer, <-chan languageServerEnvelope) {
	t.Helper()
	diagnostics := make(chan languageServerEnvelope, 64)
	connection, err := connectStdioLanguageServer(languageServerLaunch{
		Program: installation.Path, Arguments: installation.Arguments, Root: workspace,
		InitializationOptions: languageServerInitializationOptions(id, dataDir),
		Notify: func(method string, params json.RawMessage) {
			if method == "textDocument/publishDiagnostics" || (id == "neocmake" && method == "$/progress") {
				select {
				case diagnostics <- languageServerEnvelope{Method: method, Params: params}:
				default:
				}
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := connection.(*stdioLanguageServer)
	server.DidOpen(languageServerFileURI(name), language, text, 1)
	return server, diagnostics
}

func waitForRealExpandedLanguageServerReady(t *testing.T, notifications <-chan languageServerEnvelope, id string) {
	t.Helper()
	if id != "neocmake" {
		return
	}
	initializing := ""
	for {
		select {
		case notification := <-notifications:
			if notification.Method != "$/progress" {
				continue
			}
			var progress struct {
				Token json.RawMessage `json:"token"`
				Value struct {
					Kind  string `json:"kind"`
					Title string `json:"title"`
				} `json:"value"`
			}
			if json.Unmarshal(notification.Params, &progress) != nil {
				continue
			}
			if progress.Value.Kind == "begin" && progress.Value.Title == "Initializing workspace" {
				initializing = string(progress.Token)
			}
			if progress.Value.Kind == "end" && initializing != "" && string(progress.Token) == initializing {
				return
			}
		case <-time.After(20 * time.Second):
			t.Fatal("NeoCMakeLSP did not finish initializing its workspace")
		}
	}
}

func realExpandedLanguageServerPosition(uri, text string, offset int) languageServerTextPosition {
	return languageServerTextPosition{
		TextDocument: languageServerTextDocument{URI: uri},
		Position:     languageServerPositionAt(text, offset),
	}
}

func requestRealExpandedLanguageServer(t *testing.T, server *stdioLanguageServer, method string, params any) json.RawMessage {
	t.Helper()
	result, err := server.request(method, params, 20*time.Second)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return result
}

func assertRealExpandedCompletion(t *testing.T, raw json.RawMessage, want string) {
	t.Helper()
	for _, item := range parseLanguageServerCompletions(raw) {
		if strings.Contains(strings.ToLower(strings.TrimSpace(item.Label)), strings.ToLower(want)) {
			return
		}
	}
	t.Fatalf("completion does not contain %q: %s", want, raw)
}

func waitForRealExpandedDiagnostic(t *testing.T, notifications <-chan languageServerEnvelope, uri, contains string) {
	t.Helper()
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
				if contains == "" || strings.Contains(strings.ToLower(diagnostic.Message), strings.ToLower(contains)) {
					return
				}
			}
		case <-time.After(20 * time.Second):
			t.Fatalf("no diagnostic containing %q arrived for %s", contains, uri)
		}
	}
}
