package app

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func guidedOperationTestApp() *app {
	a := newApp(fakeOpener{})
	a.settings.ConflictActionStyle = conflictActionStyleGuided
	a.snapshot.Operation = string(repository.OperationTypeRebase)
	a.snapshot.OperationState = &repository.OperationState{
		Type: repository.OperationTypeRebase, Step: 2, Done: 1, Total: 5,
		FullRefs: []string{"refs/heads/a-very-long-guided-topic-name"},
		Actions:  []repository.OperationAction{repository.OperationActionContinue, repository.OperationActionAbort},
	}
	file := repository.File{Path: "src/conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true}
	a.snapshot.Files = []repository.File{file}
	a.changeRows = []changeRow{{file: file}}
	a.selectedFile = 0
	a.conflictState = newConflictState(file.Path, "<<<<<<< HEAD\nours one\n=======\ntheirs one\n>>>>>>> topic\nmiddle\n<<<<<<< HEAD\nours two\n=======\ntheirs two\n>>>>>>> topic\n", 0)
	a.conflictState.selected = 1
	a.conflictStageTab = 3
	return a
}

func TestGuidedOperationUsesLiveProgressAndCanHide(t *testing.T) {
	a := guidedOperationTestApp()
	tt := ui.NewTester(a.operationContextView, 640, 760)
	tt.Frame()

	for _, want := range []string{
		"Guided conflict resolution", "Step 2 of 5", "File: src/conflict.txt", "Hunk 2 of 2",
		"1 conflict to resolve", "Read conflict", "Choose or edit Result", "Mark resolved",
		"Review staged", "Continue", "Choose or edit Result: Current", "Mark resolved: Next",
		"Made a mistake here? Restore unresolved returns the saved unresolved file and Git stages.",
		"No recovery is available for the selected file.",
	} {
		if !tt.HasText(want) {
			t.Errorf("guided operation does not show %q: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Mark resolved: Done") {
		t.Fatal("an unresolved file must not report Mark resolved as done")
	}
	progressLabels, operationTypeLabels := 0, 0
	for _, text := range tt.Texts() {
		if text == "Step 2 of 5" {
			progressLabels++
		}
		if text == "REBASE" {
			operationTypeLabels++
		}
	}
	if progressLabels != 1 || operationTypeLabels != 1 {
		t.Fatalf("guided mode repeated progress/type %d/%d times: %q", progressLabels, operationTypeLabels, tt.Texts())
	}
	if tt.HasText("Pick a side, then Continue.") {
		t.Fatal("generic guidance duplicated the detailed guided instructions")
	}

	// Removing the actual markers advances the derived onboarding stage. No
	// separate tutorial counter or Git progress state is involved.
	a.conflictState.adoptText("resolved\n")
	tt.Frame()
	if !tt.HasText("Mark resolved: Current") {
		t.Fatalf("the real Result state did not advance to Mark resolved: %q", tt.Texts())
	}
	if tt.HasText("Choose or edit Result: Current") {
		t.Fatal("the previous onboarding stage stayed current")
	}

	if err := tt.Click("Hide guide"); err != nil {
		t.Fatalf("hide guided onboarding: %v", err)
	}
	tt.Frame()
	if tt.HasText("Guided conflict resolution") || !tt.HasText("Show guide") {
		t.Fatalf("Hide guide did not leave a compact restore point: %q", tt.Texts())
	}
	if err := tt.Click("Show guide"); err != nil {
		t.Fatalf("show guided onboarding again: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Guided conflict resolution") {
		t.Fatal("Show guide did not restore the instruction panel")
	}
}

func TestGuidedOperationFollowsStagedAndContinueState(t *testing.T) {
	a := guidedOperationTestApp()
	tt := ui.NewTester(a.operationContextView, 640, 760)
	tt.Frame()

	staged := repository.File{Path: "src/conflict.txt", Status: "M ", Staged: true}
	a.snapshot.Files = []repository.File{staged}
	a.changeRows = []changeRow{{file: staged, staged: true}}
	a.conflictState = nil
	tt.Frame()
	for _, want := range []string{"Review staged: Current", "Continue: Available", "Mark resolved: Earlier", "No conflicts left to resolve"} {
		if !tt.HasText(want) {
			t.Errorf("staged operation does not show %q: %q", want, tt.Texts())
		}
	}

	a.snapshot.Files = nil
	a.changeRows = nil
	a.selectedFile = -1
	tt.Frame()
	if !tt.HasText("Continue: Current") {
		t.Fatalf("an operation without remaining work did not reach Continue: %q", tt.Texts())
	}
}

func TestGuidedOperationRecoveryUsesDurableRecord(t *testing.T) {
	for _, test := range []struct {
		name             string
		status           string
		wantStatus       string
		wantAction       string
		wantConfirmation string
	}{
		{name: "ready", status: repository.FileRecoveryReady, wantStatus: "Ready", wantAction: "Restore unresolved…", wantConfirmation: "Restore Unresolved Conflict"},
		{name: "interrupted", status: repository.FileRecoveryInterrupted, wantStatus: "Interrupted", wantAction: "Retry…", wantConfirmation: "Retry File Recovery"},
		{name: "no record", wantStatus: "No recovery is available for the selected file."},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := guidedOperationTestApp()
			a.session = &fakeSession{}
			if test.status != "" {
				record := repository.FileRecoveryRecord{
					OperationID: "guided-recovery", Path: "src/conflict.txt", Status: test.status,
					Progress: repository.FileRecoveryPending, Error: "saved interruption detail",
				}
				record.Initial.Index.Stage2.Present = true
				a.snapshot.FileRecoveries = []repository.FileRecoveryRecord{record}
			}
			tt := ui.NewTester(a.operationContextView, 640, 760)
			tt.Frame()
			if !tt.HasText("Made a mistake here? Restore unresolved returns the saved unresolved file and Git stages.") {
				t.Fatal("the fixed recovery explanation is missing")
			}
			if !tt.HasText(test.wantStatus) {
				t.Fatalf("recovery status %q is missing: %q", test.wantStatus, tt.Texts())
			}
			if test.wantAction == "" {
				for _, unavailable := range []string{"Restore unresolved…", "Retry…"} {
					if tt.HasText(unavailable) {
						t.Errorf("no-record guidance renders fake action %q", unavailable)
					}
				}
				return
			}
			if !tt.HasText(test.wantAction) {
				t.Fatalf("%s recovery action is missing: %q", test.status, tt.Texts())
			}
			if err := tt.Click(test.wantAction); err != nil {
				t.Fatalf("activate %s recovery: %v", test.status, err)
			}
			tt.Frame()
			if a.confirm == nil || a.confirm.title != test.wantConfirmation {
				t.Fatalf("recovery used the wrong existing confirmation: %#v", a.confirm)
			}
		})
	}
}

func TestGuidedOperationFitsNarrowPanesAndThemes(t *testing.T) {
	for _, width := range []int{1200, 960, 720, 320, 240} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/dark=%v", width, dark), func(t *testing.T) {
				a := guidedOperationTestApp()
				record := repository.FileRecoveryRecord{
					OperationID: "guided-recovery", Path: "src/conflict.txt",
					Status: repository.FileRecoveryInterrupted, Progress: repository.FileRecoveryPending,
					Error: "the saved recovery stopped before updating the index",
				}
				record.Initial.Index.Stage2.Present = true
				a.snapshot.FileRecoveries = []repository.FileRecoveryRecord{record}
				tt := ui.NewTester(a.operationContextView, width, 1200)
				tt.SetDark(dark)
				tt.Frame()
				contextText := "refs/heads/a-very-long-guided-topic-name"
				if width < 360 {
					contextText = "a-very-long-guided-topic-name"
				}
				for _, want := range []string{"Guided conflict resolution", contextText, "File: src/conflict.txt", "Hunk 2 of 2", "Retry…"} {
					element, found := tt.Find(want)
					if !found || element.X < 0 || element.X+element.W > float32(width)+0.5 || element.H <= 0 {
						t.Errorf("%q is clipped at %d: %+v, found=%v", want, width, element, found)
					}
				}
				if width == 240 {
					context, contextFound := tt.Find(contextText)
					progress, progressFound := tt.Find("Step 2 of 5")
					if !contextFound || !progressFound || context.H <= progress.H {
						t.Errorf("the long guided ref stayed on one truncated line: context=%+v, progress=%+v", context, progress)
					}
				}
				for _, item := range tt.AccessAll() {
					if item.Role != ui.RoleButton || item.Offscreen {
						continue
					}
					bounds := item.Bounds
					if bounds.W <= 0 || bounds.H <= 0 || bounds.X < 0 || bounds.X+bounds.W > float32(width)+0.5 {
						t.Errorf("%s is horizontally clipped at %d: %+v", item.Name, width, bounds)
					}
				}
			})
		}
	}
}

// A compact operation card must spend its horizontal space on the operation
// and conflict guidance, not on secondary completion metadata or a long ref.
func TestOperationContextRespondsToItsPaneWidth(t *testing.T) {
	for _, test := range []struct {
		name              string
		width             int
		wantDone          bool
		wantQualifyingRef string
	}{
		{name: "narrow uses compact context", width: 300, wantQualifyingRef: "demo/rebase-topic"},
		{name: "wide keeps all metadata", width: 560, wantDone: true, wantQualifyingRef: "refs/heads/demo/rebase-topic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApp(fakeOpener{})
			a.snapshot.OperationState = &repository.OperationState{
				Type: repository.OperationTypeRebase, Step: 3, Done: 2, Total: 5,
				FullRefs: []string{"refs/heads/demo/rebase-topic"},
			}
			a.snapshot.Files = []repository.File{
				{Path: "one.txt", Status: "UU", HasMergeConflicts: true},
				{Path: "two.txt", Status: "AA", HasMergeConflicts: true},
			}
			tt := ui.NewTester(func(c *ui.Context) {
				a.operationContextView(c)
			}, test.width, 220)
			tt.SetDark(false)
			tt.Frame()

			count, found := tt.Find("2 conflicts to resolve")
			if !found {
				t.Fatalf("the operation's conflict headline is missing: %q", tt.Texts())
			}
			hint, found := tt.Find("Pick a side, then Continue.")
			if !found {
				t.Fatalf("the operation's conflict guidance is missing: %q", tt.Texts())
			}
			progress, found := tt.Find("Step 3 of 5")
			if !found {
				t.Fatal("the operation progress is missing")
			}
			if hint.Y < count.Y+count.H {
				t.Errorf("conflict guidance starts at %.1f, before the headline at %.1f ends at %.1f; it needs its own line", hint.Y, count.Y, count.Y+count.H)
			}
			if hint.H < progress.H {
				t.Errorf("conflict guidance height %.1f is smaller than normal body text", hint.H)
			}
			if slices.Contains(tt.Texts(), "2 completed") != test.wantDone {
				t.Errorf("completed metadata presence = %v, want %v", !test.wantDone, test.wantDone)
			}
			if !slices.Contains(tt.Texts(), test.wantQualifyingRef) {
				t.Errorf("context does not contain %q: %q", test.wantQualifyingRef, tt.Texts())
			}
		})
	}
}
