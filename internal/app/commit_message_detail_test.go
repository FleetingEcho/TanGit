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
			body := "Release notes for this version.\n\n- Add the native client.\n- Preserve every body line."
			session := &commitMessageDetailSession{read: func(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
				reads.Add(1)
				return repository.CommitMessage{Summary: commit.Subject, Description: body}, nil
			}}
			h, _ := commitMessageDetailHarness(t, test.tab, session)
			h.tester.Frame()
			h.waitFor(t, "the complete commit body", func() bool {
				return h.tester.HasText("Preserve every body line.")
			})
			if !h.tester.HasText(body) {
				t.Fatalf("commit body lost its multiline content: %q", h.texts())
			}
			h.tester.Frame()
			h.tester.Frame()
			if got := reads.Load(); got != 1 {
				t.Fatalf("CommitMessage reads = %d, want one lazy read for an unchanged commit", got)
			}
		})
	}
}

func TestCommitMessageBodyStaysCompactAndExpands(t *testing.T) {
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf("Detailed release note line %02d with enough words to wrap in its pane.", i+1))
	}
	body := strings.Join(lines, "\n\n")
	session := &commitMessageDetailSession{read: func(_ context.Context, commit repository.Commit) (repository.CommitMessage, error) {
		return repository.CommitMessage{Summary: commit.Subject, Description: body}, nil
	}}
	h, _ := commitMessageDetailHarness(t, sectionHistory, session)
	h.tester.Frame()
	h.waitFor(t, "the long commit body", func() bool { return h.tester.HasText(body) })
	compact, ok := h.tester.Find("Commit message")
	if !ok || compact.H <= 0 || compact.H > 144 {
		t.Fatalf("compact commit message height = %+v, want a bounded scroll area of at most 144 DIP", compact)
	}
	if _, ok := h.tester.Find("Commit files"); !ok {
		t.Fatal("the long commit message squeezed out the changed-files pane")
	}
	if err := h.tester.Click("Expand commit message"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	expanded, ok := h.tester.Find("Commit message")
	if !ok || expanded.H <= compact.H {
		t.Fatalf("expanded commit message did not grow: compact=%+v expanded=%+v", compact, expanded)
	}
	if err := h.tester.Click("Collapse commit message"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	restored, ok := h.tester.Find("Commit message")
	if !ok || restored.H > 144 {
		t.Fatalf("collapsed commit message did not restore its compact bound: %+v", restored)
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
