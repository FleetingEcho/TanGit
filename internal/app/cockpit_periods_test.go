package app

import (
	"testing"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// overviewPeriodsApp opens the Overview with every card loaded.
func overviewPeriodsApp(t *testing.T) *harness {
	t.Helper()
	h, _ := menuTestAppWithCalls(t)
	now := time.Now().Unix()
	day := int64(24 * 3600)
	h.app.snapshot.Branches = []repository.Branch{{Name: "main", Head: true}}
	h.app.session = insightsSession{
		summarySession: summarySession{fakeSession: h.app.session.(fakeSession), summary: repository.ContributorSummary{
			Contributors: []repository.Contributor{{Name: "Alice", Email: "a@x", Commits: 3, CommitTimes: []int64{now - 5*day, now - 4*day, now - day}}},
			TotalCommits: 3, FirstCommit: now - 5*day, LastCommit: now - day,
		}},
		volume: repository.CodeVolume{Commits: []repository.CommitVolume{{At: now - day, Added: 1200, Deleted: 300, Files: 4}}},
		health: repository.BranchHealth{Default: "main"},
	}
	h.app.setWorkspaceTab(sectionOverview)
	h.tester.SetSize(1280, 2400)
	h.tester.Frame()
	h.waitFor(t, "the history reads", func() bool {
		in := h.app.cockpit.insights
		return h.app.cockpit.contributors.loaded && in.volumeLoaded && in.healthLoaded && in.heatLoaded
	})
	return h
}

func pickPeriod(t *testing.T, h *harness, button, period string) {
	t.Helper()
	if err := h.tester.Click(button); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click(period); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
}

// Each card keeps its own time range: the Code changes button must not read
// the contributors again or change their period.
func TestCodeChangesPeriodLeavesContributorsAlone(t *testing.T) {
	h := overviewPeriodsApp(t)
	before := h.app.cockpit.contributors
	pickPeriod(t, h, "Code changes period", "Last month")
	after := h.app.cockpit.contributors
	if h.app.cockpit.insights.volumePeriod != contributorRangeMonth {
		t.Fatalf("Code changes period = %d, want Last month", h.app.cockpit.insights.volumePeriod)
	}
	if !after.loaded || after.token != before.token || after.period != before.period {
		t.Fatalf("Contributors changed with Code changes' period: before loaded=%v token=%d period=%d, after loaded=%v token=%d period=%d",
			before.loaded, before.token, before.period, after.loaded, after.token, after.period)
	}
}

// The Contributors period reads twice its length of history, so a longer one
// needs a new read; it must not change the Code changes period.
func TestContributorPeriodRereadsAndLeavesCodeChangesAlone(t *testing.T) {
	h := overviewPeriodsApp(t)
	before := h.app.cockpit.contributors
	volume := h.app.cockpit.insights.volumePeriod
	pickPeriod(t, h, "Contributor period", "Last month")
	h.waitFor(t, "the contributors read again", func() bool { return h.app.cockpit.contributors.loaded })
	after := h.app.cockpit.contributors
	if after.period != contributorRangeMonth {
		t.Fatalf("Contributors period = %d, want Last month", after.period)
	}
	if after.token == before.token {
		t.Fatal("a longer Contributors period must read the history again")
	}
	if h.app.cockpit.insights.volumePeriod != volume {
		t.Fatalf("Code changes period moved with Contributors: %d -> %d", volume, h.app.cockpit.insights.volumePeriod)
	}
}
