package repository

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type hostingCommandRecord struct {
	program   string
	arguments []string
	directory string
}

func newHostingSession(t *testing.T, url string) (string, Session, *[]hostingCommandRecord) {
	t.Helper()
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "readme.txt", "readme\n", "initial")
	runGit(t, dir, "remote", "add", "origin", url)
	calls := &[]hostingCommandRecord{}
	previous := runLocalHostingCommand
	runLocalHostingCommand = func(_ context.Context, directory, program string, arguments []string) (string, error) {
		*calls = append(*calls, hostingCommandRecord{
			program: program, arguments: append([]string(nil), arguments...), directory: directory,
		})
		return "https://host.example/team/project/pulls/42\n", nil
	}
	t.Cleanup(func() { runLocalHostingCommand = previous })
	return dir, session, calls
}

func TestGitHubPullRequestCommands(t *testing.T) {
	dir, session, calls := newHostingSession(t, "git@github.com:team/project.git")
	ctx := context.Background()

	tool, err := session.PullRequestTool(ctx)
	if err != nil || tool != "gh" {
		t.Fatalf("tool = %q, %v, want gh", tool, err)
	}
	url, err := session.CreatePullRequest(ctx, "Improve history", "Explanation", "topic", "main", true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.Contains(url, "/pulls/42") {
		t.Fatalf("URL = %q", url)
	}
	call := (*calls)[0]
	if call.program != "gh" || call.directory != dir {
		t.Fatalf("command = %+v", call)
	}
	for _, argument := range []string{"pr", "create", "--title", "Improve history", "--body", "Explanation", "--head", "topic", "--base", "main", "--draft"} {
		found := false
		for _, got := range call.arguments {
			if got == argument {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in %v", argument, call.arguments)
		}
	}
	link, err := session.OpenPullRequest(ctx, "topic")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !strings.Contains(link, "/pulls/42") {
		t.Fatalf("resolved URL = %q", link)
	}
	open := (*calls)[1].arguments
	if strings.Join(open, " ") != "pr view topic --json url --jq .url" {
		t.Fatalf("open arguments = %v", open)
	}
	output, err := session.PullRequestChecks(ctx, "topic")
	if err != nil || !strings.Contains(output, "pulls/42") {
		t.Fatalf("checks = %q, %v", output, err)
	}
	checks := (*calls)[2].arguments
	if strings.Join(checks, " ") != "pr checks topic" {
		t.Fatalf("checks arguments = %v", checks)
	}
}

func TestGitLabMergeRequestCommands(t *testing.T) {
	_, session, calls := newHostingSession(t, "https://gitlab.com/team/project.git")
	ctx := context.Background()
	if _, err := session.CreatePullRequest(ctx, "Draft MR", "Body", "topic", "dev", false); err != nil {
		t.Fatal(err)
	}
	arguments := (*calls)[0].arguments
	if (*calls)[0].program != "glab" {
		t.Fatalf("program = %q", (*calls)[0].program)
	}
	for _, argument := range []string{"mr", "create", "--source-branch", "topic", "--target-branch", "dev", "--yes"} {
		found := false
		for _, got := range arguments {
			if got == argument {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in %v", argument, arguments)
		}
	}
	if _, err := session.PullRequestChecks(ctx, "topic"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join((*calls)[1].arguments, " "); got != "ci status --branch topic" {
		t.Fatalf("pipeline arguments = %q", got)
	}
}

func TestUnsupportedHostFailsWithoutExecuting(t *testing.T) {
	_, session, calls := newHostingSession(t, "git@code.example:team/project.git")
	if _, err := session.PullRequestTool(context.Background()); err == nil {
		t.Fatal("expected an unsupported-host error")
	}
	if len(*calls) != 0 {
		t.Fatalf("unexpected execution: %v", *calls)
	}
}

func TestCheckPullRequestReadyUsesAuthStatus(t *testing.T) {
	_, session, calls := newHostingSession(t, "git@github.com:team/project.git")
	if err := session.CheckPullRequestReady(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 {
		t.Fatalf("auth calls = %d, want 1", len(*calls))
	}
	if (*calls)[0].program != "gh" || strings.Join((*calls)[0].arguments, " ") != "auth status" {
		t.Fatalf("auth invocation = %+v", (*calls)[0])
	}
}

func TestHostingFailuresAreActionable(t *testing.T) {
	for _, test := range []struct {
		name    string
		output  string
		wantErr error
	}{
		{name: "missing CLI", output: "gh: command not found", wantErr: ErrPullRequestSetup},
		{name: "no existing request", output: "no pull requests found for branch main", wantErr: ErrPullRequestMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, session, _ := newHostingSession(t, "git@github.com:team/project.git")
			cause := errors.New("exit status 1")
			if test.name == "missing CLI" {
				cause = exec.ErrNotFound
			}
			runLocalHostingCommand = func(context.Context, string, string, []string) (string, error) {
				return test.output, cause
			}
			_, err := session.OpenPullRequest(context.Background(), "main")
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("open error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestAuthFailureRequestsSetup(t *testing.T) {
	_, session, _ := newHostingSession(t, "git@github.com:team/project.git")
	runLocalHostingCommand = func(context.Context, string, string, []string) (string, error) {
		return "not logged in to github.com", errors.New("exit status 1")
	}
	err := session.CheckPullRequestReady(context.Background())
	if !errors.Is(err, ErrPullRequestSetup) {
		t.Fatalf("auth error = %v, want ErrPullRequestSetup", err)
	}
}
