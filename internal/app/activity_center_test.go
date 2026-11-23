package app

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func activityCenterShell(t *testing.T) (*shell, *ui.Tester, *updates) {
	t.Helper()
	s, tt, updated, _ := newShellShot(t, true, defaultTheme)
	a := s.activeApp()
	// Not the Overview: its recent-activity card would print the same rows.
	a.workspaceTab = sectionHistory
	if a.commandLog == nil {
		a.commandLog = repository.NewCommandLog()
	}
	a.commandLog.Record("git push --dry-run", "remote: authentication failed", true)
	a.commandLog.Record("git status", "clean", false)
	a.recordActivityNotice(activitySeverityInfo, "Workspace restored", "Pinned tabs were restored.", activityTarget{})
	tt.SetSize(1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	return s, tt, updated
}

// hasRow reports whether the list has a row labelled with the given prefix. The
// page behind the overlay also prints these titles, so only row labels count.
func hasRow(tt *ui.Tester, prefix string) bool {
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, prefix+" — ") {
			return true
		}
	}
	return false
}

func TestActivityCenterListsFiltersAndOpens(t *testing.T) {
	s, tt, _ := activityCenterShell(t)
	s.openActivityCenter()
	tt.Frame()
	if !tt.HasText("Activity Center") {
		t.Fatal("the Activity Center did not open")
	}
	for _, title := range []string{"git push --dry-run", "git status", "Workspace restored"} {
		if !hasRow(tt, title) {
			t.Fatalf("%q has no row in the Activity Center: %q", title, tt.Texts())
		}
	}

	// Needs attention keeps only the failed command.
	if err := tt.Click("Needs attention"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasRow(tt, "git push --dry-run") || hasRow(tt, "git status") || hasRow(tt, "Workspace restored") {
		t.Fatalf("attention filter kept the wrong rows: %q", tt.Texts())
	}
	if err := tt.Click("Needs attention"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// A source chip narrows to that source.
	if err := tt.Click("Notices"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !hasRow(tt, "Workspace restored") || hasRow(tt, "git status") {
		t.Fatalf("source filter kept the wrong rows: %q", tt.Texts())
	}
	if err := tt.Click("Notices"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// Opening a command row goes to the command log and leaves the center.
	if err := tt.Click("git push --dry-run — Failed"); err != nil {
		t.Fatalf("open the failed command: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if s.activity.open {
		t.Fatal("the Activity Center stayed open after opening a row")
	}
	if !s.activeApp().commandLogOpen {
		t.Fatal("opening a command activity did not show the command log")
	}
}

func TestActivityCenterKeyboardAndShortcutGuards(t *testing.T) {
	s, tt, _ := activityCenterShell(t)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyA)
	tt.Frame()
	if !s.activity.open {
		t.Fatal("the shortcut did not open the Activity Center")
	}
	// The palette must not open over it.
	s.openCommandPalette(nil)
	if s.palette.open {
		t.Fatal("the command palette opened over the Activity Center")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.activity.open {
		t.Fatal("Escape did not close the Activity Center")
	}
}

func TestActivityCenterIsInTheCommandPalette(t *testing.T) {
	s, _, _ := activityCenterShell(t)
	ctx := s.commandContext()
	candidate, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, "activity center", nil), "activity/open")
	if !ok || !candidate.Enabled {
		t.Fatalf("palette offers no Activity Center command: %+v", candidate)
	}
	s.invokeCommand("activity/open")
	if !s.activity.open {
		t.Fatal("the palette command did not open the Activity Center")
	}
}

func TestActivityCenterScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, tt, updated := activityCenterShell(t)
	s.openActivityCenter()
	saveScreenshot(t, dir, "activity-center-dark", tt)
	s.activity.attention = true
	saveScreenshot(t, dir, "activity-center-attention-dark", tt)
	s.activity.attention = false
	a := s.activeApp()
	a.session = contributorTestSession{
		searchSession: searchSession{fakeSession: fakeSession{}, mu: &sync.Mutex{}, filters: &[]repository.CommitFilter{}},
		contributors: []repository.Contributor{
			{Name: "Alice Example", Email: "alice@example.com", Commits: 128, LastCommit: 1_700_000_000},
			{Name: "Bob Builder", Email: "bob@example.com", Commits: 64, LastCommit: 1_690_000_000},
			{Name: "Chen Wei", Email: "chen@example.com", Commits: 9, LastCommit: 1_650_000_000},
		},
	}
	s.activity.view = activityViewContributors
	settleShot(t, tt, updated, func() bool { return s.activity.contributors.list != nil })
	saveScreenshot(t, dir, "contributors-dark", tt)
	s.toggleContributor(s.visibleContributors()[0])
	settleShot(t, tt, updated, func() bool { return !s.activity.contributors.commitsLoading })
	saveScreenshot(t, dir, "contributors-expanded-dark", tt)
}

// contributorTestSession adds the contributor list to a session that already
// answers commit searches.
type contributorTestSession struct {
	searchSession
	contributors []repository.Contributor
}

func (s contributorTestSession) Contributors(context.Context) ([]repository.Contributor, error) {
	return s.contributors, nil
}

func TestContributorsListLoadsExpandsAndFiltersHistory(t *testing.T) {
	s, h, filters := commitSearchShell(t)
	h.app.session = contributorTestSession{
		searchSession: h.app.session.(searchSession),
		contributors: []repository.Contributor{
			{Name: "Alice Example", Email: "alice@example.com", Commits: 12, FirstCommit: 1_600_000_000, LastCommit: 1_700_000_000},
			{Name: "Bob Builder", Email: "bob@example.com", Commits: 3, LastCommit: 1_690_000_000},
		},
	}
	s.openActivityCenterView(activityViewContributors)
	s.ensureContributors()
	h.waitFor(t, "the contributor list", func() bool { return !s.activity.contributors.loading && s.activity.contributors.list != nil })
	if got := s.visibleContributors(); len(got) != 2 || got[0].Name != "Alice Example" {
		t.Fatalf("contributors = %+v", got)
	}
	s.activity.query = "bob@"
	if got := s.visibleContributors(); len(got) != 1 || got[0].Name != "Bob Builder" {
		t.Fatalf("search by email = %+v", got)
	}
	s.activity.query = ""

	// Choosing a contributor reads their recent commits by author.
	s.toggleContributor(s.visibleContributors()[0])
	h.waitFor(t, "their commits", func() bool {
		return !s.activity.contributors.commitsLoading && len(s.activity.contributors.commits) > 0
	})
	if last := (*filters)[len(*filters)-1]; last.Author != "alice@example.com" || last.Limit != contributorRecentLimit {
		t.Fatalf("recent commits were read with %+v", last)
	}
	if len(s.activity.contributors.commits) != 1 || s.activity.contributors.commits[0].Subject != "Fix the ancient parser" {
		t.Fatalf("recent commits = %+v", s.activity.contributors.commits)
	}
	// A commit opens its actions in the palette.
	s.openContributorCommit(s.activity.contributors.commits[0])
	if s.activity.open || !s.palette.open || s.palette.target == nil || s.palette.target.commit.ShortHash != "deadbee" {
		t.Fatalf("commit did not open its palette actions: activity=%v palette=%v target=%+v", s.activity.open, s.palette.open, s.palette.target)
	}
	// Show in History narrows the list to the author.
	s.closeCommandPalette(false)
	h.app.filterHistoryByAuthor("alice@example.com")
	if h.app.workspaceTab != sectionHistory || h.app.historyFilter.applied.Author != "alice@example.com" {
		t.Fatalf("history was not narrowed: tab=%d filter=%+v", h.app.workspaceTab, h.app.historyFilter.applied)
	}
}

func TestContributorsViewRendersAndIsInThePalette(t *testing.T) {
	s, tt, updated := activityCenterShell(t)
	a := s.activeApp()
	a.session = contributorTestSession{
		searchSession: searchSession{fakeSession: fakeSession{}, mu: &sync.Mutex{}, filters: &[]repository.CommitFilter{}},
		contributors:  []repository.Contributor{{Name: "Alice Example", Email: "alice@example.com", Commits: 12, LastCommit: 1_700_000_000}},
	}
	ctx := s.commandContext()
	if candidate, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, "contributors", nil), "activity/contributors"); !ok || !candidate.Enabled {
		t.Fatalf("palette has no contributors command: %+v", candidate)
	}
	s.invokeCommand("activity/contributors")
	settleShot(t, tt, updated, func() bool { return s.activity.contributors.list != nil })
	if !hasRow(tt, "Alice Example") {
		t.Fatalf("the contributor is not listed: %q", tt.Texts())
	}
	if err := tt.Click("Alice Example — 12 commits"); err != nil {
		t.Fatalf("expand the contributor: %v", err)
	}
	tt.Frame()
	if s.activity.contributors.expanded != "alice@example.com" {
		t.Fatalf("expanded = %q", s.activity.contributors.expanded)
	}
}

// TestOverviewScreenshots is opt-in like the other screenshot harnesses.
func TestOverviewScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, size := range []struct {
		name string
		w, h int
	}{{"wide", 1280, 1700}, {"narrow", 720, 2300}} {
		s, tt, updated := activityCenterShell(t)
		a := s.activeApp()
		a.session = overviewShowcaseSession()
		a.workspaceTab = sectionOverview
		a.prepareCockpit()
		tt.SetSize(size.w, size.h)
		settleShot(t, tt, updated, func() bool {
			in := a.cockpit.insights
			return a.cockpit.contributors.loaded && in.volumeLoaded && in.healthLoaded
		})
		saveScreenshot(t, dir, "overview-"+size.name+"-dark", tt)
	}
}

// Hundreds of contributors stay quick to draw: one page at first, more on
// request, sortable, and searchable across the whole list.
func TestContributorsScaleToHundredsOfPeople(t *testing.T) {
	s, h, _ := commitSearchShell(t)
	var people []repository.Contributor
	for i := 0; i < 320; i++ {
		people = append(people, repository.Contributor{
			Name: fmt.Sprintf("Person %03d", i), Email: fmt.Sprintf("p%03d@example.com", i),
			Commits: 1000 - i, LastCommit: int64(1_600_000_000 + i*100),
		})
	}
	h.app.session = contributorTestSession{searchSession: h.app.session.(searchSession), contributors: people}
	s.openActivityCenterView(activityViewContributors)
	s.ensureContributors()
	h.waitFor(t, "the contributor list", func() bool { return s.activity.contributors.list != nil })

	if got := len(s.shownContributors()); got != contributorPageSize {
		t.Fatalf("first page = %d rows, want %d", got, contributorPageSize)
	}
	if got := len(s.visibleContributors()); got != 320 {
		t.Fatalf("the whole list = %d, want 320", got)
	}
	s.activity.contributors.limit = 120
	if got := len(s.shownContributors()); got != 120 {
		t.Fatalf("after show more = %d, want 120", got)
	}

	// Search reaches people beyond the page.
	s.activity.query = "p319@"
	if got := s.visibleContributors(); len(got) != 1 || got[0].Name != "Person 319" {
		t.Fatalf("search across the full list = %+v", got)
	}
	s.activity.query = ""

	// Sorting by recency puts the newest first; by name, alphabetical.
	s.activity.contributors.sortBy = contributorSortRecent
	if first := s.visibleContributors()[0]; first.Name != "Person 319" {
		t.Fatalf("most recent first = %q", first.Name)
	}
	s.activity.contributors.sortBy = contributorSortName
	if first := s.visibleContributors()[0]; first.Name != "Person 000" {
		t.Fatalf("by name first = %q", first.Name)
	}
	s.activity.contributors.sortBy = contributorSortCommits
	if first := s.visibleContributors()[0]; first.Name != "Person 000" {
		t.Fatalf("most commits first = %q", first.Name)
	}
}

// overviewShowcaseSession is a session with a year of history for screenshots
// of the Overview: contributors, code changes and branches.
func overviewShowcaseSession() insightsSession {
	now := time.Now().Unix()
	day := int64(24 * 3600)
	people := []repository.Contributor{
		{Name: "Alice Example", Email: "alice@example.com"},
		{Name: "Bob Builder", Email: "bob@example.com"},
		{Name: "Chen Wei", Email: "chen@example.com"},
		{Name: "Dana Okoye", Email: "dana@example.com"},
	}
	total := 0
	for index := range people {
		for i := 0; i < 160-index*30; i++ {
			people[index].CommitTimes = append(people[index].CommitTimes, now-int64((i*5+index*3)%360)*day)
		}
		slices.Sort(people[index].CommitTimes)
		people[index].Commits = len(people[index].CommitTimes)
		total += people[index].Commits
	}
	var volume repository.CodeVolume
	for i := 0; i < 90; i++ {
		volume.Commits = append(volume.Commits, repository.CommitVolume{At: now - int64(i*4)*3600*5, Added: 40 + (i*37)%300, Deleted: 10 + (i*53)%120, Files: 1 + i%6})
	}
	return insightsSession{
		summarySession: summarySession{fakeSession: fakeSession{}, summary: repository.ContributorSummary{
			Contributors: people, TotalCommits: total, FirstCommit: now - 360*day, LastCommit: now,
		}},
		volume: volume,
		health: repository.BranchHealth{Default: "main", Branches: []repository.BranchHealthEntry{
			{Name: "main", Current: true, LastCommit: now},
			{Name: "feature/login", Merged: true, LastCommit: now - 6*day, Behind: 12},
			{Name: "fix/typo", Merged: true, LastCommit: now - 20*day, Behind: 48},
			{Name: "experiment/graph", LastCommit: now - 210*day, Behind: 130},
			{Name: "old/release-1.2", LastCommit: now - 500*day, Behind: 900},
			{Name: "origin/wip", Remote: true, LastCommit: now - 120*day, Behind: 77},
		}},
	}
}
