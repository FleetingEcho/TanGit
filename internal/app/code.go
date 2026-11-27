package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const (
	codeFileTreeScope = "code"
	codeSidebarFiles  = 0
	codeSidebarSearch = 1

	codeSearchDebounceDelay = 300 * time.Millisecond
	codeSearchPageSize      = 500
	codeSearchAllResults    = int(^uint(0) >> 1)
)

// scheduleCodeSearch is a seam for proving that rapid edits collapse into one
// search without making interaction tests wait for a real timer.
var scheduleCodeSearch = func(delay time.Duration, run func()) {
	time.AfterFunc(delay, run)
}

var runCodeSearch = searchWorkspaceFilesWithOptions

type codeFileFormKind uint8

const (
	codeFileFormNone codeFileFormKind = iota
	codeFileFormNewFile
	codeFileFormNewFolder
	codeFileFormRename
)

type codeTabCloseKind uint8

const (
	codeTabCloseNone codeTabCloseKind = iota
	codeTabCloseOne
	codeTabCloseOthers
	codeTabCloseRight
	codeTabCloseAll
)

type codeDocument struct {
	path     string
	diskPath string
	readOnly bool
	state    *editor.State
	// loaded says the file's contents, rather than just its persistent
	// editor State, are ready for language services and pending navigation.
	loaded               bool
	generation           uint64
	loading              bool
	binary               bool
	imagePreview         *mediaPreview
	loadError            string
	saveError            string
	branchNotice         string
	pendingLine          int
	pendingColumn        int
	pendingUTF16         bool
	pendingSelection     bool
	pendingSearchOptions workspaceSearchOptions
	hoverSeq             uint64
	hoverState           *editor.State
	hoverScroll          ui.ScrollState
	definitionSeq        uint64
	completionSeq        uint64
	languageActionSeq    uint64
	languageRenameSeq    uint64
	languageReferenceSeq uint64
	signatureSeq         uint64
	signatureVersion     uint64
	signatureRequested   bool
	symbolSeq            uint64
	symbolVersion        uint64
	symbolsRequested     bool
	symbolsLoading       bool
	symbols              []languageServerDocumentSymbol
	inlaySeq             uint64
	inlayVersion         uint64
	inlayRequested       bool
	languageInlays       []editor.Inlay
	semanticSeq          uint64
	semanticVersion      uint64
	semanticRequested    bool
	formatSeq            uint64
	formatting           bool
	languageDiagnostics  map[string][]editor.Diagnostic
	blameEnabled         bool
	blameLoading         bool
	blameLoaded          bool
	blameError           string
	blameToken           uint64
	blameVersion         uint64
	blameLines           []repository.BlameLine
	blameInlays          []editor.Inlay
	blameOffsets         map[int]int
	blameGutterOffsets   map[int]int
	blameGutterWidth     float32
	blameFull            bool
	// pendingFullBlame asks for the full-file blame once the file has loaded,
	// for a document opened by the context menu's Blame action.
	pendingFullBlame       bool
	blameRevision          string
	blameRevisionPath      string
	blameNewer             []codeBlameSnapshot
	blameNavigationToken   uint64
	blameNavigationLoading bool
	blameNavigationError   string
	blameAvatarInvalidate  func()
	blameActiveLine        int
	blameAnnotationSet     bool
	// breadcrumbMemo and stickyMemo cache this frame's symbol-breadcrumb
	// results: both are recomputed only when the buffer version, the anchor
	// offset or the symbols slice changes, never per idle frame.
	breadcrumbMemo      breadcrumbMemo
	stickyMemo          breadcrumbMemo
	markdownPreviewOpen bool
	localStamp          localFileStamp
	localStampKnown     bool
	remoteStamp         remoteFileStamp
	remoteStampKnown    bool
	remoteSave          remoteSaveState
}

type codeNavigationLocation struct {
	path   string
	line   int
	column int
}

type shutdownPreparation struct {
	running   bool
	callbacks []func(bool)
}

type codeState struct {
	shutdown             shutdownPreparation
	languageEditSeq      uint64
	languageEditRunning  bool
	languageDialog       codeLanguageDialog
	languageDocument     *codeDocument
	languageOffset       int
	languageRename       string
	languageError        string
	languageSymbols      languageSymbolsViewState
	languageActions      []languageServerCodeAction
	shortcutQuery        string
	shortcutRecording    string
	shortcutSlot         int
	shortcutError        string
	languageReferences   []languageServerLocation
	languagePeekPath     string
	languagePeekState    *editor.State
	languagePeekLine     int
	languagePeekLoading  bool
	recoveryRoot         string
	recoverySignature    localFileStamp
	recoverySeq          uint64
	recoveryLoaded       bool
	recoveryEntries      map[string]codeRecoveryEntry
	recoveryEntriesSeq   uint64
	recoveryInput        []codeRecoveryDocumentInput
	recoveryInputEntries uint64
	recoveryInputValid   bool
	recoveryWrites       *codeRecoveryWriteCoordinator
	paneWidth            float32
	list                 ui.ListState
	files                []string
	paletteFiles         paletteFileMatches
	visiblePaths         []string
	rows                 []fileTreeRow
	selectedRow          int
	selectedSource       int
	// treeDirty is set by folder folding. Stable frames retain the already
	// sorted rows instead of rebuilding a large repository tree every frame.
	treeDirty      bool
	appliedFilter  string
	filter         string
	filesLoading   bool
	filesLoaded    bool
	filesError     string
	filesTruncated bool
	filesToken     uint64
	// filesOperationRefresh marks the quiet snapshot that belongs to an Editor
	// structural operation whose explicit file-index reload already ran.
	filesOperationRefresh bool
	pendingReveal         string
	// Empty directories are not returned by git ls-files, so newly created
	// ones stay in the explorer until they get a file of their own.
	extraDirectories map[string]bool
	// smartSkips are folders left out of search and indexing because they are
	// probably other people's code or generated files.
	smartSkips             []string
	pendingRevealDirectory string

	fileForm               codeFileFormKind
	fileFormPath           string
	fileFormDirectory      string
	fileFormName           string
	fileFormError          string
	fileOperationBusy      bool
	fileOperationDirectory bool
	fileOperationSeq       uint64

	pendingTabClose     codeTabCloseKind
	pendingTabClosePath string
	pendingMoveSource   string
	pendingMoveTarget   string

	documents map[string]*codeDocument
	tabs      []string
	active    string
	activeTab int
	// preview is the one temporary tab replaced by the next ordinary open.
	// Editing or explicitly pinning that document clears this marker.
	preview string
	back    []codeNavigationLocation
	forward []codeNavigationLocation

	sidebar              int
	searchFocus          ui.Handle
	searchList           ui.ListState
	searchSelected       int
	searchSelectedRow    int
	searchRows           []codeSearchRow
	searchCollapsed      map[string]bool
	searchCollapsedSeq   uint64
	searchQuery          string
	searchSubmitted      string
	searchReplaceOpen    bool
	searchReplaceQuery   string
	searchPreserveCase   bool
	searchReplaceFocus   ui.Handle
	searchReplaceRunning bool
	searchReplaceStatus  string
	// searchReplaceUndo is what the last Replace All changed on disk, so it can
	// be taken back while those files are as it left them.
	searchReplaceUndo      codeSearchReplaceUndo
	searchReplaceError     string
	searchReplaceToken     uint64
	searchReplaceCancel    context.CancelFunc
	searchSubmittedOptions workspaceSearchOptions
	searchCaseSensitive    bool
	searchWholeWord        bool
	searchRegularExpr      bool
	searchNativeOnly       bool
	searchIndex            codeSearchIndexState
	searchIndexPaused      bool
	// searchIndexActive is installed by the shell. Nil means a standalone
	// window is active; otherwise it checks visibility, focus and tab identity.
	searchIndexActive      func() bool
	searchRipgrepKnown     bool
	searchRipgrepAvailable bool
	searchInclude          string
	searchExclude          string
	// searchScopeFolder/File preserve literal context-menu scopes while the
	// include field shows the selected path. Editing that field clears them.
	searchScopeFolder string
	searchScopeFile   string
	// searchFolder is the opened workspace folder. searchFolderDraft is only
	// the editable field until the user submits or picks a different folder.
	searchFolder                string
	searchFolderDraft           string
	workspaceFormOpen           bool
	workspaceFormError          string
	searchFiltersOpen           bool
	searchFolderSelectionSeq    uint64
	searchError                 string
	searchRunning               bool
	searchLoadingMore           bool
	searchHasMore               bool
	searchAfterFiles            bool
	searchResults               []workspaceSearchMatch
	searchBackend               workspaceSearchBackend
	searchToken                 uint64
	searchDebounceSeq           uint64
	searchCancel                context.CancelFunc
	languageServersOpen         bool
	languageServersHoverBlocked bool
	markdownPreviewPercent      float32
	// markdownOptions preserves the renderer's caches across editor frames.
	// It is a pointer because renderer options contain their image-cache lock.
	markdownOptions *markdown.Options
	// remoteFileEdit carries only SSH asynchronous-save state for the Changes
	// editor. Local editing keeps its original synchronous fields and control flow.
	remoteFileEdit struct {
		save       remoteSaveState
		client     remoteWorkspaceClient
		stamp      remoteFileStamp
		stampKnown bool
		closing    bool
	}
}

func (a *app) resetCode() {
	a.code.recoverySeq++
	a.code.remoteFileEdit.save.invalidate()
	a.code.remoteFileEdit.client = nil
	a.code.remoteFileEdit.stampKnown = false
	a.code.remoteFileEdit.closing = false
	a.lsp.resetRepository()
	if a.code.searchCancel != nil {
		a.code.searchCancel()
	}
	a.resetCodeSearchIndex()
	a.code.searchIndexPaused = false
	a.code.filesToken++
	a.code.searchToken++
	a.code.searchDebounceSeq++
	for _, document := range a.code.documents {
		document.generation++
		document.remoteSave.invalidate()
		invalidateCodeBlame(document)
	}
	a.closeCodeBlameDetails()
	a.code.replaceFiles(nil)
	a.code.visiblePaths = nil
	a.code.rows = nil
	a.code.selectedRow, a.code.selectedSource = -1, -1
	a.code.treeDirty = false
	a.code.filter, a.code.appliedFilter = "", ""
	a.code.filesLoading, a.code.filesLoaded = false, false
	a.code.filesTruncated = false
	a.code.filesError = ""
	a.code.filesOperationRefresh = false
	a.code.pendingReveal = ""
	a.code.extraDirectories = nil
	a.code.smartSkips = nil
	a.code.pendingRevealDirectory = ""
	a.closeCodeFileForm()
	a.code.fileOperationBusy = false
	a.code.fileOperationSeq++
	a.code.pendingTabClose = codeTabCloseNone
	a.code.pendingTabClosePath = ""
	a.code.pendingMoveSource = ""
	a.code.pendingMoveTarget = ""
	a.code.documents = nil
	a.code.recoveryRoot = ""
	a.code.recoverySignature = localFileStamp{}
	a.code.recoveryLoaded = false
	a.code.recoveryEntries = nil
	a.code.recoveryInput = nil
	a.code.recoveryInputValid = false
	a.code.tabs = nil
	a.code.active = ""
	a.code.activeTab = 0
	a.code.preview = ""
	a.code.back = nil
	a.code.forward = nil
	a.code.sidebar = codeSidebarFiles
	a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	a.code.searchQuery, a.code.searchSubmitted = "", ""
	if a.code.searchReplaceCancel != nil {
		a.code.searchReplaceCancel()
	}
	a.code.searchReplaceOpen = false
	a.code.searchReplaceQuery = ""
	a.code.searchPreserveCase = false
	a.code.searchReplaceRunning = false
	a.code.searchReplaceStatus = ""
	a.code.searchReplaceError = ""
	a.code.searchReplaceUndo = codeSearchReplaceUndo{}
	a.code.searchReplaceToken++
	a.code.searchReplaceCancel = nil
	a.code.searchSubmittedOptions = workspaceSearchOptions{}
	a.code.searchCaseSensitive = false
	a.code.searchWholeWord = false
	a.code.searchRegularExpr = false
	a.code.searchNativeOnly = false
	a.code.searchRipgrepKnown = false
	a.code.searchRipgrepAvailable = false
	a.code.searchInclude, a.code.searchExclude = "", ""
	a.code.searchScopeFolder, a.code.searchScopeFile = "", ""
	a.code.searchFolder = ""
	a.code.searchFolderDraft = ""
	a.code.searchFiltersOpen = false
	a.code.searchFolderSelectionSeq++
	a.code.searchError = ""
	a.code.searchRunning = false
	a.code.searchLoadingMore = false
	a.code.searchHasMore = false
	a.code.searchAfterFiles = false
	a.code.searchResults, a.code.searchRows = nil, nil
	a.code.searchBackend = workspaceSearchBackendNone
	a.code.searchCollapsed = nil
	a.code.searchCancel = nil
	a.code.languageServersOpen = false
	a.code.languageServersHoverBlocked = false
}

// refreshCodeAfterCheckout adopts a different branch without resetting the
// workspace. Clean buffers and the index must be read again, while dirty
// buffers remain the user's unsaved work and are never replaced by Git's new
// file contents. The bool reports whether the snapshots represent a checkout.
func (a *app) refreshCodeAfterCheckout(old, current repository.Snapshot) bool {
	if old.Root == "" || old.Root != current.Root {
		return false
	}
	branchChanged := old.Branch != current.Branch
	detachedCheckout := old.Branch == "" && current.Branch == "" && old.Head != current.Head
	if !branchChanged && !detachedCheckout {
		return false
	}

	a.closeCodeBlameDetails()
	if a.code.searchReplaceCancel != nil {
		a.code.searchReplaceCancel()
	}
	a.code.searchReplaceToken++
	a.code.searchReplaceCancel = nil
	a.code.searchReplaceRunning = false
	a.code.searchReplaceError = ""
	a.code.searchReplaceStatus = ""
	reloadFiles := a.code.filesLoaded || a.code.filesLoading || a.workspaceTab == sectionCode || len(a.code.documents) > 0
	a.code.filesToken++
	a.code.filesLoading, a.code.filesLoaded = false, false
	a.code.filesError = ""
	a.code.back, a.code.forward = nil, nil

	resubmitSearch := a.code.searchRunning || a.code.searchSubmitted != ""
	if a.code.searchCancel != nil {
		a.code.searchCancel()
	}
	a.code.searchToken++
	a.code.searchCancel = nil
	a.code.searchRunning = false
	a.code.searchLoadingMore = false
	a.code.searchHasMore = false
	a.code.searchAfterFiles = resubmitSearch && strings.TrimSpace(a.code.searchQuery) != ""
	a.code.searchSubmitted = ""
	a.code.searchSubmittedOptions = workspaceSearchOptions{}
	a.code.searchResults, a.code.searchRows = nil, nil
	a.code.searchBackend = workspaceSearchBackendNone
	a.code.searchCollapsed = nil
	a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	for _, document := range a.code.documents {
		if document == nil || document.readOnly {
			continue
		}
		invalidateCodeBlame(document)
		if codeDocumentModified(document) {
			document.branchNotice = tr("Unsaved changes kept from previous branch")
			document.generation++
			document.loading = false
			continue
		}
		document.generation++
		document.loading = false
		document.state = nil
		document.loaded = false
		document.binary = false
		document.imagePreview = nil
		document.loadError = ""
		document.saveError = ""
		document.branchNotice = ""
		document.hoverState = nil
		document.languageDiagnostics = nil
		a.requestCodeDocument(document)
	}
	if reloadFiles {
		a.requestCodeFiles()
	} else if a.code.searchAfterFiles {
		a.code.searchAfterFiles = false
		a.beginCodeSearch()
	}
	return true
}

func (s *codeState) replaceFiles(paths []string) {
	s.files = paths
	s.paletteFiles = paletteFileMatches{}
}

func (a *app) requestCodeFiles() {
	if a.snapshot.Root == "" || a.code.filesLoading {
		return
	}
	root := a.snapshot.Root
	a.code.filesToken++
	token := a.code.filesToken
	a.code.filesLoading = true
	a.code.filesError = ""
	remote := a.location.IsRemote()
	plain := a.editorOnlyWorkspace() && !remote
	var remoteClient remoteWorkspaceClient
	var remoteErr error
	if remote {
		remoteClient, remoteErr = a.remoteWorkspaceClient()
	}
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		var paths, extraDirectories []string
		var plainSkipped int
		filesTruncated := false
		var err error
		if remote {
			err = remoteErr
			if err == nil {
				paths, err = listRemoteCodeFiles(ctx, remoteClient, root)
				if err == nil {
					extraDirectories = listRemoteCodeEmptyDirectories(ctx, remoteClient, root)
				}
			}
		} else if plain {
			var found plainFileList
			found, err = listPlainCodeFiles(root)
			paths, extraDirectories = found.Paths, found.ExtraDirectories
			plainSkipped = found.Skipped
			filesTruncated = found.Truncated
		} else {
			paths, err = listCodeFiles(root)
			if err == nil {
				extraDirectories = listCodeEmptyDirectories(root)
			}
		}
		a.updateChanged(func() bool {
			if token != a.code.filesToken {
				return false
			}
			a.code.filesLoading = false
			a.code.filesLoaded = true
			if filesTruncated && !a.code.filesTruncated {
				a.resetCodeSearchIndex()
			}
			a.code.filesTruncated = filesTruncated
			if err != nil {
				a.code.filesError = safeError(err)
				return a.workspaceTab == sectionCode
			}
			a.code.replaceFiles(paths)
			for _, directory := range extraDirectories {
				if a.code.extraDirectories == nil {
					a.code.extraDirectories = make(map[string]bool)
				}
				a.code.extraDirectories[directory] = true
			}
			if plainSkipped > 0 {
				a.notify("error", "Some files and folders could not be read",
					fmt.Sprintf("%d entries were skipped (permission denied or unreadable)", plainSkipped))
			}
			if !remote {
				a.pruneCodeDirectories(root)
			}
			a.syncCodeTree()
			a.applyPendingCodeReveal()
			// With the file list known, the folders not worth searching are
			// found, and a repository without a search index starts one in the
			// background, whatever page is showing.
			a.refreshSmartSkips()
			a.ensureCodeSearchIndex()
			if a.code.searchAfterFiles {
				a.code.searchAfterFiles = false
				a.beginCodeSearch()
			}
			return a.workspaceTab == sectionCode
		})
	}()
}

func (a *app) ensureCodeFiles() {
	if !a.code.filesLoaded && !a.code.filesLoading {
		a.requestCodeFiles()
	}
}

func (a *app) syncCodeTree() {
	a.code.treeDirty = false
	selectedPath := ""
	selectedDirectory := false
	if a.code.selectedRow >= 0 && a.code.selectedRow < len(a.code.rows) {
		item := a.code.rows[a.code.selectedRow]
		selectedPath = item.path
		selectedDirectory = item.isDirectory()
	}
	// Clearing Quick open returns to the document the user is actually viewing,
	// rather than leaving whatever unrelated row the filter last selected.
	clearingFilter := strings.TrimSpace(a.code.appliedFilter) != "" && strings.TrimSpace(a.code.filter) == ""
	revealActive := clearingFilter && a.code.active != ""
	if revealActive {
		selectedPath, selectedDirectory = a.code.active, false
		for directory := path.Dir(a.code.active); directory != "." && directory != "/"; directory = path.Dir(directory) {
			if a.fileTreeCollapsed == nil {
				a.fileTreeCollapsed = make(map[string]bool)
			}
			a.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, directory)] = false
		}
	}
	filter := strings.TrimSpace(a.code.filter)
	scope := a.code.searchFolder
	scopedFiles := make([]string, 0, len(a.code.files))
	for _, filename := range a.code.files {
		if codePathWithin(filename, scope) {
			scopedFiles = append(scopedFiles, filename)
		}
	}
	a.code.visiblePaths = filterCodePaths(scopedFiles, a.code.filter)
	a.code.appliedFilter = a.code.filter
	extraDirectories := make([]string, 0, len(a.code.extraDirectories))
	for directory := range a.code.extraDirectories {
		if codePathWithin(directory, scope) {
			extraDirectories = append(extraDirectories, directory)
		}
	}
	directories := filterCodePathsFzf(extraDirectories, a.code.filter)
	if a.fileTreeCollapsed == nil {
		a.fileTreeCollapsed = make(map[string]bool)
	}
	for _, directory := range append(fileTreeDirectories(scopedFiles), directories...) {
		key := fileTreeCollapseKey(codeFileTreeScope, directory)
		if _, known := a.fileTreeCollapsed[key]; !known {
			a.fileTreeCollapsed[key] = true
		}
	}
	collapsed := a.fileTreeCollapsed
	// Filtering reveals matches without changing the user's folder choices.
	if filter != "" {
		collapsed = nil
	}
	// Ancestors outside the opened workspace must not hide its contents while
	// the tree is built. Keep their repository-wide collapse choices intact;
	// only this scoped, temporary view treats the path to the root as expanded.
	if scope != "" && filter == "" {
		workspaceCollapsed := make(map[string]bool, len(collapsed))
		for key, value := range collapsed {
			workspaceCollapsed[key] = value
		}
		for directory := scope; directory != "." && directory != "/"; directory = path.Dir(directory) {
			workspaceCollapsed[fileTreeCollapseKey(codeFileTreeScope, directory)] = false
		}
		collapsed = workspaceCollapsed
	}
	// An active Quick open query presents fzf's ranked paths directly. Restore
	// the repository tree and its folder choices as soon as the query clears.
	a.code.rows = buildFileTreeRowsWithDirectories(a.code.visiblePaths, directories, filter == "", collapsed, codeFileTreeScope)
	if scope != "" && filter == "" {
		// Paths stay repository-relative for Git and filesystem operations, but
		// the opened folder itself and its ancestors are not Explorer rows.
		// Rebasing only their visual depth makes its children the new top level.
		depth := strings.Count(scope, "/") + 1
		rows := a.code.rows[:0]
		for _, item := range a.code.rows {
			if item.isDirectory() && codePathWithin(scope, item.path) {
				continue
			}
			item.depth = max(0, item.depth-depth)
			rows = append(rows, item)
		}
		a.code.rows = rows
	}
	a.code.selectedRow, a.code.selectedSource = -1, -1
	if selectedPath != "" {
		for row, item := range a.code.rows {
			if item.path == selectedPath && item.isDirectory() == selectedDirectory {
				a.code.selectedRow, a.code.selectedSource = row, item.source
				break
			}
		}
	}
	if a.code.selectedRow < 0 && a.code.active != "" {
		for row, item := range a.code.rows {
			if !item.isDirectory() && item.path == a.code.active {
				a.code.selectedRow, a.code.selectedSource = row, item.source
				break
			}
		}
	}
	// The ordinary Explorer starts without a selection. Quick open still
	// prepares its first match so Enter can open it immediately.
	if filter != "" && len(a.code.rows) > 0 && a.code.selectedRow < 0 {
		a.code.selectedRow = firstFileTreeFile(a.code.rows)
		if a.code.selectedRow >= 0 {
			a.code.selectedSource = a.code.rows[a.code.selectedRow].source
		}
	}
	if revealActive && a.code.selectedRow >= 0 && a.code.rows[a.code.selectedRow].path == a.code.active {
		a.code.list.ScrollIntoView(a.code.selectedRow)
	}
}

// pruneCodeDirectories forgets only remembered empty folders that no longer
// exist as real directories. Git's file index cannot report this distinction.
func (a *app) pruneCodeDirectories(root string) {
	for directory := range a.code.extraDirectories {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(directory)))
		if err != nil || !info.IsDir() {
			delete(a.code.extraDirectories, directory)
		}
	}
}

func (a *app) selectedCodePath() string {
	if a.code.selectedRow < 0 || a.code.selectedRow >= len(a.code.rows) {
		return ""
	}
	item := a.code.rows[a.code.selectedRow]
	if item.isDirectory() || item.source < 0 || item.source >= len(a.code.visiblePaths) {
		return ""
	}
	return a.code.visiblePaths[item.source]
}

func (a *app) selectCodeRow(row int) {
	if row < 0 || row >= len(a.code.rows) {
		return
	}
	a.code.selectedRow = row
	a.code.selectedSource = a.code.rows[row].source
}

func (a *app) openSelectedCodeFile() {
	if path := a.selectedCodePath(); path != "" {
		a.openCodeFile(path, 0)
	}
}

func (a *app) activeCodeDocument() *codeDocument {
	if a.code.active == "" {
		return nil
	}
	return a.code.documents[a.code.active]
}

// revealCodeFile makes an externally chosen file visible and selected in the
// explorer, even when a filter or collapsed directory previously hid it.
func (a *app) revealCodeFile(filename string) {
	a.code.sidebar = codeSidebarFiles
	a.code.filter = ""
	a.workspacePaneCollapsed[sectionCode] = false
	if a.fileTreeCollapsed == nil {
		a.fileTreeCollapsed = make(map[string]bool)
	}
	for directory := path.Dir(filename); directory != "." && directory != "/"; directory = path.Dir(directory) {
		a.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, directory)] = false
	}
	a.code.pendingReveal = filename
	a.ensureCodeFiles()
	a.syncCodeTree()
	a.applyPendingCodeReveal()
}

// applyPendingCodeReveal finishes a reveal whose file index arrived after the
// editor action. The request token and resetCode keep it repository-local.
func (a *app) applyPendingCodeReveal() {
	if a.code.pendingReveal == "" && a.code.pendingRevealDirectory == "" {
		return
	}
	for row, item := range a.code.rows {
		if !item.isDirectory() && item.path == a.code.pendingReveal {
			a.selectCodeRow(row)
			a.code.list.ScrollIntoView(row)
			a.code.pendingReveal = ""
			return
		}
	}
	if a.code.pendingRevealDirectory != "" {
		for row, item := range a.code.rows {
			if item.isDirectory() && item.path == a.code.pendingRevealDirectory {
				a.selectCodeRow(row)
				a.code.list.ScrollIntoView(row)
				a.code.pendingRevealDirectory = ""
				return
			}
		}
	}
}

func (a *app) openCodeFile(path string, line int) {
	a.openCodeLocationMode(path, line, 0, false)
}

func (a *app) openCodeLocation(path string, line, column int) {
	a.openCodeLocationMode(path, line, column, false)
}

func (a *app) openPinnedCodeFile(path string, line int) {
	a.openCodeLocationMode(path, line, 0, true)
}

func (a *app) openCodeLocationMode(path string, line, column int, pinned bool) {
	if a.snapshot.Root == "" || !validCodeRelativePath(path) {
		return
	}
	a.openCodeDocumentLocation(path, "", false, line, column, pinned)
}

func (a *app) openCodeDocumentLocation(path, diskPath string, readOnly bool, line, column int, pinned bool) {
	a.pinModifiedCodePreview()
	if a.code.documents == nil {
		a.code.documents = make(map[string]*codeDocument)
	}
	document := a.code.documents[path]
	if document == nil {
		document = &codeDocument{path: path, diskPath: diskPath, readOnly: readOnly, blameEnabled: !readOnly, state: newCodeEditorState(path, nil)}
		a.code.documents[path] = document
	}
	if pinned && a.code.preview == path {
		a.code.preview = ""
	}
	if slices.Index(a.code.tabs, path) < 0 {
		// Only the preview's tab is replaced. Its document and any read
		// already in flight survive, so clicking back cannot reload it.
		if !pinned && a.code.preview != "" {
			previewPath := a.code.preview
			if previewDocument := a.code.documents[previewPath]; previewDocument != nil {
				a.closeCodeBlameDetailsFor(previewDocument)
				invalidateCodeBlame(previewDocument)
			}
			replaced := false
			for index, tab := range a.code.tabs {
				if tab == previewPath {
					a.code.tabs[index] = path
					replaced = true
					break
				}
			}
			if !replaced {
				a.code.tabs = append(a.code.tabs, path)
			}
			a.code.preview = path
		} else {
			a.code.tabs = append(a.code.tabs, path)
			if !pinned {
				a.code.preview = path
			}
		}
	}
	// A newer ordinary navigation supersedes any queued search selection.
	document.pendingSelection = false
	document.pendingSearchOptions = workspaceSearchOptions{}
	a.code.active = path
	for index, tab := range a.code.tabs {
		if tab == path {
			a.code.activeTab = index
			break
		}
	}
	if line > 0 {
		document.pendingLine = line
		document.pendingColumn = max(0, column)
		document.pendingUTF16 = false
	}
	if document.loaded {
		a.revealCodeLine(document)
		return
	}
	if !document.loading {
		a.requestCodeDocument(document)
	}
}

// newCodeEditorState creates the editor before a file's asynchronous read.
// Only its buffer changes when the contents arrive, keeping the editor and
// everything connected to it in the pane throughout the read.
func newCodeEditorState(name string, content []byte) *editor.State {
	buffer := editor.NewBuffer(string(content))
	buffer.SetLanguage(editor.LanguageFor(name))
	return &editor.State{Buffer: buffer, SoftWrap: true, LineNumbers: editor.LineNumbersOn, Label: name}
}

func (a *app) requestCodeDocument(document *codeDocument) {
	if document == nil || document.loading || a.snapshot.Root == "" {
		return
	}
	if document.state == nil {
		document.state = newCodeEditorState(document.path, nil)
	}
	if a.location.IsRemote() {
		a.requestRemoteCodeDocument(document)
		return
	}
	invalidateCodeBlame(document)
	root := a.snapshot.Root
	path := document.path
	onDisk := document.diskPath
	if onDisk == "" {
		onDisk = filepath.Join(root, filepath.FromSlash(path))
	}
	document.generation++
	token := document.generation
	document.loading = true
	document.binary = false
	document.imagePreview = nil
	document.loadError = ""
	document.saveError = ""
	document.localStampKnown = false
	go func() {
		defer a.recoverBackground()
		content, err := readWorkingFile(onDisk)
		binary := err == nil && bytes.IndexByte(content, 0) >= 0
		var preview *mediaPreview
		if err == nil {
			if binary || isMediaPath(path) {
				decoded := decodeMediaData(content)
				if decoded.bitmap != nil {
					preview = &decoded
					binary = true
				}
			}
		}
		a.updateChanged(func() bool {
			current := a.code.documents[path]
			if current != document || token != document.generation {
				return false
			}
			document.loading = false
			if err != nil {
				document.loadError = safeError(err)
				return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
			}
			document.localStamp = stampLocalFile(content)
			document.localStampKnown = true
			if binary {
				document.binary = true
				document.imagePreview = preview
				document.loaded = true
				return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
			}
			position, scrollY := document.state.CursorPos(), document.state.ScrollY()
			document.state.Buffer.Reset(string(content))
			document.state.Buffer.SetLanguage(editor.LanguageFor(path))
			document.state.GoTo(position)
			document.state.SetScrollY(scrollY)
			document.loaded = true
			a.restoreCodeRecoveryEntry(document)
			a.configureGoLanguageDocument(document)
			a.configureCodeBlame(document)
			a.revealCodeLine(document)
			a.requestCodeBlame(document)
			a.applyPendingFullBlame(document)
			return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
		})
	}()
}

func (a *app) revealCodeLine(document *codeDocument) {
	if document == nil || !document.loaded || document.state == nil || document.pendingLine <= 0 {
		return
	}
	line := document.pendingLine - 1
	column := document.pendingColumn
	selectMatch := document.pendingSelection
	searchOptions := document.pendingSearchOptions
	document.pendingLine, document.pendingColumn = 0, 0
	document.pendingSelection = false
	document.pendingSearchOptions = workspaceSearchOptions{}
	if document.pendingUTF16 {
		column = languageServerUTF16ColumnToByte(document.state.Buffer.Line(line), column)
	}
	document.pendingUTF16 = false
	document.state.GoTo(editor.Pos{Line: line, Col: column})
	if selectMatch && line >= 0 && line < document.state.Buffer.LineCount() {
		text := document.state.Buffer.Line(line)
		if column >= 0 && column <= len(text) {
			start, end := workspaceSearchRangeWithOptions(text[column:], searchOptions)
			if start == 0 {
				offset := document.state.Buffer.Offset(editor.Pos{Line: line, Col: column})
				document.state.Select(offset, offset+end)
			}
		}
	}
	document.state.ScrollTo(line)
}

func (a *app) saveCodeDocument(document *codeDocument) {
	a.saveCodeDocumentWithOverwrite(document, false)
}

func (a *app) saveCodeDocumentWithOverwrite(document *codeDocument, overwrite bool) {
	if a.location.IsRemote() {
		a.saveRemoteCodeDocument(document, nil)
		return
	}
	if document != nil && document.readOnly {
		return
	}
	// Structural file operations rename or remove paths off the UI thread.
	// Saving during one could recreate the source after it has moved.
	if a.code.fileOperationBusy || document == nil || document.state == nil || document.state.Buffer == nil || !document.state.Buffer.Modified() {
		return
	}
	var content bytes.Buffer
	if _, err := document.state.Buffer.WriteTo(&content); err != nil {
		document.saveError = safeError(err)
		return
	}
	path := a.workingFilePath(document.path)
	if err := writeWorkingFileChecked(path, content.Bytes(), document.localStamp, document.localStampKnown && !overwrite); err != nil {
		document.saveError = safeError(err)
		if errors.Is(err, errLocalFileModified) {
			a.confirmCodeDocumentConflict(document)
		}
		return
	}
	document.state.Buffer.MarkSaved()
	document.localStamp = stampLocalFile(content.Bytes())
	document.localStampKnown = true
	document.saveError = ""
	document.branchNotice = ""
	if a.lsp.goServer != nil && strings.EqualFold(filepath.Ext(document.path), ".go") {
		a.syncGoLanguageServerDocument(document.path, document)
		a.lsp.goServer.DidSave(languageServerFileURI(a.workingFilePath(document.path)))
	}
	for _, server := range a.additionalLanguageServersForDocument(document) {
		a.syncManagedLanguageServerDocument(server, document.path, document)
		server.connection.DidSave(languageServerFileURI(a.workingFilePath(document.path)))
	}
	a.requestCodeBlame(document)
	a.invalidateCodeSearchIndex()
}

func (a *app) confirmCodeDocumentConflict(document *codeDocument) {
	if document == nil || document.state == nil || document.state.Buffer == nil {
		return
	}
	version := document.state.Buffer.Version()
	a.askConfirm(confirmRequest{
		title:            "File changed on disk",
		message:          "This file changed on disk after it was opened.",
		detail:           "Reload discards your unsaved changes. Overwrite replaces the file on disk.",
		confirmLabel:     "Overwrite",
		alternativeLabel: "Reload",
		destructive:      true,
		run: func() {
			if a.code.documents[document.path] == document && document.state != nil &&
				document.state.Buffer != nil && document.state.Buffer.Version() == version {
				a.saveCodeDocumentWithOverwrite(document, true)
			}
		},
		alternative: func() {
			if a.code.documents[document.path] == document {
				a.reloadCodeDocument(document)
			}
		},
	})
}

func (a *app) reloadCodeDocument(document *codeDocument) {
	if document == nil || document.readOnly || a.location.IsRemote() || a.code.documents[document.path] != document {
		return
	}
	if document.state != nil {
		position := document.state.CursorPos()
		document.pendingLine = position.Line + 1
		document.pendingColumn = position.Col
	}
	document.generation++
	document.loading = false
	document.state = nil
	document.loaded = false
	document.binary = false
	document.imagePreview = nil
	document.loadError = ""
	document.saveError = ""
	document.branchNotice = ""
	document.hoverState = nil
	document.languageDiagnostics = nil
	document.localStampKnown = false
	a.requestCodeDocument(document)
}

func (a *app) saveCodeEdits() {
	for _, document := range a.code.documents {
		a.saveCodeDocument(document)
	}
}

func codeDocumentModified(document *codeDocument) bool {
	return document != nil && !document.readOnly && document.state != nil && document.state.Buffer != nil && document.state.Buffer.Modified()
}

func (a *app) pinModifiedCodePreview() {
	if codeDocumentModified(a.code.documents[a.code.preview]) {
		a.code.preview = ""
	}
}

// confirmCodeChanges never writes implicitly: Save, Don't Save and Cancel are
// explicit choices. The continuation owns removal of the approved documents.
func (a *app) confirmCodeChanges(paths []string, run func()) {
	var dirty []*codeDocument
	for _, path := range paths {
		if document := a.code.documents[path]; codeDocumentModified(document) {
			dirty = append(dirty, document)
		}
	}
	if len(dirty) == 0 {
		run()
		return
	}
	names := make([]string, len(dirty))
	for i, document := range dirty {
		names[i] = document.path
	}
	a.askConfirm(confirmRequest{
		title: "Unsaved changes", message: "Save your changes before closing?",
		detail: strings.Join(names, "\n"), confirmLabel: "Save", alternativeLabel: "Don't Save",
		alternative: run,
		run: func() {
			if a.location.IsRemote() {
				a.saveRemoteCodeDocuments(dirty, func(ok bool) {
					if ok {
						run()
					}
				})
				return
			}
			for _, document := range dirty {
				if a.code.documents[document.path] != document {
					return
				}
				a.saveCodeDocument(document)
				if codeDocumentModified(document) {
					return
				}
			}
			run()
		},
	})
}

func (a *app) dirtyCodePaths() []string {
	var paths []string
	for _, path := range a.code.tabs {
		if codeDocumentModified(a.code.documents[path]) {
			paths = append(paths, path)
		}
	}
	return paths
}

func (a *app) closeCodeFiles(paths []string) {
	if a.code.fileOperationBusy {
		return
	}
	a.confirmCodeChanges(paths, func() {
		for _, path := range paths {
			a.removeCodeDocument(path)
		}
	})
}

func (a *app) closeCodeFile(path string) { a.closeCodeFiles([]string{path}) }

// discardCodeDocument removes a document after its on-disk file was deleted.
// It deliberately does not autosave: doing so could recreate a deleted file.
func (a *app) discardCodeDocument(path string) {
	if a.code.documents[path] != nil {
		a.removeCodeDocument(path)
	}
}

func (a *app) removeCodeDocument(path string) {
	document := a.code.documents[path]
	if document == nil {
		return
	}
	document.generation++
	document.remoteSave.invalidate()
	a.closeCodeBlameDetailsFor(document)
	invalidateCodeBlame(document)
	delete(a.code.documents, path)
	if a.code.preview == path {
		a.code.preview = ""
	}
	for index, candidate := range a.code.tabs {
		if candidate != path {
			continue
		}
		activeRemoved := a.code.active == path
		a.code.tabs = append(a.code.tabs[:index], a.code.tabs[index+1:]...)
		if len(a.code.tabs) == 0 {
			a.code.active = ""
			a.code.activeTab = 0
			return
		}
		if activeRemoved {
			a.code.active = a.code.tabs[min(index, len(a.code.tabs)-1)]
		}
		// Removing an inactive tab to the active one's left shifts activeTab;
		// resolve it by path instead of accidentally switching documents.
		for activeTab, tab := range a.code.tabs {
			if tab == a.code.active {
				a.code.activeTab = activeTab
				return
			}
		}
		return
	}
}

// breadcrumbMemo memoizes one symbol-breadcrumb computation per frame key.
type breadcrumbMemo struct {
	ok      bool
	version uint64
	offset  int
	symbols *languageServerDocumentSymbol
	length  int
	value   string
}

// lookup reports the cached breadcrumb for the current frame, when the
// buffer version, the anchor offset and the symbols all match what was
// computed for.
func (m *breadcrumbMemo) lookup(version uint64, offset int, symbols []languageServerDocumentSymbol) (string, bool) {
	if !m.ok || m.version != version || m.offset != offset || m.length != len(symbols) {
		return "", false
	}
	var first *languageServerDocumentSymbol
	if len(symbols) > 0 {
		first = &symbols[0]
	}
	if first != m.symbols {
		return "", false
	}
	return m.value, true
}

// store remembers the breadcrumb computed for the current frame.
func (m *breadcrumbMemo) store(version uint64, offset int, symbols []languageServerDocumentSymbol, value string) {
	var first *languageServerDocumentSymbol
	if len(symbols) > 0 {
		first = &symbols[0]
	}
	m.ok = true
	m.version = version
	m.offset = offset
	m.symbols = first
	m.length = len(symbols)
	m.value = value
}

// cachedSymbolBreadcrumb computes languageServerSymbolBreadcrumb once per
// (buffer version, offset, symbols) combination. Callers must treat the
// symbols slice as immutable between edits; LSP replaces it wholesale on
// every refresh, which is what the pointer comparison detects.
func (m *breadcrumbMemo) cachedSymbolBreadcrumb(buffer *editor.Buffer, symbols []languageServerDocumentSymbol, offset int) string {
	version := buffer.Version()
	if value, ok := m.lookup(version, offset, symbols); ok {
		return value
	}
	value := languageServerSymbolBreadcrumb(symbols, buffer.String(), offset)
	m.store(version, offset, symbols, value)
	return value
}
