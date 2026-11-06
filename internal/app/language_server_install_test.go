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
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTypeScriptFileOffersManagedLanguageServerOnce(t *testing.T) {
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
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

func TestEnabledMissingLanguageServersOfferManagedInstalls(t *testing.T) {
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"typescript": {Disabled: true},
		"tailwind":   {},
		"oxlint":     {},
	}
	h.app.offerLanguageServerInstall("src/main.tsx")
	for _, title := range []string{"Install Tailwind CSS Language Server?", "Install Oxc / Oxlint?"} {
		if !hasToastTitle(h.app.pendingToasts, title) {
			t.Errorf("missing install prompt %q: %#v", title, h.app.pendingToasts)
		}
	}
	if len(h.app.pendingToasts) != 2 {
		t.Fatalf("install prompts = %d, want only the two enabled missing servers", len(h.app.pendingToasts))
	}
}

func TestMissingLanguageServerIsDownloadRequiredInsteadOfFailed(t *testing.T) {
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	h := menuTestApp(t)
	profile, _ := languageServerProfileByID("tailwind")
	server := &managedLanguageServer{profile: profile}
	h.app.settings.LanguageServers = map[string]languageServerSettings{"tailwind": {}}
	h.app.additionalLanguageServers = map[string]*managedLanguageServer{"tailwind": server}
	h.app.startManagedLanguageServer(server)
	statuses := h.app.codeLanguageServerStatuses()
	if len(statuses) != 1 || !statuses[0].missing || statuses[0].failed || server.error != "" {
		t.Fatalf("missing language-server status = %#v, server error %q", statuses, server.error)
	}
}

func TestManagedLegacyTypeScriptServerIsDownloadRequired(t *testing.T) {
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(program string) (string, error) { return program, nil }

	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	prefix := filepath.Join(h.app.pathUserData, "tools", "node")
	cli := filepath.Join(prefix, "node_modules", "typescript-language-server", "lib", "cli.mjs")
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"typescript": {Path: "node", Arguments: []string{cli, "--stdio"}},
	}
	profile, _ := languageServerProfileByID("typescript")
	server := &managedLanguageServer{profile: profile}
	h.app.additionalLanguageServers = map[string]*managedLanguageServer{"typescript": server}

	h.app.startManagedLanguageServer(server)
	statuses := h.app.codeLanguageServerStatuses()
	if len(statuses) != 1 || !statuses[0].missing || statuses[0].failed || server.starting {
		t.Fatalf("incomplete managed TypeScript status = %#v, server = %#v", statuses, server)
	}

	tsc := filepath.Join(prefix, "node_modules", "typescript", "bin", "tsc")
	if err := os.MkdirAll(filepath.Dir(tsc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tsc, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "node_modules", "typescript", "package.json"), []byte(`{"version":"7.0.2","bin":{"tsc":"bin/tsc"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h.app.settings.LanguageServers["typescript"] = languageServerSettings{Path: "node", Arguments: []string{tsc, "--lsp", "--stdio"}}
	if !h.app.languageServerExecutableAvailable(profile) {
		t.Fatal("a complete managed TypeScript installation was still considered missing")
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

func TestLanguageServerCatalogUsesCompactActionsSearchAndScroll(t *testing.T) {
	h := menuTestApp(t)
	h.app.settingsPage = settingsPageEditor
	h.app.openSettings()
	h.tester.Frame()
	h.tester.Scroll(700, 480, 0, 1800)
	h.tester.Frame()
	h.app.languageServerCatalogQuery = "TypeScript"
	h.tester.Frame()

	download, downloadOK := h.tester.Find("Download TypeScript Language Server")
	configure, configureOK := h.tester.Find("Configure TypeScript Language Server")
	results, resultsOK := h.tester.Find("Language server results")
	source, sourceOK := h.tester.Find("Open download source: TypeScript Language Server")
	if !downloadOK || !configureOK || !resultsOK || !sourceOK {
		t.Fatalf("catalog controls missing: download=%v configure=%v scroll=%v source=%v", downloadOK, configureOK, resultsOK, sourceOK)
	}
	const typeScriptSource = "https://www.npmjs.com/package/typescript"
	if source.W <= 0 || languageServerDownloadURLs["typescript"] != typeScriptSource {
		t.Fatalf("TypeScript download source is not visible or incorrect: bounds=%+v url=%q", source, languageServerDownloadURLs["typescript"])
	}
	if download.W > 40 || configure.W > 40 {
		t.Fatalf("catalog actions are not compact icons: download=%+v configure=%+v", download, configure)
	}
	if results.H <= 0 || results.H > 420 {
		t.Fatalf("catalog results have unbounded height %.0f", results.H)
	}

	h.app.languageServerCatalogQuery = ""
	h.tester.Frame()
	if err := h.tester.Click("Search language servers"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("python")
	h.tester.Frame()
	if !h.tester.HasText("ty") || !h.tester.HasText("BasedPyright") || !h.tester.HasText("Ruff") || h.tester.HasText("TypeScript Language Server") {
		t.Fatalf("catalog search did not filter its scrollable results: %q", h.texts())
	}
}

func TestLanguageServerCatalogSortsEnabledThenName(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"go":           {Disabled: true},
		"typescript":   {},
		"basedpyright": {},
		"custom-zeta": {
			Path: "zeta", Custom: &customLanguageServerDefinition{Name: "Zeta", Extensions: map[string]string{".zeta": "zeta"}},
		},
		"custom-alpha": {
			Path: "alpha", Custom: &customLanguageServerDefinition{Name: "Alpha", Extensions: map[string]string{".alpha": "alpha"}},
		},
		"custom-disabled": {
			Disabled: true, Path: "disabled", Custom: &customLanguageServerDefinition{Name: "Disabled custom", Extensions: map[string]string{".disabled": "disabled"}},
		},
	}
	h.app.loadGoLanguageServerDraft()
	profiles := []languageServerProfile{
		{ID: "html", Name: "VS Code HTML Language Server"},
		{ID: "go", Name: "Go (gopls)"},
		{ID: "typescript", Name: "TypeScript Language Server"},
		{ID: "css", Name: "VS Code CSS Language Server"},
		{ID: "basedpyright", Name: "BasedPyright"},
	}
	h.app.sortLanguageServerCatalogProfiles(profiles)
	got := make([]string, len(profiles))
	for index, profile := range profiles {
		got[index] = profile.ID
	}
	want := []string{"basedpyright", "typescript", "go", "css", "html"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog order = %v, want enabled first then names %v", got, want)
	}

	custom := []languageServerProfile{
		{ID: "custom-zeta", Name: "Zeta", Custom: true},
		{ID: "custom-disabled", Name: "Disabled custom", Custom: true},
		{ID: "custom-alpha", Name: "Alpha", Custom: true},
	}
	h.app.sortLanguageServerCatalogProfiles(custom)
	if got := []string{custom[0].ID, custom[1].ID, custom[2].ID}; !reflect.DeepEqual(got, []string{"custom-alpha", "custom-zeta", "custom-disabled"}) {
		t.Fatalf("custom catalog order = %v", got)
	}
}

func TestLanguageServerEnableRequiresAnExecutable(t *testing.T) {
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(program string) (string, error) {
		return "", exec.ErrNotFound
	}
	h := menuTestApp(t)
	h.app.settingsPage = settingsPageEditor
	h.app.openSettings()
	h.tester.Frame()
	h.tester.Scroll(700, 480, 0, 1800)
	h.tester.Frame()
	h.app.languageServerCatalogQuery = "Oxlint"
	h.tester.Frame()

	if err := h.tester.Click("Enable Oxc / Oxlint"); err != nil {
		t.Fatalf("the Oxlint enable switch is missing: %v", err)
	}
	h.tester.Frame()
	if _, configured := h.app.settings.LanguageServers["oxlint"]; configured {
		t.Fatal("Oxlint was enabled without an installed executable")
	}
	if draft := h.app.languageServerDrafts["oxlint"]; draft == nil || draft.Enabled {
		t.Fatalf("Oxlint switch did not remain disabled: %#v", draft)
	}
	if !h.tester.HasText("Download required") {
		t.Fatalf("the download reminder is missing: %q", h.texts())
	}
}

func TestLanguageServerCatalogAddsAndTogglesCustomTy(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.settingsPage = settingsPageEditor
	h.app.openSettings()
	h.tester.Frame()
	h.tester.Scroll(700, 480, 0, 1800)
	h.tester.Frame()
	h.tester.Scroll(700, 480, 0, -300)
	h.tester.Frame()
	if err := h.tester.Click("Add custom language server"); err != nil {
		t.Fatalf("add custom server: %v; texts=%q", err, h.texts())
	}
	fields := []struct{ label, value string }{
		{"Custom server name", "My ty"},
		{"Custom server description", "Astral Python language server"},
		{"Custom server executables", "ty, /opt/python/ty"},
		{"Custom server arguments", "server"},
		{"Custom server file extensions", ".py=python, .pyi=python"},
		{"Custom server download URL", "https://pypi.org/project/ty/"},
	}
	for _, field := range fields {
		h.tester.Frame()
		if bounds, found := h.tester.Find(field.label); !found || bounds.W <= 0 || bounds.H <= 0 {
			h.tester.Scroll(700, 480, 0, 180)
			h.tester.Frame()
		}
		if err := h.tester.Click(field.label); err != nil {
			t.Fatalf("focus %s: %v; texts=%q", field.label, err, h.texts())
		}
		h.tester.Type(field.value)
	}
	h.tester.Frame()
	h.tester.Scroll(700, 480, 0, 600)
	h.tester.Frame()
	if err := h.tester.Click("Save custom language server"); err != nil {
		t.Fatalf("save custom ty: %v; texts=%q", err, h.texts())
	}
	server, ok := h.app.settings.LanguageServers["custom-my-ty"]
	if !ok || server.Custom == nil || server.Disabled || server.Path != "ty, /opt/python/ty" ||
		!reflect.DeepEqual(server.Arguments, []string{"server"}) ||
		server.Custom.Extensions[".py"] != "python" || server.Custom.Extensions[".pyi"] != "python" {
		t.Fatalf("custom ty settings = %#v", server)
	}
	if reloaded := loadAppSettings(h.app.pathUserData).LanguageServers["custom-my-ty"]; reloaded.Custom == nil ||
		reloaded.Custom.DownloadURL != "https://pypi.org/project/ty/" || !reflect.DeepEqual(reloaded, server) {
		t.Fatalf("persisted custom ty = %#v, want %#v", reloaded, server)
	}

	profiles := configuredLanguageServerProfiles(h.app.settings.LanguageServers, "src/main.py")
	if len(profiles) != 1 || profiles[0].ID != "custom-my-ty" || !profiles[0].Custom {
		t.Fatalf("Python profiles = %#v", profiles)
	}
	program, arguments := languageServerProgram(h.app.settings.LanguageServers, profiles[0])
	if program != "ty" || !reflect.DeepEqual(arguments, []string{"server"}) {
		t.Fatalf("custom ty launch = %q %q", program, arguments)
	}

	h.app.languageServerCatalogSelected = ""
	h.tester.Frame()
	results, found := h.tester.Find("Language server results")
	if !found {
		t.Fatal("language server results are missing")
	}
	h.tester.Scroll(results.X+results.W/2, results.Y+results.H/2, 0, 1800)
	h.tester.Frame()
	if !h.tester.HasText("My ty") || !h.tester.HasText("CUSTOM — User defined") || !h.tester.HasText("Open download source: My ty") {
		t.Fatalf("custom ty card or source is missing: %q", h.texts())
	}
	if err := h.tester.Click("Disable My ty"); err != nil {
		t.Fatalf("disable custom ty from its card: %v; texts=%q", err, h.texts())
	}
	server = h.app.settings.LanguageServers["custom-my-ty"]
	if !server.Disabled || server.Custom == nil {
		t.Fatalf("disabling discarded the custom profile: %#v", server)
	}
	if got := configuredLanguageServerProfiles(h.app.settings.LanguageServers, "src/main.py"); len(got) != 0 {
		t.Fatalf("disabled custom ty still matches Python: %#v", got)
	}
	h.tester.Frame()
	if err := h.tester.Click("Enable My ty"); err != nil {
		t.Fatal(err)
	}
	if h.app.settings.LanguageServers["custom-my-ty"].Disabled {
		t.Fatal("card switch did not re-enable custom ty")
	}
}

func TestCustomLanguageServerAppearsInEditorStatus(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"custom-ty": {
			Path: "ty", Arguments: []string{"server"},
			Custom: &customLanguageServerDefinition{
				Name: "ty", Extensions: map[string]string{".py": "python"},
			},
		},
	}
	h.app.additionalLanguageServers = map[string]*managedLanguageServer{
		"custom-ty": {connection: newFakeLanguageServerConnection()},
	}
	statuses := h.app.codeLanguageServerStatuses()
	if len(statuses) != 1 || statuses[0].name != "ty" || !statuses[0].running || statuses[0].failed {
		t.Fatalf("custom language-server status = %#v", statuses)
	}
}

func TestLanguageServerCardSwitchPrecedesDownload(t *testing.T) {
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(program string) (string, error) { return "/tools/" + program, nil }
	h := menuTestApp(t)
	h.app.settingsPage = settingsPageEditor
	h.app.openSettings()
	h.tester.Frame()
	h.tester.Scroll(700, 480, 0, 1800)
	h.tester.Frame()
	h.app.languageServerCatalogQuery = "TypeScript"
	h.tester.Frame()
	toggle, toggleOK := h.tester.Find("Enable TypeScript Language Server")
	download, downloadOK := h.tester.Find("Download TypeScript Language Server")
	if !toggleOK || !downloadOK || toggle.X+toggle.W > download.X {
		t.Fatalf("switch/download order: toggle=%+v found=%v download=%+v found=%v", toggle, toggleOK, download, downloadOK)
	}
	if err := h.tester.Click("Enable TypeScript Language Server"); err != nil {
		t.Fatal(err)
	}
	if h.app.settings.LanguageServers["typescript"].Disabled {
		t.Fatal("card switch did not enable TypeScript")
	}
	h.tester.Frame()
	if err := h.tester.Click("Disable TypeScript Language Server"); err != nil {
		t.Fatal(err)
	}
	if _, configured := h.app.settings.LanguageServers["typescript"]; configured {
		t.Fatal("disabling a built-in left an enabled configuration")
	}
}

func TestLanguageServerCatalogSearchesNamesLanguagesAndCommands(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"python", []string{"ty", "BasedPyright", "Ruff"}},
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

func TestPythonLanguageServerInstallersUseManagedEnvironment(t *testing.T) {
	_, err := findFirstExecutable("python3", "python")
	if err != nil {
		t.Skip("Python is unavailable")
	}
	original := runLanguageServerInstallCommand
	t.Cleanup(func() { runLanguageServerInstallCommand = original })
	dataDir := t.TempDir()
	venvBin := filepath.Join(dataDir, "tools", "python", "bin")
	if runtime.GOOS == "windows" {
		venvBin = filepath.Join(dataDir, "tools", "python", "Scripts")
	}
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	venvPython := filepath.Join(venvBin, executableName("python"))
	if err := os.WriteFile(venvPython, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ty", "ruff"} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			runLanguageServerInstallCommand = func(_ context.Context, program string, arguments, _ []string) ([]byte, error) {
				calls++
				if program != venvPython || !reflect.DeepEqual(arguments, []string{"-m", "pip", "install", "--upgrade", id}) {
					t.Errorf("installer = %q %q, want managed Python installing %s", program, arguments, id)
				}
				return nil, os.WriteFile(filepath.Join(venvBin, executableName(id)), nil, 0o755)
			}
			profile, _ := languageServerProfileByID(id)
			got, err := installLanguageServerTool(profile, dataDir)
			want := languageServerInstallation{
				Path: filepath.Join(venvBin, executableName(id)), Arguments: []string{"server"}, ProbeArguments: []string{"--version"},
			}
			if err != nil || !reflect.DeepEqual(got, want) || calls != 1 {
				t.Fatalf("managed %s = %#v, %v (%d commands); want %#v", id, got, err, calls, want)
			}
		})
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
		t.Skip("set TANGIT_TEST_REAL_LSP_INSTALL=1 to download and exercise TypeScript 7's native LSP")
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
	if len(installation.Arguments) < 3 || !strings.HasSuffix(filepath.ToSlash(installation.Arguments[0]), "/typescript/bin/tsc") ||
		!reflect.DeepEqual(installation.Arguments[1:], []string{"--lsp", "--stdio"}) {
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

func TestTypeScriptInstallerPinsNativeVersionSevenAndChecksItsExecutable(t *testing.T) {
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
		t.Run(fmt.Sprint("native-lsp=", withServer), func(t *testing.T) {
			dataDir := t.TempDir()
			prefix := filepath.Join(dataDir, "tools", "node")
			runLanguageServerInstallCommand = func(_ context.Context, _ string, arguments, _ []string) ([]byte, error) {
				want := []string{"install", "--no-audit", "--no-fund", "--prefix", prefix, "typescript@7"}
				if !reflect.DeepEqual(arguments, want) {
					t.Errorf("npm arguments = %q, want %q", arguments, want)
				}
				serverDir := filepath.Join(prefix, "node_modules", "typescript")
				if err := os.MkdirAll(serverDir, 0o755); err != nil {
					return nil, err
				}
				if err := os.WriteFile(filepath.Join(serverDir, "package.json"), []byte(`{"version":"7.0.2","bin":{"tsc":"bin/tsc"}}`), 0o644); err != nil {
					return nil, err
				}
				if withServer {
					tsDir := filepath.Join(serverDir, "bin")
					if err := os.MkdirAll(tsDir, 0o755); err != nil {
						return nil, err
					}
					if err := os.WriteFile(filepath.Join(tsDir, "tsc"), nil, 0o644); err != nil {
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
			if !withServer && err == nil {
				t.Fatalf("missing native TypeScript executable error = %v", err)
			}
		})
	}
}
