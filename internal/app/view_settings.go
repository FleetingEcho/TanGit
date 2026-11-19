package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// settingsChanged stores a change and asks for the frame that shows it with
// the new theme or metrics.
func (a *app) settingsChanged(c *ui.Context) {
	a.saveSettings()
	currentLanguage = normalizeLanguage(a.settings.Language)
	refreshTrayMenu()
	c.Invalidate()
}

const (
	settingsPageAppearance = iota
	settingsPageTypography
	settingsPageBehavior
	settingsPageEditor
	settingsPageWorkspace
	settingsPageCommands
	settingsPageUpdates
	settingsPageSSHProfiles
	settingsPageCodeEditor
	settingsPageLSP
	settingsPageCache
)

func (a *app) openSettings() {
	a.loadGoLanguageServerDraft()
	a.settingsUI.conflictActionStyleChoice = conflictActionStyleChoice(a.settings.ConflictActionStyle)
	a.settingsUI.autoFetchChoice = a.settings.AutoFetchInterval
	if backgroundFetchCustomInterval(a.settingsUI.autoFetchChoice) {
		a.settingsUI.autoFetchChoice = backgroundFetchCustomChoice
	}
	a.settingsUI.open = true
}

// openSettingsSection opens settings on a specific page, used by the
// commands menu's way into the custom command editor.
func (a *app) openSettingsSection(page int) {
	a.loadGoLanguageServerDraft()
	a.settingsUI.conflictActionStyleChoice = conflictActionStyleChoice(a.settings.ConflictActionStyle)
	a.settingsUI.autoFetchChoice = a.settings.AutoFetchInterval
	if backgroundFetchCustomInterval(a.settingsUI.autoFetchChoice) {
		a.settingsUI.autoFetchChoice = backgroundFetchCustomChoice
	}
	if page != a.settingsUI.page {
		a.stopEditorShortcutRecording()
	}
	a.settingsUI.page = page
	a.settingsUI.revealShortcuts = page == settingsPageCodeEditor
	a.settingsUI.open = true
}

// themeButton is the header's theme control. Its icon shows the appearance in
// use — a sun on a light theme, a moon on a dark one — and a click opens the
// list of themes to choose from, with the current one marked.
func (a *app) themeButton(c *ui.Context) {
	dark := c.Theme().Dark
	icon := sunIcon
	if dark {
		icon = moonIcon
	}
	button := compactIconButton(ui.Button(c, ""))
	button.Label(tr("Theme")).
		Tooltip(trf("Choose a theme (currently %s)", a.settings.Theme))
	button.Children(func() {
		ui.Icon(c, icon).FontSize(14)
	})
	button.PopoverMenuButton(func(m *ui.Menu) {
		a.themeMenuItems(m, c)
	})
}

// themeMenuItems writes the theme catalog into a menu, marking the chosen
// theme, and applies a pick. The picker's theme button and the workspace
// header's share it, so both list the same themes.
func (a *app) themeMenuItems(m *ui.Menu, c *ui.Context) {
	for _, name := range themeNames {
		if m.Item(name).Radio(a.settings.Theme == name).Chosen() {
			a.settings.Theme = name
			a.saveSettings()
			c.Invalidate()
		}
	}
}

func (a *app) closeSettings() {
	a.stopEditorShortcutRecording()
	a.settingsUI.close()
	a.closeSSHProfileForm()
	a.saveSettings()
}

// settingsModal is the preferences dialog: it sits over whatever page is
// showing, applies every change to the next frame, and stores it at once.
func (a *app) settingsModal(c *ui.Context) {
	if !a.settingsUI.open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(960).Height(660).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(18).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius+10).
				Shadow(0, 24, 64, 0, ui.RGBA(0, 0, 0, 0.3)).Clip().
				Role(ui.RoleDialog).Label(tr("Settings")).
				Children(func() {
					a.settingsHeader(c)
					a.settingsBody(c)
					ui.Divider(c)
					a.settingsFooter(c)
				})
			// Help belongs to the workspace underneath Settings. Opening it here
			// closes Settings first so two modal overlays never compete for input.
			if a.confirm == nil && !a.settings.DisableShortcuts && !c.FocusTakesText() && a.code.shortcutRecording == "" {
				if a.onOverlayShortcut(back, ui.Shift, ui.KeySlash, shortcut("settings/modal-current-shortcuts", "?", tr("Show shortcuts for the current page")), nil) {
					a.closeSettings()
					a.openCurrentKeybindings()
				}
				if a.onOverlayShortcut(back, 0, ui.KeyF1, shortcut("settings/modal-all-shortcuts", "F1", tr("Open the complete shortcut list")), nil) {
					a.closeSettings()
					a.openKeybindings()
				}
			}
			// Confirmation and host-key prompts are child overlays. While either
			// is present, Settings must not interpret its click or Escape as an
			// outside dismissal of the parent dialog as well.
			if a.confirm == nil && a.ssh.hostKeyPrompt == nil && (panel.PressedOutside() || a.onOverlayShortcut(back, 0, ui.KeyEscape, shortcut("settings/modal-close", "Escape", tr("Close settings")), nil)) {
				if a.code.shortcutRecording != "" {
					a.stopEditorShortcutRecording()
				} else {
					a.closeSettings()
				}
			}
		})
	})
}

func (a *app) settingsHeader(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
		ui.Box(c).Size(38, 38).Radius(theme.Radius + 4).Center().
			Background(theme.Accent.Alpha(0.12)).Children(func() {
			ui.Icon(c, settingsIcon).FontSize(19).TextColor(theme.Accent)
		})
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, tr("Settings")).FontSize(21).Bold()
			ui.Text(c, tr("Make TanGit look and work the way you do")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
		iconButton(c, "Close settings", "Escape", closeIcon, false, a.closeSettings)
	})
}

// settingsGroup titles a block of related settings.
func settingsGroup(c *ui.Context, icon *ui.SVG, title, hint string) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(0, 0, 5).Children(func() {
		ui.Box(c).Size(30, 30).Radius(theme.Radius + 2).Center().
			Background(theme.Accent.Alpha(0.1)).Children(func() {
			ui.Icon(c, icon).FontSize(15).TextColor(theme.Accent)
		})
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, tr(title)).FontSize(17).Bold()
			if hint != "" {
				ui.Text(c, tr(hint)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		})
	})
}

// settingsSwitchControl renders a switch and its label without a wrapping
// row, so related preferences can also share a row without changing layout.
func settingsSwitchControl(c *ui.Context, label, tooltip string, value *bool) bool {
	toggle := ui.Switch(c, value).Label(tr(label))
	if tooltip == "" {
		tooltip = label
	}
	toggle.Tooltip(tr(tooltip))
	ui.Text(c, tr(label)).FontSize(c.Theme().FontSize - 1).SingleLine()
	return toggle.Changed()
}

// settingsSwitch keeps every boolean preference in the same compact form:
// the switch itself first, then its readable label. Returning Changed lets
// callers preserve their existing persistence or confirmation behavior.
func settingsSwitch(c *ui.Context, label, tooltip string, value *bool) bool {
	changed := false
	ui.Row(c).AlignItems(ui.Center).Gap(12).Wrap().Children(func() {
		changed = settingsSwitchControl(c, label, tooltip, value)
	})
	return changed
}

// settingRow is one labelled control, with its label and hint on the left and
// the control on the right.
func settingRow(c *ui.Context, label, hint string, control func()) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(16).MinHeight(48).Children(func() {
		ui.Column(c).Gap(3).Width(154).Children(func() {
			ui.Text(c, tr(label)).Bold()
			if hint != "" {
				ui.Text(c, tr(hint)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
		})
		ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(8).Wrap().Children(control)
	})
}

// settingsBody is a small master/detail dialog: one stable category rail and
// one focused page. It replaces the old all-sections scroll, where controls
// became hard to scan and the footer disappeared below a long form.
func (a *app) settingsBody(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).Grow(1).MinHeight(0).FillWidth().Gap(18).Children(func() {
		ui.Column(c).Width(158).Gap(4).Padding(7).
			Background(theme.Surface.Alpha(0.55)).Radius(theme.Radius + 6).
			Children(func() {
				ui.Text(c, tr("PREFERENCES")).FontSize(theme.FontSize-3).Bold().
					TextColor(theme.TextMuted).Padding(4, 8, 9)
				a.settingsPageButton(c, settingsPageAppearance, themeIcon, "Appearance")
				a.settingsPageButton(c, settingsPageTypography, fontIcon, "Typography")
				a.settingsPageButton(c, settingsPageBehavior, commitIcon, "Behavior")
				a.settingsPageButton(c, settingsPageEditor, fileCodeIcon, "Editor")
				a.settingsPageButton(c, settingsPageLSP, terminalIcon, "LSP")
				a.settingsPageButton(c, settingsPageWorkspace, worktreesIcon, "Workspace")
				a.settingsPageButton(c, settingsPageCache, trashIcon, "Cache")
				a.settingsPageButton(c, settingsPageCommands, commandIcon, "Commands")
				a.settingsPageButton(c, settingsPageSSHProfiles, remotesIcon, "SSH profiles")
				a.settingsPageButton(c, settingsPageUpdates, updateIcon, "Updates")
			})
		ui.Box(c).Width(1).Background(theme.Border)
		ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().FillHeight().Children(func() {
			ui.Column(c).FillWidth().Padding(3, 8, 12, 4).Children(func() {
				switch a.settingsUI.page {
				case settingsPageTypography:
					a.typographySettings(c)
				case settingsPageBehavior:
					a.behaviorSettings(c)
				case settingsPageEditor, settingsPageCodeEditor:
					a.codeEditorSettings(c)
				case settingsPageLSP:
					settingsSection(c, func() { a.languageServerCatalog(c) })
				case settingsPageWorkspace:
					a.workspaceSettings(c)
				case settingsPageCommands:
					a.commandsSettings(c)
				case settingsPageSSHProfiles:
					a.sshProfileSettings(c)
				case settingsPageUpdates:
					settingsSection(c, func() { a.updatesSettings(c) })
				case settingsPageCache:
					settingsSection(c, func() { a.cacheSettings(c) })
				default:
					a.appearanceSettings(c)
				}
			})
		})
	})
}

func (a *app) settingsPageButton(c *ui.Context, page int, icon *ui.SVG, label string) {
	theme := c.Theme()
	selected := a.settingsUI.page == page || page == settingsPageEditor && a.settingsUI.page == settingsPageCodeEditor
	button := ui.ButtonBase(c).FillWidth().Height(40).Padding(9, 10).Gap(10).
		Radius(theme.Radius + 2).Label(trf("%s settings", tr(label)))
	button.Children(func() {
		ui.Icon(c, icon).FontSize(16)
		ui.Text(c, tr(label)).Grow(1).SingleLine()
		if selected {
			ui.Box(c).Size(5, 5).Radius(3).Background(theme.Accent)
		}
	})
	switch {
	case selected:
		button.Background(theme.Accent.Alpha(0.13)).TextColor(theme.Accent)
	case button.Hovered():
		button.Background(theme.SurfaceHover)
	}
	if button.Clicked() {
		if a.settingsUI.page != page {
			a.stopEditorShortcutRecording()
		}
		a.settingsUI.revealShortcuts = false
		a.settingsUI.page = page
	}
}

func (a *app) cacheSettings(c *ui.Context) {
	a.ensureCacheUsage()
	theme := c.Theme()
	totalDescription := "Disk space used by Zoekt code-search indexes and downloaded commit-author avatars."
	indexDescription := "Indexes built and queried by Zoekt. They speed up full-text Code Search across local repository files without rescanning every file for every query."
	avatarDescription := "Commit-author images shown next to commits. Caching avoids repeated downloads from Git hosting services."
	settingsSection(c, func() {
		settingsGroup(c, trashIcon, "Cache", "Storage used by Zoekt code-search indexes and downloaded author avatars")
		switch {
		case a.cacheCleanupBusy:
			ui.Text(c, tr("Clearing cache…")).TextColor(theme.TextMuted)
		case !a.cacheUsage.loaded:
			ui.Text(c, tr("Measuring cache…")).TextColor(theme.TextMuted)
		default:
			cacheUsageRow := func(label, description string, size int64) {
				ui.Row(c).AlignItems(ui.Center).Gap(8).MinHeight(36).Children(func() {
					ui.Text(c, tr(label)).Bold()
					cacheInfoBadge(c, label, description)
					ui.Box(c).Grow(1)
					ui.Text(c, formatRemoteTransferBytes(size)).Bold().TextColor(theme.Accent).
						Tooltip(tr(description))
				})
			}
			cacheUsageRow("Total cache", totalDescription, a.cacheUsage.totalBytes())
			cacheUsageRow("Zoekt search indexes", indexDescription, a.cacheUsage.indexBytes)
			cacheUsageRow("Cached avatars", avatarDescription, a.cacheUsage.avatarBytes)
		}
		ui.Divider(c)
		settingRow(c, "Cache", "", func() {
			iconButton(c, "Refresh cache size", "Remeasure the disk space used by Zoekt indexes and cached author avatars", refreshIcon,
				a.cacheUsage.loading || a.cacheCleanupBusy, a.refreshCacheUsage)
			iconTextButtonState(c, "Clear cache", "Delete Zoekt code-search indexes and cached author avatars. They are recreated when needed.", trashIcon,
				false, a.cacheCleanupBusy, a.openCacheCleaner)
		})
		ui.Text(c, tr("Only Zoekt indexes and downloaded avatars are included. Unsaved editor recovery and Git safety journals are never cleared.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	})
}

func cacheInfoBadge(c *ui.Context, label, description string) {
	theme := c.Theme()
	ui.Box(c).Size(20, 20).Radius(10).Center().Background(theme.Accent.Alpha(0.08)).
		Role(ui.RoleImage).Label(trf("%s information", tr(label))).Tooltip(tr(description)).Children(func() {
		ui.Icon(c, infoIcon).FontSize(13).TextColor(theme.Accent)
	})
}

func (a *app) appearanceSettings(c *ui.Context) {
	settingsSection(c, func() {
		settingsGroup(c, themeIcon, "Appearance", "Color, language, and your Home screen")
		settingRow(c, "Language", "Language of the interface", func() {
			a.languageSelect(c)
		})
		settingRow(c, "Theme", "Palettes for the whole window", func() {
			selects := ui.Select(c, &a.settings.Theme, themeNames).Grow(1)
			if selects.Changed() {
				a.settingsChanged(c)
			}
		})
		a.themePreview(c)
		settingRow(c, "Accent color", "Buttons and highlights", func() {
			a.accentPicker(c)
		})
		ui.Divider(c)
		settingRow(c, "Home background", "An image behind the repository picker", func() {
			enabled := !a.settings.HomeBackgroundDisabled
			if settingsSwitch(c, "Enable Home background", "Show or hide the chosen image without removing it", &enabled) {
				a.settings.HomeBackgroundDisabled = !enabled
				a.settingsChanged(c)
			}
			iconTextButton(c, "Choose image…", "Choose a PNG, JPEG, GIF, WebP, or BMP image, up to 10 MiB",
				fileImageIcon, false, a.browseHomeBackground)
			if a.settings.HomeBackgroundPath != "" {
				iconTextButton(c, "Remove", "Remove the Home background", trashIcon, false,
					a.removeHomeBackground)
				a.homeBackgroundPreview(c)
			}
		})
		if a.settings.HomeBackgroundPath != "" {
			settingRow(c, "Background opacity", "How strongly the Home image shows", func() {
				ui.Text(c, fmt.Sprintf("%.0f%%", a.settings.HomeBackgroundOpacity*100)).
					TextColor(c.Theme().TextMuted).Width(42)
				slider := ui.Slider(c, &a.settings.HomeBackgroundOpacity, 0.05, 1).
					Step(0.01).Width(180).Label(tr("Home image opacity")).
					Tooltip(tr("Adjust the Home image's opacity"))
				if slider.Changed() {
					a.settingsChanged(c)
				}
			})
		}
	})
}

// homeBackgroundPreview reuses Home's decoded bitmap rather than reading the
// selected file again while the dialog repaints. Its compact size leaves the
// whole Appearance page visible; Wrap on the setting row handles narrow windows.
func (a *app) homeBackgroundPreview(c *ui.Context) {
	theme := c.Theme()
	if a.settingsUI.homeBackground != nil && a.settingsUI.homeBackgroundPath == a.settings.HomeBackgroundPath {
		ui.Image(c, a.settingsUI.homeBackground).Size(138, 62).Fit(ui.Cover).
			Radius(theme.Radius + 2).Role(ui.RoleImage).Label(tr("Home background preview")).
			Tooltip(a.settings.HomeBackgroundPath)
		return
	}
	ui.Row(c).Size(138, 62).Center().Gap(6).
		Background(theme.Surface.Alpha(0.6)).Border(1, theme.Border).
		BorderStyle(ui.BorderDashed).Radius(theme.Radius + 2).Children(func() {
		ui.Icon(c, fileImageIcon).FontSize(16).TextColor(theme.TextMuted)
		ui.Text(c, tr("Preparing preview…")).FontSize(theme.FontSize - 2).
			TextColor(theme.TextMuted)
	})
}

// typographyGroup keeps one typography domain inside its own card. The small
// inner indent makes the controls belong to the heading without flattening
// interface, code, and shared layout into one long form.
func typographyGroup(c *ui.Context, title, hint string, content func()) {
	theme := c.Theme()
	ui.Column(c).Gap(12).Padding(14).Background(theme.Surface.Alpha(0.55)).
		Border(1, theme.Border).Radius(theme.Radius + 5).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, tr(title)).Bold().TextColor(theme.Accent)
			ui.Text(c, tr(hint)).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
		})
		ui.Column(c).Gap(12).Padding(0, 4).Children(content)
	})
}

func (a *app) typographySettings(c *ui.Context) {
	settingsSection(c, func() {
		settingsGroup(c, fontIcon, "Typography and layout", "Comfortable text and spacing, everywhere")
		typographyGroup(c, "Interface", "Menus, labels, and controls", func() {
			settingRow(c, "Interface font size", "Text size in controls and panes", func() {
				input := ui.NumberInput(c, &a.settings.FontSize, 9, 26, 1).Label(trf("%s settings", tr("Interface font size")))
				if input.Changed() {
					a.settingsChanged(c)
				}
			})
			settingRow(c, "Interface font", "System is the desktop's font", func() {
				selects := ui.Combobox(c, &a.settings.Font, fontChoices(a.settings.Font)).Grow(1).
					Label(tr("Search interface fonts"))
				if selects.Changed() {
					a.settingsChanged(c)
				}
			})
			settingRow(c, "Interface spacing", "Padding inside and between controls", func() {
				input := ui.NumberInput(c, &a.settings.Spacing, 2, 10, 1).Label(trf("%s settings", tr("Interface spacing")))
				if input.Changed() {
					a.settingsChanged(c)
				}
			})
		})
		typographyGroup(c, "Code buffers", "Diffs, source files, conflicts, and blame", func() {
			settingRow(c, "Buffer font size", "Text size in diffs and code buffers", func() {
				input := ui.NumberInput(c, &a.settings.BufferFontSize, 9, 26, 1).Label(trf("%s settings", tr("Buffer font size")))
				if input.Changed() {
					a.settingsChanged(c)
				}
			})
			settingRow(c, "Buffer font", "Diffs and code buffers; System is monospaced", func() {
				selects := ui.Combobox(c, &a.settings.BufferFont, fontChoices(a.settings.BufferFont)).Grow(1).
					Label(tr("Search buffer fonts"))
				if selects.Changed() {
					a.settingsChanged(c)
				}
			})
			settingRow(c, "Buffer line spacing", "Line height as a multiple of font size", func() {
				input := ui.NumberInput(c, &a.settings.BufferLineHeight, 1, 2.5, 0.1).Label(trf("%s settings", tr("Buffer line spacing")))
				if input.Changed() {
					a.settingsChanged(c)
				}
			})
		})
		typographyGroup(c, "Layout", "Button and panel rounding", func() {
			settingRow(c, "Corner radius", "Button and panel rounding", func() {
				input := ui.NumberInput(c, &a.settings.Radius, 0, 20, 1).Label(trf("%s settings", tr("Corner radius")))
				if input.Changed() {
					a.settingsChanged(c)
				}
			})
		})
	})
}

var backgroundFetchIntervals = []int{1, 5, 15, 30, 60}

const backgroundFetchCustomChoice = -1

func backgroundFetchCustomInterval(minutes int) bool {
	if minutes <= 0 {
		return false
	}
	for _, preset := range backgroundFetchIntervals {
		if minutes == preset {
			return false
		}
	}
	return true
}

func (a *app) behaviorSettings(c *ui.Context) {
	settingsSection(c, func() {
		settingsGroup(c, commitIcon, "Behavior", "Diff reading, files, and keyboard controls")
		ui.Column(c).Gap(12).Padding(15).Background(c.Theme().Surface.Alpha(0.55)).
			Border(1, c.Theme().Border).Radius(c.Theme().Radius + 5).Children(func() {
			settingRow(c, "Conflict and hunk buttons", "Choose icons, icons with labels, or guided conflict resolution", func() {
				choice := ui.Segmented(c, &a.settingsUI.conflictActionStyleChoice, tr("Icons"), tr("Icons + labels"), tr("Guided")).
					Label(tr("Conflict and hunk button style")).
					Tooltip(tr("Show conflict and Changes hunk actions as icons, icons with labels, or labeled actions with step-by-step guidance"))
				if choice.Changed() {
					a.settings.ConflictActionStyle = conflictActionStyleForChoice(a.settingsUI.conflictActionStyleChoice)
					a.settingsChanged(c)
				}
			})
			ui.Divider(c)
			rows := settingsSwitch(c, "Show diffs side by side", "", &a.settings.DiffSplit)
			if rows {
				a.applySettingsChange()
				c.Invalidate()
			}
			wrap := settingsSwitch(c, "Wrap long diff lines", "", &a.settings.DiffWrap)
			if wrap {
				a.applySettingsChange()
				c.Invalidate()
			}
			untracked := settingsSwitch(c, "Ignore untracked files", "", &a.settings.IgnoreUntracked)
			if untracked {
				a.applySettingsChange()
				c.Invalidate()
			}
			a.hotkeysSwitch(c)
			ui.Divider(c)
			ui.Text(c, tr("Background fetch")).Bold()
			ui.Text(c, tr("Fetches every remote while the window is open so branches, ahead/behind counts and tags stay current. Your working tree is not touched.")).
				TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1).MaxWidth(480)
			fetchOn := a.settings.AutoFetchInterval > 0
			if settingsSwitch(c, "Fetch remotes in the background", "", &fetchOn) {
				if fetchOn {
					a.settings.AutoFetchInterval = 5
					a.settingsUI.autoFetchChoice = 5
				} else {
					a.settings.AutoFetchInterval = 0
				}
				a.settings.AutoFetchConfigured = true
				a.applySettingsChange()
				c.Invalidate()
			}
			if fetchOn {
				a.backgroundFetchIntervalChoice(c)
			}
			if settingsSwitch(c, "Prune stale remote-tracking branches on fetch", "", &a.settings.FetchPrune) {
				a.applySettingsChange()
				c.Invalidate()
			}
		})
		ui.Divider(c)
		a.continuePreflightSettings(c)
	})
}

// backgroundFetchIntervalChoice replaces a column of preset buttons with one
// selector. Custom keeps the persisted interval in minutes and exposes a
// bounded numeric editor, so a user's existing non-preset value round-trips.
func (a *app) backgroundFetchIntervalChoice(c *ui.Context) {
	theme := c.Theme()
	selects := ui.SelectBase(c, &a.settingsUI.autoFetchChoice)
	selects.Trigger.Width(170).Padding(6, 10).Gap(8).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
		Label(tr("Background fetch interval")).
		Tooltip(tr("Choose a preset or enter a custom interval in minutes")).
		Children(func() {
			label := tr("Custom…")
			if a.settingsUI.autoFetchChoice != backgroundFetchCustomChoice {
				label = trf("%d min", a.settings.AutoFetchInterval)
			}
			ui.Text(c, label).Grow(1).SingleLine()
		})
	selects.Popup(func(panel ui.Element) {
		panel.Padding(4).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius)
		for _, minutes := range backgroundFetchIntervals {
			label := trf("%d min", minutes)
			item := selects.Item(minutes).Padding(6, 10).
				Label(tr("Background fetch interval") + ": " + label)
			if item.Highlighted() {
				item.Background(theme.SurfaceHover)
			}
			item.Children(func() { ui.Text(c, label) })
		}
		custom := selects.Item(backgroundFetchCustomChoice).Padding(6, 10).
			Label(tr("Custom fetch interval"))
		if custom.Highlighted() {
			custom.Background(theme.SurfaceHover)
		}
		custom.Children(func() { ui.Text(c, tr("Custom…")) })
	})
	if selects.Trigger.Changed() {
		if a.settingsUI.autoFetchChoice != backgroundFetchCustomChoice {
			a.settings.AutoFetchInterval = a.settingsUI.autoFetchChoice
		}
		a.settings.AutoFetchConfigured = true
		a.settingsChanged(c)
	}

	ui.Row(c).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
		// The trigger and menu were built above so their change is applied
		// before deciding whether the custom field belongs in this frame.
		if a.settingsUI.autoFetchChoice == backgroundFetchCustomChoice {
			minutes := float64(a.settings.AutoFetchInterval)
			input := ui.NumberInput(c, &minutes, 1, 24*60, 1).Width(140).
				Label(tr("Custom fetch interval in minutes"))
			if input.Changed() {
				a.settings.AutoFetchInterval = int(minutes)
				a.settings.AutoFetchConfigured = true
				a.settingsChanged(c)
			}
			ui.Text(c, tr("min")).TextColor(theme.TextMuted)
		}
	})
}

func (a *app) openCustomEditorForm() {
	a.settingsUI.customEditorFormOpen = true
	a.settingsUI.customEditorFormName = ""
	a.settingsUI.customEditorFormCommand = ""
	a.settingsUI.customEditorFormError = ""
}

func (a *app) closeCustomEditorForm() {
	a.settingsUI.closeCustomEditorForm()
}

func (a *app) submitCustomEditorForm(c *ui.Context) {
	editor := editorPreset{
		Name:    strings.TrimSpace(a.settingsUI.customEditorFormName),
		Command: strings.TrimSpace(a.settingsUI.customEditorFormCommand),
	}
	if editor.Name == "" || editor.Command == "" {
		a.settingsUI.customEditorFormError = tr("Enter an editor name and command line.")
		return
	}
	for _, preset := range a.allEditorPresets() {
		if strings.EqualFold(editor.Name, preset.Name) {
			a.settingsUI.customEditorFormError = tr("Enter a unique editor name.")
			return
		}
	}
	if !validExternalEditor(editor) {
		a.settingsUI.customEditorFormError = tr("Enter a valid single-line command.")
		return
	}
	a.settings.ExternalEditors = append(a.settings.ExternalEditors, editor)
	a.closeCustomEditorForm()
	a.settingsChanged(c)
}

func (a *app) customEditorForm(c *ui.Context) {
	if !a.settingsUI.customEditorFormOpen {
		return
	}
	theme := c.Theme()
	ui.Column(c).Gap(12).Padding(15).Background(theme.Surface.Alpha(0.55)).
		Border(1, theme.Border).Radius(theme.Radius + 5).Children(func() {
		ui.Text(c, tr("New external editor")).Bold()
		settingRow(c, "Editor name", "Shown in the external-editor menu", func() {
			ui.TextInput(c, &a.settingsUI.customEditorFormName).Label(tr("Custom editor name")).
				Placeholder(tr("Editor name")).Grow(1).AutoFocus()
		})
		settingRow(c, "Command line", "Command and arguments", func() {
			input := ui.TextInput(c, &a.settingsUI.customEditorFormCommand).Label(tr("Custom editor command line")).
				Placeholder("my-editor --wait {{filename}}").Grow(1)
			if input.Submitted() {
				a.submitCustomEditorForm(c)
			}
		})
		if a.settingsUI.customEditorFormError != "" {
			ui.Text(c, a.settingsUI.customEditorFormError).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
		} else {
			ui.Text(c, tr("Use {{filename}} for the repository path and {{line}} for a line number.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		}
		ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
			if ui.Button(c, tr("Cancel")).Clicked() {
				a.closeCustomEditorForm()
			}
			if ui.PrimaryButton(c, tr("Add editor")).Clicked() {
				a.submitCustomEditorForm(c)
			}
		})
	})
}

func (a *app) editorSettings(c *ui.Context) {
	settingsSection(c, func() {
		settingsGroup(c, fileCodeIcon, "Editor", "Built-in Code or an external program")
		builtIn := effectiveEditorCommand(a.settings.EditorCommand) == builtInEditorCommand
		if !builtIn {
			settingRow(c, "Editor command", "Fills in {{filename}} and {{line}}", func() {
				input := ui.TextInput(c, &a.settings.EditorCommand).
					Label(tr("Editor command")).
					Placeholder(vsCodeEditorCommand).Grow(1)
				if input.Changed() {
					a.settingsChanged(c)
				}
			})
		}
		settingRow(c, "Presets", "Choose the editor buttons' destination", func() {
			for _, preset := range a.allEditorPresets() {
				chosen := effectiveEditorCommand(a.settings.EditorCommand) == preset.Command
				var button ui.Element
				if chosen {
					button = ui.PrimaryButton(c, tr(preset.Name))
				} else {
					button = ui.Button(c, tr(preset.Name))
				}
				if button.Tooltip(preset.Command).Clicked() {
					a.settings.EditorCommand = preset.Command
					a.settingsChanged(c)
				}
			}
			flatIconButton(c, "Add editor…", "Add a custom external editor", plusIcon, 14, 4,
				a.settingsUI.customEditorFormOpen, a.openCustomEditorForm)
		})
		a.customEditorForm(c)
		explanation := "Repository cards and file actions use this command without changing your open repository."
		if builtIn {
			explanation = "Editor buttons open the Code page; file actions also open and reveal the selected file."
		}
		ui.Text(c, tr(explanation)).TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1).MaxWidth(480)
		ui.Divider(c)
		a.searchExclusionSettings(c)
	})
}

func (a *app) codeEditorSettings(c *ui.Context) {
	a.ensureBuiltInEditorPreferences()
	settingsSection(c, func() {
		a.editorSettings(c)
		ui.Divider(c)
		a.builtInEditorPreferenceSettings(c)
		ui.Divider(c)
		a.editorShortcutSettings(c)
	})
}

func (a *app) builtInEditorPreferenceSettings(c *ui.Context) {
	ui.Text(c, tr("Built-in editor preferences")).Bold().FontSize(c.Theme().FontSize + 1)
	settingRow(c, "Line numbers", "Absolute, relative, or hidden", func() {
		choice := ui.Combobox(c, &a.settings.EditorLineNumbers, []string{"on", "relative", "off"}).Label(tr("Line numbers"))
		if choice.Changed() {
			a.settingsChanged(c)
		}
	})
	settingRow(c, "Whitespace", "Choose which spaces and tabs are visible", func() {
		choice := ui.Combobox(c, &a.settings.EditorWhitespace, []string{"selection", "none", "all", "trailing", "boundary"}).Label(tr("Whitespace"))
		if choice.Changed() {
			a.settingsChanged(c)
		}
	})
	for _, item := range []struct {
		label string
		value *bool
	}{
		{"Wrap long lines", &a.settings.EditorWordWrap},
		{"Auto-close brackets and quotes", &a.settings.EditorAutoClose},
		{"Inlay hints", &a.settings.EditorInlayHints},
		{"Semantic highlighting", &a.settings.EditorSemanticHighlighting},
		{"Minimap", &a.settings.EditorMinimap},
		{"Sticky symbol header", &a.settings.EditorStickyScroll},
	} {
		if settingsSwitch(c, item.label, "", item.value) {
			a.settingsChanged(c)
		}
	}
	a.zoektSearchSettings(c)
}

// zoektSearchSettings is the per-repository opt-out for indexed search.
// It shares the Settings switch layout; only disabling asks for confirmation
// because that action also deletes this repository's index cache.
func (a *app) zoektSearchSettings(c *ui.Context) {
	if a.currentZoektRepositoryRoot() == "" {
		settingRow(c, "Code search", tr("Open a repository to choose whether it uses Zoekt."), func() {})
		return
	}
	enabled := a.zoektCodeSearchEnabled()
	if settingsSwitch(c, "Use Zoekt code search",
		"Zoekt builds a local full-text index of repository files to speed up Code Search. Disable it to use ripgrep or the built-in search and save disk space.", &enabled) {
		if enabled {
			a.enableZoekt(c)
		} else {
			a.confirmDisableZoekt(c)
		}
	}
}

// searchExclusionSettings edits one durable list shared by content search.
// Entries are folder names rather than globs, which makes their behavior
// consistent at every repository depth and across both backends.
func (a *app) searchExclusionSettings(c *ui.Context) {
	theme := c.Theme()
	ui.Text(c, tr("Search exclusions")).Bold().FontSize(theme.FontSize + 1)
	ui.Text(c, tr("Folder names skipped by content search")).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(480)
	settingRow(c, "Excluded folder name", "Match this folder at any depth", func() {
		input := ui.TextInput(c, &a.settingsUI.searchExcludedDirectoryDraft).
			Label(tr("New excluded folder")).Placeholder("node_modules").Grow(1)
		submitted := input.Submitted()
		added := false
		iconTextButtonState(c, "Add", "Add this folder to the search exclusions", plusIcon, false,
			len(normalizeWorkspaceSearchExcludedDirectories([]string{a.settingsUI.searchExcludedDirectoryDraft})) == 0,
			func() { added = true })
		if !submitted && !added {
			return
		}
		name := normalizeWorkspaceSearchExcludedDirectories([]string{a.settingsUI.searchExcludedDirectoryDraft})
		if len(name) == 0 {
			return
		}
		a.settings.SearchExcludedDirectories = normalizeWorkspaceSearchExcludedDirectories(
			append(a.settings.SearchExcludedDirectories, name[0]))
		a.settingsUI.searchExcludedDirectoryDraft = ""
		a.settingsChanged(c)
	})
	settingsChoiceCard(c, "Excluded folders", "Applied before searching", func() {
		ui.Row(c).Gap(8).Wrap().Children(func() {
			if len(a.settings.SearchExcludedDirectories) == 0 {
				ui.Text(c, tr("No excluded folders")).TextColor(theme.TextMuted)
				return
			}
			for _, directory := range a.settings.SearchExcludedDirectories {
				c.Key("search-exclusion:" + directory)
				button := ui.ButtonBase(c).Height(28).Padding(3, 8).Gap(6).Radius(theme.Radius).
					Background(theme.Surface).Border(1, theme.Border).
					Label(trf("Remove excluded folder %s", directory)).
					Tooltip(trf("Stop excluding %s from content search", directory))
				button.Children(func() {
					ui.Text(c, directory).SingleLine()
					ui.Icon(c, closeIcon).FontSize(12).TextColor(theme.TextMuted)
				})
				if button.Hovered() {
					button.Background(theme.SurfaceHover)
				}
				if button.Clicked() {
					kept := make([]string, 0, len(a.settings.SearchExcludedDirectories)-1)
					for _, candidate := range a.settings.SearchExcludedDirectories {
						if candidate != directory {
							kept = append(kept, candidate)
						}
					}
					a.settings.SearchExcludedDirectories = kept
					a.settingsChanged(c)
				}
			}
		})
	})
	ui.Row(c).Gap(8).Wrap().Children(func() {
		if len(a.settings.SearchExcludedDirectories) > 0 {
			iconTextButton(c, "Clear list", "Allow content search in every folder", closeIcon, false, func() {
				a.settings.SearchExcludedDirectories = []string{}
				a.settingsChanged(c)
			})
		}
		iconTextButton(c, "Restore defaults", "Restore the common generated and dependency folder exclusions", refreshIcon, false, func() {
			a.settings.SearchExcludedDirectories = defaultWorkspaceSearchExcludedDirectories()
			a.settingsChanged(c)
		})
	})
}

// settingsChoiceChip is the common enabled/disabled button used by Workspace
// selectors. A checkbox role preserves its accessible state without drawing a
// second, visually unrelated checkbox control beside the label.
func settingsChoiceChip(c *ui.Context, label, tooltip string, shown bool) ui.Element {
	theme := c.Theme()
	chip := ui.ButtonBase(c).Height(28).Padding(3, 8).Gap(6).Radius(theme.Radius).
		Role(ui.RoleCheckBox).Checked(shown).
		Background(theme.Surface).Border(1, theme.Border).TextColor(theme.TextMuted).
		Label(label).Tooltip(tooltip)
	if shown {
		chip.Background(theme.Accent.Alpha(0.16)).Border(1, theme.Accent.Alpha(0.45)).TextColor(theme.Accent)
	}
	if chip.Hovered() {
		if shown {
			chip.Background(theme.Accent.Alpha(0.24))
		} else {
			chip.Background(theme.SurfaceHover)
		}
	}
	return chip
}

// settingsChoiceCard gives each related group of choices its own visual
// boundary and enough room to scan its heading independently.
func settingsChoiceCard(c *ui.Context, title, hint string, build func()) {
	theme := c.Theme()
	ui.Column(c).FillWidth().Gap(12).Padding(14).
		Background(theme.Surface.Alpha(0.55)).Border(1, theme.Border).Radius(theme.Radius + 4).
		Children(func() {
			ui.Column(c).Gap(3).Children(func() {
				ui.Text(c, tr(title)).Bold()
				ui.Text(c, tr(hint)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			})
			build()
		})
}

// toolbarActionChoices edits one toolbar without requiring a visible action.
// Every button defaults on, including when older settings have no saved list.
func (a *app) toolbarActionChoices(c *ui.Context, actions []toolbarAction, hidden []string, labelFormat string, update func([]string)) {
	ui.Row(c).Gap(10).Wrap().Children(func() {
		for _, action := range actions {
			action := action
			visible := toolbarActionVisible(hidden, action.id)
			c.Key("toolbar-action:" + action.id)
			chip := settingsChoiceChip(c, trf(labelFormat, tr(action.label)), trf(labelFormat, tr(action.label)), visible)
			chip.Children(func() {
				ui.Icon(c, toolbarActionIcon(action.id)).FontSize(14)
				ui.Text(c, tr(action.label)).SingleLine()
			})
			chip.OnClick(func() {
				update(setToolbarActionVisible(hidden, actions, action.id, !visible))
				a.settingsChanged(c)
			})
		}
	})
}

func toolbarActionIcon(id string) *ui.SVG {
	switch id {
	case "fetch":
		return fetchIcon
	case "pull":
		return pullIcon
	case "push":
		return pushIcon
	case "refresh":
		return refreshIcon
	case "editor":
		return fileCodeIcon
	case "theme":
		return themeIcon
	case "settings":
		return settingsIcon
	case "commands":
		return commandIcon
	case "pull-requests":
		return externalLinkIcon
	case "activity":
		return historyIcon
	case "terminal":
		return terminalIcon
	case "keyboard-shortcuts":
		return keyboardIcon
	case "command-log":
		return logIcon
	default:
		return settingsIcon
	}
}

func (a *app) workspaceSettings(c *ui.Context) {
	settingsSection(c, func() {
		settingsGroup(c, worktreesIcon, "Workspace sections", "The tabs the sidebar lists")
		settingsChoiceCard(c, "Sidebar tabs", "Click to toggle; drag to reorder", func() {
			ui.Row(c).Gap(10).Wrap().Children(func() {
				a.workspaceSectionChips(c)
			})
			ui.Text(c, tr("Keep the tools you use close at hand. At least one section always stays visible.")).
				TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1).MaxWidth(480)
		})
		ui.Divider(c)
		settingsGroup(c, settingsIcon, "Toolbar buttons", "Choose which global actions stay visible")
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			settingsChoiceCard(c, "Header toolbar", "Repository, editor, and application actions", func() {
				a.toolbarActionChoices(c, headerToolbarActions, a.settings.HiddenHeaderActions, "Show %s in header", func(hidden []string) {
					a.settings.HiddenHeaderActions = hidden
				})
			})
			settingsChoiceCard(c, "Status bar", "Command, pull request, terminal, and help actions", func() {
				a.toolbarActionChoices(c, statusToolbarActions, a.settings.HiddenStatusActions, "Show %s in status bar", func(hidden []string) {
					a.settings.HiddenStatusActions = hidden
				})
			})
		})
		ui.Divider(c)
		settingsGroup(c, terminalIcon, "Terminal", "Default layout for new terminal docks")
		settingRow(c, "Default terminal position", "Where new terminal docks open", func() {
			for _, dock := range terminalDockChoices {
				dock := dock
				var button ui.Element
				if normalizeTerminalDock(a.settings.TerminalDock) == dock {
					button = ui.PrimaryButton(c, terminalDockName(dock))
				} else {
					button = ui.Button(c, terminalDockName(dock))
				}
				if button.Label(tr("Default terminal position") + ": " + terminalDockName(dock)).Clicked() {
					// A Settings default must not unexpectedly relocate an existing
					// dock. Freeze that tab's current edge before saving the default.
					if len(a.term.sessions) > 0 && a.term.dock == "" {
						a.term.dock = a.terminalDock()
					}
					a.settings.TerminalDock = dock
					a.settingsChanged(c)
				}
			}
		})
	})
}

// updatesSettings is the settings' release group: the version this build is
// and a button that opens the releases page on GitHub. Update checks and
// installation live in the shared title bar's update button.
func (a *app) updatesSettings(c *ui.Context) {
	theme := c.Theme()
	info := a.updateInfo()
	settingsGroup(c, updateIcon, "Updates", "Signed updates from GitHub")
	settingRow(c, "Version", "The release this build came from", func() {
		ui.Text(c, currentAppVersion()).TextColor(theme.TextMuted)
	})
	settingRow(c, "Update status", "TanGit also checks automatically after launch", func() {
		text, color := tr("Waiting for the first update check"), theme.TextMuted
		switch {
		case info.AvailableVersion != "":
			text = trf("Version %s is available", info.AvailableVersion)
			color = theme.Warning
		case !info.Enabled:
			text = tr("Updates are unavailable for this build")
		case !info.LastCheck.IsZero():
			text = trf("Up to date · checked %s", relTime(info.LastCheck.Unix()))
			color = theme.Success
		}
		ui.Text(c, text).TextColor(color)
	})
	ui.Text(c, tr("After an update installs, restart TanGit to use the new version.")).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		iconTextButton(c, "Check for updates",
			"Check GitHub for a newer signed release",
			updateIcon, info.AvailableVersion != "", a.checkForUpdates)
		iconTextButton(c, "Open GitHub releases",
			"Release notes and downloads, opened in your browser",
			externalLinkIcon, false, func() {
				c.Services().OpenURL(releasesPage)
			})
	})
}

// workspaceSectionChips toggles sidebar pages directly, keeping their state
// visible while users choose which pages to keep.
type workspaceSectionDrag struct{ id string }

func (a *app) workspaceSectionChips(c *ui.Context) {
	theme := c.Theme()
	visible := a.visibleSections()
	var moveSource, moveTarget string
	for _, section := range a.orderedSections() {
		id := workspaceSectionIDs[section]
		shown := a.sectionVisible(section)
		last := shown && len(visible) == 1
		c.Key("workspace-section:" + id)
		chip := settingsChoiceChip(c, trf("Show %s", tr(workspaceSections[section])),
			tr("Click to toggle; drag to reorder"), shown).Drag(workspaceSectionDrag{id: id})
		if last {
			chip.Tooltip(tr("The workspace keeps at least one section"))
		}
		if dragged, over := ui.DragOver[workspaceSectionDrag](chip); over && dragged.id != id {
			chip.Background(theme.Accent.Alpha(0.3)).Border(2, theme.Accent)
		}
		if dragged, dropped := ui.Drop[workspaceSectionDrag](chip); dropped {
			moveSource, moveTarget = dragged.id, id
		}
		chip.Children(func() {
			ui.Icon(c, sectionIcons[section]).FontSize(14)
			ui.Text(c, tr(workspaceSections[section])).SingleLine()
		})
		chip.OnClick(func() {
			if last {
				return
			}
			a.setSectionVisible(section, !shown)
			a.settingsChanged(c)
		})
	}
	if moveSource != "" && a.moveWorkspaceSection(moveSource, moveTarget) {
		a.settingsChanged(c)
	}
}

// settingsSection groups the heading and rows of one settings part, so the
// modal can space the parts further apart than the rows inside them.
func settingsSection(c *ui.Context, build func()) {
	ui.Column(c).FillWidth().Gap(12).Children(build)
}

// languageSelect is the interface language menu: the current language's own
// name is the trigger, and every supported language is an option.
func (a *app) languageSelect(c *ui.Context) {
	theme := c.Theme()
	selects := ui.SelectBase(c, &a.settings.Language)
	selects.Trigger.Grow(1).Padding(6, 10).Gap(8).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
		Children(func() {
			ui.Text(c, languageName(a.settings.Language)).Grow(1)
		})
	if selects.Trigger.Changed() {
		a.settingsChanged(c)
	}
	selects.Popup(func(panel ui.Element) {
		panel.Padding(4).Background(theme.Surface).Border(1, theme.Border).
			Radius(theme.Radius)
		for _, choice := range languageChoices {
			item := selects.Item(choice.Code).Padding(6, 10)
			if item.Highlighted() {
				item.Background(theme.SurfaceHover)
			}
			item.Children(func() {
				ui.Text(c, choice.Name)
			})
		}
	})
}

// accentPicker starts with the palette's own color, then the colors that can
// replace it. One wrapping row lets the dialog's width choose the line breaks.
// Each swatch carries a check when it is the one in use, so the row answers
// without a name to read.
func (a *app) accentPicker(c *ui.Context) {
	theme := c.Theme()
	palette := themeForSettingsCached(appSettings{Theme: a.settings.Theme}, theme.Dark).Accent
	ui.Row(c).AlignItems(ui.Center).Gap(5).Wrap().Children(func() {
		a.accentThemeSwatch(c, palette)
		for _, name := range accentNames {
			a.accentSwatch(c, name)
		}
		color := palette
		if chosen, ok := accentColor(a.settings.Accent); ok {
			color = chosen
		}
		if a.settingsUI.customAccent != color {
			a.settingsUI.customAccent = color
		}
		well := ui.ColorWell(c, &a.settingsUI.customAccent).
			Label(tr("Custom accent color")).Tooltip(tr("Choose a custom accent color"))
		if well.Changed() {
			a.settingsUI.customAccent = ui.RGB(a.settingsUI.customAccent.R, a.settingsUI.customAccent.G, a.settingsUI.customAccent.B)
			a.settings.Accent = formatCustomAccent(a.settingsUI.customAccent)
			a.settingsChanged(c)
		}
	})
}

// accentThemeSwatch is the way back to the palette's own accent: a dot of that
// color on the surface, since it is a rule rather than a color of our own.
func (a *app) accentThemeSwatch(c *ui.Context, color ui.Color) {
	theme := c.Theme()
	chosen := a.settings.Accent == ""
	button := ui.ButtonBase(c).Size(26, 26).Radius(theme.Radius + 4).Center()
	button.Label(tr("Theme color")).Tooltip(tr("Keep the palette's own accent"))
	button.Background(theme.Surface).Border(1, theme.Border)
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	if chosen {
		button.Border(2, theme.Text)
	}
	button.Children(func() {
		ui.Box(c).Size(11, 11).Radius(6).Background(color)
	})
	if button.Clicked() {
		a.settings.Accent = ""
		a.settingsChanged(c)
	}
}

// accentSwatch is one color of the accent row: the fill of buttons once it is
// chosen, with a check on it and a ring under the pointer.
func (a *app) accentSwatch(c *ui.Context, name string) {
	theme := c.Theme()
	color := accentChoices[name]
	chosen := a.settings.Accent == name
	button := ui.ButtonBase(c).Size(26, 26).Radius(theme.Radius + 4).Center()
	button.Label(tr(name)).Tooltip(tr(name)).Background(color)
	if button.Hovered() && !chosen {
		button.Border(2, theme.Text.Alpha(0.5))
	}
	if chosen {
		button.Border(2, theme.Text)
	}
	button.Children(func() {
		if chosen {
			ui.Icon(c, commitIcon).FontSize(14).TextColor(accentText(color))
		}
	})
	if button.Clicked() {
		a.settings.Accent = name
		a.settingsChanged(c)
	}
}

// themePreview draws the chosen palette's own colors, so a theme can be
// judged without leaving the dialog.
func (a *app) themePreview(c *ui.Context) {
	preview := themeForSettingsCached(a.settings, c.Theme().Dark)
	settingRow(c, "Palette", "The theme's own colors", func() {
		a.themeSwatch(c, preview.Background, "Background")
		a.themeSwatch(c, preview.Surface, "Surface")
		a.themeSwatch(c, preview.Border, "Border")
		a.themeSwatch(c, preview.Text, "Text")
		a.themeSwatch(c, preview.Accent, "Accent")
		a.themeSwatch(c, preview.Success, "Success")
		a.themeSwatch(c, preview.Warning, "Warning")
		a.themeSwatch(c, preview.Danger, "Danger")
	})
}

func (a *app) themeSwatch(c *ui.Context, color ui.Color, label string) {
	theme := c.Theme()
	ui.Box(c).Size(26, 20).Radius(theme.Radius).Border(1, theme.Border).
		Background(color).Tooltip(tr(label))
}

func (a *app) resetSettingsToDefaults() {
	if a.languageServerLifecycleBusy() {
		return
	}
	a.invalidateHomeBackgroundWork()
	a.clearSSHProfileSecrets()
	a.closeSSHProfileForm()
	a.settings = defaultSettings()
	a.settingsUI.conflictActionStyleChoice = conflictActionStyleChoice(a.settings.ConflictActionStyle)
	a.loadGoLanguageServerDraft()
	a.applySettingsChange()
}

func (a *app) settingsFooter(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		if ui.Button(c, tr("Reset to defaults")).Tooltip(tr("Restore every setting")).
			Disabled(a.languageServerLifecycleBusy()).Clicked() {
			a.resetSettingsToDefaults()
		}
		ui.Text(c, tr("Changes apply immediately and are saved automatically")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		if ui.PrimaryButton(c, tr("Done")).Clicked() {
			a.closeSettings()
		}
	})
}
