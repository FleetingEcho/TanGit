package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui"
	"lazygitapp/internal/repository"
)

// countingLiveOpener is a live repository whose Snapshot reads are counted,
// so a test can tell a refresh that actually re-read the repository apart
// from one that was suppressed.
type countingLiveOpener struct {
	liveOpener
	hits int32
}

type trackedLiveSession struct {
	liveSession
	reads *int32
}

func (s trackedLiveSession) Snapshot(ctx context.Context) (repository.Snapshot, error) {
	atomic.AddInt32(s.reads, 1)
	return s.liveSession.Snapshot(ctx)
}

func (o *countingLiveOpener) Open(ctx context.Context, path string) (repository.Session, error) {
	s, err := o.liveOpener.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	return trackedLiveSession{s.(liveSession), &o.hits}, nil
}

func (o *countingLiveOpener) readCount() int {
	return int(atomic.LoadInt32(&o.hits))
}

func (o *countingLiveOpener) resetReads() {
	atomic.StoreInt32(&o.hits, 0)
}

func testCommits(subjects ...string) []repository.Commit {
	commits := make([]repository.Commit, 0, len(subjects))
	for i, subject := range subjects {
		hash := fmt.Sprintf("%040x", len(subjects)-i)
		parent := ""
		if i+1 < len(subjects) {
			parent = fmt.Sprintf("%040x", len(subjects)-i-1)
		}
		var parents []string
		if parent != "" {
			parents = []string{parent}
		}
		commits = append(commits, repository.Commit{
			Hash:       hash,
			ShortHash:  hash[:7],
			Subject:    subject,
			AuthorName: "Ada", AuthorEmail: "ada@example.com",
			Parents: parents,
		})
	}
	return commits
}

// TestCommitDetailsLayoutDefaultsToLeftRight pins the out-of-the-box layout:
// files on the left, diff on the right, and the divider sizes the design chose.
func TestCommitDetailsLayoutDefaultsToLeftRight(t *testing.T) {
	a := newApp(fakeOpener{})
	if a.commitDetailsLayout != commitDetailsLeftRight {
		t.Fatalf("default layout = %d, want left/right", a.commitDetailsLayout)
	}
	if got := defaultSettings().CommitDetailsLayout; got != commitDetailsLeftRight {
		t.Fatalf("default settings layout = %d, want %d", got, commitDetailsLeftRight)
	}
	if a.commitFilesWidth != 300 || a.commitFilesHeight != 150 {
		t.Fatalf("default divider sizes = %vx%v, want 300x150", a.commitFilesWidth, a.commitFilesHeight)
	}
	if got := normalizeSettings(appSettings{CommitDetailsLayout: 9}).CommitDetailsLayout; got != commitDetailsLeftRight {
		t.Fatalf("an unknown stored layout normalized to %d, want %d", got, commitDetailsLeftRight)
	}
	if got := normalizeSettings(appSettings{CommitDetailsLayout: commitDetailsTopBottom}).CommitDetailsLayout; got != commitDetailsTopBottom {
		t.Fatalf("a stored top/bottom layout normalized to %d, want %d", got, commitDetailsTopBottom)
	}
}

// gripRect finds the divider grip of the commit details split: the horizontal
// one in the top/bottom layout, the right-hand vertical one in left/right.
func gripRect(t *testing.T, tester *ui.Tester, horizontal bool) ui.Rect {
	t.Helper()
	var grips []ui.Rect
	for _, info := range tester.AccessAll() {
		if info.Name != "Drag to resize" {
			continue
		}
		if wide := info.Bounds.W > info.Bounds.H; wide == horizontal {
			grips = append(grips, info.Bounds)
		}
	}
	switch len(grips) {
	case 0:
		t.Fatalf("no %s divider grip found", map[bool]string{true: "horizontal", false: "vertical"}[horizontal])
	case 1:
		return grips[0]
	default:
		inner := grips[0]
		for _, grip := range grips[1:] {
			if grip.X > inner.X {
				inner = grip
			}
		}
		return inner
	}
	return ui.Rect{}
}

// TestCommitDetailsDividersKeepIndependentSizes drags both dividers, switches
// the layout, and checks each orientation keeps its own size across switches.
func TestCommitDetailsDividersKeepIndependentSizes(t *testing.T) {
	h, _ := historyHarness(t)
	h.app.pathUserData = t.TempDir()
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })

	h.app.commitFilesWidth = 240
	h.app.commitFilesHeight = 140
	h.tester.Frame()

	grip := gripRect(t, h.tester, false)
	dragTo(t, h.tester, grip.X+grip.W/2, grip.Y+grip.H/2, 60, 0)
	draggedWidth := h.app.commitFilesWidth
	if draggedWidth <= 240 {
		t.Fatalf("dragging the left/right divider left the files pane at %v, want it wider than 240", draggedWidth)
	}
	if h.app.commitFilesHeight != 140 {
		t.Fatalf("dragging the left/right divider changed the top/bottom size to %v", h.app.commitFilesHeight)
	}

	// The layout switch keeps both divider sizes and persists the choice.
	if err := h.tester.Click("Switch layout (Top/Bottom)"); err != nil {
		t.Fatalf("switch to top/bottom: %v", err)
	}
	h.tester.Frame()
	if h.app.commitDetailsLayout != commitDetailsTopBottom {
		t.Fatal("the top/bottom choice did not stick")
	}
	if h.app.commitFilesWidth != draggedWidth {
		t.Fatalf("switching layouts changed the left/right size to %v, want %v", h.app.commitFilesWidth, draggedWidth)
	}
	if h.app.commitFilesHeight != 140 {
		t.Fatalf("switching layouts changed the top/bottom size to %v, want 140", h.app.commitFilesHeight)
	}
	if got := loadAppSettings(h.app.pathUserData).CommitDetailsLayout; got != commitDetailsTopBottom {
		t.Fatalf("the saved layout is %d, want top/bottom (%d)", got, commitDetailsTopBottom)
	}

	grip = gripRect(t, h.tester, true)
	dragTo(t, h.tester, grip.X+grip.W/2, grip.Y+grip.H/2, 0, 50)
	draggedHeight := h.app.commitFilesHeight
	if draggedHeight <= 140 {
		t.Fatalf("dragging the top/bottom divider left the files pane at %v, want it taller than 140", draggedHeight)
	}
	if h.app.commitFilesWidth != draggedWidth {
		t.Fatalf("dragging the top/bottom divider changed the left/right size to %v, want %v", h.app.commitFilesWidth, draggedWidth)
	}

	// And back: both sizes survive the round trip.
	if err := h.tester.Click("Switch layout (Left/Right)"); err != nil {
		t.Fatalf("switch to left/right: %v", err)
	}
	h.tester.Frame()
	if h.app.commitDetailsLayout != commitDetailsLeftRight {
		t.Fatal("the left/right choice did not stick")
	}
	if h.app.commitFilesWidth != draggedWidth || h.app.commitFilesHeight != draggedHeight {
		t.Fatalf("round trip kept width %v height %v, want %v/%v",
			h.app.commitFilesWidth, h.app.commitFilesHeight, draggedWidth, draggedHeight)
	}
	if got := loadAppSettings(h.app.pathUserData).CommitDetailsLayout; got != commitDetailsLeftRight {
		t.Fatalf("the saved layout is %d, want left/right (%d)", got, commitDetailsLeftRight)
	}
}

// TestCommitRefreshButtonReloadsCommits clicks the pane's own refresh button in
// both commit lists and requires a re-read, and that the button is disabled
// while another repository action is running.
func TestCommitRefreshButtonReloadsCommits(t *testing.T) {
	for _, tab := range []struct {
		name    string
		section int
	}{
		{"history", sectionHistory},
		{"graph", sectionGraph},
		{"branches", sectionBranches},
	} {
		t.Run(tab.name, func(t *testing.T) {
			opener := &countingLiveOpener{liveOpener: liveOpener{snapshot: repository.Snapshot{
				Root:     "/work/repo",
				Branch:   "main",
				Branches: []repository.Branch{{Name: "main", Head: true, Subject: "second"}},
				Commits:  testCommits("second", "first"),
			}}}
			s, tt, updated := newLiveShell(t, opener)
			s.activeApp().workspaceTab = tab.section
			openShellRepo(t, s, tt, updated, "/work/repo")
			a := s.activeApp()
			opener.resetReads()

			// The button is disabled while another action has the repository.
			a.actionBusy = true
			_ = tt.Click("Refresh commits") // a disabled button does not run
			tt.Frame()
			if got := opener.readCount(); got != 0 {
				t.Fatalf("the disabled refresh read the repository %d time(s)", got)
			}
			a.actionBusy = false

			// An external commit: only a re-read can see it.
			external := repository.Snapshot{
				Root:     "/work/repo",
				Branch:   "main",
				Branches: []repository.Branch{{Name: "main", Head: true, Subject: "made elsewhere"}},
				Commits:  testCommits("made elsewhere", "second", "first"),
			}
			opener.set(external)
			if err := tt.Click("Refresh commits"); err != nil {
				t.Fatalf("click Refresh commits: %v", err)
			}
			for range 20 {
				if !a.actionBusy {
					break
				}
				updated.waitForUpdate(t)
			}
			if a.actionBusy {
				t.Fatal("the refresh never finished")
			}
			if got := opener.readCount(); got == 0 {
				t.Fatal("Refresh commits did not re-read the repository")
			}
			found := false
			for _, commit := range a.snapshot.Commits {
				if commit.Subject == "made elsewhere" {
					found = true
				}
			}
			if !found {
				t.Fatalf("the refresh did not pick up the external commit: %+v", a.snapshot.Commits)
			}
		})
	}
}

// maximizedFixture opens a repository whose diff is showing, expanded over
// the whole window, with a recording session behind it.
func maximizedFixture(t *testing.T) (*harness, *callRecorder) {
	t.Helper()
	calls := &callRecorder{}
	snapshot := repository.Snapshot{
		Root:     "/work/project",
		Branch:   "main",
		HeadHash: "c2c2c2c2",
		Files:    []repository.File{{Path: "one.txt", Status: "M ", Tracked: true, Staged: true}},
		Branches: []repository.Branch{{
			Name: "main", DisplayName: "main (local)", Head: true, Subject: "Native history",
			UpstreamRemote: "origin", UpstreamBranch: "main", Ahead: "?", Behind: "?",
		}},
		Commits: []repository.Commit{
			{Hash: "c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2c2", ShortHash: "c2c2c2c", Subject: "Second commit",
				AuthorName: "Ada", AuthorEmail: "ada@example.com",
				Parents: []string{"c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1"}},
			{Hash: "c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1", ShortHash: "c1c1c1c", Subject: "Initial commit",
				AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		},
		Remotes: []repository.Remote{{
			Name:     "origin",
			URLs:     []string{"git@example.com:team/project.git"},
			Branches: []repository.RemoteBranch{{Name: "main", RemoteName: "origin"}},
		}},
	}
	a := newApp(recordingOpener{snapshot: snapshot, calls: calls})
	a.workspaceTab = sectionHistory
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	a.pathUserData = t.TempDir()
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the commit diff", func() bool { return h.app.commitDiffState != nil && !h.app.commitDiffLoading })
	if err := h.tester.Click("Expand diff"); err != nil {
		t.Fatalf("expand the diff: %v", err)
	}
	h.tester.Frame()
	if !h.app.diffFullscreen {
		t.Fatal("the diff did not open over the whole window")
	}
	return h, calls
}

// recordingGitSession records the repository actions the header performs, so a
// test can prove the buttons still work over the whole-window diff.
type recordingGitSession struct {
	fakeSession
}

func (s recordingGitSession) Fetch(context.Context) error {
	s.fakeSession.calls.record("fetch")
	return nil
}

func (s recordingGitSession) Pull(context.Context) error {
	s.fakeSession.calls.record("pull")
	return nil
}

func (s recordingGitSession) Push(context.Context) error {
	s.fakeSession.calls.record("push")
	return nil
}

func (s recordingGitSession) Snapshot(ctx context.Context) (repository.Snapshot, error) {
	s.fakeSession.calls.record("snapshot")
	return s.fakeSession.Snapshot(ctx)
}

type recordingOpener struct {
	snapshot repository.Snapshot
	calls    *callRecorder
}

func (o recordingOpener) Init(context.Context, string) error { return nil }

func (o recordingOpener) Open(context.Context, string) (repository.Session, error) {
	return recordingGitSession{fakeSession{snapshot: o.snapshot, calls: o.calls}}, nil
}

func countCalls(calls *callRecorder, name string) int {
	count := 0
	for _, call := range calls.all() {
		if call == name {
			count++
		}
	}
	return count
}

// dismissToasts closes the notification cards, which stack over the toolbar's
// top-right corner and would swallow the next click in a handler chain.
func dismissToasts(t *testing.T, h *harness) {
	t.Helper()
	for range 8 {
		if _, ok := h.tester.Find("Dismiss notification"); !ok {
			return
		}
		if err := h.tester.Click("Dismiss notification"); err != nil {
			return
		}
		h.tester.Frame()
	}
}

// TestRepositoryActionsStayAvailableWhileDiffMaximized drives the toolbar the
// whole-window diff shares with the window: Fetch runs immediately, Pull and
// Push ask first, and Refresh re-reads the repository.
func TestRepositoryActionsStayAvailableWhileDiffMaximized(t *testing.T) {
	h, calls := maximizedFixture(t)
	for _, label := range []string{"Fetch", "Pull", "Push", "Refresh", "Settings"} {
		if _, found := h.tester.Find(label); !found {
			t.Fatalf("the whole-window diff has no %q control: %q", label, h.texts())
		}
	}

	snapshots := countCalls(calls, "snapshot")
	if err := h.tester.Click("Refresh"); err != nil {
		t.Fatalf("click Refresh: %v", err)
	}
	h.waitFor(t, "the refresh to re-read the repository", func() bool { return countCalls(calls, "snapshot") > snapshots })
	h.waitFor(t, "the refresh to finish", func() bool { return !h.app.actionBusy })

	// The frame must repaint with the action over before the buttons work again.
	h.tester.Frame()
	if err := h.tester.Click("Fetch"); err != nil {
		t.Fatalf("click Fetch: %v", err)
	}
	h.waitFor(t, "the fetch to run", func() bool { return countCalls(calls, "fetch") > 0 })
	h.waitFor(t, "the fetch to settle", func() bool { return !h.app.actionBusy })
	h.tester.Frame()
	dismissToasts(t, h)
	if err := h.tester.Click("Pull"); err != nil {
		t.Fatalf("click Pull: %v", err)
	}
	h.waitFor(t, "the pull confirmation", func() bool { return h.app.confirm != nil })
	if err := h.tester.Click("Continue"); err != nil {
		t.Fatalf("confirm the pull: %v", err)
	}
	h.waitFor(t, "the pull to run", func() bool { return countCalls(calls, "pull") > 0 })
	h.waitFor(t, "the pull to settle", func() bool { return !h.app.actionBusy })
	h.tester.Frame()
	dismissToasts(t, h)
	if err := h.tester.Click("Push"); err != nil {
		t.Fatalf("click Push: %v", err)
	}
	h.waitFor(t, "the push confirmation", func() bool { return h.app.confirm != nil })
	if err := h.tester.Click("Continue"); err != nil {
		t.Fatalf("confirm the push: %v", err)
	}
	h.waitFor(t, "the push to run", func() bool { return countCalls(calls, "push") > 0 })
	h.waitFor(t, "the push to settle", func() bool { return !h.app.actionBusy })
}

// waitForUpdates runs pending UI updates until cond holds or the test fails.
func waitForUpdates(t *testing.T, updated *updates, what string, cond func() bool) {
	t.Helper()
	for range 20 {
		if cond() {
			return
		}
		updated.waitForUpdate(t)
	}
	if !cond() {
		t.Fatalf("timed out waiting for %s", what)
	}
}

// TestExternalCommitAppearsWhileDiffMaximized proves the quiet refresh still
// reaches a repository read while the diff covers the window, and that an open
// dialog or an in-progress edit still holds it back.
func TestExternalCommitAppearsWhileDiffMaximized(t *testing.T) {
	opener := &liveOpener{snapshot: repository.Snapshot{
		Root:    "/work/repo",
		Branch:  "main",
		Files:   []repository.File{changedFile("a.txt")},
		Commits: testCommits("first"),
	}}
	s, tt, updated := newLiveShell(t, opener)
	s.activeApp().workspaceTab = sectionChanges
	openShellRepo(t, s, tt, updated, "/work/repo")
	a := s.activeApp()
	for range 20 {
		if a.diffState != nil {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	if a.diffState == nil {
		t.Fatal("the diff never loaded")
	}
	if err := tt.Click("Expand diff"); err != nil {
		t.Fatalf("expand the diff: %v", err)
	}
	tt.Frame()
	if !a.diffFullscreen {
		t.Fatal("the diff did not open over the whole window")
	}

	// Another app commits while the whole-window diff is up.
	opener.set(repository.Snapshot{
		Root:    "/work/repo",
		Branch:  "main",
		Files:   []repository.File{changedFile("a.txt")},
		Commits: testCommits("made elsewhere", "first"),
	})
	a.requestQuietRefresh()
	waitForUpdates(t, updated, "the external commit to appear", func() bool {
		return len(a.snapshot.Commits) == 2 && a.snapshot.Commits[0].Subject == "made elsewhere"
	})
	tt.Frame()
	if !a.diffFullscreen {
		t.Fatal("the whole-window diff closed on its own")
	}

	// A dialog holds the refresh back even over the whole-window diff.
	a.settingsOpen = true
	opener.set(repository.Snapshot{
		Root:    "/work/repo",
		Branch:  "main",
		Files:   []repository.File{changedFile("a.txt")},
		Commits: testCommits("dialog held this back", "made elsewhere", "first"),
	})
	a.requestQuietRefresh()
	tt.Frame()
	tt.Frame()
	if len(a.snapshot.Commits) != 2 {
		t.Fatalf("a dialog did not hold the refresh back: %+v", a.snapshot.Commits)
	}
	a.settingsOpen = false
	a.requestQuietRefresh()
	waitForUpdates(t, updated, "the refresh after the dialog closed", func() bool {
		return len(a.snapshot.Commits) == 3
	})
	if len(a.snapshot.Commits) != 3 {
		t.Fatalf("closing the dialog did not let the refresh through: %+v", a.snapshot.Commits)
	}

	// An edit in progress holds it back too.
	a.fileEditing = true
	opener.set(repository.Snapshot{
		Root:    "/work/repo",
		Branch:  "main",
		Files:   []repository.File{changedFile("a.txt")},
		Commits: testCommits("edit held this back", "dialog held this back", "made elsewhere", "first"),
	})
	a.requestQuietRefresh()
	tt.Frame()
	tt.Frame()
	if len(a.snapshot.Commits) != 3 {
		t.Fatalf("an edit in progress did not hold the refresh back: %+v", a.snapshot.Commits)
	}
	a.fileEditing = false
}

// TestCommitDetailsLayoutPersistsAcrossRestart covers the saved preference
// round-trip through the settings file, not just the in-memory fields.
func TestCommitDetailsLayoutPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	settings := defaultSettings()
	settings.CommitDetailsLayout = commitDetailsTopBottom
	if err := saveAppSettings(dir, settings); err != nil {
		t.Fatal(err)
	}
	loaded := loadAppSettings(dir)
	if loaded.CommitDetailsLayout != commitDetailsTopBottom {
		t.Fatalf("the saved layout came back as %d, want %d", loaded.CommitDetailsLayout, commitDetailsTopBottom)
	}
	data, err := os.ReadFile(settingsStorePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"commitDetailsLayout": 1`) {
		t.Fatalf("the settings file does not record the layout: %s", data)
	}
}
