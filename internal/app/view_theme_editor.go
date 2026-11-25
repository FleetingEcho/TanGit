package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

const maxCustomThemeNameLength = 40

// uniqueCustomThemeName finds a name no saved theme uses yet.
func (a *app) uniqueCustomThemeName(base string) string {
	taken := func(name string) bool { return a.themeNameProblem(name, -1) != "" }
	if !taken(base) {
		return base
	}
	for n := 2; ; n++ {
		if name := fmt.Sprintf("%s %d", base, n); !taken(name) {
			return name
		}
	}
}

// openThemeEditor edits the active custom theme, or starts a new one from the
// palette on screen when the active theme is a built-in. A new theme is only
// saved by its first edit, so opening the editor never leaves one behind.
func (a *app) openThemeEditor() {
	if i := customThemeIndex(a.settings.CustomThemes, a.settings.Theme); i >= 0 {
		a.editCustomTheme(i)
		a.settingsUI.themeEditorPrevious = ""
	} else {
		a.startNewCustomTheme()
	}
	a.settingsUI.themeEditorOpen = true
}

func (a *app) editCustomTheme(index int) {
	a.settingsUI.themeEditorIndex = index
	a.settingsUI.themeEditorName = a.settings.CustomThemes[index].Name
	a.settingsUI.themeEditorNameErr = ""
	a.settingsUI.themeEditorCreated = false
	a.settingsUI.themeEditorNamed = false
}

// startNewCustomTheme prepares an unsaved theme; the first editor frame seeds
// its colors from what is on screen.
func (a *app) startNewCustomTheme() {
	a.settingsUI.themeEditorPrevious = a.settings.Theme
	name := a.uniqueCustomThemeName(tr("My theme"))
	a.settingsUI.themeEditorIndex = -1
	a.settingsUI.themeEditorDraft = namedCustomTheme{Name: name}
	a.settingsUI.themeEditorName = name
	a.settingsUI.themeEditorNameErr = ""
	a.settingsUI.themeEditorCreated = false
	a.settingsUI.themeEditorNamed = false
}

func (a *app) closeThemeEditor() {
	a.commitThemeName()
	// A theme that was only named, never recolored, is still one the user
	// asked for: save it with the colors it started from.
	if a.settingsUI.themeEditorIndex < 0 && a.settingsUI.themeEditorNamed &&
		len(a.settingsUI.themeEditorDraft.Colors) > 0 {
		a.useCustomTheme(nil)
	}
	a.settingsUI.themeEditorOpen = false
	a.saveSettings()
}

// hasCustomTheme reports whether the user has saved any custom theme.
func (a *app) hasCustomTheme() bool { return len(a.settings.CustomThemes) > 0 }

// editedTheme is the theme the editor changes: a saved one, or the unsaved draft.
func (a *app) editedTheme() *namedCustomTheme {
	if i := a.settingsUI.themeEditorIndex; i >= 0 && i < len(a.settings.CustomThemes) {
		return &a.settings.CustomThemes[i]
	}
	return &a.settingsUI.themeEditorDraft
}

// useCustomTheme saves the edited theme if it is still a draft and makes it
// the window's theme. A separately chosen accent would paint over the custom
// accent, so it is cleared.
func (a *app) useCustomTheme(c *ui.Context) {
	if a.settingsUI.themeEditorIndex < 0 {
		a.settings.CustomThemes = append(a.settings.CustomThemes, a.settingsUI.themeEditorDraft)
		a.settingsUI.themeEditorIndex = len(a.settings.CustomThemes) - 1
		a.settingsUI.themeEditorCreated = true
		a.notify("success", "Saved custom theme", a.editedTheme().Name)
	}
	a.settings.Theme = customThemeValue(a.editedTheme().Name)
	a.settings.Accent = ""
	a.settingsChanged(c)
}

// themeNameProblem says why name cannot name the theme at index (-1 for a
// theme not saved yet), or "" when it can. Names are compared without regard
// to case, and the built-in theme names are reserved.
func (a *app) themeNameProblem(name string, index int) string {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxCustomThemeNameLength {
		return trf("Use 1 to %d characters.", maxCustomThemeNameLength)
	}
	for builtin := range themeChoices {
		if strings.EqualFold(builtin, name) {
			return tr("This name is already used by a built-in theme.")
		}
	}
	for i, other := range a.settings.CustomThemes {
		if i != index && strings.EqualFold(other.Name, name) {
			return tr("Another custom theme already has this name.")
		}
	}
	return ""
}

// renameEditedTheme applies the name field as it is typed when the name is
// usable; otherwise the problem is shown under the field and the theme keeps
// its last good name until commitThemeName.
func (a *app) renameEditedTheme() {
	name := strings.TrimSpace(a.settingsUI.themeEditorName)
	t := a.editedTheme()
	if name == t.Name {
		a.settingsUI.themeEditorNameErr = ""
		return
	}
	if problem := a.themeNameProblem(name, a.settingsUI.themeEditorIndex); problem != "" {
		a.settingsUI.themeEditorNameErr = problem
		return
	}
	a.settingsUI.themeEditorNameErr = ""
	if a.settingsUI.themeEditorIndex < 0 {
		a.settingsUI.themeEditorNamed = true
	}
	active := a.settings.Theme == customThemeValue(t.Name)
	t.Name = name
	if active {
		a.settings.Theme = customThemeValue(name)
	}
}

// commitThemeName is called before the editor leaves the theme it is on. A
// rejected name is reported in a notification and the field returns to the
// name the theme keeps.
func (a *app) commitThemeName() {
	if a.settingsUI.themeEditorNameErr == "" {
		return
	}
	kept := a.editedTheme().Name
	a.notify("error", "Theme name not applied",
		a.settingsUI.themeEditorNameErr+" "+trf("Keeping “%s”.", kept))
	a.settingsUI.themeEditorName = kept
	a.settingsUI.themeEditorNameErr = ""
}

// removeCustomTheme deletes a saved theme, returning the window to the theme
// it replaced (or the default) when that theme was in use.
func (a *app) removeCustomTheme(index int, fallback string) {
	if index < 0 || index >= len(a.settings.CustomThemes) {
		return
	}
	value := customThemeValue(a.settings.CustomThemes[index].Name)
	a.settings.CustomThemes = slices.Delete(a.settings.CustomThemes, index, index+1)
	if a.settings.Theme == value {
		a.settings.Theme = fallback
	}
	if customThemeIndex(a.settings.CustomThemes, a.settings.Theme) < 0 {
		if _, builtin := themeChoices[a.settings.Theme]; !builtin {
			a.settings.Theme = defaultTheme
		}
	}
}

// confirmDeleteCustomTheme asks before deleting the saved theme called name,
// from the theme menu or the editor. The window returns to the theme the
// editor replaced, or to the default, when the deleted theme was in use.
func (a *app) confirmDeleteCustomTheme(name string) {
	if customThemeIndex(a.settings.CustomThemes, customThemeValue(name)) < 0 {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Delete custom theme",
		message:      trf("Delete the custom theme “%s”?", name),
		detail:       tr("The colors you chose are removed. Built-in themes are not affected."),
		confirmLabel: "Delete theme",
		destructive:  true,
		run: func() {
			index := customThemeIndex(a.settings.CustomThemes, customThemeValue(name))
			if index < 0 {
				return
			}
			fallback := a.settingsUI.themeEditorPrevious
			if fallback == "" {
				fallback = defaultTheme
			}
			a.removeCustomTheme(index, fallback)
			a.notify("success", "Deleted custom theme", name)
			// The editor's index may now point at another theme.
			a.settingsUI.themeEditorOpen = false
			a.settingsChanged(nil)
		},
	})
}

func (a *app) themeEditorModal(c *ui.Context) {
	if !a.settingsUI.themeEditorOpen {
		return
	}
	if draft := &a.settingsUI.themeEditorDraft; a.settingsUI.themeEditorIndex < 0 && len(draft.Colors) == 0 {
		draft.customThemeSetting = customThemeFromTheme(c.Theme())
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			ui.Column(c).Width(940).Height(660).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(16).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius+10).
				Shadow(0, 24, 64, 0, ui.RGBA(0, 0, 0, 0.3)).Clip().
				Role(ui.RoleDialog).Label(tr("Customize theme")).
				Children(func() {
					a.themeEditorHeader(c)
					ui.Row(c).Grow(1).MinHeight(0).FillWidth().Gap(18).Children(func() {
						ui.Scroll(c).Grow(1).MinHeight(0).FillHeight().Children(func() {
							ui.Column(c).FillWidth().Gap(14).Padding(2, 10, 8, 2).Children(func() {
								a.themeEditorBase(c)
								a.themeEditorFields(c)
							})
						})
						ui.Box(c).Width(1).Background(theme.Border)
						a.themeEditorPreview(c)
					})
					ui.Divider(c)
					a.themeEditorFooter(c)
				})
			if back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeThemeEditor()
			}
		})
	})
}

func (a *app) themeEditorHeader(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Box(c).Size(38, 38).Radius(theme.Radius + 4).Center().
			Background(theme.Accent.Alpha(0.12)).Children(func() {
			ui.Icon(c, themeIcon).FontSize(19).TextColor(theme.Accent)
		})
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, tr("Customize theme")).FontSize(21).Bold()
			ui.Text(c, tr("Pick any color. Every change applies to the whole window and is saved automatically.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
		iconButton(c, "Close theme editor", "Escape", closeIcon, false, a.closeThemeEditor)
	})
}

// themeEditorBase is the card at the top of the editor: the theme's name, the
// saved themes to switch between, and the two choices that restyle everything
// at once, the palette to copy colors from and the light or dark base.
func (a *app) themeEditorBase(c *ui.Context) {
	theme := c.Theme()
	caption := func(text string) {
		ui.Text(c, tr(text)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	}
	islandCard(c, ui.Column(c).FillWidth().Gap(10).Padding(14)).Children(func() {
		caption("Name")
		input := ui.TextInput(c, &a.settingsUI.themeEditorName).Label(tr("Theme name")).
			Placeholder(tr("Theme name")).FillWidth()
		if input.Changed() {
			a.renameEditedTheme()
			a.settingsChanged(c)
		}
		if a.settingsUI.themeEditorNameErr != "" {
			ui.Text(c, a.settingsUI.themeEditorNameErr).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
		}

		if len(a.settings.CustomThemes) > 0 {
			caption("Your themes")
			ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
				current := a.editedTheme().Name
				for i, t := range a.settings.CustomThemes {
					i, name := i, t.Name
					c.Key("theme-chip:" + name)
					// One pill: the name selects the theme, the × inside it deletes.
					shown := name == current && a.settingsUI.themeEditorIndex == i
					fill, line, text := theme.Surface, theme.Border, theme.TextMuted
					if shown {
						fill, line, text = theme.Accent.Alpha(0.16), theme.Accent.Alpha(0.45), theme.Accent
					}
					ui.Row(c).AlignItems(ui.Center).Height(28).Radius(theme.Radius).
						Background(fill).Border(1, line).Children(func() {
						pick := ui.ButtonBase(c).Padding(3, 4, 3, 10).TextColor(text).
							Label(name).Tooltip(trf("Edit %s", name))
						pick.Children(func() { ui.Text(c, name).SingleLine() })
						if pick.Clicked() {
							a.commitThemeName()
							a.editCustomTheme(i)
							a.settings.Theme = customThemeValue(name)
							a.settingsChanged(c)
						}
						remove := ui.ButtonBase(c).Padding(3, 7, 3, 3).Radius(theme.Radius).
							Label(trf("Delete %s", name)).Tooltip(tr("Delete this custom theme"))
						remove.Children(func() { ui.Icon(c, closeIcon).FontSize(11).TextColor(text) })
						if remove.Clicked() {
							a.confirmDeleteCustomTheme(name)
						}
					})
				}
				flatIconButton(c, "New theme", "Start another custom theme from the colors on screen", plusIcon, 16, 6, false, func() {
					a.commitThemeName()
					a.startNewCustomTheme()
				})
			})
		}

		caption("Colors")
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			copyFrom := ui.Button(c, "").Label(tr("Copy colors from a built-in theme")).
				Tooltip(tr("Replace your colors with a built-in palette")).Gap(6)
			copyFrom.Children(func() {
				ui.Icon(c, themeIcon).FontSize(14)
				ui.Text(c, tr("Copy from…"))
			})
			copyFrom.PopoverMenuButton(func(m *ui.Menu) {
				light, dark := themeNamesByBrightness()
				for _, group := range []struct {
					label string
					names []string
				}{{"Light themes", light}, {"Dark themes", dark}} {
					group := group
					m.Submenu(tr(group.label), func(sub *ui.Menu) {
						for _, name := range group.names {
							if sub.Item(name).Chosen() {
								source := themeForSettings(appSettings{Theme: name}, theme.Dark)
								a.editedTheme().customThemeSetting = customThemeFromTheme(source)
								a.useCustomTheme(c)
							}
						}
					})
				}
			})
		})
	})
}

func (a *app) themeEditorFields(c *ui.Context) {
	theme := c.Theme()
	resolved := buildCustomTheme(a.editedTheme().customThemeSetting)
	lastGroup := ""
	var groupBody []int
	flush := func() {
		if lastGroup == "" {
			return
		}
		title, indices := lastGroup, groupBody
		settingsChoiceCard(c, title, "", func() {
			for _, i := range indices {
				a.themeEditorRow(c, i, resolved)
			}
		})
	}
	for i, field := range themeColorFields {
		if field.group != lastGroup {
			flush()
			lastGroup, groupBody = field.group, nil
		}
		groupBody = append(groupBody, i)
	}
	flush()
	ui.Text(c, tr("Shades marked Auto follow the colors above them until you set them yourself.")).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
}

// themeEditorRow is one color: its name, its hex value, the color well that
// opens the picker, and the way back to Auto for a derived shade.
func (a *app) themeEditorRow(c *ui.Context, index int, resolved *ui.Theme) {
	theme := c.Theme()
	field := themeColorFields[index]
	c.Key("theme-color:" + field.key)
	current := themeFieldColor(resolved, field.key)
	current = ui.RGB(current.R, current.G, current.B)
	if a.settingsUI.themeEditorColors[index] != current {
		a.settingsUI.themeEditorColors[index] = current
	}
	_, set := a.editedTheme().Colors[field.key]
	ui.Row(c).AlignItems(ui.Center).Gap(12).MinHeight(44).Children(func() {
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, tr(field.label)).Bold()
			ui.Text(c, tr(field.hint)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
		if field.derived && !set {
			ui.Badge(c, tr("Auto"))
		}
		ui.Text(c, formatCustomAccent(current)).TextColor(theme.TextMuted).Selectable()
		services := c.Services()
		hex := formatCustomAccent(current)
		flatIconButton(c, "Copy color", "Copy the hex value", copyIcon, 14, 5, false,
			func() { a.notifyCopied(services, "color", hex) })
		well := ui.ColorWell(c, &a.settingsUI.themeEditorColors[index]).
			Label(trf("%s color", tr(field.label))).Tooltip(tr("Choose a color"))
		if well.Changed() {
			picked := a.settingsUI.themeEditorColors[index]
			edited := a.editedTheme()
			if edited.Colors == nil {
				edited.Colors = map[string]string{}
			}
			edited.Colors[field.key] = formatCustomAccent(ui.RGB(picked.R, picked.G, picked.B))
			a.useCustomTheme(c)
		}
		if field.derived && set {
			flatIconButton(c, "Auto", "Follow the colors above again", revertIcon, 15, 6, false, func() {
				delete(a.editedTheme().Colors, field.key)
				a.useCustomTheme(c)
			})
		}
	})
}

// themeEditorPreview is a small sample of the window drawn with the palette
// being edited, so a color can be judged without closing the dialog.
func (a *app) themeEditorPreview(c *ui.Context) {
	t := buildCustomTheme(a.editedTheme().customThemeSetting)
	ui.Column(c).Width(290).FillHeight().Gap(10).Padding(14).
		Background(t.Background).Border(1, t.Border).Radius(frameTheme(c).Radius + 6).Children(func() {
		ui.Text(c, tr("Preview")).Bold().TextColor(t.TextMuted).FontSize(c.Theme().FontSize - 2)
		ui.Column(c).FillWidth().Gap(8).Padding(12).
			Background(t.Surface).Border(1, t.Border).Radius(frameTheme(c).Radius + 4).Children(func() {
			ui.Text(c, tr("Commit message")).Bold().TextColor(t.Text)
			ui.Text(c, tr("Muted text")).TextColor(t.TextMuted).FontSize(c.Theme().FontSize - 1)
			ui.Row(c).Gap(6).Children(func() {
				previewPill(c, tr("Primary"), t.Accent, t.AccentText, t.Accent)
				previewPill(c, tr("Hover"), t.AccentHover, t.AccentText, t.AccentHover)
				previewPill(c, tr("Pressed"), t.AccentPressed, t.AccentText, t.AccentPressed)
			})
		})
		ui.Column(c).FillWidth().Gap(2).Children(func() {
			previewRow(c, t, tr("Selected row"), t.Selection, t.Text)
			previewRow(c, t, tr("Surface hover"), t.SurfaceHover, t.Text)
			previewRow(c, t, tr("Surface pressed"), t.SurfacePressed, t.Text)
			previewRow(c, t, tr("Plain row"), t.Background, t.TextMuted)
		})
		ui.Row(c).Gap(6).Children(func() {
			previewPill(c, "+12", t.Success.Alpha(0.18), t.Success, t.Success)
			previewPill(c, "M", t.Warning.Alpha(0.18), t.Warning, t.Warning)
			previewPill(c, "−4", t.Danger.Alpha(0.18), t.Danger, t.Danger)
		})
	})
}

// frameTheme is the frame's theme, as a short alias for the preview's metrics.
func frameTheme(c *ui.Context) *ui.Theme { return c.Theme() }

func previewPill(c *ui.Context, label string, fill, text, border ui.Color) {
	ui.Box(c).Padding(3, 9).Radius(frameTheme(c).Radius).Background(fill).Border(1, border.Alpha(0.5)).Children(func() {
		ui.Text(c, label).TextColor(text).FontSize(frameTheme(c).FontSize - 1).SingleLine()
	})
}

func previewRow(c *ui.Context, t *ui.Theme, label string, fill, text ui.Color) {
	ui.Box(c).FillWidth().Padding(6, 10).Radius(frameTheme(c).Radius).Background(fill).Children(func() {
		ui.Text(c, label).TextColor(text).SingleLine()
	})
}

func (a *app) themeEditorFooter(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		previous := a.settingsUI.themeEditorPrevious
		if a.settingsUI.themeEditorCreated && previous != "" {
			flatIconButton(c, "Discard and go back", trf("Remove this theme and switch back to %s", previous), backIcon, 17, 8, false, func() {
				a.removeCustomTheme(a.settingsUI.themeEditorIndex, previous)
				a.settingsChanged(c)
				a.closeThemeEditor()
			})
		}
		ui.Text(c, tr("Your custom themes are remembered the next time you open the app")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		if ui.PrimaryButton(c, tr("Done")).Clicked() {
			a.closeThemeEditor()
		}
	})
}
