package app

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

type codeBlameTestSession struct {
	fakeSession
	lines         []repository.BlameLine
	commits       []repository.Commit
	blameStarted  chan struct{}
	blameRelease  chan struct{}
	searchStarted chan struct{}
	searchRelease chan struct{}
}

func (s *codeBlameTestSession) BlameWorkingTreeFile(_ context.Context, path string) ([]repository.BlameLine, error) {
	s.calls.record("code-blame-read:" + path)
	if s.blameStarted != nil {
		close(s.blameStarted)
	}
	if s.blameRelease != nil {
		<-s.blameRelease
	}
	return append([]repository.BlameLine(nil), s.lines...), nil
}

func (s *codeBlameTestSession) SearchCommits(_ context.Context, filter repository.CommitFilter) ([]repository.Commit, error) {
	s.calls.record("code-blame-commit:" + filter.Query)
	if s.searchStarted != nil {
		close(s.searchStarted)
	}
	if s.searchRelease != nil {
		<-s.searchRelease
	}
	if filter.Query != "" {
		for _, commit := range s.commits {
			if commit.Hash == filter.Query {
				return []repository.Commit{commit}, nil
			}
		}
		return nil, nil
	}
	return append([]repository.Commit(nil), s.commits...), nil
}

func openCodeBlameDocument(t *testing.T, session *codeBlameTestSession) (*harness, *codeDocument) {
	t.Helper()
	stubCodeWorkspace(t, map[string]string{"notes.txt": "first\nsecond\n"})
	h, calls := menuTestAppWithCalls(t)
	openCodePageForTest(t, h, 1)
	h.app.openCodeFile("notes.txt", 0)
	document := waitForCodeDocument(t, h, "notes.txt")
	session.fakeSession = fakeSession{calls: calls}
	h.app.session = session
	h.app.requestCodeBlame(document)
	return h, document
}

func TestCodeBlameAnnotationsOpenCommitAndJumpToHistory(t *testing.T) {
	hash := strings.Repeat("a", 40)
	uncommitted := strings.Repeat("0", 40)
	session := &codeBlameTestSession{
		lines: []repository.BlameLine{
			{Hash: hash, Author: "Ada Lovelace", Date: "2026-01-02", UnixTimestamp: timeNow() - 60*86400, Subject: "Explain the blame model", Current: true, Text: "first"},
			{Hash: uncommitted, Author: "Not Committed Yet", Date: "2026-11-10", Text: "second"},
		},
		commits: []repository.Commit{{
			Hash: hash, ShortHash: "aaaaaaa", Subject: "Explain the blame model",
			AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com", UnixTimestamp: 1767312000,
		}},
	}
	h, document := openCodeBlameDocument(t, session)
	if h.tester.HasText("Git Blame") {
		t.Fatal("always-on editor blame still shows a toolbar toggle")
	}
	if !document.blameEnabled {
		t.Fatal("Git Blame is not enabled for a newly opened editable document")
	}
	h.waitFor(t, "working-tree blame", func() bool { return document.blameLoaded })
	if document.state.HoverDelay != codeBlameHoverDelay {
		t.Fatalf("blame hover delay = %v", document.state.HoverDelay)
	}
	if len(document.blameInlays) != 1 || !strings.Contains(document.blameInlays[0].Text, "You, 2 months ago • Explain the blame model") {
		t.Fatalf("active-line blame annotation = %#v", document.blameInlays)
	}
	if !document.blameInlays[0].Trailing {
		t.Fatal("blame annotation must keep the caret and line-end selection before its decoration")
	}
	if document.blameInlays[0].Ellipsis != "…" {
		t.Fatal("blame annotation must ellipsize instead of wrapping onto another line")
	}
	if calls := session.calls.all(); !session.calls.has("code-blame-read:notes.txt") {
		t.Fatalf("working-tree blame was not requested for notes.txt: %v", calls)
	}
	document.state.GoTo(editor.Pos{Line: 1})
	h.app.syncCodeBlameActiveLine(document)
	if len(document.blameInlays) != 1 || document.blameActiveLine != 1 ||
		!strings.Contains(document.blameInlays[0].Text, "Uncommitted changes") {
		t.Fatalf("blame did not follow the active line: %#v", document.blameInlays)
	}
	document.state.GoTo(editor.Pos{Line: 0})
	h.app.syncCodeBlameActiveLine(document)

	snapshot := document.state.Buffer.Snapshot()
	document.state.OnHover(editor.HoverRequest{Offset: snapshot.LineEnd(0), Version: snapshot.Version()})
	h.waitFor(t, "commit details", func() bool {
		return h.app.codeBlameHover.open && !h.app.codeBlameHover.loading
	})
	h.tester.Frame()
	if h.app.codeBlameHover.path != "notes.txt" || h.app.codeBlameHover.line != 1 ||
		h.app.codeBlameHover.commit.Hash != hash {
		t.Fatalf("blame commit dialog = %#v", h.app.codeBlameHover)
	}
	if !h.tester.HasText("Explain the blame model") || !h.tester.HasText("ada@example.com") {
		t.Fatalf("commit details are incomplete: %q", h.texts())
	}

	if err := h.tester.Click("Open in History"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the blamed commit in History", func() bool {
		return h.app.workspaceTab == sectionHistory && h.app.selectedCommit == 0 &&
			len(h.app.historyFilter.matches) == 1 && !h.app.historyFilter.loading
	})
	if h.app.codeBlameHover.open || h.app.historyFilter.applied.Query != hash ||
		h.app.historyFilter.applied.Branch != "" || h.app.historyFilter.matches[0].Hash != hash {
		t.Fatalf("History did not select exact blame commit: dialog=%#v query=%#v commits=%#v",
			h.app.codeBlameHover, h.app.historyFilter.applied, h.app.historyFilter.matches)
	}
}

func TestCodeBlameUncommittedDialogHasNoHistoryAction(t *testing.T) {
	uncommitted := strings.Repeat("0", 40)
	session := &codeBlameTestSession{lines: []repository.BlameLine{
		{Hash: uncommitted, Author: "Not Committed Yet", Text: "first"},
		{Hash: uncommitted, Author: "Not Committed Yet", Text: "second"},
	}}
	h, document := openCodeBlameDocument(t, session)
	h.waitFor(t, "uncommitted blame", func() bool { return document.blameLoaded })
	snapshot := document.state.Buffer.Snapshot()
	document.state.OnHover(editor.HoverRequest{Offset: snapshot.LineEnd(0), Version: snapshot.Version()})
	h.tester.Frame()
	if !h.app.codeBlameHover.open || h.app.codeBlameHover.loading {
		t.Fatalf("uncommitted dialog = %#v", h.app.codeBlameHover)
	}
	if !h.tester.HasText("Open in History") {
		t.Fatalf("uncommitted hover does not show its disabled History action: %q", h.texts())
	}
	if session.calls.has("code-blame-commit:" + uncommitted) {
		t.Fatal("uncommitted blame attempted to load a zero commit hash")
	}
}

func TestCodeBlameRefreshesAfterSameBranchRepositoryMutation(t *testing.T) {
	hash := strings.Repeat("b", 40)
	session := &codeBlameTestSession{lines: []repository.BlameLine{
		{Hash: hash, Author: "Ada", Text: "first"},
		{Hash: hash, Author: "Ada", Text: "second"},
	}}
	h, document := openCodeBlameDocument(t, session)
	h.waitFor(t, "blame before mutation", func() bool { return document.blameLoaded })
	h.app.codeBlameHover.open = true
	h.app.codeBlameHover.path = document.path
	h.app.codeBlameHover.blame = session.lines[0]
	h.app.beginRepositoryActionMessage("", "", func(context.Context, repository.Session) error { return nil }, nil)
	if h.app.codeBlameHover.open || document.blameLoaded || len(document.blameInlays) != 0 {
		t.Fatal("same-branch mutation retained stale blame while Git was running")
	}
	h.waitFor(t, "blame after mutation", func() bool {
		return !h.app.actionBusy && h.app.workspaceTab == sectionCode && document.blameLoaded
	})
}

func TestCodeBlameRejectsLateLoadAfterDisable(t *testing.T) {
	hash := strings.Repeat("b", 40)
	started, release := make(chan struct{}), make(chan struct{})
	session := &codeBlameTestSession{
		lines:        []repository.BlameLine{{Hash: hash, Author: "Ada", Date: "2026-01-02", Text: "first"}},
		blameStarted: started,
		blameRelease: release,
	}
	h, document := openCodeBlameDocument(t, session)
	<-started
	h.app.toggleCodeBlame(document)
	close(release)
	h.settle(t)
	if document.blameEnabled || document.blameLoaded || document.blameLoading || len(document.blameInlays) != 0 {
		t.Fatalf("disabled document accepted late blame: %#v", document)
	}
	if document.state.HoverDelay != 0 {
		t.Fatalf("disabled blame retained hover delay %v", document.state.HoverDelay)
	}
}

func TestCodeBlameInvalidatesAfterEditAndPreservesOtherHover(t *testing.T) {
	hash := strings.Repeat("c", 40)
	document := &codeDocument{
		path:  "notes.txt",
		state: &editor.State{Buffer: editor.NewBuffer("first\n"), LineNumbers: editor.LineNumbersOn},
	}
	snapshot := document.state.Buffer.Snapshot()
	document.blameEnabled = true
	document.blameLoaded = true
	document.blameVersion = snapshot.Version()
	document.blameLines = []repository.BlameLine{{Hash: hash, Author: "Ada", Text: "first"}}
	document.blameOffsets = map[int]int{snapshot.LineEnd(0): 0}
	document.blameInlays = []editor.Inlay{{Offset: snapshot.LineEnd(0), Text: "Ada"}}
	document.state.SetInlays(document.blameInlays)

	a := &app{}
	a.code.documents = map[string]*codeDocument{document.path: document}
	called := 0
	document.state.OnHover = func(editor.HoverRequest) { called++ }
	a.configureCodeBlame(document)
	request := editor.HoverRequest{Offset: snapshot.LineEnd(0), Version: snapshot.Version() + 1}
	document.state.OnHover(request)
	if called != 1 {
		t.Fatal("stale blame did not preserve the previous hover handler")
	}

	document.state.Insert("edited\n")
	a.syncCodeBlame(document)
	if document.blameLoaded || len(document.blameInlays) != 0 || len(document.blameOffsets) != 0 {
		t.Fatal("edited document retained stale blame annotations")
	}
	document.state.OnHover(editor.HoverRequest{Offset: snapshot.LineEnd(0), Version: snapshot.Version()})
	if called != 2 {
		t.Fatal("invalidated blame did not fall through to the previous hover handler")
	}
}

func TestCodeBlameRejectsLateCommitDetailsAfterEdit(t *testing.T) {
	hash := strings.Repeat("d", 40)
	started, release := make(chan struct{}), make(chan struct{})
	session := &codeBlameTestSession{
		lines:         []repository.BlameLine{{Hash: hash, Author: "Ada", Text: "first"}},
		commits:       []repository.Commit{{Hash: hash, ShortHash: "ddddddd", Subject: "Late details"}},
		searchStarted: started,
		searchRelease: release,
	}
	h, document := openCodeBlameDocument(t, session)
	document.blameEnabled = true
	document.blameLoaded = true
	document.blameVersion = document.state.Buffer.Version()
	document.blameLines = append([]repository.BlameLine(nil), session.lines...)
	h.app.openCodeBlameDetails(document, 0)
	<-started
	document.state.Insert("newer\n")
	h.app.syncCodeBlame(document)
	if h.app.codeBlameHover.open {
		t.Fatal("editing the source document left its stale commit dialog open")
	}
	close(release)
	h.settle(t)
	if h.app.codeBlameHover.commit.Hash != "" {
		t.Fatal("edited document accepted late commit details")
	}
}

func TestCodeBlameRejectsLateCommitDetailsAfterClose(t *testing.T) {
	hash := strings.Repeat("d", 40)
	started, release := make(chan struct{}), make(chan struct{})
	session := &codeBlameTestSession{
		lines:         []repository.BlameLine{{Hash: hash, Author: "Ada", Text: "first"}},
		commits:       []repository.Commit{{Hash: hash, ShortHash: "ddddddd", Subject: "Late details"}},
		searchStarted: started,
		searchRelease: release,
	}
	h, document := openCodeBlameDocument(t, session)
	document.blameEnabled = true
	document.blameLoaded = true
	document.blameVersion = document.state.Buffer.Version()
	document.blameLines = append([]repository.BlameLine(nil), session.lines...)
	h.app.openCodeBlameDetails(document, 0)
	<-started
	h.app.closeCodeBlameDetails()
	close(release)
	h.settle(t)
	if h.app.codeBlameHover.open || h.app.codeBlameHover.commit.Hash != "" || h.app.codeBlameHover.loading {
		t.Fatalf("closed dialog accepted late commit details: %#v", h.app.codeBlameHover)
	}
}

func TestCodeBlameReloadsWhenUndoRestoresSavedBuffer(t *testing.T) {
	hash := strings.Repeat("e", 40)
	session := &codeBlameTestSession{lines: []repository.BlameLine{
		{Hash: hash, Author: "Ada", Text: "first"},
		{Hash: hash, Author: "Ada", Text: "second"},
	}}
	h, document := openCodeBlameDocument(t, session)
	h.waitFor(t, "initial blame", func() bool { return document.blameLoaded })

	document.state.Insert("edited\n")
	h.app.syncCodeBlame(document)
	if document.blameLoaded || document.blameLoading || len(document.blameInlays) != 0 {
		t.Fatal("editing did not invalidate saved-buffer blame")
	}
	if !document.state.Buffer.Undo() || document.state.Buffer.Modified() {
		t.Fatal("undo did not restore the saved buffer")
	}
	h.app.syncCodeBlame(document)
	h.waitFor(t, "blame after undo to saved buffer", func() bool { return document.blameLoaded })
	if len(document.blameInlays) != 1 || !document.blameInlays[0].Trailing {
		t.Fatalf("restored blame annotation = %#v", document.blameInlays)
	}
}

func TestCodeBlameStaleAnnotationsDoNotRepaint(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	session := &codeBlameTestSession{blameStarted: started, blameRelease: release}
	document := &codeDocument{path: "notes.txt", state: &editor.State{Buffer: editor.NewBuffer("first\n")}, blameEnabled: true}
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionCode
	a.code.documents = map[string]*codeDocument{document.path: document}
	a.code.active = document.path
	acknowledged := acknowledgeAsyncPublications(a)

	a.requestCodeBlame(document)
	<-started
	document.generation++
	close(release)
	if <-acknowledged {
		t.Fatal("stale editor blame annotations requested a repaint")
	}
	if document.blameLoaded || len(document.blameLines) != 0 {
		t.Fatal("stale editor blame annotations were installed")
	}
}

func TestCodeBlameStaleCommitDetailsDoNotRepaint(t *testing.T) {
	hash := strings.Repeat("d", 40)
	started, release := make(chan struct{}), make(chan struct{})
	session := &codeBlameTestSession{
		commits:       []repository.Commit{{Hash: hash, ShortHash: "ddddddd", Subject: "Late details"}},
		searchStarted: started,
		searchRelease: release,
	}
	document := &codeDocument{path: "notes.txt", state: &editor.State{Buffer: editor.NewBuffer("first\n")}, blameEnabled: true}
	document.blameLoaded = true
	document.blameVersion = document.state.Buffer.Version()
	document.blameLines = []repository.BlameLine{{Hash: hash, Text: "first"}}
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionCode
	a.code.documents = map[string]*codeDocument{document.path: document}
	a.code.active = document.path
	acknowledged := acknowledgeAsyncPublications(a)

	a.openCodeBlameDetails(document, 0)
	<-started
	a.closeCodeBlameDetails()
	close(release)
	if <-acknowledged {
		t.Fatal("stale editor blame commit details requested a repaint")
	}
	if a.codeBlameHover.commit.Hash != "" {
		t.Fatal("stale editor blame commit details were installed")
	}
}

func TestCodeBlameHoverBelongsToOneDocument(t *testing.T) {
	uncommitted := strings.Repeat("0", 40)
	newDocument := func(path string) *codeDocument {
		state := &editor.State{Buffer: editor.NewBuffer("first\n")}
		return &codeDocument{
			path: path, state: state, blameEnabled: true, blameLoaded: true,
			blameVersion: state.Buffer.Version(),
			blameLines:   []repository.BlameLine{{Hash: uncommitted, Text: "first"}},
		}
	}
	first, second := newDocument("first.txt"), newDocument("second.txt")
	a := &app{}
	a.code.documents = map[string]*codeDocument{first.path: first, second.path: second}
	a.openCodeBlameDetails(first, 0)
	a.openCodeBlameDetails(second, 0)
	if !a.codeBlameHover.open || a.codeBlameHover.source != second || a.codeBlameHover.path != second.path {
		t.Fatalf("replacement blame hover = %#v", a.codeBlameHover)
	}
	a.closeCodeBlameDetailsFor(first)
	if !a.codeBlameHover.open || a.codeBlameHover.source != second {
		t.Fatal("closing the previous document dismissed the current document's hover")
	}
}
