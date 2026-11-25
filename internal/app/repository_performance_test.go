package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// performanceTestSession adds the optional performance capability to the
// ordinary fake session, recording what the dialog asked to apply.
type performanceTestSession struct {
	repository.Session
	mu      sync.Mutex
	report  repository.RepositoryPerformance
	applied []repository.PerformanceSettings
	reads   chan struct{}
}

func (s *performanceTestSession) Performance(context.Context) (repository.RepositoryPerformance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reads != nil {
		defer func() { s.reads <- struct{}{} }()
	}
	return s.report, nil
}

func (s *performanceTestSession) ApplyPerformance(_ context.Context, settings repository.PerformanceSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, settings)
	s.report.Settings = settings
	return nil
}

func TestRepositoryPerformanceDialogAppliesRecommendedSettings(t *testing.T) {
	h := menuTestApp(t)
	session := &performanceTestSession{Session: h.app.session, report: repository.RepositoryPerformance{
		TrackedFiles: 120000, GitVersion: "2.47.0", FSMonitorSupported: true, MaintenanceSupported: true,
		Settings: repository.PerformanceSettings{UntrackedCache: true},
	}}
	h.app.session = session
	h.app.openRepositoryPerformance()
	h.waitFor(t, "performance report", func() bool { return h.app.performance.report != nil })
	h.tester.Frame()
	for _, text := range []string{"Repository performance", "File system monitor", "Commit-graph", "Background maintenance", "120000 tracked files · Git 2.47.0 · large repository"} {
		if !h.tester.HasText(text) {
			t.Fatalf("dialog is missing %q", text)
		}
	}
	// Maintenance uses the system scheduler, so it is never preselected.
	if h.app.performance.desired.Maintenance {
		t.Fatal("background maintenance was preselected")
	}
	if err := h.tester.Click("Apply"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "dialog closes after applying", func() bool { return !h.app.performance.open })
	session.mu.Lock()
	defer session.mu.Unlock()
	want := repository.PerformanceSettings{FSMonitor: true, UntrackedCache: true, CommitGraph: true}
	if len(session.applied) != 1 || session.applied[0] != want {
		t.Fatalf("applied = %+v, want %+v", session.applied, want)
	}
}

func TestRepositoryPerformanceSuggestionOnlyForLargeUnoptimizedRepositories(t *testing.T) {
	for _, tc := range []struct {
		name   string
		report repository.RepositoryPerformance
		want   bool
	}{
		{"small", repository.RepositoryPerformance{TrackedFiles: 500}, false},
		{"large", repository.RepositoryPerformance{TrackedFiles: 50000}, true},
		{"optimized", repository.RepositoryPerformance{TrackedFiles: 50000,
			Settings: repository.PerformanceSettings{UntrackedCache: true, CommitGraph: true}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := menuTestApp(t)
			reads := make(chan struct{}, 2)
			h.app.session = &performanceTestSession{Session: h.app.session, report: tc.report, reads: reads}
			h.app.path = "/repo/" + tc.name
			h.app.suggestRepositoryPerformance()
			h.app.suggestRepositoryPerformance() // a second open in the same window stays quiet
			// Toasts move from the queue to the window as a frame draws them,
			// so count what the window shows.
			suggested := func() bool {
				count := 0
				for _, text := range h.tester.Texts() {
					if text == "Large repository" {
						count++
					}
				}
				if count > 1 {
					t.Fatalf("suggested %d times", count)
				}
				return count == 1
			}
			if tc.want {
				h.waitFor(t, "suggestion", suggested)
				return
			}
			<-reads
			time.Sleep(50 * time.Millisecond)
			h.updates.flushPending()
			h.tester.Frame()
			if suggested() {
				t.Fatal("suggested performance settings that would not help")
			}
		})
	}
}
