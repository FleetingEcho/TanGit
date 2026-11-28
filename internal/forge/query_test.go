package forge

import (
	"net/url"
	"strings"
	"testing"
)

func TestGitLabQueryUsesRequestedSort(t *testing.T) {
	endpoint := (Query{OrderBy: SortMilestoneDue, Sort: SortAscending}).gitlabEndpoint(FilterOpen, 50)
	query, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if got := query.Query().Get("order_by"); got != "milestone_due" {
		t.Fatalf("order_by = %q", got)
	}
	if got := query.Query().Get("sort"); got != "asc" {
		t.Fatalf("sort = %q", got)
	}
}

func TestGitHubQueryUsesSupportedRequestedSort(t *testing.T) {
	search := (Query{OrderBy: SortCreated, Sort: SortAscending}).githubSearch()
	if !strings.Contains(search, "sort:created-asc") {
		t.Fatalf("search = %q", search)
	}
}

func TestGitHubQueryOmitsGitLabOnlySort(t *testing.T) {
	search := (Query{OrderBy: SortPriority, Sort: SortDescending}).githubSearch()
	if strings.Contains(search, "sort:") {
		t.Fatalf("search = %q", search)
	}
}
