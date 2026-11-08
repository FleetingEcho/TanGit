package transfer

import (
	"context"
	"errors"
	"fmt"
)

type batchExecution struct {
	manager    *Manager
	job        *job
	applyToAll *CollisionChoice
}

type itemExecution struct {
	collisionSkip   bool
	collisionCancel bool
}

func (m *Manager) runBatch(j *job) (cancelled bool, resultErr error) {
	execution := batchExecution{manager: m, job: j}
	for index, item := range j.batch.Items {
		if j.ctx.Err() != nil {
			return true, context.Canceled
		}

		m.startItem(j)
		itemState := itemExecution{}
		request := OperationRequest{
			Item:       item,
			LocalPath:  j.batch.LocalPath,
			RemotePath: j.batch.RemotePath,
		}
		token := j.activeToken
		request.ReportProgress = func(completed int64) {
			m.reportProgress(j, token, item.Size, completed)
		}
		request.ResolveCollision = func(ctx context.Context) (CollisionChoice, error) {
			return execution.resolveCollision(ctx, item, &itemState)
		}

		err := invokeOperation(j.ctx, j.batch.Operation, request)
		switch {
		case itemState.collisionCancel || errors.Is(err, ErrCollisionCancelled):
			m.endItem(j, token, index, item, itemCancelled, nil)
			return true, ErrCollisionCancelled
		case j.ctx.Err() != nil:
			m.endItem(j, token, index, item, itemCancelled, nil)
			return true, context.Canceled
		case errors.Is(err, ErrSkip) || (err == nil && itemState.collisionSkip):
			m.endItem(j, token, index, item, itemSkipped, nil)
		case err != nil:
			m.endItem(j, token, index, item, itemFailed, err)
		default:
			m.endItem(j, token, index, item, itemCompleted, nil)
		}
	}
	return false, nil
}

func invokeOperation(ctx context.Context, operation Operation, request OperationRequest) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("transfer: operation panic: %v", recovered)
		}
	}()
	return operation(ctx, request)
}

func (e *batchExecution) resolveCollision(ctx context.Context, item Item, state *itemExecution) (CollisionChoice, error) {
	if e.applyToAll != nil {
		choice := *e.applyToAll
		state.collisionSkip = choice.Decision == CollisionSkip
		return choice, nil
	}
	if e.job.batch.OnCollision == nil {
		return CollisionChoice{}, ErrNoCollisionHandler
	}

	response := &collisionResponse{ch: make(chan CollisionChoice, 1)}
	prompt := CollisionPrompt{
		ID:         e.job.id,
		Item:       item,
		LocalPath:  e.job.batch.LocalPath,
		RemotePath: e.job.batch.RemotePath,
		respond:    response,
	}
	if err := requestCollision(e.job.batch.OnCollision, prompt); err != nil {
		return CollisionChoice{}, err
	}
	if ctx == nil {
		ctx = e.job.ctx
	}
	select {
	case choice := <-response.ch:
		switch choice.Decision {
		case CollisionSkip:
			state.collisionSkip = true
		case CollisionCancel:
			state.collisionCancel = true
			return choice, ErrCollisionCancelled
		}
		if choice.ApplyToAll && (choice.Decision == CollisionSkip || choice.Decision == CollisionOverwrite) {
			choiceCopy := choice
			e.applyToAll = &choiceCopy
		}
		return choice, nil
	case <-e.job.ctx.Done():
		return CollisionChoice{}, e.job.ctx.Err()
	case <-ctx.Done():
		return CollisionChoice{}, ctx.Err()
	}
}

func requestCollision(callback func(CollisionPrompt), prompt CollisionPrompt) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("transfer: collision callback panic: %v", recovered)
		}
	}()
	callback(prompt)
	return nil
}

type itemOutcome uint8

const (
	itemCompleted itemOutcome = iota
	itemSkipped
	itemFailed
	itemCancelled
)

func (m *Manager) startItem(j *job) {
	m.mu.Lock()
	j.nextToken++
	j.activeToken = j.nextToken
	j.itemBytes = 0
	m.mu.Unlock()
}

func (m *Manager) reportProgress(j *job, token uint64, size, completed int64) {
	m.mu.Lock()
	if j.result.Status != StatusRunning || j.activeToken != token {
		m.mu.Unlock()
		return
	}
	if completed < 0 {
		completed = 0
	}
	if size > 0 && completed > size {
		completed = size
	}
	if completed > j.itemBytes {
		previousBytes := m.bytesBeforeCurrentItem(j)
		j.itemBytes = completed
		j.result.CompletedBytes = previousBytes + completed
	}
	m.mu.Unlock()
	m.publish()
}

func (m *Manager) endItem(j *job, token uint64, index int, item Item, outcome itemOutcome, itemErr error) {
	m.mu.Lock()
	if j.activeToken != token {
		m.mu.Unlock()
		return
	}
	if outcome == itemSkipped {
		j.result.CompletedBytes -= j.itemBytes
		j.itemBytes = 0
	}
	if outcome == itemCompleted && item.Size > 0 {
		j.result.CompletedBytes += item.Size - j.itemBytes
		j.itemBytes = item.Size
	}
	j.itemBytes = 0
	j.activeToken = 0

	switch outcome {
	case itemCompleted:
		j.result.CompletedItems++
	case itemSkipped:
		j.result.SkippedItems++
	case itemFailed:
		j.result.FailedItems++
		j.result.Errors = append(j.result.Errors, ItemError{ItemIndex: index, Item: item, Err: itemErr})
	}
	m.mu.Unlock()
	m.publish()
}

// bytesBeforeCurrentItem subtracts the live current-item progress from the
// public total, leaving the bytes attributed to already ended items.
func (m *Manager) bytesBeforeCurrentItem(j *job) int64 {
	return j.result.CompletedBytes - j.itemBytes
}
