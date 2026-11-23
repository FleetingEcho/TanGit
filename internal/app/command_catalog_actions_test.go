package app

import (
	"strings"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/agentwork"
)

func TestCommandCatalogHunkActionsResolveAndRecheckExistingStatus(t *testing.T) {
	h, calls := changesTestApp(t, unstagedTrackedFile())
	h.waitFor(t, "hunk diff", func() bool { return h.app.diffState != nil })
	h.app.diffState.GoToHunk(0)
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}
	catalog := s.catalogFor(ctx)
	if err := catalog.validate(); err != nil {
		t.Fatal(err)
	}
	stage, ok := catalogCandidate(catalog.Search(ctx, "hunk", nil), "changes/hunk-stage-stage-or-unstage")
	if !ok || stage.Label != "Stage hunk" || !stage.Enabled {
		t.Fatalf("stage-hunk adapter did not use the canonical diff state: %+v", stage)
	}
	h.app.diffLoading = true
	result := catalog.Invoke(ctx, "changes/hunk-stage-stage-or-unstage")
	if result.Accepted() || !containsString(result.Reason, "load") || calls.has("stage:changed.txt") {
		t.Fatalf("stale diff still permitted a hunk mutation: result=%+v calls=%v", result, calls.all())
	}
	h.app.diffLoading = false
	h.app.diffKind = "Staged"
	catalog = s.catalogFor(ctx)
	stage, _ = catalogCandidate(catalog.Search(ctx, "stage hunk", nil), "changes/hunk-stage-stage-or-unstage")
	if stage.Label != "Unstage hunk" {
		t.Fatalf("dynamic action label diverged from the selected side: %+v", stage)
	}
}

func TestCommandCatalogConflictActionsReuseControlsAndReasons(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.conflictState = newConflictState("one.txt", "before\n<<<<<<< HEAD\nold\n=======\nnew\n>>>>>>> topic\nafter\n", 0)
	h.app.conflictStageTab = 3
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}
	catalog := s.catalogFor(ctx)
	if err := catalog.validate(); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		id    commandID
		query string
	}{{"conflicts/view-base", "View Base"}, {"conflicts/use-current", "Use current"}, {"conflicts/use-incoming", "Use incoming"}, {"conflicts/use-both", "Use both"}, {"conflicts/mark-resolved", "Mark resolved"}, {"conflicts/assistant", "Conflict Assistant"}} {
		id := target.id
		candidate, ok := catalogCandidate(catalog.Search(ctx, target.query, nil), id)
		if !ok || !candidate.Enabled {
			t.Errorf("existing conflict action %s is missing or incorrectly disabled: %+v", id, candidate)
		}
	}
	h.app.actionBusy = true
	result := catalog.Invoke(ctx, "conflicts/mark-resolved")
	if result.Accepted() || calls.has("stage:one.txt") {
		t.Fatalf("busy conflict resolution bypassed the shared mutation gate: %+v %v", result, calls.all())
	}
}

func TestCommandCatalogSelectedAgentActionsResolveCurrentTask(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionAgents
	h.app.agents.tasks = []agentwork.Task{{
		ID: "task-1", Title: "Task", Status: agentwork.StatusExited,
		Profile: agentwork.Profile{TestCommand: "go test ./..."},
	}}
	h.app.agents.selectedTaskID = "task-1"
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}
	catalog := s.catalogFor(ctx)
	for _, id := range []commandID{"agents/run-selected-tests", "agents/review-selected-result"} {
		candidate, ok := catalogCandidate(catalog.Search(ctx, "", nil), id)
		if !ok || !candidate.Enabled {
			t.Errorf("selected Agent action %s is unavailable: %+v", id, candidate)
		}
	}
	h.app.agents.tasks[0].Profile.TestCommand = ""
	candidate, ok := catalogCandidate(catalog.Search(ctx, "tests", nil), "agents/run-selected-tests")
	if !ok || candidate.Enabled || candidate.DisabledReason == "" {
		t.Fatalf("test command removal did not produce an invocation-time explanation: %+v", candidate)
	}
	h.app.agents.tasks[0].Status = agentwork.StatusArchived
	candidate, _ = catalogCandidate(catalog.Search(ctx, "review", nil), "agents/review-selected-result")
	if candidate.Enabled || candidate.DisabledReason != "Archived tasks cannot be reviewed." {
		t.Fatalf("archived review action did not reuse the archived reason: %+v", candidate)
	}
}

func containsString(value, substring string) bool {
	return len(value) >= len(substring) && len(substring) > 0 && strings.Contains(value, substring)
}
