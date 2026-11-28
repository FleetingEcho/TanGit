package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
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
	if extra, ok := ctx.Value(hostingEnvironmentKey{}).([]string); ok && len(extra) > 0 {
		cmd.Env = hostingCommandEnvironment(os.Environ(), extra)
	}
	output, err := cmd.CombinedOutput()
	return string(output), err
}

type hostingEnvironmentKey struct{}

// hostingCredentialVariables are removed from the inherited environment when
// the user supplies credentials in the app, so an old shell token or a forced
// host cannot silently override the project they chose.
var hostingCredentialVariables = []string{
	"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN", "GITLAB_HOST", "GITLAB_URI", "GL_HOST", "GITLAB_REPO",
	"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST", "GH_REPO",
}

func hostingCommandEnvironment(base, extra []string) []string {
	blocked := make(map[string]bool, len(hostingCredentialVariables))
	for _, name := range hostingCredentialVariables {
		blocked[name] = true
	}
	result := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if !blocked[name] {
			result = append(result, entry)
		}
	}
	return append(result, extra...)
}

// hostingSecretValues returns the token values in env, for redaction.
func hostingSecretValues(env []string) []string {
	var secrets []string
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		if value != "" && strings.HasSuffix(name, "TOKEN") {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func redactHostingSecrets(text string, secrets []string) string {
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, "[redacted]")
	}
	return text
}

// SetHostingEnvironment installs credentials (KEY=VALUE) for the hosting CLI.
// They apply to local repositories only: an SSH workspace runs the CLI on the
// remote machine and keeps using that machine's own authentication.
func (s *gitSession) SetHostingEnvironment(provider PullRequestProvider, env []string) {
	s.hostingHintsMu.Lock()
	s.hostingProvider = provider
	s.hostingEnv = append([]string(nil), env...)
	s.hostingHintsMu.Unlock()
}

// CheckHostingAccess verifies credentials before they are saved by asking the
// provider who the token belongs to. It runs the CLI locally.
func CheckHostingAccess(ctx context.Context, provider PullRequestProvider, env []string) (string, error) {
	tool := pullRequestTool(provider)
	if tool == "" {
		return "", errors.New("unsupported hosting provider")
	}
	secrets := hostingSecretValues(env)
	ctx = context.WithValue(ctx, hostingEnvironmentKey{}, append([]string(nil), env...))
	output, err := runLocalHostingCommand(ctx, "", tool, []string{"api", "user"})
	output = redactHostingSecrets(strings.TrimSpace(output), secrets)
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("%w: install %s first", ErrPullRequestSetup, tool)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %s", tool, redactHostingSecrets(RedactSecrets(firstUsefulLine(output, err.Error())), secrets))
	}
	var identity struct {
		Username string `json:"username"`
		Login    string `json:"login"`
	}
	if json.Unmarshal([]byte(output), &identity) != nil {
		return "", fmt.Errorf("%s: unexpected response from the server", tool)
	}
	if identity.Username != "" {
		return identity.Username, nil
	}
	return identity.Login, nil
}

// firstUsefulLine skips the CLI's bare "ERROR" banner and blank lines.
func firstUsefulLine(texts ...string) string {
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.EqualFold(line, "error") {
				return line
			}
		}
	}
	return "request failed"
}

// PullRequestProvider finds a supported host from the configured remote URLs.
// It deliberately considers transport URLs, not the visible web-URL aliases.
func (s *gitSession) pullRequestProvider() PullRequestProvider {
	if provider := s.pullRequestProviderFromRemotes(); provider != "" {
		return provider
	}
	// A host the user connected with a token is a stated fact about its provider.
	s.hostingHintsMu.Lock()
	connected := s.hostingProvider
	s.hostingHintsMu.Unlock()
	if connected != "" {
		return connected
	}
	// A company host such as git.corp.example names neither provider. The web
	// address the user set for the remote does, when it is a gitlab or github host.
	s.hostingHintsMu.Lock()
	hints := append([]string(nil), s.hostingHints...)
	s.hostingHintsMu.Unlock()
	return pullRequestProviderForHosts(hints)
}

// SetHostingHints gives the session the remotes' web addresses, so a custom
// host the user marked as GitLab or GitHub gets the matching CLI.
func (s *gitSession) SetHostingHints(addresses []string) {
	s.hostingHintsMu.Lock()
	s.hostingHints = append([]string(nil), addresses...)
	s.hostingHintsMu.Unlock()
}

func (s *gitSession) pullRequestProviderFromRemotes() PullRequestProvider {
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

// pullRequestProviderForHosts reads the provider from the host names of web
// addresses: only the host counts, never a path that happens to say "github".
func pullRequestProviderForHosts(addresses []string) PullRequestProvider {
	for _, address := range addresses {
		parsed, err := url.Parse(strings.TrimSpace(address))
		if err != nil {
			continue
		}
		if provider := pullRequestProviderForHost(parsed.Hostname()); provider != "" {
			return provider
		}
	}
	return ""
}

func pullRequestProviderForURLs(urls []string) PullRequestProvider {
	for _, remoteURL := range urls {
		if provider := pullRequestProviderForHost(remoteURLHost(remoteURL)); provider != "" {
			return provider
		}
	}
	return ""
}

func pullRequestProviderForHost(host string) PullRequestProvider {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case strings.Contains(host, "github"):
		return PullRequestGitHub
	case strings.Contains(host, "gitlab"):
		return PullRequestGitLab
	default:
		return ""
	}
}

// remoteURLHost extracts only the network host, so repository paths cannot
// accidentally select a provider. It accepts ordinary URLs and Git's common
// scp-like [user@]host:path syntax.
func remoteURLHost(remoteURL string) string {
	remoteURL = strings.TrimSpace(remoteURL)
	if parsed, err := url.Parse(remoteURL); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	at := strings.LastIndex(remoteURL, "@")
	if at < 0 {
		return ""
	}
	host, path, ok := strings.Cut(remoteURL[at+1:], ":")
	if !ok || host == "" || path == "" || strings.ContainsAny(host, "/\\") {
		return ""
	}
	return host
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
	var secrets []string
	if s.sshClient == nil {
		s.hostingHintsMu.Lock()
		env := append([]string(nil), s.hostingEnv...)
		s.hostingHintsMu.Unlock()
		if len(env) > 0 {
			ctx = context.WithValue(ctx, hostingEnvironmentKey{}, env)
			secrets = hostingSecretValues(env)
		}
	}
	if s.sshClient != nil {
		result, runErr := s.sshClient.Run(ctx, s.git.RepoPaths.WorktreePath(), program, arguments...)
		output = string(result.Output)
		err = runErr
	} else {
		output, err = runLocalHostingCommand(ctx, s.git.RepoPaths.WorktreePath(), program, arguments)
	}
	output = redactHostingSecrets(output, secrets)
	if err != nil && len(secrets) > 0 {
		if message := err.Error(); redactHostingSecrets(message, secrets) != message {
			err = errors.New(redactHostingSecrets(message, secrets))
		}
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

// RunHostingCommand is the transport seam used by the provider-neutral forge
// module. It retains this session's local/SSH execution, command logging,
// cancellation and sanitized error classification without exposing gitSession.
func (s *gitSession) RunHostingCommand(ctx context.Context, program string, arguments []string) (string, error) {
	tool, err := s.PullRequestTool(ctx)
	if err != nil {
		return "", err
	}
	if program != tool {
		return "", fmt.Errorf("hosting command %q does not match repository provider", program)
	}
	return s.runHosting(ctx, program, arguments)
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
