package transfer

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testTimeout = 10 * time.Second

func resultWithin(t *testing.T, manager *Manager, id ID) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	result, err := manager.Result(ctx, id)
	if err != nil {
		t.Fatalf("Result(%d): %v", id, err)
	}
	return result
}

func receiveWithin[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for event")
		panic("unreachable")
	}
}

func closeWithin(t *testing.T, manager *Manager) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = manager.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("Close did not finish")
	}
}

func waitWithin(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		runtime.Gosched()
	}
}

func TestFIFOAndSpecificationCopy(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	started := make(chan string, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	var order []string
	operation := func(ctx context.Context, request OperationRequest) error {
		mu.Lock()
		order = append(order, request.Item.Source)
		mu.Unlock()
		if request.Item.Source == "first" {
			started <- request.Item.Source
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if request.LocalPath != "/local" || request.RemotePath != "/remote" {
			t.Errorf("paths not propagated: %+v", request)
		}
		return nil
	}
	newBatch := func(source string) Batch {
		return Batch{Items: []Item{{Source: source, Size: 1}}, LocalPath: "/local", RemotePath: "/remote", Operation: operation}
	}

	first := newBatch("first")
	firstID, err := manager.Enqueue(first)
	if err != nil {
		t.Fatal(err)
	}
	if got := receiveWithin(t, started); got != "first" {
		t.Fatalf("started %q", got)
	}
	first.Items[0].Source = "caller mutation"
	secondID, _ := manager.Enqueue(newBatch("second"))
	thirdID, _ := manager.Enqueue(newBatch("third"))

	// Observe both queued entries directly rather than depending on scheduler
	// timing or a short sleep to prove the first batch is the only active one.
	deadline := time.Now().Add(testTimeout)
	for {
		snapshot := manager.Snapshot()
		if len(snapshot.Jobs) == 3 && snapshot.Jobs[0].Status == StatusRunning &&
			snapshot.Jobs[1].Status == StatusQueued && snapshot.Jobs[2].Status == StatusQueued {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("batches did not retain FIFO queue state: %+v", snapshot.Jobs)
		}
		runtime.Gosched()
	}
	close(release)

	for _, id := range []ID{firstID, secondID, thirdID} {
		if result := resultWithin(t, manager, id); result.Status != StatusSucceeded {
			t.Fatalf("job %d: %+v", id, result)
		}
	}
	mu.Lock()
	gotOrder := append([]string(nil), order...)
	mu.Unlock()
	if want := []string{"first", "second", "third"}; !reflect.DeepEqual(gotOrder, want) {
		t.Fatalf("FIFO order = %v, want %v", gotOrder, want)
	}

	snapshot := manager.Snapshot()
	if snapshot.Jobs[0].TotalBytes != 1 || snapshot.Jobs[0].Status != StatusSucceeded {
		t.Fatalf("source mutation changed enqueued job: %+v", snapshot.Jobs[0])
	}
}

func TestQueuedCancelCompletesOnce(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	started := make(chan struct{})
	release := make(chan struct{})
	var secondRuns atomic.Int32
	var completions atomic.Int32
	var cleanups atomic.Int32
	completed := make(chan Result, 1)

	firstID, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "active"}},
		Operation: func(ctx context.Context, _ OperationRequest) error {
			close(started)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	<-started
	secondID, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "queued"}},
		Operation: func(context.Context, OperationRequest) error {
			secondRuns.Add(1)
			return nil
		},
		Cleanup: func() error {
			cleanups.Add(1)
			return nil
		},
		OnComplete: func(result Result) {
			completions.Add(1)
			completed <- result
		},
	})

	if !manager.Cancel(secondID) {
		t.Fatal("queued Cancel was not accepted")
	}
	if manager.Cancel(secondID) || manager.Cancel(ID(9999)) {
		t.Fatal("terminal or unknown Cancel succeeded")
	}
	result := receiveWithin(t, completed)
	if result.Status != StatusCancelled || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("unexpected cancellation result: %+v", result)
	}
	if secondRuns.Load() != 0 || completions.Load() != 1 || cleanups.Load() != 1 {
		t.Fatalf("runs=%d completions=%d cleanups=%d", secondRuns.Load(), completions.Load(), cleanups.Load())
	}
	close(release)
	if result := resultWithin(t, manager, firstID); result.Status != StatusSucceeded {
		t.Fatalf("active FIFO job did not continue: %+v", result)
	}
}

func TestActiveCancelWaitsForOperationAndRetainsPartialBytes(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	started := make(chan struct{})
	operationExited := make(chan struct{})
	var cleanupRan atomic.Bool
	id, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "one", Size: 12}, {Source: "unreached", Size: 5}},
		Operation: func(ctx context.Context, request OperationRequest) error {
			request.ReportProgress(3)
			request.ReportProgress(99) // bounded to Item.Size.
			request.ReportProgress(4)
			close(started)
			<-ctx.Done()
			close(operationExited)
			return ctx.Err()
		},
		Cleanup: func() error {
			if cleanupRan.Swap(true) {
				t.Error("cleanup ran twice")
			}
			return nil
		},
	})
	<-started
	if !manager.Cancel(id) {
		t.Fatal("active Cancel was not accepted")
	}
	result := resultWithin(t, manager, id)
	<-operationExited
	if !cleanupRan.Load() {
		t.Fatal("result delivered before cleanup")
	}
	if result.Status != StatusCancelled || result.CompletedBytes != 12 || result.FailedItems != 0 {
		t.Fatalf("unexpected active cancellation result: %+v", result)
	}
	if result.CompletedItems != 0 || result.SkippedItems != 0 || result.TotalItems != 2 || result.TotalBytes != 17 {
		t.Fatalf("inaccurate cancellation counters: %+v", result)
	}
}

func TestCollisionApplyToAllSkipAndDuplicateResponses(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	prompts := make(chan CollisionPrompt, 3)
	var callbacks atomic.Int32
	id, _ := manager.Enqueue(Batch{
		Items:      []Item{{Source: "a", Size: 2}, {Source: "b", Size: 3}, {Source: "c", Size: 4}},
		LocalPath:  "/tmp/local",
		RemotePath: "/srv/remote",
		OnCollision: func(prompt CollisionPrompt) {
			callbacks.Add(1)
			prompts <- prompt
		},
		Operation: func(_ context.Context, request OperationRequest) error {
			request.ReportProgress(request.Item.Size)
			_, err := request.ResolveCollision(context.Background())
			if err != nil {
				return err
			}
			return ErrSkip
		},
	})

	prompt := receiveWithin(t, prompts)
	if prompt.Item.Source != "a" || prompt.LocalPath != "/tmp/local" || prompt.RemotePath != "/srv/remote" || prompt.ID != id {
		t.Fatalf("bad collision prompt: %+v", prompt)
	}
	prompt.Respond(CollisionChoice{Decision: CollisionSkip, ApplyToAll: true})
	// Concurrent duplicate decisions are harmless and cannot overwrite Skip.
	var responders sync.WaitGroup
	for index := 0; index < 20; index++ {
		responders.Add(1)
		go func() {
			defer responders.Done()
			prompt.Respond(CollisionChoice{Decision: CollisionOverwrite})
		}()
	}
	responders.Wait()

	result := resultWithin(t, manager, id)
	if result.Status != StatusSucceeded || result.SkippedItems != 3 || result.CompletedBytes != 0 {
		t.Fatalf("apply-all skip result: %+v", result)
	}
	if callbacks.Load() != 1 {
		t.Fatalf("collision callbacks = %d, want 1", callbacks.Load())
	}
	select {
	case extra := <-prompts:
		t.Fatalf("unexpected extra prompt: %+v", extra)
	default:
	}
}

func TestCollisionApplyToAllOverwrite(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	var callbacks atomic.Int32
	var operations atomic.Int32
	id, err := manager.Enqueue(Batch{
		Items: []Item{{Source: "a", Size: 1}, {Source: "b", Size: 2}},
		OnCollision: func(prompt CollisionPrompt) {
			callbacks.Add(1)
			// Return before responding to prove the UI may answer later from a
			// different goroutine without holding up its callback.
			go prompt.Respond(CollisionChoice{Decision: CollisionOverwrite, ApplyToAll: true})
		},
		Operation: func(_ context.Context, request OperationRequest) error {
			operations.Add(1)
			choice, err := request.ResolveCollision(context.Background())
			if err != nil {
				return err
			}
			if choice.Decision != CollisionOverwrite || !choice.ApplyToAll {
				return fmt.Errorf("unexpected collision choice: %+v", choice)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := resultWithin(t, manager, id)
	if result.Status != StatusSucceeded || result.CompletedItems != 2 || result.CompletedBytes != 3 {
		t.Fatalf("apply-all overwrite result: %+v", result)
	}
	if callbacks.Load() != 1 || operations.Load() != 2 {
		t.Fatalf("callbacks=%d operations=%d, want 1 and 2", callbacks.Load(), operations.Load())
	}
}

func TestCollisionCancelAndMissingHandler(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	cancelID, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "first"}, {Source: "unreached"}},
		OnCollision: func(prompt CollisionPrompt) {
			prompt.Respond(CollisionChoice{Decision: CollisionCancel, ApplyToAll: true})
		},
		Operation: func(_ context.Context, request OperationRequest) error {
			_, err := request.ResolveCollision(context.Background())
			return err
		},
	})
	cancelled := resultWithin(t, manager, cancelID)
	if cancelled.Status != StatusCancelled || !errors.Is(cancelled.Err, ErrCollisionCancelled) || cancelled.FailedItems != 0 {
		t.Fatalf("collision cancellation: %+v", cancelled)
	}

	missingID, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "first"}},
		Operation: func(_ context.Context, request OperationRequest) error {
			_, err := request.ResolveCollision(context.Background())
			return err
		},
	})
	failed := resultWithin(t, manager, missingID)
	if failed.Status != StatusFailed || len(failed.Errors) != 1 || !errors.Is(failed.Errors[0].Err, ErrNoCollisionHandler) {
		t.Fatalf("missing collision handler: %+v", failed)
	}
}

func TestPendingCollisionCanBeCancelledAndRespondedLate(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	promptChannel := make(chan CollisionPrompt, 1)
	id, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "blocked"}},
		OnCollision: func(prompt CollisionPrompt) {
			promptChannel <- prompt
		},
		Operation: func(ctx context.Context, request OperationRequest) error {
			_, err := request.ResolveCollision(ctx)
			return err
		},
	})
	prompt := receiveWithin(t, promptChannel)
	manager.Cancel(id)
	if result := resultWithin(t, manager, id); result.Status != StatusCancelled {
		t.Fatalf("pending collision did not cancel: %+v", result)
	}
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			prompt.Respond(CollisionChoice{Decision: CollisionOverwrite})
		}()
	}
	wait.Wait()
}

func TestPartialResultProgressAndImmutableResults(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	failure := errors.New("disk unavailable")
	var completions atomic.Int32
	id, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "good-1", Size: 10}, {Source: "bad", Size: 8}, {Source: "good-2", Size: 5}},
		Operation: func(_ context.Context, request OperationRequest) error {
			if request.Item.Source == "bad" {
				request.ReportProgress(4)
				return failure
			}
			return nil
		},
		OnComplete: func(Result) { completions.Add(1) },
	})
	result := resultWithin(t, manager, id)
	if result.Status != StatusPartial || result.CompletedItems != 2 || result.FailedItems != 1 || result.SkippedItems != 0 {
		t.Fatalf("partial counters: %+v", result)
	}
	if result.TotalBytes != 23 || result.CompletedBytes != 19 || len(result.Errors) != 1 || !errors.Is(result.Errors[0].Err, failure) {
		t.Fatalf("partial bytes/errors: %+v", result)
	}
	if result.Errors[0].ItemIndex != 1 || result.Errors[0].Item.Source != "bad" {
		t.Fatalf("wrong item error: %+v", result.Errors[0])
	}
	if completions.Load() != 1 {
		t.Fatalf("completion callbacks = %d", completions.Load())
	}

	result.Errors[0].Item.Source = "mutated"
	result.Errors = append(result.Errors, ItemError{})
	fresh := resultWithin(t, manager, id)
	if len(fresh.Errors) != 1 || fresh.Errors[0].Item.Source != "bad" {
		t.Fatalf("Result exposed internal error slices: %+v", fresh.Errors)
	}
}

func TestOperationAndCallbackPanicsDoNotStopManager(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	var completions atomic.Int32
	var cleanups atomic.Int32
	panicID, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "panic"}, {Source: "after-panic", Size: 2}},
		Operation: func(_ context.Context, request OperationRequest) error {
			if request.Item.Source == "panic" {
				panic("boom")
			}
			return nil
		},
		Cleanup: func() error {
			cleanups.Add(1)
			return nil
		},
		OnComplete: func(Result) {
			completions.Add(1)
			panic("UI callback panic")
		},
	})
	panicResult := resultWithin(t, manager, panicID)
	if panicResult.Status != StatusPartial || panicResult.CompletedItems != 1 || panicResult.FailedItems != 1 {
		t.Fatalf("panic recovery result: %+v", panicResult)
	}
	if len(panicResult.Errors) != 1 || !strings.Contains(panicResult.Errors[0].Err.Error(), "operation panic: boom") {
		t.Fatalf("missing panic error: %+v", panicResult.Errors)
	}
	// Completion runs after Result is released; wait for it before asserting.
	waitWithin(t, "panic batch completion callback", func() bool { return completions.Load() == 1 })
	if cleanups.Load() != 1 {
		t.Fatalf("cleanup count = %d", cleanups.Load())
	}

	nextID, _ := manager.Enqueue(Batch{
		Items:     []Item{{Source: "next"}},
		Operation: func(context.Context, OperationRequest) error { return nil },
	})
	if next := resultWithin(t, manager, nextID); next.Status != StatusSucceeded {
		t.Fatalf("worker stopped after callback panic: %+v", next)
	}
}

func TestRetryRetainsSourceSpecAndResetsFailure(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	var attempts atomic.Int32
	batch := Batch{
		Items:      []Item{{Source: "source", Destination: "destination", Size: 7}},
		LocalPath:  "/local",
		RemotePath: "/remote",
		Operation: func(_ context.Context, request OperationRequest) error {
			if request.LocalPath != "/local" || request.RemotePath != "/remote" || request.Item.Destination != "destination" {
				t.Errorf("retry lost source spec: %+v", request)
			}
			if attempts.Add(1) == 1 {
				return errors.New("temporary")
			}
			return nil
		},
	}
	originalID, _ := manager.Enqueue(batch)
	if result := resultWithin(t, manager, originalID); result.Status != StatusFailed {
		t.Fatalf("first attempt: %+v", result)
	}
	retryID, err := manager.Retry(originalID)
	if err != nil {
		t.Fatal(err)
	}
	if retryID == originalID {
		t.Fatal("Retry reused ID")
	}
	result := resultWithin(t, manager, retryID)
	if result.RetryOf != originalID || result.Status != StatusSucceeded || result.CompletedBytes != 7 || len(result.Errors) != 0 {
		t.Fatalf("retry result: %+v", result)
	}
	if _, err := manager.Retry(retryID); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("successful Retry error = %v", err)
	}
}

func TestCancelledRetryStartsWithFreshCollisionPolicy(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	started := make(chan struct{})
	release := make(chan struct{})
	var attempts atomic.Int32
	var completions [2]atomic.Int32
	var cleanups [2]atomic.Int32
	id, err := manager.Enqueue(Batch{
		Items: []Item{{Source: "blocked"}},
		Operation: func(ctx context.Context, _ OperationRequest) error {
			if attempts.Add(1) == 1 {
				close(started)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
		OnComplete: func(Result) {
			if attempts.Load() == 1 {
				completions[0].Add(1)
			} else {
				completions[1].Add(1)
			}
		},
		Cleanup: func() error {
			if attempts.Load() == 1 {
				cleanups[0].Add(1)
			} else {
				cleanups[1].Add(1)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	receiveWithin(t, started)
	if !manager.Cancel(id) {
		t.Fatal("active cancellation was not accepted")
	}
	cancelled := resultWithin(t, manager, id) // cleanup has completed at this point.
	if cancelled.Status != StatusCancelled {
		t.Fatalf("original attempt: %+v", cancelled)
	}
	close(release)
	retryID, err := manager.Retry(id)
	if err != nil {
		t.Fatal(err)
	}
	retried := resultWithin(t, manager, retryID)
	if retryID == id || retried.RetryOf != id || retried.Status != StatusSucceeded || retried.CompletedItems != 1 {
		t.Fatalf("cancelled retry: original=%d result=%+v", id, retried)
	}
	waitWithin(t, "both retry completion callbacks", func() bool {
		return completions[0].Load() == 1 && completions[1].Load() == 1
	})
	if cleanups[0].Load() != 1 || cleanups[1].Load() != 1 {
		t.Fatalf("cleanup counts: original=%d retry=%d", cleanups[0].Load(), cleanups[1].Load())
	}
}

func TestRetryCollisionApplyToAllDoesNotLeakAcrossExecutions(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	var callbacks atomic.Int32
	var calls atomic.Int32
	batch := Batch{
		Items: []Item{{Source: "a"}, {Source: "b"}},
		OnCollision: func(prompt CollisionPrompt) {
			callbacks.Add(1)
			if prompt.Item.Source != "a" {
				t.Errorf("ApplyToAll leaked within or across executions at item %q", prompt.Item.Source)
			}
			prompt.Respond(CollisionChoice{Decision: CollisionOverwrite, ApplyToAll: true})
		},
		Operation: func(_ context.Context, request OperationRequest) error {
			choice, err := request.ResolveCollision(context.Background())
			if err != nil {
				return err
			}
			if choice.Decision != CollisionOverwrite || !choice.ApplyToAll {
				return fmt.Errorf("wrong decision: %+v", choice)
			}
			if calls.Add(1) == 1 {
				return errors.New("fail the first execution")
			}
			return nil
		},
	}
	originalID, err := manager.Enqueue(batch)
	if err != nil {
		t.Fatal(err)
	}
	if result := resultWithin(t, manager, originalID); result.Status != StatusPartial {
		t.Fatalf("first execution: %+v", result)
	}
	if callbacks.Load() != 1 {
		t.Fatalf("first execution prompted %d times, want one", callbacks.Load())
	}

	retryID, err := manager.Retry(originalID)
	if err != nil {
		t.Fatal(err)
	}
	if result := resultWithin(t, manager, retryID); result.Status != StatusSucceeded {
		t.Fatalf("retry execution: %+v", result)
	}
	if callbacks.Load() != 2 || calls.Load() != 4 {
		t.Fatalf("callbacks=%d calls=%d, want one prompt and two calls per execution", callbacks.Load(), calls.Load())
	}
}

func TestCleanupErrorAffectsTerminalStatusExactlyOnce(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	var cleanups atomic.Int32
	var completions atomic.Int32
	id, _ := manager.Enqueue(Batch{
		Items:     []Item{{Source: "done", Size: 1}},
		Operation: func(context.Context, OperationRequest) error { return nil },
		Cleanup: func() error {
			cleanups.Add(1)
			return errors.New("cannot remove temporary output")
		},
		OnComplete: func(Result) { completions.Add(1) },
	})
	result := resultWithin(t, manager, id)
	if result.Status != StatusPartial || result.CompletedItems != 1 || result.FailedItems != 0 || len(result.Errors) != 1 || result.Errors[0].ItemIndex != -1 {
		t.Fatalf("cleanup failure result: %+v", result)
	}
	waitWithin(t, "cleanup completion callback", func() bool { return completions.Load() == 1 })
	if cleanups.Load() != 1 {
		t.Fatalf("cleanup count = %d, want 1", cleanups.Load())
	}
}

func TestCleanupPanicStillCompletesExactlyOnce(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	var cleanups atomic.Int32
	completed := make(chan Result, 1)
	id, _ := manager.Enqueue(Batch{
		Items:     []Item{{Source: "done"}},
		Operation: func(context.Context, OperationRequest) error { return nil },
		Cleanup: func() error {
			cleanups.Add(1)
			panic("cleanup exploded")
		},
		OnComplete: func(result Result) { completed <- result },
	})
	result := resultWithin(t, manager, id)
	if result.Status != StatusPartial || len(result.Errors) != 1 ||
		!strings.Contains(result.Errors[0].Err.Error(), "cleanup panic: cleanup exploded") {
		t.Fatalf("cleanup panic result: %+v", result)
	}
	callbackResult := receiveWithin(t, completed)
	if callbackResult.ID != id || callbackResult.Status != StatusPartial {
		t.Fatalf("cleanup panic completion: %+v", callbackResult)
	}
	if cleanups.Load() != 1 {
		t.Fatalf("cleanup count = %d, want 1", cleanups.Load())
	}
}

func TestCloseCancelsEverythingAndRejectsNewWork(t *testing.T) {
	manager := NewManager()

	started := make(chan struct{})
	var operationCount atomic.Int32
	var completions atomic.Int32
	var cleanups atomic.Int32
	newBatch := func() Batch {
		return Batch{
			Items: []Item{{Source: "item"}},
			Operation: func(ctx context.Context, _ OperationRequest) error {
				operationCount.Add(1)
				select {
				case <-started:
				default:
					close(started)
				}
				<-ctx.Done()
				return ctx.Err()
			},
			OnComplete: func(Result) { completions.Add(1) },
			Cleanup:    func() error { cleanups.Add(1); return nil },
		}
	}
	ids := make([]ID, 4)
	for index := range ids {
		ids[index], _ = manager.Enqueue(newBatch())
	}
	<-started

	var closes sync.WaitGroup
	for index := 0; index < 5; index++ {
		closes.Add(1)
		go func() {
			defer closes.Done()
			_ = manager.Close()
		}()
	}
	closeWait := make(chan struct{})
	go func() { closes.Wait(); close(closeWait) }()
	select {
	case <-closeWait:
	case <-time.After(testTimeout):
		t.Fatal("concurrent Close calls did not finish")
	}
	if operationCount.Load() != 1 || completions.Load() != int32(len(ids)) || cleanups.Load() != int32(len(ids)) {
		t.Fatalf("operations=%d completions=%d cleanups=%d", operationCount.Load(), completions.Load(), cleanups.Load())
	}
	for _, id := range ids {
		if result := resultWithin(t, manager, id); result.Status != StatusCancelled {
			t.Fatalf("job %d after Close: %+v", id, result)
		}
	}
	if _, err := manager.Enqueue(newBatch()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Enqueue after Close = %v", err)
	}
	if !manager.Snapshot().Closed {
		t.Fatal("final snapshot not marked closed")
	}
}

func TestSubscriptionsAreSerialDetachedAndFinal(t *testing.T) {
	manager := NewManager()

	firstUpdates := make(chan Snapshot, 32)
	secondUpdates := make(chan Snapshot, 32)
	unsubscribeFirst := manager.Subscribe(func(snapshot Snapshot) {
		if len(snapshot.Jobs) > 0 {
			snapshot.Jobs[0].Status = "listener mutation"
			if len(snapshot.Jobs[0].Errors) > 0 {
				snapshot.Jobs[0].Errors[0].Item.Source = "mutated error source"
				snapshot.Jobs[0].Errors = append(snapshot.Jobs[0].Errors, ItemError{})
			}
		}
		firstUpdates <- snapshot
	})
	unsubscribeSecond := manager.Subscribe(func(snapshot Snapshot) {
		secondUpdates <- snapshot
	})
	id, _ := manager.Enqueue(Batch{
		Items: []Item{{Source: "failure"}},
		Operation: func(context.Context, OperationRequest) error {
			return errors.New("expected")
		},
	})
	result := resultWithin(t, manager, id)
	result.Errors[0].Item.Source = "caller mutation"
	result.Errors = append(result.Errors, ItemError{})
	unsubscribeFirst()
	unsubscribeFirst()
	manager.Subscribe(func(Snapshot) { panic("subscriber panic") })
	closeWithin(t, manager)

	last := receiveWithin(t, secondUpdates)
	for {
		select {
		case last = <-secondUpdates:
			continue
		default:
		}
		break
	}
	if !last.Closed || len(last.Jobs) != 1 || last.Jobs[0].Status != StatusFailed {
		t.Fatalf("bad final detached snapshot: %+v", last)
	}
	if len(last.Jobs[0].Errors) != 1 || last.Jobs[0].Errors[0].Item.Source != "failure" {
		t.Fatalf("snapshot errors were aliased or mutated: %+v", last.Jobs[0].Errors)
	}
	fresh := manager.Snapshot()
	if len(fresh.Jobs[0].Errors) != 1 || fresh.Jobs[0].Errors[0].Item.Source != "failure" {
		t.Fatalf("manager result was changed through a snapshot: %+v", fresh.Jobs[0].Errors)
	}
	unsubscribeSecond()
}

func TestValidationAndResultContext(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	invalid := []Batch{
		{},
		{Items: []Item{{}}, Operation: nil},
		{Items: []Item{{Size: -1}}, Operation: func(context.Context, OperationRequest) error { return nil }},
		{Items: []Item{{Size: 1<<63 - 1}, {Size: 1}}, Operation: func(context.Context, OperationRequest) error { return nil }},
	}
	for _, batch := range invalid {
		if _, err := manager.Enqueue(batch); !errors.Is(err, ErrInvalidBatch) {
			t.Fatalf("validation error = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Result(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown Result = %v", err)
	}
	id, _ := manager.Enqueue(Batch{Items: []Item{{}}, Operation: func(context.Context, OperationRequest) error { return nil }})
	_ = resultWithin(t, manager, id)
	if _, err := manager.Retry(id); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("successful Retry = %v", err)
	}
	if _, err := manager.Retry(ID(999)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown Retry = %v", err)
	}
}

func TestResultHonorsContextWhileBatchKeepsRunning(t *testing.T) {
	manager := NewManager()
	defer closeWithin(t, manager)

	started := make(chan struct{})
	id, err := manager.Enqueue(Batch{
		Items: []Item{{Source: "blocked"}},
		Operation: func(ctx context.Context, _ OperationRequest) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	receiveWithin(t, started)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Result(ctx, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting Result error = %v, want context.Canceled", err)
	}
	if status := manager.Snapshot().Jobs[0].Status; status != StatusRunning {
		t.Fatalf("waiting Result cancelled the operation indirectly: %s", status)
	}
	manager.Cancel(id)
	if result := resultWithin(t, manager, id); result.Status != StatusCancelled {
		t.Fatalf("explicit cancellation after timed wait: %+v", result)
	}
}

func TestShuffledCancellationStress(t *testing.T) {
	for iteration := 0; iteration < 30; iteration++ {
		t.Run(fmt.Sprintf("iteration-%02d", iteration), func(t *testing.T) {
			manager := NewManager()
			const batchCount = 12
			var completions [batchCount]atomic.Int32
			var cleanups [batchCount]atomic.Int32
			ids := make([]ID, batchCount)
			for index := 0; index < batchCount; index++ {
				index := index
				id, err := manager.Enqueue(Batch{
					Items: []Item{{Source: "a", Size: 2}, {Source: "b", Size: 3}},
					Operation: func(ctx context.Context, request OperationRequest) error {
						if index%7 == 0 && request.Item.Source == "a" {
							panic("stress panic")
						}
						if index%5 == 0 && request.Item.Source == "b" {
							return errors.New("stress failure")
						}
						request.ReportProgress(1)
						select {
						case <-time.After(time.Duration(index%3) * time.Millisecond):
							return nil
						case <-ctx.Done():
							return ctx.Err()
						}
					},
					OnComplete: func(Result) { completions[index].Add(1) },
					Cleanup:    func() error { cleanups[index].Add(1); return nil },
				})
				if err != nil {
					t.Fatal(err)
				}
				ids[index] = id
			}

			permutation := rand.New(rand.NewSource(int64(iteration))).Perm(batchCount)
			start := make(chan struct{})
			var wait sync.WaitGroup
			for _, index := range permutation {
				index := index
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					runtime.Gosched()
					manager.Cancel(ids[index])
				}()
			}
			wait.Add(2)
			go func() {
				defer wait.Done()
				<-start
				manager.CancelAll()
			}()
			go func() {
				defer wait.Done()
				<-start
				_ = manager.Close()
			}()
			close(start)
			done := make(chan struct{})
			go func() { wait.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(testTimeout):
				t.Fatal("cancellation race deadlocked")
			}
			closeWithin(t, manager)

			for index, id := range ids {
				result := resultWithin(t, manager, id)
				if !result.Status.Terminal() {
					t.Fatalf("job %d not terminal: %+v", id, result)
				}
				if got := completions[index].Load(); got != 1 {
					t.Fatalf("job %d completion count = %d", id, got)
				}
				if got := cleanups[index].Load(); got != 1 {
					t.Fatalf("job %d cleanup count = %d", id, got)
				}
			}
		})
	}
}
