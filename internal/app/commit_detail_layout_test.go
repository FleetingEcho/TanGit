package app

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestCommitDetailToolbarsFitNarrowPanes(t *testing.T) {
	for _, width := range []int{260, 320, 480} {
		for _, branchView := range []bool{false, true} {
			t.Run(fmt.Sprintf("width_%d_branch_%v", width, branchView), func(t *testing.T) {
				a := newApp(fakeOpener{})
				commit := repository.Commit{
					Hash: strings.Repeat("1234abcd", 5), ShortHash: "1234abc",
					Subject:    "chore: release 2.5.0 with a long description that should not push the actions aside",
					AuthorName: "FleetingEcho", AuthorEmail: "a-long-email-address@example.com",
					UnixTimestamp: time.Now().Add(-7 * 24 * time.Hour).Unix(),
				}
				a.branchCommits, a.selectedBranchCommit = []repository.Commit{commit}, 0
				tt := ui.NewTester(func(c *ui.Context) {
					ui.Column(c).Fill().Background(c.Theme().Background).Children(func() {
						if branchView {
							a.branchCommitDetailView(c, repository.Branch{Head: true})
						} else {
							a.commitDetailCard(c, &commit, "Select a commit", func(c *ui.Context, commit repository.Commit) {
								a.commitActionIcons(c, commit)
							})
						}
					})
				}, width, 500)
				tt.SetDark(true)
				tt.Frame()
				labels := []string{"Toggle whitespace-only diff changes", "Change diff context lines",
					"Toggle diff layout", "Toggle diff line wrapping", "Save diff", "Expand diff"}
				var top, bottom float32
				for i, label := range labels {
					r, ok := tt.Find(tr(label))
					if !ok {
						t.Fatalf("missing %q", label)
					}
					if r.X < 0 || r.X+r.W > float32(width)+1 {
						t.Fatalf("%q overflows its pane: %+v", label, r)
					}
					if i == 0 {
						top = r.Y
					}
					bottom = max(bottom, r.Y+r.H)
				}
				if bottom-top > 60 {
					t.Fatalf("diff controls form a vertical stack %.1f points tall", bottom-top)
				}
				for _, label := range []string{commit.Subject, commit.Hash, "Copy commit hash", "Compare commit with HEAD"} {
					r, ok := tt.Find(label)
					if !ok || r.X < 0 || r.X+r.W > float32(width)+1 {
						t.Fatalf("commit header %q is missing or overflows: %+v", label, r)
					}
				}
				hash, _ := tt.Find(commit.Hash)
				copyHash, _ := tt.Find("Copy commit hash")
				if copyHash.X < hash.X+hash.W || copyHash.X-hash.X-hash.W > 5 ||
					copyHash.Y >= hash.Y+hash.H || copyHash.Y+copyHash.H <= hash.Y {
					t.Fatalf("copy control is not immediately after the hash on its row: hash=%+v copy=%+v", hash, copyHash)
				}
				if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
					saveScreenshot(t, dir, fmt.Sprintf("commit-detail-%d-branch-%v", width, branchView), tt)
				}
			})
		}
	}
}
