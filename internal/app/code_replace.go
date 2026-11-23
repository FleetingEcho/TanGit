package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

type codeSearchReplaceOpenDocument struct {
	document   *codeDocument
	state      *editor.State
	generation uint64
	version    uint64
	content    string
	changes    workspaceSearchReplacement
}

type codeSearchReplaceOutcome struct {
	open       []codeSearchReplaceOpenDocument
	pending    map[*codeDocument]int
	client     remoteWorkspaceClient
	count      int
	files      int
	failures   []string
	root       string
	generation uint64
}

// requestCodeSearchReplaceAll validates the entire scope before opening the
// destructive confirmation. In particular, disk contents must never win over
// an unsaved editor buffer during a bulk replacement.
func (a *app) requestCodeSearchReplaceAll() {
	if a.code.searchReplaceRunning || a.code.fileOperationBusy || a.code.filesLoading ||
		a.code.searchRunning || a.code.searchSubmitted == "" ||
		a.code.searchSubmitted != strings.TrimSpace(a.code.searchQuery) || a.snapshot.Root == "" {
		return
	}
	options := a.code.searchSubmittedOptions
	if _, ok := a.codeSearchReplaceDirtyPaths(options); ok {
		return
	}
	searchToken := a.code.searchToken
	a.askConfirm(confirmRequest{
		title: tr("Replace All"), message: tr("Replace all matches in the selected files?"),
		detail:       tr("This changes files on disk. Open files with unsaved changes must be saved first."),
		confirmLabel: tr("Replace All"), destructive: true,
		run: func() {
			if searchToken == a.code.searchToken {
				a.startCodeSearchReplaceAll()
			}
		},
	})
}

// codeSearchReplaceDirtyPaths refuses the complete operation before any file
// is written. The caller's visible error names exactly the unsaved files that
// need attention; out-of-scope dirty documents do not block replacement.
func (a *app) codeSearchReplaceDirtyPaths(options workspaceSearchOptions) ([]string, bool) {
	paths := a.dirtyCodePaths()
	slices.Sort(paths)
	var blocked []string
	for _, name := range paths {
		if options.pathIncluded(name) {
			blocked = append(blocked, name)
		}
	}
	if len(blocked) == 0 {
		return nil, false
	}
	a.code.searchReplaceStatus = ""
	a.code.searchReplaceError = trf("Save unsaved changes before replacing: %s", strings.Join(blocked, ", "))
	return blocked, true
}

func (a *app) startCodeSearchReplaceAll() {
	a.startCodeSearchReplaceAllWithClient(nil)
}

// startCodeSearchReplaceAllWithClient uses one captured remote session for
// both closed files and the subsequent saves of clean open buffers. Passing
// nil resolves the session normally; the explicit form keeps remote tests
// independent of a live SSH server.
func (a *app) startCodeSearchReplaceAllWithClient(client remoteWorkspaceClient) {
	if a.code.searchReplaceRunning || a.code.fileOperationBusy || a.code.filesLoading || a.snapshot.Root == "" {
		return
	}
	options := a.code.searchSubmittedOptions
	if options.Query == "" || options.Query != strings.TrimSpace(a.code.searchQuery) {
		return
	}
	if _, blocked := a.codeSearchReplaceDirtyPaths(options); blocked {
		return
	}
	if _, err := options.regularExpression(); err != nil {
		a.code.searchReplaceError = safeError(err)
		return
	}

	root := a.snapshot.Root
	generation := a.generation
	replacement := a.code.searchReplaceQuery
	preserveCase := a.code.searchPreserveCase
	paths := append([]string(nil), a.code.files...)
	openByPath := make(map[string]codeSearchReplaceOpenDocument)
	pending := make(map[string]*codeDocument)
	readOnly := make(map[string]bool)
	for name, document := range a.code.documents {
		if document == nil || !options.pathIncluded(name) {
			continue
		}
		if document.readOnly {
			readOnly[name] = true
			continue
		}
		if document.state != nil && document.state.Buffer != nil {
			openByPath[name] = codeSearchReplaceOpenDocument{
				document: document, state: document.state, generation: document.generation,
				version: document.state.Buffer.Version(), content: document.state.Buffer.String(),
			}
		} else if document.loading {
			pending[name] = document
		}
	}
	outcome := codeSearchReplaceOutcome{
		pending: make(map[*codeDocument]int), client: client, root: root, generation: generation,
	}
	for name, document := range openByPath {
		changes, err := planWorkspaceSearchReplacement(document.content, options, replacement, preserveCase)
		if err != nil {
			a.code.searchReplaceError = safeError(err)
			return
		}
		document.document = a.code.documents[name]
		document.content = ""
		document.changes = changes
		outcome.open = append(outcome.open, document)
	}
	slices.SortFunc(outcome.open, func(left, right codeSearchReplaceOpenDocument) int {
		return strings.Compare(left.document.path, right.document.path)
	})

	if a.code.searchCancel != nil {
		a.code.searchCancel()
		a.code.searchCancel = nil
	}
	a.code.searchToken++
	a.code.searchRunning = false
	a.code.searchLoadingMore = false
	a.code.searchHasMore = false
	a.code.searchReplaceToken++
	token := a.code.searchReplaceToken
	a.code.searchReplaceRunning = true
	a.code.searchReplaceStatus = ""
	a.code.searchReplaceError = ""
	ctx, cancel := context.WithCancel(a.repositoryWork.Context())
	a.code.searchReplaceCancel = cancel
	remote := a.location.IsRemote()
	var remoteErr error
	if remote && client == nil {
		client, remoteErr = a.remoteWorkspaceClient()
	}
	go func() {
		defer a.recoverBackground()
		if remoteErr == nil {
			paths = scopedCodeSearchPaths(ctx, root, paths, options, client)
		}
		for _, candidate := range paths {
			if ctx.Err() != nil {
				return
			}
			if _, loaded := openByPath[candidate]; loaded || readOnly[candidate] || !options.pathIncluded(candidate) {
				continue
			}
			count, err := 0, remoteErr
			if remote {
				if err == nil {
					count, err = replaceRemoteWorkspaceFile(ctx, client, root, candidate, options, replacement, preserveCase)
				}
			} else {
				count, err = replaceLocalWorkspaceFile(ctx, root, candidate, options, replacement, preserveCase)
			}
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				outcome.failures = append(outcome.failures, fmt.Sprintf("%s: %s", candidate, safeError(err)))
				continue
			}
			if count > 0 {
				outcome.count += count
				outcome.files++
				if document := pending[candidate]; document != nil {
					outcome.pending[document] = count
				}
			}
		}
		a.updateChanged(func() bool {
			if token != a.code.searchReplaceToken || generation != a.generation || root != a.snapshot.Root {
				return false
			}
			a.applyCodeSearchReplaceOpenDocuments(ctx, token, remote, outcome)
			return true
		})
	}()
}

// applyCodeSearchReplaceOpenDocuments verifies each clean buffer again. A late
// user edit is skipped, never reset or marked saved. Remote saves finish in
// sequence through the same stamp and pending-save machinery as ordinary saves.
func (a *app) applyCodeSearchReplaceOpenDocuments(ctx context.Context, token uint64, remote bool, outcome codeSearchReplaceOutcome) {
	applyNext := func(index int) {}
	applyNext = func(index int) {
		if token != a.code.searchReplaceToken || outcome.generation != a.generation || outcome.root != a.snapshot.Root {
			return
		}
		if index == len(outcome.open) {
			a.finishCodeSearchReplace(token, outcome)
			return
		}
		planned := outcome.open[index]
		document := planned.document
		current := a.code.documents[document.path]
		if current != document || document.readOnly || document.state != planned.state ||
			document.generation != planned.generation || document.state == nil || document.state.Buffer == nil ||
			document.state.Buffer.Version() != planned.version || codeDocumentModified(document) {
			outcome.failures = append(outcome.failures, fmt.Sprintf("%s: %s", document.path, tr("the open file changed during replacement")))
			applyNext(index + 1)
			return
		}
		if planned.changes.count == 0 {
			applyNext(index + 1)
			return
		}
		document.state.Buffer.Edit(planned.changes.edits...)
		outcome.count += planned.changes.count
		outcome.files++
		if remote {
			a.saveRemoteCodeDocumentWithClient(outcome.client, document, func(ok bool) {
				if !ok {
					outcome.failures = append(outcome.failures, fmt.Sprintf("%s: %s", document.path, document.saveError))
				}
				applyNext(index + 1)
			})
			return
		}
		a.saveCodeDocument(document)
		if codeDocumentModified(document) {
			outcome.failures = append(outcome.failures, fmt.Sprintf("%s: %s", document.path, document.saveError))
		}
		applyNext(index + 1)
	}
	applyNext(0)
}

func (a *app) finishCodeSearchReplace(token uint64, outcome codeSearchReplaceOutcome) {
	if token != a.code.searchReplaceToken || outcome.generation != a.generation || outcome.root != a.snapshot.Root {
		return
	}
	for document := range outcome.pending {
		current := a.code.documents[document.path]
		if current != document || document.state != nil {
			continue
		}
		// Retire reads that started before the disk replacement. A fresh read is
		// the only content now allowed to populate this pending document.
		document.generation++
		document.loading = false
		document.loadError = ""
		document.binary = false
		document.imagePreview = nil
		a.requestCodeDocument(document)
	}
	a.code.searchReplaceRunning = false
	a.code.searchReplaceCancel = nil
	a.code.searchReplaceStatus = trf("%d replacements in %d files", outcome.count, outcome.files)
	if outcome.count == 0 {
		a.code.searchReplaceStatus = tr("No matches to replace")
	}
	a.code.searchReplaceError = strings.Join(outcome.failures, "; ")
	a.invalidateCodeSearchIndex()
	if a.code.searchSubmitted != "" {
		a.beginCodeSearch()
	}
	if outcome.count > 0 {
		a.requestQuietRefresh()
	}
}

func replaceLocalWorkspaceFile(ctx context.Context, root, name string, options workspaceSearchOptions, replacement string, preserveCase bool) (int, error) {
	cleaned, ok := cleanWorkspaceSearchPath(name)
	if !ok || !options.pathIncluded(cleaned) {
		return 0, nil
	}
	resolvedRoot, err := resolvedCodeSearchIndexRoot(root)
	if err != nil {
		return 0, err
	}
	filename, ok := resolveWorkspaceSearchPath(resolvedRoot, cleaned)
	if !ok {
		return 0, nil
	}
	before, err := os.Stat(filename)
	if err != nil || !before.Mode().IsRegular() || before.Size() > workspaceSearchMaxFileSize {
		return 0, nil
	}
	data, err := readWorkingFile(filename)
	if err != nil {
		return 0, nil
	}
	after, err := os.Stat(filename)
	if err != nil || !sameCodeSearchReplaceFile(before, after) {
		return 0, errors.New("file changed while it was being read")
	}
	if len(data) == 0 || len(data) > workspaceSearchMaxFileSize || bytes.IndexByte(data, 0) >= 0 || ctx.Err() != nil {
		return 0, nil
	}
	changes, err := planWorkspaceSearchReplacement(string(data), options, replacement, preserveCase)
	if err != nil || changes.count == 0 {
		return 0, err
	}
	current, err := os.Stat(filename)
	if err != nil || !sameCodeSearchReplaceFile(after, current) {
		return 0, errors.New("file changed before it could be replaced")
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	if err = writeWorkingFile(filename, []byte(changes.text)); err != nil {
		return 0, err
	}
	return changes.count, nil
}

func sameCodeSearchReplaceFile(left, right os.FileInfo) bool {
	return left != nil && right != nil && right.Mode().IsRegular() &&
		left.Size() == right.Size() && left.ModTime().UnixNano() == right.ModTime().UnixNano()
}

func replaceRemoteWorkspaceFile(ctx context.Context, client remoteWorkspaceClient, root, name string, options workspaceSearchOptions, replacement string, preserveCase bool) (int, error) {
	cleaned, ok := cleanWorkspaceSearchPath(name)
	if !ok || !options.pathIncluded(cleaned) {
		return 0, nil
	}
	data, stamp, err := readRemoteWorkingFile(client, root, cleaned)
	if err != nil {
		return 0, nil
	}
	if len(data) == 0 || len(data) > workspaceSearchMaxFileSize || bytes.IndexByte(data, 0) >= 0 || ctx.Err() != nil {
		return 0, nil
	}
	changes, err := planWorkspaceSearchReplacement(string(data), options, replacement, preserveCase)
	if err != nil || changes.count == 0 {
		return 0, err
	}
	if _, err = writeRemoteWorkingFile(client, root, cleaned, []byte(changes.text), stamp, true); err != nil {
		return 0, err
	}
	return changes.count, nil
}
