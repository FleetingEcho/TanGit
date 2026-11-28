package forge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Detail is the provider-neutral, read-only review document for one request.
// Provider-specific identities remain attached to discussions for later reply
// and resolve operations, but callers do not need to understand their shape.
type Detail struct {
	Description string
	MergeStatus string
	BaseSHA     string
	StartSHA    string
	HeadSHA     string
	Files       []ChangedFile
	Commits     []ReviewCommit
	Discussions []Discussion
	// LabelNames maps a GitLab label id to its name, for system notes that say
	// "added ~12 label".
	LabelNames map[int64]string
	// Side is the information GitLab and GitHub show beside a request.
	Side Sidebar
	// Checks are the CI runs on the request's head commit (GitHub).
	Checks []Check
}

// Check is one CI run: its name, where it stands, and its page.
type Check struct {
	Name       string
	App        string
	Status     string // queued, in_progress or completed
	Conclusion string // success, failure, neutral, cancelled, skipped, ...
	URL        string
	Summary    string
}

// Person is a user of the hosting service.
type Person struct {
	Username string
	Name     string
	Avatar   string
}

// Reviewer is a person asked to review, and whether they approved.
type Reviewer struct {
	Person
	Approved bool
}

// LabelStyle is a label's colours as the service stores them ("#428bca").
type LabelStyle struct {
	Color     string
	TextColor string
}

// Sidebar is who and what a request involves. Every part is optional: a part
// the service or the token cannot give stays empty.
type Sidebar struct {
	Assignees    []Person
	Reviewers    []Reviewer
	Participants []Person
	Labels       map[string]LabelStyle
	Milestone    string
	// TimeSpent and TimeEstimate are human readable, such as "2h 30m".
	TimeSpent    string
	TimeEstimate string
	// ApprovalsLeft is how many approvals are still required; -1 when unknown.
	ApprovalsLeft int
}

type ChangedFile struct {
	Path         string
	PreviousPath string
	Status       string
	Additions    int
	Deletions    int
	Patch        string
	Truncated    bool
}

type ReviewCommit struct {
	SHA        string
	Title      string
	Author     string
	AuthoredAt time.Time
}

type Discussion struct {
	ID        string
	ReplyID   string
	Kind      DiscussionKind
	Author    string
	Body      string
	State     string
	CreatedAt time.Time
	// AuthorAvatar is the picture's address, or empty when the provider gave none.
	AuthorAvatar string
	Resolved     bool
	Resolvable   bool
	System       bool
	Path         string
	Line         int
	Outdated     bool
	// OldSide marks a Line number counted in the old version of the file.
	OldSide    bool
	ResolvedBy string
	ResolvedAt time.Time
	Replies    []Comment
}

type DiscussionKind string

const (
	DiscussionGeneral DiscussionKind = "general"
	DiscussionReview  DiscussionKind = "review"
	DiscussionCode    DiscussionKind = "code"
	DiscussionThread  DiscussionKind = "thread"
)

type Comment struct {
	ID           string
	AuthorAvatar string
	Author       string
	Body         string
	CreatedAt    time.Time
}

func (c *cliClient) Get(ctx context.Context, request ChangeRequest) (Detail, error) {
	if c == nil || c.runner == nil {
		return Detail{}, errors.New("forge command runner is unavailable")
	}
	if request.Number <= 0 {
		return Detail{}, errors.New("change request number is required")
	}
	tool, err := c.runner.PullRequestTool(ctx)
	if err != nil {
		return Detail{}, err
	}
	if (tool == "gh" && request.Provider != GitHub) || (tool == "glab" && request.Provider != GitLab) {
		return Detail{}, errors.New("change request provider does not match repository")
	}
	switch tool {
	case "gh":
		return c.getGitHub(ctx, request.Number)
	case "glab":
		return c.getGitLab(ctx, request.Number)
	default:
		return Detail{}, fmt.Errorf("unsupported hosting tool %q", tool)
	}
}

func (c *cliClient) api(ctx context.Context, tool, endpoint string) ([]byte, error) {
	output, err := c.runner.RunHostingCommand(ctx, tool, []string{"api", endpoint})
	if err != nil {
		return nil, err
	}
	return []byte(output), nil
}

type githubUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

func (u githubUser) person() Person {
	return Person{Username: u.Login, Name: u.Login, Avatar: safeWebURL(u.AvatarURL)}
}

type githubPull struct {
	User               githubUser   `json:"user"`
	Assignees          []githubUser `json:"assignees"`
	RequestedReviewers []githubUser `json:"requested_reviewers"`
	Labels             []struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"labels"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	Body           string `json:"body"`
	MergeableState string `json:"mergeable_state"`
	Base           struct {
		SHA string `json:"sha"`
	} `json:"base"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type githubFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Patch            string `json:"patch"`
}

type githubCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

type githubComment struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"original_line"`
	InReplyToID  int64  `json:"in_reply_to_id"`
}

type githubReview struct {
	ID          int64  `json:"id"`
	Body        string `json:"body"`
	State       string `json:"state"`
	SubmittedAt string `json:"submitted_at"`
	User        struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
}

const githubThreadsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){reviewThreads(first:100){nodes{id isResolved isOutdated viewerCanResolve viewerCanUnresolve comments(first:1){nodes{fullDatabaseId}}}}}}}`

type githubThreadsResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						ID                 string `json:"id"`
						IsResolved         bool   `json:"isResolved"`
						IsOutdated         bool   `json:"isOutdated"`
						ViewerCanResolve   bool   `json:"viewerCanResolve"`
						ViewerCanUnresolve bool   `json:"viewerCanUnresolve"`
						Comments           struct {
							Nodes []struct {
								FullDatabaseID string `json:"fullDatabaseId"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

func (c *cliClient) getGitHub(ctx context.Context, number int) (Detail, error) {
	base := fmt.Sprintf("repos/{owner}/{repo}/pulls/%d", number)
	paths := []string{
		base,
		base + "/files?per_page=100",
		base + "/commits?per_page=100",
		fmt.Sprintf("repos/{owner}/{repo}/issues/%d/comments?per_page=100", number),
		base + "/comments?per_page=100",
		base + "/reviews?per_page=100",
	}
	responses := make([][]byte, len(paths))
	for i, endpoint := range paths {
		data, err := c.api(ctx, "gh", endpoint)
		if err != nil {
			return Detail{}, fmt.Errorf("load GitHub pull request detail: %w", err)
		}
		responses[i] = data
	}
	threadsOutput, threadsErr := c.runner.RunHostingCommand(ctx, "gh", []string{
		"api", "graphql", "--field", "owner={owner}", "--field", "name={repo}",
		"--field", "number=" + strconv.Itoa(number), "--raw-field", "query=" + githubThreadsQuery,
	})
	var pull githubPull
	var files []githubFile
	var commits []githubCommit
	var comments, reviewComments []githubComment
	var reviews []githubReview
	for i, target := range []any{&pull, &files, &commits, &comments, &reviewComments, &reviews} {
		if err := json.Unmarshal(responses[i], target); err != nil {
			return Detail{}, fmt.Errorf("decode GitHub pull request detail: %w", err)
		}
	}
	detail := Detail{Description: pull.Body, MergeStatus: normalizeWords(pull.MergeableState), BaseSHA: pull.Base.SHA, StartSHA: pull.Base.SHA, HeadSHA: pull.Head.SHA}
	for _, file := range files {
		detail.Files = append(detail.Files, ChangedFile{
			Path: file.Filename, PreviousPath: file.PreviousFilename, Status: normalizeWords(file.Status),
			Additions: file.Additions, Deletions: file.Deletions, Patch: file.Patch,
			Truncated: file.Patch == "",
		})
	}
	for _, commit := range commits {
		author := commit.Author.Login
		if author == "" {
			author = commit.Commit.Author.Name
		}
		detail.Commits = append(detail.Commits, ReviewCommit{
			SHA: commit.SHA, Title: firstLine(commit.Commit.Message), Author: author,
			AuthoredAt: parseTime(commit.Commit.Author.Date),
		})
	}
	for _, comment := range comments {
		detail.Discussions = append(detail.Discussions, githubDiscussion(comment))
	}
	detail.Side = githubSidebar(pull, reviews, comments, reviewComments)
	if pull.Head.SHA != "" {
		// Checks are a bonus: a token without access to them leaves the tab out.
		if data, err := c.api(ctx, "gh", "repos/{owner}/{repo}/commits/"+pull.Head.SHA+"/check-runs?per_page=100"); err == nil {
			detail.Checks = parseGitHubChecks(data)
		}
	}
	detail.Discussions = append(detail.Discussions, githubReviewDiscussions(reviews)...)
	codeDiscussions := groupGitHubReviewComments(reviewComments)
	if threadsErr == nil {
		var threads githubThreadsResponse
		if err := json.Unmarshal([]byte(threadsOutput), &threads); err == nil {
			applyGitHubThreadIdentity(codeDiscussions, threads)
		}
	}
	detail.Discussions = append(detail.Discussions, codeDiscussions...)
	return detail, nil
}

// githubSidebar lists the people and labels GitHub gives with a pull request. A
// reviewer is approved when their latest review says so.
func githubSidebar(pull githubPull, reviews []githubReview, comments, reviewComments []githubComment) Sidebar {
	side := Sidebar{ApprovalsLeft: -1}
	if pull.Milestone != nil {
		side.Milestone = pull.Milestone.Title
	}
	for _, user := range pull.Assignees {
		side.Assignees = append(side.Assignees, user.person())
	}
	if len(pull.Labels) > 0 {
		side.Labels = map[string]LabelStyle{}
		for _, label := range pull.Labels {
			side.Labels[label.Name] = LabelStyle{Color: "#" + strings.TrimPrefix(label.Color, "#")}
		}
	}
	latest := map[string]string{}
	avatars := map[string]string{}
	for _, review := range reviews {
		latest[review.User.Login] = strings.ToUpper(review.State)
		avatars[review.User.Login] = review.User.AvatarURL
	}
	seen := map[string]bool{}
	for _, user := range pull.RequestedReviewers {
		seen[user.Login] = true
		side.Reviewers = append(side.Reviewers, Reviewer{Person: user.person()})
	}
	for login, state := range latest {
		if !seen[login] && state != "COMMENTED" {
			side.Reviewers = append(side.Reviewers, Reviewer{
				Person:   Person{Username: login, Name: login, Avatar: safeWebURL(avatars[login])},
				Approved: state == "APPROVED",
			})
		}
	}
	sort.SliceStable(side.Reviewers, func(i, j int) bool { return side.Reviewers[i].Username < side.Reviewers[j].Username })
	people := map[string]Person{}
	var order []string
	add := func(login, avatar string) {
		if login == "" {
			return
		}
		if _, ok := people[login]; !ok {
			people[login] = Person{Username: login, Name: login, Avatar: safeWebURL(avatar)}
			order = append(order, login)
		}
	}
	add(pull.User.Login, pull.User.AvatarURL)
	for _, user := range pull.Assignees {
		add(user.Login, user.AvatarURL)
	}
	for _, user := range pull.RequestedReviewers {
		add(user.Login, user.AvatarURL)
	}
	for _, comment := range comments {
		add(comment.User.Login, comment.User.AvatarURL)
	}
	for _, comment := range reviewComments {
		add(comment.User.Login, comment.User.AvatarURL)
	}
	for _, review := range reviews {
		add(review.User.Login, review.User.AvatarURL)
	}
	for _, login := range order {
		side.Participants = append(side.Participants, people[login])
	}
	return side
}

func applyGitHubThreadIdentity(discussions []Discussion, response githubThreadsResponse) {
	byComment := make(map[string]int, len(discussions))
	for i := range discussions {
		byComment[discussions[i].ID] = i
	}
	for _, thread := range response.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if len(thread.Comments.Nodes) == 0 {
			continue
		}
		index, ok := byComment[thread.Comments.Nodes[0].FullDatabaseID]
		if !ok {
			continue
		}
		discussion := &discussions[index]
		discussion.ReplyID = discussion.ID
		discussion.ID = thread.ID
		discussion.Resolved = thread.IsResolved
		discussion.Outdated = thread.IsOutdated
		discussion.Resolvable = (!thread.IsResolved && thread.ViewerCanResolve) || (thread.IsResolved && thread.ViewerCanUnresolve)
	}
}

func githubDiscussion(comment githubComment) Discussion {
	line := 0
	if comment.Line != nil {
		line = *comment.Line
	} else if comment.OriginalLine != nil {
		line = *comment.OriginalLine
	}
	return Discussion{
		ID: strconv.FormatInt(comment.ID, 10), Kind: DiscussionGeneral, Author: comment.User.Login, AuthorAvatar: safeWebURL(comment.User.AvatarURL), Body: comment.Body,
		CreatedAt: parseTime(comment.CreatedAt), Path: comment.Path, Line: line,
		Outdated: comment.Path != "" && comment.Line == nil,
	}
}

func githubReviewDiscussions(reviews []githubReview) []Discussion {
	result := make([]Discussion, 0, len(reviews))
	for _, review := range reviews {
		if strings.TrimSpace(review.Body) == "" && strings.TrimSpace(review.State) == "" {
			continue
		}
		result = append(result, Discussion{
			ID: strconv.FormatInt(review.ID, 10), Kind: DiscussionReview, Author: review.User.Login, AuthorAvatar: safeWebURL(review.User.AvatarURL), Body: review.Body,
			State: normalizeWords(review.State), CreatedAt: parseTime(review.SubmittedAt),
		})
	}
	return result
}

func groupGitHubReviewComments(comments []githubComment) []Discussion {
	roots := make([]Discussion, 0, len(comments))
	rootIndex := make(map[int64]int, len(comments))
	for _, comment := range comments {
		if comment.InReplyToID != 0 {
			continue
		}
		rootIndex[comment.ID] = len(roots)
		root := githubDiscussion(comment)
		root.Kind = DiscussionCode
		root.ReplyID = root.ID
		roots = append(roots, root)
	}
	for _, comment := range comments {
		if comment.InReplyToID == 0 {
			continue
		}
		if index, ok := rootIndex[comment.InReplyToID]; ok {
			roots[index].Replies = append(roots[index].Replies, Comment{
				ID: strconv.FormatInt(comment.ID, 10), Author: comment.User.Login, AuthorAvatar: safeWebURL(comment.User.AvatarURL),
				Body: comment.Body, CreatedAt: parseTime(comment.CreatedAt),
			})
		}
	}
	return roots
}

type gitlabMergeRequest struct {
	Description         string       `json:"description"`
	DetailedMergeStatus string       `json:"detailed_merge_status"`
	Assignees           []gitlabUser `json:"assignees"`
	Reviewers           []gitlabUser `json:"reviewers"`
	Milestone           *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	TimeStats struct {
		Estimate string `json:"human_time_estimate"`
		Spent    string `json:"human_total_time_spent"`
	} `json:"time_stats"`
	DiffRefs struct {
		BaseSHA  string `json:"base_sha"`
		StartSHA string `json:"start_sha"`
		HeadSHA  string `json:"head_sha"`
	} `json:"diff_refs"`
}

type gitlabUser struct {
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (u gitlabUser) person() Person {
	return Person{Username: u.Username, Name: u.Name, Avatar: safeWebURL(u.AvatarURL)}
}

// gitlabSidebar reads what the merge request itself lists, then asks for the
// approvals, the participants and the labels' colours; each of those may fail
// without losing the rest.
func (c *cliClient) gitlabSidebar(ctx context.Context, base string, mr gitlabMergeRequest) Sidebar {
	side := Sidebar{Milestone: "", TimeSpent: mr.TimeStats.Spent, TimeEstimate: mr.TimeStats.Estimate, ApprovalsLeft: -1}
	if mr.Milestone != nil {
		side.Milestone = mr.Milestone.Title
	}
	for _, user := range mr.Assignees {
		side.Assignees = append(side.Assignees, user.person())
	}
	approved := map[string]bool{}
	if data, err := c.api(ctx, "glab", base+"/approvals"); err == nil {
		var approvals struct {
			Left       *int `json:"approvals_left"`
			ApprovedBy []struct {
				User gitlabUser `json:"user"`
			} `json:"approved_by"`
		}
		if json.Unmarshal(data, &approvals) == nil {
			for _, entry := range approvals.ApprovedBy {
				approved[entry.User.Username] = true
			}
			if approvals.Left != nil {
				side.ApprovalsLeft = *approvals.Left
			}
		}
	}
	for _, user := range mr.Reviewers {
		side.Reviewers = append(side.Reviewers, Reviewer{Person: user.person(), Approved: approved[user.Username]})
	}
	if data, err := c.api(ctx, "glab", base+"/participants"); err == nil {
		var users []gitlabUser
		if json.Unmarshal(data, &users) == nil {
			for _, user := range users {
				side.Participants = append(side.Participants, user.person())
			}
		}
	}
	if data, err := c.api(ctx, "glab", "projects/:fullpath/labels?per_page=100"); err == nil {
		var labels []struct {
			Name      string `json:"name"`
			Color     string `json:"color"`
			TextColor string `json:"text_color"`
		}
		if json.Unmarshal(data, &labels) == nil {
			side.Labels = make(map[string]LabelStyle, len(labels))
			for _, label := range labels {
				side.Labels[label.Name] = LabelStyle{Color: label.Color, TextColor: label.TextColor}
			}
		}
	}
	return side
}

type gitlabDiff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
	Collapsed   bool   `json:"collapsed"`
	TooLarge    bool   `json:"too_large"`
}

type gitlabCommit struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	AuthoredDate string `json:"authored_date"`
}

type gitlabDiscussion struct {
	ID    string       `json:"id"`
	Notes []gitlabNote `json:"notes"`
}

type gitlabNote struct {
	ID         int64  `json:"id"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
	Resolved   bool   `json:"resolved"`
	Resolvable bool   `json:"resolvable"`
	System     bool   `json:"system"`
	ResolvedBy *struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"resolved_by"`
	ResolvedAt string `json:"resolved_at"`
	Author     struct {
		Username  string `json:"username"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	} `json:"author"`
	Position *struct {
		NewPath string `json:"new_path"`
		OldPath string `json:"old_path"`
		NewLine int    `json:"new_line"`
		OldLine int    `json:"old_line"`
	} `json:"position"`
}

func (c *cliClient) getGitLab(ctx context.Context, number int) (Detail, error) {
	base := fmt.Sprintf("projects/:fullpath/merge_requests/%d", number)
	paths := []string{
		base,
		base + "/diffs?per_page=100",
		base + "/commits?per_page=100",
		base + "/discussions?per_page=100",
	}
	responses := make([][]byte, len(paths))
	for i, endpoint := range paths {
		data, err := c.api(ctx, "glab", endpoint)
		if err != nil {
			return Detail{}, fmt.Errorf("load GitLab merge request detail: %w", err)
		}
		responses[i] = data
	}
	labelNames := map[int64]string{}
	if data, err := c.api(ctx, "glab", base+"/resource_label_events?per_page=100"); err == nil {
		var events []struct {
			Label *struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"label"`
		}
		if json.Unmarshal(data, &events) == nil {
			for _, event := range events {
				if event.Label != nil {
					labelNames[event.Label.ID] = event.Label.Name
				}
			}
		}
	}
	var mergeRequest gitlabMergeRequest
	var diffs []gitlabDiff
	var commits []gitlabCommit
	var discussions []gitlabDiscussion
	for i, target := range []any{&mergeRequest, &diffs, &commits, &discussions} {
		if err := json.Unmarshal(responses[i], target); err != nil {
			return Detail{}, fmt.Errorf("decode GitLab merge request detail: %w", err)
		}
	}
	detail := Detail{
		Description: mergeRequest.Description, MergeStatus: normalizeWords(mergeRequest.DetailedMergeStatus),
		BaseSHA: mergeRequest.DiffRefs.BaseSHA, StartSHA: mergeRequest.DiffRefs.StartSHA, HeadSHA: mergeRequest.DiffRefs.HeadSHA,
		LabelNames: labelNames,
		Side:       c.gitlabSidebar(ctx, base, mergeRequest),
	}
	for _, diff := range diffs {
		status := "Modified"
		switch {
		case diff.NewFile:
			status = "Added"
		case diff.DeletedFile:
			status = "Deleted"
		case diff.RenamedFile:
			status = "Renamed"
		}
		additions, deletions := patchStats(diff.Diff)
		detail.Files = append(detail.Files, ChangedFile{
			Path: diff.NewPath, PreviousPath: diff.OldPath, Status: status,
			Additions: additions, Deletions: deletions, Patch: diff.Diff,
			Truncated: diff.Collapsed || diff.TooLarge,
		})
	}
	for _, commit := range commits {
		detail.Commits = append(detail.Commits, ReviewCommit{
			SHA: commit.ID, Title: commit.Title, Author: commit.AuthorName, AuthoredAt: parseTime(commit.AuthoredDate),
		})
	}
	for _, discussion := range discussions {
		if len(discussion.Notes) == 0 {
			continue
		}
		root := gitlabComment(discussion.Notes[0])
		root.ID = discussion.ID
		for _, note := range discussion.Notes {
			if note.ResolvedBy != nil {
				root.ResolvedBy = note.ResolvedBy.Username
				if root.ResolvedBy == "" {
					root.ResolvedBy = note.ResolvedBy.Name
				}
				root.ResolvedAt = parseTime(note.ResolvedAt)
			}
		}
		for _, note := range discussion.Notes[1:] {
			comment := gitlabComment(note)
			root.Replies = append(root.Replies, Comment{ID: comment.ID, Author: comment.Author, AuthorAvatar: comment.AuthorAvatar, Body: comment.Body, CreatedAt: comment.CreatedAt})
		}
		detail.Discussions = append(detail.Discussions, root)
	}
	return detail, nil
}

func gitlabComment(note gitlabNote) Discussion {
	author := note.Author.Username
	if author == "" {
		author = note.Author.Name
	}
	discussion := Discussion{
		ID: strconv.FormatInt(note.ID, 10), Kind: DiscussionThread, Author: author, Body: note.Body,
		CreatedAt: parseTime(note.CreatedAt), AuthorAvatar: safeWebURL(note.Author.AvatarURL), Resolved: note.Resolved,
		Resolvable: note.Resolvable, System: note.System,
	}
	if note.Position != nil {
		discussion.Path = note.Position.NewPath
		discussion.Line = note.Position.NewLine
		if discussion.Path == "" {
			discussion.Path = note.Position.OldPath
		}
		if discussion.Line == 0 {
			discussion.Line = note.Position.OldLine
			discussion.OldSide = discussion.Line > 0
		}
	}
	return discussion
}

func firstLine(value string) string {
	if line, _, ok := strings.Cut(value, "\n"); ok {
		return line
	}
	return value
}

func patchStats(patch string) (additions, deletions int) {
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			additions++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			deletions++
		}
	}
	return additions, deletions
}

type githubCheckRuns struct {
	CheckRuns []struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
		App        struct {
			Name string `json:"name"`
		} `json:"app"`
		Output struct {
			Title string `json:"title"`
		} `json:"output"`
	} `json:"check_runs"`
}

func parseGitHubChecks(data []byte) []Check {
	var raw githubCheckRuns
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	checks := make([]Check, 0, len(raw.CheckRuns))
	for _, run := range raw.CheckRuns {
		checks = append(checks, Check{
			Name: run.Name, App: run.App.Name, Status: run.Status, Conclusion: run.Conclusion,
			URL: safeWebURL(run.HTMLURL), Summary: run.Output.Title,
		})
	}
	return checks
}
