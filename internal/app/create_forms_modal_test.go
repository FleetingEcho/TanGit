package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Repository-creation forms own the window while open: background navigation
// cannot switch pages, and Escape dismisses the dialog without running it.
func TestRepositoryCreationFormsAreModal(t *testing.T) {
	cases := []struct {
		name    string
		section int
		open    string
		shown   func(*app) bool
	}{
		{"remote", sectionRemotes, "Add remote…", func(a *app) bool { return a.showRemoteForm }},
		{"tag", sectionTags, "Create tag…", func(a *app) bool { return a.showTagForm }},
		{"worktree", sectionWorktrees, "New worktree…", func(a *app) bool { return a.showWorktreeForm }},
		{"submodule", sectionSubmodules, "Add submodule…", func(a *app) bool { return a.showSubmoduleForm }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := menuTestApp(t)
			h.app.settings.VisibleSections = append([]string(nil), workspaceSectionIDs...)
			h.app.workspaceTab = tc.section
			h.tester.Frame()
			if err := h.tester.Click(tc.open); err != nil {
				t.Fatalf("open form: %v", err)
			}
			h.tester.Frame()
			if !tc.shown(h.app) {
				t.Fatal("the plus button opened no form")
			}

			changes, found := h.tester.Find("Changes")
			if !found {
				t.Fatal("the background Changes tab is not shown")
			}
			// This click is intentionally intercepted by the modal; MyGo's named
			// click validation rejects a covered target before delivering it.
			h.tester.ClickAt(changes.X+changes.W/2, changes.Y+changes.H/2)
			h.tester.Frame()
			if h.app.workspaceTab != tc.section {
				t.Fatalf("background navigation changed section to %d", h.app.workspaceTab)
			}

			h.tester.Key(0, ui.KeyEscape)
			h.tester.Frame()
			if tc.shown(h.app) {
				t.Fatal("Escape left the form open")
			}
		})
	}
}
