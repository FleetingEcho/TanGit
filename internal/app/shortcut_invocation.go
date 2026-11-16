package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"lazygitapp/internal/repository"
)

// shortcutDefinition supplies stable logical provenance at registration.
// Labels are already presentation-ready; custom command names are user data,
// not entries in the translation catalog.
type shortcutDefinition struct {
	id    string
	chord string
	label string
}

// invokeShortcut records the accepted key, optionally acknowledges it, then
// hands an immutable invocation to this action. Callers must pass the returned
// invocation explicitly into any longer workflow instead of reading a mutable
// current-shortcut value on a worker.
func (a *app) invokeShortcut(def shortcutDefinition, run func(repository.ShortcutInvocation)) {
	inv := a.commandLog.RecordShortcut(repository.ShortcutInvocation{
		ID: def.id, Chord: def.chord, Label: def.label,
	})
	if !a.settings.DisableShortcutToasts && strings.TrimSpace(def.label) != "" {
		a.notifyToast(appToast{
			id: "shortcut:" + def.id, kind: "shortcut", title: def.chord,
			description: def.label, timeout: 2 * time.Second,
		})
	}
	if run != nil {
		run(inv)
	}
}

// onShortcut is the ordinary Context registration with provenance. Native
// Context, Element and window registration paths stay distinct so focus and
// modal behavior are not changed by logging.
func (a *app) onShortcut(c *ui.Context, mods ui.Modifiers, key ui.Key, def shortcutDefinition, run func()) {
	c.OnShortcut(mods, key, func() { a.invokeShortcut(def, func(_ repository.ShortcutInvocation) { run() }) })
}

func shortcut(id, chord, label string) shortcutDefinition {
	return shortcutDefinition{id: id, chord: chord, label: label}
}

func cmdShortcutChord() string {
	if ui.Cmd != ui.Ctrl {
		return "Cmd"
	}
	return "Ctrl"
}

func (a *app) onWindowShortcut(c *ui.Context, mods ui.Modifiers, key ui.Key, def shortcutDefinition, run func()) {
	c.OnWindowShortcut(mods, key, func() { a.invokeShortcut(def, func(_ repository.ShortcutInvocation) { run() }) })
}

func (a *app) onElementShortcut(e ui.Element, mods ui.Modifiers, key ui.Key, def shortcutDefinition, run func()) {
	e.OnShortcut(mods, key, func() { a.invokeShortcut(def, func(_ repository.ShortcutInvocation) { run() }) })
}

func customShortcutDefinition(command customCommandSetting) shortcutDefinition {
	name := command.Name
	if name == "" {
		name = commandTitle(command.Command)
	}
	return shortcutDefinition{
		id:    "custom/" + strings.Join(command.Contexts, ",") + "/" + command.Key + "/" + name,
		chord: customCommandDisplayKey(command.Key), label: name,
	}
}
