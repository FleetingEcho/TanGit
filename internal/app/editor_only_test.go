package app

import (
	"errors"
	"github.com/FleetingEcho/lazygitGo/internal/app/i18n"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func waitForEditorOnlyOpen(t *testing.T, a *app, tt *ui.Tester, updated *updates) {
	t.Helper()
	for range 12 {
		if a.state == stateReady && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	tt.Frame()
	if a.state != stateReady || !a.editorOnlyWorkspace() {
		t.Fatalf("plain folder did not become Editor-only: state=%v session=%T err=%q", a.state, a.session, a.errMessage)
	}
}

func TestHomeEditorModeOpensPlainFolderWithoutInitialization(t *testing.T) {
	opener := &recordedOpener{notRepository: true}
	a := newApp(opener)
	a.state = stateEmpty
	root := t.TempDir()
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()

	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Open a folder") || tt.HasText("Clone") {
		t.Fatalf("Editor mode did not update Home: %q", tt.Texts())
	}
	visibleBefore := slices.Clone(a.settings.VisibleSections)
	orderBefore := slices.Clone(a.settings.SectionOrder)
	a.path = root
	a.openFromPicker()
	waitForEditorOnlyOpen(t, a, tt, updated)

	if got, want := a.snapshot.Root, cleanRepoPath(root); got != want {
		t.Fatalf("plain root = %q, want %q", got, want)
	}
	if a.workspaceTab != sectionCode || !slices.Equal(a.visibleSections(), []int{sectionCode}) {
		t.Fatalf("plain workspace sections = %v, active = %d", a.visibleSections(), a.workspaceTab)
	}
	a.setWorkspaceTab(sectionHistory)
	if a.workspaceTab != sectionCode {
		t.Fatal("a hidden Git section opened in the plain workspace")
	}
	if !slices.Equal(a.settings.VisibleSections, visibleBefore) || !slices.Equal(a.settings.SectionOrder, orderBefore) {
		t.Fatal("plain-folder visibility changed the saved Git section preferences")
	}
	if len(opener.inited) != 0 || len(a.recentRepositories) != 1 || !a.recentRepositories[0].EditorOnly {
		t.Fatalf("plain open initialized Git or filed its card incorrectly: init=%v recents=%v", opener.inited, a.recentRepositories)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plain open created .git or could not check it: %v", err)
	}
}

func TestPickerOpenModeSelectedSegmentIsHighlightedInLightTheme(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settings.Theme = "System"
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetDark(false)
	tt.Frame()
	theme := themeForSettings(a.settings, false)
	wantFace := theme.Accent.Alpha(0.14).Over(theme.Surface).SRGB()

	assertSelection := func(selected, unselected string) {
		t.Helper()
		tt.Frame()
		if !rectHasColor(tt, selected, theme.Accent) {
			t.Errorf("%s does not use the accent text of a selected mode", selected)
		}
		got := buttonInk(t, tt, selected, -0.05, 0.5)
		if colorChannelDelta(got.R, wantFace.R) > 2 ||
			colorChannelDelta(got.G, wantFace.G) > 2 ||
			colorChannelDelta(got.B, wantFace.B) > 2 {
			t.Errorf("%s face = %v, want accent highlight %v", selected, got, wantFace)
		}
		if other := buttonInk(t, tt, unselected, -0.05, 0.5); other == got {
			t.Errorf("selected %s and unselected %s have the same face %v", selected, unselected, got)
		}
	}

	assertSelection("Git repository", "Editor")
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	assertSelection("Editor", "Git repository")
	if err := tt.Click("Git repository"); err != nil {
		t.Fatal(err)
	}
	assertSelection("Git repository", "Editor")
}

func TestEditorRecentFolderReopensWithoutGitInitialization(t *testing.T) {
	root := t.TempDir()
	opener := &recordedOpener{notRepository: true}
	a := newApp(opener)
	a.state = stateEmpty
	a.pickerOpenMode = int(openAllowPlainEditor)
	a.recentRepositories = []recentRepository{{Path: root, LastOpened: timeNow(), EditorOnly: true}}
	updated := newUpdates()
	a.update = updated.update
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	if err := tt.Click(root); err != nil {
		t.Fatal(err)
	}
	waitForEditorOnlyOpen(t, a, tt, updated)
	if a.snapshot.Root != cleanRepoPath(root) || len(opener.inited) != 0 {
		t.Fatalf("Editor recent reopened with the wrong policy: root=%q init=%v", a.snapshot.Root, opener.inited)
	}
}

func TestPickerRecentRepositoriesAndFoldersAreIndependent(t *testing.T) {
	a, tt, _ := recentTester(t,
		recentRepository{Path: "/work/shared", LastOpened: timeNow()},
		recentRepository{Path: "/work/shared", LastOpened: timeNow(), EditorOnly: true},
		recentRepository{Path: "/work/notes", LastOpened: timeNow(), EditorOnly: true},
	)
	mode, modeOK := tt.Find("Open mode")
	hero, heroOK := tt.Find("Open a repository")
	if !modeOK || !heroOK || mode.Y >= hero.Y {
		t.Fatalf("Home mode is not above the launcher: mode=%+v hero=%+v", mode, hero)
	}
	if !tt.HasText("Recent repositories") || tt.HasText("Recent folders") || tt.HasText("/work/notes") {
		t.Fatalf("Git tab mixed plain folders into its recents: %q", tt.Texts())
	}
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Recent folders") || tt.HasText("Recent repositories") || !tt.HasText("/work/notes") {
		t.Fatalf("Editor tab did not show its own recents: %q", tt.Texts())
	}
	if tt.HasText("Open worktree /work/shared") || tt.HasText("Open worktree /work/notes") {
		t.Fatalf("plain-folder cards expose Git worktree actions: %q", tt.Texts())
	}
	if err := tt.Click("More actions /work/notes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("Remove from list"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if len(a.recentRepositories) != 2 {
		t.Fatalf("Editor removal did not preserve the other cards: %+v", a.recentRepositories)
	}
	if err := tt.Click("More actions /work/shared"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("Remove from list"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if len(a.recentRepositories) != 1 || a.recentRepositories[0].EditorOnly || a.recentRepositories[0].Path != "/work/shared" {
		t.Fatalf("Editor removal changed the same path's Git card: %+v", a.recentRepositories)
	}
	if err := tt.Click("Git repository"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("/work/shared") || tt.HasText("Recent folders") {
		t.Fatalf("Git recents did not survive Editor cleanup: %q", tt.Texts())
	}
}

func TestRecentHistoryPersistenceKeepsSamePathInBothHomeModes(t *testing.T) {
	dir := t.TempDir()
	stored := []recentRepository{
		{Path: "/work/shared", LastOpened: 20},
		{Path: "/work/shared", LastOpened: 10, EditorOnly: true},
	}
	if err := saveRecentRepositories(dir, stored); err != nil {
		t.Fatal(err)
	}
	loaded := loadRecentRepositories(dir)
	if len(loaded) != 2 || loaded[0].EditorOnly || !loaded[1].EditorOnly {
		t.Fatalf("history lost its per-mode identity: %+v", loaded)
	}
	merged := mergeRecentHistory(stored[:1], loaded)
	if len(merged) != 2 {
		t.Fatalf("loading discarded one mode for the same path: %+v", merged)
	}
}

func TestHomeEditorModeStillOpensGitRepositoryWorkspace(t *testing.T) {
	root := t.TempDir()
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{Root: root, Branch: "main"}}})
	updated := newUpdates()
	a.update = updated.update
	a.path = root
	a.beginOpenWithPolicy(openAllowPlainEditor)
	for range 8 {
		if a.state == stateReady && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
	}
	if a.state != stateReady || a.session == nil || a.editorOnlyWorkspace() {
		t.Fatalf("Editor request discarded a real Git session: state=%v session=%T", a.state, a.session)
	}
	if got := a.visibleSections(); len(got) != len(workspaceSectionIDs) {
		t.Fatalf("Git workspace lost its sections: %v", got)
	}
}

func TestPlainEditorFallbackDoesNotHideOtherOpenErrors(t *testing.T) {
	a := newApp(fakeOpener{result: openResult{err: errors.New("permission denied")}})
	updated := newUpdates()
	a.update = updated.update
	a.path = t.TempDir()
	a.beginOpenWithPolicy(openAllowPlainEditor)
	for range 8 {
		if a.state == stateError && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
	}
	if a.state != stateError || a.session != nil || !strings.Contains(a.errMessage, "permission denied") {
		t.Fatalf("non-Git error fell back incorrectly: state=%v session=%T err=%q", a.state, a.session, a.errMessage)
	}
}

func TestShellCLIPlainFolderPoliciesAndDeduplication(t *testing.T) {
	opener := &recordedOpener{notRepository: true}
	s := newShell(opener)
	s.invalidate = func() {}
	updated := newUpdates()
	s.update = updated.update
	t.Cleanup(s.shutdown)
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	if err := os.Mkdir(first, 0o755); err != nil {
		t.Fatal(err)
	}
	s.start(first)
	tt := ui.NewTester(s.view, 1280, 800)
	waitForEditorOnlyOpen(t, s.activeApp(), tt, updated)
	if len(s.tabs) != 1 {
		t.Fatalf("initial CLI open made %d tabs, want 1", len(s.tabs))
	}

	second := filepath.Join(parent, "second folder")
	if err := os.Mkdir(second, 0o755); err != nil {
		t.Fatal(err)
	}
	s.openSecondInstance([]string{"./second folder"}, parent)
	waitForEditorOnlyOpen(t, s.activeApp(), tt, updated)
	if len(s.tabs) != 2 || s.activeApp().snapshot.Root != cleanRepoPath(second) {
		t.Fatalf("relative second-instance open failed: tabs=%d root=%q", len(s.tabs), s.activeApp().snapshot.Root)
	}
	s.activate(0)
	s.openSecondInstance([]string{filepath.Join(parent, ".", "second folder")}, t.TempDir())
	if len(s.tabs) != 2 || s.active != 1 {
		t.Fatalf("an open plain folder was duplicated: tabs=%d active=%d", len(s.tabs), s.active)
	}

	third := filepath.Join(parent, "third")
	if err := os.Mkdir(third, 0o755); err != nil {
		t.Fatal(err)
	}
	s.openTab(third)
	thirdApp := s.activeApp()
	for range 8 {
		if thirdApp.state == stateEmpty && thirdApp.offerInitPath != "" && len(updated.done) == 0 {
			break
		}
		updated.waitForUpdate(t)
		tt.Frame()
	}
	if thirdApp.state != stateEmpty || thirdApp.offerInitPath != third || thirdApp.editorOnlyWorkspace() {
		t.Fatalf("ordinary internal tab accepted a plain folder: state=%v offer=%q", thirdApp.state, thirdApp.offerInitPath)
	}
}

func TestPlainFolderEditorTreeSearchSaveAndChrome(t *testing.T) {
	root := t.TempDir()
	writeCodeTestFile(t, root, "note.txt", "first plain-folder-needle\n")
	writeCodeTestFile(t, root, "nested/other.txt", "other\n")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	a := newApp(repository.Opener{})
	h := &harness{app: a, updates: newUpdates()}
	a.update = h.updates.update
	a.path = root
	t.Cleanup(a.shutdown)
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.beginOpenWithPolicy(openAllowPlainEditor)
	waitForEditorOnlyOpen(t, a, h.tester, h.updates)
	openCodePageForTest(t, h, 2)

	texts := strings.Join(h.texts(), " ")
	for _, want := range []string{filepath.Base(root), "note.txt", "empty", "Editor", "Keyboard shortcuts"} {
		if !strings.Contains(texts, want) {
			t.Errorf("plain Editor is missing %q: %q", want, h.texts())
		}
	}
	for _, gone := range []string{"Changes", "History", "Clean", "Fetch", "Pull requests", "Command log"} {
		if h.tester.HasText(gone) {
			t.Errorf("plain Editor exposes Git control %q: %q", gone, h.texts())
		}
	}
	h.tester.Key(0, ui.Key2)
	h.tester.Key(0, ui.KeyF5)
	h.tester.Key(ui.Ctrl, ui.KeyL)
	h.tester.Frame()
	if a.workspaceTab != sectionCode || a.commandLogOpen || a.actionBusy {
		t.Fatalf("Git shortcuts remained active: tab=%d log=%v busy=%v", a.workspaceTab, a.commandLogOpen, a.actionBusy)
	}

	h.rightClick(t, "note.txt")
	if h.tester.HasText("View File History…") {
		t.Fatalf("plain file menu offers Git history: %q", h.texts())
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()

	h.tester.Key(ui.Cmd|ui.Shift, ui.KeyF)
	if !h.tester.Focused("Search in files") {
		t.Fatalf("workspace search did not receive focus: %q", h.texts())
	}
	h.tester.Type("plain-folder-needle")
	h.tester.Key(0, ui.KeyEnter)
	h.waitFor(t, "plain-folder search results", func() bool {
		return h.updates.flushPending() == false && !a.code.searchRunning && len(a.code.searchResults) == 1
	})
	if got := a.code.searchResults[0].Path; got != "note.txt" {
		t.Fatalf("plain-folder search result = %q, want note.txt", got)
	}

	h.app.openCodeFile("note.txt", 0)
	doc := waitForCodeDocument(t, h, "note.txt")
	doc.state.Insert("edited\n")
	h.tester.Frame()
	h.tester.Key(ui.Cmd, ui.KeyS)
	h.tester.Frame()
	if doc.state.Buffer.Modified() || doc.saveError != "" {
		t.Fatalf("plain-folder save failed: modified=%v err=%q", doc.state.Buffer.Modified(), doc.saveError)
	}
	assertCodeFileContent(t, root, "note.txt", "edited\nfirst plain-folder-needle\n")
	if _, err := os.Stat(filepath.Join(root, ".git")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("editing a plain folder initialized Git: %v", err)
	}
}

func TestPlainFolderTerminalUsesWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	a := newApp(fakeOpener{})
	a.state = stateReady
	a.snapshot.Root = root
	a.location.Root = root
	a.path = root
	updated := newUpdates()
	a.update = updated.update
	t.Cleanup(a.shutdown)
	starts, calls := captureAgentTerminalOptions(t)

	a.openTerminal()
	options := receiveAgentTerminalOptions(t, starts)
	if options.Dir != root {
		t.Fatalf("terminal directory = %q, want %q", options.Dir, root)
	}
	for range 8 {
		if calls.Load() == 1 && len(a.term.sessions) == 1 && a.term.sessions[0].pane != nil {
			break
		}
		updated.waitForUpdate(t)
	}
	if calls.Load() != 1 || len(a.term.sessions) != 1 || a.term.sessions[0].pane == nil {
		t.Fatalf("plain-folder terminal did not start once: calls=%d sessions=%+v", calls.Load(), a.term.sessions)
	}
}

func TestEditorOnlyHomeStringsAreLocalized(t *testing.T) {
	keys := []string{
		"Open a folder",
		"Browse, search, edit files, and use a terminal — no Git repository required.",
		"Open mode",
		"Git repository",
		"Folder",
		"Recent folders",
		"Search recent folders",
		"No folders match your search",
		"Open this folder in your editor",
		"Open a folder to begin",
		"Browse, search, and edit files or use a terminal without initializing Git.",
		"Workspace folder",
		"This folder has no files",
		"Terminal: open a Git repository or folder path directly in lazygitGo",
	}
	for _, language := range languageChoices[1:] {
		for _, key := range keys {
			if translated := i18n.Translations[language.Code][key]; translated == "" || translated == key {
				t.Errorf("%s has no translation for %q", language.Code, key)
			}
		}
	}
}
