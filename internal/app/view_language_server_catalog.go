package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *app) languageServerCatalog(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("Language Server Catalog")).Bold().FontSize(theme.FontSize + 1).Grow(1)
		flatIconButton(c, "Add custom language server", "Add a custom language server", plusIcon, 15, 4,
			false, func() { a.openCustomLanguageServerForm("") })
	})
	ui.Text(c, tr("Search, download, update, and configure language servers managed by TanGit.")).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(560)
	ui.SearchField(c, &a.languageServerCatalogQuery).Label(tr("Search language servers")).
		Placeholder(tr("Search by language, server, or command")).MaxWidth(560)
	if a.customLanguageServerForm.Open {
		a.customLanguageServerEditor(c)
	}
	// Keep the search field fixed while the complete catalog scrolls on its own.
	// Filtered lists shrink to their contents instead of leaving a tall empty box.
	ui.Scroll(c).Label(tr("Language server results")).MaxHeight(420).FillWidth().Children(func() {
		for _, category := range []struct{ id, label string }{{"core", "CORE — Daily development"}, {"optional", "OPTIONAL — Enable per project"}, {"custom", "CUSTOM — User defined"}} {
			profiles := make([]languageServerProfile, 0)
			for _, profile := range languageServerProfilesWithSettings(a.settings.LanguageServers) {
				if profile.Category == category.id && languageServerCatalogMatches(profile, a.languageServerCatalogQuery) {
					profiles = append(profiles, profile)
				}
			}
			if len(profiles) == 0 {
				continue
			}
			a.sortLanguageServerCatalogProfiles(profiles)
			ui.Text(c, tr(category.label)).Bold().TextColor(theme.TextMuted).Margin(8, 0, 2)
			for _, profile := range profiles {
				a.languageServerCatalogRow(c, profile)
			}
		}
	})
}

func (a *app) toggleLanguageServerProfile(c *ui.Context, profile languageServerProfile, enabled bool) {
	a.invalidateLanguageServerTest()
	if enabled && !profile.Custom && !a.languageServerExecutableAvailable(profile) {
		// Expanded checkboxes are bound to their draft and have already changed
		// by the time they reach this shared guard. Roll those bindings back too.
		if profile.ID == goLanguageServerID {
			a.goLanguageServerEnabled = false
		} else if draft := a.languageServerDrafts[profile.ID]; draft != nil {
			draft.Enabled = false
		}
		a.notify("warning", tr("Download required"), trf("Download %s first, or configure an installed executable.", profile.Name))
		return
	}
	if profile.ID == goLanguageServerID {
		a.goLanguageServerEnabled = enabled
		a.storeGoLanguageServerDraft(c)
		return
	}
	draft := a.languageServerDrafts[profile.ID]
	if draft == nil {
		return
	}
	draft.Enabled = enabled
	a.storeAdditionalLanguageServerDraft(c, profile.ID)
}

func (a *app) customLanguageServerEditor(c *ui.Context) {
	theme := c.Theme()
	form := &a.customLanguageServerForm
	title := "Add custom language server"
	if form.ID != "" {
		title = "Edit custom language server"
	}
	ui.Column(c.Key("custom-language-server-form")).Border(1, theme.Border).Radius(theme.Radius).
		Padding(10).Gap(8).MaxWidth(600).Children(func() {
		ui.Text(c, tr(title)).Bold()
		ui.TextInput(c, &form.Name).Label(tr("Custom server name")).Placeholder("ty")
		ui.TextInput(c, &form.Description).Label(tr("Custom server description")).
			Placeholder(tr("Python type checking, navigation, and completion"))
		ui.TextInput(c, &form.Path).Label(tr("Custom server executables")).Placeholder("ty, /path/to/ty").
			Tooltip(tr("Comma-separated executables, tried in order"))
		ui.TextArea(c, &form.Arguments).Label(tr("Custom server arguments")).Placeholder("server").Lines(2, 4)
		ui.TextArea(c, &form.Probe).Label(tr("Custom server version probe")).Placeholder("--version").Lines(2, 4)
		ui.TextArea(c, &form.Extensions).Label(tr("Custom server file extensions")).
			Placeholder(".py=python, .pyi=python").Lines(2, 4)
		ui.TextInput(c, &form.DownloadURL).Label(tr("Custom server download URL")).
			Placeholder("https://pypi.org/project/ty/")
		ui.Checkbox(c, &form.Enabled, tr("Enable custom language server")).Label(tr("Enable custom language server"))
		if form.Error != "" {
			ui.Text(c, form.Error).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
		}
		ui.Row(c).Gap(8).Children(func() {
			save := ui.PrimaryButton(c, tr("Save custom language server")).Label(tr("Save custom language server"))
			if save.Clicked() {
				a.saveCustomLanguageServer(c)
			}
			if ui.Button(c, tr("Cancel custom language server")).Label(tr("Cancel")).Clicked() {
				form.Open = false
				form.Error = ""
			}
		})
	})
}

func (a *app) languageServerCatalogProfileEnabled(profile languageServerProfile) bool {
	if profile.ID == goLanguageServerID {
		return a.goLanguageServerEnabled
	}
	draft := a.languageServerDrafts[profile.ID]
	return draft != nil && draft.Enabled
}

func (a *app) sortLanguageServerCatalogProfiles(profiles []languageServerProfile) {
	slices.SortStableFunc(profiles, func(left, right languageServerProfile) int {
		leftEnabled := a.languageServerCatalogProfileEnabled(left)
		rightEnabled := a.languageServerCatalogProfileEnabled(right)
		if leftEnabled != rightEnabled {
			if leftEnabled {
				return -1
			}
			return 1
		}
		if order := strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)); order != 0 {
			return order
		}
		return strings.Compare(left.ID, right.ID)
	})
}

func languageServerCatalogMatches(profile languageServerProfile, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	values := []string{profile.ID, profile.Name, profile.Program, profile.Description}
	for extension, language := range profile.Extensions {
		values = append(values, extension, language)
	}
	for filename, language := range profile.Filenames {
		values = append(values, filename, language)
	}
	return slices.ContainsFunc(values, func(value string) bool { return strings.Contains(strings.ToLower(value), query) })
}

func (a *app) languageServerCatalogRow(c *ui.Context, profile languageServerProfile) {
	theme := c.Theme()
	enabled := a.languageServerCatalogProfileEnabled(profile)
	installing := a.languageServerInstalling[profile.ID]
	selected := a.languageServerCatalogSelected == profile.ID
	ui.Column(c.Key("lsp-catalog-"+profile.ID)).Border(1, theme.Border).Radius(theme.Radius).
		Padding(10).Gap(8).Margin(0, 0, 10, 0).MaxWidth(600).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, profile.Name).Bold()
					status := tr("Not configured")
					color := theme.TextMuted
					if installing {
						status, color = tr("Installing…"), theme.Warning
					} else if enabled {
						status, color = tr("Enabled"), theme.Success
					}
					ui.Text(c, status).TextColor(color).FontSize(theme.FontSize - 1)
				})
				ui.Text(c, profile.Description).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				source := languageServerDownloadURLs[profile.ID]
				if profile.Custom {
					source = profile.DownloadURL
				}
				if source != "" {
					label := strings.TrimPrefix(strings.TrimPrefix(source, "https://"), "www.")
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Text(c, tr("Download source:")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2)
						ui.Link(c, label, source).FontSize(theme.FontSize - 2).
							Label(tr("Open download source") + ": " + profile.Name).Tooltip(source)
					})
				}
				ui.Text(c, strings.TrimSpace(profile.Program+" "+strings.Join(profile.Arguments, " "))).
					Font("monospace").FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
			})
			if installing {
				ui.Spinner(c).Label(tr("Installing " + profile.Name))
			}
			toggleLabel, toggleIcon := "Enable "+profile.Name, switchIcon
			if enabled {
				toggleLabel, toggleIcon = "Disable "+profile.Name, switchOnIcon
			}
			flatToggleIconButton(c, toggleLabel, toggleLabel, toggleIcon, 16, 4, enabled, func() {
				a.toggleLanguageServerProfile(c, profile, !enabled)
			})
			if !profile.Custom {
				installLabel, installIcon := "Download", downloadIcon
				if enabled {
					installLabel, installIcon = "Update", updateIcon
				}
				flatIconButton(c, installLabel+" "+profile.Name, installLabel+" "+profile.Name, installIcon, 14, 6,
					installing, func() { a.installLanguageServer(profile.ID) })
			}
			flatIconButton(c, "Configure "+profile.Name, "Configure "+profile.Name, settingsIcon, 14, 6,
				false, func() {
					if selected {
						a.languageServerCatalogSelected = ""
					} else {
						a.languageServerCatalogSelected = profile.ID
					}
				})
		})
		if selected {
			a.languageServerCatalogConfiguration(c, profile)
		}
	})
}

func (a *app) languageServerCatalogConfiguration(c *ui.Context, profile languageServerProfile) {
	if profile.Custom {
		ui.Row(c).Gap(8).Children(func() {
			iconTextButtonState(c, "Edit custom definition", "Edit custom definition", editIcon, false, false, func() {
				a.openCustomLanguageServerForm(profile.ID)
			})
			iconTextButtonState(c, "Remove custom server", "Remove custom server", trashIcon, false, false, func() {
				a.removeCustomLanguageServer(c, profile.ID)
			})
		})
	}
	if profile.ID == goLanguageServerID {
		toggle := ui.Checkbox(c, &a.goLanguageServerEnabled, tr("Enable gopls")).Label(tr("Enable Go (gopls)"))
		if toggle.Changed() {
			a.toggleLanguageServerProfile(c, profile, a.goLanguageServerEnabled)
		}
		path := ui.TextInput(c, &a.goLanguageServerPath).Label(profile.Name + " executable path").
			Placeholder("gopls, /path/to/gopls").
			Tooltip(tr("Comma-separated executables, tried in order")).Grow(1)
		if path.Changed() {
			a.storeGoLanguageServerDraft(c)
		}
		arguments := ui.TextArea(c, &a.goLanguageServerArgs).Label(profile.Name+" arguments").
			Placeholder("-remote=auto").Lines(2, 5).Grow(1)
		if arguments.Changed() {
			a.storeGoLanguageServerDraft(c)
		}
		testLabel := "Test " + profile.Name
		iconTextButtonState(c, testLabel, testLabel, terminalIcon, false,
			a.languageServerTestBusy && a.languageServerTestID == profile.ID, a.testGoLanguageServer)
		if a.languageServerTestBusy && a.languageServerTestID == profile.ID {
			ui.Spinner(c).Label(tr("Testing language server"))
		}
		if a.languageServerTestResult != "" && a.languageServerTestID == profile.ID {
			color := c.Theme().Danger
			if a.languageServerTestOK {
				color = c.Theme().Success
			}
			ui.Text(c, a.languageServerTestResult).TextColor(color).FontSize(c.Theme().FontSize - 1)
		}
		return
	}
	draft := a.languageServerDrafts[profile.ID]
	if draft == nil {
		return
	}
	toggle := ui.Checkbox(c, &draft.Enabled, tr("Enable "+profile.Name)).Label(tr("Enable " + profile.Name))
	if toggle.Changed() {
		a.toggleLanguageServerProfile(c, profile, draft.Enabled)
	}
	path := ui.TextInput(c, &draft.Path).Label(profile.Name + " executable path").
		Placeholder(profile.Program + ", /path/to/" + profile.Program).
		Tooltip(tr("Comma-separated executables, tried in order")).Grow(1)
	if path.Changed() {
		a.storeAdditionalLanguageServerDraft(c, profile.ID)
	}
	arguments := ui.TextArea(c, &draft.Arguments).Label(profile.Name+" arguments").
		Placeholder(strings.Join(profile.Arguments, "\n")).Lines(2, 5).Grow(1)
	if arguments.Changed() {
		a.storeAdditionalLanguageServerDraft(c, profile.ID)
	}
	testLabel := "Test " + profile.Name
	iconTextButtonState(c, testLabel, testLabel, terminalIcon, false,
		a.languageServerTestBusy && a.languageServerTestID == profile.ID, func() { a.testLanguageServerProfile(profile.ID) })
	if a.languageServerTestResult != "" && a.languageServerTestID == profile.ID {
		color := c.Theme().Danger
		if a.languageServerTestOK {
			color = c.Theme().Success
		}
		ui.Text(c, a.languageServerTestResult).TextColor(color).FontSize(c.Theme().FontSize - 1)
	}
}
