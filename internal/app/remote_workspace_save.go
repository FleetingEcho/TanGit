package app

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/egoist/mygo/ui/editor"
)

var errRemoteSaveSuperseded = errors.New("remote save superseded by a newer write")

type remoteWriteKey struct {
	client string
	root   string
	name   string
}

type remoteWriteGate struct {
	mu         sync.Mutex
	latest     uint64
	refs       uint64
	stamp      remoteFileStamp
	stampKnown bool
}

type remoteWritePermit struct {
	key      remoteWriteKey
	gate     *remoteWriteGate
	sequence uint64
}

// remoteWriteCoordinator outlives any individual codeDocument. Lifecycle
// invalidation retires UI completion callbacks, but it cannot stop an SFTP
// write already in flight; physical writes therefore need their own ordering.
type remoteWriteCoordinator struct {
	mu    sync.Mutex
	gates map[remoteWriteKey]*remoteWriteGate
}

func remoteWriteClientIdentity(client remoteWorkspaceClient) string {
	value := reflect.ValueOf(client)
	if value.IsValid() && value.Kind() == reflect.Pointer {
		return fmt.Sprintf("%T:%x", client, value.Pointer())
	}
	return fmt.Sprintf("%T:%v", client, client)
}

func (c *remoteWriteCoordinator) begin(client remoteWorkspaceClient, root, name string) remoteWritePermit {
	key := remoteWriteKey{client: remoteWriteClientIdentity(client), root: root, name: name}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gates == nil {
		c.gates = make(map[remoteWriteKey]*remoteWriteGate)
	}
	gate := c.gates[key]
	if gate == nil {
		gate = &remoteWriteGate{}
		c.gates[key] = gate
	}
	gate.latest++
	gate.refs++
	return remoteWritePermit{key: key, gate: gate, sequence: gate.latest}
}

// write runs at most one physical write for a path. If scheduling let a newer
// request obtain the lock first, the older request exits without writing. If
// the older request was already writing, the newer one waits and writes last.
func (c *remoteWriteCoordinator) write(permit remoteWritePermit, client remoteWorkspaceClient, root, name string, content []byte, previous remoteFileStamp, previousKnown bool) (remoteFileStamp, error) {
	defer c.release(permit)
	permit.gate.mu.Lock()
	defer permit.gate.mu.Unlock()
	c.mu.Lock()
	current := permit.gate.latest == permit.sequence
	c.mu.Unlock()
	if !current {
		return remoteFileStamp{}, errRemoteSaveSuperseded
	}
	// A replacement request may have captured its optimistic-lock stamp
	// before the last physical write committed. Even if intermediate saves
	// were superseded, the last successful stamp is the correct base for the
	// latest save. The optimistic check still rejects unrelated remote edits.
	if permit.gate.stampKnown {
		previous, previousKnown = permit.gate.stamp, true
	}
	stamp, err := writeRemoteWorkingFile(client, root, name, content, previous, previousKnown)
	if err == nil {
		permit.gate.stamp, permit.gate.stampKnown = stamp, true
	}
	return stamp, err
}

func (c *remoteWriteCoordinator) release(permit remoteWritePermit) {
	c.mu.Lock()
	defer c.mu.Unlock()
	permit.gate.refs--
	if permit.gate.refs == 0 && c.gates[permit.key] == permit.gate {
		delete(c.gates, permit.key)
	}
}

// remoteSaveState is manipulated only on the UI thread. sequence retires
// completions after a reload, close, rename, or repository switch. version is
// the buffer version captured by the request currently on the wire.
type remoteSaveState struct {
	sequence  uint64
	running   bool
	pending   bool
	version   uint64
	callbacks []func(bool)
}

func (s *remoteSaveState) invalidate() {
	s.sequence++
	s.running = false
	s.pending = false
	s.version = 0
	callbacks := s.callbacks
	s.callbacks = nil
	for _, callback := range callbacks {
		callback(false)
	}
}

func (a *app) requestRemoteCodeDocument(document *codeDocument) {
	if document == nil || document.loading || a.snapshot.Root == "" {
		return
	}
	if document.state == nil {
		document.state = newCodeEditorState(document.path, nil)
	}
	client, err := a.remoteWorkspaceClient()
	if err != nil {
		document.loadError = safeError(err)
		return
	}
	invalidateCodeBlame(document)
	document.remoteSave.invalidate()
	root, name := a.snapshot.Root, document.path
	document.generation++
	token, generation := document.generation, a.generation
	document.loading = true
	document.binary = false
	document.imagePreview = nil
	document.loadError = ""
	document.saveError = ""
	document.remoteStampKnown = false

	go func() {
		defer a.recoverBackground()
		content, stamp, readErr := readRemoteWorkingFile(client, root, name)
		binary := readErr == nil && bytes.IndexByte(content, 0) >= 0
		var preview *mediaPreview
		if readErr == nil {
			if binary || isMediaPath(name) {
				decoded := decodeMediaData(content)
				if decoded.bitmap != nil {
					preview = &decoded
					binary = true
				}
			}
		}
		a.updateChanged(func() bool {
			current := a.code.documents[name]
			if generation != a.generation || current != document || token != document.generation {
				return false
			}
			document.loading = false
			if readErr != nil {
				document.loadError = safeError(readErr)
				return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
			}
			document.remoteStamp, document.remoteStampKnown = stamp, true
			if binary {
				document.binary = true
				document.imagePreview = preview
				document.loaded = true
				return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
			}
			position, scrollY := document.state.CursorPos(), document.state.ScrollY()
			document.state.Buffer.Reset(string(content))
			document.state.Buffer.SetLanguage(editor.LanguageFor(name))
			document.state.GoTo(position)
			document.state.SetScrollY(scrollY)
			document.loaded = true
			// LSP processes, file:// URIs, and local hover/definition handlers are
			// intentionally not configured for an SSH workspace.
			document.state.OnHover = nil
			document.state.OnDefinition = nil
			document.state.OnCompletion = nil
			a.configureCodeBlame(document)
			a.revealCodeLine(document)
			a.requestCodeBlame(document)
			return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
		})
	}()
}

func (a *app) saveRemoteCodeDocument(document *codeDocument, done func(bool)) {
	a.saveRemoteCodeDocumentWithClient(nil, document, done)
}

func (a *app) saveRemoteCodeDocumentWithClient(client remoteWorkspaceClient, document *codeDocument, done func(bool)) {
	if document != nil && document.readOnly {
		if done != nil {
			done(true)
		}
		return
	}
	if a.code.fileOperationBusy || document == nil || document.state == nil || document.state.Buffer == nil {
		if done != nil {
			done(false)
		}
		return
	}
	if !document.state.Buffer.Modified() {
		if document.remoteSave.running || document.remoteSave.pending {
			if done != nil {
				document.remoteSave.callbacks = append(document.remoteSave.callbacks, done)
			}
			return
		}
		if done != nil {
			done(true)
		}
		return
	}
	if document.remoteSave.running {
		if done != nil {
			document.remoteSave.callbacks = append(document.remoteSave.callbacks, done)
		}
		if document.remoteSave.version != document.state.Buffer.Version() {
			document.remoteSave.pending = true
		}
		return
	}

	var err error
	if client == nil {
		client, err = a.remoteWorkspaceClient()
	}
	if err != nil {
		document.saveError = safeError(err)
		if done != nil {
			done(false)
		}
		return
	}
	var content bytes.Buffer
	if _, err := document.state.Buffer.WriteTo(&content); err != nil {
		document.saveError = safeError(err)
		if done != nil {
			done(false)
		}
		return
	}
	root, name := a.snapshot.Root, document.path
	state := document.state
	version := state.Buffer.Version()
	generation, documentGeneration := a.generation, document.generation
	previous, previousKnown := document.remoteStamp, document.remoteStampKnown
	document.remoteSave.sequence++
	sequence := document.remoteSave.sequence
	document.remoteSave.running = true
	document.remoteSave.pending = false
	document.remoteSave.version = version
	document.saveError = ""
	if done != nil {
		document.remoteSave.callbacks = append(document.remoteSave.callbacks, done)
	}
	data := append([]byte(nil), content.Bytes()...)
	writePermit := a.remoteWrites.begin(client, root, name)

	go func() {
		defer a.recoverBackground()
		stamp, writeErr := a.remoteWrites.write(writePermit, client, root, name, data, previous, previousKnown)
		a.updateChanged(func() bool {
			current := a.code.documents[name]
			if generation != a.generation || documentGeneration != document.generation || sequence != document.remoteSave.sequence || current != document {
				// A lifecycle reset has already retired this request. Never clear
				// the replacement request that may now share the same save state.
				return false
			}
			document.remoteSave.running = false
			if writeErr != nil {
				document.remoteSave.pending = false
				document.saveError = safeError(writeErr)
				callbacks := document.remoteSave.callbacks
				document.remoteSave.callbacks = nil
				for _, callback := range callbacks {
					callback(false)
				}
				return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
			}
			document.remoteStamp, document.remoteStampKnown = stamp, true
			unchanged := document.state == state && state.Buffer.Version() == version
			if unchanged {
				state.Buffer.MarkSaved()
				document.branchNotice = ""
			}
			newer := document.state != nil && document.state.Buffer != nil && document.state.Buffer.Modified() && document.state.Buffer.Version() != version
			if newer {
				document.remoteSave.pending = false
				document.saveError = ""
				a.saveRemoteCodeDocumentWithClient(client, document, nil)
				return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
			}
			document.saveError = ""
			if unchanged {
				a.requestCodeBlame(document)
				a.invalidateCodeSearchIndex()
			}
			callbacks := document.remoteSave.callbacks
			document.remoteSave.callbacks = nil
			for _, callback := range callbacks {
				callback(!codeDocumentModified(document))
			}
			return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
		})
	}()
}

func (a *app) saveRemoteCodeDocuments(documents []*codeDocument, done func(bool)) {
	if len(documents) == 0 {
		done(true)
		return
	}
	document := documents[0]
	if a.code.documents[document.path] != document {
		done(false)
		return
	}
	a.saveRemoteCodeDocument(document, func(ok bool) {
		if !ok {
			done(false)
			return
		}
		a.saveRemoteCodeDocuments(documents[1:], done)
	})
}

func (a *app) requestRemoteFileEdit(name string) {
	client, err := a.remoteWorkspaceClient()
	if err != nil {
		a.fileEditLoading = false
		a.fileEditError = safeError(err)
		return
	}
	a.code.remoteFileEdit.save.invalidate()
	a.code.remoteFileEdit.client = client
	a.code.remoteFileEdit.stampKnown = false
	a.code.remoteFileEdit.closing = false
	a.fileEditToken++
	token, generation := a.fileEditToken, a.generation
	root := a.snapshot.Root
	a.fileEditKey = name
	a.fileEditLoading = true
	a.fileEditBinary = false
	a.fileEditPreview = nil
	a.fileEditError = ""
	a.fileEditSaveError = ""
	a.fileEditTriedVersion = 0
	a.fileEditState = nil

	go func() {
		defer a.recoverBackground()
		content, stamp, readErr := readRemoteWorkingFile(client, root, name)
		binary := readErr == nil && bytes.IndexByte(content, 0) >= 0
		var preview *mediaPreview
		var state *editor.State
		if readErr == nil {
			if binary || isMediaPath(name) {
				decoded := decodeMediaData(content)
				if decoded.bitmap != nil {
					preview = &decoded
					binary = true
				}
			}
			if !binary {
				state = newCodeEditorState(name, content)
			}
		}
		a.update(func() {
			if generation != a.generation || token != a.fileEditToken || !a.fileEditing {
				return
			}
			a.fileEditLoading = false
			if readErr != nil {
				a.fileEditError = safeError(readErr)
				return
			}
			a.code.remoteFileEdit.stamp, a.code.remoteFileEdit.stampKnown = stamp, true
			if binary {
				a.fileEditBinary = true
				a.fileEditPreview = preview
				return
			}
			a.fileEditState = state
		})
	}()
}

func (a *app) saveRemoteFileEdits(done func(bool)) {
	a.saveRemoteFileEditsWithClient(nil, done)
}

func (a *app) saveRemoteFileEditsWithClient(client remoteWorkspaceClient, done func(bool)) {
	state := a.fileEditState
	if state == nil || state.Buffer == nil {
		if done != nil {
			done(true)
		}
		return
	}
	save := &a.code.remoteFileEdit.save
	version := state.Buffer.Version()
	if !state.Buffer.Modified() {
		if save.running || save.pending {
			if done != nil {
				save.callbacks = append(save.callbacks, done)
			}
			return
		}
		if done != nil {
			done(true)
		}
		return
	}
	if a.fileEditSaveError != "" && version == a.fileEditTriedVersion && !save.running {
		if done != nil {
			done(false)
		}
		return
	}
	if save.running {
		if done != nil {
			save.callbacks = append(save.callbacks, done)
		}
		if save.version != version {
			save.pending = true
		}
		return
	}
	var err error
	if client == nil {
		client, err = a.remoteWorkspaceClient()
	}
	if err != nil {
		a.fileEditSaveError = safeError(err)
		if done != nil {
			done(false)
		}
		return
	}
	var content bytes.Buffer
	if _, err := state.Buffer.WriteTo(&content); err != nil {
		a.fileEditSaveError = safeError(err)
		if done != nil {
			done(false)
		}
		return
	}
	name, root := a.fileEditKey, a.snapshot.Root
	token, generation := a.fileEditToken, a.generation
	previous, previousKnown := a.code.remoteFileEdit.stamp, a.code.remoteFileEdit.stampKnown
	save.sequence++
	sequence := save.sequence
	save.running = true
	save.pending = false
	save.version = version
	a.fileEditTriedVersion = version
	a.fileEditSaveError = ""
	if done != nil {
		save.callbacks = append(save.callbacks, done)
	}
	data := append([]byte(nil), content.Bytes()...)

	go func() {
		defer a.recoverBackground()
		stamp, writeErr := writeRemoteWorkingFile(client, root, name, data, previous, previousKnown)
		a.updateChanged(func() bool {
			if generation != a.generation || token != a.fileEditToken || sequence != save.sequence ||
				!a.fileEditing || a.fileEditKey != name || a.fileEditState != state {
				// Invalidation belongs to the lifecycle operation itself. A late
				// completion must not cancel callbacks for its replacement save.
				return false
			}
			save.running = false
			if writeErr != nil {
				save.pending = false
				a.fileEditSaveError = safeError(writeErr)
				callbacks := save.callbacks
				save.callbacks = nil
				for _, callback := range callbacks {
					callback(false)
				}
				return true
			}
			a.code.remoteFileEdit.stamp, a.code.remoteFileEdit.stampKnown = stamp, true
			unchanged := state.Buffer.Version() == version
			if unchanged {
				state.Buffer.MarkSaved()
			}
			if state.Buffer.Modified() && state.Buffer.Version() != version {
				save.pending = false
				a.fileEditSaveError = ""
				a.saveRemoteFileEditsWithClient(client, nil)
				return true
			}
			a.fileEditSaveError = ""
			callbacks := save.callbacks
			save.callbacks = nil
			for _, callback := range callbacks {
				callback(!state.Buffer.Modified())
			}
			return true
		})
	}()
}

func (a *app) closeRemoteFileEdit() {
	if a.code.remoteFileEdit.closing {
		return
	}
	a.code.remoteFileEdit.closing = true
	a.saveRemoteFileEdits(func(ok bool) {
		a.code.remoteFileEdit.closing = false
		if !ok {
			return
		}
		a.resetFileEdit()
		a.resetFileDiff()
	})
}
