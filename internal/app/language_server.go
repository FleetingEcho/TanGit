package app

import (
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

func (a *app) reportGoLanguageServerError(message string) {
	message = strings.TrimSpace(safeError(errors.New(message)))
	if message == "" || message == a.goLanguageServerError {
		return
	}
	first := a.goLanguageServerError == ""
	a.goLanguageServerError = message
	if first {
		a.notify("error", "gopls cannot analyze this workspace", message)
	}
}

func languageServerFileURI(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if len(path) >= 2 && path[1] == ':' {
		// filepath.ToSlash follows the host OS. Normalize a Windows drive path
		// explicitly as well so URI construction is consistent on every host.
		path = "/" + strings.ReplaceAll(path, `\`, "/")
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func (a *app) startGoLanguageServer() {
	if a.goLanguageServer != nil || a.goLanguageServerStarting || a.snapshot.Root == "" || a.settingsOpen {
		return
	}
	server := a.goLanguageServerSettings()
	if server.Disabled {
		return
	}
	profile, _ := languageServerProfileByID(goLanguageServerID)
	if !a.languageServerExecutableAvailable(profile) {
		a.goLanguageServerMissing = true
		a.goLanguageServerError = ""
		return
	}
	a.goLanguageServerMissing = false
	programs := languageServerExecutableCandidates(server.Path, "gopls")
	a.goLanguageServerSeq++
	sequence := a.goLanguageServerSeq
	launch := languageServerLaunch{
		Program: programs[0], Arguments: append([]string(nil), server.Arguments...), Root: a.snapshot.Root,
		Notify: func(method string, params json.RawMessage) {
			a.handleGoLanguageServerNotification(sequence, method, params)
		},
	}
	a.goLanguageServerStarting = true
	a.goLanguageServerError = ""
	go func() {
		defer a.recoverBackground()
		connection, err := connectLanguageServerCandidates(launch, programs)
		a.update(func() {
			if sequence != a.goLanguageServerSeq || a.snapshot.Root != launch.Root {
				if connection != nil {
					go connection.Close()
				}
				return
			}
			a.goLanguageServerStarting = false
			if err != nil {
				a.goLanguageServerError = safeError(err)
				return
			}
			a.goLanguageServer = connection
			a.goLanguageServerDocs = make(map[string]uint64)
			a.watchGoLanguageServer(connection, sequence)
		})
	}()
}

func (a *app) watchGoLanguageServer(connection languageServerConnection, sequence uint64) {
	go func() {
		err, ok := <-connection.Done()
		if !ok {
			err = nil
		}
		a.update(func() {
			if sequence != a.goLanguageServerSeq || a.goLanguageServer != connection {
				return
			}
			a.goLanguageServer = nil
			a.goLanguageServerDocs = nil
			if err != nil {
				a.goLanguageServerError = safeError(err)
			} else {
				a.goLanguageServerError = "language server stopped unexpectedly"
			}
		})
	}()
}

func (a *app) stopGoLanguageServer() {
	if a.goLanguageServer == nil && !a.goLanguageServerStarting && !a.goLanguageServerMissing &&
		a.goLanguageServerError == "" && len(a.goLanguageServerDocs) == 0 {
		return
	}
	a.goLanguageServerSeq++
	connection := a.goLanguageServer
	a.goLanguageServer = nil
	a.goLanguageServerStarting = false
	a.goLanguageServerMissing = false
	a.goLanguageServerError = ""
	a.goLanguageServerDocs = nil
	if connection != nil {
		go connection.Close()
	}
}

func (a *app) syncGoLanguageServer() {
	a.syncAdditionalLanguageServers()
	if a.settingsOpen {
		return
	}
	server := a.goLanguageServerSettings()
	if server.Disabled || a.snapshot.Root == "" {
		a.stopGoLanguageServer()
		return
	}
	documents := make(map[string]*codeDocument)
	for path, document := range a.code.documents {
		if strings.EqualFold(filepath.Ext(path), ".go") && document != nil && document.state != nil && document.state.Buffer != nil {
			documents[path] = document
		}
	}
	if len(documents) == 0 {
		if a.goLanguageServer != nil {
			for path := range a.goLanguageServerDocs {
				a.goLanguageServer.DidClose(languageServerFileURI(a.workingFilePath(path)))
			}
		}
		a.stopGoLanguageServer()
		return
	}
	if a.goLanguageServer == nil {
		if !a.goLanguageServerMissing && a.goLanguageServerError == "" {
			a.startGoLanguageServer()
		}
		return
	}
	for path := range a.goLanguageServerDocs {
		if documents[path] == nil {
			a.goLanguageServer.DidClose(languageServerFileURI(a.workingFilePath(path)))
			delete(a.goLanguageServerDocs, path)
		}
	}
	for path, document := range documents {
		a.syncGoLanguageServerDocument(path, document)
	}
}

func (a *app) syncGoLanguageServerDocument(path string, document *codeDocument) {
	if a.goLanguageServer == nil || document == nil || document.state == nil || document.state.Buffer == nil {
		return
	}
	version := document.state.Buffer.Version()
	previous, opened := a.goLanguageServerDocs[path]
	uri := languageServerFileURI(a.workingFilePath(path))
	if !opened {
		a.goLanguageServer.DidOpen(uri, "go", document.state.Buffer.String(), version)
		a.goLanguageServerDocs[path] = version
	} else if previous != version {
		a.goLanguageServer.DidChange(uri, document.state.Buffer.String(), version)
		a.goLanguageServerDocs[path] = version
	}
}
