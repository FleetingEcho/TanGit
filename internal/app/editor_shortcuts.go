package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const editorShortcutOutlineID = "editor.outline"

// editorShortcutAction is intentionally application data, not an arbitrary
// command string from a settings file. command is zero only for an app-owned
// editor action such as Outline; its bindings live on the editor pane.
type editorShortcutAction struct {
	id      string
	label   string
	command editor.Command
}

func editorShortcutActions() []editorShortcutAction {
	return []editorShortcutAction{
		{id: editor.MoveLinesUp.String(), label: "Move line up", command: editor.MoveLinesUp},
		{id: editor.MoveLinesDown.String(), label: "Move line down", command: editor.MoveLinesDown},
		{id: editor.DuplicateLinesUp.String(), label: "Duplicate line up", command: editor.DuplicateLinesUp},
		{id: editor.DuplicateLinesDown.String(), label: "Duplicate line down", command: editor.DuplicateLinesDown},
		{id: editor.ToggleComment.String(), label: "Toggle line comment", command: editor.ToggleComment},
		{id: editorShortcutOutlineID, label: "Open Outline"},
	}
}

func editorShortcutActionForID(id string) (editorShortcutAction, bool) {
	for _, action := range editorShortcutActions() {
		if action.id == id {
			return action, true
		}
	}
	return editorShortcutAction{}, false
}

// editorShortcutDefaults relies on MyGo's platform keymap, so future macOS
// and PC conventions come along automatically. Only app actions need their
// own fallback here. The returned slice is safe to change during resolution.
func editorShortcutBaseDefaults(action editorShortcutAction) []editor.Binding {
	if action.id == editorShortcutOutlineID {
		// Outline yields Ctrl+Shift+O to the editor's own go to symbol.
		return []editor.Binding{{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyE}}
	}
	var bindings []editor.Binding
	for _, binding := range editor.DefaultBindings() {
		if binding.Command == action.command {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func editorShortcutDefaults(action editorShortcutAction) []editor.Binding {
	return append(editorShortcutBaseDefaults(action), editorShortcutPreferredDefault(action, nil)...)
}

func editorShortcutResolvedDefaults(action editorShortcutAction, assigned map[string][]editor.Binding) []editor.Binding {
	return append(editorShortcutBaseDefaults(action), editorShortcutPreferredDefault(action, assigned)...)
}

// editorShortcutPreferredDefault adds familiar alternatives without taking a
// shortcut away from an existing editor command. Custom overrides still
// replace both these alternatives and the native defaults for that action.
func editorShortcutPreferredDefault(action editorShortcutAction, assigned map[string][]editor.Binding) []editor.Binding {
	preferred := editor.Binding{}
	switch action.command {
	case editor.MoveLinesUp:
		preferred = editor.Binding{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyUp, Command: action.command}
	case editor.MoveLinesDown:
		preferred = editor.Binding{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyDown, Command: action.command}
	case editor.DuplicateLinesDown:
		preferred = editor.Binding{Mods: ui.Ctrl | ui.Shift, Key: ui.KeyD, Command: action.command}
	default:
		return nil
	}
	for _, existing := range editor.DefaultBindings() {
		if existing.Mods == preferred.Mods && existing.Key == preferred.Key {
			return nil
		}
	}
	for id, choices := range assigned {
		if id == action.id {
			continue
		}
		for _, choice := range choices {
			if choice.Mods == preferred.Mods && choice.Key == preferred.Key {
				return nil
			}
		}
	}
	return []editor.Binding{preferred}
}

func editorShortcutChord(binding editor.Binding) string {
	name, ok := keyDisplayNames[binding.Key]
	if !ok {
		return ""
	}
	parts := make([]string, 0, 5)
	if binding.Mods&ui.Ctrl != 0 {
		parts = append(parts, "ctrl")
	}
	if binding.Mods&ui.Alt != 0 {
		parts = append(parts, "alt")
	}
	if binding.Mods&ui.Shift != 0 {
		parts = append(parts, "shift")
	}
	if binding.Mods&ui.Super != 0 {
		parts = append(parts, "super")
	}
	return strings.ToLower(strings.Join(append(parts, name), "+"))
}

func editorShortcutDisplay(binding editor.Binding) string {
	label := customCommandDisplayKey(editorShortcutChord(binding))
	if ui.Cmd == ui.Super && binding.Mods&ui.Super != 0 {
		label = strings.ReplaceAll(label, "Super", "Cmd")
	}
	switch binding.Key {
	case ui.KeyUp:
		label = strings.TrimSuffix(label, "Up") + "↑"
	case ui.KeyDown:
		label = strings.TrimSuffix(label, "Down") + "↓"
	case ui.KeyLeft:
		label = strings.TrimSuffix(label, "Left") + "←"
	case ui.KeyRight:
		label = strings.TrimSuffix(label, "Right") + "→"
	}
	return label
}

func editorShortcutDisplayList(bindings []editor.Binding) []string {
	labels := make([]string, len(bindings))
	for i, binding := range bindings {
		labels[i] = editorShortcutDisplay(binding)
	}
	return labels
}

func parseEditorShortcut(text string) (editor.Binding, error) {
	mods, key, ok := parseCustomCommandKey(text)
	if !ok {
		return editor.Binding{}, errors.New(tr("shortcuts must be like ctrl+shift+d or alt+up"))
	}
	if mods&(ui.Ctrl|ui.Alt|ui.Super) == 0 && (key < ui.KeyF1 || key > ui.KeyF12) {
		return editor.Binding{}, errors.New(tr("typing keys need Ctrl, Alt, or Command as a modifier; Shift+letter types text"))
	}
	binding := editor.Binding{Mods: mods, Key: key}
	if editorShortcutChord(binding) == "" {
		return editor.Binding{}, errors.New(tr("the key cannot be used as a shortcut"))
	}
	return binding, nil
}

type editorShortcutIssue struct {
	actionID string
	text     string
}

func (i *editorShortcutIssue) Error() string { return i.text }

func editorShortcutProblem(actionID, text string) *editorShortcutIssue {
	label := "Editor"
	if action, ok := editorShortcutActionForID(actionID); ok {
		label = tr(action.label)
	}
	return &editorShortcutIssue{actionID: actionID, text: trf("%s: %s", label, text)}
}

type editorShortcutResolution struct {
	byID   map[string][]editor.Binding
	native []editor.Binding
	issue  *editorShortcutIssue
}

type editorShortcutCandidate struct {
	id      string
	binding editor.Binding
}

func editorShortcutSignature(mods ui.Modifiers, key ui.Key, thenMods ui.Modifiers, thenKey ui.Key) string {
	return fmt.Sprintf("%d/%d:%d/%d", mods, key, thenMods, thenKey)
}

// resolveEditorShortcutOverrides turns command-ID overrides into the small,
// high-priority keymap expected by editor.State. Every overriding command's
// old defaults are explicitly suppressed, while unrelated native defaults
// remain the engine's responsibility. Any issue rejects the whole draft.
func resolveEditorShortcutOverrides(overrides map[string][]string) editorShortcutResolution {
	resolution := editorShortcutResolution{byID: make(map[string][]editor.Binding)}
	failed := func(issue *editorShortcutIssue) editorShortcutResolution {
		resolution := editorShortcutResolution{issue: issue, byID: make(map[string][]editor.Binding)}
		for _, action := range editorShortcutActions() {
			resolution.byID[action.id] = editorShortcutDefaults(action)
		}
		return resolution
	}
	overridden := make(map[string][]editor.Binding)
	orderedActions := editorShortcutActions()
	for _, action := range orderedActions {
		raw, changed := overrides[action.id]
		if !changed {
			continue
		}
		if len(raw) > 2 {
			return failed(editorShortcutProblem(action.id, tr("choose at most two shortcuts")))
		}
		chords := make([]editor.Binding, 0, len(raw))
		for _, text := range raw {
			binding, err := parseEditorShortcut(text)
			if err != nil {
				return failed(editorShortcutProblem(action.id, err.Error()))
			}
			chords = append(chords, binding)
		}
		overridden[action.id] = chords
	}

	candidates := make([]editorShortcutCandidate, 0, len(editor.DefaultBindings())+len(overridden)*2)
	for _, binding := range editor.DefaultBindings() {
		id := binding.Command.String()
		if _, replaced := overridden[id]; !replaced {
			candidates = append(candidates, editorShortcutCandidate{id: id, binding: binding})
		}
	}
	for _, action := range orderedActions {
		chords, custom := overridden[action.id]
		if !custom {
			chords = editorShortcutResolvedDefaults(action, overridden)
		}
		for _, binding := range chords {
			binding.Command = action.command
			candidates = append(candidates, editorShortcutCandidate{id: action.id, binding: binding})
		}
	}
	claims := make(map[string]string, len(candidates))
	prefixes := make(map[string]string)
	for _, candidate := range candidates {
		binding := candidate.binding
		prefix := editorShortcutSignature(binding.Mods, binding.Key, 0, 0)
		signature := editorShortcutSignature(binding.Mods, binding.Key, binding.ThenMods, binding.ThenKey)
		if previous, exists := claims[signature]; exists {
			if previous == candidate.id {
				if _, customized := overridden[candidate.id]; customized {
					return failed(editorShortcutProblem(candidate.id, trf("%s is assigned twice", editorShortcutDisplay(binding))))
				}
			} else {
				return failed(editorShortcutProblem(candidate.id, trf("%s is already assigned to %s",
					editorShortcutDisplay(binding), editorShortcutOwner(previous))))
			}
		}
		claims[signature] = candidate.id
		if binding.ThenKey == 0 {
			if previous, exists := prefixes[prefix]; exists && previous != candidate.id {
				return failed(editorShortcutProblem(candidate.id, trf("%s begins a chord already used by %s",
					editorShortcutDisplay(binding), editorShortcutOwner(previous))))
			}
		}
		prefixes[prefix] = candidate.id
	}

	for _, action := range orderedActions {
		chords, custom := overridden[action.id]
		if !custom {
			chords = editorShortcutResolvedDefaults(action, overridden)
		}
		resolution.byID[action.id] = append([]editor.Binding(nil), chords...)
		for index := range resolution.byID[action.id] {
			resolution.byID[action.id][index].Command = action.command
		}
		if !custom {
			resolution.native = append(resolution.native, editorShortcutPreferredDefault(action, overridden)...)
		}
		if !custom || action.command == editor.NoCommand {
			continue
		}
		for _, binding := range chords {
			binding.Command = action.command
			resolution.native = append(resolution.native, binding)
		}
		for _, binding := range editorShortcutDefaults(action) {
			binding.Command = editor.NoCommand
			resolution.native = append(resolution.native, binding)
		}
	}
	return resolution
}

func editorShortcutOwner(id string) string {
	if action, ok := editorShortcutActionForID(id); ok {
		return tr(action.label)
	}
	command, ok := editor.CommandNamed(id)
	if !ok {
		return tr("another editor command")
	}
	return humanizeEditorCommand(command.String())
}

// Names remain compact for conflicts without requiring every native editor
// command to become a separate settings row in this first customization pass.
func humanizeEditorCommand(name string) string {
	words := make([]string, 0, len(name)/2)
	start := 0
	for i, r := range name {
		if i > start && r >= 'A' && r <= 'Z' {
			words = append(words, strings.ToLower(name[start:i]))
			start = i
		}
	}
	if start < len(name) {
		words = append(words, strings.ToLower(name[start:]))
	}
	return strings.Join(words, " ")
}

func cloneEditorShortcutOverrides(overrides map[string][]string) map[string][]string {
	if len(overrides) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(overrides))
	for id, chords := range overrides {
		cloned[id] = append([]string{}, chords...)
	}
	return cloned
}

func (a *app) applyCodeEditorShortcuts(state *editor.State) {
	if state == nil {
		return
	}
	resolution := resolveEditorShortcutOverrides(a.settings.EditorKeyBindings)
	state.Bindings = resolution.native
}

func (a *app) outlineShortcutBindings() []editor.Binding {
	resolution := resolveEditorShortcutOverrides(a.settings.EditorKeyBindings)
	if a.settings.DisableShortcuts {
		return nil
	}
	return resolution.byID[editorShortcutOutlineID]
}

func (a *app) codeEditorOutlineShortcuts(pane ui.Element) {
	for _, binding := range a.outlineShortcutBindings() {
		binding := binding
		pane.OnShortcut(binding.Mods, binding.Key, func() {
			a.openLanguageSymbols(a.activeCodeDocument())
		})
	}
}

func (a *app) editorShortcutHelpGroup() keyBindingGroup {
	resolution := resolveEditorShortcutOverrides(a.settings.EditorKeyBindings)
	group := keyBindingGroup{title: "Code editor", icon: fileBracesIcon}
	for _, action := range editorShortcutActions() {
		bindings := resolution.byID[action.id]
		if action.id == editorShortcutOutlineID && a.settings.DisableShortcuts {
			bindings = nil
		}
		labels := editorShortcutDisplayList(bindings)
		if len(labels) == 0 && a.settings.DisableShortcuts && action.command != editor.NoCommand {
			continue
		}
		keys := strings.Join(labels, " / ")
		if keys == "" {
			keys = tr("Unassigned")
		}
		group.bindings = append(group.bindings, keyBinding{keys: keys, label: action.label})
	}
	return group
}
