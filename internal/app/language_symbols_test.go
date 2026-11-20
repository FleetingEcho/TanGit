package app

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func languageSymbolsFixture() []languageServerDocumentSymbol {
	makeSymbol := func(name string, kind, depth, line int, detail string) languageServerDocumentSymbol {
		position := languageServerPosition{Line: line}
		return languageServerDocumentSymbol{
			Name: name, Kind: kind, Depth: depth, Detail: detail,
			Range:          languageServerRange{Start: position, End: languageServerPosition{Line: line + 20}},
			SelectionRange: languageServerRange{Start: position, End: position},
		}
	}
	return []languageServerDocumentSymbol{
		makeSymbol("renderData", 6, 0, 19, "func()"),
		makeSymbol("sourceMap", 13, 1, 20, ""),
		makeSymbol("DocumentLine", 23, 0, 60, "struct {…}"),
		makeSymbol("fields", 8, 1, 61, "string"),
		makeSymbol("getStorage", 12, 0, 110, "func()"),
		makeSymbol("defaultStorage", 14, 1, 111, ""),
	}
}

func languageSymbolNames(rows []languageSymbolRow, symbols []languageServerDocumentSymbol) []string {
	names := make([]string, len(rows))
	for index, row := range rows {
		names[index] = symbols[row.source].Name
	}
	return names
}

func TestLanguageSymbolsFilterSearchCollapseAndSort(t *testing.T) {
	symbols := languageSymbolsFixture()
	kindOrder := languageSymbolNames(languageSymbolVisibleRows(symbols, "", languageSymbolFilterAll, languageSymbolSortKind, nil), symbols)
	if want := []string{"renderData", "sourceMap", "getStorage", "defaultStorage", "DocumentLine", "fields"}; !slices.Equal(kindOrder, want) {
		t.Errorf("kind order = %v, want %v", kindOrder, want)
	}
	nameOrder := languageSymbolNames(languageSymbolVisibleRows(symbols, "", languageSymbolFilterAll, languageSymbolSortName, nil), symbols)
	if want := []string{"DocumentLine", "fields", "getStorage", "defaultStorage", "renderData", "sourceMap"}; !slices.Equal(nameOrder, want) {
		t.Errorf("name order = %v, want %v", nameOrder, want)
	}
	filtered := languageSymbolVisibleRows(symbols, "", languageSymbolFilterVariables, languageSymbolSortKind, nil)
	if got, want := languageSymbolNames(filtered, symbols), []string{"renderData", "sourceMap", "DocumentLine", "fields"}; !slices.Equal(got, want) {
		t.Errorf("variables = %v, want matching fields with their parents %v", got, want)
	}
	folded := languageSymbolVisibleRows(symbols, "", languageSymbolFilterAll, languageSymbolSortPosition, map[int]bool{0: true})
	if got, want := languageSymbolNames(folded, symbols), []string{"renderData", "DocumentLine", "fields", "getStorage", "defaultStorage"}; !slices.Equal(got, want) {
		t.Errorf("folded rows = %v, want %v", got, want)
	}
	// A nested match is expanded, even when its declaration was folded.
	searchingFolded := languageSymbolVisibleRows(symbols, "sourceMap", languageSymbolFilterAll, languageSymbolSortKind, map[int]bool{0: true})
	if got, want := languageSymbolNames(searchingFolded, symbols), []string{"renderData", "sourceMap"}; !slices.Equal(got, want) {
		t.Errorf("search of folded symbol = %v, want ancestor and match %v", got, want)
	}
	if rows := languageSymbolVisibleRows(symbols, "no-such-symbol", languageSymbolFilterAll, languageSymbolSortKind, nil); len(rows) != 0 {
		t.Errorf("unmatched rows = %v, want none", languageSymbolNames(rows, symbols))
	}
	if counts := languageSymbolFilterCounts(symbols); counts[languageSymbolFilterTypes] != 1 || counts[languageSymbolFilterFunctions] != 2 || counts[languageSymbolFilterVariables] != 2 || counts[languageSymbolFilterConstants] != 1 {
		t.Errorf("filter counts = %v, want one type, two functions, two variables and one constant", counts)
	}
}

func TestLanguageSymbolsActiveSourceUsesInnermostCaretScope(t *testing.T) {
	document := &codeDocument{state: &editor.State{Buffer: editor.NewBuffer("outer\ninner\nstill inner\nafter\n")}}
	document.symbols = []languageServerDocumentSymbol{
		{Name: "outer", Depth: 0, Range: languageServerRange{Start: languageServerPosition{Line: 0}, End: languageServerPosition{Line: 3}}},
		{Name: "inner", Depth: 1, Range: languageServerRange{Start: languageServerPosition{Line: 1}, End: languageServerPosition{Line: 2}}},
		{Name: "after", Depth: 0, Range: languageServerRange{Start: languageServerPosition{Line: 3}, End: languageServerPosition{Line: 4}}},
	}
	document.state.Select(len("outer\nin"), len("outer\nin"))
	if source := languageSymbolActiveSource(document); source != 1 {
		t.Fatalf("active symbol = %d, want the inner symbol at index 1", source)
	}
	document.state.Select(len("outer\ninner\nstill inner\n"), len("outer\ninner\nstill inner\n"))
	if source := languageSymbolActiveSource(document); source != 2 {
		t.Fatalf("active symbol = %d, want the outer symbol at index 2", source)
	}
	if badge := languageSymbolBadge(languageServerDocumentSymbol{Kind: 8, Detail: "string"}); badge != "string" {
		t.Errorf("field badge = %q, want its actual type", badge)
	}
	if badge := languageSymbolBadge(languageServerDocumentSymbol{Kind: 23, Detail: "struct {…}"}); badge != "struct" {
		t.Errorf("struct badge = %q, want a concise type name rather than a declaration", badge)
	}
}

func languageSymbolsHarness(t *testing.T) (*harness, *codeDocument) {
	t.Helper()
	h := menuTestApp(t)
	lines := make([]string, 130)
	for index := range lines {
		lines[index] = "// filler"
	}
	lines[19] = "func renderData() {}"
	text := strings.Join(lines, "\n")
	_, document := languageActionTestDocument(h, text)
	offset := strings.Index(text, "renderData") + 3
	document.state.Select(offset, offset)
	document.symbols = languageSymbolsFixture()
	document.symbolsRequested = true
	document.symbolVersion = document.state.Buffer.Version()
	h.app.code.active = document.path
	h.app.code.tabs = []string{document.path}
	h.app.openLanguageSymbols(document)
	h.tester.Frame()
	return h, document
}

func TestLanguageSymbolsDialogRendersFiltersSearchAndNavigation(t *testing.T) {
	h, document := languageSymbolsHarness(t)
	for _, text := range []string{"Outline", "Symbols in main.go", "renderData", "DocumentLine", "getStorage", "6 symbols"} {
		if !h.tester.HasText(text) {
			t.Errorf("outline is missing %q: %q", text, strings.Join(h.texts(), " | "))
		}
	}
	rows := languageSymbolVisibleRows(document.symbols, "", languageSymbolFilterAll, languageSymbolSortKind, nil)
	activeRow := slices.IndexFunc(rows, func(row languageSymbolRow) bool { return row.source == 0 })
	if state := h.app.code.languageSymbols; state.selected != activeRow || activeRow < 0 || !state.list.RowHighlighted(activeRow) {
		t.Errorf("symbol under the caret was not highlighted: active=%d activeRow=%d selected=%d rows=%d highlighted=%v", languageSymbolActiveSource(document), activeRow, state.selected, len(rows), state.list.RowHighlighted(activeRow))
	}

	if err := h.tester.Click("Types symbols (1)"); err != nil {
		t.Fatalf("choose type filter: %v", err)
	}
	h.tester.Frame()
	if h.app.code.languageSymbols.filter != languageSymbolFilterTypes || !h.tester.HasText("DocumentLine") {
		t.Fatalf("type filter did not show the document type: filter=%d", h.app.code.languageSymbols.filter)
	}
	if h.tester.HasText("getStorage") {
		t.Fatal("type filter left a function visible")
	}
	if err := h.tester.Click("Types symbols (1)"); err != nil {
		t.Fatalf("clear type filter: %v", err)
	}
	if err := h.tester.Click("Search document symbols"); err != nil {
		t.Fatalf("focus symbol search: %v", err)
	}
	h.tester.Type("fields")
	h.tester.Frame()
	if h.app.code.languageSymbols.query != "fields" || !h.tester.HasText("fields") || h.tester.HasText("renderData") {
		t.Fatalf("nested symbol search did not filter the rendered list: %q", h.texts())
	}
	if err := h.tester.Click("Go to fields, main.go:62"); err != nil {
		t.Fatalf("open nested symbol: %v", err)
	}
	h.tester.Frame()
	if h.app.code.languageDialog != codeLanguageDialogNone || document.state.CursorPos().Line != 61 {
		t.Fatalf("symbol navigation failed: dialog=%v pendingLine=%d cursor=%+v", h.app.code.languageDialog, document.pendingLine, document.state.CursorPos())
	}
	// The list state must survive the click that closes the modal. The next
	// open starts a fresh view without invalidating pending list input.
	h.app.openLanguageSymbols(document)
	document.symbols = languageSymbolsFixture()
	document.symbolsRequested = true
	h.tester.Frame()
	if state := h.app.code.languageSymbols; state.query != "" || state.filter != languageSymbolFilterAll || state.collapsed != nil || h.app.code.languageDialog != codeLanguageDialogSymbols {
		t.Fatal("reopening outline did not restore its clean symbol view")
	}
}

func TestLanguageSymbolsDialogCollapseSortAndKeyboard(t *testing.T) {
	h, document := languageSymbolsHarness(t)
	if err := h.tester.Click("Collapse all symbols"); err != nil {
		t.Fatalf("collapse outline: %v", err)
	}
	h.tester.Frame()
	if h.tester.HasText("sourceMap") || h.tester.HasText("fields") {
		t.Fatal("collapsed parent rows left their children visible")
	}
	if err := h.tester.Click("Expand renderData"); err != nil {
		t.Fatalf("expand individual symbol: %v", err)
	}
	h.tester.Frame()
	if !h.tester.HasText("sourceMap") {
		t.Fatal("expanding one declaration did not restore its child")
	}

	if err := h.tester.Click("Sort document symbols"); err != nil {
		t.Fatalf("open sort menu: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.ChooseMenuItem("By name"); err != nil {
		t.Fatalf("sort symbols by name: %v", err)
	}
	h.tester.Frame()
	if h.app.code.languageSymbols.sort != languageSymbolSortName || !h.tester.HasText("by: name") {
		t.Fatal("sort selection did not update the toolbar")
	}

	if err := h.tester.Click("Search document symbols"); err != nil {
		t.Fatalf("focus symbol search: %v", err)
	}
	h.tester.Key(0, ui.KeyDown)
	h.tester.Frame()
	if h.app.code.languageSymbols.selected != 0 {
		t.Fatal("Down did not move focus from search to the first visible symbol")
	}
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	if h.app.code.languageDialog != codeLanguageDialogNone || document.state.CursorPos().Line != 60 {
		t.Fatalf("list Enter did not open the selected symbol: dialog=%v cursor=%+v", h.app.code.languageDialog, document.state.CursorPos())
	}
	h.app.openLanguageSymbols(document)
	document.symbols = languageSymbolsFixture()
	document.symbolsRequested = true
	h.tester.Frame()
	if err := h.tester.Click("Search document symbols"); err != nil {
		t.Fatalf("focus symbol search: %v", err)
	}
	if active, _ := h.tester.TextInputActive(); !active {
		t.Fatal("symbol search is not receiving text input")
	}
	h.tester.Type("Storage")
	h.tester.Frame()
	if h.app.code.languageSymbols.query != "Storage" {
		t.Fatalf("typed symbol search = %q, want Storage", h.app.code.languageSymbols.query)
	}
	if !h.tester.HasText("defaultStorage") {
		t.Fatal("case-insensitive search did not expose the nested constant")
	}
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	if h.app.code.languageDialog != codeLanguageDialogNone || document.state.CursorPos().Line != 110 {
		t.Fatalf("Enter did not open the first matching symbol: line=%d dialog=%v cursor=%+v", document.pendingLine, h.app.code.languageDialog, document.state.CursorPos())
	}
}

func TestLanguageSymbolsDialogIsWindowCentered(t *testing.T) {
	h, _ := languageSymbolsHarness(t)
	dialog, ok := h.tester.AccessByRole(ui.RoleDialog, "Outline")
	if !ok {
		t.Fatal("outline dialog is not visible")
	}
	bounds := dialog.Bounds
	centerX, centerY := bounds.X+bounds.W/2, bounds.Y+bounds.H/2
	if math.Abs(float64(centerX-640)) > 1 || math.Abs(float64(centerY-400)) > 1 {
		t.Fatalf("outline center = (%v, %v), want window center (640, 400); bounds=%+v", centerX, centerY, bounds)
	}
}

func TestOutlineToolbarButtonIsIconWithTooltip(t *testing.T) {
	h, document := languageSymbolsHarness(t)
	h.app.closeCodeLanguageDialog()
	h.app.workspaceTab = sectionCode
	h.app.code.files = []string{"main.go"}
	h.app.code.visiblePaths = []string{"main.go"}
	h.app.code.filesLoaded = true
	h.tester.Frame()
	button, ok := h.tester.AccessByRole(ui.RoleButton, "Outline")
	if !ok {
		t.Fatal("outline toolbar button is not visible")
	}
	if button.Bounds.W > 32 {
		t.Fatalf("outline button width = %v, want an icon-sized control, not a text button", button.Bounds.W)
	}
	if button.Description != "Symbols in the current document" {
		t.Fatalf("outline tooltip = %q, want Symbols in the current document", button.Description)
	}
	h.tester.Move(button.Bounds.X+button.Bounds.W/2, button.Bounds.Y+button.Bounds.H/2)
	h.tester.Advance(time.Second)
	h.tester.Frame()
	if !h.tester.HasText("Symbols in the current document") {
		t.Fatal("hovering the outline icon did not show its tooltip")
	}
	if err := h.tester.Click("Outline"); err != nil {
		t.Fatalf("open outline from icon button: %v", err)
	}
	h.tester.Frame()
	if h.app.code.languageDialog != codeLanguageDialogSymbols || h.app.code.languageDocument != document {
		t.Fatal("outline icon did not open the current document's symbols")
	}
}

func TestLanguageSymbolsDialogLoadingAndEmptyResults(t *testing.T) {
	h, document := languageSymbolsHarness(t)
	if err := h.tester.Click("Search document symbols"); err != nil {
		t.Fatalf("focus symbol search: %v", err)
	}
	h.tester.Type("no-such-symbol")
	h.tester.Frame()
	if !h.tester.HasText("No matching symbols") || h.tester.HasText("Loading document symbols…") {
		t.Fatal("an empty search did not show the filtered empty state")
	}
	connection := h.app.lsp.goServer.(*fakeLanguageServerConnection)
	var response func(json.RawMessage, error)
	connection.request = func(_ string, _ any, result func(json.RawMessage, error)) {
		response = result
	}
	h.app.closeCodeLanguageDialog()
	h.app.openLanguageSymbols(document)
	h.tester.Frame()
	if response == nil || !document.symbolsLoading || !h.tester.HasText("Loading document symbols…") {
		t.Fatal("a pending symbol request did not show its loading state")
	}
	response(json.RawMessage(`[]`), nil)
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	if document.symbolsLoading || !h.tester.HasText("No document symbols found") {
		t.Fatal("a completed empty response did not replace the loading indicator")
	}
}
