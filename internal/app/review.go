package app

import (
	"context"
	"errors"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const reviewLocalStatusMessage = "Review marks are only a status in lazygitGo; they do not change Git information."

type reviewUIState struct {
	files        map[string]bool
	requestedSeq uint64
	requestToken uint64
	loading      bool
	filter       bool
}

func newReviewUIState() reviewUIState {
	return reviewUIState{files: map[string]bool{}}
}

func (s *reviewUIState) reset() {
	s.files = map[string]bool{}
	s.requestedSeq = 0
	s.requestToken++
	s.loading = false
	s.filter = false
}

func reviewActionTooltip() string {
	return tr("Toggle whether this file's current content has been reviewed (R)") + " " + tr(reviewLocalStatusMessage)
}

// requestReviewedFiles reads review marks after a new repository snapshot.
// Marks are content fingerprints, so a refresh can invalidate a row edited by
// lazygitGo, another application, or an ordinary Git operation without special
// cases in each action.
func (a *app) requestReviewedFiles() {
	reviewer, ok := a.session.(repository.ReviewSession)
	if !ok || a.review.loading || a.review.requestedSeq == a.snapshotSeq {
		return
	}
	files := append([]repository.File(nil), a.snapshot.Files...)
	sequence := a.snapshotSeq
	generation := a.generation
	a.review.requestToken++
	token := a.review.requestToken
	a.review.requestedSeq = sequence
	a.review.loading = true
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		reviewed, err := reviewer.ReviewedFiles(ctx, files)
		a.update(func() {
			if generation != a.generation || token != a.review.requestToken {
				return
			}
			a.review.loading = false
			if sequence != a.snapshotSeq {
				return
			}
			if errors.Is(err, context.Canceled) {
				return
			}
			if err != nil {
				a.notifyError(err)
				return
			}
			a.review.files = reviewed
		})
	}()
}

func (a *app) reviewProgress() (reviewed, total int) {
	for _, file := range a.snapshot.Files {
		total++
		if a.review.files[file.Path] {
			reviewed++
		}
	}
	return reviewed, total
}

// visibleReviewFiles applies status and local review filters before
// sections/counts are built, keeping headers and keyboard selection aligned.
func (a *app) visibleReviewFiles(files []repository.File) []repository.File {
	files = changesFilesWithStatus(files, a.fileStatusFilter)
	if !a.review.filter {
		return files
	}
	visible := make([]repository.File, 0, len(files))
	for _, file := range files {
		if !a.review.files[file.Path] {
			visible = append(visible, file)
		}
	}
	return visible
}

func (a *app) hasUnreviewedFiles() bool {
	for _, file := range changesFilesWithStatus(a.snapshot.Files, a.fileStatusFilter) {
		if !a.review.files[file.Path] {
			return true
		}
	}
	return false
}

// selectNextUnreviewed advances through visible file rows and wraps, skipping
// both sides of a path already reviewed and the row from which it started.
func (a *app) selectNextUnreviewed() {
	if len(a.changeRows) == 0 {
		return
	}
	for step := 1; step <= len(a.changeRows); step++ {
		index := (a.selectedFile + step + len(a.changeRows)) % len(a.changeRows)
		row := a.changeRows[index]
		if row.isFile() && !a.review.files[row.file.Path] {
			a.selectedFile = index
			a.resetFileDiff()
			return
		}
	}
}

func (a *app) toggleSelectedReviewed() {
	change, ok := a.selectedChange()
	if !ok {
		return
	}
	a.setFileReviewed(change.file, !a.review.files[change.file.Path])
}

// setFileReviewed persists a content mark without rewriting any Git state and
// therefore without replacing the snapshot or losing the user's draft/scroll.
func (a *app) setFileReviewed(file repository.File, reviewed bool) {
	reviewer, ok := a.session.(repository.ReviewSession)
	if a.actionBusy || !ok {
		return
	}
	generation := a.generation
	ctx := a.repositoryWork.Context()
	a.actionBusy = true
	a.actionError = ""
	go func() {
		defer a.recoverBackground()
		err := reviewer.SetFileReviewed(ctx, file, reviewed)
		a.update(func() {
			if generation != a.generation {
				return
			}
			a.actionBusy = false
			if err != nil {
				a.actionError = safeError(err)
				a.notifyError(err)
				return
			}
			if a.review.files == nil {
				a.review.files = map[string]bool{}
			}
			if reviewed {
				a.review.files[file.Path] = true
			} else {
				delete(a.review.files, file.Path)
			}
			if reviewed && a.review.filter {
				// The selected row is about to disappear. Choose the next work
				// item before the following frame rebuilds section indices.
				rows := a.changeRowsFrom(a.snapshot.Files)
				a.changeRows = rows
				a.selectedFile = firstChangeFile(rows)
				a.resetFileDiff()
			}
		})
	}()
}
