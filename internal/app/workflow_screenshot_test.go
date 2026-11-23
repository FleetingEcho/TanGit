package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// TestWorkflowScreenshots renders the two review-before-change workflows. It
// is opt-in like TestScreenshot and keeps native dialog chrome out of the
// fixtures while exercising the complete MyGo view composition around it.
func TestWorkflowScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("clone-form-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.settings.Theme = "System"
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		a.openCloneForm()
		a.cloneURL = "https://example.com/team/project.git"
		a.cloneBranch = "main"
		a.cloneParent = "/work"
		a.updateCloneDestination()
		saveScreenshot(t, dir, "clone-form-light", tt)
	})

	t.Run("clone-form-validation-light", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.settings.Theme = "System"
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(false)
		a.openCloneForm()
		a.cloneURL = "https://user:secret@example.com/team/project.git"
		a.cloneBranch = "bad\nbranch"
		a.beginClone()
		saveScreenshot(t, dir, "clone-form-validation-light", tt)
	})

	t.Run("clone-progress-dark", func(t *testing.T) {
		a := newApp(screenshotOpener{snapshot: repository.Snapshot{}})
		a.settings.Theme = "System"
		tt := ui.NewTester(a.view, 1280, 800)
		tt.SetDark(true)
		a.state = stateCloning
		a.cloneURL = "git@example.com:team/project.git"
		a.cloneDestination = "/work/project"
		a.cloneProgress = repository.CloneProgress{Phase: "Receiving objects", Current: 640, Total: 1000, Percent: 64, Fraction: 0.64, Known: true}
		saveScreenshot(t, dir, "clone-progress-dark", tt)
	})

	for _, preview := range []bool{false, true} {
		name := "custom-patch-builder-dark"
		if preview {
			name = "custom-patch-preview-dark"
		}
		t.Run(name, func(t *testing.T) {
			a, tt, updated := newScreenshotHarness(t, true)
			a.workspaceTab = sectionHistory
			a.selectedCommit = 0
			tt.Frame()
			settleShot(t, tt, updated, func() bool { return a.commitDiffState != nil })
			commit := a.snapshot.Commits[0]
			a.customPatch.entries = []customPatchEntry{
				{commit: commit, file: repository.CommitFile{Path: "internal/repository/session.go", Status: "M"}, sourcePatch: screenshotPatch, whole: true, order: 0},
				{commit: commit, file: repository.CommitFile{Path: "docs/notes.md", Status: "M"}, sourcePatch: screenshotPatch, order: 1},
			}
			a.customPatch.nextOrder = 2
			a.customPatch.open = true
			if preview {
				a.customPatch.previewOpen = true
				a.customPatch.preview = &repository.PatchPreview{
					Text: screenshotPatch, Clean: true, Additions: 2, Deletions: 1,
					Files: []repository.PatchFile{{Path: "internal/repository/session.go", Additions: 2, Deletions: 1}},
				}
			}
			saveScreenshot(t, dir, name, tt)
		})
	}
}

// TestCommitDetailAndToolbarScreenshots keeps the reworked commit details and
// the whole-window diff in the shot set: both themes, both divider layouts,
// and the maximized view with its own repository toolbar.
func TestCommitDetailAndToolbarScreenshots(t *testing.T) {
	dir := os.Getenv("MYGO_SHOTS")
	if dir == "" {
		t.Skip("set MYGO_SHOTS to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run("commit-details-left-right-"+mode, func(t *testing.T) {
			a, tt, updated := newScreenshotHarness(t, dark)
			a.workspaceTab = sectionHistory
			settleShot(t, tt, updated, func() bool { return a.commitDiffState != nil && !a.commitDiffLoading })
			saveScreenshot(t, dir, "commit-details-left-right-"+mode, tt)
		})
		t.Run("commit-details-top-bottom-"+mode, func(t *testing.T) {
			a, tt, updated := newScreenshotHarness(t, dark)
			a.workspaceTab = sectionHistory
			a.commitDetailsLayout = commitDetailsTopBottom
			settleShot(t, tt, updated, func() bool { return a.commitDiffState != nil && !a.commitDiffLoading })
			saveScreenshot(t, dir, "commit-details-top-bottom-"+mode, tt)
		})
		t.Run("maximized-diff-"+mode, func(t *testing.T) {
			a, tt, updated := newScreenshotHarness(t, dark)
			a.workspaceTab = sectionHistory
			settleShot(t, tt, updated, func() bool { return a.commitDiffState != nil && !a.commitDiffLoading })
			if err := tt.Click("Expand diff"); err != nil {
				t.Fatalf("expand the diff: %v", err)
			}
			if !a.diffFullscreen {
				t.Fatal("the diff did not open over the whole window")
			}
			tt.Frame()
			saveScreenshot(t, dir, "maximized-diff-"+mode, tt)
		})
	}
}
