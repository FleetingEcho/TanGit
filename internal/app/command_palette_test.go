package app

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
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

// The Home picker has no workspace to search, so neither shortcut opens the
// palette there.
func TestCommandPaletteStaysClosedOnThePicker(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{})
	for _, mods := range []ui.Modifiers{ui.Cmd | ui.Shift, ui.Cmd} {
		tt.Key(mods, ui.KeyP)
		tt.Frame()
		if s.palette.open {
			t.Fatalf("the palette opened on the picker with modifiers %v", mods)
		}
	}
}

func TestCommandPaletteOmitsShortcutPreferences(t *testing.T) {
	s, tt, _ := readyPaletteShell(t)
	s.openCommandPalette(nil)
	tt.Frame()
	for _, label := range []string{"Enable keyboard shortcuts", "Shortcut notifications"} {
		if tt.HasText(label) {
			t.Errorf("Command Palette should leave %q in Settings", label)
		}
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
	if row, ok := tt.Find("git pull origin main"); ok {
		tt.Move(row.X+row.W/2, row.Y+row.H/2)
		tt.Frame()
		tt.Frame()
	}
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

// The commit message box and the list filter sit in a page, so the palette
// opens over them; a real dialog still keeps it from opening, and the message
// names that dialog.
func TestCommandPaletteOpensOverInlineFormsButNotDialogs(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.showCommitForm, a.filterOpen = true, true
	s.openCommandPalette(nil)
	tt.Frame()
	if !s.palette.open {
		t.Fatal("the palette did not open over the commit message box")
	}
	s.closeCommandPalette(false)
	a.showCommitForm, a.filterOpen = false, false

	a.settingsUI.open = true
	s.openCommandPalette(nil)
	if s.palette.open {
		t.Fatal("the palette opened over Settings")
	}
	if got := a.paletteBlockReason(); got != "Settings" {
		t.Fatalf("block reason = %q, want Settings", got)
	}
}

// A command form left open in Settings is not a form once Settings is closed, so
// it must not keep the palette (or the window's shortcuts) from working.
func TestClosedSettingsFormDoesNotBlockThePalette(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.openSettingsSection(settingsPageCommands)
	a.openCustomCommandForm(-1)
	a.closeSettings()
	if a.anyFormOpen() || a.paletteBlocked() {
		t.Fatalf("a closed Settings still counts as a form: %q", a.openFormName(false))
	}
	s.openCommandPalette(nil)
	tt.Frame()
	if !s.palette.open {
		t.Fatal("the palette did not open")
	}
}

// Every command row has a pin button that pins it, and the button then offers
// to unpin it.
func TestCommandPaletteRowsHaveAPinButton(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.pathUserData = t.TempDir()
	s.pathUserData = a.pathUserData
	s.openCommandPalette(nil)
	tt.Frame()
	s.palette.query = "Open Settings"
	tt.Frame()
	tt.Frame()
	// The pin is not there until the pointer is over the row, and showing it
	// does not change the row's height.
	if _, ok := tt.Find("Pin command"); ok {
		t.Fatal("the pin button shows without hovering the row")
	}
	row, ok := tt.Find("Open Settings")
	if !ok {
		t.Fatal("the row is missing")
	}
	rowBefore, _ := tt.Access("Open Settings")
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("Pin command"); !ok {
		t.Fatal("the pin button does not show when the row is hovered")
	}
	if rowAfter, _ := tt.Access("Open Settings"); rowAfter.Bounds.H != rowBefore.Bounds.H {
		t.Fatalf("hovering changed the row height from %.1f to %.1f", rowBefore.Bounds.H, rowAfter.Bounds.H)
	}
	if err := tt.Click("Pin command"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.palette.pinned) != 1 {
		t.Fatalf("pinned after the click = %v", s.palette.pinned)
	}
	tt.Frame()
	if _, ok := tt.Find("Unpin command"); !ok {
		t.Fatal("a pinned row does not offer to unpin")
	}
}

func TestCommandPaletteFilesSwitch(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.code.files = []string{"README.md", "internal/app/palette.go"}
	a.code.filesLoaded = true
	s.openCommandPalette(nil)
	tt.Frame()
	if _, ok := tt.Find("Changed files only"); ok {
		t.Fatal("the switch shows outside the file list")
	}
	s.palette.query = "/palette"
	tt.Frame()
	tt.Frame()
	if s.palette.filesChangedOnly {
		t.Fatal("the changed-files filter should start off")
	}
	if _, ok := tt.Find("internal/app/palette.go"); !ok {
		t.Fatal("the default file search missed an unchanged repository file")
	}
	if err := tt.Click("Changed files only"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if !s.palette.filesChangedOnly {
		t.Fatal("the switch did not turn on the changed-files filter")
	}
	if _, ok := tt.Find("internal/app/palette.go"); ok {
		t.Fatal("the changed-files filter still shows an unchanged file")
	}
}

func TestFileSearchShortcutPrefillsSlash(t *testing.T) {
	for _, page := range []int{sectionChanges, sectionCode} {
		t.Run(workspaceSections[page], func(t *testing.T) {
			s, tt, a := readyPaletteShell(t)
			a.workspaceTab = page
			a.code.files = []string{"README.md", "internal/app/palette.go"}
			a.code.filesLoaded = true
			s.palette.filesChangedOnly = true
			tt.Frame()
			tt.Key(ui.Cmd, ui.KeyP)
			tt.Frame()
			if !s.palette.open || s.palette.query != "/" || !tt.Focused("Search commands") || s.palette.filesChangedOnly {
				t.Fatalf("Ctrl+P did not focus the all-files palette with a slash: %+v", s.palette.query)
			}
			tt.Type("palette")
			tt.Frame()
			if s.palette.query != "/palette" || !tt.HasText("internal/app/palette.go") || tt.HasText("README.md") {
				t.Fatalf("typed file search = %q, results = %q", s.palette.query, tt.Texts())
			}
		})
	}
}

func TestCommandPaletteFilePathPansOnlyWhenTruncatedAndHovered(t *testing.T) {
	for _, test := range []struct {
		name         string
		path         string
		reduceMotion bool
		wantPan      bool
	}{
		{name: "truncated", path: strings.Repeat("deep-directory/", 6) + "login.jsx", wantPan: true},
		{name: "fits", path: "src/login.jsx"},
		{name: "reduced motion", path: strings.Repeat("deep-directory/", 6) + "login.jsx", reduceMotion: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newShell(nil)
			tt := ui.NewTesterWith(func(c *ui.Context) {
				s.commandPaletteRow(c, commandCandidate{
					ID: commandID("files/open/" + test.path), Label: test.path,
					Detail: "Open in the Editor", Group: commandGroupNavigation, Enabled: true,
				}, true, false, false, func() {})
			}, 350, 200, ui.TesterOptions{Preferences: ui.Preferences{ReduceMotion: test.reduceMotion}})
			tt.Frame()
			row, ok := tt.Find(test.path)
			if !ok {
				t.Fatal("the file row is missing")
			}
			snapshot := func() []byte {
				frame := tt.Image()
				// Sample the label above the tooltip, which may appear on hover.
				return slices.Clone(frame.Pix[:int(row.Y+25)*frame.Stride])
			}
			resting := snapshot()
			tt.Move(row.X+20, row.Y+row.H/2)
			tt.Frame()
			before := snapshot()
			tt.Advance(800 * time.Millisecond)
			tt.Frame()
			if moved := !bytes.Equal(before, snapshot()); moved != test.wantPan {
				t.Fatalf("label moved = %v, want %v", moved, test.wantPan)
			}
			if test.wantPan {
				tt.Move(20, 180)
				tt.Frame()
				tt.Advance(20 * time.Millisecond)
				tt.Frame()
				if !bytes.Equal(resting, snapshot()) {
					t.Fatal("the label did not reset after hover")
				}
			}
		})
	}
}

func TestCommandPaletteRepaintsWhenRepositoryFilesFinishLoading(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.code.filesLoading = true
	s.openCommandPalette(nil)
	s.palette.query = "/palette"
	tt.Frame()
	tt.Frame()
	repaints := 0
	s.invalidate = func() { repaints++ }
	s.updateAppChanged(a, func() bool {
		a.code.filesLoading = false
		a.code.filesLoaded = true
		a.code.files = []string{"internal/app/palette.go"}
		return false // The Editor page is not visible.
	})
	if repaints != 1 {
		t.Fatalf("file list completion caused %d repaints, want 1", repaints)
	}
	tt.Frame()
	if _, ok := tt.Find("internal/app/palette.go"); !ok {
		t.Fatal("the palette did not show the loaded file")
	}
}

// Choosing a file in the / list opens it in the Editor and shows the Editor page.
func TestCommandPaletteFileOpensInTheEditor(t *testing.T) {
	s, tt, a := readyPaletteShell(t)
	a.snapshot.Root = t.TempDir()
	a.code.files = []string{"internal/app/palette.go"}
	a.code.filesLoaded = true
	a.setWorkspaceTab(sectionChanges)
	s.openCommandPalette(nil)
	tt.Frame()
	s.palette.query = "/palette"
	tt.Frame()
	tt.Frame()
	s.invokeCommand("files/open/internal/app/palette.go")
	tt.Frame()
	if a.workspaceTab != sectionCode || a.code.active != "internal/app/palette.go" {
		t.Fatalf("the file did not open in the Editor: tab %d, active %q", a.workspaceTab, a.code.active)
	}
}
