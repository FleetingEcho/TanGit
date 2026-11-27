package repository

import "context"

// ProgressiveCommitSession publishes immutable commit metadata before optional
// line statistics and other repository details are ready.
type ProgressiveCommitSession interface {
	DetailsSnapshotWithCommitPage(context.Context, func([]Commit)) (Snapshot, error)
	SearchCommitsProgressively(context.Context, CommitFilter, func([]Commit)) ([]Commit, error)
}

// HistorySession reads and rewrites commit history without exposing working-tree mutations.
type HistorySession interface {
	// RecentCommits reads up to limit commits reachable from any ref, newest
	// first, skipping the newest skip, so History and the Graph page in
	// below the page a snapshot carries.
	RecentCommits(context.Context, int, int) ([]Commit, error)
	// Undo and Redo reverse and reapply eligible HEAD-reflog actions. Their
	// markers persist in Git, so replay remains balanced across app restarts.
	Undo(context.Context) error
	Redo(context.Context) error
	// UndoExpected and RedoExpected replay only the exact reflog action a
	// confirmation approved; a changed or unavailable preview fails closed.
	UndoExpected(context.Context, string) error
	RedoExpected(context.Context, string) error
	CheckoutCommit(context.Context, Commit) error
	CherryPickCommits(context.Context, []Commit) error
	EditCommitRange(context.Context, []Commit, CommitRangeAction) error
	// CommitMessage reads both parts of a commit's message for history editing.
	CommitMessage(context.Context, Commit) (CommitMessage, error)
	// RewordCommit changes only the selected commit's message, rebasing later
	// commits when the selection is older than HEAD.
	RewordCommit(context.Context, Commit, string, string) error
	// DropCommit removes a commit from the current branch's history.
	DropCommit(context.Context, Commit) error
	// MoveCommit reorders a commit one place toward newer or older history.
	MoveCommit(context.Context, Commit, CommitMove) error
	// CombineCommit squashes or fixes up a commit into its older neighbor.
	CombineCommit(context.Context, Commit, CombineMode) error
	// AmendCommit folds staged changes into the selected commit, including an
	// older commit, without changing its message.
	AmendCommit(context.Context, Commit) error
	// CreateFixupCommit records staged changes as a fixup of the selected commit.
	CreateFixupCommit(context.Context, Commit) error
	// ApplyFixupCommits autosquashes fixup commits from the selected commit
	// through HEAD, rewriting that range once.
	ApplyFixupCommits(context.Context, Commit) error
	CherryPickCommit(context.Context, Commit) error
	RevertCommit(context.Context, Commit) error
	// UndoLatestCommit removes exactly the approved HEAD commit, refusing if
	// HEAD changed or an operation is in progress before the reset starts.
	UndoLatestCommit(context.Context, string, Commit, ResetMode) error
	// ResetToCommit moves the branch to commit, with mode deciding what the
	// index and working tree retain.
	ResetToCommit(context.Context, Commit, ResetMode) error
	// BisectStart begins a bisect at badRef, optionally seeding goodRef as
	// known-good. BisectMark marks ref (or the commit under test) good, bad
	// or skipped; it returns the first bad commit when the bisect completes.
	// BisectReset ends the session and returns to the original HEAD.
	BisectStart(context.Context, string, string) error
	BisectMark(context.Context, string, string) (string, error)
	BisectReset(context.Context) error
	// MoveUnpushedCommits creates newBranch at HEAD and resets the current
	// branch back to its fork point from the upstream (or the default
	// branch), moving the unpushed commits onto newBranch. It returns how
	// many commits moved.
	MoveUnpushedCommits(context.Context, string) (int, error)
	// EditCommit pauses an interactive rebase on the commit so the user can
	// change it; ContinueOperation resumes afterwards.
	EditCommit(context.Context, Commit) error
	// ExportPatches writes one mailbox-format patch per commit into the
	// directory (created if needed) and returns the written paths, oldest
	// first.
	ExportPatches(context.Context, []Commit, string) ([]string, error)
	// AmPatch applies a mailbox-format patch (or a git am mailbox) with a
	// three-way fallback, preserving authorship.
	AmPatch(context.Context, string, []byte) error
	BlameFile(context.Context, File) ([]BlameLine, error)
	// BlameWorkingTreeFile attributes saved working-tree lines, including new
	// or changed lines that have no commit identity yet.
	BlameWorkingTreeFile(context.Context, string) ([]BlameLine, error)
	// BlameFileRevision reads an immutable blob and its blame at revision.
	BlameFileRevision(context.Context, string, string) (RevisionBlame, error)
	CommitFiles(context.Context, Commit) ([]CommitFile, error)
	// CommitFilesWithoutStats reads paths and change statuses for file history.
	CommitFilesWithoutStats(context.Context, Commit) ([]CommitFile, error)
	CommitFileDiff(context.Context, Commit, CommitFile, DiffOptions) (Diff, error)
	// CommitFilePatch reads one canonical, apply-ready patch with fixed three-line
	// context, binary literals and full object IDs, independent of display options.
	CommitFilePatch(context.Context, Commit, CommitFile) (string, error)
	// WorkingTreeFilePatch reads the selected file side as an apply-ready patch.
	// A file with both sides is compared from HEAD to its final worktree state.
	WorkingTreeFilePatch(context.Context, File) (string, error)
	// RestoreCommitFile replaces working-tree content with the commit's
	// version without changing the index or creating a commit.
	RestoreCommitFile(context.Context, Commit, CommitFile) error
	// CommitRange reads base..head, where head is the commit captured when an
	// agent review started rather than a branch that may have moved since.
	CommitRange(context.Context, string, string, int, int) ([]Commit, error)
	// FinalFiles compares base directly with the current index and tracked
	// worktree, then adds untracked files. FinalFileDiff reads the same final
	// change for one file without compositing an intermediate HEAD diff.
	FinalFiles(context.Context, string, DiffOptions) ([]CommitFile, error)
	FinalFileDiff(context.Context, string, CommitFile, DiffOptions) (Diff, error)
	// CompareFiles lists the files whose trees differ from the first ref to
	// the second. CompareDiff reads their whole patch and CompareFileDiff reads
	// one file without changing the checkout, index, or working tree.
	CompareFiles(context.Context, string, string, DiffOptions) ([]CommitFile, error)
	CompareDiff(context.Context, string, string, DiffOptions) (Diff, error)
	CompareFileDiff(context.Context, string, string, CommitFile, DiffOptions) (Diff, error)
	// BranchCommits reads the commits reachable from a branch without
	// checking it out, so another branch's history is browsable from the
	// working tree the user is on.
	BranchCommits(context.Context, string, int) ([]Commit, error)
	// SearchCommits reads up to filter.Limit commits matching the filter,
	// newest first, so History and a branch's commits can be narrowed by
	// message, author and branch.
	SearchCommits(context.Context, CommitFilter) ([]Commit, error)
	// Authors lists the distinct author names in the repository's history,
	// sorted, for the filter's author choices.
	Authors(context.Context) ([]string, error)
	// FileCommits reads up to limit commits that changed a path, newest
	// first, skipping the newest skip, following renames, so the file
	// history of a changed file is browsable and pages in as it scrolls.
	// The returned commits omit line counts.
	FileCommits(context.Context, string, int, int) ([]Commit, error)
	// FileCommitsIncluding reads one Git-ordered union of the checkout's path
	// history and the path history at the selected commit. It keeps later edits
	// visible when the drawer opens from an old commit or another branch.
	FileCommitsIncluding(context.Context, string, string, int, int) ([]Commit, error)
}

// ChangesSession reads and mutates the working tree, index and conflict buffers.
type ChangesSession interface {
	Diff(context.Context, File, DiffOptions) (Diff, error)
	// Media reads the two raw image versions of a binary diff without filters
	// or network access. Local LFS objects are used only when already present.
	Media(context.Context, MediaRequest) (MediaPair, error)
	Stage(context.Context, File) error
	Unstage(context.Context, File) error
	// ApplyCachedPatch puts the lines of a partial diff into the index, or,
	// when reverse is set, takes them back out of it. It is how a selection of
	// a file's diff is staged or unstaged without the rest of the file.
	ApplyCachedPatch(context.Context, string, bool) error
	// ApplyWorkingTreePatch applies a canonical patch to files only, leaving
	// the index untouched. Reverse is used to discard selected hunks.
	ApplyWorkingTreePatch(context.Context, string, bool) error
	// PreviewPatch checks an external worktree patch without changing Git.
	// PreviewPatchText is its ordinary in-memory form. PreviewPatchRequest
	// additionally binds index/worktree destination, direction, and three-way
	// behavior. ApplyPatch accepts only that exact private, state-digested
	// preview; public preview text is display data, not mutation authority.
	PreviewPatch(context.Context, string) (PatchPreview, error)
	PreviewPatchText(context.Context, string) (PatchPreview, error)
	PreviewPatchRequest(context.Context, PatchRequest) (PatchPreview, error)
	ApplyPatch(context.Context, PatchPreview) error
	StageAll(context.Context) error
	UnstageAll(context.Context) error
	Commit(context.Context, string, string, bool) error
	Discard(context.Context, File) error
	// DiscardFiles captures one recovery entry before removing the selection.
	DiscardFiles(context.Context, []File) error
	// IgnoreFile and ExcludeFile hide an untracked path through the shared
	// .gitignore or this clone's .git/info/exclude respectively.
	IgnoreFile(context.Context, File) error
	ExcludeFile(context.Context, File) error
	ResolveConflict(context.Context, File, bool) error
	// ConflictStages reads index stages without changing the index or worktree.
	ConflictStages(context.Context, File) (ConflictStages, error)
}

// BranchRemoteSession manages local branches, their upstreams and remotes.
type BranchRemoteSession interface {
	CheckoutBranch(context.Context, string) error
	CreateBranch(context.Context, string, string) error
	DeleteBranch(context.Context, string, bool) error
	RenameBranch(context.Context, string, string) error
	MergeBranch(context.Context, string) error
	Fetch(context.Context) error
	FetchRemote(context.Context, string) error
	Pull(context.Context) error
	PullWithStrategy(context.Context, PullStrategy) error
	Push(context.Context) error
	ForcePushWithLease(context.Context) error
	// PushFollowTags pushes the branch like Push and also carries tags
	// pointing at the commits being pushed (--follow-tags).
	PushFollowTags(context.Context) error
	// PushAllTags publishes every tag to the remote the current branch
	// would push to.
	PushAllTags(context.Context) error
	// StackedBranches returns the local branches below the current one in a
	// stacked workflow: ancestors of HEAD that are ahead of their upstream,
	// oldest first. Empty when the branch has no stack below it.
	StackedBranches(context.Context) ([]string, error)
	// PushStack pushes the given branches in order, oldest first, using the
	// same upstream and force rules as Push (never forced).
	PushStack(context.Context, []string) error
	// FetchPrune fetches every remote and prunes remote-tracking branches
	// whose upstream disappeared.
	FetchPrune(context.Context) error
	// PullBranch brings the latest upstream changes into the named local
	// branch: a pull while it is checked out, a fetch and a fast-forward of
	// its ref while it is not.
	PullBranch(context.Context, string) error
	// FastForwardBranch moves the named branch to its upstream without a
	// merge commit, refusing when the two have diverged.
	FastForwardBranch(context.Context, string) error
	// RebaseOntoBranch replays the current branch on top of the named one.
	RebaseOntoBranch(context.Context, string) error
	// SetUpstream makes localBranch track remote/remoteBranch; UnsetUpstream
	// removes that configuration without changing either branch's commits.
	SetUpstream(context.Context, string, string, string) error
	UnsetUpstream(context.Context, string) error
	// ForceCheckoutBranch checks the branch out even with uncommitted work,
	// discarding what is in the way.
	ForceCheckoutBranch(context.Context, string) error
	AddRemote(context.Context, string, string) error
	// UpdateRemote changes a remote's fetch URL and, when newName differs,
	// renames it. Push URLs are left untouched.
	UpdateRemote(context.Context, string, string, string) error
	RemoveRemote(context.Context, string) error
	CheckoutRemoteBranch(context.Context, string, string, string) error
	// DeleteRemoteBranch deletes only branch on the named remote. Any local
	// branch with the same name is left untouched.
	DeleteRemoteBranch(context.Context, string, string) error
}

// StashSession manages saved work and individual files within a stash.
type StashSession interface {
	StashChanges(context.Context, string, bool) error
	// StashSelectedFiles saves and removes only the chosen paths.
	StashSelectedFiles(context.Context, string, []File, bool) error
	// StashPatch saves an unstaged/untracked patch and reverses its
	// working-tree-specific counterpart, leaving the real index untouched.
	StashPatch(context.Context, string, string, string) error
	// StashStagedChanges saves only the index, leaving the unstaged work
	// in the tree, as git stash push --staged does.
	StashStagedChanges(context.Context, string) error
	ApplyStash(context.Context, int, bool) error
	RenameStash(context.Context, int, string) error
	DropStash(context.Context, int) error
	// StashFiles lists the files a stash saved: the tracked changes against
	// its base, and the untracked files it took with them.
	StashFiles(context.Context, int) ([]CommitFile, error)
	// StashFileDiff reads the diff of one file in a stash, so the saved work
	// pane can show each file on its own rather than a single stitched patch.
	StashFileDiff(context.Context, int, CommitFile, DiffOptions) (Diff, error)
	// ApplyStashFile writes one saved file's version into the working tree
	// and leaves the saved work saved.
	ApplyStashFile(context.Context, int, CommitFile) error
	// DiscardStashFile rewrites a stash without one file's change: the rest
	// of the saved work stays, the entry keeps its name, and what is gone
	// cannot be recovered.
	DiscardStashFile(context.Context, int, CommitFile, string) error
}

// WorktreeSession manages linked worktrees without bundling agent workspaces or task metadata.
type WorktreeSession interface {
	CreateWorktree(context.Context, WorktreeCreateOptions) error
	DeleteWorktree(context.Context, string, bool) error
	MoveWorktree(context.Context, string, string, bool) error
	FetchWorktree(context.Context, string) error
	PullWorktree(context.Context, string) error
	LockWorktree(context.Context, string, string) error
	UnlockWorktree(context.Context, string) error
	PreviewPruneWorktrees(context.Context, string) ([]string, error)
	PruneWorktrees(context.Context, string) error
	RepairWorktrees(context.Context, []string) error
}

// HostingSession provides the hosting CLI's pull-request and merge-request workflows.
type HostingSession interface {
	// PullRequestTool returns the configured hosting CLI, gh or glab.
	PullRequestTool(context.Context) (string, error)
	// CreatePullRequest creates a PR/MR with optional draft status.
	CreatePullRequest(context.Context, string, string, string, string, bool) (string, error)
	// OpenPullRequest views the branch's existing request in the browser.
	OpenPullRequest(context.Context, string) (string, error)
	// CheckPullRequestReady verifies the host CLI is installed and
	// authenticated before a pull-request workflow begins.
	CheckPullRequestReady(context.Context) error
	// PullRequestChecks returns the PR checks or pipeline report. Output
	// remains useful even when checks fail and the CLI returns an error.
	PullRequestChecks(context.Context, string) (string, error)
}

// ReflogSession pages through the reference log without exposing its other
// history-writing operations to the read-only Reflog pane.
type ReflogSession interface {
	// Reflog reads up to limit entries after skipping the newest skip.
	Reflog(context.Context, int, int) ([]Commit, error)
}

// FullSession is the complete repository adapter used while callers and test
// doubles migrate to individual capabilities. New consumers should accept the
// narrowest capability instead of adding another dependency on this aggregate.
type FullSession interface {
	Session
	HistorySession
	ReflogSession
	ChangesSession
	BranchRemoteSession
	StashSession
	WorktreeSession
	HostingSession
}

var _ FullSession = (*gitSession)(nil)
