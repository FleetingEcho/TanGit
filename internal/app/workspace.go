package app

import (
	"io"
	"strings"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

// workspaceSession identifies the workspace owned by a session without making
// local sessions implement remote-aware identity before they need it.
type workspaceSession interface {
	Workspace() workspace.Location
}

// openedLocation keeps the repository's native root separate from its stable
// identity. In particular, an SSH root remains a POSIX path inside Snapshot.
func openedLocation(session repository.Session, snapshot repository.Snapshot, requested string) workspace.Location {
	if identified, ok := session.(workspaceSession); ok {
		if location := identified.Workspace(); location.Valid() {
			return location
		}
	}
	if requested, err := workspace.Parse(requested); err == nil && requested.IsRemote() {
		requested.Root = snapshot.Root
		if requested.Valid() {
			return requested
		}
	}
	return workspace.Location{Kind: workspace.Local, Root: snapshot.Root}
}

// openedMainWorktreePath identifies the repository that owns the opened
// checkout. A local owner is its physical absolute path; an SSH owner retains
// its profile-qualified workspace identity.
func (a *app) openedMainWorktreePath() string {
	root := ""
	for _, worktree := range a.snapshot.Worktrees {
		if worktree.Main {
			root = strings.TrimSpace(worktree.Path)
			break
		}
	}
	if root == "" {
		return ""
	}
	location := a.location
	location.Root = root
	if !location.Valid() {
		return ""
	}
	if location.IsRemote() {
		return location.String()
	}
	return cleanRepoPath(location.String())
}

// readBeforeInstall retains ownership until a snapshot read returns normally.
// If it panics, the deferred close runs while the panic continues to the
// application's background recovery. After a normal return, the completion
// callback either installs the session or closes it.
func readBeforeInstall(session repository.Session, read func() (repository.Snapshot, error)) (snapshot repository.Snapshot, err error) {
	returned := false
	defer func() {
		if !returned {
			closeRepositorySession(session)
		}
	}()
	snapshot, err = read()
	returned = true
	return snapshot, err
}

// canonicalRemoteReference recognizes only valid SSH workspace URIs. Other
// strings continue through the existing local filepath normalization.
func canonicalRemoteReference(value string) (string, bool) {
	location, err := workspace.Parse(value)
	if err != nil || !location.IsRemote() {
		return "", false
	}
	return location.String(), true
}

// closeRepositorySession releases remote resources when a session offers an
// owner-aware Close method. Local sessions need no Close method.
func closeRepositorySession(session repository.Session) {
	if closer, ok := session.(io.Closer); ok {
		_ = closer.Close()
	}
}
