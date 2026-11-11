package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestFailedExternalLanguageServerOffersManagedInstall(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{"src/main.py": "answer = 42\n"})
	for _, profile := range languageServerProfiles {
		h.app.settings.LanguageServers[profile.ID] = languageServerSettings{Disabled: true}
	}
	openRealCodeDocument(t, h, "src/main.py")
	profile, _ := languageServerProfileByID("ruff")
	h.app.settings.LanguageServers["ruff"] = languageServerSettings{Path: "/external/ruff", Arguments: []string{"server"}}
	h.app.additionalLanguageServers["ruff"] = &managedLanguageServer{profile: profile, error: "initialize failed"}
	h.tester.Frame()

	button, found := h.tester.Find("Language servers")
	if !found {
		t.Fatal("the failed language server has no status button")
	}
	h.tester.Move(button.X+button.W/2, button.Y+button.H/2)
	h.tester.Frame()
	if _, found := h.tester.Find("Install managed Ruff"); !found {
		t.Fatalf("failed external server has no managed install action: %q", h.texts())
	}
}

func TestOwnedInstallReplacesExternalConfigurationOnlyAfterProbe(t *testing.T) {
	originalInstall := performLanguageServerInstall
	originalProbe := probeLanguageServerInstallation
	t.Cleanup(func() {
		performLanguageServerInstall = originalInstall
		probeLanguageServerInstallation = originalProbe
	})
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	external := languageServerSettings{Path: "/external/ruff", Arguments: []string{"server"}, ProbeArguments: []string{"--external-version"}}
	h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": external}

	probeStarted := make(chan struct{})
	probeRelease := make(chan struct{})
	var staging string
	performLanguageServerInstall = func(profile languageServerProfile, dataDir string) (languageServerInstallation, error) {
		return fakeOwnedLanguageServerInstallation(t, profile, dataDir), nil
	}
	probeLanguageServerInstallation = func(installation languageServerInstallation) (string, error) {
		staging = installation.OwnedRoot
		close(probeStarted)
		<-probeRelease
		return "ruff 1.2.3\nextra details", nil
	}
	h.app.installLanguageServer("ruff")
	select {
	case <-probeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("owned installation was not probed")
	}
	if got := h.app.settings.LanguageServers["ruff"]; !reflect.DeepEqual(got, external) {
		t.Fatalf("external configuration changed before probe completed: %#v", got)
	}
	close(probeRelease)
	h.waitFor(t, "managed installation", func() bool { return !h.app.languageServerInstalling["ruff"] })

	root := managedLanguageServerRoot(h.app.pathUserData, "ruff")
	got := h.app.settings.LanguageServers["ruff"]
	if got.Path != filepath.Join(root, "bin", executableName("ruff")) || got.Managed == nil || got.Managed.Root != root ||
		got.Managed.Version != "ruff 1.2.3" || !reflect.DeepEqual(got.Arguments, []string{"server"}) ||
		!reflect.DeepEqual(got.ProbeArguments, []string{"--version"}) {
		t.Fatalf("activated managed settings = %#v, root %q", got, root)
	}
	if pathWithin(staging, got.Path) {
		t.Fatalf("published settings still reference staging directory %q", staging)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging directory remains after publication: %v", err)
	}
	if _, err := readManagedLanguageServerManifest(root, "ruff"); err != nil {
		t.Fatalf("published ownership manifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", executableName("ruff"))); err != nil {
		t.Fatalf("published executable: %v", err)
	}
}

func TestFailedOwnedProbePreservesExternalConfiguration(t *testing.T) {
	originalInstall := performLanguageServerInstall
	originalProbe := probeLanguageServerInstallation
	t.Cleanup(func() {
		performLanguageServerInstall = originalInstall
		probeLanguageServerInstallation = originalProbe
	})
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	external := languageServerSettings{Path: "/external/ruff", Arguments: []string{"server"}, ProbeArguments: []string{"--external-version"}}
	h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": external}
	var staging string
	performLanguageServerInstall = func(profile languageServerProfile, dataDir string) (languageServerInstallation, error) {
		return fakeOwnedLanguageServerInstallation(t, profile, dataDir), nil
	}
	probeLanguageServerInstallation = func(installation languageServerInstallation) (string, error) {
		staging = installation.OwnedRoot
		return "", errors.New("bad executable")
	}
	h.app.installLanguageServer("ruff")
	h.waitFor(t, "failed managed probe", func() bool { return !h.app.languageServerInstalling["ruff"] })
	if got := h.app.settings.LanguageServers["ruff"]; !reflect.DeepEqual(got, external) {
		t.Fatalf("failed probe replaced external configuration: %#v", got)
	}
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed probe left staging files: %v", err)
	}
	if _, err := os.Stat(managedLanguageServerRoot(h.app.pathUserData, "ruff")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed probe published a managed root: %v", err)
	}
	if !h.tester.HasText("Could not install Ruff") {
		t.Fatalf("failed probe notification missing: %q", h.texts())
	}
}

func TestOwnedIsolatedUninstallRequiresConfirmationAndPreservesSharedState(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	root := managedLanguageServerRoot(h.app.pathUserData, "ruff")
	ownExecutable := filepath.Join(root, "bin", executableName("ruff"))
	if err := os.MkdirAll(filepath.Dir(ownExecutable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ownExecutable, []byte("owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := jsonMarshalManagedManifest("ruff")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, managedLanguageServerManifestFile), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	sharedFiles := []string{
		filepath.Join(h.app.pathUserData, "tools", "python", "shared.py"),
		filepath.Join(h.app.pathUserData, "tools", "node", "shared.js"),
		filepath.Join(h.app.pathUserData, "tools", "bin", "gopls"),
	}
	for _, path := range sharedFiles {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": {
		Disabled: true, Path: ownExecutable, Arguments: []string{"server"}, ProbeArguments: []string{"--version"},
		Managed: &managedLanguageServerSettings{Root: root, Version: "ruff 1.2.3"},
	}}
	h.app.loadGoLanguageServerDraft()
	h.app.removeManagedLanguageServer("ruff")
	if h.app.confirm == nil {
		t.Fatal("managed removal did not ask for confirmation")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("directory was removed before confirmation: %v", err)
	}
	h.app.acceptConfirm()
	h.waitFor(t, "managed removal", func() bool { return !h.app.languageServerUninstalling["ruff"] })
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned root was not removed: %v", err)
	}
	for _, path := range sharedFiles {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("shared or legacy file %q was deleted: %v", path, err)
		}
	}
	got := h.app.settings.LanguageServers["ruff"]
	if !got.Disabled || got.Path != "" || len(got.Arguments) != 0 || len(got.ProbeArguments) != 0 || got.Managed != nil {
		t.Fatalf("post-removal settings = %#v", got)
	}
	profile := mustLanguageServerProfile(t, "ruff")
	program, arguments := languageServerProgram(h.app.settings.LanguageServers, profile)
	if program != "ruff" || !reflect.DeepEqual(arguments, []string{"server"}) {
		t.Fatalf("PATH fallback = %q %q", program, arguments)
	}
	originalLookPath := languageServerLookPath
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	languageServerLookPath = func(program string) (string, error) {
		if program != "ruff" {
			t.Fatalf("fallback probed %q instead of the profile executable", program)
		}
		return "/usr/bin/ruff", nil
	}
	if !h.app.languageServerExecutableAvailable(profile) {
		t.Fatal("normal PATH fallback is unavailable after managed removal")
	}
}

func TestExternalCustomAndLegacySharedLanguageServersCannotRemoveDirectories(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	removed := 0
	originalRemove := removeManagedLanguageServerDirectory
	t.Cleanup(func() { removeManagedLanguageServerDirectory = originalRemove })
	removeManagedLanguageServerDirectory = func(string) error {
		removed++
		return errors.New("unsafe removal attempted")
	}
	root := managedLanguageServerRoot(h.app.pathUserData, "ruff")
	legacy := filepath.Join(h.app.pathUserData, "tools", "python")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]languageServerSettings{
		"ruff": {Path: filepath.Join(root, "ruff")},
		"custom-ruff": {
			Path: filepath.Join(root, "custom"), Managed: &managedLanguageServerSettings{Root: root},
			Custom: &customLanguageServerDefinition{Name: "Custom", DownloadURL: "https://example.com/", Extensions: map[string]string{".py": "python"}},
		},
		"ty": {Path: filepath.Join(legacy, "bin", "ty"), Managed: &managedLanguageServerSettings{Root: legacy}},
	}
	for id, server := range cases {
		h.app.settings.LanguageServers = map[string]languageServerSettings{id: server}
		h.app.removeManagedLanguageServer(id)
		root := ""
		if server.Managed != nil {
			root = server.Managed.Root
		}
		h.app.beginManagedLanguageServerRemoval(id, root)
		if h.app.confirm != nil {
			t.Fatalf("%s unexpectedly requested managed-removal confirmation", id)
			h.app.cancelConfirm()
		}
	}
	if removed != 0 {
		t.Fatalf("unsafe RemoveAll calls = %d", removed)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("external directory was removed: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy shared directory was removed: %v", err)
	}
}

func TestManagedUninstallRefusesMissingOrCorruptManifest(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "corrupt"}[corrupt], func(t *testing.T) {
			h := menuTestApp(t)
			h.app.pathUserData = t.TempDir()
			root := managedLanguageServerRoot(h.app.pathUserData, "ruff")
			executable := filepath.Join(root, "bin", "ruff")
			if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(executable, []byte("keep"), 0o755); err != nil {
				t.Fatal(err)
			}
			if corrupt {
				if err := os.WriteFile(filepath.Join(root, managedLanguageServerManifestFile), []byte("not json"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			server := languageServerSettings{Path: executable, Arguments: []string{"server"}, Managed: &managedLanguageServerSettings{Root: root}}
			h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": server}
			removed := false
			originalRemove := removeManagedLanguageServerDirectory
			t.Cleanup(func() { removeManagedLanguageServerDirectory = originalRemove })
			removeManagedLanguageServerDirectory = func(string) error { removed = true; return nil }
			h.app.beginManagedLanguageServerRemoval("ruff", root)
			h.waitFor(t, "manifest refusal", func() bool { return !h.app.languageServerUninstalling["ruff"] })
			if removed {
				t.Fatal("RemoveAll ran without a valid matching manifest")
			}
			if got := h.app.settings.LanguageServers["ruff"]; !reflect.DeepEqual(got, server) {
				t.Fatalf("manifest refusal changed settings: %#v", got)
			}
			if _, err := os.Stat(executable); err != nil {
				t.Fatalf("manifest refusal removed files: %v", err)
			}
		})
	}
}

func TestManagedOwnershipNormalizationRejectsInconsistentMetadata(t *testing.T) {
	dataDir := t.TempDir()
	root := managedLanguageServerRoot(dataDir, "typescript")
	script := filepath.Join(root, "node", "node_modules", "typescript", "bin", "tsc")
	valid := languageServerSettings{
		Path: "node", Arguments: []string{script, "--lsp", "--stdio"}, ProbeArguments: []string{script, "--version"},
		Managed: &managedLanguageServerSettings{Root: root, Version: "  TypeScript 7.1  "},
	}
	got := normalizeLanguageServerSettings(map[string]languageServerSettings{"typescript": valid})["typescript"]
	if got.Managed == nil || got.Managed.Version != "TypeScript 7.1" {
		t.Fatalf("valid managed metadata was rejected: %#v", got)
	}
	if unchanged := editLanguageServerLaunch(got, got.Path, got.Arguments); unchanged.Managed == nil {
		t.Fatal("a launch-independent preference edit cleared managed ownership")
	}
	if edited := editLanguageServerLaunch(got, "/external/tsc", got.Arguments); edited.Managed != nil || edited.ProbeArguments != nil {
		t.Fatalf("manual launch edit retained managed ownership/probe: %#v", edited)
	}
	invalid := valid
	invalid.Path = "/external/tsc"
	invalid.Arguments = []string{"--stdio"}
	invalid.ProbeArguments = nil
	if got := normalizeLanguageServerSettings(map[string]languageServerSettings{"typescript": invalid})["typescript"]; got.Managed != nil {
		t.Fatalf("external launch retained managed ownership: %#v", got.Managed)
	}
	legacy := valid
	legacy.Managed = &managedLanguageServerSettings{Root: filepath.Join(dataDir, "tools", "node")}
	if got := normalizeLanguageServerSettings(map[string]languageServerSettings{"typescript": legacy})["typescript"]; got.Managed != nil {
		t.Fatalf("legacy shared root retained managed ownership: %#v", got.Managed)
	}
	rust := valid
	rust.Managed = &managedLanguageServerSettings{Root: managedLanguageServerRoot(dataDir, "rust")}
	rust.Path = filepath.Join(dataDir, "tools", "lsp", "rust", "rust-analyzer")
	rust.Arguments = nil
	if got := normalizeLanguageServerSettings(map[string]languageServerSettings{"rust": rust})["rust"]; got.Managed != nil {
		t.Fatalf("rustup toolchain acquired TanGit ownership: %#v", got.Managed)
	}
}

func TestManagedLifecycleBusyAndUninstallingStates(t *testing.T) {
	originalInstall := performLanguageServerInstall
	t.Cleanup(func() { performLanguageServerInstall = originalInstall })
	calls := 0
	performLanguageServerInstall = func(languageServerProfile, string) (languageServerInstallation, error) {
		calls++
		return languageServerInstallation{}, nil
	}
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.languageServerUninstalling = map[string]bool{"ruff": true}
	h.app.installLanguageServer("ruff")
	if calls != 0 {
		t.Fatal("duplicate install started during managed removal")
	}
	root := managedLanguageServerRoot(h.app.pathUserData, "ruff")
	h.app.settings.LanguageServers = map[string]languageServerSettings{"ruff": {
		Path: filepath.Join(root, "bin", "ruff"), Managed: &managedLanguageServerSettings{Root: root},
	}}
	h.app.languageServerUninstalling = map[string]bool{"ruff": true}
	h.app.settingsPage = settingsPageEditor
	h.app.openSettings()
	h.tester.Frame()
	h.app.languageServerCatalogQuery = "Ruff"
	h.tester.Frame()
	if !h.tester.HasText("Removing…") || h.tester.HasText("Installing…") {
		t.Fatalf("managed-removal state is inaccurate: %q", h.texts())
	}
	h.app.languageServerInstalling = map[string]bool{"ruff": true}
	h.app.languageServerUninstalling = nil
	h.app.removeManagedLanguageServer("ruff")
	if h.app.confirm != nil {
		t.Fatal("duplicate removal opened confirmation while installation was busy")
	}
}

func fakeOwnedLanguageServerInstallation(t *testing.T, profile languageServerProfile, dataDir string) languageServerInstallation {
	t.Helper()
	staging, root, err := newManagedLanguageServerStaging(dataDir, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(staging, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(binDir, executableName(profile.Program))
	if err := os.WriteFile(executable, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	return languageServerInstallation{
		Path: executable, Arguments: append([]string(nil), profile.Arguments...),
		ProbeArguments: append([]string(nil), profile.VersionArguments...),
		OwnedRoot:      staging, ManagedRoot: root, ProbeRequired: true,
	}
}

func mustLanguageServerProfile(t *testing.T, id string) languageServerProfile {
	t.Helper()
	profile, ok := languageServerProfileByID(id)
	if !ok {
		t.Fatalf("profile %q not found", id)
	}
	return profile
}

func jsonMarshalManagedManifest(id string) ([]byte, error) {
	return json.Marshal(managedLanguageServerManifest{Format: 1, ID: id})
}
