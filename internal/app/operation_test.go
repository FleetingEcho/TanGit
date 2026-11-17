package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
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
	for _, want := range []string{"refs/heads/feature", "Step 2 of 5", "1 completed"} {
		if !slices.Contains(h.texts(), want) {
			t.Fatalf("operation context does not show %q: %q", want, h.texts())
		}
	}
	if _, found := h.tester.Find("Skip…"); !found {
		t.Fatal("rebase Skip entry is missing")
	}
}

func TestOperationControlsDriveConfirmedFingerprint(t *testing.T) {
	for _, action := range []repository.OperationAction{
		repository.OperationActionContinue,
		repository.OperationActionSkip,
		repository.OperationActionAbort,
	} {
		t.Run(string(action), func(t *testing.T) {
			h, calls := operationTestApp(t, richRebaseOperation())
			button := map[repository.OperationAction]string{
				repository.OperationActionContinue: "Continue",
				repository.OperationActionSkip:     "Skip…",
				repository.OperationActionAbort:    "Abort…",
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
