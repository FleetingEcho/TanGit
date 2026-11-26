package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type historyChangesTestSession struct {
	fakeSession
	calls  chan repository.HistoryChangeQuery
	search func(context.Context, func(repository.HistoryChangePage)) (repository.HistoryChangePage, error)
}

func (s historyChangesTestSession) SearchHistoryChanges(ctx context.Context, query repository.HistoryChangeQuery, _ repository.HistoryChangeCursor, publish func(repository.HistoryChangePage)) (repository.HistoryChangePage, error) {
	s.calls <- query
	return s.search(ctx, publish)
}

func TestHistoryChangesSearchStartsOnSubmitAndOpensMatchingFile(t *testing.T) {
	h := menuTestApp(t)
	calls := make(chan repository.HistoryChangeQuery, 1)
	hit := repository.HistoryChangeHit{Commit: h.app.snapshot.Commits[0], Path: "two.txt"}
	h.app.session = historyChangesTestSession{fakeSession: h.app.session.(fakeSession), calls: calls,
		search: func(context.Context, func(repository.HistoryChangePage)) (repository.HistoryChangePage, error) {
			return repository.HistoryChangePage{Hits: []repository.HistoryChangeHit{hit}, Scanned: 2}, nil
		}}
	h.app.setWorkspaceTab(sectionHistory)
	h.tester.Frame()
	if err := h.tester.Click(historyChangesLabel); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Text to find in changes"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("changed")
	h.tester.Frame()
	if h.app.historyChanges.started {
		t.Fatal("editing the query started a search")
	}
	input, inputOK := h.tester.Find("Text to find in changes")
	button, buttonOK := h.tester.Find("Search")
	if !inputOK || !buttonOK || button.X < input.X+input.W || button.Y != input.Y {
		t.Fatalf("search button should be beside input: input=%+v button=%+v", input, button)
	}
	h.press(t, ui.KeyEnter)
	h.waitFor(t, "search results", func() bool {
		return !h.app.historyChanges.loading && len(h.app.historyChanges.page.Hits) == 1
	})
	if query := <-calls; query.Text != "changed" {
		t.Fatalf("submitted %+v", query)
	}
	if err := h.tester.Click(hit.Commit.ShortHash + " " + hit.Commit.Subject + " " + hit.Path); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.waitFor(t, "matched file diff", func() bool {
		return h.app.commitDiffPath == "two.txt" && !h.app.commitDiffLoading
	})
	if h.app.historyChanges.open || h.app.workspaceTab != sectionHistory {
		t.Fatal("result did not open in History")
	}
}

func TestHistoryChangesStoppingKeepsPartialResults(t *testing.T) {
	h := menuTestApp(t)
	calls := make(chan repository.HistoryChangeQuery, 1)
	hit := repository.HistoryChangeHit{Commit: h.app.snapshot.Commits[0], Path: "two.txt"}
	h.app.session = historyChangesTestSession{fakeSession: h.app.session.(fakeSession), calls: calls,
		search: func(ctx context.Context, publish func(repository.HistoryChangePage)) (repository.HistoryChangePage, error) {
			page := repository.HistoryChangePage{Hits: []repository.HistoryChangeHit{hit}, Scanned: 25, More: true}
			publish(page)
			<-ctx.Done()
			return page, ctx.Err()
		}}
	h.app.openHistoryChanges(&repository.HistoryChangeQuery{Text: "match"})
	h.tester.Frame()
	if err := h.tester.Click("Search"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "partial search results", func() bool { return h.app.historyChanges.page.Scanned == 25 })
	if err := h.tester.Click("Stop search"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "search to stop", func() bool { return !h.app.historyChanges.loading })
	if h.app.historyChanges.stopped == "" || len(h.app.historyChanges.page.Hits) != 1 {
		t.Fatal("stopping lost the partial results or their status")
	}
	if _, ok := h.tester.Find("Resume search"); !ok {
		t.Fatal("stopped search did not offer to resume")
	}
}

func TestHistoryChangesPalettePrefillsTheSearch(t *testing.T) {
	h := menuTestApp(t)
	h.app.session = historyChangesTestSession{fakeSession: h.app.session.(fakeSession)}
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	s.palette.open = true
	s.palette.query = "#"
	guide, ok := catalogCandidate(s.commandPaletteCandidates(), "commits/guide/changed")
	if !ok || guide.Label != historyChangesLabel || guide.Detail != `Find added or removed lines with # changed:"<code>"` {
		t.Fatalf("missing code change syntax from the commit search guide: %+v", guide)
	}
	if result := s.invokeCommand(guide.ID); !result.Accepted() || s.palette.query != "# changed:" || !s.palette.open {
		t.Fatalf("guide did not fill the code query prefix: %+v", result)
	}
	s.palette.query = `# changed:"timeout = 30" branch:main path:"a folder"`
	if s.paletteCommitText() != "" {
		t.Fatal("code search also started a commit message search")
	}
	candidates := s.commandPaletteCandidates()
	if len(candidates) != 1 {
		t.Fatalf("palette candidates: %+v", candidates)
	}
	if result := s.invokeCommand(candidates[0].ID); !result.Accepted() {
		t.Fatalf("palette invocation: %+v", result)
	}
	search := h.app.historyChanges
	if !search.open || search.started || search.draft.Text != "timeout = 30" || search.draft.Ref != "main" || search.draft.Path != "a folder" {
		t.Fatalf("unexpected search form: %+v", search.draft)
	}
}

func TestHistoryChangesFiltersOpenSeparatelyAndApply(t *testing.T) {
	h := menuTestApp(t)
	calls := make(chan repository.HistoryChangeQuery, 4)
	h.app.session = historyChangesTestSession{
		fakeSession: h.app.session.(fakeSession), calls: calls,
		search: func(context.Context, func(repository.HistoryChangePage)) (repository.HistoryChangePage, error) {
			return repository.HistoryChangePage{Scanned: 5}, nil
		},
	}
	h.app.openHistoryChanges(&repository.HistoryChangeQuery{Text: "timeout"})
	h.tester.Frame()
	if err := h.tester.Click("Search"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "initial search", func() bool { return h.app.historyChanges.started && !h.app.historyChanges.loading })
	<-calls
	if _, found := h.tester.Find("File or folder"); found {
		t.Fatal("filter fields should only appear in the filters dialog")
	}
	openFilters := func(path string) {
		t.Helper()
		if err := h.tester.Click("Search filters"); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
		h.tester.Type(path)
		h.tester.Frame()
	}
	openFilters("cancelled.go")
	h.press(t, ui.KeyEscape)
	if search := h.app.historyChanges; search.showFilters || !search.open || search.draft.Path != "" {
		t.Fatalf("Escape should dismiss only the filter dialog and discard edits: %+v", search.draft)
	}
	openFilters("settings.go")
	if search := h.app.historyChanges; !search.showFilters || search.filterDraft.Path != "settings.go" {
		t.Fatalf("filter editor did not receive the path: open=%v path=%q text=%q", search.showFilters, search.filterDraft.Path, search.draft.Text)
	}
	if err := h.tester.Click("Apply filters"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if search := h.app.historyChanges; search.showFilters || search.applied.Path != "settings.go" {
		t.Fatalf("Apply did not submit path: open=%v draft=%q filtered=%q applied=%q", search.showFilters, search.draft.Path, search.filterDraft.Path, search.applied.Path)
	}
	h.waitFor(t, "filtered search", func() bool {
		search := &h.app.historyChanges
		return !search.loading && !search.showFilters && search.applied.Path == "settings.go"
	})
	if query := <-calls; query.Path != "settings.go" || query.Text != "timeout" {
		t.Fatalf("wrong filtered query: %+v", query)
	}
}
