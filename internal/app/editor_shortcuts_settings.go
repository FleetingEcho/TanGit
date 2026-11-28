package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// editorShortcutColumnWidth leaves room for the icon, spacing and the full
// recording prompt at the default theme size without wrapping it.
const editorShortcutColumnWidth = 168

func editorShortcutRecordLabel(action editorShortcutAction, slot int) string {
	if slot == 0 {
		return trf("Record primary shortcut for %s", tr(action.label))
	}
	return trf("Record secondary shortcut for %s", tr(action.label))
}

func (a *app) beginEditorShortcutRecording(action editorShortcutAction, slot int) {
	a.code.shortcutRecording = action.id
	a.code.shortcutSlot = slot
	a.code.shortcutError = ""
}

func (a *app) stopEditorShortcutRecording() {
	a.code.shortcutRecording = ""
	a.code.shortcutError = ""
}

func (a *app) setEditorShortcutBinding(c *ui.Context, action editorShortcutAction, slot int, binding *editor.Binding) {
	draft := cloneEditorShortcutOverrides(a.settings.EditorKeyBindings)
	if draft == nil {
		draft = make(map[string][]string)
	}
	_, customized := draft[action.id]
	raw := append([]string{}, draft[action.id]...)
	if !customized {
		raw = nil
		for _, fallback := range editorShortcutDefaults(action) {
			if text := editorShortcutChord(fallback); text != "" {
				raw = append(raw, text)
			}
		}
	}
	for len(raw) <= slot {
		raw = append(raw, "")
	}
	if binding == nil {
		raw = append(raw[:slot], raw[slot+1:]...)
	} else {
		text := editorShortcutChord(*binding)
		if text == "" {
			a.code.shortcutError = tr("This key cannot be used as a shortcut")
			return
		}
		previous := raw[slot]
		raw[slot] = text
		// Assigning an existing alternative to the other slot swaps the
		// bindings instead of duplicating or losing either alternative.
		for other := range raw {
			if other != slot && raw[other] == text && previous != "" {
				raw[other] = previous
				break
			}
		}
	}
	draft[action.id] = raw
	resolution := resolveEditorShortcutOverrides(draft)
	if resolution.issue != nil {
		a.code.shortcutError = resolution.issue.Error()
		return
	}
	a.code.shortcutRecording = ""
	a.code.shortcutError = ""
	a.settings.EditorKeyBindings = draft
	a.settingsChanged(c)
}

func (a *app) resetEditorShortcut(c *ui.Context, action editorShortcutAction) {
	draft := cloneEditorShortcutOverrides(a.settings.EditorKeyBindings)
	delete(draft, action.id)
	if resolution := resolveEditorShortcutOverrides(draft); resolution.issue != nil {
		a.code.shortcutError = resolution.issue.Error()
		return
	}
	a.settings.EditorKeyBindings = draft
	a.stopEditorShortcutRecording()
	a.settingsChanged(c)
}

func (a *app) restoreEditorShortcutDefaults(c *ui.Context) {
	a.settings.EditorKeyBindings = nil
	a.stopEditorShortcutRecording()
	a.settingsChanged(c)
}

func (a *app) editorShortcutCaptureEvent(c *ui.Context, action editorShortcutAction, slot int, event ui.InputEvent) bool {
	if a.code.shortcutRecording != action.id || a.code.shortcutSlot != slot {
		return false
	}
	if event.Kind == ui.InputText || event.Kind == ui.InputCompose || event.Kind == ui.InputCommand {
		return true
	}
	if event.Kind != ui.InputKeyDown {
		return false
	}
	if event.Repeat {
		return true
	}
	key := event.Key
	if key == ui.KeyUnknown {
		key = event.PhysicalKey
	}
	if key == ui.KeyUnknown {
		return true
	}
	if key == ui.KeyEscape && event.Mods == 0 {
		a.stopEditorShortcutRecording()
		return true
	}
	if key == ui.KeyTab && event.Mods == 0 {
		a.stopEditorShortcutRecording()
		return false
	}
	if key == ui.KeyBackspace && event.Mods == 0 {
		a.setEditorShortcutBinding(c, action, slot, nil)
		return true
	}
	if key == ui.KeyEnter && event.Mods == 0 {
		return true
	}
	binding, err := parseEditorShortcut(editorShortcutChord(editor.Binding{Mods: event.Mods, Key: key}))
	if err != nil {
		a.code.shortcutError = err.Error()
		return true
	}
	a.setEditorShortcutBinding(c, action, slot, &binding)
	return true
}

func (a *app) editorShortcutSettings(c *ui.Context) {
	theme := c.Theme()
	resolution := resolveEditorShortcutOverrides(a.settings.EditorKeyBindings)
	title := ui.Text(c, tr("Keyboard shortcuts")).Bold().FontSize(theme.FontSize + 1)
	if a.settingsUI.revealShortcuts {
		title.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollStart})
		a.settingsUI.revealShortcuts = false
	}
	ui.Text(c, tr("Customize shortcuts for the focused code editor. Click a key, then press a new combination.")).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	ui.Column(c).FillWidth().Gap(10).Padding(12).Background(theme.Surface.Alpha(0.55)).
		Border(1, theme.Border).Radius(theme.Radius + 5).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Icon(c, keyboardIcon).FontSize(17).TextColor(theme.Accent)
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, tr("Editor key bindings")).Bold()
				ui.Text(c, tr("Up to two shortcuts per action")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			})
			reset := compactTextButton(ui.Button(c, tr("Restore defaults"))).
				Label(tr("Restore default editor shortcuts")).Tooltip(tr("Remove all customized editor shortcuts")).
				Disabled(len(a.settings.EditorKeyBindings) == 0)
			if reset.Clicked() {
				a.restoreEditorShortcutDefaults(c)
			}
		})
		filter := ui.SearchField(c, &a.code.shortcutQuery).Label(tr("Filter editor shortcuts")).
			Placeholder(tr("Filter editor shortcuts")).Grow(1)
		if filter.Changed() {
			a.code.shortcutRecording = ""
			a.code.shortcutError = ""
		}
		if resolution.issue != nil {
			ui.Text(c, trf("Some stored shortcuts were ignored: %s", resolution.issue.Error())).
				TextColor(theme.Warning).FontSize(theme.FontSize - 1)
		}
		if a.code.shortcutError != "" {
			ui.Text(c, a.code.shortcutError).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
		}
		a.editorShortcutHeader(c)
		query := strings.ToLower(strings.TrimSpace(a.code.shortcutQuery))
		visible := 0
		for _, action := range editorShortcutActions() {
			if query != "" && !strings.Contains(strings.ToLower(tr(action.label)), query) {
				continue
			}
			visible++
			a.editorShortcutSettingRow(c, action, resolution.byID[action.id])
		}
		if visible == 0 {
			ui.Column(c).Center().Gap(7).Padding(22).Children(func() {
				ui.Icon(c, searchIcon).FontSize(20).TextColor(theme.TextMuted)
				ui.Text(c, tr("No editor shortcuts match this filter")).TextColor(theme.TextMuted).TextAlign(ui.Center)
			})
		}
	})
}

func (a *app) editorShortcutHeader(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Padding(0, 2, 3).Children(func() {
		ui.Text(c, tr("Action")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).Grow(1)
		ui.Text(c, tr("Primary")).Width(editorShortcutColumnWidth).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2)
		ui.Text(c, tr("Secondary")).Width(editorShortcutColumnWidth).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2)
		ui.Box(c).Width(24)
	})
}

func (a *app) editorShortcutSettingRow(c *ui.Context, action editorShortcutAction, bindings []editor.Binding) {
	theme := c.Theme()
	c.Key("editor-shortcut-row:" + action.id)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(3, 2).Children(func() {
		ui.Text(c, tr(action.label)).Grow(1).SingleLine().Ellipsis("…")
		for slot := 0; slot < 2; slot++ {
			bindingText := tr("Not set")
			if slot < len(bindings) {
				bindingText = editorShortcutDisplay(bindings[slot])
			}
			if slot == 1 && slot >= len(bindings) && len(bindings) == 0 {
				ui.Box(c).Width(editorShortcutColumnWidth).MinWidth(editorShortcutColumnWidth).Height(30)
				continue
			}
			c.Key(fmt.Sprintf("editor-shortcut:%d", slot))
			if a.code.shortcutRecording == action.id && a.code.shortcutSlot == slot {
				a.editorShortcutRecorder(c, action, slot)
			} else {
				button := ui.ButtonBase(c).Width(editorShortcutColumnWidth).MinWidth(editorShortcutColumnWidth).Height(30).Gap(5).Padding(3, 7).Radius(theme.Radius).
					Background(theme.Background).Border(1, theme.Border).
					Label(editorShortcutRecordLabel(action, slot)).Tooltip(tr("Click, then press a shortcut"))
				if button.Hovered() {
					button.Background(theme.SurfaceHover)
				}
				button.Children(func() {
					ui.Icon(c, keyboardIcon).FontSize(12).TextColor(theme.TextMuted)
					ui.Text(c, bindingText).FontSize(theme.FontSize - 1).SingleLine().Ellipsis("…").Grow(1).TextAlign(ui.End)
				})
				if button.Clicked() {
					a.beginEditorShortcutRecording(action, slot)
				}
			}
		}
		if _, customized := a.settings.EditorKeyBindings[action.id]; customized {
			action := action
			flatIconButton(c, trf("Reset %s shortcuts", tr(action.label)), "Restore this action's default shortcuts", revertIcon, 13, 4, false, func() {
				a.resetEditorShortcut(c, action)
			})
		} else {
			ui.Box(c).Width(24).Height(24)
		}
	})
}

func (a *app) editorShortcutRecorder(c *ui.Context, action editorShortcutAction, slot int) {
	theme := c.Theme()
	input := ui.ButtonBase(c).Width(editorShortcutColumnWidth).MinWidth(editorShortcutColumnWidth).Height(30).Gap(5).Padding(2, 7).
		AutoFocus().Border(1, theme.Accent).Radius(theme.Radius).
		Background(theme.Accent.Alpha(0.09)).
		Label(editorShortcutRecordLabel(action, slot)).Tooltip(tr("Escape cancels; Backspace clears this shortcut"))
	input.Children(func() {
		ui.Icon(c, keyboardIcon).FontSize(12).TextColor(theme.Accent)
		ui.Text(c, tr("Press a shortcut")).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Grow(1).SingleLine().Ellipsis("…").TextAlign(ui.End)
	})
	input.HandleInput(func(event ui.InputEvent) bool {
		return a.editorShortcutCaptureEvent(c, action, slot, event)
	})
}
