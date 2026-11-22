package app

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// searchSession answers commit searches from commits the loaded history does
// not hold, and records the filters it was asked for.
type searchSession struct {
	fakeSession
	commits []repository.Commit
	mu      *sync.Mutex
	filters *[]repository.CommitFilter
}

func (s searchSession) SearchCommits(_ context.Context, filter repository.CommitFilter) ([]repository.Commit, error) {
	s.mu.Lock()
	*s.filters = append(*s.filters, filter)
	s.mu.Unlock()
	var out []repository.Commit
	query, author := strings.ToLower(filter.Query), strings.ToLower(filter.Author)
	for _, commit := range s.commits {
		if query != "" && !strings.Contains(strings.ToLower(commit.Subject), query) && !strings.HasPrefix(commit.Hash, query) {
			continue
		}
		if author != "" && !strings.Contains(strings.ToLower(commit.AuthorName), author) && !strings.Contains(strings.ToLower(commit.AuthorEmail), author) {
			continue
		}
		out = append(out, commit)
	}
	return out, nil
}

func commitSearchShell(t *testing.T) (*shell, *harness, *[]repository.CommitFilter) {
	t.Helper()
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	var mu sync.Mutex
	filters := &[]repository.CommitFilter{}
	old := repository.Commit{
		Hash: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", ShortHash: "deadbee", Subject: "Fix the ancient parser",
		AuthorName: "Alice Example", AuthorEmail: "alice@example.com", Parents: []string{"abc"},
	}
	h.app.session = searchSession{fakeSession: h.app.session.(fakeSession), commits: []repository.Commit{old}, mu: &mu, filters: filters}
	h.app.snapshot.Commits = []repository.Commit{{Hash: "1111111111111111111111111111111111111111", ShortHash: "1111111", Subject: "Recent work"}}
	s.palette.open = true
	return s, h, filters
}

func waitForCommitSearch(t *testing.T, s *shell, h *harness) {
	t.Helper()
	s.ensureCommitSearch(h.app)
	h.waitFor(t, "the commit search", func() bool {
		return !s.palette.commitSearch.loading && s.palette.commitSearch.key != ""
	})
}

func TestPastedHashFindsACommitBeyondTheLoadedHistory(t *testing.T) {
	s, h, _ := commitSearchShell(t)
	s.palette.query = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	waitForCommitSearch(t, s, h)
	ctx := commandContext{Shell: s, App: h.app}
	candidate, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, s.palette.query, nil), "history/commit/deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if !ok || candidate.Label != "Fix the ancient parser" {
		t.Fatalf("a pasted hash did not find its commit: %+v", candidate)
	}

	// Enter lists every action for the commit and keeps the palette open.
	result := s.invokeCommand(candidate.ID)
	if !result.Accepted() || !s.palette.open || s.palette.target == nil {
		t.Fatalf("choosing the commit did not open its actions: %+v open=%v", result, s.palette.open)
	}
	actions := s.commandPaletteCandidates()
	have := map[string]bool{}
	for _, action := range actions {
		have[action.Label] = true
	}
	for _, label := range []string{"Show in History", "Copy commit hash", "Checkout to this commit (detached)…", "Cherry-pick onto current branch…",
		"Revert commit…", "Create a tag here…", "Create a branch here…", "Export as mailbox patch…", "Compare commit with HEAD", "Start bisect from this commit…"} {
		if !have[label] {
			t.Errorf("action %q is missing: %v", label, have)
		}
	}
	// A commit outside the checked-out branch's loaded history keeps no
	// rebase or reset actions, as the context menu does for another branch.
	for _, label := range []string{"Drop commit…", "Soft reset to this commit…", "Reword commit…"} {
		if have[label] {
			t.Errorf("history-rewriting action %q offered for a commit outside the loaded history", label)
		}
	}
	// Typing filters the actions; Escape steps back before it closes.
	s.palette.query = "revert"
	if got := s.commandPaletteCandidates(); len(got) != 1 || got[0].Label != "Revert commit…" {
		t.Fatalf("filtering the actions = %+v", got)
	}
	s.closeCommitActions()
	if s.palette.target != nil || !s.palette.open {
		t.Fatal("stepping back should leave the commit actions and keep the palette")
	}
}

func TestLoadedCommitsOfTheCurrentBranchOfferHistoryActions(t *testing.T) {
	s, h, _ := commitSearchShell(t)
	loaded := h.app.snapshot.Commits[0]
	s.openCommitActions(h.app, loaded)
	have := map[string]bool{}
	for _, action := range s.commandPaletteCandidates() {
		have[action.Label] = true
	}
	for _, label := range []string{"Edit commit (pause rebase)…", "Reword commit…", "Squash into older commit…", "Drop commit…", "Hard reset to this commit…", "Mark as base commit for rebase"} {
		if !have[label] {
			t.Errorf("a loaded commit lacks %q", label)
		}
	}
}

func TestCommitSearchSyntaxAndAuthorFallback(t *testing.T) {
	if filter, explicit := parseCommitSearch("fix by:alice branch:main"); !explicit || filter.Query != "fix" || filter.Author != "alice" || filter.Branch != "main" {
		t.Fatalf("parsed filter = %+v explicit=%v", filter, explicit)
	}
	s, h, filters := commitSearchShell(t)
	s.palette.query = "#alice"
	waitForCommitSearch(t, s, h)
	ctx := commandContext{Shell: s, App: h.app}
	// "alice" is not in any message: only the author search finds the commit.
	if _, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, s.palette.query, nil), "history/commit/deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); !ok {
		t.Fatalf("a user name did not find their commit; filters = %+v", *filters)
	}
	if len(*filters) != 2 {
		t.Fatalf("free words should be tried as message and as author: %+v", *filters)
	}
}

func TestBranchModeListsRemoteBranches(t *testing.T) {
	s, h, _ := commitSearchShell(t)
	h.app.snapshot.Remotes = []repository.Remote{{Name: "origin", Branches: []repository.RemoteBranch{{Name: "feature/x", RemoteName: "origin"}}}}
	s.palette.query = "@feature"
	ctx := commandContext{Shell: s, App: h.app}
	if candidate, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, s.palette.query, nil), "branches/remote/origin/feature/x"); !ok || len(candidate.ActionLabels) != 2 {
		t.Fatalf("remote branch row = %+v", candidate)
	}
}

// TestCommitActionsScreenshots is opt-in like the other screenshot harnesses.
func TestCommitActionsScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, tt, _, _ := newShellShot(t, true, defaultTheme)
	a := s.activeApp()
	a.workspaceTab = sectionHistory
	tt.SetSize(1280, 900)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	s.openCommandPalette(nil)
	if len(a.snapshot.Commits) == 0 {
		t.Skip("the screenshot fixture has no commits")
	}
	commit := a.snapshot.Commits[0]
	s.palette.query = "#"
	saveScreenshot(t, dir, "palette-commits-dark", tt)
	s.openCommitActions(a, commit)
	saveScreenshot(t, dir, "palette-commit-actions-dark", tt)
}

func TestAuthorSuggestionsCompleteAByWord(t *testing.T) {
	s, h, filters := commitSearchShell(t)
	h.app.authors = []string{"Alice Example", "Bob Builder"}
	ctx := commandContext{Shell: s, App: h.app}

	s.palette.query = "#by:ali"
	got := s.catalogFor(ctx).Search(ctx, s.palette.query, nil)
	if len(got) != 1 || got[0].ID != "authors/Alice Example" {
		t.Fatalf("suggestions for by:ali = %+v", got)
	}
	if len(*filters) != 0 {
		t.Fatalf("a half-typed author must not start a Git search: %+v", *filters)
	}
	s.palette.query = "#by:"
	if got := s.catalogFor(ctx).Search(ctx, s.palette.query, nil); len(got) != 2 {
		t.Fatalf("an empty by: should list every author: %+v", got)
	}
	s.palette.query = "#by:ali"
	result := s.invokeCommand("authors/Alice Example")
	if !result.Accepted() || !s.palette.open || s.palette.query != `#by:"Alice Example" ` {
		t.Fatalf("choosing an author gave %q (open=%v)", s.palette.query, s.palette.open)
	}
	if filter, explicit := parseCommitSearch(strings.TrimSpace(s.palette.query[1:])); !explicit || filter.Author != "Alice Example" {
		t.Fatalf("quoted author did not parse: %+v", filter)
	}
	waitForCommitSearch(t, s, h)
	commits := s.catalogFor(ctx).Search(ctx, s.palette.query, nil)
	if _, ok := catalogCandidate(commits, "history/commit/deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"); !ok {
		t.Fatalf("the completed author did not find their commit: %+v", commits)
	}
}

func TestBranchSuggestionsCompleteABranchWord(t *testing.T) {
	s, h, filters := commitSearchShell(t)
	h.app.snapshot.Branches = []repository.Branch{{Name: "main", Head: true}, {Name: "feature/login"}}
	h.app.snapshot.Remotes = []repository.Remote{{Name: "origin", Branches: []repository.RemoteBranch{{Name: "release/1.0", RemoteName: "origin"}}}}
	ctx := commandContext{Shell: s, App: h.app}

	s.palette.query = "#fix branch:fea"
	got := s.catalogFor(ctx).Search(ctx, s.palette.query, nil)
	if len(got) != 1 || got[0].ID != "branch-suggestions/feature/login" {
		t.Fatalf("suggestions for branch:fea = %+v", got)
	}
	if len(*filters) != 0 {
		t.Fatalf("a half-typed branch must not start a Git search: %+v", *filters)
	}
	s.palette.query = "#on:"
	if got := s.catalogFor(ctx).Search(ctx, s.palette.query, nil); len(got) != 3 {
		t.Fatalf("an empty on: should list every branch, local and remote: %+v", got)
	}
	s.palette.query = "#fix branch:rel"
	result := s.invokeCommand("branch-suggestions/origin/release/1.0")
	if !result.Accepted() || !s.palette.open || s.palette.query != "#fix branch:origin/release/1.0 " {
		t.Fatalf("choosing a branch gave %q (open=%v)", s.palette.query, s.palette.open)
	}
	if filter, _ := parseCommitSearch(strings.TrimSpace(s.palette.query[1:])); filter.Branch != "origin/release/1.0" || filter.Query != "fix" {
		t.Fatalf("completed branch did not parse: %+v", filter)
	}
}
