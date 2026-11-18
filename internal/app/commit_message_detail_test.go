package app

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type commitMessageDetailSession struct {
	fakeSession
	read func(context.Context, repository.Commit) (repository.CommitMessage, error)
}

func (s *commitMessageDetailSession) CommitMessage(ctx context.Context, commit repository.Commit) (repository.CommitMessage, error) {
	return s.read(ctx, commit)
}

func commitMessageDetailHarness(t *testing.T, tab int, session repository.Session) (*harness, *repository.Commit) {
	t.Helper()
	a := newApp(fakeOpener{})
	a.workspaceTab = tab
	a.session = session
	h := &harness{app: a, updates: newUpdates()}
	a.update = h.updates.update
	selected := repository.Commit{
		Hash: "aaaaaaaa", ShortHash: "aaaaaaa", Subject: "Release the desktop client",
		AuthorName: "Ada", AuthorEmail: "ada@example.com",
	}
	h.tester = ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() {
			if tab == sectionBranches {
				a.commitDetailBody(c, &selected, "Select a commit", nil, a.branchCommitDetailViewState())
				return
			}
			a.commitDetailBody(c, &selected, "Select a commit", nil, a.historyCommitDetailViewState())
		})
	}, 640, 700)
	return h, &selected
}

func awaitCommitMessageSignal(t *testing.T, signals <-chan string, want string) {
	t.Helper()
	select {
	case got := <-signals:
		if got != want {
			t.Fatalf("commit message signal = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for commit message signal %q", want)
	}
}

func TestCommitDetailsLoadAndRetainEveryBodyLine(t *testing.T) {
	for _, test := range []struct {
		name string
		tab  int
	}{
		{name: "history", tab: sectionHistory},
		{name: "graph", tab: sectionGraph},
		{name: "branches", tab: sectionBranches},
		{name: "reflog", tab: sectionReflog},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reads atomic.Int32
			body := "## Release notes\n\n- Add the **native** client.\n- Preserve every body line."
			session := &commitMessageDetailSession{read: func(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
				reads.Add(1)
				return repository.CommitMessage{Summary: commit.Subject, Description: body}, nil
			}}
			h, _ := commitMessageDetailHarness(t, test.tab, session)
			h.tester.Frame()
			h.waitFor(t, "the complete commit body", func() bool {
				return h.tester.HasText("Preserve every body line.")
			})
			if !h.tester.HasText("Release notes") || !h.tester.HasText("Add the native client.") ||
				h.tester.HasText("## Release notes") || h.tester.HasText("**native**") {
				t.Fatalf("commit body is not rendered as Markdown: %q", h.texts())
			}
			h.tester.Frame()
			h.tester.Frame()
			if got := reads.Load(); got != 1 {
				t.Fatalf("CommitMessage reads = %d, want one lazy read for an unchanged commit", got)
			}
		})
	}
}

func TestCommitMessageRenderedAndRawModesAllowSelectionAndCopy(t *testing.T) {
	body := "Add the **native** client.\n\nSee the release notes."
	session := &commitMessageDetailSession{read: func(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
		return repository.CommitMessage{Summary: commit.Subject, Description: body}, nil
	}}
	h, _ := commitMessageDetailHarness(t, sectionHistory, session)
	h.tester.Frame()
	h.waitFor(t, "the rendered commit body", func() bool {
		return h.tester.HasText("Add the native client.")
	})
	if h.tester.HasText("Add the **native** client.") {
		t.Fatalf("default commit body exposes raw Markdown: %q", h.texts())
	}
	copyCommitMessageLine(t, h, "Add the native client.", "native")

	if err := h.tester.Click("Show raw commit message"); err != nil {
		t.Fatalf("switch to raw Markdown: %v", err)
	}
	h.tester.Frame()
	if !h.tester.HasText(body) {
		t.Fatalf("raw mode does not expose the original Markdown source: %q", h.texts())
	}
	h.tester.SetClipboard("")
	copyCommitMessageLine(t, h, "Commit message", "**native**")

	if err := h.tester.Click("Show rendered commit message"); err != nil {
		t.Fatalf("switch back to rendered Markdown: %v", err)
	}
	h.tester.Frame()
	if !h.tester.HasText("Add the native client.") || h.tester.HasText("Add the **native** client.") {
		t.Fatalf("rendered mode did not return: %q", h.texts())
	}
}

func copyCommitMessageLine(t *testing.T, h *harness, label, want string) {
	t.Helper()
	paragraph, ok := h.tester.Find(label)
	if !ok || paragraph.W < 8 || paragraph.H <= 0 {
		t.Fatalf("cannot select commit message %q: %+v", label, paragraph)
	}
	y := paragraph.Y + paragraph.H/2
	if label == "Commit message" {
		// Raw mode is one multiline text element rather than separate
		// paragraph labels. Start on its first source line.
		y = paragraph.Y + 7
	}
	h.tester.Press(paragraph.X+2, y)
	h.tester.Move(paragraph.X+paragraph.W-2, y)
	h.tester.Frame()
	h.tester.Release(paragraph.X+paragraph.W-2, y)
	h.tester.Frame()
	h.tester.Key(ui.Ctrl, ui.KeyC)
	h.tester.Frame()
	if copied := h.tester.Clipboard(); !strings.Contains(copied, want) {
		t.Fatalf("selecting and copying %q produced %q, want it to contain %q", label, copied, want)
	}
}

func TestCommitMessageMiddleDividerResizesAndScrollsBody(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("Detailed release note line %02d with enough words to wrap in its pane.", i+1))
	}
	body := strings.Join(lines, "\n\n")
	for _, tab := range []int{sectionHistory, sectionBranches} {
		t.Run(fmt.Sprintf("section_%d", tab), func(t *testing.T) {
			session := &commitMessageDetailSession{read: func(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
				return repository.CommitMessage{Summary: commit.Subject, Description: body}, nil
			}}
			h, _ := commitMessageDetailHarness(t, tab, session)
			messageState := &h.app.commitDetailMessage
			if tab == sectionBranches {
				messageState = &h.app.branchCommitDetailMessage
			}
			h.tester.Frame()
			h.waitFor(t, "the long commit body", func() bool { return h.tester.HasText(lines[len(lines)-1]) })
			h.tester.Frame()
			before, ok := h.tester.Find("Commit message")
			if !ok || before.H <= 0 {
				t.Fatalf("missing visible commit body: %+v", before)
			}
			for _, label := range []string{"Expand diff", "Expand commit message", "Collapse commit message"} {
				if button, exists := h.tester.Find(label); exists {
					t.Errorf("details maximize control %q is still present at %+v", label, button)
				}
			}
			if _, ok := h.tester.Find("Commit files"); !ok {
				t.Fatal("the commit message displaced the changed-files pane")
			}

			pane := ui.Rect{X: before.X, W: before.W}
			dragVerticalPaneGrip(t, h.tester, pane, before.Y+before.H, 80)
			grown, grownOK := h.tester.Find("Commit message")
			if !grownOK || grown.H <= before.H {
				t.Fatalf("dragging the middle divider did not enlarge the body: before=%+v after=%+v", before, grown)
			}
			if _, ok := h.tester.Find("Commit files"); !ok {
				t.Fatal("enlarging the commit body displaced the changed-files pane")
			}

			// Even with the divider dragged to its lower limit, a very long
			// body scrolls inside Details until its final paragraph is visible.
			dragVerticalPaneGrip(t, h.tester, pane, grown.Y+grown.H, 10000)
			fullyGrown, grownOK := h.tester.Find("Commit message")
			if !grownOK || messageState.scroll.MaxY <= 0 {
				t.Fatalf("the oversized body has no internal scrolling: body=%+v maxY=%v", fullyGrown, messageState.scroll.MaxY)
			}
			h.tester.Scroll(fullyGrown.X+fullyGrown.W/2, fullyGrown.Y+fullyGrown.H/2, 0, 100000)
			h.tester.Frame()
			h.tester.Frame()
			if messageState.scroll.Y <= 0 {
				t.Fatalf("scrolling inside Details did not move the body: Y=%v maxY=%v", messageState.scroll.Y, messageState.scroll.MaxY)
			}
			last, lastOK := h.tester.Access(lines[len(lines)-1])
			if !lastOK || last.Offscreen {
				t.Fatalf("the final body paragraph is still hidden after internal scrolling: %+v", last)
			}

			dragVerticalPaneGrip(t, h.tester, pane, fullyGrown.Y+fullyGrown.H, -40)
			shrunk, shrunkOK := h.tester.Find("Commit message")
			if !shrunkOK || shrunk.H >= fullyGrown.H {
				t.Fatalf("dragging the middle divider back did not shrink the body: grown=%+v shrunk=%+v", fullyGrown, shrunk)
			}
		})
	}
}

func TestCommitMessageSelectionCancelsAndRejectsStaleReads(t *testing.T) {
	started := make(chan string, 2)
	cancelled := make(chan string, 1)
	completed := make(chan string, 2)
	responses := map[string]chan repository.CommitMessage{
		"aaaaaaaa": make(chan repository.CommitMessage),
		"bbbbbbbb": make(chan repository.CommitMessage),
	}
	session := &commitMessageDetailSession{read: func(ctx context.Context, commit repository.Commit) (repository.CommitMessage, error) {
		started <- commit.Hash
		response := responses[commit.Hash]
		select {
		case message := <-response:
			completed <- commit.Hash
			return message, nil
		case <-ctx.Done():
			cancelled <- commit.Hash
		}
		message := <-response
		completed <- commit.Hash
		return message, nil
	}}
	h, selected := commitMessageDetailHarness(t, sectionHistory, session)
	h.tester.Frame()
	awaitCommitMessageSignal(t, started, "aaaaaaaa")

	*selected = repository.Commit{Hash: "bbbbbbbb", ShortHash: "bbbbbbb", Subject: "Newer selection"}
	h.tester.Frame()
	awaitCommitMessageSignal(t, started, "bbbbbbbb")
	awaitCommitMessageSignal(t, cancelled, "aaaaaaaa")
	responses["bbbbbbbb"] <- repository.CommitMessage{Summary: selected.Subject, Description: "Body of the newer selection."}
	awaitCommitMessageSignal(t, completed, "bbbbbbbb")
	h.waitFor(t, "the selected commit's body", func() bool {
		return h.tester.HasText("Body of the newer selection.")
	})

	// This backend deliberately completes after cancellation. The request token
	// must still reject its stale result instead of replacing the visible body.
	responses["aaaaaaaa"] <- repository.CommitMessage{Summary: "Old selection", Description: "Stale body from the old selection."}
	awaitCommitMessageSignal(t, completed, "aaaaaaaa")
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if h.tester.HasText("Stale body from the old selection.") || !h.tester.HasText("Body of the newer selection.") {
		t.Fatalf("a cancelled read replaced the selected commit's body: %q", h.texts())
	}
}

func TestCommitDetailOmitsAnEmptyMessageBody(t *testing.T) {
	session := &commitMessageDetailSession{read: func(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
		return repository.CommitMessage{Summary: commit.Subject}, nil
	}}
	h, _ := commitMessageDetailHarness(t, sectionHistory, session)
	h.tester.Frame()
	h.waitFor(t, "the empty message read", func() bool { return len(h.app.commitFiles) > 0 })
	h.tester.Frame()
	if h.tester.HasText("Commit message") || h.tester.HasText("Loading the full commit message") {
		t.Fatalf("an empty commit body left a message section behind: %q", h.texts())
	}
}
