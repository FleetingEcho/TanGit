package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/jesseduffield/lazygit/pkg/commands"
	"github.com/jesseduffield/lazygit/pkg/commands/git_commands"
	"github.com/jesseduffield/lazygit/pkg/commands/git_config"
	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
	"github.com/jesseduffield/lazygit/pkg/common"
	"github.com/jesseduffield/lazygit/pkg/config"
	"github.com/jesseduffield/lazygit/pkg/gocui"
	"github.com/jesseduffield/lazygit/pkg/utils"
)

type File struct {
	Path              string
	PreviousPath      string
	Status            string
	Staged            bool
	Unstaged          bool
	Tracked           bool
	Added             bool
	Deleted           bool
	HasMergeConflicts bool
	// HasInlineMergeConflicts says Git wrote the conflict into the file as
	// markers. A conflict where one side deleted the file has none: Git
	// leaves the surviving side in the working tree instead.
	HasInlineMergeConflicts bool
	// ConflictMarkerSize is how many characters Git wrote the markers with,
	// from the file's conflict-marker-size attribute; 0 when it is unset.
	ConflictMarkerSize int
	// LinesAdded and LinesDeleted describe the final HEAD-to-working-tree
	// change. When both sides are present, the side-specific fields let the
	// staged and unstaged rows report their own diffs instead of that total.
	LinesAdded           int
	LinesDeleted         int
	StagedLinesAdded     int
	StagedLinesDeleted   int
	UnstagedLinesAdded   int
	UnstagedLinesDeleted int
}

// SnapshotCommitLimit bounds the commits a snapshot carries, keeping the read
// on repository open and after each action fast. History and the Graph page in
// older commits below it with RecentCommits.
const SnapshotCommitLimit = 200

// ConflictStage is one immutable index-stage version of an unmerged path.
// Present distinguishes an absent side (a deletion) from an empty file.
type ConflictStage struct {
	Present bool
	Content string
}

// ConflictStages maps Git's unmerged index stages: 1 base, 2 current, 3 incoming.
type ConflictStages struct {
	Base     ConflictStage
	Current  ConflictStage
	Incoming ConflictStage
}

type Snapshot struct {
	Root          string
	Branch        string
	Head          string
	Files         []File
	Commits       []Commit
	Branches      []Branch
	Remotes       []Remote
	Stashes       []Stash
	WorkSnapshots []WorkSnapshot
	Tags          []Tag
	Operation     string
	Worktrees     []Worktree
	Submodules    []Submodule
	Reflog        []Commit
}

type Commit struct {
	Hash          string
	ShortHash     string
	Subject       string
	AuthorName    string
	AuthorEmail   string
	UnixTimestamp int64
	Tags          []string
	Parents       []string
}

// CommitFilter narrows the commits History and a branch's commit list show.
// Query matches the commit message, Author the author's name or email, and
// Branch the history read; an empty field does not narrow. Limit bounds the
// result, which pages in as the list scrolls.
type CommitFilter struct {
	Query  string
	Author string
	Branch string
	Limit  int
}

type CommitFile struct {
	Path         string
	PreviousPath string
	Status       string
	LinesAdded   int
	LinesDeleted int
}

type Branch struct {
	Name           string
	DisplayName    string
	Head           bool
	DetachedHead   bool
	UpstreamRemote string
	UpstreamBranch string
	// Ahead and Behind come straight from Git: they hold a count, or the
	// placeholder "?" when Git cannot tell — the remote-tracking branch is
	// not local yet, or the upstream is gone. UpstreamGone says which.
	Ahead        string
	Behind       string
	UpstreamGone bool
	Subject      string
}

type Remote struct {
	Name     string
	URLs     []string
	PushURLs []string
	Branches []RemoteBranch
}

type RemoteBranch struct {
	Name       string
	RemoteName string
}

type Stash struct {
	Index   int
	Name    string
	Hash    string
	Recency string
}

type Tag struct {
	Name    string
	Message string
}

type Worktree struct {
	Name          string
	Path          string
	Branch        string
	Head          string
	Current       bool
	Main          bool
	Missing       bool
	OperationLive bool
}

type Submodule struct {
	Name string
	Path string
	URL  string
}

type Diff struct {
	Patch   string
	OldText string
	NewText string
	Kind    string
}

// DiffOptions is an immutable snapshot of the choices a read-only comparison
// uses. ContextLines may be zero; IgnoreWhitespace applies the same filtering
// to the file list and to each file's patch.
type DiffOptions struct {
	IgnoreWhitespace bool
	ContextLines     int
}

// ResetMode is how reset treats the index and working tree.
type ResetMode string

const (
	ResetSoft  ResetMode = "soft"
	ResetMixed ResetMode = "mixed"
	ResetHard  ResetMode = "hard"
)

func (s Snapshot) ChangeCount() int {
	return len(s.Files)
}

// WithDetails adds the history and ancillary lists to a core snapshot while
// keeping its root, branch, operation and working-tree files. Progressive
// opening uses this at the UI's current core, never at the older core that
// started the deferred read.
func (s Snapshot) WithDetails(details Snapshot) Snapshot {
	s.Commits = details.Commits
	s.Branches = details.Branches
	s.Remotes = details.Remotes
	s.Stashes = details.Stashes
	s.WorkSnapshots = details.WorkSnapshots
	s.Tags = details.Tags
	s.Worktrees = details.Worktrees
	s.Submodules = details.Submodules
	s.Reflog = details.Reflog
	return s
}

// ProgressiveSession can put the current branch and changes on screen before
// its history and ancillary lists are read. DetailsSnapshot fills only those
// deferred lists; WithDetails preserves the live core when it lands.
type ProgressiveSession interface {
	Session
	CoreSnapshot(context.Context) (Snapshot, error)
	DetailsSnapshot(context.Context) (Snapshot, error)
}

type Session interface {
	Snapshot(context.Context) (Snapshot, error)
	// RecentCommits reads up to limit commits reachable from any ref, newest
	// first, skipping the newest skip, so History and the Graph page in
	// below the page a snapshot carries.
	RecentCommits(context.Context, int, int) ([]Commit, error)
	// ReviewedFiles returns the marked paths whose HEAD-to-working-tree content
	// still matches what was reviewed. SetFileReviewed stores the mark locally.
	ReviewedFiles(context.Context, []File) (map[string]bool, error)
	SetFileReviewed(context.Context, File, bool) error
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
	StageAll(context.Context) error
	UnstageAll(context.Context) error
	Commit(context.Context, string, string, bool) error
	// Undo and Redo reverse and reapply eligible HEAD-reflog actions. Their
	// markers persist in Git, so replay remains balanced across app restarts.
	Undo(context.Context) error
	Redo(context.Context) error
	Discard(context.Context, File) error
	// DiscardFiles captures one recovery entry before removing the selection.
	DiscardFiles(context.Context, []File) error
	// IgnoreFile and ExcludeFile hide an untracked path through the shared
	// .gitignore or this clone's .git/info/exclude respectively.
	IgnoreFile(context.Context, File) error
	ExcludeFile(context.Context, File) error
	CheckoutBranch(context.Context, string) error
	CheckoutCommit(context.Context, Commit) error
	CreateBranch(context.Context, string, string) error
	DeleteBranch(context.Context, string, bool) error
	RenameBranch(context.Context, string, string) error
	MergeBranch(context.Context, string) error
	Fetch(context.Context) error
	FetchRemote(context.Context, string) error
	Pull(context.Context) error
	Push(context.Context) error
	ForcePushWithLease(context.Context) error
	CherryPickCommits(context.Context, []Commit) error
	EditCommitRange(context.Context, []Commit, CommitRangeAction) error
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
	WorkSnapshots(context.Context) ([]WorkSnapshot, error)
	CreateCheckpoint(context.Context, string) (WorkSnapshot, error)
	RestoreWorkSnapshot(context.Context, WorkSnapshot) error
	DeleteWorkSnapshot(context.Context, WorkSnapshot) error
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
	CreateTag(context.Context, string, string, string, bool) error
	DeleteTag(context.Context, string) error
	PushTag(context.Context, string, string) error
	CherryPickCommit(context.Context, Commit) error
	RevertCommit(context.Context, Commit) error
	// ResetToCommit moves the branch to commit, with mode deciding what the
	// index and working tree retain.
	ResetToCommit(context.Context, Commit, ResetMode) error
	ResolveConflict(context.Context, File, bool) error
	// ConflictStages reads index stages without changing the index or worktree.
	ConflictStages(context.Context, File) (ConflictStages, error)
	ContinueOperation(context.Context) error
	AbortOperation(context.Context) error
	CreateWorktree(context.Context, string, string, string, bool) error
	DeleteWorktree(context.Context, string, bool) error
	AddSubmodule(context.Context, string, string, string) error
	UpdateSubmodule(context.Context, string) error
	DeleteSubmodule(context.Context, string) error
	BlameFile(context.Context, File) ([]BlameLine, error)
	CommitFiles(context.Context, Commit) ([]CommitFile, error)
	CommitFileDiff(context.Context, Commit, CommitFile, DiffOptions) (Diff, error)
	// RestoreCommitFile replaces working-tree content with the commit's
	// version without changing the index or creating a commit.
	RestoreCommitFile(context.Context, Commit, CommitFile) error
	// CompareFiles lists the files whose trees differ from the first ref to
	// the second. CompareFileDiff reads one of those files without changing
	// the checkout, index, or working tree.
	CompareFiles(context.Context, string, string, DiffOptions) ([]CommitFile, error)
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
	FileCommits(context.Context, string, int, int) ([]Commit, error)
	// CommandLog is the record of every Git command this session ran, for
	// the command log the window shows.
	CommandLog() *CommandLog
}

type Opener struct{}

// ErrNotRepository marks a folder that is not inside a Git repository, so the
// picker can offer to initialize one instead of showing Git's own message.
var ErrNotRepository = errors.New("not a Git repository")

// InitRepository makes path a Git repository, as git init does, so a folder
// the user pointed at can be adopted instead of rejected.
func InitRepository(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("initialize repository: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// isRepository reports whether path belongs to a Git repository, bare or with
// a working tree, the way git itself decides it.
func isRepository(ctx context.Context, path string) (bool, error) {
	if info, err := os.Stat(path); err != nil {
		return false, err
	} else if !info.IsDir() {
		return false, fmt.Errorf("%s is not a folder", path)
	}
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--git-dir")
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		// Git exits 128 for a folder outside any repository; anything
		// else is a failure worth showing.
		if errors.As(err, &exit) && exit.ExitCode() == 128 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (Opener) Init(ctx context.Context, path string) error {
	return InitRepository(ctx, path)
}

func (Opener) Open(ctx context.Context, path string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ok, err := isRepository(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}
	if !ok {
		return nil, ErrNotRepository
	}

	cmn := common.NewDummyCommon()
	appConfig := config.NewDummyAppConfig()
	// Every Git command is written to the session's log, and the commands
	// that stream their output write it there too, so the window can show
	// what Git did. Credentials still fail as before rather than prompting
	// outside a terminal.
	commandLog := NewCommandLog()
	guiIO := oscommands.NewGuiIO(
		cmn.Log,
		commandLog.logCommand,
		commandLog.newWriter,
		func(oscommands.CredentialType) <-chan string { return nil },
	)
	osCommand := oscommands.NewOSCommand(
		cmn,
		appConfig,
		oscommands.GetPlatform(),
		guiIO,
	)
	version, err := git_commands.GetGitVersion(osCommand)
	if err != nil {
		return nil, fmt.Errorf("read Git version: %w", err)
	}
	gitConfig := git_config.NewStdCachedGitConfig(cmn.Log)
	diffConfig := config.NewDiffRendererConfigManager(cmn.UserConfig)
	git, err := newGitCommand(path, cmn, version, osCommand, gitConfig, diffConfig)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}
	repoPaths, err := git_commands.GetRepoPathsForDir(path, osCommand.Cmd)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}

	// Commands the library has no wrapper for run through this builder, which
	// pins them to the repository the way the library's own commands are.
	cmd := commands.NewGitCmdObjBuilder(cmn.Log, osCommand.Cmd, repoPaths.WorktreePath(), repoPaths.GitLocationEnvVars())

	// Commit status enrichment asks Git which loaded commits are reachable
	// from a main branch. TanGit drops that status when it makes its own
	// Commit values, so give those loaders an empty main-branch set instead
	// of paying for rev-list reads and throwing their answer away.
	commitCommon := common.NewDummyCommon()
	commitCommon.UserConfig().Git.MainBranches = []string{}

	return &gitSession{
		git:                       git,
		commandLog:                commandLog,
		cmd:                       cmd,
		mainBranches:              git_commands.NewMainBranches(cmn, cmd),
		commitMainBranches:        git_commands.NewMainBranches(commitCommon, cmd),
		hashPool:                  &utils.StringPool{},
		renameSimilarityThreshold: cmn.UserConfig().Git.RenameSimilarityThreshold,
	}, nil
}

type gitSession struct {
	git        *commands.GitCommand
	commandLog *CommandLog
	// cmd builds the commands the library has no plain wrapper for, such as
	// a non-interactive rebase, which must not run this app as its editor.
	cmd oscommands.ICmdObjBuilder
	// mainBranches supports branch metadata. commitMainBranches is deliberately
	// empty: the commit loaders otherwise calculate reachability statuses the
	// app's Commit model does not retain. hashPool interns their commit hashes.
	mainBranches       *git_commands.MainBranches
	commitMainBranches *git_commands.MainBranches
	hashPool           *utils.StringPool
	// reviewMu keeps a read-modify-write of the local review file together.
	reviewMu sync.Mutex
	// Keep numstat's rename detection identical to the commit file loader's.
	renameSimilarityThreshold int
}

func (s *gitSession) CommandLog() *CommandLog { return s.commandLog }

// Snapshot retains the complete read used by actions and refreshes. Opening
// a real repository uses CoreSnapshot and DetailsSnapshot separately instead.
func (s *gitSession) Snapshot(ctx context.Context) (Snapshot, error) {
	core, err := s.CoreSnapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	details, err := s.DetailsSnapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	return core.WithDetails(details), nil
}

// CoreSnapshot reads only what the first Changes screen needs: the current
// branch, an operation in progress, and the working tree. In particular it
// does not wait for history, tags, remotes or the reflog.
func (s *gitSession) CoreSnapshot(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	branchInfo, err := s.git.Branch.CurrentBranchInfo()
	if err != nil {
		return Snapshot{}, fmt.Errorf("read current branch: %w", err)
	}
	workingTreeState := s.git.Status.WorkingTreeState()
	operation := ""
	if workingTreeState.Any() {
		operation = workingTreeState.CommandName()
	}

	modelFiles := s.git.Loaders.FileLoader.GetStatusFiles(git_commands.GetStatusFileOptions{})
	// Lazygit's status loader leaves line counts at zero. One batch numstat
	// from HEAD to the working tree counts the final content accurately even
	// when staged and unstaged edits overlap; adding the two diffs separately
	// would double-count or credit edits that the worktree later reversed.
	trackedStats := map[string]commitLineStats{}
	stagedStats := map[string]commitLineStats{}
	unstagedStats := map[string]commitLineStats{}
	needsTrackedStats := false
	needsSideStats := false
	for _, modelFile := range modelFiles {
		file := snapshotFile(modelFile)
		if file.Tracked || file.Staged {
			needsTrackedStats = true
		}
		if file.Staged && file.Unstaged && !file.HasMergeConflicts {
			needsSideStats = true
		}
	}
	if needsTrackedStats {
		var err error
		trackedStats, err = s.workingTreeLineStats()
		if err != nil {
			return Snapshot{}, fmt.Errorf("load tracked line statistics: %w", err)
		}
	}
	if needsSideStats {
		var err error
		stagedStats, err = s.stagedLineStats()
		if err != nil {
			return Snapshot{}, fmt.Errorf("load staged line statistics: %w", err)
		}
		unstagedStats, err = s.unstagedLineStats()
		if err != nil {
			return Snapshot{}, fmt.Errorf("load unstaged line statistics: %w", err)
		}
	}
	files := make([]File, 0, len(modelFiles))
	for _, modelFile := range modelFiles {
		file := snapshotFile(modelFile)
		if !file.Tracked && !file.Staged {
			stat, err := s.untrackedLineStats(file.Path)
			if err != nil {
				return Snapshot{}, fmt.Errorf("load line statistics for %s: %w", file.Path, err)
			}
			file.LinesAdded = stat.added
			file.LinesDeleted = stat.deleted
		} else {
			stat := trackedStats[file.Path]
			file.LinesAdded = stat.added
			file.LinesDeleted = stat.deleted
		}
		if file.Staged && file.Unstaged && !file.HasMergeConflicts {
			staged := stagedStats[file.Path]
			file.StagedLinesAdded = staged.added
			file.StagedLinesDeleted = staged.deleted
			unstaged := unstagedStats[file.Path]
			file.UnstagedLinesAdded = unstaged.added
			file.UnstagedLinesDeleted = unstaged.deleted
		}
		files = append(files, file)
	}

	head := ""
	if branchInfo.DetachedHead {
		head = branchInfo.RefName
	}
	return Snapshot{
		Root:      s.git.RepoPaths.WorktreePath(),
		Branch:    branchInfo.DisplayName,
		Head:      head,
		Files:     files,
		Operation: operation,
	}, nil
}

// DetailsSnapshot reads the lists that are not needed to make the working
// tree useful. Snapshot and progressive opening attach them to a core with
// WithDetails rather than duplicating their installation rules.
func (s *gitSession) DetailsSnapshot(ctx context.Context) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	modelCommits, err := s.recentCommits(SnapshotCommitLimit)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load recent commits: %w", err)
	}
	commits := make([]Commit, 0, len(modelCommits))
	for _, commit := range modelCommits {
		commits = append(commits, snapshotCommit(commit))
	}
	modelBranches, err := s.localBranches()
	if err != nil {
		return Snapshot{}, fmt.Errorf("load local branches: %w", err)
	}
	branches := make([]Branch, 0, len(modelBranches))
	for _, branch := range modelBranches {
		branches = append(branches, snapshotBranch(branch))
	}
	modelRemotes := s.git.Loaders.RemoteLoader.GetRemotes()
	remoteBranches, err := s.git.Loaders.RemoteLoader.GetRemoteBranchesByRemoteName()
	if err != nil {
		return Snapshot{}, fmt.Errorf("load remote branches: %w", err)
	}
	remotes := make([]Remote, 0, len(modelRemotes))
	for _, remote := range modelRemotes {
		item := Remote{
			Name:     remote.Name,
			URLs:     redactURLs(remote.Urls),
			PushURLs: redactURLs(remote.PushUrls),
		}
		for _, branch := range remoteBranches[remote.Name] {
			item.Branches = append(item.Branches, RemoteBranch{Name: branch.Name, RemoteName: branch.RemoteName})
		}
		remotes = append(remotes, item)
	}
	modelStashes := s.git.Loaders.StashLoader.GetStashEntries("")
	stashes := make([]Stash, 0, len(modelStashes))
	for _, stash := range modelStashes {
		stashes = append(stashes, Stash{Index: stash.Index, Name: stash.Name, Hash: stash.Hash, Recency: stash.Recency})
	}
	modelTags, err := s.git.Loaders.TagLoader.GetTags()
	if err != nil {
		return Snapshot{}, fmt.Errorf("load tags: %w", err)
	}
	tags := make([]Tag, 0, len(modelTags))
	for _, tag := range modelTags {
		tags = append(tags, Tag{Name: tag.Name, Message: tag.Message})
	}
	modelWorktrees, err := s.git.Loaders.Worktrees.GetWorktrees()
	if err != nil {
		return Snapshot{}, fmt.Errorf("load worktrees: %w", err)
	}
	worktrees := make([]Worktree, 0, len(modelWorktrees))
	for _, worktree := range modelWorktrees {
		worktrees = append(worktrees, Worktree{
			Name:          worktree.Name,
			Path:          worktree.Path,
			Branch:        worktree.Branch,
			Head:          worktree.Head,
			Current:       worktree.IsCurrent,
			Main:          worktree.IsMain,
			Missing:       worktree.IsPathMissing,
			OperationLive: worktree.IsRebasingOrBisecting,
		})
	}
	modelSubmodules, err := s.git.Submodule.GetConfigs(nil)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load submodules: %w", err)
	}
	submodules := make([]Submodule, 0, len(modelSubmodules))
	for _, submodule := range modelSubmodules {
		submodules = append(submodules, Submodule{Name: submodule.FullName(), Path: submodule.FullPath(), URL: RedactSecrets(submodule.Url)})
	}
	workSnapshots, err := s.WorkSnapshots(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load recovery: %w", err)
	}
	modelReflog, err := s.recentReflog(SnapshotCommitLimit)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load reflog: %w", err)
	}
	reflog := make([]Commit, 0, len(modelReflog))
	for _, commit := range modelReflog {
		reflog = append(reflog, snapshotCommit(commit))
	}

	return Snapshot{
		Commits:       commits,
		Branches:      branches,
		Remotes:       remotes,
		Stashes:       stashes,
		WorkSnapshots: workSnapshots,
		Tags:          tags,
		Worktrees:     worktrees,
		Submodules:    submodules,
		Reflog:        reflog,
	}, nil
}

// RecentCommits reads up to limit commits reachable from any ref, newest
// first, skipping the newest skip. Git's log takes a count from the newest
// commit and no offset, so the skipped and wanted commits are read together,
// as FileCommits does.
func (s *gitSession) RecentCommits(ctx context.Context, limit, skip int) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, fmt.Errorf("read recent commits: no limit chosen")
	}
	if skip < 0 {
		skip = 0
	}
	modelCommits, err := s.recentCommits(limit + skip)
	if err != nil {
		return nil, fmt.Errorf("read recent commits: %w", err)
	}
	if skip >= len(modelCommits) {
		return nil, nil
	}
	modelCommits = modelCommits[skip:]
	commits := make([]Commit, 0, len(modelCommits))
	for _, commit := range modelCommits {
		commits = append(commits, snapshotCommit(commit))
	}
	return commits, nil
}

// diffOptionArgs gives every patch read the same explicit whitespace and
// context behavior. Context zero is meaningful and must not become Git's
// default merely because the options' other field is false.
func diffOptionArgs(options DiffOptions) ([]string, error) {
	if options.ContextLines < 0 {
		return nil, fmt.Errorf("diff context cannot be negative")
	}
	args := []string{fmt.Sprintf("--unified=%d", options.ContextLines)}
	if options.IgnoreWhitespace {
		args = append(args, "--ignore-all-space")
	}
	return args, nil
}

func (s *gitSession) Diff(ctx context.Context, file File, options DiffOptions) (Diff, error) {
	if err := ctx.Err(); err != nil {
		return Diff{}, err
	}
	optionArgs, err := diffOptionArgs(options)
	if err != nil {
		return Diff{}, fmt.Errorf("load file diff: %w", err)
	}

	paths := fileNames(file)
	if !file.Tracked && !file.Staged {
		// An untracked file has no side in the index to diff against, so git
		// is asked to compare it with /dev/null: the answer is the patch that
		// adds it, which is both what the pane shows and what a selection of
		// its lines is built from.
		args := append([]string{"git", "diff", "--no-index", "--no-color", "--no-ext-diff"}, optionArgs...)
		args = append(args, "--", "/dev/null")
		args = append(args, paths...)
		outputBytes, commandErr := s.cmd.New(args).GetCmd().CombinedOutput()
		if commandErr != nil {
			var exitError *exec.ExitError
			// Git uses status 1 to report that the two sides differ.
			if !errors.As(commandErr, &exitError) || exitError.ExitCode() != 1 {
				return Diff{}, fmt.Errorf("load untracked file diff: %w: %s", commandErr, outputBytes)
			}
		}
		patch := string(outputBytes)
		if strings.TrimSpace(patch) != "" {
			return Diff{Patch: patch, Kind: "Untracked"}, nil
		}
		// An empty file has nothing for git to report, and a directory has no
		// content to show; reading the path tells the two apart as the pane
		// always has.
		content, err := os.ReadFile(filepath.Join(s.git.RepoPaths.WorktreePath(), file.Path))
		if err != nil {
			return Diff{}, fmt.Errorf("read untracked file: %w", err)
		}
		return Diff{NewText: string(content), Kind: "Untracked"}, nil
	}

	staged := file.Staged && !file.Unstaged
	args := append(optionArgs, "--")
	args = append(args, paths...)
	patch, err := s.git.Diff.GetDiff(staged, args...)
	if err != nil {
		return Diff{}, fmt.Errorf("load file diff: %w", err)
	}
	kind := "Unstaged"
	if staged {
		kind = "Staged"
	}
	return Diff{Patch: patch, Kind: kind}, nil
}

// workingTreeLineStats reads the final HEAD-to-working-tree change. The two
// side-specific readers below use the same numstat parsing and rename handling.
func (s *gitSession) workingTreeLineStats() (map[string]commitLineStats, error) {
	return s.headLineStats(false)
}

func (s *gitSession) stagedLineStats() (map[string]commitLineStats, error) {
	return s.headLineStats(true)
}

func (s *gitSession) unstagedLineStats() (map[string]commitLineStats, error) {
	return s.diffLineStats(false, "")
}

// headLineStats reads one side relative to HEAD. An unborn branch has no HEAD,
// so its empty tree is the equivalent base for files already added to its index.
func (s *gitSession) headLineStats(cached bool) (map[string]commitLineStats, error) {
	stats, headErr := s.diffLineStats(cached, "HEAD")
	if headErr == nil {
		return stats, nil
	}
	// A readable commit means HEAD itself was not the problem: another bad
	// object or read error must not turn into an everything-is-new diff.
	if err := s.cmd.New([]string{"git", "rev-parse", "--verify", "HEAD"}).Run(); err == nil {
		return nil, headErr
	}
	// A detached damaged HEAD has no branch to fall back from either. Only a
	// symbolic branch whose ref does not exist yet is genuinely unborn.
	ref, err := s.cmd.New([]string{"git", "symbolic-ref", "--quiet", "HEAD"}).RunWithOutput()
	if err != nil {
		return nil, headErr
	}
	ref = strings.TrimSpace(ref)
	if ref == "" || s.cmd.New([]string{"git", "show-ref", "--verify", "--quiet", ref}).Run() == nil {
		return nil, headErr
	}
	stats, emptyErr := s.diffLineStats(cached, models.EmptyTreeCommitHash)
	if emptyErr != nil {
		return nil, headErr
	}
	return stats, nil
}

func (s *gitSession) diffLineStats(cached bool, base string) (map[string]commitLineStats, error) {
	args := []string{
		"git", "diff", "--no-ext-diff", "--numstat", "-z",
		fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
	}
	if cached {
		args = append(args, "--cached")
	}
	if base != "" {
		args = append(args, base)
	}
	args = append(args, "--")
	output, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(output))
	}
	return parseCommitLineStats(output), nil
}

// untrackedLineStats compares an untracked file with the empty file. Lazygit's
// status loader leaves its line counts at zero, while --no-index gives the same
// whole-file addition that the untracked diff pane presents. Git returns 1 when
// the two sides differ; that is the expected result, not a failed statistics
// read. An empty file produces no numstat record.
func (s *gitSession) untrackedLineStats(path string) (commitLineStats, error) {
	command := s.cmd.New([]string{
		"git", "diff", "--no-index", "--no-ext-diff", "--numstat", "-z",
		"/dev/null", "--", path,
	}).GetCmd()
	outputBytes, err := command.CombinedOutput()
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			return commitLineStats{}, fmt.Errorf("%w: %s", err, outputBytes)
		}
	}
	output := string(outputBytes)
	if output == "" {
		return commitLineStats{}, nil
	}
	columns := strings.SplitN(output, "\t", 3)
	if len(columns) != 3 {
		return commitLineStats{}, fmt.Errorf("unexpected numstat output %q", output)
	}
	// Binary files use dashes, which correctly count as no textual changes.
	added, _ := strconv.Atoi(columns[0])
	deleted, _ := strconv.Atoi(columns[1])
	return commitLineStats{added: added, deleted: deleted}, nil
}

func (s *gitSession) Stage(ctx context.Context, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.WorkingTree.StageFiles(fileNames(file), nil); err != nil {
		return fmt.Errorf("stage file: %w", err)
	}
	return nil
}

func (s *gitSession) Unstage(ctx context.Context, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.WorkingTree.UnStageFile(fileNames(file), file.Tracked); err != nil {
		return fmt.Errorf("unstage file: %w", err)
	}
	return nil
}

// ApplyCachedPatch applies a partial diff to the index only, leaving the
// working tree untouched: forward it stages the lines the patch holds, and
// reversed it takes them back out of the index. The patch names the file it
// changes, so no path has to be passed with it.
func (s *gitSession) ApplyCachedPatch(ctx context.Context, patchText string, reverse bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Git reads the patch from a file of the app's own, which is absolute:
	// the command runs in the worktree, so a relative path would resolve
	// against it rather than against the process' temporary folder.
	file, err := os.CreateTemp("", "tangit-line-stage-*.patch")
	if err != nil {
		return fmt.Errorf("write the patch: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.WriteString(patchText); err != nil {
		file.Close()
		return fmt.Errorf("write the patch: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write the patch: %w", err)
	}
	args := []string{"git", "apply", "--cached"}
	if reverse {
		args = append(args, "--reverse")
	}
	args = append(args, name)
	output, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return fmt.Errorf("apply lines to the index: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// ApplyWorkingTreePatch applies a partial diff to the working tree only. The
// check runs immediately before the mutation so a stale or inapplicable patch
// cannot partially change a file; the real index is never involved.
func (s *gitSession) ApplyWorkingTreePatch(ctx context.Context, patchText string, reverse bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reverse {
		if _, err := s.captureWorkSnapshot(ctx, RecoverySnapshotKind, "Discard a hunk"); err != nil {
			return err
		}
	}
	if err := s.applyWorkingTreePatch(ctx, patchText, reverse); err != nil {
		return err
	}
	if reverse {
		return s.pruneRecovery(ctx)
	}
	return nil
}

func (s *gitSession) applyWorkingTreePatch(ctx context.Context, patchText string, reverse bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.CreateTemp("", "tangit-worktree-*.patch")
	if err != nil {
		return fmt.Errorf("write the patch: %w", err)
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.WriteString(patchText); err != nil {
		file.Close()
		return fmt.Errorf("write the patch: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write the patch: %w", err)
	}
	args := []string{"git", "apply", "--recount"}
	if reverse {
		args = append(args, "--reverse")
	}
	for _, extra := range []string{"--check", ""} {
		command := append([]string(nil), args...)
		if extra != "" {
			command = append(command, extra)
		}
		output, err := s.cmd.New(append(command, name)).RunWithOutput()
		if err != nil {
			return fmt.Errorf("apply patch to the working tree: %w: %s", err, strings.TrimSpace(output))
		}
	}
	return nil
}

func (s *gitSession) StageAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.WorkingTree.StageAll(false); err != nil {
		return fmt.Errorf("stage all files: %w", err)
	}
	return nil
}

func (s *gitSession) UnstageAll(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.WorkingTree.UnstageAll(); err != nil {
		return fmt.Errorf("unstage all files: %w", err)
	}
	return nil
}

func (s *gitSession) Commit(ctx context.Context, summary string, description string, amend bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if summary == "" {
		return fmt.Errorf("commit summary is required")
	}
	if err := s.commitReadiness(amend); err != nil {
		return err
	}
	if amend {
		if err := s.amendHeadWithMessage(summary, description, false); err != nil {
			return fmt.Errorf("amend commit: %w", err)
		}
		return nil
	}
	// Build the command directly: lazygit's optional SkipHookPrefix must never
	// turn into --no-verify here. Once this command begins it is not cancelled.
	args := []string{"git", "commit", "-m", summary}
	if description != "" {
		args = append(args, "-m", description)
	}
	if output, err := s.cmd.New(args).RunWithOutput(); err != nil {
		return fmt.Errorf("create commit: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func (s *gitSession) discardFile(ctx context.Context, file File) error {
	if !file.Tracked {
		if file.Staged {
			if err := s.Unstage(ctx, file); err != nil {
				return err
			}
		}
		path, err := worktreeFilePath(s.git.RepoPaths.WorktreePath(), file.Path)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove untracked file: %w", err)
		}
		return nil
	}
	if err := s.git.WorkingTree.DiscardAllFileChanges(modelFile(file)); err != nil {
		return fmt.Errorf("discard file changes: %w", err)
	}
	return nil
}

func (s *gitSession) Discard(ctx context.Context, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if file.HasMergeConflicts {
		return fmt.Errorf("resolve merge conflicts before discarding %s", file.Path)
	}
	if _, err := s.captureWorkSnapshot(ctx, RecoverySnapshotKind, "Discard "+file.Path); err != nil {
		return err
	}
	if err := s.discardFile(ctx, file); err != nil {
		return err
	}
	return s.pruneRecovery(ctx)
}

func (s *gitSession) DiscardFiles(ctx context.Context, files []File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	for _, file := range files {
		if file.HasMergeConflicts {
			return fmt.Errorf("resolve merge conflicts before discarding %s", file.Path)
		}
	}
	if _, err := s.captureWorkSnapshot(ctx, RecoverySnapshotKind, fmt.Sprintf("Discard %d changes", len(files))); err != nil {
		return err
	}
	for _, file := range files {
		if err := s.discardFile(ctx, file); err != nil {
			return err
		}
	}
	return s.pruneRecovery(ctx)
}

func (s *gitSession) CheckoutBranch(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Branch.Checkout(name, git_commands.CheckoutOptions{}); err != nil {
		return fmt.Errorf("checkout branch: %w", err)
	}
	return nil
}

func (s *gitSession) CreateBranch(ctx context.Context, name string, base string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("branch name is required")
	}
	if base == "" {
		base = "HEAD"
	}
	if err := s.git.Branch.New(name, base); err != nil {
		return fmt.Errorf("create branch: %w", err)
	}
	return nil
}

func (s *gitSession) DeleteBranch(ctx context.Context, name string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Branch.LocalDelete([]string{name}, force); err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}
	return nil
}

func (s *gitSession) RenameBranch(ctx context.Context, oldName string, newName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(newName) == "" {
		return fmt.Errorf("branch name is required")
	}
	if err := s.git.Branch.Rename(oldName, newName); err != nil {
		return fmt.Errorf("rename branch: %w", err)
	}
	return nil
}

func (s *gitSession) MergeBranch(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Branch.Merge(name, git_commands.MERGE_VARIANT_REGULAR); err != nil {
		return fmt.Errorf("merge branch: %w", err)
	}
	return nil
}

func (s *gitSession) Fetch(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Sync.Fetch(nil); err != nil {
		return fmt.Errorf("fetch: %w", safeNetworkError(err, false))
	}
	return nil
}

func (s *gitSession) FetchRemote(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("fetch: remote name is required")
	}
	if err := s.git.Sync.FetchRemote(gocui.NewFakeTask(), name); err != nil {
		return fmt.Errorf("fetch %s: %w", name, safeNetworkError(err, false))
	}
	return nil
}

func (s *gitSession) Pull(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	branch, err := s.currentBranch()
	if err != nil {
		return err
	}
	if branch.UpstreamRemote == "" || branch.UpstreamBranch == "" {
		return fmt.Errorf("pull: branch %s: %w", branch.Name, ErrNoUpstream)
	}
	if err := s.git.Sync.Pull(nil, git_commands.PullOptions{
		RemoteName: branch.UpstreamRemote,
		BranchName: branch.UpstreamBranch,
	}); err != nil {
		return fmt.Errorf("pull: %w", safeNetworkError(err, false))
	}
	return nil
}

func (s *gitSession) Push(ctx context.Context) error {
	return s.push(ctx, false)
}

func (s *gitSession) ForcePushWithLease(ctx context.Context) error {
	return s.push(ctx, true)
}

func (s *gitSession) push(ctx context.Context, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	branch, err := s.currentBranch()
	if err != nil {
		return err
	}

	remote := branch.PushRemote
	target := branch.PushBranch
	setUpstream := false
	if remote == "" {
		remote = branch.UpstreamRemote
		if remote == "" {
			remotes := s.git.Loaders.RemoteLoader.GetRemotes()
			if len(remotes) == 0 {
				return fmt.Errorf("push: repository has no remotes")
			}
			remote = remotes[0].Name
		}
		target = branch.Name
		setUpstream = true
	}
	if target == "" {
		target = branch.Name
	}

	if err := s.git.Sync.Push(nil, git_commands.PushOpts{
		ForceWithLease: force,
		SetUpstream:    setUpstream,
		Remote:         remote,
		Refspecs:       []string{fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch.Name, target)},
	}); err != nil {
		return fmt.Errorf("push: %w", safeNetworkError(err, force))
	}
	return nil
}

// localBranch loads the named local branch from Git.
func (s *gitSession) localBranch(name string) (*models.Branch, error) {
	branches, err := s.localBranches()
	if err != nil {
		return nil, fmt.Errorf("load branch %s: %w", name, err)
	}
	for _, branch := range branches {
		if branch.Name == name {
			return branch, nil
		}
	}
	return nil, fmt.Errorf("branch %s no longer exists", name)
}

func (s *gitSession) PullBranch(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	branch, err := s.localBranch(name)
	if err != nil {
		return err
	}
	// The checked-out branch pulls as a working-tree update, which merges
	// as the user's Git configuration says.
	if branch.Head && !branch.DetachedHead {
		return s.Pull(ctx)
	}
	upstream, err := s.fetchUpstream(branch)
	if err != nil {
		return err
	}
	if !s.git.Branch.IsAncestor(branch.Name, upstream) {
		return fmt.Errorf("pull: %s cannot fast-forward to %s; check it out and pull to merge", branch.Name, upstream)
	}
	// Moving the ref of a branch that is not checked out brings its
	// commits in without touching the working tree.
	update := fmt.Sprintf("update refs/heads/%s %s\n", branch.Name, upstream)
	if err := s.git.Branch.UpdateBranchRefs(update, "pull: fast-forward "+branch.Name); err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	return nil
}

func (s *gitSession) FastForwardBranch(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	branch, err := s.localBranch(name)
	if err != nil {
		return err
	}
	upstream, err := s.fetchUpstream(branch)
	if err != nil {
		return err
	}
	if !s.git.Branch.IsAncestor(branch.Name, upstream) {
		return fmt.Errorf("fast-forward: %s has diverged from %s", branch.Name, upstream)
	}
	if branch.Head && !branch.DetachedHead {
		// The checked-out branch takes its working tree along.
		if err := s.git.Branch.FastForwardMerge(upstream, "", ""); err != nil {
			return fmt.Errorf("fast-forward: %w", err)
		}
		return nil
	}
	update := fmt.Sprintf("update refs/heads/%s %s\n", branch.Name, upstream)
	if err := s.git.Branch.UpdateBranchRefs(update, "fast-forward "+branch.Name); err != nil {
		return fmt.Errorf("fast-forward: %w", err)
	}
	return nil
}

// fetchUpstream refreshes the remote-tracking ref a branch tracks and
// returns its name, so the branch can move to it.
func (s *gitSession) fetchUpstream(branch *models.Branch) (string, error) {
	if branch.UpstreamRemote == "" || branch.UpstreamBranch == "" {
		return "", fmt.Errorf("branch %s: %w", branch.Name, ErrNoUpstream)
	}
	if err := s.git.Sync.FetchRemoteBranches(
		gocui.NewFakeTask(),
		branch.UpstreamRemote,
		[]string{branch.UpstreamBranch},
	); err != nil {
		return "", fmt.Errorf("fetch %s: %w", branch.UpstreamRemote, safeNetworkError(err, false))
	}
	return branch.UpstreamRemote + "/" + branch.UpstreamBranch, nil
}

func (s *gitSession) RebaseOntoBranch(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := s.currentBranch()
	if err != nil {
		return err
	}
	if current.Name == name {
		return fmt.Errorf("rebase: cannot rebase %s onto itself", name)
	}
	// The library's rebase runs this program as Git's sequence editor, which
	// a GUI cannot answer; a plain rebase with no editor replays the commits
	// as they are, which is what "rebase current onto this" means.
	cmd := s.cmd.New([]string{"git", "rebase", "--autostash", name})
	cmd.AddEnvVars("GIT_EDITOR=:", "GIT_SEQUENCE_EDITOR=:")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rebase onto %s: %w", name, err)
	}
	return nil
}

func (s *gitSession) SetUpstream(ctx context.Context, remote string, remoteBranch string, localBranch string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote = strings.TrimSpace(remote)
	remoteBranch = strings.TrimSpace(remoteBranch)
	localBranch = strings.TrimSpace(localBranch)
	if remote == "" || remoteBranch == "" || localBranch == "" {
		return fmt.Errorf("set upstream: remote, remote branch, and local branch are required")
	}
	if err := s.git.Branch.SetUpstream(remote, remoteBranch, localBranch); err != nil {
		return fmt.Errorf("set upstream: %w", err)
	}
	return nil
}

func (s *gitSession) UnsetUpstream(ctx context.Context, localBranch string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	localBranch = strings.TrimSpace(localBranch)
	if localBranch == "" {
		return fmt.Errorf("unset upstream: local branch is required")
	}
	if err := s.git.Branch.UnsetUpstream(localBranch); err != nil {
		return fmt.Errorf("unset upstream: %w", err)
	}
	return nil
}

func (s *gitSession) ForceCheckoutBranch(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Branch.Checkout(name, git_commands.CheckoutOptions{Force: true}); err != nil {
		return fmt.Errorf("force checkout branch: %w", err)
	}
	return nil
}

func (s *gitSession) AddRemote(ctx context.Context, name string, url string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	url = strings.TrimSpace(url)
	if name == "" || url == "" {
		return fmt.Errorf("remote name and URL are required")
	}
	if urlContainsCredentials(url) {
		return fmt.Errorf("add remote: %w", ErrCredentialsInURL)
	}
	if err := s.git.Remote.AddRemote(name, url); err != nil {
		return fmt.Errorf("add remote: %w", err)
	}
	return nil
}

func (s *gitSession) UpdateRemote(ctx context.Context, oldName string, newName string, url string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	url = strings.TrimSpace(url)
	if oldName == "" || newName == "" || url == "" {
		return fmt.Errorf("remote name and URL are required")
	}

	// The edit form receives the snapshot's redacted URL. Saving that value
	// unchanged means "leave the destination alone", not "replace the real URL
	// with its display form". Any actual edit must be credential-free.
	unchangedURL := false
	for _, remote := range s.git.Loaders.RemoteLoader.GetRemotes() {
		if remote.Name == oldName && len(remote.Urls) > 0 && url == RedactSecrets(remote.Urls[0]) {
			unchangedURL = true
			break
		}
	}
	if !unchangedURL {
		if urlContainsCredentials(url) {
			return fmt.Errorf("update remote: %w", ErrCredentialsInURL)
		}
		// Change the URL while the old name still identifies the remote. If the
		// subsequent rename fails, the user can retry the same edit by old name.
		if err := s.git.Remote.UpdateRemoteUrl(oldName, url); err != nil {
			return fmt.Errorf("update remote URL: %w", err)
		}
	}
	if newName != oldName {
		if err := s.git.Remote.RenameRemote(oldName, newName); err != nil {
			return fmt.Errorf("rename remote: %w", err)
		}
	}
	return nil
}

func (s *gitSession) RemoveRemote(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Remote.RemoveRemote(name); err != nil {
		return fmt.Errorf("remove remote: %w", err)
	}
	return nil
}

func (s *gitSession) CheckoutRemoteBranch(ctx context.Context, remote string, branch string, localName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	localName = strings.TrimSpace(localName)
	if localName == "" {
		localName = branch
	}
	if err := s.git.Branch.New(localName, remote+"/"+branch); err != nil {
		return fmt.Errorf("checkout remote branch: %w", err)
	}
	return nil
}

func (s *gitSession) DeleteRemoteBranch(ctx context.Context, remote string, branch string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote = strings.TrimSpace(remote)
	branch = strings.TrimSpace(branch)
	if remote == "" || branch == "" {
		return fmt.Errorf("delete remote branch: remote and branch are required")
	}
	if err := s.git.Remote.DeleteRemoteBranch(gocui.NewFakeTask(), remote, []string{branch}); err != nil {
		return fmt.Errorf("delete remote branch %s/%s: %w", remote, branch, err)
	}
	return nil
}

func (s *gitSession) StashChanges(ctx context.Context, message string, includeUntracked bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Desktop stash"
	}
	var err error
	if includeUntracked {
		err = s.git.Stash.StashIncludeUntrackedChanges(message)
	} else {
		err = s.git.Stash.Push(message)
	}
	if err != nil {
		return fmt.Errorf("stash changes: %w", err)
	}
	return nil
}

func (s *gitSession) StashSelectedFiles(ctx context.Context, message string, files []File, includeUntracked bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("stash selected files: no files chosen")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Desktop stash"
	}
	args := []string{"git", "stash", "push", "-m", message}
	if includeUntracked {
		args = append(args, "--include-untracked")
	}
	args = append(args, "--")
	seen := map[string]bool{}
	for _, file := range files {
		for _, path := range []string{file.Path, file.PreviousPath} {
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			args = append(args, ":(literal)"+path)
		}
	}
	if !includeUntracked {
		for _, file := range files {
			if !file.Tracked {
				return fmt.Errorf("stash selected files: include untracked to save %s", file.Path)
			}
		}
	}
	output, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return fmt.Errorf("stash selected files: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

// StashPatch constructs a native stash over HEAD and a temporary index holding
// only patchText. removalPatch is built from the same selections against the
// current working tree: its unselected lines differ when other edits surround
// the saved ones. The real index is never used, and the stash is safely stored
// before the working tree changes.
func (s *gitSession) StashPatch(ctx context.Context, message, patchText, removalPatch string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(patchText) == "" || strings.TrimSpace(removalPatch) == "" {
		return fmt.Errorf("stash selected lines: empty patch")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Desktop selected-line stash"
	}
	base, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return fmt.Errorf("stash selected lines: HEAD is required")
	}
	patchFile, err := os.CreateTemp("", "tangit-stash-*.patch")
	if err != nil {
		return err
	}
	patchName := patchFile.Name()
	defer os.Remove(patchName)
	if _, err := patchFile.WriteString(patchText); err != nil {
		patchFile.Close()
		return err
	}
	if err := patchFile.Close(); err != nil {
		return err
	}
	scratch, err := os.CreateTemp("", "tangit-stash-index-")
	if err != nil {
		return err
	}
	indexName := scratch.Name()
	scratch.Close()
	os.Remove(indexName)
	defer os.Remove(indexName)
	env := []string{"GIT_INDEX_FILE=" + indexName}
	if output, err := s.cmd.New([]string{"git", "read-tree", base}).AddEnvVars(env...).RunWithOutput(); err != nil {
		return fmt.Errorf("stash selected lines: prepare temporary index: %w: %s", err, strings.TrimSpace(output))
	}
	if output, err := s.cmd.New([]string{"git", "apply", "--cached", "--recount", patchName}).AddEnvVars(env...).RunWithOutput(); err != nil {
		return fmt.Errorf("stash selected lines: apply to temporary index: %w: %s", err, strings.TrimSpace(output))
	}
	tree, err := s.cmd.New([]string{"git", "write-tree"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return fmt.Errorf("stash selected lines: write tree: %w: %s", err, strings.TrimSpace(tree))
	}
	baseTree, err := s.gitText("rev-parse", base+"^{tree}")
	if err != nil {
		return err
	}
	identity := []string{
		"GIT_AUTHOR_NAME=TanGit", "GIT_AUTHOR_EMAIL=recovery@localhost",
		"GIT_COMMITTER_NAME=TanGit", "GIT_COMMITTER_EMAIL=recovery@localhost",
	}
	indexCommit, err := s.cmd.New([]string{"git", "commit-tree", baseTree, "-p", base, "-m", "TanGit selected-line stash index"}).AddEnvVars(identity...).RunWithOutput()
	if err != nil {
		return fmt.Errorf("stash selected lines: write index: %w: %s", err, strings.TrimSpace(indexCommit))
	}
	commit, err := s.cmd.New([]string{"git", "commit-tree", strings.TrimSpace(tree), "-p", base, "-p", strings.TrimSpace(indexCommit), "-m", message}).AddEnvVars(identity...).RunWithOutput()
	if err != nil {
		return fmt.Errorf("stash selected lines: write commit: %w: %s", err, strings.TrimSpace(commit))
	}
	if output, err := s.cmd.New([]string{"git", "stash", "store", "-m", message, strings.TrimSpace(commit)}).RunWithOutput(); err != nil {
		return fmt.Errorf("stash selected lines: store: %w: %s", err, strings.TrimSpace(output))
	}
	if err := s.applyWorkingTreePatch(ctx, removalPatch, true); err != nil {
		return fmt.Errorf("selected lines were saved, but could not be removed from the working tree: %w", err)
	}
	return nil
}

func (s *gitSession) StashStagedChanges(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Desktop stash"
	}
	if err := s.git.Stash.SaveStagedChanges(message); err != nil {
		return fmt.Errorf("stash staged changes: %w", err)
	}
	return nil
}

func (s *gitSession) ApplyStash(ctx context.Context, index int, pop bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if pop {
		err = s.git.Stash.Pop(index)
	} else {
		err = s.git.Stash.Apply(index)
	}
	if err != nil {
		return fmt.Errorf("apply stash: %w", err)
	}
	return nil
}

func (s *gitSession) RenameStash(ctx context.Context, index int, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("stash name is required")
	}
	if err := s.git.Stash.Rename(index, message); err != nil {
		return fmt.Errorf("rename stash: %w", err)
	}
	return nil
}

func (s *gitSession) DropStash(ctx context.Context, index int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Stash.Drop(index); err != nil {
		return fmt.Errorf("drop stash: %w", err)
	}
	return nil
}

func (s *gitSession) StashFiles(ctx context.Context, index int) ([]CommitFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output, err := s.cmd.New([]string{
		"git", "stash", "show", "--name-status", "-z", "-u",
		fmt.Sprintf("refs/stash@{%d}", index),
	}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("load stash files: %w", err)
	}
	files, err := parseNameStatus(output)
	if err != nil {
		return nil, fmt.Errorf("load stash files: %w", err)
	}
	return files, nil
}

// parseNameStatus reads Git's NUL-separated name-status output. A rename or
// copy carries two paths, the old then the new.
func parseNameStatus(output string) ([]CommitFile, error) {
	fields := strings.Split(output, "\x00")
	files := make([]CommitFile, 0, len(fields)/2)
	for i := 0; i < len(fields); {
		status := strings.TrimSpace(fields[i])
		i++
		if status == "" {
			continue
		}
		if i >= len(fields) {
			return nil, fmt.Errorf("malformed name-status output")
		}
		file := CommitFile{Status: status, Path: fields[i]}
		i++
		if status[0] == 'R' || status[0] == 'C' {
			if i >= len(fields) {
				return nil, fmt.Errorf("malformed rename output")
			}
			file.PreviousPath = file.Path
			file.Path = fields[i]
			i++
		}
		files = append(files, file)
	}
	return files, nil
}

func (s *gitSession) StashFileDiff(ctx context.Context, index int, file CommitFile, options DiffOptions) (Diff, error) {
	if err := ctx.Err(); err != nil {
		return Diff{}, err
	}
	optionArgs, err := diffOptionArgs(options)
	if err != nil {
		return Diff{}, fmt.Errorf("load stash file diff: %w", err)
	}
	ref := fmt.Sprintf("refs/stash@{%d}", index)
	paths := []string{file.Path}
	if file.PreviousPath != "" && file.PreviousPath != file.Path {
		paths = append(paths, file.PreviousPath)
	}
	diffArgs := append([]string{"git", "diff", "--no-color", "--no-ext-diff"}, optionArgs...)
	diffArgs = append(diffArgs, ref+"^1", ref, "--")
	diffArgs = append(diffArgs, paths...)
	patch, err := s.cmd.New(diffArgs).RunWithOutput()
	if err != nil {
		return Diff{}, fmt.Errorf("load stash file diff: %w", err)
	}
	// Untracked files ride in the stash's third parent rather than in the
	// commit the stash points at, so the tree-to-tree diff above leaves them
	// empty. Check that parent exists before reading it: an ignored
	// whitespace-only tracked change can also produce an empty primary patch.
	if strings.TrimSpace(patch) == "" {
		untracked, revErr := s.gitText("rev-parse", "--verify", "--quiet", ref+"^3")
		if revErr == nil && untracked != "" {
			untrackedArgs := append([]string{"git", "diff", "--no-color", "--no-ext-diff"}, optionArgs...)
			untrackedArgs = append(untrackedArgs, models.EmptyTreeCommitHash, ref+"^3", "--")
			untrackedArgs = append(untrackedArgs, paths...)
			patch, err = s.cmd.New(untrackedArgs).RunWithOutput()
			if err != nil {
				return Diff{}, fmt.Errorf("load stash file diff: %w", err)
			}
		}
	}
	return Diff{Patch: patch, Kind: "Stash"}, nil
}

// stashFileNames lists the paths a saved file touches: a rename carries its
// old name beside the new one.
func stashFileNames(file CommitFile) []string {
	names := []string{file.Path}
	if file.PreviousPath != "" && file.PreviousPath != file.Path {
		names = append(names, file.PreviousPath)
	}
	return names
}

// gitText runs a git subcommand and returns its output trimmed, for the
// plumbing the stash rewrite needs.
func (s *gitSession) gitText(args ...string) (string, error) {
	output, err := s.cmd.New(append([]string{"git"}, args...)).RunWithOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// treeEntry reads a path's mode and blob out of a tree, and whether the tree
// carries the path at all.
func (s *gitSession) treeEntry(tree, name string) (string, string, bool, error) {
	output, err := s.gitText("ls-tree", tree, "--", ":(literal)"+name)
	if err != nil {
		return "", "", false, err
	}
	if output == "" {
		return "", "", false, nil
	}
	fields := strings.Fields(output) // "<mode> <type> <sha>\t<name>"
	if len(fields) < 3 {
		return "", "", false, fmt.Errorf("malformed ls-tree output: %q", output)
	}
	return fields[0], fields[2], true, nil
}

// treeWithout writes a tree again without the saved change to the names
// given: each takes the base's version, or leaves when the base never had
// it. The index it works through is a scratch file, so the repository's own
// index never moves.
func (s *gitSession) treeWithout(names []string, tree, base string) (string, error) {
	scratch, err := os.CreateTemp("", "tan-git-stash-index-")
	if err != nil {
		return "", err
	}
	index := scratch.Name()
	scratch.Close()
	os.Remove(index) // git insists on making the index itself
	defer os.Remove(index)
	env := []string{"GIT_INDEX_FILE=" + index}
	if err := s.cmd.New([]string{"git", "read-tree", tree}).AddEnvVars(env...).Run(); err != nil {
		return "", err
	}
	for _, name := range names {
		baseMode, baseBlob, inBase, err := s.treeEntry(base, name)
		if err != nil {
			return "", err
		}
		if inBase {
			// Without the saved change the name holds the base's version.
			err = s.cmd.New([]string{
				"git", "update-index", "--add",
				"--cacheinfo", baseMode + "," + baseBlob + "," + name,
			}).AddEnvVars(env...).Run()
			if err != nil {
				return "", err
			}
			continue
		}
		_, _, inTree, err := s.treeEntry(tree, name)
		if err != nil {
			return "", err
		}
		if inTree {
			// The saved change added the name; without it the name is gone.
			err = s.cmd.New([]string{
				"git", "update-index", "--force-remove", "--", name,
			}).AddEnvVars(env...).Run()
			if err != nil {
				return "", err
			}
		}
	}
	built, err := s.cmd.New([]string{"git", "write-tree"}).AddEnvVars(env...).RunWithOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(built), nil
}

// commitTree writes a commit over a tree with the parents and message given,
// wearing the original author's identity (name, email and date): the rewrite
// changes the saved work, not whose work it is.
func (s *gitSession) commitTree(tree string, parents []string, message string, author []string) (string, error) {
	args := []string{"git", "commit-tree", tree}
	for _, parent := range parents {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", strings.TrimSpace(message))
	output, err := s.cmd.New(args).AddEnvVars(
		"GIT_AUTHOR_NAME="+author[0],
		"GIT_AUTHOR_EMAIL="+author[1],
		"GIT_AUTHOR_DATE="+author[2],
	).RunWithOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// RestoreCommitFile writes one historical file version to the working tree.
// A path absent from that commit is removed. Like ApplyStashFile, this never
// stages the result: the index stays exactly as the user left it.
func (s *gitSession) RestoreCommitFile(ctx context.Context, commit Commit, file CommitFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(commit.Hash) == "" {
		return fmt.Errorf("restore commit file: no commit chosen")
	}
	for _, name := range stashFileNames(file) {
		_, _, present, err := s.treeEntry(commit.Hash+"^{tree}", name)
		if err != nil {
			return fmt.Errorf("restore commit file: %w", err)
		}
		if present {
			if err := s.cmd.New([]string{
				"git", "restore", "--source", commit.Hash, "--worktree", "--", name,
			}).Run(); err != nil {
				return fmt.Errorf("restore commit file: %w", err)
			}
			continue
		}
		if err := s.dropWorktreeFile(name); err != nil {
			return fmt.Errorf("restore commit file: %w", err)
		}
	}
	return nil
}

// ApplyStashFile writes one saved file's version into the working tree and
// leaves the saved work saved: the stash still lists the file afterwards.
func (s *gitSession) ApplyStashFile(ctx context.Context, index int, file CommitFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref := fmt.Sprintf("refs/stash@{%d}", index)
	// A saved version sits in the work commit's tree; a file that was
	// untracked when saved rides in a small commit beside it, so look
	// there too before calling the name a saved deletion.
	sources := []string{ref}
	if untracked, err := s.gitText("rev-parse", "--verify", "--quiet", ref+"^3"); err == nil && untracked != "" {
		sources = append(sources, untracked)
	}
	for _, name := range stashFileNames(file) {
		source := ""
		for _, candidate := range sources {
			_, _, saved, err := s.treeEntry(candidate+"^{tree}", name)
			if err != nil {
				return fmt.Errorf("apply stash file: %w", err)
			}
			if saved {
				source = candidate
				break
			}
		}
		if source != "" {
			// The saved version lands in the working tree alone. A plain
			// `git checkout <ref> -- <path>` would stage it as well.
			if err := s.cmd.New([]string{
				"git", "restore", "--source", source, "--worktree", "--", name,
			}).Run(); err != nil {
				return fmt.Errorf("apply stash file: %w", err)
			}
			continue
		}
		// The stash saved a deletion, or a rename's old name: taking it
		// takes the file out of the working tree.
		if err := s.dropWorktreeFile(name); err != nil {
			return fmt.Errorf("apply stash file: %w", err)
		}
	}
	return nil
}

// dropWorktreeFile removes one file from the working tree alone. The index
// keeps the file, so the deletion shows as an unstaged change rather than
// being staged behind the user's back.
func (s *gitSession) dropWorktreeFile(name string) error {
	root, err := s.gitText("rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	// The name comes from git's own listing; the join keeps it within the
	// working tree all the same.
	target := filepath.Join(root, filepath.Clean("/"+name))
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// DiscardStashFile rewrites the saved work without one file's change: the
// rest stays saved, as if the file had never been stashed, and the entry
// keeps its name. The rewritten entry is stored before the old one drops, so
// an interrupted rewrite leaves the saved work twice rather than not at all.
func (s *gitSession) DiscardStashFile(ctx context.Context, index int, file CommitFile, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ref := fmt.Sprintf("refs/stash@{%d}", index)
	names := stashFileNames(file)

	// A stash is two or three commits around the saved work: the work
	// itself, the index kept beside it, and untracked files when there
	// were any. Each needs the file's change undone, against the commit
	// the work was taken from.
	parents, err := s.gitText("rev-list", "--parents", "-n", "1", ref)
	if err != nil {
		return fmt.Errorf("discard stash file: %w", err)
	}
	parts := strings.Fields(parents)
	if len(parts) < 2 {
		return fmt.Errorf("discard stash file: %s holds no saved work", ref)
	}
	work, base := parts[0], parts[1]

	authorText, err := s.gitText("log", "-1", "--format=%an%x00%ae%x00%aI", work)
	if err != nil {
		return fmt.Errorf("discard stash file: %w", err)
	}
	author := strings.Split(authorText, "\x00")
	if len(author) != 3 {
		return fmt.Errorf("discard stash file: %s has no author to keep", ref)
	}

	workTree, err := s.treeWithout(names, work+"^{tree}", base+"^{tree}")
	if err != nil {
		return fmt.Errorf("discard stash file: %w", err)
	}
	keep := []string{base}
	if len(parts) >= 3 {
		kept := parts[2]
		before, err := s.gitText("rev-parse", kept+"^{tree}")
		if err != nil {
			return fmt.Errorf("discard stash file: %w", err)
		}
		indexTree, err := s.treeWithout(names, before, base+"^{tree}")
		if err != nil {
			return fmt.Errorf("discard stash file: %w", err)
		}
		if indexTree != before {
			message, err := s.gitText("log", "-1", "--format=%B", kept)
			if err != nil {
				return fmt.Errorf("discard stash file: %w", err)
			}
			kept, err = s.commitTree(indexTree, []string{base}, message, author)
			if err != nil {
				return fmt.Errorf("discard stash file: %w", err)
			}
		}
		keep = append(keep, kept)
	}
	if len(parts) >= 4 {
		// Untracked files ride in the third parent — a root commit holding
		// only them against the empty tree: without the saved change the
		// file simply leaves it.
		kept := parts[3]
		before, err := s.gitText("rev-parse", kept+"^{tree}")
		if err != nil {
			return fmt.Errorf("discard stash file: %w", err)
		}
		untrackedTree, err := s.treeWithout(names, before, models.EmptyTreeCommitHash)
		if err != nil {
			return fmt.Errorf("discard stash file: %w", err)
		}
		if untrackedTree != before {
			message, err := s.gitText("log", "-1", "--format=%B", kept)
			if err != nil {
				return fmt.Errorf("discard stash file: %w", err)
			}
			kept, err = s.commitTree(untrackedTree, nil, message, author)
			if err != nil {
				return fmt.Errorf("discard stash file: %w", err)
			}
		}
		keep = append(keep, kept)
	}

	message, err := s.gitText("log", "-1", "--format=%B", work)
	if err != nil {
		return fmt.Errorf("discard stash file: %w", err)
	}
	rewritten, err := s.commitTree(workTree, keep, message, author)
	if err != nil {
		return fmt.Errorf("discard stash file: %w", err)
	}

	// The store puts the rewritten entry on the top: the old one, now held
	// one entry deeper, drops after it.
	if err := s.git.Stash.Store(rewritten, name); err != nil {
		return fmt.Errorf("discard stash file: %w", err)
	}
	if err := s.git.Stash.Drop(index + 1); err != nil {
		return fmt.Errorf("discard stash file: %w", err)
	}
	return nil
}

func (s *gitSession) CreateTag(ctx context.Context, name string, ref string, message string, annotated bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	ref = strings.TrimSpace(ref)
	if name == "" {
		return fmt.Errorf("tag name is required")
	}
	var err error
	if annotated {
		message = strings.TrimSpace(message)
		if message == "" {
			return fmt.Errorf("annotated tag message is required")
		}
		err = s.git.Tag.CreateAnnotatedObj(name, ref, message, false).Run()
	} else {
		err = s.git.Tag.CreateLightweightObj(name, ref, false).Run()
	}
	if err != nil {
		return fmt.Errorf("create tag: %w", err)
	}
	return nil
}

func (s *gitSession) DeleteTag(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Tag.LocalDelete([]string{name}); err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

func (s *gitSession) PushTag(ctx context.Context, remote string, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return fmt.Errorf("remote name is required")
	}
	if err := s.git.Tag.Push(nil, remote, []string{name}); err != nil {
		return fmt.Errorf("push tag: %w", err)
	}
	return nil
}

func (s *gitSession) CherryPickCommit(ctx context.Context, commit Commit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Cherry-pick only needs the hash and whether the commit is a merge, so
	// the model is built from the commit the app holds. Reading it back from
	// a bounded recent list would fail for a commit deeper than that page.
	model := models.NewCommit(&utils.StringPool{}, models.NewCommitOpts{
		Hash:    commit.Hash,
		Parents: append([]string(nil), commit.Parents...),
	})
	if err := s.git.Rebase.CherryPickCommits([]*models.Commit{model}); err != nil {
		return fmt.Errorf("cherry-pick commit: %w", err)
	}
	return nil
}

func (s *gitSession) RevertCommit(ctx context.Context, commit Commit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Commit.Revert([]string{commit.Hash}, len(commit.Parents) > 1); err != nil {
		return fmt.Errorf("revert commit: %w", err)
	}
	return nil
}

// ResetToCommit moves HEAD to commit. Soft keeps the difference staged, mixed
// keeps it only in the working tree, and hard discards it.
func (s *gitSession) ResetToCommit(ctx context.Context, commit Commit, mode ResetMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch mode {
	case ResetSoft, ResetMixed, ResetHard:
	default:
		return fmt.Errorf("reset to %s: invalid mode %q", commit.ShortHash, mode)
	}
	if err := s.git.Commit.ResetToCommit(commit.Hash, string(mode), nil); err != nil {
		return fmt.Errorf("%s reset to %s: %w", mode, commit.ShortHash, err)
	}
	return nil
}

// CheckoutCommit checks out commit without moving or creating a branch.
func (s *gitSession) CheckoutCommit(ctx context.Context, commit Commit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.cmd.New([]string{"git", "checkout", "--detach", commit.Hash}).Run(); err != nil {
		return fmt.Errorf("checkout commit %s: %w", commit.ShortHash, err)
	}
	return nil
}

func (s *gitSession) ConflictStages(ctx context.Context, file File) (ConflictStages, error) {
	if err := ctx.Err(); err != nil {
		return ConflictStages{}, err
	}
	if !file.HasMergeConflicts {
		return ConflictStages{}, fmt.Errorf("read conflict: %s is not conflicted", file.Path)
	}
	var stages ConflictStages
	for stage, destination := range map[int]*ConflictStage{1: &stages.Base, 2: &stages.Current, 3: &stages.Incoming} {
		present, err := s.indexHasStage(file.Path, stage)
		if err != nil {
			return ConflictStages{}, fmt.Errorf("read conflict stage %d: %w", stage, err)
		}
		destination.Present = present
		if !present {
			continue
		}
		output, err := s.cmd.New([]string{"git", "show", fmt.Sprintf(":%d:%s", stage, file.Path)}).RunWithOutput()
		if err != nil {
			return ConflictStages{}, fmt.Errorf("read conflict stage %d: %w: %s", stage, err, strings.TrimSpace(output))
		}
		destination.Content = output
	}
	return stages, nil
}

func (s *gitSession) ResolveConflict(ctx context.Context, file File, current bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !file.HasMergeConflicts {
		return fmt.Errorf("resolve conflict: %s is not conflicted", file.Path)
	}
	if err := s.resolveFileConflict(file.Path, current); err != nil {
		return fmt.Errorf("resolve conflict: %w", err)
	}
	return nil
}

func (s *gitSession) ContinueOperation(ctx context.Context) error {
	return s.finishOperation(ctx, "continue")
}

func (s *gitSession) AbortOperation(ctx context.Context) error {
	return s.finishOperation(ctx, "abort")
}

func (s *gitSession) finishOperation(ctx context.Context, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state := s.git.Status.WorkingTreeState()
	if state.None() {
		return fmt.Errorf("no merge, rebase, cherry-pick, or revert is in progress")
	}
	operation := state.CommandName()
	if err := s.finishGitOperation(operation, action); err != nil {
		return fmt.Errorf("%s %s: %w", action, operation, err)
	}
	return nil
}

func (s *gitSession) CreateWorktree(ctx context.Context, path string, base string, branch string, detach bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	base = strings.TrimSpace(base)
	branch = strings.TrimSpace(branch)
	if path == "" || base == "" {
		return fmt.Errorf("worktree path and base ref are required")
	}
	if err := s.git.Worktree.New(git_commands.NewWorktreeOpts{Path: path, Base: base, Branch: branch, Detach: detach}); err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}
	return nil
}

func (s *gitSession) DeleteWorktree(ctx context.Context, path string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Worktree.Delete(path, force); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	return nil
}

func (s *gitSession) AddSubmodule(ctx context.Context, name string, path string, url string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	path = strings.TrimSpace(path)
	url = strings.TrimSpace(url)
	if name == "" || path == "" || url == "" {
		return fmt.Errorf("submodule name, path, and URL are required")
	}
	if urlContainsCredentials(url) {
		return fmt.Errorf("add submodule: %w", ErrCredentialsInURL)
	}
	if err := s.git.Submodule.Add(name, path, url); err != nil {
		return fmt.Errorf("add submodule: %w", err)
	}
	return nil
}

func (s *gitSession) UpdateSubmodule(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.git.Submodule.Update(path); err != nil {
		return fmt.Errorf("update submodule: %w", err)
	}
	return nil
}

func (s *gitSession) DeleteSubmodule(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	configs, err := s.git.Submodule.GetConfigs(nil)
	if err != nil {
		return fmt.Errorf("load submodule: %w", err)
	}
	for _, config := range configs {
		if config.FullPath() == path {
			if err := s.git.Submodule.Delete(config); err != nil {
				return fmt.Errorf("delete submodule: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("delete submodule: path %s is no longer configured", path)
}

// ignoreRule is the repository-rooted, escaped form Git expects in ignore
// files. In particular a leading # or ! must not become a comment or negation.
func ignoreRule(path string) string {
	var rule strings.Builder
	rule.Grow(len(path) + 4)
	rule.WriteByte('/')
	for i := 0; i < len(path); i++ {
		char := path[i]
		if (i == 0 && (char == '#' || char == '!')) || strings.ContainsRune("[]*", rune(char)) {
			rule.WriteByte('\\')
		}
		rule.WriteByte(char)
	}
	return rule.String()
}

// appendIgnoreRule writes beside the repository, never relative to TanGit's
// process working directory. Lazygit's high-level Ignore helper uses the
// latter, which is correct in its terminal process but not in this desktop one.
func appendIgnoreRule(filename, path string) error {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return err
	} else if info.Size() > 0 {
		last := []byte{0}
		if _, err := file.ReadAt(last, info.Size()-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			if _, err := file.WriteString("\n"); err != nil {
				return err
			}
		}
	}
	_, err = file.WriteString(ignoreRule(path) + "\n")
	return err
}

func (s *gitSession) IgnoreFile(ctx context.Context, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if file.Tracked || file.Staged {
		return fmt.Errorf("ignore untracked, unstaged file: %s is tracked or staged", file.Path)
	}
	filename := filepath.Join(s.git.RepoPaths.WorktreePath(), ".gitignore")
	if err := appendIgnoreRule(filename, file.Path); err != nil {
		return fmt.Errorf("ignore file: %w", err)
	}
	return nil
}

func (s *gitSession) ExcludeFile(ctx context.Context, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if file.Tracked || file.Staged {
		return fmt.Errorf("exclude untracked, unstaged file: %s is tracked or staged", file.Path)
	}
	infoDir := filepath.Join(s.git.RepoPaths.RepoGitDirPath(), "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("exclude file: %w", err)
	}
	if err := appendIgnoreRule(filepath.Join(infoDir, "exclude"), file.Path); err != nil {
		return fmt.Errorf("exclude file: %w", err)
	}
	return nil
}

func (s *gitSession) currentBranch() (*models.Branch, error) {
	branches, err := s.localBranches()
	if err != nil {
		return nil, fmt.Errorf("load current branch: %w", err)
	}
	for _, branch := range branches {
		if branch.Head && !branch.DetachedHead {
			return branch, nil
		}
	}
	return nil, fmt.Errorf("operation requires a local branch checkout")
}

func (s *gitSession) CommitFiles(ctx context.Context, commit Commit) ([]CommitFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	from := models.EmptyTreeCommitHash
	if len(commit.Parents) > 0 {
		from = commit.Parents[0]
	}
	files, err := s.git.Loaders.CommitFileLoader.GetFilesInDiff(from, commit.Hash, false)
	if err != nil {
		return nil, fmt.Errorf("load commit files: %w", err)
	}
	output, err := s.cmd.New([]string{
		"git", "diff", "--no-ext-diff", "--numstat", "-z",
		fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
		from, commit.Hash,
	}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("load commit line statistics: %w", err)
	}
	stats := parseCommitLineStats(output)
	result := make([]CommitFile, 0, len(files))
	for _, file := range files {
		stat := stats[file.Path]
		result = append(result, CommitFile{
			Path: file.Path, PreviousPath: file.PreviousPath, Status: file.ChangeStatus,
			LinesAdded: stat.added, LinesDeleted: stat.deleted,
		})
	}
	return result, nil
}

type commitLineStats struct {
	added   int
	deleted int
}

// parseCommitLineStats reads git diff --numstat -z. Ordinary records hold
// "added\tdeleted\tpath"; a rename has an empty path followed by separate old
// and new path fields. Binary files use dashes, which correctly count as no
// textual line additions or deletions.
func parseCommitLineStats(output string) map[string]commitLineStats {
	stats := make(map[string]commitLineStats)
	fields := strings.Split(output, "\x00")
	for i := 0; i < len(fields); {
		columns := strings.SplitN(fields[i], "\t", 3)
		i++
		if len(columns) != 3 {
			continue
		}
		added, addedErr := strconv.Atoi(columns[0])
		deleted, deletedErr := strconv.Atoi(columns[1])
		if addedErr != nil {
			added = 0
		}
		if deletedErr != nil {
			deleted = 0
		}
		path := columns[2]
		if path == "" {
			// Skip the old path and key the rename by its new path, matching
			// the name-status records returned by the commit file loader.
			if i+1 >= len(fields) {
				break
			}
			path = fields[i+1]
			i += 2
		}
		if path != "" {
			stats[path] = commitLineStats{added: added, deleted: deleted}
		}
	}
	return stats
}

func (s *gitSession) CommitFileDiff(ctx context.Context, commit Commit, file CommitFile, options DiffOptions) (Diff, error) {
	if err := ctx.Err(); err != nil {
		return Diff{}, err
	}
	optionArgs, err := diffOptionArgs(options)
	if err != nil {
		return Diff{}, fmt.Errorf("load commit file diff: %w", err)
	}
	from := models.EmptyTreeCommitHash
	if len(commit.Parents) > 0 {
		from = commit.Parents[0]
	}
	args := append([]string{
		"git", "diff", "--no-color", "--no-ext-diff", "--submodule",
		fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
	}, optionArgs...)
	args = append(args, from, commit.Hash, "--", file.Path)
	if file.PreviousPath != "" && file.PreviousPath != file.Path {
		args = append(args, file.PreviousPath)
	}
	patch, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return Diff{}, fmt.Errorf("load commit file diff: %w", err)
	}
	return Diff{Patch: patch, Kind: "Commit"}, nil
}

// compareArgs builds the common, option-safe part of a tree-to-tree diff.
// --end-of-options keeps a ref that starts with a dash from becoming a Git
// option; command arguments are never interpolated into a shell command.
func (s *gitSession) compareArgs(from, to, outputMode string, options DiffOptions) ([]string, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return nil, fmt.Errorf("choose both refs to compare")
	}
	optionArgs, err := diffOptionArgs(options)
	if err != nil {
		return nil, err
	}
	args := []string{
		"git", "diff", "--no-color", "--no-ext-diff",
		fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold),
	}
	args = append(args, optionArgs...)
	if outputMode != "" {
		args = append(args, outputMode, "-z")
	}
	return append(args, "--end-of-options", from, to), nil
}

func (s *gitSession) CompareFiles(ctx context.Context, from, to string, options DiffOptions) ([]CommitFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nameArgs, err := s.compareArgs(from, to, "--name-status", options)
	if err != nil {
		return nil, fmt.Errorf("load comparison files: %w", err)
	}
	output, err := s.cmd.New(nameArgs).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("load comparison files: %w", err)
	}
	files, err := parseNameStatus(output)
	if err != nil {
		return nil, fmt.Errorf("load comparison files: %w", err)
	}

	statArgs, err := s.compareArgs(from, to, "--numstat", options)
	if err != nil {
		return nil, fmt.Errorf("load comparison line statistics: %w", err)
	}
	output, err = s.cmd.New(statArgs).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("load comparison line statistics: %w", err)
	}
	stats := parseCommitLineStats(output)
	for i := range files {
		stat := stats[files[i].Path]
		files[i].LinesAdded = stat.added
		files[i].LinesDeleted = stat.deleted
	}
	return files, nil
}

func (s *gitSession) CompareFileDiff(ctx context.Context, from, to string, file CommitFile, options DiffOptions) (Diff, error) {
	if err := ctx.Err(); err != nil {
		return Diff{}, err
	}
	args, err := s.compareArgs(from, to, "", options)
	if err != nil {
		return Diff{}, fmt.Errorf("load comparison file diff: %w", err)
	}
	paths := []string{file.Path}
	if file.PreviousPath != "" && file.PreviousPath != file.Path {
		paths = append(paths, file.PreviousPath)
	}
	args = append(args, "--")
	args = append(args, paths...)
	patch, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return Diff{}, fmt.Errorf("load comparison file diff: %w", err)
	}
	return Diff{Patch: patch, Kind: "Compare"}, nil
}

// BranchCommits reads the commits of branch with `git log <branch>`, so a
// branch's history is browsable without checking it out. limit bounds the
// list like the repository snapshot does.
func (s *gitSession) BranchCommits(ctx context.Context, branch string, limit int) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return nil, fmt.Errorf("read branch commits: no branch chosen")
	}
	modelCommits, err := s.branchCommits(branch, limit)
	if err != nil {
		return nil, fmt.Errorf("read commits of %s: %w", branch, err)
	}
	commits := make([]Commit, 0, len(modelCommits))
	for _, commit := range modelCommits {
		commits = append(commits, snapshotCommit(commit))
	}
	return commits, nil
}

// commitLogFormat is one commit per line: its fields separated by the unit
// separator, none of which a hash, name, date, parent list or subject line
// contains, so parsing stays unambiguous.
const commitLogFormat = "--format=%H%x1f%h%x1f%an%x1f%ae%x1f%at%x1f%P%x1f%D%x1f%s"

// SearchCommits reads up to filter.Limit commits matching the filter with
// `git log`, newest first. Filtering runs in Git rather than on the page the
// snapshot carries, so a match older than that page is still found, and a
// branch's own history can be searched without checking it out.
func (s *gitSession) SearchCommits(ctx context.Context, filter CommitFilter) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if filter.Limit <= 0 {
		return nil, fmt.Errorf("search commits: no limit chosen")
	}
	args := []string{"git", "log", fmt.Sprintf("--max-count=%d", filter.Limit), commitLogFormat}
	if query := strings.TrimSpace(filter.Query); query != "" {
		// A commit message is prose, so the words are matched literally and
		// without regard to case rather than as a regular expression.
		args = append(args, "--grep", query, "--fixed-strings", "--regexp-ignore-case")
	}
	if author := strings.TrimSpace(filter.Author); author != "" {
		args = append(args, "--author", author, "--regexp-ignore-case")
	}
	branch := strings.TrimSpace(filter.Branch)
	if branch != "" {
		if strings.HasPrefix(branch, "-") {
			return nil, fmt.Errorf("search commits: invalid branch %q", branch)
		}
		args = append(args, branch)
	} else {
		// History is every branch's history, like the snapshot it shows.
		args = append(args, "--all")
	}
	args = append(args, "--")
	output, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("search commits: %w", err)
	}
	return parseCommitLog(output), nil
}

// parseCommitLog reads the lines SearchCommits asked Git for.
func parseCommitLog(output string) []Commit {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	commits := make([]Commit, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x1f")
		if len(fields) < 8 {
			continue
		}
		timestamp := int64(0)
		if _, err := fmt.Sscanf(fields[4], "%d", &timestamp); err != nil {
			timestamp = 0
		}
		var parents []string
		if fields[5] != "" {
			parents = strings.Fields(fields[5])
		}
		commits = append(commits, Commit{
			Hash:          fields[0],
			ShortHash:     fields[1],
			AuthorName:    fields[2],
			AuthorEmail:   fields[3],
			UnixTimestamp: timestamp,
			Parents:       parents,
			Tags:          commitLogTags(fields[6]),
			Subject:       fields[7],
		})
	}
	return commits
}

// commitLogTags picks the tags out of a commit's decorations, which also name
// branches and HEAD.
func commitLogTags(decorations string) []string {
	var tags []string
	for _, decoration := range strings.Split(decorations, ",") {
		decoration = strings.TrimSpace(decoration)
		if tag, ok := strings.CutPrefix(decoration, "tag: "); ok && tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// Authors lists the distinct author names of the whole history, sorted, so
// the filter can offer the repository's authors. Reading just the names of
// the commits is far cheaper than reading the commits themselves.
func (s *gitSession) Authors(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output, err := s.cmd.New([]string{"git", "log", "--all", "--format=%an"}).RunWithOutput()
	if err != nil {
		return nil, fmt.Errorf("read commit authors: %w", err)
	}
	seen := make(map[string]struct{})
	authors := make([]string, 0, 16)
	for _, line := range strings.Split(output, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		authors = append(authors, name)
	}
	sort.Slice(authors, func(i, j int) bool {
		left, right := strings.ToLower(authors[i]), strings.ToLower(authors[j])
		if left == right {
			return authors[i] < authors[j]
		}
		return left < right
	})
	return authors, nil
}

// FileCommits reads up to limit commits that changed path, newest first,
// skipping the newest skip, following renames across the history, so the
// file's own history is browsable without a checkout and pages in as the
// drawer scrolls. Git is asked for the skipped and wanted commits together,
// since its log takes a count from the newest commit and no offset.
func (s *gitSession) FileCommits(ctx context.Context, path string, limit, skip int) ([]Commit, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("read file history: no path chosen")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("read file history: no limit chosen")
	}
	if skip < 0 {
		skip = 0
	}
	modelCommits, err := s.fileCommits(path, limit+skip)
	if err != nil {
		return nil, fmt.Errorf("read the history of %s: %w", path, err)
	}
	if skip >= len(modelCommits) {
		return nil, nil
	}
	modelCommits = modelCommits[skip:]
	commits := make([]Commit, 0, len(modelCommits))
	for _, commit := range modelCommits {
		commits = append(commits, snapshotCommit(commit))
	}
	return commits, nil
}

func worktreeFilePath(root string, path string) (string, error) {
	cleanPath := filepath.Clean(path)
	if cleanPath == "." || filepath.IsAbs(cleanPath) || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid repository path %q", path)
	}
	return filepath.Join(root, cleanPath), nil
}

func fileNames(file File) []string {
	names := []string{file.Path}
	if file.PreviousPath != "" {
		names = append(names, file.PreviousPath)
	}
	return names
}

func snapshotFile(file *models.File) File {
	return File{
		Path:                    file.Path,
		PreviousPath:            file.PreviousPath,
		Status:                  file.ShortStatus,
		Staged:                  file.HasStagedChanges,
		Unstaged:                file.HasUnstagedChanges,
		Tracked:                 file.Tracked,
		Added:                   file.Added,
		Deleted:                 file.Deleted,
		HasMergeConflicts:       file.HasMergeConflicts,
		HasInlineMergeConflicts: file.HasInlineMergeConflicts,
		ConflictMarkerSize:      file.ConflictMarkerSize,
		LinesAdded:              file.LinesAdded,
		LinesDeleted:            file.LinesDeleted,
	}
}

func modelFile(file File) *models.File {
	return &models.File{
		Path:                    file.Path,
		PreviousPath:            file.PreviousPath,
		ShortStatus:             file.Status,
		HasStagedChanges:        file.Staged,
		HasUnstagedChanges:      file.Unstaged,
		Tracked:                 file.Tracked,
		Added:                   file.Added,
		Deleted:                 file.Deleted,
		HasMergeConflicts:       file.HasMergeConflicts,
		HasInlineMergeConflicts: file.HasInlineMergeConflicts,
		ConflictMarkerSize:      file.ConflictMarkerSize,
		LinesAdded:              file.LinesAdded,
		LinesDeleted:            file.LinesDeleted,
	}
}

func snapshotCommit(commit *models.Commit) Commit {
	return Commit{
		Hash:          commit.Hash(),
		ShortHash:     commit.ShortHash(),
		Subject:       commit.Name,
		AuthorName:    commit.AuthorName,
		AuthorEmail:   commit.AuthorEmail,
		UnixTimestamp: commit.UnixTimestamp,
		Tags:          append([]string(nil), commit.Tags...),
		Parents:       append([]string(nil), commit.Parents()...),
	}
}

func snapshotBranch(branch *models.Branch) Branch {
	return Branch{
		Name:           branch.Name,
		DisplayName:    branch.DisplayName,
		Head:           branch.Head,
		DetachedHead:   branch.DetachedHead,
		UpstreamRemote: branch.UpstreamRemote,
		UpstreamBranch: branch.UpstreamBranch,
		Ahead:          branch.AheadForPull,
		Behind:         branch.BehindForPull,
		UpstreamGone:   branch.UpstreamGone,
		Subject:        branch.Subject,
	}
}
