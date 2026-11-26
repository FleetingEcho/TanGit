package app

import (
	"encoding/json"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	themeEditorVisual = iota
	themeEditorJSONMode
)

type themeJSONDraft struct {
	text    string
	source  string
	problem string
	preview *namedCustomTheme
}

// seedThemeJSON follows visual edits while the JSON is untouched. Pasted
// changes survive switching modes until the user saves or discards them.
func (a *app) seedThemeJSON() {
	draft := &a.settingsUI.themeEditorJSON
	if draft.text != draft.source {
		return
	}
	data, _ := json.MarshalIndent(a.editedTheme(), "", "  ")
	source := string(data)
	if source != draft.source {
		*draft = themeJSONDraft{text: source, source: source}
	}
}

func (a *app) validateThemeJSON(text string) (namedCustomTheme, string) {
	var theme namedCustomTheme
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&theme); err != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		return namedCustomTheme{}, tr("Enter valid theme JSON with name, base, and colors.")
	}
	theme.Name = strings.TrimSpace(theme.Name)
	if problem := a.themeNameProblem(theme.Name, a.settingsUI.themeEditorIndex); problem != "" {
		return namedCustomTheme{}, problem
	}
	if theme.Base != "light" && theme.Base != "dark" {
		return namedCustomTheme{}, tr("Base must be “light” or “dark”.")
	}
	if len(theme.Colors) == 0 {
		return namedCustomTheme{}, tr("Provide at least one theme color.")
	}
	for _, key := range slices.Sorted(maps.Keys(theme.Colors)) {
		if themeColorFieldIndex(key) < 0 {
			return namedCustomTheme{}, trf("Unknown theme color %q.", key)
		}
		color, ok := parseCustomAccent(strings.TrimSpace(theme.Colors[key]))
		if !ok {
			return namedCustomTheme{}, trf("Color %q must use #RRGGBB.", key)
		}
		theme.Colors[key] = formatCustomAccent(color)
	}
	return theme, ""
}

func (a *app) applyThemeJSON(c *ui.Context, save bool) {
	draft := &a.settingsUI.themeEditorJSON
	theme, problem := a.validateThemeJSON(draft.text)
	draft.problem = problem
	if problem != "" {
		return
	}
	if !save {
		draft.preview = &theme
		return
	}
	*a.editedTheme() = theme
	a.settingsUI.themeEditorName = theme.Name
	a.settingsUI.themeEditorNameErr = ""
	a.useCustomTheme(c)
	*draft = themeJSONDraft{}
	a.seedThemeJSON()
}

func (a *app) themeEditorJSONView(c *ui.Context) {
	draft := &a.settingsUI.themeEditorJSON
	if draft.source == "" {
		a.seedThemeJSON()
	}
	ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).FillHeight().Gap(8).Children(func() {
		a.themeEditorNamePicker(c)
		field := ui.TextArea(c, &draft.text).Label(tr("Theme JSON")).Font("monospace").
			FillWidth().Grow(1).MinHeight(0)
		if field.Changed() {
			draft.problem = ""
		}
		if draft.problem != "" {
			ui.Text(c, draft.problem).TextColor(c.Theme().Danger)
		}
		ui.Row(c).Gap(8).Wrap().Children(func() {
			ui.Button(c, tr("Apply")).OnClick(func() { a.applyThemeJSON(c, false) })
			ui.PrimaryButton(c, tr("Save theme")).OnClick(func() { a.applyThemeJSON(c, true) })
			ui.Button(c, tr("Discard")).Disabled(draft.text == draft.source && draft.preview == nil).OnClick(func() {
				*draft = themeJSONDraft{}
				a.seedThemeJSON()
			})
		})
	})
}
