package app

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

var languageServerSemanticTokenTypes = []string{
	"namespace", "type", "class", "enum", "interface", "struct", "typeParameter", "parameter",
	"variable", "property", "enumMember", "event", "function", "method", "macro", "keyword",
	"modifier", "comment", "string", "number", "regexp", "operator", "decorator",
}

type languageServerDocumentSymbol struct {
	Name           string                         `json:"name"`
	Detail         string                         `json:"detail"`
	Kind           int                            `json:"kind"`
	Range          languageServerRange            `json:"range"`
	SelectionRange languageServerRange            `json:"selectionRange"`
	Children       []languageServerDocumentSymbol `json:"children"`
	Location       *languageServerLocation        `json:"location,omitempty"`
	Depth          int                            `json:"-"`
}

type languageServerInlayHint struct {
	Position     languageServerPosition `json:"position"`
	Label        json.RawMessage        `json:"label"`
	PaddingLeft  bool                   `json:"paddingLeft"`
	PaddingRight bool                   `json:"paddingRight"`
}

type languageServerSemanticTokens struct {
	Data []uint32 `json:"data"`
}

func (a *app) syncLanguageDocumentFeatures(document *codeDocument) {
	if document == nil || !document.loaded || document.state == nil || document.state.Buffer == nil {
		return
	}
	version := document.state.Buffer.Version()
	// The buffer is copied only when a request is actually issued below, not
	// on every frame this runs on.
	var text string
	textLoaded := false
	bufferText := func() string {
		if !textLoaded {
			text = document.state.Buffer.String()
			textLoaded = true
		}
		return text
	}
	uri := languageServerFileURI(a.workingFilePath(document.path))
	settings := effectiveBuiltInEditorPreferences(a.settings)
	if settings.EditorInlayHints && a.languageServerDocumentFeatureSupported(document, "textDocument/inlayHint") && (!document.inlayRequested || document.inlayVersion != version) {
		document.inlayVersion = version
		document.inlayRequested = true
		end := languageServerPositionAt(bufferText(), len(bufferText()))
		params := map[string]any{"textDocument": languageServerTextDocument{URI: uri},
			"range": languageServerRange{Start: languageServerPosition{}, End: end}}
		a.languageServerActionRequest(document, &document.inlaySeq, "textDocument/inlayHint", params,
			func(raw json.RawMessage, err error) bool {
				if err != nil {
					document.inlayRequested = false
					return false
				}
				document.languageInlays = parseLanguageServerInlayHints(raw, bufferText())
				syncCodeDocumentInlays(document)
				return true
			})
	} else if !settings.EditorInlayHints {
		document.inlayRequested = false
		if len(document.languageInlays) > 0 {
			document.languageInlays = nil
			syncCodeDocumentInlays(document)
		}
	}
	if settings.EditorSemanticHighlighting && a.languageServerDocumentFeatureSupported(document, "textDocument/semanticTokens/full") && (!document.semanticRequested || document.semanticVersion != version) {
		document.semanticVersion = version
		document.semanticRequested = true
		tokenTypes := a.languageServerSemanticTokenLegendForDocument(document)
		a.languageServerActionRequest(document, &document.semanticSeq, "textDocument/semanticTokens/full",
			map[string]any{"textDocument": languageServerTextDocument{URI: uri}}, func(raw json.RawMessage, err error) bool {
				if err != nil {
					document.semanticRequested = false
					return false
				}
				document.state.SemanticTokens = parseLanguageServerSemanticTokens(raw, bufferText(), tokenTypes)
				return true
			})
	} else if !settings.EditorSemanticHighlighting {
		document.semanticRequested = false
		document.state.SemanticTokens = nil
	}
	if !document.signatureRequested || document.signatureVersion != version {
		document.signatureVersion = version
		document.signatureRequested = true
		offset, ok := codeLanguageOffset(document)
		if ok && offset > 0 && strings.ContainsRune("(,", rune(bufferText()[offset-1])) && a.languageServerDocumentFeatureSupported(document, "textDocument/signatureHelp") {
			params := languageServerTextPosition{TextDocument: languageServerTextDocument{URI: uri}, Position: languageServerPositionAt(bufferText(), offset)}
			a.languageServerActionRequest(document, &document.signatureSeq, "textDocument/signatureHelp", params,
				func(raw json.RawMessage, err error) bool {
					content := parseLanguageServerSignatureHelp(raw)
					if err != nil || content == "" {
						document.signatureRequested = false
						return false
					}
					document.state.ShowHover(offset, offset, content, nil)
					return true
				})
		}
	}
}

type languageServerDocumentFeatureSupport interface{ SupportsLanguageFeature(string) bool }
type languageServerSemanticTokenLegendSupport interface{ SemanticTokenTypes() []string }

func languageServerSemanticTokenLegend(raw json.RawMessage) []string {
	var initialized struct {
		Capabilities struct {
			SemanticTokensProvider struct {
				Legend struct {
					TokenTypes []string `json:"tokenTypes"`
				} `json:"legend"`
			} `json:"semanticTokensProvider"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(raw, &initialized) == nil && len(initialized.Capabilities.SemanticTokensProvider.Legend.TokenTypes) > 0 {
		return initialized.Capabilities.SemanticTokensProvider.Legend.TokenTypes
	}
	return languageServerSemanticTokenTypes
}

func (a *app) languageServerSemanticTokenLegendForDocument(document *codeDocument) []string {
	if strings.EqualFold(filepath.Ext(document.path), ".go") {
		if support, ok := a.lsp.goServer.(languageServerSemanticTokenLegendSupport); ok {
			if tokenTypes := support.SemanticTokenTypes(); len(tokenTypes) > 0 {
				return tokenTypes
			}
		}
		return languageServerSemanticTokenTypes
	}
	for _, server := range a.additionalLanguageServersForDocument(document) {
		if support, ok := server.connection.(languageServerSemanticTokenLegendSupport); ok {
			if tokenTypes := support.SemanticTokenTypes(); len(tokenTypes) > 0 {
				return tokenTypes
			}
		}
	}
	return languageServerSemanticTokenTypes
}

func (a *app) languageServerDocumentFeatureSupported(document *codeDocument, method string) bool {
	if document == nil {
		return false
	}
	if strings.EqualFold(filepath.Ext(document.path), ".go") {
		support, ok := a.lsp.goServer.(languageServerDocumentFeatureSupport)
		return ok && support.SupportsLanguageFeature(method)
	}
	for _, server := range a.additionalLanguageServersForDocument(document) {
		if support, ok := server.connection.(languageServerDocumentFeatureSupport); ok && support.SupportsLanguageFeature(method) {
			return true
		}
	}
	return false
}

func parseLanguageServerSignatureHelp(raw json.RawMessage) string {
	var help struct {
		Signatures []struct {
			Label         string          `json:"label"`
			Documentation json.RawMessage `json:"documentation"`
		} `json:"signatures"`
		ActiveSignature int `json:"activeSignature"`
	}
	if json.Unmarshal(raw, &help) != nil || len(help.Signatures) == 0 {
		return ""
	}
	index := max(0, min(help.ActiveSignature, len(help.Signatures)-1))
	signature := help.Signatures[index]
	content := "```\n" + signature.Label + "\n```"
	if documentation := languageServerMarkupText(signature.Documentation); documentation != "" {
		content += "\n\n" + documentation
	}
	return content
}

func parseLanguageServerDocumentSymbols(raw json.RawMessage) []languageServerDocumentSymbol {
	var symbols []languageServerDocumentSymbol
	if json.Unmarshal(raw, &symbols) != nil {
		return nil
	}
	var flattened []languageServerDocumentSymbol
	var add func([]languageServerDocumentSymbol, int)
	add = func(items []languageServerDocumentSymbol, depth int) {
		for _, symbol := range items {
			children := symbol.Children
			symbol.Children = nil
			symbol.Depth = depth
			if symbol.Location != nil {
				symbol.Range = symbol.Location.Range
				symbol.SelectionRange = symbol.Location.Range
			}
			flattened = append(flattened, symbol)
			add(children, depth+1)
		}
	}
	add(symbols, 0)
	return flattened
}

func parseLanguageServerInlayHints(raw json.RawMessage, text string) []editor.Inlay {
	var hints []languageServerInlayHint
	if json.Unmarshal(raw, &hints) != nil {
		return nil
	}
	inlays := make([]editor.Inlay, 0, len(hints))
	for _, hint := range hints {
		offset, ok := languageServerExactOffsetAt(text, hint.Position)
		if !ok {
			continue
		}
		label := ""
		if json.Unmarshal(hint.Label, &label) != nil {
			var parts []struct {
				Value string `json:"value"`
			}
			if json.Unmarshal(hint.Label, &parts) == nil {
				for _, part := range parts {
					label += part.Value
				}
			}
		}
		if label == "" {
			continue
		}
		if hint.PaddingLeft {
			label = " " + label
		}
		if hint.PaddingRight {
			label += " "
		}
		inlays = append(inlays, editor.Inlay{Offset: offset, Text: label})
	}
	return inlays
}

func parseLanguageServerSemanticTokens(raw json.RawMessage, text string, tokenTypes []string) []editor.SemanticToken {
	if len(tokenTypes) == 0 {
		tokenTypes = languageServerSemanticTokenTypes
	}
	var response languageServerSemanticTokens
	if json.Unmarshal(raw, &response) != nil || len(response.Data)%5 != 0 {
		return nil
	}
	line, character := 0, 0
	tokens := make([]editor.SemanticToken, 0, len(response.Data)/5)
	for i := 0; i < len(response.Data); i += 5 {
		deltaLine, deltaStart, length, tokenType := int(response.Data[i]), int(response.Data[i+1]), int(response.Data[i+2]), int(response.Data[i+3])
		line += deltaLine
		if deltaLine == 0 {
			character += deltaStart
		} else {
			character = deltaStart
		}
		start, okStart := languageServerExactOffsetAt(text, languageServerPosition{Line: line, Character: character})
		end, okEnd := languageServerExactOffsetAt(text, languageServerPosition{Line: line, Character: character + length})
		if !okStart || !okEnd || tokenType < 0 || tokenType >= len(tokenTypes) {
			continue
		}
		tokens = append(tokens, editor.SemanticToken{Start: start, End: end, Kind: semanticEditorTokenKind(tokenTypes[tokenType])})
	}
	return tokens
}

func semanticEditorTokenKind(kind string) editor.TokenKind {
	switch kind {
	case "type", "class", "enum", "interface", "struct", "typeParameter", "namespace":
		return editor.TokenType
	case "function", "method", "event":
		return editor.TokenFunction
	case "property":
		return editor.TokenProperty
	case "enumMember":
		return editor.TokenConstant
	case "number":
		return editor.TokenNumber
	case "string":
		return editor.TokenString
	case "regexp":
		return editor.TokenRegexp
	case "comment":
		return editor.TokenComment
	case "keyword", "modifier":
		return editor.TokenKeyword
	case "operator":
		return editor.TokenOperator
	case "macro", "decorator":
		return editor.TokenMeta
	default:
		return editor.TokenVariable
	}
}

func languageServerSymbolBreadcrumb(symbols []languageServerDocumentSymbol, text string, offset int) string {
	position := languageServerPositionAt(text, offset)
	var names []string
	for _, symbol := range symbols {
		if languageServerPositionWithin(position, symbol.Range) {
			if symbol.Depth < len(names) {
				names = names[:symbol.Depth]
			}
			for len(names) < symbol.Depth {
				names = append(names, "")
			}
			names = append(names, symbol.Name)
		}
	}
	filtered := names[:0]
	for _, name := range names {
		if name != "" {
			filtered = append(filtered, name)
		}
	}
	return strings.Join(filtered, "  ›  ")
}

func languageServerPositionWithin(position languageServerPosition, r languageServerRange) bool {
	less := func(a, b languageServerPosition) bool {
		return a.Line < b.Line || a.Line == b.Line && a.Character < b.Character
	}
	return !less(position, r.Start) && !less(r.End, position)
}
