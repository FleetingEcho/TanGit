package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
)

func TestSettingsSeparatesLanguageServersFromEditor(t *testing.T) {
	h := menuTestApp(t)
	h.app.openSettingsSection(settingsPageCodeEditor)
	h.tester.Frame()
	if _, found := h.tester.Access("Code editor settings"); found {
		t.Error("the settings rail still has a separate Code editor category")
	}
	if err := h.tester.Click("LSP settings"); err != nil {
		t.Fatalf("open the standalone LSP category: %v", err)
	}
	h.tester.Frame()
	if h.app.settingsUI.page != settingsPageLSP || !h.tester.HasText("Language Server Catalog") {
		t.Fatalf("LSP category did not show the language server catalog: %q", h.texts())
	}
	if h.tester.HasText("Editor key bindings") {
		t.Fatal("LSP category still contains editor shortcut controls")
	}
	if err := h.tester.Click("Editor settings"); err != nil {
		t.Fatalf("return to Editor settings: %v", err)
	}
	h.tester.Frame()
	for _, text := range []string{"Editor key bindings", "Built-in editor preferences", "Presets"} {
		if !h.tester.HasText(text) {
			t.Errorf("Editor category lost %q", text)
		}
	}
	if h.tester.HasText("Language Server Catalog") {
		t.Fatal("Editor category still embeds the language server catalog")
	}
	preferences, foundPreferences := h.tester.Find("Built-in editor preferences")
	presets, foundPresets := h.tester.Find("Presets")
	shortcuts, foundShortcuts := h.tester.Find("Editor key bindings")
	if !foundPreferences || !foundPresets || !foundShortcuts {
		t.Fatal("Editor page is missing a grouped preference section")
	}
	if shortcuts.Y <= preferences.Y || shortcuts.Y <= presets.Y {
		t.Fatalf("keyboard shortcuts must be last on the Editor page: preferences=%v presets=%v shortcuts=%v", preferences.Y, presets.Y, shortcuts.Y)
	}
}

func TestCodeEditorDeepLinkUsesEditorCategory(t *testing.T) {
	h := menuTestApp(t)
	h.app.openSettingsSection(settingsPageCodeEditor)
	h.tester.Frame()
	if !h.tester.HasText("Editor key bindings") {
		t.Fatal("Code editor deep link no longer opens its shortcut settings")
	}
	h.app.openSettingsSection(settingsPageLSP)
	h.tester.Frame()
	if !h.tester.HasText("Language Server Catalog") {
		t.Fatal("navigating from the deep link did not open LSP")
	}
	h.app.openSettingsSection(settingsPageEditor)
	h.tester.Frame()
	for _, text := range []string{"Editor key bindings", "Built-in editor preferences", "Presets"} {
		if !strings.Contains(strings.Join(h.tester.Texts(), "\n"), text) {
			t.Errorf("Editor category does not retain %q", text)
		}
	}
}

func TestEditorShortcutRecorderKeepsPromptOnOneLine(t *testing.T) {
	for _, width := range []int{640, 720, 960, 1280} {
		t.Run(fmt.Sprintf("width=%d", width), func(t *testing.T) {
			h := editorShortcutSettingsHarness(t, "Move line up")
			h.tester.SetSize(width, 800)
			action, ok := editorShortcutActionForID(editor.MoveLinesUp.String())
			if !ok {
				t.Fatal("Move line up action is missing")
			}
			h.app.beginEditorShortcutRecording(action, 0)
			h.tester.Frame()
			// The binding's row sits below the Editor page's earlier sections,
			// and an element scrolled out of its pane has no size: bring it in.
			label := editorShortcutRecordLabel(action, 0)
			recorder, found := h.tester.Access(label)
			for range 30 {
				if found && recorder.Bounds.H > 0 {
					break
				}
				h.tester.Wheel(float32(width)/2, 400, 0, 500)
				h.tester.Advance(time.Second)
				h.tester.Frame()
				recorder, found = h.tester.Access(label)
			}
			if !found {
				t.Fatal("shortcut recorder is missing")
			}
			prompt, found := h.tester.Find(tr("Press a shortcut…"))
			if !found {
				t.Fatalf("recording prompt is not visible at width %d", width)
			}
			if prompt.H >= recorder.Bounds.H {
				t.Fatalf("recording prompt is multiple lines or clipped: prompt=%+v control=%+v", prompt, recorder.Bounds)
			}
			if prompt.X < recorder.Bounds.X || prompt.Y < recorder.Bounds.Y ||
				prompt.X+prompt.W > recorder.Bounds.X+recorder.Bounds.W+0.5 ||
				prompt.Y+prompt.H > recorder.Bounds.Y+recorder.Bounds.H+0.5 {
				t.Fatalf("recording prompt is outside its control: prompt=%+v control=%+v", prompt, recorder.Bounds)
			}
		})
	}
}
