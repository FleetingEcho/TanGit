package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

const remoteConflictText = "first\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nlast\n"

// remoteConflictHarness opens only the conflict pane, through an injected SSH
// filesystem. A real host file at the same POSIX path proves that neither local
// file seam is consulted for the remote read or writes.
func remoteConflictHarness(t *testing.T, client *remoteTestClient, root string) (*harness, *callRecorder, *int32, *int32) {
	t.Helper()
	filename := filepath.Join(root, "conflict.txt")
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte("unrelated local sentinel\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() { readWorkingFile, writeWorkingFile = originalRead, originalWrite })
	var localReads, localWrites int32
	readWorkingFile = func(name string) ([]byte, error) {
		atomic.AddInt32(&localReads, 1)
		return os.ReadFile(name)
	}
	writeWorkingFile = func(name string, data []byte) error {
		atomic.AddInt32(&localWrites, 1)
		return os.WriteFile(name, data, 0o644)
	}

	file := repository.File{Path: "conflict.txt", Status: "UU", Tracked: true, Unstaged: true, HasMergeConflicts: true}
	snapshot := repository.Snapshot{Root: root, Branch: "main", Files: []repository.File{file}}
	calls := &callRecorder{}
	a := newApp(fakeOpener{calls: calls, result: openResult{snapshot: snapshot}})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.state = stateReady
	a.workspaceTab = sectionChanges
	a.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: root}
	a.snapshot = snapshot
	a.session = fakeSession{snapshot: snapshot, calls: calls}
	a.changeRows = a.changeRowsFrom(snapshot.Files)
	a.diffKey = "conflict\x00conflict.txt"
	a.diffGeneration = 1
	a.diffLoading = true
	a.requestConflictFileRead(file, a.diffGeneration, true, client, nil)
	h.waitFor(t, "the remote conflict read", func() bool {
		return !a.diffLoading && a.conflictState != nil && a.conflictView != nil
	})
	if a.diffError != "" {
		t.Fatalf("remote conflict read failed: %s", a.diffError)
	}
	if got := a.conflictState.text(); got != remoteConflictText {
		t.Fatalf("remote conflict = %q, want the remote bytes", got)
	}
	if atomic.LoadInt32(&localReads) != 0 {
		t.Fatal("remote conflict read used the local readWorkingFile seam")
	}
	local, err := os.ReadFile(filename)
	if err != nil || string(local) != "unrelated local sentinel\n" {
		t.Fatalf("host file was disturbed by the remote read: %q %v", local, err)
	}
	return h, calls, &localReads, &localWrites
}

func seedRemoteConflict(client *remoteTestClient, root string) string {
	filename := root + "/conflict.txt"
	client.infos[filename] = remoteTestRegular(filename, remoteConflictText, 1)
	client.contents[filename] = []byte(remoteConflictText)
	return filename
}

func remoteConflictContents(t *testing.T, client *remoteTestClient, filename string) string {
	t.Helper()
	client.mu.Lock()
	defer client.mu.Unlock()
	return string(client.contents[filename])
}

func waitRemoteConflictSave(t *testing.T, h *harness) {
	t.Helper()
	h.waitFor(t, "the remote conflict save", func() bool {
		return !h.app.actionBusy && !h.app.conflictState.remoteSave.running
	})
}

func TestRemoteConflictReadsEditsAndWritesOnlyThroughSSH(t *testing.T) {
	client := newRemoteTestClient()
	root := t.TempDir()
	filename := seedRemoteConflict(client, root)
	h, _, localReads, localWrites := remoteConflictHarness(t, client, root)
	state := h.app.conflictState

	h.app.resolveConflictSide(conflictCurrent)
	waitRemoteConflictSave(t, h)
	if got, want := remoteConflictContents(t, client, filename), "first\nours\nlast\n"; got != want {
		t.Fatalf("Use current remotely wrote %q, want %q", got, want)
	}
	h.app.undoConflictResolution()
	waitRemoteConflictSave(t, h)
	if got := remoteConflictContents(t, client, filename); got != remoteConflictText {
		t.Fatalf("Undo remotely wrote %q, want the original", got)
	}
	h.app.resolveConflictSide(conflictIncoming)
	waitRemoteConflictSave(t, h)
	if got, want := remoteConflictContents(t, client, filename), "first\ntheirs\nlast\n"; got != want {
		t.Fatalf("Use incoming remotely wrote %q, want %q", got, want)
	}
	h.app.undoConflictResolution()
	waitRemoteConflictSave(t, h)
	h.app.resolveConflictSide(conflictBoth)
	waitRemoteConflictSave(t, h)
	if got, want := remoteConflictContents(t, client, filename), "first\nours\ntheirs\nlast\n"; got != want {
		t.Fatalf("Use both remotely wrote %q, want %q", got, want)
	}
	if state != h.app.conflictState || h.app.conflictView.ReadOnly {
		t.Fatal("asynchronous hunk actions did not restore the editable conflict pane")
	}

	h.app.conflictView.Insert("manual\n")
	h.app.syncConflictEdits()
	waitRemoteConflictSave(t, h)
	if got := remoteConflictContents(t, client, filename); !strings.Contains(got, "manual") || got != h.app.conflictState.text() {
		t.Fatalf("manual remote edit = %q, state = %q", got, h.app.conflictState.text())
	}
	if h.app.conflictView.Buffer.Modified() {
		t.Fatal("completed remote manual save left the saved buffer modified")
	}
	if atomic.LoadInt32(localReads) != 0 || atomic.LoadInt32(localWrites) != 0 {
		t.Fatalf("remote conflict used local seams: reads=%d writes=%d", *localReads, *localWrites)
	}
	local, err := os.ReadFile(filepath.Join(root, "conflict.txt"))
	if err != nil || string(local) != "unrelated local sentinel\n" {
		t.Fatalf("host file was overwritten by remote saves: %q %v", local, err)
	}
}

func TestRemoteConflictQueuesNewerTypedVersionAfterStampedSave(t *testing.T) {
	client := newRemoteTestClient()
	root := t.TempDir()
	filename := seedRemoteConflict(client, root)
	h, _, localReads, localWrites := remoteConflictHarness(t, client, root)
	started := make(chan struct{})
	release := make(chan struct{})
	var writes int
	client.writeFile = func(_ string, _ []byte, _ os.FileMode) error {
		client.mu.Lock()
		writes++
		current := writes
		client.mu.Unlock()
		if current == 1 {
			close(started)
			<-release
		}
		return nil
	}
	h.app.conflictView.Insert("first edit\n")
	h.app.syncConflictEdits()
	<-started
	h.app.conflictView.Insert("second edit\n")
	want := h.app.conflictView.Buffer.String()
	h.app.syncConflictEdits()
	if got := len(client.atomicWrites); got != 0 {
		t.Fatalf("blocked SSH write unexpectedly completed %d atomic replacements", got)
	}
	close(release)
	h.waitFor(t, "both serialized remote manual saves", func() bool {
		return !h.app.actionBusy && !h.app.conflictState.remoteSave.running && !h.app.conflictView.Buffer.Modified()
	})
	client.mu.Lock()
	writeCount := len(client.atomicWrites)
	client.mu.Unlock()
	if writeCount != 2 {
		t.Fatalf("newer typing caused %d saves, want two sequential stamped writes", writeCount)
	}
	if got := remoteConflictContents(t, client, filename); got != want {
		t.Fatalf("remote contents = %q, want latest editor version %q", got, want)
	}
	if atomic.LoadInt32(localReads) != 0 || atomic.LoadInt32(localWrites) != 0 {
		t.Fatal("queued remote saves touched the host filesystem")
	}
}

func TestRemoteConflictRejectsExternallyChangedFileAndRollsBackPick(t *testing.T) {
	client := newRemoteTestClient()
	root := t.TempDir()
	filename := seedRemoteConflict(client, root)
	h, _, localReads, localWrites := remoteConflictHarness(t, client, root)
	client.mu.Lock()
	client.infos[filename] = remoteTestRegular(filename, "externally resolved\n", 2)
	client.contents[filename] = []byte("externally resolved\n")
	client.mu.Unlock()

	h.app.resolveConflictSide(conflictCurrent)
	waitRemoteConflictSave(t, h)
	if len(client.atomicWrites) != 0 {
		t.Fatal("a stale remote conflict save reached atomic replacement")
	}
	if got := remoteConflictContents(t, client, filename); got != "externally resolved\n" {
		t.Fatalf("stale save overwrote external contents: %q", got)
	}
	if got := h.app.conflictState.text(); got != remoteConflictText || len(h.app.conflictState.regions) != 1 {
		t.Fatalf("failed remote pick did not roll back: %q %#v", got, h.app.conflictState.regions)
	}
	if h.app.conflictView.ReadOnly {
		t.Fatal("failed remote pick left the editor read-only")
	}
	if atomic.LoadInt32(localReads) != 0 || atomic.LoadInt32(localWrites) != 0 {
		t.Fatal("stale remote save used a local filesystem seam")
	}
}

func TestRemoteConflictSaveCompletionCannotRestoreAClosedPane(t *testing.T) {
	client := newRemoteTestClient()
	root := t.TempDir()
	filename := seedRemoteConflict(client, root)
	h, _, localReads, localWrites := remoteConflictHarness(t, client, root)
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	client.writeFile = func(_ string, _ []byte, _ os.FileMode) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}
	h.app.resolveConflictSide(conflictCurrent)
	<-started
	h.app.resetFileDiff()
	close(release)
	h.waitFor(t, "the retired remote save", func() bool { return !h.app.actionBusy })
	if h.app.conflictState != nil || h.app.conflictView != nil {
		t.Fatal("retired save recreated the closed conflict pane")
	}
	if got, want := remoteConflictContents(t, client, filename), "first\nours\nlast\n"; got != want {
		t.Fatalf("valid in-flight stamped write = %q, want %q", got, want)
	}
	if atomic.LoadInt32(localReads) != 0 || atomic.LoadInt32(localWrites) != 0 {
		t.Fatal("retired remote save used a local filesystem seam")
	}
}
