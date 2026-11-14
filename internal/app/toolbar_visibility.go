package app

import "slices"

// toolbarAction describes one optional global action. IDs are persisted;
// labels follow the button's existing translation.
type toolbarAction struct {
	id    string
	label string
}

var headerToolbarActions = []toolbarAction{
	{id: "fetch", label: "Fetch"},
	{id: "pull", label: "Pull"},
	{id: "push", label: "Push"},
	{id: "refresh", label: "Refresh"},
	{id: "editor", label: "Open in editor"},
	{id: "theme", label: "Theme"},
	{id: "settings", label: "Settings"},
}

var statusToolbarActions = []toolbarAction{
	{id: "commands", label: "Commands"},
	{id: "pull-requests", label: "Pull requests"},
	{id: "terminal", label: "Toggle terminal"},
	{id: "keyboard-shortcuts", label: "Keyboard shortcuts"},
	{id: "command-log", label: "Command log"},
}

// normalizeHiddenToolbarActions drops unknown IDs and duplicates. Nil means
// an older settings file, where every action was visible, just like empty.
func normalizeHiddenToolbarActions(hidden []string, actions []toolbarAction) []string {
	if len(hidden) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(hidden))
	for _, action := range actions {
		if slices.Contains(hidden, action.id) {
			normalized = append(normalized, action.id)
		}
	}
	return normalized
}

func toolbarActionVisible(hidden []string, id string) bool {
	return !slices.Contains(hidden, id)
}

func (a *app) headerActionVisible(id string) bool {
	return toolbarActionVisible(a.settings.HiddenHeaderActions, id)
}

func (a *app) statusActionVisible(id string) bool {
	return toolbarActionVisible(a.settings.HiddenStatusActions, id)
}

func (a *app) anyHeaderActionVisible(ids ...string) bool {
	for _, id := range ids {
		if a.headerActionVisible(id) {
			return true
		}
	}
	return false
}

// setToolbarActionVisible updates one independent choice without imposing a
// minimum number of visible buttons.
func setToolbarActionVisible(hidden []string, actions []toolbarAction, id string, visible bool) []string {
	if visible {
		hidden = slices.DeleteFunc(slices.Clone(hidden), func(candidate string) bool { return candidate == id })
	} else if !slices.Contains(hidden, id) {
		hidden = append(slices.Clone(hidden), id)
	}
	return normalizeHiddenToolbarActions(hidden, actions)
}
