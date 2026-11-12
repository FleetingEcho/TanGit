package repository

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/git_commands"
)

var (
	// ErrNothingToUndo reports that no eligible, not-yet-undone reflog action
	// exists. It is distinct from a Git command that failed while undoing one.
	ErrNothingToUndo = errors.New("no eligible Git operation to undo")
	// ErrNothingToRedo reports that no undone action can currently be reapplied.
	ErrNothingToRedo = errors.New("no eligible Git operation to redo")
	// ErrReflogReplayChanged reports that the exact approved action is no longer
	// the next replay, as when another Git client changes history meanwhile.
	ErrReflogReplayChanged = errors.New("Git history changed; review the operation and try again")
)

const (
	// Keep Lazygit's native marker names: the reflog is persisted state shared
	// by Git clients, so TanGit and Lazygit can understand each other's actions.
	reflogUndoMarker = "[lazygit undo]"
	reflogRedoMarker = "[lazygit redo]"
)

type reflogActionKind uint8

const (
	reflogCheckout reflogActionKind = iota
	reflogCommit
	reflogRebase
	reflogCurrentRebase
)

func (a reflogAction) identity() string {
	return fmt.Sprintf("%d:%s:%s", a.kind, a.from, a.to)
}

type reflogEntry struct {
	hash string
	name string
}

type reflogAction struct {
	kind reflogActionKind
	from string
	to   string
}

var (
	checkoutReflogPattern = regexp.MustCompile(`^checkout: moving from (\S+) to (\S+)`)
	rebaseStartPattern    = regexp.MustCompile(`^rebase (?:-i )?\(start\)`)
	rebaseFinishPattern   = regexp.MustCompile(`^rebase (?:-i )?\((?:finish|abort)\)`)
)

// Undo reverses the newest eligible action in the HEAD reflog. Undo and Redo
// write their own marked reflog entries rather than maintaining process-local
// stacks, so repeated calls and calls after restarting TanGit stay balanced.
func (s *gitSession) Undo(ctx context.Context) error {
	return s.replayReflog(ctx, false, nil)
}

// UndoExpected refuses a confirmation whose approved action is no longer next.
func (s *gitSession) UndoExpected(ctx context.Context, expected string) error {
	return s.replayReflog(ctx, false, &expected)
}

// Redo reapplies the eligible action reversed by the newest unmatched Undo.
func (s *gitSession) Redo(ctx context.Context) error {
	return s.replayReflog(ctx, true, nil)
}

// RedoExpected refuses a confirmation whose approved action is no longer next.
func (s *gitSession) RedoExpected(ctx context.Context, expected string) error {
	return s.replayReflog(ctx, true, &expected)
}

// ReflogReplayIdentity binds a confirmation to the exact visible reflog action.
// Both endpoint hashes and the action kind must match when it is accepted.
func ReflogReplayIdentity(commits []Commit, redo bool) (string, bool) {
	entries := make([]reflogEntry, 0, len(commits))
	for _, commit := range commits {
		entries = append(entries, reflogEntry{hash: commit.Hash, name: commit.Subject})
	}
	action, ok := findReflogAction(entries, redo)
	if !ok {
		return "", false
	}
	return action.identity(), true
}

// ReflogReplayDescription describes the operation a confirmation would replay.
// The app's display snapshot is bounded, so an eligible action outside it has
// no preview; expected replay revalidates its identity against the full reflog.
func ReflogReplayDescription(commits []Commit, operations []OperationRecord, redo bool) (string, bool) {
	entries := make([]reflogEntry, 0, len(commits))
	for _, commit := range commits {
		entries = append(entries, reflogEntry{hash: commit.Hash, name: commit.Subject})
	}
	action, ok := findReflogAction(entries, redo)
	if !ok {
		return "", false
	}
	if action.kind == reflogRebase {
		for _, record := range operations {
			if record.Kind == OperationRebase && record.Status == OperationSucceeded &&
				record.Summary != "" && record.HeadBefore == action.from && record.HeadAfter == action.to {
				return record.Summary, true
			}
		}
		return "completed rebase", true
	}
	for _, entry := range entries {
		checkout := checkoutReflogPattern.FindStringSubmatch(entry.name)
		if action.kind == reflogCheckout && len(checkout) > 0 && checkout[1] == action.from && checkout[2] == action.to {
			return entry.name, true
		}
		if action.kind == reflogCommit && entry.hash == action.to && isCommitResetOrPull(entry.name) {
			return entry.name, true
		}
	}
	return "", false
}

func (s *gitSession) replayReflog(ctx context.Context, redo bool, expected *string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	verb := "undo"
	if redo {
		verb = "redo"
	}
	if state := s.workingTreeState(); state.Any() {
		return fmt.Errorf("cannot %s while a %s is in progress", verb, state.Effective().CommandName())
	}

	// Read all reflog entries on every call. Undo markers may be older than the
	// Reflog panel's display page, and using a stale or bounded list would select
	// the wrong action after repeated operations.
	modelEntries, err := s.recentReflog(0)
	if err != nil {
		return fmt.Errorf("read reflog for %s: %w", verb, err)
	}
	entries := make([]reflogEntry, 0, len(modelEntries))
	for _, entry := range modelEntries {
		entries = append(entries, reflogEntry{hash: entry.Hash(), name: entry.Name})
	}
	action, ok := findReflogAction(entries, redo)
	if expected != nil && (!ok || action.identity() != *expected) {
		return ErrReflogReplayChanged
	}
	if !ok {
		if redo {
			return ErrNothingToRedo
		}
		return ErrNothingToUndo
	}

	marker := reflogUndoMarker
	if redo {
		marker = reflogRedoMarker
	}
	switch action.kind {
	case reflogCheckout:
		target := action.from
		if redo {
			target = action.to
		}
		if err := s.git.Branch.Checkout(target, git_commands.CheckoutOptions{
			EnvVars: []string{"GIT_REFLOG_ACTION=" + marker},
		}); err != nil {
			return fmt.Errorf("%s checkout of %s: %w", verb, target, err)
		}
		return nil

	case reflogCommit:
		// Undoing a commit, reset, or pull is soft so its changes remain in the
		// index. Reapplying it is a hard reset to the recorded post-action hash.
		target, mode := action.from, ResetSoft
		if redo {
			target, mode = action.to, ResetHard
		}
		if err := s.resetForReflogReplay(target, mode, marker); err != nil {
			return fmt.Errorf("%s commit, reset, or pull: %w", verb, err)
		}
		return nil

	case reflogRebase:
		target := action.from
		if redo {
			target = action.to
		}
		if err := s.resetForReflogReplay(target, ResetHard, marker); err != nil {
			return fmt.Errorf("%s rebase: %w", verb, err)
		}
		return nil
	}
	return fmt.Errorf("cannot %s the selected reflog action", verb)
}

// findReflogAction balances user actions against marked undo/redo entries while
// walking newest-to-oldest. Unsupported entries are skipped. A rebase counts as
// one action only after its finish/abort entry has been paired with its start;
// encountering a start without a finish means a rebase is currently active.
func findReflogAction(entries []reflogEntry, redo bool) (reflogAction, bool) {
	counter := 0
	rebaseFinishHash := ""

	for index, entry := range entries {
		previousHash := ""
		if index+1 < len(entries) {
			previousHash = entries[index+1].hash
		}

		var action *reflogAction
		checkoutMatch := checkoutReflogPattern.FindStringSubmatch(entry.name)
		if rebaseFinishHash == "" {
			switch {
			case strings.HasPrefix(entry.name, reflogUndoMarker):
				counter++
			case strings.HasPrefix(entry.name, reflogRedoMarker):
				counter--
			case rebaseFinishPattern.MatchString(entry.name):
				rebaseFinishHash = entry.hash
			case len(checkoutMatch) > 0:
				action = &reflogAction{kind: reflogCheckout, from: checkoutMatch[1], to: checkoutMatch[2]}
			case isCommitResetOrPull(entry.name):
				action = &reflogAction{kind: reflogCommit, from: previousHash, to: entry.hash}
			case rebaseStartPattern.MatchString(entry.name):
				action = &reflogAction{kind: reflogCurrentRebase, from: previousHash}
			}
		} else if rebaseStartPattern.MatchString(entry.name) {
			action = &reflogAction{kind: reflogRebase, from: previousHash, to: rebaseFinishHash}
			rebaseFinishHash = ""
		}

		if action == nil {
			continue
		}
		if action.kind == reflogCurrentRebase {
			return reflogAction{}, false
		}
		// A same-hash action did not move HEAD. The root commit has no previous
		// commit to reset to, so it is not an undoable action either.
		if action.from == "" || action.to == "" || action.from == action.to {
			continue
		}

		if redo {
			if counter == 0 {
				return reflogAction{}, false
			}
			if counter == 1 {
				return *action, true
			}
		} else if counter == 0 {
			return *action, true
		}
		counter--
	}
	return reflogAction{}, false
}

func isCommitResetOrPull(name string) bool {
	return strings.HasPrefix(name, "commit") ||
		strings.HasPrefix(name, "reset: moving to") ||
		strings.HasPrefix(name, "pull")
}

// resetForReflogReplay protects tracked and staged work around a hard reset.
// Submodules are excluded just as in Lazygit: resetting the superproject must
// not discard a submodule's independent working-tree changes. If the reset
// fails after stashing, restoring the work is attempted before returning.
func (s *gitSession) resetForReflogReplay(ref string, mode ResetMode, marker string) error {
	reset := func() error {
		return s.git.Commit.ResetToCommit(ref, string(mode), []string{"GIT_REFLOG_ACTION=" + marker})
	}
	if mode != ResetHard {
		return reset()
	}

	dirty, err := s.hasTrackedChangesExceptSubmodules()
	if err != nil {
		return fmt.Errorf("inspect working tree: %w", err)
	}
	if !dirty {
		return reset()
	}
	if err := s.git.Stash.Push("TanGit auto-stash before reset to " + ref); err != nil {
		return fmt.Errorf("auto-stash working tree: %w", err)
	}
	if err := reset(); err != nil {
		resetErr := fmt.Errorf("hard reset to %s: %w", ref, err)
		if restoreErr := s.git.Stash.Pop(0); restoreErr != nil {
			return errors.Join(resetErr, fmt.Errorf("restore auto-stash after failed reset: %w", restoreErr))
		}
		return resetErr
	}
	if err := s.git.Stash.Pop(0); err != nil {
		return fmt.Errorf("restore auto-stash: %w", err)
	}
	return nil
}

func (s *gitSession) hasTrackedChangesExceptSubmodules() (bool, error) {
	files, err := s.statusFiles(git_commands.GetStatusFileOptions{})
	if err != nil {
		return false, err
	}
	configs, err := s.submoduleConfigs(nil)
	if err != nil {
		return false, err
	}
	for _, file := range files {
		if file.IsSubmodule(configs) {
			continue
		}
		if file.Tracked || file.HasStagedChanges {
			return true, nil
		}
	}
	return false, nil
}
