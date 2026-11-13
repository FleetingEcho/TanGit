package app

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestConflictActionStyleNormalizesAndPersists(t *testing.T) {
	if got := defaultSettings().ConflictActionStyle; got != conflictActionStyleIcons {
		t.Fatalf("default action style = %q, want icons", got)
	}
	for input, want := range map[string]string{
		"":                  conflictActionStyleIcons,
		"icons":             conflictActionStyleIcons,
		"icons+labels":      conflictActionStyleIconsLabels,
		" ICONS+LABELS ":    conflictActionStyleIconsLabels,
		"labels":            conflictActionStyleIcons,
		"unknown-toolstrip": conflictActionStyleIcons,
	} {
		if got := normalizeConflictActionStyle(input); got != want {
			t.Errorf("normalize %q = %q, want %q", input, got, want)
		}
	}

	dir := t.TempDir()
	if err := saveAppSettings(dir, appSettings{ConflictActionStyle: " ICONS+LABELS "}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(settingsStorePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "ICONS+LABELS") || !strings.Contains(string(data), `"conflictActionStyle": "icons+labels"`) {
		t.Fatalf("settings did not store the canonical enum:\n%s", data)
	}
	if got := loadAppSettings(dir).ConflictActionStyle; got != conflictActionStyleIconsLabels {
		t.Fatalf("loaded action style = %q, want icons+labels", got)
	}
	if err := os.WriteFile(settingsStorePath(dir), []byte(`{"theme":"Tokyo Night"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadAppSettings(dir).ConflictActionStyle; got != conflictActionStyleIcons {
		t.Fatalf("legacy settings action style = %q, want icons", got)
	}
}

func actionPresentationSize(t *testing.T, h *harness, label, style string, width, height int) ui.AccessInfo {
	t.Helper()
	info, found := h.tester.Access(label)
	if !found {
		t.Fatalf("%q has no accessible action: %q", label, h.texts())
	}
	if info.Name != label || info.Description == "" {
		t.Fatalf("%q accessibility = name %q, description %q", label, info.Name, info.Description)
	}
	outOfView := func(bounds ui.Rect) bool {
		return bounds.Y < 0 || bounds.Y+bounds.H > float32(height)+0.5
	}
	bounds := info.Bounds
	if bounds.W <= 0 || bounds.H <= 0 || bounds.X < 0 || outOfView(bounds) ||
		bounds.X+bounds.W > float32(width)+0.5 {
		t.Fatalf("%q is clipped or has no hit target at %dx%d: %+v", label, width, height, bounds)
	}
	if group, compact := h.tester.Access("Apply"); compact && group.HasPopup != ui.PopupNone && style == conflictActionStyleIconsLabels {
		// A measured compact Apply group keeps common side choices as direct
		// icons; opening Apply supplies their visible labels and descriptions.
		style = conflictActionStyleIcons
	}
	switch style {
	case conflictActionStyleIcons:
		if bounds.W != 34 {
			t.Fatalf("icons %q width = %.1f, want the established 34 DIP", label, bounds.W)
		}
	case conflictActionStyleIconsLabels:
		if bounds.W <= 34 {
			t.Fatalf("icons+labels %q width = %.1f, want room for visible text", label, bounds.W)
		}
	}
	return info
}

// matrixAction accepts either an on-screen button or the same action in its
// narrow-window Popover menu. Grouping is expected for labels at 720 DIP; an
// unavailable or clipped trigger is not.
func matrixAction(t *testing.T, h *harness, label, toolbarLabel, style string, width, height int) (ui.AccessInfo, bool) {
	t.Helper()
	if _, found := h.tester.Access(label); found {
		return actionPresentationSize(t, h, label, style, width, height), true
	}
	triggerLabel := toolbarLabel
	trigger, found := h.tester.Access(triggerLabel)
	if !found {
		switch toolbarLabel {
		case "Conflict apply actions":
			triggerLabel = "Apply"
		case "Conflict navigation":
			triggerLabel = "Hunk"
		}
		trigger, found = h.tester.Access(triggerLabel)
	}
	if !found || trigger.HasPopup == ui.PopupNone {
		t.Fatalf("%q disappeared without its %q Popover trigger: %q", label, triggerLabel, h.texts())
	}
	bounds := trigger.Bounds
	if bounds.W <= 0 || bounds.H <= 0 || bounds.X < 0 || bounds.Y < 0 ||
		bounds.X+bounds.W > float32(width)+0.5 || bounds.Y+bounds.H > float32(height)+0.5 {
		t.Fatalf("%q Popover trigger is clipped: %+v", triggerLabel, bounds)
	}
	h.tester.ClickAt(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
	h.tester.Frame()
	item, found := h.tester.Access(label)
	if !found || item.Bounds.W <= 0 || item.Bounds.H <= 0 {
		t.Fatalf("%q is not reachable from %q Popover: %+v, %q", label, triggerLabel, item, h.texts())
	}
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	return item, false
}

func hoverAction(t *testing.T, h *harness, info ui.AccessInfo, explanation string) {
	t.Helper()
	bounds := info.Bounds
	h.tester.Move(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
	h.tester.Advance(time.Second)
	h.tester.Frame()
	if !h.tester.HasText(explanation) {
		t.Fatalf("hover did not expose %q: %q", explanation, h.texts())
	}
	h.tester.Move(0, 0)
	h.tester.Advance(2 * time.Second)
	h.tester.Frame()
}

func TestConflictActionStyleAcrossViewportsAndThemes(t *testing.T) {
	const content = "<<<<<<< HEAD\nours\n||||||| base\ncommon\n=======\ntheirs\n>>>>>>> topic\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() { readWorkingFile, writeWorkingFile = originalRead, originalWrite })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	writeWorkingFile = func(string, []byte) error { return nil }

	for _, viewport := range []struct{ width, height int }{{1280, 800}, {960, 640}, {720, 800}} {
		for _, dark := range []bool{false, true} {
			for _, style := range []string{conflictActionStyleIcons, conflictActionStyleIconsLabels} {
				name := fmt.Sprintf("%dx%d/dark=%v/%s", viewport.width, viewport.height, dark, style)
				t.Run(name, func(t *testing.T) {
					h, _ := changesTestAppIn(t, []repository.File{{
						Path: "conflict.txt", Status: "UU", Unstaged: true, Tracked: true, HasMergeConflicts: true,
					}}, viewport.width, viewport.height)
					h.app.settings.Theme = "System"
					h.app.settings.ConflictActionStyle = style
					h.tester.SetDark(dark)
					h.waitFor(t, "the styled conflict", func() bool { return h.app.conflictState != nil })
					h.tester.Frame()

					action, actionVisible := matrixAction(t, h, "Use current", "Conflict apply actions", style, viewport.width, viewport.height)
					if actionVisible && !strings.Contains(action.Description, "Keep the current side's hunk") {
						t.Fatalf("Use current description = %q", action.Description)
					}
					if viewport.width == 960 && actionVisible {
						hoverAction(t, h, action, "Keep the current side's hunk")
					}
					disabled, disabledVisible := matrixAction(t, h, "Previous conflict", "Conflict navigation", style, viewport.width, viewport.height)
					if !disabled.Disabled || !strings.Contains(disabled.Description, "Already at the first conflict.") {
						t.Fatalf("Previous conflict disabled explanation = %+v", disabled)
					}
					if viewport.width == 960 && disabledVisible {
						hoverAction(t, h, disabled, "Already at the first conflict.")
					}
				})
			}
		}
	}
}

func TestChangesHunkActionStyleAcrossViewportsAndThemes(t *testing.T) {
	for _, viewport := range []struct{ width, height int }{{1280, 800}, {960, 640}, {720, 800}} {
		for _, dark := range []bool{false, true} {
			for _, style := range []string{conflictActionStyleIcons, conflictActionStyleIconsLabels} {
				name := fmt.Sprintf("%dx%d/dark=%v/%s", viewport.width, viewport.height, dark, style)
				t.Run(name, func(t *testing.T) {
					h, _ := twoHunkChangesApp(t, unstagedTrackedFile())
					h.app.settings.Theme = "System"
					h.app.settings.ConflictActionStyle = style
					h.tester.SetSize(viewport.width, viewport.height)
					h.tester.SetDark(dark)
					h.tester.Frame()

					action, actionVisible := matrixAction(t, h, "Stage hunk", "Diff hunk actions", style, viewport.width, viewport.height)
					if actionVisible && !strings.Contains(action.Description, "leaving other hunks unstaged") {
						t.Fatalf("Stage hunk description = %q", action.Description)
					}
					disabled, disabledVisible := matrixAction(t, h, "Previous hunk", "Diff hunk actions", style, viewport.width, viewport.height)
					if !disabled.Disabled || !strings.Contains(disabled.Description, "Already at the first diff hunk.") {
						t.Fatalf("Previous hunk disabled explanation = %+v", disabled)
					}
					if viewport.width == 960 && actionVisible {
						hoverAction(t, h, action, "leaving other hunks unstaged")
						if disabledVisible {
							hoverAction(t, h, disabled, "Already at the first diff hunk.")
						}
					}
				})
			}
		}
	}
}

func TestContextHelpExplainsConflictActionsWithoutInventedShortcuts(t *testing.T) {
	a := newApp(fakeOpener{})
	a.state = stateReady
	a.workspaceTab = sectionChanges
	a.conflictState = newConflictState("conflict.txt", "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\n", 0)
	a.conflictStageTab = 3
	a.openCurrentKeybindings()
	tester := ui.NewTester(a.keybindingsModal, 960, 640)
	tester.Frame()
	for _, text := range []string{
		"Undo resolution restores Result to before the last hunk pick. It does not change the index or Git operation history; Undo can also reverse Restore initial conflict.",
		"Restore initial conflict resets Result to the conflict first opened, including markers removed by earlier picks. It does not reset a branch, index, or commit.",
		"Mark resolved stages the file exactly as it is. Combined conflict diffs cannot use Stage hunk or Discard hunk; resolve the conflict or choose a whole-file action instead.",
		"Conflict Assistant runs only after an explicit request. It previews suggestions on a clone; Accept writes Result only with V2 recovery and never stages, continues, or commits.",
		"Undo unsaved Result text, or the last hunk resolution when Result has no unsaved edits",
	} {
		if !tester.HasText(text) {
			t.Errorf("conflict Context help is missing %q", text)
		}
	}
	for _, group := range a.visibleKeyBindingGroups() {
		switch group.title {
		case "Everywhere":
			for _, binding := range group.bindings {
				if binding.keys == "Ctrl+Shift+Z" || binding.keys == "z" || binding.keys == "Z" {
					t.Errorf("Result help advertises unavailable Git history key %+v", binding)
				}
			}
		case "Changes":
			keys := 0
			for _, binding := range group.bindings {
				if binding.keys != "" {
					keys++
				}
				if binding.keys == "s" || binding.keys == "d" || binding.keys == "Space" {
					t.Errorf("Result help advertises ordinary Changes key %+v", binding)
				}
			}
			if keys != 4 {
				t.Errorf("Result Changes help has %d shortcuts, want its four real conflict chords", keys)
			}
		}
	}
	for _, binding := range a.changesActionHelp() {
		if binding.keys != "" {
			t.Errorf("explanatory action unexpectedly claims shortcut %q", binding.keys)
		}
	}
}

func TestContextHelpExplainsOrdinaryHunkTargets(t *testing.T) {
	a := newApp(fakeOpener{})
	a.state = stateReady
	a.workspaceTab = sectionChanges
	a.openCurrentKeybindings()
	tester := ui.NewTester(a.keybindingsModal, 960, 640)
	tester.Frame()
	for _, text := range []string{
		"Stage hunk applies only the current hunk to the index. Unstage hunk reverses only that index hunk and leaves its content in the working tree.",
		"Discard hunk asks for confirmation, removes only the current working-tree hunk, leaves the index untouched, and keeps a recovery copy for 7 days in Saved work.",
	} {
		if !tester.HasText(text) {
			t.Errorf("ordinary Changes Context help is missing %q", text)
		}
	}
	for _, group := range a.visibleKeyBindingGroups() {
		if group.title != "Changes" {
			continue
		}
		for _, binding := range group.bindings {
			switch binding.keys {
			case "F7 / Shift+F7", "Alt+1 / Alt+2", "Alt+3 / Alt+4", "Alt+Z":
				t.Errorf("ordinary Changes help advertises conflict-only key %+v", binding)
			}
		}
	}
}

func TestActionControlCatalogsCoverEveryLanguage(t *testing.T) {
	before := currentLanguage
	t.Cleanup(func() { currentLanguage = before })
	languages := []string{"en", "zh-Hans", "zh-Hant", "ja", "es", "fr"}
	for _, entry := range actionControlTranslations {
		want := []string{entry.english, entry.zhHans, entry.zhHant, entry.japanese, entry.spanish, entry.french}
		for index, language := range languages {
			currentLanguage = language
			if got := tr(entry.english); got != want[index] {
				t.Errorf("%s catalog %q = %q, want %q", language, entry.english, got, want[index])
			}
		}
	}
}

func TestSettingsActionStyleIsImmediatePersistentAndModalSafe(t *testing.T) {
	const content = "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() { readWorkingFile, writeWorkingFile = originalRead, originalWrite })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	writeWorkingFile = func(string, []byte) error { return nil }

	for _, dark := range []bool{false, true} {
		for _, target := range []string{conflictActionStyleIcons, conflictActionStyleIconsLabels} {
			t.Run(fmt.Sprintf("dark=%v/target=%s", dark, target), func(t *testing.T) {
				h, _ := changesTestAppIn(t, []repository.File{{
					Path: "conflict.txt", Status: "UU", Unstaged: true, Tracked: true, HasMergeConflicts: true,
				}}, 960, 640)
				h.app.pathUserData = t.TempDir()
				h.app.settings.Theme = "System"
				initial := conflictActionStyleIconsLabels
				targetLabel := "Icons"
				otherLabel := "Icons + labels"
				if target == conflictActionStyleIconsLabels {
					initial = conflictActionStyleIcons
					targetLabel, otherLabel = otherLabel, targetLabel
				}
				h.app.settings.ConflictActionStyle = initial
				h.tester.SetDark(dark)
				h.waitFor(t, "the Settings conflict fixture", func() bool { return h.app.conflictState != nil })
				h.app.openSettingsSection(settingsPageBehavior)
				h.tester.Frame()

				if err := h.tester.Click(targetLabel); err != nil {
					t.Fatalf("choose %s: %v; texts=%q", targetLabel, err, h.texts())
				}
				h.tester.Frame()
				if h.app.settings.ConflictActionStyle != target {
					t.Fatalf("immediate action style = %q, want %q", h.app.settings.ConflictActionStyle, target)
				}
				if got := loadAppSettings(h.app.pathUserData).ConflictActionStyle; got != target {
					t.Fatalf("persisted action style = %q, want %q", got, target)
				}
				if selected, ok := h.tester.AccessByRole(ui.RoleRadio, targetLabel); !ok || !selected.Checked {
					t.Fatalf("%s radio is not selected: %+v, found=%v", targetLabel, selected, ok)
				}

				if err := h.tester.Click("Done"); err != nil {
					t.Fatalf("close Settings: %v", err)
				}
				h.tester.Frame()
				h.tester.Frame()
				if h.app.settingsOpen {
					t.Fatal("Settings stayed open")
				}
				if _, found := h.tester.AccessByRole(ui.RoleDialog, "Settings"); found {
					t.Fatal("the dismissed Settings dialog stayed in the accessibility tree")
				}
				actionPresentationSize(t, h, "Use current", target, 960, 640)

				// Reopening rebuilds the modal with current state. Selecting the
				// other segment and returning again must not touch a stale element.
				h.app.openSettingsSection(settingsPageBehavior)
				h.tester.Frame()
				if selected, ok := h.tester.AccessByRole(ui.RoleRadio, targetLabel); !ok || !selected.Checked {
					t.Fatalf("reopened %s radio is stale: %+v, found=%v", targetLabel, selected, ok)
				}
				if err := h.tester.Click(otherLabel); err != nil {
					t.Fatalf("switch back to %s: %v", otherLabel, err)
				}
				h.tester.Frame()
				if err := h.tester.Click("Done"); err != nil {
					t.Fatalf("close rebuilt Settings: %v", err)
				}
				h.tester.Frame()
				h.tester.Frame()
				otherStyle := conflictActionStyleForChoice(1 - conflictActionStyleChoice(target))
				action := actionPresentationSize(t, h, "Use current", otherStyle, 960, 640)
				bounds := action.Bounds
				h.tester.ClickAt(bounds.X+bounds.W/2, bounds.Y+bounds.H/2)
				h.tester.Frame()
				if got := h.app.conflictState.text(); got != "ours\n" {
					t.Fatalf("returned conflict action wrote %q, want ours", got)
				}
			})
		}
	}
}
