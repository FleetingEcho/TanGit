package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

type managedLanguageServer struct {
	profile      languageServerProfile
	connection   languageServerConnection
	starting     bool
	analyzing    bool
	missing      bool
	sequence     uint64
	error        string
	requestError bool
	statusBusy   bool
	statusError  bool
	progress     map[string]string
	statusDetail string
	documents    map[string]uint64
	workspaceKey string
}

func (a *app) syncAdditionalLanguageServers() {
	if a.settingsOpen || a.snapshot.Root == "" {
		a.stopAdditionalLanguageServers()
		return
	}
	if a.additionalLanguageServers == nil {
		a.additionalLanguageServers = make(map[string]*managedLanguageServer)
	}
	wanted := make(map[string]map[string]*codeDocument)
	for path, document := range a.code.documents {
		if document == nil {
			continue
		}
		// A replacement preview belongs to the workspace while its contents
		// load. Stopping its server during that gap would restart TypeScript
		// and discard the project every time the user opens another file.
		for _, profile := range configuredLanguageServerProfiles(a.settings.LanguageServers, path) {
			if profile.ID == goLanguageServerID {
				continue
			}
			if wanted[profile.ID] == nil {
				wanted[profile.ID] = make(map[string]*codeDocument)
			}
			wanted[profile.ID][path] = document
		}
	}
	for id, server := range a.additionalLanguageServers {
		if len(wanted[id]) == 0 {
			a.stopManagedLanguageServer(server)
			delete(a.additionalLanguageServers, id)
		}
	}
	for id, documents := range wanted {
		server := a.additionalLanguageServers[id]
		if server == nil {
			profile, _ := a.customLanguageServerProfileByID(id)
			server = &managedLanguageServer{profile: profile}
			a.additionalLanguageServers[id] = server
		}
		if id == "rust" {
			var inputs []string
			for path := range documents {
				inputs = append(inputs, path)
			}
			for _, path := range a.code.files {
				if filepath.Base(path) == "Cargo.toml" || filepath.Base(path) == "rust-project.json" {
					inputs = append(inputs, path)
				}
			}
			sort.Strings(inputs)
			key := strings.Join(inputs, "\x00")
			if key != server.workspaceKey {
				if server.workspaceKey != "" {
					a.stopManagedLanguageServer(server)
				}
				server.workspaceKey = key
			}
		}
		if server.connection == nil {
			if !server.starting && !server.missing && server.error == "" {
				a.startManagedLanguageServer(server)
			}
			continue
		}
		ready := false
		for _, document := range documents {
			if document != nil && document.state != nil && document.state.Buffer != nil {
				ready = true
				break
			}
		}
		if !ready && len(server.documents) > 0 {
			// The replacement is still loading. Keep the old document open
			// until its successor can be opened in the same synchronization.
			continue
		}
		// Open the replacement before closing the old preview. TypeScript
		// disposes an inferred project when its last open document closes;
		// keeping one open throughout the switch retains the analyzed project.
		for path, document := range documents {
			a.syncManagedLanguageServerDocument(server, path, document)
		}
		for path := range server.documents {
			if documents[path] == nil {
				server.connection.DidClose(languageServerFileURI(a.workingFilePath(path)))
				delete(server.documents, path)
			}
		}
	}
}

func (a *app) startManagedLanguageServer(server *managedLanguageServer) {
	if !server.profile.Custom && !a.languageServerExecutableAvailable(server.profile) {
		server.starting = false
		server.analyzing = false
		server.missing = true
		server.error = ""
		return
	}
	server.missing = false
	program, arguments := languageServerProgram(a.settings.LanguageServers, server.profile)
	programs := languageServerExecutableCandidates(a.settings.LanguageServers[server.profile.ID].Path, server.profile.Program)
	id, dataDir := server.profile.ID, a.dataDir()
	workspaceFiles := append([]string(nil), a.code.files...)
	var openFiles []string
	for path := range a.code.documents {
		if _, matches := languageServerProfileForPath(server.profile, path); matches {
			openFiles = append(openFiles, path)
		}
	}
	server.sequence++
	sequence := server.sequence
	server.starting = true
	server.error = ""
	launch := languageServerLaunch{
		Program: program, Arguments: arguments, Root: a.snapshot.Root,
		Notify: func(method string, params json.RawMessage) {
			a.handleManagedLanguageServerNotification(server, sequence, method, params)
		},
	}
	go func() {
		defer a.recoverBackground()
		launch.InitializationOptions = languageServerInitializationOptions(id, dataDir)
		if id == "rust" {
			launch.InitializationOptions = rustLanguageServerInitializationOptions(launch.Root, workspaceFiles, openFiles)
		}
		connection, err := connectLanguageServerCandidates(launch, programs)
		a.update(func() {
			if a.additionalLanguageServers[server.profile.ID] != server || server.sequence != sequence || a.snapshot.Root != launch.Root {
				if connection != nil {
					go connection.Close()
				}
				return
			}
			server.starting = false
			if err != nil {
				server.error = safeError(err)
				return
			}
			server.connection = connection
			server.documents = make(map[string]uint64)
			a.watchManagedLanguageServer(server, connection, sequence)
		})
	}()
}

// Git roots often contain nested Cargo projects (for example src-tauri).
// Link those manifests explicitly, and treat loose Rust files as standalone
// projects instead of asking rust-analyzer to discover a nonexistent workspace.
func rustLanguageServerInitializationOptions(root string, files, openFiles []string) map[string]any {
	projects := make(map[string]bool)
	addManifest := func(relative string) bool {
		full := filepath.Join(root, filepath.FromSlash(relative))
		if info, err := os.Stat(full); err == nil && info.Mode().IsRegular() {
			projects[full] = true
			return true
		}
		return false
	}
	// A root workspace owns its members; avoid loading it once per crate.
	rootProject := addManifest("Cargo.toml") || addManifest("rust-project.json")
	if !rootProject {
		for _, file := range files {
			if validCodeRelativePath(file) && (filepath.Base(file) == "Cargo.toml" || filepath.Base(file) == "rust-project.json") {
				addManifest(file)
			}
		}
		for _, file := range openFiles {
			if !validCodeRelativePath(file) {
				continue
			}
			found := false
			for directory := filepath.Dir(filepath.FromSlash(file)); directory != "."; directory = filepath.Dir(directory) {
				if addManifest(filepath.Join(directory, "Cargo.toml")) || addManifest(filepath.Join(directory, "rust-project.json")) {
					found = true
					break
				}
			}
			if !found {
				projects[filepath.Join(root, filepath.FromSlash(file))] = true
			}
		}
	}
	onlyStandalone := true
	for project := range projects {
		if filepath.Ext(project) != ".rs" {
			onlyStandalone = false
		}
	}
	linked := make([]string, 0, len(projects))
	for project := range projects {
		linked = append(linked, project)
	}
	sort.Strings(linked)
	options := map[string]any{"linkedProjects": linked}
	// Cargo's single-file check requires nightly; keep ordinary loose files
	// usable on stable Rust while retaining semantic editor features.
	if onlyStandalone {
		options["checkOnSave"] = false
	}
	return options
}

// TypeScript 7 is itself the language server and needs no legacy tsserver
// initialization options. Keep this hook for servers that need options later.
func languageServerInitializationOptions(id, dataDir string) map[string]any {
	return nil
}

func (a *app) watchManagedLanguageServer(server *managedLanguageServer, connection languageServerConnection, sequence uint64) {
	go func() {
		err, ok := <-connection.Done()
		if !ok {
			err = nil
		}
		a.update(func() {
			if a.additionalLanguageServers[server.profile.ID] != server || server.sequence != sequence || server.connection != connection {
				return
			}
			server.connection = nil
			server.documents = nil
			server.analyzing = false
			server.statusBusy = false
			server.statusError = false
			server.progress = nil
			server.statusDetail = ""
			if err != nil {
				server.error = safeError(err)
			} else {
				server.error = "language server stopped unexpectedly"
			}
		})
	}()
}

func (a *app) stopManagedLanguageServer(server *managedLanguageServer) {
	if server == nil {
		return
	}
	server.sequence++
	connection := server.connection
	server.connection = nil
	server.starting = false
	server.analyzing = false
	server.missing = false
	server.error = ""
	server.requestError = false
	server.statusBusy = false
	server.statusError = false
	server.progress = nil
	server.statusDetail = ""
	server.documents = nil
	if connection != nil {
		go connection.Close()
	}
}

func (a *app) stopAdditionalLanguageServers() {
	for id, server := range a.additionalLanguageServers {
		a.stopManagedLanguageServer(server)
		delete(a.additionalLanguageServers, id)
	}
}

func (a *app) syncManagedLanguageServerDocument(server *managedLanguageServer, path string, document *codeDocument) {
	if server == nil || server.connection == nil || document == nil || document.state == nil || document.state.Buffer == nil {
		return
	}
	language, matches := languageServerProfileForPath(server.profile, path)
	if !matches {
		return
	}
	version := document.state.Buffer.Version()
	previous, opened := server.documents[path]
	uri := languageServerFileURI(a.workingFilePath(path))
	if !opened {
		server.connection.DidOpen(uri, language, document.state.Buffer.String(), version)
		server.documents[path] = version
	} else if previous != version {
		server.connection.DidChange(uri, document.state.Buffer.String(), version)
		server.documents[path] = version
	}
}

func (a *app) additionalLanguageServersForDocument(document *codeDocument) []*managedLanguageServer {
	servers := make([]*managedLanguageServer, 0, 2)
	if document == nil {
		return servers
	}
	for _, profile := range configuredLanguageServerProfiles(a.settings.LanguageServers, document.path) {
		if profile.ID == goLanguageServerID {
			continue
		}
		server := a.additionalLanguageServers[profile.ID]
		if server != nil && server.connection != nil {
			servers = append(servers, server)
		}
	}
	return servers
}

func (a *app) additionalLanguageRequest(document *codeDocument, requestSequence *uint64, offset int, method string,
	result func(json.RawMessage, string) bool) {
	servers := a.additionalLanguageServersForDocument(document)
	if len(servers) == 0 || document == nil || document.state == nil || document.state.Buffer == nil ||
		a.code.documents[document.path] != document {
		return
	}
	text := document.state.Buffer.String()
	version := document.state.Buffer.Version()
	*requestSequence++
	sequence := *requestSequence
	params := languageServerTextPosition{
		TextDocument: languageServerTextDocument{URI: languageServerFileURI(a.workingFilePath(document.path))},
		Position:     languageServerPositionAt(text, offset),
	}
	delivered := false
	for _, server := range servers {
		a.syncManagedLanguageServerDocument(server, document.path, document)
		connection := server.connection
		connection.Request(method, params, func(raw json.RawMessage, err error) {
			a.update(func() {
				if delivered || server.connection != connection || a.code.documents[document.path] != document ||
					document.state == nil || document.state.Buffer == nil || *requestSequence != sequence ||
					document.state.Buffer.Version() != version {
					return
				}
				if err != nil {
					if isLanguageServerContentModified(err) || isLanguageServerRequestTimeout(err) {
						return
					}
					if server.error == "" {
						server.error = safeError(err)
						server.requestError = true
						a.notify("error", server.profile.Name+" request failed", server.error)
					}
					return
				}
				if server.requestError {
					server.error = ""
					server.requestError = false
				}
				delivered = result(raw, text)
			})
		})
	}
}

func (a *app) requestAdditionalLanguageHover(document *codeDocument, request editor.HoverRequest) {
	a.additionalLanguageRequest(document, &document.hoverSeq, request.Offset, "textDocument/hover", func(raw json.RawMessage, text string) bool {
		content, hoverRange, ok := parseLanguageServerHover(raw)
		if !ok || strings.TrimSpace(content) == "" {
			return false
		}
		start, end := languageServerWordRange(text, request.Offset)
		if hoverRange != nil {
			start = languageServerOffsetAt(text, hoverRange.Start)
			end = languageServerOffsetAt(text, hoverRange.End)
		}
		showLanguageServerHover(document, start, end, content)
		return true
	})
}

func (a *app) requestAdditionalLanguageDefinition(document *codeDocument, offset int) {
	source := a.currentCodeNavigationLocation()
	a.additionalLanguageRequest(document, &document.definitionSeq, offset, "textDocument/definition", func(raw json.RawMessage, _ string) bool {
		location, ok := firstLanguageServerLocation(raw)
		if !ok {
			return false
		}
		return a.openLanguageServerDefinition(source, location)
	})
}

func (a *app) requestAdditionalLanguageCompletions(document *codeDocument, request editor.CompletionRequest) {
	a.additionalLanguageRequest(document, &document.completionSeq, request.Offset, "textDocument/completion", func(raw json.RawMessage, _ string) bool {
		items := parseLanguageServerCompletions(raw)
		if len(items) == 0 {
			return false
		}
		document.state.ShowCompletions(max(0, request.Offset-len(request.Prefix)), items)
		return true
	})
}

func (a *app) handleManagedLanguageServerNotification(server *managedLanguageServer, sequence uint64, method string, raw json.RawMessage) {
	if method == "$/progress" {
		var progress struct {
			Token json.RawMessage `json:"token"`
			Value struct {
				Kind    string `json:"kind"`
				Title   string `json:"title"`
				Message string `json:"message"`
			} `json:"value"`
		}
		if json.Unmarshal(raw, &progress) != nil || len(progress.Token) == 0 || progress.Value.Kind == "" {
			return
		}
		a.update(func() {
			if a.additionalLanguageServers[server.profile.ID] != server || server.sequence != sequence {
				return
			}
			if server.progress == nil {
				server.progress = make(map[string]string)
			}
			key := string(progress.Token)
			if progress.Value.Kind == "end" {
				delete(server.progress, key)
			} else {
				detail := strings.TrimSpace(progress.Value.Message)
				if detail == "" {
					detail = strings.TrimSpace(progress.Value.Title)
				}
				server.progress[key] = detail
			}
			server.updateAnalysisStatus()
		})
		return
	}
	if method == "experimental/serverStatus" {
		var status struct {
			Health    string `json:"health"`
			Quiescent bool   `json:"quiescent"`
			Message   string `json:"message"`
		}
		if json.Unmarshal(raw, &status) != nil {
			return
		}
		a.update(func() {
			if a.additionalLanguageServers[server.profile.ID] != server || server.sequence != sequence {
				return
			}
			server.statusBusy = !status.Quiescent
			server.statusDetail = strings.TrimSpace(status.Message)
			if status.Health == "warning" || status.Health == "error" {
				if server.error == "" || server.statusError {
					server.error = server.statusDetail
					if server.error == "" {
						server.error = status.Health
					}
					server.statusError = true
				}
			} else if server.statusError {
				server.error = ""
				server.statusError = false
			}
			server.updateAnalysisStatus()
		})
		return
	}
	if method == "window/showMessage" {
		var message struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &message) != nil || !languageServerWorkspaceError(message.Type, message.Message) {
			return
		}
		a.update(func() {
			if a.additionalLanguageServers[server.profile.ID] != server || server.sequence != sequence {
				return
			}
			detail := strings.TrimSpace(message.Message)
			if detail == server.error {
				return
			}
			server.error = detail
			a.notify("error", server.profile.Name+" cannot analyze this workspace", server.error)
		})
		return
	}
	if method != "textDocument/publishDiagnostics" {
		return
	}
	a.handleLanguageServerDiagnostics(server.profile.ID, raw, func() bool {
		return a.additionalLanguageServers[server.profile.ID] == server && server.sequence == sequence
	})
}

func (server *managedLanguageServer) updateAnalysisStatus() {
	server.analyzing = server.statusBusy || len(server.progress) > 0
}

func (a *app) isAdditionalLanguageServerPath(path string) bool {
	for _, profile := range languageServerProfilesWithSettings(a.settings.LanguageServers) {
		if profile.ID == goLanguageServerID {
			continue
		}
		if _, ok := languageServerProfileForPath(profile, path); ok {
			return true
		}
	}
	return false
}
