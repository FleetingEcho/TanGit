package app

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

type managedLanguageServer struct {
	profile    languageServerProfile
	connection languageServerConnection
	starting   bool
	sequence   uint64
	error      string
	documents  map[string]uint64
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
		if document == nil || document.state == nil || document.state.Buffer == nil {
			continue
		}
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
			profile, _ := languageServerProfileByID(id)
			server = &managedLanguageServer{profile: profile}
			a.additionalLanguageServers[id] = server
		}
		if server.connection == nil {
			if !server.starting && server.error == "" {
				a.startManagedLanguageServer(server)
			}
			continue
		}
		for path := range server.documents {
			if documents[path] == nil {
				server.connection.DidClose(languageServerFileURI(a.workingFilePath(path)))
				delete(server.documents, path)
			}
		}
		for path, document := range documents {
			a.syncManagedLanguageServerDocument(server, path, document)
		}
	}
}

func (a *app) startManagedLanguageServer(server *managedLanguageServer) {
	program, arguments := languageServerProgram(a.settings.LanguageServers, server.profile)
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
		connection, err := connectLanguageServer(launch)
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
	server.error = ""
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
					if server.error == "" {
						server.error = safeError(err)
						a.notify("error", server.profile.Name+" request failed", server.error)
					}
					return
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
			server.error = strings.TrimSpace(message.Message)
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

func (a *app) additionalLanguageServerStatus(path string) (string, string, bool) {
	var names []string
	for _, profile := range configuredLanguageServerProfiles(a.settings.LanguageServers, path) {
		if profile.ID == goLanguageServerID {
			continue
		}
		server := a.additionalLanguageServers[profile.ID]
		if server == nil {
			continue
		}
		if server.error != "" {
			return profile.Name + " issue", server.error, true
		}
		if server.starting {
			names = append(names, "Starting "+profile.Name+"…")
		} else if server.connection != nil {
			names = append(names, profile.Name)
		}
	}
	return strings.Join(names, " + "), "", false
}

func isAdditionalLanguageServerPath(path string) bool {
	for _, profile := range languageServerProfiles {
		if profile.ID == goLanguageServerID {
			continue
		}
		if _, ok := languageServerProfileForPath(profile, path); ok {
			return true
		}
	}
	return false
}
