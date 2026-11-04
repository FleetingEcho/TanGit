package app

import (
	"testing"

	"lazygitapp/internal/repository"
)

func TestAheadBehindHidesGitPlaceholders(t *testing.T) {
	known := repository.Branch{Ahead: "3", Behind: "2"}
	if ahead, behind, ok := aheadBehind(known); !ok || ahead != "3" || behind != "2" {
		t.Fatalf("known counts = %q/%q/%v", ahead, behind, ok)
	}

	// Git writes "?" for both, together, when it cannot count them because
	// the remote-tracking ref is not local.
	for _, branch := range []repository.Branch{
		{Ahead: "?", Behind: "?"},
		{Ahead: "?", Behind: "0"},
		{Ahead: "", Behind: ""},
	} {
		if _, _, ok := aheadBehind(branch); ok {
			t.Errorf("%+v reported known counts", branch)
		}
	}
}

func TestCountHidesZeroAndPlaceholders(t *testing.T) {
	for _, value := range []string{"", "?", "0"} {
		if shown, ok := count(value); ok || shown != "" {
			t.Errorf("count(%q) = %q/%v, want hidden", value, shown, ok)
		}
	}
	if shown, ok := count("3"); !ok || shown != "3" {
		t.Errorf("count(3) = %q/%v", shown, ok)
	}
}

// The header's ahead and behind counts are arrows, so their tooltips carry
// the words: what the counts mean, and what settles them.
func TestAheadBehindTooltipsNameTheUpstream(t *testing.T) {
	branch := repository.Branch{UpstreamRemote: "origin", UpstreamBranch: "master"}
	if got := aheadTooltip(branch); got != "Commits here are not on origin/master yet. Push to publish them." {
		t.Errorf("ahead tooltip = %q", got)
	}
	if got := behindTooltip(branch); got != "origin/master has commits this branch does not. Pull to bring them in." {
		t.Errorf("behind tooltip = %q", got)
	}
	// A branch whose upstream Git cannot name still explains itself, with no
	// gap left where the name would be.
	if got := aheadTooltip(repository.Branch{}); got != "Commits here are not on the upstream yet. Push to publish them." {
		t.Errorf("no-upstream ahead tooltip = %q", got)
	}
	if got := behindTooltip(repository.Branch{}); got != "The upstream has commits this branch does not. Pull to bring them in." {
		t.Errorf("no-upstream behind tooltip = %q", got)
	}
}

func TestUpstreamUnknownExplainsWhy(t *testing.T) {
	branch := repository.Branch{UpstreamRemote: "origin", UpstreamBranch: "main"}
	if note := upstreamUnknown(branch); note != "Git cannot tell ahead or behind until origin/main is fetched." {
		t.Errorf("note = %q", note)
	}
	branch.UpstreamGone = true
	if note := upstreamUnknown(branch); note != "Upstream origin/main no longer exists." {
		t.Errorf("gone note = %q", note)
	}
	// A branch tracking no remote has nothing to explain.
	if note := upstreamUnknown(repository.Branch{}); note != "" {
		t.Errorf("no-upstream note = %q", note)
	}
}
