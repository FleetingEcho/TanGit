package app

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// copyCommitText copies text that needs a Git read (the full message, the
// patch) without blocking the frame: the read runs off the UI goroutine and
// the clipboard is written, and the toast shown, when it returns.
func (a *app) copyCommitText(services ui.Services, what string, read func(context.Context, repository.Session) (string, error)) {
	session := a.session
	if session == nil {
		return
	}
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		text, err := read(ctx, session)
		a.update(func() {
			if err != nil {
				a.notifyError(err)
				return
			}
			services.WriteClipboard(text)
			a.notify("success", tr("Copied ")+tr(what), shorten(text, 120))
		})
	}()
}

func (a *app) copyCommitMessage(services ui.Services, commit repository.Commit) {
	a.copyCommitText(services, "commit message", func(ctx context.Context, session repository.Session) (string, error) {
		message, err := requireHistorySession(session).CommitMessage(ctx, commit)
		if err != nil {
			return "", err
		}
		if message.Description == "" {
			return message.Summary, nil
		}
		return message.Summary + "\n\n" + message.Description, nil
	})
}

func (a *app) copyCommitPatch(services ui.Services, commit repository.Commit) {
	a.copyCommitText(services, "patch", func(ctx context.Context, session repository.Session) (string, error) {
		patcher, ok := session.(overviewPatchSession)
		if !ok {
			return "", context.Canceled
		}
		return patcher.CommitPatch(ctx, commit)
	})
}

// commitWebURL is the commit's page on the hosting site: the origin remote's
// when it has a web address, else the first remote that does. GitHub's path
// differs from GitLab's, which also serves custom hosts.
func (a *app) commitWebURL(commit repository.Commit) string {
	if commit.Hash == "" {
		return ""
	}
	for pass := 0; pass < 2; pass++ {
		for _, remote := range a.snapshot.Remotes {
			if (remote.Name == "origin") != (pass == 0) {
				continue
			}
			web := a.remoteWebAddress(remote)
			if web == "" {
				continue
			}
			parsed, err := url.Parse(web)
			if err != nil {
				continue
			}
			if strings.EqualFold(parsed.Hostname(), "github.com") {
				return strings.TrimRight(web, "/") + "/commit/" + commit.Hash
			}
			return strings.TrimRight(web, "/") + "/-/commit/" + commit.Hash
		}
	}
	return ""
}

// beginWorktreeAtCommit opens the Create Worktree form on a detached checkout
// of the commit; naming a branch in the form turns it into a branch checkout.
func (a *app) beginWorktreeAtCommit(commit repository.Commit) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.worktreeBase = commit.Hash
	a.worktreeTemporary = false
	a.worktreeDetach = true
	a.worktreeOrphan = false
	a.worktreeBranch = ""
	a.worktreePath = a.suggestedWorktreePath()
	a.showWorktreeForm = true
}

// showCommitInOtherView jumps from History to Graph and back.
func (a *app) showCommitInOtherView(commit repository.Commit) {
	target := sectionGraph
	if a.workspaceTab == sectionGraph {
		target = sectionHistory
	}
	a.showCommitInSection(target, commit.Hash)
}

type branchesContainingSession interface {
	BranchesContaining(context.Context, repository.Commit) ([]string, error)
}

// showCommitInBranches selects, in Branches, the branch that has the commit as
// its tip, else the checked-out branch when it reaches the commit, else the
// first branch that does.
func (a *app) showCommitInBranches(commit repository.Commit) {
	session := a.session
	if session == nil {
		return
	}
	finder, ok := session.(branchesContainingSession)
	if !ok {
		return
	}
	ctx := a.repositoryWork.Context()
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		names, err := finder.BranchesContaining(ctx, commit)
		a.update(func() {
			if generation != a.generation {
				return
			}
			if err != nil {
				a.notifyError(err)
				return
			}
			if len(names) == 0 {
				a.notify("", tr("No local branch contains this commit."), commit.ShortHash)
				return
			}
			chosen := names[0]
			if !a.branchTip(chosen, commit) {
				for _, branch := range a.snapshot.Branches {
					if branch.Head && slices.Contains(names, branch.Name) {
						chosen = branch.Name
						break
					}
				}
			}
			a.branchSearch = ""
			a.revealBranch(chosen)
		})
	}()
}

// branchTip reports whether the commit is known to be the named branch's tip:
// BranchesContaining lists tips first, so only the head commit needs a check.
func (a *app) branchTip(name string, commit repository.Commit) bool {
	for _, branch := range a.snapshot.Branches {
		if branch.Name == name {
			return branch.Head && a.snapshot.HeadHash == commit.Hash
		}
	}
	return false
}
