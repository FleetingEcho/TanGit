package forge

import (
	"context"
	"slices"
	"testing"
)

func TestGitHubMutationsUseIssueReviewAndApprovalEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		mutation Mutation
		want     []string
	}{
		{name: "comment", mutation: Mutation{Kind: MutationComment, Body: "hello"}, want: []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/42/comments", "--raw-field", "body=hello"}},
		{name: "code reply", mutation: Mutation{Kind: MutationReply, Body: "fixed", Discussion: Discussion{ID: "thread-node", ReplyID: "9", Kind: DiscussionCode}}, want: []string{"api", "--method", "POST", "repos/{owner}/{repo}/pulls/42/comments/9/replies", "--raw-field", "body=fixed"}},
		{name: "resolve", mutation: Mutation{Kind: MutationResolve, Resolved: true, Discussion: Discussion{ID: "thread-node", Kind: DiscussionCode, Resolvable: true}}, want: []string{"api", "graphql", "--raw-field", "query=mutation($threadId:ID!){resolveReviewThread(input:{threadId:$threadId}){thread{id isResolved}}}", "--raw-field", "threadId=thread-node"}},
		{name: "approve", mutation: Mutation{Kind: MutationApprove, HeadSHA: "head"}, want: []string{"api", "--method", "POST", "repos/{owner}/{repo}/pulls/42/reviews", "--raw-field", "event=APPROVE", "--raw-field", "commit_id=head"}},
		{name: "line comment", mutation: Mutation{Kind: MutationLineComment, Body: "nit", Line: LineTarget{Path: "main.go", NewLine: 12, HeadSHA: "head"}}, want: []string{"api", "--method", "POST", "repos/{owner}/{repo}/pulls/42/comments", "--raw-field", "body=nit", "--raw-field", "commit_id=head", "--raw-field", "path=main.go", "--raw-field", "line=12", "--raw-field", "side=RIGHT"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &detailRunner{tool: "gh", responses: map[string]string{"gh " + stringsJoin(test.want): `{}`}}
			err := NewCLIClient(runner).Mutate(context.Background(), ChangeRequest{Provider: GitHub, Number: 42}, test.mutation)
			if err != nil {
				t.Fatal(err)
			}
			if len(runner.calls) != 1 || runner.calls[0] != "gh "+stringsJoin(test.want) {
				t.Fatalf("calls = %q", runner.calls)
			}
		})
	}
}

func TestGitLabMutationsUseDiscussionAndApprovalEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		mutation Mutation
		want     []string
	}{
		{name: "comment", mutation: Mutation{Kind: MutationComment, Body: "hello"}, want: []string{"api", "--method", "POST", "projects/:fullpath/merge_requests/7/notes", "--raw-field", "body=hello"}},
		{name: "reply", mutation: Mutation{Kind: MutationReply, Body: "done", Discussion: Discussion{ID: "thread"}}, want: []string{"api", "--method", "POST", "projects/:fullpath/merge_requests/7/discussions/thread/notes", "--raw-field", "body=done"}},
		{name: "resolve", mutation: Mutation{Kind: MutationResolve, Resolved: true, Discussion: Discussion{ID: "thread", Resolvable: true}}, want: []string{"api", "--method", "PUT", "projects/:fullpath/merge_requests/7/discussions/thread", "--raw-field", "resolved=true"}},
		{name: "approve", mutation: Mutation{Kind: MutationApprove, HeadSHA: "head"}, want: []string{"api", "--method", "POST", "projects/:fullpath/merge_requests/7/approve", "--raw-field", "sha=head"}},
		{name: "line comment", mutation: Mutation{Kind: MutationLineComment, Body: "nit", Line: LineTarget{Path: "new.go", PreviousPath: "old.go", OldLine: 5, NewLine: 6, BaseSHA: "base", StartSHA: "start", HeadSHA: "head"}}, want: []string{"api", "--method", "POST", "projects/:fullpath/merge_requests/7/discussions", "--raw-field", "body=nit", "--raw-field", "position[position_type]=text", "--raw-field", "position[base_sha]=base", "--raw-field", "position[start_sha]=start", "--raw-field", "position[head_sha]=head", "--raw-field", "position[old_path]=old.go", "--raw-field", "position[new_path]=new.go", "--raw-field", "position[old_line]=5", "--raw-field", "position[new_line]=6"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &detailRunner{tool: "glab", responses: map[string]string{"glab " + stringsJoin(test.want): `{}`}}
			err := NewCLIClient(runner).Mutate(context.Background(), ChangeRequest{Provider: GitLab, Number: 7}, test.mutation)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(runner.calls, []string{"glab " + stringsJoin(test.want)}) {
				t.Fatalf("calls = %q", runner.calls)
			}
		})
	}
}

func stringsJoin(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += " "
		}
		result += value
	}
	return result
}
