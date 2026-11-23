package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/workspace"
)

type continueDrivenSession struct {
	fakeSession
	mu            sync.Mutex
	source        repository.ContinueSource
	checkpointErr error
	captureErr    error
}

func newContinueDrivenSession(calls *callRecorder) *continueDrivenSession {
	operation := richRebaseOperation()
	return &continueDrivenSession{
		fakeSession: fakeSession{calls: calls, snapshot: repository.Snapshot{Root: "/work/project", Branch: "main"}},
		source: repository.ContinueSource{
			Operation:       operation,
			IndexHash:       strings.Repeat("a", 64),
			WorkingTreeHash: strings.Repeat("b", 64),
		},
	}
}

func (s *continueDrivenSession) Snapshot(context.Context) (repository.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.fakeSession.snapshot
	operation := s.source.Operation
	snapshot.Operation = string(operation.Type)
	snapshot.OperationState = &operation
	return snapshot, nil
}

func (s *continueDrivenSession) OperationState(context.Context) (repository.OperationState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source.Operation, nil
}

func (s *continueDrivenSession) CaptureContinueSource(ctx context.Context) (repository.ContinueSource, error) {
	if err := ctx.Err(); err != nil {
		return repository.ContinueSource{}, err
	}
	s.calls.record("capture-continue-source")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source, s.captureErr
}

func (s *continueDrivenSession) DriveOperation(_ context.Context, options repository.OperationDriveOptions) error {
	s.calls.record("drive:" + string(options.Action) + ":" + options.ExpectedFingerprint)
	return nil
}

func (s *continueDrivenSession) CreateCheckpoint(_ context.Context, name string) (repository.WorkSnapshot, error) {
	s.calls.record("checkpoint:" + name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkpointErr != nil {
		return repository.WorkSnapshot{}, s.checkpointErr
	}
	return repository.WorkSnapshot{ID: "checkpoint", Kind: repository.CheckpointSnapshotKind, Name: name}, nil
}

func (s *continueDrivenSession) updateSource(update func(*repository.ContinueSource)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	update(&s.source)
}

func continueHarness(t *testing.T, required, optional string, runner continueTestRunner) (*harness, *continueDrivenSession, *callRecorder) {
	t.Helper()
	calls := &callRecorder{}
	session := newContinueDrivenSession(calls)
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 900)
	a.update = h.updates.update
	a.path = "/work/project"
	a.settings.ContinuePreflight.RequiredCommand = required
	a.settings.ContinuePreflight.OptionalCommand = optional
	a.continueTestRunner = runner
	openFixture(t, h.tester)
	h.settle(t)
	return h, session, calls
}

func waitContinueComplete(t *testing.T, h *harness) {
	t.Helper()
	h.waitFor(t, "Continue checks to finish", func() bool {
		review := h.app.continuePreflight.review
		return review != nil && review.complete
	})
	h.tester.Frame()
}

func TestContinueRequiredCommandMustBeConfigured(t *testing.T) {
	var runs atomic.Int32
	h, _, calls := continueHarness(t, "", "optional", continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
		runs.Add(1)
		return "", nil
	}))
	h.app.confirmContinueOperation()
	waitContinueComplete(t, h)
	if runs.Load() != 0 {
		t.Fatal("optional check ran while its required gate was unconfigured")
	}
	for _, want := range []string{"Required check is not configured", "fully trusted", "not sandboxed", "Configure commands"} {
		if !h.tester.HasText(want) {
			t.Errorf("unconfigured required check does not show %q: %q", want, h.texts())
		}
	}
	h.app.finishContinueReview(false)
	h.settle(t)
	if calls.has("drive:continue:approved-operation") {
		t.Fatal("an unconfigured required check reached DriveOperation")
	}
}

func TestContinueOptionalBlankIsSkippedAndSuccessUsesDriverCAS(t *testing.T) {
	var commands []string
	var mu sync.Mutex
	h, _, calls := continueHarness(t, "required", "", continueTestRunnerFunc(func(_ context.Context, root, command string) (string, error) {
		mu.Lock()
		commands = append(commands, root+":"+command)
		mu.Unlock()
		return "required output", nil
	}))
	if err := h.tester.Click("Continue"); err != nil {
		t.Fatalf("open Continue preflight from the operation control: %v", err)
	}
	waitContinueComplete(t, h)
	if calls.has("drive:continue:approved-operation") {
		t.Fatal("Continue ran before the user reviewed the completed checks")
	}
	for _, want := range []string{"Required: passed", "Optional: skipped", "required output", "Checkpoint: skipped"} {
		if !h.tester.HasText(want) {
			t.Errorf("completed review does not show %q: %q", want, h.texts())
		}
	}
	if err := h.tester.Click("Continue operation"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the CAS-driven Continue", func() bool { return calls.has("drive:continue:approved-operation") })
	mu.Lock()
	defer mu.Unlock()
	if len(commands) != 1 || commands[0] != "/work/project:required" {
		t.Fatalf("trusted check commands = %#v", commands)
	}
}

func TestContinueRequiredFailureAndSafetyTextCannotReachDriver(t *testing.T) {
	h, _, calls := continueHarness(t, "required", "optional", continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
		return "test diagnostics\n__IT_IS_SAFE__", errors.New("exit status 1")
	}))
	h.app.confirmContinueOperation()
	waitContinueComplete(t, h)
	for _, want := range []string{"Required: failed", "test diagnostics", "__IT_IS_SAFE__", "Retry checks"} {
		if !h.tester.HasText(want) {
			t.Errorf("failed required review does not show %q: %q", want, h.texts())
		}
	}
	if h.tester.HasText("Override optional failure and continue") {
		t.Fatal("required failure offered an optional override")
	}
	h.app.finishContinueReview(false)
	h.app.finishContinueReview(true)
	h.settle(t)
	if calls.has("drive:continue:approved-operation") {
		t.Fatal("required failure or its output reached DriveOperation")
	}
}

func TestContinueOptionalFailureRequiresExplicitOverrideAndUsesSameCAS(t *testing.T) {
	h, _, calls := continueHarness(t, "required", "optional", continueTestRunnerFunc(func(_ context.Context, _ string, command string) (string, error) {
		if command == "optional" {
			return "optional failed\n__IT_IS_SAFE__", errors.New("exit status 2")
		}
		return "required passed", nil
	}))
	h.app.confirmContinueOperation()
	waitContinueComplete(t, h)
	if !h.tester.HasText("Optional: failed") || !h.tester.HasText("optional failed") {
		t.Fatalf("optional failure and output are not visible: %q", h.texts())
	}
	h.app.finishContinueReview(false)
	h.settle(t)
	if calls.has("drive:continue:approved-operation") {
		t.Fatal("optional failure did not wait for an explicit override")
	}
	if err := h.tester.Click("Override optional failure and continue"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the explicitly overridden CAS-driven Continue", func() bool {
		return calls.has("drive:continue:approved-operation")
	})
}

func TestContinueCancellationOutputRetryAndNoPrematureDriver(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	h, _, calls := continueHarness(t, "required", "", continueTestRunnerFunc(func(ctx context.Context, _, _ string) (string, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return "partial cancellation output", ctx.Err()
	}))
	h.app.confirmContinueOperation()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("required check did not start")
	}
	h.waitFor(t, "the running review", func() bool {
		return h.app.continuePreflight.review != nil && h.app.continuePreflight.review.running
	})
	h.tester.Frame()
	if err := h.tester.Click("Cancel checks"); err != nil {
		t.Fatal(err)
	}
	waitContinueComplete(t, h)
	if !h.tester.HasText("Required: cancelled") || !h.tester.HasText("partial cancellation output") {
		t.Fatalf("cancellation is not visible: %q", h.texts())
	}
	h.app.finishContinueReview(false)
	h.settle(t)
	if calls.has("drive:continue:approved-operation") {
		t.Fatal("a cancelled required check reached DriveOperation")
	}

	h.app.continueTestRunner = continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
		return "retry passed", nil
	})
	if err := h.tester.Click("Retry checks"); err != nil {
		t.Fatal(err)
	}
	waitContinueComplete(t, h)
	if !h.tester.HasText("Required: passed") || !h.tester.HasText("retry passed") {
		t.Fatalf("retry did not replace the cancelled attempt: %q", h.texts())
	}
}

func TestContinueStaleOperationAndCheckpointFailureNeverReachDriver(t *testing.T) {
	t.Run("stale operation", func(t *testing.T) {
		h, session, calls := continueHarness(t, "required", "", continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
			return "changed operation", nil
		}))
		// The runner seam must not be a sandbox: this callback models the same
		// external state mutation a real command can perform.
		h.app.continueTestRunner = continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
			session.updateSource(func(source *repository.ContinueSource) { source.Operation.Fingerprint = "different-operation" })
			return "changed operation", nil
		})
		h.app.confirmContinueOperation()
		waitContinueComplete(t, h)
		if !h.tester.HasText("Operation changed during checks") || !h.tester.HasText("Retry checks") {
			t.Fatalf("stale operation is not explained: %q", h.texts())
		}
		h.app.finishContinueReview(false)
		h.settle(t)
		for _, call := range calls.all() {
			if strings.HasPrefix(call, "drive:continue:") {
				t.Fatalf("stale operation reached DriveOperation: %q", calls.all())
			}
		}
		h.app.continueTestRunner = continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
			return "fresh retry passed", nil
		})
		if err := h.tester.Click("Retry checks"); err != nil {
			t.Fatal(err)
		}
		waitContinueComplete(t, h)
		if h.app.continuePreflight.review.stale || !h.tester.HasText("fresh retry passed") {
			t.Fatalf("stale retry did not use the newly reviewed operation: %+v", h.app.continuePreflight.review)
		}
		if allowed, reason := h.app.continueGate(false); !allowed {
			t.Fatalf("fresh retry stayed gated by the old fingerprint: %s", reason)
		}
	})

	t.Run("same-length diff and checkpoint failure", func(t *testing.T) {
		h, session, calls := continueHarness(t, "required", "", nil)
		session.checkpointErr = errors.New("checkpoint refused")
		before := session.source.WorkingTreeHash
		h.app.continueTestRunner = continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
			session.updateSource(func(source *repository.ContinueSource) {
				source.WorkingTreeHash = strings.Repeat("c", len(before))
			})
			return "same-length canonical diff", nil
		})
		h.app.confirmContinueOperation()
		waitContinueComplete(t, h)
		if !h.tester.HasText("same-length canonical diff") || !h.tester.HasText("Checkpoint failed") {
			t.Fatalf("changed source/checkpoint failure is not visible: %q", h.texts())
		}
		h.app.finishContinueReview(false)
		h.settle(t)
		for _, call := range calls.all() {
			if strings.HasPrefix(call, "drive:continue:") {
				t.Fatalf("failed mandatory checkpoint reached DriveOperation: %q", calls.all())
			}
		}
	})
}

func TestContinueChangedSourceGetsCheckpointThenFinalSourceRecheck(t *testing.T) {
	h, session, calls := continueHarness(t, "required", "", nil)
	h.app.continueTestRunner = continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
		session.updateSource(func(source *repository.ContinueSource) { source.WorkingTreeHash = strings.Repeat("c", 64) })
		return "created a file", nil
	})
	h.app.confirmContinueOperation()
	waitContinueComplete(t, h)
	if !calls.has("checkpoint:Before continuing rebase") {
		t.Fatalf("test-made changes did not get a checkpoint: %q", calls.all())
	}
	session.updateSource(func(source *repository.ContinueSource) { source.WorkingTreeHash = strings.Repeat("d", 64) })
	h.app.finishContinueReview(false)
	h.settle(t)
	for _, call := range calls.all() {
		if strings.HasPrefix(call, "drive:continue:") {
			t.Fatalf("a same-length post-checkpoint edit reached DriveOperation: %q", calls.all())
		}
	}
}

func TestContinueCloseGenerationAndShutdownCancelWork(t *testing.T) {
	for _, test := range []string{"close", "generation", "shutdown"} {
		t.Run(test, func(t *testing.T) {
			started := make(chan struct{})
			var once sync.Once
			h, _, calls := continueHarness(t, "required", "", continueTestRunnerFunc(func(ctx context.Context, _, _ string) (string, error) {
				once.Do(func() { close(started) })
				<-ctx.Done()
				return "late output", ctx.Err()
			}))
			h.app.confirmContinueOperation()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("check did not start")
			}
			h.waitFor(t, "the running review", func() bool {
				return h.app.continuePreflight.review != nil && h.app.continuePreflight.review.running
			})
			switch test {
			case "close":
				h.app.closeContinueReview()
			case "generation":
				h.app.generation++
				h.app.continuePreflight.cancel()
			case "shutdown":
				h.app.shutdown()
			}
			if test != "shutdown" {
				h.settle(t)
			} else {
				h.tester.Frame()
			}
			for _, call := range calls.all() {
				if strings.HasPrefix(call, "drive:continue:") {
					t.Fatalf("%s allowed late work to reach DriveOperation: %q", test, calls.all())
				}
			}
			if test != "shutdown" {
				h.app.closeContinueReview()
			}
		})
	}
}

func TestContinueChecksAreExplicitlyDisabledForSSH(t *testing.T) {
	h, _, calls := continueHarness(t, "required", "", continueTestRunnerFunc(func(context.Context, string, string) (string, error) {
		t.Fatal("trusted local command ran for an SSH workspace")
		return "", nil
	}))
	h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/work/project"}
	h.app.confirmContinueOperation()
	h.tester.Frame()
	if h.app.continuePreflight.review != nil {
		t.Fatal("SSH opened a local trusted-command workflow")
	}
	if !h.tester.HasText("Continue checks run locally") {
		t.Fatalf("SSH refusal has no local-command reason: %q", h.texts())
	}
	for _, call := range calls.all() {
		if strings.HasPrefix(call, "drive:continue:") {
			t.Fatalf("SSH Continue bypassed preflight: %q", calls.all())
		}
	}
}
