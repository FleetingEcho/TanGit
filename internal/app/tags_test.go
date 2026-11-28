package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

type tagActionsSession struct{ fakeSession }

func (s tagActionsSession) CreateTag(_ context.Context, name, ref, message string, annotated bool) error {
	s.calls.record(fmt.Sprintf("create-tag:%s|%s|%s|%t", name, ref, message, annotated))
	return nil
}

func (s tagActionsSession) PushTag(_ context.Context, remote, name string) error {
	s.calls.record("push-tag:" + remote + ":" + name)
	return nil
}

func (s tagActionsSession) DeleteTag(_ context.Context, name string) error {
	s.calls.record("delete-tag:" + name)
	return nil
}

func tagActionsTestApp(t *testing.T) (*harness, *callRecorder) {
	t.Helper()
	h, calls := menuTestAppWithCalls(t)
	h.app.session = tagActionsSession{fakeSession: h.app.session.(fakeSession)}
	h.app.workspaceTab = sectionTags
	h.tester.Frame()
	return h, calls
}

func TestTagCreateFormSubmitsLightweightAndAnnotatedTags(t *testing.T) {
	for _, variant := range []struct {
		name, ref, message string
		annotated          bool
	}{
		{name: "v3.0.0", ref: "HEAD"},
		{name: "v3.1.0", ref: "feature", message: "Release notes", annotated: true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			h, calls := tagActionsTestApp(t)
			h.clickButton(t, "Create tag")
			if !h.app.showTagForm || h.app.tagRef != "HEAD" {
				t.Fatalf("create form open=%t target=%q, want HEAD", h.app.showTagForm, h.app.tagRef)
			}
			checkSubmit := func(disabled bool) {
				t.Helper()
				button, found := h.tester.AccessByRole(ui.RoleButton, "Create tag")
				if !found || button.Disabled != disabled {
					t.Fatalf("Create tag button found=%t disabled=%t, want disabled=%t", found, button.Disabled, disabled)
				}
			}
			checkSubmit(true)
			h.tester.Type(variant.name)
			h.tester.Frame()
			if variant.annotated {
				h.clickAndSettle(t, "Target ref")
				h.tester.Key(ui.Ctrl, ui.KeyA)
				h.tester.Type(variant.ref)
				h.tester.Frame()
				h.clickAndSettle(t, "Annotated tag")
				checkSubmit(true)
				h.clickAndSettle(t, "Tag message")
				h.tester.Type(variant.message)
				h.tester.Frame()
			}
			checkSubmit(false)
			h.clickButton(t, "Create tag")
			call := fmt.Sprintf("create-tag:%s|%s|%s|%t", variant.name, variant.ref, variant.message, variant.annotated)
			if h.app.confirm == nil || calls.has(call) {
				t.Fatal("creating the tag did not wait for confirmation")
			}
			h.clickButton(t, "Add tag")
			h.waitForCall(t, calls, call)
		})
	}
}

func TestTagToolbarAndMenuConfirmActions(t *testing.T) {
	for _, action := range []struct {
		item, submit, confirm, call string
		inMenu                      bool
	}{
		{"Push tag", "Push selected tag", "Continue", "push-tag:origin:build/interim", true},
		{"Delete tag", "", "Delete", "delete-tag:build/interim", true},
		{"Delete remote tag", "Delete remote tag", "Delete remote tag", "delete-remote-tag:origin:build/interim", true},
		{"Push tag", "Push selected tag", "Continue", "push-tag:origin:build/interim", false},
		{"Delete tag", "", "Delete", "delete-tag:build/interim", false},
		{"Delete remote tag", "Delete remote tag", "Delete remote tag", "delete-remote-tag:origin:build/interim", false},
		{"Push all tags", "", "Continue", "push-all-tags", false},
	} {
		entry := "toolbar"
		if action.inMenu {
			entry = "menu"
		}
		t.Run(entry+"/"+action.item, func(t *testing.T) {
			h, calls := tagActionsTestApp(t)
			if action.inMenu {
				h.rightClick(t, "build/interim")
				h.chooseMenuItem(t, action.item)
			} else {
				h.clickAndSettle(t, "build/interim")
				h.clickButton(t, action.item)
			}
			if action.submit != "" {
				if h.app.tagRemote != "origin" {
					t.Fatalf("remote = %q, want origin", h.app.tagRemote)
				}
				h.clickButton(t, action.submit)
			}
			if h.app.confirm == nil || calls.has(action.call) {
				t.Fatalf("%s did not wait for confirmation", action.item)
			}
			h.clickButton(t, action.confirm)
			h.waitForCall(t, calls, action.call)
		})
	}
}

func TestTagMenuCopiesTheClickedTag(t *testing.T) {
	h, _ := tagActionsTestApp(t)
	h.rightClick(t, "build/interim")
	h.chooseMenuItem(t, "Copy tag name")
	if got := h.tester.Clipboard(); got != "build/interim" {
		t.Fatalf("clipboard = %q, want build/interim", got)
	}
}

// Searching tags narrows the visible list but keeps the source tag selected,
// so actions and the details pane still refer to the matching tag.
func TestTagSearchFiltersWithoutChangingTheSelectedTag(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionTags
	h.tester.Frame()

	if err := h.tester.Click("Search repository tags"); err != nil {
		t.Fatalf("focus tag search: %v", err)
	}
	h.tester.Type("interim")
	h.tester.Frame()

	visible := h.app.visibleTags()
	if len(visible) != 1 || visible[0].tag.Name != "build/interim" {
		t.Fatalf("tag search rows = %#v, want only build/interim", visible)
	}
	if h.app.selectedTag != 1 {
		t.Fatalf("filtered selection targets source tag %d, want 1", h.app.selectedTag)
	}
	if !h.tester.HasText("Interim build") {
		t.Fatalf("filtered tag details are missing: %q", h.texts())
	}
}
