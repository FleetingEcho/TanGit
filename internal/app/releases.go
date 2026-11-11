package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	updaternative "github.com/egoist/mygo/plugins/updater/native"
)

// releasesPage is where the app's releases, with their notes and downloads,
// live on GitHub. The version row in Settings links here.
const releasesPage = "https://github.com/FleetingEcho/TanGit/releases"

// appVersionFallback is the version to show when the build carries none, as
// `go run` and the tests do. `mygo build` links the version mygo.json names,
// which is the one a release is stamped with.
const appVersionFallback = "1.3.0"

// appUpdateInfo is the updater state the title bar and Settings share. The
// updater owns the check and installation flow; the app only presents its
// latest result.
type appUpdateInfo struct {
	AvailableVersion string
	LastCheck        time.Time
	Enabled          bool
}

// currentAppVersion is the running build's version.
func currentAppVersion() string {
	if v := strings.TrimSpace(mygo.App.Version()); v != "" {
		return v
	}
	return appVersionFallback
}

// setupUpdater installs the native update window before the application
// starts. TanGit has no webview, so the native plugin keeps updates available
// on every platform without adding a browser runtime.
func setupUpdater() {
	mygo.Use(updaternative.Plugin)
}

// watchUpdater connects the process-wide updater to this window's chrome.
// Automatic checks happen in the plugin; the shell only keeps the version it
// should advertise and opens the plugin window when its icon is clicked.
func watchUpdater(s *shell) {
	s.checkForUpdates = updater.CheckForUpdates
	s.setUpdateInfo(appUpdateInfo{
		AvailableVersion: updater.AvailableVersion(),
		LastCheck:        updater.LastCheck(),
		Enabled:          mygo.Updater.Enabled(),
	})
	s.stopUpdateWatch = updater.OnChange(func() {
		info := appUpdateInfo{
			AvailableVersion: updater.AvailableVersion(),
			LastCheck:        updater.LastCheck(),
			Enabled:          mygo.Updater.Enabled(),
		}
		s.update(func() { s.setUpdateInfo(info) })
	})
}

func (s *shell) setUpdateInfo(info appUpdateInfo) {
	s.availableUpdate = info.AvailableVersion
	s.lastUpdateCheck = info.LastCheck
	s.updaterEnabled = info.Enabled
}

func (s *shell) updateInfo() appUpdateInfo {
	return appUpdateInfo{
		AvailableVersion: s.availableUpdate,
		LastCheck:        s.lastUpdateCheck,
		Enabled:          s.updaterEnabled,
	}
}
