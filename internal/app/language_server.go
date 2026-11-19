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
	if message == "" || message == a.lsp.goError {
		return
	}
	first := a.lsp.goError == ""
	a.lsp.goError = message
	if first {
		a.notify("error", trf("%s cannot analyze this workspace", "gopls"), message)
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
	if a.lsp.goServer != nil || a.lsp.goStarting || a.snapshot.Root == "" || a.settingsUI.open {
		return
	}
	server := a.goLanguageServerSettings()
	if server.Disabled {
		return
	}
	profile, _ := languageServerProfileByID(goLanguageServerID)
	if !a.languageServerExecutableAvailable(profile) {
		a.lsp.goMissing = true
		a.lsp.goError = ""
		return
	}
	a.lsp.goMissing = false
	programs := languageServerExecutableCandidates(server.Path, "gopls")
	a.lsp.goSeq++
	sequence := a.lsp.goSeq
	launch := languageServerLaunch{
		Program: programs[0], Arguments: append([]string(nil), server.Arguments...), Root: a.snapshot.Root,
		Notify: func(method string, params json.RawMessage) {
			a.handleGoLanguageServerNotification(sequence, method, params)
		},
	}
	a.lsp.goStarting = true
	a.lsp.goRestartPending = false
	a.lsp.goError = ""
	go func() {
		defer a.recoverBackground()
		connection, err := connectLanguageServerCandidates(launch, programs)
		a.updateChanged(func() bool {
			if sequence != a.lsp.goSeq || a.snapshot.Root != launch.Root {
				if connection != nil {
					go connection.Close()
				}
				return false
			}
			before := a.codeLanguageServerStatuses()
			a.lsp.goStarting = false
			if err != nil {
				a.lsp.goError = safeError(err)
				return a.codeLanguageServerPresentationChanged(before)
			}
			a.lsp.goServer = connection
			a.lsp.goDocs = make(map[string]uint64)
			a.watchGoLanguageServer(connection, sequence)
			return a.codeLanguageServerPresentationChanged(before)
		})
	}()
}

func (a *app) watchGoLanguageServer(connection languageServerConnection, sequence uint64) {
	go func() {
		err, ok := <-connection.Done()
		if !ok {
			err = nil
		}
		a.updateChanged(func() bool {
			if sequence != a.lsp.goSeq || a.lsp.goServer != connection {
				return false
			}
			before := a.codeLanguageServerStatuses()
			a.lsp.goServer = nil
			a.lsp.goDocs = nil
			if err != nil {
				a.lsp.goError = safeError(err)
			} else {
				a.lsp.goError = "language server stopped unexpectedly"
			}
			if !a.lsp.goRestartUsed {
				a.lsp.goRestartUsed = true
				a.lsp.goRestartPending = true
			}
			return a.codeLanguageServerPresentationChanged(before)
		})
	}()
}

func (a *app) stopGoLanguageServer() {
	a.lsp.stopGo()
}

func (a *app) syncGoLanguageServer() {
	if a.location.IsRemote() {
		a.stopGoLanguageServer()
		a.stopAdditionalLanguageServers()
		return
	}
	a.syncAdditionalLanguageServers()
	if a.settingsUI.open {
		return
	}
	server := a.goLanguageServerSettings()
	if server.Disabled || a.snapshot.Root == "" {
		a.stopGoLanguageServer()
		return
	}
	documents := make(map[string]*codeDocument)
	for path, document := range a.code.documents {
		if strings.EqualFold(filepath.Ext(path), ".go") && document != nil {
			// Loading previews still need their server retained. Otherwise each
			// replacement briefly looks like the workspace's last Go document.
			documents[path] = document
		}
	}
	if len(documents) == 0 {
		if a.lsp.goServer != nil {
			for path := range a.lsp.goDocs {
				a.lsp.goServer.DidClose(languageServerFileURI(a.workingFilePath(path)))
			}
		}
		a.stopGoLanguageServer()
		return
	}
	if a.lsp.goServer == nil {
		// A successful download or PATH change can make an executable usable
		// after we recorded it missing. Reconcile before deciding not to start.
		if a.lsp.goMissing {
			profile, _ := languageServerProfileByID(goLanguageServerID)
			if a.languageServerExecutableAvailable(profile) {
				a.lsp.goMissing = false
			}
		}
		if !a.lsp.goMissing && (a.lsp.goError == "" || a.lsp.goRestartPending) {
			a.startGoLanguageServer()
		}
		return
	}
	ready := false
	for _, document := range documents {
		if document.loaded && document.state != nil && document.state.Buffer != nil {
			ready = true
			break
		}
	}
	if !ready && len(a.lsp.goDocs) > 0 {
		return
	}
	// As with the additional servers, open the replacement before closing
	// its predecessor so gopls never sees a hollow preview-switching gap.
	for path, document := range documents {
		a.syncGoLanguageServerDocument(path, document)
	}
	for path := range a.lsp.goDocs {
		if documents[path] == nil {
			a.lsp.goServer.DidClose(languageServerFileURI(a.workingFilePath(path)))
			delete(a.lsp.goDocs, path)
		}
	}
}

func (a *app) syncGoLanguageServerDocument(path string, document *codeDocument) {
	if a.lsp.goServer == nil || document == nil || !document.loaded || document.state == nil || document.state.Buffer == nil {
		return
	}
	version := document.state.Buffer.Version()
	previous, opened := a.lsp.goDocs[path]
	uri := languageServerFileURI(a.workingFilePath(path))
	if !opened {
		a.lsp.goServer.DidOpen(uri, "go", document.state.Buffer.String(), version)
		a.lsp.goDocs[path] = version
	} else if previous != version {
		a.lsp.goServer.DidChange(uri, document.state.Buffer.String(), version)
		a.lsp.goDocs[path] = version
	}
}
