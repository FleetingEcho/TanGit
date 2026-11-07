package app

import (
	"runtime"

	"github.com/egoist/mygo"
)

// installNativeHelpShortcut supplies the physical ? key through GTK's menu
// accelerator on Linux. MyGo v0.7.4 maps an unshifted / key event, but drops a
// shifted ? before its ui shortcuts see it. The menu's accelerator maps Shift+/
// through the keyboard layout instead; hiding the bar keeps TanGit's own chrome.
func installNativeHelpShortcut(s *shell) {
	if runtime.GOOS != "linux" || s.window == nil {
		return
	}
	s.window.SetAutoHideMenuBar(true)
	s.window.SetMenu(nativeHelpMenu(s))
}

func nativeHelpMenu(s *shell) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: tr("Help"), Submenu: []*mygo.MenuItem{
			{
				ID:          "current-page-shortcuts",
				Label:       tr("Keyboard shortcuts"),
				Accelerator: "Shift+/",
				Click: func(*mygo.MenuItem, *mygo.Window) {
					s.request(s.openNativeCurrentKeybindings)
				},
			},
		}},
	})
}

// openNativeCurrentKeybindings applies the same modal scope as the ordinary ?
// shortcut: Settings and the reference hand over to help, while another dialog
// keeps the keyboard it owns.
func (s *shell) openNativeCurrentKeybindings() {
	a := s.activeApp()
	if a == nil || a.settings.DisableShortcuts {
		return
	}
	if a.modalOpen() && !a.settingsOpen && !a.keybindingsOpen {
		return
	}
	if a.settingsOpen {
		a.closeSettings()
	}
	a.openCurrentKeybindings()
}
