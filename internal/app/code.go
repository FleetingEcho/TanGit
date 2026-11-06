package app

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"lazygitapp/internal/repository"
)

const (
	codeFileTreeScope = "code"
	codeSidebarFiles  = 0
	codeSidebarSearch = 1

	codeSearchDebounceDelay = 300 * time.Millisecond
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
	path                string
	diskPath            string
	readOnly            bool
	state               *editor.State
	generation          uint64
	loading             bool
	binary              bool
	imagePreview        *mediaPreview
	loadError           string
	saveError           string
	branchNotice        string
	pendingLine         int
	pendingColumn       int
	pendingUTF16        bool
	hoverSeq            uint64
	hoverState          *editor.State
	hoverScroll         ui.ScrollState
	definitionSeq       uint64
	completionSeq       uint64
	languageDiagnostics map[string][]editor.Diagnostic
	markdownPreviewOpen bool
}

type codeNavigationLocation struct {
	path   string
	line   int
	column int
}

// codeSearchRow is either a collapsible file heading (match == -1) or one
// match within that file, so the virtualized list can retain keyboard support.
type codeSearchRow struct {
	path    string
	match   int
	matches int
}

func (r codeSearchRow) isFile() bool { return r.match < 0 }

type codeState struct {
	paneWidth      float32
	list           ui.ListState
	files          []string
	visiblePaths   []string
	rows           []fileTreeRow
	selectedRow    int
	selectedSource int
	// treeDirty is set by folder folding. Stable frames retain the already
	// sorted rows instead of rebuilding a large repository tree every frame.
	treeDirty     bool
	appliedFilter string
	filter        string
	filesLoading  bool
	filesLoaded   bool
	filesError    string
	filesToken    uint64
	// filesOperationRefresh marks the quiet snapshot that belongs to an Editor
	// structural operation whose explicit file-index reload already ran.
	filesOperationRefresh bool
	pendingReveal         string
	// Empty directories are not returned by git ls-files, so newly created
	// ones stay in the explorer until they get a file of their own.
	extraDirectories       map[string]bool
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

	sidebar                int
	searchFocus            ui.Handle
	searchList             ui.ListState
	searchSelected         int
	searchSelectedRow      int
	searchRows             []codeSearchRow
	searchCollapsed        map[string]bool
	searchQuery            string
	searchSubmitted        string
	searchSubmittedOptions workspaceSearchOptions
	searchCaseSensitive    bool
	searchWholeWord        bool
	searchRegularExpr      bool
	searchInclude          string
	searchExclude          string
	// searchFolder is the opened workspace folder. searchFolderDraft is only
	// the editable field until the user submits or picks a different folder.
	searchFolder                string
	searchFolderDraft           string
	searchFiltersOpen           bool
	searchFolderSelectionSeq    uint64
	searchError                 string
	searchRunning               bool
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
}

func (a *app) resetCode() {
	a.stopGoLanguageServer()
	a.stopAdditionalLanguageServers()
	if a.code.searchCancel != nil {
		a.code.searchCancel()
	}
	a.code.filesToken++
	a.code.searchToken++
	a.code.searchDebounceSeq++
	for _, document := range a.code.documents {
		document.generation++
	}
	a.code.files = nil
	a.code.visiblePaths = nil
	a.code.rows = nil
	a.code.selectedRow, a.code.selectedSource = -1, -1
	a.code.treeDirty = false
	a.code.filter, a.code.appliedFilter = "", ""
	a.code.filesLoading, a.code.filesLoaded = false, false
	a.code.filesError = ""
	a.code.filesOperationRefresh = false
	a.code.pendingReveal = ""
	a.code.extraDirectories = nil
	a.code.pendingRevealDirectory = ""
	a.closeCodeFileForm()
	a.code.fileOperationBusy = false
	a.code.fileOperationSeq++
	a.code.pendingTabClose = codeTabCloseNone
	a.code.pendingTabClosePath = ""
	a.code.pendingMoveSource = ""
	a.code.pendingMoveTarget = ""
	a.code.documents = nil
	a.code.tabs = nil
	a.code.active = ""
	a.code.activeTab = 0
	a.code.preview = ""
	a.code.back = nil
	a.code.forward = nil
	a.code.sidebar = codeSidebarFiles
	a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	a.code.searchQuery, a.code.searchSubmitted = "", ""
	a.code.searchSubmittedOptions = workspaceSearchOptions{}
	a.code.searchCaseSensitive = false
	a.code.searchWholeWord = false
	a.code.searchRegularExpr = false
	a.code.searchInclude, a.code.searchExclude = "", ""
	a.code.searchFolder = ""
	a.code.searchFolderDraft = ""
	a.code.searchFiltersOpen = false
	a.code.searchFolderSelectionSeq++
	a.code.searchError = ""
	a.code.searchRunning = false
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
		if codeDocumentModified(document) {
			document.branchNotice = tr("Unsaved changes kept from previous branch")
			document.generation++
			document.loading = false
			continue
		}
		document.generation++
		document.loading = false
		document.state = nil
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

func (a *app) requestCodeFiles() {
	if a.snapshot.Root == "" || a.code.filesLoading {
		return
	}
	root := a.snapshot.Root
	a.code.filesToken++
	token := a.code.filesToken
	a.code.filesLoading = true
	a.code.filesError = ""
	go func() {
		defer a.recoverBackground()
		paths, err := listCodeFiles(root)
		var emptyDirectories []string
		if err == nil {
			emptyDirectories = listCodeEmptyDirectories(root)
		}
		a.update(func() {
			if token != a.code.filesToken {
				return
			}
			a.code.filesLoading = false
			a.code.filesLoaded = true
			if err != nil {
				a.code.filesError = safeError(err)
				return
			}
			a.code.files = paths
			for _, directory := range emptyDirectories {
				if a.code.extraDirectories == nil {
					a.code.extraDirectories = make(map[string]bool)
				}
				a.code.extraDirectories[directory] = true
			}
			a.pruneCodeDirectories(root)
			a.syncCodeTree()
			a.applyPendingCodeReveal()
			if a.code.searchAfterFiles {
				a.code.searchAfterFiles = false
				a.beginCodeSearch()
			}
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
	if len(a.code.rows) > 0 && a.code.selectedRow < 0 {
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
		document = &codeDocument{path: path, diskPath: diskPath, readOnly: readOnly}
		a.code.documents[path] = document
		if a.code.preview != "" {
			previewPath := a.code.preview
			if previewDocument := a.code.documents[previewPath]; previewDocument != nil {
				previewDocument.generation++
			}
			delete(a.code.documents, previewPath)
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
		} else {
			a.code.tabs = append(a.code.tabs, path)
		}
		if pinned {
			a.code.preview = ""
		} else {
			a.code.preview = path
		}
	} else if pinned && a.code.preview == path {
		a.code.preview = ""
	}
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
	if document.state != nil {
		a.revealCodeLine(document)
		return
	}
	if !document.loading {
		a.requestCodeDocument(document)
	}
}

func (a *app) requestCodeDocument(document *codeDocument) {
	if document == nil || document.loading || a.snapshot.Root == "" {
		return
	}
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
	go func() {
		defer a.recoverBackground()
		content, err := readWorkingFile(onDisk)
		binary := err == nil && bytes.IndexByte(content, 0) >= 0
		var preview *mediaPreview
		var state *editor.State
		if err == nil {
			if binary || isMediaPath(path) {
				decoded := decodeMediaData(content)
				if decoded.bitmap != nil {
					preview = &decoded
					binary = true
				}
			}
			if !binary {
				buffer := editor.NewBuffer(string(content))
				buffer.SetLanguage(editor.LanguageFor(path))
				state = &editor.State{
					Buffer:      buffer,
					SoftWrap:    true,
					LineNumbers: editor.LineNumbersOn,
					Label:       path,
				}
			}
		}
		a.update(func() {
			current := a.code.documents[path]
			if current != document || token != document.generation {
				return
			}
			document.loading = false
			if err != nil {
				document.loadError = safeError(err)
				return
			}
			if binary {
				document.binary = true
				document.imagePreview = preview
				return
			}
			document.state = state
			a.configureGoLanguageDocument(document)
			a.revealCodeLine(document)
		})
	}()
}

func (a *app) revealCodeLine(document *codeDocument) {
	if document == nil || document.state == nil || document.pendingLine <= 0 {
		return
	}
	line := document.pendingLine - 1
	column := document.pendingColumn
	document.pendingLine, document.pendingColumn = 0, 0
	if document.pendingUTF16 {
		column = languageServerUTF16ColumnToByte(document.state.Buffer.Line(line), column)
	}
	document.pendingUTF16 = false
	document.state.GoTo(editor.Pos{Line: line, Col: column})
	document.state.ScrollTo(line)
}

func (a *app) saveCodeDocument(document *codeDocument) {
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
	if err := writeWorkingFile(path, content.Bytes()); err != nil {
		document.saveError = safeError(err)
		return
	}
	document.state.Buffer.MarkSaved()
	document.saveError = ""
	document.branchNotice = ""
	if a.goLanguageServer != nil && strings.EqualFold(filepath.Ext(document.path), ".go") {
		a.syncGoLanguageServerDocument(document.path, document)
		a.goLanguageServer.DidSave(languageServerFileURI(a.workingFilePath(document.path)))
	}
	for _, server := range a.additionalLanguageServersForDocument(document) {
		a.syncManagedLanguageServerDocument(server, document.path, document)
		server.connection.DidSave(languageServerFileURI(a.workingFilePath(document.path)))
	}
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

// rebuildCodeSearchRows groups consecutive results from the same sorted file.
// The selected match is kept where possible; collapsing its file selects the
// file heading rather than leaving the list pointing at a hidden result.
func (a *app) rebuildCodeSearchRows() {
	selectedFile := ""
	if a.code.searchSelectedRow >= 0 && a.code.searchSelectedRow < len(a.code.searchRows) {
		row := a.code.searchRows[a.code.searchSelectedRow]
		if row.isFile() {
			selectedFile = row.path
		}
	}
	a.code.searchRows = nil
	for first := 0; first < len(a.code.searchResults); {
		path := a.code.searchResults[first].Path
		end := first + 1
		for end < len(a.code.searchResults) && a.code.searchResults[end].Path == path {
			end++
		}
		a.code.searchRows = append(a.code.searchRows, codeSearchRow{path: path, match: -1, matches: end - first})
		if !a.code.searchCollapsed[path] {
			for match := first; match < end; match++ {
				a.code.searchRows = append(a.code.searchRows, codeSearchRow{path: path, match: match})
			}
		}
		first = end
	}
	if len(a.code.searchRows) == 0 {
		a.code.searchSelected, a.code.searchSelectedRow = -1, -1
		return
	}
	if a.code.searchSelected < 0 || a.code.searchSelected >= len(a.code.searchResults) {
		a.code.searchSelected = 0
	}
	a.code.searchSelectedRow = -1
	for row, item := range a.code.searchRows {
		if selectedFile != "" && item.isFile() && item.path == selectedFile {
			a.code.searchSelectedRow = row
			break
		}
		if selectedFile == "" && item.match == a.code.searchSelected {
			a.code.searchSelectedRow = row
			break
		}
	}
	if a.code.searchSelectedRow < 0 {
		path := a.code.searchResults[a.code.searchSelected].Path
		for row, item := range a.code.searchRows {
			if item.isFile() && item.path == path {
				a.code.searchSelectedRow = row
				break
			}
		}
	}
}

func (a *app) selectCodeSearchRow(row int) {
	if row < 0 || row >= len(a.code.searchRows) {
		return
	}
	a.code.searchSelectedRow = row
	if match := a.code.searchRows[row].match; match >= 0 {
		a.code.searchSelected = match
	}
}

func (a *app) toggleCodeSearchFile(path string) {
	if a.code.searchCollapsed == nil {
		a.code.searchCollapsed = make(map[string]bool)
	}
	a.code.searchCollapsed[path] = !a.code.searchCollapsed[path]
	a.rebuildCodeSearchRows()
}

func (a *app) revealCodeSearchMatch(match int) {
	if match < 0 || match >= len(a.code.searchResults) {
		return
	}
	delete(a.code.searchCollapsed, a.code.searchResults[match].Path)
	a.code.searchSelected = match
	a.code.searchSelectedRow = -1
	a.rebuildCodeSearchRows()
	for row, item := range a.code.searchRows {
		if item.match == match {
			a.code.searchSelectedRow = row
			a.code.searchList.ScrollIntoView(row)
			return
		}
	}
}

// codeSearchQueryChanged debounces reactive searches. Clearing the field is
// immediate: it cancels stale work, old results, and option highlights at once.
func (a *app) codeSearchQueryChanged() {
	if strings.TrimSpace(a.code.searchQuery) == "" {
		a.beginCodeSearch()
		return
	}
	// Every edit retires the previous search immediately. Only the replacement
	// search is debounced; an old spinner or result callback must not survive
	// while the user is typing a different query.
	if a.code.searchCancel != nil {
		a.code.searchCancel()
		a.code.searchCancel = nil
	}
	a.code.searchToken++
	a.code.searchRunning = false
	a.code.searchAfterFiles = false
	a.code.searchDebounceSeq++
	sequence := a.code.searchDebounceSeq
	scheduleCodeSearch(codeSearchDebounceDelay, func() {
		a.update(func() {
			if sequence == a.code.searchDebounceSeq {
				a.beginCodeSearch()
			}
		})
	})
}

func (a *app) beginCodeSearch() {
	a.code.searchDebounceSeq++
	query := strings.TrimSpace(a.code.searchQuery)
	if a.code.filesLoading && query != "" {
		a.code.searchAfterFiles = true
		return
	}
	a.code.searchAfterFiles = false
	if a.code.searchCancel != nil {
		a.code.searchCancel()
		a.code.searchCancel = nil
	}
	a.code.searchToken++
	token := a.code.searchToken
	a.code.searchSubmitted = query
	folder, validFolder := normalizedCodeSearchFolder(a.code.searchFolder)
	if !validFolder {
		a.code.searchError = tr("Search folder must stay inside the repository")
		a.code.searchSubmittedOptions = workspaceSearchOptions{}
		a.code.searchResults, a.code.searchRows = nil, nil
		a.code.searchBackend = workspaceSearchBackendNone
		a.code.searchRunning = false
		return
	}
	a.code.searchFolder = folder
	a.code.searchFolderDraft = folder
	a.code.searchSubmittedOptions = workspaceSearchOptions{
		Query:               query,
		CaseSensitive:       a.code.searchCaseSensitive,
		WholeWord:           a.code.searchWholeWord,
		RegularExpr:         a.code.searchRegularExpr,
		Include:             a.code.searchInclude,
		Exclude:             a.code.searchExclude,
		Folder:              a.code.searchFolder,
		ExcludedDirectories: append([]string{}, a.settings.SearchExcludedDirectories...),
	}
	a.code.searchError = ""
	a.code.searchResults, a.code.searchRows = nil, nil
	a.code.searchBackend = workspaceSearchBackendNone
	a.code.searchCollapsed = nil
	a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	if query == "" || a.snapshot.Root == "" {
		if query == "" {
			a.code.searchCaseSensitive = false
			a.code.searchWholeWord = false
			a.code.searchRegularExpr = false
			a.code.searchInclude, a.code.searchExclude = "", ""
			a.code.searchFiltersOpen = false
			// The already captured empty options remain the single source of
			// truth for highlighting; no cleared option can leak into a later run.
			a.code.searchSubmittedOptions = workspaceSearchOptions{}
		}
		a.code.searchRunning = false
		return
	}
	root := a.snapshot.Root
	paths := append([]string(nil), a.code.files...)
	options := a.code.searchSubmittedOptions
	ctx, cancel := context.WithCancel(context.Background())
	a.code.searchCancel = cancel
	a.code.searchRunning = true
	go func() {
		defer a.recoverBackground()
		matches, backend, err := runCodeSearch(ctx, root, paths, options, 500)
		if ctx.Err() == nil {
			matches = rankWorkspaceSearchMatches(matches, options)
		}
		a.update(func() {
			if token != a.code.searchToken {
				return
			}
			a.code.searchRunning = false
			a.code.searchCancel = nil
			a.code.searchError = safeError(err)
			a.code.searchResults = matches
			a.code.searchBackend = backend
			a.code.searchSelected = 0
			a.code.searchSelectedRow = -1
			a.rebuildCodeSearchRows()
		})
	}()
}

func (a *app) openCodeSearchMatch(match workspaceSearchMatch) {
	a.openCodeLocation(match.Path, match.Line, match.Column-1)
}

func (a *app) navigateCodeSearch(direction int) {
	match, ok := wrappedSearchRow(a.code.searchSelected, len(a.code.searchResults), direction)
	if !ok {
		return
	}
	a.revealCodeSearchMatch(match)
	a.openCodeSearchMatch(a.code.searchResults[match])
}
