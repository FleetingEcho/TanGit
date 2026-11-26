package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	updaternative "github.com/egoist/mygo/plugins/updater/native"
)

const projectRepositoryURL = "https://github.com/FleetingEcho/lazygitGo"

// appVersionFallback is the version to show when the build carries none, as
// `go run` and the tests do. `mygo build` links the version mygo.json names,
// which is the one a release is stamped with.
const appVersionFallback = "1.11.4"

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
// starts. lazygitGo has no webview, so the native plugin keeps updates available
// on every platform without adding a browser runtime.
func setupUpdater() {
	mygo.Use(updaternative.Plugin)
}

// watchUpdater connects the process-wide updater to this window's chrome.
// Automatic checks happen in the plugin; the shell only keeps the version it
// should advertise and opens the plugin window when its icon is clicked.
func watchUpdater(s *shell) {
	s.checkForUpdates = updater.CheckForUpdates
	nativeEnabled := mygo.Updater.Enabled()
	if usePackageUpdates(nativeEnabled) {
		if debianInstallerAvailable() {
			s.packageUpdate.install = installDebianUpdate
		}
		s.packageUpdate.restart = func() { mygo.App.Relaunch() }
		watchPackageUpdates(s, checkPackageRelease)
		return
	}
	s.setUpdateInfo(appUpdateInfo{
		AvailableVersion: updater.AvailableVersion(),
		LastCheck:        updater.LastCheck(),
		Enabled:          nativeEnabled,
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

// watchPackageUpdates checks the latest release once at launch, and again
// whenever the update button is pressed.
func watchPackageUpdates(s *shell, check func(context.Context) (*packageRelease, error)) {
	run := func(manual bool) {
		p := &s.packageUpdate
		if manual {
			p.open = true
		}
		if p.checking || p.installing || p.installed {
			return
		}
		p.checking = true
		p.release = nil
		p.err = ""
		go func() {
			release, err := check(context.Background())
			s.update(func() { s.finishPackageCheck(release, err) })
		}()
	}
	s.setUpdateInfo(appUpdateInfo{Enabled: true})
	s.checkForUpdates = func() { run(true) }
	run(false)
}

// finishPackageCheck publishes the result to the badge and the update dialog.
func (s *shell) finishPackageCheck(release *packageRelease, err error) {
	info := appUpdateInfo{Enabled: true}
	if err == nil {
		info.LastCheck = time.Now()
		if release != nil {
			info.AvailableVersion = release.Version
		}
	} else {
		info = s.updateInfo()
	}
	s.setUpdateInfo(info)
	p := &s.packageUpdate
	p.checking = false
	p.release = release
	p.err = safeError(err)
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
