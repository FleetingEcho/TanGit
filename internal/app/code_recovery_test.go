package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

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
