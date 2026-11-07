package app

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestCompareRefsDialogLoadsFilesAndAppliesOptions(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.waitFor(t, "the underlying working-tree diff", func() bool { return h.app.diffState != nil })
	underlyingDiff := h.app.diffState
	underlyingFile := h.app.selectedFile

	h.press(t, ui.KeyW)
	if !h.app.compare.open {
		t.Fatal("W did not open Compare refs")
	}
	if h.app.compare.from != "HEAD~1" || h.app.compare.to != "HEAD" {
		t.Fatalf("default refs = %q to %q", h.app.compare.from, h.app.compare.to)
	}
	if err := h.tester.Click("Compare"); err != nil {
		t.Fatalf("start comparison: %v", err)
	}
	h.waitFor(t, "the changed-file list and first diff", func() bool {
		return len(h.app.compare.files) == 2 && h.app.compare.diffState != nil
	})
	wantListCall := "compare-files:HEAD~1:HEAD:false:3"
	if !calls.has(wantListCall) {
		t.Fatalf("comparison call %q missing from %q", wantListCall, calls.all())
	}
	if h.app.compare.selected != 0 || h.app.compare.diffState.ContextLines != 3 {
		t.Fatalf("initial selection/context = %d/%d", h.app.compare.selected, h.app.compare.diffState.ContextLines)
	}

	if err := h.tester.Click("compare-two.txt"); err != nil {
		t.Fatalf("choose the second compared file: %v", err)
	}
	h.waitFor(t, "the second compared file's diff", func() bool {
		return calls.has("compare-file-diff:HEAD~1:HEAD:compare-two.txt:false:3") &&
			h.app.compare.selected == 1 && h.app.compare.diffState != nil
	})

	if err := h.tester.Click("Ignore whitespace"); err != nil {
		t.Fatalf("toggle whitespace: %v", err)
	}
	h.tester.Frame()
	h.waitFor(t, "the whitespace-ignoring comparison", func() bool {
		return calls.has("compare-files:HEAD~1:HEAD:true:3") &&
			calls.has("compare-file-diff:HEAD~1:HEAD:compare-one.txt:true:3") &&
			h.app.compare.diffState != nil
	})

	// Choose zero context through the real select and its pop-up option.
	if err := h.tester.Click("3"); err != nil {
		t.Fatalf("open the context choices: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("0"); err != nil {
		t.Fatalf("choose zero context: %v; texts %q", err, h.texts())
	}
	h.tester.Frame()
	h.waitFor(t, "the zero-context comparison", func() bool {
		return calls.has("compare-files:HEAD~1:HEAD:true:0") &&
			calls.has("compare-file-diff:HEAD~1:HEAD:compare-one.txt:true:0") &&
			h.app.compare.diffState != nil && !h.app.compare.filesLoading
	})
	if h.app.compare.diffState.ContextLines != 0 {
		t.Errorf("viewer context = %d, want zero", h.app.compare.diffState.ContextLines)
	}

	if err := h.tester.Click("Swap refs"); err != nil {
		t.Fatalf("swap refs: %v", err)
	}
	h.waitFor(t, "the reversed comparison", func() bool {
		return calls.has("compare-files:HEAD:HEAD~1:true:0") &&
			h.app.compare.files != nil && !h.app.compare.filesLoading
	})
	if h.app.compare.from != "HEAD" || h.app.compare.to != "HEAD~1" {
		t.Errorf("swapped refs = %q to %q", h.app.compare.from, h.app.compare.to)
	}

	h.press(t, ui.KeyEscape)
	if h.app.compare.open {
		t.Error("Escape left Compare refs open")
	}
	if h.app.diffState != underlyingDiff || h.app.selectedFile != underlyingFile {
		t.Error("closing the comparison changed the underlying file or diff")
	}

	// The same feature is reachable without the keyboard from the header.
	if err := h.tester.Click("Compare refs"); err != nil {
		t.Fatalf("open comparison from the header: %v", err)
	}
	h.tester.Frame()
	if !h.app.compare.open {
		t.Error("header Compare refs button did not open the dialog")
	}
	h.press(t, ui.KeyEscape)
}

type controlledCompareListRequest struct {
	from, to string
	options  repository.DiffOptions
	response chan controlledCompareListResponse
}

type controlledCompareListResponse struct {
	files []repository.CommitFile
	err   error
}

type controlledCompareSession struct {
	fakeSession
	requests chan *controlledCompareListRequest
}

func (s *controlledCompareSession) CompareFiles(_ context.Context, from, to string, options repository.DiffOptions) ([]repository.CommitFile, error) {
	request := &controlledCompareListRequest{
		from: from, to: to, options: options,
		response: make(chan controlledCompareListResponse, 1),
	}
	s.requests <- request
	response := <-request.response
	return response.files, response.err
}

func TestCompareDropsAnOutdatedFileList(t *testing.T) {
	snapshot := repository.Snapshot{
		Root: "/work/project", Branch: "main",
		Commits: []repository.Commit{
			{Hash: "c2", ShortHash: "c2", Parents: []string{"c1"}},
			{Hash: "c1", ShortHash: "c1"},
		},
	}
	session := &controlledCompareSession{
		fakeSession: fakeSession{snapshot: snapshot},
		requests:    make(chan *controlledCompareListRequest, 2),
	}
	a := newApp(fixedSessionOpener{session: session})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = snapshot.Root
	openFixture(t, h.tester)
	h.settle(t)

	h.press(t, ui.KeyW)
	if err := h.tester.Click("Compare"); err != nil {
		t.Fatal(err)
	}
	first := nextCompareListRequest(t, session.requests)
	if first.from != "HEAD~1" || first.to != "HEAD" {
		t.Fatalf("first request = %q to %q", first.from, first.to)
	}

	// Swap while the first read is blocked. The replacement has its own refs
	// and options, and the first response is no longer useful.
	if err := h.tester.Click("Swap refs"); err != nil {
		t.Fatal(err)
	}
	second := nextCompareListRequest(t, session.requests)
	if second.from != "HEAD" || second.to != "HEAD~1" {
		t.Fatalf("replacement request = %q to %q", second.from, second.to)
	}
	second.response <- controlledCompareListResponse{files: []repository.CommitFile{
		{Path: "new-result.txt", Status: "M"},
	}}
	h.waitFor(t, "the replacement comparison", func() bool {
		return len(h.app.compare.files) == 1 && h.app.compare.files[0].Path == "new-result.txt"
	})

	first.response <- controlledCompareListResponse{files: []repository.CommitFile{
		{Path: "outdated-result.txt", Status: "M"},
	}}
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if len(h.app.compare.files) != 1 || h.app.compare.files[0].Path != "new-result.txt" {
		t.Fatalf("outdated response replaced the current files: %#v", h.app.compare.files)
	}
	h.press(t, ui.KeyEscape)
}

func nextCompareListRequest(t *testing.T, requests <-chan *controlledCompareListRequest) *controlledCompareListRequest {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a comparison request")
		return nil
	}
}
