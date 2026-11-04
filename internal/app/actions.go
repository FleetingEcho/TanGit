package app

import (
	"errors"
	"fmt"
	"strings"

	"context"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

func (a *app) beginOpen() {
	path := strings.TrimSpace(a.path)
	if path == "" {
		a.state = stateError
		a.errMessage = "Choose a repository folder first."
		return
	}

	// A window with tabs takes the open over when the repository already has
	// a page, or when the window is full: the hook reports it, and nothing of
	// this page's own read starts.
	if a.claimOpen != nil && a.claimOpen(path) {
		return
	}

	if a.cancelOpen != nil {
		a.cancelOpen()
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.cancelOpen = cancel
	a.generation++
	generation := a.generation
	a.state = stateLoading
	a.errMessage = ""

	go func() {
		defer a.recoverBackground()
		a.openRepository(ctx, path, generation)
	}()
}

// openRepository puts a real repository's working tree on screen as soon as
// its core read completes, then fills history and ancillary data in a second
// read. Sessions that do not offer the progressive path keep their original
// one-snapshot behavior, including the fakes used by actions and UI tests.
func (a *app) openRepository(ctx context.Context, path string, generation uint64) {
	session, err := a.opener.Open(ctx, path)
	if err != nil {
		a.update(func() { a.setOpenError(path, generation, err) })
		return
	}
	progressive, ok := session.(repository.ProgressiveSession)
	if !ok {
		snapshot, err := session.Snapshot(ctx)
		a.update(func() {
			if generation != a.generation {
				return
			}
			if err != nil {
				a.setOpenError(path, generation, err)
				return
			}
			a.cancelOpen = nil
			a.installOpenedRepository(session, snapshot)
		})
		return
	}

	core, coreErr := progressive.CoreSnapshot(ctx)
	coreReady := make(chan uint64, 1)
	a.update(func() {
		if generation != a.generation {
			coreReady <- 0
			return
		}
		if coreErr != nil {
			a.setOpenError(path, generation, coreErr)
			coreReady <- 0
			return
		}
		a.installOpenedRepository(session, core)
		a.repositoryDetailsLoading = true
		coreReady <- a.snapshotSeq
	})
	coreSequence := <-coreReady
	if coreSequence == 0 {
		return
	}

	details, detailsErr := progressive.DetailsSnapshot(ctx)
	a.update(func() {
		if generation != a.generation {
			return
		}
		a.cancelOpen = nil
		if !a.repositoryDetailsLoading {
			return
		}
		a.repositoryDetailsLoading = false
		defer func() {
			if a.refreshPending {
				a.requestQuietRefresh()
			}
		}()
		// An action reads a complete replacement snapshot. Its sequence change
		// (or still-running state) makes this older deferred read irrelevant.
		if coreSequence != a.snapshotSeq || a.actionBusy {
			return
		}
		if detailsErr != nil {
			err := fmt.Errorf("load repository details: %w", detailsErr)
			a.actionError = err.Error()
			a.notifyError(err)
			return
		}
		selected := a.captureSelection("")
		a.snapshot = a.snapshot.WithDetails(details)
		// The core has no branches, so its first default-filter attempt could
		// not name the checked-out branch. Now that details supply it, start
		// both commit pages on that branch as a full-snapshot open does.
		a.applyDefaultCommitFilters(a.snapshot)
		a.snapshotSeq++
		a.resetCommitPaging(a.snapshot)
		a.restoreSelection(selected)
	})
}

// setOpenError handles a failed initial or core read on the UI loop.
func (a *app) setOpenError(path string, generation uint64, err error) {
	if generation != a.generation {
		return
	}
	a.cancelOpen = nil
	if errors.Is(err, repository.ErrNotRepository) {
		// The folder is plain: offer to make it a repository instead of
		// showing Git's own message.
		a.state = stateEmpty
		a.errMessage = ""
		a.offerInitPath = path
		return
	}
	a.state = stateError
	a.errMessage = err.Error()
}

// installOpenedRepository establishes the state shared by legacy and core
// opening. The sequence changes even for the core so deferred or quiet reads
// from an older snapshot cannot land over it.
func (a *app) installOpenedRepository(session repository.Session, snapshot repository.Snapshot) {
	a.state = stateReady
	a.session = session
	a.commandLog = session.CommandLog()
	a.commandLogOpen = false
	a.confirm = nil
	a.snapshot = snapshot
	a.snapshotSeq++
	a.repositoryDetailsLoading = false
	a.path = snapshot.Root
	a.rememberOpened(snapshot.Root)
	a.resetRepositoryState(snapshot)
	a.startRepositoryWatch(session)
	a.refreshWindowTitle()
}

// resetRepositoryState clears everything a frame would read from the
// repository that was open before, so a new one starts with its own choices.
func (a *app) resetRepositoryState(snapshot repository.Snapshot) {
	// The first frame of the Changes section chooses its first file and
	// reads its diff, so nothing here reads the old repository's rows.
	a.changeRows = nil
	a.selectedFile = -1
	// The commit list starts at the page the snapshot carries; more pages
	// exist only when that page filled.
	a.resetCommitPaging(snapshot)
	// A new repository's filters are its own: an old filter's branch and its
	// authors belong to the repository that was open before. Each commit
	// page then starts on the branch checked out, which reading the two
	// gives it; the empty snapshot of a closed repository has no branch to
	// default to, and keeps every commit.
	a.historyFilter.reset()
	a.graphFilter.reset()
	a.applyDefaultCommitFilters(snapshot)
	a.filterOpen = false
	a.authorsToken++
	a.authors = nil
	a.authorsLoading = false
	a.authorsError = ""
	a.branchFilter = repository.CommitFilter{}
	a.branchFilterDraft = repository.CommitFilter{}
	// A new repository shows every change and both sections open.
	a.fileSearch = ""
	a.ignoreUntracked = false
	a.fileTreeCollapsed = nil
	a.stagedExpanded, a.changesExpanded = true, true
	a.resetFileDiff()
	a.closeFileHistory()
	a.selectedCommit = -1
	if len(snapshot.Commits) > 0 {
		a.selectedCommit = 0
	}
	a.branchSearch = ""
	a.selectedBranchRow, a.selectedBranch = -1, -1
	if len(snapshot.Branches) > 0 {
		a.selectedBranchRow, a.selectedBranch = 0, 0
	}
	a.remoteFilter = ""
	a.selectedRemoteRow, a.selectedRemote = -1, -1
	a.selectedRemoteBranchRow, a.selectedRemoteBranch = -1, -1
	a.remoteBranchFilter = ""
	if len(snapshot.Remotes) > 0 {
		a.selectedRemoteRow, a.selectedRemote = 0, 0
		if len(snapshot.Remotes[0].Branches) > 0 {
			a.selectedRemoteBranch, a.selectedRemoteBranchRow = 0, 0
		}
	}
	a.stashFilter = ""
	a.selectedStashRow, a.selectedStash = -1, -1
	if len(snapshot.Stashes) > 0 {
		a.selectedStashRow, a.selectedStash = 0, 0
	}
	a.resetStashDetails()
	a.selectedTag = -1
	a.selectedTagRow = -1
	a.tagFilter = ""
	if len(snapshot.Tags) > 0 {
		a.selectedTag, a.selectedTagRow = 0, 0
	}
	a.worktreeFilter = ""
	a.selectedWorktreeRow, a.selectedWorktree = -1, -1
	if len(snapshot.Worktrees) > 0 {
		a.selectedWorktreeRow, a.selectedWorktree = 0, 0
	}
	a.submoduleFilter = ""
	a.selectedSubmoduleRow, a.selectedSubmodule = -1, -1
	if len(snapshot.Submodules) > 0 {
		a.selectedSubmoduleRow, a.selectedSubmodule = 0, 0
	}
	a.reflogFilter = ""
	a.selectedReflogRow, a.selectedReflog = -1, -1
	if len(snapshot.Reflog) > 0 {
		a.selectedReflogRow, a.selectedReflog = 0, 0
	}
	a.resetBranchCommits()
	a.resetCommitDetails()
	a.resetComparison()
}

// resetCommitPaging starts the commit list at the page a snapshot carries:
// another page exists only when that page filled. A page read in flight is
// dropped, because the snapshot replaced the list it was reading for.
func (a *app) resetCommitPaging(snapshot repository.Snapshot) {
	a.commitsMore = len(snapshot.Commits) >= repository.SnapshotCommitLimit
	a.commitsLoading = false
	a.commitsError = ""
	a.commitsToken++
}

// resetFileDiff drops the working-tree diff and anything reading one.
func (a *app) resetFileDiff() {
	if a.cancelDiff != nil {
		a.cancelDiff()
		a.cancelDiff = nil
	}
	a.diffGeneration++
	a.diffKey = ""
	a.diffKind = ""
	a.diffPatch = ""
	a.diffState = nil
	a.diffLoading = false
	a.diffError = ""
	a.conflictState = nil
	a.conflictView = nil
	a.conflictSides = nil
	a.conflictScrollTo = -1
	a.conflictSaveError = ""
	a.conflictSaveVersion = 0
	a.conflictDiffState = nil
	a.conflictDiffPatch = ""
	a.resetFileEdit()
	a.lineSelectMode = false
	a.lineSelectRange, a.lineSelectHas = editor.DiffLineRange{}, false
}

// beginDiff reads the chosen change's diff again, as an action that rewrote
// the working tree needs.
func (a *app) beginDiff() {
	a.blameGeneration++
	a.blameState = nil
	a.blameLoading = false
	a.blameError = ""
	a.resetFileDiff()
	a.requestFileDiff()
}

// requestFileDiff reads the diff of the chosen change unless the pane already
// shows or is reading it. The detail pane asks on every frame, so a filter, a
// folded section or an action that moved the choice re-reads the diff without
// a click.
func (a *app) requestFileDiff() {
	change, ok := a.selectedChange()
	if a.session == nil || !ok {
		a.resetFileDiff()
		return
	}
	file := change.diffFile()
	key := file.Path + "\x00" + change.sideKey()
	if file.HasMergeConflicts {
		// A conflicted file is resolved, not diffed: the pane shows the
		// file itself, so the read is keyed by its path alone.
		key = "conflict\x00" + file.Path
	}
	if a.diffKey == key {
		return
	}

	session := a.session
	repositoryGeneration := a.generation
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelDiff = cancel
	a.diffGeneration++
	generation := a.diffGeneration
	a.diffKey = key
	a.diffLoading = true
	a.diffError = ""
	a.diffState = nil
	a.conflictState = nil
	a.conflictView = nil
	a.conflictSides = nil
	a.conflictSaveError = ""
	a.conflictSaveVersion = 0
	a.conflictDiffState = nil
	a.conflictDiffPatch = ""

	if file.HasMergeConflicts {
		a.requestConflictFile(file, generation)
		return
	}

	// Settings belong to the UI thread. Carry the options this request used
	// into its worker rather than reading app fields there.
	layout, wrap := a.diffLayout(), a.diffWrap
	options := a.diffOptions
	go func() {
		defer a.recoverBackground()
		diff, err := session.Diff(ctx, file, options)
		var state *editor.DiffState
		if err == nil {
			state, err = buildDiffState(layout, wrap, file, diff, options)
			if err == nil && state != nil {
				state.LineSelection = canonicalDiffOptions(options) && diffSupportsLineStaging(file, diff.Kind)
				if state.LineSelection {
					// Clicks on the numbers are reported back so the mode
					// that picks a range can extend it. The state is passed
					// along because a newer diff may have replaced this one
					// by the time a click lands.
					state.OnLineSelection = func(clicked editor.DiffLineRange) {
						a.extendLineSelection(state, clicked)
					}
				}
			}
		}
		a.update(func() {
			if generation != a.diffGeneration || repositoryGeneration != a.generation {
				return
			}
			a.cancelDiff = nil
			a.diffLoading = false
			if err != nil {
				a.diffError = err.Error()
				return
			}
			a.diffKind = diff.Kind
			a.diffPatch = diff.Patch
			a.diffState = state
		})
	}()
}

func (a *app) beginBlame() {
	change, ok := a.selectedChange()
	if a.session == nil || !ok {
		return
	}
	file := change.file
	session := a.session
	repositoryGeneration := a.generation
	a.blameGeneration++
	blameGeneration := a.blameGeneration
	a.blameLoading = true
	a.blameError = ""
	a.blameState = nil
	go func() {
		defer a.recoverBackground()
		output, err := session.BlameFile(context.Background(), file)
		var state *editor.State
		if err == nil {
			buffer := editor.NewBuffer(output)
			state = &editor.State{Buffer: buffer, ReadOnly: true, SoftWrap: false, Label: "Git blame output"}
		}
		a.update(func() {
			if repositoryGeneration != a.generation || blameGeneration != a.blameGeneration {
				return
			}
			a.blameLoading = false
			if err != nil {
				a.blameError = err.Error()
				return
			}
			a.blameState = state
		})
	}()
}

func (a *app) beginFileAction(stage bool) {
	change, ok := a.selectedChange()
	if a.actionBusy || a.session == nil || !ok {
		return
	}
	a.beginStageFile(change.file, stage)
}

func (a *app) beginRepositoryAction(
	keepPath string,
	action func(context.Context, repository.Session) error,
	onSuccess func(),
) {
	a.beginRepositoryActionMessage("", keepPath, action, onSuccess)
}

// beginRepositoryActionMessage runs an action and, when it succeeds, says so
// with message: the silent actions are the ones whose own pane already
// shows the result, while a push or a delete deserves a notification.
func (a *app) beginRepositoryActionMessage(
	message string,
	keepPath string,
	action func(context.Context, repository.Session) error,
	onSuccess func(),
) {
	if a.actionBusy || a.session == nil {
		return
	}

	session := a.session
	repositoryGeneration := a.generation
	// The lists keep their place by what they show, not by index: an action
	// or a refresh that moves a row must not move the user's choice with it.
	selected := a.captureSelection(keepPath)
	selectedCommitHash := selected.commit
	a.actionBusy = true
	a.actionError = ""

	go func() {
		defer a.recoverBackground()
		ctx := context.Background()
		err := action(ctx, session)
		var snapshot repository.Snapshot
		read := false
		if err == nil {
			var snapshotErr error
			snapshot, snapshotErr = session.Snapshot(ctx)
			if snapshotErr != nil {
				err = snapshotErr
			} else {
				read = true
			}
		} else if left, snapshotErr := session.Snapshot(ctx); snapshotErr == nil {
			// A command that stops on a conflict exits with an error while it
			// leaves the working tree changed: a merge, rebase, cherry-pick or
			// revert waiting to be resolved. Read what it left, so the Changes
			// section shows the conflict rather than the state before the
			// action, and switching to it after the error needs no refresh.
			snapshot = left
			read = true
		}
		a.update(func() {
			if repositoryGeneration != a.generation {
				return
			}
			a.actionBusy = false
			defer func() {
				if a.refreshPending {
					a.requestQuietRefresh()
				}
			}()
			if read {
				a.repositoryDetailsLoading = false
				a.snapshot = snapshot
				a.snapshotSeq++
				a.resetCommitPaging(snapshot)
				a.restoreSelection(selected)
				// An action changed the commits, so the branch browse must read
				// them again.
				a.resetBranchCommits()
				a.resetCommitDetails()
				// A narrowed commit list is a snapshot of Git's search, so each
				// page that is filtered must be read again for the action's
				// commits to show.
				if a.historyFilter.narrows() {
					a.runCommitFilter(sectionHistory, selectedCommitHash)
				}
				if a.graphFilter.narrows() {
					a.runCommitFilter(sectionGraph, selectedCommitHash)
				}
				a.beginDiff()
				a.resetStashDetails()
			}
			if err != nil {
				a.actionError = err.Error()
				if a.commandLog != nil {
					a.commandLog.Note(err.Error())
				}
				a.notifyError(err)
				return
			}
			if onSuccess != nil {
				onSuccess()
			}
			if message != "" {
				a.notify("success", message, "")
			}
		})
	}()
}

func (a *app) beginRefresh() {
	selectedPath := ""
	if row, ok := a.selectedChange(); ok {
		selectedPath = row.file.Path
	}
	a.beginRepositoryAction(selectedPath, func(context.Context, repository.Session) error {
		return nil
	}, nil)
}

// repositorySelection is what the sections show, named rather than indexed:
// the chosen file and the side of the working tree it is on, then the chosen
// commit, branch, stash, tag, remote, worktree and submodule. A snapshot read
// after an action, or a background refresh, puts it back so the lists move
// under the data rather than under the user.
type reflogSelection struct {
	hash      string
	subject   string
	timestamp int64
}

type repositorySelection struct {
	path         string
	staged       bool
	branch       string
	commit       string
	remote       string
	remoteBranch string
	stash        string
	tag          string
	worktree     string
	submodule    string
	reflog       reflogSelection
}

// captureSelection names what the sections show now. A caller naming a file
// keeps that file, as a row's icon does; otherwise the chosen file is kept on
// the side of the working tree it is on.
func (a *app) captureSelection(keepPath string) repositorySelection {
	selected := repositorySelection{path: keepPath}
	if row, ok := a.selectedChange(); ok {
		if selected.path == "" {
			selected.path, selected.staged = row.file.Path, row.staged
		} else if row.file.Path == selected.path {
			selected.staged = row.staged
		}
	}
	if a.selectedBranch >= 0 && a.selectedBranch < len(a.snapshot.Branches) {
		selected.branch = a.snapshot.Branches[a.selectedBranch].Name
	}
	selected.commit = commitHashAt(a.visibleCommits(), a.selectedCommit)
	if a.selectedRemote >= 0 && a.selectedRemote < len(a.snapshot.Remotes) {
		selected.remote = a.snapshot.Remotes[a.selectedRemote].Name
		branches := a.snapshot.Remotes[a.selectedRemote].Branches
		if a.selectedRemoteBranch >= 0 && a.selectedRemoteBranch < len(branches) {
			selected.remoteBranch = branches[a.selectedRemoteBranch].Name
		}
	}
	if a.selectedStash >= 0 && a.selectedStash < len(a.snapshot.Stashes) {
		selected.stash = a.snapshot.Stashes[a.selectedStash].Hash
	}
	if a.selectedTag >= 0 && a.selectedTag < len(a.snapshot.Tags) {
		selected.tag = a.snapshot.Tags[a.selectedTag].Name
	}
	if a.selectedWorktree >= 0 && a.selectedWorktree < len(a.snapshot.Worktrees) {
		selected.worktree = a.snapshot.Worktrees[a.selectedWorktree].Path
	}
	if a.selectedSubmodule >= 0 && a.selectedSubmodule < len(a.snapshot.Submodules) {
		selected.submodule = a.snapshot.Submodules[a.selectedSubmodule].Path
	}
	if a.selectedReflog >= 0 && a.selectedReflog < len(a.snapshot.Reflog) {
		entry := a.snapshot.Reflog[a.selectedReflog]
		selected.reflog = reflogSelection{entry.Hash, entry.Subject, entry.UnixTimestamp}
	}
	return selected
}

// restoreSelection puts the choices captureSelection named back on the
// snapshot the app now holds, rebuilding the Changes rows around them.
func (a *app) restoreSelection(selected repositorySelection) {
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, selected.path, selected.staged)
	if a.selectedFile < 0 {
		a.selectedFile = firstChangeFile(a.changeRows)
	}
	a.selectedBranch = selectedBranchIndex(a.snapshot.Branches, selected.branch)
	a.syncBranchRow()
	a.selectedCommit = selectedCommitIndex(a.visibleCommits(), selected.commit)
	a.selectedRemote = selectedRemoteIndex(a.snapshot.Remotes, selected.remote)
	if a.selectedRemote >= 0 {
		a.selectedRemoteBranch = selectedRemoteBranchIndex(a.snapshot.Remotes[a.selectedRemote].Branches, selected.remoteBranch)
	} else {
		a.selectedRemoteBranch = -1
	}
	a.syncRemoteRow()
	a.selectedStash = selectedStashIndex(a.snapshot.Stashes, selected.stash)
	a.syncStashRow()
	a.selectedTag = selectedTagIndex(a.snapshot.Tags, selected.tag)
	a.syncTagRow()
	a.selectedWorktree = selectedWorktreeIndex(a.snapshot.Worktrees, selected.worktree)
	a.syncWorktreeRow()
	a.selectedSubmodule = selectedSubmoduleIndex(a.snapshot.Submodules, selected.submodule)
	a.syncSubmoduleRow()
	a.selectedReflog = selectedReflogIndex(a.snapshot.Reflog, selected.reflog)
	a.syncReflogRow()
}

func selectedBranchIndex(branches []repository.Branch, name string) int {
	for i, branch := range branches {
		if branch.Name == name {
			return i
		}
	}
	if len(branches) > 0 {
		return 0
	}
	return -1
}

func selectedCommitIndex(commits []repository.Commit, hash string) int {
	for i, commit := range commits {
		if commit.Hash == hash {
			return i
		}
	}
	if len(commits) > 0 {
		return 0
	}
	return -1
}

func selectedRemoteIndex(remotes []repository.Remote, name string) int {
	for i, remote := range remotes {
		if remote.Name == name {
			return i
		}
	}
	if len(remotes) > 0 {
		return 0
	}
	return -1
}

func selectedRemoteBranchIndex(branches []repository.RemoteBranch, name string) int {
	for i, branch := range branches {
		if branch.Name == name {
			return i
		}
	}
	if len(branches) > 0 {
		return 0
	}
	return -1
}

func selectedStashIndex(stashes []repository.Stash, hash string) int {
	for i, stash := range stashes {
		if stash.Hash == hash {
			return i
		}
	}
	if len(stashes) > 0 {
		return 0
	}
	return -1
}

func selectedTagIndex(tags []repository.Tag, name string) int {
	for i, tag := range tags {
		if tag.Name == name {
			return i
		}
	}
	if len(tags) > 0 {
		return 0
	}
	return -1
}

func selectedWorktreeIndex(worktrees []repository.Worktree, path string) int {
	for i, worktree := range worktrees {
		if worktree.Path == path {
			return i
		}
	}
	if len(worktrees) > 0 {
		return 0
	}
	return -1
}

func selectedSubmoduleIndex(submodules []repository.Submodule, path string) int {
	for i, submodule := range submodules {
		if submodule.Path == path {
			return i
		}
	}
	if len(submodules) > 0 {
		return 0
	}
	return -1
}

func selectedReflogIndex(entries []repository.Commit, selected reflogSelection) int {
	for i, entry := range entries {
		if entry.Hash == selected.hash && entry.Subject == selected.subject &&
			entry.UnixTimestamp == selected.timestamp {
			return i
		}
	}
	if len(entries) > 0 {
		return 0
	}
	return -1
}

func (a *app) browse() {
	go func() {
		defer a.recoverBackground()
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:      a.window,
			Title:       "Open Git Repository",
			DefaultPath: a.path,
			ButtonLabel: "Open",
			Directory:   true,
		})
		a.update(func() {
			if err != nil {
				a.state = stateError
				a.errMessage = err.Error()
				return
			}
			if len(paths) == 0 {
				return
			}
			a.path = paths[0]
			a.beginOpen()
		})
	}()
}

// cancelLoading stops the open in flight and puts the picker back, for a read
// that takes longer than the user wants to wait.
func (a *app) cancelLoading() {
	if a.cancelOpen != nil {
		a.cancelOpen()
		a.cancelOpen = nil
	}
	a.generation++
	a.state = stateEmpty
	a.errMessage = ""
	a.offerInitPath = ""
}

// initializeRepository adopts the folder the picker offered: it makes a Git
// repository there, then opens it like any other.
func (a *app) initializeRepository() {
	path := a.offerInitPath
	if path == "" {
		return
	}
	a.offerInitPath = ""
	a.state = stateLoading
	a.errMessage = ""
	a.generation++
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		err := a.opener.Init(context.Background(), path)
		a.update(func() {
			if generation != a.generation {
				return
			}
			if err != nil {
				a.state = stateError
				a.errMessage = err.Error()
				return
			}
			a.path = path
			a.beginOpen()
		})
	}()
}

// dismissInitOffer leaves a plain folder alone, back on the picker.
func (a *app) dismissInitOffer() {
	a.offerInitPath = ""
}

// chooseAnotherFolder drops the init offer and opens the folder dialog, for a
// folder that turned out to be the wrong one.
func (a *app) chooseAnotherFolder() {
	a.offerInitPath = ""
	a.browse()
}

// closeRepository puts the open repository aside: in a window with tabs that
// is closing the page, and on its own it leaves the picker in place.
func (a *app) closeRepository() {
	if a.closeTab != nil {
		a.closeTab()
		return
	}
	a.shutdown()
}

// shutdown releases everything the open repository held, leaving the app on
// the picker, so another repository can be opened without restarting.
func (a *app) shutdown() {
	a.stopRepositoryWatch()
	if a.cancelOpen != nil {
		a.cancelOpen()
		a.cancelOpen = nil
	}
	a.generation++
	a.invalidateHomeBackgroundWork()
	// The pane's editors are written before their state goes, as closing the
	// window leaves no frame to save them.
	a.flushEdits()
	a.actionBusy = false
	a.actionError = ""
	a.repositoryDetailsLoading = false
	a.refreshToken++
	a.refreshBusy = false
	a.refreshPending = false
	a.resetTerminal()
	a.diffFullscreen = false
	a.closeAllForms()
	a.resetRepositoryState(repository.Snapshot{})
	a.session = nil
	a.commandLog = nil
	a.commandLogOpen = false
	a.confirm = nil
	a.pendingToasts = nil
	a.snapshot = repository.Snapshot{}
	a.state = stateEmpty
	a.path = ""
	a.errMessage = ""
	a.offerInitPath = ""
	a.refreshWindowTitle()
}

// refreshWindowTitle names the repository in the window's title when the app
// owns the window. A page of a tabbed window leaves the title to the shell,
// which names whichever page is on screen.
func (a *app) refreshWindowTitle() {
	if a.window == nil || a.closeTab != nil {
		return
	}
	title := appName
	if a.state == stateReady && a.snapshot.Root != "" {
		title = repoName(a.snapshot.Root) + " — " + appName
	}
	a.window.SetTitle(title)
}
