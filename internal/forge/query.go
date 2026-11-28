package forge

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DraftFilter narrows a listing by draft status.
type DraftFilter int

const (
	DraftAny DraftFilter = iota
	DraftOnly
	DraftExclude
)

// SortField names the server-side ordering shared by the request list. GitLab
// supports every value; GitHub supports created_at and updated_at.
type SortField string

const (
	SortCreated       SortField = "created_at"
	SortUpdated       SortField = "updated_at"
	SortMerged        SortField = "merged_at"
	SortLabelPriority SortField = "label_priority"
	SortPriority      SortField = "priority"
	SortMilestoneDue  SortField = "milestone_due"
	SortPopularity    SortField = "popularity"
	SortTitle         SortField = "title"
)

type SortDirection string

const (
	SortDescending SortDirection = "desc"
	SortAscending  SortDirection = "asc"
)

// Query is the provider-neutral search a listing can be narrowed by. Empty
// fields do not narrow. Dates are YYYY-MM-DD and apply to the last update.
type Query struct {
	State         StateFilter
	Text          string
	Author        string
	Assignee      string
	Reviewer      string
	SourceBranch  string
	TargetBranch  string
	Label         string
	UpdatedAfter  string
	UpdatedBefore string
	Draft         DraftFilter
	OrderBy       SortField
	Sort          SortDirection
	// Page is the 1-based page of the listing; zero is the first.
	Page int
}

// ValidDate reports whether value is empty or a YYYY-MM-DD date.
func ValidDate(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func (q Query) filtered() bool {
	return q.Text != "" || q.Author != "" || q.Assignee != "" || q.Reviewer != "" || q.SourceBranch != "" ||
		q.TargetBranch != "" || q.Label != "" || q.UpdatedAfter != "" || q.UpdatedBefore != "" || q.Draft != DraftAny ||
		q.OrderBy != "" || q.Sort != ""
}

func (q Query) sortValues() (SortField, SortDirection) {
	field := q.OrderBy
	switch field {
	case SortCreated, SortUpdated, SortMerged, SortLabelPriority, SortPriority, SortMilestoneDue, SortPopularity, SortTitle:
	default:
		field = SortUpdated
	}
	direction := q.Sort
	if direction != SortAscending && direction != SortDescending {
		direction = SortDescending
	}
	return field, direction
}

// gitlabEndpoint builds a merge-request listing of the current project. The
// response uses the same field names the glab listing parser already accepts.
func (q Query) gitlabEndpoint(filter StateFilter, limit int) string {
	values := url.Values{}
	state := "all"
	switch filter {
	case FilterOpen:
		state = "opened"
	case FilterMerged:
		state = "merged"
	case FilterClosed:
		state = "closed"
	}
	values.Set("state", state)
	values.Set("per_page", strconv.Itoa(limit))
	if q.Page > 1 {
		values.Set("page", strconv.Itoa(q.Page))
	}
	orderBy, direction := q.sortValues()
	values.Set("order_by", string(orderBy))
	values.Set("sort", string(direction))
	set := func(key, value string) {
		if value = strings.TrimSpace(value); value != "" {
			values.Set(key, value)
		}
	}
	set("search", q.Text)
	set("author_username", strings.TrimPrefix(q.Author, "@"))
	set("assignee_username", strings.TrimPrefix(q.Assignee, "@"))
	set("reviewer_username", strings.TrimPrefix(q.Reviewer, "@"))
	set("source_branch", q.SourceBranch)
	set("target_branch", q.TargetBranch)
	set("labels", q.Label)
	if q.UpdatedAfter != "" {
		values.Set("updated_after", q.UpdatedAfter+"T00:00:00Z")
	}
	if q.UpdatedBefore != "" {
		values.Set("updated_before", q.UpdatedBefore+"T23:59:59Z")
	}
	switch q.Draft {
	case DraftOnly:
		values.Set("wip", "yes")
	case DraftExclude:
		values.Set("wip", "no")
	}
	return "projects/:fullpath/merge_requests?" + values.Encode()
}

// githubSearch builds the search qualifiers gh passes to GitHub.
func (q Query) githubSearch() string {
	var parts []string
	if text := strings.TrimSpace(q.Text); text != "" {
		parts = append(parts, text)
	}
	add := func(qualifier, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if strings.ContainsAny(value, " \t\"") {
			value = strconv.Quote(value)
		}
		parts = append(parts, qualifier+":"+value)
	}
	add("author", strings.TrimPrefix(q.Author, "@"))
	add("assignee", strings.TrimPrefix(q.Assignee, "@"))
	add("review-requested", strings.TrimPrefix(q.Reviewer, "@"))
	add("head", q.SourceBranch)
	add("base", q.TargetBranch)
	add("label", q.Label)
	switch {
	case q.UpdatedAfter != "" && q.UpdatedBefore != "":
		parts = append(parts, "updated:"+q.UpdatedAfter+".."+q.UpdatedBefore)
	case q.UpdatedAfter != "":
		parts = append(parts, "updated:>="+q.UpdatedAfter)
	case q.UpdatedBefore != "":
		parts = append(parts, "updated:<="+q.UpdatedBefore)
	}
	switch q.Draft {
	case DraftOnly:
		parts = append(parts, "draft:true")
	case DraftExclude:
		parts = append(parts, "draft:false")
	}
	orderBy, direction := q.sortValues()
	switch orderBy {
	case SortCreated:
		parts = append(parts, "sort:created-"+string(direction))
	case SortUpdated:
		parts = append(parts, "sort:updated-"+string(direction))
	}
	return strings.Join(parts, " ")
}
