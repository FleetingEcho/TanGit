package app

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

func TestCommitActionButtonsUseDistinctIcons(t *testing.T) {
	a := newApp(fakeOpener{})
	commit := repository.Commit{Hash: strings.Repeat("a", 40), Parents: []string{strings.Repeat("b", 40)}}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Padding(8).Children(func() { a.commitActionIcons(c, commit) })
	}, 500, 48)
	tt.SetDark(true)
	tt.Frame()
	for _, pair := range [][2]string{
		{"Soft reset to this commit", "Mixed reset to this commit"},
		{"Compare commit with HEAD", "Compare parent with commit"},
	} {
		first, firstOK := tt.Find(pair[0])
		second, secondOK := tt.Find(pair[1])
		if !firstOK || !secondOK {
			t.Fatalf("action buttons %q are missing", pair)
		}
		crop := func(r ui.Rect) *image.RGBA {
			bounds := image.Rect(0, 0, int(r.W), int(r.H))
			icon := image.NewRGBA(bounds)
			draw.Draw(icon, bounds, tt.Image(), image.Point{X: int(r.X), Y: int(r.Y)}, draw.Src)
			return icon
		}
		firstIcon, secondIcon := crop(first), crop(second)
		if firstIcon.Bounds().Size() != secondIcon.Bounds().Size() {
			t.Fatalf("action buttons %q have different sizes: %+v %+v", pair, first, second)
		}
		if bytes.Equal(firstIcon.Pix, secondIcon.Pix) {
			t.Errorf("action buttons %q render the same icon", pair)
		}
	}
}

// Choosing another commit keeps the file list and diff on the screen while
// the next ones load instead of replacing them with blank loading panes.
func TestCommitDetailsKeepTheirContentWhileLoading(t *testing.T) {
	session := &controlledCommitDetailPublicationSession{
		started: make(chan string, 1),
		release: make(chan []repository.CommitFile, 1),
	}
	t.Cleanup(func() { session.release <- []repository.CommitFile{{Path: "next.go"}} })
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionHistory
	a.commitFilesHash = "first"
	a.commitFiles = []repository.CommitFile{{Path: "old.go"}}
	a.selectedCommitFile, a.selectedCommitFileRow = 0, 0
	a.commitDiffHash, a.commitDiffPath = "first", "old.go"
	a.commitDiffState = &editor.DiffState{Old: editor.NewBuffer("before\n"), New: editor.NewBuffer("after\n")}
	previous := a.commitDiffState
	next := repository.Commit{Hash: "second", Subject: "Next commit"}
	a.requestCommitDetails(next)
	<-session.started
	if a.commitFiles == nil || a.commitDiffState != previous {
		t.Fatal("choosing another commit cleared the current file list or diff")
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.commitDetailBody(c, &next, "Select a commit", nil, a.historyCommitDetailViewState())
	}, 800, 600)
	tt.Frame()
	if !tt.HasText("old.go") || tt.HasText(tr("Loading commit details…")) {
		t.Fatal("the next commit replaced its existing files with a loading pane")
	}
	if a.commitDiffState != previous {
		t.Fatal("drawing the next commit unloaded the diff already on screen")
	}
	if _, ok := tt.Find(tr("Loading commit details")); !ok {
		t.Fatal("the retained commit files do not show their load in progress")
	}
}

func TestCommitDetailsSplitLeavesSpaceBetweenPanes(t *testing.T) {
	for _, layout := range []int{commitDetailsLeftRight, commitDetailsTopBottom} {
		t.Run(fmt.Sprint(layout), func(t *testing.T) {
			a := newApp(fakeOpener{})
			a.commitDetailsLayout = layout
			a.commitFilesWidth, a.commitFilesHeight = 100, 80
			tt := ui.NewTester(func(c *ui.Context) {
				a.commitDetailsSplit(c,
					func() { ui.Box(c).Fill().Label("First commit details pane").Background(c.Theme().Surface) },
					func() { ui.Box(c).Fill().Label("Second commit details pane").Background(c.Theme().Surface) },
				).Fill()
			}, 240, 200)
			tt.Frame()
			first, firstOK := tt.Find("First commit details pane")
			second, secondOK := tt.Find("Second commit details pane")
			if !firstOK || !secondOK {
				t.Fatalf("split panes are missing: first=%+v second=%+v", first, second)
			}
			gap := second.X - (first.X + first.W)
			if layout == commitDetailsTopBottom {
				gap = second.Y - (first.Y + first.H)
			}
			if gap < commitDetailsPaneGap-1 {
				t.Fatalf("space between commit details panes = %.1f, want at least %.1f: first=%+v second=%+v",
					gap, commitDetailsPaneGap-1, first, second)
			}
		})
	}
}

func TestCommitFilesPanelCollapsesAndRestores(t *testing.T) {
	for _, layout := range []int{commitDetailsLeftRight, commitDetailsTopBottom} {
		t.Run(fmt.Sprint(layout), func(t *testing.T) {
			h, _ := historyHarness(t)
			h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
			h.waitFor(t, "the commit diff", func() bool {
				return h.app.commitDiffState != nil && !h.app.commitDiffLoading
			})
			h.app.commitDetailsLayout = layout
			h.tester.Frame()
			width, height := h.app.commitFilesWidth, h.app.commitFilesHeight
			diff, path, selected := h.app.commitDiffState, h.app.commitDiffPath, h.app.selectedCommitFile
			if layout == commitDetailsLeftRight {
				mode, modeOK := h.tester.Find("Show files as a flat list")
				collapse, collapseOK := h.tester.Find("Collapse changed files")
				grip := gripRect(t, h.tester, false)
				buttonGap := collapse.X - (mode.X + mode.W)
				if !modeOK || !collapseOK || buttonGap < 4 || buttonGap > 9 {
					t.Fatalf("commit file header icon gap = %.1f, want 4–9 DIP: mode=%+v collapse=%+v",
						buttonGap, mode, collapse)
				}
				if grip.X-(collapse.X+collapse.W) < 7 {
					t.Fatalf("collapse button has no trailing margin before the divider: collapse=%+v grip=%+v",
						collapse, grip)
				}
			}

			if err := h.tester.Click("Collapse changed files"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if !h.app.commitFilesCollapsed || h.tester.HasText("Commit files") ||
				!h.tester.HasText("Expand changed files") {
				t.Fatalf("collapsed commit files left the wrong controls: %q", h.texts())
			}
			if h.app.commitDiffState != diff || h.app.commitDiffPath != path || h.app.selectedCommitFile != selected {
				t.Fatal("collapsing commit files changed or unloaded the selected diff")
			}

			if err := h.tester.Click("Expand changed files"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if h.app.commitFilesCollapsed || !h.tester.HasText("Commit files") ||
				h.app.commitFilesWidth != width || h.app.commitFilesHeight != height {
				t.Fatalf("restoring commit files lost its pane state: collapsed=%v width=%v height=%v",
					h.app.commitFilesCollapsed, h.app.commitFilesWidth, h.app.commitFilesHeight)
			}
			if h.app.commitDiffState != diff || h.app.commitDiffPath != path || h.app.selectedCommitFile != selected {
				t.Fatal("restoring commit files changed or unloaded the selected diff")
			}
		})
	}
}

func TestBranchCommitFilesPanelMatchesHistoryAndCollapses(t *testing.T) {
	for _, layout := range []int{commitDetailsLeftRight, commitDetailsTopBottom} {
		t.Run(fmt.Sprint(layout), func(t *testing.T) {
			h := branchPageHarness(t)
			h.waitFor(t, "the branch commit files", func() bool { return len(h.app.branchCommitFiles) > 0 })
			h.waitFor(t, "the branch commit diff", func() bool {
				return h.app.branchCommitDiffState != nil && !h.app.branchCommitDiffLoading
			})
			h.app.commitDetailsLayout = layout
			h.tester.Frame()
			for _, label := range []string{
				"Commit files", "Collapse changed files",
				"Switch layout (Left/Right)", "Switch layout (Top/Bottom)",
			} {
				if _, ok := h.tester.Find(tr(label)); !ok {
					t.Fatalf("branch commit details lack the History control %q: %q", label, h.texts())
				}
			}
			if h.tester.HasText("Changed files") {
				t.Fatal("branch commit details kept their separate legacy file-pane header")
			}

			diff := h.app.branchCommitDiffState
			path, selected := h.app.branchCommitDiffPath, h.app.selectedBranchCommitFile
			if err := h.tester.Click("Collapse changed files"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if h.tester.HasText("Commit files") || !h.tester.HasText("Expand changed files") {
				t.Fatalf("collapsed branch commit files left the wrong controls: %q", h.texts())
			}
			if h.app.branchCommitDiffState != diff || h.app.branchCommitDiffPath != path ||
				h.app.selectedBranchCommitFile != selected {
				t.Fatal("collapsing branch commit files changed or unloaded the selected diff")
			}
			if err := h.tester.Click("Expand changed files"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if !h.tester.HasText("Commit files") || h.app.branchCommitDiffState != diff ||
				h.app.branchCommitDiffPath != path || h.app.selectedBranchCommitFile != selected {
				t.Fatal("restoring branch commit files did not retain its content and selected diff")
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, fmt.Sprintf("branch-commit-details-layout-%d", layout), h.tester)
			}
		})
	}
}

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
				if !branchView {
					leftRight, leftRightOK := tt.Find(tr("Switch layout (Left/Right)"))
					topBottom, topBottomOK := tt.Find(tr("Switch layout (Top/Bottom)"))
					whitespace, whitespaceOK := tt.Find(tr("Toggle whitespace-only diff changes"))
					if !leftRightOK || !topBottomOK || !whitespaceOK ||
						leftRight.X >= whitespace.X || topBottom.X >= whitespace.X {
						t.Fatalf("commit layout control is not at the left of the diff toolbar: left/right=%+v top/bottom=%+v whitespace=%+v",
							leftRight, topBottom, whitespace)
					}
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
				if !branchView {
					subject, _ := tt.Find(commit.Subject)
					expand, _ := tt.Find(tr("Expand diff"))
					if expand.X <= subject.X || expand.Y >= subject.Y+subject.H || expand.Y+expand.H <= subject.Y ||
						float32(width)-(expand.X+expand.W) > 24 {
						t.Fatalf("expand control is not at the commit header's top-right: subject=%+v expand=%+v width=%d",
							subject, expand, width)
					}
				}
				if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
					saveScreenshot(t, dir, fmt.Sprintf("commit-detail-%d-branch-%v", width, branchView), tt)
				}
			})
		}
	}
}
