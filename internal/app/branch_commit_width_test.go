package app

import (
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// The selected commit lists hash, author and relative time beside its subject.
// Give this metadata a comfortable reading column instead of forcing wraps at
// 280 DIP, while retaining a real project header above it.
func TestBranchCommitsStartWithComfortableMetadataWidth(t *testing.T) {
	a := newApp(fakeOpener{})
	a.workspaceTab = sectionBranches
	a.state = stateReady
	a.snapshot = repository.Snapshot{
		Root: "/work/project", Branch: "feature",
		Branches: []repository.Branch{{Name: "feature", Head: true}},
	}
	commit := repository.Commit{
		Hash: "5e289c999999999999", ShortHash: "5e289c9",
		Subject:    "Align branch commit details and prevent redundant repaints",
		AuthorName: "FleetingEcho", AuthorEmail: "echo@example.com",
		UnixTimestamp: time.Now().Add(-time.Hour).Unix(),
		LinesAdded:    126, LinesDeleted: 87,
	}
	a.snapshot.Commits = []repository.Commit{commit}
	a.branchCommits = []repository.Commit{commit}
	if a.branchCommitsWidth < 360 {
		t.Fatalf("default commit list width = %.1f, want a comfortable 360 or wider", a.branchCommitsWidth)
	}

	tt := ui.NewTester(a.branchesView, 1200, 760)
	tt.Frame()
	hash, hashOK := tt.Find(commit.ShortHash)
	author, authorOK := tt.Find(commit.AuthorName)
	when := relTime(commit.UnixTimestamp)
	relative, relativeOK := tt.Find(when)
	if !hashOK || !authorOK || !relativeOK {
		t.Fatalf("commit metadata missing: hash=%v author=%v time=%v (%q)", hashOK, authorOK, relativeOK, tt.Texts())
	}
	paneInfo, ok := tt.AccessByRole(ui.RoleList, tr("Commits"))
	if !ok {
		t.Fatal("the selected branch has no accessible commit list")
	}
	// Test against the actual interactive list, not the similarly named
	// heading text box, whose width is unrelated to the pane's viewport.
	listPane := paneInfo.Bounds
	for name, rect := range map[string]ui.Rect{"hash": hash, "author": author, "relative": relative} {
		if math.Abs(float64(rect.X)-float64(listPane.X)) < 0.1 {
			t.Fatalf("%s overlaps the Commits title: %+v and %+v", name, rect, listPane)
		}
		if rect.X+rect.W > listPane.X+listPane.W+1 {
			t.Errorf("%s is clipped beyond the Commits list: %+v; list=%+v", name, rect, listPane)
		}
	}
}
