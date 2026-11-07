package app

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/workspace"
)

type shutdownTrackingSession struct {
	fakeSession
	closes atomic.Int32
}

func (s *shutdownTrackingSession) Close() error {
	s.closes.Add(1)
	return nil
}

func remoteShutdownEditor(h *harness, client *remoteTestClient) (*editor.State, *shutdownTrackingSession) {
	session := &shutdownTrackingSession{}
	h.app.session = session
	h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/repo"}
	h.app.snapshot.Root = "/srv/repo"
	filename := "/srv/repo/note.txt"
	client.infos[filename] = remoteTestRegular(filename, "original", 1)
	client.contents[filename] = []byte("original")
	state := &editor.State{Buffer: editor.NewBuffer("original")}
	state.Insert("unsaved")
	h.app.fileEditing = true
	h.app.fileEditKey = "note.txt"
	h.app.fileEditState = state
	h.app.code.remoteFileEdit.client = client
	h.app.code.remoteFileEdit.stamp = stampRemoteFile(client.infos[filename])
	h.app.code.remoteFileEdit.stampKnown = true
	return state, session
}

func TestPrepareShutdownJoinsRemoteSaveBeforeClosingSession(t *testing.T) {
	h := menuTestApp(t)
	client := newRemoteTestClient()
	state, session := remoteShutdownEditor(h, client)
	started := make(chan struct{})
	release := make(chan struct{})
	client.writeFile = func(string, []byte, os.FileMode) error {
		close(started)
		<-release
		return nil
	}

	results := make(chan bool, 1)
	h.app.closeRepository()
	h.app.prepareShutdown(func(ok bool) { results <- ok })
	<-started
	if session.closes.Load() != 0 || h.app.session != session || h.app.fileEditState != state {
		t.Fatal("close invalidated the editor or closed SSH before its save completed")
	}
	close(release)
	result := false
	h.waitFor(t, "the shared remote shutdown", func() bool {
		select {
		case result = <-results:
			return true
		default:
			return false
		}
	})
	if !result {
		t.Fatal("remote save reported failure")
	}
	client.mu.Lock()
	writes := len(client.atomicWrites)
	client.mu.Unlock()
	if writes != 1 {
		t.Fatalf("reentrant close started %d writes, want one shared save", writes)
	}
	if session.closes.Load() != 1 || h.app.session != nil || h.app.fileEditing || h.app.fileEditState != nil || h.app.fileEditKey != "" {
		t.Fatalf("successful close did not tear down exactly once: closes=%d session=%v", session.closes.Load(), h.app.session)
	}
}

func TestPrepareShutdownFailureKeepsRemoteWorkspaceAlive(t *testing.T) {
	h := menuTestApp(t)
	client := newRemoteTestClient()
	state, session := remoteShutdownEditor(h, client)
	client.writeFile = func(string, []byte, os.FileMode) error {
		return errors.New("remote disk full")
	}

	result := make(chan bool, 1)
	h.app.prepareShutdown(func(ok bool) { result <- ok })
	completed := false
	h.waitFor(t, "the failed remote shutdown", func() bool {
		select {
		case ok := <-result:
			completed = true
			if ok {
				t.Fatal("failed remote save allowed shutdown")
			}
			return true
		default:
			return false
		}
	})
	if !completed {
		t.Fatal("missing remote shutdown result")
	}
	if session.closes.Load() != 0 || h.app.session != session || h.app.state != stateReady {
		t.Fatal("failed close reset the repository or closed its session")
	}
	if !h.app.fileEditing || h.app.fileEditState != state || !state.Buffer.Modified() || h.app.code.shutdown.running {
		t.Fatal("failed close lost the dirty editor or left shutdown stuck")
	}
}

func TestLateRemoteCodeSaveDoesNotInvalidateReplacement(t *testing.T) {
	h := menuTestApp(t)
	h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/repo"}
	h.app.snapshot.Root = "/srv/repo"
	client := newRemoteTestClient()
	filename := "/srv/repo/note.txt"
	client.infos[filename] = remoteTestRegular(filename, "original", 1)
	client.contents[filename] = []byte("original")
	started := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	release := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	var calls atomic.Int32
	client.writeFile = func(_ string, _ []byte, _ os.FileMode) error {
		index := int(calls.Add(1) - 1)
		if index < len(started) {
			close(started[index])
			<-release[index]
		}
		return nil
	}
	state := &editor.State{Buffer: editor.NewBuffer("original")}
	state.Insert("A")
	document := &codeDocument{
		path: "note.txt", state: state,
		remoteStamp: stampRemoteFile(client.infos[filename]), remoteStampKnown: true,
	}
	h.app.code.documents = map[string]*codeDocument{"note.txt": document}
	h.app.saveRemoteCodeDocumentWithClient(client, document, nil)
	<-started[0]

	// A lifecycle reset rejects A's completion. B then reuses the same document
	// and save state while A is still on the wire.
	document.generation++
	document.remoteSave.invalidate()
	state.Insert("B")
	bDone, bOK := false, false
	h.app.saveRemoteCodeDocumentWithClient(client, document, func(ok bool) {
		bDone, bOK = true, ok
	})
	<-started[1]
	sequence := document.remoteSave.sequence
	close(release[0])
	h.updates.waitForUpdate(t)
	if bDone || !document.remoteSave.running || document.remoteSave.sequence != sequence {
		t.Fatal("late A completion canceled replacement save B")
	}
	close(release[1])
	h.waitFor(t, "replacement remote save B", func() bool { return bDone })
	if !bOK || state.Buffer.Modified() {
		t.Fatalf("replacement save did not complete and MarkSaved: done=%v ok=%v modified=%v", bDone, bOK, state.Buffer.Modified())
	}
}

func TestSSHLifecycleModalsOwnKeyboard(t *testing.T) {
	for _, test := range []struct {
		name string
		open func(*app)
	}{
		{name: "workspace picker", open: func(a *app) { a.sshWorkspaceOpen = true }},
		{name: "profile form", open: func(a *app) { a.sshProfileFormOpen = true }},
		{name: "host-key prompt", open: func(a *app) { a.sshHostKeyPrompt = &sshHostKeyPrompt{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApp(fakeOpener{})
			test.open(a)
			if !a.modalOpen() {
				t.Fatal("SSH modal did not own the keyboard")
			}
		})
	}
}

func TestPrepareShutdownCleansUpSSHModalsAndOperations(t *testing.T) {
	a := newApp(fakeOpener{})
	resolveCtx, resolveCancel := context.WithCancel(context.Background())
	testCtx, testCancel := context.WithCancel(context.Background())
	a.sshWorkspaceOpen = true
	a.sshWorkspaceResolving = true
	a.sshWorkspaceResolveCancel = resolveCancel
	a.sshProfileFormOpen = true
	a.sshProfileTestBusy = true
	a.sshProfileTestCancel = testCancel
	prompt := &sshHostKeyPrompt{response: make(chan bool, 1)}
	a.sshHostKeyPrompt = prompt
	if !a.modalOpen() {
		t.Fatal("SSH picker/profile/host-key modal was not detected")
	}

	called := false
	a.prepareShutdown(func(ok bool) {
		called = true
		if !ok {
			t.Error("local preparation failed")
		}
	})
	if !called {
		t.Fatal("local preparation was not synchronous")
	}
	if resolveCtx.Err() == nil || testCtx.Err() == nil {
		t.Fatal("workspace resolution/profile test was not canceled")
	}
	select {
	case accepted := <-prompt.response:
		if accepted {
			t.Fatal("shutdown trusted the host key")
		}
	case <-time.After(time.Second):
		t.Fatal("host-key prompt was not answered")
	}
	if a.sshWorkspaceOpen || a.sshProfileFormOpen || a.sshHostKeyPrompt != nil || a.modalOpen() {
		t.Fatal("SSH forms or prompts survived shutdown preparation")
	}
}
