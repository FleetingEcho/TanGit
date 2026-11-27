package app

import (
	"strings"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const overviewSearchDelay = 275 * time.Millisecond

func (o *overviewState) searching() bool {
	return strings.TrimSpace(o.query) != ""
}

func (o *overviewState) activeRepoFilter() string {
	if o.searching() {
		return ""
	}
	return o.filter
}

func overviewSearchForRepo(query string, repo overviewRepo) repository.CommitHistorySearch {
	unpushed := overviewQueryIsUnpushed(query)
	query = overviewDropUnpushed(query)
	filter, _ := parseCommitSearch(query)
	search := repository.CommitHistorySearch{Author: strings.TrimSpace(filter.Author), Unpushed: unpushed}
	words := strings.Fields(strings.ToLower(filter.Query))
	if len(words) == 0 && search.Author == "" && strings.TrimSpace(query) != "" {
		words = []string{strings.ToLower(strings.TrimSpace(query))}
	}
	name := strings.ToLower(repo.displayName() + " " + repoName(repo.Path))
	for _, word := range words {
		if !strings.Contains(name, word) {
			search.Terms = append(search.Terms, word)
		}
	}
	return search
}

func (a *app) overviewSearchChanged(o *overviewState) {
	query := strings.TrimSpace(o.query)
	if query == o.requestedQuery {
		return
	}
	o.requestedQuery = query
	if query == "" {
		a.reloadOverview()
		return
	}
	a.stopOverview()
	o.resetTimeline()
	token := o.token
	o.searchTimer = time.AfterFunc(overviewSearchDelay, func() {
		a.update(func() {
			if a.overview == o && o.token == token {
				a.reloadOverview()
			}
		})
	})
}
