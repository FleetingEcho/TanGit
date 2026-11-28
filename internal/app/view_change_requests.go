package app

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/FleetingEcho/lazygitGo/internal/forge"
	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type changeRequestsUIState struct {
	provider      forge.Provider
	newURL        string
	issuesURL     string
	page          int
	more          bool
	loadingMore   bool
	moreErr       string
	items         []forge.ChangeRequest
	selected      int
	list          ui.ListState
	loading       bool
	loaded        bool
	err           string
	token         uint64
	detail        *forge.Detail
	detailFor     string
	detailLoading bool
	detailErr     string
	detailToken   uint64
	commentDraft  string
	replyDraft    string
	replyFor      string
	expandedFile  string
	mutationBusy  bool
	mutationErr   string
	mutationToken uint64
	lineTarget    *forge.LineTarget
	lineDraft     string
	// readOnly is set when the token is known not to allow changes; every
	// control that would write is then hidden.
	readOnly bool
	// filter picks the list tab: 0 Open, 1 Merged, 2 Closed, 3 All.
	filter int
	// search is the box above the list; filters is what the Filter dialog applied.
	search      string
	filters     changeRequestFilterValues
	filterOpen  bool
	filterDraft changeRequestFilterValues
	filterErr   string
	// searchApplied is the text the shown list was fetched with.
	searchApplied string
	// afterAt and beforeAt back the date pickers; zero means not set.
	afterAt, beforeAt time.Time
	facets            forge.Facets
	facetsLoaded      bool
	facetsLoading     bool
	facetsToken       uint64
	tab               int
	rawMarkdown       bool
	showDisplay       bool
	md                markdown.Options
	// diffState draws the chosen file with the same viewer History uses.
	diffState      *editor.DiffState
	diffKey        string
	diffErr        string
	filesWidth     float32
	filesCollapsed bool
	diffMax        bool
	fileQuery      string
	fileSearch     ui.Handle
	drawerSearch   ui.Handle
	drawerWidth    float32
	// threadFlip holds threads whose folded state differs from the default.
	threadFlip map[string]bool
	// fileSearchOpen shows the changed files' search field.
	fileSearchOpen bool
	// waitingDetails marks a load postponed until the repository's remotes are known.
	waitingDetails bool
}

type forgeCommandSession interface {
	PullRequestTool(context.Context) (string, error)
	RunHostingCommand(context.Context, string, []string) (string, error)
}

func (a *app) resetChangeRequests() {
	a.changeRequests.waitingDetails = false
	a.changeRequests.provider = ""
	a.changeRequests.newURL, a.changeRequests.issuesURL = "", ""
	a.changeRequests.page, a.changeRequests.more = 0, false
	a.changeRequests.loadingMore, a.changeRequests.moreErr = false, ""
	a.changeRequests.token++
	a.changeRequests.items = nil
	a.changeRequests.selected = -1
	a.changeRequests.loading = false
	a.changeRequests.loaded = false
	a.changeRequests.err = ""
	a.changeRequests.commentDraft = ""
	a.changeRequests.replyDraft = ""
	a.changeRequests.replyFor = ""
	a.changeRequests.threadFlip = nil
	a.changeRequests.fileSearchOpen = false
	a.changeRequests.fileQuery = ""
	a.changeRequests.expandedFile = ""
	a.changeRequests.mutationBusy = false
	a.changeRequests.mutationErr = ""
	a.changeRequests.mutationToken++
	a.changeRequests.lineTarget = nil
	a.changeRequests.lineDraft = ""
	a.changeRequests.readOnly = false
	a.changeRequests.search = ""
	a.changeRequests.filters = changeRequestFilterValues{}
	a.changeRequests.filterOpen = false
	a.changeRequests.facets = forge.Facets{}
	a.changeRequests.facetsLoaded = false
	a.changeRequests.facetsLoading = false
	a.changeRequests.facetsToken++
	a.changeRequests.diffState, a.changeRequests.diffKey, a.changeRequests.diffErr = nil, "", ""
	a.resetChangeRequestDetail()
}

func (a *app) resetChangeRequestDetail() {
	a.changeRequests.detailToken++
	a.changeRequests.detail = nil
	a.changeRequests.detailFor = ""
	a.changeRequests.detailLoading = false
	a.changeRequests.detailErr = ""
}

// newChangeRequestURL is the page that starts a request, worked out from the
// address of a listed one: GitLab's .../-/merge_requests/N and GitHub's
// .../pull/N each sit beside theirs.
func newChangeRequestURL(items []forge.ChangeRequest) string {
	return projectPageURL(items, "/-/merge_requests/new", "/compare")
}

// issuesURL is the project's issue list, worked out the same way.
func issuesURL(items []forge.ChangeRequest) string {
	return projectPageURL(items, "/-/issues", "/issues")
}

// projectPageURL appends a page to the project address found in a listed
// request's link: gitlab for a GitLab project, github for a GitHub one.
func projectPageURL(items []forge.ChangeRequest, gitlab, github string) string {
	for _, item := range items {
		if i := strings.Index(item.URL, "/merge_requests/"); i > 0 {
			return strings.TrimSuffix(item.URL[:i], "/-") + gitlab
		}
		if i := strings.Index(item.URL, "/pull/"); i > 0 {
			return item.URL[:i] + github
		}
	}
	return ""
}

// requestReadOnly: the first phase only reads. Nothing here changes the
// remote, whatever the token allows.
func (a *app) requestReadOnly() bool { return true }

// requestNumberPill is the #number badge of a list row; it opens the request's
// page in the browser.
func (a *app) requestNumberPill(c *ui.Context, item forge.ChangeRequest, color ui.Color) {
	if item.URL == "" || !ui.SafeToOpen(item.URL) || a.openExternalURL == nil {
		pill(c, fmt.Sprintf("#%d", item.Number), color, nil)
		return
	}
	button := ui.ButtonBase(c).Padding(0).Shrink(0).Radius(9).
		Label(fmt.Sprintf("#%d", item.Number)).Tooltip(tr("Open this request in your browser"))
	button.Children(func() { pill(c, fmt.Sprintf("#%d", item.Number), color, nil) })
	if button.Clicked() {
		a.openExternalURL(item.URL)
	}
}

// changeRequestPage is how many requests one read of the list returns.
const changeRequestPage = 50

// loadMoreChangeRequests reads the page after the listed requests, as the list
// is scrolled to its end.
func (a *app) loadMoreChangeRequests() {
	cr := &a.changeRequests
	if a.session == nil || cr.loading || cr.loadingMore || !cr.more || cr.moreErr != "" {
		return
	}
	runner, ok := a.session.(forgeCommandSession)
	if !ok {
		return
	}
	query := a.changeRequestQuery()
	query.Page = cr.page + 1
	token, generation := cr.token, a.generation
	client := forge.NewCLIClient(runner)
	ctx := a.repositoryWork.Context()
	cr.loadingMore = true
	go func() {
		defer a.recoverBackground()
		items, err := client.Search(ctx, query, changeRequestPage)
		a.update(func() {
			if token != a.changeRequests.token || generation != a.generation {
				return
			}
			cr := &a.changeRequests
			cr.loadingMore = false
			if err != nil {
				cr.moreErr = changeRequestErrorMessage(err)
				return
			}
			seen := make(map[string]bool, len(cr.items))
			for _, item := range cr.items {
				seen[changeRequestKey(item)] = true
			}
			for _, item := range items {
				if !seen[changeRequestKey(item)] {
					cr.items = append(cr.items, item)
				}
			}
			cr.page = query.Page
			cr.more = len(items) >= changeRequestPage
		})
	}()
}

func (a *app) failChangeRequests(err error) {
	a.changeRequests.err = changeRequestErrorMessage(err)
	a.notifyError(err)
}

func (a *app) refreshChangeRequests() {
	if a.session == nil || a.changeRequests.loading {
		return
	}
	runner, ok := a.session.(forgeCommandSession)
	if !ok {
		a.changeRequests.loaded = true
		a.failChangeRequests(errors.New("This repository session cannot access hosting requests."))
		return
	}
	if a.repositoryDetailsLoading {
		// The remotes arrive with the details; matching the host before then fails.
		a.changeRequests.waitingDetails = true
		a.changeRequests.loading = true
		a.changeRequests.err = ""
		return
	}
	a.applyHostingHints(a.session)
	a.changeRequests.token++
	token := a.changeRequests.token
	generation := a.generation
	client := forge.NewCLIClient(runner)
	ctx := a.repositoryWork.Context()
	query := a.changeRequestQuery()
	a.changeRequests.loading = true
	a.changeRequests.err = ""
	a.resetChangeRequestDetail()
	go func() {
		defer a.recoverBackground()
		items, err := client.Search(ctx, query, changeRequestPage)
		provider := forge.Provider("")
		if len(items) > 0 {
			provider = items[0].Provider
		} else if tool, toolErr := runner.PullRequestTool(ctx); toolErr == nil {
			switch tool {
			case "gh":
				provider = forge.GitHub
			case "glab":
				provider = forge.GitLab
			}
		}
		caps, capsErr := client.Capabilities(ctx)
		a.update(func() {
			if token != a.changeRequests.token || generation != a.generation {
				return
			}
			a.changeRequests.readOnly = capsErr == nil && !caps.CanWrite
			if provider != "" {
				a.changeRequests.provider = provider
			}
			a.changeRequests.loading = false
			a.changeRequests.loaded = true
			if err != nil {
				a.failChangeRequests(err)
				return
			}
			a.changeRequests.items = items
			a.changeRequests.page = 1
			a.changeRequests.more = len(items) >= changeRequestPage
			a.changeRequests.loadingMore, a.changeRequests.moreErr = false, ""
			if address := newChangeRequestURL(items); address != "" {
				a.changeRequests.newURL = address
			}
			if address := issuesURL(items); address != "" {
				a.changeRequests.issuesURL = address
			}
			if len(items) == 0 {
				a.changeRequests.selected = -1
			} else if a.changeRequests.selected < 0 || a.changeRequests.selected >= len(items) {
				a.changeRequests.list.Choose(0)
			}
			if a.changeRequests.selected >= 0 && a.changeRequests.selected < len(items) {
				a.loadChangeRequestDetail(items[a.changeRequests.selected], false)
			}
		})
	}()
}

// changeRequestFilterValues are the advanced filters, as typed.
type changeRequestFilterValues struct {
	Author, Assignee, Reviewer, Source, Target, Label, After, Before string
	Draft                                                            int
	OrderBy                                                          forge.SortField
	Sort                                                             forge.SortDirection
}

func (v changeRequestFilterValues) active() int {
	count := 0
	for _, text := range []string{v.Author, v.Assignee, v.Reviewer, v.Source, v.Target, v.Label, v.After, v.Before} {
		if strings.TrimSpace(text) != "" {
			count++
		}
	}
	if v.Draft != 0 {
		count++
	}
	return count
}

func (a *app) changeRequestQuery() forge.Query {
	v := a.changeRequests.filters
	orderBy := v.OrderBy
	if orderBy == "" {
		orderBy = forge.SortUpdated
	}
	direction := v.Sort
	if direction == "" {
		direction = forge.SortDescending
	}
	a.changeRequests.searchApplied = strings.TrimSpace(a.changeRequests.search)
	searchText, typed := parseChangeRequestSearch(a.changeRequests.search)
	pick := func(dialog, written string) string {
		if dialog = strings.TrimSpace(dialog); dialog != "" {
			return dialog
		}
		return written
	}
	draft := v.Draft
	if draft == 0 {
		draft = typed.Draft
	}
	return forge.Query{
		State: changeRequestFilters[a.changeRequests.filter].filter, Text: searchText,
		Author: pick(v.Author, typed.Author), Assignee: pick(v.Assignee, typed.Assignee), Reviewer: pick(v.Reviewer, typed.Reviewer),
		SourceBranch: pick(v.Source, typed.Source), TargetBranch: pick(v.Target, typed.Target), Label: pick(v.Label, typed.Label),
		UpdatedAfter: pick(v.After, typed.After), UpdatedBefore: pick(v.Before, typed.Before), Draft: forge.DraftFilter(draft),
		OrderBy: orderBy, Sort: direction,
	}
}

var changeRequestFilters = []struct {
	filter forge.StateFilter
	label  string
}{
	{forge.FilterOpen, "Open"}, {forge.FilterMerged, "Merged"}, {forge.FilterClosed, "Closed"}, {forge.FilterAll, "All"},
}

// chooseChangeRequestFilter switches the list tab and asks the provider for it.
func (a *app) chooseChangeRequestFilter() {
	a.changeRequests.loading = false // drops the answer still on its way for the old tab
	a.changeRequests.items = nil
	a.changeRequests.selected = -1
	a.changeRequests.page, a.changeRequests.more = 0, false
	a.changeRequests.loadingMore, a.changeRequests.moreErr = false, ""
	a.changeRequests.loaded = false
	a.changeRequests.err = ""
	// The next list starts from its top, not where the last one was scrolled.
	a.changeRequests.list.ScrollTo(0, ui.Start)
	a.resetChangeRequestDetail()
	a.refreshChangeRequests()
}

// changeRequestErrorMessage turns "cannot tell the provider" into what to do
// about it; any other failure keeps its own (redacted) text.
func changeRequestErrorMessage(err error) string {
	if strings.Contains(err.Error(), "pull request actions support") {
		return tr("This repository's git remote uses a host the app cannot match to GitHub or GitLab. Open “Connect host”, enter your server address, and put this remote's host name in “Git remote host”.")
	}
	return safeError(err)
}

func changeRequestKey(item forge.ChangeRequest) string {
	return fmt.Sprintf("%s:%d", item.Provider, item.Number)
}

func (a *app) loadSelectedChangeRequestDetail(force bool) {
	if item, ok := a.selectedChangeRequest(); ok {
		a.loadChangeRequestDetail(item, force)
	}
}

func (a *app) loadChangeRequestDetail(item forge.ChangeRequest, force bool) {
	if a.session == nil {
		return
	}
	key := changeRequestKey(item)
	if !force && a.changeRequests.detailFor == key && (a.changeRequests.detailLoading || a.changeRequests.detail != nil) {
		return
	}
	runner, ok := a.session.(forgeCommandSession)
	if !ok {
		a.changeRequests.detailErr = "This repository session cannot access hosting request details."
		return
	}
	a.changeRequests.detailToken++
	token := a.changeRequests.detailToken
	generation := a.generation
	client := forge.NewCLIClient(runner)
	ctx := a.repositoryWork.Context()
	a.changeRequests.detailFor = key
	a.changeRequests.detail = nil
	a.changeRequests.detailLoading = true
	a.changeRequests.detailErr = ""
	go func() {
		defer a.recoverBackground()
		detail, err := client.Get(ctx, item)
		a.update(func() {
			if token != a.changeRequests.detailToken || generation != a.generation || a.changeRequests.detailFor != key {
				return
			}
			a.changeRequests.detailLoading = false
			if err != nil {
				a.changeRequests.detailErr = safeError(err)
				return
			}
			a.changeRequests.detail = &detail
		})
	}()
}

func (a *app) beginChangeRequestMutation(mutation forge.Mutation) {
	item, ok := a.selectedChangeRequest()
	if !ok || a.session == nil || a.changeRequests.mutationBusy {
		return
	}
	runner, ok := a.session.(forgeCommandSession)
	if !ok {
		a.changeRequests.mutationErr = "This repository session cannot update hosting requests."
		return
	}
	a.changeRequests.mutationToken++
	token := a.changeRequests.mutationToken
	generation := a.generation
	key := changeRequestKey(item)
	client := forge.NewCLIClient(runner)
	ctx := a.repositoryWork.Context()
	a.changeRequests.mutationBusy = true
	a.changeRequests.mutationErr = ""
	go func() {
		defer a.recoverBackground()
		err := client.Mutate(ctx, item, mutation)
		a.update(func() {
			if token != a.changeRequests.mutationToken || generation != a.generation {
				return
			}
			a.changeRequests.mutationBusy = false
			selected, selectedOK := a.selectedChangeRequest()
			if !selectedOK || changeRequestKey(selected) != key {
				return
			}
			if err != nil {
				a.changeRequests.mutationErr = safeError(err)
				return
			}
			switch mutation.Kind {
			case forge.MutationComment:
				a.changeRequests.commentDraft = ""
			case forge.MutationReply:
				a.changeRequests.replyDraft = ""
				a.changeRequests.replyFor = ""
			case forge.MutationLineComment:
				a.changeRequests.lineTarget = nil
				a.changeRequests.lineDraft = ""
			}
			a.loadChangeRequestDetail(item, true)
		})
	}()
}

func (a *app) selectedChangeRequest() (forge.ChangeRequest, bool) {
	row := a.changeRequests.selected
	if row < 0 || row >= len(a.changeRequests.items) {
		return forge.ChangeRequest{}, false
	}
	return a.changeRequests.items[row], true
}

func changeRequestProviderName(provider forge.Provider) string {
	if provider == forge.GitLab {
		return "GitLab"
	}
	return "GitHub"
}

func changeRequestKind(provider forge.Provider) string {
	if provider == forge.GitLab {
		return "MR"
	}
	return "PR"
}

const requestPartChecks = "checks"

const (
	requestPartDescription = "description"
	requestPartDiscussions = "discussions"
	requestPartActivity    = "activity"
	requestPartCommits     = "commits"
	requestPartChanges     = "changes"
	requestPartSidebar     = "sidebar"
)

var requestParts = []struct{ id, label string }{
	{requestPartDescription, "Description"},
	{requestPartDiscussions, "Discussions"},
	{requestPartActivity, "System activity"},
	{requestPartCommits, "Commits"},
	{requestPartChanges, "Changed files"},
	{requestPartSidebar, "People and labels"},
}

func (a *app) requestPartVisible(id string) bool {
	return !slices.Contains(a.settings.HiddenRequestParts, id)
}

func (a *app) setRequestPartVisible(id string, visible bool) {
	hidden := slices.DeleteFunc(slices.Clone(a.settings.HiddenRequestParts), func(part string) bool { return part == id })
	if !visible {
		hidden = append(hidden, id)
	}
	a.settings.HiddenRequestParts = hidden
	a.saveSettings()
}

// openRequestLink opens a link written in a request's Markdown. Relative links
// (uploads, "/group/project/-/issues/3") resolve against the request's server.
func (a *app) requestHostingAccount() (hostingAccount, bool) {
	item, ok := a.selectedChangeRequest()
	if !ok {
		return hostingAccount{}, false
	}
	host := hostingHostname(item.URL)
	for _, account := range a.settings.HostingAccounts {
		if hostingHostname(account.URL) == host || slices.Contains(account.Aliases, host) {
			return account, true
		}
	}
	return hostingAccount{}, false
}

// requestIssueLink is the description-link template configured separately
// from the toolbar's project issue-list page.
func (a *app) requestIssueLink() string {
	if account, ok := a.requestHostingAccount(); ok {
		return account.IssueLink
	}
	return ""
}

func (a *app) requestIssuesURL() string {
	if account, ok := a.requestHostingAccount(); ok && account.IssuesURL != "" {
		return account.IssuesURL
	}
	return a.changeRequests.issuesURL
}

var issueNumberPattern = regexp.MustCompile(`(^|[^\w&/#\[(])#(\d{3,})\b`)

// linkIssueNumbers turns each bare #1234 outside code into a Markdown link
// built from template. Without a template the text is left alone.
func linkIssueNumbers(text, template string) string {
	if template == "" || !strings.Contains(text, "#") {
		return text
	}
	build := func(id string) string {
		if strings.Contains(template, "{id}") {
			return strings.ReplaceAll(template, "{id}", id)
		}
		return template + id
	}
	lines := strings.Split(text, "\n")
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		parts := strings.Split(line, "`")
		for j := 0; j < len(parts); j += 2 { // odd parts are inline code
			parts[j] = issueNumberPattern.ReplaceAllStringFunc(parts[j], func(match string) string {
				sub := issueNumberPattern.FindStringSubmatch(match)
				address := strings.NewReplacer("(", "%28", ")", "%29", " ", "%20").Replace(build(sub[2]))
				if !ui.SafeToOpen(address) {
					return match
				}
				return sub[1] + "[#" + sub[2] + "](" + address + ")"
			})
		}
		lines[i] = strings.Join(parts, "`")
	}
	return strings.Join(lines, "\n")
}

func (a *app) openRequestLink(link string) {
	if a.openExternalURL == nil || strings.HasPrefix(link, "#") {
		return
	}
	target, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return
	}
	if item, ok := a.selectedChangeRequest(); ok && !target.IsAbs() {
		if base, err := url.Parse(item.URL); err == nil {
			target = base.ResolveReference(target)
		}
	}
	if resolved := target.String(); ui.SafeToOpen(resolved) {
		a.openExternalURL(resolved)
	}
}

// requestMarkdown shows text rendered, or as the raw source when the user
// switched the code toggle on. Links are clickable in the rendered form.
func (a *app) requestMarkdown(c *ui.Context, key, text string) {
	if a.changeRequests.rawMarkdown {
		c.Key("raw:" + key)
		ui.Text(c, text).Selectable().Font(a.settings.BufferFont).FontSize(float32(a.settings.BufferFontSize))
		return
	}
	a.changeRequests.md.OnLink = a.openRequestLink
	c.Key("md:" + key)
	markdown.View(c, linkIssueNumbers(text, a.requestIssueLink()), &a.changeRequests.md)
}

func requestStateLabel(item forge.ChangeRequest) string {
	switch item.State {
	case forge.StateMerged:
		return tr("Merged")
	case forge.StateClosed:
		return tr("Closed")
	}
	return tr("Open")
}

func (a *app) requestStateColor(c *ui.Context, state forge.State) ui.Color {
	theme := c.Theme()
	switch state {
	case forge.StateMerged:
		return theme.Accent
	case forge.StateClosed:
		return theme.Danger
	}
	return theme.Success
}

func (a *app) changeRequestsView(c *ui.Context) {
	theme := c.Theme()
	issuesURL := a.requestIssuesURL()
	a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionPullRequests],
		"Expand list pane", func() {
			islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Icon(c, pullRequestIcon).FontSize(14).TextColor(theme.Accent)
					ui.Text(c, tr("PR/MR")).Bold().Tooltip(tr("Pull request or merge request"))
					if len(a.changeRequests.items) > 0 {
						changeRequestCountBadge(c, len(a.changeRequests.items), a.changeRequests.more)
					}
					ui.Spacer(c).Grow(1)
					flatIconButton(c, "New pull request", "Open the page that creates a pull or merge request",
						plusIcon, 14, 3, a.changeRequests.newURL == "" || a.openExternalURL == nil, func() {
							a.openExternalURL(a.changeRequests.newURL)
						})
					flatIconButton(c, "Issues", "Open this project's issues in your browser",
						issueIcon, 14, 3, issuesURL == "" || a.openExternalURL == nil, func() {
							a.openExternalURL(issuesURL)
						})
					flatIconButton(c, "Connect host", "Set the server address and access token", settingsIcon, 14, 3,
						false, a.openHostingAccess)
					flatIconButton(c, "Refresh pull requests", "Reload pull and merge requests", refreshIcon, 14, 3,
						a.changeRequests.loading, a.refreshChangeRequests)
					paneCollapseButton(c, &a.workspacePaneCollapsed[sectionPullRequests], "Collapse list pane")
				})
				labels := make([]string, len(changeRequestFilters))
				for i, entry := range changeRequestFilters {
					labels[i] = tr(entry.label)
				}
				filterTabs := ui.Tabs(c, &a.changeRequests.filter, labels...).Label(tr("Request state"))
				if filterTabs.Changed() {
					a.chooseChangeRequestFilter()
				}
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.SearchField(c.Key("request-search"), &a.changeRequests.search).
						Label(tr("Search pull requests")).
						Placeholder(tr("Search title or description, or @user for an author, then press Enter")).Grow(1).
						OnSubmit(a.chooseChangeRequestFilter)
					// Emptying the box (backspace or the clear button) drops the
					// search at once, without waiting for Enter.
					if strings.TrimSpace(a.changeRequests.search) == "" && a.changeRequests.searchApplied != "" {
						a.chooseChangeRequestFilter()
					}
					commitFilterButton(c, "Filter by author, branch, label or date",
						a.changeRequests.filters.active() > 0, a.openChangeRequestFilters)
				})
				if a.changeRequests.loading && len(a.changeRequests.items) == 0 {
					loadingPane(c, "Fetching", "Fetching…")
					return
				}
				if a.changeRequests.err != "" {
					ui.Column(c).Grow(1).Center().Gap(10).Padding(24).Children(func() {
						ui.Text(c, tr("Check your PR/MR host configuration, then click Refresh to try again.")).TextColor(theme.TextMuted).TextAlign(ui.Center)
						if ui.Button(c, tr("Config PR/MR host")).Clicked() {
							a.openHostingAccess()
						}
					})
					return
				}
				if a.changeRequests.loaded && len(a.changeRequests.items) == 0 {
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("No pull or merge requests found")).TextColor(theme.TextMuted)
					})
					return
				}
				a.changeRequests.list.NearEnd = 2
				a.changeRequests.list.EndReached = func(int) { a.loadMoreChangeRequests(); c.Invalidate() }
				list := a.keyboardList(c, &a.changeRequests.list, len(a.changeRequests.items)).Grow(1).Gap(4).Rows(func(row ui.ListRow) {
					item := a.changeRequests.items[row.Index]
					rc := row.Context
					primary := theme.Text
					if row.Selected() {
						primary = theme.AccentText
					}
					services := rc.Services()
					muted := secondaryRowColor(theme, row)
					// The chosen row's fill reaches the card's edge: a dark outline there
					// would read as a gap between the fill and the card.
					cardBorder := theme.Border.Alpha(0.6)
					if row.Selected() {
						cardBorder = theme.Accent
					}
					// On the chosen row the accent fill is the background, so the badges
					// take its contrast color instead of their own.
					tone := func(own ui.Color) ui.Color {
						if row.Selected() {
							return theme.AccentText
						}
						return own
					}
					// The card is the list row itself, so the chosen row's fill stops at
					// the card's border; the gap between cards belongs to the list.
					ui.Row(rc).FillWidth().AlignItems(ui.Stretch).Padding(0, 10+scrollbarGutter(theme), 0, 0).Clip().
						Border(1, cardBorder).Radius(theme.Radius + 2).Children(func() {
						// On the All tab a colored bar fills the card's left edge and is
						// clipped by its rounded corners, so there is no gap around it.
						// The other tabs already say the state, so they have no bar. It
						// keeps its own color on the chosen row, where a contrast color
						// would vanish.
						if changeRequestFilters[a.changeRequests.filter].filter == forge.FilterAll {
							ui.Box(rc).Width(4).Shrink(0).Background(a.requestStateColor(rc, item.State)).Tooltip(requestStateLabel(item))
						}
						ui.Row(rc).Grow(1).MinWidth(0).Gap(9).AlignItems(ui.Start).Padding(7, 0, 7, 10).Children(func() {
							if item.Author != "" {
								a.personAvatar(rc, item.Author, item.AuthorAvatar, 26)
							}
							ui.Column(rc).Grow(1).MinWidth(0).Gap(3).Children(func() {
								ui.Text(rc, requestListTitle(item)).Bold().SingleLine().TextColor(primary).Tooltip(item.Title)
								ui.Row(rc).Gap(5).Wrap().AlignItems(ui.Center).Children(func() {
									a.requestNumberPill(rc, item, tone(theme.TextMuted))
									if item.Draft {
										pill(rc, tr("Draft"), tone(theme.TextMuted), requestDraftIcon)
									}
									who := item.Author
									if age := relativeTime(item.UpdatedAt); age != "" {
										who = strings.TrimSpace(who + " · " + age)
									}
									if who != "" {
										ui.Text(rc, who).SingleLine().TextColor(muted).FontSize(theme.FontSize - 1)
									}
								})
								// Branches are badges; a long name is cut in its middle, where
								// the part that tells branches apart usually is.
								ui.Row(rc).Gap(5).AlignItems(ui.Center).Children(func() {
									a.monoChip(rc, middleEllipsis(item.SourceBranch, 26), tone(theme.Accent))
									ui.Text(rc, "→").TextColor(muted).FontSize(theme.FontSize - 2)
									// The usual target fades; an unusual one (a release branch) shows.
									if isDefaultBranchName(item.TargetBranch) {
										a.monoChip(rc, item.TargetBranch, tone(theme.TextMuted))
									} else {
										a.monoChip(rc, item.TargetBranch, tone(theme.Accent))
									}
								})
							})
						})
					}).PopoverMenu(func(m *ui.Menu) { a.changeRequestRowMenu(m, row.Index, services) })
				})
				if a.keyboardListChanged(&a.changeRequests.list, list) {
					a.loadSelectedChangeRequestDetail(false)
				}
				switch {
				case a.changeRequests.loadingMore:
					ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
						ui.Spinner(c).Label(tr("Fetching"))
						ui.Text(c, tr("Fetching…")).TextColor(theme.TextMuted)
					})
				case a.changeRequests.moreErr != "":
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, a.changeRequests.moreErr).TextColor(theme.Danger).SingleLine().Grow(1).
							Tooltip(a.changeRequests.moreErr)
						if ui.Button(c, tr("Retry")).Clicked() {
							a.changeRequests.moreErr = ""
							a.loadMoreChangeRequests()
						}
					})
				}
			})
		}, func() {
			islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
				item, ok := a.selectedChangeRequest()
				if !ok {
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("Select a pull or merge request")).TextColor(theme.TextMuted)
					})
					return
				}
				a.changeRequestHeader(c, item)
				if a.changeRequests.detailLoading {
					loadingPane(c, "Loading request details", "Reading files, commits and discussions.")
					return
				}
				if a.changeRequests.detailErr != "" {
					ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
						ui.Text(c, a.changeRequests.detailErr).TextColor(theme.Danger).TextAlign(ui.Center)
						if ui.Button(c, tr("Retry")).Clicked() {
							a.loadSelectedChangeRequestDetail(true)
						}
					})
					return
				}
				if a.changeRequests.detail != nil {
					a.changeRequestTabs(c, *a.changeRequests.detail)
				}
			})
		})
}

func changeRequestCountBadge(c *ui.Context, count int, more bool) {
	ui.Badge(c, strconv.Itoa(count)).Tooltip(changeRequestCountTooltip(count, more))
}

func changeRequestCountTooltip(count int, more bool) string {
	tooltip := trf("%d pull or merge requests shown.", count)
	if more {
		tooltip = trf("%d pull or merge requests loaded so far. Scroll down to load more.", count)
	}
	return tooltip
}

// changeRequestHeader follows GitLab's own page: kind and number, a wrapping
// title, a row of status badges, then who, from where, and when.
func (a *app) changeRequestHeader(c *ui.Context, item forge.ChangeRequest) {
	theme := c.Theme()
	approveHead := ""
	if a.changeRequests.detail != nil {
		approveHead = a.changeRequests.detail.HeadSHA
	}
	ui.Row(c).AlignItems(ui.Start).Gap(8).Children(func() {
		ui.Column(c).Grow(1).Gap(4).Children(func() {
			a.underlinedLink(c, fmt.Sprintf("%s #%d", changeRequestKind(item.Provider), item.Number),
				tr("Open this request in your browser"), item.URL)
			ui.Text(c, item.Title).FontSize(19).Bold().Selectable()
		})
		flatIconButton(c, "Open in browser", "Open this request on "+changeRequestProviderName(item.Provider), externalLinkIcon, 15, 4,
			item.URL == "" || a.openExternalURL == nil, func() { a.openExternalURL(item.URL) })
		flatIconButton(c, "Refresh request details", "Reload files, commits and discussions", refreshIcon, 15, 4,
			a.changeRequests.detailLoading, func() { a.loadSelectedChangeRequestDetail(true) })
		flatIconButton(c, "Display options", "Choose which parts to show", settingsIcon, 15, 4,
			false, func() { a.changeRequests.showDisplay = !a.changeRequests.showDisplay })
		if !a.requestReadOnly() {
			if ui.PrimaryButton(c, tr("Approve")).Disabled(a.changeRequests.mutationBusy || item.State != forge.StateOpen || approveHead == "").Clicked() {
				a.beginChangeRequestMutation(forge.Mutation{Kind: forge.MutationApprove, HeadSHA: approveHead})
			}
		}
	})
	ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
		a.requestStatePill(c, item)
		if item.Draft {
			pill(c, tr("Draft"), theme.TextMuted, requestDraftIcon)
		}
		for _, label := range item.Labels {
			c.Key("label:" + label)
			a.requestLabel(c, label)
		}

	})
	ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
		if item.Author != "" {
			a.personAvatar(c, item.Author, item.AuthorAvatar, 22)
			ui.Text(c, item.Author).Bold()
		}
		ui.Text(c, tr("requested to merge")).TextColor(theme.TextMuted)
		a.branchChip(c, item, item.SourceBranch, true)
		ui.Text(c, tr("into")).TextColor(theme.TextMuted)
		a.branchChip(c, item, item.TargetBranch, false)
		if age := relativeTime(item.UpdatedAt); age != "" {
			ui.Text(c, age).TextColor(theme.TextMuted).Tooltip(requestTime(item.UpdatedAt))
		}
	})
}

func (a *app) changeRequestDisplayOptions(c *ui.Context) {
	if !a.changeRequests.showDisplay {
		return
	}
	theme := c.Theme()
	closeModal := func() { a.changeRequests.showDisplay = false }
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(380).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 8).
				Role(ui.RoleDialog).Label(tr("Display options"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, settingsIcon).FontSize(18).TextColor(theme.Accent)
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Display options")).FontSize(18).Bold()
						ui.Text(c, tr("Choose which parts to show")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close", "Escape", closeIcon, false, closeModal)
				})
				ui.Column(c).FillWidth().Gap(10).Padding(4, 0).Children(func() {
					for _, part := range requestParts {
						part := part
						visible := a.requestPartVisible(part.id)
						c.Key("display:" + part.id)
						if ui.Checkbox(c, &visible, tr(part.label)).Changed() {
							a.setRequestPartVisible(part.id, visible)
						}
					}
				})
				ui.Row(c).Justify(ui.End).Children(func() {
					iconTextButton(c, "Done", "Close", commandSuccessIcon, true, closeModal)
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				closeModal()
			}
		})
	})
}

type requestTab struct {
	id    string
	label string
	count int
}

// changeRequestTabs is GitLab's Overview / Commits / Changes strip, each with
// a count. Parts the user turned off leave the strip or the Overview.
func (a *app) changeRequestTabs(c *ui.Context, detail forge.Detail) {
	theme := c.Theme()
	tabs := []requestTab{{id: "overview", label: tr("Overview"), count: userDiscussionCount(detail.Discussions)}}
	if a.requestPartVisible(requestPartCommits) {
		tabs = append(tabs, requestTab{id: requestPartCommits, label: tr("Commits"), count: len(detail.Commits)})
	}
	if a.requestPartVisible(requestPartChanges) {
		tabs = append(tabs, requestTab{id: requestPartChanges, label: tr("Changes"), count: len(detail.Files)})
	}
	if len(detail.Checks) > 0 {
		tabs = append(tabs, requestTab{id: requestPartChecks, label: tr("Checks"), count: len(detail.Checks)})
	}
	if a.changeRequests.tab >= len(tabs) {
		a.changeRequests.tab = 0
	}
	strip := ui.TabsBase(c, &a.changeRequests.tab, len(tabs))
	strip.List.Gap(2).FillWidth().AlignItems(ui.Center).Children(func() {
		for i, tab := range tabs {
			selected := i == a.changeRequests.tab
			element := strip.Tab(i).Padding(0, 12).Radius(0)
			element.Children(func() {
				ui.Column(c).Gap(0).Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(8, 0).Children(func() {
						text := ui.Text(c, tab.label)
						if selected {
							text.Bold()
						} else {
							text.TextColor(theme.TextMuted)
						}
						if tab.count > 0 {
							ui.Badge(c, fmt.Sprintf("%d", tab.count))
						}
					})
					line := ui.Box(c).FillWidth().Height(2)
					if selected {
						line.Background(theme.Accent)
					}
				})
			})
		}
		if detail.MergeStatus != "" {
			ui.Box(c).Grow(1)
			pill(c, decisionLabel(detail.MergeStatus), decisionColor(c, detail.MergeStatus), nil)
		}
	})
	ui.Divider(c)
	if tabs[a.changeRequests.tab].id == requestPartChanges {
		if a.changeRequests.mutationErr != "" {
			ui.Text(c, a.changeRequests.mutationErr).TextColor(theme.Danger)
		}
		if a.changeRequests.diffMax {
			// One diff state is drawn at a time: the full-window view has it now.
			ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
				ui.Text(c, tr("The changes are open in the drawer.")).TextColor(theme.TextMuted)
				if ui.Button(c, tr("Close the changes")).Clicked() {
					a.changeRequests.diffMax = false
				}
			})
			return
		}
		a.changeRequestFiles(c, detail, false)
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(14).Padding(2, 0, 12).Children(func() {
			if a.changeRequests.mutationErr != "" {
				ui.Text(c, a.changeRequests.mutationErr).TextColor(theme.Danger)
			}
			switch tabs[a.changeRequests.tab].id {
			case requestPartCommits:
				a.changeRequestCommits(c, detail)
			case requestPartChecks:
				a.changeRequestChecks(c, detail)
			default:
				ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(14).Children(func() { a.changeRequestOverview(c, detail) })
					if a.requestPartVisible(requestPartSidebar) {
						a.changeRequestSidebar(c, detail)
					}
				})
			}
		})
	})
}

func (a *app) changeRequestOverview(c *ui.Context, detail forge.Detail) {
	theme := c.Theme()
	if a.requestPartVisible(requestPartDescription) {
		ui.Column(c).FillWidth().Gap(8).Padding(12).Border(1, theme.Border).Radius(theme.Radius + 2).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, tr("Description")).Bold().Grow(1)
				label, tip := "Show raw Markdown", "Show the Markdown source"
				if a.changeRequests.rawMarkdown {
					label, tip = "Show rendered Markdown", "Show the rendered description"
				}
				flatIconButton(c, label, tip, rawMarkdownIcon, 15, 4, false,
					func() { a.changeRequests.rawMarkdown = !a.changeRequests.rawMarkdown })
			})
			if strings.TrimSpace(detail.Description) == "" {
				ui.Text(c, tr("No description provided")).TextColor(theme.TextMuted)
			} else {
				a.requestMarkdown(c, "description", detail.Description)
			}
		})
	}
	if !a.requestReadOnly() {
		c.Key("request-comment-composer")
		ui.Column(c).FillWidth().Gap(7).Padding(10).Border(1, theme.Border).Radius(theme.Radius + 2).Children(func() {
			ui.TextArea(c, &a.changeRequests.commentDraft).Label(tr("Add comment")).
				Placeholder(tr("Write a comment using Markdown")).Height(72)
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, tr("Post comment")).
					Disabled(a.changeRequests.mutationBusy || strings.TrimSpace(a.changeRequests.commentDraft) == "").Clicked() {
					a.beginChangeRequestMutation(forge.Mutation{Kind: forge.MutationComment, Body: a.changeRequests.commentDraft})
				}
			})
		})
	}
	if a.requestPartVisible(requestPartDiscussions) {
		a.changeRequestDiscussions(c, detail)
	}
}

// browserLink is a quiet text button that opens a page on the hosting site.
func (a *app) browserLink(c *ui.Context, label, address string) {
	iconTextButtonState(c, label, "Open in your browser", externalLinkIcon, false,
		address == "" || a.openExternalURL == nil, func() { a.openExternalURL(address) })
}

// underlinedLink is a short underlined word that opens a page in the browser,
// with the full description as its tooltip.
func (a *app) underlinedLink(c *ui.Context, label, tooltip, address string) {
	if address == "" || !ui.SafeToOpen(address) {
		ui.Text(c, label).TextColor(c.Theme().TextMuted)
		return
	}
	ui.Link(c, label, address).Tooltip(tooltip)
}

func sha1Hex(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}

// requestFileURL jumps to one file's diff on the request's changes page, using
// the anchor each site gives that file.
func requestFileURL(item forge.ChangeRequest, path string) string {
	section := requestSectionURL(item, "changes")
	if section == "" {
		return ""
	}
	if _, gitlab := requestWebBase(item.URL); gitlab {
		return section + "#" + sha1Hex(path)
	}
	sum := sha256.Sum256([]byte(path))
	return section + "#diff-" + hex.EncodeToString(sum[:])
}

// changeRequestChecks lists the CI runs of the head commit, worst first.
func (a *app) changeRequestChecks(c *ui.Context, detail forge.Detail) {
	theme := c.Theme()
	rank := func(check forge.Check) int {
		switch {
		case check.Status != "completed":
			return 1
		case check.Conclusion == "failure" || check.Conclusion == "timed_out" || check.Conclusion == "cancelled" || check.Conclusion == "action_required":
			return 0
		case check.Conclusion == "success":
			return 3
		}
		return 2
	}
	checks := slices.Clone(detail.Checks)
	sort.SliceStable(checks, func(i, j int) bool { return rank(checks[i]) < rank(checks[j]) })
	failed, running := 0, 0
	for _, check := range checks {
		switch rank(check) {
		case 0:
			failed++
		case 1:
			running++
		}
	}
	summary := trf("%d checks", len(checks))
	switch {
	case failed > 0:
		summary = trf("%d failing, %d total", failed, len(checks))
	case running > 0:
		summary = trf("%d running, %d total", running, len(checks))
	}
	ui.Text(c, summary).TextColor(theme.TextMuted)
	for i, check := range checks {
		check, i := check, i
		c.Key(fmt.Sprintf("check:%d:%s", i, check.Name))
		color, label := theme.TextMuted, check.Conclusion
		switch rank(check) {
		case 0:
			color = theme.Danger
		case 1:
			color, label = theme.Warning, strings.ReplaceAll(check.Status, "_", " ")
		case 3:
			color = theme.Success
		}
		label = strings.ReplaceAll(label, "_", " ")
		card := ui.ButtonBase(c).FillWidth().Gap(10).AlignItems(ui.Center).Padding(7, 10).
			Border(1, theme.Border).Radius(theme.Radius).Tooltip(tr("Open check in browser")).
			Disabled(check.URL == "" || a.openExternalURL == nil)
		if card.Hovered() {
			card.Background(theme.SurfaceHover)
		}
		card.Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, check.Name).SingleLine().Tooltip(check.Name)
				if sub := strings.TrimSpace(check.Summary); sub != "" {
					ui.Text(c, sub).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().Tooltip(sub)
				} else if check.App != "" {
					ui.Text(c, check.App).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine()
				}
			})
			pill(c, label, color, nil)
			ui.Icon(c, externalLinkIcon).FontSize(12).TextColor(theme.TextMuted)
		})
		if card.Clicked() && check.URL != "" && a.openExternalURL != nil {
			a.openExternalURL(check.URL)
		}
	}
}

func (a *app) changeRequestCommits(c *ui.Context, detail forge.Detail) {
	theme := c.Theme()
	item, _ := a.selectedChangeRequest()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(0, scrollbarGutter(theme), 0, 0).Children(func() {
		ui.Text(c, fmt.Sprintf("%d", len(detail.Commits))+" "+tr("Commits")).TextColor(theme.TextMuted).Grow(1)
		a.underlinedLink(c, tr("All"), tr("All commits in browser"), requestSectionURL(item, "commits"))
	})
	if len(detail.Commits) == 0 {
		ui.Text(c, tr("No commits reported")).TextColor(theme.TextMuted)
	}
	// The cards sit close together in their own column: the page's wide gap is
	// for its sections, and a long history would otherwise be mostly empty space.
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		for _, commit := range detail.Commits {
			commit := commit
			c.Key("review-commit:" + commit.SHA)
			target := requestCommitURL(item, commit.SHA)
			card := ui.ButtonBase(c).FillWidth().Gap(10).AlignItems(ui.Center).Padding(5, 10).
				Border(1, theme.Border).Radius(theme.Radius).Tooltip(tr("Open commit in browser")).
				Disabled(target == "" || a.openExternalURL == nil)
			if card.Hovered() {
				card.Background(theme.SurfaceHover)
			}
			card.Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, commit.Title).SingleLine().Tooltip(commit.Title)
					ui.Text(c, commit.Author).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				})
				if len(commit.SHA) >= 8 {
					ui.Text(c, commit.SHA[:8]).TextColor(theme.TextMuted).Font(a.settings.BufferFont).FontSize(theme.FontSize - 1)
				}
				ui.Icon(c, externalLinkIcon).FontSize(12).TextColor(theme.TextMuted)
			})
			if card.Clicked() && target != "" && a.openExternalURL != nil {
				a.openExternalURL(target)
			}
		}
	})
}

// requestFullPatch gives a provider's per-file hunks the git header the shared
// diff viewer expects.
func requestFullPatch(file forge.ChangedFile) string {
	if strings.HasPrefix(file.Patch, "diff --git ") || strings.HasPrefix(file.Patch, "--- ") {
		return file.Patch
	}
	oldPath := file.PreviousPath
	if oldPath == "" {
		oldPath = file.Path
	}
	from, to := "a/"+oldPath, "b/"+file.Path
	header := "diff --git " + from + " " + to + "\n"
	switch strings.ToLower(file.Status) {
	case "added":
		header += "new file mode 100644\n"
		from = "/dev/null"
	case "deleted", "removed":
		header += "deleted file mode 100644\n"
		to = "/dev/null"
	}
	patch := file.Patch
	if !strings.HasSuffix(patch, "\n") {
		patch += "\n"
	}
	return header + "--- " + from + "\n+++ " + to + "\n" + patch
}

// requestDiffState builds, once per file, the viewer state for a request's file.
func (a *app) requestDiffState(item forge.ChangeRequest, file forge.ChangedFile) *editor.DiffState {
	state := &a.changeRequests
	key := fmt.Sprintf("%s|%s|%d", changeRequestKey(item), file.Path, len(file.Patch))
	if state.diffKey == key {
		return state.diffState
	}
	state.diffKey, state.diffState, state.diffErr = key, nil, ""
	built, err := buildDiffState(a.diffLayout(), a.diffWrap, repository.File{Path: file.Path},
		repository.Diff{Patch: requestFullPatch(file), Kind: "Request"}, repository.DiffOptions{ContextLines: 3})
	if err != nil {
		state.diffErr = safeError(err)
		return nil
	}
	if built != nil {
		built.Font = selectedBufferFont(a.settings.BufferFont, a.settings.BufferFontSize)
		built.LineHeight = float32(a.settings.BufferLineHeight)
	}
	state.diffState = built
	return built
}

func fileStatusInitial(status string) string {
	switch strings.ToLower(status) {
	case "added":
		return "A"
	case "deleted", "removed":
		return "D"
	case "renamed":
		return "R"
	}
	return "M"
}

// requestChosenFile is the file the Changes tab shows: the one picked, or the
// first when none is.
func (a *app) requestChosenFile(detail forge.Detail) (forge.ChangedFile, int) {
	for i, file := range detail.Files {
		if file.Path == a.changeRequests.expandedFile {
			return file, i
		}
	}
	return detail.Files[0], 0
}

// changeRequestFiles is the Changes tab as History shows a commit: the files on
// the left, the chosen file's diff beside them in the shared diff viewer, a
// divider between them to drag, and a button to fold the file list away.
// requestFileMenu is the right-click menu of a changed file: its path, its
// address on the hosting site, and the site itself.
func (a *app) requestFileMenu(m *ui.Menu, services ui.Services, item forge.ChangeRequest, file forge.ChangedFile) {
	address := requestFileURL(item, file.Path)
	if m.Item(tr("Open in browser")).Disabled(address == "" || a.openExternalURL == nil).Chosen() {
		a.openExternalURL(address)
	}
	if m.Item(tr("Open in editor")).Disabled(a.snapshot.Root == "" || a.location.IsRemote()).Chosen() {
		path := a.repositoryPath(file.Path)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			a.notify("error", tr("Could not open the file"),
				trf("%s is not in the checked out repository. Switch to the request's branch first.", file.Path))
		} else {
			a.launchEditor(path, 0)
		}
	}
	m.Separator()
	if m.Item(tr("Copy path")).Chosen() {
		a.notifyCopied(services, "path", file.Path)
	}
	if m.Item(tr("Copy absolute path")).Disabled(a.snapshot.Root == "").Chosen() {
		a.notifyCopied(services, "absolute path", a.repositoryPath(file.Path))
	}
	if m.Item(tr("Copy file name")).Chosen() {
		a.notifyCopied(services, "file name", filepath.Base(file.Path))
	}
	if m.Item(tr("Copy URL")).Disabled(address == "").Chosen() {
		a.notifyCopied(services, "URL", address)
	}
}

func (a *app) changeRequestFiles(c *ui.Context, detail forge.Detail, maximized bool) {
	theme := c.Theme()
	item, _ := a.selectedChangeRequest()
	if len(detail.Files) == 0 {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(8, 0).Children(func() {
			ui.Text(c, tr("No changed files reported")).TextColor(theme.TextMuted).Grow(1)
			a.browserLink(c, tr("All changes in browser"), requestSectionURL(item, "changes"))
		})
		return
	}
	if a.changeRequests.filesWidth <= 0 {
		a.changeRequests.filesWidth = 290
	}
	_, chosen := a.requestChosenFile(detail)
	// A plain split: no drawn divider and no rail, only an invisible edge to
	// drag between the files and the diff.
	host := ui.Row(c).Grow(1).FillWidth().Padding(8, 0, 0, 0).AlignItems(ui.Stretch)
	// open eases between 0 (folded) and 1 (shown), like the other panes; it
	// starts at its target so a pane built folded or open does not animate.
	open := b2f(!a.changeRequests.filesCollapsed)
	if paneCollapseDuration > 0 {
		open = host.Animate("files-open", open, paneCollapseDuration)
	}
	files := func(width float32) {
		ui.Column(c).Width(width).Shrink(0).Gap(6).Padding(0, 4, 0, 0).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Icon(c, folderIcon).FontSize(14).TextColor(theme.TextMuted)
				ui.Text(c, tr("Changed files")).Bold().SingleLine().Grow(1)
				ui.Badge(c, fmt.Sprintf("%d", len(detail.Files)))
				finderOpen := a.changeRequests.fileSearchOpen
				label, tip := "Find a file", "Search the changed files by path"
				if finderOpen {
					label, tip = "Close file finder", "Close the file search and show every file"
				}
				finder := compactIconButton(ui.Button(c, "")).Label(tr(label)).Tooltip(tr(tip)).Checked(finderOpen)
				color := theme.Text
				if finderOpen {
					finder.Background(theme.Accent.Alpha(0.14))
					color = theme.Accent
				}
				finder.Children(func() { ui.Icon(c, searchIcon).FontSize(14).TextColor(color) })
				if finder.Clicked() {
					a.changeRequests.fileSearchOpen = !finderOpen
					if !a.changeRequests.fileSearchOpen {
						a.changeRequests.fileQuery = ""
					}
				}
				flatIconButton(c, "Collapse changed files", "Collapse changed files", paneExpandedIcon, 15, 3, false,
					func() { a.changeRequests.filesCollapsed = true })
			})
			query := strings.ToLower(strings.TrimSpace(a.changeRequests.fileQuery))
			shown := make([]int, 0, len(detail.Files))
			for i, entry := range detail.Files {
				if query == "" || strings.Contains(strings.ToLower(entry.Path+"\n"+entry.PreviousPath), query) {
					shown = append(shown, i)
				}
			}
			// The tab and the drawer can both be built in the frame the drawer
			// opens, and one handle may only bind one input per build.
			searchHandle := &a.changeRequests.fileSearch
			if maximized {
				searchHandle = &a.changeRequests.drawerSearch
			}
			if a.changeRequests.fileSearchOpen {
				input := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Padding(4, 8).
					Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius)
				var edit ui.Element
				input.Children(func() {
					ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
					edit = ui.TextInputBase(c, &a.changeRequests.fileQuery).Grow(1).MinWidth(0).Bind(searchHandle).
						AutoFocus().Label(tr("Find a file")).Placeholder(tr("Find a file…"))
					if strings.TrimSpace(a.changeRequests.fileQuery) != "" {
						ui.Text(c, fmt.Sprintf("%d / %d", len(shown), len(detail.Files))).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						flatIconButton(c, "Clear file search", "Show every file", closeIcon, 11, 3, false,
							func() { a.changeRequests.fileQuery = "" })
					}
				})
				edit.OnTextKey(func(key ui.TextKey) bool {
					if key.Key == ui.KeyEscape && key.Mods == 0 {
						a.changeRequests.fileSearchOpen = false
						a.changeRequests.fileQuery = ""
						return true
					}
					return false
				})
			}
			if len(shown) == 0 {
				listSearchEmpty(c, "No files match this filter")
				return
			}
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(3).Children(func() {
					for _, i := range shown {
						entry, i := detail.Files[i], i
						c.Key("review-file:" + entry.Path)
						button := ui.ButtonBase(c).FillWidth().Gap(7).Padding(5, 8).AlignItems(ui.Center).Radius(theme.Radius)
						if i == chosen {
							button.Background(theme.Accent.Alpha(0.16))
						} else if button.Hovered() {
							button.Background(theme.SurfaceHover)
						}
						button.Children(func() {
							pill(c, fileStatusInitial(entry.Status), fileStatusColor(theme, entry.Status), nil)
							ui.Text(c, entry.Path).Grow(1).SingleLine().Tooltip(entry.Path)
							ui.Text(c, fmt.Sprintf("+%d", entry.Additions)).TextColor(theme.Success).FontSize(theme.FontSize - 2)
							ui.Text(c, fmt.Sprintf("−%d", entry.Deletions)).TextColor(theme.Danger).FontSize(theme.FontSize - 2)
						})
						button.PopoverMenu(func(m *ui.Menu) { a.requestFileMenu(m, c.Services(), item, entry) })
						if button.Clicked() {
							a.changeRequests.expandedFile = entry.Path
						}
					}
				})
			})
		})
	}
	const railWidth = 34
	host.Children(func() {
		if a.changeRequests.filesCollapsed && open <= 0 || open > 0 && open < paneRailShowsBelow {
			// The folded file list leaves a slim rail with the way back; mid-slide
			// the rail is drawn at the shrinking width so it never pops in.
			width := float32(railWidth) + max(a.changeRequests.filesWidth-railWidth, 0)*open
			rail := ui.Column(c).Width(width).Shrink(0).AlignItems(ui.Center).Padding(8, 0).Clip().
				Border(1, theme.Border).Radius(theme.Radius + 4)
			rail.Children(func() {
				flatIconButton(c, "Expand changed files", "Expand changed files", paneCollapsedIcon, 15, 4, false,
					func() { a.changeRequests.filesCollapsed = false })
			})
			ui.Box(c).Width(8).Shrink(0)
		} else if open < 1 {
			width := float32(railWidth) + max(a.changeRequests.filesWidth-railWidth, 0)*open
			ui.Column(c).Width(width).Shrink(0).Clip().Children(func() { files(a.changeRequests.filesWidth) })
			ui.Box(c).Width(8).Shrink(0)
		}
		if open >= 1 && !a.changeRequests.filesCollapsed {
			files(a.changeRequests.filesWidth)
			// A thin line, drawn in the middle of a wider edge that takes the drag.
			edge := ui.Column(c).Width(10).Shrink(0).AlignItems(ui.Center).Cursor(ui.CursorResizeEW).Label(tr("Drag to resize"))
			line := theme.Border
			if edge.Hovered() {
				line = theme.Accent
			}
			edge.Children(func() {
				ui.Box(c).Width(1).Grow(1).Background(line)
			})
			if dx, _, held := edge.Dragged(); held && dx != 0 {
				a.changeRequests.filesWidth = min(max(a.changeRequests.filesWidth+dx, 180), 560)
			}
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Padding(0, 0, 0, 4).Children(func() {
			a.requestDiffPane(c, detail, maximized)
		})
	})
}

// requestDiffPane is the chosen file's header and diff, shared by the Changes
// tab and its whole-window view.
func (a *app) requestDiffPane(c *ui.Context, detail forge.Detail, maximized bool) {
	theme := c.Theme()
	item, _ := a.selectedChangeRequest()
	file, _ := a.requestChosenFile(detail)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, file.Path).Bold().Grow(1).SingleLine().Tooltip(file.Path)
		if file.Status != "" {
			pill(c, file.Status, fileStatusColor(theme, file.Status), nil)
		}
		a.diffLayoutButton(c)
		a.diffWrapButton(c)
		if !maximized {
			iconButton(c, "Expand diff", "Open the changes in a drawer", expandIcon, false,
				func() { a.changeRequests.diffMax = true })
		}
	})
	if file.Patch == "" || file.Truncated {
		note := tr("The diff is too large or not provided here.")
		if file.Patch == "" && !file.Truncated {
			note = tr("No text diff here (binary, renamed or empty).")
		}
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Text(c, note).TextColor(theme.TextMuted).TextAlign(ui.Center)
			a.browserLink(c, tr("View in browser"), requestFileURL(item, file.Path))
		})
		if file.Patch == "" {
			return
		}
	}
	if state := a.requestDiffState(item, file); state != nil {
		if file.Truncated {
			ui.Text(c, tr("This diff is shortened.")).TextColor(theme.Warning).FontSize(theme.FontSize - 1)
		}
		editor.DiffView(c, state).Grow(1)
	} else if a.changeRequests.diffErr != "" {
		ui.Text(c, a.changeRequests.diffErr).TextColor(theme.Danger)
	}
}

// requestDiffFullscreen is the drawer for a request's changes, as the file
// history has one: files and diff side by side, an outward edge to drag, and a
// click on the dimmed page, Escape or the buttons to close it.
func (a *app) requestDiffFullscreen(c *ui.Context, overlayTopInset float32) {
	detail := a.changeRequests.detail
	item, hasItem := a.selectedChangeRequest()
	if !a.changeRequests.diffMax || detail == nil || len(detail.Files) == 0 || !hasItem || a.workspaceTab != sectionPullRequests {
		a.changeRequests.diffMax = false
		return
	}
	theme := c.Theme()
	closeDrawer := func() { a.changeRequests.diffMax = false }
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(overlayTopInset).Right(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, 0.3)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			width := &a.changeRequests.drawerWidth
			if *width < 30 {
				*width = 88
			}
			*width = min(*width, 98)
			ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).WidthPercent(100 - *width).
				Tooltip(tr("Close the changes")).OnClick(closeDrawer)
			ui.Column(c).Absolute().Right(0).Top(0).Bottom(0).WidthPercent(*width).
				Padding(islandGap / 2).Gap(0).Background(islandBackdrop(theme)).
				Role(ui.RoleDialog).Label(tr("Changed files")).Children(func() {
				islandCard(c, ui.Column(c).Gap(5).Padding(10, 12)).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
						iconButton(c, "Close the changes", "Escape", chevronLeftIcon, false, closeDrawer)
						ui.Icon(c, diffSplitIcon).FontSize(15).TextColor(theme.Accent)
						ui.Text(c, fmt.Sprintf("%s #%d  %s", changeRequestKind(item.Provider), item.Number, item.Title)).
							FontSize(16).Bold().Grow(1).SingleLine().Tooltip(item.Title)
						ui.Badge(c, fmt.Sprintf("%d %s", len(detail.Files), tr("Changed files")))
						iconButton(c, "Close the changes", "Escape", closeIcon, false, closeDrawer)
					})
				})
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
					a.changeRequestFiles(c, *detail, true)
				})
			})
			handle := ui.Box(c).Absolute().Top(0).Bottom(0).LeftPercent(100-*width).Width(10).
				Translate(-5, 0).Cursor(ui.CursorResizeEW).Label(tr("Drag to resize"))
			if dx, _, held := handle.Dragged(); held && dx != 0 {
				if total := back.Bounds().W; total > 0 {
					*width = min(max(*width-dx*100/total, 30), 98)
				}
			}
			if back.OverlayShortcut(0, ui.KeyEscape) {
				closeDrawer()
			}
		})
	})
}

func userDiscussionCount(discussions []forge.Discussion) int {
	count := 0
	for _, discussion := range discussions {
		if !discussion.System {
			count++
		}
	}
	return count
}

func requestTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// avatarInitial is a round letter mark standing in for a profile picture.
func (a *app) personAvatar(c *ui.Context, name, avatarURL string, size float32) {
	a.urlAvatar(c, name, avatarURL, size)
}

var (
	activityLabelIDs  = regexp.MustCompile(`~(\d+)`)
	activityLabelNote = regexp.MustCompile(`^(added|removed)\s+(?:~\d+[\s,]*(?:and\s+)?)+\s*labels?`)
)

// changeRequestDiscussions is GitLab's Activity: system events and discussion
// threads on one timeline, oldest first.
func (a *app) changeRequestDiscussions(c *ui.Context, detail forge.Detail) {
	theme := c.Theme()
	entries := make([]forge.Discussion, 0, len(detail.Discussions))
	for _, discussion := range detail.Discussions {
		if discussion.System && !a.requestPartVisible(requestPartActivity) {
			continue
		}
		entries = append(entries, discussion)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		x, y := entries[i].CreatedAt, entries[j].CreatedAt
		return !x.IsZero() && !y.IsZero() && x.Before(y)
	})
	ui.Text(c, tr("Activity")).FontSize(theme.FontSize + 4).Bold()
	if len(entries) == 0 {
		ui.Text(c, tr("No discussions yet")).TextColor(theme.TextMuted)
		return
	}
	// No gap between entries, so the timeline line stays unbroken.
	ui.Column(c).FillWidth().Gap(0).Children(func() {
		for i, entry := range entries {
			entry, first, last := entry, i == 0, i == len(entries)-1
			c.Key("review-activity:" + entry.ID)
			if entry.System {
				a.activityEvent(c, detail, entry, first, last)
			} else {
				a.activityThread(c, detail, entry, first, last)
			}
		}
	})
}

// timelineRow draws one entry beside the timeline's vertical line. node is the
// marker on the line; content fills the rest.
func timelineRow(c *ui.Context, size float32, first, last bool, node, content func()) {
	theme := c.Theme()
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start).Children(func() {
		// Both are absolute and the marker comes last, so it is painted over
		// the line; an opaque backing hides the line behind it.
		// The line runs from the first marker's centre to the last's.
		center := size / 2
		if size < 12 {
			center = 9.5
		}
		line := ui.Box(c).Absolute().Left(13.5).Width(1).Background(theme.Border)
		if first {
			line.Top(center)
		} else {
			line.Top(0)
		}
		if last {
			line.Height(max(center-line0(first, center), 0))
		} else {
			line.Bottom(0)
		}
		// size is the marker's width, so it can be centered on the line by hand.
		ui.Column(c).Absolute().Left((28 - size) / 2).Top(0).Width(size).Children(node)
		ui.Box(c).Width(28).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Padding(0, 0, 14, 0).Children(content)
	})
}

func (a *app) activityEvent(c *ui.Context, detail forge.Detail, note forge.Discussion, isFirst, last bool) {
	theme := c.Theme()
	first, rest, _ := strings.Cut(strings.TrimSpace(htmlNoteToMarkdown(note.Body)), "\n")
	first = strings.ReplaceAll(first, "**", "")
	approved := strings.HasPrefix(first, "approved this merge request")
	markerSize := float32(9)
	if approved {
		markerSize = 22
	}
	timelineRow(c, markerSize, isFirst, last, func() {
		if approved {
			ui.Box(c).Size(22, 22).Radius(11).Background(theme.Background).Center().Children(func() {
				ui.Box(c).Absolute().Left(0).Top(0).Size(22, 22).Radius(11).Background(theme.Success.Alpha(0.2)).PassThrough()
				ui.Icon(c, checkCircleIcon).FontSize(14).TextColor(theme.Success)
			})
			return
		}
		ui.Box(c).Size(9, 9).Radius(5).Margin(5, 0, 0, 0).Background(theme.TextMuted).Border(2, theme.Background)
	}, func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
			ui.Text(c, note.Author).Bold()
			if ids := activityLabelIDs.FindAllStringSubmatch(first, -1); len(ids) > 0 && activityLabelNote.MatchString(first) {
				verb, _, _ := strings.Cut(first, " ")
				ui.Text(c, verb).TextColor(theme.TextMuted)
				for _, id := range ids {
					number, _ := strconv.ParseInt(id[1], 10, 64)
					name := detail.LabelNames[number]
					if name == "" {
						name = "~" + id[1]
					}
					c.Key("event-label:" + name)
					a.requestLabel(c, name)
				}
				ui.Text(c, tr("label")).TextColor(theme.TextMuted)
			} else {
				ui.Text(c, first).TextColor(theme.TextMuted).Selectable().MinWidth(0)
			}
			if age := relativeTime(note.CreatedAt); age != "" {
				ui.Text(c, age).TextColor(theme.TextMuted).Tooltip(requestTime(note.CreatedAt))
			}
		})
		if strings.TrimSpace(rest) != "" {
			a.requestMarkdown(c, "event:"+note.ID, rest)
		}
	})
}

// threadOpen says whether a thread's replies are shown: resolved threads start
// folded, open ones expanded, and a click flips either.
func (a *app) threadOpen(d forge.Discussion) bool {
	open := !d.Resolved
	if a.changeRequests.threadFlip[d.ID] {
		open = !open
	}
	return open
}

func (a *app) activityThread(c *ui.Context, detail forge.Detail, d forge.Discussion, first, last bool) {
	theme := c.Theme()
	onDiff := d.Path != ""
	timelineRow(c, 28, first, last, func() {
		a.personAvatar(c, d.Author, d.AuthorAvatar, 28)
	}, func() {
		ui.Column(c).FillWidth().Border(1, theme.Border).Radius(theme.Radius + 2).Clip().Children(func() {
			ui.Column(c).FillWidth().Gap(4).Padding(10, 12).Children(func() {
				ui.Row(c).FillWidth().MinWidth(0).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					ui.Text(c, d.Author).Bold()
					if onDiff {
						where := tr("the diff")
						if d.Outdated {
							where = tr("an old version of the diff")
						}
						ui.Text(c, tr("started a thread on")).TextColor(theme.TextMuted)
						ui.Text(c, where).Bold()
					}
					if age := relativeTime(d.CreatedAt); age != "" {
						ui.Text(c, age).TextColor(theme.TextMuted).Tooltip(requestTime(d.CreatedAt))
					}
					if d.State != "" {
						pill(c, d.State, decisionColor(c, d.State), nil)
					}
				})
				if d.Resolved {
					line := tr("Resolved")
					if age := relativeTime(d.ResolvedAt); age != "" {
						line += " " + age
					}
					if d.ResolvedBy != "" {
						line += " " + tr("by") + " " + d.ResolvedBy
					}
					ui.Text(c, line).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				}
			})
			open := onDiff && len(d.Replies) > 0 || onDiff && d.Resolved
			if open {
				c.Key("thread-toggle:" + d.ID)
				shown := a.threadOpen(d)
				toggle := ui.ButtonBase(c).FillWidth().Justify(ui.Start).Gap(8).Padding(8, 12).AlignItems(ui.Center).Background(theme.Surface)
				toggle.Children(func() {
					icon := chevronRightIcon
					if shown {
						icon = chevronDownIcon
					}
					ui.Icon(c, icon).FontSize(12).TextColor(theme.TextMuted)
					switch {
					case shown:
						ui.Text(c, tr("Collapse replies")).TextColor(theme.Accent)
					case len(d.Replies) > 0:
						last := d.Replies[len(d.Replies)-1]
						noun := tr("reply")
						if len(d.Replies) != 1 {
							noun = tr("replies")
						}
						ui.Text(c, fmt.Sprintf("%d %s", len(d.Replies), noun)).TextColor(theme.Accent)
						ui.Text(c, tr("Last reply by")+" "+last.Author+" "+relativeTime(last.CreatedAt)).TextColor(theme.TextMuted)
					default:
						ui.Text(c, tr("Show comment")).TextColor(theme.Accent)
					}
				})
				if toggle.Clicked() {
					if a.changeRequests.threadFlip == nil {
						a.changeRequests.threadFlip = map[string]bool{}
					}
					a.changeRequests.threadFlip[d.ID] = !a.changeRequests.threadFlip[d.ID]
				}
				if !shown {
					return
				}
			}
			if onDiff {
				a.discussionSnippet(c, detail, d)
			}
			ui.Column(c).FillWidth().Gap(8).Padding(10, 12).Children(func() {
				if strings.TrimSpace(d.Body) != "" {
					a.requestMarkdown(c, "discussion:"+d.ID, d.Body)
				}
				for _, reply := range d.Replies {
					reply := reply
					c.Key("review-reply:" + reply.ID)
					ui.Row(c).FillWidth().Gap(9).Padding(8, 10).Background(theme.Surface.Alpha(0.5)).Radius(theme.Radius).Children(func() {
						a.personAvatar(c, reply.Author, reply.AuthorAvatar, 24)
						ui.Column(c).Grow(1).Gap(3).Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Text(c, reply.Author).Bold().FontSize(theme.FontSize - 1)
								ui.Text(c, relativeTime(reply.CreatedAt)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Tooltip(requestTime(reply.CreatedAt))
							})
							a.requestMarkdown(c, "reply:"+reply.ID, reply.Body)
						})
					})
				}
				a.discussionActions(c, d)
			})
		})
	})
}

// discussionActions are the write controls under a thread: resolve, reply and
// the reply box. A read-only token has none of them.
func (a *app) discussionActions(c *ui.Context, d forge.Discussion) {
	if a.requestReadOnly() {
		return
	}
	ui.Row(c).Gap(7).Children(func() {
		if d.Resolvable {
			label := tr("Resolve")
			if d.Resolved {
				label = tr("Reopen")
			}
			if ui.Button(c, label).Disabled(a.changeRequests.mutationBusy).Clicked() {
				a.beginChangeRequestMutation(forge.Mutation{Kind: forge.MutationResolve, Discussion: d, Resolved: !d.Resolved})
			}
		}
		if ui.Button(c, tr("Reply")).Disabled(a.changeRequests.mutationBusy).Clicked() {
			a.changeRequests.replyFor = d.ID
			a.changeRequests.replyDraft = ""
		}
	})
	if a.changeRequests.replyFor == d.ID {
		c.Key("discussion-reply-composer:" + d.ID)
		ui.Column(c).Gap(6).Children(func() {
			ui.TextArea(c, &a.changeRequests.replyDraft).Label(tr("Reply to discussion")).
				Placeholder(tr("Write a reply using Markdown")).Height(64).AutoFocus()
			ui.Row(c).Justify(ui.End).Gap(7).Children(func() {
				if ui.Button(c, tr("Cancel")).Disabled(a.changeRequests.mutationBusy).Clicked() {
					a.changeRequests.replyFor = ""
					a.changeRequests.replyDraft = ""
				}
				if ui.PrimaryButton(c, tr("Reply")).
					Disabled(a.changeRequests.mutationBusy || strings.TrimSpace(a.changeRequests.replyDraft) == "").Clicked() {
					a.beginChangeRequestMutation(forge.Mutation{Kind: forge.MutationReply, Body: a.changeRequests.replyDraft, Discussion: d})
				}
			})
		})
	}
}

// discussionSnippet shows the lines of the diff the thread is about: the file,
// then a few lines of its patch ending at the commented line.
func (a *app) discussionSnippet(c *ui.Context, detail forge.Detail, d forge.Discussion) {
	theme := c.Theme()
	var patch string
	for _, file := range detail.Files {
		if file.Path == d.Path || file.PreviousPath == d.Path {
			patch = file.Patch
			break
		}
	}
	lines := parseReviewPatch(patch)
	target := -1
	for i, line := range lines {
		if d.Line > 0 && (d.OldSide && line.oldLine == d.Line && line.kind != '+' || !d.OldSide && line.newLine == d.Line && line.kind != '-') {
			target = i
			break
		}
	}
	ui.Column(c).FillWidth().Children(func() {
		ui.Text(c, d.Path).Bold().FontSize(theme.FontSize-1).Selectable().Padding(8, 12).Background(theme.Surface)
		if target < 0 {
			return
		}
		from := max(target-4, 0)
		ui.Column(c).FillWidth().Children(func() {
			for i := from; i <= target; i++ {
				line := lines[i]
				if line.kind == '@' {
					continue
				}
				c.Key(fmt.Sprintf("snippet:%s:%d", d.ID, i))
				background := ui.Color{}
				switch line.kind {
				case '+':
					background = theme.Success.Alpha(0.12)
				case '-':
					background = theme.Danger.Alpha(0.12)
				}
				ui.Row(c).FillWidth().MinWidth(0).Gap(6).Padding(1, 8).Background(background).Children(func() {
					for _, number := range []int{line.oldLine, line.newLine} {
						ui.Text(c, patchLineNumber(number)).Width(34).Shrink(0).SingleLine().TextAlign(ui.End).
							TextColor(theme.TextMuted).Font(a.settings.BufferFont).FontSize(float32(a.settings.BufferFontSize))
					}
					ui.Text(c, line.text).Grow(1).MinWidth(0).SingleLine().Font(a.settings.BufferFont).FontSize(float32(a.settings.BufferFontSize))
				})
			}
		})
	})
}

var rawMarkdownIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M5.5 4 2 8l3.5 4M10.5 4 14 8l-3.5 4M9 3 7 13"/></svg>`))

type reviewPatchLine struct {
	text    string
	oldLine int
	newLine int
	kind    byte
}

var reviewHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

func parseReviewPatch(patch string) []reviewPatchLine {
	lines := strings.Split(strings.TrimSuffix(patch, "\n"), "\n")
	result := make([]reviewPatchLine, 0, len(lines))
	oldLine, newLine := 0, 0
	for _, text := range lines {
		if match := reviewHunkHeader.FindStringSubmatch(text); len(match) == 3 {
			oldLine, _ = strconv.Atoi(match[1])
			newLine, _ = strconv.Atoi(match[2])
			result = append(result, reviewPatchLine{text: text, kind: '@'})
			continue
		}
		line := reviewPatchLine{text: text}
		switch {
		case strings.HasPrefix(text, "+") && !strings.HasPrefix(text, "+++"):
			line.kind, line.newLine = '+', newLine
			newLine++
		case strings.HasPrefix(text, "-") && !strings.HasPrefix(text, "---"):
			line.kind, line.oldLine = '-', oldLine
			oldLine++
		case strings.HasPrefix(text, " "):
			line.kind, line.oldLine, line.newLine = ' ', oldLine, newLine
			oldLine++
			newLine++
		}
		result = append(result, line)
	}
	return result
}

func (a *app) changeRequestPatchView(c *ui.Context, detail forge.Detail, file forge.ChangedFile) {
	theme := c.Theme()
	item, selected := a.selectedChangeRequest()
	canComment := selected && detail.HeadSHA != "" && !a.requestReadOnly()
	if item.Provider == forge.GitLab {
		canComment = canComment && detail.BaseSHA != "" && detail.StartSHA != ""
	}
	ui.Column(c).Padding(6).Background(theme.Surface).Children(func() {
		for index, line := range parseReviewPatch(file.Patch) {
			line := line
			c.Key(fmt.Sprintf("patch-line:%s:%d", file.Path, index))
			background := ui.Color{}
			switch line.kind {
			case '+':
				background = theme.Success.Alpha(0.10)
			case '-':
				background = theme.Danger.Alpha(0.10)
			}
			button := ui.ButtonBase(c).FillWidth().Padding(2, 5).Background(background).Children(func() {
				ui.Text(c, fmt.Sprintf("%4s %4s", patchLineNumber(line.oldLine), patchLineNumber(line.newLine))).
					Width(74).Shrink(0).TextColor(theme.TextMuted).Font(a.settings.BufferFont).FontSize(float32(a.settings.BufferFontSize))
				ui.Text(c, line.text).Grow(1).Selectable().Font(a.settings.BufferFont).FontSize(float32(a.settings.BufferFontSize))
			})
			if button.Clicked() && canComment && (line.oldLine > 0 || line.newLine > 0) {
				a.changeRequests.lineTarget = &forge.LineTarget{
					Path: file.Path, PreviousPath: file.PreviousPath, OldLine: line.oldLine, NewLine: line.newLine,
					BaseSHA: detail.BaseSHA, StartSHA: detail.StartSHA, HeadSHA: detail.HeadSHA,
				}
				a.changeRequests.lineDraft = ""
			}
			target := a.changeRequests.lineTarget
			if target != nil && target.Path == file.Path && target.OldLine == line.oldLine && target.NewLine == line.newLine {
				c.Key(fmt.Sprintf("line-comment:%s:%d:%d", file.Path, line.oldLine, line.newLine))
				ui.Column(c).Gap(6).Margin(3, 6, 6, 82).Children(func() {
					ui.TextArea(c, &a.changeRequests.lineDraft).Label(tr("Comment on line")).
						Placeholder(tr("Write a line comment using Markdown")).Height(64).AutoFocus()
					ui.Row(c).Justify(ui.End).Gap(7).Children(func() {
						if ui.Button(c, tr("Cancel")).Disabled(a.changeRequests.mutationBusy).Clicked() {
							a.changeRequests.lineTarget = nil
							a.changeRequests.lineDraft = ""
						}
						if ui.PrimaryButton(c, tr("Post line comment")).
							Disabled(a.changeRequests.mutationBusy || strings.TrimSpace(a.changeRequests.lineDraft) == "").Clicked() {
							a.beginChangeRequestMutation(forge.Mutation{Kind: forge.MutationLineComment, Body: a.changeRequests.lineDraft, Line: *target})
						}
					})
				})
			}
		}
	})
}

func patchLineNumber(line int) string {
	if line <= 0 {
		return ""
	}
	return strconv.Itoa(line)
}

func fileStatusColor(theme *ui.Theme, status string) ui.Color {
	switch strings.ToLower(status) {
	case "added":
		return theme.Success
	case "deleted", "removed":
		return theme.Danger
	case "renamed":
		return theme.Accent
	}
	return theme.Warning
}

var (
	htmlListItem = regexp.MustCompile(`(?i)<li>`)
	htmlCode     = regexp.MustCompile(`(?i)</?code>`)
	htmlTags     = regexp.MustCompile(`(?i)</?(ul|ol|li|p|br)\s*/?>`)
)

// htmlNoteToMarkdown turns the small HTML GitLab puts in system notes (lists
// of commits, code spans) into Markdown the viewer can show.
func htmlNoteToMarkdown(body string) string {
	if !strings.Contains(body, "<") {
		return body
	}
	body = htmlListItem.ReplaceAllString(body, "\n- ")
	body = htmlCode.ReplaceAllString(body, "`")
	body = htmlTags.ReplaceAllString(body, "")
	return html.UnescapeString(body)
}

// line0 is where a timeline line starts: at the marker's centre for the first
// entry, at the top otherwise.
func line0(first bool, center float32) float32 {
	if first {
		return center
	}
	return 0
}

// requestColor reads a "#rrggbb" colour from the service, or reports false.
func requestColor(value string) (ui.Color, bool) {
	value = strings.TrimSpace(value)
	if len(value) != 7 || value[0] != '#' {
		return ui.Color{}, false
	}
	if _, err := strconv.ParseUint(value[1:], 16, 32); err != nil {
		return ui.Color{}, false
	}
	return ui.Hex(value), true
}

// requestLabel draws a label in the colours the service gives it, falling back
// to the theme's when it gave none.
func (a *app) requestLabel(c *ui.Context, name string) {
	var style forge.LabelStyle
	if detail := a.changeRequests.detail; detail != nil {
		style = detail.Side.Labels[name]
	}
	fill, ok := requestColor(style.Color)
	if !ok {
		labelPill(c, name)
		return
	}
	text, hasText := requestColor(style.TextColor)
	if !hasText {
		text = c.Theme().AccentText
	}
	size := c.Theme().FontSize - 2
	scope, value, scoped := strings.Cut(name, "::")
	if !scoped || scope == "" || value == "" {
		ui.Row(c).AlignItems(ui.Center).Shrink(0).Radius(9).Padding(1, 8).Background(fill).Children(func() {
			ui.Text(c, name).Bold().FontSize(size).SingleLine().TextColor(text)
		})
		return
	}
	ui.Row(c).AlignItems(ui.Center).Shrink(0).Radius(9).Border(1, fill).Clip().Children(func() {
		ui.Box(c).Padding(1, 7).Background(fill).Children(func() {
			ui.Text(c, scope).Bold().FontSize(size).SingleLine().TextColor(text)
		})
		ui.Box(c).Padding(1, 7).Background(c.Theme().Surface).Children(func() {
			ui.Text(c, value).Bold().FontSize(size).SingleLine().TextColor(c.Theme().Text)
		})
	})
}

// changeRequestSidebar is the column beside the Overview: assignees, reviewers
// with their approvals, labels, milestone, time and participants.
func (a *app) changeRequestSidebar(c *ui.Context, detail forge.Detail) {
	theme := c.Theme()
	side := detail.Side
	section := func(title string, body func()) {
		ui.Column(c).FillWidth().Gap(7).Padding(0, 0, 12, 0).Children(func() {
			ui.Text(c, title).Bold()
			body()
		})
	}
	none := func() { ui.Text(c, tr("None")).TextColor(theme.TextMuted) }
	person := func(p forge.Person, trailing func()) {
		c.Key("side-person:" + p.Username)
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			a.personAvatar(c, p.Username, p.Avatar, 24)
			name := p.Name
			if name == "" {
				name = p.Username
			}
			ui.Text(c, name).SingleLine().Grow(1).Tooltip("@" + p.Username)
			if trailing != nil {
				trailing()
			}
		})
	}
	ui.Column(c).Width(250).Shrink(0).Gap(10).Padding(0, 18, 0, 14).Children(func() {
		section(tr("Assignee"), func() {
			if len(side.Assignees) == 0 {
				none()
			}
			for _, p := range side.Assignees {
				person(p, nil)
			}
		})
		ui.Divider(c)
		section(fmt.Sprintf("%d %s", len(side.Reviewers), tr("Reviewers")), func() {
			if side.ApprovalsLeft == 0 {
				ui.Text(c, tr("All required approvals given")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			}
			if len(side.Reviewers) == 0 {
				none()
			}
			for _, r := range side.Reviewers {
				r := r
				person(r.Person, func() {
					if r.Approved {
						ui.Icon(c, checkCircleIcon).FontSize(15).TextColor(theme.Success).Tooltip(tr("Approved"))
					} else {
						ui.Box(c).Size(14, 14).Radius(7).Border(1, theme.TextMuted).Tooltip(tr("Not approved yet"))
					}
				})
			}
		})
		ui.Divider(c)
		section(tr("Labels"), func() {
			item, _ := a.selectedChangeRequest()
			if len(item.Labels) == 0 {
				none()
				return
			}
			ui.Row(c).FillWidth().Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
				for _, label := range item.Labels {
					c.Key("side-label:" + label)
					a.requestLabel(c, label)
				}
			})
		})
		ui.Divider(c)
		section(tr("Milestone"), func() {
			if side.Milestone == "" {
				none()
				return
			}
			ui.Text(c, side.Milestone)
		})
		if side.TimeSpent != "" || side.TimeEstimate != "" {
			ui.Divider(c)
			section(tr("Time tracking"), func() {
				if side.TimeSpent != "" {
					ui.Text(c, tr("Spent")+": "+side.TimeSpent)
				}
				if side.TimeEstimate != "" {
					ui.Text(c, tr("Estimate")+": "+side.TimeEstimate).TextColor(theme.TextMuted)
				}
			})
		}
		if len(side.Participants) > 0 {
			ui.Divider(c)
			section(fmt.Sprintf("%d %s", len(side.Participants), tr("Participants")), func() {
				ui.Row(c).FillWidth().Gap(4).Wrap().Children(func() {
					for _, p := range side.Participants {
						c.Key("participant:" + p.Username)
						ui.Row(c).Tooltip(p.Name + " (@" + p.Username + ")").Children(func() {
							a.personAvatar(c, p.Username, p.Avatar, 24)
						})
					}
				})
			})
		}
	})
}

// changeRequestRowMenu is the context menu of one listed request. Everything in
// it reads or copies; nothing changes the request on the hosting site.
func (a *app) changeRequestRowMenu(m *ui.Menu, index int, services ui.Services) {
	if index < 0 || index >= len(a.changeRequests.items) {
		return
	}
	item := a.changeRequests.items[index]
	choose := func() {
		if a.changeRequests.selected != index {
			a.changeRequests.selected = index
			a.loadSelectedChangeRequestDetail(false)
		}
	}
	open := func(target string) {
		if target != "" && ui.SafeToOpen(target) && a.openExternalURL != nil {
			a.openExternalURL(target)
		}
	}
	canOpen := func(target string) bool { return target != "" && ui.SafeToOpen(target) && a.openExternalURL != nil }

	if m.Item(tr("Open in browser")).Disabled(!canOpen(item.URL)).Chosen() {
		open(item.URL)
	}
	m.Submenu(tr("Open page in browser"), func(sub *ui.Menu) {
		entries := []struct {
			label, target, branch string
		}{
			{"Commits", requestSectionURL(item, "commits"), ""},
			{"Changes", requestSectionURL(item, "changes"), ""},
			{"Source branch: %s", requestBranchURL(item, item.SourceBranch, true), item.SourceBranch},
			{"Target branch: %s", requestBranchURL(item, item.TargetBranch, false), item.TargetBranch},
		}
		for i, entry := range entries {
			if i == 2 {
				sub.Separator()
			}
			label := tr(entry.label)
			if entry.branch != "" {
				label = trf(entry.label, entry.branch)
			}
			if sub.Item(label).Disabled(!canOpen(entry.target)).Chosen() {
				open(entry.target)
			}
		}
	})
	m.Submenu(tr("Copy"), func(sub *ui.Menu) {
		command := requestCheckoutCommand(item)
		if sub.Item(tr("Link")).Disabled(item.URL == "").Chosen() {
			a.notifyCopied(services, "request link", item.URL)
		}
		if sub.Item(tr("Number")).Chosen() {
			a.notifyCopied(services, "request number", requestNumberText(item))
		}
		if sub.Item(tr("Title")).Chosen() {
			a.notifyCopied(services, "request title", item.Title)
		}
		sub.Separator()
		if sub.Item(tr("Source branch name")).Disabled(item.SourceBranch == "").Chosen() {
			a.notifyCopied(services, "branch name", item.SourceBranch)
		}
		if sub.Item(tr("Checkout command")).Disabled(command == "").Chosen() {
			a.notifyCopied(services, "checkout command", command)
		}
	})
	if item.Author != "" {
		m.Separator()
		if m.Item(trf("Show requests by %s", item.Author)).Chosen() {
			choose()
			a.changeRequests.filters.Author = item.Author
			a.changeRequests.filterDraft = a.changeRequests.filters
			a.chooseChangeRequestFilter()
		}
	}
}

// requestNumberText is the number the way the provider writes it.
func requestNumberText(item forge.ChangeRequest) string {
	if item.Provider == forge.GitLab {
		return fmt.Sprintf("!%d", item.Number)
	}
	return fmt.Sprintf("#%d", item.Number)
}

// requestCheckoutCommand is the provider CLI command that checks the request out.
func requestCheckoutCommand(item forge.ChangeRequest) string {
	switch item.Provider {
	case forge.GitHub:
		return fmt.Sprintf("gh pr checkout %d", item.Number)
	case forge.GitLab:
		return fmt.Sprintf("glab mr checkout %d", item.Number)
	}
	return ""
}

// requestListTitle is the title as the list shows it: a draft's own "Draft:"
// prefix is dropped because the row already marks it as a draft.
func requestListTitle(item forge.ChangeRequest) string {
	title := strings.TrimSpace(item.Title)
	if item.Draft {
		for _, prefix := range []string{"draft:", "[draft]", "(draft)"} {
			if len(title) > len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
				return strings.TrimSpace(title[len(prefix):])
			}
		}
	}
	return title
}

func isDefaultBranchName(name string) bool {
	return name == "main" || name == "master"
}

// middleEllipsis shortens a name to at most limit characters by cutting its
// middle, keeping both the start and the end.
func middleEllipsis(name string, limit int) string {
	runes := []rune(name)
	if limit < 5 || len(runes) <= limit {
		return name
	}
	head := (limit - 1) / 2
	tail := limit - 1 - head
	return string(runes[:head]) + "…" + string(runes[len(runes)-tail:])
}

// parseChangeRequestSearch lets the search box carry filters as words:
// @user, user:name or author:user, assignee:, reviewer:, source:, target:, label:,
// after:YYYY-MM-DD, before:YYYY-MM-DD and draft:yes|no. Anything else is the
// text the host searches titles and descriptions for. A dialog filter wins
// over the same filter written here.
func parseChangeRequestSearch(search string) (text string, typed changeRequestFilterValues) {
	fields := map[string]*string{
		"author": &typed.Author, "user": &typed.Author, "assignee": &typed.Assignee, "reviewer": &typed.Reviewer,
		"source": &typed.Source, "head": &typed.Source, "target": &typed.Target, "base": &typed.Target,
		"label": &typed.Label, "after": &typed.After, "before": &typed.Before,
	}
	var words []string
	for _, word := range strings.Fields(search) {
		if len(word) > 1 && word[0] == '@' {
			name := word[1:]
			// "@user:name" is a common slip for "@name".
			if key, value, ok := strings.Cut(name, ":"); ok && value != "" &&
				(strings.EqualFold(key, "user") || strings.EqualFold(key, "author")) {
				name = value
			}
			if typed.Author == "" {
				typed.Author = name
				continue
			}
		} else if key, value, ok := strings.Cut(word, ":"); ok && value != "" {
			key = strings.ToLower(key)
			if key == "draft" {
				switch strings.ToLower(value) {
				case "yes", "true", "only":
					typed.Draft = 1
					continue
				case "no", "false", "exclude":
					typed.Draft = 2
					continue
				}
			} else if target := fields[key]; target != nil && *target == "" {
				if (key == "after" || key == "before") && !forge.ValidDate(value) {
					words = append(words, word)
					continue
				}
				*target = value
				continue
			}
		}
		words = append(words, word)
	}
	return strings.Join(words, " "), typed
}
