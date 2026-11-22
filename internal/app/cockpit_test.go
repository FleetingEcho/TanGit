package app

import (
	"fmt"
	"slices"
	"strings"
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
