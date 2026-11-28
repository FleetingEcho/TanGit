package app

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func hostingProfileTestApp(accounts ...hostingAccount) (*app, *fakeSSHKeychain) {
	a := newApp(nil)
	keychain := &fakeSSHKeychain{}
	a.sshSecretBackend = keychain
	a.settings.HostingAccounts = normalizeHostingAccounts(accounts)
	return a, keychain
}

func clickHostingControl(t *testing.T, tester *ui.Tester, name string) {
	t.Helper()
	for _, info := range tester.AccessAll() {
		if info.Name == name && info.Focusable {
			tester.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
			return
		}
	}
	t.Fatalf("no focusable hosting control named %q", name)
}

func TestNormalizeHostingAccountsKeepsSameHostProfilesDistinct(t *testing.T) {
	accounts := normalizeHostingAccounts([]hostingAccount{
		{Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"},
		{ID: "profile-one", Name: "Team", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"},
		{ID: "profile-two", Name: " team ", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"},
	})
	if len(accounts) != 3 {
		t.Fatalf("profiles = %d, want 3", len(accounts))
	}
	if accounts[0].ID == "" || accounts[0].Name != "GitLab · gitlab.example.com" {
		t.Fatalf("legacy profile migration = %+v", accounts[0])
	}
	if accounts[1].Name != "Team" || sameHostingProfileName(accounts[1].Name, accounts[2].Name) {
		t.Fatalf("duplicate normalized names = %q, %q", accounts[1].Name, accounts[2].Name)
	}
	if hostingSecretRef(accounts[1]) == hostingSecretRef(accounts[2]) {
		t.Fatal("same-host profiles share a token key")
	}
}

func TestHostingTokenReadsByProfileAndMigratesLegacyOnSave(t *testing.T) {
	accounts := normalizeHostingAccounts([]hostingAccount{
		{ID: "profile-one", Name: "One", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"},
		{ID: "profile-two", Name: "Two", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"},
	})
	a, keychain := hostingProfileTestApp(accounts...)
	first, second := accounts[0], accounts[1]
	if err := a.sshSecrets().Set(hostingSecretRef(first), "token-one"); err != nil {
		t.Fatal(err)
	}
	if err := a.sshSecrets().Set(hostingSecretRef(second), "token-two"); err != nil {
		t.Fatal(err)
	}
	if err := a.sshSecrets().Set(legacyHostingSecretRef(first), "legacy-token"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		account hostingAccount
		want    string
	}{{first, "token-one"}, {second, "token-two"}} {
		if token, found, err := a.hostingToken(test.account); err != nil || !found || token != test.want {
			t.Fatalf("hostingToken(%s) = %q, %v, %v", test.account.Name, token, found, err)
		}
	}

	legacy := normalizeHostingAccounts([]hostingAccount{{Provider: hostingProviderGitLab, URL: "https://code.example.com"}})[0]
	_ = a.sshSecrets().Set(legacyHostingSecretRef(legacy), "legacy-token")
	keychain.Delete(string(hostingSecretRef(legacy)))
	a.settings.HostingAccounts = append(a.settings.HostingAccounts, legacy)
	account, found, err := a.hostingToken(legacy)
	if err != nil || !found || account != "legacy-token" {
		t.Fatalf("legacy token fallback = %q, %v, %v", account, found, err)
	}
	a.hostingAccess = hostingAccessUIState{
		open: true, profileID: legacy.ID, name: legacy.Name, provider: legacy.Provider, url: legacy.URL,
		token: account, hasToken: true,
	}
	a.saveHostingAccess()
	migrated, found, err := a.hostingToken(legacy)
	if err != nil || !found || migrated != "legacy-token" {
		t.Fatalf("migrated token = %q, %v, %v", migrated, found, err)
	}
	if _, err := keychain.Get(string(legacyHostingSecretRef(legacy))); err == nil {
		t.Fatal("old token key survived a successful profile-key migration")
	}
	encoded, err := json.Marshal(a.settings)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"token-one", "token-two", "legacy-token"} {
		if strings.Contains(string(encoded), token) {
			t.Fatalf("token leaked into settings JSON: %s", encoded)
		}
	}
}

func TestNewProfileTokenRemovalDoesNotDeleteLegacyAccountToken(t *testing.T) {
	legacy := hostingAccount{ID: "legacy-one", Name: "Legacy", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"}
	second := hostingAccount{ID: "profile-two", Name: "Second", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"}
	a, keychain := hostingProfileTestApp(legacy, second)
	_ = keychain.Set(string(legacyHostingSecretRef(legacy)), []byte("legacy-token"))
	_ = a.sshSecrets().Set(hostingSecretRef(second), "second-token")
	a.hostingAccess = hostingAccessUIState{
		open: true, profileID: second.ID, name: second.Name, provider: second.Provider, url: second.URL,
		token: "second-token", hasToken: true,
	}
	a.removeHostingToken()
	if value, err := keychain.Get(string(legacyHostingSecretRef(legacy))); err != nil || string(value) != "legacy-token" {
		t.Fatalf("new profile removed the legacy token: %q, %v", value, err)
	}
	if _, found, _ := a.hostingToken(second); found {
		t.Fatal("new profile unexpectedly fell back to another profile's legacy token")
	}
	if token, found, _ := a.hostingToken(legacy); !found || token != "legacy-token" {
		t.Fatalf("legacy profile token = %q, %v", token, found)
	}
}

func TestHostingProfileRejectsDuplicateNamesWithoutRenamingTheOtherAccount(t *testing.T) {
	a, _ := hostingProfileTestApp(
		hostingAccount{ID: "one", Name: "Team", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"},
		hostingAccount{ID: "two", Name: "Other", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com"},
	)
	a.hostingAccess = hostingAccessUIState{
		open: true, profileID: "two", name: "  tEaM  ", provider: hostingProviderGitLab,
		url: "https://gitlab.example.com", token: "unused",
	}
	a.saveHostingAccess()
	if !strings.Contains(a.hostingAccess.err, "already in use") || !a.hostingAccess.open {
		t.Fatalf("duplicate-name result = open:%v error:%q", a.hostingAccess.open, a.hostingAccess.err)
	}
	if a.settings.HostingAccounts[0].Name != "Team" || a.settings.HostingAccounts[1].Name != "Other" {
		t.Fatalf("duplicate save changed names: %+v", a.settings.HostingAccounts)
	}
}

func TestRepositoryBindingsShareProfileButKeepProjectFields(t *testing.T) {
	a, _ := hostingProfileTestApp(hostingAccount{
		ID: "shared", Name: "Shared", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com",
		Project: "legacy/default", IssuesURL: "https://issues.example.com/default", IssueLink: "https://issues.example.com/default/{id}",
	})
	_ = a.sshSecrets().Set(hostingSecretRef(a.settings.HostingAccounts[0]), "shared-token")
	for _, repo := range []struct{ path, project string }{{"/work/one", "group/one"}, {"/work/two", "group/two"}} {
		a.openHostingAccessForRepository(repo.path)
		a.selectHostingProfile("shared")
		a.hostingAccess.project = repo.project
		a.hostingAccess.issuesURL = "https://issues.example.com/" + repo.project
		a.hostingAccess.issueLink = "https://issues.example.com/" + repo.project + "/{id}"
		a.saveHostingAccess()
	}
	account := a.settings.HostingAccounts[0]
	if account.Project != "legacy/default" || account.IssuesURL != "https://issues.example.com/default" {
		t.Fatalf("repository save overwrote legacy defaults: %+v", account)
	}
	for _, repo := range []struct{ path, project string }{{"/work/one", "group/one"}, {"/work/two", "group/two"}} {
		binding, ok := a.hostingBindingForRepository(repo.path)
		if !ok || binding.ProfileID != "shared" || binding.Project != repo.project {
			t.Fatalf("binding for %s = %+v, %v", repo.path, binding, ok)
		}
		if !strings.Contains(binding.IssueLink, repo.project) {
			t.Fatalf("binding issue link = %q", binding.IssueLink)
		}
	}
	if count := a.hostingBindingCount("shared"); count != 2 {
		t.Fatalf("shared binding count = %d", count)
	}

	a.openHostingAccess()
	a.hostingAccess.name = "Renamed shared"
	a.saveHostingAccess()
	if a.settings.HostingAccounts[0].Name != "Renamed shared" || a.settings.HostingAccounts[0].ID != "shared" {
		t.Fatalf("profile rename = %+v", a.settings.HostingAccounts[0])
	}
	if a.settings.HostingBindings["/work/one"].Project != "group/one" || a.settings.HostingBindings["/work/two"].Project != "group/two" {
		t.Fatalf("shared edit changed bindings: %+v", a.settings.HostingBindings)
	}
	a.hostingAccess = hostingAccessUIState{open: true, profileID: "shared", name: "Renamed shared", provider: hostingProviderGitLab, url: "https://gitlab.example.com"}
	a.deleteHostingProfile()
	if len(a.settings.HostingAccounts) != 1 || !strings.Contains(a.hostingAccess.err, "2 repositories") {
		t.Fatalf("bound profile was deleted or not blocked: accounts=%+v error=%q", a.settings.HostingAccounts, a.hostingAccess.err)
	}
}

func TestProfileCreatedFromRepositoryKeepsProjectFieldsInBinding(t *testing.T) {
	a, _ := hostingProfileTestApp()
	a.openHostingAccessForRepository("/work/new")
	a.beginHostingProfile()
	a.hostingAccess.name = "Repository profile"
	a.hostingAccess.provider = hostingProviderGitLab
	a.hostingAccess.url = "https://gitlab.example.com"
	a.hostingAccess.project = "group/repository"
	a.hostingAccess.issuesURL = "https://issues.example.com/group/repository"
	a.hostingAccess.issueLink = "https://issues.example.com/group/repository/{id}"
	a.hostingAccess.token = "token"
	a.saveHostingAccess()
	if len(a.settings.HostingAccounts) != 1 {
		t.Fatalf("accounts = %+v", a.settings.HostingAccounts)
	}
	account := a.settings.HostingAccounts[0]
	if account.Project != "" || account.IssuesURL != "" || account.IssueLink != "" {
		t.Fatalf("repository fields leaked into global defaults: %+v", account)
	}
	binding := a.settings.HostingBindings["/work/new"]
	if binding.ProfileID != account.ID || binding.Project != "group/repository" ||
		binding.IssuesURL != "https://issues.example.com/group/repository" ||
		binding.IssueLink != "https://issues.example.com/group/repository/{id}" {
		t.Fatalf("repository binding = %+v", binding)
	}
}

type hostingBindingSession struct {
	fakeSession
	provider repository.PullRequestProvider
	env      []string
}

func (s *hostingBindingSession) SetHostingEnvironment(provider repository.PullRequestProvider, env []string) {
	s.provider = provider
	s.env = append([]string(nil), env...)
}

func TestExplicitBindingDrivesCredentialsAndIssueLinks(t *testing.T) {
	a, _ := hostingProfileTestApp(hostingAccount{
		ID: "bound", Name: "Bound", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com", Aliases: []string{"other.example.com"},
	})
	account := a.settings.HostingAccounts[0]
	_ = a.sshSecrets().Set(hostingSecretRef(account), "bound-token")
	a.settings.HostingBindings = map[string]hostingBinding{"/work/project": {
		ProfileID: "bound", Project: "group/bound",
		IssuesURL: "https://issues.example.com/bound", IssueLink: "https://issues.example.com/bound/{id}",
	}}
	session := &hostingBindingSession{}
	a.session = session
	a.snapshot.Root = "/work/project"
	a.snapshot.Remotes = []repository.Remote{{Name: "origin", URLs: []string{"git@code.example:group/repository.git"}}}
	a.applyHostingAccess(session)
	if session.provider != repository.PullRequestGitLab || !slices.Contains(session.env, "GITLAB_TOKEN=bound-token") || !slices.Contains(session.env, "GITLAB_REPO=https://gitlab.example.com/group/bound") {
		t.Fatalf("bound hosting environment = %q %q", session.provider, session.env)
	}
	if a.requestIssuesURL() != "https://issues.example.com/bound" || a.requestIssueLink() != "https://issues.example.com/bound/{id}" {
		t.Fatalf("bound issue links = %q %q", a.requestIssuesURL(), a.requestIssueLink())
	}

	a.settings.HostingBindings = nil
	a.snapshot.Remotes = []repository.Remote{{Name: "origin", URLs: []string{"git@gitlab.example.com:group/repository.git"}}}
	a.applyHostingAccess(session)
	if session.provider != repository.PullRequestGitLab {
		t.Fatal("a unique legacy match did not remain available")
	}
	a.settings.HostingAccounts = append(a.settings.HostingAccounts, hostingAccount{
		ID: "second", Name: "Second", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com",
	})
	a.applyHostingAccess(session)
	if session.provider != "" || len(session.env) != 0 {
		t.Fatalf("ambiguous same-host profiles selected an arbitrary token: %q %q", session.provider, session.env)
	}
}

func TestHostingProfileChangesSpreadBetweenTabs(t *testing.T) {
	source, _ := hostingProfileTestApp(hostingAccount{
		ID: "shared", Name: "Shared", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com", Aliases: []string{"git.example.com"},
	})
	target, _ := hostingProfileTestApp()
	source.settings.HostingBindings = map[string]hostingBinding{"/work/one": {ProfileID: "shared", Project: "group/one"}}
	s := &shell{tabs: []*tab{{app: source}, {app: target}}}
	s.spreadHostingAccess(source)
	if len(target.settings.HostingAccounts) != 1 || target.settings.HostingBindings["/work/one"].Project != "group/one" {
		t.Fatalf("target hosting settings = %+v %+v", target.settings.HostingAccounts, target.settings.HostingBindings)
	}
	if s.settings.HostingBindings["/work/one"].ProfileID != "shared" {
		t.Fatal("the shell did not receive the shared binding")
	}
	target.settings.HostingAccounts[0].Aliases[0] = "changed.example.com"
	if source.settings.HostingAccounts[0].Aliases[0] != "git.example.com" {
		t.Fatal("tabs mutate each other's profile alias storage")
	}
}

func TestHomepageCreatesAndLoadsNamedProfilesWithoutARepository(t *testing.T) {
	a, tester, _ := recentTester(t, recentRepository{Path: "/work/recent", LastOpened: timeNow()})
	a.sshSecretBackend = &fakeSSHKeychain{}
	if err := tester.Click("Git hosting settings"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !a.hostingAccess.open || a.hostingAccess.editing || a.hostingAccess.repoKey != "" || !tester.HasText("No Git hosts yet") {
		t.Fatal("Homepage did not open the global configuration list first")
	}
	if err := tester.Click("Add configuration"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !a.hostingAccess.editing || !tester.HasText("Add Git host") ||
		!tester.HasText("Project path") || !tester.HasText("Issue link") || !tester.HasText("Description issue link") {
		t.Fatal("Homepage did not open the complete global add form from the list")
	}
	if err := tester.Click("Close editor and return to configurations"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !a.hostingAccess.open || a.hostingAccess.editing || !tester.HasText("Saved configurations") {
		t.Fatal("editor close did not return to the configuration list")
	}
	if err := tester.Click("Add configuration"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	// The name input auto-focuses. Focusable controls distinguish the actual
	// inputs from the rows' static labels and Info badges.
	tester.Type("Shared profile")
	tester.Frame()
	for _, step := range []struct{ label, text string }{
		{"Server address", "https://gitlab.example.com"},
		{"Project path", "group/global"},
		{"Issue link", "https://issues.example.com/group/global"},
		{"Issue link in descriptions", "https://issues.example.com/group/global/{id}"},
		{"Access token", "home-token"},
	} {
		clickHostingControl(t, tester, step.label)
		tester.Type(step.text)
		tester.Frame()
	}
	if err := tester.Click("Save"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !a.hostingAccess.open || a.hostingAccess.editing || len(a.settings.HostingAccounts) != 1 || !tester.HasText("Configuration saved") {
		t.Fatalf("Homepage did not return to the profile list after saving: state=%+v accounts=%+v", a.hostingAccess, a.settings.HostingAccounts)
	}
	if a.hostingAccess.token != "" || a.hostingAccess.hasToken || a.hostingAccess.showToken {
		t.Fatal("saved token remained in the list UI state")
	}
	profile := a.settings.HostingAccounts[0]
	if profile.Name != "Shared profile" || profile.ID == "" || profile.Project != "group/global" ||
		profile.IssuesURL != "https://issues.example.com/group/global" ||
		profile.IssueLink != "https://issues.example.com/group/global/{id}" || len(a.settings.HostingBindings) != 0 {
		t.Fatalf("Homepage profile = %+v bindings=%+v", profile, a.settings.HostingBindings)
	}
	if token, found, err := a.hostingToken(profile); err != nil || !found || token != "home-token" {
		t.Fatalf("Homepage profile token = %q, %v, %v", token, found, err)
	}

	a.openHostingAccessForRepository("/work/recent")
	tester.Frame()
	if a.hostingAccess.repoKey != "/work/recent" || a.hostingAccess.editing || tester.HasText("Project path") {
		t.Fatal("recent repository did not open its configuration list first")
	}
	a.selectHostingProfile(profile.ID)
	tester.Frame()
	if !a.hostingAccess.editing || !tester.HasText("Project path") {
		t.Fatal("recent repository edit form did not expose its independent project fields")
	}
	a.hostingAccess.project = "group/recent"
	a.saveHostingAccess()
	tester.Frame()
	if binding := a.settings.HostingBindings["/work/recent"]; binding.ProfileID != profile.ID || binding.Project != "group/recent" {
		t.Fatalf("recent repository binding = %+v", binding)
	}
	if a.hostingAccess.editing || !tester.HasText("Shared profile") {
		t.Fatal("repository save did not return to the configuration list")
	}
	if selected, ok := tester.AccessByRole(ui.RoleCheckBox, "Use configuration Shared profile"); !ok || !selected.Checked {
		t.Fatalf("repository-bound configuration selector = %+v, found=%v", selected, ok)
	}
}

func TestHostingProfileListScrollsAndShowsRepositoryUsage(t *testing.T) {
	a, tester, _ := recentTester(t,
		recentRepository{Path: "/work/alpha", Name: "Alpha repository", LastOpened: timeNow()},
		recentRepository{Path: "/work/beta", Name: "Beta repository", LastOpened: timeNow()},
	)
	for index := 1; index <= 8; index++ {
		a.settings.HostingAccounts = append(a.settings.HostingAccounts, hostingAccount{
			ID: "profile-" + string(rune('a'+index-1)), Name: trf("Profile %d", index),
			Provider: hostingProviderGitLab, URL: "https://gitlab.example.com",
		})
	}
	a.settings.HostingAccounts = normalizeHostingAccounts(a.settings.HostingAccounts)
	a.settings.HostingBindings = map[string]hostingBinding{
		"/work/alpha": {ProfileID: "profile-a"},
		"/work/beta":  {ProfileID: "profile-a"},
	}
	a.openHostingAccessForRepository("/work/alpha")
	tester.Frame()

	list, ok := tester.Find("Git hosting list")
	if !ok || list.H != hostingAccessListHeight {
		t.Fatalf("configuration list bounds = %+v, found=%v", list, ok)
	}
	edit, editOK := tester.Find("Edit configuration Profile 1")
	remove, removeOK := tester.Find("Delete configuration Profile 1")
	if !editOK || !removeOK || edit.W > 40 || remove.W > 40 {
		t.Fatalf("configuration icon actions = edit:%+v/%v delete:%+v/%v", edit, editOK, remove, removeOK)
	}
	if selected, ok := tester.AccessByRole(ui.RoleCheckBox, "Use configuration Profile 1"); !ok || !selected.Checked {
		t.Fatalf("bound profile selector = %+v, found=%v", selected, ok)
	}
	if err := tester.Click("Repositories using Profile 1"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("Alpha repository") || !tester.HasText("Beta repository") {
		t.Fatalf("repository usage popup = %q", tester.Texts())
	}
	if binding := a.settings.HostingBindings["/work/alpha"]; binding.ProfileID != "profile-a" {
		t.Fatalf("repository list button changed the selection: %+v", binding)
	}
	a.hostingAccess.usageOpen = false
	tester.Frame()
	if err := tester.Click("Repositories using Profile 2"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !tester.HasText("No repositories use this configuration.") {
		t.Fatal("unbound configuration did not expose its repository-list icon and empty state")
	}
	a.hostingAccess.usageOpen = false
	tester.Frame()
	if err := tester.Click("Use configuration Profile 2"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if binding := a.settings.HostingBindings["/work/alpha"]; binding.ProfileID != "profile-b" {
		t.Fatalf("selected repository binding = %+v", binding)
	}
	if selected, ok := tester.AccessByRole(ui.RoleCheckBox, "Use configuration Profile 2"); !ok || !selected.Checked {
		t.Fatalf("newly selected profile selector = %+v, found=%v", selected, ok)
	}
	if err := tester.Click("Use configuration Profile 2"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if _, found := a.settings.HostingBindings["/work/alpha"]; found {
		t.Fatal("clicking the selected configuration did not clear the repository binding")
	}
	if selected, ok := tester.AccessByRole(ui.RoleCheckBox, "Use configuration Profile 2"); !ok || selected.Checked {
		t.Fatalf("cleared profile stayed selected: %+v, found=%v", selected, ok)
	}
	if err := tester.Click("Use configuration Profile 1"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if binding := a.settings.HostingBindings["/work/alpha"]; binding.ProfileID != "profile-a" {
		t.Fatalf("replacement repository binding = %+v", binding)
	}
	lastBefore, ok := tester.Find("Profile 8")
	if !ok {
		t.Fatal("last configuration was not built inside the scroll list")
	}
	tester.Scroll(list.X+list.W/2, list.Y+list.H/2, 0, 1000)
	tester.Frame()
	lastAfter, ok := tester.Find("Profile 8")
	if !ok || lastAfter.Y >= lastBefore.Y {
		t.Fatalf("configuration list did not scroll: before=%+v after=%+v", lastBefore, lastAfter)
	}
}

func TestHostingProfileListDeleteRequiresConfirmation(t *testing.T) {
	a, tester, _ := recentTester(t)
	keychain := &fakeSSHKeychain{}
	a.sshSecretBackend = keychain
	a.settings.HostingAccounts = normalizeHostingAccounts([]hostingAccount{{
		ID: "delete-me", Name: "Delete me", Provider: hostingProviderGitLab, URL: "https://gitlab.example.com",
	}})
	account := a.settings.HostingAccounts[0]
	if err := keychain.Set(string(hostingSecretRef(account)), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	a.openHostingAccess()
	tester.Frame()
	if err := tester.Click("Delete configuration Delete me"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if a.confirm == nil || !a.confirm.destructive || a.confirm.confirmLabel != "Delete configuration" {
		t.Fatalf("delete confirmation = %+v", a.confirm)
	}
	if len(a.settings.HostingAccounts) != 1 {
		t.Fatal("configuration was deleted before confirmation")
	}
	a.acceptConfirm()
	if len(a.settings.HostingAccounts) != 0 || a.confirm != nil || !a.hostingAccess.open || a.hostingAccess.editing {
		t.Fatalf("confirmed deletion left accounts=%+v confirm=%+v state=%+v", a.settings.HostingAccounts, a.confirm, a.hostingAccess)
	}
	if _, err := keychain.Get(string(hostingSecretRef(account))); err == nil {
		t.Fatal("confirmed deletion left the access token in the keychain")
	}
}

func TestHomepageProviderButtonUsesSavedBrand(t *testing.T) {
	a, tester, _ := recentTester(t)
	a.settings.HostingAccounts = []hostingAccount{{Provider: hostingProviderGitHub, Name: "GitHub", URL: "https://github.example.com"}}
	tester.Frame()
	button, ok := tester.Find("Git hosting settings")
	if !ok || button.W <= 0 || button.H <= 0 {
		t.Fatal("saved-profile Homepage brand entry is missing")
	}
	if icon := hostingProviderIcon(hostingProviderName(hostingProviderGitHub)); icon != githubIcon {
		t.Fatal("GitHub profile uses the wrong provider icon")
	}
}
