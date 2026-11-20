package app

import (
	"sync"
	"sync/atomic"
)

// One application's cache root has one lifecycle for index publication and
// reclamation. A builder holds a read lease from before any staging file is
// created until its directory has been published or discarded. An explicit
// cache clear takes the write lease, so it cannot race a half-written staging
// directory or a newly published generation.
var codeSearchIndexMaintenance = struct {
	sync.RWMutex
	requested atomic.Bool
}{}

// acquireCodeSearchIndexBuild prevents writers while a generation is built.
// Nil means a cache-clear operation has suspended new index construction.
// The caller must release every non-nil lease on every return path.
func acquireCodeSearchIndexBuild() (release func(), ok bool) {
	codeSearchIndexMaintenance.RLock()
	if !codeSearchIndexMaintenance.requested.Load() {
		return codeSearchIndexMaintenance.RUnlock, true
	}
	codeSearchIndexMaintenance.RUnlock()
	return nil, false
}

// beginCodeSearchIndexMaintenance is a non-blocking, UI-thread-safe way to
// prevent new build leases before cancelling the current builders.
func beginCodeSearchIndexMaintenance() { codeSearchIndexMaintenance.requested.Store(true) }

// withCodeSearchIndexMaintenance waits for existing builders and executes a
// deletion while holding the exclusive write lease. It must run off the UI
// thread. Its cleanup also protects the next build after a failing removal.
func withCodeSearchIndexMaintenance(work func()) {
	codeSearchIndexMaintenance.Lock()
	defer func() {
		codeSearchIndexMaintenance.requested.Store(false)
		codeSearchIndexMaintenance.Unlock()
	}()
	work()
}

// invalidateSearchIndexBeforeCacheClear cancels builders in every tab before
// filesystem removal, rather than only invalidating their state afterwards.
func (a *app) invalidateSearchIndexBeforeCacheClear() {
	beginCodeSearchIndexMaintenance()
	if a.onCachesCleared != nil {
		a.onCachesCleared()
	}
}
