package app

import "testing"

// A reflog entry names a real commit, so its detail pane must show that
// commit's changed files and diff rather than only an explanatory placeholder.
func TestReflogEntryShowsCommitDetails(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionReflog
	h.tester.Frame()

	h.waitFor(t, "the reflog commit files", func() bool {
		return calls.has("commit-files:c2c2c2c2") && len(h.app.commitFiles) > 0
	})
	if !h.tester.HasText("one.txt") || !h.tester.HasText("two.txt") {
		t.Fatalf("reflog details did not show changed files in %q", h.tester.Texts())
	}
}
