package app

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/egoist/mygo/ui/editor"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const (
	codeBlameHoverDelay  = time.Second
	codeBlameGutterWidth = float32(400)
)

// codeBlameHoverState owns the asynchronous details behind MyGo's native
// editor hover. The hover itself remains editor-owned, so moving into the card
// keeps it open and pressing a button never turns it into a modal dialog.
type codeBlameHoverState struct {
	open           bool
	source         *codeDocument
	path           string
	line           int
	blame          repository.BlameLine
	commit         repository.Commit
	description    string
	files          []repository.CommitFile
	filesLoaded    bool
	filesCollapsed bool
	infoExpanded   bool
	loading        bool
	loadError      string
	token          uint64
}

// historyDiffTarget survives the asynchronous History, file-list and
// diff reads needed to reveal one blamed source location.
type historyDiffTarget struct {
	change *repository.HistoryChangeQuery
	hash   string
	path   string
	line   int
}

func codeBlameAvailable(document *codeDocument) bool {
	return document != nil && !document.readOnly && !document.binary &&
		document.loadError == "" && document.state != nil && document.state.Buffer != nil
}

func clearCodeBlameAnnotations(document *codeDocument) {
	document.blameLoading = false
	document.blameLoaded = false
	document.blameVersion = 0
	document.blameLines = nil
	document.blameInlays = nil
	document.blameOffsets = nil
	document.blameGutterOffsets = nil
	document.blameAnnotationSet = false
	if document.state != nil {
		document.state.LineAnnotationWidth = 0
		document.state.SetLineAnnotations(nil)
		syncCodeDocumentInlays(document)
	}
}

func invalidateCodeBlame(document *codeDocument) {
	if document == nil {
		return
	}
	document.blameToken++
	document.blameError = ""
	clearCodeBlameAnnotations(document)
}

func (a *app) syncCodeBlame(document *codeDocument) {
	defer a.guardCodeBlame(document)
	if !codeBlameAvailable(document) || !document.blameEnabled {
		return
	}
	version := document.state.Buffer.Version()
	if document.state.Buffer.Modified() ||
		((document.blameLoaded || document.blameLoading) && version != document.blameVersion) {
		a.closeCodeBlameDetailsFor(document)
		invalidateCodeBlame(document)
		return
	}
	if !document.blameLoaded && !document.blameLoading && document.blameError == "" {
		// Undo can restore the saved buffer after an edit invalidated blame.
		// Reload it then, just as after the document's initial open or a save.
		a.requestCodeBlame(document)
		return
	}
	a.syncCodeBlameActiveLine(document)
}

// syncCodeBlameActiveLine renders blame only beside the caret's current line.
// Data for the other lines remains cached, so cursor movement is local and does
// not rerun Git or fetch commit details.
func (a *app) syncCodeBlameActiveLine(document *codeDocument) {
	defer a.guardCodeBlame(document)
	if !codeBlameAvailable(document) || !document.blameEnabled || !document.blameLoaded ||
		document.state.Buffer.Modified() {
		return
	}
	line := document.state.CursorPos().Line
	if document.blameAnnotationSet && document.blameActiveLine == line {
		return
	}
	if a.codeBlameHover.open && a.codeBlameHover.source == document && a.codeBlameHover.line != line+1 {
		a.closeCodeBlameDetails()
	}
	document.blameActiveLine = line
	document.blameInlays = nil
	document.blameOffsets = nil
	document.blameAnnotationSet = false
	if line < 0 || line >= len(document.blameLines) {
		syncCodeDocumentInlays(document)
		return
	}
	blame := document.blameLines[line]
	if !committedBlameHash(blame.Hash) && !uncommittedBlameHash(blame.Hash) {
		syncCodeDocumentInlays(document)
		return
	}
	offset := document.state.Buffer.Snapshot().LineEnd(line)
	document.blameInlays = []editor.Inlay{{Offset: offset, Text: formatCodeBlame(blame), Trailing: true, Ellipsis: "…"}}
	document.blameOffsets = map[int]int{offset: line}
	document.blameAnnotationSet = true
	syncCodeDocumentInlays(document)
}

func (a *app) codeBlameAnnotationAvatar(document *codeDocument, blame repository.BlameLine) *editor.LineAnnotationAvatar {
	if document == nil || !committedBlameHash(blame.Hash) {
		return nil
	}
	commit := repository.Commit{Hash: blame.Hash, AuthorName: blame.Author, AuthorEmail: blame.AuthorEmail}
	return &editor.LineAnnotationAvatar{
		Initial:    authorInitial(blame.Author, blame.AuthorEmail),
		Background: avatarColor(blame.Author, blame.AuthorEmail),
		Bitmap: func() *ui.Bitmap {
			return a.authorAvatarBitmap(commit, document.blameAvatarInvalidate)
		},
	}
}

// syncCodeBlameGutter shows a compact commit summary at the first line of
// every contiguous blamed range. Continuation lines stay blank without
// allocating invisible gutter entries; selecting any line still reveals its
// own trailing annotation and hover target.
func (a *app) syncCodeBlameGutter(document *codeDocument) {
	defer a.guardCodeBlame(document)
	if document == nil || document.state == nil {
		return
	}
	document.blameGutterOffsets = nil
	if !codeBlameAvailable(document) || !document.blameEnabled || !document.blameFull ||
		!document.blameLoaded || document.state.Buffer.Modified() {
		document.state.LineAnnotationWidth = 0
		document.state.SetLineAnnotations(nil)
		return
	}
	snapshot := document.state.Buffer.Snapshot()
	lineCount := min(len(document.blameLines), snapshot.LineCount())
	annotations := make([]editor.LineAnnotation, 0)
	offsets := make(map[int]int)
	previousHash := ""
	previousSlot := -1
	for line := 0; line < lineCount; line++ {
		blame := document.blameLines[line]
		if !committedBlameHash(blame.Hash) && !uncommittedBlameHash(blame.Hash) {
			previousHash = ""
			continue
		}
		if line != 0 && blame.Hash == previousHash {
			continue
		}
		offsets[snapshot.LineEnd(line)] = line
		line := line
		annotation := editor.LineAnnotation{
			Line:     line,
			LastLine: codeBlameRangeLastLine(document, line, blame.Hash),
			Border:   codeBlameRangeColor(blame.Hash, &previousSlot),
			Avatar:   a.codeBlameAnnotationAvatar(document, blame),
		}
		annotation.Date, annotation.Text = formatCodeBlameGutter(blame)
		// Details open from the info button; hovering or clicking the gutter's
		// text does nothing, so moving across it never pops a card up.
		annotation.DetailsClick = func() { a.openCodeBlameDetails(document, line) }
		annotation.DetailsTooltip = tr("Show commit details")
		if committedBlameHash(blame.Hash) && blame.PreviousHash != "" && !document.blameNavigationLoading {
			annotation.ActionClick = func() { a.openPreviousCodeBlame(document, line) }
			annotation.ActionTooltip = tr("View blame before this change")
		}
		annotations = append(annotations, annotation)
		previousHash = blame.Hash
	}
	document.blameGutterOffsets = offsets
	width := document.blameGutterWidth
	if width <= 0 {
		width = codeBlameGutterWidth
	}
	document.state.LineAnnotationWidth = width
	document.state.SetLineAnnotations(annotations)
}

// codeBlameRangeColor colors a blame range by its commit, not its author, so
// two ranges stay apart even when one person wrote both. The color follows the
// commit hash and is moved on when it would repeat the range just above.
func codeBlameRangeColor(hash string, previousSlot *int) ui.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(hash))
	slot := int(h.Sum32() % uint32(len(accentNames)))
	if slot == *previousSlot {
		slot = (slot + 1) % len(accentNames)
	}
	*previousSlot = slot
	return accentChoices[accentNames[slot]]
}

func codeBlameRangeLastLine(document *codeDocument, line int, hash string) int {
	lastLine := line
	for lastLine+1 < len(document.blameLines) && document.blameLines[lastLine+1].Hash == hash {
		lastLine++
	}
	return lastLine
}

func syncCodeDocumentInlays(document *codeDocument) {
	if document == nil || document.state == nil {
		return
	}
	inlays := make([]editor.Inlay, 0, len(document.languageInlays)+len(document.blameInlays))
	inlays = append(inlays, document.languageInlays...)
	inlays = append(inlays, document.blameInlays...)
	document.state.SetInlays(inlays)
}

func (a *app) toggleCodeBlame(document *codeDocument) {
	defer a.guardCodeBlame(document)
	if !codeBlameAvailable(document) {
		return
	}
	a.closeCodeBlameDetailsFor(document)
	document.blameEnabled = true
	document.state.HoverDelay = codeBlameHoverDelay
	if document.blameError != "" {
		a.requestCodeBlame(document)
		return
	}
	document.blameFull = !document.blameFull
	restoreWorking := !document.blameFull &&
		(document.blameRevision != "" || document.blameNavigationLoading)
	if restoreWorking {
		a.restoreWorkingCodeBlame(document)
	}
	if restoreWorking {
		// Historical lines belong to the read-only parent snapshot, not to the
		// restored working copy. Re-blame the final code instead of reusing them.
		a.requestCodeBlame(document)
		return
	}
	if !document.blameLoaded && !document.blameLoading {
		a.requestCodeBlame(document)
		return
	}
	a.syncCodeBlameGutter(document)
	a.syncCodeBlameActiveLine(document)
}

// openGitFileBlameInBuiltInEditor opens the file in the editor with the
// full-file blame gutter showing, once the file has loaded.
func (a *app) openGitFileBlameInBuiltInEditor(root, relative string) {
	a.openGitFileInBuiltInEditor(root, relative)
	document := a.code.documents[relative]
	if document == nil || a.code.active != relative {
		return
	}
	if document.loaded {
		a.showFullCodeBlame(document)
		return
	}
	document.pendingFullBlame = true
}

func (a *app) applyPendingFullBlame(document *codeDocument) {
	if document == nil || !document.pendingFullBlame {
		return
	}
	document.pendingFullBlame = false
	a.showFullCodeBlame(document)
}

// showFullCodeBlame turns the full-file gutter on, never off, so choosing Blame
// twice leaves it showing.
func (a *app) showFullCodeBlame(document *codeDocument) {
	if codeBlameAvailable(document) && !document.blameFull {
		a.toggleCodeBlame(document)
	}
}

func (a *app) requestCodeBlame(document *codeDocument) {
	defer a.guardCodeBlame(document)
	if !codeBlameAvailable(document) || !document.blameEnabled {
		return
	}
	a.restoreWorkingCodeBlame(document)
	a.closeCodeBlameDetailsFor(document)
	invalidateCodeBlame(document)
	document.state.HoverDelay = codeBlameHoverDelay
	if document.state.Buffer.Modified() || a.session == nil ||
		a.code.documents[document.path] != document {
		return
	}

	session := a.session
	repositoryGeneration := a.generation
	documentGeneration := document.generation
	path := document.path
	state := document.state
	snapshot := state.Buffer.Snapshot()
	version := snapshot.Version()
	document.blameToken++
	token := document.blameToken
	document.blameVersion = version
	document.blameLoading = true

	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		lines, err := requireHistorySession(session).BlameWorkingTreeFile(ctx, path)
		a.updateChanged(func() bool {
			defer a.guardCodeBlame(document)
			if repositoryGeneration != a.generation || documentGeneration != document.generation ||
				token != document.blameToken || !document.blameEnabled || document.state != state ||
				a.code.documents[path] != document || state.Buffer.Version() != version {
				return false
			}
			document.blameLoading = false
			if state.Buffer.Modified() {
				return false
			}
			if err != nil {
				document.blameEnabled = false
				document.blameError = safeError(err)
				state.HoverDelay = 0
				clearCodeBlameAnnotations(document)
				return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
			}

			valid := make([]bool, len(lines))
			for index := 0; index < len(lines) && index < snapshot.LineCount(); index++ {
				// Buffer and Git normalize line endings differently. Validating the
				// source content keeps annotations from binding to a replaced line.
				valid[index] = strings.TrimSuffix(lines[index].Text, "\r") == snapshot.Line(index)
			}
			for index := range valid {
				if !valid[index] {
					lines[index].Hash = ""
				}
			}
			document.blameLines = lines
			document.blameVersion = version
			document.blameLoaded = true
			document.blameError = ""
			a.syncCodeBlameActiveLine(document)
			a.syncCodeBlameGutter(document)
			return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
		})
	}()
}

func committedBlameHash(hash string) bool {
	return hash != "" && strings.Trim(hash, "0") != ""
}

func uncommittedBlameHash(hash string) bool {
	return hash != "" && !committedBlameHash(hash)
}

func formatCodeBlame(line repository.BlameLine) string {
	if uncommittedBlameHash(line.Hash) {
		return "  " + tr("Uncommitted changes")
	}
	author := line.Author
	if line.Current {
		author = tr("You")
	}
	when := codeBlameRelativeTime(line.UnixTimestamp)
	if when == "" {
		when = line.Date
	}
	subject := line.Subject
	if subject == "" {
		subject = shortBlameHash(line.Hash)
	}
	return fmt.Sprintf("  %s, %s • %s", author, when, subject)
}

func formatCodeBlameGutter(line repository.BlameLine) (string, string) {
	if uncommittedBlameHash(line.Hash) {
		return "", tr("Uncommitted changes")
	}
	author := line.Author
	if author == "" {
		author = line.AuthorEmail
	}
	if line.Current {
		author = tr("You")
	}
	if author == "" {
		author = shortBlameHash(line.Hash)
	}
	subject := line.Subject
	if subject == "" {
		subject = shortBlameHash(line.Hash)
	}
	return formatCodeBlameGutterDate(line), author + " • " + subject
}

func formatCodeBlameGutterDate(line repository.BlameLine) string {
	// Prefer the author timezone already recorded by Git, so a commit never
	// moves to an adjacent day because the developer viewing it is elsewhere.
	if date, err := time.Parse("2006-01-02", line.Date); err == nil {
		return date.Format("Jan 2,2006")
	}
	if line.UnixTimestamp > 0 {
		return time.Unix(line.UnixTimestamp, 0).UTC().Format("Jan 2,2006")
	}
	return line.Date
}

func codeBlameRelativeTime(unix int64) string {
	if unix <= 0 {
		return ""
	}
	age := int64(0)
	if now := timeNow(); now > unix {
		age = now - unix
	}
	unit := func(count int64, singular, plural string) string {
		word := plural
		if count == 1 {
			word = singular
		}
		return trf("%d %s ago", count, tr(word))
	}
	switch {
	case age < 60:
		return tr("just now")
	case age < 3600:
		return unit(age/60, "minute", "minutes")
	case age < 86400:
		return unit(age/3600, "hour", "hours")
	case age < 7*86400:
		return unit(age/86400, "day", "days")
	case age < 30*86400:
		return unit(age/(7*86400), "week", "weeks")
	case age < 365*86400:
		return unit(age/(30*86400), "month", "months")
	default:
		return unit(age/(365*86400), "year", "years")
	}
}

// guardCodeBlame is deferred by every blame entry point that runs on the UI
// thread. A fault there must never take the app down: it turns blame off for
// the document, closes its card, and says so.
func (a *app) guardCodeBlame(document *codeDocument) {
	r := recover()
	if r == nil {
		return
	}
	a.disableCodeBlameAfterFault(document, describeFault(r))
}

func (a *app) disableCodeBlameAfterFault(document *codeDocument, reason string) {
	defer func() { _ = recover() }()
	a.codeBlameHover = codeBlameHoverState{token: a.codeBlameHover.token + 1}
	if document != nil {
		document.blameEnabled = false
		document.blameFull = false
		document.blameToken++
		document.blameError = reason
		document.blameNavigationLoading = false
		document.blameLoading = false
		document.blameLoaded = false
		document.blameLines = nil
		document.blameInlays = nil
		document.blameOffsets = nil
		document.blameGutterOffsets = nil
		document.blameAnnotationSet = false
		if document.state != nil {
			document.state.HoverDelay = 0
			func() {
				defer func() { _ = recover() }()
				document.state.ShowHover(-1, -1, "", nil)
			}()
			document.state.LineAnnotationWidth = 0
			document.state.SetLineAnnotations(nil)
			syncCodeDocumentInlays(document)
		}
	}
	a.notify("error", tr("Git blame failed"), reason)
}

func (a *app) configureCodeBlame(document *codeDocument) {
	if document == nil || document.state == nil {
		return
	}
	document.state.OnLineAnnotationResize = func(width float32) {
		document.blameGutterWidth = width
		document.state.LineAnnotationWidth = width
	}
	previous := document.state.OnHover
	previousRender := document.state.RenderHover
	document.state.RenderHover = func(c *ui.Context, panel ui.Element, hover *editor.HoverCard) {
		if a.codeBlameHover.open && a.codeBlameHover.source == document {
			a.renderCodeBlameHover(c, panel, hover)
			return
		}
		if previousRender != nil {
			previousRender(c, panel, hover)
		} else {
			editor.DefaultHover(c, panel, hover)
		}
	}
	document.state.OnHover = func(request editor.HoverRequest) {
		line, ok := codeBlameAt(document, request)
		if !ok {
			a.closeCodeBlameDetailsFor(document)
			if previous != nil {
				previous(request)
			}
			return
		}
		if line >= 0 && line < len(document.blameLines) {
			a.openCodeBlameDetails(document, line)
		}
	}
}

func codeBlameAt(document *codeDocument, request editor.HoverRequest) (int, bool) {
	if !codeBlameAvailable(document) || !document.blameEnabled || !document.blameLoaded ||
		document.state.Buffer.Modified() || request.Version != document.blameVersion ||
		document.state.Buffer.Version() != document.blameVersion {
		return 0, false
	}
	// Only the editor's own trailing annotation opens its card on hover. The
	// full-file gutter has its own button.
	line, ok := document.blameOffsets[request.Offset]
	return line, ok
}

func (a *app) openCodeBlameDetails(document *codeDocument, line int) {
	defer a.guardCodeBlame(document)
	if !codeBlameAvailable(document) || line < 0 || line >= len(document.blameLines) {
		return
	}
	blame := document.blameLines[line]
	// Each native card belongs to exactly one source document. Dismiss a
	// previous document's card rather than letting it render new global data.
	a.closeCodeBlameDetails()
	document.hoverSeq++ // reject language-server hover responses already in flight
	hover := &a.codeBlameHover
	hover.token++
	token := hover.token
	hover.open = true
	hover.source = document
	hover.path = document.path
	hover.line = line + 1
	hover.blame = blame
	hover.commit = repository.Commit{}
	hover.description = ""
	hover.files = nil
	hover.filesLoaded = false
	hover.filesCollapsed = false
	hover.infoExpanded = true
	hover.loading = false
	hover.loadError = ""
	offset := document.state.Buffer.Snapshot().LineEnd(line)
	document.state.ShowHover(offset, offset, "", func(c *ui.Context) {
		if a.codeBlameHover.open && a.codeBlameHover.source == document {
			a.codeBlameDetailsCard(c)
		}
	})
	if !committedBlameHash(blame.Hash) || a.session == nil {
		return
	}

	session := a.session
	repositoryGeneration := a.generation
	documentGeneration := document.generation
	blameToken := document.blameToken
	sourcePath := document.path
	bufferVersion := document.state.Buffer.Version()
	hover.loading = true
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		commits, err := requireCommitSearchSession(session).SearchCommits(ctx, repository.CommitFilter{Query: blame.Hash, Limit: 1})
		if err == nil && (len(commits) != 1 || commits[0].Hash != blame.Hash) {
			err = fmt.Errorf("commit %s was not found", strings.TrimSpace(shortBlameHash(blame.Hash)))
		}
		var message repository.CommitMessage
		var files []repository.CommitFile
		var messageErr, filesErr error
		if err == nil {
			message, messageErr = requireHistorySession(session).CommitMessage(ctx, commits[0])
			files, filesErr = requireHistorySession(session).CommitFiles(ctx, commits[0])
		}
		a.updateChanged(func() bool {
			defer a.guardCodeBlame(document)
			if repositoryGeneration != a.generation || token != hover.token || !hover.open || hover.blame.Hash != blame.Hash ||
				document.generation != documentGeneration || document.blameToken != blameToken ||
				document.path != sourcePath || a.code.documents[sourcePath] != document ||
				!document.blameEnabled || document.state == nil || document.state.Buffer.Version() != bufferVersion {
				return false
			}
			hover.loading = false
			if err != nil {
				hover.loadError = safeError(err)
			} else {
				hover.commit = commits[0]
				hover.description = message.Description
				hover.files = files
				hover.filesLoaded = filesErr == nil
				var failures []string
				for _, detailErr := range []error{messageErr, filesErr} {
					if detailErr != nil {
						failures = append(failures, safeError(detailErr))
					}
				}
				hover.loadError = strings.Join(failures, "\n")
			}
			return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
		})
	}()
}

func (a *app) closeCodeBlameDetails() {
	hover := &a.codeBlameHover
	if hover.source != nil && hover.source.state != nil {
		// An invalid anchor removes the native hover without exposing another
		// API or interfering with the editor's own pointer lifecycle.
		hover.source.state.ShowHover(-1, -1, "", nil)
	}
	hover.token++
	hover.open = false
	hover.source = nil
	hover.loading = false
}

func (a *app) closeCodeBlameDetailsFor(document *codeDocument) {
	if document != nil && a.codeBlameHover.open && a.codeBlameHover.source == document {
		a.closeCodeBlameDetails()
	}
}

func (a *app) invalidateCodeBlameForRepositoryMutation() {
	a.closeCodeBlameDetails()
	for _, document := range a.code.documents {
		invalidateCodeBlame(document)
	}
}

func (a *app) refreshCodeBlameAfterRepositoryMutation() {
	a.closeCodeBlameDetails()
	for _, document := range a.code.documents {
		if document.blameEnabled {
			a.requestCodeBlame(document)
		} else {
			invalidateCodeBlame(document)
		}
	}
}

func (a *app) openCodeBlameCommitInHistory() {
	hover := &a.codeBlameHover
	if !hover.open || !committedBlameHash(hover.blame.Hash) || !a.sectionVisible(sectionHistory) {
		return
	}
	hash := hover.blame.Hash
	path := hover.blame.CommitPath
	if path == "" {
		path = hover.path
	}
	line := hover.blame.CommitLine
	if line < 1 {
		line = hover.line
	}
	a.closeCodeBlameDetails()
	query := a.commitQueryFor(sectionHistory)
	query.applied = repository.CommitFilter{Query: hash}
	query.draft = query.applied
	query.matches = nil
	query.err = ""
	query.limit = repository.SnapshotCommitLimit
	query.more = false
	query.defaulted = false
	a.setWorkspaceTab(sectionHistory)
	a.selectedCommit = -1
	a.resetCommitDetails()
	a.historyDiffTarget = &historyDiffTarget{hash: hash, path: path, line: line}
	a.runCommitFilter(sectionHistory, hash)
}
