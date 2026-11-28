// Package forge presents GitHub pull requests and GitLab merge requests through
// one read-only model. Provider-specific commands and response shapes stay
// behind Client so the application does not need to understand either CLI.
package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Provider string

const (
	GitHub Provider = "github"
	GitLab Provider = "gitlab"
)

type State string

const (
	StateOpen   State = "open"
	StateClosed State = "closed"
	StateMerged State = "merged"
)

// ChangeRequest is the provider-neutral summary needed by the workspace page.
// Number is scoped to the repository, just like a GitHub PR number or GitLab IID.
type ChangeRequest struct {
	Provider     Provider
	Number       int
	Title        string
	State        State
	Draft        bool
	URL          string
	Author       string
	AuthorAvatar string
	SourceBranch string
	TargetBranch string
	// CrossRepo is set when the source branch lives in another repository (a
	// fork). SourceRepoURL is that repository's web address when the provider's
	// listing names it; empty means it is unknown, never "the same repository".
	CrossRepo      bool
	SourceRepoURL  string
	ReviewDecision string
	Labels         []string
	UpdatedAt      time.Time
}

// CommandRunner is the internal seam between the forge module and the local or
// SSH repository transport. Implementations choose the matching authenticated
// CLI and run it in the repository's working directory.
type CommandRunner interface {
	PullRequestTool(context.Context) (string, error)
	RunHostingCommand(context.Context, string, []string) (string, error)
}

// Client hides provider discovery, CLI arguments and incompatible JSON shapes.
// List returns at most the requested number of requests, newest first.
type Client interface {
	List(context.Context, int) ([]ChangeRequest, error)
	ListByState(context.Context, StateFilter, int) ([]ChangeRequest, error)
	Search(context.Context, Query, int) ([]ChangeRequest, error)
	Get(context.Context, ChangeRequest) (Detail, error)
	Mutate(context.Context, ChangeRequest, Mutation) error
	Capabilities(context.Context) (Capabilities, error)
	Facets(context.Context) (Facets, error)
}

type cliClient struct{ runner CommandRunner }

func NewCLIClient(runner CommandRunner) Client { return &cliClient{runner: runner} }

// StateFilter chooses which requests a listing asks the provider for, so a
// tab like Merged is answered by the server rather than from a newest-100 slice.
type StateFilter string

const (
	FilterOpen   StateFilter = "open"
	FilterMerged StateFilter = "merged"
	FilterClosed StateFilter = "closed"
	FilterAll    StateFilter = "all"
)

func (c *cliClient) List(ctx context.Context, limit int) ([]ChangeRequest, error) {
	return c.ListByState(ctx, FilterAll, limit)
}

func (c *cliClient) ListByState(ctx context.Context, filter StateFilter, limit int) ([]ChangeRequest, error) {
	return c.Search(ctx, Query{State: filter}, limit)
}

// Search lists requests narrowed by text and filters, all answered by the
// provider. A query with only a state uses the plain list commands.
func (c *cliClient) Search(ctx context.Context, query Query, limit int) ([]ChangeRequest, error) {
	filter := query.State
	if filter == "" {
		filter = FilterAll
	}
	if c == nil || c.runner == nil {
		return nil, errors.New("forge command runner is unavailable")
	}
	if limit <= 0 {
		limit = 100
	}
	tool, err := c.runner.PullRequestTool(ctx)
	if err != nil {
		return nil, err
	}
	page := max(query.Page, 1)
	var args []string
	switch tool {
	case "gh":
		// gh has no page option: read everything up to this page, drop the rest.
		args = []string{"pr", "list", "--state", string(filter), "--limit", strconv.Itoa(limit * page), "--json", "number,title,state,isDraft,url,author,headRefName,baseRefName,headRepository,headRepositoryOwner,isCrossRepository,updatedAt,reviewDecision,labels"}
		if search := query.githubSearch(); search != "" {
			args = append(args, "--search", search)
		}
	case "glab":
		if query.filtered() {
			args = []string{"api", query.gitlabEndpoint(filter, limit)}
			break
		}
		args = []string{"mr", "list"}
		switch filter {
		case FilterMerged:
			args = append(args, "--merged")
		case FilterClosed:
			args = append(args, "--closed")
		case FilterAll:
			args = append(args, "--all")
		}
		args = append(args, "--per-page", strconv.Itoa(limit), "--page", strconv.Itoa(page), "--output", "json")
	default:
		return nil, fmt.Errorf("unsupported hosting tool %q", tool)
	}
	output, err := c.runner.RunHostingCommand(ctx, tool, args)
	if err != nil {
		return nil, err
	}
	if tool == "gh" {
		items, err := parseGitHub([]byte(output))
		if skip := limit * (page - 1); err == nil && skip > 0 {
			items = items[min(skip, len(items)):]
		}
		return items, err
	}
	return parseGitLab([]byte(output))
}

type githubRequest struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	Draft          bool   `json:"isDraft"`
	URL            string `json:"url"`
	HeadRefName    string `json:"headRefName"`
	BaseRefName    string `json:"baseRefName"`
	IsCrossRepo    bool   `json:"isCrossRepository"`
	HeadRepository *struct {
		Name string `json:"name"`
	} `json:"headRepository"`
	HeadOwner *struct {
		Login string `json:"login"`
	} `json:"headRepositoryOwner"`
	UpdatedAt      string `json:"updatedAt"`
	ReviewDecision string `json:"reviewDecision"`
	Author         struct {
		Login string `json:"login"`
	} `json:"author"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func parseGitHub(data []byte) ([]ChangeRequest, error) {
	var raw []githubRequest
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode GitHub pull requests: %w", err)
	}
	requests := make([]ChangeRequest, 0, len(raw))
	for _, item := range raw {
		labels := make([]string, 0, len(item.Labels))
		for _, label := range item.Labels {
			if name := strings.TrimSpace(label.Name); name != "" {
				labels = append(labels, name)
			}
		}
		sourceRepo := ""
		if item.IsCrossRepo && item.HeadRepository != nil && item.HeadOwner != nil {
			sourceRepo = githubRepoURL(item.URL, item.HeadOwner.Login, item.HeadRepository.Name)
		}
		requests = append(requests, ChangeRequest{
			CrossRepo: item.IsCrossRepo, SourceRepoURL: sourceRepo,
			Provider: GitHub, Number: item.Number, Title: item.Title,
			State: normalizeState(item.State, false), Draft: item.Draft, URL: safeWebURL(item.URL),
			Author: item.Author.Login, AuthorAvatar: githubAvatar(item.URL, item.Author.Login), SourceBranch: item.HeadRefName, TargetBranch: item.BaseRefName,
			ReviewDecision: normalizeWords(item.ReviewDecision), Labels: labels, UpdatedAt: parseTime(item.UpdatedAt),
		})
	}
	return requests, nil
}

// GitLab has changed some glab JSON field names over time. A small custom
// decoder accepts both API-style snake_case and CLI-style alternatives without
// exposing that compatibility work to callers.
func parseGitLab(data []byte) ([]ChangeRequest, error) {
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode GitLab merge requests: %w", err)
	}
	requests := make([]ChangeRequest, 0, len(raw))
	for _, item := range raw {
		number := intField(item, "iid", "number", "id")
		title := stringField(item, "title")
		stateText := stringField(item, "state")
		sourceProject, targetProject := intField(item, "source_project_id"), intField(item, "target_project_id")
		requests = append(requests, ChangeRequest{
			CrossRepo: sourceProject != 0 && targetProject != 0 && sourceProject != targetProject,
			Provider:  GitLab, Number: number, Title: title,
			State: normalizeState(stateText, boolField(item, "merged")),
			Draft: boolField(item, "draft", "work_in_progress") || strings.HasPrefix(strings.ToLower(strings.TrimSpace(title)), "draft:"),
			URL:   safeWebURL(stringField(item, "web_url", "webUrl", "url")), Author: nestedStringField(item, "author", "username", "login", "name"),
			AuthorAvatar: safeWebURL(nestedStringField(item, "author", "avatar_url", "avatarUrl")),
			SourceBranch: stringField(item, "source_branch", "sourceBranch"), TargetBranch: stringField(item, "target_branch", "targetBranch"),
			ReviewDecision: normalizeWords(stringField(item, "detailed_merge_status", "merge_status", "mergeStatus")),
			Labels:         stringSliceField(item, "labels"), UpdatedAt: parseTime(stringField(item, "updated_at", "updatedAt")),
		})
	}
	return requests, nil
}

func normalizeState(value string, merged bool) State {
	if merged || strings.EqualFold(value, "merged") {
		return StateMerged
	}
	if strings.EqualFold(value, "closed") {
		return StateClosed
	}
	return StateOpen
}

func normalizeWords(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", " "))
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + strings.ToLower(value[1:])
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	return parsed
}

func safeWebURL(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}
	return value
}

func stringField(item map[string]json.RawMessage, names ...string) string {
	for _, name := range names {
		var value string
		if raw, ok := item[name]; ok && json.Unmarshal(raw, &value) == nil {
			return value
		}
	}
	return ""
}

func intField(item map[string]json.RawMessage, names ...string) int {
	for _, name := range names {
		var value int
		if raw, ok := item[name]; ok && json.Unmarshal(raw, &value) == nil {
			return value
		}
	}
	return 0
}

func boolField(item map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		var value bool
		if raw, ok := item[name]; ok && json.Unmarshal(raw, &value) == nil && value {
			return true
		}
	}
	return false
}

func nestedStringField(item map[string]json.RawMessage, object string, names ...string) string {
	var nested map[string]json.RawMessage
	if raw, ok := item[object]; !ok || json.Unmarshal(raw, &nested) != nil {
		return ""
	}
	return stringField(nested, names...)
}

func stringSliceField(item map[string]json.RawMessage, name string) []string {
	var values []string
	if raw, ok := item[name]; ok {
		_ = json.Unmarshal(raw, &values)
	}
	return values
}

// githubAvatar is the picture GitHub serves for a login, on the same server as
// the pull request (github.com or an enterprise host).
func githubAvatar(requestURL, login string) string {
	parsed, err := url.Parse(requestURL)
	if login == "" || err != nil || parsed.Host == "" {
		return ""
	}
	return safeWebURL(parsed.Scheme + "://" + parsed.Host + "/" + url.PathEscape(login) + ".png?size=96")
}

// githubRepoURL is the web address of owner/name on the host of a pull request
// URL, or "" when the pieces do not make a safe address.
func githubRepoURL(requestURL, owner, name string) string {
	parsed, err := url.Parse(requestURL)
	if err != nil || parsed.Host == "" || owner == "" || name == "" {
		return ""
	}
	return safeWebURL(parsed.Scheme + "://" + parsed.Host + "/" + url.PathEscape(owner) + "/" + url.PathEscape(name))
}
