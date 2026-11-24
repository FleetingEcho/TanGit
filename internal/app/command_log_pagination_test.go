package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func commandLogFilterChoice(t *testing.T, tester *ui.Tester, filter, choice string) {
	t.Helper()
	if err := tester.Click(filter); err != nil {
		t.Fatalf("open %s: %v", filter, err)
	}
	tester.Frame()
	item, found := tester.AccessByRole(ui.RoleOption, choice)
	if !found {
		item, found = tester.AccessByRole(ui.RoleMenuItem, choice)
	}
	if !found {
		for _, candidate := range tester.AccessAll() {
			if candidate.Name == choice && (candidate.Role == ui.RoleMenuItemCheckBox || candidate.Role == ui.RoleMenuItemRadio) {
				item, found = candidate, true
				break
			}
		}
	}
	if !found || item.Bounds.W == 0 || item.Bounds.H == 0 {
		t.Fatalf("%s option %q is not visible: %q", filter, choice, tester.Texts())
	}
	tester.ClickAt(item.Bounds.X+item.Bounds.W/2, item.Bounds.Y+item.Bounds.H/2)
	tester.Frame()
}

func TestCommandLogTimeAndStatusFilters(t *testing.T) {
	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.Local)
	entries := []repository.CommandEntry{
		{At: now.Add(-10 * time.Minute), Command: "git rev-parse HEAD"},
		{At: now.Add(-2 * time.Hour), Command: "git bisect log"},
		{At: now.Add(-12 * time.Hour), Command: "git worktree list --porcelain"},
		{At: now.Add(-48 * time.Hour), Command: "git diff HEAD"},
		{At: now.Add(-4 * 24 * time.Hour), Command: "git ls-files -u -z"},
		{At: now.Add(-30 * 24 * time.Hour), Command: "git for-each-ref"},
		{At: time.Time{}, Command: "unknown timestamp"},
		{At: now.Add(time.Hour), Command: "future timestamp"},
	}
	allCommands := []string{"git rev-parse HEAD", "git bisect log", "git worktree list --porcelain", "git diff HEAD", "git ls-files -u -z", "git for-each-ref", "unknown timestamp", "future timestamp"}
	cases := []struct {
		window string
		want   []string
	}{
		{"", allCommands},
		{commandLogRangeHour, []string{"git rev-parse HEAD"}},
		{commandLogRangeThreeHours, []string{"git rev-parse HEAD", "git bisect log"}},
		{commandLogRangeDay, []string{"git rev-parse HEAD", "git bisect log", "git worktree list --porcelain"}},
		{commandLogRangeThreeDays, []string{"git rev-parse HEAD", "git bisect log", "git worktree list --porcelain", "git diff HEAD"}},
		{commandLogRangeWeek, []string{"git rev-parse HEAD", "git bisect log", "git worktree list --porcelain", "git diff HEAD", "git ls-files -u -z"}},
		{commandLogRangeAll, allCommands},
	}
	for _, test := range cases {
		t.Run(test.window, func(t *testing.T) {
			matches := map[string]bool{}
			for _, entry := range entries {
				if commandLogMatchesFilters(entry, repository.CommandOriginGit, "", "", "", test.window, now) {
					matches[entry.Command] = true
				}
			}
			if len(matches) != len(test.want) {
				t.Fatalf("matches = %v, want %v", matches, test.want)
			}
			for _, command := range test.want {
				if !matches[command] {
					t.Errorf("window %q excluded %q: %v", test.window, command, matches)
				}
			}
		})
	}

	failed := repository.CommandEntry{At: now, Failed: true}
	running := repository.CommandEntry{At: now, Running: true}
	if commandLogEntryStatus(failed) != commandLogStatusFailed || commandLogEntryStatus(running) != commandLogStatusRunning {
		t.Fatal("failed/running status is not distinct from successful completion")
	}
	for _, entry := range []repository.CommandEntry{{At: now}, failed, running} {
		if !commandLogMatchesFilters(entry, repository.CommandOriginGit, "", commandLogEntryStatus(entry), "", "", now) {
			t.Errorf("status filter excluded matching entry: %+v", entry)
		}
	}
}

func TestCommandLogLazyPagesFollowUserScrollAndStableIDs(t *testing.T) {
	a, tester, _ := realCommandHarness(t)
	a.attachRepositoryCommandLog(repository.NewCommandLog())
	for i := range 55 {
		a.commandLog.Record(fmt.Sprintf("git status %02d", i), "", i%7 == 0)
	}
	tester.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.openCommandLog()
	tester.Frame()
	if a.commandLogLimit != 20 || !tester.HasText("$ git status 54") || tester.HasText("$ git status 34") {
		t.Fatalf("initial page is not the newest 20: limit=%d texts=%q", a.commandLogLimit, tester.Texts())
	}
	if !tester.HasText("Showing 20 of 55 commands") {
		t.Fatalf("missing page progress: %q", tester.Texts())
	}
	page, found := tester.Find("Command history")
	if !found {
		t.Fatal("virtual command list has no accessible geometry")
	}
	scrollEnd := func() {
		tester.Scroll(page.X+page.W/2, page.Y+page.H/2, 0, 10000)
	}
	scrollEnd()
	if a.commandLogLimit != 40 || len(a.commandLogVisibleEntries()) != 55 || !tester.HasText("$ git status 34") || !tester.HasText("Showing 40 of 55 commands") {
		t.Fatalf("first user scroll did not load exactly 20 more: limit=%d texts=%q", a.commandLogLimit, tester.Texts())
	}
	tester.Frame()
	if a.commandLogLimit != 40 {
		t.Fatalf("repainting automatically loaded another page: %d", a.commandLogLimit)
	}
	if first, _ := a.commandLogList.Visible(); first == 0 {
		t.Fatal("pagination test never moved away from the newest rows")
	} else {
		id := a.commandLogVisibleEntries()[55-1-first].ID()
		a.commandLog.Record("git status newer", "", false)
		tester.Frame()
		newFirst, _ := a.commandLogList.Visible()
		if got := a.commandLogVisibleEntries()[56-1-newFirst].ID(); got != id {
			t.Fatalf("new record changed the row under the reader: got ID %d, want %d", got, id)
		}
	}
	scrollEnd()
	if a.commandLogLimit != len(a.commandLogVisibleEntries()) || len(a.commandLogVisibleEntries()) != 56 || tester.HasText("Scroll down to load more") {
		t.Fatalf("second page did not reach the final partial page: limit=%d texts=%q", a.commandLogLimit, tester.Texts())
	}
	scrollEnd()
	if a.commandLogLimit != 56 || len(a.commandLogEntries()) != 56 {
		t.Fatalf("end-of-log loading or recording diverged: page=%d records=%d", a.commandLogLimit, len(a.commandLogEntries()))
	}
}

func TestCommandLogStatusAndSourceMenusUpdateRowsAndResetPages(t *testing.T) {
	a, tester, _ := realCommandHarness(t)
	a.attachRepositoryCommandLog(repository.NewCommandLog())
	a.commandLog.Record("git status", "clean", false)
	a.commandLog.Record("git push", "rejected", true)
	a.commandLog.Begin("git fetch")
	a.commandLog.RecordPalette("workspace/changes", "Open Changes")
	a.commandLog.RecordShortcut(repository.ShortcutInvocation{ID: "changes/mark-resolved", Chord: "m", Label: "Mark resolved"})
	a.commandLog.BeginUserCommand("printf direct")("direct output", false)
	a.openCommandLog()
	a.commandLogLimit = 40
	tester.Frame()

	commandLogFilterChoice(t, tester, "Status filter", "Failed")
	if a.commandLogLimit > 20 || len(a.commandLogVisibleEntries()) != 1 || !tester.HasText("$ git push") || tester.HasText("$ git status") || tester.HasText("$ git fetch") {
		t.Fatalf("failed status did not filter and reset the page: %q", tester.Texts())
	}
	commandLogFilterChoice(t, tester, "Status filter", "Running")
	if !tester.HasText("$ git fetch") || tester.HasText("$ git push") {
		t.Fatalf("running status is not distinct: %q", tester.Texts())
	}
	commandLogFilterChoice(t, tester, "Status filter", "All statuses")
	commandLogFilterChoice(t, tester, "Source filter", "Command Palette")
	if !a.commandLogUser || !tester.HasText("Open Changes") || tester.HasText("$ git status") {
		t.Fatalf("palette source did not select accepted palette actions: %q", tester.Texts())
	}
	commandLogFilterChoice(t, tester, "Source filter", "Keyboard")
	if !tester.HasText("[m] Mark resolved") || tester.HasText("Open Changes") {
		t.Fatalf("keyboard source did not retain command identity: %q", tester.Texts())
	}
	commandLogFilterChoice(t, tester, "Source filter", "Your commands")
	if !tester.HasText("$ printf direct") || tester.HasText("[m] Mark resolved") {
		t.Fatalf("direct-command source did not select only direct commands: %q", tester.Texts())
	}
	commandLogFilterChoice(t, tester, "Source filter", "Background Git commands")
	if a.commandLogUser || !tester.HasText("$ git push") || tester.HasText("$ printf direct") {
		t.Fatalf("background source did not return to Git commands: %q", tester.Texts())
	}
	commandLogFilterChoice(t, tester, "Time range filter", "Last hour")
	if got := len(a.commandLogVisibleEntries()); got != 3 {
		t.Fatalf("last hour returned %d background commands, want 3", got)
	}
	commandLogFilterChoice(t, tester, "Time range filter", "Last week")
	if a.commandLogRange != commandLogRangeWeek {
		t.Fatalf("week filter was not applied: %q", a.commandLogRange)
	}
	if err := tester.Click("Clear filters"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if a.commandLogStatus != "" || a.commandLogSource != "" || a.commandLogRange != commandLogRangeAll || a.commandLogLimit > 20 {
		t.Fatalf("clear filters did not reset the display: status=%q source=%q range=%q limit=%d",
			a.commandLogStatus, a.commandLogSource, a.commandLogRange, a.commandLogLimit)
	}
}

func TestCommandLogCompactRowOutputMenu(t *testing.T) {
	a, tester, _ := realCommandHarness(t)
	a.attachRepositoryCommandLog(repository.NewCommandLog())
	a.commandLog.BeginUserCommand("printf output")("copiable output", false)
	openUserCommandLogForTest(t, a, tester)
	if tester.HasText("copiable output") {
		t.Fatal("ordinary output should be collapsed in the compact row")
	}
	commandLogFilterChoice(t, tester, "Command actions", "Show output")
	if !tester.HasText("copiable output") {
		t.Fatalf("show output did not reveal the command result: %q", tester.Texts())
	}
	commandLogFilterChoice(t, tester, "Command actions", "Copy output")
	if got := tester.Clipboard(); got != "copiable output" {
		t.Fatalf("copied output = %q", got)
	}
	commandLogFilterChoice(t, tester, "Command actions", "Hide output")
	if tester.HasText("copiable output") {
		t.Fatal("hide output did not restore the compact row")
	}
}
