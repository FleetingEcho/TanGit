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

func TestGoLanguageServerSettingsPersistAndTestWithoutAShell(t *testing.T) {
	original := runLanguageServerVersion
	t.Cleanup(func() { runLanguageServerVersion = original })
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
	if !h.tester.HasText("Go language server") || !h.tester.HasText("Enable gopls") {
		t.Fatalf("Editor settings have no language-server profile: %q", h.texts())
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

	if err := h.tester.Click("Enable gopls"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if server := h.app.goLanguageServerSettings(); !server.Disabled {
		t.Fatalf("the enable switch did not persist disabled state: %#v", server)
	}
	if err := h.tester.Click("Enable gopls"); err != nil {
		t.Fatal(err)
	}

	if err := h.tester.Click("gopls executable path"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(ui.Cmd, ui.KeyA)
	h.tester.Type("C:/tools/gopls.exe")
	if err := h.tester.Click("gopls arguments"); err != nil {
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
	if err := h.tester.Click("Test server"); err != nil {
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
