package app

import (
	"context"

	"lazygitapp/internal/repository"
)

// Method-specific seams keep read-only views independent from unrelated
// history rewrites or working-tree mutations.
type workingTreeDiffSession interface {
	Diff(context.Context, repository.File, repository.DiffOptions) (repository.Diff, error)
}

type branchCommitsSession interface {
	BranchCommits(context.Context, string, int) ([]repository.Commit, error)
}

type commitSearchSession interface {
	SearchCommits(context.Context, repository.CommitFilter) ([]repository.Commit, error)
}

func requireWorkingTreeDiffSession(session repository.Session) workingTreeDiffSession {
	return session.(workingTreeDiffSession)
}

func requireBranchCommitsSession(session repository.Session) branchCommitsSession {
	return session.(branchCommitsSession)
}

func requireCommitSearchSession(session repository.Session) commitSearchSession {
	return session.(commitSearchSession)
}

// The real opener supplies every required capability (enforced by
// repository.FullSession's compile-time assertion). Keeping the stored Session
// narrow lets adapters and tests implement only the workflows they exercise.
func requireHistorySession(session repository.Session) repository.HistorySession {
	return session.(repository.HistorySession)
}

func requireChangesSession(session repository.Session) repository.ChangesSession {
	return session.(repository.ChangesSession)
}

func requireBranchRemoteSession(session repository.Session) repository.BranchRemoteSession {
	return session.(repository.BranchRemoteSession)
}

func requireStashSession(session repository.Session) repository.StashSession {
	return session.(repository.StashSession)
}

func requireWorktreeSession(session repository.Session) repository.WorktreeSession {
	return session.(repository.WorktreeSession)
}

func requireHostingSession(session repository.Session) repository.HostingSession {
	return session.(repository.HostingSession)
}
