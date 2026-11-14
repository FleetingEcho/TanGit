package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

func homeWorktreeSnapshot() repository.Snapshot {
	return repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Worktrees: []repository.Worktree{
			{Name: "project", Path: "/work/project", Branch: "main", Current: true, Main: true},
			{Name: "feature", Path: "/work/feature", Branch: "feature"},
			{Name: "missing", Path: "/work/missing", Branch: "missing", Missing: true},
		},
	}
}

func TestHomeWorktreePickerDiscoversAndOpensExplicitTarget(t *testing.T) {
	opener := &recordedOpener{snapshot: homeWorktreeSnapshot()}
	a := newApp(opener)
	updated := newUpdates()
	a.update = updated.update
	a.path = "/work/project"
	var opened string
	a.claimOpen = func(path string) bool {
		opened = path
		return true
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	a.openHomeWorktrees("/work/project")
	updated.waitForUpdate(t)
	tt.Frame()
	if !a.homeWorktrees.open || a.homeWorktrees.loading || !a.anyFormOpen() {
		t.Fatalf("the worktree chooser did not finish opening: %+v", a.homeWorktrees)
	}
	for _, want := range []string{"Choose a main or linked worktree to open", "Main worktree", "feature", "Missing"} {
		if !tt.HasText(want) {
			t.Errorf("chooser does not show %q: %v", want, tt.Texts())
		}
	}
	if !slices.Equal(opener.opened, []string{"/work/project"}) {
		t.Fatalf("discovery opened %v, want only the explicit source", opener.opened)
	}

	if err := tt.Click("Open worktree /work/feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if opened != "/work/feature" || a.path != "/work/feature" {
		t.Fatalf("selected worktree opened %q (path %q), want /work/feature", opened, a.path)
	}
	if a.homeWorktrees.open || a.anyFormOpen() {
		t.Fatal("choosing a worktree left the homepage modal open")
	}
}

func TestHomeWorktreePickerDisablesMissingTargets(t *testing.T) {
	a := newApp(&recordedOpener{snapshot: homeWorktreeSnapshot()})
	a.homeWorktrees.open = true
	a.homeWorktrees.source = "/work/project"
	a.homeWorktrees.location = workspace.Location{Kind: workspace.Local, Root: "/work/project"}
	a.homeWorktrees.worktrees = homeWorktreeSnapshot().Worktrees
	opened := 0
	a.claimOpen = func(string) bool {
		opened++
		return true
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	if err := tt.Click("Open worktree /work/missing"); err != nil {
		t.Fatal(err)
	}
	a.openHomeWorktree(homeWorktreeSnapshot().Worktrees[2])
	if opened != 0 || a.path != "" {
		t.Fatalf("missing worktree opened %q through %d calls", a.path, opened)
	}
}

func TestHomeWorktreePickerPreservesRemoteWorkspaceIdentity(t *testing.T) {
	snapshot := homeWorktreeSnapshot()
	snapshot.Root = "/srv/project"
	for i := range snapshot.Worktrees {
		snapshot.Worktrees[i].Path = []string{"/srv/project", "/srv/feature", "/srv/missing"}[i]
	}
	opener := remoteHomeWorktreeOpener{snapshot: snapshot}
	a := newApp(opener)
	updated := newUpdates()
	a.update = updated.update
	a.openHomeWorktrees("ssh://prod/srv/project")
	updated.waitForUpdate(t)

	var opened string
	a.claimOpen = func(path string) bool {
		opened = path
		return true
	}
	a.openHomeWorktree(a.homeWorktrees.worktrees[1])
	if opened != "ssh://prod/srv/feature" {
		t.Fatalf("remote worktree opened %q, want its SSH profile and remote path", opened)
	}
}

func TestHomeWorktreePickerRejectsAReadFinishedAfterClose(t *testing.T) {
	release := make(chan struct{})
	a := newApp(fakeOpener{release: release, result: openResult{snapshot: homeWorktreeSnapshot()}})
	updated := newUpdates()
	a.update = updated.update
	a.openHomeWorktrees("/work/project")
	a.closeHomeWorktrees()
	close(release)
	updated.waitForUpdate(t)

	if a.homeWorktrees.open || len(a.homeWorktrees.worktrees) != 0 {
		t.Fatalf("a closed chooser accepted its stale read: %+v", a.homeWorktrees)
	}
}

func TestRecentRepositoryWorktreeIconOpensQuickDropdown(t *testing.T) {
	opener := &recordedOpener{snapshot: homeWorktreeSnapshot()}
	a := newApp(opener)
	a.recentRepositories = []recentRepository{{Path: "/work/project", LastOpened: timeNow()}}
	updated := newUpdates()
	a.update = updated.update
	var opened string
	a.claimOpen = func(path string) bool {
		opened = path
		return true
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if _, found := tt.Find("Open worktree"); found {
		t.Fatal("worktree selection is still presented before a repository is chosen")
	}

	if err := tt.Click("Open worktree /work/project"); err != nil {
		t.Fatal(err)
	}
	if !a.homeWorktrees.dropdown || tt.HasText("Choose a main or linked worktree to open") {
		t.Fatalf("the icon did not use a dropdown: state=%+v texts=%v", a.homeWorktrees, tt.Texts())
	}
	updated.waitForUpdate(t)
	tt.Frame()
	if got := tt.Menu(); !slices.Contains(got, "project · Main worktree") || !slices.Contains(got, "feature · feature") {
		t.Fatalf("quick worktree menu = %v", got)
	}
	if opened != "" {
		t.Fatalf("clicking the worktree icon opened the card's repository: %q", opened)
	}
	if err := tt.ChooseMenuItem("feature · feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if opened != "/work/feature" || a.path != "/work/feature" {
		t.Fatalf("dropdown opened %q (path %q), want the selected worktree", opened, a.path)
	}
}

func TestRecentRepositoryWorktreeIconKeepsFirstCardDropdownOpen(t *testing.T) {
	opener := &recordedOpener{snapshot: homeWorktreeSnapshot()}
	a := newApp(opener)
	a.recentRepositories = []recentRepository{
		{Path: "/work/project", LastOpened: timeNow()},
		{Path: "/work/other", LastOpened: timeNow()},
	}
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 380, 900)
	tt.Frame()

	if err := tt.Click("Open worktree /work/project"); err != nil {
		t.Fatal(err)
	}
	if !a.homeWorktrees.menuOpen || a.homeWorktrees.source != "/work/project" {
		t.Fatalf("first card's dropdown was replaced: state=%+v menu=%v", a.homeWorktrees, tt.Menu())
	}
	if !slices.Equal(opener.opened, []string{"/work/project"}) {
		t.Fatalf("clicking one card discovered repositories %v", opener.opened)
	}
	updated.waitForUpdate(t)
	tt.Frame()
	tt.Frame()
	if got := tt.Menu(); !slices.Contains(got, "project · Main worktree") || !slices.Contains(got, "feature · feature") {
		t.Fatalf("first card's dropdown did not stay open with its results: %v", got)
	}
}

func TestRecentRepositoryWorktreeIconShowsMessageWhenNoLinkedWorktree(t *testing.T) {
	for _, test := range []struct {
		name      string
		worktrees []repository.Worktree
	}{
		{name: "no worktree records"},
		{name: "main worktree only", worktrees: []repository.Worktree{
			{Name: "project", Path: "/work/project", Branch: "main", Current: true, Main: true},
		}},
		{name: "missing linked worktree only", worktrees: []repository.Worktree{
			{Name: "project", Path: "/work/project", Branch: "main", Current: true, Main: true},
			{Name: "missing", Path: "/work/missing", Branch: "missing", Missing: true},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := homeWorktreeSnapshot()
			snapshot.Worktrees = test.worktrees
			a := newApp(&recordedOpener{snapshot: snapshot})
			a.recentRepositories = []recentRepository{
				{Path: "/work/project", LastOpened: timeNow()},
				{Path: "/work/other", LastOpened: timeNow()},
			}
			updated := newUpdates()
			a.update = updated.update
			tt := ui.NewTester(a.view, 1280, 800)
			tt.Frame()

			if err := tt.Click("Open worktree /work/project"); err != nil {
				t.Fatal(err)
			}
			updated.waitForUpdate(t)
			if len(a.pendingToasts) != 0 {
				t.Fatalf("an empty result escaped the dropdown as notifications: %#v", a.pendingToasts)
			}
			tt.Frame()
			if !a.homeWorktrees.open || !a.homeWorktrees.dropdown || !a.homeWorktrees.menuOpen {
				t.Fatalf("the result closed its dropdown: %+v", a.homeWorktrees)
			}
			if got := tt.Menu(); !slices.Equal(got, []string{"No linked worktrees"}) {
				t.Fatalf("empty worktree dropdown = %v", got)
			}
		})
	}
}

func TestRecentRepositoryWorktreeIconExplainsMissingRepositoryInDropdown(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-repository")
	a := newApp(repository.Opener{})
	a.recentRepositories = []recentRepository{{Path: missing, LastOpened: timeNow()}}
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	if err := tt.Click("Open worktree " + missing); err != nil {
		t.Fatal(err)
	}
	updated.waitForUpdate(t)
	if len(a.pendingToasts) != 0 {
		t.Fatalf("a missing repository escaped the dropdown as notifications: %#v", a.pendingToasts)
	}
	tt.Frame()
	if !a.homeWorktrees.open || !a.homeWorktrees.dropdown || !a.homeWorktrees.menuOpen {
		t.Fatalf("the missing-repository result closed its dropdown: %+v", a.homeWorktrees)
	}
	if got := tt.Menu(); !slices.Equal(got, []string{"Repository folder no longer exists"}) {
		t.Fatalf("missing-repository dropdown = %v", got)
	}
}

func TestRecentRepositoryMenuDiscoversWorktreesOnDemand(t *testing.T) {
	opener := &recordedOpener{snapshot: homeWorktreeSnapshot()}
	a := newApp(opener)
	a.recentRepositories = []recentRepository{{Path: "/work/project", LastOpened: timeNow()}}
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	if err := tt.RightClick("/work/project"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("Open worktree"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.homeWorktrees.open || a.homeWorktrees.source != "/work/project" {
		t.Fatalf("recent-card action opened %+v", a.homeWorktrees)
	}
	updated.waitForUpdate(t)
	if !slices.Equal(opener.opened, []string{"/work/project"}) {
		t.Fatalf("recent-card discovery opened %v", opener.opened)
	}
	a.closeHomeWorktrees()
}

type remoteHomeWorktreeOpener struct {
	snapshot repository.Snapshot
}

func (remoteHomeWorktreeOpener) Init(context.Context, string) error { return nil }

func (o remoteHomeWorktreeOpener) Open(context.Context, string) (repository.Session, error) {
	return remoteHomeWorktreeSession{
		fakeSession: fakeSession{snapshot: o.snapshot},
		location:    workspace.Location{Kind: workspace.SSH, ProfileID: "prod", Root: "/srv/project"},
	}, nil
}

type remoteHomeWorktreeSession struct {
	fakeSession
	location workspace.Location
}

func (s remoteHomeWorktreeSession) Workspace() workspace.Location { return s.location }
