package app

import (
	"context"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
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

func requireReflogSession(session repository.Session) repository.ReflogSession {
	return session.(repository.ReflogSession)
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

// applyHostingHints tells the session the web addresses of the remotes, which
// settle the hosting provider for company hosts. It runs on the UI goroutine,
// before the work that needs the provider starts, because the addresses come
// from settings.
func (a *app) applyHostingHints(session repository.Session) {
	hinter, ok := session.(interface{ SetHostingHints([]string) })
	if !ok {
		return
	}
	var addresses []string
	for _, remote := range a.snapshot.Remotes {
		if web := a.remoteWebAddress(remote); web != "" {
			addresses = append(addresses, web)
		}
	}
	hinter.SetHostingHints(addresses)
}

func requireHostingSession(session repository.Session) repository.HostingSession {
	return session.(repository.HostingSession)
}
