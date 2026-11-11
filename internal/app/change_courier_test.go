package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

type courierTargetSession struct {
	fakeSession
	preview repository.PatchPreview
	err     error
}

func (s *courierTargetSession) PreviewPatchText(_ context.Context, text string) (repository.PatchPreview, error) {
	s.calls.record("courier-target-preview:" + text)
	if s.err != nil {
		return repository.PatchPreview{}, s.err
	}
	preview := s.preview
	preview.Text = text
	return preview, nil
}

func (s *courierTargetSession) ApplyPatch(_ context.Context, preview repository.PatchPreview) error {
	s.calls.record("courier-target-apply:" + preview.Text)
	return s.err
}

type courierOpener struct {
	path   string
	target repository.Session
	calls  *callRecorder
	err    error
}

func (o courierOpener) Init(context.Context, string) error { return nil }
func (o courierOpener) Open(_ context.Context, path string) (repository.Session, error) {
	o.calls.record("courier-open:" + path)
	if o.err != nil {
		return nil, o.err
	}
	if path != o.path || o.target == nil {
		return nil, errors.New("unexpected courier destination")
	}
	return o.target, nil
}

func courierTestApp(t *testing.T, target *courierTargetSession, openErr error) (*harness, *callRecorder) {
	t.Helper()
	h, calls := menuTestAppWithCalls(t)
	target.fakeSession.calls = calls
	h.app.snapshot.Worktrees = []repository.Worktree{
		{Path: "/work/project", Name: "project", Branch: "main", Current: true, Main: true},
		{Path: "/work/feature", Name: "feature", Branch: "feature"},
		{Path: "/work/missing", Name: "missing", Branch: "broken", Missing: true},
	}
	h.app.opener = courierOpener{path: "/work/feature", target: target, calls: calls, err: openErr}
	h.tester.Frame()
	return h, calls
}

func TestChangeCourierPreviewsAndAppliesThroughTargetSession(t *testing.T) {
	calls := &callRecorder{}
	target := &courierTargetSession{fakeSession: fakeSession{calls: calls}, preview: repository.PatchPreview{
		Clean: true, Files: []repository.PatchFile{{Path: "one.txt", Additions: 1, Deletions: 1}},
	}}
	h, calls := courierTestApp(t, target, nil)
	h.app.openChangeCourier(fakeWorkingTreePatch, "Custom Patch · 1 file")
	h.waitFor(t, "destination preview", func() bool { return h.app.courier.preview != nil })
	if h.app.courier.preview == nil || !h.app.courier.preview.Clean || h.app.courier.destination != "/work/feature" {
		t.Fatalf("courier preview = %#v at %q", h.app.courier.preview, h.app.courier.destination)
	}
	for _, call := range []string{"courier-open:/work/feature", "courier-target-preview:" + fakeWorkingTreePatch} {
		if !calls.has(call) {
			t.Fatalf("missing %q in %v", call, calls.all())
		}
	}
	if err := h.tester.Click("Send as unstaged changes"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "courier apply", func() bool { return !h.app.actionBusy && !h.app.courier.open })
	if !calls.has("courier-target-apply:" + fakeWorkingTreePatch) {
		t.Fatalf("target did not apply frozen patch: %v", calls.all())
	}
	if h.app.courier.open || h.app.actionBusy {
		t.Fatalf("courier did not finish: %#v", h.app.courier)
	}
	for _, call := range calls.all() {
		if call == "custom-apply:"+fakeWorkingTreePatch {
			t.Fatal("source session applied the courier patch")
		}
	}
}

func TestChangeCourierConflictHasNoApplyAction(t *testing.T) {
	calls := &callRecorder{}
	target := &courierTargetSession{fakeSession: fakeSession{calls: calls}, preview: repository.PatchPreview{
		Clean: false, Diagnostic: "one.txt already changed",
	}}
	h, calls := courierTestApp(t, target, nil)
	h.app.openChangeCourier(fakeWorkingTreePatch, "Current hunk · one.txt")
	h.waitFor(t, "conflicted destination", func() bool { return h.app.courier.preview != nil })
	if !h.tester.HasText("Destination conflicts with these changes") || h.tester.HasText("Send as unstaged changes") {
		t.Fatal("conflicted destination did not restrict the dialog to dismissal")
	}
	h.app.beginCourierApply()
	h.settle(t)
	for _, call := range calls.all() {
		if call == "courier-target-apply:"+fakeWorkingTreePatch {
			t.Fatal("conflicted preview reached ApplyPatch")
		}
	}
}

func TestChangeCourierCloseRejectsLateDestination(t *testing.T) {
	calls := &callRecorder{}
	started := make(chan struct{})
	release := make(chan struct{})
	target := &blockingCourierTarget{courierTargetSession: courierTargetSession{fakeSession: fakeSession{calls: calls}}, started: started, release: release}
	h, _ := courierTestApp(t, &target.courierTargetSession, nil)
	// Use the blocking concrete target, not the embedded value's default method.
	h.app.opener = courierOpener{path: "/work/feature", target: target, calls: calls}
	h.app.openChangeCourier(fakeWorkingTreePatch, "Custom Patch")
	<-started
	h.app.closeChangeCourier()
	close(release)
	h.settle(t)
	if h.app.courier.open || h.app.courier.preview != nil {
		t.Fatal("closed courier accepted a late preview")
	}
}

type blockingCourierTarget struct {
	courierTargetSession
	started chan struct{}
	release <-chan struct{}
}

func (s *blockingCourierTarget) PreviewPatchText(ctx context.Context, text string) (repository.PatchPreview, error) {
	close(s.started)
	<-s.release
	return s.courierTargetSession.PreviewPatchText(ctx, text)
}

type courierSourceSession struct {
	fakeSession
	patch string
}

func (s courierSourceSession) WorkingTreeFilePatch(_ context.Context, file repository.File) (string, error) {
	s.calls.record("working-tree-file-patch:" + file.Path)
	return s.patch, nil
}

func TestChangeCourierRejectsSelectionsReboundToNewerSource(t *testing.T) {
	newerPatch := strings.Replace(fakeWorkingTreePatch, "+TWO", "+UPDATED", 1)
	for _, test := range []struct {
		name   string
		action func(*app)
	}{
		{name: "selected lines", action: func(a *app) { a.beginSelectedLinesCourier() }},
		{name: "current hunk", action: func(a *app) { a.beginCurrentHunkCourier() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, calls := changesTestApp(t, unstagedTrackedFile())
			h.waitFor(t, "source diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
			h.app.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
			h.app.diffState.GoToHunk(0)
			calls = h.app.session.(fakeSession).calls
			targetCalls := &callRecorder{}
			target := &courierTargetSession{fakeSession: fakeSession{calls: targetCalls}, preview: repository.PatchPreview{Clean: true}}
			h.app.snapshot.Worktrees = []repository.Worktree{
				{Path: "/work/project", Name: "project", Branch: "main", Current: true, Main: true},
				{Path: "/work/feature", Name: "feature", Branch: "feature"},
			}
			h.app.opener = courierOpener{path: "/work/feature", target: target, calls: targetCalls}
			h.app.session = courierSourceSession{fakeSession: fakeSession{calls: calls}, patch: newerPatch}

			test.action(h.app)
			h.waitFor(t, "source version check", func() bool { return !h.app.courier.sourceLoading })
			if !strings.Contains(h.app.courier.previewError, "changed after this diff") {
				t.Fatalf("newer source error = %q", h.app.courier.previewError)
			}
			if h.app.courier.patchText != "" || len(targetCalls.all()) != 0 {
				t.Fatalf("rebound selection reached destination: patch %q, calls %v", h.app.courier.patchText, targetCalls.all())
			}
		})
	}
}

func TestChangeCourierDiffHasOneContextSensitiveAction(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "source diff", func() bool { return h.app.diffState != nil && h.app.diffPatch != "" })
	if !h.tester.HasText("Send current hunk to worktree…") || h.tester.HasText("Send selected lines to worktree…") {
		t.Fatal("unselected diff did not offer only the current-hunk courier action")
	}
	if got := h.app.courierButtonTooltip("Preview changes"); got != "No other available worktrees. Create a worktree for the destination branch first." {
		t.Fatalf("missing-worktree tooltip = %q", got)
	}
	button, ok := h.tester.Find("Send current hunk to worktree…")
	if !ok {
		t.Fatal("courier button has no pointer target")
	}
	h.tester.Move(button.X+button.W/2, button.Y+button.H/2)
	h.tester.Advance(time.Second)
	h.tester.Frame()
	if !h.tester.HasText("No other available worktrees. Create a worktree for the destination branch first.") {
		t.Fatal("disabled courier button does not show why it cannot run on hover")
	}
	h.app.actionBusy = true
	if got := h.app.courierButtonTooltip("Preview changes"); got != "Wait for the current action to finish, then send these changes to another worktree." {
		t.Fatalf("busy-action tooltip = %q", got)
	}
	h.app.actionBusy = false
	h.app.snapshot.Worktrees = []repository.Worktree{
		{Path: "/work/project", Name: "project"},
		{Path: "/work/feature", Name: "feature"},
	}
	if got := h.app.courierButtonTooltip("Preview changes"); got != "Preview changes" {
		t.Fatalf("enabled courier tooltip = %q", got)
	}

	h.app.diffState.SetSelectedLines(editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1}, true)
	h.tester.Frame()
	if !h.tester.HasText("Send selected lines to worktree…") || h.tester.HasText("Send current hunk to worktree…") {
		t.Fatal("selected diff did not replace the hunk action with the selected-lines courier action")
	}
}

func TestCustomPatchCourierHandoffUsesCompletePreviewBytes(t *testing.T) {
	calls := &callRecorder{}
	target := &courierTargetSession{fakeSession: fakeSession{calls: calls}, preview: repository.PatchPreview{Clean: true}}
	h, calls := courierTestApp(t, target, nil)
	session := customPatchSession{fakeSession: fakeSession{calls: calls}, previewSession: customPatchPreviewSession(t)}
	h.app.session = session
	h.app.customPatch.entries = []customPatchEntry{{
		commit: repository.Commit{Hash: "abc", ShortHash: "abc"},
		file:   repository.CommitFile{Path: "changed.txt", Status: "M"}, sourcePatch: customPatchFixture, whole: true,
	}}
	h.app.beginCustomPatchPreview()
	h.waitFor(t, "source preview", func() bool { return h.app.customPatch.preview != nil })
	if h.app.customPatch.preview == nil {
		t.Fatal("source preview did not finish")
	}
	h.app.beginCustomPatchPreviewCourier()
	h.settle(t)
	if !calls.has("courier-target-preview:" + customPatchFixture) {
		t.Fatalf("handoff did not preserve complete canonical bytes: %v", calls.all())
	}
}
