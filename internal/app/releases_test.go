package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/agentcli"
)

func TestRenamedReleaseUsesTanGitUpdateIdentity(t *testing.T) {
	data, err := os.ReadFile("../../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Name, Slug, Identifier, Version string
		Updates                         struct{ GitHub, PublicKey string }
		Windows                         struct{ DisplayName, InstallDirName string }
		Linux                           struct{ Command string }
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Name != appName || config.Windows.DisplayName != appName {
		t.Errorf("packaged names = %q, %q; want %q", config.Name, config.Windows.DisplayName, appName)
	}
	if config.Slug != "lazygitgo" || config.Windows.InstallDirName != appName || config.Linux.Command != appName {
		t.Errorf("distribution slug, Windows installation directory and Linux command = %q, %q, %q",
			config.Slug, config.Windows.InstallDirName, config.Linux.Command)
	}
	if config.Identifier != "dev.tangit.desktop" {
		t.Errorf("bundle identifier = %q, want dev.tangit.desktop for in-place upgrades", config.Identifier)
	}
	if config.Updates.GitHub != "FleetingEcho/lazygitGo" ||
		config.Updates.PublicKey != "1rrogZ62wSzE01n1qmMjC5ZWdTf18oXbVz34aN/zShw=" {
		t.Error("the release must use the feed and signing key trusted by TanGit")
	}
	if !versionNewer(config.Version, "1.23.0") {
		t.Errorf("version %q is not newer than the last TanGit build", config.Version)
	}
}

func TestRenamedAppKeepsTanGitUserData(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses XDG directories to isolate MyGo's default paths")
	}
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv(agentcli.DataDirEnvironment, "")
	t.Setenv("TANGIT_AGENT_DATA_DIR", "")
	oldName := mygo.App.Name()
	t.Cleanup(func() {
		mygo.App.SetName(oldName)
		for _, name := range []mygo.PathName{mygo.PathUserData, mygo.PathCache, mygo.PathLogs} {
			mygo.App.SetPath(name, "")
		}
	})
	mygo.App.SetName(agentcli.LegacyDataDirectoryName)
	configureAppIdentity()
	fresh := filepath.Join(root, "config", appName)
	if dir, err := mygo.App.Path(mygo.PathUserData); err != nil || dir != fresh {
		t.Fatalf("fresh desktop data path = %q (%v), want %q", dir, err, fresh)
	}
	if dir, err := agentcli.ResolveDataDir(""); err != nil || dir != fresh {
		t.Fatalf("fresh CLI data path = %q (%v), want %q", dir, err, fresh)
	}
	legacy := filepath.Join(root, "config", "TanGit")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "updater.json"), []byte(`{"automaticChecks":false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	mygo.App.SetName(agentcli.LegacyDataDirectoryName)
	configureAppIdentity()
	if got := mygo.App.Name(); got != "lazygitGo" {
		t.Errorf("app name = %q, want lazygitGo", got)
	}
	data, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || data != legacy {
		t.Fatalf("desktop data path = %q (%v), want %q", data, err, legacy)
	}
	cli, err := agentcli.ResolveDataDir("")
	if err != nil || cli != data {
		t.Errorf("CLI data path = %q (%v), want %q", cli, err, data)
	}
	if contents, err := os.ReadFile(filepath.Join(data, "updater.json")); err != nil || string(contents) != `{"automaticChecks":false}` {
		t.Errorf("existing updater preferences = %s (%v)", contents, err)
	}
}

// The app always has a version to show, even when it runs from `go run` and
// the build carried none.
func TestCurrentAppVersionIsNeverEmpty(t *testing.T) {
	if version := currentAppVersion(); strings.TrimSpace(version) == "" {
		t.Fatal("the running build has no version to show")
	}
	if appVersionFallback == "" {
		t.Fatal("the fallback version is empty")
	}
}

func TestUpdateSettingsShowsStatusAndOpensRepository(t *testing.T) {
	h := menuTestApp(t)
	var openedURL string
	h.app.openExternalURL = func(url string) { openedURL = url }
	h.app.updateInfo = func() appUpdateInfo {
		return appUpdateInfo{Enabled: true, LastCheck: time.Unix(timeNow(), 0)}
	}
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if err := h.tester.Click("Updates settings"); err != nil {
		t.Fatalf("open Updates settings: %v", err)
	}
	h.tester.Frame()

	if !h.tester.HasText("TanGit is now lazygitGo.") {
		t.Fatal("the settings do not show the rename notice")
	}
	if err := h.tester.Click("Open GitHub repository"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if openedURL != "https://github.com/FleetingEcho/lazygitGo" {
		t.Fatalf("repository button opened %q", openedURL)
	}
	if !h.tester.HasText(currentAppVersion()) {
		t.Fatalf("the settings do not show the running version: %v", h.texts())
	}
	if !h.tester.HasText("Up to date · checked just now") {
		t.Fatalf("the settings do not show the successful check: %v", h.texts())
	}
}

func TestUpdateSettingsAdvertisesAvailableVersion(t *testing.T) {
	h := menuTestApp(t)
	h.app.updateInfo = func() appUpdateInfo {
		return appUpdateInfo{Enabled: true, AvailableVersion: "1.1.0"}
	}
	h.tester.Key(ui.Ctrl, ui.KeyComma)
	h.tester.Frame()
	if err := h.tester.Click("Updates settings"); err != nil {
		t.Fatalf("open Updates settings: %v", err)
	}
	h.tester.Frame()

	if !h.tester.HasText("Version 1.1.0 is available") {
		t.Fatalf("the settings do not advertise the update: %v", h.texts())
	}
}
