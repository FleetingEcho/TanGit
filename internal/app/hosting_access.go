package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"sort"
	"strconv"
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
	// ID is immutable. Repository bindings and token references use it rather
	// than the display name, which may be renamed without moving credentials.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Project and the two issue URLs are legacy defaults for repositories
	// without an explicit binding. A binding keeps its own copies.
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

// hostingBinding selects one global profile for one repository. Project and
// issue URLs stay here so sharing a profile never points another repository at
// the first repository's project.
type hostingBinding struct {
	ProfileID string `json:"profileId"`
	Project   string `json:"project,omitempty"`
	IssuesURL string `json:"issuesURL,omitempty"`
	IssueLink string `json:"issueLink,omitempty"`
}

// hostingWebRemote is one remote's web address field; saved is what the field
// held when the dialog opened, so an untouched field writes nothing.
type hostingWebRemote struct {
	name, url, saved string
}

type hostingAccessUIState struct {
	open      bool
	editing   bool
	usageOpen bool
	usageID   string
	profileID string
	name      string
	repoKey   string
	repoPath  string
	provider  string
	url       string
	project   string
	alias     string
	issuesURL string
	issueLink string
	// remoteURL is the open repository's web URL. It seeds a provider-specific
	// issue link for a new account without replacing a value already saved.
	remoteURL string
	// webRemotes are the editable web addresses of this repository's remotes,
	// saved as the same per-repository overrides the Remotes panel edits.
	webRemotes []hostingWebRemote
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
	// noticeOK colors the notice as a success, as after a connection test passes.
	noticeOK bool
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

func normalizeHostingProfileName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

func sameHostingProfileName(left, right string) bool {
	return strings.EqualFold(normalizeHostingProfileName(left), normalizeHostingProfileName(right))
}

func defaultHostingProfileName(account hostingAccount) string {
	return hostingProviderName(account.Provider) + " · " + hostingHostname(account.URL)
}

func hostingProfileNameAvailable(accounts []hostingAccount, name, excludeID string) bool {
	name = normalizeHostingProfileName(name)
	if name == "" {
		return false
	}
	for _, account := range accounts {
		if account.ID != excludeID && sameHostingProfileName(account.Name, name) {
			return false
		}
	}
	return true
}

func uniqueHostingProfileName(accounts []hostingAccount, base, excludeID string) string {
	base = normalizeHostingProfileName(base)
	for suffix := 1; ; suffix++ {
		name := base
		if suffix > 1 {
			name = strings.TrimSpace(base + " (" + strconv.Itoa(suffix) + ")")
		}
		if hostingProfileNameAvailable(accounts, name, excludeID) {
			return name
		}
	}
}

func validHostingProfileID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("-_.", r) {
			return false
		}
	}
	return true
}

func legacyHostingProfileID(account hostingAccount) string {
	sum := sha256.Sum256([]byte(account.Provider + "\x00" + account.URL))
	return "legacy-" + hex.EncodeToString(sum[:8])
}

func newHostingProfileID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return "profile-" + hex.EncodeToString(id[:]), nil
}

func normalizeHostingAccounts(accounts []hostingAccount) []hostingAccount {
	if len(accounts) == 0 {
		return nil
	}
	result := make([]hostingAccount, 0, len(accounts))
	seenIDs := map[string]bool{}
	for _, account := range accounts {
		account.Provider = strings.ToLower(strings.TrimSpace(account.Provider))
		if account.Provider != hostingProviderGitLab && account.Provider != hostingProviderGitHub {
			continue
		}
		serverURL, ok := normalizeHostingURL(account.URL)
		project, projectOK := normalizeHostingProject(account.Project)
		if !ok || !projectOK {
			continue
		}
		account.ID = strings.TrimSpace(account.ID)
		if !validHostingProfileID(account.ID) || seenIDs[account.ID] {
			account.ID = legacyHostingProfileID(account)
			for suffix := 2; seenIDs[account.ID]; suffix++ {
				account.ID = legacyHostingProfileID(account) + "-" + strconv.Itoa(suffix)
			}
		}
		seenIDs[account.ID] = true
		account.Name = normalizeHostingProfileName(account.Name)
		if account.Name == "" {
			account.Name = defaultHostingProfileName(account)
		}
		account.Name = uniqueHostingProfileName(result, account.Name, account.ID)
		result = append(result, hostingAccount{
			ID: account.ID, Name: account.Name, Provider: account.Provider, URL: serverURL, Project: project,
			Aliases:   normalizeHostingAliases(account.Aliases),
			IssuesURL: strings.TrimSpace(account.IssuesURL), IssueLink: strings.TrimSpace(account.IssueLink),
		})
	}
	return result
}

func normalizeHostingBindings(bindings map[string]hostingBinding) map[string]hostingBinding {
	if len(bindings) == 0 {
		return nil
	}
	result := make(map[string]hostingBinding, len(bindings))
	for path, binding := range bindings {
		path = strings.TrimSpace(path)
		binding.ProfileID = strings.TrimSpace(binding.ProfileID)
		project, ok := normalizeHostingProject(binding.Project)
		if path == "" || binding.ProfileID == "" || !ok {
			continue
		}
		binding.Project = project
		binding.IssuesURL = strings.TrimSpace(binding.IssuesURL)
		binding.IssueLink = strings.TrimSpace(binding.IssueLink)
		result[cleanRecentPath(path)] = binding
	}
	if len(result) == 0 {
		return nil
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

func legacyHostingSecretRef(account hostingAccount) sshworkspace.SecretRef {
	return sshworkspace.SecretRef("hosting/" + account.Provider + "/" + hostingHostname(account.URL))
}

// hostingSecretRef scopes named accounts independently, including two profiles
// on the same server. Legacy references remain readable during migration.
func hostingSecretRef(account hostingAccount) sshworkspace.SecretRef {
	if account.ID == "" {
		return legacyHostingSecretRef(account)
	}
	return sshworkspace.SecretRef("hosting/profile/" + account.ID + "/token")
}

func usesLegacyHostingSecret(account hostingAccount) bool {
	return account.ID == "" || strings.HasPrefix(account.ID, "legacy-")
}

func (a *app) hostingToken(account hostingAccount) (string, bool, error) {
	token, found, err := a.sshSecrets().Get(hostingSecretRef(account))
	if found || !usesLegacyHostingSecret(account) {
		return token, found, err
	}
	legacyToken, legacyFound, legacyErr := a.sshSecrets().Get(legacyHostingSecretRef(account))
	if legacyFound {
		return legacyToken, true, nil
	}
	if err != nil {
		return "", false, err
	}
	return legacyToken, legacyFound, legacyErr
}

func (a *app) hostingAccountIndexByID(id string) int {
	for i, account := range a.settings.HostingAccounts {
		if account.ID == id {
			return i
		}
	}
	return -1
}

func hostingBindingKey(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return cleanRecentPath(path)
}

func (a *app) hostingBindingForRepository(path string) (hostingBinding, bool) {
	binding, ok := a.settings.HostingBindings[hostingBindingKey(path)]
	return binding, ok && binding.ProfileID != ""
}

func hostingAccountWithBinding(account hostingAccount, binding hostingBinding) hostingAccount {
	account.Project = binding.Project
	account.IssuesURL = binding.IssuesURL
	account.IssueLink = binding.IssueLink
	return account
}

// boundHostingAccount resolves the explicit repository choice before any
// remote-host matching is attempted.
func (a *app) boundHostingAccount() (hostingAccount, bool) {
	if a.session == nil {
		return hostingAccount{}, false
	}
	binding, ok := a.hostingBindingForRepository(a.remoteWebRepositoryKey())
	if !ok {
		return hostingAccount{}, false
	}
	index := a.hostingAccountIndexByID(binding.ProfileID)
	if index < 0 {
		return hostingAccount{}, false
	}
	return hostingAccountWithBinding(a.settings.HostingAccounts[index], binding), true
}

func (a *app) hostingBindingCount(profileID string) int {
	count := 0
	for _, binding := range a.settings.HostingBindings {
		if binding.ProfileID == profileID {
			count++
		}
	}
	return count
}

// hostingRepositoryHosts lists the web hosts this repository's remotes live on.
func (a *app) hostingRepositoryHosts() []string {
	state := a.hostingAccess
	var hosts []string
	add := func(host string) {
		if host != "" && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	add(state.remoteHost)
	for _, remote := range state.webRemotes {
		add(hostingHostname(remote.saved))
	}
	return hosts
}

// hostingAccountFitsRepository reports whether the configuration's server is one
// of this repository's hosts, directly or through an alias. A repository whose
// remote host is unknown can use any configuration.
func (a *app) hostingAccountFitsRepository(account hostingAccount) bool {
	hosts := a.hostingRepositoryHosts()
	if len(hosts) == 0 {
		return true
	}
	for _, host := range hosts {
		if hostingHostname(account.URL) == host || slices.Contains(account.Aliases, host) {
			return true
		}
	}
	return false
}

func (a *app) toggleHostingBinding(profileID string) {
	state := &a.hostingAccess
	if state.repoKey == "" {
		return
	}
	index := a.hostingAccountIndexByID(profileID)
	if index < 0 {
		return
	}
	account := a.settings.HostingAccounts[index]
	binding, found := a.hostingBindingForRepository(state.repoKey)
	if found && binding.ProfileID == profileID {
		delete(a.settings.HostingBindings, state.repoKey)
		state.profileID = ""
		state.err = ""
		state.notice = ""
		a.saveSettings()
		a.notifyHostingAccessChanged()
		a.reloadChangeRequestsAfterAccessChange()
		return
	}
	if !found {
		binding = hostingBinding{
			Project: account.Project, IssuesURL: account.IssuesURL, IssueLink: account.IssueLink,
		}
	}
	binding.ProfileID = profileID
	if a.settings.HostingBindings == nil {
		a.settings.HostingBindings = make(map[string]hostingBinding)
	}
	a.settings.HostingBindings[state.repoKey] = binding
	state.profileID = profileID
	state.err = ""
	state.notice = ""
	// A different host is allowed (an SSH alias, a mirror, an enterprise domain)
	// but worth a warning, since requests will go to that configuration's server.
	if !a.hostingAccountFitsRepository(account) {
		state.notice = trf("%s is for %s, not for this repository's host (%s).", account.Name, hostingHostname(account.URL), strings.Join(a.hostingRepositoryHosts(), ", "))
	}
	a.saveSettings()
	a.notifyHostingAccessChanged()
	a.reloadChangeRequestsAfterAccessChange()
}

type hostingRepositoryUse struct {
	Name string
	Path string
}

func (a *app) hostingRepositories(profileID string) []hostingRepositoryUse {
	result := make([]hostingRepositoryUse, 0, a.hostingBindingCount(profileID))
	for path, binding := range a.settings.HostingBindings {
		if binding.ProfileID != profileID {
			continue
		}
		name := repoName(path)
		want := cleanRecentPath(path)
		for _, recent := range a.recentRepositories {
			if !recent.EditorOnly && cleanRecentPath(recent.Path) == want {
				name = recent.displayName()
				break
			}
		}
		result = append(result, hostingRepositoryUse{Name: name, Path: path})
	}
	slices.SortFunc(result, func(left, right hostingRepositoryUse) int {
		if order := strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)); order != 0 {
			return order
		}
		return strings.Compare(left.Path, right.Path)
	})
	return result
}

// hostingEnvironment builds the credential variables the hosting CLI reads.
//
// project is the repository's project path. glab rejects GITLAB_HOST when no
// git remote has exactly that hostname (an SSH alias, for instance), so a
// known project is passed as GITLAB_REPO instead, which has no such check.
// hostingLoginEnvironment aims the hosting CLI at the configuration's server
// and project without supplying a token, so it uses its own saved login.
func hostingLoginEnvironment(account hostingAccount, project string) []string {
	host := hostingHostname(account.URL)
	if account.Provider == hostingProviderGitHub {
		var env []string
		if host != "github.com" {
			env = append(env, "GH_HOST="+host)
		}
		if project != "" {
			env = append(env, "GH_REPO="+project)
		}
		return env
	}
	if project != "" {
		return []string{"GITLAB_REPO=" + account.URL + "/" + project}
	}
	return []string{"GITLAB_HOST=" + host}
}

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

func matchedHostingProject(account hostingAccount, hosts map[string]string) (string, bool) {
	project, matches := hosts[hostingHostname(account.URL)]
	for _, alias := range account.Aliases {
		if !matches {
			project, matches = hosts[alias]
		}
	}
	return project, matches
}

// applyHostingAccess gives a session its explicit repository binding, or the
// legacy remote-host match when no binding exists. It runs on the UI goroutine.
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
	apply := func(account hostingAccount, remoteProject string) {
		project := account.Project
		if project == "" {
			project = remoteProject
		}
		token, found, err := a.hostingToken(account)
		if err == nil && found {
			env = hostingEnvironment(account, token, project)
		} else {
			// No saved token: gh and glab keep using their own login, aimed at
			// this configuration's server and project.
			env = hostingLoginEnvironment(account, project)
		}
		provider = repository.PullRequestProvider(account.Provider)
	}
	if account, bound := a.boundHostingAccount(); bound {
		remoteProject, matches := matchedHostingProject(account, hosts)
		if !matches && len(hosts) == 1 {
			// An explicit selection is allowed to override aliases. A single remote
			// still gives the CLI its project scope without pointing at another repo.
			for _, project := range hosts {
				remoteProject = project
			}
		}
		apply(account, remoteProject)
		setter.SetHostingEnvironment(provider, env)
		return
	}
	matches := make([]hostingAccount, 0, 1)
	for _, account := range a.settings.HostingAccounts {
		if _, ok := matchedHostingProject(account, hosts); ok {
			matches = append(matches, account)
		}
	}
	if len(matches) == 1 {
		account := matches[0]
		remoteProject, _ := matchedHostingProject(account, hosts)
		apply(account, remoteProject)
	}
	// Multiple matching named profiles are deliberately ambiguous until the
	// repository chooses one. Never pick an arbitrary account's token.
	setter.SetHostingEnvironment(provider, env)
}

func (a *app) openHostingAccess() {
	if a.session != nil {
		a.openHostingAccessForRepository(a.remoteWebRepositoryKey())
		return
	}
	a.openHostingAccessForRepository("")
}

func (a *app) openHostingAccessForRepository(path string) {
	a.settings.HostingAccounts = normalizeHostingAccounts(a.settings.HostingAccounts)
	a.settings.HostingBindings = normalizeHostingBindings(a.settings.HostingBindings)
	state := hostingAccessUIState{open: true, provider: hostingProviderGitLab, repoPath: path, repoKey: hostingBindingKey(path)}
	if runner, ok := a.session.(forgeCommandSession); ok {
		if tool, err := runner.PullRequestTool(context.Background()); err == nil && tool == "gh" {
			state.provider = hostingProviderGitHub
		}
	}
	useWebURL := func(webURL string) bool {
		serverURL, ok := normalizeHostingURL(webURL)
		if !ok {
			return false
		}
		state.url = serverURL
		state.remoteHost = hostingHostname(webURL)
		state.remoteURL = webURL
		return true
	}
	if a.session != nil && state.repoKey == hostingBindingKey(a.remoteWebRepositoryKey()) {
		for _, remote := range a.snapshot.Remotes {
			if useWebURL(a.remoteWebAddress(remote)) {
				break
			}
		}
	} else if remotes := a.settings.RemoteWebURLs[state.repoKey]; len(remotes) > 0 {
		if !useWebURL(remotes["origin"]) {
			for _, webURL := range remotes {
				if useWebURL(webURL) {
					break
				}
			}
		}
	}
	if state.repoKey != "" {
		if a.session != nil && state.repoKey == hostingBindingKey(a.remoteWebRepositoryKey()) {
			for _, remote := range a.snapshot.Remotes {
				webURL := a.remoteWebAddress(remote)
				state.webRemotes = append(state.webRemotes, hostingWebRemote{name: remote.Name, url: webURL, saved: webURL})
			}
		} else {
			// A repository that is not open has no remote list; offer origin and
			// any remote that already has a saved address.
			names := []string{"origin"}
			for name := range a.settings.RemoteWebURLs[state.repoKey] {
				if name != "origin" {
					names = append(names, name)
				}
			}
			sort.Strings(names[1:])
			for _, name := range names {
				webURL := a.settings.RemoteWebURLs[state.repoKey][name]
				if name == "origin" && webURL == "" {
					webURL = state.remoteURL
				}
				state.webRemotes = append(state.webRemotes, hostingWebRemote{name: name, url: webURL, saved: webURL})
			}
		}
	}
	if binding, ok := a.hostingBindingForRepository(state.repoKey); ok && a.hostingAccountIndexByID(binding.ProfileID) >= 0 {
		state.profileID = binding.ProfileID
	} else if state.repoKey == "" && len(a.settings.HostingAccounts) > 0 {
		state.profileID = a.settings.HostingAccounts[0].ID
	} else {
		for _, account := range a.settings.HostingAccounts {
			if account.URL == state.url || state.remoteHost != "" && (hostingHostname(account.URL) == state.remoteHost || slices.Contains(account.Aliases, state.remoteHost)) {
				state.profileID = account.ID
				break
			}
		}
	}
	if state.profileID == "" && strings.EqualFold(hostingHostname(state.url), "github.com") {
		state.provider = hostingProviderGitHub
	}
	a.hostingAccess = state
	a.loadHostingAccessDraft()
}

func (a *app) beginHostingProfile() {
	state := a.hostingAccess
	state.editing = true
	state.usageOpen = false
	state.usageID = ""
	state.profileID = ""
	state.name = ""
	state.project = ""
	state.alias = ""
	state.token = ""
	state.hasToken = false
	state.showToken = false
	state.err = ""
	state.notice = ""
	a.hostingAccess = state
	a.loadHostingAccessDraft()
}

func (a *app) selectHostingProfile(id string) {
	if a.hostingAccountIndexByID(id) < 0 {
		return
	}
	a.hostingAccess.profileID = id
	a.hostingAccess.editing = true
	a.hostingAccess.usageOpen = false
	a.hostingAccess.usageID = ""
	a.hostingAccess.err = ""
	a.hostingAccess.notice = ""
	a.loadHostingAccessDraft()
}

func (a *app) closeHostingAccessEditor() {
	a.showHostingAccessList("")
}

func (a *app) showHostingAccessList(notice string) {
	state := &a.hostingAccess
	state.seq++
	state.editing = false
	state.usageOpen = false
	state.usageID = ""
	state.token = ""
	state.hasToken = false
	state.showToken = false
	state.busy = false
	state.err = ""
	state.notice = notice
}

// loadHostingAccessDraft fills the form from the selected profile. Repository
// bindings replace only project fields; aliases and credentials stay global.
func (a *app) loadHostingAccessDraft() {
	state := &a.hostingAccess
	state.project = ""
	state.issuesURL = defaultHostingIssuesURL(state.provider, state.remoteURL)
	state.issueLink = defaultHostingIssueLink(state.provider, state.remoteURL)
	state.issuesURL = strings.TrimSpace(state.issuesURL)
	state.issueLink = strings.TrimSpace(state.issueLink)
	state.token, state.hasToken = "", false
	state.showToken = false
	state.alias = ""
	index := a.hostingAccountIndexByID(state.profileID)
	if index < 0 {
		if state.remoteHost != "" && state.remoteHost != hostingHostname(state.url) {
			state.alias = state.remoteHost
		}
		return
	}
	account := a.settings.HostingAccounts[index]
	state.provider, state.url = account.Provider, account.URL
	state.name = account.Name
	state.project = account.Project
	state.issuesURL = account.IssuesURL
	state.issueLink = account.IssueLink
	if binding, ok := a.hostingBindingForRepository(state.repoKey); ok && binding.ProfileID == account.ID {
		state.project = binding.Project
		state.issuesURL = binding.IssuesURL
		state.issueLink = binding.IssueLink
	}
	state.alias = strings.Join(account.Aliases, ", ")
	// The saved token fills the field, hidden until the eye is pressed.
	state.token, state.hasToken, _ = a.hostingToken(account)
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
	account := hostingAccount{
		ID: a.hostingAccess.profileID, Name: normalizeHostingProfileName(a.hostingAccess.name),
		Provider: a.hostingAccess.provider, URL: serverURL, Project: project,
		Aliases:   normalizeHostingAliases(strings.Split(a.hostingAccess.alias, ",")),
		IssuesURL: strings.TrimSpace(a.hostingAccess.issuesURL),
		IssueLink: strings.TrimSpace(a.hostingAccess.issueLink),
	}
	if account.Name == "" {
		account.Name = uniqueHostingProfileName(a.settings.HostingAccounts, defaultHostingProfileName(account), account.ID)
	} else if !hostingProfileNameAvailable(a.settings.HostingAccounts, account.Name, account.ID) {
		return hostingAccount{}, errors.New(tr("Configuration name already in use"))
	}
	return account, nil
}

// hostingDraftToken is the typed token, or the saved one when the field is empty.
func (a *app) hostingDraftToken(account hostingAccount) string {
	if token := strings.TrimSpace(a.hostingAccess.token); token != "" {
		return token
	}
	if account.ID == "" {
		return ""
	}
	token, _, _ := a.hostingToken(account)
	return token
}

func (a *app) saveHostingAccess() {
	state := &a.hostingAccess
	account, err := a.hostingDraft()
	if err != nil {
		state.err, state.notice = err.Error(), ""
		return
	}
	if account.ID == "" {
		account.ID, err = newHostingProfileID()
		if err != nil {
			state.err, state.notice = err.Error(), ""
			return
		}
	}
	token := strings.TrimSpace(state.token)
	webChanges, err := a.hostingWebAddressDraft()
	if err != nil {
		state.err, state.notice = err.Error(), ""
		return
	}
	if index := a.hostingAccountIndexByID(account.ID); index >= 0 {
		previous := a.settings.HostingAccounts[index]
		if state.repoKey != "" {
			account.Project, account.IssuesURL, account.IssueLink = previous.Project, previous.IssuesURL, previous.IssueLink
		}
		a.settings.HostingAccounts[index] = account
	} else {
		if state.repoKey != "" {
			// A profile created from one repository gets that repository's project
			// fields in its binding, not as defaults inherited by every repository.
			account.Project, account.IssuesURL, account.IssueLink = "", "", ""
		}
		a.settings.HostingAccounts = append(a.settings.HostingAccounts, account)
	}
	if state.repoKey != "" {
		if a.settings.HostingBindings == nil {
			a.settings.HostingBindings = map[string]hostingBinding{}
		}
		project, _ := normalizeHostingProject(state.project)
		a.settings.HostingBindings[state.repoKey] = hostingBinding{
			ProfileID: account.ID, Project: project,
			IssuesURL: strings.TrimSpace(state.issuesURL), IssueLink: strings.TrimSpace(state.issueLink),
		}
	}
	a.applyHostingWebAddresses(webChanges)
	persistent := true
	if token != "" {
		persistent = a.sshSecrets().Set(hostingSecretRef(account), token) == nil
		if persistent && usesLegacyHostingSecret(account) {
			_ = a.sshSecrets().Delete(legacyHostingSecretRef(account))
		}
	}
	a.saveSettings()
	state.profileID = account.ID
	a.showHostingAccessList(tr("Configuration saved"))
	a.notifyHostingAccessChanged()
	a.reloadChangeRequestsAfterAccessChange()
	if !persistent {
		a.notify("warning", "Token saved for this session only", tr("The system keychain is unavailable, so the token will be forgotten when the app closes."))
	}
}

func (a *app) removeHostingToken() {
	state := &a.hostingAccess
	account, err := a.hostingDraft()
	if err != nil {
		state.err = err.Error()
		return
	}
	_ = a.sshSecrets().Delete(hostingSecretRef(account))
	if usesLegacyHostingSecret(account) {
		_ = a.sshSecrets().Delete(legacyHostingSecretRef(account))
	}
	state.token, state.hasToken = "", false
	state.notice = tr("Token removed")
	state.err = ""
	a.notifyHostingAccessChanged()
	a.reloadChangeRequestsAfterAccessChange()
}

func (a *app) deleteHostingProfileByID(id string) {
	state := &a.hostingAccess
	index := a.hostingAccountIndexByID(id)
	if index < 0 {
		return
	}
	account := a.settings.HostingAccounts[index]
	if count := a.hostingBindingCount(account.ID); count > 0 {
		state.err = trf("This configuration is used by %d repositories; change those bindings before deleting it.", count)
		return
	}
	list := a.settings.HostingAccounts
	a.settings.HostingAccounts = append(append([]hostingAccount(nil), list[:index]...), list[index+1:]...)
	_ = a.sshSecrets().Delete(hostingSecretRef(account))
	if usesLegacyHostingSecret(account) {
		_ = a.sshSecrets().Delete(legacyHostingSecretRef(account))
	}
	a.saveSettings()
	state.profileID = ""
	state.editing = false
	state.err = ""
	state.notice = trf("Deleted configuration %s", account.Name)
	a.notifyHostingAccessChanged()
	a.reloadChangeRequestsAfterAccessChange()
}

// deleteHostingProfile remains the draft-oriented operation used by focused
// state tests. User-facing deletion always goes through the confirmation below.
func (a *app) deleteHostingProfile() {
	a.deleteHostingProfileByID(a.hostingAccess.profileID)
}

func (a *app) confirmDeleteHostingProfile(id string) {
	index := a.hostingAccountIndexByID(id)
	if index < 0 {
		return
	}
	account := a.settings.HostingAccounts[index]
	if count := a.hostingBindingCount(id); count > 0 {
		a.hostingAccess.err = trf("This configuration is used by %d repositories; change those bindings before deleting it.", count)
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Delete Git host",
		message:      trf("Delete the configuration “%s”?", account.Name),
		detail:       tr("Its saved access token will also be removed from the system keychain."),
		confirmLabel: "Delete configuration",
		destructive:  true,
		run:          func() { a.deleteHostingProfileByID(id) },
	})
}

func (a *app) notifyHostingAccessChanged() {
	if a.onHostingAccessChanged != nil {
		a.onHostingAccessChanged()
	}
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
	env := hostingEnvironment(account, token, account.Project)
	if token == "" {
		// No token: check the login gh or glab already holds for this server.
		env = hostingLoginEnvironment(account, account.Project)
	}
	provider := repository.PullRequestGitLab
	if account.Provider == hostingProviderGitHub {
		provider = repository.PullRequestGitHub
	}
	state.seq++
	seq := state.seq
	state.busy, state.err, state.notice, state.noticeOK = true, "", tr("Testing connection…"), false
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
			a.hostingAccess.noticeOK = true
		})
	}()
}

// hostingWebAddressDraft validates the web address fields. An empty field
// returns that remote to its automatic address; an unchanged one writes nothing.
func (a *app) hostingWebAddressDraft() (map[string]string, error) {
	state := a.hostingAccess
	changes := map[string]string{}
	for _, remote := range state.webRemotes {
		typed := strings.TrimSpace(remote.url)
		if typed == strings.TrimSpace(remote.saved) {
			continue
		}
		if typed == "" {
			changes[remote.name] = ""
			continue
		}
		webURL, ok := normalizeRemoteWebURL(typed)
		if !ok {
			return nil, errors.New(remote.name + ": " + tr("Enter a valid HTTP(S) web URL without credentials."))
		}
		changes[remote.name] = webURL
	}
	return changes, nil
}

func (a *app) applyHostingWebAddresses(changes map[string]string) {
	if len(changes) == 0 {
		return
	}
	state := &a.hostingAccess
	root := state.repoKey
	remotes := make(map[string]string, len(a.settings.RemoteWebURLs[root])+len(changes))
	for name, value := range a.settings.RemoteWebURLs[root] {
		remotes[name] = value
	}
	for name, webURL := range changes {
		if webURL == "" {
			delete(remotes, name)
		} else {
			remotes[name] = webURL
		}
	}
	if a.settings.RemoteWebURLs == nil {
		a.settings.RemoteWebURLs = make(map[string]map[string]string)
	}
	a.settings.RemoteWebURLs[root] = remotes
	a.settings.RemoteWebURLs = normalizeRemoteWebURLs(a.settings.RemoteWebURLs)
	for i := range state.webRemotes {
		if webURL, ok := changes[state.webRemotes[i].name]; ok {
			state.webRemotes[i].url, state.webRemotes[i].saved = webURL, webURL
		}
	}
}
