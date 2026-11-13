package app

import (
	"fmt"
	"sort"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func requireRect(t *testing.T, tester *ui.Tester, label string) ui.Rect {
	t.Helper()
	r, ok := tester.Find(label)
	if !ok {
		t.Fatalf("missing %q: %q", label, tester.Texts())
	}
	return r
}

func requireRectInside(t *testing.T, label string, child, parent ui.Rect) {
	t.Helper()
	const tolerance = float32(1.01)
	if child.W < tolerance || child.H < tolerance ||
		child.X < parent.X-tolerance || child.Y < parent.Y-tolerance ||
		child.X+child.W > parent.X+parent.W+tolerance ||
		child.Y+child.H > parent.Y+parent.H+tolerance {
		t.Errorf("%s is clipped by its parent: child=%+v parent=%+v", label, child, parent)
	}
}

func responsiveConflictHarness(t *testing.T, width, height int, style string, dark bool) *harness {
	t.Helper()
	const content = "one\n<<<<<<< HEAD\nours\n||||||| base\ncommon\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	h, _ := changesTestAppIn(t, []repository.File{{
		Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true,
	}}, width, height)
	h.app.settings.ConflictActionStyle = style
	h.tester.SetDark(dark)
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	return h
}

func assertConflictParentGeometry(t *testing.T, tester *ui.Tester) {
	t.Helper()
	details := requireRect(t, tester, "File details")
	toolbar := requireRect(t, tester, "Conflict toolbar")
	content := requireRect(t, tester, "Conflict content")
	requireRectInside(t, "Conflict toolbar", toolbar, details)
	requireRectInside(t, "Conflict content", content, details)
	if content.W < 120 || content.H < 96 {
		t.Errorf("conflict editor has no usable room: content=%+v details=%+v toolbar=%+v", content, details, toolbar)
	}
	if content.Y < toolbar.Y+toolbar.H-1 {
		t.Errorf("conflict toolbar overlaps its content: toolbar=%+v content=%+v", toolbar, content)
	}
	for _, group := range []string{"Hunk group", "View group", "Apply group"} {
		r := requireRect(t, tester, group)
		requireRectInside(t, group, r, toolbar)
	}
	mark := requireRect(t, tester, "Mark resolved")
	requireRectInside(t, "Mark resolved", mark, toolbar)
	if mark.W < 80 || mark.H < 24 {
		t.Errorf("Mark resolved was squeezed instead of reflowed: mark=%+v toolbar=%+v", mark, toolbar)
	}
	view := requireRect(t, tester, "View group")
	if _, direct := tester.Find("Base"); direct {
		for _, tab := range []string{"Base", "Current", "Incoming", "Result"} {
			r := requireRect(t, tester, tab)
			requireRectInside(t, tab, r, view)
			if r.W < 28 || r.H < 24 {
				t.Errorf("View tab %q was squeezed: %+v", tab, r)
			}
		}
		return
	}
	viewButton := requireRect(t, tester, "View")
	requireRectInside(t, "View action", viewButton, view)
	if err := tester.Click("View"); err != nil {
		t.Errorf("open compact View actions: %v", err)
		return
	}
	tester.Frame()
	for _, item := range []string{"Base", "Current", "Incoming", "Result", "Conflict Assistant"} {
		r := requireRect(t, tester, item)
		if r.W < 28 || r.H < 24 {
			t.Errorf("View menu item %q has no usable target: %+v", item, r)
		}
	}
	closePopoverForTest(t, tester)
	assertCompactApplyActions(t, tester)
}

func closePopoverForTest(t *testing.T, tester *ui.Tester) {
	t.Helper()
	tester.Key(0, ui.KeyEscape)
	tester.Frame()
}

func assertCompactApplyActions(t *testing.T, tester *ui.Tester) {
	t.Helper()
	apply := requireRect(t, tester, "Apply group")
	_, direct := tester.Find("Use current")
	if direct {
		for _, action := range []string{"Use current", "Use incoming"} {
			r := requireRect(t, tester, action)
			requireRectInside(t, action, r, apply)
			if r.W < 24 || r.H < 24 {
				t.Errorf("direct Apply action %q was squeezed: %+v", action, r)
			}
		}
	}
	if err := tester.Click("Apply"); err != nil {
		t.Errorf("open compact Apply overflow: %v", err)
		return
	}
	tester.Frame()
	items := []string{"Use base", "Use both", "Restore initial conflict"}
	if !direct {
		items = append(items, "Use current", "Use incoming")
	}
	for _, item := range items {
		if r := requireRect(t, tester, item); r.W < 28 || r.H < 24 {
			t.Errorf("Apply overflow item %q has no usable target: %+v", item, r)
		}
	}
	if !tester.HasText("Result already matches the initial conflict.") {
		t.Errorf("Apply overflow lost its disabled reason: %q", tester.Texts())
	}
	closePopoverForTest(t, tester)
}

func TestConflictControlsFitTheirActualPaneAtEveryResponsiveSize(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) {
		return []byte("one\n<<<<<<< HEAD\nours\n||||||| base\ncommon\n=======\ntheirs\n>>>>>>> topic\nlast\n"), nil
	}
	for _, style := range []string{conflictActionStyleIcons, conflictActionStyleIconsLabels} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dark_%t", style, dark), func(t *testing.T) {
				h := responsiveConflictHarness(t, 1280, 640, style, dark)
				for _, width := range []int{1280, 960, 720, 960, 1280, 720} {
					t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
						h.tester.SetSize(width, 640)
						h.tester.Frame()
						assertConflictParentGeometry(t, h.tester)
					})
				}
			})
		}
	}
}

func assertHeaderControlsFit(t *testing.T, tester *ui.Tester, labels ...string) {
	t.Helper()
	header := requireRect(t, tester, "Repository header")
	type labeledRect struct {
		label string
		rect  ui.Rect
	}
	controls := make([]labeledRect, 0, len(labels))
	for _, label := range labels {
		r := requireRect(t, tester, label)
		requireRectInside(t, label, r, header)
		if r.W < 24 || r.H < 24 {
			t.Errorf("header action %q has no usable target: %+v", label, r)
		}
		controls = append(controls, labeledRect{label, r})
	}
	sort.Slice(controls, func(i, j int) bool { return controls[i].rect.X < controls[j].rect.X })
	for i := 1; i < len(controls); i++ {
		previous, current := controls[i-1], controls[i]
		if current.rect.X < previous.rect.X+previous.rect.W-1 {
			t.Errorf("header actions %q and %q overlap: %+v %+v", previous.label, current.label, previous.rect, current.rect)
		}
	}
}

func TestActiveRebaseHeaderKeepsApplicationActionsInsideTheirParent(t *testing.T) {
	h, _ := operationTestApp(t, richRebaseOperation())
	for _, dark := range []bool{false, true} {
		h.tester.SetDark(dark)
		for _, width := range []int{1280, 960, 720, 960, 1280, 720} {
			t.Run(fmt.Sprintf("dark_%t/width_%d", dark, width), func(t *testing.T) {
				h.tester.SetSize(width, 640)
				h.tester.Frame()
				assertHeaderControlsFit(t, h.tester,
					"Continue", "Skip…", "Abort…", "Fetch", "Pull", "Push", "Refresh", "Open in editor", "Theme", "Settings")
				header := requireRect(t, h.tester, "Repository header")
				branch := requireRect(t, h.tester, "main")
				requireRectInside(t, "active-operation branch", branch, header)
				if width >= 960 {
					title := requireRect(t, h.tester, "project")
					requireRectInside(t, "wide repository title", title, header)
				}
			})
		}
	}
}

func TestOrdinaryBranchHeaderKeepsItsIdentityAndActions(t *testing.T) {
	h := menuTestApp(t)
	for _, dark := range []bool{false, true} {
		h.tester.SetDark(dark)
		for _, width := range []int{1280, 960, 720, 1280} {
			t.Run(fmt.Sprintf("dark_%t/width_%d", dark, width), func(t *testing.T) {
				h.tester.SetSize(width, 640)
				h.tester.Frame()
				assertHeaderControlsFit(t, h.tester, "Theme", "Settings")
				header := requireRect(t, h.tester, "Repository header")
				for _, identity := range []string{"project", "main"} {
					r := requireRect(t, h.tester, identity)
					requireRectInside(t, identity, r, header)
				}
			})
		}
	}
}
