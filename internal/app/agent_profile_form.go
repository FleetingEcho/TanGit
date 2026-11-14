package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"lazygitapp/internal/agentwork"
)

const agentManualProfile = "(Manual profile)"

func splitAgentProfilePaths(value string) []string {
	entries := splitWorktreePaths(value)
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry = strings.TrimSpace(entry); entry != "" {
			paths = append(paths, entry)
		}
	}
	return paths
}

// parseAgentEnvironment accepts only explicit portable KEY=value entries. It
// never reads a process environment or discovers .env files itself.
func parseAgentEnvironment(value string) (map[string]string, error) {
	environment := map[string]string{}
	for index, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, entry, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !validAgentEnvironmentKey(key) {
			return nil, fmt.Errorf("environment line %d must be KEY=value with a portable variable name", index+1)
		}
		if strings.HasPrefix(key, "TANGIT_AGENT_") {
			return nil, fmt.Errorf("environment line %d uses TanGit's reserved identity prefix", index+1)
		}
		if strings.ContainsRune(entry, '\x00') {
			return nil, fmt.Errorf("environment line %d contains a null byte", index+1)
		}
		environment[key] = entry
	}
	if len(environment) == 0 {
		return nil, nil
	}
	return environment, nil
}

func validAgentEnvironmentKey(key string) bool {
	if key == "" {
		return false
	}
	for index, r := range key {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || index > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func (a *app) agentFormProfile() (agentwork.Profile, error) {
	port := 0
	if value := strings.TrimSpace(a.agents.developmentPort); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 65535 {
			return agentwork.Profile{}, fmt.Errorf("development port must be between 0 and 65535")
		}
		port = parsed
	}
	environment, err := parseAgentEnvironment(a.agents.environment)
	if err != nil {
		return agentwork.Profile{}, err
	}
	name := ""
	if a.agents.templateChoice != agentManualProfile {
		name = strings.TrimSpace(a.agents.templateChoice)
	}
	profile := agentwork.Profile{
		Name:          name,
		LaunchCommand: strings.TrimSpace(a.agents.launchCommand),
		ResumeCommand: strings.TrimSpace(a.agents.resumeCommand),
		RunCommand:    strings.TrimSpace(a.agents.runCommand),
		SetupCommand:  strings.TrimSpace(a.agents.setupCommand),
		TestCommand:   strings.TrimSpace(a.agents.testCommand),
		Environment:   environment,
		SparsePaths:   splitAgentProfilePaths(a.agents.sparsePaths),
		IgnoredFiles:  splitAgentProfilePaths(a.agents.ignoredFiles),
		Resources: agentwork.Resources{
			DevelopmentPort: port,
			DatabaseName:    strings.TrimSpace(a.agents.databaseName),
			CacheDirectory:  strings.TrimSpace(a.agents.cacheDirectory),
		},
	}
	return profile, nil
}

func (a *app) selectedAgentDependencies() []string {
	dependencies := make([]string, 0, len(a.agents.dependencies))
	for taskID, selected := range a.agents.dependencies {
		if selected != nil && *selected {
			dependencies = append(dependencies, taskID)
		}
	}
	slices.Sort(dependencies)
	return dependencies
}

func agentTemplateOptions(templates []agentwork.WorkspaceTemplate) []string {
	options := make([]string, 0, len(templates)+1)
	options = append(options, agentManualProfile)
	for _, template := range templates {
		options = append(options, template.Name)
	}
	return options
}

func (a *app) reloadAgentTemplates() error {
	store, err := a.ensureAgentStore()
	if err != nil {
		return err
	}
	templates, err := store.ListTemplates()
	if err != nil {
		return err
	}
	a.agents.templates = templates
	if a.agents.templateChoice == "" {
		a.agents.templateChoice = agentManualProfile
	}
	found := a.agents.templateChoice == agentManualProfile
	for _, template := range templates {
		found = found || template.Name == a.agents.templateChoice
	}
	if !found {
		a.agents.templateChoice = agentManualProfile
	}
	return nil
}

func (a *app) applySelectedAgentTemplate() {
	for _, template := range a.agents.templates {
		if template.Name != a.agents.templateChoice {
			continue
		}
		profile := template.Profile.Clone()
		a.agents.templateName = template.Name
		a.agents.launchCommand = profile.LaunchCommand
		a.agents.resumeCommand = profile.ResumeCommand
		a.agents.setupCommand = profile.SetupCommand
		a.agents.testCommand = profile.TestCommand
		a.agents.runCommand = profile.RunCommand
		a.agents.sparsePaths = strings.Join(profile.SparsePaths, "\n")
		a.agents.ignoredFiles = strings.Join(profile.IgnoredFiles, "\n")
		a.agents.environment = formatAgentEnvironment(profile.Environment)
		a.agents.developmentPort = ""
		if profile.Resources.DevelopmentPort > 0 {
			a.agents.developmentPort = strconv.Itoa(profile.Resources.DevelopmentPort)
		}
		a.agents.databaseName = profile.Resources.DatabaseName
		a.agents.cacheDirectory = profile.Resources.CacheDirectory
		return
	}
}

func formatAgentEnvironment(environment map[string]string) string {
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+environment[key])
	}
	return strings.Join(lines, "\n")
}

func (a *app) saveAgentTemplate() {
	if a.agents.templatesBusy {
		return
	}
	name := strings.TrimSpace(a.agents.templateName)
	if name == "" {
		return
	}
	profile, err := a.agentFormProfile()
	if err != nil {
		a.notify("error", "Could not save workspace template", safeError(err))
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not save workspace template", safeError(err))
		return
	}
	a.agents.templatesBusy = true
	defer func() { a.agents.templatesBusy = false }()
	template := agentwork.WorkspaceTemplate{Name: name, Profile: profile}
	exists := false
	for _, existing := range a.agents.templates {
		exists = exists || existing.Name == name
	}
	if exists {
		_, err = store.UpdateTemplate(name, profile)
	} else {
		_, err = store.CreateTemplate(template)
	}
	if err != nil {
		a.notify("error", "Could not save workspace template", safeError(err))
		return
	}
	if err := a.reloadAgentTemplates(); err != nil {
		a.notify("error", "Template saved, but the list could not be refreshed", safeError(err))
		return
	}
	a.agents.templateChoice = name
	a.notify("success", "Workspace template saved", name)
}

func (a *app) confirmDeleteAgentTemplate() {
	name := strings.TrimSpace(a.agents.templateChoice)
	if name == "" || name == agentManualProfile || a.agents.templatesBusy {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Delete Workspace Template",
		message:      trf("Delete workspace template %s?", name),
		detail:       "Existing agent tasks keep their independent profile copies. Only the reusable template is deleted.",
		confirmLabel: "Delete template",
		destructive:  true,
		run: func() {
			store, err := a.ensureAgentStore()
			if err == nil {
				a.agents.templatesBusy = true
				err = store.DeleteTemplate(name)
				a.agents.templatesBusy = false
			}
			if err != nil {
				a.notify("error", "Could not delete workspace template", safeError(err))
				return
			}
			if err := a.reloadAgentTemplates(); err != nil {
				a.notify("error", "Template deleted, but the list could not be refreshed", safeError(err))
				return
			}
			if a.agents.templateName == name {
				a.agents.templateName = ""
			}
			a.notify("success", "Workspace template deleted", name)
		},
	})
}
