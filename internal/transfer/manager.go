package transfer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Manager owns one sequential FIFO worker and a snapshot delivery goroutine.
// A Manager must be closed when no longer needed.
type Manager struct {
	mu          sync.Mutex
	jobs        map[ID]*job
	order       []ID
	queue       []*job
	nextID      ID
	closed      bool
	subscribers map[uint64]func(Snapshot)
	nextSubID   uint64

	wake         chan struct{}
	workerDone   chan struct{}
	notify       chan struct{}
	notifyStop   chan struct{}
	notifyDone   chan struct{}
	closeDone    chan struct{}
	closeOnce    sync.Once
	notifyCloser sync.Once
}

type job struct {
	id      ID
	retryOf ID
	batch   Batch
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	settled chan struct{}

	finishClaim atomic.Bool
	result      Result // guarded by Manager.mu
	activeToken uint64 // guarded by Manager.mu
	nextToken   uint64 // guarded by Manager.mu
	itemBytes   int64  // guarded by Manager.mu
}

// NewManager starts an idle transfer manager.
func NewManager() *Manager {
	m := &Manager{
		jobs:        make(map[ID]*job),
		subscribers: make(map[uint64]func(Snapshot)),
		wake:        make(chan struct{}, 1),
		workerDone:  make(chan struct{}),
		notify:      make(chan struct{}, 1),
		notifyStop:  make(chan struct{}),
		notifyDone:  make(chan struct{}),
		closeDone:   make(chan struct{}),
	}
	go m.worker()
	go m.notifier()
	return m
}

// Enqueue appends a batch to the FIFO queue. The batch specification is copied.
func (m *Manager) Enqueue(batch Batch) (ID, error) {
	return m.enqueue(batch, 0)
}

func (m *Manager) enqueue(batch Batch, retryOf ID) (ID, error) {
	if err := validateBatch(batch); err != nil {
		return 0, err
	}
	batch.Items = append([]Item(nil), batch.Items...)

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return 0, ErrClosed
	}
	m.nextID++
	id := m.nextID
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{
		id:      id,
		retryOf: retryOf,
		batch:   batch,
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		settled: make(chan struct{}),
		result: Result{
			ID:         id,
			RetryOf:    retryOf,
			Status:     StatusQueued,
			TotalItems: len(batch.Items),
			TotalBytes: totalBytes(batch.Items),
		},
	}
	m.jobs[id] = j
	m.order = append(m.order, id)
	m.queue = append(m.queue, j)
	m.mu.Unlock()

	m.publish()
	m.signal(m.wake)
	return id, nil
}

// Cancel cancels a queued or running batch. Queued cancellation completes its
// cleanup and callback before Cancel returns. Active cancellation only signals
// its context; use Result or CancelAll when completion must be awaited.
func (m *Manager) Cancel(id ID) bool {
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	if j == nil {
		return false
	}
	return m.cancelJob(j)
}

func (m *Manager) cancelJob(j *job) bool {
	claimed := false
	accepted := false
	m.mu.Lock()
	if !j.result.Status.Terminal() && !j.finishClaim.Load() {
		switch j.result.Status {
		case StatusQueued:
			// Claim while holding mu so the worker cannot start this job between
			// the state check and the terminal transition.
			if j.finishClaim.CompareAndSwap(false, true) {
				claimed = true
				accepted = true
				j.result.Status = StatusCancelled
				j.result.Err = context.Canceled
			}
		case StatusRunning:
			accepted = true
			j.cancel()
		}
	}
	m.mu.Unlock()

	if claimed {
		j.cancel()
		m.finishClaimed(j, true, context.Canceled)
	}
	return accepted
}

// CancelAll cancels every unfinished batch present at the call and waits for
// their operations, cleanup hooks, and terminal results. Batches enqueued
// concurrently after this snapshot are not implicitly cancelled.
func (m *Manager) CancelAll() {
	m.mu.Lock()
	jobs := make([]*job, 0, len(m.order))
	for _, id := range m.order {
		j := m.jobs[id]
		if !j.result.Status.Terminal() {
			jobs = append(jobs, j)
		}
	}
	m.mu.Unlock()

	// Signal every active operation before waiting for any queued cleanup.
	for _, j := range jobs {
		m.cancelJob(j)
	}
	for _, j := range jobs {
		<-j.done
	}
}

// Retry enqueues a failed, partial, or cancelled batch as a new ID. The full
// source specification and callbacks are retained, while progress, errors,
// cancellation, and collision ApplyToAll state start fresh.
func (m *Manager) Retry(id ID) (ID, error) {
	m.mu.Lock()
	j := m.jobs[id]
	if j == nil {
		m.mu.Unlock()
		return 0, ErrNotFound
	}
	status := j.result.Status
	retryable := status == StatusFailed || status == StatusPartial || status == StatusCancelled
	if !retryable {
		m.mu.Unlock()
		return 0, ErrNotRetryable
	}
	batch := j.batch
	batch.Items = append([]Item(nil), batch.Items...)
	m.mu.Unlock()
	return m.enqueue(batch, id)
}

// Result waits for a terminal result or ctx cancellation.
func (m *Manager) Result(ctx context.Context, id ID) (Result, error) {
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	if j == nil {
		return Result{}, ErrNotFound
	}
	select {
	case <-j.done:
		m.mu.Lock()
		result := cloneResult(j.result)
		m.mu.Unlock()
		return result, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// Snapshot returns an immutable-by-copy view of all jobs.
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	snapshot := m.snapshotLocked()
	m.mu.Unlock()
	return snapshot
}

// Subscribe registers a serial snapshot listener. Updates may coalesce. The
// listener runs on the manager's non-UI notification goroutine; it must not
// block indefinitely or call Close. The returned function is idempotent.
// Subscribing after Close returns a no-op unsubscribe function.
func (m *Manager) Subscribe(listener func(Snapshot)) (unsubscribe func()) {
	if listener == nil {
		return func() {}
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return func() {}
	}
	m.nextSubID++
	id := m.nextSubID
	m.subscribers[id] = listener
	m.mu.Unlock()
	m.publish()

	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			delete(m.subscribers, id)
			m.mu.Unlock()
		})
	}
}

// Close rejects new work, cancels all outstanding work, waits for the worker,
// and delivers a final snapshot. It is safe to call concurrently and more than
// once. An Operation must observe its context for Close to finish.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		jobs := make([]*job, 0, len(m.order))
		for _, id := range m.order {
			j := m.jobs[id]
			if !j.result.Status.Terminal() {
				jobs = append(jobs, j)
			}
		}
		m.mu.Unlock()
		m.publish()

		for _, j := range jobs {
			m.cancelJob(j)
		}
		m.signal(m.wake)
		<-m.workerDone
		// A concurrent Cancel may already have marked a queued job terminal
		// while its cleanup or callback is still running.
		for _, id := range m.orderSnapshot() {
			j := m.jobByID(id)
			<-j.settled
		}

		m.notifyCloser.Do(func() { close(m.notifyStop) })
		<-m.notifyDone
		close(m.closeDone)
	})
	<-m.closeDone
	return nil
}

func (m *Manager) orderSnapshot() []ID {
	m.mu.Lock()
	order := append([]ID(nil), m.order...)
	m.mu.Unlock()
	return order
}

func (m *Manager) jobByID(id ID) *job {
	m.mu.Lock()
	j := m.jobs[id]
	m.mu.Unlock()
	return j
}

func totalBytes(items []Item) int64 {
	var total int64
	for _, item := range items {
		total += item.Size
	}
	return total
}

func (m *Manager) worker() {
	defer close(m.workerDone)
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return
		}
		var next *job
		for len(m.queue) > 0 {
			candidate := m.queue[0]
			m.queue = m.queue[1:]
			if candidate.result.Status != StatusQueued || candidate.finishClaim.Load() {
				continue
			}
			candidate.result.Status = StatusRunning
			next = candidate
			break
		}
		m.mu.Unlock()

		if next != nil {
			m.publish()
			cancelled, err := m.runBatch(next)
			m.finish(next, cancelled, err)
			continue
		}
		<-m.wake
	}
}

func (m *Manager) finish(j *job, cancelled bool, runErr error) {
	if !j.finishClaim.CompareAndSwap(false, true) {
		return
	}
	m.finishClaimed(j, cancelled, runErr)
}

func (m *Manager) finishClaimed(j *job, cancelled bool, runErr error) {
	cleanupErr := callCleanup(j.batch.Cleanup)

	m.mu.Lock()
	if cleanupErr != nil {
		j.result.Errors = append(j.result.Errors, ItemError{ItemIndex: -1, Err: cleanupErr})
	}
	if runErr == nil {
		runErr = cleanupErr
	} else if cleanupErr != nil {
		runErr = errors.Join(runErr, cleanupErr)
	}
	j.result.Err = runErr
	switch {
	case cancelled || errors.Is(runErr, context.Canceled) || errors.Is(runErr, ErrCollisionCancelled):
		j.result.Status = StatusCancelled
	case runErr != nil || j.result.FailedItems > 0:
		if j.result.CompletedItems+j.result.SkippedItems > 0 {
			j.result.Status = StatusPartial
		} else {
			j.result.Status = StatusFailed
		}
	default:
		j.result.Status = StatusSucceeded
	}
	result := cloneResult(j.result)
	callback := j.batch.OnComplete
	m.mu.Unlock()
	m.publish()

	j.cancel()
	close(j.done)
	callCompletion(callback, result)
	close(j.settled)
}

func callCleanup(cleanup func() error) (err error) {
	if cleanup == nil {
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("transfer: cleanup panic: %v", recovered)
		}
	}()
	return cleanup()
}

func callCompletion(callback func(Result), result Result) {
	if callback == nil {
		return
	}
	defer func() { _ = recover() }()
	callback(result)
}

func cloneResult(result Result) Result {
	result.Errors = append([]ItemError(nil), result.Errors...)
	return result
}

func (m *Manager) snapshotLocked() Snapshot {
	snapshot := Snapshot{Jobs: make([]Result, 0, len(m.order)), Closed: m.closed}
	for _, id := range m.order {
		snapshot.Jobs = append(snapshot.Jobs, cloneResult(m.jobs[id].result))
	}
	return snapshot
}

func (m *Manager) notifier() {
	defer close(m.notifyDone)
	for {
		select {
		case <-m.notify:
			m.deliverSnapshot()
		case <-m.notifyStop:
			m.deliverSnapshot()
			m.mu.Lock()
			m.subscribers = make(map[uint64]func(Snapshot))
			m.mu.Unlock()
			return
		}
	}
}

func (m *Manager) deliverSnapshot() {
	m.mu.Lock()
	listeners := make([]func(Snapshot), 0, len(m.subscribers))
	for _, listener := range m.subscribers {
		listeners = append(listeners, listener)
	}
	snapshot := m.snapshotLocked()
	m.mu.Unlock()

	for _, listener := range listeners {
		listenerSnapshot := Snapshot{Jobs: make([]Result, len(snapshot.Jobs)), Closed: snapshot.Closed}
		for index, result := range snapshot.Jobs {
			listenerSnapshot.Jobs[index] = cloneResult(result)
		}
		func() {
			defer func() { _ = recover() }()
			listener(listenerSnapshot)
		}()
	}
}

func (m *Manager) publish() {
	m.signal(m.notify)
}

func (m *Manager) signal(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}
