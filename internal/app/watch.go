package app

import (
	"context"

	"lazygitapp/internal/repository"
)

// startRepositoryWatch connects a real repository's filesystem notifications
// to the same quiet refresh used by focus changes and the timer. Failure to
// watch is deliberately quiet: polling still keeps the repository current.
func (a *app) startRepositoryWatch(session repository.Session) {
	if a.cancelRepositoryWatch != nil {
		a.cancelRepositoryWatch()
		a.cancelRepositoryWatch = nil
	}
	watched, ok := session.(repository.WatchingSession)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Register cancellation before starting any filesystem work. In particular,
	// closing a page while a large tree is being walked must stop that walk.
	a.cancelRepositoryWatch = cancel
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		events, err := watched.Watch(ctx)
		if err != nil {
			cancel()
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-events:
				if !ok {
					return
				}
				a.update(func() {
					if generation != a.generation {
						return
					}
					// A shell refreshes only its visible, focused page. The
					// existing focus/tab hooks catch everything else up.
					if a.externalRefresh != nil {
						a.externalRefresh()
						return
					}
					a.requestQuietRefresh()
				})
			}
		}
	}()
}

// stopRepositoryWatch releases OS watchers before a tab is closed or its
// repository is replaced. Cancelling is idempotent, so every shutdown path can
// call it without coordinating with the watch goroutine.
func (a *app) stopRepositoryWatch() {
	if a.cancelRepositoryWatch != nil {
		a.cancelRepositoryWatch()
		a.cancelRepositoryWatch = nil
	}
}
