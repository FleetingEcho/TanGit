package app

import (
	"context"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// commitLineStatBatch is how many commits one read asks for. A page of rows on
// screen is far fewer, and one read for the lot is cheaper than one each.
const commitLineStatBatch = 50

type commitLineCount struct{ added, deleted int }

// commitLineCounts is the added and deleted lines of a commit as its row shows
// them. Commits read with their counts answer at once. A commit read without
// them (the session defers the read, which is slow on a large repository) is
// queued here the first time a row asks, and answers false until the batch it
// belongs to comes back; that read repaints the rows. Only commits some row
// has asked about are ever counted.
func (a *app) commitLineCounts(commit repository.Commit) (added, deleted int, ready bool) {
	if !commit.StatsPending {
		return commit.LinesAdded, commit.LinesDeleted, true
	}
	if a.commitCountsGeneration != a.generation || a.commitCounts == nil {
		a.commitCounts = make(map[string]commitLineCount)
		a.commitCountQueue = nil
		a.commitCountsBusy = false
		a.commitCountsGeneration = a.generation
	}
	if count, ok := a.commitCounts[commit.Hash]; ok {
		return count.added, count.deleted, true
	}
	for _, queued := range a.commitCountQueue {
		if queued.Hash == commit.Hash {
			return 0, 0, false
		}
	}
	a.commitCountQueue = append(a.commitCountQueue, commit)
	a.startCommitLineCounts()
	return 0, 0, false
}

// startCommitLineCounts reads the next batch off the UI goroutine, one batch
// at a time, and publishes it by hash.
func (a *app) startCommitLineCounts() {
	reader, ok := a.session.(interface {
		CommitLineStats(context.Context, []repository.Commit) ([]repository.Commit, error)
	})
	if !ok || a.commitCountsBusy || len(a.commitCountQueue) == 0 {
		return
	}
	batch := a.commitCountQueue
	if len(batch) > commitLineStatBatch {
		batch = batch[:commitLineStatBatch]
	}
	a.commitCountQueue = append([]repository.Commit(nil), a.commitCountQueue[len(batch):]...)
	batch = append([]repository.Commit(nil), batch...)
	a.commitCountsBusy = true
	generation := a.generation
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		filled, err := reader.CommitLineStats(ctx, batch)
		if ctx.Err() != nil {
			return
		}
		a.updateChanged(func() bool {
			if generation != a.generation {
				return false
			}
			a.commitCountsBusy = false
			for i, commit := range batch {
				count := commitLineCount{}
				// A failed read still ends the animation: the counts are only
				// decoration, and a row that waits forever says less than none.
				if err == nil && i < len(filled) {
					count = commitLineCount{filled[i].LinesAdded, filled[i].LinesDeleted}
				}
				a.commitCounts[commit.Hash] = count
			}
			a.startCommitLineCounts()
			return true
		})
	}()
}
