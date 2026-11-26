package repository

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseIndexEntryCountReadsOnlyTheHeader(t *testing.T) {
	header := []byte{'D', 'I', 'R', 'C', 0, 0, 0, 2, 0, 1, 0x86, 0xa0}
	if got := parseIndexEntryCount(bytes.NewReader(header)); got != 100000 {
		t.Fatalf("index entries = %d, want 100000", got)
	}
	if got := parseIndexEntryCount(strings.NewReader("not an index")); got != 0 {
		t.Fatalf("invalid index entries = %d, want 0", got)
	}
}

func performanceTestRepository(t *testing.T) (string, *gitSession) {
	t.Helper()
	// Keep the user's global configuration, and its maintenance list, out of
	// the test in both directions.
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Performance Test")
	runGit(t, dir, "config", "user.email", "performance@example.com")
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "files")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, session.(*gitSession)
}

func TestPerformanceReadsAndTogglesRepositorySettings(t *testing.T) {
	ctx := context.Background()
	dir, session := performanceTestRepository(t)
	report, err := session.Performance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.TrackedFiles != 3 || report.Large() || report.GitVersion == "" {
		t.Fatalf("report = %+v", report)
	}
	if report.Settings != (PerformanceSettings{}) || report.Optimized() {
		t.Fatalf("fresh repository settings = %+v", report.Settings)
	}

	desired := report.Settings
	desired.UntrackedCache, desired.CommitGraph = true, true
	if err := session.ApplyPerformance(ctx, desired); err != nil {
		t.Fatal(err)
	}
	report, err = session.Performance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Settings.UntrackedCache || !report.Settings.CommitGraph {
		t.Fatalf("settings after enabling = %+v", report.Settings)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "objects", "info", "commit-graph")); err != nil {
		t.Fatalf("commit-graph was not written: %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "config", "fetch.writeCommitGraph")); got != "true" {
		t.Fatalf("fetch.writeCommitGraph = %q", got)
	}

	// A global value is overridden locally when the user turns a setting off.
	runGit(t, dir, "config", "--global", "core.untrackedCache", "true")
	if err := session.ApplyPerformance(ctx, PerformanceSettings{}); err != nil {
		t.Fatal(err)
	}
	report, err = session.Performance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Settings.UntrackedCache || report.Settings.CommitGraph {
		t.Fatalf("settings after disabling = %+v", report.Settings)
	}
}

func TestPerformanceRecognizesMaintenanceRegistration(t *testing.T) {
	dir, session := performanceTestRepository(t)
	// Register by hand: git maintenance start would install a system timer.
	runGit(t, dir, "config", "--global", "--add", "maintenance.repo", dir)
	report, err := session.Performance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Settings.Maintenance {
		t.Fatalf("registered repository was not recognized: %+v", report)
	}
}

func TestFSMonitorCookiesDoNotNotifyTheRepositoryWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir, session := performanceTestRepository(t)
	report, err := session.Performance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !report.FSMonitorSupported {
		t.Skip("this Git has no built-in file system monitor")
	}
	desired := report.Settings
	desired.FSMonitor = true
	if err := session.ApplyPerformance(ctx, desired); err != nil {
		t.Fatal(err)
	}
	defer gitBestEffort(dir, "fsmonitor--daemon", "stop")
	runGit(t, dir, "status") // starts the daemon and creates its cookie folder
	events, err := session.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitRepositoryEvent(t, events)
	for range 3 {
		if _, err := session.SnapshotQuiet(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case <-events:
			t.Fatal("a status query through the file system monitor notified the watcher")
		case <-time.After(3 * repositoryEventDelay):
		}
	}
	report, err = session.Performance(ctx)
	if err != nil || !report.Settings.FSMonitor {
		t.Fatalf("fsmonitor setting = %+v, %v", report.Settings, err)
	}
}

// gitBestEffort runs git and ignores its result, for cleanup.
func gitBestEffort(dir string, args ...string) {
	_ = exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
}
