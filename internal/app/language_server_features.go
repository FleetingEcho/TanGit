package app

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type languageServerPosition struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type languageServerRange struct {
	Start languageServerPosition `json:"start"`
	End   languageServerPosition `json:"end"`
}

type languageServerLocation struct {
	URI                  string              `json:"uri"`
	Range                languageServerRange `json:"range"`
	TargetURI            string              `json:"targetUri"`
	TargetRange          languageServerRange `json:"targetRange"`
	TargetSelectionRange languageServerRange `json:"targetSelectionRange"`
}

type languageServerTextDocument struct {
	URI string `json:"uri"`
}

type languageServerTextPosition struct {
	TextDocument languageServerTextDocument `json:"textDocument"`
	Position     languageServerPosition     `json:"position"`
}

func (a *app) configureGoLanguageDocument(document *codeDocument) {
	if document == nil || document.state == nil ||
		(!strings.EqualFold(filepath.Ext(document.path), ".go") && !a.isAdditionalLanguageServerPath(document.path)) {
		return
	}
	document.state.OnHover = func(request editor.HoverRequest) {
		if strings.EqualFold(filepath.Ext(document.path), ".go") {
			a.requestGoLanguageHover(document, request)
		} else {
			a.requestAdditionalLanguageHover(document, request)
		}
	}
	document.state.RenderHover = func(c *ui.Context, panel ui.Element, hover *editor.HoverCard) {
		renderGoLanguageHover(c, panel, hover, document)
	}
	document.state.OnDefinition = func(offset int) {
		if strings.EqualFold(filepath.Ext(document.path), ".go") {
			a.requestGoLanguageDefinition(document, offset)
		} else {
			a.requestAdditionalLanguageDefinition(document, offset)
		}
	}
	document.state.OnCompletion = func(request editor.CompletionRequest) {
		if strings.EqualFold(filepath.Ext(document.path), ".go") {
			a.requestGoLanguageCompletions(document, request)
		} else {
			a.requestAdditionalLanguageCompletions(document, request)
		}
	}
	a.offerLanguageServerInstall(document.path)
}

func languageServerPositionAt(text string, offset int) languageServerPosition {
	offset = max(0, min(len(text), offset))
	lineStart := strings.LastIndexByte(text[:offset], '\n') + 1
	return languageServerPosition{
		Line:      strings.Count(text[:lineStart], "\n"),
		Character: len(utf16.Encode([]rune(text[lineStart:offset]))),
	}
}

func languageServerOffsetAt(text string, position languageServerPosition) int {
	if position.Line < 0 || position.Character < 0 {
		return 0
	}
	start := 0
	for range position.Line {
		next := strings.IndexByte(text[start:], '\n')
		if next < 0 {
			return len(text)
		}
		start += next + 1
	}
	lineEnd := strings.IndexByte(text[start:], '\n')
	if lineEnd < 0 {
		lineEnd = len(text) - start
	}
	line := text[start : start+lineEnd]
	return start + languageServerUTF16ColumnToByte(line, position.Character)
}

func languageServerUTF16ColumnToByte(line string, character int) int {
	if character <= 0 {
		return 0
	}
	units, offset := 0, 0
	for offset < len(line) {
		r, size := utf8.DecodeRuneInString(line[offset:])
		width := 1
		if r > 0xffff {
			width = 2
		}
		if units+width > character {
			break
		}
		units += width
		offset += size
	}
	return offset
}

func languageServerWordRange(text string, offset int) (int, int) {
	offset = max(0, min(len(text), offset))
	start, end := offset, offset
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		start -= size
	}
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		end += size
	}
	return start, end
}

func (a *app) goLanguageRequest(document *codeDocument, requestSequence *uint64, offset int, method string,
	result func(json.RawMessage, error, string, uint64)) {
	connection := a.goLanguageServer
	if connection == nil || document == nil || document.state == nil || document.state.Buffer == nil ||
		a.code.documents[document.path] != document {
		return
	}
	a.syncGoLanguageServerDocument(document.path, document)
	text := document.state.Buffer.String()
	version := document.state.Buffer.Version()
	*requestSequence++
	sequence := *requestSequence
	params := languageServerTextPosition{
		TextDocument: languageServerTextDocument{URI: languageServerFileURI(a.workingFilePath(document.path))},
		Position:     languageServerPositionAt(text, offset),
	}
	connection.Request(method, params, func(raw json.RawMessage, err error) {
		a.update(func() {
			if a.goLanguageServer != connection || a.code.documents[document.path] != document ||
				document.state == nil || document.state.Buffer == nil ||
				*requestSequence != sequence || document.state.Buffer.Version() != version {
				return
			}
			if err != nil {
				a.reportGoLanguageServerError(err.Error())
			}
			result(raw, err, text, version)
		})
	})
}

func (a *app) requestGoLanguageHover(document *codeDocument, request editor.HoverRequest) {
	a.goLanguageRequest(document, &document.hoverSeq, request.Offset, "textDocument/hover", func(raw json.RawMessage, err error, text string, _ uint64) {
		if err != nil {
			return
		}
		content, hoverRange, ok := parseLanguageServerHover(raw)
		if !ok || strings.TrimSpace(content) == "" {
			return
		}
		start, end := languageServerWordRange(text, request.Offset)
		if hoverRange != nil {
			start = languageServerOffsetAt(text, hoverRange.Start)
			end = languageServerOffsetAt(text, hoverRange.End)
		}
		showLanguageServerHover(document, start, end, content)
	})
}

func languageServerHoverCodeBlock(content string) (string, *editor.Language, bool) {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) < 2 {
		return "", nil, false
	}
	opening := strings.TrimSpace(lines[0])
	if !strings.HasPrefix(opening, "```") || strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return "", nil, false
	}
	for _, line := range lines[1 : len(lines)-1] {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			return "", nil, false
		}
	}
	info := strings.Fields(strings.TrimSpace(strings.TrimPrefix(opening, "```")))
	if len(info) == 0 {
		return "", nil, false
	}
	language := editor.LanguageNamed(info[0])
	if language == nil {
		return "", nil, false
	}
	return strings.Join(lines[1:len(lines)-1], "\n"), language, true
}

func languageServerHoverWithoutFences(content string) string {
	lines := strings.Split(content, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func showLanguageServerHover(document *codeDocument, start, end int, content string) {
	language := editor.LanguageNamed("markdown")
	if code, codeLanguage, ok := languageServerHoverCodeBlock(content); ok {
		content, language = code, codeLanguage
	} else {
		// Mixed Markdown hovers are rendered as one buffer. Remove their fence
		// delimiters instead of exposing Markdown source such as ```xml while
		// retaining the code and prose inside the blocks.
		content = languageServerHoverWithoutFences(content)
	}
	buffer := editor.NewBuffer(content)
	buffer.SetLanguage(language)
	document.hoverState = &editor.State{
		Buffer: buffer, ReadOnly: true, FitContent: true, SoftWrap: true,
		LineNumbers: editor.LineNumbersOff, NoFolding: true, NoActiveLine: true, NoScrollbars: true,
		Label: "Hover details",
	}
	document.hoverScroll = ui.ScrollState{}
	document.state.ShowHover(start, end, content, nil)
}

func renderGoLanguageHover(c *ui.Context, panel ui.Element, hover *editor.HoverCard, document *codeDocument) {
	theme := c.Theme()
	codeTheme := hover.Theme()
	panel.Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
		Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.2)).Padding(theme.Space(2)).
		MaxWidth(theme.Space(120)).Margin(0, 0, theme.Space(1), 0).Selectable()
	panel.Children(func() {
		ui.Scroll(c).MaxHeight(theme.Space(60)).KeepScrollbars(true).TrackScroll(&document.hoverScroll).Children(func() {
			ui.Column(c).Gap(theme.Space(1.5)).Children(func() {
				for _, diagnostic := range hover.Diagnostics() {
					ui.Row(c).Gap(theme.Space(1.5)).Children(func() {
						ui.Box(c).Width(3).Radius(1.5).Background(codeTheme.SeverityColor(diagnostic.Severity)).AlignSelf(ui.Stretch)
						message := diagnostic.Message
						if diagnostic.Source != "" {
							message += " (" + diagnostic.Source + ")"
						}
						ui.Text(c, message).FontSize(theme.FontSize - 1).Shrink(1)
					})
				}
				if state := document.hoverState; state != nil && state.Buffer != nil && state.Buffer.String() == hover.Text() {
					editor.View(c, state).Width(theme.Space(110))
				} else if text := hover.Text(); text != "" {
					ui.Text(c, text).Font(hover.Font().Family).FontSize(hover.Font().Size).TextColor(codeTheme.Text)
				}
				hover.Content()
			})
		})
	})
}

func parseLanguageServerHover(raw json.RawMessage) (string, *languageServerRange, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, false
	}
	var hover struct {
		Contents json.RawMessage      `json:"contents"`
		Range    *languageServerRange `json:"range"`
	}
	if json.Unmarshal(raw, &hover) != nil {
		return "", nil, false
	}
	return languageServerMarkupText(hover.Contents), hover.Range, true
}

func languageServerMarkupText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var marked struct {
		Kind     string `json:"kind"`
		Language string `json:"language"`
		Value    string `json:"value"`
	}
	if json.Unmarshal(raw, &marked) == nil && marked.Value != "" {
		if marked.Language != "" {
			return "```" + marked.Language + "\n" + marked.Value + "\n```"
		}
		return marked.Value
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			if value := languageServerMarkupText(part); value != "" {
				texts = append(texts, value)
			}
		}
		return strings.Join(texts, "\n\n")
	}
	return ""
}

func (a *app) requestGoLanguageDefinition(document *codeDocument, offset int) {
	source := a.currentCodeNavigationLocation()
	a.goLanguageRequest(document, &document.definitionSeq, offset, "textDocument/definition", func(raw json.RawMessage, err error, _ string, _ uint64) {
		if err != nil {
			return
		}
		location, ok := firstLanguageServerLocation(raw)
		if !ok {
			return
		}
		a.openLanguageServerDefinition(source, location)
	})
}

func (a *app) openLanguageServerDefinition(source codeNavigationLocation, location languageServerLocation) bool {
	name, ok := languageServerFilePath(location.URI)
	if !ok {
		return false
	}
	if source.path != "" {
		a.code.back = append(a.code.back, source)
		a.code.forward = nil
	}
	if path, inside := languageServerPathWithinRepository(a.snapshot.Root, name); inside {
		a.openCodeLanguageServerLocation(path, location.Range.Start)
	} else {
		a.openExternalCodeLanguageServerLocation(name, location.Range.Start)
	}
	return true
}

func firstLanguageServerLocation(raw json.RawMessage) (languageServerLocation, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return languageServerLocation{}, false
	}
	var locations []languageServerLocation
	if raw[0] == '[' {
		if json.Unmarshal(raw, &locations) != nil || len(locations) == 0 {
			return languageServerLocation{}, false
		}
	} else {
		locations = make([]languageServerLocation, 1)
		if json.Unmarshal(raw, &locations[0]) != nil {
			return languageServerLocation{}, false
		}
	}
	location := locations[0]
	if location.TargetURI != "" {
		location.URI = location.TargetURI
		location.Range = location.TargetSelectionRange
		if location.Range == (languageServerRange{}) {
			location.Range = location.TargetRange
		}
	}
	return location, location.URI != ""
}

func languageServerRepositoryPath(root, uri string) (string, bool) {
	name, ok := languageServerFilePath(uri)
	if !ok {
		return "", false
	}
	return languageServerPathWithinRepository(root, name)
}

func languageServerFilePath(uri string) (string, bool) {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return "", false
	}
	name := filepath.FromSlash(parsed.Path)
	if runtime.GOOS == "windows" && len(name) >= 3 && (name[0] == '\\' || name[0] == '/') && name[2] == ':' {
		name = name[1:]
	}
	return filepath.Clean(name), filepath.IsAbs(name) || strings.HasPrefix(parsed.Path, "/")
}

func languageServerPathWithinRepository(root, name string) (string, bool) {
	rootSlash := strings.TrimRight(filepath.ToSlash(filepath.Clean(root)), "/")
	nameSlash := filepath.ToSlash(filepath.Clean(name))
	compareRoot, compareName := rootSlash, nameSlash
	if runtime.GOOS == "windows" {
		compareRoot, compareName = strings.ToLower(compareRoot), strings.ToLower(compareName)
	}
	if strings.HasPrefix(compareName, compareRoot+"/") {
		relative := strings.TrimPrefix(nameSlash[len(rootSlash):], "/")
		return relative, validCodeRelativePath(relative)
	}
	relative, err := filepath.Rel(root, name)
	if err != nil {
		return "", false
	}
	relative = filepath.ToSlash(relative)
	return relative, validCodeRelativePath(relative)
}

func (a *app) openCodeLanguageServerLocation(path string, position languageServerPosition) {
	a.openCodeLocation(path, 0, 0)
	document := a.code.documents[path]
	if document == nil {
		return
	}
	document.pendingLine = position.Line + 1
	document.pendingColumn = position.Character
	document.pendingUTF16 = true
	a.revealCodeLine(document)
}

func (a *app) openExternalCodeLanguageServerLocation(name string, position languageServerPosition) {
	name = filepath.Clean(name)
	if !filepath.IsAbs(name) || editor.LanguageFor(name) == nil {
		return
	}
	a.openCodeDocumentLocation(name, name, true, 0, 0, false)
	document := a.code.documents[name]
	if document == nil {
		return
	}
	document.pendingLine = position.Line + 1
	document.pendingColumn = position.Character
	document.pendingUTF16 = true
	a.revealCodeLine(document)
}

func (a *app) currentCodeNavigationLocation() codeNavigationLocation {
	document := a.activeCodeDocument()
	if document == nil || document.state == nil {
		return codeNavigationLocation{}
	}
	position := document.state.CursorPos()
	return codeNavigationLocation{path: document.path, line: position.Line + 1, column: position.Col}
}

func (a *app) navigateCodeHistory(back bool) {
	from, to := &a.code.forward, &a.code.back
	if back {
		from, to = &a.code.back, &a.code.forward
	}
	if len(*from) == 0 {
		return
	}
	current := a.currentCodeNavigationLocation()
	target := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	if current.path != "" {
		*to = append(*to, current)
	}
	if filepath.IsAbs(target.path) {
		a.openCodeDocumentLocation(target.path, target.path, true, target.line, target.column, false)
	} else {
		a.openCodeLocation(target.path, target.line, target.column)
	}
}

func (a *app) requestGoLanguageCompletions(document *codeDocument, request editor.CompletionRequest) {
	a.goLanguageRequest(document, &document.completionSeq, request.Offset, "textDocument/completion", func(raw json.RawMessage, err error, _ string, _ uint64) {
		if err != nil {
			return
		}
		items := parseLanguageServerCompletions(raw)
		if len(items) > 0 {
			document.state.ShowCompletions(max(0, request.Offset-len(request.Prefix)), items)
		}
	})
}

func parseLanguageServerCompletions(raw json.RawMessage) []editor.Completion {
	var values []struct {
		Label            string          `json:"label"`
		Detail           string          `json:"detail"`
		Kind             int             `json:"kind"`
		InsertText       string          `json:"insertText"`
		InsertTextFormat int             `json:"insertTextFormat"`
		FilterText       string          `json:"filterText"`
		Documentation    json.RawMessage `json:"documentation"`
		TextEdit         *struct {
			NewText string `json:"newText"`
		} `json:"textEdit"`
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '{' {
		var list struct {
			Items json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &list) != nil {
			return nil
		}
		raw = list.Items
	}
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	items := make([]editor.Completion, 0, len(values))
	for _, value := range values {
		item := editor.Completion{
			Label: value.Label, Detail: value.Detail, Insert: value.InsertText,
			FilterText: value.FilterText, Documentation: languageServerMarkupText(value.Documentation),
			Kind: languageServerCompletionKind(value.Kind),
		}
		if value.TextEdit != nil {
			item.Insert = value.TextEdit.NewText
		}
		if value.InsertTextFormat == 2 {
			if snippet, err := editor.ParseSnippet(item.Insert); err == nil {
				item.Snippet = snippet
				item.Kind = editor.CompletionSnippet
			}
		}
		items = append(items, item)
	}
	return items
}

func languageServerCompletionKind(kind int) editor.CompletionKind {
	switch kind {
	case 2:
		return editor.CompletionMethod
	case 3, 4:
		return editor.CompletionFunction
	case 5, 10:
		return editor.CompletionField
	case 7, 8, 13, 22:
		return editor.CompletionType
	case 9:
		return editor.CompletionModule
	case 21:
		return editor.CompletionConstant
	case 14:
		return editor.CompletionKeyword
	case 15:
		return editor.CompletionSnippet
	default:
		return editor.CompletionVariable
	}
}

func (a *app) handleGoLanguageServerNotification(sequence uint64, method string, raw json.RawMessage) {
	if method == "window/showMessage" {
		var message struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &message) != nil || !languageServerWorkspaceError(message.Type, message.Message) {
			return
		}
		a.update(func() {
			if sequence != a.goLanguageServerSeq {
				return
			}
			detail := strings.TrimSpace(message.Message)
			if strings.Contains(strings.ToLower(detail), "invalid go version") {
				detail += ". Update gopls or select a newer gopls executable in Settings."
			}
			a.reportGoLanguageServerError(detail)
		})
		return
	}
	if method != "textDocument/publishDiagnostics" {
		return
	}
	a.handleLanguageServerDiagnostics(goLanguageServerID, raw, func() bool { return sequence == a.goLanguageServerSeq })
}

func (a *app) handleLanguageServerDiagnostics(serverID string, raw json.RawMessage, valid func() bool) {
	var params struct {
		URI         string `json:"uri"`
		Version     *int64 `json:"version"`
		Diagnostics []struct {
			Range    languageServerRange `json:"range"`
			Severity int                 `json:"severity"`
			Message  string              `json:"message"`
			Source   string              `json:"source"`
		} `json:"diagnostics"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return
	}
	a.update(func() {
		if !valid() {
			return
		}
		path, ok := languageServerDocumentPath(a.snapshot.Root, params.URI)
		document := a.code.documents[path]
		if !ok || document == nil || document.state == nil || document.state.Buffer == nil {
			return
		}
		version := document.state.Buffer.Version()
		if params.Version != nil && uint64(*params.Version) != version {
			return
		}
		text := document.state.Buffer.String()
		diagnostics := make([]editor.Diagnostic, 0, len(params.Diagnostics))
		for _, diagnostic := range params.Diagnostics {
			diagnostics = append(diagnostics, editor.Diagnostic{
				Start:    languageServerOffsetAt(text, diagnostic.Range.Start),
				End:      languageServerOffsetAt(text, diagnostic.Range.End),
				Severity: languageServerDiagnosticSeverity(diagnostic.Severity),
				Message:  diagnostic.Message, Source: diagnostic.Source,
			})
		}
		if document.languageDiagnostics == nil {
			document.languageDiagnostics = make(map[string][]editor.Diagnostic)
		}
		document.languageDiagnostics[serverID] = diagnostics
		combined := make([]editor.Diagnostic, 0)
		for _, serverDiagnostics := range document.languageDiagnostics {
			combined = append(combined, serverDiagnostics...)
		}
		document.state.SetDiagnostics(version, combined)
	})
}

func languageServerWorkspaceError(messageType int, message string) bool {
	lower := strings.ToLower(message)
	return messageType == 1 || strings.Contains(lower, "invalid go version") ||
		strings.Contains(lower, "errors loading workspace") || strings.Contains(lower, "failed to load workspace")
}

func languageServerDocumentPath(root, uri string) (string, bool) {
	name, ok := languageServerFilePath(uri)
	if !ok {
		return "", false
	}
	if path, inside := languageServerPathWithinRepository(root, name); inside {
		return path, true
	}
	return filepath.Clean(name), filepath.IsAbs(name)
}

func languageServerDiagnosticSeverity(severity int) editor.Severity {
	switch severity {
	case 2:
		return editor.SeverityWarning
	case 3:
		return editor.SeverityInfo
	case 4:
		return editor.SeverityHint
	default:
		return editor.SeverityError
	}
}
