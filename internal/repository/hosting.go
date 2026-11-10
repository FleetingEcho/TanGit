package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// PullRequestProvider identifies which hosting CLI can act on a repository.
type PullRequestProvider string

const (
	PullRequestGitHub PullRequestProvider = "github"
	PullRequestGitLab PullRequestProvider = "gitlab"
)

// ErrPullRequestSetup requires installing or authenticating a hosting CLI.
var ErrPullRequestSetup = errors.New("pull request hosting setup is incomplete")

// ErrPullRequestMissing reports that no request exists yet. This is not an
// authentication error and should offer PR creation rather than setup help.
var ErrPullRequestMissing = errors.New("no pull request exists for this branch")

// runLocalHostingCommand is a seam for tests: host CLIs must never be invoked
// by the unit suite. Remote sessions go through their own connected client.
var runLocalHostingCommand = func(ctx context.Context, directory, program string, arguments []string) (string, error) {
	cmd := exec.CommandContext(ctx, program, arguments...)
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// PullRequestProvider finds a supported host from the configured remote URLs.
// It deliberately considers transport URLs, not the visible web-URL aliases.
func (s *gitSession) pullRequestProvider() PullRequestProvider {
	preferred := ""
	if branch, err := s.currentBranch(); err == nil {
		preferred = branch.PushRemote
		if preferred == "" {
			preferred = branch.UpstreamRemote
		}
	}
	for _, remote := range s.git.Loaders.RemoteLoader.GetRemotes() {
		if preferred != "" && remote.Name != preferred {
			continue
		}
		provider := pullRequestProviderForURLs(append(append([]string{}, remote.Urls...), remote.PushUrls...))
		if preferred != "" || provider != "" {
			return provider
		}
	}
	return ""
}

func pullRequestProviderForURLs(urls []string) PullRequestProvider {
	for _, url := range urls {
		lower := strings.ToLower(url)
		if strings.Contains(lower, "github.com") {
			return PullRequestGitHub
		}
		if strings.Contains(lower, "gitlab.com") {
			return PullRequestGitLab
		}
	}
	return ""
}

func pullRequestTool(provider PullRequestProvider) string {
	switch provider {
	case PullRequestGitHub:
		return "gh"
	case PullRequestGitLab:
		return "glab"
	}
	return ""
}

// PullRequestTool reports which supported CLI can serve this repository, or
// an actionable error describing why hosting actions are unavailable.
func (s *gitSession) PullRequestTool(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	provider := s.pullRequestProvider()
	if tool := pullRequestTool(provider); tool != "" {
		return tool, nil
	}
	return "", errors.New("pull request actions support GitHub (gh) and GitLab (glab) repositories; configure one of those remotes")
}

// runHosting executes gh or glab in the repository without prefixing it with
// Git. Output and failure are recorded in the command log just like a Git
// action, so users can diagnose authentication, missing CLIs and API errors.
func (s *gitSession) runHosting(ctx context.Context, program string, arguments []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if program == "" {
		return "", errors.New("hosting command is required")
	}
	var output string
	var err error
	complete := s.commandLog.Begin(program + " " + strings.Join(arguments, " "))
	if s.sshClient != nil {
		result, runErr := s.sshClient.Run(ctx, s.git.RepoPaths.WorktreePath(), program, arguments...)
		output = string(result.Output)
		err = runErr
	} else {
		output, err = runLocalHostingCommand(ctx, s.git.RepoPaths.WorktreePath(), program, arguments)
	}
	complete(output, err != nil)
	if err != nil {
		// A missing CLI and a missing PR are not the same problem. Preserve
		// actionable classifications instead of turning both into a generic
		// red command-log entry.
		details := strings.ToLower(output + " " + err.Error())
		if errors.Is(err, exec.ErrNotFound) ||
			strings.Contains(details, "executable file not found") ||
			strings.Contains(details, "command not found") ||
			strings.Contains(details, ": "+strings.ToLower(program)+": not found") {
			return output, fmt.Errorf("%w: install %s, then authenticate it with '%s auth login'", ErrPullRequestSetup, program, program)
		}
		if strings.Contains(details, "no pull requests found") || strings.Contains(details, "no merge requests found") {
			return output, ErrPullRequestMissing
		}
		return output, fmt.Errorf("%s: %w%s", program, err, commandOutputSummary(output))
	}
	return output, nil
}

// CheckPullRequestReady verifies the hosting CLI exists and is authenticated
// before a workflow opens a form or starts a check read. It is called only
// after an explicit action, never while drawing the UI.
func (s *gitSession) CheckPullRequestReady(ctx context.Context) error {
	tool, err := s.PullRequestTool(ctx)
	if err != nil {
		return err
	}
	_, err = s.runHosting(ctx, tool, []string{"auth", "status"})
	if err == nil || errors.Is(err, ErrPullRequestSetup) {
		return err
	}
	return fmt.Errorf("%w: authenticate %s with '%s auth login', then retry. %s",
		ErrPullRequestSetup, tool, tool, commandOutputSummary(safeHostingErrorText(err)))
}

func safeHostingErrorText(err error) string {
	if err == nil {
		return ""
	}
	return RedactSecrets(err.Error())
}

// CreatePullRequest creates a GitHub pull request or GitLab merge request.
// It does not open a browser unless the caller explicitly asks to view the PR
// afterwards; the returned output normally includes its URL.
func (s *gitSession) CreatePullRequest(ctx context.Context, title, body, head, base string, draft bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	title = strings.TrimSpace(title)
	head = strings.TrimSpace(head)
	base = strings.TrimSpace(base)
	if title == "" || head == "" {
		return "", errors.New("a pull request title and head branch are required")
	}
	tool, err := s.PullRequestTool(ctx)
	if err != nil {
		return "", err
	}
	var arguments []string
	switch tool {
	case "gh":
		arguments = []string{"pr", "create", "--title", title, "--body", body, "--head", head}
		if base != "" {
			arguments = append(arguments, "--base", base)
		}
		if draft {
			arguments = append(arguments, "--draft")
		}
	case "glab":
		// --yes prevents an interactive TTY confirmation in the native app.
		arguments = []string{"mr", "create", "--title", title, "--description", body, "--source-branch", head, "--yes"}
		if base != "" {
			arguments = append(arguments, "--target-branch", base)
		}
		if draft {
			arguments = append(arguments, "--draft")
		}
	}
	output, err := s.runHosting(ctx, tool, arguments)
	if err != nil {
		return output, err
	}
	return strings.TrimSpace(output), nil
}

// OpenPullRequest resolves the existing request's URL without launching a
// browser. The caller opens it on the desktop, which is essential for SSH
// workspaces where this CLI executes on the remote host.
func (s *gitSession) OpenPullRequest(ctx context.Context, branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", errors.New("a branch is required to view its pull request")
	}
	tool, err := s.PullRequestTool(ctx)
	if err != nil {
		return "", err
	}
	var arguments []string
	switch tool {
	case "gh":
		arguments = []string{"pr", "view", branch, "--json", "url", "--jq", ".url"}
	case "glab":
		arguments = []string{"mr", "view", branch, "--jq", ".web_url"}
	}
	output, err := s.runHosting(ctx, tool, arguments)
	if err != nil {
		return "", err
	}
	link := pullRequestLink(output)
	if link == "" {
		return "", fmt.Errorf("could not find a pull request URL for %s", branch)
	}
	return link, nil
}

// pullRequestLink extracts a validated browser URL from CLI output. It does
// not allow credentials or non-HTTP schemes to reach the desktop opener.
func pullRequestLink(output string) string {
	for _, field := range strings.Fields(output) {
		field = strings.Trim(field, `"'<>();,`)
		candidate, err := url.ParseRequestURI(field)
		if err != nil || (candidate.Scheme != "https" && candidate.Scheme != "http") ||
			candidate.Host == "" || candidate.User != nil {
			continue
		}
		return field
	}
	return ""
}

// PullRequestChecks returns the CLI's check/pipeline report for the branch.
// gh exits non-zero when checks fail even though its report is useful; the
// report is returned alongside any error for this reason.
func (s *gitSession) PullRequestChecks(ctx context.Context, branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", errors.New("a branch is required to read its checks")
	}
	tool, err := s.PullRequestTool(ctx)
	if err != nil {
		return "", err
	}
	var arguments []string
	switch tool {
	case "gh":
		arguments = []string{"pr", "checks", branch}
	case "glab":
		arguments = []string{"ci", "status", "--branch", branch}
	}
	output, err := s.runHosting(ctx, tool, arguments)
	output = strings.TrimSpace(output)
	if output == "" && err == nil {
		output = "No checks or pipeline activity was reported for " + branch + "."
	}
	return output, err
}
