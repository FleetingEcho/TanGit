package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

const codeBlameHoverDelay = time.Second

// codeBlameHoverState owns the asynchronous details behind MyGo's native
// editor hover. The hover itself remains editor-owned, so moving into the card
// keeps it open and pressing a button never turns it into a modal dialog.
type codeBlameHoverState struct {
	open      bool
	source    *codeDocument
	path      string
	line      int
	blame     repository.BlameLine
	commit    repository.Commit
	loading   bool
	loadError string
	token     uint64
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
	document.blameAnnotationSet = false
	if document.state != nil {
		document.state.SetInlays(nil)
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
		document.state.SetInlays(nil)
		return
	}
	blame := document.blameLines[line]
	if !committedBlameHash(blame.Hash) && !uncommittedBlameHash(blame.Hash) {
		document.state.SetInlays(nil)
		return
	}
	offset := document.state.Buffer.Snapshot().LineEnd(line)
	document.blameInlays = []editor.Inlay{{Offset: offset, Text: formatCodeBlame(blame), Trailing: true, Ellipsis: "…"}}
	document.blameOffsets = map[int]int{offset: line}
	document.blameAnnotationSet = true
	document.state.SetInlays(document.blameInlays)
}

func (a *app) toggleCodeBlame(document *codeDocument) {
	if !codeBlameAvailable(document) {
		return
	}
	a.closeCodeBlameDetailsFor(document)
	if document.blameEnabled {
		document.blameEnabled = false
		document.state.HoverDelay = 0
		invalidateCodeBlame(document)
		return
	}
	document.blameEnabled = true
	document.state.HoverDelay = codeBlameHoverDelay
	a.requestCodeBlame(document)
}

func (a *app) requestCodeBlame(document *codeDocument) {
	if !codeBlameAvailable(document) || !document.blameEnabled {
		return
	}
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

	go func() {
		defer a.recoverBackground()
		lines, err := session.BlameWorkingTreeFile(context.Background(), path)
		a.update(func() {
			if repositoryGeneration != a.generation || documentGeneration != document.generation ||
				token != document.blameToken || !document.blameEnabled || document.state != state ||
				a.code.documents[path] != document || state.Buffer.Version() != version {
				return
			}
			document.blameLoading = false
			if state.Buffer.Modified() {
				return
			}
			if err != nil {
				document.blameEnabled = false
				document.blameError = safeError(err)
				state.HoverDelay = 0
				clearCodeBlameAnnotations(document)
				return
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

func (a *app) configureCodeBlame(document *codeDocument) {
	if document == nil || document.state == nil {
		return
	}
	previous := document.state.OnHover
	document.state.OnHover = func(request editor.HoverRequest) {
		line, ok := codeBlameAt(document, request)
		if !ok {
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
	line, ok := document.blameOffsets[request.Offset]
	return line, ok
}

func (a *app) openCodeBlameDetails(document *codeDocument, line int) {
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
	go func() {
		defer a.recoverBackground()
		commits, err := session.SearchCommits(context.Background(), repository.CommitFilter{Query: blame.Hash, Limit: 1})
		if err == nil && (len(commits) != 1 || commits[0].Hash != blame.Hash) {
			err = fmt.Errorf("commit %s was not found", strings.TrimSpace(shortBlameHash(blame.Hash)))
		}
		a.update(func() {
			if repositoryGeneration != a.generation || token != hover.token || !hover.open || hover.blame.Hash != blame.Hash ||
				document.generation != documentGeneration || document.blameToken != blameToken ||
				document.path != sourcePath || a.code.documents[sourcePath] != document ||
				!document.blameEnabled || document.state == nil || document.state.Buffer.Version() != bufferVersion {
				return
			}
			hover.loading = false
			if err != nil {
				hover.loadError = safeError(err)
				return
			}
			hover.commit = commits[0]
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
	a.runCommitFilter(sectionHistory, hash)
}
