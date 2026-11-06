package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lazygitapp/internal/repository"
)

// autoRefreshInterval is how often a focused window asks its repository
// whether another app changed it. It is long enough that reading the
// repository costs little, and short enough that a commit made in a terminal
// appears while the user is still looking at the app.
const autoRefreshInterval = 10 * time.Second

// requestQuietRefresh reads the repository in the background and applies what
// changed, without a toast, an error or a selection moving. It does nothing
// while the user is editing a file, filling a form, holding a dialog, picking
// diff lines or resolving a conflict: their work comes first, and the next
// look tries again.
func (a *app) requestQuietRefresh() {
	if a.refreshBusy || a.actionBusy || a.repositoryDetailsLoading {
		// A save that lands during a repository read or action must not be
		// lost. One follow-up read begins when the current work finishes.
		if a.session != nil && a.state == stateReady {
			a.refreshPending = true
		}
		return
	}
	if !a.quietRefreshReady() {
		return
	}
	a.refreshPending = false
	a.refreshToken++
	refreshToken := a.refreshToken
	session := a.session
	// The snapshot the app holds now. A read that started before an action
	// must not land after it and put the old data back.
	generation := a.generation
	sequence := a.snapshotSeq
	a.refreshBusy = true
	go func() {
		defer a.recoverQuietRefresh(refreshToken)
		snapshot, err := session.Snapshot(context.Background())
		a.updateChanged(func() bool {
			if refreshToken != a.refreshToken {
				return false
			}
			defer func() {
				a.refreshBusy = false
				if a.refreshPending {
					a.requestQuietRefresh()
				}
			}()
			if generation != a.generation || sequence != a.snapshotSeq {
				return false
			}
			if err != nil || !a.quietRefreshAllowed() {
				// A repository that cannot be read is not the user's problem
				// until they ask: the refresh stays quiet, and the next look
				// tries again.
				return false
			}
			was, now := signSnapshot(a.snapshot), signSnapshot(snapshot)
			if was == now {
				// Empty directories do not alter the Git signature. Their matching
				// quiet snapshot still completes the operation's refresh cycle.
				a.code.filesOperationRefresh = false
				return false
			}
			a.applyQuietSnapshot(snapshot, was, now)
			return true
		})
	}()
}

// requestAmbientRefresh starts a refresh only when the repository is idle.
// Focus, show, tab and timer notifications describe the state at this moment;
// a snapshot already in flight is already observing it, so queuing another
// read would only amplify repeated OS focus notifications. Filesystem events
// continue to use requestQuietRefresh directly because a save can land after
// an in-flight snapshot has read the changed file.
func (a *app) requestAmbientRefresh() {
	if a.refreshBusy || a.actionBusy || a.repositoryDetailsLoading {
		return
	}
	a.requestQuietRefresh()
}

// recoverQuietRefresh contains a failed repository read just like the generic
// background boundary, but applies its cleanup only to the read that panicked.
// In particular, a fault from a closed repository must not clear the busy flags
// or report an error over its replacement.
func (a *app) recoverQuietRefresh(refreshToken uint64) {
	r := recover()
	if r == nil {
		return
	}
	reason := describeFault(r)
	a.update(func() {
		if refreshToken != a.refreshToken {
			return
		}
		a.reportBackgroundFault(reason)
	})
}

// quietRefreshReady reports whether a quiet refresh may start now: there is a
// repository to read, and the user is not in the middle of something.
func (a *app) quietRefreshReady() bool {
	return a.session != nil && a.state == stateReady && a.quietRefreshAllowed()
}

// quietRefreshAllowed reports whether a snapshot landing now would not
// disturb the user. Reading a commit, a diff or a file's history is not in
// the way: the refresh keeps those choices. Typing, holding a dialog, picking
// diff lines or resolving a conflict is.
func (a *app) quietRefreshAllowed() bool {
	if a.session == nil || a.state != stateReady || a.actionBusy || a.repositoryDetailsLoading {
		return false
	}
	if a.fileEditing || a.modalOpen() {
		return false
	}
	if a.blameState != nil || a.blameLoading {
		return false
	}
	// Picking diff lines is a stage in progress: the pane holds the range and
	// the diff behind it, so a refreshed diff would drop it.
	if a.lineSelectMode {
		return false
	}
	if _, ok := a.selectedLineCount(); ok {
		return false
	}
	// Text typed into the conflict editor is written at the top of the next
	// frame, so a refresh landing before then would drop it. A resolved hunk,
	// on the other hand, is written to the file at once: a change another app
	// makes to a conflict can be taken while the pane is open, and the pane
	// then reads the file as it now stands.
	if a.conflictView != nil && a.conflictView.Buffer != nil && a.conflictView.Buffer.Modified() {
		return false
	}
	return true
}

// applyQuietSnapshot makes snapshot the app's, keeping what the user chose.
// was and now are the signs of the snapshot it replaces and of snapshot, read
// once by the caller. Only the lists a change can move are rebuilt: a commit's
// files and a stash's files are keyed by the commit and the stash, so they read
// again on their own when their choice moves, and never for a snapshot that
// left them alone.
func (a *app) applyQuietSnapshot(snapshot repository.Snapshot, was, now snapshotSigns) {
	operationRefreshed := a.code.filesOperationRefresh
	a.code.filesOperationRefresh = false
	old := a.snapshot
	refsChanged := was.refs != now.refs
	filesChanged := was.files != now.files
	codeFilesChanged := was.codeFiles != now.codeFiles
	selected := a.captureSelection("")

	if !refsChanged {
		// The read returns the snapshot's first page; the pages below it are
		// the ones the list paged in, and nothing about them changed.
		snapshot.Commits = old.Commits
	}
	if a.snapshot.Branch != snapshot.Branch || a.snapshot.Root != snapshot.Root {
		a.commitSelection = nil
	}
	a.snapshot = snapshot
	a.snapshotSeq++
	checkout := a.refreshCodeAfterCheckout(old, snapshot)
	if refsChanged && !checkout {
		a.refreshCodeBlameAfterRepositoryMutation()
	}
	if codeFilesChanged && a.code.filesLoaded && !checkout && !operationRefreshed {
		a.code.filesLoaded = false
		a.requestCodeFiles()
	}
	if refsChanged {
		a.resetCommitPaging(snapshot)
	}
	a.restoreSelection(selected)

	if refsChanged {
		// The branch browse is a read of its own, against a branch that
		// moved: ask for it again, keeping the commit and the file the user
		// was reading.
		if a.workspaceTab == sectionBranches && a.branchCommitsBranch != "" {
			a.branchCommitsKeep = commitHashAt(a.branchCommits, a.selectedBranchCommit)
			if a.selectedBranchCommitFile >= 0 && a.selectedBranchCommitFile < len(a.branchCommitFiles) {
				a.branchCommitFilesKeepPath = a.branchCommitFiles[a.selectedBranchCommitFile].Path
			}
			a.branchCommitsBranch = ""
		}
		// A narrowed list is a snapshot of Git's search, so each filtered
		// page is read again for the changed commits to show.
		if a.historyFilter.narrows() {
			a.runCommitFilter(sectionHistory, selected.commit)
		}
		if a.graphFilter.narrows() {
			a.runCommitFilter(sectionGraph, selected.commit)
		}
	}
	if filesChanged && (selected.path == "" || fileSignature(old, selected.path) != fileSignature(snapshot, selected.path)) {
		// A file can be saved again without its status changing, and the
		// diff on screen is then out of date: read the chosen one again. A
		// change to another file leaves the pane, and any conflict being
		// resolved in it, alone.
		a.beginDiff()
	}
}

// fileState names one file as the working-tree signature does: its status and
// the size and time on disk, or "!" when it is not there.
func fileState(root string, file repository.File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%+v\x00", file)
	info, err := os.Stat(filepath.Join(root, file.Path))
	if err != nil {
		b.WriteString("!")
	} else {
		fmt.Fprintf(&b, "%d:%d", info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
}

// fileSignature names one file of the working tree as filesSignature does: its
// status and the size and time on disk. A file that is no longer changed has an
// empty signature.
func fileSignature(snapshot repository.Snapshot, path string) string {
	for _, file := range snapshot.Files {
		if file.Path == path {
			return fileState(snapshot.Root, file)
		}
	}
	return ""
}

// snapshotSigns names the working tree, refs and durable recovery data that a
// read found.
type snapshotSigns struct {
	files      string
	refs       string
	codeFiles  string
	operations string
	recovery   string
}

// signSnapshot names what a read found, so two reads with the same signs mean
// nothing changed and the frame can be left alone.
func signSnapshot(snapshot repository.Snapshot) snapshotSigns {
	return snapshotSigns{
		files: filesSignature(snapshot), refs: refsSignature(snapshot), codeFiles: codeFilesSignature(snapshot),
		operations: operationsSignature(snapshot), recovery: recoverySignature(snapshot),
	}
}

// operationsSignature and recoverySignature cover their durable records only.
// Unlike relative stash recency, every field is stable until the journal or a
// recovery ref actually changes.
func operationsSignature(snapshot repository.Snapshot) string {
	var b strings.Builder
	for _, record := range snapshot.Operations {
		fmt.Fprintf(&b, "%d\x00%s\x00%s\x00%s\x00%s\x00%t\x00%d\x00%d\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00",
			record.Version, record.ID, record.Kind, record.Summary, record.Status, record.Partial,
			record.CreatedAt, record.FinishedAt, record.UndoneAt, record.Worktree,
			record.Branch, record.BranchAfter, record.HeadBefore, record.HeadAfter,
			record.BeforeState, record.AfterState, record.StashAfter, record.ObjectID,
			record.RefName, record.Message, record.UndoKind)
		fmt.Fprintf(&b, "%s\x00", record.Error)
		writeWorkSnapshotSignature(&b, record.Snapshot)
	}
	return b.String()
}

func recoverySignature(snapshot repository.Snapshot) string {
	var b strings.Builder
	for i := range snapshot.WorkSnapshots {
		writeWorkSnapshotSignature(&b, &snapshot.WorkSnapshots[i])
	}
	return b.String()
}

func writeWorkSnapshotSignature(b *strings.Builder, snapshot *repository.WorkSnapshot) {
	if snapshot == nil {
		b.WriteString("nil\x00")
		return
	}
	fmt.Fprintf(b, "%s\x00%s\x00%s\x00%s\x00%d\x00",
		snapshot.ID, snapshot.Hash, snapshot.Kind, snapshot.Name, snapshot.CreatedAt)
}

// codeFilesSignature names only changes that can alter the Code explorer's
// paths. Saving an already tracked file changes its Git status, size and mtime,
// but not the tree; rebuilding the index for every keystroke would make the
// list flicker and lose its stable scroll state.
func codeFilesSignature(snapshot repository.Snapshot) string {
	var b strings.Builder
	for _, file := range snapshot.Files {
		if file.Tracked && !file.Added && !file.Deleted && file.PreviousPath == "" {
			continue
		}
		fmt.Fprintf(&b, "%s\x00%s\x00%t\x00%t\x00%t\x00",
			file.Path, file.PreviousPath, file.Tracked, file.Added, file.Deleted)
	}
	return b.String()
}

// filesSignature names the working tree: its changes, the operation Git is in
// the middle of, and the size and time of every changed file. The stat is what
// catches a file saved again when its status alone did not move.
func filesSignature(snapshot repository.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00", snapshot.Root, snapshot.Operation)
	for _, file := range snapshot.Files {
		b.WriteString(fileState(snapshot.Root, file))
		b.WriteString("\x00")
	}
	return b.String()
}

// refsSignature names the refs and the lists built from them: the commits, the
// branches, the tags, the stashes, the remotes, the worktrees and the
// submodules. A stash's recency is a relative time, so it is left out: it
// would change on every read without the repository changing.
func refsSignature(snapshot repository.Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%s\x00", snapshot.Branch, snapshot.Head)
	commits := snapshot.Commits
	if len(commits) > repository.SnapshotCommitLimit {
		commits = commits[:repository.SnapshotCommitLimit]
	}
	for _, commit := range commits {
		fmt.Fprintf(&b, "%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%v\x00%v\x00",
			commit.Hash, commit.ShortHash, commit.Subject, commit.AuthorName,
			commit.AuthorEmail, commit.UnixTimestamp, commit.Tags, commit.Parents)
	}
	for _, branch := range snapshot.Branches {
		fmt.Fprintf(&b, "%+v\x00", branch)
	}
	for _, remote := range snapshot.Remotes {
		fmt.Fprintf(&b, "%s\x00%v\x00%v\x00", remote.Name, remote.URLs, remote.PushURLs)
		for _, branch := range remote.Branches {
			fmt.Fprintf(&b, "%s\x00%s\x00", branch.Name, branch.RemoteName)
		}
	}
	for _, stash := range snapshot.Stashes {
		fmt.Fprintf(&b, "%d\x00%s\x00%s\x00", stash.Index, stash.Name, stash.Hash)
	}
	for _, tag := range snapshot.Tags {
		fmt.Fprintf(&b, "%s\x00%s\x00", tag.Name, tag.Message)
	}
	for _, worktree := range snapshot.Worktrees {
		fmt.Fprintf(&b, "%+v\x00", worktree)
	}
	for _, submodule := range snapshot.Submodules {
		fmt.Fprintf(&b, "%+v\x00", submodule)
	}
	for _, commit := range snapshot.Reflog {
		fmt.Fprintf(&b, "%s\x00%s\x00", commit.Hash, commit.Subject)
	}
	return b.String()
}
