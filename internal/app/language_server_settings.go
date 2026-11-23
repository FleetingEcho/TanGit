package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

const goLanguageServerID = "go"

var errLanguageServerNoResponse = errors.New("the language server did not respond within 5 seconds")

var runLanguageServerVersion = func(program string, arguments []string) (string, error) {
	return runLanguageServerVersionArguments(program, arguments, []string{"version"})
}

// languageServerLookPath distinguishes a managed download or configured system
// executable from a catalog entry that has not been installed yet.
var languageServerLookPath = lookPathLanguageServerProgram

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
// Require lazygitGo's managed TypeScript 7 native LSP entry point and version.
func (a *app) managedLanguageServerDependenciesAvailable(profile languageServerProfile) bool {
	server := a.settings.LanguageServers[profile.ID]
	if server.Managed != nil {
		root := server.Managed.Root
		if a.dataDir() == "" || root != managedLanguageServerRoot(a.dataDir(), profile.ID) ||
			!languageServerSettingsReferenceRoot(server, root) {
			return false
		}
		if _, err := readManagedLanguageServerManifest(root, profile.ID); err != nil {
			return false
		}
		for _, path := range languageServerExecutableCandidates(server.Path, "") {
			if path != "" && pathWithin(root, path) && !regularLanguageServerFile(path) {
				return false
			}
		}
		for _, arguments := range [][]string{server.Arguments, server.ProbeArguments} {
			for _, argument := range arguments {
				if pathWithin(root, argument) && !regularLanguageServerFile(argument) {
					return false
				}
			}
		}
		return true
	}
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

func regularLanguageServerFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func sameLanguageServerPath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func runLanguageServerVersionArguments(program string, arguments, versionArguments []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	program, err := languageServerLookPath(program)
	if err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, program, append(append([]string(nil), arguments...), versionArguments...)...)
	command.Env = languageServerCommandEnvironment(command.Environ(), program, nil)
	output, err := command.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", errLanguageServerNoResponse
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
	Formatter string
}

func (a *app) loadGoLanguageServerDraft() {
	a.invalidateLanguageServerTest()
	server := a.settings.LanguageServers[goLanguageServerID]
	a.lsp.goEnabled = !server.Disabled
	a.lsp.goPath = server.Path
	a.lsp.goArgs = strings.Join(server.Arguments, "\n")
	a.lsp.goFormatterCommand = server.Formatter
	a.loadAdditionalLanguageServerDrafts()
}

func (a *app) loadAdditionalLanguageServerDrafts() {
	a.lsp.drafts = make(map[string]*languageServerDraft)
	for _, profile := range languageServerProfilesWithSettings(a.settings.LanguageServers) {
		if profile.ID == goLanguageServerID {
			continue
		}
		server, configured := a.settings.LanguageServers[profile.ID]
		a.lsp.drafts[profile.ID] = &languageServerDraft{
			Enabled: configured && !server.Disabled, Path: server.Path, Arguments: strings.Join(server.Arguments, "\n"),
			Formatter: server.Formatter,
		}
	}
}

func (a *app) storeAdditionalLanguageServerDraft(c *ui.Context, id string) {
	profile, ok := a.customLanguageServerProfileByID(id)
	draft := a.lsp.drafts[id]
	if !ok || profile.ID == goLanguageServerID || draft == nil || a.lsp.installing[id] || a.lsp.uninstalling[id] {
		return
	}
	if server := a.lsp.additional[id]; server != nil {
		a.stopManagedLanguageServer(server)
		delete(a.lsp.additional, id)
	}
	if a.settings.LanguageServers == nil {
		a.settings.LanguageServers = make(map[string]languageServerSettings)
	}
	server := editLanguageServerLaunch(a.settings.LanguageServers[id], strings.TrimSpace(draft.Path), languageServerArguments(draft.Arguments))
	server.Disabled = !draft.Enabled
	server.Formatter = strings.TrimSpace(draft.Formatter)
	if !draft.Enabled && server.Custom == nil && server.Managed == nil && server.Formatter == "" {
		delete(a.settings.LanguageServers, id)
	} else {
		a.settings.LanguageServers[id] = server
	}
	a.settingsChanged(c)
}

func (a *app) invalidateLanguageServerTest() {
	a.lsp.invalidateTest()
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

func editLanguageServerLaunch(server languageServerSettings, path string, arguments []string) languageServerSettings {
	if server.Path != path || !slices.Equal(server.Arguments, arguments) {
		// Editing a managed command turns it into a user-owned launch setting.
		// Its files stay in place, but lazygitGo no longer has removal authority.
		server.Managed = nil
		server.ProbeArguments = nil
	}
	server.Path = path
	server.Arguments = arguments
	return server
}

func (a *app) storeGoLanguageServerDraft(c *ui.Context) {
	if a.lsp.installing[goLanguageServerID] || a.lsp.uninstalling[goLanguageServerID] {
		return
	}
	a.invalidateLanguageServerTest()
	a.stopGoLanguageServer()
	path := strings.TrimSpace(a.lsp.goPath)
	arguments := languageServerArguments(a.lsp.goArgs)
	server := editLanguageServerLaunch(a.settings.LanguageServers[goLanguageServerID], path, arguments)
	server.Disabled = !a.lsp.goEnabled
	server.Formatter = strings.TrimSpace(a.lsp.goFormatterCommand)
	if a.settings.LanguageServers == nil {
		a.settings.LanguageServers = make(map[string]languageServerSettings)
	}
	if !server.Disabled && server.Path == "" && len(server.Arguments) == 0 && server.Formatter == "" {
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
	if a.lsp.testBusy {
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
		if managedProbe := a.settings.LanguageServers[id].ProbeArguments; len(managedProbe) > 0 && !profile.ProtocolProbe {
			arguments = nil
			probeArguments = managedProbe
		}
	}
	a.lsp.testSeq++
	sequence := a.lsp.testSeq
	a.lsp.testID = id
	a.lsp.testBusy = true
	a.lsp.testResult = ""
	a.lsp.testOK = false
	workspaceRoot, dataDir := a.snapshot.Root, a.dataDir()
	go func() {
		defer a.recoverBackground()
		var output string
		var err error
		if profile.ProtocolProbe {
			connection, connectErr := connectLanguageServerCandidates(languageServerLaunch{
				Program: programs[0], Arguments: arguments, Root: workspaceRoot,
				InitializationOptions: languageServerInitializationOptions(profile.ID, dataDir),
			}, programs)
			err = connectErr
			if connection != nil {
				connection.Close()
				waitForLanguageServerClose(connection)
			}
		} else {
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
		}
		a.update(func() {
			if sequence != a.lsp.testSeq {
				return
			}
			a.lsp.testBusy = false
			name := profile.Name
			if id == goLanguageServerID {
				name = "gopls"
			}
			if err != nil {
				// safeError preserves external command output. Replace only the
				// known application-authored timeout on the UI thread.
				message := safeError(err)
				if errors.Is(err, errLanguageServerNoResponse) {
					if prefix := strings.TrimSuffix(message, errLanguageServerNoResponse.Error()); prefix != message {
						message = prefix + tr("the language server did not respond within 5 seconds")
					}
				}
				a.lsp.testResult = trf("Could not start %s: %s", name, message)
				return
			}
			a.lsp.testOK = true
			if output == "" {
				output = trf("%s started successfully", name)
			}
			a.lsp.testResult = output
		})
	}()
}
