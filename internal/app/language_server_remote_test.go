package app

import (
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/workspace"
)

func TestRemoteDocumentsDoNotStartLocalLanguageServers(t *testing.T) {
	var launches, lookups, stats atomic.Int32
	originalConnect := connectLanguageServer
	originalLookPath := languageServerLookPath
	originalStat := languageServerStat
	connectLanguageServer = func(languageServerLaunch) (languageServerConnection, error) {
		launches.Add(1)
		return newFakeLanguageServerConnection(), nil
	}
	languageServerLookPath = func(program string) (string, error) {
		lookups.Add(1)
		return program, nil
	}
	languageServerStat = func(string) (os.FileInfo, error) {
		stats.Add(1)
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() {
		connectLanguageServer = originalConnect
		languageServerLookPath = originalLookPath
		languageServerStat = originalStat
	})

	for _, documentPath := range []string{"main.go", "src/main.rs", "src/main.ts"} {
		t.Run(documentPath, func(t *testing.T) {
			a := remoteLanguageServerTestApp(documentPath)
			a.code.files = []string{"Cargo.toml", "src/main.rs", "src/main.ts"}
			a.syncGoLanguageServer()

			if got := launches.Load(); got != 0 {
				t.Fatalf("remote %s caused %d local language-server launches", documentPath, got)
			}
			if got := lookups.Load(); got != 0 {
				t.Fatalf("remote %s caused %d local executable lookups", documentPath, got)
			}
			if got := stats.Load(); got != 0 {
				t.Fatalf("remote %s caused %d local manifest stats", documentPath, got)
			}
			if a.lsp.goServer != nil || len(a.lsp.additional) != 0 {
				t.Fatalf("remote synchronization retained servers: gopls=%v additional=%v",
					a.lsp.goServer, a.lsp.additional)
			}
		})
	}
}

func TestSwitchingFromLocalToRemoteStopsExistingLanguageServers(t *testing.T) {
	a := remoteLanguageServerTestApp("main.go")
	a.code.documents["src/main.rs"] = &codeDocument{path: "src/main.rs"}
	a.code.documents["src/main.ts"] = &codeDocument{path: "src/main.ts"}
	goConnection := newFakeLanguageServerConnection()
	a.lsp.goServer = goConnection
	a.lsp.goDocs = map[string]uint64{"main.go": 1}
	connections := []*fakeLanguageServerConnection{goConnection}
	for _, id := range []string{"rust", "typescript"} {
		profile, _ := languageServerProfileByID(id)
		connection := newFakeLanguageServerConnection()
		connections = append(connections, connection)
		a.lsp.additional[id] = &managedLanguageServer{
			profile: profile, connection: connection, starting: true,
			documents: map[string]uint64{"source": 1},
		}
	}

	a.syncGoLanguageServer()

	if a.lsp.goServer != nil || a.lsp.goStarting || len(a.lsp.goDocs) != 0 {
		t.Fatalf("remote switch retained gopls state: server=%v starting=%v documents=%v",
			a.lsp.goServer, a.lsp.goStarting, a.lsp.goDocs)
	}
	if len(a.lsp.additional) != 0 {
		t.Fatalf("remote switch retained additional servers: %v", a.lsp.additional)
	}
	for index, connection := range connections {
		select {
		case <-connection.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("connection %d was not closed after the local-to-remote switch", index)
		}
	}
}

func TestAdditionalLanguageServerSynchronizationAlsoRejectsRemoteDocuments(t *testing.T) {
	var lookups atomic.Int32
	originalLookPath := languageServerLookPath
	languageServerLookPath = func(program string) (string, error) {
		lookups.Add(1)
		return program, nil
	}
	t.Cleanup(func() { languageServerLookPath = originalLookPath })
	a := remoteLanguageServerTestApp("src/main.ts")
	a.syncAdditionalLanguageServers()
	if got := lookups.Load(); got != 0 {
		t.Fatalf("direct additional-server synchronization performed %d local lookups", got)
	}
	if len(a.lsp.additional) != 0 {
		t.Fatalf("direct additional-server synchronization created servers: %v", a.lsp.additional)
	}
}

func remoteLanguageServerTestApp(documentPath string) *app {
	const root = "/srv/remote/repository"
	return &app{
		snapshot: repository.Snapshot{Root: root},
		location: workspace.Location{Kind: workspace.SSH, ProfileID: "server", Root: root},
		code: codeState{documents: map[string]*codeDocument{
			documentPath: {path: documentPath},
		}},
		lsp: languageServerUIState{additional: make(map[string]*managedLanguageServer)},
		settings: appSettings{LanguageServers: map[string]languageServerSettings{
			"go": {}, "rust": {}, "typescript": {},
		}},
	}
}
