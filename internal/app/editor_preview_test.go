package app

import (
	"os"
	"testing"
)

func TestScreenshotEditorOutline(t *testing.T) {
	directory := os.Getenv("MYGO_SHOTS")
	if directory == "" {
		t.Skip("set MYGO_SHOTS to a directory to write the Outline preview")
	}
	h, _ := languageSymbolsHarness(t)
	h.app.workspaceTab = sectionCode
	h.app.code.files = []string{"main.go"}
	h.app.code.visiblePaths = []string{"main.go"}
	h.app.code.filesLoaded = true
	h.tester.SetDark(true)
	h.tester.Frame()
	h.tester.Move(0, 0)
	saveScreenshot(t, directory, "editor-outline-dark", h.tester)
}

func TestScreenshotEditorShortcutSettings(t *testing.T) {
	directory := os.Getenv("MYGO_SHOTS")
	if directory == "" {
		t.Skip("set MYGO_SHOTS to a directory to write the shortcut settings preview")
	}
	h := editorShortcutSettingsHarness(t, "")
	h.app.settings.EditorKeyBindings = map[string][]string{
		"moveLinesUp":        {"ctrl+shift+up"},
		"duplicateLinesDown": {"ctrl+shift+d", "f9"},
		"editor.outline":     {"ctrl+shift+o"},
	}
	h.tester.SetDark(true)
	h.tester.Frame()
	h.tester.Move(0, 0)
	saveScreenshot(t, directory, "editor-shortcuts-dark", h.tester)
	h.app.beginEditorShortcutRecording(editorShortcutActions()[0], 0)
	h.tester.Frame()
	saveScreenshot(t, directory, "editor-shortcuts-recording-dark", h.tester)
}
