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
	c.DescribeShortcut(ui.ShortcutDescriptor{ID: def.id, Label: def.label, AlreadyRecorded: true}, mods, key)
	c.OnShortcut(mods, key, func() { a.invokeShortcut(def, shortcutCallback(run)) })
}

func shortcut(id, chord, label string) shortcutDefinition {
	return shortcutDefinition{id: id, chord: chord, label: label}
}

func (a *app) installShortcutObserver(c *ui.Context) {
	c.ObserveShortcuts(a.onNativeShortcut)
}

func cmdShortcutChord() string {
	if ui.Cmd != ui.Ctrl {
		return "Cmd"
	}
	return "Ctrl"
}

func (a *app) onWindowShortcut(c *ui.Context, mods ui.Modifiers, key ui.Key, def shortcutDefinition, run func()) {
	c.DescribeShortcut(ui.ShortcutDescriptor{ID: def.id, Label: def.label, AlreadyRecorded: true}, mods, key)
	c.OnWindowShortcut(mods, key, func() { a.invokeShortcut(def, shortcutCallback(run)) })
}

func (a *app) onElementShortcut(e ui.Element, mods ui.Modifiers, key ui.Key, def shortcutDefinition, run func()) {
	e.DescribeShortcut(ui.ShortcutDescriptor{ID: def.id, Label: def.label, AlreadyRecorded: true}, mods, key)
	e.OnShortcut(mods, key, func() { a.invokeShortcut(def, shortcutCallback(run)) })
}

// onElementSubmit annotates the callback, never the view construction itself.
// Registration must not acknowledge anything before a real submit action.
func (a *app) onElementSubmit(e ui.Element, def shortcutDefinition, run func()) {
	e.DescribeShortcut(ui.ShortcutDescriptor{ID: def.id, Label: def.label, AlreadyRecorded: true}, 0, ui.KeyEnter)
	e.OnSubmit(func() { a.invokeShortcut(def, shortcutCallback(run)) })
}

// onOverlayShortcut preserves the exact trigger/priority of MyGo modal
// shortcuts. It reports only a shortcut the active overlay actually receives;
// callers can keep their existing body and pointer-click branches separate.
func (a *app) onOverlayShortcut(e ui.Element, mods ui.Modifiers, key ui.Key, def shortcutDefinition, run func()) bool {
	e.DescribeShortcut(ui.ShortcutDescriptor{ID: def.id, Label: def.label, AlreadyRecorded: true}, mods, key)
	if !e.OverlayShortcut(mods, key) {
		return false
	}
	a.invokeShortcut(def, shortcutCallback(run))
	return true
}

func shortcutCallback(run func()) func(repository.ShortcutInvocation) {
	if run == nil {
		return nil
	}
	return func(_ repository.ShortcutInvocation) { run() }
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
