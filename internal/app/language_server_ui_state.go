package app

// languageServerUIState owns Settings drafts, catalog/install presentation and
// repository-bound language-server connections. Only resetRepository clears
// live connections; settings and install work may outlive a repository.
type languageServerUIState struct {
	goEnabled          bool
	goPath             string
	goArgs             string
	goFormatterCommand string
	drafts             map[string]*languageServerDraft

	testID     string
	testSeq    uint64
	testBusy   bool
	testOK     bool
	testResult string

	installPrompted map[string]bool
	installing      map[string]bool
	uninstalling    map[string]bool
	catalogQuery    string
	catalogSelected string
	customForm      customLanguageServerForm

	goServer         languageServerConnection
	goStarting       bool
	goMissing        bool
	goSeq            uint64
	goError          string
	goRestartPending bool
	goRestartUsed    bool
	goDocs           map[string]uint64
	additional       map[string]*managedLanguageServer
}

func (s *languageServerUIState) invalidateTest() {
	s.testSeq++
	s.testBusy = false
	s.testResult = ""
	s.testOK = false
}

func (s *languageServerUIState) stopGo() {
	if s.goServer == nil && !s.goStarting && !s.goMissing &&
		s.goError == "" && len(s.goDocs) == 0 {
		return
	}
	s.goSeq++
	connection := s.goServer
	s.goServer = nil
	s.goStarting = false
	s.goMissing = false
	s.goError = ""
	s.goRestartPending = false
	s.goRestartUsed = false
	s.goDocs = nil
	if connection != nil {
		go connection.Close()
	}
}

func stopManagedLanguageServerConnection(server *managedLanguageServer) {
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
	server.restartPending = false
	server.restartUsed = false
	server.documents = nil
	if connection != nil {
		go connection.Close()
	}
}

func (s *languageServerUIState) stopAdditional() {
	for id, server := range s.additional {
		stopManagedLanguageServerConnection(server)
		delete(s.additional, id)
	}
}

// resetRepository rejects stale starts, drops document versions and closes
// every connection without touching drafts, catalog choices or install jobs.
func (s *languageServerUIState) resetRepository() {
	s.stopGo()
	s.stopAdditional()
}
