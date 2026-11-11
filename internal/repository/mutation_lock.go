package repository

import (
	"path"
	"path/filepath"
	"sync"
)

// worktreeMutationLocks coordinates independent sessions for the same checkout
// inside TanGit. Preview/preflight and its subsequent write must hold the same
// lock, otherwise two open tabs can both approve an older state and overwrite
// each other's work. External Git processes remain subject to Git's own checks
// and, for branch deletion, the expected-old-value ref update.
var worktreeMutationLocks sync.Map

func (s *gitSession) repositoryIdentity() string {
	root := filepath.Clean(s.git.RepoPaths.WorktreePath())
	if s.sshClient == nil {
		return root
	}
	return "ssh\x00" + s.location.ProfileID + "\x00" + path.Clean(s.git.RepoPaths.WorktreePath())
}

func (s *gitSession) lockWorktreeMutation() func() {
	key := s.repositoryIdentity()
	value, _ := worktreeMutationLocks.LoadOrStore(key, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}
