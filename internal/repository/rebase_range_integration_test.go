package repository

import (
	"context"
	"strings"
	"testing"
)

// The visual rebase entry points deliberately leave range rewriting on the
// already validated EditCommitRange path. Keep a real-Git guard beside the new
// plan integration tests so a nonconsecutive visual selection cannot become a
// different rebase plan silently.
func TestVisualRebaseKeepsConsecutiveRangeValidation(t *testing.T) {
	dir, commits := newRebasePlanRepository(t)
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
	if err != nil {
		t.Fatal(err)
	}
	before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	for _, selected := range [][]Commit{
		{commits[0], commits[2]}, // gap: first is not selected
		{commits[0], commits[0]}, // duplicate row
		{commits[2]},             // not a range
	} {
		if err := session.EditCommitRange(context.Background(), selected, RangeDrop); err == nil {
			t.Fatalf("invalid range %#v was accepted", selected)
		}
		if after := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); after != before {
			t.Fatalf("invalid range changed HEAD from %s to %s", before, after)
		}
	}
}
