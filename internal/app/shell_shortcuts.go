package app

import (
	"github.com/egoist/mygo/ui"
	"lazygitapp/internal/repository"
)

// shortcutApp resolves at invocation time, not when a tab was built. Global
// tab commands therefore belong to the repository the user is actually
// looking at, including after a switch created another tab's view.
func (s *shell) shortcutApp() *app {
	if s == nil {
		return nil
	}
	return s.activeApp()
}

func (s *shell) onShortcut(c *ui.Context, mods ui.Modifiers, key ui.Key, def shortcutDefinition, run func()) {
	c.DescribeShortcut(ui.ShortcutDescriptor{ID: def.id, Label: def.label, AlreadyRecorded: true}, mods, key)
	c.OnShortcut(mods, key, func() {
		if current := s.shortcutApp(); current != nil {
			current.invokeShortcut(def, func(_ repository.ShortcutInvocation) { run() })
			return
		}
		run()
	})
}

func (s *shell) onElementShortcut(e ui.Element, mods ui.Modifiers, key ui.Key, def shortcutDefinition, run func()) {
	e.DescribeShortcut(ui.ShortcutDescriptor{ID: def.id, Label: def.label, AlreadyRecorded: true}, mods, key)
	e.OnShortcut(mods, key, func() {
		if current := s.shortcutApp(); current != nil {
			current.invokeShortcut(def, func(_ repository.ShortcutInvocation) { run() })
			return
		}
		run()
	})
}
