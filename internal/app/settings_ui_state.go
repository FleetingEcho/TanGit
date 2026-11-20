package app

import "github.com/egoist/mygo/ui"

// settingsUIState owns transient Settings controls and the decoded Home
// background. Persisted appSettings and settingsLoaded remain separate.
type settingsUIState struct {
	open                      bool
	page                      int
	revealShortcuts           bool
	conflictActionStyleChoice int
	autoFetchChoice           int

	customEditorFormOpen    bool
	customEditorFormName    string
	customEditorFormCommand string
	customEditorFormError   string

	customCommandFormOpen     bool
	customCommandFormEdit     int
	customCommandFormName     string
	customCommandFormCommand  string
	customCommandFormKey      string
	customCommandFormContexts []string
	customCommandFormError    string

	searchExcludedDirectoryDraft string
	customAccent                 ui.Color

	homeBackground             *ui.Bitmap
	homeBackgroundPath         string
	homeBackgroundPending      string
	homeBackgroundFailed       string
	homeBackgroundGeneration   uint64
	homeBackgroundSelectionSeq uint64
}

func (s *settingsUIState) closeCustomEditorForm() {
	s.customEditorFormOpen = false
	s.customEditorFormName = ""
	s.customEditorFormCommand = ""
	s.customEditorFormError = ""
}

// close keeps the selected page and custom-command draft, matching the
// existing modal behavior, while retiring controls scoped to this opening.
func (s *settingsUIState) close() {
	s.open = false
	s.revealShortcuts = false
	s.closeCustomEditorForm()
}

func (s *settingsUIState) closeCustomCommandForm() {
	s.customCommandFormOpen = false
	s.customCommandFormError = ""
}

// invalidateHomeBackground rejects every unfinished picker and decoder. The
// persisted image path is intentionally independent of this transient reset.
func (s *settingsUIState) invalidateHomeBackground() {
	s.homeBackgroundSelectionSeq++
	s.homeBackgroundGeneration++
	s.homeBackgroundPending = ""
	s.homeBackgroundFailed = ""
}

func (s *settingsUIState) clearHomeBackground() {
	s.invalidateHomeBackground()
	s.homeBackground = nil
	s.homeBackgroundPath = ""
}
