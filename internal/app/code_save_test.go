package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestCodeSaveRefusesExternalFileChanges(t *testing.T) {
	files := map[string]string{"note.txt": "original\n"}
	written := stubCodeWorkspace(t, files)
	h := menuTestApp(t)
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("note.txt", 0)
	document := waitForCodeDocument(t, h, "note.txt")
	document.state.Insert("local\n")
	files["note.txt"] = "external\n"

	h.app.saveCodeDocument(document)

	if document.saveError != errLocalFileModified.Error() {
		t.Fatalf("save error = %q, want %q", document.saveError, errLocalFileModified)
	}
	if len(written) != 0 || files["note.txt"] != "external\n" {
		t.Fatalf("conflicting save wrote %q over external contents %q", written["note.txt"], files["note.txt"])
	}
	if !document.state.Buffer.Modified() {
		t.Fatal("conflicting save cleared the dirty buffer")
	}
}

func TestCodeSaveTracksTheLastSuccessfulContents(t *testing.T) {
	files := map[string]string{"note.txt": "original\n"}
	stubCodeWorkspace(t, files)
	h := menuTestApp(t)
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("note.txt", 0)
	document := waitForCodeDocument(t, h, "note.txt")

	document.state.Insert("first\n")
	h.app.saveCodeDocument(document)
	document.state.Insert("second\n")
	h.app.saveCodeDocument(document)

	if document.saveError != "" || document.state.Buffer.Modified() || !strings.Contains(files["note.txt"], "second") {
		t.Fatalf("second save failed: error=%q modified=%v contents=%q", document.saveError, document.state.Buffer.Modified(), files["note.txt"])
	}
}

func TestCodeSaveDetectsSameSizeExternalReplacement(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "note.txt")
	if err := os.WriteFile(filename, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := menuTestApp(t)
	h.app.snapshot.Root = directory
	h.app.code.documents = map[string]*codeDocument{}
	h.app.openCodeFile("note.txt", 0)
	document := waitForCodeDocument(t, h, "note.txt")
	document.state.Insert("local\n")
	if err := os.WriteFile(filename, []byte("after!\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.app.saveCodeDocument(document)

	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "after!\n" || document.saveError != errLocalFileModified.Error() {
		t.Fatalf("contents=%q error=%q", contents, document.saveError)
	}
}

func TestCodeSaveConflictCanReloadOrOverwrite(t *testing.T) {
	for _, action := range []string{"Reload", "Overwrite"} {
		t.Run(action, func(t *testing.T) {
			files := map[string]string{"note.txt": "original\n"}
			stubCodeWorkspace(t, files)
			h := menuTestApp(t)
			openCodePageForTest(t, h, 1)
			h.app.openCodeFile("note.txt", 0)
			document := waitForCodeDocument(t, h, "note.txt")
			document.state.GoTo(editor.Pos{Line: 0, Col: 4})
			document.state.Insert("local\n")
			local := document.state.Buffer.String()
			files["note.txt"] = "external\n"

			h.app.saveCodeDocument(document)
			h.tester.Frame()
			if h.app.confirm == nil || h.app.confirm.confirmLabel != "Overwrite" ||
				h.app.confirm.alternativeLabel != "Reload" || !h.app.confirm.destructive {
				t.Fatalf("conflict confirmation = %+v", h.app.confirm)
			}
			if err := h.tester.Click(action); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()

			if action == "Reload" {
				document = waitForCodeDocument(t, h, "note.txt")
				if got := document.state.Buffer.String(); got != "external\n" || document.state.Buffer.Modified() {
					t.Fatalf("reloaded buffer = %q modified=%v", got, document.state.Buffer.Modified())
				}
				return
			}
			if got := files["note.txt"]; got != local || document.state.Buffer.Modified() || document.saveError != "" {
				t.Fatalf("overwritten file = %q modified=%v error=%q", got, document.state.Buffer.Modified(), document.saveError)
			}
		})
	}
}

func TestCodeManualSaveAndUnsavedCloseChoices(t *testing.T) {
	for _, choice := range []string{"Cancel", "Don't Save", "Save", "Save failure"} {
		t.Run(choice, func(t *testing.T) {
			written := stubCodeWorkspace(t, map[string]string{"note.txt": "original\n", "other.txt": "other\n"})
			h := menuTestApp(t)
			openCodePageForTest(t, h, 2)
			h.app.openCodeFile("note.txt", 0)
			doc := waitForCodeDocument(t, h, "note.txt")
			doc.state.Insert("changed\n")
			h.tester.Frame()
			_, dirtyDot := h.tester.Find("Unsaved changes")
			if !dirtyDot || h.tester.HasText("Saved automatically") {
				t.Fatal("unsaved tab indicator/status is incorrect")
			}
			h.app.openCodeFile("other.txt", 0)
			waitForCodeDocument(t, h, "other.txt")
			h.app.setWorkspaceTab(sectionHistory)
			h.tester.Frame()
			h.app.setWorkspaceTab(sectionCode)
			h.tester.Frame()
			if len(written) != 0 || h.app.code.documents["note.txt"] != doc {
				t.Fatal("navigation saved or replaced a dirty preview")
			}
			if choice == "Save failure" {
				writeWorkingFile = func(string, []byte) error { return errors.New("disk full") }
			}
			h.app.closeCodeFile("note.txt")
			h.tester.Frame()
			if h.app.confirm == nil {
				t.Fatal("closing dirty tab did not prompt")
			}
			button := choice
			if button == "Save failure" {
				button = "Save"
			}
			if err := h.tester.Click(button); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			kept := choice == "Cancel" || choice == "Save failure"
			if (h.app.code.documents["note.txt"] != nil) != kept {
				t.Fatalf("close choice %s lost or retained wrong tab", choice)
			}
			if choice == "Save" {
				if !strings.Contains(written["note.txt"], "changed") {
					t.Fatal("save choice did not write")
				}
			} else if len(written) != 0 {
				t.Fatal("cancel/discard wrote changes")
			}
			if choice == "Save failure" && (doc.saveError == "" || !doc.state.Buffer.Modified()) {
				t.Fatal("failed save lost dirty state or error")
			}
		})
	}
}

func TestCodeSaveShortcutsKeepUndoAndClearDirtyDot(t *testing.T) {
	written := stubCodeWorkspace(t, map[string]string{"note.txt": "original\n"})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("note.txt", 0)
	doc := waitForCodeDocument(t, h, "note.txt")
	for _, mods := range []ui.Modifiers{ui.Ctrl, ui.Cmd} {
		doc.state.Insert("edit\n")
		h.tester.Frame()
		h.tester.Key(mods, ui.KeyS)
		h.tester.Frame()
		_, dirtyDot := h.tester.Find("Unsaved changes")
		if doc.state.Buffer.Modified() || !strings.Contains(written["note.txt"], "edit") || dirtyDot {
			t.Fatalf("save shortcut mods=%v modified=%v written=%q dot=%v texts=%q", mods, doc.state.Buffer.Modified(), written["note.txt"], h.tester.HasText("●"), h.texts())
		}
	}
	doc.state.Run(editor.Undo)
	h.tester.Frame()
	if !doc.state.Buffer.Modified() {
		t.Fatal("saving destroyed undo history")
	}
}

func TestCodeExplorerRetainsFolderChoicesAcrossNavigationAndRefresh(t *testing.T) {
	stubCodeWorkspace(t, map[string]string{"src/nested/file.txt": "content", "docs/guide.txt": "guide"})
	h := menuTestApp(t)
	openCodePageForTest(t, h, 2)
	if len(h.app.code.rows) != 2 {
		t.Fatalf("initial rows=%+v", h.app.code.rows)
	}
	if err := h.tester.Click("Expand directory src"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.tester.HasText("nested") || h.tester.HasText("file.txt") {
		t.Fatal("opening parent unexpectedly expanded children")
	}
	if err := h.tester.Click("Expand directory src/nested"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.app.setWorkspaceTab(sectionHistory)
	h.tester.Frame()
	h.app.setWorkspaceTab(sectionCode)
	h.tester.Frame()
	h.app.requestCodeFiles()
	h.waitFor(t, "refreshed explorer", func() bool { return !h.app.code.filesLoading })
	h.tester.Frame()
	if !h.tester.HasText("file.txt") || h.tester.HasText("guide.txt") {
		t.Fatal("navigation or refresh changed folder choices")
	}
	h.app.code.filter = "guide"
	h.tester.Frame()
	if !h.tester.HasText("guide.txt") {
		t.Fatal("filter failed to reveal a match")
	}
	h.app.code.filter = ""
	h.tester.Frame()
	if h.tester.HasText("guide.txt") {
		t.Fatal("filter changed remembered folder choices")
	}
}

func TestClosingRepositoryTabsProtectsAllUnsavedBuffers(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{})
	for _, root := range []string{"/work/one", "/work/two"} {
		page := readyShellTab(s, root)
		state := &editor.State{Buffer: editor.NewBuffer("original")}
		state.Insert("dirty")
		page.app.code.documents = map[string]*codeDocument{"note.txt": {path: "note.txt", state: state}}
		page.app.code.tabs = []string{"note.txt"}
		page.app.code.active = "note.txt"
	}
	count := len(s.tabs)
	s.closeAll()
	tt.Frame()
	if len(s.tabs) != count || s.activeApp().confirm == nil {
		t.Fatal("repository close skipped unsaved prompt")
	}
	s.activeApp().cancelConfirm()
	tt.Frame()
	if len(s.tabs) != count {
		t.Fatal("cancel removed repositories")
	}
	s.closeAll()
	tt.Frame()
	for range 2 {
		if err := tt.Click("Don't Save"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if len(s.tabs) != 1 || s.activeTab().repository() {
		t.Fatal("discard all did not close repositories")
	}
}
