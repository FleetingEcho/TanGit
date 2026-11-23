package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func enabledCommandState(commandContext) commandState {
	return commandState{Visible: true, Enabled: true}
}

func catalogCandidate(candidates []commandCandidate, id commandID) (commandCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.ID == id {
			return candidate, true
		}
	}
	return commandCandidate{}, false
}

func TestCommandCatalogSearchRanksMatchesAndKeepsDisabled(t *testing.T) {
	specs := []commandSpec{
		{ID: "exact", Label: "Push", Resolve: enabledCommandState},
		{ID: "prefix", Label: "Push to remote", Resolve: enabledCommandState},
		{ID: "token", Label: "Force push", Resolve: enabledCommandState},
		{ID: "keyword", Label: "Publish branch", Keywords: []string{"push"}, Resolve: enabledCommandState},
		{ID: "disabled", Label: "Push disabled branch", Resolve: func(commandContext) commandState {
			return commandState{Visible: true, DisabledReason: "No upstream branch"}
		}},
	}
	catalog := newCommandCatalog(specs)
	results := catalog.Search(commandContext{}, "push", nil)
	if len(results) != len(specs) {
		t.Fatalf("search dropped commands, including disabled ones: %+v", results)
	}
	if results[0].ID != "exact" {
		t.Fatalf("exact match did not rank first: %+v", results)
	}
	if candidate, ok := catalogCandidate(results, "prefix"); !ok || candidate.matchRank != 1 {
		t.Fatalf("prefix match was not ranked correctly: %+v", candidate)
	}
	if candidate, ok := catalogCandidate(results, "token"); !ok || candidate.matchRank != 2 {
		t.Fatalf("token match was not ranked correctly: %+v", candidate)
	}
	if candidate, ok := catalogCandidate(results, "keyword"); !ok || candidate.matchRank != 3 {
		t.Fatalf("keyword match was not ranked correctly: %+v", candidate)
	}
	candidate, ok := catalogCandidate(results, "disabled")
	if !ok || candidate.Enabled || candidate.DisabledReason != "No upstream branch" {
		t.Fatalf("disabled explanation was lost: %+v", candidate)
	}
}

func TestCommandCatalogSearchCanonicalLocalizedChordAndFuzzy(t *testing.T) {
	catalog := newCommandCatalog([]commandSpec{
		{ID: "terminal/toggle", Label: "Toggle Terminal", Chord: "Ctrl+`", Keywords: []string{"console"}, Resolve: enabledCommandState},
		{ID: "custom", Label: "Perform upload safely here", Resolve: enabledCommandState},
	})
	for _, query := range []string{"terminal/toggle", "ctrl+`", "console", "push"} {
		if got := catalog.Search(commandContext{}, query, nil); len(got) == 0 {
			t.Errorf("query %q did not search ID, chord, keyword and fuzzy text", query)
		}
	}
}

func TestCommandCatalogRecentIsOnlyTieBreaker(t *testing.T) {
	catalog := newCommandCatalog([]commandSpec{
		{ID: "alpha", Label: "Publish alpha", Resolve: enabledCommandState},
		{ID: "beta", Label: "Publish beta", Resolve: enabledCommandState},
		{ID: "exact", Label: "Publish", Resolve: enabledCommandState},
	})
	results := catalog.Search(commandContext{}, "publish", []commandID{"beta"})
	if results[0].ID != "exact" || results[1].ID != "beta" {
		t.Fatalf("recent outranked match quality or did not break the tie: %+v", results)
	}
}

func TestCommandCatalogInvokeRevalidatesCandidate(t *testing.T) {
	enabled := true
	runs := 0
	catalog := newCommandCatalog([]commandSpec{{
		ID: "repository/push", Label: "Push",
		Resolve: func(commandContext) commandState {
			return commandState{Visible: true, Enabled: enabled, DisabledReason: "Refresh changed the target"}
		},
		Run: func(commandContext) { runs++ },
	}})
	results := catalog.Search(commandContext{}, "push", nil)
	if len(results) != 1 || !results[0].Enabled {
		t.Fatalf("candidate fixture did not start enabled: %+v", results)
	}
	enabled = false
	result := catalog.Invoke(commandContext{}, results[0].ID)
	if result.Accepted() || runs != 0 || !strings.Contains(result.Reason, "changed") {
		t.Fatalf("stale candidate bypassed invocation checks: result=%+v runs=%d", result, runs)
	}
	enabled = true
	if result := catalog.Invoke(commandContext{}, "push"); result.Accepted() {
		// This ID does not exist even though its label occurs in the real spec.
		t.Fatal("invoke resolved a label instead of a command ID")
	}
}

func TestCommandCatalogStaticIDsUniqueAndCoverKeyboardCommands(t *testing.T) {
	catalog := newCommandCatalog(staticCommandSpecs())
	if err := catalog.validate(); err != nil {
		t.Fatal(err)
	}
	ids := make(map[commandID]bool, len(catalog.specs))
	for _, spec := range catalog.specs {
		ids[spec.ID] = true
	}
	for _, command := range keyboardCommands() {
		id := commandID("git/" + command.group + "/" + command.label)
		if ids[id] {
			continue
		}
		switch command.label {
		case "Fetch from the remotes", "Pull from the upstream branch", "Push to the remote":
			continue
		}
		t.Errorf("keyboard command %q has no catalog adapter", id)
	}
}

func TestCommandCatalogEmptyQueryIsBoundedAndContextual(t *testing.T) {
	specs := make([]commandSpec, 0, 100)
	for index := 0; index < 100; index++ {
		specs = append(specs, commandSpec{ID: commandID(strings.Repeat("x", index+1)), Label: "Long tail", Resolve: enabledCommandState})
	}
	results := newCommandCatalog(specs).Search(commandContext{}, "", nil)
	if len(results) != 0 {
		t.Fatalf("unscored long-tail commands crowded the empty query: %d", len(results))
	}
}

func readyPaletteShell(t *testing.T) (*shell, *ui.Tester, *app) {
	t.Helper()
	s, tt, _ := newShellTester(t, repository.Snapshot{})
	tab := readyShellTab(s, "/work/palette")
	tab.app.session = fakeSession{snapshot: tab.app.snapshot}
	if tab.app.commandLog == nil {
		tab.app.commandLog = repository.NewCommandLog()
	}
	tab.app.snapshot.Branch = "main"
	tab.app.workspaceTab = sectionChanges
	s.active = 1
	tt.Frame()
	return s, tt, tab.app
}

func TestCommandPalettePickerOpensAndCloses(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{})
	tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	tt.Frame()
	if !s.palette.open {
		t.Fatal("Cmd/Ctrl+Shift+P did not open the palette on the picker")
	}
	if !tt.Focused("Search commands") {
		t.Fatal("palette did not focus its search field")
	}
	tt.Type("terminal")
	tt.Frame()
	if s.palette.query != "terminal" {
		t.Fatalf("palette query did not receive text: %q", s.palette.query)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	tt.Frame()
	if s.palette.open {
		t.Fatalf("Escape did not close the palette: query=%q crash=%q focused=%v", s.palette.query, s.crash, tt.Focused("Search commands"))
	}
	if _, ok := tt.AccessByRole(ui.RoleDialog, "Command Palette"); ok {
		t.Fatal("closed palette remained in the accessibility tree")
	}
}

func TestCommandPaletteModalOwnsRepositoryLetters(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	tt.Frame()
	tt.TypeKey(0, ui.KeyP, "p")
	tt.Frame()
	if a.confirm != nil {
		t.Fatal("ordinary repository pull shortcut fired behind the palette")
	}
	if s.palette.query != "p" {
		t.Fatalf("palette text went to the repository instead of search: %q", s.palette.query)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
}

func TestCommandPaletteDangerousCommandOnlyOpensExistingConfirmation(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	s.openCommandPalette(nil)
	tt.Frame()
	s.palette.query = "Push to the remote"
	tt.Frame()
	results := s.commandPaletteCandidates()
	if len(results) == 0 {
		t.Fatal("push was not searchable")
	}
	s.palette.selected = slices.IndexFunc(results, func(candidate commandCandidate) bool { return candidate.ID == commandOpenPush })
	if s.palette.selected < 0 {
		t.Fatal("canonical push command missing")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if s.palette.open || a.confirm == nil || a.confirm.title != "Push" {
		t.Fatalf("push did not open only its existing confirmation: palette=%v confirm=%+v", s.palette.open, a.confirm)
	}
}

func TestCommandPaletteDisabledCommitExplainsAndDoesNotRun(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.snapshot.Files = nil
	s.openCommandPalette(nil)
	s.palette.query = "Commit staged changes"
	tt.Frame()
	candidate, ok := catalogCandidate(s.commandPaletteCandidates(), commandCommitStaged)
	if !ok || candidate.Enabled || candidate.DisabledReason != "No staged changes to commit" {
		t.Fatalf("commit disabled reason is missing: %+v", candidate)
	}
	result := s.invokeCommand(commandCommitStaged)
	tt.Frame()
	if result.Accepted() || a.showCommitForm || !s.palette.open {
		t.Fatalf("disabled commit executed or closed the palette: result=%+v form=%v open=%v", result, a.showCommitForm, s.palette.open)
	}
}

func TestCommandPaletteSelectedNavigationRestoresFocusAndRecordsProvenance(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	tt.Frame()
	s.openCommandPalette(nil)
	tt.Frame()
	s.palette.query = "Open History"
	tt.Frame()
	results := s.commandPaletteCandidates()
	index := slices.IndexFunc(results, func(candidate commandCandidate) bool { return candidate.ID == "workspace/history" })
	if index < 0 {
		t.Fatalf("history navigation missing: %+v", results)
	}
	s.palette.selected = index
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	if a.workspaceTab != sectionHistory || s.palette.open {
		t.Fatalf("palette did not navigate and close: section=%d open=%v", a.workspaceTab, s.palette.open)
	}
	if a.commandLog == nil {
		t.Fatal("ready app has no command logger")
	}
	entries := a.commandLog.Entries()
	if len(entries) == 0 || entries[len(entries)-1].InvocationSource != "palette" || entries[len(entries)-1].InvocationID != "workspace/history" {
		t.Fatalf("palette provenance was not retained: %+v", entries)
	}
	if entries[len(entries)-1].Shortcut != nil {
		t.Fatal("palette invocation was mislabeled as a keyboard shortcut")
	}
}

func BenchmarkCommandCatalogSearch300(b *testing.B) {
	specs := make([]commandSpec, 300)
	for index := range specs {
		specs[index] = commandSpec{ID: commandID(fmt.Sprintf("group/command-%03d", index)), Label: fmt.Sprintf("Repository command %03d", index), Keywords: []string{"terminal", "push", "agent"}, Resolve: enabledCommandState}
	}
	catalog := newCommandCatalog(specs)
	ctx := commandContext{}
	recent := []commandID{"group/command-250", "group/command-222"}
	b.ReportAllocs()
	for b.Loop() {
		if results := catalog.Search(ctx, "push", recent); len(results) == 0 {
			b.Fatal("benchmark search returned no commands")
		}
	}
}

func TestCommandPaletteNarrowDialogAndOptionsStayInBounds(t *testing.T) {
	s, tt, _ := readyPaletteShell(t)
	tt.SetSize(720, 800)
	s.openCommandPalette(nil)
	tt.Frame()
	dialog, ok := tt.AccessByRole(ui.RoleDialog, "Command Palette")
	if !ok {
		t.Fatal("palette has no named dialog role")
	}
	if dialog.Bounds.X < 0 || dialog.Bounds.X+dialog.Bounds.W > 720 {
		t.Fatalf("palette overflows narrow window: %+v", dialog.Bounds)
	}
	for _, access := range tt.AccessAll() {
		if access.Role != ui.RoleOption || access.Offscreen {
			continue
		}
		if access.Bounds.X < dialog.Bounds.X || access.Bounds.X+access.Bounds.W > dialog.Bounds.X+dialog.Bounds.W+0.5 {
			t.Fatalf("result %q overflows dialog: row=%+v dialog=%+v", access.Name, access.Bounds, dialog.Bounds)
		}
	}
}

// A shell line always shows its command text on both of its rows, next to the
// badge that says where it runs.
func TestCommandPaletteShellRowsShowTheCommand(t *testing.T) {
	s, tt, _ := readyPaletteShell(t)
	s.openCommandPalette(nil)
	tt.Frame()
	s.palette.query = "!git status"
	tt.Frame()
	tt.Frame()
	rects := 0
	for _, text := range []string{"git status", "Terminal", "Command log"} {
		rect, ok := tt.Find(text)
		if !ok {
			t.Fatalf("%q is not on the shell rows", text)
		}
		if rect.W < 20 {
			t.Fatalf("%q is only %.0f DIP wide", text, rect.W)
		}
		rects++
	}
	if rects != 3 {
		t.Fatal("missing text")
	}
}

// Up and Down move between the two rows of a typed shell line even when the
// shell history has entries; they used to walk the history instead.
func TestCommandPaletteArrowsMoveBetweenShellRows(t *testing.T) {
	s, tt, _ := readyPaletteShell(t)
	s.palette.shellHistory = []string{"git status", "git log"}
	s.openCommandPalette(nil)
	tt.Frame()
	s.palette.query = "!git pull origin main"
	tt.Frame()
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if s.palette.selected != 1 {
		t.Fatalf("Down left the selection on row %d, query %q", s.palette.selected, s.palette.query)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if s.palette.selected != 0 || s.palette.query != "!git pull origin main" {
		t.Fatalf("Up did not return to the first row: row %d, query %q", s.palette.selected, s.palette.query)
	}
}

// A custom command's row has an edit button that opens its form, and the
// palette shows the edited command afterwards.
func TestCommandPaletteCustomCommandCanBeEdited(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.pathUserData = t.TempDir()
	a.settings.CustomCommands = []customCommandSetting{{Name: "git pull", Command: "git pull origin main", Contexts: []string{"everywhere"}}}
	s.openCommandPalette(nil)
	tt.Frame()
	s.palette.query = "git pull"
	tt.Frame()
	tt.Frame()
	if err := tt.Click("Edit command"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.palette.open || !a.settingsUI.open || !a.settingsUI.customCommandFormOpen || a.settingsUI.customCommandFormEdit != 0 {
		t.Fatalf("the edit form did not open for the command: palette=%v settings=%v form=%v edit=%d",
			s.palette.open, a.settingsUI.open, a.settingsUI.customCommandFormOpen, a.settingsUI.customCommandFormEdit)
	}
	a.settingsUI.customCommandFormName = "pull main"
	a.settingsUI.customCommandFormCommand = "git pull --ff-only origin main"
	a.submitCustomCommandForm(nil)
	a.settingsUI.open = false
	s.openCommandPalette(nil)
	ctx := commandContext{Shell: s, App: a}
	found := false
	for _, candidate := range s.catalogFor(ctx).Search(ctx, "pull main", nil) {
		if candidate.Label == "pull main" && candidate.Detail == "git pull --ff-only origin main" {
			found = true
		}
	}
	if !found {
		t.Fatal("the palette does not show the edited command")
	}
}
