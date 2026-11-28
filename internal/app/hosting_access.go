package app

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/sshworkspace"
)

const (
	hostingProviderGitLab = "gitlab"
	hostingProviderGitHub = "github"
)

// hostingAccount is the non-secret part of a connected hosting server. The
// token is stored separately under hostingSecretRef.
type hostingAccount struct {
	Provider string `json:"provider"`
	// URL is the server root, such as https://gitlab.example.com.
	URL string `json:"url"`
	// Project optionally pins a project path (group/project) for every
	// repository on this server. Empty lets the CLI use each repository's remote.
	Project string `json:"project,omitempty"`
	// Aliases are other host names this server's repositories use in their git
	// remotes, such as git.example.com for gitlab.example.com.
	Aliases []string `json:"aliases,omitempty"`
	// IssuesURL is the project issue-list page opened by the PR/MR toolbar.
	IssuesURL string `json:"issuesURL,omitempty"`
	// IssueLink turns a bare #1234 in a request's text into a link. {id} stands
	// for the number; a link without it gets the number appended.
	IssueLink string `json:"issueLink,omitempty"`
}

type hostingAccessUIState struct {
	open      bool
	provider  string
	url       string
	project   string
	alias     string
	issuesURL string
	issueLink string
	// remoteURL is the open repository's web URL. It seeds a provider-specific
	// issue link for a new account without replacing a value already saved.
	remoteURL string
	// remoteHost is the host of the open repository's remote, offered as the
	// alias when it differs from the server address.
	remoteHost string
	token      string
	hasToken   bool
	showToken  bool
	busy       bool
	seq        uint64
	notice     string
	err        string
}

func normalizeHostingURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.IndexFunc(raw, unicode.IsControl) >= 0 || strings.ContainsAny(raw, " \t") {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(parsed.Host), true
}

func normalizeHostingProject(project string) (string, bool) {
	project = strings.Trim(strings.TrimSpace(project), "/")
	if strings.IndexFunc(project, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return "", false
	}
	return project, true
}

func normalizeHostingAccounts(accounts []hostingAccount) []hostingAccount {
	var result []hostingAccount
	seen := map[string]bool{}
	for _, account := range accounts {
		account.Provider = strings.ToLower(strings.TrimSpace(account.Provider))
		if account.Provider != hostingProviderGitLab && account.Provider != hostingProviderGitHub {
			continue
		}
		serverURL, ok := normalizeHostingURL(account.URL)
		project, projectOK := normalizeHostingProject(account.Project)
		key := account.Provider + "|" + serverURL
		if !ok || !projectOK || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, hostingAccount{
			Provider: account.Provider, URL: serverURL, Project: project,
			Aliases:   normalizeHostingAliases(account.Aliases),
			IssuesURL: strings.TrimSpace(account.IssuesURL), IssueLink: strings.TrimSpace(account.IssueLink),
		})
	}
	return result
}

// normalizeHostingAliases keeps bare, lowercase, unique host names.
func normalizeHostingAliases(aliases []string) []string {
	var result []string
	for _, alias := range aliases {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if i := strings.Index(alias, "://"); i >= 0 {
			alias = alias[i+3:]
		}
		alias = strings.TrimSuffix(strings.SplitN(alias, "/", 2)[0], ":")
		if at := strings.LastIndex(alias, "@"); at >= 0 {
			alias = alias[at+1:]
		}
		if host, _, ok := strings.Cut(alias, ":"); ok {
			alias = host
		}
		if alias != "" && !strings.ContainsAny(alias, " \t") && !slices.Contains(result, alias) {
			result = append(result, alias)
		}
	}
	return result
}

func hostingProviderName(provider string) string {
	if provider == hostingProviderGitHub {
		return "GitHub"
	}
	return "GitLab"
}

func hostingHostname(serverURL string) string {
	parsed, err := url.Parse(serverURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// defaultHostingIssuesURL builds the provider's ordinary project issue-list
// URL from a remote's web address.
func defaultHostingIssuesURL(provider, projectURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(projectURL))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	project := strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
	if project == "" {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = "/" + project
	if provider == hostingProviderGitHub {
		parsed.Path += "/issues"
	} else {
		parsed.Path += "/-/issues"
	}
	return parsed.String()
}

// defaultHostingIssueLink is separate from the issue-list page: it is the
// template used only for bare issue numbers inside request descriptions.
func defaultHostingIssueLink(provider, projectURL string) string {
	issuesURL := defaultHostingIssuesURL(provider, projectURL)
	if issuesURL == "" {
		return ""
	}
	return strings.TrimRight(issuesURL, "/") + "/{id}"
}

// hostingSecretRef reuses the profile keychain and its session-only fallback.
func hostingSecretRef(account hostingAccount) sshworkspace.SecretRef {
	return sshworkspace.SecretRef("hosting/" + account.Provider + "/" + hostingHostname(account.URL))
}

func (a *app) hostingAccountIndex(provider, serverURL string) int {
	for i, account := range a.settings.HostingAccounts {
		if account.Provider == provider && account.URL == serverURL {
			return i
		}
	}
	return -1
}

// hostingEnvironment builds the credential variables the hosting CLI reads.
//
// project is the repository's project path. glab rejects GITLAB_HOST when no
// git remote has exactly that hostname (an SSH alias, for instance), so a
// known project is passed as GITLAB_REPO instead, which has no such check.
func hostingEnvironment(account hostingAccount, token, project string) []string {
	if token == "" {
		return nil
	}
	host := hostingHostname(account.URL)
	switch account.Provider {
	case hostingProviderGitHub:
		env := []string{}
		if host == "github.com" {
			env = append(env, "GH_TOKEN="+token)
		} else {
			env = append(env, "GH_HOST="+host, "GH_ENTERPRISE_TOKEN="+token)
		}
		if project != "" {
			env = append(env, "GH_REPO="+project)
		}
		return env
	default:
		env := []string{"GITLAB_TOKEN=" + token}
		if project != "" {
			env = append(env, "GITLAB_REPO="+account.URL+"/"+project)
		} else {
			env = append(env, "GITLAB_HOST="+host)
		}
		return env
	}
}

// applyHostingAccess gives the session the saved credentials of the server
// this repository's remotes point to. It runs on the UI goroutine.
func (a *app) applyHostingAccess(session repository.Session) {
	setter, ok := session.(interface {
		SetHostingEnvironment(repository.PullRequestProvider, []string)
	})
	if !ok {
		return
	}
	// hosts maps each remote's web host to that remote's project path.
	hosts := map[string]string{}
	for _, remote := range a.snapshot.Remotes {
		web := a.remoteWebAddress(remote)
		host := hostingHostname(web)
		if host == "" {
			continue
		}
		if _, seen := hosts[host]; !seen {
			path := ""
			if parsed, err := url.Parse(web); err == nil {
				path = strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
			}
			hosts[host] = path
		}
	}
	var env []string
	provider := repository.PullRequestProvider("")
	for _, account := range a.settings.HostingAccounts {
		remoteProject, matches := hosts[hostingHostname(account.URL)]
		for _, alias := range account.Aliases {
			if !matches {
				remoteProject, matches = hosts[alias]
			}
		}
		if !matches {
			continue
		}
		project := account.Project
		if project == "" {
			project = remoteProject
		}
		token, found, err := a.sshSecrets().Get(hostingSecretRef(account))
		if err == nil && found {
			env = hostingEnvironment(account, token, project)
			provider = repository.PullRequestProvider(account.Provider)
			break
		}
	}
	setter.SetHostingEnvironment(provider, env)
}

func (a *app) openHostingAccess() {
	state := hostingAccessUIState{open: true, provider: hostingProviderGitLab}
	if runner, ok := a.session.(forgeCommandSession); ok {
		if tool, err := runner.PullRequestTool(context.Background()); err == nil && tool == "gh" {
			state.provider = hostingProviderGitHub
		}
	}
	remotes := a.snapshot.Remotes
	if a.session == nil {
		remotes = nil // on the home page no repository's remotes apply
	}
	for _, remote := range remotes {
		webURL := a.remoteWebAddress(remote)
		if serverURL, ok := normalizeHostingURL(webURL); ok {
			state.url = serverURL
			state.remoteHost = hostingHostname(serverURL)
			state.remoteURL = webURL
			break
		}
	}
	if state.url == "" && len(a.settings.HostingAccounts) > 0 {
		// Without a repository's remote to go by, start from the first saved
		// server so its address and token show up.
		account := a.settings.HostingAccounts[0]
		state.provider, state.url = account.Provider, account.URL
	}
	a.hostingAccess = state
	a.loadHostingAccessDraft()
}

// loadHostingAccessDraft fills the form from a saved account for the chosen
// provider and server, if there is one.
func (a *app) loadHostingAccessDraft() {
	state := &a.hostingAccess
	state.project, state.alias = "", ""
	state.issuesURL = defaultHostingIssuesURL(state.provider, state.remoteURL)
	state.issueLink = defaultHostingIssueLink(state.provider, state.remoteURL)
	state.token, state.hasToken = "", false
	serverURL, ok := normalizeHostingURL(state.url)
	if !ok {
		return
	}
	if state.remoteHost != "" && state.remoteHost != hostingHostname(serverURL) {
		state.alias = state.remoteHost
	}
	if index := a.hostingAccountIndex(state.provider, serverURL); index >= 0 {
		account := a.settings.HostingAccounts[index]
		state.project = account.Project
		state.issuesURL = account.IssuesURL
		state.issueLink = account.IssueLink
		if len(account.Aliases) > 0 {
			state.alias = strings.Join(account.Aliases, ", ")
		}
		// The saved token fills the field, hidden until the eye is pressed.
		state.token, state.hasToken, _ = a.sshSecrets().Get(hostingSecretRef(account))
	}
}

func (a *app) closeHostingAccess() {
	a.hostingAccess.seq++
	a.hostingAccess = hostingAccessUIState{seq: a.hostingAccess.seq}
}

func (a *app) hostingDraft() (hostingAccount, error) {
	serverURL, ok := normalizeHostingURL(a.hostingAccess.url)
	if !ok {
		return hostingAccount{}, errors.New(tr("Enter the server address, such as https://gitlab.example.com."))
	}
	project, ok := normalizeHostingProject(a.hostingAccess.project)
	if !ok {
		return hostingAccount{}, errors.New(tr("The project path cannot contain spaces."))
	}
	return hostingAccount{
		Provider: a.hostingAccess.provider, URL: serverURL, Project: project,
		Aliases:   normalizeHostingAliases(strings.Split(a.hostingAccess.alias, ",")),
		IssuesURL: strings.TrimSpace(a.hostingAccess.issuesURL),
		IssueLink: strings.TrimSpace(a.hostingAccess.issueLink),
	}, nil
}

// hostingDraftToken is the typed token, or the saved one when the field is empty.
func (a *app) hostingDraftToken(account hostingAccount) string {
	if token := strings.TrimSpace(a.hostingAccess.token); token != "" {
		return token
	}
	token, _, _ := a.sshSecrets().Get(hostingSecretRef(account))
	return token
}

func (a *app) saveHostingAccess() {
	state := &a.hostingAccess
	account, err := a.hostingDraft()
	if err != nil {
		state.err, state.notice = err.Error(), ""
		return
	}
	token := strings.TrimSpace(state.token)
	if token == "" && !state.hasToken {
		state.err, state.notice = tr("Enter an access token."), ""
		return
	}
	if index := a.hostingAccountIndex(account.Provider, account.URL); index >= 0 {
		a.settings.HostingAccounts[index] = account
	} else {
		a.settings.HostingAccounts = append(a.settings.HostingAccounts, account)
	}
	persistent := true
	if token != "" {
		persistent = a.sshSecrets().Set(hostingSecretRef(account), token) == nil
	}
	a.saveSettings()
	a.closeHostingAccess()
	a.reloadChangeRequestsAfterAccessChange()
	if !persistent {
		a.notify("warning", "Token saved for this session only", tr("The system keychain is unavailable, so the token will be forgotten when the app closes."))
	}
}

func (a *app) removeHostingAccess() {
	account, err := a.hostingDraft()
	if err != nil {
		a.hostingAccess.err = err.Error()
		return
	}
	if index := a.hostingAccountIndex(account.Provider, account.URL); index >= 0 {
		list := a.settings.HostingAccounts
		a.settings.HostingAccounts = append(append([]hostingAccount(nil), list[:index]...), list[index+1:]...)
	}
	_ = a.sshSecrets().Delete(hostingSecretRef(account))
	a.saveSettings()
	a.closeHostingAccess()
	a.reloadChangeRequestsAfterAccessChange()
}

func (a *app) reloadChangeRequestsAfterAccessChange() {
	if a.session != nil {
		a.applyHostingAccess(a.session)
	}
	if a.changeRequests.loading {
		return
	}
	a.changeRequests.loaded = false
	a.changeRequests.err = ""
	a.refreshChangeRequests()
}

func (a *app) testHostingAccess() {
	state := &a.hostingAccess
	account, err := a.hostingDraft()
	if err != nil {
		state.err, state.notice = err.Error(), ""
		return
	}
	token := a.hostingDraftToken(account)
	if token == "" {
		state.err, state.notice = tr("Enter an access token."), ""
		return
	}
	env := hostingEnvironment(account, token, account.Project)
	provider := repository.PullRequestGitLab
	if account.Provider == hostingProviderGitHub {
		provider = repository.PullRequestGitHub
	}
	state.seq++
	seq := state.seq
	state.busy, state.err, state.notice = true, "", tr("Testing connection…")
	go func() {
		defer a.recoverBackground()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		user, err := repository.CheckHostingAccess(ctx, provider, env)
		a.update(func() {
			if seq != a.hostingAccess.seq {
				return
			}
			a.hostingAccess.busy = false
			if err != nil {
				a.hostingAccess.notice = ""
				a.hostingAccess.err = statusErrorLine(safeError(err))
				return
			}
			a.hostingAccess.err = ""
			a.hostingAccess.notice = trf("Connected as %s.", user)
		})
	}()
}
