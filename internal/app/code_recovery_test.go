package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
)

func TestCodeRecoveryInputCacheSkipsUnchangedFrames(t *testing.T) {
	a := &app{}
	state := &editor.State{Buffer: editor.NewBuffer("base")}
	state.Insert("changed")
	a.code.documents = map[string]*codeDocument{"main.go": {
		path: "main.go", state: state, localStamp: stampLocalFile([]byte("base")), localStampKnown: true,
	}}
	if !a.codeRecoveryInputChanged() {
		t.Fatal("initial dirty buffer did not invalidate recovery input")
	}
	if a.codeRecoveryInputChanged() {
		t.Fatal("unchanged buffer rematerialized recovery input")
	}
	state.Insert("!")
	if !a.codeRecoveryInputChanged() || a.codeRecoveryInputChanged() {
		t.Fatal("buffer version did not invalidate exactly one recovery snapshot")
	}
	a.code.recoveryEntriesSeq++
	if !a.codeRecoveryInputChanged() || a.codeRecoveryInputChanged() {
		t.Fatal("retained recovery entries did not invalidate exactly one snapshot")
	}
}

func TestCodeRecoveryWritesCannotFinishOutOfOrder(t *testing.T) {
	original := codeRecoveryWriter
	defer func() { codeRecoveryWriter = original }()
	calls := make(chan string, 3)
	releaseOld := make(chan struct{})
	codeRecoveryWriter = func(_ string, recovery codeRecoveryFile) error {
		path := recovery.Entries[0].Path
		calls <- path
		if path == "old" {
			<-releaseOld
		}
		return nil
	}
	coordinator := codeRecoveryWriteCoordinator{}
	results := make(chan error, 3)
	write := func(path string) {
		permit := coordinator.next("recovery.json")
		go func() {
			results <- coordinator.write(permit, codeRecoveryFile{Entries: []codeRecoveryEntry{{Path: path}}})
		}()
	}
	write("old")
	if path := <-calls; path != "old" {
		t.Fatalf("first recovery write = %q", path)
	}
	write("retired")
	write("new")
	deadline := time.Now().Add(time.Second)
	for {
		coordinator.mu.Lock()
		latest := coordinator.gates["recovery.json"].latest
		coordinator.mu.Unlock()
		if latest == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("replacement writes were not scheduled")
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseOld)
	for range 3 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("coordinated recovery write did not complete")
		}
	}
	if path := <-calls; path != "new" {
		t.Fatalf("durable recovery write = %q, want newest snapshot", path)
	}
	select {
	case path := <-calls:
		t.Fatalf("retired recovery snapshot %q reached disk", path)
	default:
	}
}

func TestCodeRecoveryWritesAreIndependentPerWorkspace(t *testing.T) {
	original := codeRecoveryWriter
	defer func() { codeRecoveryWriter = original }()
	var calls []string
	codeRecoveryWriter = func(filename string, _ codeRecoveryFile) error {
		calls = append(calls, filename)
		return nil
	}
	coordinator := codeRecoveryWriteCoordinator{}
	first := coordinator.next("workspace-a.json")
	second := coordinator.next("workspace-b.json")
	if err := coordinator.write(first, codeRecoveryFile{}); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.write(second, codeRecoveryFile{}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "workspace-a.json" || calls[1] != "workspace-b.json" {
		t.Fatalf("independent recovery writes = %#v; one workspace retired another", calls)
	}
}

func TestCodeRecoveryRoundTripAndRemoval(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "workspace")
	filename := codeRecoveryPath(dir, root)
	recovery := codeRecoveryFile{Version: codeRecoveryVersion, Root: root, Entries: []codeRecoveryEntry{{
		Path: "main.go", Stamp: stampLocalFile([]byte("base")), Content: []byte("changed"),
	}}}
	if err := writeCodeRecovery(filename, recovery); err != nil {
		t.Fatal(err)
	}
	got, err := readCodeRecovery(filename, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || string(got.Entries[0].Content) != "changed" || got.Entries[0].Stamp != recovery.Entries[0].Stamp {
		t.Fatalf("recovery = %+v", got)
	}
	if err := writeCodeRecovery(filename, codeRecoveryFile{Version: codeRecoveryVersion, Root: root}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filename); !os.IsNotExist(err) {
		t.Fatalf("recovery file remains: %v", err)
	}
}

func TestReadCodeRecoveryRejectsAnotherWorkspace(t *testing.T) {
	dir := t.TempDir()
	filename := filepath.Join(dir, "recovery.json")
	if err := writeCodeRecovery(filename, codeRecoveryFile{Version: codeRecoveryVersion, Root: "/one", Entries: []codeRecoveryEntry{{Path: "a", Content: []byte("x")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := readCodeRecovery(filename, "/two"); err == nil {
		t.Fatal("accepted another workspace")
	}
}

func TestCodeRecoveryRoundTripPreservesNonUTF8(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "workspace")
	filename := codeRecoveryPath(dir, root)
	content := []byte{0xff, 'x', '\n'}
	recovery := codeRecoveryFile{Version: codeRecoveryVersion, Root: root, Entries: []codeRecoveryEntry{{
		Path: "main.go", Stamp: stampLocalFile([]byte("base")), Content: content,
	}}}
	if err := writeCodeRecovery(filename, recovery); err != nil {
		t.Fatal(err)
	}
	got, err := readCodeRecovery(filename, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || !bytes.Equal(got.Entries[0].Content, content) {
		t.Fatalf("non-UTF-8 recovery changed bytes: got %v want %v", got.Entries[0].Content, content)
	}
}

func TestCodeRecoveryReadFailureBlocksWrites(t *testing.T) {
	h := menuTestApp(t)
	root := t.TempDir()
	h.app.snapshot.Root = root
	h.app.pathUserData = t.TempDir()
	filename := codeRecoveryPath(h.app.pathUserData, root)
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	damaged := []byte(`{"version":999,"root":"future","entries":[]}`)
	if err := os.WriteFile(filename, damaged, 0o600); err != nil {
		t.Fatal(err)
	}

	h.app.syncCodeRecovery()
	h.updates.waitForUpdate(t)
	if h.app.code.recoveryLoaded {
		t.Fatal("an unreadable recovery file was marked as loaded")
	}
	sequence := h.app.code.recoverySeq
	h.app.syncCodeRecovery()
	if h.app.code.recoverySeq != sequence {
		t.Fatal("an unreadable recovery file allowed a replacement write to be scheduled")
	}
	got, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, damaged) {
		t.Fatalf("unreadable recovery was overwritten: got %q want %q", got, damaged)
	}
}

func TestCodeRecoveryWriteFailureRetriesUnchangedBuffer(t *testing.T) {
	h := menuTestApp(t)
	h.app.snapshot.Root = t.TempDir()
	h.app.pathUserData = t.TempDir()
	h.app.code.recoveryRoot = h.app.snapshot.Root
	h.app.code.recoveryLoaded = true
	state := &editor.State{Buffer: editor.NewBuffer("base")}
	state.Insert("changed")
	h.app.code.documents = map[string]*codeDocument{"main.go": {
		path: "main.go", state: state, localStamp: stampLocalFile([]byte("base")), localStampKnown: true,
	}}
	if err := os.WriteFile(filepath.Join(h.app.pathUserData, "editor-recovery"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	h.app.syncCodeRecovery()
	h.waitFor(t, "the failed recovery write to become retryable", func() bool {
		return h.app.code.recoverySignature == (localFileStamp{})
	})
	if h.app.code.recoverySignature != (localFileStamp{}) {
		t.Fatal("failed recovery write was treated as durable")
	}
	sequence := h.app.code.recoverySeq
	h.app.syncCodeRecovery()
	if h.app.code.recoverySeq == sequence {
		t.Fatal("unchanged dirty buffer was not scheduled for retry")
	}
	h.app.code.recoverySeq++
	h.updates.waitForUpdate(t)
}
