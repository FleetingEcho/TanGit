package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// saveDialog is a stubbed desktop dialog: it records what it was asked and
// answers with the path the test chose.
type saveDialog struct {
	opts mygo.SaveDialogOptions
	path string
	err  error
	done chan struct{}
}

// stubSaveDialog replaces chooseSavePath for one test and puts the desktop's
// own back afterwards.
func stubSaveDialog(t *testing.T, dialog *saveDialog) {
	t.Helper()
	restore := chooseSavePath
	chooseSavePath = func(opts mygo.SaveDialogOptions) (string, error) {
		dialog.opts = opts
		close(dialog.done)
		return dialog.path, dialog.err
	}
	t.Cleanup(func() { chooseSavePath = restore })
}

// The download icon writes the patch Git printed, to the folder and under the
// name the user chose in the desktop's save dialog.
func TestSavingDiffWritesThePatch(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })

	dir := t.TempDir()
	saved := filepath.Join(dir, "renamed.patch")
	dialog := &saveDialog{path: saved, done: make(chan struct{})}
	stubSaveDialog(t, dialog)

	if err := h.tester.Click("Save diff"); err != nil {
		t.Fatalf("the diff header has no save button: %v", err)
	}
	<-dialog.done
	h.waitFor(t, "the save notification", func() bool {
		return containsText(h.texts(), "Saved the diff")
	})

	content, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("the saved diff is not on disk: %v", err)
	}
	if string(content) != h.app.diffPatch {
		t.Fatalf("the saved file holds %q, the pane shows %q", content, h.app.diffPatch)
	}

	want := filepath.Join("/work/project", "changed.txt.patch")
	if dialog.opts.DefaultPath != want {
		t.Fatalf("the dialog started at %q, want %q", dialog.opts.DefaultPath, want)
	}
	if dialog.opts.Title != "Save diff" || dialog.opts.ButtonLabel != "Save" {
		t.Fatalf("the dialog is titled %q with button %q", dialog.opts.Title, dialog.opts.ButtonLabel)
	}
	if len(dialog.opts.Filters) != 1 || len(dialog.opts.Filters[0].Extensions) != 2 {
		t.Fatalf("the dialog filters patches with %+v", dialog.opts.Filters)
	}
	if !containsText(h.texts(), saved) {
		t.Fatalf("the notification does not name the file: %q", h.texts())
	}
}

// A commit's diff is saved from its own patch: the History pane is not the
// working-tree diff, and the file it writes must be the one on screen.
func TestSavingACommitDiffWritesItsOwnPatch(t *testing.T) {
	session := patchCommitSession{fakeSession{snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Commits: []repository.Commit{
			{Hash: "c1c1c1c1", ShortHash: "c1c1c1c", Subject: "Initial commit", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		},
	}}}
	a := newApp(fixedSessionOpener{session: session})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)

	a.workspaceTab = sectionHistory
	h.tester.Frame()
	h.settle(t)
	h.waitFor(t, "the commit's diff", func() bool {
		return a.commitDiffState != nil && a.commitDiffPatch != ""
	})

	saved := filepath.Join(t.TempDir(), "commit.patch")
	dialog := &saveDialog{path: saved, done: make(chan struct{})}
	stubSaveDialog(t, dialog)

	if err := h.tester.Click("Save diff"); err != nil {
		t.Fatalf("the commit header has no save button: %v", err)
	}
	<-dialog.done
	h.waitFor(t, "the save notification", func() bool {
		return containsText(h.texts(), "Saved the diff")
	})

	content, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("the commit diff is not on disk: %v", err)
	}
	if string(content) != fakeWorkingTreePatch {
		t.Fatalf("the saved file holds %q, want the commit's patch", content)
	}
	if a.diffPatch != "" && string(content) == a.diffPatch {
		t.Fatal("the commit diff was saved from the working-tree patch")
	}
	want := filepath.Join("/work/project", "one.txt.patch")
	if dialog.opts.DefaultPath != want {
		t.Fatalf("the dialog started at %q, want %q", dialog.opts.DefaultPath, want)
	}
}

// Cancel is not an error: the dialog answers with no path, and nothing is
// written or said.
func TestCancelingTheSaveWritesNothing(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "the diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })

	saved := filepath.Join(t.TempDir(), "never.patch")
	dialog := &saveDialog{path: "", done: make(chan struct{})}
	stubSaveDialog(t, dialog)

	if err := h.tester.Click("Save diff"); err != nil {
		t.Fatalf("the diff header has no save button: %v", err)
	}
	<-dialog.done
	h.settle(t)

	if _, err := os.Stat(saved); !os.IsNotExist(err) {
		t.Fatalf("a canceled save wrote a file: %v", err)
	}
	if containsText(h.texts(), "Saved the diff") || containsText(h.texts(), "Could not save the diff") {
		t.Fatalf("a canceled save showed a notification: %q", h.texts())
	}
}

// patchCommitSession answers a commit's file diff with a patch, as a
// repository does, so the export path can be driven from History.
type patchCommitSession struct{ fakeSession }

func (s patchCommitSession) CommitFileDiff(context.Context, repository.Commit, repository.CommitFile, repository.DiffOptions) (repository.Diff, error) {
	return repository.Diff{Patch: fakeWorkingTreePatch, Kind: "Commit"}, nil
}
