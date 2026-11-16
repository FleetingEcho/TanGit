package editor

import "testing"

func TestSemanticTokensOverrideLexerColor(t *testing.T) {
	ct := newEditorTest(t, "go", "var value = 1")
	ct.st.SemanticTokens = []SemanticToken{{Start: 4, End: 9, Kind: TokenFunction}}
	ct.tt.Frame()
	line := ct.st.ed.view.line(0)
	var colors runColors
	ct.st.ed.view.colorsOf(line, &colors)
	if got := colors.at(line.toDisp(5, false), ct.st.ed.view.theme.Text); got != ct.st.ed.view.theme.TokenColor(TokenFunction) {
		t.Fatalf("semantic color = %v, want function color", got)
	}
}
