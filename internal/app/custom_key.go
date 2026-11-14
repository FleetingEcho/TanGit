package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// parseCustomCommandKey parses a user-written shortcut like "ctrl+g",
// "shift+f5" or "super+k" into modifiers and a key. Matching is
// case-insensitive and ignores surrounding whitespace on each part.
func parseCustomCommandKey(key string) (ui.Modifiers, ui.Key, bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(key)), "+")
	if len(parts) == 0 {
		return 0, 0, false
	}
	var mods ui.Modifiers
	for _, part := range parts[:len(parts)-1] {
		switch strings.TrimSpace(part) {
		case "ctrl", "control":
			mods |= ui.Ctrl
		case "shift":
			mods |= ui.Shift
		case "alt", "option":
			mods |= ui.Alt
		case "super", "cmd", "command", "meta", "win":
			mods |= ui.Super
		default:
			return 0, 0, false
		}
	}
	k, ok := lookupKeyName(strings.TrimSpace(parts[len(parts)-1]))
	if !ok {
		return 0, 0, false
	}
	return mods, k, true
}

var namedKeys = map[string]ui.Key{
	"enter": ui.KeyEnter, "return": ui.KeyEnter,
	"escape": ui.KeyEscape, "esc": ui.KeyEscape,
	"tab":       ui.KeyTab,
	"space":     ui.KeySpace,
	"backspace": ui.KeyBackspace,
	"delete":    ui.KeyDelete, "del": ui.KeyDelete,
	"insert": ui.KeyInsert,
	"home":   ui.KeyHome,
	"end":    ui.KeyEnd,
	"pageup": ui.KeyPageUp, "pgup": ui.KeyPageUp,
	"pagedown": ui.KeyPageDown, "pgdn": ui.KeyPageDown,
	"up": ui.KeyUp, "down": ui.KeyDown, "left": ui.KeyLeft, "right": ui.KeyRight,
	"minus": ui.KeyMinus, "-": ui.KeyMinus,
	"equal": ui.KeyEqual, "=": ui.KeyEqual,
	"comma": ui.KeyComma, ",": ui.KeyComma,
	"period": ui.KeyPeriod, ".": ui.KeyPeriod,
	"slash": ui.KeySlash, "/": ui.KeySlash,
	"backslash": ui.KeyBackslash, "\\": ui.KeyBackslash,
	"semicolon": ui.KeySemicolon, ";": ui.KeySemicolon,
	"quote": ui.KeyQuote, "'": ui.KeyQuote,
	"[": ui.KeyBracketLeft, "bracketleft": ui.KeyBracketLeft,
	"]": ui.KeyBracketRight, "bracketright": ui.KeyBracketRight,
	"backquote": ui.KeyBackquote, "`": ui.KeyBackquote,
}

func lookupKeyName(name string) (ui.Key, bool) {
	if len(name) == 1 {
		r := rune(name[0])
		switch {
		case r >= 'a' && r <= 'z':
			return ui.Key(int(ui.KeyA) + int(r-'a')), true
		case r >= '0' && r <= '9':
			return ui.Key(int(ui.Key0) + int(r-'0')), true
		}
	}
	if len(name) >= 2 && name[0] == 'f' {
		var number int
		if _, err := fmt.Sscanf(name[1:], "%d", &number); err == nil && number >= 1 && number <= 12 {
			return ui.Key(int(ui.KeyF1) + number - 1), true
		}
	}
	key, ok := namedKeys[name]
	return key, ok
}

// keyDisplayName is the inverse of lookupKeyName for the help surface.
var keyDisplayNames = func() map[ui.Key]string {
	names := map[ui.Key]string{}
	for name, key := range namedKeys {
		if _, exists := names[key]; !exists {
			names[key] = displayKeyName(name)
		}
	}
	for i := 0; i < 26; i++ {
		names[ui.Key(int(ui.KeyA)+i)] = strings.ToUpper(string(rune('a' + i)))
	}
	for i := 0; i < 10; i++ {
		names[ui.Key(int(ui.Key0)+i)] = string(rune('0' + i))
	}
	for i := 1; i <= 12; i++ {
		names[ui.Key(int(ui.KeyF1)+i-1)] = fmt.Sprintf("F%d", i)
	}
	return names
}()

func displayKeyName(name string) string {
	switch name {
	case "pageup", "pagedown":
		return strings.ToUpper(name[:2]) + name[2:]
	case "bracketleft":
		return "["
	case "bracketright":
		return "]"
	}
	if len(name) == 1 {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// customCommandDisplayKey renders a stored shortcut for display, falling back
// to the raw text for anything the parser rejected at save time.
func customCommandDisplayKey(key string) string {
	mods, keyValue, ok := parseCustomCommandKey(key)
	if !ok {
		return key
	}
	parts := make([]string, 0, 4)
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if mods&ui.Super != 0 {
		parts = append(parts, "Super")
	}
	name, ok := keyDisplayNames[keyValue]
	if !ok {
		name = "?"
	}
	parts = append(parts, name)
	return strings.Join(parts, "+")
}
