package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const goLanguageServerID = "go"

var runLanguageServerVersion = func(program string, arguments []string) (string, error) {
	return runLanguageServerVersionArguments(program, arguments, []string{"version"})
}

// languageServerLookPath distinguishes a managed download or configured system
// executable from a catalog entry that has not been installed yet.
var languageServerLookPath = exec.LookPath

func (a *app) languageServerExecutableAvailable(profile languageServerProfile) bool {
	executable := false
	for _, program := range languageServerExecutableCandidates(a.settings.LanguageServers[profile.ID].Path, profile.Program) {
		if _, err := languageServerLookPath(program); err == nil {
			executable = true
			break
		}
	}
	if !executable {
		return false
	}
	return a.managedLanguageServerDependenciesAvailable(profile)
}

// A managed TypeScript Language Server is launched through the user's Node.js
// executable, so checking only Node can hide an obsolete TS6 wrapper install.
// Require TanGit's managed TypeScript 7 native LSP entry point and version.
func (a *app) managedLanguageServerDependenciesAvailable(profile languageServerProfile) bool {
	if profile.ID != "typescript" {
		return true
	}
	dataDir := a.dataDir()
	if dataDir == "" {
		return true
	}
	prefix := filepath.Join(dataDir, "tools", "node")
	managedNodeModules := filepath.Join(prefix, "node_modules")
	managed := false
	for _, argument := range a.settings.LanguageServers[profile.ID].Arguments {
		if pathWithin(managedNodeModules, argument) {
			managed = true
			break
		}
	}
	if !managed {
		return true
	}
	manifest, err := os.ReadFile(filepath.Join(managedNodeModules, "typescript", "package.json"))
	if err != nil {
		return false
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(manifest, &metadata) != nil || !strings.HasPrefix(metadata.Version, "7.") {
		return false
	}
	executable, err := nodePackageExecutable(prefix, "typescript", profile.Program)
	if err != nil {
		return false
	}
	for _, argument := range a.settings.LanguageServers[profile.ID].Arguments {
		if sameLanguageServerPath(argument, executable) {
			return true
		}
	}
	return false
}

func sameLanguageServerPath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func runLanguageServerVersionArguments(program string, arguments, versionArguments []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, program, append(append([]string(nil), arguments...), versionArguments...)...)
	output, err := command.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", errors.New("the language server did not respond within 5 seconds")
	}
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text != "" {
			return "", fmt.Errorf("%s: %w", text, err)
		}
		return "", err
	}
	return text, nil
}

type languageServerDraft struct {
	Enabled   bool
	Path      string
	Arguments string
}

func (a *app) loadGoLanguageServerDraft() {
	a.invalidateLanguageServerTest()
	server := a.settings.LanguageServers[goLanguageServerID]
	a.goLanguageServerEnabled = !server.Disabled
	a.goLanguageServerPath = server.Path
	a.goLanguageServerArgs = strings.Join(server.Arguments, "\n")
	a.loadAdditionalLanguageServerDrafts()
}

func (a *app) loadAdditionalLanguageServerDrafts() {
	a.languageServerDrafts = make(map[string]*languageServerDraft)
	for _, profile := range languageServerProfilesWithSettings(a.settings.LanguageServers) {
		if profile.ID == goLanguageServerID {
			continue
		}
		server, configured := a.settings.LanguageServers[profile.ID]
		a.languageServerDrafts[profile.ID] = &languageServerDraft{
			Enabled: configured && !server.Disabled, Path: server.Path, Arguments: strings.Join(server.Arguments, "\n"),
		}
	}
}

func (a *app) storeAdditionalLanguageServerDraft(c *ui.Context, id string) {
	profile, ok := a.customLanguageServerProfileByID(id)
	draft := a.languageServerDrafts[id]
	if !ok || profile.ID == goLanguageServerID || draft == nil {
		return
	}
	if server := a.additionalLanguageServers[id]; server != nil {
		a.stopManagedLanguageServer(server)
		delete(a.additionalLanguageServers, id)
	}
	if a.settings.LanguageServers == nil {
		a.settings.LanguageServers = make(map[string]languageServerSettings)
	}
	server := a.settings.LanguageServers[id]
	server.Disabled = !draft.Enabled
	server.Path = strings.TrimSpace(draft.Path)
	server.Arguments = languageServerArguments(draft.Arguments)
	if !draft.Enabled && server.Custom == nil {
		delete(a.settings.LanguageServers, id)
	} else {
		a.settings.LanguageServers[id] = server
	}
	a.settingsChanged(c)
}

func (a *app) invalidateLanguageServerTest() {
	a.languageServerTestSeq++
	a.languageServerTestBusy = false
	a.languageServerTestResult = ""
	a.languageServerTestOK = false
}

func languageServerArguments(text string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	arguments := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			arguments = append(arguments, line)
		}
	}
	return arguments
}

func (a *app) storeGoLanguageServerDraft(c *ui.Context) {
	a.invalidateLanguageServerTest()
	a.stopGoLanguageServer()
	server := languageServerSettings{
		Disabled:  !a.goLanguageServerEnabled,
		Path:      strings.TrimSpace(a.goLanguageServerPath),
		Arguments: languageServerArguments(a.goLanguageServerArgs),
	}
	if a.settings.LanguageServers == nil {
		a.settings.LanguageServers = make(map[string]languageServerSettings)
	}
	if !server.Disabled && server.Path == "" && len(server.Arguments) == 0 {
		delete(a.settings.LanguageServers, goLanguageServerID)
	} else {
		a.settings.LanguageServers[goLanguageServerID] = server
	}
	if len(a.settings.LanguageServers) == 0 {
		a.settings.LanguageServers = nil
	}
	a.settingsChanged(c)
}

func (a *app) goLanguageServerSettings() languageServerSettings {
	return a.settings.LanguageServers[goLanguageServerID]
}

func (a *app) testGoLanguageServer() {
	a.testLanguageServerProfile(goLanguageServerID)
}

func (a *app) testLanguageServerProfile(id string) {
	if a.languageServerTestBusy {
		return
	}
	profile, ok := a.customLanguageServerProfileByID(id)
	if !ok {
		return
	}
	_, arguments := languageServerProgram(a.settings.LanguageServers, profile)
	programs := languageServerExecutableCandidates(a.settings.LanguageServers[id].Path, profile.Program)
	probeArguments := profile.VersionArguments
	if id != goLanguageServerID {
		// Transport flags such as --stdio and --lsp start the long-running
		// server; a version probe needs only the user's explicit global flags.
		arguments = append([]string(nil), a.settings.LanguageServers[id].Arguments...)
		if managedProbe := a.settings.LanguageServers[id].ProbeArguments; len(managedProbe) > 0 {
			arguments = nil
			probeArguments = managedProbe
		}
	}
	a.languageServerTestSeq++
	sequence := a.languageServerTestSeq
	a.languageServerTestID = id
	a.languageServerTestBusy = true
	a.languageServerTestResult = ""
	a.languageServerTestOK = false
	go func() {
		defer a.recoverBackground()
		var output string
		var err error
		for _, program := range programs {
			if id == goLanguageServerID {
				output, err = runLanguageServerVersion(program, arguments)
			} else {
				output, err = runLanguageServerVersionArguments(program, arguments, probeArguments)
			}
			if err == nil {
				break
			}
		}
		a.update(func() {
			if sequence != a.languageServerTestSeq {
				return
			}
			a.languageServerTestBusy = false
			if err != nil {
				name := profile.Name
				if id == goLanguageServerID {
					name = "gopls"
				}
				a.languageServerTestResult = "Could not start " + name + ": " + safeError(err)
				return
			}
			a.languageServerTestOK = true
			if output == "" {
				if id == goLanguageServerID {
					output = "gopls started successfully"
				} else {
					output = profile.Name + " started successfully"
				}
			}
			a.languageServerTestResult = output
		})
	}()
}
