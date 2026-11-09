package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTypeScriptFileOffersManagedLanguageServerOnce(t *testing.T) {
	h := menuTestApp(t)
	h.app.offerLanguageServerInstall("src/main.ts")
	if len(h.app.pendingToasts) != 1 {
		t.Fatalf("install prompts = %d, want 1", len(h.app.pendingToasts))
	}
	toast := h.app.pendingToasts[0]
	if toast.title != "Install TypeScript Language Server?" || toast.action != "Download" || toast.onAction == nil {
		t.Fatalf("TypeScript install prompt = %#v", toast)
	}
	h.app.offerLanguageServerInstall("src/other.tsx")
	if len(h.app.pendingToasts) != 1 {
		t.Fatalf("the same install was offered more than once: %#v", h.app.pendingToasts)
	}

	h.app.settings.LanguageServers = map[string]languageServerSettings{"typescript": {Disabled: true}}
	h.app.languageServerInstallPrompted = nil
	h.app.pendingToasts = nil
	h.app.offerLanguageServerInstall("src/disabled.ts")
	if len(h.app.pendingToasts) != 0 {
		t.Fatalf("an explicitly disabled server was offered: %#v", h.app.pendingToasts)
	}
}

func TestOpeningTypeScriptFileShowsManagedInstallNotification(t *testing.T) {
	stubCodeWorkspace(t, map[string]string{"src/main.ts": "const answer = 42\n"})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("src/main.ts", 0)
	waitForCodeDocument(t, h, "src/main.ts")
	if !h.tester.HasText("Install TypeScript Language Server?") || !h.tester.HasText("Download") {
		t.Fatalf("TypeScript install notification is missing: %q", h.texts())
	}
}

func TestManagedLanguageServerInstallAutomaticallyConfiguresIt(t *testing.T) {
	original := performLanguageServerInstall
	t.Cleanup(func() { performLanguageServerInstall = original })
	performLanguageServerInstall = func(profile languageServerProfile, dataDir string) (languageServerInstallation, error) {
		if profile.ID != "typescript" {
			t.Fatalf("installed profile = %q", profile.ID)
		}
		if dataDir == "" {
			t.Fatal("installer received no data directory")
		}
		return languageServerInstallation{
			Path: "C:/TanGit/tools/node.exe", Arguments: []string{"server.js", "--stdio"},
			ProbeArguments: []string{"server.js", "--version"},
		}, nil
	}

	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.installLanguageServer("typescript")
	for range 8 {
		if !h.app.languageServerInstalling["typescript"] {
			break
		}
		h.updates.waitForUpdate(t)
	}
	if h.app.languageServerInstalling["typescript"] {
		t.Fatal("TypeScript installation remained busy after its update")
	}
	want := languageServerSettings{
		Path: "C:/TanGit/tools/node.exe", Arguments: []string{"server.js", "--stdio"},
		ProbeArguments: []string{"server.js", "--version"},
	}
	if got := h.app.settings.LanguageServers["typescript"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("installed TypeScript settings = %#v, want %#v", got, want)
	}
	if got := loadAppSettings(h.app.pathUserData).LanguageServers["typescript"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("persisted TypeScript settings = %#v, want %#v", got, want)
	}
	if draft := h.app.languageServerDrafts["typescript"]; draft == nil || !draft.Enabled || draft.Path != want.Path {
		t.Fatalf("installed TypeScript draft = %#v", draft)
	}
	if !hasToastTitle(h.app.pendingToasts, "TypeScript Language Server installed") {
		t.Fatalf("success notification missing: %#v", h.app.pendingToasts)
	}
}

func TestManagedLanguageServerInstallFailureDoesNotConfigureIt(t *testing.T) {
	original := performLanguageServerInstall
	t.Cleanup(func() { performLanguageServerInstall = original })
	performLanguageServerInstall = func(languageServerProfile, string) (languageServerInstallation, error) {
		return languageServerInstallation{}, errors.New("download unavailable")
	}
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.installLanguageServer("ruff")
	for range 8 {
		if !h.app.languageServerInstalling["ruff"] {
			break
		}
		h.updates.waitForUpdate(t)
	}
	if h.app.languageServerInstalling["ruff"] {
		t.Fatal("Ruff installation remained busy after its update")
	}
	if _, configured := h.app.settings.LanguageServers["ruff"]; configured {
		t.Fatalf("failed install wrote settings: %#v", h.app.settings.LanguageServers["ruff"])
	}
	if !hasToastTitle(h.app.pendingToasts, "Could not install Ruff") {
		t.Fatalf("failure notification missing: %#v", h.app.pendingToasts)
	}
}

func TestLanguageServerCatalogSearchesNamesLanguagesAndCommands(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"python", []string{"BasedPyright", "Ruff"}},
		{"docker-langserver", []string{"Dockerfile Language Server"}},
		{".vue", []string{"Tailwind CSS Language Server", "VS Code ESLint Language Server", "Vue Language Server", "Oxc / Oxlint"}},
	}
	for _, test := range tests {
		for _, profile := range languageServerProfiles {
			got := languageServerCatalogMatches(profile, test.query)
			want := false
			for _, name := range test.want {
				want = want || profile.Name == name
			}
			if got != want {
				t.Errorf("query %q match for %s = %v, want %v", test.query, profile.Name, got, want)
			}
		}
	}
}

func TestNodePackageExecutableUsesManifestAndRejectsEscapes(t *testing.T) {
	prefix := t.TempDir()
	packageDir := filepath.Join(prefix, "node_modules", "example-lsp")
	if err := os.MkdirAll(filepath.Join(packageDir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"bin":{"example":"bin/server.js"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(packageDir, "bin", "server.js")
	if err := os.WriteFile(want, []byte("server"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := nodePackageExecutable(prefix, "example-lsp", "example"); err != nil || got != want {
		t.Fatalf("node executable = %q, %v; want %q", got, err, want)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"bin":{"example":"../../../outside.js"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := nodePackageExecutable(prefix, "example-lsp", "example"); err == nil || !strings.Contains(err.Error(), "invalid executable path") {
		t.Fatalf("escaping executable error = %v", err)
	}
}

func TestRealManagedTypeScriptInstall(t *testing.T) {
	if os.Getenv("TANGIT_TEST_REAL_LSP_INSTALL") != "1" {
		t.Skip("set TANGIT_TEST_REAL_LSP_INSTALL=1 to download and exercise typescript-language-server")
	}
	profile, ok := languageServerProfileByID("typescript")
	if !ok {
		t.Fatal("TypeScript language-server profile is missing")
	}
	dataDir := t.TempDir()
	installation, err := installLanguageServerTool(profile, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(installation.Arguments) == 0 || !strings.HasSuffix(filepath.ToSlash(installation.Arguments[0]), "/typescript-language-server/lib/cli.mjs") {
		t.Fatalf("managed TypeScript launch = %q %q", installation.Path, installation.Arguments)
	}
	if len(installation.ProbeArguments) != 2 || installation.ProbeArguments[1] != "--version" {
		t.Fatalf("managed TypeScript probe = %q", installation.ProbeArguments)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, installation.Path, installation.ProbeArguments...)
	output, err := command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		t.Fatalf("managed TypeScript server version = %q, %v", output, err)
	}

	// A version probe succeeds even when the server cannot locate TypeScript.
	// Use a workspace without node_modules to exercise the managed fallback.
	root := t.TempDir()
	path := filepath.Join(root, "main.ts")
	source := "const answer: number = 42;\nanswer;\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	connection, err := connectStdioLanguageServer(languageServerLaunch{
		Program: installation.Path, Arguments: installation.Arguments, Root: root,
		InitializationOptions: languageServerInitializationOptions(profile.ID, dataDir),
	})
	if err != nil {
		t.Fatalf("initialize managed TypeScript in a workspace without TypeScript: %v", err)
	}
	defer connection.Close()
	connection.DidOpen(languageServerFileURI(path), "typescript", source, 1)
	type hoverResult struct {
		raw json.RawMessage
		err error
	}
	result := make(chan hoverResult, 1)
	connection.Request("textDocument/hover", map[string]any{
		"textDocument": map[string]string{"uri": languageServerFileURI(path)},
		"position":     map[string]int{"line": 1, "character": 2},
	}, func(raw json.RawMessage, err error) { result <- hoverResult{raw, err} })
	select {
	case response := <-result:
		if response.err != nil || !strings.Contains(string(response.raw), "number") {
			t.Fatalf("TypeScript hover = %s, %v", response.raw, response.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("TypeScript hover timed out")
	}

}

func hasToastTitle(toasts []appToast, title string) bool {
	for _, toast := range toasts {
		if toast.title == title {
			return true
		}
	}
	return false
}

func TestTypeScriptInstallerPinsCompatibleCompilerAndChecksItsServer(t *testing.T) {
	binDir := t.TempDir()
	for _, name := range []string{"node", "npm"} {
		if err := os.WriteFile(filepath.Join(binDir, executableName(name)), []byte("unused"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)
	original := runLanguageServerInstallCommand
	t.Cleanup(func() { runLanguageServerInstallCommand = original })
	for _, withServer := range []bool{false, true} {
		t.Run(fmt.Sprint("tsserver=", withServer), func(t *testing.T) {
			dataDir := t.TempDir()
			prefix := filepath.Join(dataDir, "tools", "node")
			runLanguageServerInstallCommand = func(_ context.Context, _ string, arguments, _ []string) ([]byte, error) {
				want := []string{"install", "--no-audit", "--no-fund", "--prefix", prefix, "typescript-language-server@latest", "typescript@6"}
				if !reflect.DeepEqual(arguments, want) {
					t.Errorf("npm arguments = %q, want %q", arguments, want)
				}
				serverDir := filepath.Join(prefix, "node_modules", "typescript-language-server")
				if err := os.MkdirAll(serverDir, 0o755); err != nil {
					return nil, err
				}
				if err := os.WriteFile(filepath.Join(serverDir, "package.json"), []byte(`{"bin":"cli.mjs"}`), 0o644); err != nil {
					return nil, err
				}
				if err := os.WriteFile(filepath.Join(serverDir, "cli.mjs"), nil, 0o644); err != nil {
					return nil, err
				}
				if withServer {
					tsDir := filepath.Join(prefix, "node_modules", "typescript", "lib")
					if err := os.MkdirAll(tsDir, 0o755); err != nil {
						return nil, err
					}
					if err := os.WriteFile(filepath.Join(tsDir, "tsserver.js"), nil, 0o644); err != nil {
						return nil, err
					}
				}
				return nil, nil
			}
			profile, _ := languageServerProfileByID("typescript")
			_, err := installLanguageServerTool(profile, dataDir)
			if withServer && err != nil {
				t.Fatal(err)
			}
			if !withServer && (err == nil || !strings.Contains(err.Error(), "tsserver.js")) {
				t.Fatalf("missing tsserver error = %v", err)
			}
		})
	}
}
