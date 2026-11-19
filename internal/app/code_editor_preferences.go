package app

import "github.com/egoist/mygo/ui/editor"

func (a *app) applyCodeEditorPreferences(state *editor.State) {
	if state == nil {
		return
	}
	settings := effectiveBuiltInEditorPreferences(a.settings)
	state.SoftWrap = settings.EditorWordWrap
	state.NoAutoClose = !settings.EditorAutoClose
	switch settings.EditorLineNumbers {
	case "relative":
		state.LineNumbers = editor.LineNumbersRelative
	case "off":
		state.LineNumbers = editor.LineNumbersOff
	default:
		state.LineNumbers = editor.LineNumbersOn
	}
	switch settings.EditorWhitespace {
	case "none":
		state.Whitespace = editor.WhitespaceNone
	case "all":
		state.Whitespace = editor.WhitespaceAll
	case "trailing":
		state.Whitespace = editor.WhitespaceTrailing
	case "boundary":
		state.Whitespace = editor.WhitespaceBoundary
	default:
		state.Whitespace = editor.WhitespaceSelection
	}
}

func effectiveBuiltInEditorPreferences(settings appSettings) appSettings {
	if settings.EditorPreferencesConfigured {
		return settings
	}
	defaults := defaultSettings()
	settings.EditorWordWrap = defaults.EditorWordWrap
	settings.EditorLineNumbers = defaults.EditorLineNumbers
	settings.EditorWhitespace = defaults.EditorWhitespace
	settings.EditorAutoClose = defaults.EditorAutoClose
	settings.EditorInlayHints = defaults.EditorInlayHints
	settings.EditorSemanticHighlighting = defaults.EditorSemanticHighlighting
	settings.EditorMinimap = defaults.EditorMinimap
	settings.EditorStickyScroll = defaults.EditorStickyScroll
	return settings
}

func (a *app) ensureBuiltInEditorPreferences() {
	if a.settings.EditorPreferencesConfigured {
		return
	}
	settings := effectiveBuiltInEditorPreferences(a.settings)
	settings.EditorPreferencesConfigured = true
	a.settings = settings
}
