package app

import "context"

// sshUIState owns the transient SSH workspace picker, profile form and their
// asynchronous requests. Dependencies and session credentials stay outside
// this state: closing a form must not discard a shared opener or its secrets.
type sshUIState struct {
	browser sshBrowserState

	workspaceOpen          bool
	workspaceProfileID     string
	workspaceRoot          string
	workspaceError         string
	workspaceResolving     bool
	workspaceResolveSeq    uint64
	workspaceResolveCancel context.CancelFunc

	profileFormOpen   bool
	profileDraft      sshProfileDraft
	profileFormError  string
	profileFormNotice string
	profileTestBusy   bool
	profileTestSeq    uint64
	profileTestCancel context.CancelFunc

	// A handshake owns this prompt only until it receives the user's trust
	// decision or its open/test context is cancelled.
	hostKeyPrompt *sshHostKeyPrompt
}

func (s *sshUIState) cancelWorkspaceResolve() {
	s.workspaceResolveSeq++
	s.workspaceResolving = false
	if s.workspaceResolveCancel != nil {
		s.workspaceResolveCancel()
		s.workspaceResolveCancel = nil
	}
}

func (s *sshUIState) cancelProfileTest() {
	s.profileTestSeq++
	s.profileTestBusy = false
	if s.profileTestCancel != nil {
		s.profileTestCancel()
		s.profileTestCancel = nil
	}
}

func (s *sshUIState) clearProfileForm() {
	s.profileFormOpen = false
	s.profileDraft = sshProfileDraft{}
	s.profileFormError = ""
	s.profileFormNotice = ""
}

// resetProfileForm also drops credential drafts. Persistent profiles and the
// opener's session secret store are deliberately not part of this reset.
func (s *sshUIState) resetProfileForm() {
	s.cancelProfileTest()
	s.clearProfileForm()
}

func (s *sshUIState) resetWorkspacePicker() {
	s.cancel()
	s.clearProfileForm()
	s.workspaceOpen = false
	s.workspaceError = ""
}

func (s *sshUIState) cancel() {
	s.cancelWorkspaceResolve()
	s.cancelProfileTest()
}

func (s *sshUIState) reset() {
	s.resetWorkspacePicker()
}
