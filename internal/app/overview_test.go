package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type overviewSearchOpener struct{ pathOpener }

func (overviewSearchOpener) Open(context.Context, string) (repository.Session, error) {
	return overviewSearchSession{}, nil
}

type overviewSearchSession struct{ overviewPagingSession }

func (overviewSearchSession) CommitHistoryOrder(_ context.Context, _ string, from, to time.Time, search repository.CommitHistorySearch) ([]string, error) {
	if from.IsZero() && to.IsZero() && len(search.Terms) == 1 && search.Terms[0] == "needle" {
		return []string{"0000037"}, nil
	}
	return nil, nil
}

func TestOverviewSearchUsesAllRepositoriesAndRestoresPeriod(t *testing.T) {
	a := newApp(overviewSearchOpener{})
	updated := newUpdates()
	a.update = updated.update
	o := newOverviewState()
	a.overview = o
	t.Cleanup(a.stopOverview)
	o.config = overviewConfig{Overviews: []overviewDef{{ID: "overview", Days: 7, Repos: []overviewRepo{
		{Path: "/work/alpha"}, {Path: "/work/beta"},
	}}}}
	o.filter = "/work/alpha"
	o.syncRange()
	a.reloadOverview()
	updated.waitForPending(t, 2)
	tt := ui.NewTester(a.overviewView, 1100, 650)
	tt.Frame()
	if err := tt.Click("Search commits"); err != nil {
		t.Fatal(err)
	}
	tt.Type("needle")
	if _, ok := tt.Find("All time"); !ok {
		t.Fatal("search did not temporarily show All time")
	}
	updated.waitForUpdate(t) // The debounce timer starts the two repository reads.
	updated.waitForPending(t, 2)
	tt.Frame()
	rows := o.visibleCommits()
	if len(rows) != 2 || rows[0].Repo == rows[1].Repo || o.activeRepoFilter() != "" ||
		!o.readPeriod.From.IsZero() || !o.readPeriod.To.IsZero() {
		t.Fatalf("search did not read all-time matches from both repositories: %+v", rows)
	}
	if err := tt.Click("Clear search"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("1 week"); !ok || o.activeRepoFilter() != "/work/alpha" ||
		o.readPeriod.From.IsZero() || o.readPeriod.To.IsZero() {
		t.Fatal("clearing search did not restore the previous range and sidebar filter")
	}
}

func TestOverviewCustomPeriodCanBeAppliedAndReopened(t *testing.T) {
	a := newApp(nil)
	a.pathUserData = t.TempDir()
	o := newOverviewState()
	a.overview = o
	o.config = overviewConfig{Active: "overview", Overviews: []overviewDef{{ID: "overview"}}}
	if err := saveOverviewConfig(a.dataDir(), o.config); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.overviewRangeControl(c, o)
		a.overviewPeriodModal(c, o)
	}, 800, 500)
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := tt.Find("3 days"); !ok {
		t.Fatal("default period should be three days")
	}
	click("Time range")
	click("Custom period")
	if o.periodDraft == nil {
		t.Fatal("custom period did not open")
	}
	from := time.Date(2026, 10, 5, 9, 15, 0, 0, time.Local)
	to := from.Add(50 * time.Minute)
	o.periodDraft.From, o.periodDraft.To = to, from
	tt.Frame()
	if _, ok := tt.Find("From must be on or before To."); !ok {
		t.Fatal("reversed period did not show validation")
	}
	o.periodDraft.From, o.periodDraft.To = from, to
	tt.Frame()
	click("Apply")
	if got := o.overviewReadPeriod(time.Now()); !got.From.Equal(from) || !got.To.Equal(to.Add(59*time.Second)) {
		t.Fatalf("applied search period = %+v", got)
	}
	saved := loadOverviewConfig(a.dataDir())
	current := saved.current()
	if current == nil {
		t.Fatal("applied custom period did not retain the active Overview")
	}
	if period := current.Period; period == nil || !period.From.Equal(from) || !period.To.Equal(to) {
		t.Fatalf("saved period = %+v", period)
	}
	click(from.Format(overviewPeriodLayout) + " – " + to.Format(overviewPeriodLayout))
	if o.periodDraft == nil || !o.periodDraft.From.Equal(from) || !o.periodDraft.To.Equal(to) {
		t.Fatal("clicking the displayed range did not reopen the saved period")
	}
	click("Cancel")
	click("Time range")
	click("1 week")
	if o.period != nil || overviewRanges[o.rangeIdx] != 7 {
		t.Fatal("choosing a preset did not clear the custom period")
	}
}

func TestOverviewSidebarRemovesOnlyAfterConfirmation(t *testing.T) {
	a := newApp(nil)
	a.pathUserData = t.TempDir()
	alpha, beta := t.TempDir(), t.TempDir()
	file := filepath.Join(alpha, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	o := newOverviewState()
	a.overview = o
	o.config = overviewConfig{Overviews: []overviewDef{{
		ID: "overview", Repos: []overviewRepo{{Path: alpha, Name: "Alpha"}, {Path: beta, Name: "Beta"}},
	}}}
	if err := saveOverviewConfig(a.dataDir(), o.config); err != nil {
		t.Fatal(err)
	}
	o.filter = alpha
	o.repos[alpha], o.repos[beta] = &overviewRepoState{count: 1}, &overviewRepoState{count: 1}
	o.commits = []overviewCommit{{Repo: alpha}, {Repo: beta}}
	o.detail = &overviewCommitDetail{row: o.commits[0]}
	tt := ui.NewTester(func(c *ui.Context) {
		a.overviewSidebar(c, o)
		a.confirmModal(c)
	}, 800, 500)
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	openRepoMenu := func(label string) {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no repository row shows %q", label)
		}
		tt.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
		tt.Frame()
	}
	openRepoMenu("Alpha")
	click("Remove from Overview")
	if _, ok := tt.Find("Remove Alpha from this Overview?"); !ok {
		t.Fatal("removal confirmation is missing")
	}
	click("Cancel")
	if len(o.followed()) != 2 || o.filter != alpha {
		t.Fatal("cancelling removal changed the Overview")
	}
	openRepoMenu("Alpha")
	click("Remove from Overview")
	click("Remove")
	saved := loadOverviewConfig(a.dataDir())
	if repos := saved.current().Repos; len(repos) != 1 || repos[0].Path != beta {
		t.Fatalf("saved repositories after confirmation: %+v", repos)
	}
	if o.filter != "" || o.detail != nil || len(o.commits) != 1 || o.commits[0].Repo != beta {
		t.Fatal("removal left stale timeline state")
	}
	if contents, err := os.ReadFile(file); err != nil || string(contents) != "keep" {
		t.Fatalf("removal affected the local files: %q, %v", contents, err)
	}
}

func TestOverviewPickerDeleteRequiresConfirmation(t *testing.T) {
	a := newApp(fakeOpener{})
	a.pathUserData = t.TempDir()
	config := overviewConfig{Overviews: []overviewDef{
		{ID: "one", Name: "First overview"},
		{ID: "two", Name: "Second overview"},
	}}
	if err := saveOverviewConfig(a.dataDir(), config); err != nil {
		t.Fatal(err)
	}
	a.openOverviewPicker()
	tt := ui.NewTester(func(c *ui.Context) {
		a.overviewPickerModal(c)
		a.confirmModal(c)
	}, 800, 500)
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("Delete First overview")
	if _, ok := tt.Find("Delete Overview"); !ok || !a.overviewPicker.open {
		t.Fatal("delete did not open confirmation over the picker")
	}
	click("Cancel")
	saved := loadOverviewConfig(a.dataDir())
	if !a.overviewPicker.open || saved.byID("one") == nil {
		t.Fatal("cancelling delete removed the saved overview")
	}
	click("Delete First overview")
	click("Delete")
	saved = loadOverviewConfig(a.dataDir())
	if !a.overviewPicker.open || saved.byID("one") != nil || saved.byID("two") == nil {
		t.Fatal("confirmation did not remove only the chosen overview")
	}
}

func TestOverviewRenameAndOpenCommitHistory(t *testing.T) {
	const path = "/work/project"
	commit := repository.Commit{Hash: "1234567890123456", ShortHash: "1234567", Subject: "Overview commit", UnixTimestamp: time.Now().Unix()}
	s, tt, updated := newShellTester(t, repository.Snapshot{Commits: []repository.Commit{
		{Hash: "other", Subject: "Other commit"},
	}})
	openShellRepo(t, s, tt, updated, path)
	existing := s.activeTab()
	page := newSettledShellTab(s, "")
	s.tabs = append(s.tabs, page)
	a := page.app
	o := newOverviewState()
	a.overview = o
	a.opener = overviewPagingOpener{}
	o.ctx, o.cancel = context.WithCancel(context.Background())
	t.Cleanup(a.stopOverview)
	o.config = overviewConfig{Overviews: []overviewDef{{ID: "overview", Name: "My Overview", Repos: []overviewRepo{{Path: path}}}}}
	if err := saveOverviewConfig(a.dataDir(), o.config); err != nil {
		t.Fatal(err)
	}
	o.repos[path] = &overviewRepoState{branch: "main", count: 1}
	o.commits = []overviewCommit{{Repo: path, Commit: commit}}
	s.activateTab(page)
	tt.Frame()

	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("Choose repositories")
	if !o.setup.open || o.setup.isNew || o.selectedCount() != 1 {
		t.Fatal("sidebar add button did not open the current Overview's repository selection")
	}
	click("Close")
	if o.setup.open {
		t.Fatal("close button did not dismiss the edit dialog")
	}
	repo, ok := tt.Find("main")
	if !ok {
		t.Fatal("repository row is missing")
	}
	tt.RightClickAt(repo.X+repo.W/2, repo.Y+repo.H/2)
	tt.Frame()
	click("Rename")
	tt.Key(ui.Ctrl, ui.KeyA)
	tt.Type("Backend")
	click("Save")
	saved := loadOverviewConfig(a.dataDir())
	if got := saved.current().Repos[0].displayName(); got != "Backend" {
		t.Fatalf("saved repository name = %q", got)
	}
	label := "Backend " + commit.ShortHash + " " + commit.Subject
	click(label)
	if s.activeTab() != page {
		t.Fatal("opening the disclosure switched tabs")
	}
	if _, ok := tt.Find("Open commit details"); !ok {
		t.Fatal("expanded preview has no Open commit details button")
	}
	updated.waitForUpdate(t)
	tt.Frame()
	if _, ok := tt.Find("The full commit description."); !ok {
		t.Fatal("expanded preview did not load the description")
	}
	firstFile, ok := tt.Find("changes.go")
	if !ok {
		t.Fatal("expanded preview did not load the changed files")
	}
	details, ok := tt.Find("Open commit details")
	if !ok || details.X < firstFile.X+200 || details.Y <= firstFile.Y {
		t.Fatalf("Open commit details is not at the lower right: button=%+v file=%+v", details, firstFile)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "overview-commit-preview", tt)
	}
	tt.Scroll(firstFile.X+10, firstFile.Y+5, 0, 1200)
	lastFile, ok := tt.Find("file-09.go")
	if !ok || lastFile.Y <= firstFile.Y || lastFile.Y >= details.Y {
		t.Fatalf("last file is not visible in the scrolled list: %+v", lastFile)
	}
	if after, _ := tt.Find("Open commit details"); after != details {
		t.Fatalf("scrolling files moved the Open commit details button: before=%+v after=%+v", details, after)
	}
	click(label)
	if _, ok := tt.Find("Open commit details"); ok {
		t.Fatal("collapsed preview still shows the Open commit details button")
	}
	click(label)
	tabCount := len(s.tabs)
	click("Open commit details")
	tt.Frame()
	dest := s.activeApp()
	if dest != existing.app || len(s.tabs) != tabCount || dest.state != stateReady ||
		dest.workspaceTab != sectionHistory || dest.historyFilter.applied.Query != commit.Hash {
		t.Fatalf("Details did not reuse the existing tab for History: tabs=%d state=%v section=%v target=%+v",
			len(s.tabs), dest.state, dest.workspaceTab, dest.overviewCommitTarget)
	}
	// Progressive opens may install a default branch after the History tab is visible.
	dest.openDefaultCommitFilters("main")
	if selected, ok := dest.selectedVisibleCommit(); !ok || selected.Hash != commit.Hash ||
		len(dest.visibleCommits()) != 1 || dest.historyFilter.applied.Branch != "" {
		t.Fatal("the default branch filter replaced the selected commit")
	}
	if _, ok := tt.Find(commit.Subject); !ok {
		t.Fatal("History does not show the selected commit")
	}
	if _, ok := tt.Find("Other commit"); ok {
		t.Fatal("History shows an unrelated commit")
	}
}

func TestOverviewSetupMatchesSavedSubfolderToRecentRoot(t *testing.T) {
	root := t.TempDir()
	subfolder := filepath.Join(root, "server")
	nestedRepo := filepath.Join(root, "nested")
	opener := repository.Opener{}
	for _, dir := range []string{root, subfolder, nestedRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if dir != subfolder {
			if err := opener.Init(context.Background(), dir); err != nil {
				t.Fatal(err)
			}
		}
	}
	a := newApp(opener)
	a.pathUserData = t.TempDir()
	updated := newUpdates()
	a.update = updated.update
	a.recentRepositories = []recentRepository{{Path: root}}
	a.overview = newOverviewState()
	a.overview.config = overviewConfig{Active: "overview", Overviews: []overviewDef{{
		ID: "overview", Name: "Overview",
		Repos: []overviewRepo{{Path: subfolder, Name: "NIGHT"}, {Path: nestedRepo}},
	}}}
	if err := saveOverviewConfig(a.dataDir(), a.overview.config); err != nil {
		t.Fatal(err)
	}
	a.openOverviewSetup()
	updated.waitForUpdate(t)
	setup := &a.overview.setup
	if !setup.selected[root] || setup.names[root] != "NIGHT" ||
		len(setup.custom) != 1 || setup.custom[0] != nestedRepo {
		t.Fatalf("matched repository selection = %+v", setup)
	}
	a.opener = nil // Verify saved selection without starting a new timeline read.
	a.applyOverviewSetup()
	saved := loadOverviewConfig(a.dataDir())
	current := saved.current()
	if current == nil {
		t.Fatal("saved repository selection did not retain the active Overview")
	}
	repos := current.Repos
	if len(repos) != 2 || repos[0].Path != root || repos[0].Name != "NIGHT" || repos[1].Path != nestedRepo {
		t.Fatalf("saved selection = %+v", repos)
	}
}

func TestOverviewCommitReusesRepositoryTabWhileOpening(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{})
	const path = "/work/project"
	count := len(s.tabs)
	s.openOverviewCommitTab(path, repository.Commit{Hash: "first", Subject: "First commit"})
	opened := s.activeTab()
	if len(s.tabs) != count+1 || opened.app.state != stateLoading {
		t.Fatal("Details did not start opening the repository")
	}
	s.openOverviewCommitTab(path, repository.Commit{Hash: "second", Subject: "Second commit"})
	if s.activeTab() != opened || len(s.tabs) != count+1 {
		t.Fatal("Details created another tab while the same repository was opening")
	}
	for range 12 {
		if opened.app.state == stateReady {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	selected, ok := opened.app.selectedVisibleCommit()
	if opened.app.workspaceTab != sectionHistory || !ok || selected.Hash != "second" {
		t.Fatalf("History did not select the latest request after opening: %+v", selected)
	}
	s.openTab(path)
	if s.activeTab() != opened || len(s.tabs) != count+1 {
		t.Fatal("opening the repository again did not reuse the tab")
	}
}

type overviewPagingOpener struct{ pathOpener }

func (overviewPagingOpener) Open(context.Context, string) (repository.Session, error) {
	return overviewPagingSession{}, nil
}

type overviewPagingSession struct{ fakeSession }

func (overviewPagingSession) CommitMessage(context.Context, repository.Commit) (repository.CommitMessage, error) {
	return repository.CommitMessage{Description: "The full commit description."}, nil
}

func (overviewPagingSession) CommitFilesWithoutStats(context.Context, repository.Commit) ([]repository.CommitFile, error) {
	files := []repository.CommitFile{{Path: "internal/app/changes.go", Status: "M"}}
	for i := 1; i < 10; i++ {
		files = append(files, repository.CommitFile{Path: fmt.Sprintf("internal/app/file-%02d.go", i), Status: "A"})
	}
	return files, nil
}

func (overviewPagingSession) CommitHistoryHead(context.Context) (string, string, error) {
	return "main", "head", nil
}

func (overviewPagingSession) CommitHistoryOrder(context.Context, string, time.Time, time.Time, repository.CommitHistorySearch) ([]string, error) {
	hashes := make([]string, 72)
	for i := range hashes {
		hashes[i] = fmt.Sprintf("%07d", i)
	}
	return hashes, nil
}

func (overviewPagingSession) CommitHistoryPage(_ context.Context, hashes []string) ([]repository.Commit, error) {
	commits := make([]repository.Commit, 0, len(hashes))
	for _, hash := range hashes {
		i, err := strconv.Atoi(hash)
		if err != nil {
			return nil, err
		}
		commits = append(commits, repository.Commit{Hash: hash, ShortHash: hash, Subject: hash, UnixTimestamp: int64(1000 - i)})
	}
	return commits, nil
}

func TestOverviewTimelineAutomaticallyLoadsNextPage(t *testing.T) {
	a := newApp(overviewPagingOpener{})
	updated := newUpdates()
	a.update = updated.update
	a.overview = newOverviewState()
	t.Cleanup(a.stopOverview)
	o := a.overview
	o.config = overviewConfig{Overviews: []overviewDef{{Repos: []overviewRepo{{Path: "/work/project"}}}}}
	a.reloadOverview()
	updated.waitForUpdate(t)
	tt := ui.NewTester(a.overviewView, 1100, 650)
	tt.Frame()
	if len(o.commits) != overviewPerRepoLimit || o.anyLoading() || !o.canLoadMore() {
		t.Fatal("first page did not settle before scrolling")
	}
	viewport, ok := tt.Find("Recent commits")
	if !ok {
		t.Fatal("no timeline viewport")
	}
	tt.Scroll(viewport.X+180, viewport.Y+120, 0, 4000)
	if !o.anyLoading() {
		t.Fatal("scrolling to the bottom did not request a page")
	}
	updated.waitForUpdate(t)
	tt.Frame()
	if len(o.commits) != 72 || o.canLoadMore() || o.commits[71].Commit.Hash != "0000071" {
		t.Fatalf("next page was not appended once: rows=%d more=%t", len(o.commits), o.canLoadMore())
	}
}

func TestOverviewTimelineMergesRepositoryPagesByCommitTime(t *testing.T) {
	a := newApp(nil)
	a.overview = newOverviewState()
	o := a.overview
	o.repos["busy"] = &overviewRepoState{}
	o.repos["quiet"] = &overviewRepoState{}
	busy := make([]repository.Commit, overviewPerRepoLimit+1)
	for i := range busy {
		busy[i].UnixTimestamp = int64(100 - i)
	}
	a.finishOverviewRepo(o.token, "busy", overviewRepoPage{commits: busy}, nil)
	a.finishOverviewRepo(o.token, "quiet", overviewRepoPage{commits: []repository.Commit{
		{UnixTimestamp: 80}, {UnixTimestamp: 49},
	}}, nil)
	first := o.visibleCommits()
	if len(first) != overviewPerRepoLimit+1 || first[len(first)-1].Commit.UnixTimestamp != 51 {
		t.Fatal("the timeline exposed a commit older than the other repository's next page")
	}
	a.finishOverviewRepo(o.token, "busy", overviewRepoPage{commits: []repository.Commit{
		{UnixTimestamp: 50}, {UnixTimestamp: 48},
	}}, nil)
	rows := o.visibleCommits()
	if len(rows) != overviewPerRepoLimit+4 {
		t.Fatalf("unexpected merged row count: %d", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Commit.UnixTimestamp < rows[i].Commit.UnixTimestamp {
			t.Fatalf("commits out of order around index %d", i)
		}
	}
}
