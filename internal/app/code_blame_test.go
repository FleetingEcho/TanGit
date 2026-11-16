package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
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
	message       *repository.CommitMessage
	messageError  error
	files         []repository.CommitFile
	filesError    error
	filesStarted  chan struct{}
	filesRelease  chan struct{}
}

func (s *codeBlameTestSession) CommitMessage(ctx context.Context, commit repository.Commit) (repository.CommitMessage, error) {
	if s.messageError != nil {
		return repository.CommitMessage{}, s.messageError
	}
	if s.message != nil {
		return *s.message, nil
	}
	return s.fakeSession.CommitMessage(ctx, commit)
}

func (s *codeBlameTestSession) CommitFiles(ctx context.Context, commit repository.Commit) ([]repository.CommitFile, error) {
	if s.filesStarted != nil {
		close(s.filesStarted)
	}
	if s.filesRelease != nil {
		<-s.filesRelease
	}
	if s.filesError != nil {
		return nil, s.filesError
	}
	if s.files != nil {
		return append([]repository.CommitFile(nil), s.files...), nil
	}
	return s.fakeSession.CommitFiles(ctx, commit)
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

type codeBlameHistoryTestSession struct {
	*codeBlameTestSession
	files []repository.CommitFile
	diff  repository.Diff
}

func (s *codeBlameHistoryTestSession) CommitFiles(_ context.Context, commit repository.Commit) ([]repository.CommitFile, error) {
	s.calls.record("code-blame-history-files:" + commit.Hash)
	return append([]repository.CommitFile(nil), s.files...), nil
}

func (s *codeBlameHistoryTestSession) CommitFileDiff(_ context.Context, _ repository.Commit, file repository.CommitFile, _ repository.DiffOptions) (repository.Diff, error) {
	s.calls.record("code-blame-history-diff:" + file.Path)
	return s.diff, nil
}

func openCodeBlameDocument(t *testing.T, session *codeBlameTestSession) (*harness, *codeDocument) {
	t.Helper()
	return openCodeBlameTextDocument(t, session, "first\nsecond\n")
}

func openCodeBlameTextDocument(t *testing.T, session *codeBlameTestSession, content string) (*harness, *codeDocument) {
	t.Helper()
	stubCodeWorkspace(t, map[string]string{"notes.txt": content})
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

	h.app.session = &codeBlameHistoryTestSession{
		codeBlameTestSession: session,
		files:                []repository.CommitFile{{Path: "other.txt", Status: "M"}, {Path: "notes.txt", Status: "M"}},
		diff:                 repository.Diff{OldText: "old first\nold second\n", NewText: "first\nsecond\n", Kind: "Commit"},
	}
	if err := h.tester.Click("Open in History"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the blamed commit in History", func() bool {
		return h.app.workspaceTab == sectionHistory && h.app.selectedCommit == 0 &&
			len(h.app.historyFilter.matches) == 1 && !h.app.historyFilter.loading &&
			h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	if h.app.codeBlameHover.open || h.app.historyFilter.applied.Query != hash ||
		h.app.historyFilter.applied.Branch != "" || h.app.historyFilter.matches[0].Hash != hash {
		t.Fatalf("History did not select exact blame commit: dialog=%#v query=%#v commits=%#v",
			h.app.codeBlameHover, h.app.historyFilter.applied, h.app.historyFilter.matches)
	}
	// BlameLine coordinates added later may be absent in older/incomplete
	// data; the hover's current path and one-based line are the fallback.
	if h.app.selectedCommitFile != 1 || h.app.commitDiffPath != "notes.txt" || h.app.commitDiffState.ScrollY() != 0 {
		t.Fatalf("History fallback opened file %d %q at scroll %v, want notes.txt line 1",
			h.app.selectedCommitFile, h.app.commitDiffPath, h.app.commitDiffState.ScrollY())
	}
}

func TestCodeBlameHistoryScrollsBlamedFileIntoView(t *testing.T) {
	hash := strings.Repeat("e", 40)
	session := &codeBlameTestSession{
		lines: []repository.BlameLine{{Hash: hash, Text: "first"}},
		commits: []repository.Commit{{
			Hash: hash, ShortHash: "eeeeeee", Subject: "A commit with many files",
		}},
	}
	h, document := openCodeBlameDocument(t, session)
	// The file list shares its height with the diff only in the top/bottom
	// layout; this test asserts against the height the divider stores.
	h.app.commitDetailsLayout = commitDetailsTopBottom
	h.waitFor(t, "working-tree blame", func() bool { return document.blameLoaded })
	snapshot := document.state.Buffer.Snapshot()
	document.state.OnHover(editor.HoverRequest{Offset: snapshot.LineEnd(0), Version: snapshot.Version()})
	h.waitFor(t, "commit details", func() bool {
		return h.app.codeBlameHover.open && !h.app.codeBlameHover.loading
	})

	files := make([]repository.CommitFile, 4)
	for index := range files {
		files[index] = repository.CommitFile{Path: fmt.Sprintf("file-%02d.txt", index), Status: "M"}
	}
	const targetSource = 4
	files = append(files, repository.CommitFile{Path: "notes.txt", Status: "M"})
	h.app.session = &codeBlameHistoryTestSession{
		codeBlameTestSession: session,
		files:                files,
		diff:                 repository.Diff{NewText: "first\nsecond\n", Kind: "Commit"},
	}
	if err := h.tester.Click("Open in History"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the blamed file in a long commit file list", func() bool {
		return h.app.workspaceTab == sectionHistory && h.app.selectedCommitFile == targetSource &&
			h.app.selectedCommitFileRow >= 0 && h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	first, last := h.app.commitFileList.Visible()
	if selected := h.app.selectedCommitFileRow; selected < first || selected > last {
		t.Fatalf("selected file row %d is outside the visible rows %d-%d", selected, first, last)
	}
	row, ok := h.tester.Find("notes.txt")
	if !ok {
		t.Fatal("the selected file was not rendered in the visible list")
	}
	header, ok := h.tester.Find("Commit files")
	if !ok {
		t.Fatal("the commit file list header is missing")
	}
	if row.Y < header.Y || row.Y+row.H > header.Y+h.app.commitFilesHeight {
		t.Fatalf("selected file bounds %v are outside the commit file pane ending at %v", row, header.Y+h.app.commitFilesHeight)
	}

	// Reveal the choice when it changes, without pulling the list back after
	// the user deliberately scrolls to another part of it.
	h.app.commitFileList.ScrollIntoView(0)
	h.tester.Frame()
	if first, _ := h.app.commitFileList.Visible(); first != 0 {
		t.Fatalf("a later frame overrode manual scrolling: visible rows start at %d, want 0", first)
	}
}

func TestCodeBlameHistoryOpensBlamedFileAtCommitLine(t *testing.T) {
	hash := strings.Repeat("f", 40)
	var content, oldContent strings.Builder
	blame := make([]repository.BlameLine, 40)
	for index := range blame {
		line := index + 1
		fmt.Fprintf(&content, "line %d\n", line)
		fmt.Fprintf(&oldContent, "old line %d\n", line)
		blame[index] = repository.BlameLine{
			Hash: hash, CommitPath: "dir/notes.txt", CommitLine: line + 3,
			Author: "Ada", Text: fmt.Sprintf("line %d", line),
		}
	}
	session := &codeBlameTestSession{
		lines: blame,
		commits: []repository.Commit{{
			Hash: hash, ShortHash: "fffffff", Subject: "Track the blamed line",
			AuthorName: "Ada", AuthorEmail: "ada@example.com",
		}},
	}
	h, document := openCodeBlameTextDocument(t, session, content.String())
	h.waitFor(t, "blame for the source file", func() bool { return document.blameLoaded })
	history := &codeBlameHistoryTestSession{
		codeBlameTestSession: session,
		files:                []repository.CommitFile{{Path: "other.txt", Status: "M"}, {Path: "dir/notes.txt", Status: "M"}},
		diff: repository.Diff{
			OldText: "history 1\nhistory 2\nhistory 3\n" + oldContent.String(),
			NewText: "history 1\nhistory 2\nhistory 3\n" + content.String(),
			Kind:    "Commit",
		},
	}
	h.app.session = history
	h.app.fileTreeCollapsed[fileTreeCollapseKey(fileTreeCommitScope, "dir")] = true

	document.state.GoTo(editor.Pos{Line: 11})
	h.app.syncCodeBlameActiveLine(document)
	snapshot := document.state.Buffer.Snapshot()
	document.state.OnHover(editor.HoverRequest{Offset: snapshot.LineEnd(11), Version: snapshot.Version()})
	h.waitFor(t, "the blamed line's commit details", func() bool {
		return h.app.codeBlameHover.open && !h.app.codeBlameHover.loading
	})
	if h.app.codeBlameHover.line != 12 {
		t.Fatalf("hover line = %d, want 12", h.app.codeBlameHover.line)
	}
	if err := h.tester.Click("Open in History"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the blamed file's diff", func() bool {
		return h.app.workspaceTab == sectionHistory && h.app.commitDiffState != nil && !h.app.commitDiffLoading
	})
	if h.app.selectedCommitFile != 1 || h.app.commitDiffPath != "dir/notes.txt" || h.app.historyFilter.matches[0].Hash != hash {
		t.Fatalf("History opened file %d %q for %v, want the pre-rename dir/notes.txt at %s",
			h.app.selectedCommitFile, h.app.commitDiffPath, h.app.historyFilter.matches, hash)
	}
	if h.app.fileTreeCollapsed[fileTreeCollapseKey(fileTreeCommitScope, "dir")] {
		t.Fatal("History left the blamed file's parent directory folded")
	}
	state := h.app.commitDiffState
	if got, want := state.ScrollY(), state.LineTop(editor.DiffNew, 14); got != want {
		t.Fatalf("diff scroll = %v, blamed commit line 15 top = %v, height = %v, viewport = %v; target=%#v", got, want,
			state.LineTop(editor.DiffNew, state.New.LineCount()), state.ViewportHeight(), h.app.codeBlameHistoryTarget)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "code-blame-history-commit-line", h.tester)
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

func TestCodeBlameCardLoadsMessageFilesAndExactTotals(t *testing.T) {
	hash := strings.Repeat("c", 40)
	session := &codeBlameTestSession{
		lines:   []repository.BlameLine{{Hash: hash, Author: "Ada Lovelace", Current: true, UnixTimestamp: timeNow() - 3*86400, Subject: "Improve server errors", Text: "first"}},
		commits: []repository.Commit{{Hash: hash, Subject: "Improve server errors", AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com", UnixTimestamp: timeNow() - 3*86400, Parents: []string{strings.Repeat("d", 40)}, Tags: []string{"v2.0"}}},
		message: &repository.CommitMessage{Summary: "Improve server errors", Description: "Include the original error for better debugging."},
		files: []repository.CommitFile{
			{Path: "server/server.go", LinesAdded: 2, LinesDeleted: 1},
			{Path: "server/config.go", PreviousPath: "old/config.go", LinesAdded: 4},
			{Path: "docs/server.md", LinesAdded: 12, LinesDeleted: 3},
		},
	}
	h, document := openCodeBlameDocument(t, session)
	h.waitFor(t, "working-tree blame", func() bool { return document.blameLoaded })
	h.app.snapshot.HeadHash = hash
	h.app.openCodeBlameDetails(document, 0)
	h.waitFor(t, "rich commit details", func() bool { return !h.app.codeBlameHover.loading })
	h.tester.Frame()
	if h.app.codeBlameHover.filesCollapsed || !h.app.codeBlameHover.infoExpanded {
		t.Fatal("commit card sections must default to expanded")
	}
	if bounds, ok := h.tester.Find("Changed files"); !ok || bounds.H != 144 {
		t.Fatalf("file list does not retain its fixed height: %+v", bounds)
	}
	for _, text := range []string{"Improve server errors", "Include the original error for better debugging.", "You", "v2.0", "Changes in this commit (3)", "server/server.go", "old/config.go → server/config.go", "+18", "−4"} {
		if !h.tester.HasText(text) {
			t.Errorf("card missing %q: %v", text, h.texts())
		}
	}
	if err := h.tester.Click("Changes in this commit (3)"); err != nil {
		t.Fatal(err)
	}
	if h.tester.HasText("server/server.go") || !h.tester.HasText("+18") {
		t.Fatal("collapsing files lost totals or retained file rows")
	}
	if err := h.tester.Click("Changes in this commit (3)"); err != nil {
		t.Fatal(err)
	}
	if !h.tester.HasText("ada@example.com") || !h.tester.HasText("ddddddd") {
		t.Fatal("expanded metadata missing author or parent")
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "blame-commit-card-editor", h.tester)
	}
}

func TestCodeBlameCardKeepsPartialDetailsOnReadFailure(t *testing.T) {
	for _, part := range []string{"message", "files"} {
		t.Run(part, func(t *testing.T) {
			hash := strings.Repeat("c", 40)
			session := &codeBlameTestSession{
				lines:   []repository.BlameLine{{Hash: hash, Author: "Ada", Text: "first"}},
				commits: []repository.Commit{{Hash: hash, Subject: "Keep the commit visible"}},
			}
			if part == "message" {
				session.messageError = errors.New("message unavailable")
			} else {
				session.filesError = errors.New("files unavailable")
			}
			h, document := openCodeBlameDocument(t, session)
			h.waitFor(t, "working-tree blame", func() bool { return document.blameLoaded })
			h.app.openCodeBlameDetails(document, 0)
			h.waitFor(t, "partial details", func() bool { return !h.app.codeBlameHover.loading })
			h.tester.Frame()
			if h.app.codeBlameHover.commit.Hash != hash || !h.tester.HasText(part+" unavailable") {
				t.Fatal("partial failure discarded the commit or error")
			}
			if part == "message" && !h.tester.HasText("Changes in this commit (2)") {
				t.Fatal("message failure discarded files")
			}
			if part == "files" && (!h.tester.HasText("Existing body") || h.tester.HasText("Changes in this commit (0)")) {
				t.Fatal("file failure lost body or presented a false empty commit")
			}
		})
	}
}

func TestCodeBlameRejectsLateFileDetailsAfterClose(t *testing.T) {
	hash := strings.Repeat("c", 40)
	session := &codeBlameTestSession{
		lines:        []repository.BlameLine{{Hash: hash, Text: "first"}},
		commits:      []repository.Commit{{Hash: hash, Subject: "Old commit"}},
		filesStarted: make(chan struct{}), filesRelease: make(chan struct{}),
	}
	h, document := openCodeBlameDocument(t, session)
	h.waitFor(t, "working-tree blame", func() bool { return document.blameLoaded })
	completed := make(chan bool, 1)
	updateChanged := h.app.updateChanged
	h.app.updateChanged = func(fn func() bool) {
		updateChanged(func() bool {
			changed := fn()
			completed <- changed
			return changed
		})
	}
	h.app.openCodeBlameDetails(document, 0)
	<-session.filesStarted
	h.app.closeCodeBlameDetails()
	close(session.filesRelease)
	h.waitFor(t, "late file detail publication", func() bool {
		select {
		case changed := <-completed:
			if changed {
				t.Error("stale file details requested a repaint")
			}
			return true
		default:
			return false
		}
	})
	if h.app.codeBlameHover.open || h.app.codeBlameHover.filesLoaded || h.app.codeBlameHover.commit.Hash != "" {
		t.Fatal("closed card accepted late file details")
	}
}

func TestCodeBlameCardFitsSmallWindowsAndLargeFonts(t *testing.T) {
	for _, size := range []struct {
		width, height int
		font          float32
	}{{720, 680, 14}, {360, 320, 20}} {
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			a := newApp(fakeOpener{})
			a.codeBlameHover = codeBlameHoverState{
				open: true, path: "server/server.go", line: 42,
				blame:       repository.BlameLine{Hash: strings.Repeat("a", 40), Current: true, UnixTimestamp: timeNow() - 3*86400},
				commit:      repository.Commit{Subject: "Improve server errors", AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com", Parents: []string{strings.Repeat("b", 40)}},
				description: "Include the original error for better debugging.", filesLoaded: true, infoExpanded: true,
			}
			for i := 0; i < 30; i++ {
				a.codeBlameHover.files = append(a.codeBlameHover.files, repository.CommitFile{Path: fmt.Sprintf("server/very-long-folder-name/config-%02d.go", i), LinesAdded: i, LinesDeleted: 1})
			}
			tt := ui.NewTester(func(c *ui.Context) {
				theme := *c.Theme()
				theme.FontSize = size.font
				ui.Themed(c, &theme).Fill().Children(func() {
					ui.Column(c).Fill().Padding(12).Background(c.Theme().Background).Children(func() {
						ui.Column(c).Padding(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() { a.codeBlameDetailsCard(c) })
					})
				})
			}, size.width, size.height)
			for _, label := range []string{"Copy commit hash", "Open in History", "Changes in this commit (30)"} {
				bounds, ok := tt.Find(label)
				if !ok || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > float32(size.width) || bounds.Y+bounds.H > float32(size.height) {
					t.Fatalf("%s outside viewport: %+v", label, bounds)
				}
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, fmt.Sprintf("blame-card-%dx%d-light", size.width, size.height), tt)
				tt.SetDark(true)
				saveScreenshot(t, dir, fmt.Sprintf("blame-card-%dx%d-dark", size.width, size.height), tt)
			}
			// Scroll over the header, outside the nested file list, to reach
			// metadata even when large text makes the entire card overflow.
			tt.Scroll(40, 50, 0, 1600)
			tt.Frame()
			bounds, ok := tt.Find("server/server.go:42")
			if !ok || bounds.Y < 0 || bounds.Y+bounds.H > float32(size.height) {
				t.Fatalf("card footer remains unreachable after scrolling: %+v", bounds)
			}
			a.codeBlameHover.token++ // a newly opened card starts at its author header
			tt.Frame()
			bounds, ok = tt.Find("Copy commit hash")
			if !ok || bounds.Y < 0 || bounds.Y+bounds.H > float32(size.height) {
				t.Fatalf("new card retained the old scroll position: %+v", bounds)
			}
		})
	}
}

func TestCodeBlameFileListHasFixedHeightAndScrollsIndependently(t *testing.T) {
	a := newApp(fakeOpener{})
	a.codeBlameHover = codeBlameHoverState{
		open: true, path: "server/server.go", line: 42,
		blame:       repository.BlameLine{Hash: strings.Repeat("a", 40)},
		commit:      repository.Commit{Subject: "Improve server errors", AuthorName: "Ada", AuthorEmail: "ada@example.com", UnixTimestamp: timeNow() - 86400},
		filesLoaded: true, infoExpanded: true,
	}
	for i := 0; i < 30; i++ {
		a.codeBlameHover.files = append(a.codeBlameHover.files, repository.CommitFile{Path: fmt.Sprintf("server/file-%02d.go", i)})
	}
	a.codeBlameHover.files[0].LinesAdded = 1100
	a.codeBlameHover.files[0].LinesDeleted = 10
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(12).Background(c.Theme().Background).Children(func() {
			ui.Column(c).Padding(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() { a.codeBlameDetailsCard(c) })
		})
	}, 720, 780)
	list, ok := tt.Find("Changed files")
	if !ok || list.H != 144 {
		t.Fatalf("file list height = %+v, want 144", list)
	}
	info, _ := tt.Find("More info")
	insertions, _ := tt.Find("Insertions")
	if !tt.HasText("+1100") || !tt.HasText("−10") || !tt.HasText("Deletions") {
		t.Fatal("exact commit totals missing")
	}
	tt.Scroll(list.X+list.W/2, list.Y+list.H/2, 0, 2400)
	tt.Frame()
	last, ok := tt.Find("server/file-29.go")
	if !ok || last.Y < list.Y || last.Y+last.H > list.Y+list.H {
		t.Fatalf("last file is not reachable inside its scroll region: %+v list %+v", last, list)
	}
	afterInfo, _ := tt.Find("More info")
	afterInsertions, _ := tt.Find("Insertions")
	if afterInfo != info || afterInsertions != insertions {
		t.Fatal("scrolling files moved the metadata or commit totals")
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "blame-fixed-file-list-scrolled", tt)
	}
	a.codeBlameHover.files = a.codeBlameHover.files[:1]
	tt.Frame()
	shortList, _ := tt.Find("Changed files")
	shortInfo, _ := tt.Find("More info")
	if shortList.H != 144 || shortInfo != info {
		t.Fatalf("one file changed the fixed layout: list %+v info %+v, want %+v", shortList, shortInfo, info)
	}
}

func TestCodeBlameCardUsesWindowHeightToShowExpandedContent(t *testing.T) {
	a := newApp(fakeOpener{})
	a.codeBlameHover = codeBlameHoverState{
		open: true, path: "server/server.go", line: 42,
		blame:       repository.BlameLine{Hash: strings.Repeat("a", 40)},
		commit:      repository.Commit{Subject: "Improve server errors", AuthorName: "Ada", AuthorEmail: "ada@example.com", Parents: []string{strings.Repeat("b", 40)}, UnixTimestamp: timeNow() - 86400},
		description: strings.Repeat("Explain another part of the server change.\n", 16),
		filesLoaded: true, infoExpanded: true,
		files: []repository.CommitFile{{Path: "server/server.go", LinesAdded: 1100, LinesDeleted: 10}},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(12).Background(c.Theme().Background).Children(func() {
			ui.Column(c).Padding(8).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() { a.codeBlameDetailsCard(c) })
		})
	}, 720, 1000)
	card, ok := tt.Find("Commit details")
	if !ok || card.H <= 620 {
		t.Fatalf("card retained an arbitrary height cap: %+v", card)
	}
	for _, label := range []string{"Copy commit hash", "Changed files", "Insertions", "Deletions", "server/server.go:42"} {
		bounds, ok := tt.Find(label)
		if !ok || bounds.Y < card.Y || bounds.Y+bounds.H > card.Y+card.H || bounds.Y+bounds.H > 1000 {
			t.Fatalf("%s is not visible without scrolling: %+v card %+v", label, bounds, card)
		}
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "blame-expanded-card-full-height", tt)
	}
}

func TestCodeBlameNativeGoHoverFitsAndPrioritizesCurrentFile(t *testing.T) {
	for _, tc := range []struct {
		width, height int
		font          float64
		line          int
	}{{1280, 800, 13, 0}, {1280, 1000, 18, 0}, {900, 900, 16, 0}, {1280, 800, 13, 16}} {
		t.Run(fmt.Sprintf("%dx%d-font%.0f-line%d", tc.width, tc.height, tc.font, tc.line), func(t *testing.T) {
			h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{"main.go": "package main\n" + strings.Repeat("// source line\n", 40)}, func(method string, _ any, result func(json.RawMessage, error)) {
				if method == "textDocument/hover" {
					result(json.RawMessage(`{"contents":{"kind":"plaintext","value":"Language-server documentation"}}`), nil)
				}
			})
			h.app.settings.FontSize = tc.font
			h.tester.SetSize(tc.width, tc.height)
			hash := strings.Repeat("9", 40)
			subject := "Add expandable branch views and refresh bilingual documentation with complete descriptions"
			session := &codeBlameTestSession{
				fakeSession: fakeSession{calls: &callRecorder{}},
				lines:       []repository.BlameLine{{Hash: hash, Author: "JakeZT", AuthorEmail: "author@example.com", Subject: subject, Current: true, Text: "package main"}},
				commits:     []repository.Commit{{Hash: hash, Subject: subject, AuthorName: "JakeZT", AuthorEmail: "author@example.com", UnixTimestamp: timeNow() - 7*86400, Parents: []string{strings.Repeat("7", 40)}}},
				message:     &repository.CommitMessage{Summary: subject},
			}
			for i := 0; i < 21; i++ {
				session.files = append(session.files, repository.CommitFile{Path: fmt.Sprintf("internal/app/a-very-long-directory-name/changed-file-%02d_test.go", i)})
			}
			session.files[17] = repository.CommitFile{Path: "main.go", LinesAdded: 1100, LinesDeleted: 10}
			first := session.lines[0]
			for len(session.lines) < 41 {
				line := first
				line.Text = "// source line"
				session.lines = append(session.lines, line)
			}
			h.app.session = session
			document.state.GoTo(editor.Pos{Line: tc.line})
			h.app.requestCodeBlame(document)
			h.waitFor(t, "Go working-tree blame", func() bool { return document.blameLoaded })
			document.state.GoTo(editor.Pos{Line: tc.line})
			h.app.syncCodeBlameActiveLine(document)
			h.tester.Frame()
			snapshot := document.state.Buffer.Snapshot()
			document.state.OnHover(editor.HoverRequest{Offset: snapshot.LineEnd(tc.line), Version: snapshot.Version()})
			h.waitFor(t, "native Go blame card", func() bool { return h.app.codeBlameHover.open && !h.app.codeBlameHover.loading })
			h.tester.Frame()
			panel, ok := h.tester.AccessByRole(ui.RoleTooltip, "")
			if !ok {
				t.Fatal("native editor hover not shown")
			}
			if panel.Bounds.Y < 0 || panel.Bounds.Y+panel.Bounds.H > float32(tc.height) {
				t.Fatalf("hover outside window: %+v", panel.Bounds)
			}
			for _, label := range []string{"Copy commit hash", "Open in History", "Insertions", "Deletions", "+1100", "−10", fmt.Sprintf("main.go:%d", tc.line+1)} {
				bounds, ok := h.tester.Find(label)
				if !ok || bounds.X < panel.Bounds.X || bounds.X+bounds.W > panel.Bounds.X+panel.Bounds.W || bounds.Y < panel.Bounds.Y || bounds.Y+bounds.H > panel.Bounds.Y+panel.Bounds.H {
					t.Fatalf("%s clipped by native hover: %+v panel %+v", label, bounds, panel.Bounds)
				}
			}
			title, ok := h.tester.Find(subject)
			if !ok || title.W > panel.Bounds.W-24+0.5 || title.H < float32(tc.font)*2 {
				t.Fatalf("long subject did not wrap inside the card: %+v", title)
			}
			current, ok := h.tester.Find("Current file: main.go")
			next, found := h.tester.Find(session.files[0].Path)
			if !ok || !found || current.Y >= next.Y {
				t.Fatalf("current file was not placed first: %+v %+v", current, next)
			}

			photo := h.tester.Image()
			sample := func(r ui.Rect) color.RGBA {
				return color.RGBAModel.Convert(photo.At(int(r.X+3), int(r.Y+r.H/2))).(color.RGBA)
			}
			if sample(current) == sample(next) {
				t.Fatal("current file has no distinct background")
			}
			if document.hoverScroll.MaxY != 0 {
				t.Fatalf("blame still inherits the LSP scroller: %+v", document.hoverScroll)
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, fmt.Sprintf("native-go-blame-%dx%d-font%.0f-line%d", tc.width, tc.height, tc.font, tc.line), h.tester)
			}
			// Returning to symbol hover must still use the document's LSP renderer.
			document.state.OnHover(editor.HoverRequest{Offset: 1, Version: snapshot.Version()})
			h.waitFor(t, "language hover after blame", func() bool {
				return document.hoverState != nil && document.hoverState.Buffer.String() == "Language-server documentation"
			})
			if h.app.codeBlameHover.open {
				t.Fatal("symbol hover retained the blame card")
			}
		})
	}
}

func TestCodeBlameCurrentFileMatchesHistoricalAndRenamedPaths(t *testing.T) {
	for _, tc := range []struct {
		name, current, historical string
		files                     []repository.CommitFile
		want                      int
	}{
		{"working path", "current.go", "", []repository.CommitFile{{Path: "other.go"}, {Path: "current.go"}}, 1},
		{"historical path", "new.go", "old.go", []repository.CommitFile{{Path: "other.go"}, {Path: "old.go"}}, 1},
		{"rename in commit", "latest.go", "old.go", []repository.CommitFile{{Path: "other.go"}, {Path: "renamed.go", PreviousPath: "old.go"}}, 1},
		{"no matching file", "missing.go", "", []repository.CommitFile{{Path: "other.go"}}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hover := codeBlameHoverState{path: tc.current, blame: repository.BlameLine{CommitPath: tc.historical}, files: tc.files}
			if got := codeBlameCurrentFileIndex(&hover); got != tc.want {
				t.Fatalf("current file = %d, want %d", got, tc.want)
			}
		})
	}
}
