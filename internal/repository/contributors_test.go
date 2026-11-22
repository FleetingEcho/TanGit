package repository

import (
	"strconv"
	"testing"
	"time"
)

func TestParseContributorsGroupsByEmailAndSortsByCommits(t *testing.T) {
	output := "Alice\x1falice@example.com\x1f300\n" +
		"Bob\x1fbob@example.com\x1f200\n" +
		"alice smith\x1fALICE@example.com\x1f100\n" +
		"Alice S\x1falice@example.com\x1f400\n" +
		"\x1f\x1f0\n" + "garbage\n"
	got := parseContributors(output)
	if len(got) != 2 {
		t.Fatalf("contributors = %+v", got)
	}
	alice, bob := got[0], got[1]
	if alice.Email != "alice@example.com" || alice.Commits != 3 || alice.Name != "Alice S" {
		t.Fatalf("alice = %+v (want 3 commits under her newest name)", alice)
	}
	if alice.FirstCommit != 100 || alice.LastCommit != 400 {
		t.Fatalf("alice span = %d..%d", alice.FirstCommit, alice.LastCommit)
	}
	if bob.Commits != 1 || bob.Name != "Bob" {
		t.Fatalf("bob = %+v", bob)
	}
}

func TestSummarizeContributorsCountsRecentActivityAndWeeks(t *testing.T) {
	now := time.Unix(10_000_000, 0)
	day := int64(24 * 3600)
	output := "Alice\x1falice@example.com\x1f" + strconv.FormatInt(now.Unix()-2*day, 10) + "\n" + // this week
		"Alice\x1falice@example.com\x1f" + strconv.FormatInt(now.Unix()-9*day, 10) + "\n" + // last week
		"Bob\x1fbob@example.com\x1f" + strconv.FormatInt(now.Unix()-100*day, 10) + "\n" + // long ago
		"Bob\x1fbob@example.com\x1f" + strconv.FormatInt(now.Unix()-20*day, 10) + "\n"
	summary := summarizeContributors(output, now)
	if summary.TotalCommits != 4 || len(summary.Contributors) != 2 {
		t.Fatalf("totals = %d commits, %d contributors", summary.TotalCommits, len(summary.Contributors))
	}
	if summary.ActiveRecent != 2 {
		t.Fatalf("active in the last 30 days = %d, want 2", summary.ActiveRecent)
	}
	weeks := summary.WeeklyCommits
	if len(weeks) != ContributorWeeks || weeks[ContributorWeeks-1] != 1 || weeks[ContributorWeeks-2] != 1 || weeks[ContributorWeeks-3] != 1 {
		t.Fatalf("weekly commits = %v", weeks)
	}
	total := 0
	for _, count := range weeks {
		total += count
	}
	if total != 3 {
		t.Fatalf("the commit 100 days ago must fall outside the series: %v", weeks)
	}
}
