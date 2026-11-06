package app

import (
	"path"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// codeExplorerDrag stays repository-relative and carries no row index. Virtual
// rows can appear or disappear during a drag without changing its target.
type codeExplorerDrag struct {
	path string
}

func (a *app) applyPendingCodeActions() {
	kind, selected := a.code.pendingTabClose, a.code.pendingTabClosePath
	a.code.pendingTabClose, a.code.pendingTabClosePath = codeTabCloseNone, ""
	var closing []string
	switch kind {
	case codeTabCloseOne:
		closing = append(closing, selected)
	case codeTabCloseOthers:
		if a.code.documents[selected] != nil {
			for _, candidate := range a.code.tabs {
				if candidate != selected {
					closing = append(closing, candidate)
				}
			}
		}
	case codeTabCloseRight:
		for index, candidate := range a.code.tabs {
			if candidate == selected {
				closing = append(closing, a.code.tabs[index+1:]...)
				break
			}
		}
	case codeTabCloseAll:
		closing = append(closing, a.code.tabs...)
	}
	if len(closing) > 0 {
		a.closeCodeFiles(closing)
	}

	source, directory := a.code.pendingMoveSource, a.code.pendingMoveTarget
	a.code.pendingMoveSource, a.code.pendingMoveTarget = "", ""
	if source != "" && validCodeManagedPath(source) && (directory == "" || validCodeManagedPath(directory)) {
		destination := joinCodePath(directory, path.Base(source))
		if source != destination {
			a.beginCodeFileOperation(codeFileMove, source, destination)
		}
	}
}

func (a *app) queueCodeTabClose(kind codeTabCloseKind, path string) {
	if a.code.fileOperationBusy {
		return
	}
	a.code.pendingTabClose = kind
	a.code.pendingTabClosePath = path
}

func (a *app) codeTabMenu(m *ui.Menu, path string, index int, services ui.Services) {
	if m.Item(tr("Close Tab")).Disabled(a.code.fileOperationBusy).Chosen() {
		a.queueCodeTabClose(codeTabCloseOne, path)
	}
	if m.Item(tr("Close Other Tabs")).Disabled(a.code.fileOperationBusy || len(a.code.tabs) <= 1).Chosen() {
		a.queueCodeTabClose(codeTabCloseOthers, path)
	}
	if m.Item(tr("Close Tabs to the Right")).Disabled(a.code.fileOperationBusy || index < 0 || index >= len(a.code.tabs)-1).Chosen() {
		a.queueCodeTabClose(codeTabCloseRight, path)
	}
	if m.Item(tr("Close All Tabs")).Disabled(a.code.fileOperationBusy || len(a.code.tabs) == 0).Chosen() {
		a.queueCodeTabClose(codeTabCloseAll, path)
	}
	m.Separator()
	a.codeCopyPathItems(m, path, services)
}

func (a *app) codeExplorerMenu(m *ui.Menu, item fileTreeRow, services ui.Services) {
	if item.path == "" || !validCodeManagedPath(item.path) {
		return
	}
	a.selectCodeTreePath(item.path, item.isDirectory())
	if item.isDirectory() && m.Item(tr("Open folder as workspace")).Chosen() {
		a.openCodeWorkspaceFolder(item.path)
		a.focusCodeSearch()
		if strings.TrimSpace(a.code.searchQuery) != "" {
			a.beginCodeSearch()
		}
	}
	directory := item.path
	if !item.isDirectory() {
		directory = path.Dir(item.path)
		if directory == "." {
			directory = ""
		}
	}
	if m.Item(tr("New File…")).Disabled(a.code.fileOperationBusy).Chosen() {
		a.openCodeFileForm(codeFileFormNewFile, directory, "")
	}
	if m.Item(tr("New Folder…")).Disabled(a.code.fileOperationBusy).Chosen() {
		a.openCodeFileForm(codeFileFormNewFolder, directory, "")
	}
	m.Separator()
	if !item.isDirectory() && m.Item(tr("View File History…")).Chosen() {
		a.openFileHistory(item.path)
	}
	if m.Item(tr("Rename…")).Disabled(a.code.fileOperationBusy).Chosen() {
		a.openCodeFileForm(codeFileFormRename, directory, item.path)
	}
	if m.Item(tr("Delete…")).Disabled(a.code.fileOperationBusy).Chosen() {
		a.confirmCodeDelete(item)
	}
	m.Separator()
	a.codeCopyPathItems(m, item.path, services)
}

func (a *app) codeCopyPathItems(m *ui.Menu, path string, services ui.Services) {
	if m.Item(tr("Copy path")).Chosen() {
		a.notifyCopied(services, "path", a.repositoryPath(path))
	}
	if m.Item(tr("Copy relative path")).Chosen() {
		a.notifyCopied(services, "relative path", path)
	}
}

func (a *app) selectCodeTreePath(path string, directory bool) {
	for row, item := range a.code.rows {
		if item.path == path && item.isDirectory() == directory {
			a.selectCodeRow(row)
			return
		}
	}
}

func (a *app) selectedCodeDirectory() string {
	if a.code.selectedRow < 0 || a.code.selectedRow >= len(a.code.rows) {
		return a.code.searchFolder
	}
	item := a.code.rows[a.code.selectedRow]
	if item.isDirectory() {
		return item.path
	}
	directory := path.Dir(item.path)
	if directory == "." {
		return ""
	}
	return directory
}

func (a *app) openCodeFileForm(kind codeFileFormKind, directory, source string) {
	if a.code.fileOperationBusy || (directory != "" && !validCodeManagedPath(directory)) {
		return
	}
	a.code.fileForm = kind
	a.code.fileFormPath = source
	a.code.fileFormDirectory = directory
	a.code.fileFormName = ""
	a.code.fileFormError = ""
	if kind == codeFileFormRename && validCodeManagedPath(source) {
		a.code.fileFormName = path.Base(source)
	}
}

func (a *app) closeCodeFileForm() {
	a.code.fileForm = codeFileFormNone
	a.code.fileFormPath = ""
	a.code.fileFormDirectory = ""
	a.code.fileFormName = ""
	a.code.fileFormError = ""
}

func (a *app) cancelCodeFileForm() {
	if !a.code.fileOperationBusy {
		a.closeCodeFileForm()
	}
}

func (a *app) submitCodeFileForm() {
	if a.code.fileOperationBusy || a.code.fileForm == codeFileFormNone {
		return
	}
	name, ok := cleanCodeEntryName(a.code.fileFormName)
	if !ok {
		a.code.fileFormError = tr("Enter a valid name without path separators.")
		return
	}
	destination := joinCodePath(a.code.fileFormDirectory, name)
	if !validCodeManagedPath(destination) {
		a.code.fileFormError = tr("The file must stay inside the repository and outside .git.")
		return
	}
	kind := codeFileCreateFile
	source := ""
	switch a.code.fileForm {
	case codeFileFormNewFolder:
		kind = codeFileCreateFolder
	case codeFileFormRename:
		kind = codeFileRename
		source = a.code.fileFormPath
		if source == destination {
			a.closeCodeFileForm()
			return
		}
	}
	a.beginCodeFileOperation(kind, source, destination)
}

func (a *app) confirmCodeDelete(item fileTreeRow) {
	title, message := "Delete file", trf("Delete %s?", item.path)
	detail := "The file is removed from the working tree. This cannot be undone from the Editor."
	if item.isDirectory() {
		title = "Delete folder"
		message = trf("Delete %s and everything inside it?", item.path)
		detail = "All files and subfolders are removed from the working tree. This cannot be undone from the Editor."
	}
	path := item.path
	a.askConfirm(confirmRequest{
		title: title, message: message, detail: detail, confirmLabel: "Delete", destructive: true,
		run: func() { a.beginCodeFileOperation(codeFileDelete, path, path) },
	})
}

func (a *app) beginCodeFileOperation(kind codeFileOperationKind, source, destination string) {
	root := a.snapshot.Root
	if root == "" || a.code.fileOperationBusy || !validCodeManagedPath(destination) ||
		(kind != codeFileCreateFile && kind != codeFileCreateFolder && !validCodeManagedPath(source)) {
		return
	}
	if source != "" {
		if a.fileEditing && codePathWithin(a.fileEditKey, source) {
			a.saveFileEdits()
			if a.fileEditSaveError != "" {
				a.notify("error", "Could not save", tr("Save the open file before changing its path."))
				return
			}
			a.closeFileEdit()
		}
	}
	a.code.fileOperationDirectory = false
	for _, item := range a.code.rows {
		if item.path == source {
			a.code.fileOperationDirectory = item.isDirectory()
			break
		}
	}
	a.code.fileOperationBusy = true
	a.code.fileOperationSeq++
	sequence, generation := a.code.fileOperationSeq, a.generation
	a.code.fileFormError = ""
	go func() {
		defer a.recoverBackground()
		err := performCodeFileOperation(root, kind, source, destination)
		a.update(func() {
			if sequence != a.code.fileOperationSeq || generation != a.generation || a.snapshot.Root != root {
				return
			}
			if err != nil {
				a.code.fileOperationBusy = false
				message := safeError(err)
				if a.code.fileForm != codeFileFormNone {
					a.code.fileFormError = message
				} else {
					a.notify("error", "File operation failed", message)
				}
				return
			}
			a.completeCodeFileOperation(kind, source, destination, a.code.fileOperationDirectory)
			a.code.fileOperationBusy = false
			a.closeCodeFileForm()
			// A pre-operation list read may still be in flight. Invalidate it
			// instead of refusing the fresh read or accepting stale paths later.
			a.code.filesToken++
			a.code.filesLoading = false
			a.code.filesLoaded = false
			a.code.filesOperationRefresh = true
			// A repository read that started before this mutation is stale. Let the
			// operation queue its replacement rather than applying the old snapshot.
			a.snapshotSeq++
			a.requestCodeFiles()
			a.requestQuietRefresh()
			title := "Created file"
			if kind == codeFileCreateFolder {
				title = "Created folder"
			} else if kind == codeFileRename {
				title = "Renamed"
			} else if kind == codeFileMove {
				title = "Moved"
			} else if kind == codeFileDelete {
				title = "Deleted"
			}
			a.notify("success", title, destination)
		})
	}()
}

// completeCodeFileOperation keeps open buffers and view state attached to a
// renamed path without saving or losing any unsaved edits.
func (a *app) completeCodeFileOperation(kind codeFileOperationKind, source, destination string, directory bool) {
	switch kind {
	case codeFileCreateFile:
		a.revealCodeFile(destination)
		return
	case codeFileCreateFolder:
		if a.code.extraDirectories == nil {
			a.code.extraDirectories = make(map[string]bool)
		}
		a.code.extraDirectories[destination] = true
		a.revealCodeDirectory(destination)
		return
	case codeFileDelete:
		for _, candidate := range append([]string(nil), a.code.tabs...) {
			if codePathWithin(candidate, source) {
				a.discardCodeDocument(candidate)
			}
		}
		a.rekeyCodeDirectories(source, "")
	case codeFileRename, codeFileMove:
		for _, candidate := range append([]string(nil), a.code.tabs...) {
			if codePathWithin(candidate, source) {
				a.rekeyCodeDocument(candidate, relocateCodePath(candidate, source, destination))
			}
		}
		a.rekeyCodeDirectories(source, destination)
	}
	// Removing the final entry does not remove its real parent directory. Keep
	// that parent visible even though git ls-files no longer reports it.
	if parent := path.Dir(source); parent != "." && parent != "/" {
		if a.code.extraDirectories == nil {
			a.code.extraDirectories = make(map[string]bool)
		}
		a.code.extraDirectories[parent] = true
	}
	a.clearCodeSearchResults()
	if a.fileHistoryOpen && codePathWithin(a.fileHistoryPath, source) {
		a.fileHistoryOpen = false
		a.fileHistoryToken++
	}
	if kind == codeFileDelete {
		return
	}
	if directory {
		a.revealCodeDirectory(destination)
	} else {
		a.revealCodeFile(destination)
	}
}

func (a *app) rekeyCodeDocument(source, destination string) {
	document := a.code.documents[source]
	if document == nil || destination == "" || source == destination {
		return
	}
	delete(a.code.documents, source)
	if a.code.preview == source {
		a.code.preview = destination
	}
	document.path = destination
	document.generation++
	document.saveError = ""
	if document.state != nil {
		document.state.Label = destination
		document.state.Buffer.SetLanguage(editor.LanguageFor(destination))
	} else {
		document.loading = false
		document.loadError = ""
		document.binary = false
		document.imagePreview = nil
	}
	a.code.documents[destination] = document
	if document.state == nil {
		a.requestCodeDocument(document)
	}
	for index, candidate := range a.code.tabs {
		if candidate == source {
			a.code.tabs[index] = destination
		}
	}
	if a.code.active == source {
		a.code.active = destination
	}
}

func (a *app) rekeyCodeDirectories(source, destination string) {
	if destination != "" {
		if a.code.extraDirectories == nil {
			a.code.extraDirectories = make(map[string]bool)
		}
		a.code.extraDirectories[destination] = true
	}
	for directory := range a.code.extraDirectories {
		if codePathWithin(directory, source) {
			delete(a.code.extraDirectories, directory)
			if destination != "" {
				a.code.extraDirectories[relocateCodePath(directory, source, destination)] = true
			}
		}
	}
	for key, collapsed := range a.fileTreeCollapsed {
		if !strings.HasPrefix(key, codeFileTreeScope+"\x00") {
			continue
		}
		directory := strings.TrimPrefix(key, codeFileTreeScope+"\x00")
		if !codePathWithin(directory, source) {
			continue
		}
		delete(a.fileTreeCollapsed, key)
		if destination != "" {
			a.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, relocateCodePath(directory, source, destination))] = collapsed
		}
	}
}

func (a *app) revealCodeDirectory(directory string) {
	a.code.sidebar = codeSidebarFiles
	a.code.filter = ""
	a.workspacePaneCollapsed[sectionCode] = false
	if a.fileTreeCollapsed == nil {
		a.fileTreeCollapsed = make(map[string]bool)
	}
	for parent := path.Dir(directory); parent != "." && parent != "/"; parent = path.Dir(parent) {
		a.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, parent)] = false
	}
	a.code.pendingReveal = ""
	a.code.pendingRevealDirectory = directory
	a.syncCodeTree()
	a.applyPendingCodeReveal()
}

func (a *app) clearCodeSearchResults() {
	a.code.searchSubmitted = ""
	a.code.searchResults = nil
	a.code.searchBackend = workspaceSearchBackendNone
	a.code.searchSelected, a.code.searchSelectedRow = -1, -1
	a.rebuildCodeSearchRows()
}

func relocateCodePath(path, source, destination string) string {
	if path == source {
		return destination
	}
	return joinCodePath(destination, strings.TrimPrefix(strings.TrimPrefix(path, source), "/"))
}
