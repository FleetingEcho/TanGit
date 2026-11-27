package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// harness drives an app in a tester window, running the app's updates on the
// test goroutine as MyGo's Window.Update runs them on the UI loop.
type harness struct {
	app     *app
	tester  *ui.Tester
	updates *updates
}

// menuTestApp opens a repository with files and branches.
func menuTestApp(t *testing.T) *harness {
	t.Helper()
	h, _ := menuTestAppWithCalls(t)
	return h
}

// menuTestAppWithCalls opens the menu fixture and returns what its session was
// asked to do.
func menuTestAppWithCalls(t *testing.T) (*harness, *callRecorder) {
	t.Helper()
	calls := &callRecorder{}
	a := newApp(fakeOpener{calls: calls, result: openResult{snapshot: repository.Snapshot{
		Root:     "/work/project",
		Branch:   "main",
		HeadHash: "c2c2c2c2",
		Files: []repository.File{
			{Path: "one.txt", Status: "M ", Staged: true, Tracked: true},
			{Path: "two.txt", Status: "??", Unstaged: true, Added: true},
		},
		Branches: []repository.Branch{
			{Name: "main", DisplayName: "main (local)", Head: true, Subject: "Native history", UpstreamRemote: "origin", UpstreamBranch: "main", Ahead: "?", Behind: "?"},
			{Name: "feature", DisplayName: "feature", Subject: "Desktop work"},
		},
		Commits: []repository.Commit{
			{Hash: "c2c2c2c2", ShortHash: "c2c2c2c", Subject: "Second commit", AuthorName: "Ada", AuthorEmail: "ada@example.com", Parents: []string{"c1c1c1c1"}},
			{Hash: "c1c1c1c1", ShortHash: "c1c1c1c", Subject: "Initial commit", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		},
		Reflog: []repository.Commit{
			{Hash: "c2c2c2c2", ShortHash: "c2c2c2c", Subject: "checkout: moving from feature to main", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
		},
		Remotes: []repository.Remote{
			{Name: "origin", URLs: []string{"git@example.com:team/project.git"}, Branches: []repository.RemoteBranch{{Name: "main", RemoteName: "origin"}, {Name: "feature/menus", RemoteName: "origin"}}},
		},
		Tags: []repository.Tag{
			{Name: "v2.0.0", Message: "Current release"},
			{Name: "build/interim", Message: "Interim build"},
			{Name: "v1.0.0", Message: "Previous release"},
		},
	}}})
	// Established interaction fixtures retain the pre-Overview landing and
	// numeric section positions. Fresh-profile migration has dedicated tests.
	a.settings.OpenRepositoriesOnOverview = false
	a.settings.SectionOrder = slices.Clone(workspaceSectionIDs)
	// Most navigation fixtures exercise Changes; app startup itself defaults to Code.
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, calls
}

// settle runs the updates queued so far, building a frame each time, until
// none is left. It also waits for the repository to open, which completes on a
// goroutine a plain drain can miss.
func (h *harness) settle(t *testing.T) {
	t.Helper()
	for range 8 {
		if h.app.state == stateReady && len(h.updates.done) == 0 {
			break
		}
		h.updates.waitForUpdate(t)
	}
	h.tester.Frame()
}

// press presses a key and settles the frame it changed.
func (h *harness) press(t *testing.T, key ui.Key) {
	t.Helper()
	// Shortcut routing belongs to the committed view. In particular, a
	// fixture's new custom binding must be registered before F5 is injected.
	h.tester.Frame()
	h.tester.Key(0, key)
	h.tester.Frame()
	h.settle(t)
}

// rightClick opens the context menu of the element showing label, and settles
// the frame that drew it.
func (h *harness) rightClick(t *testing.T, label string) {
	t.Helper()
	r, found := h.tester.Find(label)
	if !found {
		t.Fatalf("no row shows %q", label)
	}
	// MyGo v0.8 deliberately rejects named clicks that resolve to a sibling
	// input receiver. List captions name a visual label inside the row, while
	// the row owns the menu, so exercise that real row coordinate explicitly.
	h.tester.RightClickAt(r.X+r.W/2, r.Y+r.H/2)
	h.tester.Frame()
	h.settle(t)
}

// texts returns the texts of the last frame.
func (h *harness) texts() []string { return h.tester.Texts() }

// waitFor runs updates and builds frames until cond holds, so a read the app
// started on a goroutine has landed.
func (h *harness) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 8 {
		if cond() {
			return
		}
		h.updates.waitForUpdate(t)
		h.tester.Frame()
	}
	if !cond() {
		state := "no app"
		if h.app != nil {
			focused := []string{}
			if h.tester != nil {
				for _, info := range h.tester.AccessAll() {
					if info.Focused {
						focused = append(focused, info.Name)
					}
				}
			}
			state = fmt.Sprintf("page=%d focus=%q native_focus=%v action_busy=%v command_running=%v forms=%v terminal=%v editor_only=%v shortcuts_disabled=%v",
				h.app.workspaceTab, h.app.keyboardFocus, focused, h.app.actionBusy,
				h.app.customCommandRunning, h.app.anyFormOpen(), h.app.terminalVisible(),
				h.app.editorOnlyWorkspace(), h.app.settings.DisableShortcuts)
		}
		t.Fatalf("timed out waiting for %s (%s)", what, state)
	}
}

// requireMenuLabels checks that the drawn context menu shows every label of
// want. PopoverMenu draws the menu in the window instead of asking the system
// for one, so its labels show as texts.
func (h *harness) requireMenuLabels(t *testing.T, want ...string) {
	t.Helper()
	texts := h.texts()
	for _, label := range want {
		// An action the commit menu groups in a submenu is not drawn until the
		// group opens; its group being offered is what this can see.
		if group, grouped := commitMenuGroups[label]; grouped && !slices.Contains(texts, label) {
			label = group
		}
		if !slices.Contains(texts, label) {
			t.Errorf("menu missing %q, texts %q", label, texts)
		}
	}
}

// commitMenuGroups names the submenu each commit action sits in, so tests
// reach it as a person does: through the group.
var commitMenuGroups = func() map[string]string {
	groups := map[string][]string{
		"Custom Patch": {"Add all files to Custom Patch", "Review Custom Patch"},
		"Copy": {"Copy commit hash", "Copy short hash", "Copy subject", "Copy commit message",
			"Copy author", "Copy web URL", "Copy patch"},
		"Rewrite history": {"Mark as base commit for rebase", "Cancel marked base commit", "Open Rebase TODO",
			"Edit commit (pause rebase)…", "Reword commit…", "Drop commit…", "Amend with staged changes…",
			"Create fixup commit from staged changes…", "Apply fixups from this commit through HEAD…",
			"Squash into older commit…", "Fixup into older commit…", "Move commit toward HEAD",
			"Move commit toward older history"},
		"Reset": {"Undo latest commit and keep changes staged…", "Undo latest commit and keep changes unstaged…",
			"Soft reset to this commit…", "Mixed reset to this commit…", "Hard reset to this commit…"},
		"Create": {"Create a tag here…", "Create a branch here…", "Create worktree here…", "Export as mailbox patch…"},
		"Bisect": {"Mark commit good (bisect)", "Mark commit bad (bisect)", "Skip commit (bisect)",
			"Start bisect from this commit…"},
		"Compare": {"Compare commit with HEAD", "Compare first parent with commit"},
	}
	out := map[string]string{}
	for group, labels := range groups {
		for _, label := range labels {
			out[label] = group
		}
	}
	return out
}()

// menuOffers reports whether the open menu has label, in its submenu when the
// commit menu groups it. It chooses the item to find out, so call it where an
// absent item is the expected answer; the texts alone cannot say, because a
// submenu's items are not drawn until it opens.
func (h *harness) menuOffers(label string) bool {
	return chooseMenuPath(h.tester, label) == nil
}

// chooseMenuPath chooses label from the open menu, through its submenu when the
// commit menu groups it in one.
func chooseMenuPath(t *ui.Tester, label string) error {
	if err := t.ChooseMenuItem(label); err == nil {
		return nil
	} else if group, grouped := commitMenuGroups[label]; grouped {
		if groupErr := t.ChooseMenuItem(group, label); groupErr == nil {
			return nil
		} else {
			return groupErr
		}
	} else {
		return err
	}
}

// chooseMenuItem chooses the item from the open menu itself, rather than a
// matching button elsewhere in the frame (commit rows also have icon bars).
func (h *harness) chooseMenuItem(t *testing.T, label string) {
	t.Helper()
	if err := chooseMenuPath(h.tester, label); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.settle(t)
}

func TestFileRowContextMenu(t *testing.T) {
	h := menuTestApp(t)
	h.app.selectedFile = 0

	h.rightClick(t, "one.txt")
	h.requireMenuLabels(t, "Open in built-in editor", "Unstage", "Blame", "Find file histories", "Discard changes…", "Copy relative path", "Copy absolute path")
	if slices.Contains(h.texts(), "Stage") {
		t.Errorf("a staged file offers Stage in its menu: %q", h.texts())
	}
	h.press(t, ui.KeyEscape)

	h.rightClick(t, "two.txt")
	h.requireMenuLabels(t, "Open in built-in editor", "Stage", "Find file histories", "Discard changes…", "Copy relative path", "Copy absolute path")

	// The menu acts on the row clicked, not on the row selected: the details
	// pane follows it, and the discard confirmation it opens names it.
	h.chooseMenuItem(t, "Discard changes…")
	change, ok := h.app.selectedChange()
	if !ok || change.file.Path != "two.txt" {
		t.Fatalf("the menu should select the row clicked, selectedFile=%d", h.app.selectedFile)
	}
	if h.app.confirm == nil {
		t.Fatal("Discard started no confirmation")
	}
}

// Every file menu offers both ways to copy its path, and each item copies the
// path it names: the relative one Git shows, and the absolute one a terminal
// or editor can open. The commit file list offers the same pair.
func TestFileMenusOfferBothPaths(t *testing.T) {
	h := menuTestApp(t)

	h.rightClick(t, "two.txt")
	h.requireMenuLabels(t, "Copy relative path", "Copy absolute path")
	h.chooseMenuItem(t, "Copy relative path")
	if got := h.tester.Clipboard(); got != "two.txt" {
		t.Fatalf("Copy relative path copied %q", got)
	}

	h.rightClick(t, "one.txt")
	h.chooseMenuItem(t, "Copy absolute path")
	if got := h.tester.Clipboard(); got != "/work/project/one.txt" {
		t.Fatalf("Copy absolute path copied %q", got)
	}

	// The commit file list offers the same pair.
	h.press(t, sectionKeys[sectionHistory])
	h.waitFor(t, "the commit files", func() bool { return len(h.app.commitFiles) > 0 })
	h.rightClick(t, "one.txt")
	h.requireMenuLabels(t, "Open in built-in editor", "View file changes…", "Copy relative path", "Copy absolute path")
	h.chooseMenuItem(t, "Copy relative path")
	if got := h.tester.Clipboard(); got != "one.txt" {
		t.Fatalf("the commit file menu copied %q", got)
	}
}

func TestBranchRowContextMenu(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, sectionKeys[sectionBranches])

	h.rightClick(t, "feature")
	h.requireMenuLabels(t, "Checkout", "Force checkout…", "Merge into current…", "Rebase current onto this…", "Rename…", "New branch here…", "Copy branch name", "Delete branch…")
	h.press(t, ui.KeyEscape)

	// The checked-out branch has no Checkout, Merge or Delete.
	h.rightClick(t, "main (local)")
	h.requireMenuLabels(t, "Rename…", "Copy branch name")
	for _, label := range []string{"Checkout", "Merge into current…", "Delete branch…"} {
		if slices.Contains(h.texts(), label) {
			t.Errorf("the current branch offers %q: %q", label, h.texts())
		}
	}
	h.press(t, ui.KeyEscape)

	// The menu acts on the row clicked: the new branch starts there, not at
	// the branch selected.
	h.app.selectedBranch = 0
	h.rightClick(t, "feature")
	h.chooseMenuItem(t, "New branch here…")
	if !h.app.showBranchForm || h.app.newBranchBase != "feature" {
		t.Fatalf("New branch here… opened %v from %q", h.app.showBranchForm, h.app.newBranchBase)
	}
}

func TestSectionShortcuts(t *testing.T) {
	h := menuTestApp(t)
	if len(sectionKeys) != 10 {
		t.Fatalf("got %d digit shortcuts, want ten", len(sectionKeys))
	}
	if len(workspaceSections) <= len(sectionKeys) {
		t.Fatal("this test must cover sections beyond the available digits")
	}
	// This test walks every section with a digit, including specialist tabs
	// the default set hides. Additional sections remain available in the
	// sidebar rather than reassigning any existing shortcut.
	h.app.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
	h.app.applySettingsChange()
	h.tester.Frame()
	for tab, key := range sectionKeys {
		h.press(t, key)
		if h.app.workspaceTab != tab {
			t.Fatalf("key %d moved to section %d, want %d", key, h.app.workspaceTab, tab)
		}
	}
}

func TestGraphTabDrawsTheGraph(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, sectionKeys[sectionGraph])
	if h.app.workspaceTab != sectionGraph {
		t.Fatalf("the graph shortcut landed on section %d", h.app.workspaceTab)
	}
	if !slices.Contains(h.texts(), "Commit graph") {
		t.Fatalf("the graph section shows no graph: %q", h.texts())
	}
	if !slices.Contains(h.texts(), "Second commit") {
		t.Fatalf("the graph lists no commits: %q", h.texts())
	}
	dot := false
	for _, text := range h.texts() {
		if strings.Contains(text, "●") {
			dot = true
		}
	}
	if !dot {
		t.Fatalf("the graph drew no commit dots: %q", h.texts())
	}
}

func TestUnknownAheadBehindShowsNoQuestionMarks(t *testing.T) {
	h := menuTestApp(t)
	for _, text := range h.texts() {
		if strings.Contains(text, "? ahead") || strings.Contains(text, "? behind") {
			t.Fatalf("the header shows a placeholder count: %q", text)
		}
	}

	h.press(t, sectionKeys[sectionBranches])
	h.waitFor(t, "the branch commits", func() bool {
		return len(h.app.branchCommits) > 0
	})
	for _, text := range h.texts() {
		if strings.Contains(text, "Ahead ?") || strings.Contains(text, "Behind ?") {
			t.Fatalf("the branch pane shows placeholder counts: %q", text)
		}
	}
	if !slices.Contains(h.texts(), "Git cannot tell ahead or behind until origin/main is fetched.") {
		t.Fatalf("the branch pane does not explain the missing counts: %q", h.texts())
	}
}

func TestBranchCommitsBrowseWithoutCheckout(t *testing.T) {
	h := menuTestApp(t)
	h.app.selectedBranch = 1 // feature
	h.press(t, sectionKeys[sectionBranches])
	h.waitFor(t, "the branch commits", func() bool {
		return h.app.branchCommitsBranch == "feature" && len(h.app.branchCommits) > 0
	})
	if !slices.Contains(h.texts(), "Second commit") {
		t.Fatalf("the branch pane lists no commits: %q", h.texts())
	}
	h.waitFor(t, "the commit files", func() bool {
		return len(h.app.branchCommitFiles) > 0
	})
	h.waitFor(t, "the commit diff", func() bool {
		return h.app.branchCommitDiffState != nil || h.app.branchCommitDiffError != ""
	})
	if !slices.Contains(h.texts(), "one.txt") {
		t.Fatalf("the commit pane lists no files: %q", h.texts())
	}
	if h.app.branchCommitDiffState == nil {
		t.Fatalf("the commit file loaded no diff: %q", h.app.branchCommitDiffError)
	}
}

func TestCommitWithDescriptionRequiresStagedFiles(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	if err := h.tester.Click("More actions"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	item, ok := h.tester.Access("Commit with a description…")
	if !ok || !item.Disabled {
		t.Fatalf("description commit is enabled without staged files: %+v, found=%v", item, ok)
	}
	h.press(t, ui.KeyEscape)

	h.app.snapshot.Files = []repository.File{{Path: "changed.txt", Status: "M ", Staged: true, Tracked: true}}
	h.tester.Frame()
	if err := h.tester.Click("More actions"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	item, ok = h.tester.Access("Commit with a description…")
	if !ok || item.Disabled {
		t.Fatalf("description commit is disabled with staged files: %+v, found=%v", item, ok)
	}
	h.chooseMenuItem(t, "Commit with a description…")
	if !h.app.showCommitForm {
		t.Fatal("the enabled description action did not open its form")
	}
}

func TestShortcutsAreSilentWhileAFormIsOpen(t *testing.T) {
	h := menuTestApp(t)
	if err := h.tester.Click("⋯"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.chooseMenuItem(t, "Commit with a description…")
	if !h.app.showCommitForm {
		t.Fatal("the commit form did not open")
	}

	// A section switch would close the form under the user.
	h.press(t, ui.Key2)
	if h.app.workspaceTab != sectionChanges || !h.app.showCommitForm {
		t.Fatalf("section switch leaked into the form: tab %d, form %v", h.app.workspaceTab, h.app.showCommitForm)
	}

	// Letters typed into the form reach it rather than the section's shortcuts.
	h.tester.Click("Summary")
	h.tester.Type("hello")
	h.tester.Frame()
	if h.app.commitSummary != "hello" {
		t.Fatalf("the form took %q, want %q", h.app.commitSummary, "hello")
	}

	h.press(t, ui.KeyEscape)
	if h.app.showCommitForm {
		t.Fatal("Escape did not close the commit form")
	}
}

func TestFileShortcutsActOnTheSelection(t *testing.T) {
	h := menuTestApp(t)
	h.app.selectedFile = 1
	h.press(t, ui.KeySpace)
	if h.app.selectedFile != 1 {
		t.Fatalf("Space moved the selection to %d", h.app.selectedFile)
	}
	if h.app.actionError != "" {
		t.Fatalf("Space failed: %s", h.app.actionError)
	}
}

func TestRefreshShortcut(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, ui.KeyF5)
	if h.app.actionError != "" {
		t.Fatalf("F5 failed: %s", h.app.actionError)
	}
	if !slices.Contains(h.texts(), "project") {
		t.Fatalf("the refresh lost the repository: %q", h.texts())
	}
}
