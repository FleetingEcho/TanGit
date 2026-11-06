package app

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"
)

// customLanguageServerDefinition is the part of a user-defined server that
// cannot be represented by executable settings alone.
type customLanguageServerDefinition struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	DownloadURL string            `json:"downloadUrl,omitempty"`
	Extensions  map[string]string `json:"extensions"`
}

type customLanguageServerForm struct {
	Open        bool
	ID          string
	Name        string
	Description string
	Path        string
	Arguments   string
	Probe       string
	Extensions  string
	DownloadURL string
	Enabled     bool
	Error       string
}

func customLanguageServerProfile(id string, server languageServerSettings) (languageServerProfile, bool) {
	definition := server.Custom
	if definition == nil || strings.TrimSpace(definition.Name) == "" || len(definition.Extensions) == 0 {
		return languageServerProfile{}, false
	}
	programs := languageServerExecutableCandidates(server.Path, "")
	if len(programs) == 0 || programs[0] == "" {
		return languageServerProfile{}, false
	}
	description := strings.TrimSpace(definition.Description)
	if description == "" {
		description = "User-defined language server"
	}
	probe := append([]string(nil), server.ProbeArguments...)
	if len(probe) == 0 {
		probe = []string{"--version"}
	}
	return languageServerProfile{
		ID: id, Name: strings.TrimSpace(definition.Name), Category: "custom", Description: description,
		Program: programs[0], Arguments: append([]string(nil), server.Arguments...), VersionArguments: probe,
		Extensions: definition.Extensions, DownloadURL: definition.DownloadURL, Custom: true,
	}, true
}

func languageServerProfilesWithSettings(settings map[string]languageServerSettings) []languageServerProfile {
	profiles := append([]languageServerProfile(nil), languageServerProfiles...)
	ids := make([]string, 0, len(settings))
	for id := range settings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if profile, ok := customLanguageServerProfile(id, settings[id]); ok {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

func (a *app) customLanguageServerProfileByID(id string) (languageServerProfile, bool) {
	if profile, ok := languageServerProfileByID(id); ok {
		return profile, true
	}
	return customLanguageServerProfile(id, a.settings.LanguageServers[id])
}

// parseCustomLanguageServerExtensions accepts .py=python, .pyi=python. The
// shorthand .py means .py=py, which is useful for languages whose LSP id
// matches their ordinary extension.
func parseCustomLanguageServerExtensions(value string) (map[string]string, error) {
	extensions := make(map[string]string)
	for _, item := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' }) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		extension, language, found := strings.Cut(item, "=")
		extension = strings.ToLower(strings.TrimSpace(extension))
		language = strings.ToLower(strings.TrimSpace(language))
		if !found {
			language = strings.TrimPrefix(extension, ".")
		}
		if !strings.HasPrefix(extension, ".") || len(extension) < 2 || strings.ContainsAny(extension, "/\\ \t") || language == "" || strings.ContainsAny(language, "/\\ \t,") {
			return nil, fmt.Errorf("invalid file type %q; use .py=python", item)
		}
		extensions[extension] = language
	}
	if len(extensions) == 0 {
		return nil, fmt.Errorf("add at least one file extension")
	}
	return extensions, nil
}

func formatCustomLanguageServerExtensions(extensions map[string]string) string {
	items := make([]string, 0, len(extensions))
	for extension, language := range extensions {
		items = append(items, extension+"="+language)
	}
	sort.Strings(items)
	return strings.Join(items, ", ")
}

func validCustomLanguageServerDownload(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func (a *app) openCustomLanguageServerForm(id string) {
	form := customLanguageServerForm{Open: true, ID: id, Enabled: true, Probe: "--version"}
	if server := a.settings.LanguageServers[id]; server.Custom != nil {
		form.Name = server.Custom.Name
		form.Description = server.Custom.Description
		form.Path = server.Path
		form.Arguments = strings.Join(server.Arguments, "\n")
		form.Probe = strings.Join(server.ProbeArguments, "\n")
		form.Extensions = formatCustomLanguageServerExtensions(server.Custom.Extensions)
		form.DownloadURL = server.Custom.DownloadURL
		form.Enabled = !server.Disabled
	}
	a.customLanguageServerForm = form
}

func customLanguageServerID(name string, existing map[string]languageServerSettings) string {
	var slug strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			slug.WriteRune(r)
		} else if slug.Len() > 0 && !strings.HasSuffix(slug.String(), "-") {
			slug.WriteByte('-')
		}
	}
	base := "custom-" + strings.Trim(slug.String(), "-")
	if base == "custom-" {
		base = "custom-server"
	}
	id := base
	for suffix := 2; ; suffix++ {
		if _, exists := existing[id]; !exists {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func (a *app) saveCustomLanguageServer(c *ui.Context) {
	form := &a.customLanguageServerForm
	if !form.Open {
		return
	}
	name := strings.TrimSpace(form.Name)
	path := strings.TrimSpace(form.Path)
	extensions, err := parseCustomLanguageServerExtensions(form.Extensions)
	downloadURL := strings.TrimSpace(form.DownloadURL)
	switch {
	case name == "":
		form.Error = tr("Enter a language server name.")
	case path == "":
		form.Error = tr("Enter at least one executable.")
	case err != nil:
		form.Error = err.Error()
	case !validCustomLanguageServerDownload(downloadURL):
		form.Error = tr("Download URL must be a valid HTTPS address.")
	default:
		id := form.ID
		if id == "" {
			id = customLanguageServerID(name, a.settings.LanguageServers)
		}
		if server := a.additionalLanguageServers[id]; server != nil {
			a.stopManagedLanguageServer(server)
			delete(a.additionalLanguageServers, id)
		}
		if a.settings.LanguageServers == nil {
			a.settings.LanguageServers = make(map[string]languageServerSettings)
		}
		a.settings.LanguageServers[id] = languageServerSettings{
			Disabled: !form.Enabled, Path: path, Arguments: languageServerArguments(form.Arguments),
			ProbeArguments: languageServerArguments(form.Probe),
			Custom: &customLanguageServerDefinition{
				Name: name, Description: strings.TrimSpace(form.Description), DownloadURL: downloadURL, Extensions: extensions,
			},
		}
		form.Open = false
		a.loadGoLanguageServerDraft()
		a.languageServerCatalogSelected = id
		// A custom extension may not occur in any built-in profile. Install the
		// editor callbacks on already-open documents as well as future loads.
		for _, document := range a.code.documents {
			a.configureGoLanguageDocument(document)
			// Language-server setup replaces OnHover; keep blame and its
			// preserved language hover installed around the new callback.
			a.configureCodeBlame(document)
		}
		a.settingsChanged(c)
	}
}

func (a *app) removeCustomLanguageServer(c *ui.Context, id string) {
	if a.settings.LanguageServers[id].Custom == nil {
		return
	}
	if server := a.additionalLanguageServers[id]; server != nil {
		a.stopManagedLanguageServer(server)
		delete(a.additionalLanguageServers, id)
	}
	delete(a.settings.LanguageServers, id)
	delete(a.languageServerDrafts, id)
	if a.languageServerCatalogSelected == id {
		a.languageServerCatalogSelected = ""
	}
	a.settingsChanged(c)
}
