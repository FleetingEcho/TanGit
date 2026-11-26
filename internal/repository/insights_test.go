package repository

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParseCodeVolumeReadsShortstatLines(t *testing.T) {
	output := "\x1e300\n\n 3 files changed, 10 insertions(+), 2 deletions(-)\n" +
		"\x1e200\n\n 1 file changed, 5 insertions(+)\n" +
		"\x1e100\n\n 2 files changed, 7 deletions(-)\n" +
		"\x1e50\n"
	volume := parseCodeVolume(output)
	want := []CommitVolume{{300, 10, 2, 3}, {200, 5, 0, 1}, {100, 0, 7, 2}, {50, 0, 0, 0}}
	if len(volume.Commits) != len(want) {
		t.Fatalf("commits = %+v", volume.Commits)
	}
	for index, commit := range volume.Commits {
		if commit != want[index] {
			t.Errorf("commit %d = %+v, want %+v", index, commit, want[index])
		}
	}
}

func TestParseBranchHealthSkipsRemoteHeadsAndReadsDistance(t *testing.T) {
	output := "refs/heads/main\x1fmain\x1f300\x1f*\x1f0 0\n" +
		"refs/heads/old\x1fold\x1f100\x1f \x1f2 40\n" +
		"refs/remotes/origin/HEAD\x1forigin\x1f300\x1f \x1f0 0\n" +
		"refs/remotes/origin/feature\x1forigin/feature\x1f250\x1f \x1f3 1\n"
	got := parseBranchHealth(output, map[string]bool{"refs/heads/old": true}, true)
	if len(got) != 3 {
		t.Fatalf("branches = %+v", got)
	}
	if got[0].Name != "main" || !got[0].Current || got[1].Name != "origin/feature" || !got[1].Remote || got[1].Ahead != 3 || got[1].Behind != 1 {
		t.Fatalf("order or distance wrong: %+v", got)
	}
	if old := got[2]; old.Name != "old" || !old.Merged || old.Behind != 40 {
		t.Fatalf("old = %+v", old)
	}
}

func TestCodeVolumeLimitReturnsNewestAcrossPeriodWindows(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	base := time.Now().Add(-4 * time.Hour).Truncate(time.Second)
	times := make([]int64, 8)
	for index := range times {
		timeIndex := index
		times[timeIndex] = base.Add(time.Duration(index+1) * 30 * time.Minute).Unix()
		cmd := exec.Command("git", "-C", dir, "commit", "--allow-empty", "-m", "windowed work")
		date := time.Unix(times[timeIndex], 0).UTC().Format(time.RFC3339)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("create dated commit: %v: %s", err, output)
		}
	}
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	enhanced, ok := session.(interface {
		CodeVolume(context.Context, string, int64, int) (CodeVolume, error)
	})
	if !ok {
		t.Fatal("session has no code-volume reader")
	}
	for _, test := range []struct {
		limit   int
		limited bool
	}{{limit: 4, limited: true}, {limit: 5, limited: true}, {limit: 8}} {
		volume, err := enhanced.CodeVolume(context.Background(), "", base.Unix(), test.limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(volume.Commits) != test.limit || volume.Limited != test.limited {
			t.Fatalf("CodeVolume(limit %d) = %d commits, limited %v", test.limit, len(volume.Commits), volume.Limited)
		}
		for index, commit := range volume.Commits {
			want := times[len(times)-1-index]
			if commit.At != want {
				t.Fatalf("CodeVolume(limit %d) commit %d time = %d, want newest %d", test.limit, index, commit.At, want)
			}
		}
	}
}

// The real Git commands answer in the format the parsers read.
func TestCodeVolumeAndBranchHealthAgainstARealRepository(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "a.txt", "one\ntwo\nthree\n", "root")
	runGit(t, dir, "checkout", "-b", "merged-feature")
	writeHistoryCommit(t, dir, "b.txt", "b1\nb2\n", "feature")
	runGit(t, dir, "checkout", "main")
	runGit(t, dir, "merge", "--no-ff", "merged-feature", "-m", "merge feature")
	runGit(t, dir, "checkout", "-b", "open-work")
	writeHistoryCommit(t, dir, "c.txt", "c\n", "open work")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "edit")

	ctx := context.Background()
	insights, ok := session.(interface {
		CodeVolume(context.Context, string, int64, int) (CodeVolume, error)
		BranchHealth(context.Context) (BranchHealth, error)
	})
	if !ok {
		t.Fatal("the session lacks the insight reads")
	}
	volume, err := insights.CodeVolume(ctx, "", time.Now().Add(-time.Hour).Unix(), 100)
	if err != nil {
		t.Fatal(err)
	}
	added, deleted := 0, 0
	for _, commit := range volume.Commits {
		added += commit.Added
		deleted += commit.Deleted
	}
	// root 3 + feature 2 + open work 1 + edit 2 added; edit deleted 1 line.
	if len(volume.Commits) != 4 || added != 8 || deleted != 1 {
		t.Fatalf("volume = %+v (added %d, deleted %d)", volume.Commits, added, deleted)
	}
	onlyMain, err := insights.CodeVolume(ctx, "main", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(onlyMain.Commits) != 3 {
		t.Fatalf("main alone should have root, feature and edit: %+v", onlyMain.Commits)
	}

	health, err := insights.BranchHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if health.Default != "main" {
		t.Fatalf("default branch = %q", health.Default)
	}
	byName := map[string]BranchHealthEntry{}
	for _, branch := range health.Branches {
		byName[branch.Name] = branch
	}
	if !byName["main"].Current || !byName["merged-feature"].Merged || byName["open-work"].Merged {
		t.Fatalf("merged or current wrong: %+v", byName)
	}
	if byName["open-work"].Ahead != 1 && byName["open-work"].Ahead != -1 {
		t.Fatalf("open-work ahead = %d", byName["open-work"].Ahead)
	}
}
