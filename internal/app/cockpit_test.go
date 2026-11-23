package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

func TestCockpitSummaryCleanRepository(t *testing.T) {
	summary := buildCockpitSummary(cockpitInput{Snapshot: repository.Snapshot{Root: "/work/clean", Branch: "main"}})
	if !summary.Repository.Clean || len(summary.Attention) != 1 || summary.Attention[0].ID != "clean" {
		t.Fatalf("clean summary is wrong: %+v", summary)
	}
	if summary.Operation != nil || summary.Agents.Loaded || summary.Collaboration.ChecksCached {
		t.Fatalf("zero state was misreported as loaded or cached: %+v", summary)
	}
}

func TestCockpitSummaryWorkingTreeAndDivergence(t *testing.T) {
	input := cockpitInput{Snapshot: repository.Snapshot{
		Root: "/work/dirty", Branch: "feature",
		Files: []repository.File{
			{Path: "staged", Staged: true, Tracked: true},
			{Path: "modified", Unstaged: true, Tracked: true},
			{Path: "new", Unstaged: true},
			{Path: "conflict", HasMergeConflicts: true, Staged: true, Unstaged: true, Tracked: true},
		},
		Branches: []repository.Branch{{Name: "feature", Head: true, Ahead: "3", Behind: "2", UpstreamRemote: "origin", UpstreamBranch: "feature"}},
	}, Reviewed: 2, ReviewTotal: 4}
	summary := buildCockpitSummary(input)
	if summary.WorkingTree.Staged != 1 || summary.WorkingTree.Unstaged != 1 || summary.WorkingTree.Untracked != 1 || summary.WorkingTree.Conflicted != 1 {
		t.Fatalf("working-tree counts are wrong: %+v", summary.WorkingTree)
	}
	if summary.Collaboration.Ahead != 3 || summary.Collaboration.Behind != 2 || summary.Collaboration.Upstream != "origin/feature" {
		t.Fatalf("divergence is wrong: %+v", summary.Collaboration)
	}
	if len(summary.Attention) != 3 || summary.Attention[0].ID != "conflicts" || summary.Attention[1].ID != "branch-ahead" || summary.Attention[2].ID != "staged" {
		t.Fatalf("attention priority/cap is wrong: %+v", summary.Attention)
	}
}

func TestCockpitSummaryOperationAndFailurePriority(t *testing.T) {
	for _, test := range []struct {
		name  string
		input cockpitInput
		want  string
	}{
		{name: "rebase", input: cockpitInput{Snapshot: repository.Snapshot{Operation: "rebase"}}, want: "operation"},
		{name: "bisect", input: cockpitInput{Snapshot: repository.Snapshot{Bisect: &repository.BisectState{Active: true}}}, want: "operation"},
		{name: "failure", input: cockpitInput{ActionError: "fatal: token=ghp_secret", Snapshot: repository.Snapshot{Operation: "merge"}}, want: "failure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			summary := buildCockpitSummary(test.input)
			if len(summary.Attention) == 0 || summary.Attention[0].ID != test.want {
				t.Fatalf("wrong highest-priority attention: %+v", summary.Attention)
			}
			if strings.Contains(fmt.Sprint(summary.Attention), "ghp_secret") {
				t.Fatal("failure attention was not redacted")
			}
		})
	}
}

func TestCockpitSummaryAgentStatesAndLatestFiveActivity(t *testing.T) {
	activities := make([]activityItem, 8)
	for index := range activities {
		activities[index] = activityItem{ID: activityID{Source: activitySourceCommand, Repo: "repo", Value: fmt.Sprint(index)}, Title: fmt.Sprint(index)}
	}
	summary := buildCockpitSummary(cockpitInput{
		AgentsLoaded: true,
		Agents: []agentwork.Task{
			{ID: "input", Status: agentwork.StatusNeedsInput},
			{ID: "tests", Status: agentwork.StatusTesting, LastTest: &agentwork.TestResult{Status: agentwork.TestStatusFailed}},
			{ID: "ready", Status: agentwork.StatusReadyForReview},
			{ID: "blocked", Status: agentwork.StatusBlocked},
			{ID: "running", Status: agentwork.StatusRunning},
		},
		Recent: activities,
	})
	if summary.Agents.NeedsInput != 1 || summary.Agents.Testing != 1 || summary.Agents.ReadyReview != 1 || summary.Agents.Blocked != 1 || summary.Agents.Running != 1 || summary.Agents.FailedTests != 1 {
		t.Fatalf("agent summary counts are wrong: %+v", summary.Agents)
	}
	if summary.Attention[0].ID != "agent-input" {
		t.Fatalf("needs-input did not outrank the other agent states: %+v", summary.Attention)
	}
	if len(summary.Recent) != 8 {
		t.Fatal("pure summary unexpectedly filtered its supplied recent copy")
	}
	if !slices.Equal(activities, summary.Recent) || len(cockpitInput{Recent: activities}.Recent) == 0 {
		// The copy check below mutates the original, proving no caller slice is retained.
		t.Fatal("recent fixture was not built")
	}
	activities[0].Title = "changed"
	if summary.Recent[0].Title == "changed" {
		t.Fatal("summary retained the caller's recent slice")
	}
}

func TestCockpitSummaryLoadingIsNotEmpty(t *testing.T) {
	summary := buildCockpitSummary(cockpitInput{
		DetailsLoading: true, AgentsLoading: true,
		ChecksLoading: true, Snapshot: repository.Snapshot{Root: "/work/loading"},
	})
	if !summary.Repository.DetailsLoading || !summary.Agents.Loading || !summary.Collaboration.ChecksLoading {
		t.Fatalf("loading flags were lost: %+v", summary)
	}
	if len(summary.Attention) != 2 || summary.Attention[1].ID != "details-loading" {
		t.Fatalf("progressive loading was omitted or treated as a failure: %+v", summary.Attention)
	}
}

func TestCockpitSummaryAttentionMaximumThree(t *testing.T) {
	summary := buildCockpitSummary(cockpitInput{
		ActionError: "failure", DirtyEditors: 2,
		Agents:    []agentwork.Task{{Status: agentwork.StatusNeedsInput}},
		Transfers: []activityTransfer{{State: activityStateFailed}},
		Snapshot: repository.Snapshot{
			Operation: "merge",
			Files:     []repository.File{{HasMergeConflicts: true, Tracked: true}},
			Branches:  []repository.Branch{{Head: true, Ahead: "1", Behind: "0"}},
		},
	})
	if len(summary.Attention) != 3 || summary.Attention[0].ID != "failure" || summary.Attention[1].ID != "operation" || summary.Attention[2].ID != "conflicts" {
		t.Fatalf("attention order/cap is wrong: %+v", summary.Attention)
	}
}

func TestOverviewStartPreferenceMigration(t *testing.T) {
	fresh := defaultSettings()
	if !fresh.OpenRepositoriesOnOverview || !fresh.OverviewStartConfigured || fresh.SectionOrder[0] != "overview" {
		t.Fatalf("fresh defaults did not opt into Overview: %+v", fresh)
	}
	legacy := normalizeSettings(appSettings{
		Theme: defaultTheme, VisibleSections: []string{"code", "changes"}, SectionOrder: []string{"code", "changes"},
	})
	// Existing profiles are moved onto Overview once, as the landing page too.
	if !legacy.OpenRepositoriesOnOverview || !legacy.OverviewStartConfigured || !legacy.OverviewPlacementMigrated {
		t.Fatalf("legacy profile was not given Overview as its landing page: %+v", legacy)
	}
	// Existing profiles keep their own order and landing page, and get Overview
	// shown and first once.
	if !slices.Equal(legacy.SectionOrder[:3], []string{"overview", "code", "changes"}) || !slices.Contains(legacy.VisibleSections, "overview") {
		t.Fatalf("legacy profile was not given Overview first: order=%v visible=%v", legacy.SectionOrder, legacy.VisibleSections)
	}
	a := newApp(nil)
	a.state = stateReady
	a.session = fakeSession{}
	a.settings = legacy
	a.settingsLoaded = true
	if !a.repositoryStartsOnOverview() {
		t.Fatal("migrated legacy profile does not start on Overview")
	}
	a.settings.OpenRepositoriesOnOverview = false
	if a.repositoryStartsOnOverview() {
		t.Fatal("a later choice of another landing page was ignored")
	}
	a.settings = fresh
	if !a.repositoryStartsOnOverview() {
		t.Fatal("fresh loaded profile did not select Overview")
	}
	a.session = nil
	if a.repositoryStartsOnOverview() || a.sectionVisible(sectionOverview) {
		t.Fatal("editor-only workspace exposed or selected Overview")
	}
}

func TestOverviewViewCommandsUseCatalogAndSurviveExtraFrames(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.setWorkspaceTab(sectionOverview)
	var invoked []commandID
	h.app.invokeCommand = func(id commandID) commandInvokeResult {
		invoked = append(invoked, id)
		return commandInvokeResult{Status: commandInvokeAccepted}
	}
	h.app.resolveCommand = func(commandID) commandState { return commandState{Visible: true, Enabled: true} }
	// Tall enough for the last card to be on screen.
	h.tester.SetSize(1280, 1800)
	h.tester.Frame()
	h.tester.Frame()
	texts := h.texts()
	for _, expected := range []string{"Continue where you left off", "Working tree", "Operation & recovery", "Collaboration", "Agent tasks", "Recent activity"} {
		if !slices.Contains(texts, expected) {
			t.Errorf("Overview is missing %q: %q", expected, texts)
		}
	}
	if err := h.tester.Click("Open Command log"); err != nil {
		t.Fatal(err)
	}
	if len(invoked) != 1 || invoked[0] != commandOpenLog {
		t.Fatalf("CTA did not use Command Catalog: %v", invoked)
	}
	// Attention and cards can disappear/appear across builds without retaining
	// stale elements or losing stable identities.
	h.app.actionError = "fatal: example"
	h.tester.Frame()
	h.app.actionError = ""
	h.tester.Frame()
	h.tester.Frame()
	if h.app.crash != "" {
		t.Fatalf("Overview rebuild crashed: %s", h.app.crash)
	}
}

func TestOverviewResponsiveWidths(t *testing.T) {
	for _, width := range []int{720, 1280, 1920} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			h, _ := menuTestAppWithCalls(t)
			h.app.setWorkspaceTab(sectionOverview)
			h.tester.SetSize(width, 900)
			h.tester.Frame()
			h.tester.Frame()
			if h.app.crash != "" {
				t.Fatalf("Overview crashed at %d DIP: %s", width, h.app.crash)
			}
			if info, ok := h.tester.Access("Continue where you left off"); ok {
				if info.Bounds.X < 0 || info.Bounds.X+info.Bounds.W > float32(width) {
					t.Fatalf("attention overflows %d DIP: %+v", width, info.Bounds)
				}
			}
		})
	}
}

func BenchmarkCockpitSummaryLargeRepository(b *testing.B) {
	input := cockpitInput{Snapshot: repository.Snapshot{Root: "/work/large", Branch: "main"}}
	for index := 0; index < 5000; index++ {
		input.Snapshot.Files = append(input.Snapshot.Files, repository.File{Path: fmt.Sprintf("file-%d", index), Staged: index%3 == 0, Unstaged: true, Tracked: index%5 != 0})
	}
	for index := 0; index < 300; index++ {
		input.Agents = append(input.Agents, agentwork.Task{ID: fmt.Sprint(index), Status: agentwork.StatusRunning})
	}
	for index := 0; index < 500; index++ {
		input.Recent = append(input.Recent, activityItem{OccurredAt: time.Unix(int64(index), 0)})
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = buildCockpitSummary(input)
	}
}

// summarySession adds the contributor statistics to a fake session.
type summarySession struct {
	fakeSession
	summary repository.ContributorSummary
	// byBranch answers a branch's read with its own summary; reads records the
	// branches that were read.
	byBranch map[string]repository.ContributorSummary
	reads    *[]string
	mu       *sync.Mutex
}

func (s summarySession) ContributorSummary(_ context.Context, branch string) (repository.ContributorSummary, error) {
	if s.reads != nil {
		s.mu.Lock()
		*s.reads = append(*s.reads, branch)
		s.mu.Unlock()
	}
	if summary, ok := s.byBranch[branch]; ok {
		return summary, nil
	}
	return s.summary, nil
}

// Entering the Overview reads the contributor statistics once and shows them;
// switching the period recounts them without reading Git again.
func TestOverviewShowsContributorStatistics(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	now := time.Now().Unix()
	day := int64(24 * 3600)
	h.app.session = summarySession{fakeSession: h.app.session.(fakeSession), summary: repository.ContributorSummary{
		Contributors: []repository.Contributor{
			{Name: "Alice Example", Email: "alice@example.com", Commits: 4, CommitTimes: []int64{now - 200*day, now - 40*day, now - 2*day, now - day}},
			{Name: "Bob Builder", Email: "bob@example.com", Commits: 2, CommitTimes: []int64{now - 300*day, now - 3*day}},
		},
		TotalCommits: 6, FirstCommit: now - 300*day, LastCommit: now - day,
	}}
	h.app.setWorkspaceTab(sectionOverview)
	h.tester.SetSize(1280, 1800)
	h.tester.Frame()
	h.waitFor(t, "the contributor statistics", func() bool { return h.app.cockpit.contributors.loaded })
	h.tester.Frame()
	texts := h.texts()
	for _, want := range []string{"Contributors", "Most active", "3 commits", "Last week"} {
		if !slices.Contains(texts, want) {
			t.Errorf("Overview is missing %q: %q", want, texts)
		}
	}
	// The default period is the last week: Alice has 2 commits, Bob 1.
	if _, ok := h.tester.Find("Alice Example — 2 commits"); !ok {
		t.Fatalf("top contributor for the week is missing: %q", texts)
	}
	if err := h.tester.Click("Last year"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if _, ok := h.tester.Find("Alice Example — 4 commits"); !ok {
		t.Fatalf("switching to a year did not recount: %q", h.texts())
	}

	// A session that cannot read contributors leaves the card out.
	plain, _ := menuTestAppWithCalls(t)
	plain.app.setWorkspaceTab(sectionOverview)
	plain.tester.SetSize(1280, 1800)
	plain.tester.Frame()
	plain.tester.Frame()
	if slices.Contains(plain.texts(), "Top contributors") || !plain.app.cockpit.contributors.unavailable {
		t.Fatal("an unsupported session should have no Contributors card")
	}
}

func TestContributorRangeCountsPeriodsAndTrend(t *testing.T) {
	now := time.Unix(100_000_000, 0)
	day := int64(24 * 3600)
	at := func(daysAgo int64) int64 { return now.Unix() - daysAgo*day }
	summary := repository.ContributorSummary{
		Contributors: []repository.Contributor{
			{Name: "A", Email: "a@x", Commits: 5, CommitTimes: []int64{at(400), at(20), at(12), at(2), at(1)}},
			{Name: "B", Email: "b@x", Commits: 2, CommitTimes: []int64{at(10), at(3)}},
			{Name: "C", Email: "c@x", Commits: 1, CommitTimes: []int64{at(500)}},
		},
		TotalCommits: 8, FirstCommit: at(500), LastCommit: at(1),
	}
	week := computeContributorRange(summary, contributorRangeWeek, now)
	if week.Commits != 3 || week.Active != 2 || week.Total != 3 {
		t.Fatalf("week = %+v", week)
	}
	// The week before had A's commit 12 days ago and B's 10 days ago.
	if week.PreviousCommits != 2 {
		t.Fatalf("previous week = %d, want 2", week.PreviousCommits)
	}
	if len(week.Top) != 2 || week.Top[0].Contributor.Name != "A" || week.Top[0].Commits != 2 {
		t.Fatalf("top of the week = %+v", week.Top)
	}
	sum := 0
	for _, count := range week.Buckets {
		sum += count
	}
	if len(week.Buckets) != 7 || sum != 3 || week.Buckets[5] != 1 {
		t.Fatalf("week chart = %v", week.Buckets)
	}
	month := computeContributorRange(summary, contributorRangeMonth, now)
	if month.Commits != 6 || month.Active != 2 {
		t.Fatalf("month = %+v", month)
	}
	all := computeContributorRange(summary, contributorRangeAll, now)
	if all.Commits != 8 || all.Active != 3 || all.PreviousCommits != 0 || len(all.Buckets) != 24 {
		t.Fatalf("all time = %+v", all)
	}
	total := 0
	for _, count := range all.Buckets {
		total += count
	}
	if total != 8 {
		t.Fatalf("all-time chart lost commits: %v", all.Buckets)
	}
	if all.TopFiveShare != 100 {
		t.Fatalf("top five share = %d", all.TopFiveShare)
	}
}

// The branch picker counts another branch's history, and a branch read earlier
// is reused instead of read again.
func TestOverviewContributorsFollowTheChosenBranch(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	now := time.Now().Unix()
	day := int64(24 * 3600)
	var reads []string
	mu := &sync.Mutex{}
	h.app.session = summarySession{
		fakeSession: h.app.session.(fakeSession), reads: &reads, mu: mu,
		summary: repository.ContributorSummary{
			Contributors: []repository.Contributor{{Name: "Alice", Email: "a@x", Commits: 2, CommitTimes: []int64{now - day, now - 2*day}}},
			TotalCommits: 2,
		},
		byBranch: map[string]repository.ContributorSummary{
			"feature": {
				Contributors: []repository.Contributor{{Name: "Zoe", Email: "z@x", Commits: 1, CommitTimes: []int64{now - day}}},
				TotalCommits: 1,
			},
		},
	}
	h.app.snapshot.Branches = []repository.Branch{{Name: "main", Head: true}, {Name: "feature"}}
	h.app.setWorkspaceTab(sectionOverview)
	h.tester.SetSize(1280, 1800)
	h.tester.Frame()
	h.waitFor(t, "all branches", func() bool { return h.app.cockpit.contributors.loaded })
	if got := h.app.cockpit.contributors.stats.Top; len(got) != 1 || got[0].Contributor.Name != "Alice" {
		t.Fatalf("all-branches top = %+v", got)
	}

	h.app.selectContributorBranch("feature", "feature")
	h.waitFor(t, "the feature branch", func() bool { return h.app.cockpit.contributors.loaded })
	if got := h.app.cockpit.contributors.stats.Top; len(got) != 1 || got[0].Contributor.Name != "Zoe" {
		t.Fatalf("feature top = %+v", got)
	}

	// Back to every branch: from the cache, with no further read.
	h.app.selectContributorBranch("", "")
	if !h.app.cockpit.contributors.loaded || h.app.cockpit.contributors.stats.Top[0].Contributor.Name != "Alice" {
		t.Fatalf("a branch read earlier was not reused: %+v", h.app.cockpit.contributors.stats.Top)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reads) != 2 || reads[0] != "" || reads[1] != "feature" {
		t.Fatalf("branches read = %q, want [\"\" feature]", reads)
	}
	choices := h.app.contributorBranchChoices()
	if len(choices) < 3 || choices[0].branch != "" || choices[1].branch != "main" {
		t.Fatalf("branch choices = %+v", choices)
	}
}

func TestCommitHeatmapCountsDaysStreaksAndLevels(t *testing.T) {
	now := time.Date(2026, 6, 10, 15, 0, 0, 0, time.Local) // a Wednesday
	at := func(daysAgo int, hour int) int64 {
		day := time.Date(2026, 6, 10, hour, 0, 0, 0, time.Local).AddDate(0, 0, -daysAgo)
		return day.Unix()
	}
	summary := repository.ContributorSummary{Contributors: []repository.Contributor{
		{Name: "A", Email: "a@x", CommitTimes: []int64{at(0, 9), at(0, 11), at(1, 10), at(2, 10), at(9, 10), at(400, 10)}},
	}}
	heat := buildCommitHeatmap(summary, 4, now)
	if len(heat.Weeks) != 4 {
		t.Fatalf("weeks = %d", len(heat.Weeks))
	}
	// 5 commits fall inside four weeks; the one 400 days ago does not.
	if heat.Total != 5 || heat.ActiveDays != 4 || heat.Max != 2 {
		t.Fatalf("total=%d active=%d max=%d", heat.Total, heat.ActiveDays, heat.Max)
	}
	if heat.LongestStreak != 3 {
		t.Fatalf("longest streak = %d, want 3 (today, yesterday and the day before)", heat.LongestStreak)
	}
	if heat.Busiest.Count != 2 || !heat.Busiest.Date.Equal(time.Date(2026, 6, 10, 0, 0, 0, 0, time.Local)) {
		t.Fatalf("busiest = %+v", heat.Busiest)
	}
	// The last column starts on Monday; days after today are out of range.
	last := heat.Weeks[3]
	if weekdayIndex(last[0].Date) != 0 || last[2].Count != 2 || last[3].InRange {
		t.Fatalf("last week = %+v", last)
	}
	if heatLevel(0, 10) != 0 || heatLevel(1, 10) != 1 || heatLevel(10, 10) != 4 || heatLevel(5, 10) != 2 {
		t.Fatal("heat levels are off")
	}
}

func TestCodeVolumeBucketsByPeriod(t *testing.T) {
	now := time.Unix(100_000_000, 0)
	day := int64(24 * 3600)
	volume := repository.CodeVolume{Commits: []repository.CommitVolume{
		{At: now.Unix() - day, Added: 10, Deleted: 2},
		{At: now.Unix() - 2*day, Added: 5},
		{At: now.Unix() - 40*day, Added: 99}, // outside the week
	}}
	stats := bucketCodeVolume(volume, contributorRangeWeek, now)
	if stats.Added != 15 || stats.Deleted != 2 || stats.Commits != 2 || len(stats.BucketAdded) != 7 {
		t.Fatalf("week = %+v", stats)
	}
	if stats.BucketAdded[5] != 10 || stats.BucketDeleted[5] != 2 || stats.BucketAdded[4] != 5 {
		t.Fatalf("week buckets = %v / %v", stats.BucketAdded, stats.BucketDeleted)
	}
	all := bucketCodeVolume(volume, contributorRangeAll, now)
	if all.Added != 114 || len(all.BucketAdded) != 24 {
		t.Fatalf("all = %+v", all)
	}
	if since := codeVolumeSince(contributorRangeWeek, now); since != now.Unix()+1-7*day {
		t.Fatalf("since = %d", since)
	}
	if codeVolumeSince(contributorRangeAll, now) != 0 {
		t.Fatal("the whole history has no start")
	}
}

func TestBranchHealthGroupsMergedStaleAndFarBehind(t *testing.T) {
	now := time.Unix(200_000_000, 0)
	day := int64(24 * 3600)
	health := repository.BranchHealth{Default: "main", Branches: []repository.BranchHealthEntry{
		{Name: "main", Current: true, LastCommit: now.Unix()},
		{Name: "done", Merged: true, LastCommit: now.Unix() - 5*day, Behind: 3},
		{Name: "abandoned", LastCommit: now.Unix() - 200*day, Behind: 40},
		{Name: "ancient", LastCommit: now.Unix() - 400*day, Behind: 900},
		{Name: "drifting", LastCommit: now.Unix() - 10*day, Behind: 350},
		{Name: "healthy", LastCommit: now.Unix() - 2*day, Behind: 4},
		{Name: "origin/main", Remote: true, LastCommit: now.Unix()},
		{Name: "origin/done", Remote: true, Merged: true, LastCommit: now.Unix() - 9*day},
	}}
	stats := classifyBranchHealth(health, now)
	if stats.Local != 6 || stats.Remote != 2 {
		t.Fatalf("local=%d remote=%d", stats.Local, stats.Remote)
	}
	merged, stale, behind := stats.Groups[branchCategoryMerged], stats.Groups[branchCategoryStale], stats.Groups[branchCategoryBehind]
	if len(merged) != 2 || len(stale) != 2 || len(behind) != 1 {
		t.Fatalf("groups = merged %v stale %v behind %v", merged, stale, behind)
	}
	if stale[0].Name != "ancient" || behind[0].Name != "drifting" {
		t.Fatalf("order wrong: stale %v behind %v", stale, behind)
	}
}

// insightsSession answers the code-volume and branch-health reads, with the
// contributor summary the heat map is counted from.
type insightsSession struct {
	summarySession
	volume repository.CodeVolume
	health repository.BranchHealth
}

func (s insightsSession) CodeVolume(context.Context, string, int64, int) (repository.CodeVolume, error) {
	return s.volume, nil
}

func (s insightsSession) BranchHealth(context.Context) (repository.BranchHealth, error) {
	return s.health, nil
}

func TestOverviewShowsHeatmapCodeChangesAndBranchHealth(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	now := time.Now().Unix()
	day := int64(24 * 3600)
	h.app.snapshot.Branches = []repository.Branch{{Name: "main", Head: true}, {Name: "done"}, {Name: "old"}}
	h.app.session = insightsSession{
		summarySession: summarySession{fakeSession: h.app.session.(fakeSession), summary: repository.ContributorSummary{
			Contributors: []repository.Contributor{{Name: "Alice", Email: "a@x", Commits: 3, CommitTimes: []int64{now - 5*day, now - 4*day, now - day}}},
			TotalCommits: 3, FirstCommit: now - 5*day, LastCommit: now - day,
		}},
		volume: repository.CodeVolume{Commits: []repository.CommitVolume{{At: now - day, Added: 1200, Deleted: 300, Files: 4}}},
		health: repository.BranchHealth{Default: "main", Branches: []repository.BranchHealthEntry{
			{Name: "main", Current: true, LastCommit: now},
			{Name: "done", Merged: true, LastCommit: now - 3*day, Behind: 2},
			{Name: "old", LastCommit: now - 300*day, Behind: 40},
		}},
	}
	h.app.setWorkspaceTab(sectionOverview)
	h.tester.SetSize(1280, 2400)
	h.tester.Frame()
	h.waitFor(t, "the history reads", func() bool {
		in := h.app.cockpit.insights
		return h.app.cockpit.contributors.loaded && in.volumeLoaded && in.healthLoaded
	})
	h.tester.Frame()
	texts := h.texts()
	for _, want := range []string{"Commit activity", "Code changes", "Branch health", "+1,200", "−300", "3 commits in the last year"} {
		if !slices.Contains(texts, want) {
			t.Errorf("Overview is missing %q: %q", want, texts)
		}
	}
	// The merged group is first; its branch has a delete button, which asks
	// before removing anything.
	if _, ok := h.tester.Find("1 merged — safe to delete"); !ok {
		t.Fatalf("merged group chip is missing: %q", texts)
	}
	if err := h.tester.Click("Delete branch done"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.title != "Delete Branch" {
		t.Fatalf("deleting a merged branch did not ask first: %#v", h.app.confirm)
	}
	h.app.confirm = nil
	h.tester.Frame()
	// The stale group lists the old branch.
	if err := h.tester.Click("1 stale (90+ days)"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if _, ok := h.tester.Find("old"); !ok {
		t.Fatalf("the stale branch is not listed: %q", h.texts())
	}
}
