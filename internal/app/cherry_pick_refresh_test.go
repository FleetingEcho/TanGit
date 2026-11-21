package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lazygitapp/internal/repository"
)

// A successful cherry-pick creates a new commit on the checked-out branch.
// The action's refresh must put that new object into the visible, default
// current-branch History; otherwise users reasonably retry the source commit.
func TestCherryPickedCommitAppearsInRefreshedCurrentBranchHistory(t *testing.T) {
	a, tester, updated := realCommandHarness(t)
	root := a.snapshot.Root
	// A tag with the same short name as the checked-out branch must not capture
	// the branch-filtered History query after that branch advances.
	runGitIn(t, root, "tag", "main")
	runGitIn(t, root, "checkout", "-b", "feature")
	// Give the source commit a parent other than the destination HEAD. Without
	// this setup, an immediate cherry-pick can reproduce the very same object
	// (same tree, parent, message and second-resolution timestamps), which is
	// not the cross-history cherry-pick this regression is about.
	if err := os.WriteFile(filepath.Join(root, "setup.txt"), []byte("setup\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "add", "setup.txt")
	runGitIn(t, root, "commit", "-m", "feature setup")
	if err := os.WriteFile(filepath.Join(root, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "add", "feature.txt")
	runGitIn(t, root, "commit", "-m", "feature to cherry-pick")
	sourceHash := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	runGitIn(t, root, "checkout", "main")
	before := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))

	a.setWorkspaceTab(sectionHistory)
	tester.Frame()
	a.confirmCommitActionFor(repository.Commit{
		Hash: sourceHash, ShortHash: sourceHash[:7], Subject: "feature to cherry-pick",
	}, false)
	tester.Frame()
	if a.confirm == nil {
		t.Fatal("cherry-pick did not open its confirmation")
	}
	a.confirm.run()
	for a.actionBusy || a.historyFilter.loading {
		updated.waitForUpdate(t)
		tester.Frame()
	}
	if a.actionError != "" {
		t.Fatalf("first cherry-pick failed: %s", a.actionError)
	}

	pickedHash := strings.TrimSpace(runGitIn(t, root, "rev-parse", "HEAD"))
	if pickedHash == before || pickedHash == sourceHash {
		t.Fatalf("cherry-pick HEAD = %s, before = %s, source = %s", pickedHash, before, sourceHash)
	}
	if a.snapshot.HeadHash != pickedHash {
		t.Fatalf("refreshed HEAD = %s, Git HEAD = %s", a.snapshot.HeadHash, pickedHash)
	}
	commits := a.visibleCommits()
	found := false
	for _, commit := range commits {
		if commit.Hash == pickedHash {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("new cherry-pick %s is absent from visible History: %+v; snapshot: %+v; filter: %+v",
			pickedHash, commits, a.snapshot.Commits, a.historyFilter)
	}
}
