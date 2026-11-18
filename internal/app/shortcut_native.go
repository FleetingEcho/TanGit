package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// onNativeShortcut observes native MyGo controls. Application registrations
// already logged by invokeShortcut are marked, so each accepted event lands in
// this repository's log exactly once. Unannotated framework navigation remains
// useful with the receiver's actual accessible name rather than guessed verbs.
func (a *app) onNativeShortcut(event ui.ShortcutEvent) {
	if event.AlreadyRecorded {
		return
	}
	chord := shortcutChord(event.Modifiers, event.Key)
	id := event.ActionID
	if id == "" {
		id = "native/" + chord
	}
	label := event.Label
	if label == "" {
		label = tr("Keyboard shortcut")
	}
	if event.ActionID == "" {
		switch event.Key {
		case ui.KeyUp, ui.KeyDown, ui.KeyJ, ui.KeyK, ui.KeyLeft, ui.KeyRight:
			label = tr("Move through a list")
		case ui.KeyHome, ui.KeyEnd:
			label = tr("Jump to the first or last item")
		case ui.KeyPageUp, ui.KeyPageDown:
			label = tr("Move a page at a time")
		case ui.KeyF10:
			label = tr("Open the selected item’s context menu")
		}
	}
	a.invokeShortcut(shortcutDefinition{id: id, chord: chord, label: label}, nil)
}

var shortcutKeyNames = map[ui.Key]string{
	ui.KeyA: "a", ui.KeyB: "b", ui.KeyC: "c", ui.KeyD: "d", ui.KeyE: "e",
	ui.KeyF: "f", ui.KeyG: "g", ui.KeyH: "h", ui.KeyI: "i", ui.KeyJ: "j",
	ui.KeyK: "k", ui.KeyL: "l", ui.KeyM: "m", ui.KeyN: "n", ui.KeyO: "o",
	ui.KeyP: "p", ui.KeyQ: "q", ui.KeyR: "r", ui.KeyS: "s", ui.KeyT: "t",
	ui.KeyU: "u", ui.KeyV: "v", ui.KeyW: "w", ui.KeyX: "x", ui.KeyY: "y",
	ui.KeyZ: "z", ui.Key0: "0", ui.Key1: "1", ui.Key2: "2", ui.Key3: "3",
	ui.Key4: "4", ui.Key5: "5", ui.Key6: "6", ui.Key7: "7", ui.Key8: "8",
	ui.Key9: "9", ui.KeyF1: "F1", ui.KeyF2: "F2", ui.KeyF3: "F3", ui.KeyF4: "F4",
	ui.KeyF5: "F5", ui.KeyF6: "F6", ui.KeyF7: "F7", ui.KeyF8: "F8",
	ui.KeyF9: "F9", ui.KeyF10: "F10", ui.KeyF11: "F11", ui.KeyF12: "F12",
	ui.KeyEnter: "Enter", ui.KeySpace: "Space", ui.KeyTab: "Tab",
	ui.KeyEscape: "Escape", ui.KeyUp: "Up", ui.KeyDown: "Down",
	ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyHome: "Home",
	ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
	ui.KeySlash: "/", ui.KeySemicolon: ";", ui.KeyComma: ",",
	ui.KeyBackquote: "`", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]",
}

func shortcutChord(mods ui.Modifiers, key ui.Key) string {
	parts := make([]string, 0, 5)
	if mods&ui.Cmd != 0 {
		parts = append(parts, cmdShortcutChord())
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Ctrl != 0 && ui.Cmd != ui.Ctrl {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if name, ok := shortcutKeyNames[key]; ok {
		parts = append(parts, name)
	} else {
		parts = append(parts, strconv.Itoa(int(key)))
	}
	return strings.Join(parts, "+")
}
