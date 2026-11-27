package app

import (
	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// codeBlameSnapshot is one immutable step in blame navigation. Working-tree
// snapshots have an empty revision; their content remains local and is never
// restored by writing the file.
type codeBlameSnapshot struct {
	revision string
	path     string
	content  string
	lines    []repository.BlameLine
	position editor.Pos
	scrollY  float64
}

func currentCodeBlamePath(document *codeDocument) string {
	if document != nil && document.blameRevisionPath != "" {
		return document.blameRevisionPath
	}
	if document == nil {
		return ""
	}
	return document.path
}

func captureCodeBlameSnapshot(document *codeDocument) codeBlameSnapshot {
	return codeBlameSnapshot{
		revision: document.blameRevision,
		path:     currentCodeBlamePath(document),
		content:  document.state.Buffer.String(),
		lines:    document.blameLines,
		position: document.state.CursorPos(),
		scrollY:  document.state.ScrollY(),
	}
}

// installCodeBlameSnapshot changes only the editor's in-memory, read-only view.
// The document keeps its working-tree identity in a.code.documents and tabs.
func (a *app) installCodeBlameSnapshot(document *codeDocument, snapshot codeBlameSnapshot) {
	if document == nil || document.state == nil || document.state.Buffer == nil {
		return
	}
	a.closeCodeBlameDetailsFor(document)
	document.blameNavigationToken++
	document.blameToken++
	document.blameNavigationLoading = false
	document.blameNavigationError = ""
	document.blameRevision = snapshot.revision
	document.blameRevisionPath = snapshot.path

	state := document.state
	state.ReadOnly = snapshot.revision != ""
	state.Buffer.Reset(snapshot.content)
	state.Buffer.SetLanguage(editor.LanguageFor(snapshot.path))
	document.blameLines = snapshot.lines
	document.blameLoaded = true
	document.blameLoading = false
	document.blameError = ""
	document.blameVersion = state.Buffer.Version()
	document.blameInlays = nil
	document.blameOffsets = nil
	document.blameGutterOffsets = nil
	document.blameAnnotationSet = false
	state.LineAnnotationWidth = 0
	state.SetLineAnnotations(nil)
	state.GoTo(snapshot.position)
	state.SetScrollY(snapshot.scrollY)
	a.syncCodeBlameActiveLine(document)
	a.syncCodeBlameGutter(document)
}

// openPreviousCodeBlame follows Git's own pre-change coordinates. A commit with
// no previous header is the file's initial addition and offers no further step.
func (a *app) openPreviousCodeBlame(document *codeDocument, line int) {
	defer a.guardCodeBlame(document)
	if !codeBlameAvailable(document) || !document.blameEnabled || !document.blameLoaded ||
		document.blameNavigationLoading || line < 0 || line >= len(document.blameLines) || a.session == nil {
		return
	}
	blame := document.blameLines[line]
	if !committedBlameHash(blame.Hash) || blame.PreviousHash == "" || document.state.Buffer.Modified() {
		return
	}
	path := blame.PreviousPath
	if path == "" {
		path = currentCodeBlamePath(document)
	}
	revision := blame.PreviousHash
	current := captureCodeBlameSnapshot(document)
	document.blameNewer = append(document.blameNewer, current)
	document.blameNavigationToken++
	token := document.blameNavigationToken
	document.blameToken++
	blameToken := document.blameToken
	document.blameNavigationLoading = true
	document.blameNavigationError = ""
	a.closeCodeBlameDetailsFor(document)
	a.syncCodeBlameGutter(document)

	session := a.session
	repositoryGeneration := a.generation
	documentGeneration := document.generation
	state := document.state
	bufferVersion := state.Buffer.Version()
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		revisionBlame, err := requireHistorySession(session).BlameFileRevision(ctx, revision, path)
		a.updateChanged(func() bool {
			defer a.guardCodeBlame(document)
			if repositoryGeneration != a.generation || documentGeneration != document.generation ||
				token != document.blameNavigationToken || blameToken != document.blameToken ||
				document.state != state || a.code.documents[document.path] != document {
				return false
			}
			document.blameNavigationLoading = false
			if len(document.blameNewer) > 0 {
				document.blameNewer = document.blameNewer[:len(document.blameNewer)-1]
			}
			if err != nil || state.Buffer.Version() != bufferVersion {
				if err != nil {
					document.blameNavigationError = safeError(err)
				}
				a.syncCodeBlameGutter(document)
				return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
			}
			document.blameNewer = append(document.blameNewer, current)
			a.installCodeBlameSnapshot(document, codeBlameSnapshot{
				revision: revision,
				path:     path,
				content:  revisionBlame.Content,
				lines:    revisionBlame.Lines,
				position: editor.Pos{Line: min(line, max(0, len(revisionBlame.Lines)-1))},
			})
			return a.workspaceTab == sectionCode && a.activeCodeDocument() == document
		})
	}()
}

// openNewerCodeBlame cancels a pending older read, or restores the next newer
// snapshot. No repository write or checkout is involved in either direction.
func (a *app) openNewerCodeBlame(document *codeDocument) {
	if document == nil || len(document.blameNewer) == 0 {
		return
	}
	last := len(document.blameNewer) - 1
	snapshot := document.blameNewer[last]
	document.blameNewer = document.blameNewer[:last]
	document.blameNavigationToken++
	document.blameToken++
	if document.blameNavigationLoading {
		document.blameNavigationLoading = false
		document.blameNavigationError = ""
		a.syncCodeBlameGutter(document)
		return
	}
	a.installCodeBlameSnapshot(document, snapshot)
}

// restoreWorkingCodeBlame leaves history before a working-tree reload or
// mutation refresh. The first saved snapshot is always the working tree.
func (a *app) restoreWorkingCodeBlame(document *codeDocument) {
	if document == nil || (document.blameRevision == "" && len(document.blameNewer) == 0) {
		return
	}
	if len(document.blameNewer) > 0 {
		snapshot := document.blameNewer[0]
		document.blameNewer = nil
		a.installCodeBlameSnapshot(document, snapshot)
		return
	}
	// Every historical transition saves the working snapshot before starting.
	// Reaching this fallback means navigation was already invalidated elsewhere;
	// clear its identity rather than fabricating replacement file content.
	document.blameRevision = ""
	document.blameRevisionPath = ""
	document.blameNavigationLoading = false
	document.blameNavigationToken++
	document.blameToken++
	document.state.ReadOnly = false
}

func (a *app) codeBlameNavigation(c *ui.Context, document *codeDocument) {
	if document == nil || document.blameRevision == "" {
		return
	}
	theme := c.Theme()
	button := compactIconButton(ui.ButtonBase(c.Key("newer-code-blame")).Radius(theme.Radius))
	button.Label(tr("Newer blame")).Tooltip(tr("Return to the newer version")).
		Background(theme.Accent.Alpha(0.20)).Border(1, theme.Accent.Alpha(0.55))
	if button.Hovered() {
		button.Background(theme.Accent.Alpha(0.32))
	}
	button.Children(func() {
		ui.Icon(c, backIcon).FontSize(16).TextColor(theme.Accent)
	})
	if button.Clicked() {
		a.openNewerCodeBlame(document)
	}
	ui.Badge(c, shortBlameHash(document.blameRevision)).Tooltip(currentCodeBlamePath(document) + " @ " + document.blameRevision)
}
