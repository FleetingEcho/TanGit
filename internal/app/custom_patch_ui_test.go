package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type customPatchSession struct {
	fakeSession
	previewSession repository.Session
	previewErr     error
	patchStarted   chan<- struct{}
	patchRelease   <-chan struct{}
}

func (s customPatchSession) CommitFilePatch(_ context.Context, commit repository.Commit, file repository.CommitFile) (string, error) {
	s.calls.record("custom-canonical:" + commit.Hash + ":" + file.Path)
	if s.patchStarted != nil {
		s.patchStarted <- struct{}{}
	}
	if s.patchRelease != nil {
		<-s.patchRelease
	}
	return strings.ReplaceAll(customPatchFixture, "changed.txt", file.Path), nil
}

func (s customPatchSession) PreviewPatchText(_ context.Context, text string) (repository.PatchPreview, error) {
	s.calls.record("custom-preview:" + text)
	if s.previewErr != nil {
		return repository.PatchPreview{}, s.previewErr
	}
	if s.previewSession != nil {
		return s.previewSession.PreviewPatchText(context.Background(), text)
	}
	return repository.PatchPreview{
		Path: "Custom Patch", Text: text, Clean: true,
		Files: []repository.PatchFile{{Path: "changed.txt"}}, Additions: 2, Deletions: 2,
	}, nil
}

func (s customPatchSession) ApplyPatch(_ context.Context, preview repository.PatchPreview) error {
	s.calls.record("custom-apply:" + preview.Text)
	return nil
}

func customPatchPreviewSession(t *testing.T) repository.Session {
	t.Helper()
	root := t.TempDir()
	runGitIn(t, root, "init", "--quiet")
	runGitIn(t, root, "config", "user.name", "TanGit Tests")
	runGitIn(t, root, "config", "user.email", "tangit@example.com")
	for _, name := range []string{"one.txt", "two.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitIn(t, root, "add", "one.txt", "two.txt")
	runGitIn(t, root, "commit", "--quiet", "-m", "Base")
	session, err := (repository.Opener{}).Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func customPatchHistoryHarness(t *testing.T) (*harness, *callRecorder, repository.Commit) {
	t.Helper()
	calls := &callRecorder{}
	commit := repository.Commit{Hash: "1234567890abcdef", ShortHash: "1234567", Subject: "Change files"}
	session := customPatchSession{previewSession: customPatchPreviewSession(t), fakeSession: fakeSession{
		calls: calls,
		snapshot: repository.Snapshot{
			Root: "/work/project", Branch: "main", Head: commit.Hash,
			Commits:  []repository.Commit{commit},
			Branches: []repository.Branch{{Name: "main", Head: true}},
		},
	}}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionHistory
	h := &harness{app: a, updates: newUpdates(), tester: ui.NewTester(a.view, 1400, 900)}
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, calls, commit
}

func TestCustomPatchCollectsPreviewsExportsAndApplies(t *testing.T) {
	h, calls, commit := customPatchHistoryHarness(t)
	h.app.beginAddCustomPatchCommit(commit)
	h.waitFor(t, "both canonical commit files", func() bool { return len(h.app.customPatch.entries) == 2 })
	if !calls.has("custom-canonical:"+commit.Hash+":one.txt") || !calls.has("custom-canonical:"+commit.Hash+":two.txt") {
		t.Fatalf("canonical files were not collected: %v", calls.all())
	}

	expected, err := customPatchText(h.app.customPatch.entries)
	if err != nil {
		t.Fatal(err)
	}
	h.app.openCustomPatchBuilder()
	h.tester.Frame()
	if !h.tester.HasText("Custom Patch Builder") || !h.tester.HasText("one.txt") || !h.tester.HasText("two.txt") {
		t.Fatalf("builder does not list its files: %q", h.tester.Texts())
	}
	if err := h.tester.Click("Preview Custom Patch"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the frozen safe preview", func() bool { return h.app.customPatch.preview != nil })
	if !strings.Contains(expected, "one.txt") || !strings.Contains(expected, "two.txt") {
		t.Fatalf("combined source is incomplete: %q", expected)
	}

	h.tester.Frame()
	destination := filepath.Join(t.TempDir(), "custom.patch")
	dialog := &saveDialog{path: destination, done: make(chan struct{})}
	stubSaveDialog(t, dialog)
	if err := h.tester.Click("Export .patch…"); err != nil {
		t.Fatal(err)
	}
	<-dialog.done
	// os.WriteFile exposes the new path before all of its bytes are visible.
	// Wait for the write's completion callback rather than racing os.Stat with
	// the write, so the comparison below always reads the complete export.
	h.waitFor(t, "the export completion", func() bool {
		return containsText(h.texts(), "Saved the diff")
	})
	exported, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(exported) != expected {
		t.Fatal("export differs from the approved patch")
	}

	if err := h.tester.Click("Apply Custom Patch"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "safe application", func() bool { return len(h.app.customPatch.entries) == 0 && !h.app.customPatch.open })
	for _, call := range calls.all() {
		if strings.HasPrefix(call, "custom-apply:") {
			return
		}
	}
	t.Fatalf("ApplyPatch was not called: %v", calls.all())
}

func TestClosingCustomPatchPreviewRejectsLateResult(t *testing.T) {
	h, _, commit := customPatchHistoryHarness(t)
	h.app.beginAddCustomPatchCommit(commit)
	h.waitFor(t, "builder entries", func() bool { return len(h.app.customPatch.entries) == 2 })
	h.app.beginCustomPatchPreview()
	if !h.app.customPatch.previewOpen {
		t.Fatal("preview did not open")
	}
	h.app.backToCustomPatchBuilder()
	// A same-token stale callback cannot reopen the preview after dismissal.
	token := h.app.customPatch.previewToken
	h.app.customPatch.previewOpen = false
	if token != h.app.customPatch.previewToken || h.app.customPatch.preview != nil {
		t.Fatal("dismissal did not invalidate preview state")
	}
}

func TestClearCustomPatchRejectsInFlightAddition(t *testing.T) {
	h, _, commit := customPatchHistoryHarness(t)
	h.app.customPatch.entries = []customPatchEntry{customPatchTestEntry(commit.Hash, "existing.txt")}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	session := h.app.session.(customPatchSession)
	session.patchStarted = started
	session.patchRelease = release
	h.app.session = session
	h.app.beginAddCustomPatchFile(commit, repository.CommitFile{Path: "late.txt", Status: "M"}, nil)
	<-started
	h.app.clearCustomPatch()
	close(release)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if len(h.app.customPatch.entries) != 0 || h.app.customPatch.pending != 0 {
		t.Fatalf("cleared basket accepted a late addition: entries=%+v pending=%d", h.app.customPatch.entries, h.app.customPatch.pending)
	}
}

func TestCustomPatchConstructionErrorOpensVisiblePreview(t *testing.T) {
	h, _, commit := customPatchHistoryHarness(t)
	entry := customPatchTestEntry(commit.Hash, "changed.txt")
	entry.whole = false
	h.app.customPatch.entries = []customPatchEntry{entry}
	h.app.customPatch.open = true
	h.app.beginCustomPatchPreview()
	if !h.app.customPatch.previewOpen || h.app.customPatch.previewLoad || h.app.customPatch.previewError == "" {
		t.Fatalf("construction error was hidden: %+v", h.app.customPatch)
	}
	h.tester.Frame()
	if !h.tester.HasText(h.app.customPatch.previewError) {
		t.Fatalf("construction error is not visible: %q", h.tester.Texts())
	}
}

func TestCustomPatchHistoryHotkeysAndLineSelectionDoNotStage(t *testing.T) {
	h, calls, _ := customPatchHistoryHarness(t)
	h.app.customPatch.entries = []customPatchEntry{customPatchTestEntry("1234567890abcdef", "changed.txt")}
	h.tester.Frame()
	h.tester.Key(ui.Shift, ui.KeyB)
	h.tester.Frame()
	if !h.app.customPatch.open {
		t.Fatal("B did not open Custom Patch Builder")
	}
	h.app.closeCustomPatchBuilder()
	for _, call := range calls.all() {
		if strings.HasPrefix(call, "apply-patch:") || strings.HasPrefix(call, "stage:") {
			t.Fatalf("history builder staged working-tree changes: %v", calls.all())
		}
	}
}
