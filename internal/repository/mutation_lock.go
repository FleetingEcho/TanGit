package repository

import (
	"path/filepath"
	"sync"
)

// worktreeMutationLocks coordinates independent sessions for the same checkout
// inside TanGit. Preview/preflight and its subsequent write must hold the same
// lock, otherwise two open tabs can both approve an older state and overwrite
// each other's work. External Git processes remain subject to Git's own checks
// and, for branch deletion, the expected-old-value ref update.
var worktreeMutationLocks sync.Map

func (s *gitSession) lockWorktreeMutation() func() {
	key := filepath.Clean(s.git.RepoPaths.WorktreePath())
	value, _ := worktreeMutationLocks.LoadOrStore(key, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}
