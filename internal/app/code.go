package app

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const (
	codeFileTreeScope = "code"
	codeSidebarFiles  = 0
	codeSidebarSearch = 1
)

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
	path          string
	state         *editor.State
	generation    uint64
	loading       bool
	binary        bool
	loadError     string
	saveError     string
	triedVersion  uint64
	pendingLine   int
	pendingColumn int
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
	pendingReveal string
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

	sidebar             int
	searchFocus         ui.Handle
	searchList          ui.ListState
	searchSelected      int
	searchSelectedRow   int
	searchRows          []codeSearchRow
	searchCollapsed     map[string]bool
	searchQuery         string
	searchSubmitted     string
	searchCaseSensitive bool
	searchRunning       bool
	searchResults       []workspaceSearchMatch
	searchToken         uint64
	searchCancel        context.CancelFunc
}

func (a *app) resetCode() {
	if a.code.searchCancel != nil {
		a.code.searchCancel()
	}
	a.code.filesToken++
	a.code.searchToken++
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
	a.code.sidebar = codeSidebarFiles
	a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	a.code.searchQuery, a.code.searchSubmitted = "", ""
	a.code.searchCaseSensitive = false
	a.code.searchRunning = false
	a.code.searchResults, a.code.searchRows = nil, nil
	a.code.searchCollapsed = nil
	a.code.searchCancel = nil
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
			a.pruneCodeDirectories(root)
			a.syncCodeTree()
			a.applyPendingCodeReveal()
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
	a.code.visiblePaths = filterCodePaths(a.code.files, a.code.filter)
	a.code.appliedFilter = a.code.filter
	directories := make([]string, 0, len(a.code.extraDirectories))
	filter := strings.ToLower(strings.TrimSpace(a.code.filter))
	for directory := range a.code.extraDirectories {
		if filter == "" || strings.Contains(strings.ToLower(directory), filter) {
			directories = append(directories, directory)
		}
	}
	a.code.rows = buildFileTreeRowsWithDirectories(a.code.visiblePaths, directories, true, a.fileTreeCollapsed, codeFileTreeScope)
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
	a.openCodeLocation(path, line, 0)
}

func (a *app) openCodeLocation(path string, line, column int) {
	if a.snapshot.Root == "" || !validCodeRelativePath(path) {
		return
	}
	a.saveCodeEdits()
	if a.code.documents == nil {
		a.code.documents = make(map[string]*codeDocument)
	}
	document := a.code.documents[path]
	if document == nil {
		document = &codeDocument{path: path}
		a.code.documents[path] = document
		a.code.tabs = append(a.code.tabs, path)
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
	document.generation++
	token := document.generation
	document.loading = true
	document.binary = false
	document.loadError = ""
	document.saveError = ""
	document.triedVersion = 0
	go func() {
		defer a.recoverBackground()
		content, err := readWorkingFile(filepath.Join(root, filepath.FromSlash(path)))
		binary := bytes.IndexByte(content, 0) >= 0
		var state *editor.State
		if err == nil && !binary {
			buffer := editor.NewBuffer(string(content))
			buffer.SetLanguage(editor.LanguageFor(path))
			state = &editor.State{
				Buffer:      buffer,
				SoftWrap:    true,
				LineNumbers: editor.LineNumbersOn,
				Label:       path,
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
				return
			}
			document.state = state
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
	document.state.GoTo(editor.Pos{Line: line, Col: column})
	document.state.ScrollTo(line)
}

func (a *app) saveCodeDocument(document *codeDocument) {
	// Structural file operations rename or remove paths off the UI thread.
	// Saving during one could recreate the source after it has moved.
	if a.code.fileOperationBusy || document == nil || document.state == nil || document.state.Buffer == nil || !document.state.Buffer.Modified() {
		return
	}
	version := document.state.Buffer.Version()
	if document.saveError != "" && version == document.triedVersion {
		return
	}
	document.triedVersion = version
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
}

func (a *app) saveCodeEdits() {
	for _, document := range a.code.documents {
		a.saveCodeDocument(document)
	}
}

func (a *app) closeCodeFile(path string) {
	document := a.code.documents[path]
	if document == nil || a.code.fileOperationBusy {
		return
	}
	// An explicit close retries a previous failed write; ordinary frames avoid
	// rewriting the same failed version over and over.
	document.saveError = ""
	a.saveCodeDocument(document)
	// A close is not permission to discard text the OS could not save. Keep
	// the tab and its error visible so the user can retry or copy the buffer.
	if document.state != nil && document.state.Buffer != nil && document.state.Buffer.Modified() {
		return
	}
	a.removeCodeDocument(path)
}

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

func (a *app) beginCodeSearch() {
	if a.code.filesLoading {
		return
	}
	query := strings.TrimSpace(a.code.searchQuery)
	if a.code.searchCancel != nil {
		a.code.searchCancel()
		a.code.searchCancel = nil
	}
	a.code.searchToken++
	token := a.code.searchToken
	a.code.searchSubmitted = query
	a.code.searchResults, a.code.searchRows = nil, nil
	a.code.searchCollapsed = nil
	a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	if query == "" || a.snapshot.Root == "" {
		a.code.searchRunning = false
		return
	}
	root := a.snapshot.Root
	paths := append([]string(nil), a.code.files...)
	caseSensitive := a.code.searchCaseSensitive
	ctx, cancel := context.WithCancel(context.Background())
	a.code.searchCancel = cancel
	a.code.searchRunning = true
	go func() {
		defer a.recoverBackground()
		matches := searchWorkspaceFiles(ctx, root, paths, query, caseSensitive, 500)
		a.update(func() {
			if token != a.code.searchToken {
				return
			}
			a.code.searchRunning = false
			a.code.searchCancel = nil
			a.code.searchResults = matches
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
