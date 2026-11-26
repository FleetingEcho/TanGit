package app

import (
	"context"

	"github.com/egoist/mygo/ui"
)

type packageUpdateState struct {
	open, checking, installing, installed bool
	applying                              bool
	release                               *packageRelease
	err                                   string
	install                               func(context.Context, packageRelease, func()) error
	restart                               func()
}

func (p *packageUpdateState) canInstall() bool {
	return p.install != nil && p.release != nil && trustedDebianAsset(*p.release)
}

func (s *shell) startPackageInstall() {
	p := &s.packageUpdate
	if p.checking || p.installing || p.installed || !p.canInstall() {
		return
	}
	p.installing = true
	p.applying = false
	p.err = ""
	release, install := *p.release, p.install
	go func() {
		err := install(context.Background(), release, func() {
			s.update(func() { p.applying = true })
		})
		s.update(func() {
			p.installing = false
			p.applying = false
			p.err = safeError(err)
			if err == nil {
				p.installed = true
				s.availableUpdate = ""
			}
		})
	}()
}

func (s *shell) packageUpdateView(c *ui.Context) {
	p := &s.packageUpdate
	if !p.open {
		return
	}
	theme := c.Theme()
	services := c.Services()
	closeDialog := func() { p.open = false }
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		if !p.installing {
			back.OnShortcut(0, ui.KeyEscape, closeDialog)
		}
		back.Children(func() {
			ui.Column(c).Width(460).MaxWidthPercent(92).Padding(18).Gap(12).
				Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Software update")).Children(func() {
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, updateIcon).FontSize(20).TextColor(theme.Accent)
					ui.Text(c, tr("Software update")).FontSize(19).Bold()
				})
				switch {
				case p.checking:
					packageUpdateStatus(c, tr("Checking for updates…"))
				case p.installing && p.applying:
					packageUpdateStatus(c, tr("Installing update…"))
				case p.installing:
					packageUpdateStatus(c, tr("Downloading update…"))
				case p.installed:
					ui.Text(c, trf("Version %s is installed. Restart to use it.", p.release.Version))
				case p.release != nil && p.canInstall():
					ui.Text(c, trf("Version %s is available", p.release.Version))
					ui.Text(c, tr("Your system will ask for permission to install the update.")).TextColor(theme.TextMuted)
				case p.release != nil:
					ui.Text(c, packageUpdateAdvice(*p.release))
				case p.err == "":
					ui.Text(c, trf("Version %s is the latest release.", currentAppVersion()))
				}
				if p.err != "" {
					ui.Scroll(c).MaxHeight(140).FillWidth().Children(func() {
						ui.Text(c, p.err).TextColor(theme.Danger).Selectable()
					})
				}
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					ui.Button(c, tr("Close")).Disabled(p.installing).OnClick(closeDialog)
					if p.release != nil && p.err != "" && !p.installing {
						ui.Button(c, tr("Open GitHub releases")).OnClick(func() {
							services.OpenURL(projectRepositoryURL + "/releases")
						})
					}
					switch {
					case p.installed:
						ui.PrimaryButton(c, tr("Restart now")).OnClick(p.restart)
					case p.checking || p.installing:
					case p.canInstall():
						ui.PrimaryButton(c, tr("Download and install")).OnClick(s.startPackageInstall)
					case p.release != nil:
						page := p.release.Page
						if page == "" {
							page = projectRepositoryURL + "/releases"
						}
						ui.PrimaryButton(c, tr("Release notes and downloads")).OnClick(func() { services.OpenURL(page) })
					case p.err != "":
						ui.PrimaryButton(c, tr("Try again")).OnClick(s.checkForUpdates)
					}
				})
			})
		})
	})
}

func packageUpdateStatus(c *ui.Context, text string) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Spinner(c).Label(text)
		ui.Text(c, text)
	})
}
