// Package transfer executes independent batch transfer operations sequentially.
// It deliberately knows nothing about SSH or a user interface.
package transfer

import (
	"context"
	"errors"
	"fmt"
)

// ID identifies an enqueued batch. IDs are never reused, including by Retry.
type ID uint64

// Status is the lifecycle state of a batch.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusPartial   Status = "partial"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Terminal reports whether no more operation work remains for the status.
func (s Status) Terminal() bool {
	switch s {
	case StatusSucceeded, StatusPartial, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// Item describes one source in a batch. Size is the expected source size in
// bytes; zero means unknown. Destination is interpreted by Operation.
type Item struct {
	Source      string
	Destination string
	Size        int64
}

// Batch is a self-contained transfer specification. LocalPath and RemotePath
// are passed to every operation without interpretation. Items are copied when
// the batch is enqueued.
//
// Cleanup is run exactly once for every accepted batch, including queued
// cancellation and operation panics. OnComplete is called exactly once after
// cleanup and the terminal snapshot update. Callbacks must not block on Close.
type Batch struct {
	Items       []Item
	LocalPath   string
	RemotePath  string
	Operation   Operation
	OnCollision func(CollisionPrompt)
	OnComplete  func(Result)
	Cleanup     func() error
}

// Operation performs one item. It should observe ctx and return promptly when
// ctx is cancelled. An operation panic is recovered and recorded as an item
// error rather than terminating the manager.
type Operation func(ctx context.Context, request OperationRequest) error

// OperationRequest contains the immutable item specification and worker hooks.
type OperationRequest struct {
	Item       Item
	LocalPath  string
	RemotePath string

	// ReportProgress reports absolute bytes processed for this item, not a
	// delta. Values are monotonic and bounded to Item.Size when Size is nonzero.
	// Calls from worker goroutines created by Operation are safe, but must not
	// outlive the operation.
	ReportProgress func(completedBytes int64)

	// ResolveCollision asks the batch's OnCollision callback for a decision and
	// waits without holding manager locks. Respond may be called from any
	// goroutine. ApplyToAll is remembered only for the current batch execution.
	ResolveCollision func(ctx context.Context) (CollisionChoice, error)
}

// CollisionDecision determines how an operation handles a destination clash.
type CollisionDecision uint8

const (
	CollisionSkip CollisionDecision = iota + 1
	CollisionOverwrite
	CollisionCancel
)

// CollisionChoice is a decision and whether to reuse it for later collisions
// in the same batch execution.
type CollisionChoice struct {
	Decision   CollisionDecision
	ApplyToAll bool
}

// CollisionPrompt is a request for an asynchronous UI decision. Only the first
// valid Respond call has an effect; late and duplicate responses are ignored.
type CollisionPrompt struct {
	ID         ID
	Item       Item
	LocalPath  string
	RemotePath string

	respond *collisionResponse
}

// Respond supplies a decision. It is safe to call concurrently or after the
// operation has been cancelled.
func (p CollisionPrompt) Respond(choice CollisionChoice) {
	if p.respond == nil || !validCollisionChoice(choice) {
		return
	}
	p.respond.respond(choice)
}

// ItemError identifies a failed item. ItemIndex is -1 for a batch-level error,
// such as a cleanup failure.
type ItemError struct {
	ItemIndex int
	Item      Item
	Err       error
}

// Result is also the per-job representation in Snapshot. During execution its
// counters are live. CompletedBytes includes reported bytes from failed or
// cancelled partial items; skipped bytes are not counted. On successful items
// without a final progress report, Item.Size is counted automatically.
type Result struct {
	ID      ID
	RetryOf ID
	Status  Status

	TotalItems     int
	CompletedItems int
	SkippedItems   int
	FailedItems    int

	TotalBytes     int64
	CompletedBytes int64

	Errors []ItemError
	Err    error
}

// Snapshot is a deep copy of all job results in submission order. Mutating its
// slices does not affect the manager or other subscribers.
type Snapshot struct {
	Jobs   []Result
	Closed bool
}

var (
	// ErrClosed is returned when work is added after Close begins.
	ErrClosed = errors.New("transfer: manager is closed")
	// ErrNotFound identifies an unknown batch ID.
	ErrNotFound = errors.New("transfer: batch not found")
	// ErrNotRetryable is returned unless a batch is failed, partial, or cancelled.
	ErrNotRetryable = errors.New("transfer: batch is not retryable")
	// ErrInvalidBatch indicates an empty item list, nil operation, or negative size.
	ErrInvalidBatch = errors.New("transfer: invalid batch")
	// ErrNoCollisionHandler indicates ResolveCollision was used without OnCollision.
	ErrNoCollisionHandler = errors.New("transfer: no collision handler")
	// ErrSkip can be returned after CollisionSkip to count the item as skipped.
	ErrSkip = errors.New("transfer: item skipped")
	// ErrCollisionCancelled is returned to the operation for CollisionCancel.
	ErrCollisionCancelled = errors.New("transfer: batch cancelled by collision decision")
)

func validateBatch(batch Batch) error {
	if batch.Operation == nil || len(batch.Items) == 0 {
		return ErrInvalidBatch
	}
	var total int64
	for _, item := range batch.Items {
		if item.Size < 0 {
			return fmt.Errorf("%w: negative item size", ErrInvalidBatch)
		}
		if total > (1<<63-1)-item.Size {
			return fmt.Errorf("%w: total byte size overflows int64", ErrInvalidBatch)
		}
		total += item.Size
	}
	return nil
}

func validCollisionChoice(choice CollisionChoice) bool {
	return choice.Decision >= CollisionSkip && choice.Decision <= CollisionCancel
}

type collisionResponse struct {
	ch chan CollisionChoice
}

func (r *collisionResponse) respond(choice CollisionChoice) {
	select {
	case r.ch <- choice:
	default:
	}
}
