package app

import (
	"context"
	"strings"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/forge"
	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestChangeRequestCountBadgeExplainsPaging(t *testing.T) {
	if tooltip := changeRequestCountTooltip(50, true); !strings.Contains(tooltip, "50") || !strings.Contains(tooltip, "Scroll down to load more") {
		t.Fatalf("paged count tooltip = %q", tooltip)
	}
}

func TestPullRequestCountIsNotShownInWorkspaceSidebar(t *testing.T) {
	a := newApp(fakeOpener{})
	a.changeRequests.items = make([]forge.ChangeRequest, 50)
	if got := a.sectionCount(sectionPullRequests); got != 0 {
		t.Fatalf("PR/MR sidebar count = %d, want none", got)
	}
}

type changeRequestListSession struct {
	fakeSession
	tool   string
	output string
	calls  [][]string
}

func TestPullRequestCommentMutationClearsDraftAndRefreshesDetail(t *testing.T) {
	h := menuTestApp(t)
	session := &changeRequestListSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Branch: "feature"}},
		tool:        "gh",
		output:      `[{"number":42,"title":"Ship it","state":"OPEN","url":"https://example.com/pull/42","author":{"login":"octo"},"headRefName":"feature","baseRefName":"main"}]`,
	}
	h.app.session = session
	h.app.state = stateReady
	h.app.setWorkspaceTab(sectionPullRequests)
	h.waitFor(t, "the pull request detail", func() bool { return h.app.changeRequests.detail != nil })

	h.app.changeRequests.commentDraft = "hello review"
	h.app.beginChangeRequestMutation(forge.Mutation{Kind: forge.MutationComment, Body: h.app.changeRequests.commentDraft})
	h.waitFor(t, "the comment mutation", func() bool {
		return !h.app.changeRequests.mutationBusy && h.app.changeRequests.commentDraft == ""
	})

	wantEndpoint := "repos/{owner}/{repo}/issues/42/comments"
	found := false
	for _, args := range session.calls {
		if len(args) == 6 && args[0] == "api" && args[1] == "--method" && args[2] == "POST" && args[3] == wantEndpoint && args[5] == "body=hello review" {
			found = true
		}
	}
	if !found {
		t.Fatalf("comment command missing from calls %q", session.calls)
	}
}

func TestParseReviewPatchTracksOldAndNewCoordinates(t *testing.T) {
	lines := parseReviewPatch("@@ -10,3 +20,4 @@ heading\n context\n-old\n+new\n+more\n tail\n")
	if len(lines) != 6 {
		t.Fatalf("lines = %#v", lines)
	}
	wants := [][2]int{{0, 0}, {10, 20}, {11, 0}, {0, 21}, {0, 22}, {12, 23}}
	for i, want := range wants {
		if lines[i].oldLine != want[0] || lines[i].newLine != want[1] {
			t.Errorf("line %d coordinates = %d/%d, want %d/%d", i, lines[i].oldLine, lines[i].newLine, want[0], want[1])
		}
	}
}

func (s *changeRequestListSession) PullRequestTool(context.Context) (string, error) {
	return s.tool, nil
}

func (s *changeRequestListSession) RunHostingCommand(_ context.Context, _ string, args []string) (string, error) {
	s.calls = append(s.calls, append([]string(nil), args...))
	if len(args) > 1 && args[0] == "api" {
		if args[1] == "repos/{owner}/{repo}/pulls/42" {
			return `{"body":"Details","mergeable_state":"clean"}`, nil
		}
		return `[]`, nil
	}
	return s.output, nil
}

func TestPullRequestsSectionLoadsGitHubAndPublishesSelection(t *testing.T) {
	h := menuTestApp(t)
	session := &changeRequestListSession{
		fakeSession: fakeSession{snapshot: repository.Snapshot{Branch: "feature"}},
		tool:        "gh",
		output:      `[{"number":42,"title":"Ship it","state":"OPEN","url":"https://example.com/pull/42","author":{"login":"octo"},"headRefName":"feature","baseRefName":"main"}]`,
	}
	h.app.session = session
	h.app.state = stateReady
	h.app.setWorkspaceTab(sectionPullRequests)
	h.waitFor(t, "the pull request list", func() bool { return h.app.changeRequests.loaded })
	h.waitFor(t, "the pull request detail", func() bool { return h.app.changeRequests.detail != nil })

	if h.app.changeRequests.err != "" || len(h.app.changeRequests.items) != 1 {
		t.Fatalf("change requests = %#v, error %q", h.app.changeRequests.items, h.app.changeRequests.err)
	}
	if h.app.changeRequests.selected != 0 || h.app.changeRequests.items[0].Number != 42 {
		t.Fatalf("selection = %d, requests %#v", h.app.changeRequests.selected, h.app.changeRequests.items)
	}
	if len(session.calls) == 0 || len(session.calls[0]) < 2 || session.calls[0][0] != "pr" || session.calls[0][1] != "list" {
		t.Fatalf("hosting calls = %q", session.calls)
	}
	if h.app.changeRequests.detail.Description != "Details" {
		t.Fatalf("detail = %#v", h.app.changeRequests.detail)
	}
}
