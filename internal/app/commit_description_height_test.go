package app

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func commitDescriptionTestHarness(t *testing.T, tab int, description string) (*harness, *repository.Commit) {
	t.Helper()
	session := &commitMessageDetailSession{read: func(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
		return repository.CommitMessage{Summary: commit.Subject, Description: description}, nil
	}}
	h, selected := commitMessageDetailHarness(t, tab, session)
	h.waitFor(t, "the loaded commit description", func() bool { return h.tester.HasText("Description") })
	for range 4 {
		h.tester.Frame()
	}
	return h, selected
}

func tabName(tab int) string {
	if tab == sectionBranches {
		return "branches"
	}
	return "history"
}

func TestCommitDescriptionShortBodyIsFullyVisible(t *testing.T) {
	for _, tab := range []int{sectionHistory, sectionBranches} {
		t.Run(tabName(tab), func(t *testing.T) {
			h, _ := commitDescriptionTestHarness(t, tab, "First line.\nSecond line.\nThird line.\nFourth line.")
			message := &h.app.commitDetailMessage
			if tab == sectionBranches {
				message = &h.app.branchCommitDetailMessage
			}
			if !h.tester.HasText("Fourth line") || message.scroll.MaxY != 0 {
				t.Fatalf("short Description is clipped or scrollable: height=%v maxY=%v texts=%q",
					message.height, message.scroll.MaxY, h.texts())
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, "commit-description-short-"+tabName(tab), h.tester)
			}
		})
	}
}

func TestCommitDescriptionLongBodyExpandsToBoundedScroll(t *testing.T) {
	const last = "The final paragraph of the long release notes."
	body := strings.Repeat("A detailed release note with enough words to wrap in the commit pane.\n\n", 48) + last
	h, _ := commitDescriptionTestHarness(t, sectionHistory, body)
	message := &h.app.commitDetailMessage
	if message.height <= commitMessageDefaultHeight || message.height > commitDescriptionMaxAutoHeight {
		t.Fatalf("long Description height = %v, want above the default within the auto cap", message.height)
	}
	if len(message.heightByCommit) != 0 {
		t.Fatalf("auto-sizing was incorrectly recorded as a manual override: %v", message.heightByCommit)
	}
	if message.scroll.MaxY <= 0 {
		t.Fatalf("long Description is not internally scrollable: height=%v maxY=%v", message.height, message.scroll.MaxY)
	}
	message.scroll.Y = 0
	h.tester.Frame()
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "commit-description-long", h.tester)
	}
	message.scroll.Y = message.scroll.MaxY
	h.tester.Frame()
	if !h.tester.HasText(last) {
		t.Fatalf("long Description cannot scroll to its last paragraph: %q", h.texts())
	}
}

func TestCommitDescriptionNarrowWindowRecomputesHeight(t *testing.T) {
	body := strings.Repeat("Several words make a paragraph wrap within the current pane width. ", 9)
	h, _ := commitDescriptionTestHarness(t, sectionHistory, body)
	message := &h.app.commitDetailMessage
	before := message.height
	if len(message.heightByCommit) != 0 {
		t.Fatalf("untouched Description unexpectedly has a manual height: %v", message.heightByCommit)
	}

	h.tester.SetSize(360, 700)
	for range 5 {
		h.tester.Frame()
	}
	if message.height <= before {
		t.Fatalf("narrow-window text wrapping did not increase message height: before=%v after=%v", before, message.height)
	}
	if message.scroll.MaxY < 0 {
		t.Fatalf("narrow text has an invalid scroll range: %v", message.scroll.MaxY)
	}
	if len(message.heightByCommit) != 0 {
		t.Fatalf("window resize was mistaken for a manual divider gesture: %v", message.heightByCommit)
	}
}

func TestCommitDescriptionRawModeAlsoUsesBoundedScrolling(t *testing.T) {
	h, _ := commitDescriptionTestHarness(t, sectionHistory, strings.Repeat("Raw release content line.\n", 60))
	message := &h.app.commitDetailMessage
	message.raw = true
	for range 5 {
		h.tester.Frame()
	}
	if message.height <= commitMessageDefaultHeight || message.height > commitDescriptionMaxAutoHeight {
		t.Fatalf("raw Description has no useful automatic height: %v", message.height)
	}
	if message.scroll.MaxY <= 0 {
		t.Fatalf("raw Description is not scrollable after automatic sizing: height=%v maxY=%v", message.height, message.scroll.MaxY)
	}
}

func TestCommitDescriptionManualHeightOverridesAutoAndRestoresPerCommit(t *testing.T) {
	body := strings.Repeat("A paragraph with several words that naturally takes more vertical space.\n\n", 12)
	for _, tab := range []int{sectionHistory, sectionBranches} {
		t.Run(tabName(tab), func(t *testing.T) {
			h, selected := commitDescriptionTestHarness(t, tab, body)
			message := &h.app.commitDetailMessage
			if tab == sectionBranches {
				message = &h.app.branchCommitDetailMessage
			}
			header, found := h.tester.Find("Commit message")
			if !found {
				t.Fatal("commit message viewport is missing")
			}
			before := message.height
			dragVerticalPaneGrip(t, h.tester, ui.Rect{X: header.X, W: header.W}, header.Y+header.H, 90)
			chosen := message.height
			if chosen <= before {
				t.Fatalf("drag did not increase the Description height: before=%v after=%v", before, chosen)
			}
			commitHash := selected.Hash
			if message.heightByCommit[commitHash] != chosen {
				t.Fatalf("manual height was not recorded by commit: hash=%q overrides=%v", commitHash, message.heightByCommit)
			}

			for range 3 {
				h.tester.Frame()
			}
			if message.height != chosen {
				t.Fatalf("refresh changed the user's Description height: chosen=%v now=%v", chosen, message.height)
			}
			message.raw = true
			h.tester.Frame()
			h.tester.Frame()
			if message.height != chosen {
				t.Fatalf("raw/Markdown toggle overrode a manual height: chosen=%v now=%v", chosen, message.height)
			}

			selected.Hash = "bbbbbbbb"
			h.waitFor(t, "the different commit to adapt its own default height", func() bool {
				return message.hash == selected.Hash && message.message.Description != ""
			})
			for range 3 {
				h.tester.Frame()
			}
			selected.Hash = commitHash
			h.waitFor(t, "returning to the manually sized commit", func() bool {
				return message.hash == selected.Hash && message.message.Description != ""
			})
			for range 3 {
				h.tester.Frame()
			}
			if message.height != chosen {
				t.Fatalf("returning to commit %s did not restore height %v, got %v",
					commitHash, chosen, message.height)
			}
		})
	}
}
