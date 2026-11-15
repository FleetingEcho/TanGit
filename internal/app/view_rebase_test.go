package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type rebaseViewSession struct {
	operationDrivenSession
	plan repository.RebasePlan
}

func (s *rebaseViewSession) PrepareRebasePlan(_ context.Context, options repository.RebasePlanOptions) (repository.RebasePlan, error) {
	s.calls.record("prepare-plan:" + options.TargetRef + ":" + options.MarkedBase)
	plan := s.plan
	plan.Active = false
	plan.TargetRef = options.TargetRef
	plan.MarkedBaseSHA = options.MarkedBase
	return plan, nil
}

func (s *rebaseViewSession) ReadRebasePlan(context.Context) (repository.RebasePlan, error) {
	s.calls.record("read-plan")
	return s.plan, nil
}

func (s *rebaseViewSession) StartRebasePlan(_ context.Context, plan repository.RebasePlan, edited string) error {
	s.calls.record("start-plan:" + plan.TargetRef + ":" + plan.MarkedBaseSHA + ":" + edited)
	return nil
}

func (s *rebaseViewSession) EditRebasePlan(_ context.Context, plan repository.RebasePlan, edited string) error {
	s.calls.record("edit-plan:" + plan.Fingerprint + ":" + plan.OperationFingerprint + ":" + edited)
	return nil
}

func (s *rebaseViewSession) RebaseOntoBranchFromBase(_ context.Context, name, base string) error {
	s.calls.record("rebase-from-base:" + name + ":" + base)
	return nil
}

func rebaseViewModel() (repository.OperationState, repository.RebasePlan) {
	first, second := strings.Repeat("a", 40), strings.Repeat("b", 40)
	steps := []repository.OperationStep{
		{Action: "pick", Hash: first, Subject: "Already applied commit", Status: repository.OperationStepDone, Raw: "pick " + first + " Already applied commit"},
		{Action: "edit", Hash: second, Subject: "Current conflicted commit", Status: repository.OperationStepCurrent, Raw: "edit " + second + " Current conflicted commit"},
		{Action: "exec", Status: repository.OperationStepPending, Raw: "exec make test"},
		{Action: "update-ref", UpdateRef: "refs/heads/stack/topic", Status: repository.OperationStepPending, Raw: "update-ref refs/heads/stack/topic"},
	}
	state := richRebaseOperation()
	state.Steps = steps
	state.Step, state.Done, state.Total = 2, 1, 4
	planText := "exec make test\nupdate-ref refs/heads/stack/topic\n"
	return state, repository.RebasePlan{
		Text: planText, Fingerprint: "todo-version", Steps: steps[2:], Active: true,
		OperationFingerprint: state.Fingerprint, HeadSHA: state.OrigSHA,
	}
}

func rebaseViewHarness(t *testing.T, width, height int) (*harness, *rebaseViewSession) {
	t.Helper()
	state, plan := rebaseViewModel()
	calls := &callRecorder{}
	session := &rebaseViewSession{
		operationDrivenSession: operationDrivenSession{
			fakeSession: fakeSession{calls: calls, snapshot: repository.Snapshot{Root: "/work/project", Branch: "feature"}},
			state:       state,
		},
		plan: plan,
	}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionHistory
	a.historyWorkspace = historyWorkspaceAuto
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, width, height)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, session
}

func TestRebaseWorkspaceRendersOperationStepsAtWideAndNarrowWidths(t *testing.T) {
	for _, width := range []int{1100, 420, 320} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			state, _ := rebaseViewModel()
			a := newApp(fakeOpener{})
			a.workspaceTab = sectionHistory
			a.snapshot.Operation = "rebase"
			a.snapshot.OperationState = &state
			a.snapshot.Commits = []repository.Commit{{Subject: "ordinary history must not become a rebase plan"}}
			a.session = fakeSession{snapshot: a.snapshot}
			tt := ui.NewTester(func(c *ui.Context) { a.rebaseHistoryView(c) }, width, 720)
			tt.SetDark(true)
			tt.Frame()
			for _, want := range []string{
				"Rebase TODO", "edit", "current", "pick", "done", "exec", "pending",
				strings.Repeat("a", 40), "Already applied commit", "Current conflicted commit",
				"update-ref", "refs/heads/stack/topic", state.OntoSHA, "Step 2 of 4",
			} {
				if !tt.HasText(want) {
					t.Errorf("Rebase TODO lacks %q: %q", want, tt.Texts())
				}
			}
			if tt.HasText("ordinary history must not become a rebase plan") {
				t.Fatal("ordinary commit rows were used as a rebase plan mock")
			}
			for _, label := range []string{strings.Repeat("a", 40), "refs/heads/stack/topic", "Current conflicted commit", tr("onto ") + state.OntoSHA} {
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("missing row content %q", label)
				}
				if r.X < 0 || r.X+r.W > float32(width)+1 {
					t.Fatalf("%q overflows width %d: %+v", label, width, r)
				}
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, fmt.Sprintf("rebase-todo-%d", width), tt)
			}
		})
	}
}

func TestRebasePlanEditorSubmitsWholeTodoWithDirectives(t *testing.T) {
	h, session := rebaseViewHarness(t, 1100, 800)
	session.plan.Text = "exec make test\nlabel onto\nreset onto\nupdate-ref refs/heads/stack/topic\n"
	h.app.openActiveRebasePlan()
	h.tester.Frame()
	h.settle(t)
	if !h.app.rebasePlanEditorOpen || h.app.rebasePlan == nil || !h.tester.HasText("Complete Git rebase todo") {
		t.Fatalf("active todo editor did not load: open=%v plan=%#v texts=%q", h.app.rebasePlanEditorOpen, h.app.rebasePlan, h.texts())
	}
	original := h.app.rebasePlanText
	// Reordering is legal; adding, removing or replacing a directive is not.
	h.app.rebasePlanText = "update-ref refs/heads/stack/topic\nexec make test\nlabel onto\nreset onto\n"
	edited := h.app.rebasePlanText
	if edited == original {
		t.Fatal("test did not reorder the todo")
	}
	h.app.rebasePlanError = ""
	h.tester.Frame()
	if err := h.tester.Click("Save TODO"); err != nil {
		t.Fatalf("save active TODO: %v; action error %q", err, h.app.actionError)
	}
	h.tester.Frame()
	h.settle(t)
	want := "edit-plan:todo-version:approved-operation:" + edited
	if !session.calls.has(want) {
		t.Fatalf("session calls = %q, want whole TODO %q", session.calls.all(), want)
	}
	for _, directive := range []string{"exec make test", "update-ref refs/heads/stack/topic", "label onto", "reset onto"} {
		if !strings.Contains(want, directive) {
			t.Errorf("submitted plan lost %q", directive)
		}
	}
}

func TestRebasePlanEditorActionsFitCompactWindows(t *testing.T) {
	for _, height := range []int{220, 320} {
		t.Run(fmt.Sprintf("height_%d", height), func(t *testing.T) {
			_, plan := rebaseViewModel()
			a := newApp(fakeOpener{})
			a.rebasePlanEditorOpen = true
			a.rebasePlan = &plan
			a.rebasePlanText = plan.Text
			a.session = fakeSession{}
			tt := ui.NewTester(a.rebasePlanModal, 360, height)
			tt.SetDark(true)
			tt.Frame()
			for _, label := range []string{"Cancel", "Save TODO"} {
				r, ok := tt.Find(label)
				if !ok || r.W <= 0 || r.H <= 0 || r.Y < 0 || r.Y+r.H > float32(height)+1 {
					t.Fatalf("%q is not visible in %dx%d: %+v, texts %q", label, 360, height, r, tt.Texts())
				}
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, fmt.Sprintf("rebase-plan-editor-%d", height), tt)
			}
		})
	}
}

func TestRepositoryResetDropsAllRebaseDraftsAndMarks(t *testing.T) {
	a := newApp(fakeOpener{})
	_, plan := rebaseViewModel()
	full := strings.Repeat("e", 40)
	a.historyWorkspace = historyWorkspaceRebase
	a.markedBaseCommit = repository.Commit{Hash: full}
	a.rebaseTargetRef = "target"
	a.rebaseDraft = &plan
	a.rebasePlan = &plan
	a.rebasePlanText = plan.Text
	a.rebasePlanEditorOpen = true
	a.rebasePlanLoading = true
	a.rebasePlanError = "old error"
	a.selectedRebaseStep = 2
	token := a.rebasePlanToken
	a.resetRepositoryState(repository.Snapshot{
		Root: "/second/clone", Branch: "main",
		Commits: []repository.Commit{{Hash: full, Subject: "Same history in another clone"}},
	})
	if a.rebasePlanToken == token {
		t.Fatal("repository switch did not invalidate the in-flight rebase-plan read")
	}
	if a.markedBaseCommit.Hash != "" || a.rebaseTargetRef != "" || a.rebaseDraft != nil ||
		a.rebasePlan != nil || a.rebasePlanText != "" || a.rebasePlanEditorOpen ||
		a.rebasePlanLoading || a.rebasePlanError != "" || a.selectedRebaseStep != -1 ||
		a.historyWorkspace != historyWorkspaceAuto {
		t.Fatalf("repository-bound rebase state survived: %#v", a)
	}
}

func TestRebaseStepActionUsesPlanCASInsteadOfHistoryRewrite(t *testing.T) {
	h, session := rebaseViewHarness(t, 1000, 760)
	hash := strings.Repeat("c", 40)
	step := repository.OperationStep{
		Action: "pick", Hash: hash, Subject: "Pending commit", Status: repository.OperationStepPending,
		Raw: "pick " + hash + " Pending commit",
	}
	session.state.Steps = append(session.state.Steps, step)
	session.plan.Steps = append(session.plan.Steps, step)
	session.plan.Text += step.Raw + "\n"
	state := *h.app.snapshot.OperationState
	state.Steps = append(state.Steps, step)
	h.app.snapshot.OperationState = &state
	h.app.changeActiveRebaseStep(step, "reword")
	h.tester.Frame()
	h.settle(t)
	wantText := "exec make test\nupdate-ref refs/heads/stack/topic\nreword " + hash + " Pending commit\n"
	want := "edit-plan:todo-version:approved-operation:" + wantText
	if !session.calls.has(want) {
		t.Fatalf("visual action calls = %q, want %q (action error %q)", session.calls.all(), want, h.app.actionError)
	}
	if session.calls.has("reword:" + hash + ":Existing body") {
		t.Fatal("an active TODO action incorrectly invoked the ordinary history rewrite")
	}
}

func TestMarkedBaseUsesFullSHAAndOrdinaryRebaseStaysPlain(t *testing.T) {
	state, plan := rebaseViewModel()
	calls := &callRecorder{}
	session := &rebaseViewSession{
		operationDrivenSession: operationDrivenSession{fakeSession: fakeSession{calls: calls}},
		plan:                   plan,
	}
	a := newApp(fakeOpener{})
	_ = state
	if err := a.rebaseOntoBranch(context.Background(), session, "topic"); err != nil {
		t.Fatal(err)
	}
	if !calls.has("rebase-onto:topic") || calls.has("rebase-from-base:topic:") {
		t.Fatalf("ordinary rebase calls = %q", calls.all())
	}
	full := strings.Repeat("d", 40)
	a.markedBaseCommit = repository.Commit{Hash: full, ShortHash: full[:7]}
	if err := a.rebaseOntoBranch(context.Background(), session, "topic"); err != nil {
		t.Fatal(err)
	}
	if !calls.has("rebase-from-base:topic:" + full) {
		t.Fatalf("marked-base calls = %q, want full SHA", calls.all())
	}
}

func TestConflictAndRebaseWorkspacesSwitchBothWays(t *testing.T) {
	h, _ := rebaseViewHarness(t, 1100, 800)
	h.app.snapshot.Files = []repository.File{{Path: "conflicted.txt", HasMergeConflicts: true, Tracked: true}}
	h.app.setWorkspaceTab(sectionChanges)
	h.tester.Frame()
	if err := h.tester.Click("Back to Rebase TODO"); err != nil {
		t.Fatalf("open Rebase TODO from conflict UI: %v; texts %q", err, h.texts())
	}
	h.tester.Frame()
	if h.app.workspaceTab != sectionHistory || !h.tester.HasText("Rebase TODO") {
		t.Fatalf("conflict back action did not open real Rebase TODO: %q", h.texts())
	}
	if err := h.tester.Click("Resolve conflicts"); err != nil {
		t.Fatalf("return to conflict UI: %v; texts %q", err, h.texts())
	}
	if h.app.workspaceTab != sectionChanges {
		t.Fatalf("Resolve conflicts selected workspace %d", h.app.workspaceTab)
	}
}
