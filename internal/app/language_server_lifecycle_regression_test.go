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
)

func TestManagedAvailabilityRejectsRemovedExecutableAndReferencedFiles(t *testing.T) {
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	// The managed-installation checks below, rather than the host PATH fixture,
	// must decide whether each referenced file is usable.
	languageServerLookPath = func(program string) (string, error) { return program, nil }

	for _, removed := range []string{"executable", "launch dependency", "probe dependency"} {
		t.Run(removed, func(t *testing.T) {
			h := menuTestApp(t)
			h.app.pathUserData = t.TempDir()
			server, files := lifecycleRegressionManagedInstallation(t, h.app.pathUserData, "ruff")
			h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": server}
			profile := mustLanguageServerProfile(t, "ruff")
			if !h.app.languageServerExecutableAvailable(profile) {
				t.Fatal("complete managed installation is unavailable")
			}
			path := files[removed]
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if h.app.languageServerExecutableAvailable(profile) {
				t.Fatalf("managed installation remains available after removing its %s %q", removed, path)
			}
		})
	}
}

func TestProtocolProbeSettingsTestInitializesManagedLaunchWithoutVersionProbe(t *testing.T) {
	connection := newFakeLanguageServerConnection()
	launches := make(chan languageServerLaunch, 1)
	stubLanguageServerConnection(t, func(launch languageServerLaunch) (languageServerConnection, error) {
		launches <- launch
		return connection, nil
	})

	const node = "/tools/node"
	serverScript := "/tools/lsp/intelephense/intelephense.js"
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"intelephense": {
			Path: node, Arguments: []string{serverScript, "--stdio"},
			ProbeArguments: []string{"-e", "process.stdout.write(require('./package.json').version)"},
			Managed:        &managedLanguageServerSettings{Root: "/tools/lsp/intelephense"},
		},
	}
	h.app.testLanguageServerProfile("intelephense")
	h.waitFor(t, "the managed protocol-probe settings test", func() bool {
		return !h.app.lsp.testBusy && h.app.lsp.testResult != ""
	})
	var launch languageServerLaunch
	select {
	case launch = <-launches:
	default:
		t.Fatal("protocol probe finished without initializing a language-server connection")
	}
	if launch.Program != node || !reflect.DeepEqual(launch.Arguments, []string{serverScript, "--stdio"}) {
		t.Fatalf("protocol probe launch = %q %q, want %q [%s --stdio]", launch.Program, launch.Arguments, node, serverScript)
	}
	if launch.Root != h.app.snapshot.Root || !connection.isClosed() || !h.app.lsp.testOK {
		t.Fatalf("protocol probe root=%q closed=%v ok=%v result=%q", launch.Root, connection.isClosed(), h.app.lsp.testOK, h.app.lsp.testResult)
	}
}

func TestFailedManagedPublicationPreservesPreviousInstallationAndSettings(t *testing.T) {
	originalInstall := performLanguageServerInstall
	originalProbe := probeLanguageServerInstallation
	t.Cleanup(func() {
		performLanguageServerInstall = originalInstall
		probeLanguageServerInstallation = originalProbe
	})
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	previous, files := lifecycleRegressionManagedInstallation(t, h.app.pathUserData, "ruff")
	h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": previous}
	h.app.saveSettings()

	var staging string
	performLanguageServerInstall = func(profile languageServerProfile, dataDir string) (languageServerInstallation, error) {
		return fakeOwnedLanguageServerInstallation(t, profile, dataDir), nil
	}
	probeLanguageServerInstallation = func(installation languageServerInstallation) (string, error) {
		staging = installation.OwnedRoot
		// Model a staging publication failure after a successful probe. No
		// production rename seam is needed to exercise the preservation contract.
		if err := os.RemoveAll(installation.OwnedRoot); err != nil {
			return "", err
		}
		return "ruff 2.0.0", nil
	}
	h.app.installLanguageServer("ruff")
	h.waitFor(t, "the failed managed replacement", func() bool { return !h.app.lsp.installing["ruff"] })

	if got := h.app.settings.LanguageServers["ruff"]; !reflect.DeepEqual(got, previous) {
		t.Fatalf("failed publication replaced managed settings: got %#v, want %#v", got, previous)
	}
	if got := loadAppSettings(h.app.pathUserData).LanguageServers["ruff"]; !reflect.DeepEqual(got, previous) {
		t.Fatalf("failed publication replaced persisted settings: got %#v, want %#v", got, previous)
	}
	for name, path := range files {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "original "+name {
			t.Errorf("previous %s was not preserved: data=%q err=%v", name, data, err)
		}
	}
	if _, err := readManagedLanguageServerManifest(previous.Managed.Root, "ruff"); err != nil {
		t.Fatalf("previous ownership manifest was not preserved: %v", err)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed publication left staging directory %q: %v", staging, err)
	}
	if !h.tester.HasText("Could not install Ruff") {
		t.Fatalf("failed publication has no error notification: %q", h.texts())
	}
}

func TestPythonManagedInstallationWorksAfterStagingPublication(t *testing.T) {
	_, err := exec.LookPath("python3")
	if err != nil {
		_, err = exec.LookPath("python")
	}
	if err != nil {
		t.Skip("Python is unavailable")
	}
	originalInstall := runLanguageServerInstallCommand
	t.Cleanup(func() { runLanguageServerInstallCommand = originalInstall })
	profile := mustLanguageServerProfile(t, "ruff")
	runLanguageServerInstallCommand = func(ctx context.Context, program string, arguments, environment []string) ([]byte, error) {
		run := func(program string, arguments ...string) ([]byte, error) {
			command := exec.CommandContext(ctx, program, arguments...)
			command.Env = environment
			return command.CombinedOutput()
		}
		if len(arguments) == 3 && arguments[0] == "-m" && arguments[1] == "venv" {
			return run(program, "-m", "venv", "--without-pip", arguments[2])
		}
		if len(arguments) != 5 || arguments[0] != "-m" || arguments[1] != "pip" || arguments[4] != "ruff" {
			return nil, fmt.Errorf("unexpected installer command: %s %q", program, arguments)
		}
		output, err := run(program, "-c", `import sysconfig; print(sysconfig.get_paths()["purelib"])`)
		if err != nil {
			return output, err
		}
		files := map[string]string{
			filepath.Join(strings.TrimSpace(string(output)), "ruff-1.0.dist-info", "METADATA"):         "Metadata-Version: 2.1\nName: ruff\nVersion: 1.0\n",
			filepath.Join(strings.TrimSpace(string(output)), "ruff-1.0.dist-info", "entry_points.txt"): "[console_scripts]\nruff = fake_ruff:main\n",
			filepath.Join(strings.TrimSpace(string(output)), "fake_ruff.py"):                           "def main():\n    print('ruff relocation-ok')\n    return 0\n",
			// Reproduce pip's staging-specific shebang. TanGit must not launch it.
			filepath.Join(filepath.Dir(program), executableName(profile.Program)): "#!" + program + "\n",
		}
		for path, contents := range files {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}

	dataDir := t.TempDir()
	staging, root, err := newManagedLanguageServerStaging(dataDir, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	installation, err := installPythonLanguageServer(context.Background(), staging, profile, "ruff")
	if err != nil {
		t.Fatal(err)
	}
	installation.OwnedRoot = staging
	installation.ManagedRoot = root
	installation.ProbeRequired = true
	published, version, err := prepareLanguageServerInstallation(profile, dataDir, installation)
	if err != nil {
		t.Fatal(err)
	}
	if published.ManagedRoot != root || published.OwnedRoot != "" || version != "ruff relocation-ok" {
		t.Fatalf("published Python installation = %#v, version %q", published, version)
	}
	if !pathWithin(root, published.Path) || len(published.ProbeArguments) == 0 || !pathWithin(root, published.ProbeArguments[0]) {
		t.Fatalf("Python launch was not rebased to the managed root: %#v", published)
	}
	if strings.Contains(published.Path, staging) || strings.Contains(strings.Join(published.ProbeArguments, " "), staging) {
		t.Fatalf("published Python launch retains staging path %q: %#v", staging, published)
	}
	output, err := probeLanguageServerInstallation(published)
	if err != nil || strings.TrimSpace(output) != "ruff relocation-ok" {
		t.Fatalf("published Python launch = %q, %v", output, err)
	}
}

func TestManagedUninstallRejectsConfigurationChangesWhileBusy(t *testing.T) {
	originalRemove := removeManagedLanguageServerDirectory
	t.Cleanup(func() { removeManagedLanguageServerDirectory = originalRemove })
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	server, _ := lifecycleRegressionManagedInstallation(t, h.app.pathUserData, "ruff")
	h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": server}
	h.app.loadGoLanguageServerDraft()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	removeManagedLanguageServerDirectory = func(root string) error {
		started <- struct{}{}
		<-release
		return os.RemoveAll(root)
	}
	h.app.beginManagedLanguageServerRemoval("ruff", server.Managed.Root)
	<-started

	draft := h.app.lsp.drafts["ruff"]
	draft.Path = "/external/ruff"
	draft.Arguments = "server"
	draft.Enabled = true
	h.app.storeAdditionalLanguageServerDraft(nil, "ruff")
	h.app.toggleLanguageServerProfile(nil, mustLanguageServerProfile(t, "ruff"), true)
	h.app.resetSettingsToDefaults()
	if got := h.app.settings.LanguageServers["ruff"]; !reflect.DeepEqual(got, server) {
		t.Fatalf("configuration changed during managed removal: got %#v, want %#v", got, server)
	}
	close(release)
	h.waitFor(t, "managed removal after rejected edits", func() bool { return !h.app.lsp.uninstalling["ruff"] })
	got := h.app.settings.LanguageServers["ruff"]
	if got.Path != "" || len(got.Arguments) != 0 || len(got.ProbeArguments) != 0 || got.Managed != nil || !got.Disabled {
		t.Fatalf("completed removal did not restore PATH fallback: %#v", got)
	}
}

func TestFailedManagedUninstallPreservesFilesSettingsAndReportsFailure(t *testing.T) {
	originalRemove := removeManagedLanguageServerDirectory
	t.Cleanup(func() { removeManagedLanguageServerDirectory = originalRemove })
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	server, files := lifecycleRegressionManagedInstallation(t, h.app.pathUserData, "ruff")
	h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": server}
	h.app.saveSettings()
	removeManagedLanguageServerDirectory = func(root string) error {
		if root != server.Managed.Root {
			t.Errorf("removal root = %q, want %q", root, server.Managed.Root)
		}
		return errors.New("disk refused removal")
	}

	h.app.beginManagedLanguageServerRemoval("ruff", server.Managed.Root)
	h.waitFor(t, "the failed managed removal", func() bool { return !h.app.lsp.uninstalling["ruff"] })
	if got := h.app.settings.LanguageServers["ruff"]; !reflect.DeepEqual(got, server) {
		t.Fatalf("failed uninstall changed settings: got %#v, want %#v", got, server)
	}
	if got := loadAppSettings(h.app.pathUserData).LanguageServers["ruff"]; !reflect.DeepEqual(got, server) {
		t.Fatalf("failed uninstall changed persisted settings: got %#v, want %#v", got, server)
	}
	for name, path := range files {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "original "+name {
			t.Errorf("failed uninstall changed %s: data=%q err=%v", name, data, err)
		}
	}
	if _, err := readManagedLanguageServerManifest(server.Managed.Root, "ruff"); err != nil {
		t.Fatalf("failed uninstall changed ownership manifest: %v", err)
	}
	if !h.tester.HasText("Could not remove Ruff") || !h.tester.HasText("disk refused removal") {
		t.Fatalf("failed uninstall notification is missing or incomplete: %q", h.texts())
	}
}

func TestDuplicateManagedInstallStartsOnlyOneWorker(t *testing.T) {
	originalInstall := performLanguageServerInstall
	t.Cleanup(func() { performLanguageServerInstall = originalInstall })
	started := make(chan struct{}, 2)
	firstStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	performLanguageServerInstall = func(languageServerProfile, string) (languageServerInstallation, error) {
		started <- struct{}{}
		select {
		case firstStarted <- struct{}{}:
		default:
		}
		<-release
		return languageServerInstallation{Path: "ruff", Arguments: []string{"server"}}, nil
	}
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.installLanguageServer("ruff")
	<-firstStarted
	h.app.installLanguageServer("ruff")
	close(release)
	h.waitFor(t, "the single managed install worker", func() bool {
		return !h.app.lsp.installing["ruff"]
	})
	if got := len(started); got != 1 {
		t.Fatalf("managed install starts = %d, want 1", got)
	}
}

func TestDuplicateManagedRemovalStartsOnlyOneWorker(t *testing.T) {
	originalRemove := removeManagedLanguageServerDirectory
	t.Cleanup(func() { removeManagedLanguageServerDirectory = originalRemove })
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	server, _ := lifecycleRegressionManagedInstallation(t, h.app.pathUserData, "ruff")
	h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": server}
	started := make(chan struct{}, 2)
	firstStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	removeManagedLanguageServerDirectory = func(root string) error {
		started <- struct{}{}
		select {
		case firstStarted <- struct{}{}:
		default:
		}
		<-release
		return os.RemoveAll(root)
	}
	h.app.beginManagedLanguageServerRemoval("ruff", server.Managed.Root)
	<-firstStarted
	h.app.beginManagedLanguageServerRemoval("ruff", server.Managed.Root)
	close(release)
	h.waitFor(t, "the single managed removal worker", func() bool {
		return !h.app.lsp.uninstalling["ruff"]
	})
	if got := len(started); got != 1 {
		t.Fatalf("managed removal starts = %d, want 1", got)
	}
	if _, err := os.Stat(server.Managed.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed root remains after the single removal: %v", err)
	}
}

func TestUnexpectedLanguageServerExitRestartsOnceAcrossRepeatedFrames(t *testing.T) {
	for _, test := range []struct {
		name string
		id   string
		path string
		text string
		err  error
	}{
		{name: "Go crash", id: "go", path: "main.go", text: "package main\n", err: errors.New("gopls crashed")},
		{name: "managed clean exit", id: "ruff", path: "main.py", text: "answer = 42\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			connections := make(chan *fakeLanguageServerConnection, 4)
			stubLanguageServerConnection(t, func(languageServerLaunch) (languageServerConnection, error) {
				connection := newFakeLanguageServerConnection()
				connections <- connection
				return connection, nil
			})
			stubCodeWorkspace(t, map[string]string{test.path: test.text})
			h := menuTestApp(t)
			h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Disabled: test.id != "go"}}
			h.app.settings.LanguageServers[test.id] = languageServerSettings{}
			openCodePageForTest(t, h, 1)
			h.app.openCodeFile(test.path, 0)
			waitForCodeDocument(t, h, test.path)
			h.waitFor(t, "the initial language-server connection", func() bool { return len(connections) == 1 })
			first := <-connections
			if test.err != nil {
				first.done <- test.err
			}
			close(first.done)
			h.waitFor(t, "the unexpected language-server exit", func() bool {
				if test.id == "go" {
					return h.app.lsp.goRestartUsed
				}
				server := h.app.lsp.additional[test.id]
				return server != nil && server.restartUsed
			})
			// The watcher must arrange one replacement. The connection itself is
			// created on a goroutine, so wait for that event rather than assuming
			// a fixed number of frames gives the goroutine a chance to run.
			h.waitFor(t, "the replacement connection attempt", func() bool { return len(connections) == 1 })
			if got := len(connections); got != 1 {
				var detail string
				if test.id == "go" {
					detail = h.app.lsp.goError
				} else if server := h.app.lsp.additional[test.id]; server != nil {
					detail = server.error
				}
				t.Fatalf("replacement connections after crash/exit = %d, want exactly 1 (error=%q)", got, detail)
			}
			second := <-connections
			h.waitFor(t, "the replacement language-server connection", func() bool {
				if test.id == "go" {
					return h.app.lsp.goServer == second
				}
				server := h.app.lsp.additional[test.id]
				return server != nil && server.connection == second
			})
			for range 4 {
				h.tester.Frame()
			}
			if got := len(connections); got != 0 {
				t.Fatalf("recovery was not debounced: %d extra connections", got)
			}
			if test.err != nil {
				second.done <- test.err
			}
			close(second.done)
			h.waitFor(t, "the second unexpected language-server exit", func() bool {
				if test.id == "go" {
					return h.app.lsp.goServer == nil && h.app.lsp.goError != ""
				}
				server := h.app.lsp.additional[test.id]
				return server != nil && server.connection == nil && server.error != ""
			})
			for range 4 {
				h.tester.Frame()
			}
			if got := len(connections); got != 0 {
				t.Fatalf("one-shot recovery started %d additional replacements", got)
			}
		})
	}
}

func TestNeoCMakePullDiagnosticsRejectStaleResponses(t *testing.T) {
	type pull struct {
		params any
		answer func(json.RawMessage, error)
	}
	pulls := make(chan pull, 4)
	connection := newFakeLanguageServerConnection()
	connection.request = func(method string, params any, answer func(json.RawMessage, error)) {
		if method == "textDocument/diagnostic" {
			pulls <- pull{params: params, answer: answer}
		}
	}
	stubLanguageServerConnection(t, func(languageServerLaunch) (languageServerConnection, error) {
		return connection, nil
	})
	stubCodeWorkspace(t, map[string]string{"CMakeLists.txt": "cmake_minimum_required(VERSION 3.20)\n"})
	h := menuTestApp(t)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"go":       {Disabled: true},
		"neocmake": {},
	}
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("CMakeLists.txt", 0)
	document := waitForCodeDocument(t, h, "CMakeLists.txt")
	h.waitFor(t, "the initial NeoCMake diagnostic pull", func() bool { return len(pulls) == 1 })
	stale := <-pulls
	document.state.Insert("PROJECT(TanGit)\n")
	h.tester.Frame()
	h.waitFor(t, "the changed NeoCMake diagnostic pull", func() bool { return len(pulls) == 1 })
	current := <-pulls
	stale.answer(json.RawMessage(`{"kind":"full","items":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":5}},"severity":1,"message":"stale diagnostic"}]}`), nil)
	h.tester.Frame()
	if got := document.state.Diagnostics(); len(got) != 0 {
		t.Fatalf("stale initial diagnostic pull was applied after an edit: %#v", got)
	}
	position, ok := current.params.(languageServerDiagnosticParams)
	if !ok || position.TextDocument.URI != "file:///work/project/CMakeLists.txt" {
		t.Fatalf("diagnostic pull params = %#v", current.params)
	}
	current.answer(json.RawMessage(`{"kind":"full","items":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":5}},"severity":1,"message":"command name should be lowercased"}]}`), nil)
	h.waitFor(t, "NeoCMake pull diagnostics", func() bool {
		diagnostics := document.state.Diagnostics()
		return len(diagnostics) == 1 && diagnostics[0].Message == "command name should be lowercased"
	})

	document.state.Insert("project(TanGit)\n")
	h.tester.Frame()
	h.waitFor(t, "the next NeoCMake diagnostic pull", func() bool { return len(pulls) == 1 })
	latest := <-pulls
	current.answer(json.RawMessage(`{"kind":"full","items":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":5}},"severity":1,"message":"old response"}]}`), nil)
	h.tester.Frame()
	if got := document.state.Diagnostics(); len(got) != 1 || got[0].Message != "command name should be lowercased" {
		t.Fatalf("stale pull diagnostics replaced the current version: %#v", got)
	}
	latest.answer(json.RawMessage(`{"kind":"full","items":[]}`), nil)
	h.waitFor(t, "cleared NeoCMake pull diagnostics", func() bool { return len(document.state.Diagnostics()) == 0 })
}

func lifecycleRegressionManagedInstallation(t *testing.T, dataDir, id string) (languageServerSettings, map[string]string) {
	t.Helper()
	root := managedLanguageServerRoot(dataDir, id)
	files := map[string]string{
		"executable":        filepath.Join(root, "bin", executableName(id)),
		"launch dependency": filepath.Join(root, "lib", "launch.py"),
		"probe dependency":  filepath.Join(root, "lib", "probe.py"),
	}
	for name, path := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("original "+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := jsonMarshalManagedManifest(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, managedLanguageServerManifestFile), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	return languageServerSettings{
		Disabled: true, Path: files["executable"],
		Arguments:      []string{files["launch dependency"], "server"},
		ProbeArguments: []string{files["probe dependency"], "--version"},
		Managed:        &managedLanguageServerSettings{Root: root, Version: "ruff 1.0.0"},
	}, files
}
