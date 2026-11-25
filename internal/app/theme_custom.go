package app

import (
	"math"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

// customThemePrefix marks a user-made theme in appSettings.Theme: the saved
// value is the prefix followed by the theme's name, and its colors live in
// appSettings.CustomThemes, so each survives a restart like any other setting.
const customThemePrefix = "custom:"

// namedCustomTheme is one theme the user saved from the theme editor.
type namedCustomTheme struct {
	Name string `json:"name"`
	customThemeSetting
}

func customThemeValue(name string) string { return customThemePrefix + name }

func isCustomThemeValue(theme string) bool { return strings.HasPrefix(theme, customThemePrefix) }

// customThemeIndex finds the saved theme a Theme value names, or -1.
func customThemeIndex(list []namedCustomTheme, theme string) int {
	if !isCustomThemeValue(theme) {
		return -1
	}
	name := strings.TrimPrefix(theme, customThemePrefix)
	for i, t := range list {
		if t.Name == name {
			return i
		}
	}
	return -1
}

// normalizeCustomThemes drops themes that cannot be used: unnamed, duplicated
// or colorless ones.
func normalizeCustomThemes(list []namedCustomTheme) []namedCustomTheme {
	var out []namedCustomTheme
	seen := map[string]bool{}
	for _, t := range list {
		t.Name = strings.TrimSpace(t.Name)
		t.customThemeSetting = normalizeCustomTheme(t.customThemeSetting)
		if t.Name == "" || seen[t.Name] || len(t.Colors) == 0 {
			continue
		}
		seen[t.Name] = true
		out = append(out, t)
	}
	return out
}

// customThemeSetting is the saved custom palette: a light or dark base for
// everything the editor does not name, and the colors the user chose as
// #rrggbb strings. A color that is absent is derived (hover and pressed
// shades) or taken from the base.
type customThemeSetting struct {
	Base   string            `json:"base,omitempty"` // "dark" (default) or "light"
	Colors map[string]string `json:"colors,omitempty"`
}

// themeColorField describes one editable color of the theme editor.
type themeColorField struct {
	key, label, hint, group string
	// derived colors follow from others until the user sets them.
	derived bool
}

const (
	themeGroupSurfaces = "Surfaces and text"
	themeGroupAccent   = "Accent"
	themeGroupStatus   = "Status colors"
	themeGroupStates   = "Hover and pressed shades"
)

var themeColorFields = []themeColorField{
	{key: "background", label: "Background", hint: "The window behind every card", group: themeGroupSurfaces},
	{key: "surface", label: "Surface", hint: "Cards, inputs and menus", group: themeGroupSurfaces},
	{key: "border", label: "Border", hint: "Outlines and dividers", group: themeGroupSurfaces},
	{key: "text", label: "Text", hint: "Primary text", group: themeGroupSurfaces},
	{key: "textMuted", label: "Muted text", hint: "Hints, timestamps and secondary text", group: themeGroupSurfaces},
	{key: "accent", label: "Accent", hint: "Buttons, links and highlights", group: themeGroupAccent},
	{key: "accentText", label: "Text on accent", hint: "Labels drawn on an accent fill", group: themeGroupAccent, derived: true},
	{key: "success", label: "Success", hint: "Added lines and confirmations", group: themeGroupStatus},
	{key: "warning", label: "Warning", hint: "Cautions and modified files", group: themeGroupStatus},
	{key: "danger", label: "Danger", hint: "Errors, deleted lines and destructive actions", group: themeGroupStatus},
	{key: "surfaceHover", label: "Surface hover", hint: "A surface under the pointer", group: themeGroupStates, derived: true},
	{key: "surfacePressed", label: "Surface pressed", hint: "A surface while pressed", group: themeGroupStates, derived: true},
	{key: "accentHover", label: "Accent hover", hint: "An accent fill under the pointer", group: themeGroupStates, derived: true},
	{key: "accentPressed", label: "Accent pressed", hint: "An accent fill while pressed", group: themeGroupStates, derived: true},
}

// themeColorCount is len(themeColorFields), constant for the editor's state.
const themeColorCount = 14

func themeColorFieldIndex(key string) int {
	for i, field := range themeColorFields {
		if field.key == key {
			return i
		}
	}
	return -1
}

// themeFieldColor reads the color a field names from a built theme.
func themeFieldColor(t *ui.Theme, key string) ui.Color {
	switch key {
	case "background":
		return t.Background
	case "surface":
		return t.Surface
	case "border":
		return t.Border
	case "text":
		return t.Text
	case "textMuted":
		return t.TextMuted
	case "accent":
		return t.Accent
	case "accentText":
		return t.AccentText
	case "success":
		return t.Success
	case "warning":
		return t.Warning
	case "danger":
		return t.Danger
	case "surfaceHover":
		return t.SurfaceHover
	case "surfacePressed":
		return t.SurfacePressed
	case "accentHover":
		return t.AccentHover
	case "accentPressed":
		return t.AccentPressed
	}
	return ui.Color{}
}

func setThemeFieldColor(t *ui.Theme, key string, color ui.Color) {
	switch key {
	case "background":
		t.Background = color
	case "surface":
		t.Surface = color
	case "border":
		t.Border = color
	case "text":
		t.Text = color
	case "textMuted":
		t.TextMuted = color
	case "accent":
		t.Accent = color
	case "accentText":
		t.AccentText = color
	case "success":
		t.Success = color
	case "warning":
		t.Warning = color
	case "danger":
		t.Danger = color
	case "surfaceHover":
		t.SurfaceHover = color
	case "surfacePressed":
		t.SurfacePressed = color
	case "accentHover":
		t.AccentHover = color
	case "accentPressed":
		t.AccentPressed = color
	}
}

// buildCustomTheme turns the saved colors into a palette. Anything the user
// did not choose comes from the base theme; hover and pressed shades follow
// the surface, text and accent they belong to once those have been changed.
func buildCustomTheme(s customThemeSetting) *ui.Theme {
	explicit := func(key string) (ui.Color, bool) {
		return parseCustomAccent(s.Colors[key])
	}
	// The toolkit base (shadows, focus and selection tints, scroll bars, the
	// direction of hover shades) follows the background: a dark page gets the
	// dark base and a light one the light base. Without a chosen background
	// the saved base applies.
	light := s.Base == "light"
	if background, ok := explicit("background"); ok {
		light = colorBrightness(background) > 0.18
	}
	var t *ui.Theme
	if light {
		t = systemLightTheme()
	} else {
		t = themeChoices[defaultTheme].build()
	}
	for _, field := range themeColorFields {
		if field.derived || field.key == "accent" {
			continue
		}
		if color, ok := explicit(field.key); ok {
			setThemeFieldColor(t, field.key, color)
		}
	}
	_, surfaceSet := explicit("surface")
	_, textSet := explicit("text")
	if surfaceSet || textSet {
		t.SurfaceHover = t.Surface.Mix(t.Text, 0.08)
		t.SurfacePressed = t.Surface.Mix(t.Text, 0.16)
	}
	if color, ok := explicit("accent"); ok {
		applyAccent(t, color)
	}
	for _, key := range []string{"surfaceHover", "surfacePressed", "accentHover", "accentPressed", "accentText"} {
		if color, ok := explicit(key); ok {
			setThemeFieldColor(t, key, color)
		}
	}
	return t
}

// customThemeFromTheme captures every editable color of t, so the editor can
// start from any palette (the one on screen, or a built-in) and adjust it.
func customThemeFromTheme(t *ui.Theme) customThemeSetting {
	setting := customThemeSetting{Base: "dark", Colors: make(map[string]string, len(themeColorFields))}
	if !t.Dark {
		setting.Base = "light"
	}
	for _, field := range themeColorFields {
		setting.Colors[field.key] = formatCustomAccent(themeFieldColor(t, field.key))
	}
	return setting
}

// normalizeCustomTheme drops anything a settings file should not carry: an
// unknown base, unknown keys and malformed colors.
func normalizeCustomTheme(s customThemeSetting) customThemeSetting {
	if s.Base != "light" {
		s.Base = "dark"
	}
	colors := make(map[string]string, len(s.Colors))
	for key, value := range s.Colors {
		if themeColorFieldIndex(key) < 0 {
			continue
		}
		if color, ok := parseCustomAccent(strings.ToLower(strings.TrimSpace(value))); ok {
			colors[key] = formatCustomAccent(color)
		}
	}
	if len(colors) == 0 {
		colors = nil
	}
	s.Colors = colors
	return s
}

// customThemeSignature is the cache key part of a custom palette.
func customThemeSignature(s customThemeSetting) string {
	keys := make([]string, 0, len(s.Colors))
	for key := range s.Colors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(s.Base)
	for _, key := range keys {
		b.WriteString("|" + key + "=" + s.Colors[key])
	}
	return b.String()
}

// colorBrightness is the WCAG relative luminance of an opaque color. About 0.18
// is the point where black and white text contrast equally, so it splits light
// backgrounds from dark ones. It runs from 0 for
// black to 1 for white.
func colorBrightness(c ui.Color) float64 {
	channel := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}
