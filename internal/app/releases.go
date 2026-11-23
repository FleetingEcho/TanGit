package app

import (
	"context"
	"runtime"
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
	if usesPackageUpdateCheck() {
		watchPackageUpdates(s)
		return
	}
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

// usesPackageUpdateCheck is true for a release build that cannot replace
// itself where it is installed, such as the .deb: it checks GitHub by itself.
func usesPackageUpdateCheck() bool {
	return runtime.GOOS == "linux" && !mygo.IsDev() && !mygo.Updater.Enabled()
}

// watchPackageUpdates checks the latest release once at launch, and again
// whenever the update button is pressed.
func watchPackageUpdates(s *shell) {
	run := func(manual bool) {
		go func() {
			release, err := checkPackageRelease(context.Background())
			s.update(func() { s.finishPackageCheck(release, err, manual) })
		}()
	}
	s.setUpdateInfo(appUpdateInfo{Enabled: true})
	s.checkForUpdates = func() { run(true) }
	run(false)
}

// finishPackageCheck records a check's result. A manual check also tells the
// user what it found and starts the download of the package.
func (s *shell) finishPackageCheck(release *packageRelease, err error, manual bool) {
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
	if !manual {
		return
	}
	a := s.activeApp()
	if a == nil {
		return
	}
	switch {
	case err != nil:
		a.notify("error", "Could not check for updates", safeError(err))
	case release == nil:
		a.notifyTranslated("success", tr("TanGit is up to date"), trf("Version %s is the latest release.", currentAppVersion()))
	default:
		a.notifyTranslated("info", tr("Update available"), packageUpdateAdvice(*release))
		if a.openExternalURL != nil {
			if release.DebURL != "" {
				a.openExternalURL(release.DebURL)
			} else {
				a.openExternalURL(release.Page)
			}
		}
	}
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
