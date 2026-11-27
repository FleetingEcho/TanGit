package app

import (
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func (h *harness) openFileActions(t *testing.T) {
	t.Helper()
	if err := h.tester.Click("File actions"); err != nil {
		t.Fatalf("open the file actions menu: %v; texts %q", err, h.texts())
	}
	h.tester.Frame()
}

func (h *harness) menuHasItem(label string) bool {
	for _, text := range h.texts() {
		if text == label {
			return true
		}
	}
	return false
}

// prepareBlameWorkspace gives the Changes harness real working-tree files so
// the same validation used by the built-in editor runs in these action tests.
func prepareBlameWorkspace(t *testing.T, h *harness, files map[string]string) {
	t.Helper()
	useRealCodeWorkspace(t, h, files)
	h.app.setWorkspaceTab(sectionChanges)
	h.tester.Frame()
	h.settle(t)
}

func TestFileActionsMenuOpensNewFileBlameInPinnedEditorTab(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "new.go", Status: "A ", Staged: true, Added: true},
	})
	prepareBlameWorkspace(t, h, map[string]string{"new.go": "package new\n"})
	h.openFileActions(t)
	h.requireMenuLabels(t, "Blame", "Unstage", "Stash selected file…", "Discard changes…")
	h.chooseMenuItem(t, "Blame")
	document := waitForCodeDocument(t, h, "new.go")
	h.waitFor(t, "the editor blame", func() bool { return document.blameLoaded })

	if h.app.workspaceTab != sectionCode || h.app.activeCodeDocument() != document {
		t.Fatalf("Blame did not open new.go in Code: tab=%d active=%q", h.app.workspaceTab, h.app.code.active)
	}
	if h.app.code.preview != "" {
		t.Fatalf("Blame left the explicit editor tab as preview %q", h.app.code.preview)
	}
	if !document.blameEnabled || !h.tester.HasText("Git Blame") {
		t.Fatalf("the editor blame control is missing or inactive: enabled=%v texts=%q", document.blameEnabled, h.texts())
	}
	if !calls.has("blame-working-tree:new.go") {
		t.Fatalf("new.go was not blamed from the working tree: %q", calls.all())
	}
	if h.app.blameState != nil {
		t.Fatal("Blame opened the retired full-file report")
	}
}

func TestFileActionsMenuBlamesTrackedWorkingTreeInEditor(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	prepareBlameWorkspace(t, h, map[string]string{"changed.txt": "current contents\n"})
	h.openFileActions(t)
	h.chooseMenuItem(t, "Blame")
	document := waitForCodeDocument(t, h, "changed.txt")
	h.waitFor(t, "the editor blame", func() bool { return document.blameLoaded })
	if !calls.has("blame-working-tree:changed.txt") || calls.has("blame-file:changed.txt") {
		t.Fatalf("tracked editor source was not blamed from the working tree: %q", calls.all())
	}
}

func TestBlameIsOfferedOnlyForFilesThatExist(t *testing.T) {
	t.Run("untracked", func(t *testing.T) {
		h, calls := changesTestApp(t, []repository.File{{Path: "scratch.txt", Status: "??", Unstaged: true, Added: true}})
		prepareBlameWorkspace(t, h, map[string]string{"scratch.txt": "scratch\n"})
		h.openFileActions(t)
		h.chooseMenuItem(t, "Blame")
		document := waitForCodeDocument(t, h, "scratch.txt")
		h.waitFor(t, "the editor blame", func() bool { return document.blameLoaded })
		if !calls.has("blame-working-tree:scratch.txt") {
			t.Fatalf("an untracked file was not blamed from the working tree: %q", calls.all())
		}
	})
	t.Run("deleted", func(t *testing.T) {
		h, _ := changesTestApp(t, []repository.File{{Path: "gone.txt", Status: " D", Unstaged: true, Tracked: true, Deleted: true}})
		h.tester.Frame()
		h.openFileActions(t)
		if h.menuHasItem("Blame") {
			t.Fatalf("a deleted file offers Blame: %q", h.texts())
		}
	})
}

func TestFileRowMenuOpensNewFileBlameInEditor(t *testing.T) {
	h, _ := changesTestApp(t, []repository.File{
		{Path: "new.go", Status: "A ", Staged: true, Added: true},
	})
	prepareBlameWorkspace(t, h, map[string]string{"new.go": "package new\n"})
	h.rightClick(t, "new.go")
	h.requireMenuLabels(t, "Blame")
	h.chooseMenuItem(t, "Blame")
	document := waitForCodeDocument(t, h, "new.go")
	if h.app.workspaceTab != sectionCode || h.app.activeCodeDocument() != document {
		t.Fatalf("row Blame did not open new.go in Code: tab=%d active=%q", h.app.workspaceTab, h.app.code.active)
	}
}

func TestEditorBlameIconTogglesFullFileAndKeepsActiveLine(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	prepareBlameWorkspace(t, h, map[string]string{"changed.txt": "changed\n"})
	h.openFileActions(t)
	h.chooseMenuItem(t, "Blame")
	document := waitForCodeDocument(t, h, "changed.txt")
	h.waitFor(t, "the editor blame", func() bool { return document.blameLoaded })

	if !document.blameEnabled || !document.blameLoaded || !document.blameFull ||
		len(document.blameInlays) != 1 || len(document.state.LineAnnotations()) != 1 {
		t.Fatalf("Blame menu did not show full-file blame and retain the active line: enabled=%v loaded=%v full=%v inlays=%#v annotations=%#v",
			document.blameEnabled, document.blameLoaded, document.blameFull, document.blameInlays, document.state.LineAnnotations())
	}
	h.clickAndSettle(t, "Git Blame")
	if !document.blameEnabled || !document.blameLoaded || document.blameFull ||
		len(document.blameInlays) != 1 || len(document.state.LineAnnotations()) != 0 {
		t.Fatalf("hiding full-file blame hid the selected-line annotation: enabled=%v loaded=%v full=%v inlays=%#v annotations=%#v",
			document.blameEnabled, document.blameLoaded, document.blameFull, document.blameInlays, document.state.LineAnnotations())
	}
	h.clickAndSettle(t, "Git Blame")
	if !document.blameFull || len(document.blameInlays) != 1 || len(document.state.LineAnnotations()) != 1 {
		t.Fatalf("the blame icon did not restore full-file blame and retain the active line: full=%v inlays=%#v annotations=%#v",
			document.blameFull, document.blameInlays, document.state.LineAnnotations())
	}
}
