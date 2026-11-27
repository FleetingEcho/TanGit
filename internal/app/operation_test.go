package app

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type operationDrivenSession struct {
	fakeSession
	state repository.OperationState
}

func (s *operationDrivenSession) Snapshot(context.Context) (repository.Snapshot, error) {
	state := s.state
	snapshot := s.fakeSession.snapshot
	snapshot.Operation = string(state.Type)
	snapshot.OperationState = &state
	return snapshot, nil
}

func (s *operationDrivenSession) OperationState(context.Context) (repository.OperationState, error) {
	return s.state, nil
}

func (s *operationDrivenSession) DriveOperation(_ context.Context, options repository.OperationDriveOptions) error {
	s.calls.record("drive:" + string(options.Action) + ":" + options.ExpectedFingerprint)
	return nil
}

func operationTestApp(t *testing.T, state repository.OperationState) (*harness, *callRecorder) {
	t.Helper()
	calls := &callRecorder{}
	session := &operationDrivenSession{fakeSession: fakeSession{calls: calls, snapshot: repository.Snapshot{
		Root: "/work/project", Branch: "main",
	}}, state: state}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1440, 900)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, calls
}

func richRebaseOperation() repository.OperationState {
	return repository.OperationState{
		Type:        repository.OperationTypeRebase,
		FullRefs:    []string{"refs/heads/feature"},
		OntoSHA:     strings.Repeat("1", 40),
		OrigSHA:     strings.Repeat("2", 40),
		IncomingSHA: strings.Repeat("3", 40),
		StoppedSHA:  strings.Repeat("3", 40),
		Step:        2,
		Done:        1,
		Total:       5,
		Actions: []repository.OperationAction{
			repository.OperationActionContinue,
			repository.OperationActionSkip,
			repository.OperationActionAbort,
		},
		Fingerprint: "approved-operation",
	}
}

func TestOperationContextShowsRealRefsAndProgress(t *testing.T) {
	h, _ := operationTestApp(t, richRebaseOperation())
	h.tester.Frame()
	for _, want := range []string{"refs/heads/feature", "Step 2 of 5"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("operation context does not show %q: %q", want, h.texts())
		}
	}
	card, found := h.tester.Find("Operation context")
	if !found {
		t.Fatal("operation context card is missing")
	}
	h.tester.Move(card.X+card.W/2, card.Y+card.H/2)
	h.tester.Advance(time.Second)
	for _, want := range []string{"refs/heads/feature", "onto: " + richRebaseOperation().OntoSHA, "orig: " + richRebaseOperation().OrigSHA} {
		if !h.tester.HasText(want) {
			t.Errorf("the full operation tooltip does not show %q: %q", want, h.texts())
		}
	}
	h.tester.Move(0, 0)
	h.tester.Advance(2 * time.Second)
	if _, found := h.tester.Find("Skip…"); !found {
		t.Fatal("rebase Skip entry is missing")
	}
}

func TestOperationControlsDriveConfirmedFingerprint(t *testing.T) {
	for _, action := range []repository.OperationAction{
		repository.OperationActionSkip,
		repository.OperationActionAbort,
	} {
		t.Run(string(action), func(t *testing.T) {
			h, calls := operationTestApp(t, richRebaseOperation())
			button := map[repository.OperationAction]string{
				repository.OperationActionSkip:  "Skip…",
				repository.OperationActionAbort: "Abort…",
			}[action]
			if err := h.tester.Click(button); err != nil {
				t.Fatalf("open %s confirmation: %v", action, err)
			}
			h.tester.Frame()
			if h.app.confirm == nil {
				t.Fatalf("%s did not open its confirmation", action)
			}
			h.app.confirm.run()
			h.tester.Frame()
			h.settle(t)
			want := "drive:" + string(action) + ":approved-operation"
			if !calls.has(want) {
				t.Fatalf("session calls = %q, want %q (confirmation %#v)", calls.all(), want, h.app.confirm)
			}
		})
	}
}

func TestGuidedStyleKeepsExistingOperationDrivers(t *testing.T) {
	for _, test := range []struct {
		action repository.OperationAction
		button string
	}{
		{action: repository.OperationActionContinue, button: "Continue operation"},
		{action: repository.OperationActionSkip, button: "Skip…"},
		{action: repository.OperationActionAbort, button: "Abort…"},
	} {
		t.Run(string(test.action), func(t *testing.T) {
			h, _, calls := continueHarness(t, "required", "", continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
				return "required check passed", nil
			}))
			h.app.settings.ConflictActionStyle = conflictActionStyleGuided
			h.tester.Frame()
			switch test.action {
			case repository.OperationActionContinue:
				h.app.confirmContinueOperation()
				waitContinueComplete(t, h)
				if !h.tester.HasText("Continue preflight") {
					t.Fatalf("guided style removed Continue preflight: %q", h.texts())
				}
			default:
				if err := h.tester.Click(test.button); err != nil {
					t.Fatalf("open the existing %s confirmation: %v", test.action, err)
				}
				h.tester.Frame()
				if h.app.confirm == nil {
					t.Fatalf("guided style removed %s confirmation", test.action)
				}
				h.app.confirm.run()
			}
			if test.action == repository.OperationActionContinue {
				if err := h.tester.Click(test.button); err != nil {
					t.Fatalf("approve Continue after preflight: %v", err)
				}
			}
			h.tester.Frame()
			h.settle(t)
			want := "drive:" + string(test.action) + ":approved-operation"
			if !calls.has(want) {
				t.Fatalf("guided %s bypassed the fingerprint driver: %q", test.action, calls.all())
			}
		})
	}
}

func TestMergeDoesNotOfferSkip(t *testing.T) {
	state := richRebaseOperation()
	state.Type = repository.OperationTypeMerge
	state.Actions = []repository.OperationAction{repository.OperationActionContinue, repository.OperationActionAbort}
	h, _ := operationTestApp(t, state)
	h.tester.Frame()
	if _, found := h.tester.Find("Skip…"); found {
		t.Fatal("merge offered an unsupported Skip action")
	}
}
