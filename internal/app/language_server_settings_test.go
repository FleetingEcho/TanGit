package app

import (
	"errors"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestLanguageServerSettingsNormalizePathsAndArguments(t *testing.T) {
	got := normalizeLanguageServerSettings(map[string]languageServerSettings{
		" Go ": {Path: "  /tools/gopls  ", Arguments: []string{" -remote=auto ", "", "  -debug"}},
		"":     {Disabled: true},
	})
	want := map[string]languageServerSettings{
		"go": {Path: "/tools/gopls", Arguments: []string{"-remote=auto", "-debug"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized language servers = %#v, want %#v", got, want)
	}
	if got := normalizeLanguageServerSettings(map[string]languageServerSettings{
		"go": {},
	}); got != nil {
		t.Fatalf("a default profile should not occupy the settings file: %#v", got)
	}
	if got := normalizeLanguageServerSettings(map[string]languageServerSettings{
		"typescript": {},
	}); !reflect.DeepEqual(got, map[string]languageServerSettings{"typescript": {}}) {
		t.Fatalf("an enabled opt-in profile was discarded: %#v", got)
	}
	formatterOnly := map[string]languageServerSettings{
		"go":   {Formatter: "  gofmt  "},
		"ruff": {Disabled: true, Formatter: " ruff format --stdin-filename {{file}} "},
	}
	wantFormatters := map[string]languageServerSettings{
		"go":   {Formatter: "gofmt"},
		"ruff": {Disabled: true, Formatter: "ruff format --stdin-filename {{file}}"},
	}
	if got := normalizeLanguageServerSettings(formatterOnly); !reflect.DeepEqual(got, wantFormatters) {
		t.Fatalf("formatter-only settings did not survive normalization: %#v, want %#v", got, wantFormatters)
	}
}

func scrollLanguageServerSettingIntoView(t *testing.T, h *harness, label string) {
	t.Helper()
	for range 12 {
		bounds, found := h.tester.Find(label)
		if !found {
			t.Fatalf("language-server setting %q is not built: %q", label, h.texts())
		}
		if bounds.W > 0 && bounds.H > 0 && bounds.Y >= 0 && bounds.Y+bounds.H <= 800 {
			return
		}
		delta := float32(360)
		if bounds.Y < 0 {
			delta = -360
		}
		h.tester.Scroll(700, 480, 0, delta)
		h.tester.Frame()
	}
	t.Fatalf("could not scroll language-server setting %q into view", label)
}

func TestLanguageServerFormatterSettingsPersistEnabledAndDisabled(t *testing.T) {
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(program string) (string, error) { return program, nil }
	for _, test := range []struct {
		name             string
		id               string
		query            string
		profileName      string
		initiallyEnabled bool
		command          string
	}{
		{name: "Go", id: "go", query: "gopls", profileName: "Go (gopls)", initiallyEnabled: true, command: "gofmt"},
		{name: "Ruff", id: "ruff", query: "Ruff", profileName: "Ruff", command: "ruff format --stdin-filename {{file}}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := menuTestApp(t)
			h.app.pathUserData = t.TempDir()
			h.app.settingsPage = settingsPageEditor
			h.app.openSettings()
			h.tester.Frame()
			h.tester.Scroll(700, 480, 0, 1800)
			h.tester.Frame()
			if err := h.tester.Click("Search language servers"); err != nil {
				t.Fatal(err)
			}
			h.tester.Type(test.query)
			h.tester.Frame()
			if err := h.tester.Click("Configure " + test.profileName); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if test.initiallyEnabled {
				scrollLanguageServerSettingIntoView(t, h, "Enable "+test.profileName)
				if err := h.tester.Click("Enable " + test.profileName); err != nil {
					t.Fatal(err)
				}
				h.tester.Frame()
			}
			scrollLanguageServerSettingIntoView(t, h, test.profileName+" formatter command")
			if err := h.tester.Click(test.profileName + " formatter command"); err != nil {
				t.Fatal(err)
			}
			h.tester.Type(test.command)
			h.tester.Frame()
			server := h.app.settings.LanguageServers[test.id]
			if !server.Disabled || server.Formatter != test.command {
				t.Fatalf("disabled formatter settings = %#v", server)
			}
			if reloaded := loadAppSettings(h.app.pathUserData).LanguageServers[test.id]; !reloaded.Disabled || reloaded.Formatter != test.command {
				t.Fatalf("persisted disabled formatter settings = %#v", reloaded)
			}
			h.app.loadGoLanguageServerDraft()
			if h.app.goFormatterCommand != "" && test.id != "go" {
				t.Fatal("another language's formatter leaked into the Go draft")
			}
			if test.id == "go" && h.app.goFormatterCommand != test.command {
				t.Fatalf("reloaded Go formatter = %q", h.app.goFormatterCommand)
			}
			if test.id != "go" {
				draft := h.app.languageServerDrafts[test.id]
				if draft == nil || draft.Enabled || draft.Formatter != test.command {
					t.Fatalf("reloaded disabled formatter draft = %#v", draft)
				}
			}
			scrollLanguageServerSettingIntoView(t, h, "Enable "+test.profileName)
			if err := h.tester.Click("Enable " + test.profileName); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			server = h.app.settings.LanguageServers[test.id]
			if server.Disabled || server.Formatter != test.command {
				t.Fatalf("enabled formatter settings = %#v", server)
			}
			if reloaded := loadAppSettings(h.app.pathUserData).LanguageServers[test.id]; reloaded.Disabled || reloaded.Formatter != test.command {
				t.Fatalf("persisted enabled formatter settings = %#v", reloaded)
			}
		})
	}
}

func TestAdditionalLanguageServerDraftsLoadAsOptIn(t *testing.T) {
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{"typescript": {Path: "C:/tools/tsls.exe"}}
	h.app.loadGoLanguageServerDraft()
	if draft := h.app.languageServerDrafts["typescript"]; draft == nil || !draft.Enabled || draft.Path != "C:/tools/tsls.exe" {
		t.Fatalf("TypeScript draft = %#v", draft)
	}
	if draft := h.app.languageServerDrafts["tailwind"]; draft == nil || draft.Enabled {
		t.Fatalf("Tailwind should be opt-in: %#v", draft)
	}
}

func TestCustomLanguageServerEditPreservesFormatter(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	const formatter = "example-format --file {{file}}"
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"custom-example": {
			Path: "example-ls", Formatter: formatter,
			Custom: &customLanguageServerDefinition{Name: "Example", Extensions: map[string]string{".example": "example"}},
		},
	}
	h.app.customLanguageServerForm = customLanguageServerForm{
		Open: true, ID: "custom-example", Name: "Example edited", Path: "example-ls", Arguments: "server",
		Extensions: ".example=example", Enabled: true, Description: "Updated definition",
	}
	h.app.settingsPage = settingsPageEditor
	h.app.openSettings()
	h.tester.Frame()
	scrollLanguageServerSettingIntoView(t, h, "Save custom language server")
	if err := h.tester.Click("Save custom language server"); err != nil {
		t.Fatal(err)
	}
	server := h.app.settings.LanguageServers["custom-example"]
	if server.Formatter != formatter || server.Custom == nil || server.Custom.Name != "Example edited" {
		t.Fatalf("custom edit lost its formatter or changes: %#v", server)
	}
	if reloaded := loadAppSettings(h.app.pathUserData).LanguageServers["custom-example"]; reloaded.Formatter != formatter {
		t.Fatalf("persisted custom formatter = %#v", reloaded)
	}
}

func TestGoLanguageServerSettingsPersistAndTestWithoutAShell(t *testing.T) {
	original := runLanguageServerVersion
	t.Cleanup(func() { runLanguageServerVersion = original })
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(program string) (string, error) { return "/tools/" + program, nil }
	type invocation struct {
		program   string
		arguments []string
	}
	called := make(chan invocation, 1)
	runLanguageServerVersion = func(program string, arguments []string) (string, error) {
		called <- invocation{program: program, arguments: append([]string(nil), arguments...)}
		return "golang.org/x/tools/gopls v9.9.9", nil
	}

	h := menuTestApp(t)
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Editor settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.tester.HasText("gopls executable path") || h.tester.HasText("Test server") || !h.tester.HasText("Go (gopls)") {
		t.Fatalf("Editor settings should have only the catalog's Go profile: %q", h.texts())
	}
	for _, label := range []string{
		"TypeScript Language Server", "VS Code HTML Language Server", "VS Code CSS Language Server",
		"VS Code JSON Language Server", "Tailwind CSS Language Server", "VS Code ESLint Language Server",
		"Rust Analyzer", "Go (gopls)", "BasedPyright", "Ruff", "YAML Language Server",
		"Vue Language Server", "Svelte Language Server", "Astro Language Server",
		"Bash Language Server", "Dockerfile Language Server", "Marksman", "Oxc / Oxlint",
	} {
		if !h.tester.HasText(label) {
			t.Fatalf("Editor settings have no %s profile: %q", label, h.texts())
		}
	}

	h.tester.Scroll(700, 480, 0, 1800)
	h.tester.Frame()
	if err := h.tester.Click("Search language servers"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("gopls")
	h.tester.Frame()
	if err := h.tester.Click("Configure Go (gopls)"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Enable Go (gopls)"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if server := h.app.goLanguageServerSettings(); !server.Disabled {
		t.Fatalf("the enable switch did not persist disabled state: %#v", server)
	}
	if err := h.tester.Click("Enable Go (gopls)"); err != nil {
		t.Fatal(err)
	}

	h.tester.Scroll(700, 480, 0, 300)
	h.tester.Frame()
	if err := h.tester.Click("Go (gopls) executable path"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("C:/tools/gopls.exe")
	if err := h.tester.Click("Go (gopls) arguments"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("-remote=auto\n-debug")
	h.tester.Frame()

	server := h.app.goLanguageServerSettings()
	if server.Disabled || server.Path != "C:/tools/gopls.exe" ||
		!reflect.DeepEqual(server.Arguments, []string{"-remote=auto", "-debug"}) {
		t.Fatalf("stored gopls profile = %#v", server)
	}
	h.tester.Scroll(700, 480, 0, 360)
	h.tester.Frame()
	if err := h.tester.Click("Test Go (gopls)"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the language-server test", func() bool {
		return !h.app.languageServerTestBusy && h.app.languageServerTestResult != ""
	})
	h.tester.Frame()
	invoked := <-called
	if invoked.program != server.Path || !reflect.DeepEqual(invoked.arguments, server.Arguments) {
		t.Fatalf("version command = %#v, want program %q arguments %q", invoked, server.Path, server.Arguments)
	}
	if !h.app.languageServerTestOK || !h.tester.HasText("golang.org/x/tools/gopls v9.9.9") {
		t.Fatalf("successful test was not shown: ok=%v texts=%q", h.app.languageServerTestOK, h.texts())
	}
}

func TestLanguageServerCatalogConfiguresGoInline(t *testing.T) {
	h := menuTestApp(t)
	h.app.settingsPage = settingsPageEditor
	h.app.openSettings()
	h.tester.Frame()
	h.tester.Scroll(700, 480, 0, 1800)
	h.tester.Frame()
	if err := h.tester.Click("Search language servers"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("gopls")
	h.tester.Frame()
	if err := h.tester.Click("Configure Go (gopls)"); err != nil {
		t.Fatalf("Go cannot be configured from its catalog row: %v", err)
	}
	h.tester.Frame()
	if !h.tester.HasText("Enable Go (gopls)") || h.tester.HasText("Use the Go language server controls above to configure gopls.") {
		t.Fatalf("Go catalog configuration is missing: %q", h.texts())
	}
	h.tester.Scroll(700, 480, 0, 300)
	h.tester.Frame()
	if err := h.tester.Click("Go (gopls) executable path"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("/zed/gopls")
	h.tester.Frame()
	if got := h.app.goLanguageServerSettings().Path; got != "/zed/gopls" {
		t.Fatalf("catalog gopls path = %q, want /zed/gopls", got)
	}
	if err := h.tester.Click("Enable Go (gopls)"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.goLanguageServerSettings().Disabled {
		t.Fatal("the catalog Enable control did not update the gopls profile")
	}
}

func TestGoLanguageServerTestTriesConfiguredProgramsInOrder(t *testing.T) {
	original := runLanguageServerVersion
	t.Cleanup(func() { runLanguageServerVersion = original })
	var programs []string
	runLanguageServerVersion = func(program string, _ []string) (string, error) {
		programs = append(programs, program)
		if program == "/missing/gopls" {
			return "", errors.New("not installed")
		}
		return "gopls from Zed", nil
	}
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"go": {Path: "/missing/gopls, /zed/gopls"},
	}
	h.app.testGoLanguageServer()
	h.waitFor(t, "the fallback language-server test", func() bool {
		return !h.app.languageServerTestBusy && h.app.languageServerTestResult != ""
	})
	if !reflect.DeepEqual(programs, []string{"/missing/gopls", "/zed/gopls"}) ||
		!h.app.languageServerTestOK || h.app.languageServerTestResult != "gopls from Zed" {
		t.Fatalf("fallback test programs=%v ok=%v result=%q", programs, h.app.languageServerTestOK, h.app.languageServerTestResult)
	}
}

func TestGoLanguageServerTestReportsFailure(t *testing.T) {
	original := runLanguageServerVersion
	t.Cleanup(func() { runLanguageServerVersion = original })
	runLanguageServerVersion = func(string, []string) (string, error) {
		return "", errors.New("executable not found")
	}
	h := menuTestApp(t)
	h.app.testGoLanguageServer()
	h.waitFor(t, "the failed language-server test", func() bool {
		return !h.app.languageServerTestBusy && h.app.languageServerTestResult != ""
	})
	if h.app.languageServerTestOK || h.app.languageServerTestResult != "Could not start gopls: executable not found" {
		t.Fatalf("failed test state = ok %v, result %q", h.app.languageServerTestOK, h.app.languageServerTestResult)
	}
}
