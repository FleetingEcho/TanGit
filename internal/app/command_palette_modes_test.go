package app

import (
	"strings"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
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
	// Two rows: the command log, and the integrated terminal for commands that
	// ask for input. A non-Git line puts the terminal first.
	if len(got) != 2 || got[0].Detail != "echo hi" || got[0].Label != "Run in terminal" || got[0].ID != "commands/run-line-terminal" || got[1].ID != "commands/run-line" {
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
	h.app.code.filesLoaded = true
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
	// The default search includes unchanged files and applies the query.
	candidates := s.catalogFor(ctx).Search(ctx, s.palette.query, nil)
	if _, ok := catalogCandidate(candidates, "files/open/internal/app/palette.go"); !ok {
		t.Fatalf("workspace file search missed an unchanged file: %+v", candidates)
	}
	if _, ok := catalogCandidate(candidates, "files/open/README.md"); ok {
		t.Fatal("workspace file search matched an unrelated file")
	}
	s.palette.filesChangedOnly = true
	candidates = s.catalogFor(ctx).Search(ctx, s.palette.query, nil)
	if _, ok := catalogCandidate(candidates, "files/open/internal/app/palette.go"); ok {
		t.Fatalf("the changed-files list offered an unchanged file: %+v", candidates)
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

func TestRecentRepositoryModeAndRowActions(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	s.tabs = []*tab{{app: h.app}}
	s.active = 0
	h.app.recentRepositories = []recentRepository{{Path: "/work/alpha", Name: "Alpha"}, {Path: "/work/beta"}}
	h.app.snapshot.Branches = []repository.Branch{{Name: "main", Head: true}, {Name: "feature"}}
	h.app.snapshot.Commits = []repository.Commit{{Hash: "abc123", ShortHash: "abc", Subject: "Fix it"}}
	ctx := commandContext{Shell: s, App: h.app}
	s.palette.open = true

	s.palette.query = "~"
	repos := s.catalogFor(ctx).Search(ctx, s.palette.query, nil)
	if len(repos) != 2 || repos[0].Label == "" {
		t.Fatalf("recent repositories = %+v", repos)
	}
	s.palette.query = "~bet"
	if got := s.catalogFor(ctx).Search(ctx, s.palette.query, nil); len(got) != 1 || got[0].ID != "recent/open//work/beta" {
		t.Fatalf("filtered repositories = %+v", got)
	}

	s.palette.query = "@"
	for _, branch := range s.catalogFor(ctx).Search(ctx, s.palette.query, nil) {
		want := 5
		if branch.ID == "branches/checkout/main" || strings.HasPrefix(string(branch.ID), "branches/remote/") {
			want = 2
		}
		if len(branch.ActionLabels) != want {
			t.Errorf("%s has %d actions, want %d", branch.ID, len(branch.ActionLabels), want)
		}
	}
	s.palette.query = "#x"
	for _, commit := range s.catalogFor(ctx).Search(ctx, s.palette.query, nil) {
		if len(commit.ActionLabels) != 2 {
			t.Errorf("commit row actions = %v", commit.ActionLabels)
		}
	}
	s.palette.query = "!"
	if got := s.catalogFor(ctx).Search(ctx, s.palette.query, nil); len(got) != 1+len(shellExamples) || got[0].ID != "commands/run-hint" || got[0].Enabled {
		t.Fatalf("empty shell mode should hint, not run: %+v", got)
	}
}

func TestPaletteLearnsUsageAndOffersDisplayToggles(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	s := newShell(nil)
	ctx := commandContext{Shell: s, App: h.app}
	for _, id := range []commandID{"settings/diff-split", "settings/diff-wrap", "settings/diff-whitespace"} {
		if _, ok := catalogCandidate(s.catalogFor(ctx).Search(ctx, string(id), nil), id); !ok {
			t.Errorf("%s is missing", id)
		}
	}
	plain := s.catalogFor(ctx).SearchWith(ctx, "settings/diff-", commandSearchHints{})
	used := s.catalogFor(ctx).SearchWith(ctx, "settings/diff-", commandSearchHints{Uses: map[commandID]int{"settings/diff-wrap": 9}})
	if len(plain) < 2 || len(used) < 2 || used[0].ID != "settings/diff-wrap" {
		t.Fatalf("most used command did not lead: %+v", used[:min(2, len(used))])
	}
}

// Saving a ! line opens the command form, filled in, to confirm; nothing is
// saved until it is submitted, and a name already taken is refused.
func TestSaveShellLineOpensTheCommandForm(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.pathUserData = t.TempDir()
	h.app.saveShellLineAsCustomCommand("git pull origin main")
	ui := h.app.settingsUI
	if !h.app.settingsUI.open || ui.page != settingsPageCommands || !ui.customCommandFormOpen ||
		ui.customCommandFormName != "git pull" || ui.customCommandFormCommand != "git pull origin main" {
		t.Fatalf("the command form is not open and filled in: %+v", ui)
	}
	if len(h.app.settings.CustomCommands) != 0 {
		t.Fatal("the command was saved before it was confirmed")
	}
	h.app.submitCustomCommandForm(nil)
	if len(h.app.settings.CustomCommands) != 1 || h.app.settings.CustomCommands[0].Command != "git pull origin main" {
		t.Fatalf("custom commands = %+v", h.app.settings.CustomCommands)
	}

	// Another command with the same name is refused and called out.
	h.app.openCustomCommandForm(-1)
	h.app.settingsUI.customCommandFormName = "Git Pull"
	h.app.settingsUI.customCommandFormCommand = "git pull --rebase"
	if len(h.app.customCommandFormConflicts()) != 1 {
		t.Fatalf("the clashing name was not reported: %v", h.app.customCommandFormConflicts())
	}
	h.app.submitCustomCommandForm(nil)
	if len(h.app.settings.CustomCommands) != 1 || h.app.settingsUI.customCommandFormError == "" {
		t.Fatalf("a clashing name was saved: %+v", h.app.settings.CustomCommands)
	}

	// The same line again edits the saved command instead of adding a copy.
	h.app.saveShellLineAsCustomCommand("git pull origin main")
	if h.app.settingsUI.customCommandFormEdit != 0 {
		t.Fatalf("the saved line was not opened for editing: %d", h.app.settingsUI.customCommandFormEdit)
	}
}
