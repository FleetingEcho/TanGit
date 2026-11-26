package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// themeEditorNewThemeButton appears only for a saved theme. While a draft is
// unsaved its Create/Cancel actions own the row, with no second Add button.
func (a *app) themeEditorNewThemeButton(c *ui.Context) {
	if a.settingsUI.themeEditorIndex < 0 {
		return
	}
	flatIconButton(c, "New theme", "Start another custom theme from the colors on screen",
		plusIcon, 15, 5, false, func() { a.beginNewCustomTheme(c) })
}

// selectThemeEditorName loads a saved theme for editing. In Visual mode it
// also becomes the active theme; JSON mode keeps its preview independent.
func (a *app) selectThemeEditorName(c *ui.Context, name string) {
	state := &a.settingsUI
	for index, saved := range a.settings.CustomThemes {
		if !strings.EqualFold(name, saved.Name) {
			continue
		}
		state.themeEditorName = saved.Name
		state.themeEditorNameErr = ""
		if index == state.themeEditorIndex {
			return
		}
		a.editCustomTheme(index)
		a.seedThemeJSON()
		if state.themeEditorMode == themeEditorVisual {
			a.settings.Theme = customThemeValue(saved.Name)
			a.settings.Accent = ""
			a.settingsChanged(c)
		}
		return
	}
}

// beginNewCustomTheme starts a separate draft from the palette on screen and
// puts its name field first, rather than overloading the saved-theme selector.
func (a *app) beginNewCustomTheme(c *ui.Context) {
	a.startNewCustomTheme()
	state := &a.settingsUI
	state.themeEditorDraft.customThemeSetting = customThemeFromTheme(c.Theme())
	a.seedThemeJSON()
	state.themeEditorNameInput.Focus(c)
}

// saveThemeEditorName applies the name form's single explicit action.
func (a *app) saveThemeEditorName(c *ui.Context) {
	if a.settingsUI.themeEditorIndex >= 0 {
		a.renameSelectedTheme(c)
		return
	}
	a.createCustomTheme(c)
}

func (a *app) createCustomTheme(c *ui.Context) {
	state := &a.settingsUI
	if len(state.themeEditorDraft.Colors) == 0 {
		state.themeEditorDraft.customThemeSetting = customThemeFromTheme(c.Theme())
	}
	a.useCustomTheme(c)
	if state.themeEditorIndex >= 0 {
		a.seedThemeJSON()
	}
}

func (a *app) renameSelectedTheme(c *ui.Context) {
	state := &a.settingsUI
	name := strings.TrimSpace(state.themeEditorName)
	if problem := a.themeNameProblem(name, state.themeEditorIndex); problem != "" {
		state.themeEditorNameErr = problem
		return
	}
	theme := a.editedTheme()
	if a.settings.Theme == customThemeValue(theme.Name) {
		a.settings.Theme = customThemeValue(name)
	}
	theme.Name = name
	state.themeEditorName = name
	state.themeEditorNameErr = ""
	state.themeEditorRenaming = false
	a.seedThemeJSON()
	a.settingsChanged(c)
}

// cancelThemeEditorName discards an unfinished rename or draft. Cancelling a
// new draft returns to the previous saved theme, or the first saved theme, so
// theme switching stays inside the dialog.
func (a *app) cancelThemeEditorName(c *ui.Context) {
	state := &a.settingsUI
	state.themeEditorNameErr = ""
	if state.themeEditorIndex >= 0 {
		state.themeEditorName = a.editedTheme().Name
		state.themeEditorRenaming = false
		return
	}
	index := customThemeIndex(a.settings.CustomThemes, state.themeEditorPrevious)
	if index < 0 && len(a.settings.CustomThemes) > 0 {
		index = 0
	}
	if index >= 0 {
		a.editCustomTheme(index)
		a.seedThemeJSON()
		if state.themeEditorMode == themeEditorVisual {
			a.settings.Theme = customThemeValue(a.editedTheme().Name)
			a.settings.Accent = ""
			a.settingsChanged(c)
		}
		return
	}
	state.themeEditorOpen = false
	state.themeEditorJSON = themeJSONDraft{}
}

// themeEditorNamePicker is either a saved-theme selector and its actions, or
// a name form for a new theme / rename. It never infers navigation from typing.
func (a *app) themeEditorNamePicker(c *ui.Context) {
	state := &a.settingsUI
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		if state.themeEditorIndex >= 0 && !state.themeEditorRenaming {
			current := a.editedTheme().Name
			chosen := current
			names := make([]string, len(a.settings.CustomThemes))
			for index, saved := range a.settings.CustomThemes {
				names[index] = saved.Name
			}
			ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Select(c.Key("saved-theme-picker"), &chosen, names).Grow(1).
					Label(tr("Custom theme")).Tooltip(tr("Switch to another saved theme"))
				if chosen != current {
					state.themeEditorName = chosen
					a.selectThemeEditorName(c, chosen)
				}
				if state.themeEditorMode == themeEditorJSONMode {
					a.themeEditorNewThemeButton(c)
				}
				flatIconButton(c, "Rename theme", "Rename theme", editIcon, 15, 5, false, func() {
					state.themeEditorRenaming = true
					state.themeEditorName = a.editedTheme().Name
					state.themeEditorNameErr = ""
					state.themeEditorNameInput.Focus(c)
				})
				flatIconButton(c, "Delete this custom theme", "Delete this custom theme", trashIcon, 15, 5, false, func() {
					a.confirmDeleteCustomTheme(a.editedTheme().Name)
				})
			})
			return
		}

		ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
			input := ui.TextInput(c.Key("theme-name-input"), &state.themeEditorName).
				Bind(&state.themeEditorNameInput).Label(tr("Theme name")).
				Placeholder(tr("Theme name")).FillWidth().MinWidth(0)
			if input.Changed() {
				state.themeEditorNameErr = ""
			}
			input.OnSubmit(func() { a.saveThemeEditorName(c) })
			action, tooltip := "Create theme", "Create theme"
			if state.themeEditorIndex >= 0 {
				action, tooltip = "Rename theme", "Rename theme"
			}
			primaryIconButton(c, action, tooltip, commitIcon, false, func() { a.saveThemeEditorName(c) })
			flatIconButton(c, "Cancel", "Cancel", closeIcon, 15, 5, false, func() {
				a.cancelThemeEditorName(c)
			})
			if state.themeEditorMode == themeEditorJSONMode {
				a.themeEditorNewThemeButton(c)
			}
		})
	})
	if state.themeEditorIndex < 0 {
		ui.Text(c, tr("Save or reset the current new theme before adding another.")).
			TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1)
	}
	if state.themeEditorNameErr != "" {
		ui.Text(c, state.themeEditorNameErr).TextColor(c.Theme().Danger).
			FontSize(c.Theme().FontSize - 1)
	}
}
