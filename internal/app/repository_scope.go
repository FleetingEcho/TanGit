package app

import (
	"context"
	"sync"
)

// repositoryScope owns cancellation for work whose result belongs to the
// currently installed repository. Replacing or closing that repository
// cancels the old scope before another session can publish into the tab.
type repositoryScope struct {
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

func newRepositoryScope() repositoryScope {
	ctx, cancel := context.WithCancel(context.Background())
	return repositoryScope{ctx: ctx, cancel: cancel}
}

func (s *repositoryScope) Context() context.Context {
	s.mu.RLock()
	ctx := s.ctx
	s.mu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *repositoryScope) Replace() {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	previous := s.cancel
	s.ctx, s.cancel = ctx, cancel
	s.mu.Unlock()
	if previous != nil {
		previous()
	}
}

func (s *repositoryScope) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
