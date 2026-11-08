package app

import (
	"context"
	"errors"
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
	installation, err := installLanguageServerTool(profile, t.TempDir())
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
}

func hasToastTitle(toasts []appToast, title string) bool {
	for _, toast := range toasts {
		if toast.title == title {
			return true
		}
	}
	return false
}
