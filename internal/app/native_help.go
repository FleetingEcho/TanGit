package app

import (
	"runtime"

	"github.com/egoist/mygo"
)

// Native accelerators also recognize shifted punctuation when the toolkit's
// logical-key mapping reports KeyUnknown. macOS needs the application menu;
// the Linux workaround uses the window's hidden menu bar.
func installNativeHelpShortcut(s *shell) {
	if s.window == nil {
		return
	}
	switch runtime.GOOS {
	case "linux":
		s.window.SetAutoHideMenuBar(true)
		menu := nativeHelpMenu(s)
		s.window.SetMenu(menu)
		s.nativeHelpItem = menu.ItemByID("current-page-shortcuts")
	case "darwin":
		menu := nativeApplicationHelpMenu(mygo.App.Menu(), s)
		mygo.App.SetMenu(menu)
		s.nativeHelpItem = menu.ItemByID("current-page-shortcuts")
	}
}

func nativeApplicationHelpMenu(existing *mygo.Menu, s *shell) *mygo.Menu {
	var items []*mygo.MenuItem
	if existing != nil {
		for _, item := range existing.Items() {
			if item.ID != "tangit-help" {
				items = append(items, item)
			}
		}
	}
	// Keep the standard App, Edit, View and Window menus and their accelerators.
	return mygo.NewMenu(append(items, nativeHelpMenu(s).Items()...))
}

func nativeHelpMenu(s *shell) *mygo.Menu {
	accelerator := "Shift+/"
	if runtime.GOOS == "darwin" {
		accelerator = "Shift+?"
	}
	return mygo.NewMenu([]*mygo.MenuItem{
		{ID: "tangit-help", Label: tr("Help"), Submenu: []*mygo.MenuItem{
			{
				ID:          "current-page-shortcuts",
				Label:       tr("Keyboard shortcuts"),
				Accelerator: accelerator,
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
	if !nativeHelpAvailable(a) {
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

// A native accelerator is resolved before text reaches the UI. Disable its
// menu item during text input so typing ? is neither intercepted nor dropped.
func nativeHelpAvailable(a *app) bool {
	return a != nil && !a.settings.DisableShortcuts && !a.keyboardTextFocus && (!a.modalOpen() || a.settingsOpen || a.keybindingsOpen)
}

func (s *shell) refreshNativeHelpEnabled() {
	if s.nativeHelpItem == nil {
		return
	}
	enabled := nativeHelpAvailable(s.activeApp())
	if s.nativeHelpItem.IsEnabled() != enabled {
		s.nativeHelpItem.SetEnabled(enabled)
	}
}
