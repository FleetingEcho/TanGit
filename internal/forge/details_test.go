package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type detailRunner struct {
	tool      string
	responses map[string]string
	calls     []string
}

func TestGitHubThreadIdentityEnablesResolveWithoutLosingRESTReplyID(t *testing.T) {
	discussions := []Discussion{{ID: "9", ReplyID: "9", Kind: DiscussionCode}}
	var response githubThreadsResponse
	err := json.Unmarshal([]byte(`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"id":"thread-node","isResolved":false,"isOutdated":true,"viewerCanResolve":true,"comments":{"nodes":[{"fullDatabaseId":"9"}]}}]}}}}}`), &response)
	if err != nil {
		t.Fatal(err)
	}
	applyGitHubThreadIdentity(discussions, response)
	if discussions[0].ID != "thread-node" || discussions[0].ReplyID != "9" || !discussions[0].Resolvable || !discussions[0].Outdated {
		t.Fatalf("discussion = %#v", discussions[0])
	}
}

func (r *detailRunner) PullRequestTool(context.Context) (string, error) { return r.tool, nil }

func (r *detailRunner) RunHostingCommand(_ context.Context, tool string, args []string) (string, error) {
	key := tool + " " + strings.Join(args, " ")
	r.calls = append(r.calls, key)
	response, ok := r.responses[key]
	if !ok {
		return "", fmt.Errorf("unexpected command %s", key)
	}
	return response, nil
}

func TestGitHubDetailNormalizesFilesCommitsAndThreadReplies(t *testing.T) {
	base := "repos/{owner}/{repo}/pulls/42"
	runner := &detailRunner{tool: "gh", responses: map[string]string{
		"gh api " + base:                                              `{"body":"Why this changes","mergeable_state":"clean"}`,
		"gh api " + base + "/files?per_page=100":                      `[{"filename":"new.go","previous_filename":"old.go","status":"renamed","additions":4,"deletions":2,"patch":"@@"}]`,
		"gh api " + base + "/commits?per_page=100":                    `[{"sha":"abcdef","commit":{"message":"Subject\n\nBody","author":{"name":"Ada","date":"2026-11-27T10:00:00Z"}},"author":{"login":"ada"}}]`,
		"gh api repos/{owner}/{repo}/issues/42/comments?per_page=100": `[{"id":1,"body":"general","created_at":"2026-11-27T10:00:00Z","user":{"login":"sam"}}]`,
		"gh api " + base + "/comments?per_page=100":                   `[{"id":2,"body":"line","created_at":"2026-11-27T10:01:00Z","user":{"login":"lee"},"path":"new.go","line":7},{"id":3,"in_reply_to_id":2,"body":"reply","created_at":"2026-11-27T10:02:00Z","user":{"login":"ada"}}]`,
		"gh api " + base + "/reviews?per_page=100":                    `[{"id":4,"body":"looks good","state":"APPROVED","submitted_at":"2026-11-27T10:03:00Z","user":{"login":"pat"}}]`,
	}}
	detail, err := NewCLIClient(runner).Get(context.Background(), ChangeRequest{Provider: GitHub, Number: 42})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Description != "Why this changes" || detail.MergeStatus != "Clean" || len(detail.Files) != 1 || detail.Files[0].PreviousPath != "old.go" {
		t.Fatalf("detail = %#v", detail)
	}
	if len(detail.Commits) != 1 || detail.Commits[0].Title != "Subject" || detail.Commits[0].Author != "ada" {
		t.Fatalf("commits = %#v", detail.Commits)
	}
	if len(detail.Discussions) != 3 || detail.Discussions[1].State != "Approved" || detail.Discussions[2].Path != "new.go" || len(detail.Discussions[2].Replies) != 1 {
		t.Fatalf("discussions = %#v", detail.Discussions)
	}
}

func TestGitLabDetailNormalizesDiffsCommitsAndResolvedDiscussion(t *testing.T) {
	base := "projects/:fullpath/merge_requests/7"
	runner := &detailRunner{tool: "glab", responses: map[string]string{
		"glab api " + base:                               `{"description":"MR body","detailed_merge_status":"not_approved"}`,
		"glab api " + base + "/diffs?per_page=100":       `[{"old_path":"old.go","new_path":"new.go","renamed_file":true,"diff":"--- a/old.go\n+++ b/new.go\n-old\n+new\n+more"}]`,
		"glab api " + base + "/commits?per_page=100":     `[{"id":"123456","title":"Change","author_name":"Lin","authored_date":"2026-11-27T10:00:00Z"}]`,
		"glab api " + base + "/discussions?per_page=100": `[{"id":"thread","notes":[{"id":10,"body":"Please change","resolved":true,"resolvable":true,"created_at":"2026-11-27T10:01:00Z","author":{"username":"reviewer"},"position":{"new_path":"new.go","new_line":12}},{"id":11,"body":"Done","created_at":"2026-11-27T10:02:00Z","author":{"username":"author"}}]}]`,
	}}
	detail, err := NewCLIClient(runner).Get(context.Background(), ChangeRequest{Provider: GitLab, Number: 7})
	if err != nil {
		t.Fatal(err)
	}
	if detail.MergeStatus != "Not approved" || len(detail.Files) != 1 || detail.Files[0].Status != "Renamed" || detail.Files[0].Additions != 2 || detail.Files[0].Deletions != 1 {
		t.Fatalf("detail = %#v", detail)
	}
	if len(detail.Discussions) != 1 || !detail.Discussions[0].Resolved || detail.Discussions[0].Line != 12 || len(detail.Discussions[0].Replies) != 1 {
		t.Fatalf("discussions = %#v", detail.Discussions)
	}
}
