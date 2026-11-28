package forge

import (
	"context"
	"slices"
	"testing"
)

type fakeRunner struct {
	tool string
	out  string
	args []string
}

func (f *fakeRunner) PullRequestTool(context.Context) (string, error) { return f.tool, nil }
func (f *fakeRunner) RunHostingCommand(_ context.Context, _ string, args []string) (string, error) {
	f.args = slices.Clone(args)
	return f.out, nil
}

func TestCLIClientListsGitHubPullRequests(t *testing.T) {
	runner := &fakeRunner{tool: "gh", out: `[{"number":42,"title":"Ship it","state":"OPEN","isDraft":true,"url":"https://github.example/pull/42","author":{"login":"octo"},"headRefName":"feature","baseRefName":"main","updatedAt":"2026-11-27T10:20:30Z","reviewDecision":"REVIEW_REQUIRED","labels":[{"name":"ui"}]}]`}
	got, err := NewCLIClient(runner).List(context.Background(), 25)
	if err != nil || len(got) != 1 {
		t.Fatalf("List() = %#v, %v", got, err)
	}
	request := got[0]
	if request.Provider != GitHub || request.Number != 42 || request.State != StateOpen || !request.Draft || request.Author != "octo" || request.ReviewDecision != "Review required" || !slices.Equal(request.Labels, []string{"ui"}) {
		t.Fatalf("request = %#v", request)
	}
	if !slices.Contains(runner.args, "25") {
		t.Fatalf("args = %q", runner.args)
	}
}

func TestCLIClientListsGitLabMergeRequestsAcrossFieldStyles(t *testing.T) {
	runner := &fakeRunner{tool: "glab", out: `[{"iid":7,"title":"Draft: refactor","state":"merged","draft":true,"web_url":"https://gitlab.example/-/merge_requests/7","author":{"username":"fox"},"source_branch":"topic","target_branch":"trunk","updated_at":"2026-11-27T10:20:30.123Z","detailed_merge_status":"not_approved","labels":["backend"]}]`}
	got, err := NewCLIClient(runner).List(context.Background(), 100)
	if err != nil || len(got) != 1 {
		t.Fatalf("List() = %#v, %v", got, err)
	}
	request := got[0]
	if request.Provider != GitLab || request.Number != 7 || request.State != StateMerged || !request.Draft || request.Author != "fox" || request.SourceBranch != "topic" || request.ReviewDecision != "Not approved" || !slices.Equal(request.Labels, []string{"backend"}) {
		t.Fatalf("request = %#v", request)
	}
}

func TestProviderURLsCannotCarryCredentialsOrUnsafeSchemes(t *testing.T) {
	got, err := parseGitHub([]byte(`[
		{"number":1,"url":"https://token@example.com/pull/1"},
		{"number":2,"url":"file:///tmp/request"},
		{"number":3,"url":"https://example.com/pull/3"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].URL != "" || got[1].URL != "" || got[2].URL != "https://example.com/pull/3" {
		t.Fatalf("sanitized URLs = %q, %q, %q", got[0].URL, got[1].URL, got[2].URL)
	}
}

func TestParseGitHubForkNamesTheSourceRepository(t *testing.T) {
	data := `[
	 {"number":42,"title":"fork","state":"OPEN","url":"https://github.example.com/group/project/pull/42","headRefName":"patch-1","baseRefName":"master","isCrossRepository":true,"headRepository":{"name":"project"},"headRepositoryOwner":{"login":"alice"},"author":{"login":"alice"}},
	 {"number":43,"title":"same repo","state":"OPEN","url":"https://github.example.com/group/project/pull/43","headRefName":"topic","baseRefName":"master","isCrossRepository":false,"headRepository":{"name":"project"},"headRepositoryOwner":{"login":"group"},"author":{"login":"alice"}},
	 {"number":44,"title":"deleted fork","state":"CLOSED","url":"https://github.example.com/group/project/pull/44","headRefName":"gone","baseRefName":"master","isCrossRepository":true,"headRepository":null,"headRepositoryOwner":null,"author":{"login":"bob"}}
	]`
	items, err := parseGitHub([]byte(data))
	if err != nil || len(items) != 3 {
		t.Fatalf("parse = %v, %v", items, err)
	}
	if !items[0].CrossRepo || items[0].SourceRepoURL != "https://github.example.com/alice/project" {
		t.Fatalf("fork = %+v", items[0])
	}
	if items[1].CrossRepo || items[1].SourceRepoURL != "" {
		t.Fatalf("same-repo request = %+v", items[1])
	}
	if !items[2].CrossRepo || items[2].SourceRepoURL != "" {
		t.Fatalf("deleted fork must stay unknown, not become the base repository: %+v", items[2])
	}
}

func TestParseGitLabMarksCrossProjectRequests(t *testing.T) {
	data := `[{"iid":7,"title":"a","state":"opened","web_url":"https://gitlab.example.com/group/project/-/merge_requests/7","source_branch":"x","target_branch":"main","source_project_id":9,"target_project_id":3},
	{"iid":8,"title":"b","state":"opened","web_url":"https://gitlab.example.com/group/project/-/merge_requests/8","source_branch":"y","target_branch":"main","source_project_id":3,"target_project_id":3}]`
	items, err := parseGitLab([]byte(data))
	if err != nil || len(items) != 2 || !items[0].CrossRepo || items[1].CrossRepo {
		t.Fatalf("parse = %+v, %v", items, err)
	}
}
