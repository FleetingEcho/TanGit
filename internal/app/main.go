package app

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/jesseduffield/lazygit/pkg/app/daemon"
	"github.com/sirupsen/logrus"

	"lazygitapp/internal/repository"
)

type openState uint8

// appName is the product name window titles and the picker show.
const appName = "TanGit"

const (
	stateEmpty openState = iota
	stateLoading
	stateReady
	stateError
)

type repositoryOpener interface {
	Open(context.Context, string) (repository.Session, error)
	// Init makes a folder a Git repository, so the picker can adopt a
	// folder that has none yet.
	Init(context.Context, string) error
}

type app struct {
	// Keyboard state belongs to the UI thread; only stable handles survive builds.
	keyboardDiffCanSelect bool
	keyboardTextFocus     bool
	keyboardModalWasOpen  bool
	keyboardModalReturn   string
	keyboardLastPane      string
	keyboardCurrentGroup  string
	keybindingsScope      string
	keyboardPrevious      []keyboardPane
	keyboardLastList      *ui.ListState
	keyboardLineAnchor    int
	keyboardCopied        []repository.Commit
	keyboardPanes         []keyboardPane
	keyboardHandles       map[string]*ui.Handle
	keyboardFocus         string
	keyboardSection       int
	keyboardSectionKnown  bool
	keyboardMoved         map[*ui.ListState]bool

	opener repositoryOpener
	update func(func())
	window *mygo.Window

	path       string
	state      openState
	snapshot   repository.Snapshot
	session    repository.Session
	errMessage string
	generation uint64
	cancelOpen context.CancelFunc
	// offerInitPath is the folder the picker offers to initialize, because
	// it holds no Git repository yet.
	offerInitPath string
	// The tab hooks connect the app to the window around it, and are nil
	// when it runs on its own (tests), which hides the tab controls.
	//
	// openTab opens a repository in a page of its own, openHomeTab shows the
	// picker in one, claimOpen reports that an open was taken over by an
	// existing page or the window's cap, and closeTab gives this app's page
	// up.
	openTab     func(path string)
	openHomeTab func()
	claimOpen   func(path string) bool
	closeTab    func()
	// onRecentRenamed carries a name given to one page's repository to the
	// window's other pages, so a pill, a card, and the next opening agree.
	onRecentRenamed func(path, name string)
	// externalRefresh lets the shell limit filesystem-triggered reads to the
	// page the user can see. Nil means a standalone app refreshes directly.
	externalRefresh func()

	// term is the shell pane the header button toggles.
	term terminalState

	// pathUserData caches the per-user directory the history is stored in,
	// "" until a window exists to resolve it from.
	pathUserData       string
	recentRepositories []recentRepository
	// historyLoaded records that the data directory was resolved and its
	// history read, so a frame never re-reads the file.
	historyLoaded bool
	// recentFilter narrows the cards by their display name or path.
	// visibleRecent holds original history indices for the filtered grid, so
	// opening, renaming, and removing a match always affect the right entry.
	recentFilter   string
	visibleRecent  []int
	recentGrid     ui.GridState
	selectedRecent int

	// pathCopiedAt is when the status bar's path was put on the clipboard,
	// zero while the bar shows the path itself. It shows "Copied" in its
	// place for two seconds, then the path again.
	pathCopiedAt time.Time

	// settings is the persisted preferences the settings modal edits.
	settings       appSettings
	settingsLoaded bool
	settingsOpen   bool
	settingsPage   int
	// homeBackground is decoded once for the path in settings, then reused
	// by Home and its Settings preview. The pending/failed paths prevent a
	// rebuild from starting the same read again; the two generations reject
	// results superseded by another selection or by removal.
	homeBackground             *ui.Bitmap
	homeBackgroundPath         string
	homeBackgroundPending      string
	homeBackgroundFailed       string
	homeBackgroundGeneration   uint64
	homeBackgroundSelectionSeq uint64
	// The shell supplies the process-wide updater to each tab. Tests and
	// standalone app views keep harmless defaults.
	checkForUpdates func()
	updateInfo      func() appUpdateInfo
	// sectionsOpen is the popup of the settings' section picker, which
	// stays open while several sections are chosen.
	sectionsOpen bool
	// keybindingsOpen shows the shortcut reference, with the switch that
	// turns every shortcut off; keybindingFilter narrows its list.
	keybindingsOpen        bool
	keybindingsCurrentOnly bool
	keybindingFilter       string
	// commandLogOpen shows every Git command the session ran and what it
	// printed; commandLog is that record, nil until a repository opens.
	commandLogOpen bool
	commandLog     *repository.CommandLog
	// confirm is the question a destructive action asked, drawn over the
	// window until the user answers it.
	confirm *confirmRequest
	// pendingToasts are the notifications queued by actions, shown on the
	// next frame; toastSeq names each AddToast so two alike still stack.
	pendingToasts []appToast
	toastSeq      int
	// pendingForget is a repository whose entry the picker's context menu
	// removed, applied at the top of the next build pass: a menu must not
	// rewrite the list a pass is reading.
	pendingForget string
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
	workspacePaneCollapsed [sectionReflog + 1]bool
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
	fileSearch      string
	ignoreUntracked bool
	fileTreeMode    bool
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
	// navSelected is the sidebar row the active section shows, which differs
	// from workspaceTab once sections are hidden.
	navSelected     int
	navList         ui.ListState
	commitList      ui.ListState
	selectedCommit  int
	commitSelection map[string]bool
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
	historyFilter commitQuery
	graphFilter   commitQuery
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
	sectionSearch      [sectionReflog + 1]ui.Handle
	remoteBranchSearch ui.Handle

	// Selecting a branch reads its commits without checking it out, then the
	// files and diff of the commit chosen from them.
	branchCommits        []repository.Commit
	branchCommitsBranch  string
	branchCommitsLoading bool
	branchCommitsError   string
	branchCommitsToken   uint64
	branchFilter         repository.CommitFilter
	branchFilterDraft    repository.CommitFilter
	branchCommitList     ui.ListState
	selectedBranchCommit int
	// branchCommitsKeep is the commit a quiet refresh keeps chosen when it
	// reads the branch's commits again, and branchCommitFilesKeepPath the
	// file it keeps chosen within that commit. They are empty otherwise.
	branchCommitsKeep           string
	branchCommitFilesKeepPath   string
	branchCommitFiles           []repository.CommitFile
	branchCommitFileRows        []fileTreeRow
	branchCommitFileList        ui.ListState
	selectedBranchCommitFileRow int
	selectedBranchCommitFile    int
	// branchCommitFilesHeight is the room the branch commit's files take
	// above its diff, which the line between them drags.
	branchCommitFilesHeight  float32
	branchCommitFilesLoading bool
	branchCommitFilesError   string
	branchCommitFilesHash    string
	branchCommitDiffState    *editor.DiffState
	branchCommitDiffLoading  bool
	branchCommitDiffError    string
	branchCommitDiffHash     string
	branchCommitDiffPath     string
	branchCommitDiffPatch    string
	branchCommitToken        uint64
	branchCommitPanePercent  float32
	branchListWidth          float32
	branchCommitsWidth       float32
	branchCommitsCollapsed   bool
	remoteList               ui.ListState
	remoteFilter             string
	selectedRemoteRow        int
	selectedRemote           int
	remoteBranches           ui.ListState
	remoteBranchFilter       string
	selectedRemoteBranchRow  int
	selectedRemoteBranch     int
	stashList                ui.ListState
	stashFilter              string
	selectedStashRow         int
	selectedStash            int
	stashFiles               []repository.CommitFile
	stashFileRows            []fileTreeRow
	stashFilesHash           string
	stashFilesHeight         float32
	stashFilesLoading        bool
	stashFilesError          string
	stashFileList            ui.ListState
	selectedStashFileRow     int
	selectedStashFile        int
	tagList                  ui.ListState
	tagFilter                string
	selectedTagRow           int
	selectedTag              int
	worktreeList             ui.ListState
	worktreeFilter           string
	selectedWorktreeRow      int
	selectedWorktree         int
	submoduleList            ui.ListState
	submoduleFilter          string
	selectedSubmoduleRow     int
	selectedSubmodule        int
	reflogList               ui.ListState
	reflogFilter             string
	selectedReflogRow        int
	selectedReflog           int

	diffState *editor.DiffState
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
	// conflictDiffState is that review, and conflictDiffPatch the combined
	// diff Git prints for the conflicted file, kept so the review can be
	// saved as the other diffs are.
	conflictDiffState *editor.DiffState
	conflictDiffPatch string
	// fileEditing swaps the selected file's diff for an editor of the file
	// itself. fileEditState is that editor's buffer, fileEditKey the file it
	// was read from, and the loading, binary, error and save fields say what
	// the pane should draw around it. fileEditToken drops a read the choice
	// replaced.
	fileEditing          bool
	fileEditKey          string
	fileEditState        *editor.State
	fileEditLoading      bool
	fileEditBinary       bool
	fileEditError        string
	fileEditSaveError    string
	fileEditTriedVersion uint64
	fileEditToken        uint64
	// diffFullscreen shows the active diff over the whole window.
	diffFullscreen bool
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
	// lineSelectMode is the mode in which clicking line numbers adds them to
	// the selection instead of starting it over, so a range is picked without
	// holding Shift. lineSelectRange and lineSelectHas are the selection the
	// pane showed before the click being handled, which is what a click
	// extends.
	lineSelectMode  bool
	lineSelectRange editor.DiffLineRange
	lineSelectHas   bool
	actionBusy      bool
	actionError     string
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

	showCommitForm         bool
	commitSummary          string
	commitBody             string
	commitAmend            bool
	showBranchForm         bool
	newBranchName          string
	newBranchBase          string
	showRenameForm         bool
	renameBranch           string
	showRemoteForm         bool
	remoteOriginalName     string
	remoteName             string
	remoteURL              string
	showRemoteCheckoutForm bool
	remoteLocalBranch      string
	showStashForm          bool
	stashMessage           string
	stashIncludeUntracked  bool
	showStashRenameForm    bool
	stashRename            string
	stashDiffState         *editor.DiffState
	stashDiffHash          string
	stashDiffPath          string
	stashDiffOptions       repository.DiffOptions
	stashDiffPatch         string
	stashDiffLoading       bool
	stashDiffError         string
	showTagForm            bool
	tagName                string
	tagRef                 string
	tagMessage             string
	tagAnnotated           bool
	showTagPushForm        bool
	tagRemote              string
	showWorktreeForm       bool
	worktreePath           string
	worktreeBase           string
	worktreeBranch         string
	worktreeDetach         bool
	showSubmoduleForm      bool
	submoduleName          string
	submodulePath          string
	submoduleURL           string

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
	showCommitRewordForm bool
	commitRewordCommit   repository.Commit
	commitRewordSummary  string
	commitRewordBody     string
	commitRewordLoading  bool
	commitRewordError    string
	commitRewordToken    uint64
	blameState           *editor.State
	blameLoading         bool
	blameError           string
	blameGeneration      uint64

	commitFiles           []repository.CommitFile
	commitFileRows        []fileTreeRow
	commitFileList        ui.ListState
	selectedCommitFileRow int
	selectedCommitFile    int
	// commitFilesHeight is the room the commit's files take above its diff,
	// which the line between them drags.
	commitFilesHeight  float32
	commitFilesHash    string
	commitFilesLoading bool
	commitFilesError   string
	commitDiffHash     string
	commitDiffPath     string
	commitDiffOptions  repository.DiffOptions
	// commitDiffPatch is the diff the pane shows, kept so it can be saved.
	commitDiffPatch   string
	commitDiffLoading bool
	commitDiffError   string
	commitDiffState   *editor.DiffState

	// diffSplit shows diffs side by side instead of unified, and diffWrap
	// wraps long diff lines instead of scrolling them sideways; both are
	// what the header buttons toggle. diffOptions controls the patches every
	// ordinary diff read asks Git for; Compare keeps independent options.
	diffSplit   bool
	diffWrap    bool
	diffOptions repository.DiffOptions

	// graphListPercent is the Graph section's share of the workspace width,
	// wider than the other sections because the lanes need the room.
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
		checkForUpdates:             func() {},
		updateInfo:                  func() appUpdateInfo { return appUpdateInfo{} },
		selectedFile:                -1,
		filePaneWidth:               340,
		navPaneWidth:                navPaneDefault,
		selectedCommit:              -1,
		selectedBranchRow:           -1,
		selectedBranch:              -1,
		selectedBranchCommit:        -1,
		selectedBranchCommitFileRow: -1,
		selectedRemoteRow:           -1,
		selectedRemote:              -1,
		selectedRemoteBranchRow:     -1,
		selectedRemoteBranch:        -1,
		selectedStashRow:            -1,
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
		// The commit's files start at the height they had before the line
		// between them and the diff could be dragged.
		commitFilesHeight: 150,
		stashFilesHeight:  180,
		// The branches column is narrower, so its two panes start smaller.
		branchCommitFilesHeight: 110,
		// The branches column shows branch names, the commits column the
		// subjects, so both stay narrow and leave the commit its room.
		branchListWidth:    240,
		branchCommitsWidth: 280,
		// The Graph lays its lanes out from left to right, so the list pane
		// takes the larger share of the workspace.
		graphListPercent: 58,
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
		settings: defaultSettings(),
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
	a.term.percent = terminalPanelPercent
	a.compare.list.Selected = &a.compare.selected
	a.compare.list.Key = func(row int) any {
		if row < 0 || row >= len(a.compare.files) {
			return row
		}
		return a.compare.files[row].Path
	}
	a.compare.list.Label = func(row int) string {
		if row < 0 || row >= len(a.compare.files) {
			return ""
		}
		return a.compare.files[row].Path
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
	// The cards draw their own frame: the grid only places them, since a
	// chosen cell paints a frame of the theme's radius around content the
	// card insets, leaving a lip at the corners where the two disagree.
	a.recentGrid.Key = func(item int) any {
		if item >= 0 && item < len(a.visibleRecent) {
			return a.recentRepositories[a.visibleRecent[item]].Path
		}
		return item
	}
	a.recentGrid.Label = func(item int) string {
		if item >= 0 && item < len(a.visibleRecent) {
			return a.recentRepositories[a.visibleRecent[item]].Path
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
		return branch.Name
	}
	a.remoteBranches.Label = func(row int) string {
		branch, ok := a.remoteBranchAt(row)
		if !ok {
			return ""
		}
		return branch.Name
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
	a.reflogList.Key = func(row int) any {
		rows := a.visibleReflog()
		entry, ok := sourceRow(rows, a.snapshot.Reflog, row)
		if !ok {
			return row
		}
		// Reflog may record identical reset operations in the same second.
		// Include the snapshot seat so those entries never share pointer state.
		return struct {
			Source    int
			Hash      string
			Timestamp int64
			Subject   string
		}{rows[row], entry.Hash, entry.UnixTimestamp, entry.Subject}
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
	return a
}

// launchPath is the repository a launch from the terminal opens: the first
// argument that is not a flag, since a bundle on macOS passes -psn_… and
// other platforms pass their own switches.
func launchPath(args []string) string {
	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		// Resolve a terminal path while the caller's working directory is
		// unambiguous. The initial tab, errors, and recent list then all use
		// the same repository name, including when the argument contains spaces.
		absolute, err := filepath.Abs(arg)
		if err != nil {
			return arg
		}
		return absolute
	}
	return ""
}

// Main runs the TanGit desktop application.
func Main() {
	// Lazygit's history-editing commands invoke this executable as Git's editor.
	// Handle their short-lived todo-file instructions before any GUI or updater
	// initialization, or Git would open another TanGit window and hang the rebase.
	if daemon.InDaemonMode() {
		daemon.Handle(logrus.New().WithField("component", "git-editor"))
		return
	}

	setupUpdater()
	// The window reads the history and settings once it exists: the data
	// directory is resolved from the application, so a first frame already
	// has the chosen theme and the recent repositories.
	opener := repository.Opener{}
	path := launchPath(os.Args[1:])
	mygo.App.WhenReady(func() {
		openAppWindow(opener, path)
		setupTray()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// openAppWindow gives the app its window: one title bar drawn by the app, a
// tab per open repository, and a picker tab to open more. A path opens that
// repository straight away, as a launch from the terminal asks for.
func openAppWindow(opener repositoryOpener, path string) {
	s := newShell(opener)
	win := mygo.NewWindow(appWindowOptions(ui.View(s.view)))
	s.window = win
	s.update = win.Update
	s.invalidate = win.Invalidate
	watchUpdater(s)
	mainWindow = win
	win.OnClose(hideOnClose)
	win.OnClosed(s.shutdown)
	// A window shown, focused or restored reads its repositories again, so
	// what another app changed while TanGit was hidden appears without an
	// F5; losing focus or hiding stands the timer aside.
	win.OnShow(s.handleWindowShown)
	win.OnFocus(s.handleWindowFocused)
	win.OnBlur(s.handleWindowBlurred)
	win.OnHide(s.handleWindowHidden)
	win.OnRestore(s.handleWindowShown)
	// The settings and history are read once the window exists, because the
	// data directory is resolved from it.
	s.loadPreferences()
	installNativeHelpShortcut(s)
	s.start(path)
	s.startAutoRefresh()
}

// appWindowOptions is the shape of TanGit's only window: a hidden title bar
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
		Content:           content,
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
