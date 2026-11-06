package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type patchImportSession struct {
	fakeSession
	preview    repository.PatchPreview
	previewErr error
	applyErr   error
}

func (s patchImportSession) PreviewPatch(_ context.Context, path string) (repository.PatchPreview, error) {
	s.calls.record("preview-patch-file:" + path)
	if s.previewErr != nil {
		return repository.PatchPreview{}, s.previewErr
	}
	preview := s.preview
	preview.Path = path
	return preview, nil
}

func (s patchImportSession) ApplyPatch(_ context.Context, preview repository.PatchPreview) error {
	s.calls.record("apply-imported-patch:" + preview.Path)
	return s.applyErr
}

func patchImportTestApp(t *testing.T, session repository.Session, calls *callRecorder) *harness {
	t.Helper()
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates(), tester: ui.NewTester(a.view, 1280, 800)}
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h
}

type openPatchDialog struct {
	opts mygo.OpenDialogOptions
	path string
	err  error
	done chan struct{}
}

func stubOpenPatchDialog(t *testing.T, dialog *openPatchDialog) {
	t.Helper()
	restore := choosePatchPath
	choosePatchPath = func(opts mygo.OpenDialogOptions) ([]string, error) {
		dialog.opts = opts
		close(dialog.done)
		if dialog.path == "" {
			return nil, dialog.err
		}
		return []string{dialog.path}, dialog.err
	}
	t.Cleanup(func() { choosePatchPath = restore })
}

func importedPatchPreview() repository.PatchPreview {
	return repository.PatchPreview{
		Clean:     true,
		Text:      "diff --git a/changed.txt b/changed.txt\n@@ -1 +1 @@\n-old\n+new\n",
		Additions: 1, Deletions: 1,
		Files: []repository.PatchFile{{Path: "changed.txt", Additions: 1, Deletions: 1}},
	}
}

func TestPatchImportPreviewsThenAppliesTheSameFile(t *testing.T) {
	calls := &callRecorder{}
	session := patchImportSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: "/work/project", Branch: "main"}, calls: calls},
		preview:     importedPatchPreview(),
	}
	h := patchImportTestApp(t, session, calls)
	path := filepath.Join(t.TempDir(), "incoming.diff")
	contents := []byte("diff --git a/changed.txt b/changed.txt\n")
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	dialog := &openPatchDialog{path: path, done: make(chan struct{})}
	stubOpenPatchDialog(t, dialog)

	if err := h.tester.Click("Apply patch…"); err != nil {
		t.Fatalf("open patch import: %v", err)
	}
	<-dialog.done
	if dialog.opts.Title != "Open patch" || dialog.opts.ButtonLabel != "Preview patch" ||
		dialog.opts.DefaultPath != "/work/project" || dialog.opts.Directory || dialog.opts.Multiple {
		t.Fatalf("native patch picker options = %+v", dialog.opts)
	}
	if len(dialog.opts.Filters) != 1 || !slices.Equal(dialog.opts.Filters[0].Extensions, []string{"patch", "diff"}) {
		t.Fatalf("native patch filters = %+v", dialog.opts.Filters)
	}
	h.waitFor(t, "the read-only patch preview", func() bool { return h.app.patchPreview != nil })
	h.tester.Frame()
	if got := h.app.patchPreview.Path; got != path {
		t.Fatalf("preview path = %q, want %q", got, path)
	}
	for _, text := range []string{"Apply Patch/Diff", "Patch applies cleanly", "changed.txt", "Patch text"} {
		if !slices.Contains(h.tester.Texts(), text) {
			t.Errorf("clean preview does not show %q: %q", text, h.tester.Texts())
		}
	}
	if calls.has("apply-imported-patch:" + path) {
		t.Fatal("preview applied the patch before Apply Patch was chosen")
	}
	before := len(calls.all())
	if before == 0 || !calls.has("preview-patch-file:"+path) {
		t.Fatalf("the selected file was not previewed: %v", calls.all())
	}

	if err := h.tester.Click("Apply Patch"); err != nil {
		t.Fatalf("apply clean patch: %v", err)
	}
	h.waitFor(t, "the atomic patch application", func() bool { return calls.has("apply-imported-patch:" + path) })
	h.settle(t)
	if h.app.patchPreviewOpen || h.app.patchPreview != nil {
		t.Fatal("successful apply left the preview open")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, contents) {
		t.Fatalf("applying changed the source patch: %q", got)
	}
}

func TestConflictedPatchPreviewOnlyDismissesWithoutApplying(t *testing.T) {
	calls := &callRecorder{}
	session := patchImportSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: "/work/project", Branch: "main"}, calls: calls},
		preview: repository.PatchPreview{
			Clean: false, Diagnostic: "error: patch failed: changed.txt:1\nerror: changed.txt: patch does not apply",
			Text:  "diff --git a/changed.txt b/changed.txt\n",
			Files: []repository.PatchFile{{Path: "changed.txt", Additions: 4, Deletions: 2}},
		},
	}
	h := patchImportTestApp(t, session, calls)
	path := filepath.Join(t.TempDir(), "conflict.patch")
	if err := os.WriteFile(path, []byte("patch"), 0o644); err != nil {
		t.Fatal(err)
	}
	dialog := &openPatchDialog{path: path, done: make(chan struct{})}
	stubOpenPatchDialog(t, dialog)

	h.tester.Key(ui.Ctrl, ui.KeyI)
	<-dialog.done
	h.waitFor(t, "the conflict diagnostics", func() bool { return h.app.patchPreview != nil })
	h.tester.Frame()
	texts := h.tester.Texts()
	if !slices.Contains(texts, "Patch cannot be applied") || !strings.Contains(strings.Join(texts, "\n"), "patch does not apply") {
		t.Fatalf("conflict diagnostics are missing: %q", texts)
	}
	if _, ok := h.tester.Find("Apply Patch"); ok {
		t.Fatal("a conflicted patch exposes Apply Patch")
	}
	if err := h.tester.Click("Discard Patch"); err != nil {
		t.Fatalf("discard conflicted patch preview: %v", err)
	}
	h.tester.Frame()
	if h.app.patchPreviewOpen || h.app.patchPreview != nil {
		t.Fatal("Discard Patch did not clear the preview")
	}
	for _, call := range calls.all() {
		if call == "apply-imported-patch:"+path {
			t.Fatal("Discard Patch applied the conflicted patch")
		}
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "patch" {
		t.Fatalf("Discard Patch changed the source file: %q, %v", contents, err)
	}
}

func TestCancelingPatchPickerAndPreviewStartsNoApply(t *testing.T) {
	calls := &callRecorder{}
	session := patchImportSession{fakeSession: fakeSession{
		snapshot: repository.Snapshot{Root: "/work/project", Branch: "main"}, calls: calls,
	}, preview: importedPatchPreview()}
	h := patchImportTestApp(t, session, calls)
	canceled := &openPatchDialog{done: make(chan struct{})}
	stubOpenPatchDialog(t, canceled)
	h.app.beginPatchImport()
	<-canceled.done
	h.settle(t)
	if h.app.patchPreviewOpen || calls.has("preview-patch-file:") {
		t.Fatalf("canceling the native picker left preview state: %+v", h.app.patchPreview)
	}
	if containsText(h.texts(), "Could not preview patch") {
		t.Fatal("canceling the native picker showed an error")
	}
	if len(calls.all()) != 0 {
		t.Fatalf("canceling the native picker called Git: %v", calls.all())
	}
}

func TestDiscardingPreviewInvalidatesAnOverdueResult(t *testing.T) {
	calls := &callRecorder{}
	started := make(chan struct{})
	release := make(chan repository.PatchPreview, 1)
	session := &blockingPatchPreviewSession{fakeSession: fakeSession{
		snapshot: repository.Snapshot{Root: "/work/project", Branch: "main"}, calls: calls,
	}, started: started, release: release}
	h := patchImportTestApp(t, session, calls)
	path := filepath.Join(t.TempDir(), "slow.patch")
	dialog := &openPatchDialog{path: path, done: make(chan struct{})}
	stubOpenPatchDialog(t, dialog)
	h.app.beginPatchImport()
	<-dialog.done
	<-started
	if !h.app.patchPreviewLoading {
		t.Fatal("slow preview is not marked as loading")
	}
	h.app.discardPatchPreview()
	release <- importedPatchPreview()
	h.settle(t)
	if h.app.patchPreviewOpen || h.app.patchPreview != nil || h.app.patchPreviewLoading {
		t.Fatal("an overdue preview reopened a discarded dialog")
	}
	if calls.has("apply-imported-patch:" + path) {
		t.Fatal("discarding an overdue result applied the patch")
	}
}

type blockingPatchPreviewSession struct {
	fakeSession
	started chan struct{}
	release chan repository.PatchPreview
}

func (s *blockingPatchPreviewSession) PreviewPatch(context.Context, string) (repository.PatchPreview, error) {
	close(s.started)
	return <-s.release, nil
}

func TestPatchPreviewOperationalErrorStaysDismissible(t *testing.T) {
	calls := &callRecorder{}
	session := patchImportSession{fakeSession: fakeSession{
		snapshot: repository.Snapshot{Root: "/work/project", Branch: "main"}, calls: calls,
	}, previewErr: errors.New("cannot read selected patch")}
	h := patchImportTestApp(t, session, calls)
	path := filepath.Join(t.TempDir(), "missing.diff")
	dialog := &openPatchDialog{path: path, done: make(chan struct{})}
	stubOpenPatchDialog(t, dialog)
	h.app.beginPatchImport()
	<-dialog.done
	h.waitFor(t, "the picker error", func() bool { return h.app.patchPreviewError != "" })
	h.tester.Frame()
	if !slices.Contains(h.tester.Texts(), "Could not preview patch") {
		t.Fatalf("preview error is not visible: %q", h.tester.Texts())
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.patchPreviewOpen {
		t.Fatal("Escape did not close the failed preview")
	}
}
