package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

func (a *app) languageServerCatalog(c *ui.Context) {
	theme := c.Theme()
	ui.Text(c, tr("Language Server Catalog")).Bold().FontSize(theme.FontSize + 1)
	ui.Text(c, tr("Search, download, update, and configure language servers managed by TanGit.")).
		TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(560)
	ui.SearchField(c, &a.languageServerCatalogQuery).Label(tr("Search language servers")).
		Placeholder(tr("Search by language, server, or command")).MaxWidth(560)
	for _, category := range []struct{ id, label string }{{"core", "CORE — Daily development"}, {"optional", "OPTIONAL — Enable per project"}} {
		profiles := make([]languageServerProfile, 0)
		for _, profile := range languageServerProfiles {
			if profile.Category == category.id && languageServerCatalogMatches(profile, a.languageServerCatalogQuery) {
				profiles = append(profiles, profile)
			}
		}
		if len(profiles) == 0 {
			continue
		}
		ui.Text(c, tr(category.label)).Bold().TextColor(theme.TextMuted).Margin(8, 0, 2)
		for _, profile := range profiles {
			a.languageServerCatalogRow(c, profile)
		}
	}
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
	draft := a.languageServerDrafts[profile.ID]
	enabled := profile.ID == goLanguageServerID && a.goLanguageServerEnabled || draft != nil && draft.Enabled
	installing := a.languageServerInstalling[profile.ID]
	selected := a.languageServerCatalogSelected == profile.ID
	ui.Column(c).Key("lsp-catalog-"+profile.ID).Border(1, theme.Border).Radius(theme.Radius).
		Padding(10).Gap(8).MaxWidth(600).Children(func() {
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
				ui.Text(c, strings.TrimSpace(profile.Program+" "+strings.Join(profile.Arguments, " "))).
					Font("monospace").FontSize(theme.FontSize - 2).TextColor(theme.TextMuted)
			})
			if installing {
				ui.Spinner(c).Label(tr("Installing " + profile.Name))
			}
			installLabel := "Download"
			if enabled {
				installLabel = "Update"
			}
			button := ui.Button(c, tr(installLabel)).Disabled(installing).Label(tr(installLabel + " " + profile.Name))
			if button.Clicked() {
				a.installLanguageServer(profile.ID)
			}
			configure := ui.Button(c, tr("Configure")).Label(tr("Configure " + profile.Name))
			if configure.Clicked() {
				if selected {
					a.languageServerCatalogSelected = ""
				} else {
					a.languageServerCatalogSelected = profile.ID
				}
			}
		})
		if selected {
			a.languageServerCatalogConfiguration(c, profile)
		}
	})
}

func (a *app) languageServerCatalogConfiguration(c *ui.Context, profile languageServerProfile) {
	if profile.ID == goLanguageServerID {
		ui.Text(c, tr("Use the Go language server controls above to configure gopls.")).TextColor(c.Theme().TextMuted)
		return
	}
	draft := a.languageServerDrafts[profile.ID]
	if draft == nil {
		return
	}
	toggle := ui.Checkbox(c, &draft.Enabled, tr("Enable "+profile.Name)).Label(tr("Enable " + profile.Name))
	if toggle.Changed() {
		a.storeAdditionalLanguageServerDraft(c, profile.ID)
	}
	path := ui.TextInput(c, &draft.Path).Label(profile.Name + " executable path").Placeholder(profile.Program).Grow(1)
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
