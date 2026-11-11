package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestRememberRecentMovesToFrontAndDropsDuplicates(t *testing.T) {
	references := []recentRepository{{Path: "/w/a", LastOpened: 1}, {Path: "/w/b", LastOpened: 2}}
	out := rememberRecent(references, "/w/b", 99)
	if len(out) != 2 {
		t.Fatalf("want 2 repositories, got %d", len(out))
	}
	if out[0].Path != "/w/b" || out[0].LastOpened != 99 {
		t.Fatalf("reopened repository is not first with the new time: %+v", out[0])
	}
	if out[1].Path != "/w/a" {
		t.Fatalf("older repository missing: %+v", out)
	}
	// The caller's slice is never shared with the app's copy.
	references[0].Path = "/changed"
	if out[1].Path != "/w/a" {
		t.Fatalf("rememberRecent returned a slice sharing its input: %+v", out)
	}
}

func TestRememberRecentCapsTheListAndIgnoresEmptyPaths(t *testing.T) {
	var out []recentRepository
	for i := range maxRecentRepositories + 3 {
		out = rememberRecent(out, repoPath(i), int64(i))
	}
	if len(out) != maxRecentRepositories {
		t.Fatalf("want %d stored, got %d", maxRecentRepositories, len(out))
	}
	if out[0].Path != repoPath(maxRecentRepositories+2) {
		t.Fatalf("newest repository should be first, got %q", out[0].Path)
	}
	if kept := rememberRecent(out, "  ", 1); len(kept) != len(out) {
		t.Fatalf("an empty path must not enter the list: %+v", kept)
	}
}

func TestRememberRecentSeparatesRemoteProfilesAndCanonicalizesRoots(t *testing.T) {
	references := []recentRepository{{Path: "ssh://first/work/old/../project", LastOpened: 1, Name: "Remote"}}
	references = rememberRecent(references, "ssh://first/work/project", 2)
	references = rememberRecent(references, "ssh://first/work/./project/../project", 3)
	if len(references) != 1 || references[0].Path != "ssh://first/work/project" ||
		references[0].LastOpened != 3 || references[0].Name != "Remote" {
		t.Fatalf("canonical remote reopen created incorrect history: %+v", references)
	}
	references = rememberRecent(references, "ssh://second/work/project", 4)
	if len(references) != 2 {
		t.Fatalf("identical roots on different SSH profiles were deduplicated: %+v", references)
	}
}

func TestRecentStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := []recentRepository{{Path: "/w/a", LastOpened: 42}, {Path: "/w/b", LastOpened: 7}}
	if err := saveRecentRepositories(dir, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadRecentRepositories(dir)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("round trip changed the history: want %+v, got %+v", want, got)
	}
	if !filepath.IsAbs(recentStorePath(dir)) {
		t.Fatalf("history path is not absolute: %q", recentStorePath(dir))
	}
}

func TestRecentStoreToleratesAMissingOrDamagedFile(t *testing.T) {
	dir := t.TempDir()
	missing := loadRecentRepositories(dir)
	if missing != nil {
		t.Fatalf("a missing file must give an empty history, got %+v", missing)
	}
	if err := os.WriteFile(recentStorePath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	damaged := loadRecentRepositories(dir)
	if damaged != nil {
		t.Fatalf("a damaged file must give an empty history, got %+v", damaged)
	}
	// No directory to store in is not an error either.
	if err := saveRecentRepositories("", nil); err != nil {
		t.Fatalf("saving without a directory: %v", err)
	}
}

// recentTester builds an app still on the picker with the given history.
func recentTester(t *testing.T, repositories ...recentRepository) (*app, *ui.Tester, *updates) {
	t.Helper()
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Files:  []repository.File{{Path: "one.txt", Status: "M ", Staged: true, Tracked: true}},
	}}})
	a.state = stateEmpty
	a.recentRepositories = repositories
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	// Build one frame so the labels exist to click.
	tt.Frame()
	return a, tt, updated
}

func TestRecentPickerListsHistory(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/w/first-project", LastOpened: timeNow()},
		recentRepository{Path: "/w/second-project", LastOpened: timeNow()},
	)
	texts := strings.Join(tt.Texts(), " ")
	for _, want := range []string{"Recent repositories", "/w/first-project", "/w/second-project"} {
		if !strings.Contains(texts, want) {
			t.Errorf("picker does not show %q; texts=%v", want, tt.Texts())
		}
	}
	if a.state != stateEmpty {
		t.Fatalf("state changed without any action: %v", a.state)
	}
}

func TestRecentPickerOffersNoListWhenHistoryIsEmpty(t *testing.T) {
	_, tt, _ := recentTester(t)
	texts := strings.Join(tt.Texts(), " ")
	if strings.Contains(texts, "Recent repositories") {
		t.Fatalf("the welcome text must stand in without history: %v", tt.Texts())
	}
	if !strings.Contains(texts, "Open a Git repository to begin") {
		t.Fatalf("welcome text missing: %v", tt.Texts())
	}
}

func TestFilterRecentMatchesNamesAndPathsWithoutLosingOriginalIndices(t *testing.T) {
	repositories := []recentRepository{
		{Path: "/work/api", Name: "Backend service"},
		{Path: "/work/website"},
		{Path: "/work/mobile", Name: "Client app"},
	}
	got := filterRecent(repositories, "CLIENT")
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("nickname match = %v, want original index [2]", got)
	}
	got = filterRecent(repositories, "site")
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("path match = %v, want original index [1]", got)
	}
	got = filterRecent(repositories, "work")
	if len(got) != 3 {
		t.Fatalf("shared path should match every entry, got %v", got)
	}
	if got := filterRecent(repositories, "missing"); len(got) != 0 {
		t.Fatalf("unmatched filter returned %v", got)
	}
}

func TestRecentSearchOpensTheMatchingOriginalRepository(t *testing.T) {
	opener := &recordedOpener{snapshot: repository.Snapshot{Root: "/work/second", Branch: "main"}}
	a := newApp(opener)
	a.state = stateEmpty
	a.recentRepositories = []recentRepository{
		{Path: "/work/first", Name: "Alpha", LastOpened: timeNow()},
		{Path: "/work/second", Name: "Beta", LastOpened: timeNow() - 60},
	}
	updates := newUpdates()
	a.update = updates.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click("Search recent repositories"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Beta")
	tt.Frame()
	if len(a.visibleRecent) != 1 || a.visibleRecent[0] != 1 {
		t.Fatalf("filtered grid = %v, want original index [1]", a.visibleRecent)
	}
	if tt.HasText("/work/first") || !tt.HasText("/work/second") {
		t.Fatalf("search did not narrow the cards: %v", tt.Texts())
	}
	if err := tt.Click("/work/second"); err != nil {
		t.Fatal(err)
	}
	for range 8 {
		if a.state == stateReady && len(updates.done) == 0 {
			break
		}
		updates.waitForUpdate(t)
		tt.Frame()
	}
	if len(opener.opened) != 1 || opener.opened[0] != "/work/second" {
		t.Fatalf("filtered card opened %v, want /work/second", opener.opened)
	}
}

func TestRecentSearchNoMatchHasAHelpfulEmptyState(t *testing.T) {
	a, tt, _ := recentTester(t, recentRepository{Path: "/work/project", LastOpened: timeNow()})
	if err := tt.Click("Search recent repositories"); err != nil {
		t.Fatal(err)
	}
	tt.Type("nothing-here")
	tt.Frame()
	if !tt.HasText("No repositories match your search") || tt.HasText("/work/project") {
		t.Fatalf("no-match state is missing or still shows the card: %v", tt.Texts())
	}
	if len(a.visibleRecent) != 0 {
		t.Fatalf("no-match grid contains %v", a.visibleRecent)
	}
}

func TestFilteredRecentRenameAndRemoveTargetTheOriginalCard(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/work/first", Name: "Alpha", LastOpened: timeNow()},
		recentRepository{Path: "/work/second", Name: "Beta", LastOpened: timeNow()})
	if err := tt.Click("Search recent repositories"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Beta")
	tt.Frame()
	if err := tt.Click("Rename /work/second"); err != nil {
		t.Fatal(err)
	}
	if a.renameRecentPath != "/work/second" || !a.showRenameRecent {
		t.Fatalf("rename targeted %q, open=%v", a.renameRecentPath, a.showRenameRecent)
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if err := tt.Click("Remove /work/second from the recent list"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if len(a.recentRepositories) != 1 || a.recentRepositories[0].Path != "/work/first" {
		t.Fatalf("remove left the wrong history: %+v", a.recentRepositories)
	}
}

func TestRecentRowContextMenuHidesEntry(t *testing.T) {
	a, tt, _ := recentTester(t, recentRepository{Path: "/w/keep", LastOpened: timeNow()},
		recentRepository{Path: "/w/hide", LastOpened: timeNow()})
	if err := tt.RightClick("/w/hide"); err != nil {
		t.Fatalf("right click: %v", err)
	}
	tt.Frame()
	if a.state != stateEmpty {
		t.Fatalf("a right click must open the menu, not the repository: state=%v", a.state)
	}
	if !tt.HasText("Remove from list") {
		t.Fatalf("the row menu misses Remove from list: %v", tt.Texts())
	}
	if err := tt.Click("Remove from list"); err != nil {
		t.Fatalf("choose: %v", err)
	}
	// The menu defers the change to the next pass, so the frame that
	// still holds the menu keeps its rows.
	tt.Frame()
	tt.Frame()
	texts := strings.Join(tt.Texts(), " ")
	if strings.Contains(texts, "/w/hide") {
		t.Fatalf("entry was not hidden: %v", tt.Texts())
	}
	if !strings.Contains(texts, "/w/keep") {
		t.Fatalf("the other entry went with it: %v", tt.Texts())
	}
	if len(a.recentRepositories) != 1 {
		t.Fatalf("history not updated: %+v", a.recentRepositories)
	}
}

func TestRecentRowOpensTheRepositoryOnAClick(t *testing.T) {
	a, tt, updates := recentTester(t, recentRepository{Path: "/work/project", LastOpened: timeNow()})
	if err := tt.Click("/work/project"); err != nil {
		t.Fatalf("click row: %v", err)
	}
	for range 8 {
		if a.state == stateReady && len(updates.done) == 0 {
			break
		}
		updates.waitForUpdate(t)
		tt.Frame()
	}
	if a.state != stateReady {
		t.Fatalf("one click did not open the repository: state=%v err=%q", a.state, a.errMessage)
	}
	if a.path != "/work/project" {
		t.Fatalf("opened path is %q, want the clicked repository", a.path)
	}
	// Opening it again moves it to the front of the history.
	if a.recentRepositories[0].Path != "/work/project" {
		t.Fatalf("opened repository not remembered: %+v", a.recentRepositories)
	}
}

func TestOpenedRepositoryIsRemembered(t *testing.T) {
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root: "/work/new-project", Branch: "main",
	}}})
	updated := newUpdates()
	a.update = updated.update
	a.state = stateEmpty
	a.path = "/work/new-project/"
	a.beginOpen()
	for range 8 {
		if a.state == stateReady && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
	}
	if a.state != stateReady {
		t.Fatalf("open failed: state=%v err=%q", a.state, a.errMessage)
	}
	if len(a.recentRepositories) != 1 || a.recentRepositories[0].Path != "/work/new-project" {
		t.Fatalf("history not kept after an open: %+v", a.recentRepositories)
	}
}

func TestHistoryStaysVisibleAfterAFailedOpen(t *testing.T) {
	a, tt, _ := recentTester(t, recentRepository{Path: "/w/keep", LastOpened: timeNow()})
	a.state = stateError
	a.errMessage = "not a Git repository"
	tt.Frame()
	texts := strings.Join(tt.Texts(), " ")
	for _, want := range []string{"Could not open repository", "not a Git repository",
		"Recent repositories", "/w/keep"} {
		if !strings.Contains(texts, want) {
			t.Errorf("the failed open hides %q; texts=%v", want, tt.Texts())
		}
	}
}

func TestEnterWithAnEmptyFieldOpensTheSelectedRow(t *testing.T) {
	opener := &recordedOpener{snapshot: repository.Snapshot{
		Root:   "/work/elsewhere",
		Branch: "main",
	}}
	a := newApp(opener)
	a.state = stateEmpty
	a.recentRepositories = []recentRepository{{Path: "/w/chosen", LastOpened: timeNow()}}
	updates := newUpdates()
	a.update = updates.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if a.path != "" {
		t.Fatalf("the picker must start with an empty field, got %q", a.path)
	}
	// The picker has no separate Open button: Enter in the field opens the
	// path it holds, or, while empty, the row the history highlights.
	openFixture(t, tt)
	for range 8 {
		if a.state == stateReady && len(updates.done) == 0 {
			break
		}
		updates.waitForUpdate(t)
		tt.Frame()
	}
	if a.state != stateReady {
		t.Fatalf("Enter did not use the highlighted row: state=%v err=%q", a.state, a.errMessage)
	}
	if len(opener.opened) != 1 || opener.opened[0] != "/w/chosen" {
		t.Fatalf("Enter used the field's inverse of the highlighted row: %v", opener.opened)
	}
}

// TestHistorySurvivesARestart is the promise of the picker: what an earlier
// run opened is on screen when the app starts again.
func TestHistorySurvivesARestart(t *testing.T) {
	dir := t.TempDir()

	// First run: the user opens a repository, which is written to disk.
	first := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root:   "/w/reopened",
		Branch: "main",
	}}})
	first.pathUserData = dir
	first.state = stateEmpty
	first.path = "/w/reopened"
	updates := newUpdates()
	first.update = updates.update
	first.beginOpen()
	for range 8 {
		if first.state == stateReady && len(updates.done) == 0 {
			break
		}
		updates.waitForUpdate(t)
	}
	if first.state != stateReady {
		t.Fatalf("first run did not open the repository: state=%v err=%q", first.state, first.errMessage)
	}

	// Second run: a fresh app, as the launcher starts it, reads the file.
	second := newApp(fakeOpener{})
	second.pathUserData = dir
	second.loadHistory()
	if len(second.recentRepositories) != 1 || second.recentRepositories[0].Path != "/w/reopened" {
		t.Fatalf("history lost across a restart: %+v", second.recentRepositories)
	}
	tt := ui.NewTester(second.view, 1280, 800)
	tt.Frame()
	texts := strings.Join(tt.Texts(), " ")
	for _, want := range []string{"Recent repositories", "/w/reopened"} {
		if !strings.Contains(texts, want) {
			t.Errorf("homepage misses %q after a restart; texts=%v", want, tt.Texts())
		}
	}
}

func TestHistoryWaitsForTheDataDirectory(t *testing.T) {
	dir := t.TempDir()
	want := []recentRepository{{Path: "/w/waited", LastOpened: 5}}
	if err := saveRecentRepositories(dir, want); err != nil {
		t.Fatalf("save: %v", err)
	}

	a := newApp(fakeOpener{})
	// Before a window exists there may be no directory to read from; the
	// picker then starts empty instead of failing, and tries again.
	a.loadHistory()
	if a.historyLoaded {
		t.Fatal("the history must not be considered read without a directory")
	}
	if len(a.recentRepositories) != 0 {
		t.Fatalf("nothing could have been read: %+v", a.recentRepositories)
	}

	a.pathUserData = dir
	a.loadHistory()
	if !a.historyLoaded || len(a.recentRepositories) != 1 ||
		a.recentRepositories[0].Path != "/w/waited" {
		t.Fatalf("history not read once the directory was known: loaded=%v %+v",
			a.historyLoaded, a.recentRepositories)
	}

	// Reading it again never overwrites what the running app has in memory.
	a.recentRepositories = nil
	a.loadHistory()
	if a.recentRepositories != nil {
		t.Fatalf("the file was re-read on a later pass: %+v", a.recentRepositories)
	}
}

// recordedOpener remembers the paths it was asked to open, so a test can
// check which repository the picker handed over.
type recordedOpener struct {
	snapshot repository.Snapshot
	opened   []string
	inited   []string
	initErr  error
	// notRepository makes Open refuse until Init has run, as a plain folder
	// does, and lets the picker adopt it afterwards.
	notRepository bool
}

func (o *recordedOpener) Open(_ context.Context, path string) (repository.Session, error) {
	o.opened = append(o.opened, path)
	if o.notRepository {
		return nil, repository.ErrNotRepository
	}
	return fakeSession{snapshot: o.snapshot}, nil
}

func (o *recordedOpener) Init(_ context.Context, path string) error {
	o.inited = append(o.inited, path)
	if o.initErr != nil {
		return o.initErr
	}
	o.notRepository = false
	return nil
}

// repoPath names a distinct repository path for index i.
func repoPath(i int) string {
	return "/work/repo-" + string(rune('a'+i))
}

// The name given to a card survives opening the repository again: it belongs
// to the card, not to one opening of it.
func TestRememberRecentKeepsTheCardName(t *testing.T) {
	out := rememberRecent([]recentRepository{
		{Path: "/w/a", LastOpened: 1, Name: "Client site"},
	}, "/w/a", 99)
	if out[0].Name != "Client site" {
		t.Fatalf("reopening dropped the card's name: %+v", out[0])
	}
	if out[0].LastOpened != 99 {
		t.Fatalf("reopening did not move the entry to now: %+v", out[0])
	}
}

// A card's name is the card's alone: the pencil on it opens a modal that
// names it, and the repository's path is untouched.
func TestRecentCardNameIsRenamedFromThePencil(t *testing.T) {
	dir := t.TempDir()
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/w/client/project", LastOpened: timeNow()},
		recentRepository{Path: "/w/other/project", LastOpened: timeNow() - 60},
	)
	a.pathUserData = dir
	if err := tt.Click("Rename /w/client/project"); err != nil {
		t.Fatalf("open the rename modal: %v", err)
	}
	tt.Frame()
	if !a.showRenameRecent {
		t.Fatal("the pencil opened no rename modal")
	}
	if a.state != stateEmpty {
		t.Fatalf("the pencil must name the card, not open it: state=%v", a.state)
	}
	for _, want := range []string{"Rename repository", "Card name", "Save name"} {
		if !tt.HasText(want) {
			t.Fatalf("the rename modal is missing %q: %v", want, tt.Texts())
		}
	}
	tt.Type("Client site")
	tt.Frame()
	if err := tt.Click("Save name"); err != nil {
		t.Fatalf("save the name: %v", err)
	}
	// The frame that holds the form leaves the list to the next pass, as a
	// removal does.
	tt.Frame()
	tt.Frame()

	if a.showRenameRecent {
		t.Fatal("saving left the rename modal open")
	}
	if got := a.recentRepositories[0].Name; got != "Client site" {
		t.Fatalf("the card's name is %q, want Client site", got)
	}
	if got := a.recentRepositories[0].Path; got != "/w/client/project" {
		t.Fatalf("the repository path changed to %q", got)
	}
	texts := strings.Join(tt.Texts(), " ")
	if !strings.Contains(texts, "Client site") {
		t.Fatalf("the renamed card still shows its folder name: %v", tt.Texts())
	}
	// The name is written to the store, so the next start shows it.
	stored := loadRecentRepositories(dir)
	if len(stored) != 2 || stored[0].Name != "Client site" ||
		stored[0].Path != "/w/client/project" {
		t.Fatalf("the name did not reach the store: %+v", stored)
	}
}

// Escape closes the rename modal without touching the name.
func TestRecentRenameEscapeLeavesTheNameAlone(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/w/project", LastOpened: timeNow(), Name: "Old name"},
	)
	if err := tt.Click("Rename /w/project"); err != nil {
		t.Fatalf("open the rename modal: %v", err)
	}
	tt.Frame()
	tt.Type("New name")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.showRenameRecent {
		t.Fatal("Escape left the rename modal open")
	}
	if got := a.recentRepositories[0].Name; got != "Old name" {
		t.Fatalf("Escape changed the name to %q", got)
	}
}

// An empty name, or the folder's own, leaves the card showing the folder
// name; the context menu offers the same rename as the pencil.
func TestRecentRenameClearsBackToTheFolderName(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/w/project", LastOpened: timeNow(), Name: "Nickname"},
	)
	if err := tt.RightClick("/w/project"); err != nil {
		t.Fatalf("right click: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Rename…") {
		t.Fatalf("the row menu misses Rename…: %v", tt.Texts())
	}
	if err := tt.Click("Rename…"); err != nil {
		t.Fatalf("choose Rename…: %v", err)
	}
	tt.Frame()
	if !a.showRenameRecent {
		t.Fatal("Rename… opened no modal")
	}
	// The field starts on the name the card holds, so it can be replaced.
	if a.renameRecentName != "Nickname" {
		t.Fatalf("the field starts at %q, want the card's name", a.renameRecentName)
	}
	a.renameRecentName = ""
	if err := tt.Click("Save name"); err != nil {
		t.Fatalf("save the empty name: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.recentRepositories[0].Name; got != "" {
		t.Fatalf("the card kept the name %q", got)
	}
	texts := strings.Join(tt.Texts(), " ")
	if strings.Contains(texts, "Nickname") || !strings.Contains(texts, "project") {
		t.Fatalf("the card does not fall back to its folder name: %v", tt.Texts())
	}
}

// A name is bounded to what a card can show, and the folder's own name is
// the same as having none: the card falls back to it either way.
func TestRecentCardNameIsBoundedAndFallsBackToTheFolderName(t *testing.T) {
	a, _, _ := recentTester(t,
		recentRepository{Path: "/w/project", LastOpened: timeNow()},
	)
	a.pathUserData = t.TempDir()
	long := "名" + strings.Repeat("字", recentNameLimit+5)
	if !a.renameRecentEntry("/w/project", long) {
		t.Fatal("the name was not stored")
	}
	if got := []rune(a.recentRepositories[0].Name); len(got) != recentNameLimit {
		t.Fatalf("the name was stored %d characters long, want %d", len(got), recentNameLimit)
	}
	if got := a.recentRepositories[0].displayName(); got != a.recentRepositories[0].Name {
		t.Fatalf("the card shows %q, not the name given", got)
	}

	if !a.renameRecentEntry("/w/project", "project") {
		t.Fatal("the folder's own name was not taken as a clearing")
	}
	if got := a.recentRepositories[0].Name; got != "" {
		t.Fatalf("the card kept the name %q", got)
	}
	if got := a.recentRepositories[0].displayName(); got != "project" {
		t.Fatalf("the card shows %q, want the folder name", got)
	}
}

// Enter in the rename field saves the name, and is not also the picker's key
// to open the repository behind the modal.
func TestRecentRenameSavesOnEnter(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/w/project", LastOpened: timeNow()},
	)
	if err := tt.Click("Rename /w/project"); err != nil {
		t.Fatalf("open the rename modal: %v", err)
	}
	tt.Frame()
	tt.Type("House")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()
	if a.showRenameRecent {
		t.Fatal("Enter left the rename modal open")
	}
	if a.state != stateEmpty {
		t.Fatalf("Enter in the field opened the repository: state=%v", a.state)
	}
	if got := a.recentRepositories[0].Name; got != "House" {
		t.Fatalf("Enter saved the name %q", got)
	}
}
