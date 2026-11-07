package app

import (
	"context"

	"lazygitapp/internal/repository"
)

const reviewLocalStatusMessage = "Review marks are only a status in TanGit; they do not change Git information."

func reviewActionTooltip() string {
	return tr("Toggle whether this file's current content has been reviewed (R)") + " " + tr(reviewLocalStatusMessage)
}

// requestReviewedFiles reads review marks after a new repository snapshot.
// Marks are content fingerprints, so a refresh can invalidate a row edited by
// TanGit, another application, or an ordinary Git operation without special
// cases in each action.
func (a *app) requestReviewedFiles() {
	if a.session == nil || a.reviewLoading || a.reviewRequestedSeq == a.snapshotSeq {
		return
	}
	session := a.session
	files := append([]repository.File(nil), a.snapshot.Files...)
	sequence := a.snapshotSeq
	generation := a.generation
	a.reviewRequestToken++
	token := a.reviewRequestToken
	a.reviewRequestedSeq = sequence
	a.reviewLoading = true
	go func() {
		defer a.recoverBackground()
		reviewed, err := session.ReviewedFiles(context.Background(), files)
		a.update(func() {
			if generation != a.generation || token != a.reviewRequestToken {
				return
			}
			a.reviewLoading = false
			if sequence != a.snapshotSeq {
				return
			}
			if err != nil {
				a.notifyError(err)
				return
			}
			a.reviewedFiles = reviewed
		})
	}()
}

func (a *app) reviewProgress() (reviewed, total int) {
	for _, file := range a.snapshot.Files {
		total++
		if a.reviewedFiles[file.Path] {
			reviewed++
		}
	}
	return reviewed, total
}

// visibleReviewFiles applies review filtering before sections/counts are
// built, keeping headers and keyboard selection consistent with visible rows.
func (a *app) visibleReviewFiles(files []repository.File) []repository.File {
	if !a.reviewFilter {
		return files
	}
	visible := make([]repository.File, 0, len(files))
	for _, file := range files {
		if !a.reviewedFiles[file.Path] {
			visible = append(visible, file)
		}
	}
	return visible
}

func (a *app) hasUnreviewedFiles() bool {
	for _, file := range a.snapshot.Files {
		if !a.reviewedFiles[file.Path] {
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
		if row.isFile() && !a.reviewedFiles[row.file.Path] {
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
	a.setFileReviewed(change.file, !a.reviewedFiles[change.file.Path])
}

// setFileReviewed persists a content mark without rewriting any Git state and
// therefore without replacing the snapshot or losing the user's draft/scroll.
func (a *app) setFileReviewed(file repository.File, reviewed bool) {
	if a.actionBusy || a.session == nil {
		return
	}
	session := a.session
	generation := a.generation
	a.actionBusy = true
	a.actionError = ""
	go func() {
		defer a.recoverBackground()
		err := session.SetFileReviewed(context.Background(), file, reviewed)
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
			if a.reviewedFiles == nil {
				a.reviewedFiles = map[string]bool{}
			}
			if reviewed {
				a.reviewedFiles[file.Path] = true
			} else {
				delete(a.reviewedFiles, file.Path)
			}
			if reviewed && a.reviewFilter {
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
