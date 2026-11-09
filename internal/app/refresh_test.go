package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// liveOpener hands out sessions that read the snapshot it holds now, so a
// test can change the repository, as another app would, between reads.
type liveOpener struct {
	mu       sync.Mutex
	snapshot repository.Snapshot
}

func (o *liveOpener) Init(context.Context, string) error { return nil }

func (o *liveOpener) Open(_ context.Context, path string) (repository.Session, error) {
	snapshot := o.current()
	snapshot.Root = path
	return liveSession{opener: o, fakeSession: fakeSession{snapshot: snapshot}}, nil
}

func (o *liveOpener) current() repository.Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.snapshot
}

func (o *liveOpener) set(snapshot repository.Snapshot) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.snapshot = snapshot
}

// liveSession is a fake whose Snapshot reads the opener, so the same session
// sees a change another app made.
type liveSession struct {
	fakeSession
	opener *liveOpener
}

func (s liveSession) Snapshot(context.Context) (repository.Snapshot, error) {
	return s.opener.current(), nil
}

type countingLiveSession struct {
	liveSession
	hits int32
}

func (s *countingLiveSession) Snapshot(ctx context.Context) (repository.Snapshot, error) {
	atomic.AddInt32(&s.hits, 1)
	return s.liveSession.Snapshot(ctx)
}

// countingSession counts the snapshot reads a quiet refresh asked for.
type countingSession struct {
	fakeSession
	hits *int32
}

func (s countingSession) Snapshot(context.Context) (repository.Snapshot, error) {
	atomic.AddInt32(s.hits, 1)
	return s.fakeSession.snapshot, nil
}

type blockingSnapshotSession struct {
	fakeSession
	started chan struct{}
	release chan struct{}
	once    sync.Once
	hits    int32
}

func (s *blockingSnapshotSession) Snapshot(context.Context) (repository.Snapshot, error) {
	atomic.AddInt32(&s.hits, 1)
	s.once.Do(func() { close(s.started) })
	<-s.release
	return s.fakeSession.snapshot, nil
}

type blockingPanickingSnapshotSession struct {
	fakeSession
	started chan struct{}
	release chan struct{}
}

func (s *blockingPanickingSnapshotSession) Snapshot(context.Context) (repository.Snapshot, error) {
	close(s.started)
	<-s.release
	panic("the old repository read failed")
}

func (s *blockingPanickingSnapshotSession) CommandLog() *repository.CommandLog { return nil }

// panickingSession fails its first snapshot read with a panic, as a fault in
// the Git layer would, and reads normally after.
type panickingSession struct {
	fakeSession
	reads int
}

func (s *panickingSession) Snapshot(context.Context) (repository.Snapshot, error) {
	s.reads++
	if s.reads == 1 {
		panic("the repository read failed")
	}
	return s.fakeSession.snapshot, nil
}

// newLiveShell builds the tabbed window on a repository that can change.
func newLiveShell(t *testing.T, opener repositoryOpener) (*shell, *ui.Tester, *updates) {
	t.Helper()
	s := newShell(opener)
	s.pathUserData = t.TempDir()
	updated := newUpdates()
	s.update = updated.update
	s.invalidate = func() {}
	s.start("")
	tt := ui.NewTester(s.view, 1280, 800)
	tt.Frame()
	return s, tt, updated
}

// openLiveRepo opens path without drawing a frame, so a test starts from a
// settled app with no diff read on its way.
func openLiveRepo(t *testing.T, s *shell, updated *updates, path string) *app {
	t.Helper()
	a := s.activeApp()
	a.path = path
	a.beginOpen()
	for a.state != stateReady {
		if a.state == stateError || a.state == stateEmpty {
			t.Fatalf("the repository did not open: %v %q", a.state, a.errMessage)
		}
		updated.waitForUpdate(t)
	}
	return a
}

// readyTab adds a settled page reading through session, whose snapshot it
// already holds, so a test can count or break the reads a refresh asks for.
func readyTab(s *shell, root string, session repository.Session) *tab {
	t := s.newTab(root)
	t.app.state = stateReady
	t.app.workspaceTab = sectionChanges
	t.app.path = root
	snapshot := repository.Snapshot{Root: root, Branch: "main"}
	t.app.snapshot = snapshot
	t.app.session = session
	s.tabs = append(s.tabs, t)
	return t
}

// readyCountingTab adds a settled page whose snapshot reads are counted.
func readyCountingTab(s *shell, root string, hits *int32) *tab {
	snapshot := repository.Snapshot{Root: root, Branch: "main"}
	return readyTab(s, root, countingSession{fakeSession: fakeSession{snapshot: snapshot}, hits: hits})
}

// changedFile is a working-tree change the quiet refresh can put on the list.
func changedFile(path string) repository.File {
	return repository.File{Path: path, Status: "M", Tracked: true, Unstaged: true}
}

func TestCodeFileSignatureIgnoresEditsButTracksPathChanges(t *testing.T) {
	clean := repository.Snapshot{Root: "/work/repo"}
	edited := repository.Snapshot{Root: "/work/repo", Files: []repository.File{{
		Path: "main.go", Status: " M", Tracked: true, Unstaged: true, LinesAdded: 3,
	}}}
	editedAgain := edited
	editedAgain.Files = append([]repository.File(nil), edited.Files...)
	editedAgain.Files[0].LinesAdded = 7
	if got, want := signSnapshot(edited).codeFiles, signSnapshot(clean).codeFiles; got != want {
		t.Fatalf("editing a tracked file changed the Code index signature: %q != %q", got, want)
	}
	if got, want := signSnapshot(editedAgain).codeFiles, signSnapshot(edited).codeFiles; got != want {
		t.Fatalf("saving more content changed the Code index signature: %q != %q", got, want)
	}

	untracked := edited
	untracked.Files = append(untracked.Files, repository.File{Path: "new.go", Status: "??", Added: true})
	deleted := edited
	deleted.Files = append(deleted.Files, repository.File{Path: "old.go", Status: " D", Tracked: true, Deleted: true})
	renamed := edited
	renamed.Files = append(renamed.Files, repository.File{
		Path: "new-name.go", PreviousPath: "old-name.go", Status: "R ", Tracked: true,
	})
	for name, snapshot := range map[string]repository.Snapshot{
		"untracked file": untracked,
		"deleted file":   deleted,
		"renamed file":   renamed,
	} {
		if signSnapshot(snapshot).codeFiles == signSnapshot(edited).codeFiles {
			t.Errorf("%s did not change the Code index signature", name)
		}
	}
}

func TestQuietRefreshDoesNotReindexCodeTreeForContentEdits(t *testing.T) {
	opener := &liveOpener{snapshot: repository.Snapshot{Root: "/work/repo", Branch: "main"}}
	s, tt, updated := newLiveShell(t, opener)
	openShellRepo(t, s, tt, updated, "/work/repo")
	a := s.activeApp()
	a.code.filesLoaded = true
	a.code.filesLoading = false
	a.code.files = []string{"main.go"}
	token := a.code.filesToken

	opener.set(repository.Snapshot{
		Root: "/work/repo", Branch: "main", Files: []repository.File{changedFile("main.go")},
	})
	a.requestQuietRefresh()
	drainRefresh(t, updated, 1)
	if a.code.filesToken != token || !a.code.filesLoaded || a.code.filesLoading {
		t.Fatalf("content edit reindexed Code tree: token %d->%d loaded=%v loading=%v",
			token, a.code.filesToken, a.code.filesLoaded, a.code.filesLoading)
	}
}

func TestQuietRefreshReindexesCodeAfterExternalCheckout(t *testing.T) {
	originalList := listCodeFiles
	t.Cleanup(func() { listCodeFiles = originalList })
	listCodeFiles = func(string) ([]string, error) { return []string{"new-branch.go"}, nil }
	opener := &liveOpener{snapshot: repository.Snapshot{Root: "/work/repo", Branch: "main", Head: "old-head"}}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, "/work/repo")
	a.code.filesLoaded = true
	a.code.files = []string{"old-branch.go"}
	token := a.code.filesToken

	opener.set(repository.Snapshot{Root: "/work/repo", Branch: "feature", Head: "new-head"})
	a.requestQuietRefresh()
	drainRefresh(t, updated, 1)
	updated.waitForUpdate(t)
	if a.code.filesToken == token || !a.code.filesLoaded || a.code.filesLoading ||
		len(a.code.files) != 1 || a.code.files[0] != "new-branch.go" {
		t.Fatalf("external checkout left a stale Code index: token %d->%d loaded=%v loading=%v files=%q",
			token, a.code.filesToken, a.code.filesLoaded, a.code.filesLoading, a.code.files)
	}
}

// drainRefresh runs the reads the window queued, as the UI loop would.
func drainRefresh(t *testing.T, updated *updates, reads int) {
	t.Helper()
	for range reads {
		updated.waitForUpdate(t)
	}
}

// A file and a commit made in another app appear without the user pressing
// refresh.
func TestAQuietRefreshShowsChangesMadeByAnotherApp(t *testing.T) {
	opener := &liveOpener{snapshot: repository.Snapshot{
		Root:    "/work/repo",
		Branch:  "main",
		Files:   []repository.File{changedFile("a.txt")},
		Commits: []repository.Commit{{Hash: "1111111", ShortHash: "1111111", Subject: "first"}},
	}}
	s, tt, updated := newLiveShell(t, opener)
	s.activeApp().workspaceTab = sectionChanges
	openShellRepo(t, s, tt, updated, "/work/repo")
	a := s.activeApp()
	tt.Frame()

	opener.set(repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{changedFile("a.txt"), changedFile("new.txt")},
		Commits: []repository.Commit{
			{Hash: "2222222", ShortHash: "2222222", Subject: "made elsewhere"},
			{Hash: "1111111", ShortHash: "1111111", Subject: "first"},
		},
	})
	a.requestQuietRefresh()
	drainRefresh(t, updated, 1)
	tt.Frame()

	seen := false
	for _, file := range a.snapshot.Files {
		if file.Path == "new.txt" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("the file another app made did not appear: %v", a.snapshot.Files)
	}
	if len(a.snapshot.Commits) != 2 || a.snapshot.Commits[0].Hash != "2222222" {
		t.Fatalf("the commit another app made did not appear: %v", a.snapshot.Commits)
	}
}

// The file the user chose stays chosen when a refresh moves it down the list.
func TestAQuietRefreshKeepsTheChosenFile(t *testing.T) {
	opener := &liveOpener{snapshot: repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{changedFile("a.txt"), changedFile("c.txt")},
	}}
	s, tt, updated := newLiveShell(t, opener)
	s.activeApp().workspaceTab = sectionChanges
	openShellRepo(t, s, tt, updated, "/work/repo")
	a := s.activeApp()
	tt.Frame()

	a.selectedFile = changeRowIndex(a.changeRows, "c.txt", false)
	if a.selectedFile < 0 {
		t.Fatalf("the test could not choose c.txt: %v", a.changeRows)
	}

	opener.set(repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{changedFile("a.txt"), changedFile("b.txt"), changedFile("c.txt")},
	})
	a.requestQuietRefresh()
	drainRefresh(t, updated, 1)

	change, ok := a.selectedChange()
	if !ok || change.file.Path != "c.txt" {
		t.Fatalf("the refresh moved the user's choice: %q (%v)", change.file.Path, a.changeRows)
	}
}

// Nothing is applied, and no frame is asked for, while the user is editing a
// file or holding a dialog.
func TestAQuietRefreshStandsAsideWhileTheUserIsBusy(t *testing.T) {
	original := repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{changedFile("a.txt")},
	}
	opener := &liveOpener{snapshot: original}
	s, tt, updated := newLiveShell(t, opener)
	s.activeApp().workspaceTab = sectionChanges
	openShellRepo(t, s, tt, updated, "/work/repo")
	a := s.activeApp()
	tt.Frame()

	cases := []struct {
		name string
		busy func(bool)
	}{
		{"editing a file", func(on bool) { a.fileEditing = on }},
		{"a dialog", func(on bool) { a.settingsOpen = on }},
		{"a diff line selection", func(on bool) { a.lineSelectMode = on }},
	}
	for _, tc := range cases {
		tc.busy(true)
		opener.set(repository.Snapshot{
			Root:   "/work/repo",
			Branch: "main",
			Files:  []repository.File{changedFile("a.txt"), changedFile("b.txt")},
		})
		a.requestQuietRefresh()
		if a.refreshBusy {
			t.Fatalf("a quiet refresh started while %s", tc.name)
		}
		if len(a.snapshot.Files) != 1 {
			t.Fatalf("a quiet refresh changed the snapshot while %s", tc.name)
		}
		tc.busy(false)
		opener.set(original)
	}
}

// A conflict resolved in another app is taken while its pane is open: a pick
// is written to the file at once, so nothing the user did is waiting in
// memory.
func TestAQuietRefreshTakesAConflictAnotherAppResolved(t *testing.T) {
	still := repository.File{
		Path: "a.txt", Status: "UU", Tracked: true, Unstaged: true, HasMergeConflicts: true,
	}
	gone := repository.File{
		Path: "b.txt", Status: "UU", Tracked: true, Unstaged: true, HasMergeConflicts: true,
	}
	opener := &liveOpener{snapshot: repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{still, gone},
	}}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, "/work/repo")
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, "a.txt", false)
	if a.selectedFile < 0 {
		t.Fatalf("the test could not choose the conflicted file: %v", a.changeRows)
	}

	opener.set(repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{still},
	})
	a.requestQuietRefresh()
	if !a.refreshBusy {
		t.Fatal("a quiet refresh did not start while a conflict was open")
	}
	drainRefresh(t, updated, 1)

	if len(a.snapshot.Files) != 1 || a.snapshot.Files[0].Path != "a.txt" {
		t.Fatalf("the conflict another app resolved is still listed: %v", a.snapshot.Files)
	}
	if change, ok := a.selectedChange(); !ok || change.file.Path != "a.txt" {
		t.Fatalf("the refresh moved the user's choice: %v", a.changeRows)
	}
}

// Text typed into the conflict editor is not on disk yet, so a refresh waits
// rather than dropping it.
func TestAQuietRefreshStandsAsideWhileConflictTextIsUnsaved(t *testing.T) {
	conflicted := repository.File{
		Path: "a.txt", Status: "UU", Tracked: true, Unstaged: true, HasMergeConflicts: true,
	}
	opener := &liveOpener{snapshot: repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{conflicted},
	}}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, "/work/repo")
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, "a.txt", false)

	buffer := editor.NewBuffer("one\ntwo\n")
	buffer.Set("one\ntwo\nthree\n")
	a.conflictView = &editor.State{Buffer: buffer}

	opener.set(repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{conflicted, changedFile("b.txt")},
	})
	a.requestQuietRefresh()
	if a.refreshBusy {
		t.Fatal("a quiet refresh started over text the editor had not written")
	}
	if len(a.snapshot.Files) != 1 {
		t.Fatal("a quiet refresh changed the snapshot under an unsaved edit")
	}
}

// The commit the user is reading stays chosen when another app commits above
// it: the refresh moves the list under the data, not under the user.
func TestAQuietRefreshKeepsTheChosenCommit(t *testing.T) {
	opener := &liveOpener{snapshot: repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Commits: []repository.Commit{
			{Hash: "2222222", ShortHash: "2222222", Subject: "second"},
			{Hash: "1111111", ShortHash: "1111111", Subject: "first"},
		},
	}}
	s, tt, updated := newLiveShell(t, opener)
	openShellRepo(t, s, tt, updated, "/work/repo")
	a := s.activeApp()
	a.selectedCommit = 1

	opener.set(repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Commits: []repository.Commit{
			{Hash: "3333333", ShortHash: "3333333", Subject: "made elsewhere"},
			{Hash: "2222222", ShortHash: "2222222", Subject: "second"},
			{Hash: "1111111", ShortHash: "1111111", Subject: "first"},
		},
	})
	a.requestQuietRefresh()
	drainRefresh(t, updated, 1)

	commits := a.visibleCommits()
	if a.selectedCommit < 0 || a.selectedCommit >= len(commits) {
		t.Fatalf("the refresh chose commit %d of %d", a.selectedCommit, len(commits))
	}
	if got := commits[a.selectedCommit].Hash; got != "1111111" {
		t.Fatalf("the refresh moved the commit under the user to %s", got)
	}
}

// An external save during a slow read is not dropped: exactly one follow-up
// read observes what changed after the first read started.
func TestAQuietRefreshQueuesAnEventThatArrivesWhileReading(t *testing.T) {
	s, _, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	session := &blockingSnapshotSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{
			Root: "/work/repo", Branch: "main", Files: []repository.File{changedFile("external.txt")},
		}},
		started: make(chan struct{}), release: make(chan struct{}),
	}
	a := readyTab(s, "/work/repo", session).app
	a.requestQuietRefresh()
	<-session.started
	a.requestQuietRefresh()
	if !a.refreshPending {
		t.Fatal("a refresh requested during a read was discarded")
	}
	close(session.release)
	drainRefresh(t, updated, 2)
	if a.refreshBusy || a.refreshPending {
		t.Fatalf("the queued refresh did not finish: busy=%v pending=%v", a.refreshBusy, a.refreshPending)
	}
	if got := atomic.LoadInt32(&session.hits); got != 2 {
		t.Fatalf("an event during a read caused %d reads, want 2", got)
	}
	if a.snapshot.ChangeCount() != 1 || a.snapshot.Files[0].Path != "external.txt" {
		t.Fatalf("the queued read missed the external change: %+v", a.snapshot.Files)
	}
}

// A Windows console process can briefly take and return focus while Git is
// being read. Returning focus must not turn one snapshot into an endless
// chain of snapshots; the read already in flight observes the current state.
func TestFocusDuringAQuietRefreshDoesNotQueueAnotherRead(t *testing.T) {
	s, _, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	session := &blockingSnapshotSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: "/work/repo", Branch: "main"}},
		started:     make(chan struct{}), release: make(chan struct{}),
	}
	tab := readyTab(s, "/work/repo", session)
	s.activate(s.indexOf(tab))
	<-session.started

	s.handleWindowBlurred()
	s.handleWindowFocused()
	if tab.app.refreshPending {
		t.Fatal("focus during a repository read queued another refresh")
	}

	close(session.release)
	drainRefresh(t, updated, 1)
	if got := atomic.LoadInt32(&session.hits); got != 1 {
		t.Fatalf("focus during a repository read caused %d reads, want 1", got)
	}
}

// An event during an action remains pending after the action's own snapshot
// lands. The follow-up catches edits made after that snapshot was read.
func TestAQuietRefreshQueuesAnEventThatArrivesDuringAnAction(t *testing.T) {
	opener := &liveOpener{snapshot: repository.Snapshot{Root: "/work/repo", Branch: "main"}}
	session := &countingLiveSession{liveSession: liveSession{opener: opener}}
	s, _, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	a := readyTab(s, "/work/repo", session).app
	started := make(chan struct{})
	release := make(chan struct{})
	a.beginRepositoryAction("", func(context.Context, repository.Session) error {
		close(started)
		<-release
		return nil
	}, nil)
	<-started
	opener.set(repository.Snapshot{
		Root: "/work/repo", Branch: "main", Files: []repository.File{changedFile("external.txt")},
	})
	a.requestQuietRefresh()
	if !a.refreshPending {
		t.Fatal("an external change during an action was discarded")
	}
	close(release)
	for a.actionBusy || a.refreshBusy || a.refreshPending {
		updated.waitForUpdate(t)
	}
	if got := atomic.LoadInt32(&session.hits); got != 2 {
		t.Fatalf("the action and its follow-up caused %d reads, want 2", got)
	}
	if len(a.snapshot.Files) != 1 || a.snapshot.Files[0].Path != "external.txt" {
		t.Fatalf("the follow-up missed the external file: %+v", a.snapshot.Files)
	}
}

// Closing a repository does not leave its unfinished read holding the busy
// flag. Its completion also cannot clear the new repository's read when the
// two workers finish out of order.
func TestAQuietRefreshFromAClosedRepositoryDoesNotBlockItsReplacement(t *testing.T) {
	s, _, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	oldSession := &blockingSnapshotSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Root: "/work/old", Branch: "main"}},
		started:     make(chan struct{}), release: make(chan struct{}),
	}
	newSession := &blockingSnapshotSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{
			Root: "/work/new", Branch: "main", Files: []repository.File{changedFile("new.txt")},
		}},
		started: make(chan struct{}), release: make(chan struct{}),
	}
	unblock := func(release chan struct{}) {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	t.Cleanup(func() {
		unblock(oldSession.release)
		unblock(newSession.release)
	})

	a := readyTab(s, "/work/old", oldSession).app
	a.requestQuietRefresh()
	<-oldSession.started
	a.shutdown()
	a.installOpenedRepository(newSession, repository.Snapshot{Root: "/work/new", Branch: "main"})
	a.requestQuietRefresh()
	select {
	case <-newSession.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the old repository's unfinished read blocked the new repository")
	}

	unblock(oldSession.release)
	updated.waitForUpdate(t)
	if !a.refreshBusy {
		t.Fatal("the old read's completion cleared the new read's busy flag")
	}
	unblock(newSession.release)
	updated.waitForUpdate(t)
	if a.refreshBusy || a.refreshPending || a.snapshot.Root != "/work/new" ||
		len(a.snapshot.Files) != 1 || a.snapshot.Files[0].Path != "new.txt" {
		t.Fatalf("the replacement repository did not settle correctly: busy=%v pending=%v snapshot=%+v",
			a.refreshBusy, a.refreshPending, a.snapshot)
	}
}

// A panic belongs to the read that raised it, too. An old repository's fault
// cannot clear its replacement's in-flight refresh or show a stale error.
func TestAClosedRepositorysPanickingRefreshDoesNotDisturbItsReplacement(t *testing.T) {
	s, _, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	oldSession := &blockingPanickingSnapshotSession{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	newSession := &blockingSnapshotSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{
			Root: "/work/new", Branch: "main", Files: []repository.File{changedFile("new.txt")},
		}},
		started: make(chan struct{}), release: make(chan struct{}),
	}
	unblock := func(release chan struct{}) {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	t.Cleanup(func() {
		unblock(oldSession.release)
		unblock(newSession.release)
	})

	a := readyTab(s, "/work/old", oldSession).app
	a.requestQuietRefresh()
	<-oldSession.started
	a.shutdown()
	a.installOpenedRepository(newSession, repository.Snapshot{Root: "/work/new", Branch: "main"})
	a.requestQuietRefresh()
	select {
	case <-newSession.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the old repository's unfinished read blocked the new repository")
	}

	unblock(oldSession.release)
	updated.waitForUpdate(t)
	if !a.refreshBusy {
		t.Fatal("the old read's panic cleared the new read's busy flag")
	}
	if len(a.pendingToasts) != 0 {
		t.Fatalf("the old read posted an error over its replacement: %+v", a.pendingToasts)
	}

	unblock(newSession.release)
	updated.waitForUpdate(t)
	if a.refreshBusy || a.refreshPending || a.snapshot.Root != "/work/new" ||
		len(a.snapshot.Files) != 1 || a.snapshot.Files[0].Path != "new.txt" {
		t.Fatalf("the replacement repository did not settle correctly: busy=%v pending=%v snapshot=%+v",
			a.refreshBusy, a.refreshPending, a.snapshot)
	}
}

// A fault in a quiet refresh is reported without wedging the timer: the next
// look starts as usual.
func TestAQuietRefreshThatPanicsTriesAgain(t *testing.T) {
	s, _, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	session := &panickingSession{fakeSession: fakeSession{
		snapshot: repository.Snapshot{Root: "/work/repo", Branch: "main"},
	}}
	a := readyTab(s, "/work/repo", session).app

	a.requestQuietRefresh()
	if !a.refreshBusy {
		t.Fatal("the quiet refresh did not start")
	}
	drainRefresh(t, updated, 1)
	if a.refreshBusy {
		t.Fatal("a failed read left the refresh busy, so the timer would stop")
	}

	a.requestQuietRefresh()
	if !a.refreshBusy {
		t.Fatal("the next look did not start after the fault")
	}
	drainRefresh(t, updated, 1)
	if a.refreshBusy {
		t.Fatal("the second read did not finish")
	}
}

// A read that found nothing new leaves the snapshot alone, so the frame and
// the panes are not rebuilt for nothing.
func TestAQuietRefreshLeavesAnUnchangedRepositoryAlone(t *testing.T) {
	opener := &liveOpener{snapshot: repository.Snapshot{
		Root:   "/work/repo",
		Branch: "main",
		Files:  []repository.File{changedFile("a.txt")},
	}}
	s, tt, updated := newLiveShell(t, opener)
	s.activeApp().workspaceTab = sectionChanges
	openShellRepo(t, s, tt, updated, "/work/repo")
	a := s.activeApp()
	tt.Frame()

	before := a.snapshotSeq
	a.requestQuietRefresh()
	drainRefresh(t, updated, 1)
	if a.snapshotSeq != before {
		t.Fatal("an unchanged repository rebuilt the snapshot")
	}
}

// Reopening the window reads every page, so what changed in the tray is there
// when it comes back.
func TestShowingTheWindowRefreshesEveryPage(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	var first, second int32
	readyCountingTab(s, "/work/first", &first)
	readyCountingTab(s, "/work/second", &second)
	tt.Frame()

	s.handleWindowHidden()
	s.handleWindowShown()
	drainRefresh(t, updated, 2)

	if got := atomic.LoadInt32(&first); got != 1 {
		t.Errorf("the first page was read %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&second); got != 1 {
		t.Errorf("the second page was read %d times, want 1", got)
	}
}

// The timer reads the page on screen when the window is visible and focused,
// and stands aside while it is hidden or in the background.
func TestTheAutoRefreshTimerFollowsWindowFocus(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	var hits int32
	tab := readyCountingTab(s, "/work/repo", &hits)
	s.activate(s.indexOf(tab))
	drainRefresh(t, updated, 1) // the page coming in reads once
	tt.Frame()

	// Visible and focused: the timer reads.
	atomic.StoreInt32(&hits, 0)
	s.autoRefreshTick()
	drainRefresh(t, updated, 1)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("a visible, focused window was read %d times, want 1", got)
	}

	// In the background the timer stands aside, and taking focus back reads.
	s.handleWindowBlurred()
	s.autoRefreshTick()
	if tab.app.refreshBusy {
		t.Fatal("a background window started a read from the timer")
	}
	s.handleWindowFocused()
	drainRefresh(t, updated, 1)
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("taking focus read the page %d times, want 2", got)
	}

	// In the tray nothing is read until the window is shown again.
	s.handleWindowHidden()
	s.autoRefreshTick()
	if tab.app.refreshBusy {
		t.Fatal("a hidden window started a read from the timer")
	}
	s.handleWindowShown()
	drainRefresh(t, updated, 1)
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Fatalf("showing the window read the page %d times, want 3", got)
	}
}

// Bringing a page to the front reads it again, because its repository may
// have moved while another page was on screen.
func TestSwitchingPagesRefreshesThePageComingIn(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/picker"})
	var first, second int32
	firstTab := readyCountingTab(s, "/work/first", &first)
	readyCountingTab(s, "/work/second", &second)
	s.activate(s.indexOf(firstTab))
	drainRefresh(t, updated, 1)
	atomic.StoreInt32(&first, 0)
	atomic.StoreInt32(&second, 0)
	tt.Frame()

	tt.Frame()
	s.activate(2)
	drainRefresh(t, updated, 1)

	if got := atomic.LoadInt32(&second); got != 1 {
		t.Errorf("the page coming in was read %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&first); got != 0 {
		t.Errorf("the page leaving was read %d times, want 0", got)
	}
}
