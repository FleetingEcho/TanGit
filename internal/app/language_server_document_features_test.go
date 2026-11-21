package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestParseLanguageServerSignatureHelp(t *testing.T) {
	raw := json.RawMessage(`{"activeSignature":1,"signatures":[{"label":"first()"},{"label":"second(x int)","documentation":{"kind":"markdown","value":"**docs**"}}]}`)
	if got := parseLanguageServerSignatureHelp(raw); got != "```\nsecond(x int)\n```\n\n**docs**" {
		t.Fatalf("signature = %q", got)
	}
}

func TestParseLanguageServerDocumentSymbolsFlattensHierarchy(t *testing.T) {
	raw := json.RawMessage(`[{"name":"Outer","range":{"start":{"line":0,"character":0},"end":{"line":5,"character":0}},"selectionRange":{"start":{"line":0,"character":5},"end":{"line":0,"character":10}},"children":[{"name":"Inner","range":{"start":{"line":1,"character":0},"end":{"line":2,"character":0}},"selectionRange":{"start":{"line":1,"character":5},"end":{"line":1,"character":10}}}]}]`)
	symbols := parseLanguageServerDocumentSymbols(raw)
	if len(symbols) != 2 || symbols[0].Name != "Outer" || symbols[0].Depth != 0 || symbols[1].Name != "Inner" || symbols[1].Depth != 1 {
		t.Fatalf("symbols = %+v", symbols)
	}
	if got := languageServerSymbolBreadcrumb(symbols, "outer\ninner\n\n\n\n", len("outer\nin")); got != "Outer  ›  Inner" {
		t.Fatalf("breadcrumb = %q", got)
	}
}

func TestParseLanguageServerInlayHintsSupportsUTF16AndLabelParts(t *testing.T) {
	raw := json.RawMessage(`[{"position":{"line":0,"character":3},"label":[{"value":": int"}],"paddingLeft":true}]`)
	got := parseLanguageServerInlayHints(raw, "a😀b")
	want := []editor.Inlay{{Offset: 5, Text: " : int"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inlays = %+v, want %+v", got, want)
	}
}

func TestParseLanguageServerSemanticTokens(t *testing.T) {
	// function at UTF-16 columns 1..3 on the first line, then a property on
	// the next line. The emoji proves offsets are converted to bytes.
	raw := json.RawMessage(`{"data":[0,1,2,12,0,1,0,1,9,0]}`)
	got := parseLanguageServerSemanticTokens(raw, "a😀b\nx", nil)
	want := []editor.SemanticToken{{Start: 1, End: 5, Kind: editor.TokenFunction}, {Start: 7, End: 8, Kind: editor.TokenProperty}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %+v, want %+v", got, want)
	}
}

func TestLanguageServerSemanticTokensUseNegotiatedLegend(t *testing.T) {
	initialized := json.RawMessage(`{"capabilities":{"semanticTokensProvider":{"legend":{"tokenTypes":["string","function"]}}}}`)
	tokenTypes := languageServerSemanticTokenLegend(initialized)
	if !reflect.DeepEqual(tokenTypes, []string{"string", "function"}) {
		t.Fatalf("legend = %v", tokenTypes)
	}
	raw := json.RawMessage(`{"data":[0,0,1,1,0]}`)
	got := parseLanguageServerSemanticTokens(raw, "x", tokenTypes)
	want := []editor.SemanticToken{{Start: 0, End: 1, Kind: editor.TokenFunction}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %+v, want %+v", got, want)
	}
}
