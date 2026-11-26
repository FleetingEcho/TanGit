package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/FleetingEcho/lazygitGo/internal/app/launch"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/jesseduffield/lazygit/pkg/app/daemon"
	"github.com/sirupsen/logrus"
	"golang.org/x/term"

	"github.com/FleetingEcho/lazygitGo/internal/agentcli"
	"github.com/FleetingEcho/lazygitGo/internal/agentwork"
	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/sshworkspace"
	"github.com/FleetingEcho/lazygitGo/internal/workspace"
)

type openState uint8

// appName is the product name window titles and the picker show.
const appName = "lazygitGo"

const (
	stateEmpty openState = iota
	stateLoading
	stateReady
	stateError
	stateCloning
)

type repositoryOpener interface {
	Open(context.Context, string) (repository.Session, error)
	// Init makes a folder a Git repository, so the picker can adopt a
	// folder that has none yet.
	Init(context.Context, string) error
}

type app struct {
	// Keyboard state belongs to the UI thread; only stable handles survive builds.
	keyboardDiffCanSelect      bool
	keyboardDiffCanMutateHunks bool
	keyboardTextFocus          bool
	keyboardFocusVisible       bool
	keyboardModalWasOpen       bool
	keyboardModalReturn        string
	keyboardLastPane           string
	keyboardCurrentGroup       string
	keybindingsScope           string
	keyboardPrevious           []keyboardPane
	keyboardLastList           *ui.ListState
	keyboardLineAnchor         int
	keyboardCopied             []repository.Commit
	keyboardPanes              []keyboardPane
	keyboardHandles            map[string]*ui.Handle
	keyboardFocus              string
	keyboardSection            int
	keyboardSectionKnown       bool
	keyboardMoved              map[*ui.ListState]bool

	opener repositoryOpener
	update func(func())
	// updateChanged applies a background read on the UI thread without
	// repainting when its callback reports that nothing visible changed.
	updateChanged func(func() bool)
	window        *mygo.Window
	// services is refreshed from the active window so keyboard-only actions
	// can use the same clipboard and browser services as pointer actions.
	services ui.Services

	path string
	// location is the stable local/SSH identity used by tabs and recents.
	// snapshot.Root remains the native root used by repository operations.
	location       workspace.Location
	state          openState
	snapshot       repository.Snapshot
	session        repository.Session
	repositoryWork repositoryScope
	errMessage     string
	generation     uint64
	cancelOpen     context.CancelFunc
	// clone fields belong to Home's remote-clone form and progress view. The
	// generation invalidates native-picker, progress and completion callbacks.
	cloneOpen              bool
	cloneURL               string
	cloneBranch            string
	cloneDestination       string
	cloneParent            string
	cloneDestinationManual bool
	cloneValidationShown   bool
	cloneURLError          string
	cloneBranchError       string
	cloneDestinationError  string
	// cloneModeChoice is the translated label of the chosen clone mode; the
	// depth, single-branch and sparse fields only apply to some modes.
	cloneModeChoice   string
	cloneDepth        string
	cloneSingleBranch bool
	cloneSparse       string
	cloneDepthError   string
	cloneSparseError  string
	cloneError        string
	cloneProgress     repository.CloneProgress
	cloneGeneration   uint64
	cancelClone       context.CancelFunc
	// pickerOpenMode is Home's transient Git/Editor choice. It is request
	// policy, not the active workspace's identity and not a persisted setting.
	pickerOpenMode int
	// offerInitPath is the folder the picker offers to initialize, because
	// it holds no Git repository yet.
	offerInitPath string
	// The tab hooks connect the app to the window around it, and are nil
	// when it runs on its own (tests), which hides the tab controls.
	//
	// openTab opens a repository in a page of its own. openTerminalTab also
	// reveals that page's integrated terminal once its repository is ready.
	// openHomeTab shows the picker in one, claimOpen reports that an open was
	// taken over by an existing page or the window's cap, and closeTab gives
	// this app's page up.
	openTab         func(path string, policy openPolicy)
	openTerminalTab func(path string)
	openHomeTab     func()
	claimOpen       func(path string) bool
	closeTab        func()
	// Shell-owned workflows are hooked rather than stored MyGo elements or
	// contexts, keeping standalone app tests independent of window chrome.
	shellOverlayOpen   func() bool
	showActivityCenter func()
	showContributors   func()
	showCommandPalette func()
	invokeCommand      func(commandID) commandInvokeResult
	resolveCommand     func(commandID) commandState
	openActivityTarget func(activityTarget) activityOpenResult
	// onRecentRenamed carries a name given to one page's repository to the
	// window's other pages, so a pill, a card, and the next opening agree.
	onRecentRenamed func(path, name string)
	// externalRefresh lets the shell limit filesystem-triggered reads to the
	// page the user can see. Nil means a standalone app refreshes directly.
	externalRefresh func()
	// cacheOpenRoots reports every open tab's repository root, so reclaiming
	// caches for a forgotten card never deletes data another tab is using.
	cacheOpenRoots func() []string
	// onCachesCleared lets the shell cancel and invalidate every tab's index
	// when the user clears cache, before or after the affected files are removed.
	onCachesCleared func()
	// onZoektPreferenceChanged cancels every tab's builders for the same local
	// repository before its cache is deleted, including non-visible tabs.
	onZoektPreferenceChanged func(canonicalRoot string)

	// term owns the shared dock's shells, tabs, splits, edge and restored size.
	term terminalState
	// openTerminalOnReady belongs to this tab only. A new open or failed open
	// cancels it so an earlier worktree request cannot target another root.
	openTerminalOnReady bool
	// agents links durable tasks to this repository family's worktrees.
	agents           agentUIState
	assistance       assistUIState
	agentIntegration agentIntegrationUIState
	agentStore       *agentwork.Store

	// remoteTransfers owns this page's lazy transfer manager and all of its
	// callbacks. The shell closes it before replacing or shutting down a page.
	remoteTransfers remoteTransferUIState
	// localTransfers tracks independent cancellable native-drop imports.
	localTransfers localTransferUIState

	// pathUserData caches the per-user directory the history is stored in,
	// "" until a window exists to resolve it from. pathCache holds reproducible
	// data such as Zoekt shards outside every repository working tree.
	pathUserData       string
	pathCache          string
	cacheCleanupBusy   bool
	cacheCleanupSerial uint64
	cacheUsage         cacheUsageState
	cacheRepositories  cacheRepositoriesState
	authorAvatars      *authorAvatarStore
	recentRepositories []recentRepository
	// historyLoading and historyLoaded keep filesystem reads away from
	// picker creation and rendering. Results join the UI-owned history only
	// through update, preserving renames and openings made while it reads.
	// recentNameOverrides also retains a cross-tab rename whose target card has
	// not arrived from that tab's delayed history read yet.
	historyLoading      bool
	historyLoaded       bool
	historyRevision     uint64
	recentNameOverrides map[string]string
	// recentFilter narrows the cards by their display name or path.
	// visibleRecent holds original history indices for the filtered grid, so
	// opening, renaming, and removing a match always affect the right entry.
	recentFilter   string
	visibleRecent  []int
	selectedRecent int
	homeWorktrees  homeWorktreePickerState

	// pathCopiedAt is when the status bar's path was put on the clipboard,
	// zero while the bar shows the path itself. It shows "Copied" in its
	// place for half a second, then the path again.
	pathCopiedAt time.Time

	// settings is the persisted preferences the settings modal edits.
	settings       appSettings
	settingsLoaded bool
	settingsUI     settingsUIState
	ssh            sshUIState
	lsp            languageServerUIState

	// SSH profiles persist in settings, but their credentials never do. These
	// dependencies and session secrets outlive any individual picker or form.
	sshCreateEntry    func(context.Context, sshworkspace.Profile, string, string, bool) (sshFolderListing, error)
	sshBrowseFolder   func(context.Context, sshworkspace.Profile, string, bool) (sshFolderListing, error)
	sshSecretBackend  sshKeychain
	sshSessionSecrets map[string]string
	sshSecretsMu      sync.RWMutex
	sshTestConnection func(context.Context, sshworkspace.Profile, sshProfileSecrets) error
	// The shell supplies the process-wide updater to each tab. Tests and
	// standalone app views keep harmless defaults.
	checkForUpdates func()
	updateInfo      func() appUpdateInfo
	// keybindingsOpen shows the shortcut reference, with the switch that
	// turns every shortcut off; keybindingFilter narrows its list.
	keybindingsOpen        bool
	keybindingsCurrentOnly bool
	keybindingFilter       string
	// commandLogOpen shows every Git command the session ran and what it
	// printed; commandLog is that record, nil until a repository opens.
	// openExternalURL launches external URLs in the user's desktop browser,
	// never on an SSH server. Header capture binds this to a live UI service.
	openExternalURL   func(string)
	commandLogOpen    bool
	commandLog        *repository.CommandLog
	commandLogVisible atomic.Bool
	// paletteOutputVisible is set while the command palette shows this app's
	// shell-command output, which then needs the same live repaints as the log.
	paletteOutputVisible atomic.Bool
	// commandLogFramePending coalesces output bursts into one queued UI
	// publication, which checks tab and modal visibility when it runs.
	commandLogFramePending atomic.Bool
	// Custom shell jobs have one owner and a real context so closing a
	// repository cannot leave a job running against a different checkout.
	customCommandSeq     uint64
	customCommandRunning bool
	customCommandCancel  context.CancelFunc
	customCommandDone    chan struct{}
	// Continue preflight owns its trusted local checks independently of custom
	// commands. Its source, cancellation and generation are repository-scoped.
	continuePreflight  continuePreflightUIState
	continueTestRunner continueTestRunner
	// commandLogUser selects the user's commands instead of background Git.
	// Query, status, source and time range narrow that section independently.
	// The clock is replaceable only by deterministic real-geometry tests.
	commandLogUser   bool
	commandLogQuery  string
	commandLogStatus string
	commandLogSource string
	commandLogRange  string
	commandLogClock  func() time.Time
	// Pagination controls only how many already-recorded matches are rendered.
	// Expansion is user-scroll driven and never performs I/O in a frame.
	commandLogLimit     int
	commandLogList      ui.ListState
	commandLogExpanded  map[uint64]bool
	commandLogCollapsed map[uint64]bool
	// commandLogScroll belongs to the document, not one command entry.
	// commandLogReveal is a one-shot, identity-bound scroll request.
	commandLogScroll ui.ScrollState
	commandLogReveal *commandLogFailureTarget
	// commandPromptOpen shows the one-off shell command line that ':' opens;
	// commandPromptText holds what the user typed into it.
	commandPromptOpen bool
	commandPromptText string
	// lastAutoFetch throttles background fetches per app instance.
	lastAutoFetch time.Time
	// confirm is the question a destructive action asked, drawn over the
	// window until the user answers it.
	confirm *confirmRequest
	// pendingToasts are the notifications queued by actions, shown on the
	// next frame; toastSeq names each AddToast so two alike still stack.
	pendingToasts []appToast
	toastSeq      int
	// activityNotices is an explicit, bounded session projection for important
	// events with no canonical command/journal/agent/transfer record.
	activityNotices   []activityNotice
	activityNoticeSeq uint64
	// pendingForget is a repository whose entry the picker's context menu
	// removed, applied at the top of the next build pass: a menu must not
	// rewrite the list a pass is reading.
	pendingForget           string
	pendingForgetEditorOnly bool
	// pendingRename is the name a card was given, and the repository it
	// belongs to, applied at the top of the next build pass for the same
	// reason.
	pendingRename     bool
	pendingRenamePath string
	pendingRenameName string
	// showRenameRecent and its fields are the modal that names a card of the
	// picker, or a page's repository: the repository's own label, not its
	// folder. renameRecentFromTab tells the modal which of the two asked.
	showRenameRecent    bool
	renameRecentPath    string
	renameRecentName    string
	renameRecentFromTab bool
	// crash is the message of a fault caught while building a frame. While
	// it is set the window draws the safe screen instead of the app, so a
	// fault in one pane leaves a way out rather than closing the window.
	crash string

	selectedFile  int
	filePaneWidth float32
	// filePaneCollapsed hides the Changes list so its selected file can use
	// the full workspace. A narrow restore rail remains at the left edge.
	filePaneCollapsed bool
	// workspacePaneCollapsed independently remembers the primary list state
	// of every other workspace page; collapsing History must not also hide
	// Graph's list when the user switches pages.
	workspacePaneCollapsed [workspaceSectionCount]bool
	// The Changes list as built this frame: a section header, then the files
	// of that section. selectedFile indexes it, so it may point at a header
	// until the next frame moves it to the first file. mergeRows, stagedRows
	// and changesRows are the sections the lists below show; the sections
	// scroll on their own so each header stays fixed while its files move.
	changeRows  []changeRow
	mergeRows   []changeRow
	stagedRows  []changeRow
	changesRows []changeRow
	// mergeList, stagedList and fileList are the section lists; their
	// selected indices are local to the section, and filesView keeps
	// selectedFile, the index into changeRows every action reads, in step
	// with whichever list the user moved.
	mergeList       ui.ListState
	stagedList      ui.ListState
	fileList        ui.ListState
	mergeSelected   int
	stagedSelected  int
	changesSelected int
	// fileSearch filters the Changes list by file path, and ignoreUntracked
	// leaves untracked files out of it.
	fileSearch         string
	ignoreUntracked    bool
	changesFiltersOpen bool
	// startSectionChoice is the text of the Settings selector for the page a
	// repository opens on.
	startSectionChoice string
	fileStatusFilter   changesStatusFilter
	// review owns content-current review marks, filtering and asynchronous
	// publication identity as one feature state.
	review       reviewUIState
	fileTreeMode bool
	// fileTreeCollapsed scopes directory folds by pane, so the same path in
	// Changes and a commit can be expanded independently.
	fileTreeCollapsed map[string]bool
	mergeExpanded     bool
	stagedExpanded    bool
	changesExpanded   bool
	// changesTopHeight is the room the merge and staged lists take above the
	// changes, which the line under them drags. Until the user drags it, the
	// height follows the sections' files; after, it keeps where the user left
	// it as files come and go.
	changesTopHeight float32
	changesTopAuto   bool
	// mergeHeight is the room the Merge Changes section takes within that
	// top room, with a line under it the user drags. Until the user drags it,
	// it follows the conflicted files; after, it keeps where the user left
	// it as files come and go.
	mergeHeight  float32
	mergeAuto    bool
	navPaneWidth float32
	// navPaneCollapsed hides the workspace section list while keeping the
	// active section on screen and a narrow restore rail within reach.
	navPaneCollapsed bool
	workspaceTab     int
	cockpit          cockpitUIState
	// code owns the repository explorer, persistent editor documents and
	// workspace search independently of the diff-specific file editor.
	code codeState
	// navSelected is the sidebar row the active section shows, which differs
	// from workspaceTab once sections are hidden.
	navSelected    int
	navList        ui.ListState
	commitList     ui.ListState
	selectedCommit int
	// History's Rebase mode reads OperationState.Steps directly. A marked
	// base is session-local UI state and always stores Git's full commit ID.
	historyWorkspace     int
	markedBaseCommit     repository.Commit
	rebaseStepList       ui.ListState
	selectedRebaseStep   int
	rebaseTargetRef      string
	rebaseDraft          *repository.RebasePlan
	rebasePlan           *repository.RebasePlan
	rebasePlanText       string
	rebasePlanEditorOpen bool
	rebasePlanLoading    bool
	rebasePlanError      string
	rebasePlanToken      uint64
	commitSelectionMode  bool
	commitSelection      map[string]bool
	// commitsMore says the commit list has another page below it; loading
	// and error are that read's own state, and token drops one a newer
	// snapshot or page replaced.
	commitsMore    bool
	commitsLoading bool
	commitsError   string
	commitsToken   uint64
	// The History filter narrows the commit list with Git's own search: what
	// the list shows, the draft the modal edits, and the matches, which page
	// in by asking for a larger limit as the list reaches its end. The Graph
	// keeps its own, so narrowing one page leaves the other whole.
	historyFilter    commitQuery
	graphFilter      commitQuery
	graphLayoutCache graphLayoutCacheState
	// terminalDockHandle identifies the terminal dock's root control across
	// frames, so the app can tell whether the dock owns the keyboard.
	terminalDockHandle ui.Handle
	branchVisibleCache filteredCache
	reflogVisibleCache filteredCache
	remoteVisibleCache filteredCache
	filePathsMemo      map[filePathsMemoKey][]string
	fileTreeRowsMemo   map[fileTreeRowsMemoKeyType][]fileTreeRow
	// filterOpen is the commit filter modal, for History, the Graph, or a
	// branch's commit list; filterTarget is the section it was opened from.
	filterOpen   bool
	filterTarget int
	// authors are the repository's commit authors the filter offers, read
	// when the modal opens or is refreshed.
	authors           []string
	authorsLoading    bool
	authorsError      string
	authorsToken      uint64
	branchList        ui.ListState
	branchSearch      string
	selectedBranchRow int
	selectedBranch    int
	// sectionSearch gives the window-wide / shortcut a stable focus target
	// for the active panel. remoteBranchSearch is the second search in Remotes.
	sectionSearch      [workspaceSectionCount]ui.Handle
	remoteBranchSearch ui.Handle

	// Selecting a branch reads its commits without checking it out, then the
	// files and diff of the commit chosen from them.
	branchCommits            []repository.Commit
	branchCommitsBranch      string
	branchCommitsLoading     bool
	branchCommitsLoadingMore bool
	branchCommitsError       string
	branchCommitsMoreError   string
	branchCommitsMore        bool
	branchCommitsLimit       int
	branchCommitsToken       uint64
	branchFilter             repository.CommitFilter
	branchFilterDraft        repository.CommitFilter
	branchCommitList         ui.ListState
	selectedBranchCommit     int
	// branchCommitsKeep is the commit a quiet refresh keeps chosen when it
	// reads the branch's commits again, and branchCommitFilesKeepPath the
	// file it keeps chosen within that commit. They are empty otherwise.
	branchCommitsKeep           string
	branchCommitFilesKeepPath   string
	branchCommitDetailMessage   commitMessageDetailState
	branchCommitFiles           []repository.CommitFile
	branchCommitFileRows        []fileTreeRow
	branchCommitFileList        ui.ListState
	selectedBranchCommitFileRow int
	selectedBranchCommitFile    int
	// Branch details use the same two layouts as History, with independent
	// divider positions and collapse state for this separate commit selection.
	branchCommitFilesHeight    float32
	branchCommitFilesWidth     float32
	branchCommitFilesCollapsed bool
	branchCommitFilesLoading   bool
	branchCommitFilesError     string
	branchCommitFilesHash      string
	branchCommitDiffState      *editor.DiffState
	branchCommitDiffMedia      *mediaReview
	branchCommitDiffLoading    bool
	branchCommitDiffError      string
	branchCommitDiffHash       string
	branchCommitDiffPath       string
	branchCommitDiffPatch      string
	branchCommitToken          uint64
	branchCommitPanePercent    float32
	branchListWidth            float32
	branchCommitsWidth         float32
	branchCommitsCollapsed     bool
	stackSync                  stackSyncUIState
	performance                performanceUIState
	branchFullscreen           bool
	branchHeaderHeight         float32
	remoteList                 ui.ListState
	remoteFilter               string
	selectedRemoteRow          int
	selectedRemote             int
	remoteBranches             ui.ListState
	remoteBranchFilter         string
	selectedRemoteBranchRow    int
	selectedRemoteBranch       int
	stashList                  ui.ListState
	savedWorkPanel             int
	journalScroll              ui.ScrollState
	journalReveal              string
	journalNotice              string
	workSnapshotList           ui.ListState
	selectedWorkSnapshotRow    int
	fileRecoveryRows           ui.ListState
	stashFilter                string
	selectedStashRow           int
	selectedStash              int
	stashFiles                 []repository.CommitFile
	stashFileRows              []fileTreeRow
	stashFilesHash             string
	stashFilesHeight           float32
	stashFilesLoading          bool
	stashFilesError            string
	stashFileList              ui.ListState
	selectedStashFileRow       int
	selectedStashFile          int
	tagList                    ui.ListState
	tagFilter                  string
	selectedTagRow             int
	selectedTag                int
	worktreeList               ui.ListState
	worktreeFilter             string
	selectedWorktreeRow        int
	selectedWorktree           int
	submoduleList              ui.ListState
	submoduleFilter            string
	selectedSubmoduleRow       int
	selectedSubmodule          int
	reflogList                 ui.ListState
	reflogFilter               string
	selectedReflogRow          int
	selectedReflog             int
	// Reflog pages are appended from Git only after the list reaches its end.
	reflogMore    bool
	reflogLoading bool
	reflogError   string
	reflogToken   uint64

	diffState *editor.DiffState
	diffMedia *mediaReview
	diffKey   string
	diffKind  string
	// diffPatch is the diff the pane shows, kept so a selection of its lines
	// can be turned into the partial patch that stages or unstages them.
	diffPatch   string
	diffLoading bool
	diffError   string
	// compare is the independent, read-only ref-comparison dialog. Its
	// requests keep their own refs and options so changing the form cannot
	// change a read already in flight.
	compare        comparisonState
	diffGeneration uint64
	cancelDiff     context.CancelFunc
	// conflictState is the conflicted file the pane shows, and conflictView
	// the read-only editor that draws it. The conflict markers are resolved
	// in place, so the pane needs the file's text rather than a diff.
	conflictState *conflictState
	conflictView  *editor.State
	// conflictStages are immutable index-stage buffers. conflictStageTab is
	// 0 Base, 1 Current, 2 Incoming, 3 editable working-tree Result.
	conflictStages     repository.ConflictStages
	conflictStageViews [3]*editor.State
	conflictStageTab   int
	// conflictSides is the conflict Git wrote without markers, where one side
	// deleted the file and the pane shows the side it kept instead of a merge.
	conflictSides *conflictSides
	// conflictScrollTo is the line to bring into view on the next frame, or
	// -1 when the view should stay where the user left it.
	conflictScrollTo int
	// conflictSaveError is the last failed write of an edited conflict, and
	// conflictSaveVersion the buffer version it failed on, so the pane tries
	// again when the text changes rather than on every frame.
	conflictSaveError   string
	conflictSaveVersion uint64
	// conflictReview shows the two sides of the conflict as a diff instead
	// of the editable merge text, so the change between this branch and the
	// incoming one can be read side by side or unified and saved.
	conflictReview bool
	// Guided help visibility is transient view state only. Workflow progress
	// always comes from the live operation, index and conflict buffers.
	guidedOperationGuideHidden bool
	// conflictDiffState is that review, and conflictDiffPatch the combined
	// diff Git prints for the conflicted file, kept so the review can be
	// saved as the other diffs are.
	conflictDiffState *editor.DiffState
	conflictDiffPatch string
	// fileEditing swaps the selected file's diff for an editor of the file
	// itself. fileEditState is that editor's buffer, fileEditKey the file it
	// was read from, and the loading, binary, preview, error and save fields say what
	// the pane should draw around it. fileEditToken drops a read the choice
	// replaced.
	fileEditing          bool
	fileEditKey          string
	fileEditState        *editor.State
	fileEditLoading      bool
	fileEditBinary       bool
	fileEditPreview      *mediaPreview
	fileEditError        string
	fileEditSaveError    string
	fileEditTriedVersion uint64
	fileEditToken        uint64
	// diffFullscreen shows the active diff over the whole window.
	diffFullscreen bool
	// diffFullscreenDetailsHeight starts at the title card's compact
	// height and grows when its middle divider is dragged down. The details
	// scroll keeps metadata reachable when that chosen height is short.
	diffFullscreenDetailsHeight float32
	diffFullscreenDetailsScroll ui.ScrollState
	diffFullscreenMessageScroll ui.ScrollState
	diffFullscreenMessageHeight float32
	// The whole-window diff lists the files of the pane it reads, so another
	// file can be read without leaving it. diffFullscreenFile is the row
	// chosen, diffFullscreenRows the rows the frame built (the list's key and
	// label read them), and diffFullscreenFilesWidth the room the list takes,
	// which the line beside it drags. The list can collapse into a restore
	// rail while the selected file and its diff stay loaded.
	diffFullscreenFileList       ui.ListState
	diffFullscreenFile           int
	diffFullscreenRows           []fullscreenFile
	diffFullscreenFilesWidth     float32
	diffFullscreenFilesCollapsed bool
	// lineSelectMode adds independently clicked lines to lineSelectRanges;
	// adjacent ones coalesce, while gaps stay unselected. lineSelectRange and
	// lineSelectHas retain the viewer's latest range as the Shift-click anchor.
	lineSelectMode bool
	// hunkMode shows the diff's hunk actions (stage, discard, previous and
	// next hunk); the toolbar otherwise keeps to a single button for it.
	hunkMode         bool
	lineSelectRange  editor.DiffLineRange
	lineSelectHas    bool
	lineSelectRanges []editor.DiffLineRange
	actionBusy       bool
	actionError      string
	fileMutation     fileMutationState
	actionFailure    *commandLogFailureTarget
	// patchPreviewOpen owns the read-only patch import dialog. Its token
	// invalidates a native pick or preview that finishes after cancellation or
	// after another repository opens; the repository revalidates before apply.
	patchPreviewOpen    bool
	patchPreviewLoading bool
	patchPreview        *repository.PatchPreview
	patchPreviewError   string
	patchPreviewToken   uint64
	// customPatch is a session-local basket of canonical historical patches.
	// Its previews use the same immutable, repository-bound workflow as imports.
	customPatch customPatchState
	// courier copies frozen canonical patch bytes to another worktree. Its
	// preview and target session always belong to the chosen destination.
	courier changeCourierState
	// repositoryDetailsLoading is the deferred history/ancillary read of a
	// progressive open. refreshBusy is a quiet refresh reading the repository
	// in the background; there is one at a time, and refreshToken rejects an
	// obsolete read after shutdown. snapshotSeq names the snapshot the app
	// holds, so no older read can land after an action and restore it.
	repositoryDetailsLoading bool
	refreshBusy              bool
	refreshPending           bool
	refreshToken             uint64
	snapshotSeq              uint64
	cancelRepositoryWatch    context.CancelFunc

	showCommitForm          bool
	commitSummary           string
	commitBody              string
	commitAmend             bool
	showBranchForm          bool
	newBranchName           string
	newBranchBase           string
	showMoveCommitsForm     bool
	moveCommitsName         string
	showRenameForm          bool
	renameBranch            string
	showRemoteForm          bool
	remoteOriginalName      string
	remoteOriginalURL       string
	remoteName              string
	remoteURL               string
	showRemoteWebURLForm    bool
	remoteWebOriginalName   string
	remoteWebURL            string
	remoteWebURLError       string
	showRemoteCheckoutForm  bool
	remoteLocalBranch       string
	showStashForm           bool
	showPRForm              bool
	prTitle                 string
	prBody                  string
	prBase                  string
	prDraft                 bool
	prError                 string
	prChecksOpen            bool
	prChecksText            string
	prChecksError           string
	prChecksLoading         bool
	prChecksToken           uint64
	prSetupBusy             bool
	stashSelection          []repository.File
	stashPatch              string
	stashRemovalPatch       string
	stashMessage            string
	stashIncludeUntracked   bool
	showRecoveryModal       bool
	fileRecoveryFocus       string
	fileRecoveryRefreshSeq  uint64
	checkpointName          string
	showStashRenameForm     bool
	stashRename             string
	stashDiffState          *editor.DiffState
	stashDiffMedia          *mediaReview
	stashDiffHash           string
	stashDiffPath           string
	stashDiffOptions        repository.DiffOptions
	stashDiffPatch          string
	stashDiffLoading        bool
	stashDiffError          string
	showTagForm             bool
	showTagRemoteDeleteForm bool
	tagName                 string
	tagRef                  string
	tagMessage              string
	tagAnnotated            bool
	showTagPushForm         bool
	tagRemote               string
	showWorktreeForm        bool
	worktreePath            string
	worktreeBase            string
	worktreeBranch          string
	worktreeDetach          bool
	worktreeForce           bool
	worktreeTrack           bool
	worktreeGuessRemote     bool
	worktreeLock            bool
	worktreeLockReason      string
	worktreeOrphan          bool
	worktreeNoCheckout      bool
	worktreeSparsePaths     string
	worktreeTemporary       bool
	showWorktreeMoveForm    bool
	worktreeMovePath        string
	worktreeMoveForce       bool
	showWorktreeLockForm    bool
	worktreeLockTarget      string
	worktreeLockTargetName  string
	showWorktreeRepairForm  bool
	worktreeRepairPaths     string
	showWorktreePruneForm   bool
	worktreePruneExpire     string
	worktreePrunePreview    []string
	worktreePruneError      string
	worktreePruneLoading    bool
	worktreePruneToken      uint64
	showSubmoduleForm       bool
	submoduleName           string
	submodulePath           string
	submoduleURL            string

	// The commit actions that need a name: a tag or a branch created at a
	// commit picked from the history, opened as a small modal.
	showCommitTagForm    bool
	commitTagCommit      repository.Commit
	commitTagName        string
	commitTagAnnotated   bool
	commitTagMessage     string
	showCommitBranchForm bool
	commitBranchCommit   repository.Commit
	commitBranchName     string
	// Reword has its own modal because, unlike amending HEAD from Changes, it
	// edits only the message of the selected historical commit. The token drops
	// a slower message read after the form closes or switches to another commit.
	showCommitRewordForm   bool
	showBisectForm         bool
	bisectBadCommit        repository.Commit
	showExportPatchForm    bool
	exportPatchDir         string
	exportPatchCommits     []repository.Commit
	bisectGoodRef          string
	bisectFormError        string
	commitRewordCommit     repository.Commit
	commitRewordSummary    string
	commitRewordBody       string
	commitRewordLoading    bool
	commitRewordError      string
	commitRewordToken      uint64
	blameState             *editor.State
	blameLoading           bool
	blameError             string
	blameGeneration        uint64
	codeBlameHover         codeBlameHoverState
	codeBlameHistoryTarget *codeBlameHistoryTarget

	commitDetailMessage   commitMessageDetailState
	commitFiles           []repository.CommitFile
	commitFileRows        []fileTreeRow
	commitFileList        ui.ListState
	selectedCommitFileRow int
	selectedCommitFile    int
	// commitFilesHeight and commitFilesWidth are the room the commit's files
	// take above or beside its diff, which the line between them drags.
	// commitDetailsLayout chooses those two arrangements.
	commitFilesHeight    float32
	commitFilesWidth     float32
	commitFilesCollapsed bool
	commitDetailsLayout  int
	commitFilesHash      string
	commitFilesLoading   bool
	commitFilesError     string
	// File and diff reads have independent cancellation and request tokens.
	// Tokens, not only the selected hash/path, reject an A-B-A completion.
	cancelCommitFiles     context.CancelFunc
	commitFilesGeneration uint64
	commitDiffHash        string
	commitDiffPath        string
	commitDiffOptions     repository.DiffOptions
	cancelCommitDiff      context.CancelFunc
	commitDiffGeneration  uint64
	// commitDiffPatch is the diff the pane shows, kept so it can be saved.
	commitDiffPatch   string
	commitDiffLoading bool
	commitDiffError   string
	commitDiffState   *editor.DiffState
	commitDiffMedia   *mediaReview

	// diffSplit shows diffs side by side instead of unified, and diffWrap
	// wraps long diff lines instead of scrolling them sideways; both are
	// what the header buttons toggle. diffOptions controls the patches every
	// ordinary diff read asks Git for; Compare keeps independent options.
	diffSplit   bool
	diffWrap    bool
	diffOptions repository.DiffOptions

	// graphListPercent is the Graph list's share of the workspace width.
	// The splitter keeps whatever percentage the user drags it to.
	graphListPercent float32

	// fileHistory is the drawer in the commits pane: the commits that
	// changed fileHistoryPath, read when a file's menu asks for them, then
	// the files of the commit chosen from them and the diff of the file
	// chosen from those.
	fileHistoryOpen     bool
	fileHistoryPath     string
	fileHistoryCommits  []repository.Commit
	fileHistoryList     ui.ListState
	fileHistorySelected int
	fileHistoryLoading  bool
	fileHistoryError    string
	fileHistoryToken    uint64
	// more says the last page filled, so scrolling to the end reads
	// another; loadingMore and moreError are that read's own state, and
	// read counts the commits fetched from Git, which leads the commits
	// shown when a page repeated one the history had shifted under it.
	fileHistoryMore        bool
	fileHistoryLoadingMore bool
	fileHistoryMoreError   string
	fileHistoryRead        int

	fileHistoryFiles        []repository.CommitFile
	fileHistoryFileList     ui.ListState
	fileHistorySelectedFile int
	fileHistoryFilesLoading bool
	fileHistoryFilesError   string
	fileHistoryFilesHash    string
	fileHistoryFilesToken   uint64

	fileHistoryDiffState   *editor.DiffState
	fileHistoryDiffMedia   *mediaReview
	fileHistoryDiffLoading bool
	fileHistoryDiffError   string
	fileHistoryDiffHash    string
	fileHistoryDiffPath    string
	fileHistoryDiffPatch   string
	fileHistoryDiffToken   uint64

	// The drawer's width and its two inner dividers, all draggable and in
	// percentages of the room they share. The commit and file columns fold
	// independently into restore rails without disturbing the loaded diff.
	fileHistoryWidth            float32
	fileHistoryShown            float32
	fileHistoryCommitPercent    float32
	fileHistoryFilePercent      float32
	fileHistoryCommitsCollapsed bool
	fileHistoryFilesCollapsed   bool
}

func newApp(opener repositoryOpener) *app {
	a := &app{
		opener:                      opener,
		update:                      func(fn func()) { fn() },
		repositoryWork:              newRepositoryScope(),
		checkForUpdates:             func() {},
		updateInfo:                  func() appUpdateInfo { return appUpdateInfo{} },
		selectedFile:                -1,
		review:                      newReviewUIState(),
		fileTreeMode:                true,
		filePaneWidth:               340,
		navPaneWidth:                navPaneDefault,
		selectedCommit:              -1,
		selectedRebaseStep:          -1,
		selectedBranchRow:           -1,
		selectedBranch:              -1,
		selectedBranchCommit:        -1,
		selectedBranchCommitFileRow: -1,
		selectedRemoteRow:           -1,
		selectedRemote:              -1,
		selectedRemoteBranchRow:     -1,
		selectedRemoteBranch:        -1,
		selectedStashRow:            -1,
		selectedWorkSnapshotRow:     -1,
		selectedStash:               -1,
		selectedStashFileRow:        -1,
		selectedTagRow:              -1,
		selectedTag:                 -1,
		selectedWorktreeRow:         -1,
		selectedWorktree:            -1,
		selectedSubmoduleRow:        -1,
		selectedSubmodule:           -1,
		selectedReflogRow:           -1,
		selectedReflog:              -1,
		selectedCommitFileRow:       -1,
		selectedCommitFile:          -1,
		// The whole-window diff's file list starts wide enough for a path.
		diffFullscreenFile:       -1,
		diffFullscreenFilesWidth: 300,
		// Commit message and file/diff panes have independently adjustable heights.
		commitDetailMessage: commitMessageDetailState{height: commitMessageDefaultHeight},
		commitFilesHeight:   150,
		commitFilesWidth:    300,
		stashFilesHeight:    180,
		// Branch details keep their own message and file/diff pane sizes.
		branchCommitDetailMessage: commitMessageDetailState{height: commitMessageDefaultHeight},
		branchCommitFilesHeight:   110,
		branchCommitFilesWidth:    300,
		// Give commit metadata (subject/hash/author/time) enough horizontal room
		// to read without cramping CJK labels or burying names in the diff stats.
		// The splitter remains available for further manual adjustment.
		branchListWidth:    240,
		branchCommitsWidth: 360,
		branchHeaderHeight: 160,
		// The Graph list starts at about a third of the workspace, wider than
		// History's list so subjects and stats read well, and leaves the rest
		// to the commit details. Its percentage splitter stays draggable and
		// follows window resizing.
		graphListPercent: 36,
		// Both changes sections show their files until a click folds them.
		mergeExpanded:   true,
		stagedExpanded:  true,
		changesExpanded: true,
		// The line between the staged side and the changes starts under the
		// files, so neither side opens with room it does not fill.
		changesTopAuto: true,
		// The line under the merge changes starts under their files too.
		mergeAuto: true,
		// The settings modal starts at the stored values, defaults until the
		// settings file is read.
		settings:          defaultSettings(),
		sshSecretBackend:  mygo.Secrets,
		sshSessionSecrets: map[string]string{},
		// The file history drawer is wide enough for its three columns.
		fileHistoryWidth:         80,
		fileHistoryCommitPercent: 30,
		fileHistoryFilePercent:   30,
		diffOptions:              repository.DiffOptions{ContextLines: 3},
		compare: comparisonState{
			selected:  -1,
			context:   "3",
			listWidth: 280,
		},
	}
	if prepared, ok := opener.(interface{ forApp(*app) repositoryOpener }); ok {
		a.opener = prepared.forApp(a)
	}
	a.sshTestConnection = a.testSSHConnection
	a.code.paneWidth = 300
	a.code.markdownPreviewPercent = 50
	a.code.selectedRow, a.code.selectedSource = -1, -1
	a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	a.code.list.Selected = &a.code.selectedRow
	a.code.list.Key = func(row int) any { return fileTreeKey(a.code.rows, row) }
	a.code.list.Label = func(row int) string { return fileTreeLabel(a.code.rows, row) }
	a.code.searchList.Selected = &a.code.searchSelectedRow
	a.code.searchList.Key = func(row int) any {
		if row < 0 || row >= len(a.code.searchRows) {
			return row
		}
		item := a.code.searchRows[row]
		if item.isFile() {
			return "file\x00" + item.path
		}
		match := a.code.searchResults[item.match]
		return fmt.Sprintf("%s\x00%d\x00%d", match.Path, match.Line, match.Column)
	}
	a.code.searchList.Label = func(row int) string {
		if row < 0 || row >= len(a.code.searchRows) {
			return ""
		}
		item := a.code.searchRows[row]
		if item.isFile() {
			return item.path
		}
		match := a.code.searchResults[item.match]
		return fmt.Sprintf("%s:%d: %s", match.Path, match.Line, match.Text)
	}
	a.compare.list.Selected = &a.compare.selectedRow
	a.compare.list.Key = func(row int) any {
		return fileTreeKey(a.compare.fileRows, row)
	}
	a.compare.list.Label = func(row int) string {
		return fileTreeLabel(a.compare.fileRows, row)
	}
	a.navList.Selected = &a.navSelected
	a.navList.Key = func(row int) any {
		if section, ok := a.navRow(row); ok {
			return workspaceSectionIDs[section]
		}
		return row
	}
	a.navList.Label = func(row int) string {
		if section, ok := a.navRow(row); ok {
			return tr(workspaceSections[section])
		}
		return ""
	}
	a.fileList.Selected = &a.changesSelected
	a.stagedList.Selected = &a.stagedSelected
	a.mergeList.Selected = &a.mergeSelected
	a.fileList.Key = func(row int) any { return changeKey(a.changesRows, row) }
	a.stagedList.Key = func(row int) any { return changeKey(a.stagedRows, row) }
	a.mergeList.Key = func(row int) any { return changeKey(a.mergeRows, row) }
	a.fileList.Label = func(row int) string { return changeLabel(a.changesRows, row) }
	a.stagedList.Label = func(row int) string { return changeLabel(a.stagedRows, row) }
	a.mergeList.Label = func(row int) string { return changeLabel(a.mergeRows, row) }
	a.commitList.Selected = &a.selectedCommit
	a.rebaseStepList.Selected = &a.selectedRebaseStep
	a.rebaseStepList.Key = func(row int) any {
		steps := a.visibleRebaseSteps()
		if row < 0 || row >= len(steps) {
			return row
		}
		step := steps[row]
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d", step.Action, step.Hash, step.UpdateRef, row)
	}
	a.rebaseStepList.Label = func(row int) string {
		steps := a.visibleRebaseSteps()
		if row < 0 || row >= len(steps) {
			return ""
		}
		return rebaseStepLabel(steps[row])
	}
	a.commitList.EndReached = func(int) { a.requestMoreCommits() }
	a.commitList.Key = func(row int) any {
		commits := a.visibleCommits()
		if row < 0 || row >= len(commits) {
			return row
		}
		return commits[row].Hash
	}
	a.commitList.Label = func(row int) string {
		commits := a.visibleCommits()
		if row < 0 || row >= len(commits) {
			return ""
		}
		return commits[row].Subject
	}
	a.branchList.Selected = &a.selectedBranchRow
	a.branchList.Key = func(row int) any {
		branch, ok := a.branchAt(row)
		if !ok {
			return row
		}
		return branch.Name
	}
	a.branchList.Label = func(row int) string {
		branch, ok := a.branchAt(row)
		if !ok {
			return ""
		}
		return branchDisplay(branch)
	}
	a.branchCommitList.Selected = &a.selectedBranchCommit
	a.branchCommitList.EndReached = func(int) {
		if a.selectedBranch >= 0 && a.selectedBranch < len(a.snapshot.Branches) {
			a.requestMoreBranchCommits(a.snapshot.Branches[a.selectedBranch].Name)
		}
	}
	a.branchCommitList.Key = func(row int) any { return a.branchCommits[row].Hash }
	a.branchCommitList.Label = func(row int) string { return a.branchCommits[row].Subject }
	a.branchCommitFileList.Selected = &a.selectedBranchCommitFileRow
	a.branchCommitFileList.Key = func(row int) any { return fileTreeKey(a.branchCommitFileRows, row) }
	a.branchCommitFileList.Label = func(row int) string { return fileTreeLabel(a.branchCommitFileRows, row) }
	a.remoteList.Selected = &a.selectedRemoteRow
	a.remoteList.Key = func(row int) any {
		remote, ok := a.remoteAt(row)
		if !ok {
			return row
		}
		return remote.Name
	}
	a.remoteList.Label = func(row int) string {
		remote, ok := a.remoteAt(row)
		if !ok {
			return ""
		}
		return remote.Name
	}
	a.remoteBranches.Selected = &a.selectedRemoteBranchRow
	a.remoteBranches.Key = func(row int) any {
		if a.selectedRemote < 0 || a.selectedRemote >= len(a.snapshot.Remotes) {
			return ""
		}
		branch, ok := a.remoteBranchAt(row)
		if !ok {
			return row
		}
		return struct {
			Remote string
			Branch string
		}{a.snapshot.Remotes[a.selectedRemote].Name, branch.Name}
	}
	a.remoteBranches.Label = func(row int) string {
		branch, ok := a.remoteBranchAt(row)
		if !ok {
			return ""
		}
		return branch.Name
	}
	a.workSnapshotList.Selected = &a.selectedWorkSnapshotRow
	a.workSnapshotList.Key = func(row int) any {
		if row >= 0 && row < len(a.snapshot.WorkSnapshots) {
			return a.snapshot.WorkSnapshots[row].ID
		}
		return row
	}
	a.workSnapshotList.Label = func(row int) string {
		if row >= 0 && row < len(a.snapshot.WorkSnapshots) {
			return a.snapshot.WorkSnapshots[row].Name
		}
		return ""
	}
	a.stashList.Selected = &a.selectedStashRow
	a.stashList.Key = func(row int) any {
		stash, ok := a.stashAt(row)
		if !ok {
			return row
		}
		return stash.Hash
	}
	a.stashList.Label = func(row int) string {
		stash, ok := a.stashAt(row)
		if !ok {
			return ""
		}
		return stash.Name
	}
	a.stashFileList.Selected = &a.selectedStashFileRow
	a.stashFileList.Key = func(row int) any { return fileTreeKey(a.stashFileRows, row) }
	a.stashFileList.Label = func(row int) string { return fileTreeLabel(a.stashFileRows, row) }
	a.tagList.Selected = &a.selectedTagRow
	a.tagList.Key = func(row int) any {
		tag, ok := a.tagAt(row)
		if !ok {
			return row
		}
		return tag.Name
	}
	a.tagList.Label = func(row int) string {
		tag, ok := a.tagAt(row)
		if !ok {
			return ""
		}
		return tag.Name
	}
	a.worktreeList.Selected = &a.selectedWorktreeRow
	a.worktreeList.Key = func(row int) any {
		worktree, ok := sourceRow(a.visibleWorktrees(), a.snapshot.Worktrees, row)
		if !ok {
			return row
		}
		return worktree.Path
	}
	a.worktreeList.Label = func(row int) string {
		worktree, ok := sourceRow(a.visibleWorktrees(), a.snapshot.Worktrees, row)
		if !ok {
			return ""
		}
		return worktree.Name
	}
	a.submoduleList.Selected = &a.selectedSubmoduleRow
	a.submoduleList.Key = func(row int) any {
		submodule, ok := sourceRow(a.visibleSubmodules(), a.snapshot.Submodules, row)
		if !ok {
			return row
		}
		return submodule.Path
	}
	a.submoduleList.Label = func(row int) string {
		submodule, ok := sourceRow(a.visibleSubmodules(), a.snapshot.Submodules, row)
		if !ok {
			return ""
		}
		return submodule.Name
	}
	a.reflogList.Selected = &a.selectedReflogRow
	a.reflogList.EndReached = func(int) { a.requestMoreReflog() }
	a.reflogList.Key = func(row int) any {
		rows := a.visibleReflog()
		_, ok := sourceRow(rows, a.snapshot.Reflog, row)
		if !ok {
			return row
		}
		// Source rows move whenever a new reflog record is prepended.
		// Identity therefore uses content plus its duplicate occurrence.
		return reflogRowIdentity(a.snapshot.Reflog, rows[row])
	}
	a.reflogList.Label = func(row int) string {
		entry, ok := sourceRow(a.visibleReflog(), a.snapshot.Reflog, row)
		if !ok {
			return ""
		}
		return entry.Subject
	}
	a.commitFileList.Selected = &a.selectedCommitFileRow
	a.commitFileList.Key = func(row int) any { return fileTreeKey(a.commitFileRows, row) }
	a.commitFileList.Label = func(row int) string { return fileTreeLabel(a.commitFileRows, row) }
	a.diffFullscreenFileList.Selected = &a.diffFullscreenFile
	a.diffFullscreenFileList.Key = func(row int) any {
		if row < 0 || row >= len(a.diffFullscreenRows) {
			return ""
		}
		return a.diffFullscreenRows[row].id
	}
	a.diffFullscreenFileList.Label = func(row int) string {
		if row < 0 || row >= len(a.diffFullscreenRows) {
			return ""
		}
		return a.diffFullscreenRows[row].label
	}
	a.fileHistoryList.Selected = &a.fileHistorySelected
	a.fileHistoryList.Key = func(row int) any { return a.fileHistoryCommits[row].Hash }
	a.fileHistoryList.Label = func(row int) string { return a.fileHistoryCommits[row].Subject }
	a.fileHistoryFileList.Selected = &a.fileHistorySelectedFile
	a.fileHistoryFileList.Key = func(row int) any {
		file := a.fileHistoryFiles[row]
		return file.Path + "\x00" + file.Status
	}
	a.fileHistoryFileList.Label = func(row int) string { return a.fileHistoryFiles[row].Path }
	a.updateChanged = func(fn func() bool) {
		a.update(func() { fn() })
	}
	return a
}

// launchPath is the repository a launch from the terminal opens: the first
// argument that is not a flag, since a bundle on macOS passes -psn_… and
// other platforms pass their own switches.
// argsWithTerminalDefault adds "." to a command line that names no
// repository when it was typed in a terminal, so `lazygitGo` alone opens the
// current directory. Flags alone do not name one, and neither does any other
// launch.
func argsWithTerminalDefault(args []string, fromTerminal bool) []string {
	if !fromTerminal || len(args) == 0 {
		return args
	}
	for _, arg := range args[1:] {
		if arg != "" && !strings.HasPrefix(arg, "-") {
			return args
		}
	}
	return append(slices.Clone(args), ".")
}

func launchPath(args []string) string {
	workingDir, _ := os.Getwd()
	return launchPathFrom(workingDir, args)
}

func launchPathFrom(workingDir string, args []string) string {
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		// SSH references are already absolute workspace identities. Resolving
		// them as native paths would turn ssh:// into a local ssh:/ path.
		if remote, ok := canonicalRemoteReference(arg); ok {
			return remote
		}
		// Resolve a terminal path while the caller's working directory is
		// unambiguous. The initial tab, errors, and recent list then all use
		// the same repository name, including when the argument contains spaces.
		// A second instance supplies its working directory explicitly because
		// the existing app's working directory is unrelated to the caller's.
		if workingDir != "" && !filepath.IsAbs(arg) {
			return filepath.Join(workingDir, arg)
		}
		absolute, err := filepath.Abs(arg)
		if err != nil {
			return arg
		}
		return absolute
	}
	return ""
}

// runAgentCommand handles only the explicit agent subcommand. Dispatching it
// before daemon, launcher and single-instance setup keeps automation headless
// and prevents a second lazygitGo window or a forwarded repository-open request.
func runAgentCommand(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 || args[0] != "agent" {
		return 0, false
	}
	return agentcli.Execute(ctx, args[1:], stdin, stdout, stderr), true
}

// Main runs the lazygitGo desktop application or its explicit headless CLI.
func Main() {
	if code, handled := runAgentCommand(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}

	// Lazygit's history-editing commands invoke this executable as Git's editor.
	// Handle their short-lived todo-file instructions before any GUI or updater
	// initialization, or Git would open another lazygitGo window and hang the rebase.
	if daemon.InDaemonMode() {
		daemon.Handle(logrus.New().WithField("component", "git-editor"))
		return
	}

	// Typing the bare command in a terminal means "here", as `lazygitGo .`
	// does. A desktop shortcut or the Dock has no terminal on stdin and keeps
	// opening the picker. This runs before the launcher detaches, which
	// removes the terminal the check needs.
	os.Args = argsWithTerminalDefault(os.Args, term.IsTerminal(int(os.Stdin.Fd())))

	launched, err := launch.LaunchInBackground()
	if err != nil {
		log.Printf("Could not detach the lazygitGo launcher: %v", err)
	}
	if launched {
		return
	}
	// MyGo includes the app name in its single-instance key. Use the same
	// key as TanGit while an older installation may still be running.
	mygo.App.SetName(agentcli.LegacyDataDirectoryName)
	// A command typed while lazygitGo is running forwards its path to that app's
	// tabbed window instead of creating a second process and window.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	configureAppIdentity()
	setupUpdater()
	// The window reads the history and settings once it exists: the data
	// directory is resolved from the application, so a first frame already
	// has the chosen theme and the recent repositories.
	opener := newSSHWorkspaceOpener(repository.Opener{})
	path := launchPath(os.Args[1:])
	mygo.App.WhenReady(func() {
		openAppWindow(opener, path)
		setupTray()
	})
	deliverAgentDesktopNotification = showAgentDesktopNotification
	runErr := mygo.App.Run()
	// OnClosed has already cancelled every page's queue. Let their SFTP
	// cleanup finish before the opener closes the clients those queues use.
	waitForRemoteTransferClose()
	if err := opener.Close(); err != nil {
		runErr = errors.Join(runErr, err)
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}

func configureAppIdentity() {
	// MyGo derives these directories from the app name. Pin the existing
	// locations before renaming it so settings, updater state, and local
	// recovery data stay accessible across upgrades.
	if configDir, err := os.UserConfigDir(); err == nil && agentcli.DefaultDataDirectoryName(configDir) == agentcli.LegacyDataDirectoryName {
		for _, name := range []mygo.PathName{mygo.PathUserData, mygo.PathCache, mygo.PathLogs} {
			if dir, err := mygo.App.Path(name); err == nil {
				mygo.App.SetPath(name, dir)
			} else {
				log.Printf("Could not open the existing %s directory: %v", name, err)
			}
		}
	}
	mygo.App.SetName(appName)
}

// openAppWindow gives the app its window: one title bar drawn by the app, a
// tab per open repository, and a picker tab to open more. A path opens that
// repository straight away, as a launch from the terminal asks for.
func openAppWindow(opener repositoryOpener, path string) {
	s := newShell(opener)
	win := mygo.NewWindow(appWindowOptions(ui.View(s.view)))
	s.window = win
	s.update = win.Update
	// Unlike Update, RunOnMain does not itself request a frame. Each page's
	// publication path can therefore skip paint for hidden or unchanged state.
	s.postMain = mygo.RunOnMain
	s.invalidate = win.Invalidate
	watchUpdater(s)
	mainWindow = win
	appShell = s
	mygo.App.OnSecondInstance(s.openSecondInstance)
	resumeQuit := mygo.App.Quit
	stopQuitWatch := mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		resumeQuit = e.Retry
	})
	win.OnClosed(stopQuitWatch)
	allowClose := false
	win.OnClose(func(e *mygo.CloseEvent) {
		if hideOnCloseEnabled() {
			hideOnClose(e)
			return
		}
		if allowClose {
			allowClose = false
			return
		}
		e.PreventDefault()
		wasQuitting := quitting
		finishClose := win.Close
		if wasQuitting {
			// Capture this request before confirmation yields to another event.
			// A plain Quit here would lose Relaunch after PreventDefault cancels it.
			finishClose = resumeQuit
		}
		quitting = false
		closing := append([]*tab(nil), s.tabs...)
		dirty := false
		for _, page := range closing {
			dirty = dirty || len(page.app.dirtyCodePaths()) > 0
		}
		if dirty {
			win.Show()
			win.Focus()
		}
		s.confirmCodeTabs(closing, func() {
			// OnClosed is too late to flush remote Changes edits: the window and
			// its event loop may already be gone. Prepare first, then close for real.
			s.prepareTabsShutdown(closing, func() {
				allowClose = true
				finishClose()
			})
		})
	})
	win.OnClosed(s.shutdown)
	// A window shown, focused or restored reads its repositories again, so
	// what another app changed while lazygitGo was hidden appears without an
	// F5; losing focus or hiding stands the timer aside.
	win.OnShow(s.handleWindowShown)
	win.OnFocus(s.handleWindowFocused)
	win.OnBlur(s.handleWindowBlurred)
	win.OnHide(s.handleWindowHidden)
	win.OnRestore(s.handleWindowShown)
	// The settings and history are read once the window exists, because the
	// data directory is resolved from it.
	s.loadPreferences()
	avatarDirectory := ""
	if s.pathCache != "" {
		avatarDirectory = filepath.Join(s.pathCache, "avatars")
	}
	s.authorAvatars = newAuthorAvatarStore(avatarDirectory)
	win.OnClosed(s.authorAvatars.cancel)
	installNativeHelpShortcut(s)
	s.start(path)
	s.startAutoRefresh()
}

// appWindowOptions is the shape of lazygitGo's only window: a hidden title bar
// under the app's own bar, and the platform's window controls or the app's.
func appWindowOptions(content *ui.Content) mygo.WindowOptions {
	opts := mygo.WindowOptions{
		Title:    appName,
		Width:    1040,
		Height:   700,
		MinWidth: 720,
		// The title bar takes 46 of the window's height, so the page keeps
		// about the room a 440-tall window gave it before.
		MinHeight:     480,
		StateKey:      "main",
		TitleBarStyle: mygo.TitleBarHidden,
		// On macOS the system keeps its traffic lights, and the bar makes
		// room for them on the left. Elsewhere the bar draws the controls
		// itself, on the right, so the system's are hidden.
		HideWindowButtons: !systemWindowControls,
		// The native frame keeps its platform resize behavior. Transparency
		// lets the root surface below draw the same rounded corners on Linux,
		// where MyGo has no native CornerRadius for framed windows.
		Transparent: true,
		Content:     content,
	}
	if systemWindowControls {
		// AppKit puts the lights in the top corner; give them the bar's
		// own centre instead, where the app's other chrome sits.
		opts.TrafficLightPosition = &macOSTrafficLightInset
	}
	return opts
}

// macOSWindowButtonHeight is the height of a standard traffic light in the
// window's title bar.
const macOSWindowButtonHeight float32 = 14

// macOSTrafficLightInset places the close button's top corner so that the
// button's centre lands on the centre of the bar.
var macOSTrafficLightInset = mygo.Point{
	X: 20,
	Y: int(titleBarHeight/2 - macOSWindowButtonHeight/2),
}
