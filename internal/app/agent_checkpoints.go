package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentCheckpointHeadMismatch struct {
	captured string
	current  string
}

func (e *agentCheckpointHeadMismatch) Error() string {
	return fmt.Sprintf("task HEAD is %s, but the checkpoint captured %s", agentShortHash(e.current), agentShortHash(e.captured))
}

// createAgentCheckpoint captures the explicit task worktree before invoking an
// optional continuation. Accept uses that continuation to sequence its main
// repository mutation after both the snapshot and durable record have landed.
func (a *app) createAgentCheckpoint(taskID, reason string, continuation ...func(error)) {
	var done func(error)
	if len(continuation) > 0 {
		done = continuation[0]
	}
	a.runAgentCheckpoint(taskID, "Could not create agent checkpoint", "Agent checkpoint created", done,
		func(ctx context.Context, target repository.Session, task agentwork.Task, store *agentwork.Store) (agentwork.Task, error) {
			snapshot, err := target.Snapshot(ctx)
			if err != nil {
				return agentwork.Task{}, err
			}
			if strings.TrimSpace(snapshot.HeadHash) == "" {
				return agentwork.Task{}, errors.New("task worktree HEAD is unavailable")
			}
			captured, err := target.CreateCheckpoint(ctx, reason)
			if err != nil {
				return agentwork.Task{}, err
			}
			return store.RecordCheckpoint(task.ID, agentwork.Checkpoint{
				SnapshotID: captured.ID,
				HeadCommit: snapshot.HeadHash,
				Reason:     reason,
			})
		})
}

func (a *app) restoreAgentCheckpoint(taskID, checkpointID string) {
	a.runAgentCheckpoint(taskID, "Could not restore agent checkpoint", "Agent checkpoint restored", func(err error) {
		if err == nil && a.agents.review.open && a.agents.review.taskID == taskID {
			a.reloadAgentReview()
		}
	},
		func(ctx context.Context, target repository.Session, task agentwork.Task, store *agentwork.Store) (agentwork.Task, error) {
			var checkpoint agentwork.Checkpoint
			found := false
			for _, candidate := range task.Checkpoints {
				if candidate.ID == checkpointID {
					checkpoint = candidate
					found = true
					break
				}
			}
			if !found {
				return agentwork.Task{}, errors.New("agent checkpoint was not found")
			}

			// HEAD is an identity guard, not a readiness or cleanliness inference.
			// Refuse before creating even a safety snapshot when newer commits exist.
			current, err := target.Snapshot(ctx)
			if err != nil {
				return agentwork.Task{}, err
			}
			if current.HeadHash != checkpoint.HeadCommit {
				return agentwork.Task{}, &agentCheckpointHeadMismatch{captured: checkpoint.HeadCommit, current: current.HeadHash}
			}

			safetyReason := "Before restore " + agentShortHash(checkpoint.ID)
			safety, err := target.CreateCheckpoint(ctx, safetyReason)
			if err != nil {
				return agentwork.Task{}, err
			}
			updated, err := store.RecordCheckpoint(task.ID, agentwork.Checkpoint{
				SnapshotID: safety.ID,
				HeadCommit: current.HeadHash,
				Reason:     safetyReason,
			})
			if err != nil {
				return agentwork.Task{}, err
			}
			// RestoreWorkSnapshot applies only W/I/U work. Repository-level
			// RestoreConflict checks still reject overlap with newer dirty paths.
			err = target.RestoreWorkSnapshot(ctx, repository.WorkSnapshot{
				ID: checkpoint.SnapshotID, Hash: checkpoint.SnapshotID,
				Kind: repository.CheckpointSnapshotKind, Name: checkpoint.Reason,
				CreatedAt: checkpoint.CreatedAt.Unix(),
			})
			return updated, err
		})
}

type agentCheckpointOperation func(context.Context, repository.Session, agentwork.Task, *agentwork.Store) (agentwork.Task, error)

// runAgentCheckpoint owns every session it opens. A completion merges durable
// checkpoint history only into the same task in the same repository generation;
// a late success therefore cannot continue an Accept after a tab/worktree reset.
func (a *app) runAgentCheckpoint(
	taskID, failureTitle, successTitle string,
	done func(error),
	operation agentCheckpointOperation,
) {
	if taskID == "" || a.agents.checkpointBusy[taskID] {
		if done != nil {
			done(errors.New("an agent checkpoint operation is already running for this task"))
		}
		return
	}
	var task agentwork.Task
	found := false
	for _, candidate := range a.agents.tasks {
		if candidate.ID == taskID {
			task = candidate.Clone()
			found = true
			break
		}
	}
	if !found || task.WorktreePath == "" {
		if done != nil {
			done(errors.New("agent task is no longer available"))
		}
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", failureTitle, safeError(err))
		if done != nil {
			done(err)
		}
		return
	}
	if a.agents.checkpointBusy == nil {
		a.agents.checkpointBusy = map[string]bool{}
	}
	a.agents.checkpointBusy[taskID] = true
	generation := a.generation
	worktree := task.WorktreePath
	openPath := a.worktreeOpenPath(worktree)

	go func() {
		defer a.recoverBackground()
		ctx := context.Background()
		persisted, err := store.Get(taskID)
		if err == nil && persisted.WorktreePath != worktree {
			err = errors.New("agent task worktree changed before the checkpoint operation")
		}
		var target repository.Session
		if err == nil {
			target, err = a.opener.Open(ctx, openPath)
		}
		// This goroutine is the sole owner of an explicitly opened target.
		// It never closes a review session or the current repository tab.
		defer closeRepositorySession(target)

		var updated agentwork.Task
		if err == nil {
			updated, err = operation(ctx, target, persisted, store)
		}
		a.update(func() {
			delete(a.agents.checkpointBusy, taskID)
			if generation != a.generation {
				return
			}
			identityMatches := false
			if updated.ID == taskID {
				for index := range a.agents.tasks {
					if a.agents.tasks[index].ID == taskID && a.agents.tasks[index].WorktreePath == worktree {
						a.agents.tasks[index] = updated.Clone()
						identityMatches = true
						break
					}
				}
			} else {
				for _, candidate := range a.agents.tasks {
					if candidate.ID == taskID && candidate.WorktreePath == worktree {
						identityMatches = true
						break
					}
				}
			}
			if err != nil {
				var mismatch *agentCheckpointHeadMismatch
				if errors.As(err, &mismatch) {
					a.notify("warning", "Checkpoint not restored", "Newer work was detected in the task worktree. "+mismatch.Error()+". No changes were made.")
				} else {
					a.notify("error", failureTitle, safeError(err))
				}
				if done != nil {
					done(err)
				}
				return
			}
			if !identityMatches {
				if done != nil {
					done(errors.New("agent task identity changed before the checkpoint completed"))
				}
				return
			}
			a.notify("success", successTitle, task.Title)
			if done != nil {
				done(nil)
			}
		})
	}()
}
