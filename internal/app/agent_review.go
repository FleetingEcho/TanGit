package app

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentReviewMode uint8

const (
	agentReviewFinal agentReviewMode = iota
	agentReviewCommits
	agentReviewDirty
)

// agentReviewState owns a read-only session for the task worktree. It never
// substitutes the repository tab's session: commits, dirty files and the final
// base-to-worktree result all come from the same explicit task target.
type agentReviewState struct {
	open      bool
	taskID    string
	title     string
	feedback  string
	base      string
	head      string
	worktree  string
	target    repository.Session
	cancel    context.CancelFunc
	token     uint64
	loading   bool
	err       string
	mode      agentReviewMode
	options   repository.DiffOptions
	listWidth float32

	finalFiles  []repository.CommitFile
	commits     []repository.Commit
	commitFiles []repository.CommitFile
	dirtyFiles  []repository.File

	list               ui.ListState
	selectedRow        int
	selectedFile       int
	selectedCommit     int
	selectedCommitFile string

	diffKey     string
	diffPatch   string
	diffState   *editor.DiffState
	diffLoading bool
	diffError   string
}

func (a *app) openAgentReview(task agentwork.Task) {
	if task.ID == "" || task.BaseCommit == "" || task.WorktreePath == "" {
		return
	}
	a.resetAgentReview()
	a.agents.review.open = true
	a.agents.review.taskID = task.ID
	a.agents.review.title = task.Title
	a.agents.review.base = task.BaseCommit
	a.agents.review.worktree = task.WorktreePath
	a.agents.review.options = repository.DiffOptions{ContextLines: 3}
	a.agents.review.selectedFile = -1
	a.agents.review.selectedCommit = -1
	a.reloadAgentReview()
}

func (a *app) closeAgentReview() {
	a.resetAgentReview()
}

// resetAgentReview invalidates callbacks before closing their session. Every
// completion closes an uninstalled target, so replacement and repository reset
// cannot leak remote connections or apply results to another task.
func (a *app) resetAgentReview() {
	review := &a.agents.review
	review.token++
	if review.cancel != nil {
		review.cancel()
		review.cancel = nil
	}
	closeRepositorySession(review.target)
	review.target = nil
	review.open = false
	review.taskID = ""
	review.title = ""
	review.feedback = ""
	review.base = ""
	review.head = ""
	review.worktree = ""
	review.loading = false
	review.err = ""
	review.finalFiles = nil
	review.commits = nil
	review.commitFiles = nil
	review.dirtyFiles = nil
	review.selectedFile = -1
	review.selectedCommit = -1
	review.selectedCommitFile = ""
	a.clearAgentReviewDiff()
}

func (a *app) clearAgentReviewDiff() {
	review := &a.agents.review
	review.diffKey = ""
	review.diffPatch = ""
	review.diffState = nil
	review.diffLoading = false
	review.diffError = ""
}

func (a *app) reloadAgentReview() {
	review := &a.agents.review
	if !review.open || review.taskID == "" {
		return
	}
	review.token++
	if review.cancel != nil {
		review.cancel()
	}
	closeRepositorySession(review.target)
	review.target = nil
	review.loading = true
	review.err = ""
	review.finalFiles = nil
	review.commits = nil
	review.commitFiles = nil
	review.dirtyFiles = nil
	review.selectedFile = -1
	review.selectedCommit = -1
	review.selectedCommitFile = ""
	a.clearAgentReviewDiff()

	token := review.token
	generation := a.generation
	taskID, worktree, base := review.taskID, review.worktree, review.base
	options := review.options
	openPath := a.worktreeOpenPath(worktree)
	ctx, cancel := context.WithCancel(context.Background())
	review.cancel = cancel
	go func() {
		defer a.recoverBackground()
		defer cancel()
		var target repository.Session
		ownedByCompletion := false
		defer func() {
			if !ownedByCompletion {
				closeRepositorySession(target)
			}
		}()
		target, err := a.opener.Open(ctx, openPath)
		var snapshot repository.Snapshot
		var finalFiles []repository.CommitFile
		var commits []repository.Commit
		if err == nil {
			snapshot, err = target.Snapshot(ctx)
		}
		if err == nil {
			finalFiles, err = target.FinalFiles(ctx, base, options)
		}
		if err == nil {
			commits, err = target.CommitRange(ctx, base, snapshot.HeadHash, 500, 0)
		}
		ownedByCompletion = true
		a.update(func() {
			if token == a.agents.review.token {
				a.agents.review.cancel = nil
			}
			if generation != a.generation || token != a.agents.review.token ||
				!a.agents.review.open || taskID != a.agents.review.taskID || worktree != a.agents.review.worktree {
				closeRepositorySession(target)
				return
			}
			review := &a.agents.review
			review.loading = false
			if err != nil {
				closeRepositorySession(target)
				review.err = safeError(err)
				return
			}
			review.target = target
			review.head = snapshot.HeadHash
			review.finalFiles = finalFiles
			review.commits = commits
			review.dirtyFiles = snapshot.Files
			if len(finalFiles) > 0 {
				review.selectedFile = 0
			}
			if len(commits) > 0 {
				review.selectedCommit = 0
			}
		})
	}()
}

func (a *app) setAgentReviewMode(mode agentReviewMode) {
	review := &a.agents.review
	if review.mode == mode {
		return
	}
	review.mode = mode
	review.selectedRow = 0
	review.selectedFile = -1
	review.selectedCommitFile = ""
	a.clearAgentReviewDiff()
	switch mode {
	case agentReviewFinal:
		if len(review.finalFiles) > 0 {
			review.selectedFile = 0
		}
	case agentReviewDirty:
		if len(review.dirtyFiles) > 0 {
			review.selectedFile = 0
		}
	case agentReviewCommits:
		if len(review.commits) > 0 {
			review.selectedCommit = 0
			a.requestAgentReviewCommitFiles()
		}
	}
}

func (a *app) selectAgentReviewRow(row int) {
	review := &a.agents.review
	review.selectedRow = row
	a.clearAgentReviewDiff()
	if review.mode == agentReviewCommits {
		if row < 0 || row >= len(review.commits) {
			review.selectedCommit = -1
			review.commitFiles = nil
			return
		}
		if review.selectedCommit != row {
			review.selectedCommit = row
			review.commitFiles = nil
			review.selectedCommitFile = ""
			a.requestAgentReviewCommitFiles()
		}
		return
	}
	count := len(review.finalFiles)
	if review.mode == agentReviewDirty {
		count = len(review.dirtyFiles)
	}
	if row < 0 || row >= count {
		review.selectedFile = -1
		return
	}
	review.selectedFile = row
}

func (a *app) selectedAgentReviewCommit() (repository.Commit, bool) {
	review := &a.agents.review
	if review.selectedCommit < 0 || review.selectedCommit >= len(review.commits) {
		return repository.Commit{}, false
	}
	return review.commits[review.selectedCommit], true
}

func (a *app) requestAgentReviewCommitFiles() {
	review := &a.agents.review
	commit, ok := a.selectedAgentReviewCommit()
	if !ok || review.target == nil || review.commitFiles != nil {
		return
	}
	// An empty successful result is cached separately by the selected commit's
	// identity in diffKey; loading only starts once for that selection.
	key := commit.Hash + ":files"
	if review.diffKey == key {
		return
	}
	review.diffKey = key
	target := review.target
	token, generation := review.token, a.generation
	go func() {
		defer a.recoverBackground()
		files, err := target.CommitFiles(context.Background(), commit)
		a.updateChanged(func() bool {
			current := &a.agents.review
			if generation != a.generation || token != current.token || current.diffKey != key {
				return false
			}
			if err != nil {
				current.diffError = safeError(err)
				return a.workspaceTab == sectionAgents
			}
			if files == nil {
				files = []repository.CommitFile{}
			}
			current.commitFiles = files
			current.diffKey = ""
			current.diffError = ""
			if len(files) > 0 {
				current.selectedCommitFile = files[0].Path
			}
			return a.workspaceTab == sectionAgents
		})
	}()
}

func (a *app) selectedAgentReviewCommitFile() (repository.CommitFile, bool) {
	review := &a.agents.review
	path := review.selectedCommitFile
	for _, file := range review.commitFiles {
		if file.Path == path {
			return file, true
		}
	}
	return repository.CommitFile{}, false
}

// requestAgentReviewDiff derives one key from the mode, commit and final path.
// A late response can therefore never replace a different review selection.
func (a *app) requestAgentReviewDiff() {
	review := &a.agents.review
	if review.loading || review.err != "" || review.target == nil || review.diffLoading {
		return
	}
	var (
		key    string
		path   string
		commit repository.Commit
		file   repository.CommitFile
		dirty  repository.File
	)
	switch review.mode {
	case agentReviewFinal:
		if review.selectedFile < 0 || review.selectedFile >= len(review.finalFiles) {
			return
		}
		file = review.finalFiles[review.selectedFile]
		key, path = "final:"+file.Path+"\x00"+file.PreviousPath, file.Path
	case agentReviewDirty:
		if review.selectedFile < 0 || review.selectedFile >= len(review.dirtyFiles) {
			return
		}
		dirty = review.dirtyFiles[review.selectedFile]
		key, path = fmt.Sprintf("dirty:%s:%t:%t:%s", dirty.Path, dirty.Staged, dirty.Unstaged, dirty.PreviousPath), dirty.Path
	case agentReviewCommits:
		var ok bool
		commit, ok = a.selectedAgentReviewCommit()
		if !ok {
			return
		}
		if review.commitFiles == nil {
			a.requestAgentReviewCommitFiles()
			return
		}
		file, ok = a.selectedAgentReviewCommitFile()
		if !ok {
			return
		}
		key, path = "commit:"+commit.Hash+":"+file.Path+"\x00"+file.PreviousPath, file.Path
	}
	if review.diffKey == key {
		return
	}
	review.diffKey = key
	review.diffPatch = ""
	review.diffState = nil
	review.diffLoading = true
	review.diffError = ""
	target := review.target
	base, options := review.base, review.options
	mode := review.mode
	token, generation := review.token, a.generation
	layout, wrap := a.diffLayout(), a.diffWrap
	go func() {
		defer a.recoverBackground()
		var diff repository.Diff
		var err error
		switch mode {
		case agentReviewFinal:
			diff, err = target.FinalFileDiff(context.Background(), base, file, options)
		case agentReviewDirty:
			diff, err = target.Diff(context.Background(), dirty, options)
		case agentReviewCommits:
			diff, err = target.CommitFileDiff(context.Background(), commit, file, options)
		}
		var state *editor.DiffState
		if err == nil && (diff.Patch != "" || diff.OldText != "" || diff.NewText != "") {
			state, err = buildDiffState(layout, wrap, repository.File{Path: path}, diff, options)
		}
		a.updateChanged(func() bool {
			current := &a.agents.review
			if generation != a.generation || token != current.token || current.diffKey != key {
				return false
			}
			current.diffLoading = false
			if err != nil {
				current.diffError = safeError(err)
				return a.workspaceTab == sectionAgents
			}
			current.diffPatch = diff.Patch
			current.diffState = state
			return a.workspaceTab == sectionAgents
		})
	}()
}

func (a *app) agentReviewFileLabel() string {
	review := &a.agents.review
	if review.mode == agentReviewCommits {
		if file, ok := a.selectedAgentReviewCommitFile(); ok {
			return file.Path
		}
		return ""
	}
	if review.selectedFile < 0 {
		return ""
	}
	if review.mode == agentReviewDirty && review.selectedFile < len(review.dirtyFiles) {
		return review.dirtyFiles[review.selectedFile].Path
	}
	if review.mode == agentReviewFinal && review.selectedFile < len(review.finalFiles) {
		return review.finalFiles[review.selectedFile].Path
	}
	return ""
}

// sendAgentReviewFile hands the exact reviewed base-relative file patch to
// Change Courier. Destination previews and application retain their existing
// clean-apply and destination-identity guards; the source task is unchanged.
func (a *app) sendAgentReviewFile() {
	review := &a.agents.review
	path := a.agentReviewFileLabel()
	if !review.open || path == "" || review.diffPatch == "" || review.diffLoading {
		return
	}
	a.openChangeCourier(review.diffPatch, trf("Agent review · %s", path))
}

func (a *app) agentReviewTask() (agentwork.Task, bool) {
	for _, task := range a.agents.tasks {
		if task.ID == a.agents.review.taskID {
			return task.Clone(), true
		}
	}
	return agentwork.Task{}, false
}
