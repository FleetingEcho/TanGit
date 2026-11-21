package app

import (
	"testing"

	"lazygitapp/internal/repository"
)

func TestCommandPaletteModesAndShellEntry(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}

	for _, id := range []commandID{"commands/run", "commands/customize"} {
		if _, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, string(id), nil), id); !ok {
			t.Errorf("%s is missing from the palette", id)
		}
	}
	if mode, rest := commandQueryMode("  !git log"); mode != '!' || rest != "git log" {
		t.Fatalf("shell mode = %q %q", mode, rest)
	}
	if mode, rest := commandQueryMode("@ main"); mode != '@' || rest != "main" {
		t.Fatalf("branch mode = %q %q", mode, rest)
	}

	s.palette.open = true
	s.palette.query = "!echo hi"
	catalog := s.catalogFor(ctx)
	got := catalog.Search(ctx, s.palette.query, nil)
	if len(got) != 1 || got[0].ID != "commands/run-line" || got[0].Label != "echo hi" {
		t.Fatalf("shell mode candidates = %+v", got)
	}
	// Prefixed commands never leak into the default list.
	for _, c := range catalog.Search(ctx, "", nil) {
		if c.ID == "commands/run-line" {
			t.Fatal("shell line appeared in the default list")
		}
	}
}

func TestShiftedKeyboardChordsSpellOutShift(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}
	redo, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, "Redo the latest undone", nil), "git/Everywhere/Redo the latest undone Git operation")
	if !ok || redo.Chord != "Shift+Z" {
		t.Fatalf("redo chord = %+v", redo)
	}
}

func TestPinnedCommandsLeadTheEmptyList(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}
	s.togglePinnedCommand("theme/Tokyo Night")
	got := s.catalogFor(ctx).SearchPinned(ctx, "", nil, s.palette.pinned)
	if len(got) == 0 || got[0].ID != "theme/Tokyo Night" || !got[0].Pinned {
		t.Fatalf("pinned command is not first: %+v", got[:min(len(got), 2)])
	}
}

func TestStashRemoteAndWorkspaceFileCommands(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	h.app.snapshot.Stashes = []repository.Stash{{Index: 0, Name: "stash@{0}: WIP on main", Recency: "2 hours ago"}}
	h.app.snapshot.Remotes = []repository.Remote{{Name: "origin", URLs: []string{"https://example.com/team/project.git"}}}
	h.app.code.files = []string{"README.md", "internal/app/palette.go", "docs/guide.md"}
	ctx := commandContext{Shell: s, App: h.app}
	for query, id := range map[string]commandID{
		"apply stash":  "stash/apply/0",
		"pop stash":    "stash/pop/0",
		"fetch remote": "remotes/fetch/origin",
		"browser":      "remotes/open-web/origin",
	} {
		if _, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, query, nil), id); !ok {
			t.Errorf("%q did not offer %s", query, id)
		}
	}
	s.palette.open = true
	s.palette.query = "/palgo"
	candidates := s.catalogFor(ctx).Search(ctx, s.palette.query, nil)
	if _, ok := catalogCandidate(candidates, "files/open/internal/app/palette.go"); !ok {
		t.Fatalf("workspace file search missed an unchanged file: %+v", candidates)
	}
	if _, ok := catalogCandidate(candidates, "files/open/README.md"); ok {
		t.Fatal("workspace file search matched an unrelated file")
	}
}

// A disabled command can offer the command that removes its reason, reached
// with Tab (action 1) and revalidated when run.
func TestDisabledCommitOffersToStageEverything(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	h.app.snapshot.Files = []repository.File{{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true}}
	ctx := commandContext{Shell: s, App: h.app}
	commit, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, "Commit staged changes", nil), commandCommitStaged)
	if !ok || commit.Enabled || len(commit.ActionLabels) != 1 || commit.FixID == "" {
		t.Fatalf("disabled commit has no fix offer: %+v", commit)
	}
	if result := s.catalogFor(ctx).InvokeAction(ctx, commandCommitStaged, 0); result.Accepted() {
		t.Fatal("a disabled command ran")
	}
}
