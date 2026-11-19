package app

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"
)

const (
	languageSymbolFilterAll = iota
	languageSymbolFilterTypes
	languageSymbolFilterFunctions
	languageSymbolFilterVariables
	languageSymbolFilterConstants
)

const (
	languageSymbolSortKind = iota
	languageSymbolSortName
	languageSymbolSortPosition

	languageSymbolRowHeight float32 = 38
	languageSymbolMaxRows           = 10
)

// languageSymbolsViewState is view state only. The document owns the parsed
// LSP symbols; opening the modal resets every search and presentation choice.
type languageSymbolsViewState struct {
	query       string
	filter      int
	sort        int
	collapsed   map[int]bool
	selected    int
	reveal      bool
	searchFocus ui.Handle
	list        ui.ListState
}

type languageSymbolRow struct {
	source      int
	depth       int
	matches     bool
	hasChildren bool
}

type languageSymbolNode struct {
	source   int
	children []int
}

// languageSymbolTree recovers the parent/child relation from the depth-first
// LSP flattening. Clamping malformed depths also prevents an orphan from
// creating arbitrary indentation or losing its siblings when a parent folds.
func languageSymbolTree(symbols []languageServerDocumentSymbol) ([]languageSymbolNode, []int) {
	nodes := make([]languageSymbolNode, len(symbols))
	roots := make([]int, 0, len(symbols))
	stack := make([]int, 0, len(symbols))
	for index, symbol := range symbols {
		nodes[index].source = index
		depth := max(symbol.Depth, 0)
		if len(stack) == 0 {
			depth = 0
		} else {
			depth = min(depth, len(stack))
		}
		stack = stack[:depth]
		if depth == 0 {
			roots = append(roots, index)
		} else {
			parent := stack[depth-1]
			nodes[parent].children = append(nodes[parent].children, index)
		}
		stack = append(stack, index)
	}
	return nodes, roots
}

func languageSymbolFilterMatches(symbol languageServerDocumentSymbol, filter int) bool {
	switch filter {
	case languageSymbolFilterTypes:
		switch symbol.Kind {
		case 2, 3, 4, 5, 10, 11, 23, 26:
			return true
		}
	case languageSymbolFilterFunctions:
		switch symbol.Kind {
		case 6, 9, 12:
			return true
		}
	case languageSymbolFilterVariables:
		switch symbol.Kind {
		case 7, 8, 13:
			return true
		}
	case languageSymbolFilterConstants:
		switch symbol.Kind {
		case 14, 15, 16, 17, 22:
			return true
		}
	case languageSymbolFilterAll:
		return true
	}
	return false
}

func languageSymbolMatches(symbol languageServerDocumentSymbol, query string, filter int) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	return languageSymbolFilterMatches(symbol, filter) &&
		(query == "" || strings.Contains(strings.ToLower(symbol.Name+" "+symbol.Detail), query))
}

func languageSymbolGroupRank(symbol languageServerDocumentSymbol) int {
	switch {
	case languageSymbolFilterMatches(symbol, languageSymbolFilterFunctions):
		return 0
	case languageSymbolFilterMatches(symbol, languageSymbolFilterTypes):
		return 1
	case languageSymbolFilterMatches(symbol, languageSymbolFilterVariables):
		return 2
	case languageSymbolFilterMatches(symbol, languageSymbolFilterConstants):
		return 3
	default:
		return 4
	}
}

func sortLanguageSymbolIndices(indices []int, symbols []languageServerDocumentSymbol, order int) {
	if order == languageSymbolSortPosition {
		return
	}
	sort.SliceStable(indices, func(i, j int) bool {
		left, right := symbols[indices[i]], symbols[indices[j]]
		if order == languageSymbolSortKind {
			leftKind, rightKind := languageSymbolGroupRank(left), languageSymbolGroupRank(right)
			if leftKind != rightKind {
				return leftKind < rightKind
			}
			return false
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
}

// languageSymbolVisibleRows includes the ancestors of search/filter matches,
// not only the matching leaves. Searching still expands the outline so a match
// inside a folded declaration cannot disappear without explanation.
func languageSymbolVisibleRows(symbols []languageServerDocumentSymbol, query string, filter, order int, collapsed map[int]bool) []languageSymbolRow {
	if len(symbols) == 0 {
		return nil
	}
	nodes, roots := languageSymbolTree(symbols)
	matches := make([]bool, len(symbols))
	memo := make(map[int]bool, len(symbols))
	var subtreeMatches func(int) bool
	subtreeMatches = func(index int) bool {
		if cached, ok := memo[index]; ok {
			return cached
		}
		matched := languageSymbolMatches(symbols[index], query, filter)
		matches[index] = matched
		for _, child := range nodes[index].children {
			matched = subtreeMatches(child) || matched
		}
		memo[index] = matched
		return matched
	}
	for _, root := range roots {
		subtreeMatches(root)
	}
	rows := make([]languageSymbolRow, 0, len(symbols))
	filtered := strings.TrimSpace(query) != "" || filter != languageSymbolFilterAll
	var appendRows func([]int, int)
	appendRows = func(indices []int, depth int) {
		sortLanguageSymbolIndices(indices, symbols, order)
		for _, index := range indices {
			if !subtreeMatches(index) {
				continue
			}
			rows = append(rows, languageSymbolRow{source: index, depth: depth, matches: matches[index], hasChildren: len(nodes[index].children) > 0})
			if len(nodes[index].children) > 0 && (filtered || !collapsed[index]) {
				appendRows(nodes[index].children, depth+1)
			}
		}
	}
	appendRows(roots, 0)
	return rows
}

func languageSymbolFilterCounts(symbols []languageServerDocumentSymbol) [5]int {
	var counts [5]int
	for _, symbol := range symbols {
		for filter := languageSymbolFilterTypes; filter <= languageSymbolFilterConstants; filter++ {
			if languageSymbolFilterMatches(symbol, filter) {
				counts[filter]++
			}
		}
	}
	return counts
}

func languageSymbolActiveSource(document *codeDocument) int {
	offset, ok := codeLanguageOffset(document)
	if !ok {
		return -1
	}
	position := languageServerPositionAt(document.state.Buffer.String(), offset)
	selected, depth := -1, -1
	for index, symbol := range document.symbols {
		if symbol.Depth >= depth && languageServerPositionWithin(position, symbol.Range) {
			selected, depth = index, symbol.Depth
		}
	}
	return selected
}

func languageSymbolKindName(symbol languageServerDocumentSymbol) string {
	switch symbol.Kind {
	case 2:
		return "module"
	case 3:
		return "namespace"
	case 4:
		return "package"
	case 5:
		return "class"
	case 6, 9, 12:
		return "func"
	case 7:
		return "property"
	case 8:
		return "field"
	case 10:
		return "enum"
	case 11:
		return "interface"
	case 14:
		return "const"
	case 15:
		return "string"
	case 16:
		return "number"
	case 17:
		return "bool"
	case 18:
		return "array"
	case 20:
		return "key"
	case 22:
		return "const"
	case 23:
		return "struct"
	case 26:
		return "type"
	default:
		return "var"
	}
}

func languageSymbolBadge(symbol languageServerDocumentSymbol) string {
	detail := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(symbol.Detail), ":"))
	if len(detail) <= 22 && !strings.ContainsAny(detail, "(){};,=\n\r\t") &&
		!strings.ContainsFunc(detail, unicode.IsSpace) && detail != "" {
		return detail
	}
	return languageSymbolKindName(symbol)
}

func languageSymbolGlyph(symbol languageServerDocumentSymbol) string {
	switch symbol.Kind {
	case 5:
		return "C"
	case 6, 9, 12:
		return "ƒ"
	case 7, 8:
		return "F"
	case 10, 22:
		return "E"
	case 11:
		return "I"
	case 14, 15, 16, 17:
		return "C"
	case 23:
		return "S"
	case 26:
		return "T"
	default:
		return "V"
	}
}

func languageSymbolColor(symbol languageServerDocumentSymbol) ui.Color {
	switch {
	case languageSymbolFilterMatches(symbol, languageSymbolFilterFunctions):
		return ui.Hex("#b18ae8")
	case languageSymbolFilterMatches(symbol, languageSymbolFilterTypes):
		return ui.Hex("#bb91e8")
	case languageSymbolFilterMatches(symbol, languageSymbolFilterConstants):
		return ui.Hex("#32b995")
	default:
		return ui.Hex("#8baee8")
	}
}

func (a *app) languageSymbolDialogView(c *ui.Context) {
	document := a.code.languageDocument
	state := &a.code.languageSymbols
	theme := c.Theme()
	ui.Overlay(c, func() {
		c.Key("code-language-symbols-dialog")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.28)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(576).MaxWidthPercent(92).MaxHeightPercent(88).
				Padding(18).Gap(12).Background(theme.Background).Clip().
				Border(1, theme.Border).Radius(theme.Radius + 5).
				Role(ui.RoleDialog).Label(tr("Outline"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(32, 32).Radius(7).Center().Background(theme.Surface).Children(func() {
						ui.Icon(c, fileBracesIcon).FontSize(17).TextColor(theme.TextMuted)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Outline")).FontSize(18).Bold()
						if document != nil {
							ui.Textf(c, tr("Symbols in %s"), document.path).TextColor(theme.TextMuted).
								FontSize(theme.FontSize - 1).SingleLine().Ellipsis("…").Tooltip(document.path)
						}
					})
					iconButton(c, "Close outline", "Escape", closeIcon, false, a.closeCodeLanguageDialog)
				})

				var search ui.Element
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Padding(4, 8).
					Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
					ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
					search = ui.TextInputBase(c, &state.query).Grow(1).MinWidth(0).Bind(&state.searchFocus).
						AutoFocus().Label(tr("Search document symbols")).Placeholder(tr("Search document symbols"))
					if state.query != "" {
						flatIconButton(c, "Clear symbol search", "Clear symbol search", closeIcon, 11, 3, false, func() {
							state.query = ""
							state.searchFocus.Focus(c)
						})
					}
				})
				search.OnTextKey(func(key ui.TextKey) bool {
					if document == nil || (key.Key != ui.KeyDown && (key.Key != ui.KeyEnter || key.Mods != 0)) {
						return false
					}
					rows := languageSymbolVisibleRows(document.symbols, state.query, state.filter, state.sort, state.collapsed)
					if len(rows) == 0 {
						return false
					}
					selected := state.selected
					if selected < 0 || selected >= len(rows) || key.Key == ui.KeyDown {
						selected = 0
					}
					if key.Key == ui.KeyEnter {
						a.openLanguageSymbol(document, document.symbols[rows[selected].source])
						key.SetText("")
						return true
					}
					state.selected = selected
					state.list.Choose(selected)
					state.list.Focus(c)
					return true
				})
				a.languageSymbolsFilterButtons(c)
				a.languageSymbolsToolbar(c)

				var symbols []languageServerDocumentSymbol
				if document != nil {
					symbols = document.symbols
				}
				rows := languageSymbolVisibleRows(symbols, state.query, state.filter, state.sort, state.collapsed)
				if len(rows) == 0 {
					ui.Column(c).FillWidth().Center().Gap(8).Padding(24).Children(func() {
						if a.code.languageError != "" {
							ui.Icon(c, conflictIcon).FontSize(20).TextColor(theme.Danger)
							ui.Text(c, a.code.languageError).TextColor(theme.Danger).TextAlign(ui.Center)
						} else if document != nil && document.symbolsLoading {
							ui.Spinner(c).Label(tr("Loading document symbols"))
							ui.Text(c, tr("Loading document symbols…")).TextColor(theme.TextMuted)
						} else {
							title, detail := tr("No document symbols found"), tr("This document has no navigable symbols")
							if strings.TrimSpace(state.query) != "" || state.filter != languageSymbolFilterAll {
								title, detail = tr("No matching symbols"), tr("Try another search or symbol type")
							}
							ui.Icon(c, searchIcon).FontSize(20).TextColor(theme.TextMuted)
							ui.Text(c, title).Bold()
							ui.Text(c, detail).TextColor(theme.TextMuted).TextAlign(ui.Center)
						}
					})
				} else {
					active := languageSymbolActiveSource(document)
					if state.reveal {
						state.selected = -1
						for index, row := range rows {
							if row.source == active {
								state.selected = index
								break
							}
						}
						if state.selected < 0 {
							state.selected = 0
						}
						state.list.ScrollIntoView(state.selected)
						state.reveal = false
					}
					state.list.Selected = &state.selected
					state.list.Key = func(index int) any { return rows[index].source }
					state.list.Label = func(index int) string {
						return languageSymbolAccessibleLabel(document, symbols[rows[index].source])
					}
					state.list.RowHighlighted = func(index int) bool { return rows[index].source == active }
					height := float32(min(len(rows), languageSymbolMaxRows)) * languageSymbolRowHeight
					if available := back.Bounds().H; available > 0 {
						height = min(height, max(languageSymbolRowHeight*2, available-310))
					}
					list := ui.List(c, &state.list, len(rows)).FillWidth().Height(height).Label(tr("Document symbols"))
					list.Rows(func(row ui.ListRow) {
						a.languageSymbolRow(row, document, symbols, rows[row.Index])
					})
					if list.Submitted() && state.selected >= 0 && state.selected < len(rows) {
						a.openLanguageSymbol(document, symbols[rows[state.selected].source])
					}
				}

				ui.Box(c).Height(1).Background(theme.Border)
				ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
					ui.Icon(c, fileBracesIcon).FontSize(12).TextColor(theme.TextMuted)
					ui.Textf(c, tr("%d symbols"), len(symbols)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
					ui.Text(c, tr("Enter to open")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCodeLanguageDialog()
			}
		})
	})
}

func (a *app) languageSymbolsFilterButtons(c *ui.Context) {
	state := &a.code.languageSymbols
	theme := c.Theme()
	counts := [5]int{}
	if document := a.code.languageDocument; document != nil {
		counts = languageSymbolFilterCounts(document.symbols)
	}
	ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
		for filter, label := range []string{"", "Types", "Functions", "Variables", "Constants"} {
			if filter == languageSymbolFilterAll {
				continue
			}
			filter, label := filter, label
			active := state.filter == filter
			button := ui.ButtonBase(c).Height(30).Gap(5).Padding(3, 9).Radius(5).
				Background(theme.Surface).Border(1, theme.Border).
				Label(trf("%s symbols (%d)", tr(label), counts[filter])).Checked(active)
			if active {
				button.Border(1, theme.Accent).Background(theme.Accent.Alpha(0.09))
			} else if button.Hovered() {
				button.Background(theme.SurfaceHover)
			}
			button.Children(func() {
				color := theme.Text
				if active {
					color = theme.Accent
				}
				ui.Text(c, tr(label)).FontSize(theme.FontSize - 1).TextColor(color)
				ui.Textf(c, "%d", counts[filter]).FontSize(theme.FontSize-2).
					TextColor(theme.TextMuted).Padding(1, 4).Radius(3).Background(theme.TextMuted.Alpha(0.10))
			})
			if button.Clicked() {
				if active {
					state.filter = languageSymbolFilterAll
				} else {
					state.filter = filter
				}
				state.onFilterChanged()
			}
		}
	})
}

func (a *app) languageSymbolsToolbar(c *ui.Context) {
	theme := c.Theme()
	state := &a.code.languageSymbols
	ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
		sortLabel := tr("by: kind")
		if state.sort == languageSymbolSortName {
			sortLabel = tr("by: name")
		} else if state.sort == languageSymbolSortPosition {
			sortLabel = tr("by: position")
		}
		sortButton := ui.ButtonBase(c).Gap(5).Padding(3, 5).Radius(theme.Radius).
			Label(tr("Sort document symbols")).Tooltip(tr("Sort document symbols"))
		if sortButton.Hovered() {
			sortButton.Background(theme.SurfaceHover)
		}
		sortButton.Children(func() {
			ui.Icon(c, arrowDownIcon).FontSize(11).TextColor(theme.TextMuted)
			ui.Text(c, sortLabel).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
			ui.Icon(c, chevronDownIcon).FontSize(11).TextColor(theme.TextMuted)
		})
		sortButton.PopoverMenuButton(func(m *ui.Menu) {
			for order, label := range []string{"By kind", "By name", "By position"} {
				order := order
				if order == state.sort {
					label = "✓ " + label
				}
				if m.Item(tr(label)).ID(fmt.Sprintf("language-symbol-sort:%d", order)).Chosen() {
					state.sort = order
				}
			}
		})
		ui.Spacer(c)
		flatIconButton(c, "Expand all symbols", "Expand all symbols", expandIcon, 13, 4, false, func() {
			state.collapsed = nil
		})
		flatIconButton(c, "Collapse all symbols", "Collapse all symbols", shrinkIcon, 13, 4, false, func() {
			var symbols []languageServerDocumentSymbol
			if document := a.code.languageDocument; document != nil {
				symbols = document.symbols
			}
			state.collapseAll(symbols)
		})
	})
}

func languageSymbolAccessibleLabel(document *codeDocument, symbol languageServerDocumentSymbol) string {
	path := ""
	if document != nil {
		path = document.path
	}
	return trf("Go to %s, %s:%d", symbol.Name, path, symbol.SelectionRange.Start.Line+1)
}

func (a *app) languageSymbolRow(row ui.ListRow, document *codeDocument, symbols []languageServerDocumentSymbol, visible languageSymbolRow) {
	theme := row.Context.Theme()
	c := row.Context
	symbol := symbols[visible.source]
	hasChildren := visible.hasChildren
	color := theme.TextMuted
	if row.Selected() {
		color = theme.AccentText
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(1).Padding(0, 5, 0, float32(5+visible.depth*15)).Children(func() {
		if hasChildren {
			icon, action := chevronDownIcon, "Collapse %s"
			if a.code.languageSymbols.collapsed[visible.source] && strings.TrimSpace(a.code.languageSymbols.query) == "" && a.code.languageSymbols.filter == languageSymbolFilterAll {
				icon, action = chevronRightIcon, "Expand %s"
			}
			button := ui.ButtonBase(c).Size(18, languageSymbolRowHeight-2).
				Label(trf(action, symbol.Name)).Tooltip(trf(action, symbol.Name))
			button.Children(func() {
				ui.Icon(c, icon).FontSize(12).TextColor(color)
			})
			if button.Clicked() {
				if a.code.languageSymbols.collapsed == nil {
					a.code.languageSymbols.collapsed = make(map[int]bool)
				}
				a.code.languageSymbols.collapsed[visible.source] = !a.code.languageSymbols.collapsed[visible.source]
			}
		} else {
			ui.Box(c).Width(18).Height(languageSymbolRowHeight - 2)
		}
		button := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(languageSymbolRowHeight-2).Gap(7).Padding(0, 5).
			Label(languageSymbolAccessibleLabel(document, symbol)).Tooltip(fmt.Sprintf("%s:%d", document.path, symbol.SelectionRange.Start.Line+1))
		button.Children(func() {
			ui.Text(c, languageSymbolGlyph(symbol)).Width(16).TextAlign(ui.Center).
				FontSize(theme.FontSize - 1).Bold().TextColor(languageSymbolColor(symbol))
			nameColor := theme.Text
			if (strings.TrimSpace(a.code.languageSymbols.query) != "" || a.code.languageSymbols.filter != languageSymbolFilterAll) && !visible.matches {
				nameColor = theme.TextMuted
			}
			if row.Selected() {
				nameColor = theme.AccentText
			}
			ui.Text(c, symbol.Name).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").TextColor(nameColor).
				Tooltip(symbol.Name)
			badgeColor := languageSymbolColor(symbol)
			if row.Selected() {
				badgeColor = theme.AccentText
			}
			ui.Text(c, languageSymbolBadge(symbol)).MaxWidth(82).Padding(1, 4).Radius(3).
				FontSize(theme.FontSize - 2).SingleLine().Ellipsis("…").TextColor(badgeColor).
				Background(badgeColor.Alpha(0.11))
			ui.Textf(c, "%d", symbol.SelectionRange.Start.Line+1).Width(32).TextAlign(ui.End).
				FontSize(theme.FontSize - 1).TextColor(color)
		})
		if button.Clicked() {
			a.openLanguageSymbol(document, symbol)
		}
	})
}

func (s *languageSymbolsViewState) onFilterChanged() {
	s.selected = -1
	s.reveal = true
}

func (s *languageSymbolsViewState) collapseAll(symbols []languageServerDocumentSymbol) {
	nodes, roots := languageSymbolTree(symbols)
	s.collapsed = make(map[int]bool)
	var collapse func([]int)
	collapse = func(indices []int) {
		for _, index := range indices {
			if len(nodes[index].children) > 0 {
				s.collapsed[index] = true
				collapse(nodes[index].children)
			}
		}
	}
	collapse(roots)
	s.selected = -1
}

func (a *app) openLanguageSymbol(document *codeDocument, symbol languageServerDocumentSymbol) {
	if document == nil {
		return
	}
	position := symbol.SelectionRange.Start
	a.closeCodeLanguageDialog()
	a.openCodeLanguageServerLocation(document.path, position)
}
