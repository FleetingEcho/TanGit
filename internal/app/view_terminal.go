package app

import (
	"fmt"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// newTerminal is a seam so tests can drive the pane without a real shell.
var newTerminal = terminal.New

// terminalPanelPercent is the bottom panel's initial share of the workspace.
const terminalPanelPercent = 100.0 / 3.0

const terminalSessionLimit = 6

// terminalSessionState is one independent shell. Its attempt rejects startup
// and exit callbacks after Close, including a Close while the shell starts.
// name is the user's label; shell and directory provide its default identity,
// while title remains the shell's latest OSC-reported title.
type terminalSessionState struct {
	id        uint64
	groupID   uint64
	pane      *terminal.Terminal
	loading   bool
	err       string
	name      string
	shell     string
	directory string
	title     string
	attempt   uint64
	// Search belongs to this shell alone. Keeping the handle and latest status
	// here preserves an independent find bar across splits and drag reordering.
	searchOpen   bool
	searchText   string
	searchStatus terminal.SearchStatus
	searchHandle ui.Handle
}

// terminalTabDrag carries session identity, not its current position: a drop
// may reorder the views without redirecting that shell's asynchronous events.
type terminalTabDrag struct{ id uint64 }

// terminalState owns the bottom panel. Hiding or maximizing it keeps every
// shell running; only an explicit close or a shell's own exit ends one.
type terminalState struct {
	sessions       []terminalSessionState
	nextID         uint64
	nextGroupID    uint64
	active         int
	panelOpen      bool
	panelMaximized bool
	// percent is the bottom panel's restored height; shown is the workspace
	// share in the current split. Maximizing does not overwrite either one.
	percent float32
	shown   float32
	// Each top-level terminal tab owns an independent local split tree. layout
	// mirrors the active tab for the existing split code; layouts retains the
	// other tabs while they are not rendered.
	layout         *terminalLayoutNode
	layouts        map[uint64]*terminalLayoutNode
	activeSessions map[uint64]uint64
	// Top-level custom names belong to the group, not whichever split is
	// selected. A split's name never implicitly renames its parent tab.
	groupNames map[uint64]string
	// Group selection is deferred so a tab is never replaced halfway through
	// the build that owns its terminal handles.
	pendingGroup uint64
	// A drop is applied before the next build, never while its cards are
	// being constructed: moving a session midway through a build would bind
	// that terminal's persistent handle to two views in the same frame.
	pendingMoveID     uint64
	pendingMoveTarget int
	// renameID and renameGroup identify the split or top-level tab in the
	// app-owned rename modal. Stable identities keep reordering and shell-title
	// callbacks out of the form.
	renameID    uint64
	renameGroup uint64
	renameName  string
}

func (a *app) activeTerminal() *terminalSessionState {
	if len(a.term.sessions) == 0 {
		return nil
	}
	if a.term.active < 0 || a.term.active >= len(a.term.sessions) {
		a.term.active = 0
	}
	return &a.term.sessions[a.term.active]
}

func (a *app) terminalSessionByID(id uint64) (int, *terminalSessionState) {
	for i := range a.term.sessions {
		if a.term.sessions[i].id == id && id != 0 {
			return i, &a.term.sessions[i]
		}
	}
	return -1, nil
}

func terminalSessionNumber(session *terminalSessionState) uint64 {
	if session.id != 0 {
		return session.id
	}
	return 1
}

func (a *app) terminalSessionCount() int { return len(a.term.sessions) }

func (a *app) activeTerminalGroup() uint64 {
	active := a.activeTerminal()
	if active == nil {
		return 0
	}
	if active.groupID == 0 {
		a.term.nextGroupID++
		active.groupID = a.term.nextGroupID
	}
	return active.groupID
}

// storeTerminalLayout keeps the active tab's split tree while another tab is
// selected. No terminal view is removed or rebuilt by this bookkeeping.
func (a *app) storeTerminalLayout() {
	group := a.activeTerminalGroup()
	if group == 0 || a.term.layout == nil {
		return
	}
	if a.term.layouts == nil {
		a.term.layouts = make(map[uint64]*terminalLayoutNode)
	}
	a.term.layouts[group] = a.term.layout
	if a.term.activeSessions == nil {
		a.term.activeSessions = make(map[uint64]uint64)
	}
	if active := a.activeTerminal(); active != nil {
		a.term.activeSessions[group] = active.id
	}
}

func (a *app) terminalGroups() []uint64 {
	groups := make([]uint64, 0, len(a.term.sessions))
	for _, session := range a.term.sessions {
		if len(groups) == 0 || groups[len(groups)-1] != session.groupID {
			groups = append(groups, session.groupID)
		}
	}
	return groups
}

func (a *app) terminalGroupSize(group uint64) int {
	count := 0
	for _, session := range a.term.sessions {
		if session.groupID == group {
			count++
		}
	}
	return count
}

func (a *app) selectTerminalGroup(group uint64) {
	if group == 0 || group == a.activeTerminalGroup() {
		return
	}
	a.storeTerminalLayout()
	activeID := a.term.activeSessions[group]
	selected := -1
	var session terminalSessionState
	for index, candidate := range a.term.sessions {
		if candidate.groupID != group {
			continue
		}
		if selected < 0 || candidate.id == activeID {
			selected = index
			session = candidate
		}
		if candidate.id == activeID {
			break
		}
	}
	if selected < 0 {
		return
	}
	a.term.active = selected
	a.term.layout = a.term.layouts[group]
	if a.term.layout == nil {
		a.term.layout = &terminalLayoutNode{sessionID: session.id}
		a.storeTerminalLayout()
	}
}

func (a *app) setTerminalLayout(group uint64, layout *terminalLayoutNode) {
	if group == 0 {
		return
	}
	if a.term.layouts == nil {
		a.term.layouts = make(map[uint64]*terminalLayoutNode)
	}
	if layout == nil {
		delete(a.term.layouts, group)
		delete(a.term.activeSessions, group)
	} else {
		a.term.layouts[group] = layout
	}
	if group == a.activeTerminalGroup() {
		a.term.layout = layout
	}
}

// ensureTerminalSession starts the first shell on demand. Existing sessions,
// tabs, splits and scrollback are reused by either terminal presentation.
func (a *app) ensureTerminalSession() {
	if a.activeTerminal() == nil {
		a.term.nextGroupID++
		a.term.sessions = append(a.term.sessions, terminalSessionState{groupID: a.term.nextGroupID})
		a.term.active = 0
	}
	active := a.activeTerminal()
	group := a.activeTerminalGroup()
	a.term.layout = a.term.layouts[group]
	if active.pane == nil {
		a.startTerminal()
	}
	if a.term.layout == nil {
		a.term.layout = &terminalLayoutNode{sessionID: active.id}
		a.storeTerminalLayout()
	}
}

// toggleTerminal opens or hides the bottom panel without changing the active
// workspace page. Every shell and its scrollback stays alive while hidden.
func (a *app) toggleTerminal() { a.toggleTerminalPanel() }

// openTerminal restores the bottom panel and starts its first shell on demand.
func (a *app) openTerminal() {
	a.term.panelOpen = true
	if a.term.percent <= 0 || a.term.percent >= 100 {
		a.term.percent = terminalPanelPercent
	}
	a.ensureTerminalSession()
}

func (a *app) toggleTerminalPanel() {
	if a.term.panelOpen {
		a.leaveTerminal()
		return
	}
	a.openTerminal()
}

func (a *app) terminalVisible() bool { return a.term.panelOpen }

func (a *app) hideTerminalView() { a.leaveTerminal() }

func (a *app) leaveTerminal() {
	a.term.panelOpen = false
	a.term.panelMaximized = false
}

// toggleTerminalMaximized gives the panel the whole content area, then returns
// it to the exact percentage height it had before maximization.
func (a *app) toggleTerminalMaximized() {
	if a.term.panelOpen {
		a.term.panelMaximized = !a.term.panelMaximized
	}
}

func (a *app) startTerminal() {
	a.startTerminalAt(a.term.active)
}

// startTerminalAt launches a user's shell in the repository being browsed.
// Both the repository generation and the session attempt travel with it: a
// result is closed instead of installed after its repository or slot closes.
func (a *app) startTerminalAt(index int) {
	if index < 0 || index >= len(a.term.sessions) {
		return
	}
	session := &a.term.sessions[index]
	if session.loading || session.pane != nil {
		return
	}
	if session.id == 0 {
		a.term.nextID++
		session.id = a.term.nextID
	}
	sessionID := session.id
	session.attempt++
	attempt := session.attempt
	session.loading = true
	session.err = ""
	dir := a.snapshot.Root
	session.shell = terminalDefaultShellName()
	session.directory = terminalDisplayDirectory(dir)
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		pane, err := newTerminal(terminal.Options{
			Dir: dir,
			OnSearch: func(status terminal.SearchStatus) {
				a.update(func() {
					_, current := a.terminalSessionByID(sessionID)
					if generation != a.generation || current == nil || attempt != current.attempt {
						return
					}
					current.searchStatus = status
				})
			},
			OpenLink: func(link string) bool {
				_, current := a.terminalSessionByID(sessionID)
				if generation != a.generation || current == nil || attempt != current.attempt {
					return false
				}
				path, line, ok := terminalFileLink(dir, link)
				if !ok {
					return false
				}
				a.launchEditor(path, line)
				return true
			},
			OnTitle: func(title string) {
				a.update(func() {
					_, current := a.terminalSessionByID(sessionID)
					if generation != a.generation || current == nil || attempt != current.attempt {
						return
					}
					current.title = title
				})
			},
			OnExit: func(int) {
				a.update(func() {
					currentIndex, current := a.terminalSessionByID(sessionID)
					if generation != a.generation || current == nil || attempt != current.attempt {
						return
					}
					a.closeTerminalAt(currentIndex)
				})
			},
		})
		a.update(func() {
			_, current := a.terminalSessionByID(sessionID)
			if generation != a.generation || current == nil || attempt != current.attempt || current.pane != nil {
				if pane != nil {
					_ = pane.Close()
				}
				return
			}
			current.loading = false
			if err != nil {
				current.err = safeError(err)
				return
			}
			current.pane = pane
		})
	}()
}

func (a *app) terminalAtLimit() bool {
	if a.terminalSessionCount() < terminalSessionLimit {
		return false
	}
	a.notify("error", "Terminal limit reached",
		trf("TanGit keeps up to %d terminal tabs and split panes open at once. Close a terminal before opening another.", terminalSessionLimit))
	return true
}

// newTerminalTab opens a shell in its own top-level tab rather than splitting
// the current card. Each tab retains its own local split layout when revisited.
func (a *app) newTerminalTab() {
	if !a.terminalVisible() || a.terminalAtLimit() {
		return
	}
	a.storeTerminalLayout()
	a.term.nextID++
	a.term.nextGroupID++
	index := len(a.term.sessions)
	a.term.sessions = append(a.term.sessions, terminalSessionState{
		id: a.term.nextID, groupID: a.term.nextGroupID,
	})
	a.term.active = index
	a.startTerminalAt(index)
	a.term.layout = &terminalLayoutNode{sessionID: a.term.nextID}
	a.storeTerminalLayout()
}

func (a *app) splitTerminalRight() { a.splitTerminal(false) }
func (a *app) splitTerminalBelow() { a.splitTerminal(true) }

// splitTerminal opens another independent shell in the requested direction.
// The buttons stay available at the limit so selecting one explains the limit.
func (a *app) splitTerminal(below bool) {
	active := a.activeTerminal()
	if !a.terminalVisible() || active == nil || active.pane == nil {
		return
	}
	if a.terminalAtLimit() {
		return
	}
	activeIndex := a.term.active
	activeID := active.id
	activeGroup := active.groupID
	insert := activeIndex + 1
	a.term.nextID++
	newID := a.term.nextID
	a.term.sessions = append(a.term.sessions, terminalSessionState{})
	copy(a.term.sessions[insert+1:], a.term.sessions[insert:])
	a.term.sessions[insert] = terminalSessionState{id: newID, groupID: activeGroup}
	if a.term.layout == nil {
		a.term.layout = &terminalLayoutNode{sessionID: activeID}
	}
	if !a.term.layout.split(activeID, newID, below) {
		a.term.sessions = append(a.term.sessions[:insert], a.term.sessions[insert+1:]...)
		a.term.active = activeIndex
		return
	}
	a.term.active = insert
	a.storeTerminalLayout()
	a.startTerminalAt(insert)
}

// closeTerminal exits only the active shell. Hiding is deliberately separate:
// the last Close hides the empty panel, but Hide never terminates a process.
func (a *app) closeTerminal() { a.closeTerminalAt(a.term.active) }

func (a *app) closeTerminalAt(index int) {
	if index < 0 || index >= len(a.term.sessions) {
		return
	}
	a.storeTerminalLayout()
	a.endTerminalSearch(&a.term.sessions[index])
	session := a.term.sessions[index]
	activeID := uint64(0)
	if active := a.activeTerminal(); active != nil {
		activeID = active.id
	}
	if session.pane != nil {
		_ = session.pane.Close()
	}
	if a.term.renameID == session.id {
		a.closeTerminalRename()
	}
	layout := a.term.layouts[session.groupID]
	if session.groupID == a.activeTerminalGroup() {
		layout = a.term.layout
	}
	layout = removeTerminalLayout(layout, session.id)
	a.term.sessions = append(a.term.sessions[:index], a.term.sessions[index+1:]...)
	if a.terminalGroupSize(session.groupID) == 0 {
		delete(a.term.groupNames, session.groupID)
		if a.term.renameGroup == session.groupID {
			a.closeTerminalRename()
		}
	}
	if len(a.term.sessions) == 0 {
		a.term.active = 0
		a.term.pendingMoveID = 0
		a.term.pendingMoveTarget = 0
		a.term.pendingGroup = 0
		a.term.layout = nil
		a.term.layouts = nil
		a.term.activeSessions = nil
		a.term.groupNames = nil
		a.term.panelOpen = false
		a.leaveTerminal()
		return
	}
	a.setTerminalLayout(session.groupID, layout)
	if session.id == activeID {
		// Keep another split in this tab active even when the next global
		// session belongs to a different top-level tab.
		a.term.active = min(index, len(a.term.sessions)-1)
		if a.term.sessions[a.term.active].groupID != session.groupID && index > 0 &&
			a.term.sessions[index-1].groupID == session.groupID {
			a.term.active = index - 1
		}
	} else if activeIndex, _ := a.terminalSessionByID(activeID); activeIndex >= 0 {
		a.term.active = activeIndex
	}
	a.term.layout = a.term.layouts[a.activeTerminalGroup()]
}

// requestCloseTerminalAt closes only the selected shell. Confirmation belongs
// to whole-tab and close-all actions, which can exit multiple shells at once.
func (a *app) requestCloseTerminalAt(index int) {
	a.closeTerminalAt(index)
}

// requestCloseTerminalGroup closes one top-level tab and all of its splits.
// Only multiple shells in that tab require confirmation; shells in other tabs
// neither trigger the question nor inflate its count.
func (a *app) requestCloseTerminalGroup(group uint64) {
	count := a.terminalGroupSize(group)
	if count == 0 {
		return
	}
	if count == 1 {
		a.closeTerminalGroup(group)
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Close terminal tab",
		message:      trf("%d terminals are still running.", count),
		detail:       "This closes the selected terminal tab and exits its shells. Unsaved work in those shells will be lost.",
		confirmLabel: "Close tab",
		destructive:  true,
		run:          func() { a.closeTerminalGroup(group) },
	})
}

func (a *app) closeTerminalGroup(group uint64) {
	for index := len(a.term.sessions) - 1; index >= 0; index-- {
		if a.term.sessions[index].groupID == group {
			a.closeTerminalAt(index)
		}
	}
}

// requestCloseAllTerminals makes a multi-shell exit explicit. The individual
// close controls have their own confirmation; closing every shell has its own.
func (a *app) requestCloseAllTerminals() {
	count := a.terminalSessionCount()
	if count == 0 {
		a.leaveTerminal()
		return
	}
	if count == 1 {
		a.closeTerminal()
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Close all terminals",
		message:      trf("%d terminals are still running.", count),
		detail:       "This exits every terminal shell. Unsaved work in those shells will be lost.",
		confirmLabel: "Close all",
		destructive:  true,
		run: func() {
			for len(a.term.sessions) > 0 {
				a.closeTerminalAt(0)
			}
		},
	})
}

// moveTerminalTab moves a view into its target position. Each session keeps
// its stable ID, terminal handle and callbacks; intermediate cards shift over.
func (a *app) moveTerminalTab(sourceID uint64, target int) {
	source, session := a.terminalSessionByID(sourceID)
	if source < 0 || session == nil || target < 0 || target >= len(a.term.sessions) || source == target ||
		a.term.sessions[target].groupID != session.groupID {
		return
	}
	moved := *session
	copy(a.term.sessions[source:], a.term.sessions[source+1:])
	copy(a.term.sessions[target+1:], a.term.sessions[target:])
	a.term.sessions[target] = moved
	a.orderTerminalLayoutLeaves()
	a.term.active = target
}

// resetTerminal drops every shell and anything still starting one, so a
// repository opens without a session, title or error from the one before.
func (a *app) resetTerminal() {
	for len(a.term.sessions) > 0 {
		a.closeTerminalAt(0)
	}
	a.leaveTerminal()
	a.term.panelOpen = false
	a.term.active = 0
	a.term.pendingMoveID = 0
	a.term.pendingMoveTarget = 0
	a.term.pendingGroup = 0
	a.term.layout = nil
	a.term.layouts = nil
	a.term.activeSessions = nil
	a.term.groupNames = nil
	a.closeTerminalRename()
}

// terminalButton is the status-bar entry to the application's only terminal
// presentation, the persistent bottom dock.
func (a *app) terminalButton(c *ui.Context) {
	var button ui.Element
	if a.term.panelOpen {
		button = compactIconButton(ui.PrimaryButton(c, ""))
	} else {
		button = compactIconButton(ui.Button(c, ""))
	}
	clicked := button.
		Label(tr("Toggle terminal")).
		Tooltip(tr("Terminal (Ctrl+`)")).
		Clicked()
	button.Children(func() {
		ui.Icon(c, terminalIcon).FontSize(14)
	})
	if clicked {
		a.toggleTerminalPanel()
	}
}

// terminalPaneView keeps page-wide actions separate from each shell's island.
// Every card owns its title, drag handle, rename and immediate-close buttons.
func (a *app) terminalPaneView(c *ui.Context) {
	if a.term.pendingGroup != 0 {
		a.selectTerminalGroup(a.term.pendingGroup)
		a.term.pendingGroup = 0
	}
	if a.term.pendingMoveID != 0 {
		a.moveTerminalTab(a.term.pendingMoveID, a.term.pendingMoveTarget)
		a.term.pendingMoveID = 0
		a.term.pendingMoveTarget = 0
	}
	ui.Column(c).Grow(1).Gap(0).Label(tr("Terminal pane")).Children(func() {
		islandCard(c, ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(4, 8)).Label(tr("Terminal actions")).Children(func() {
			a.terminalTabsView(c)
			active := a.activeTerminal()
			splitDisabled := active == nil || active.pane == nil
			flatIconButton(c, "New terminal tab", "Open an independent terminal in a new tab", plusIcon, 15, 5, false, a.newTerminalTab)
			flatIconButton(c, "Search terminal", "Search the active terminal (Ctrl+F)", searchIcon, 15, 5, splitDisabled, a.openTerminalSearch)
			flatIconButton(c, "Split terminal right", "Open another independent terminal to the right", splitIcon, 15, 5, splitDisabled, a.splitTerminalRight)
			flatIconButton(c, "Split terminal below", "Open another independent terminal below", splitBelowIcon, 15, 5, splitDisabled, a.splitTerminalBelow)
			maximizeLabel, maximizeButtonIcon := "Maximize terminal", expandIcon
			if a.term.panelMaximized {
				maximizeLabel, maximizeButtonIcon = "Restore terminal size", shrinkIcon
			}
			flatIconButton(c, maximizeLabel, maximizeLabel, maximizeButtonIcon, 15, 5, false, a.toggleTerminalMaximized)
			// Hide never closes a terminal: all processes and their scrollback
			// stay in app state for the next time the pane opens.
			flatIconButton(c, "Hide terminal", "Keep terminal processes running", chevronDownIcon, 15, 5, false, a.hideTerminalView)
			closeLabel := "Close terminal"
			if a.terminalSessionCount() > 1 {
				closeLabel = "Close all terminals"
			}
			flatIconButton(c, closeLabel, closeLabel, closeIcon, 15, 5, false, a.requestCloseAllTerminals)
		})
		if len(a.term.sessions) > 0 {
			a.terminalSessionsView(c)
		}
	})
}

// terminalTabsView puts top-level selection, renaming and closing in the main
// toolbar. Group names are independent; defaults use the selected shell.
func (a *app) terminalTabsView(c *ui.Context) {
	theme := c.Theme()
	activeGroup := a.activeTerminalGroup()
	ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(3).Label(tr("Terminal tabs")).Children(func() {
		for _, group := range a.terminalGroups() {
			session := a.terminalGroupSession(group)
			if session == nil {
				continue
			}
			c.Key(fmt.Sprintf("terminal-group-%d", group))
			tab := ui.Row(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(2).Padding(1, 3).Radius(theme.Radius)
			if group == activeGroup || tab.Hovered() {
				tab.Background(theme.SurfaceHover)
			}
			tab.Children(func() {
				button := ui.ButtonBase(c).Grow(1).MinWidth(0).AlignItems(ui.Center).Gap(5).Padding(3, 5).Radius(theme.Radius).
					Label(fmt.Sprintf("%s %d", tr("Terminal tab"), group)).Tooltip(tr("Switch terminal tabs"))
				button.PopoverMenu(func(m *ui.Menu) { a.terminalGroupMenu(m, group) })
				button.Children(func() {
					ui.Icon(c, terminalIcon).FontSize(13).TextColor(theme.TextMuted)
					ui.Text(c, a.terminalGroupLabel(group, session)).Bold().SingleLine()
					if detail := terminalSessionDetail(session); detail != "" {
						ui.Text(c, detail).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).SingleLine().Grow(1).MinWidth(0)
					} else {
						ui.Spacer(c).Grow(1)
					}
					if count := a.terminalGroupSize(group); count > 1 {
						ui.Badge(c, fmt.Sprintf("%d", count))
					}
				})
				if button.Clicked() {
					a.term.pendingGroup = group
				}
				renameLabel := fmt.Sprintf("%s %d", tr("Rename tab"), group)
				flatIconButton(c, renameLabel, renameLabel, editIcon, 12, 2, false, func() {
					a.openTerminalGroupRename(group)
				})
				closeLabel := fmt.Sprintf("%s %d", tr("Close terminal tab"), group)
				flatIconButton(c, closeLabel, closeLabel, closeIcon, 12, 2, false, func() {
					a.requestCloseTerminalGroup(group)
				})
			})
		}
	})
}

func (a *app) terminalGroupMenu(m *ui.Menu, group uint64) {
	session := a.terminalGroupSession(group)
	if session == nil {
		return
	}
	if m.Item(tr("Rename tab")).Chosen() {
		a.openTerminalGroupRename(group)
	}
	m.Separator()
	if m.Item(tr("Close terminal tab")).Chosen() {
		a.requestCloseTerminalGroup(group)
	}
}

// terminalSessionView builds one card from a stable snapshot. Drops queue a
// move for the next build instead of changing which shell this card renders.
func (a *app) terminalSessionView(c *ui.Context, index int) {
	if index < 0 || index >= len(a.term.sessions) {
		return
	}
	session := a.term.sessions[index]
	c.Key(fmt.Sprintf("terminal-card-%d", session.id))
	theme := c.Theme()
	number := terminalSessionNumber(&session)
	title := terminalSessionLabel(&session)
	card := islandCard(c, ui.Column(c).Grow(1).Gap(6).Padding(6, 10)).Label(title)
	if dragged, ok := ui.Drop[terminalTabDrag](card); ok {
		a.term.pendingMoveID = dragged.id
		a.term.pendingMoveTarget = index
	}
	dragged, over := ui.DragOver[terminalTabDrag](card)
	targeted := over && dragged.id != session.id
	if targeted {
		card.Border(1, theme.Accent)
	}
	card.Children(func() {
		// The maximized dock already names its shells in the main toolbar. Drop
		// this redundant card header so the terminal buffer gets the extra rows.
		if !a.term.panelMaximized {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				tab := ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(2).Padding(2, 4).Radius(theme.Radius).
					Drag(terminalTabDrag{id: session.id})
				tab.Label(tr("Select terminal") + fmt.Sprintf(" %d", number)).Tooltip(tr("Drag to reorder terminals"))
				if index == a.term.active || tab.Hovered() {
					tab.Background(theme.SurfaceHover)
				}
				tab.Children(func() {
					button := ui.ButtonBase(c).Grow(1).MinWidth(0).Gap(6).Padding(2).Radius(theme.Radius)
					button.Label(tr("Focus terminal") + fmt.Sprintf(" %d", number))
					button.PopoverMenu(func(m *ui.Menu) { a.terminalSessionMenu(m, session.id) })
					button.Children(func() {
						ui.Icon(c, terminalIcon).FontSize(13).TextColor(theme.TextMuted)
						ui.Text(c, title).Bold().SingleLine()
						if detail := terminalSessionDetail(&session); detail != "" {
							ui.Text(c, detail).TextColor(theme.TextMuted).SingleLine().Grow(1)
						}
					})
					if button.Clicked() {
						a.term.active = index
					}
					renameLabel := fmt.Sprintf("%s %d", tr("Rename terminal"), number)
					flatIconButton(c, renameLabel, renameLabel, editIcon, 13, 4, false, func() {
						a.openTerminalRename(session.id)
					})
					closeLabel := fmt.Sprintf("%s %d", tr("Close terminal"), number)
					flatIconButton(c, closeLabel, closeLabel, closeIcon, 13, 4, false, func() {
						a.requestCloseTerminalAt(index)
					})
				})
			})
		}
		if targeted {
			ui.Column(c).Grow(1).Center().Gap(6).Margin(2).
				Background(theme.Accent.Alpha(0.08)).Border(1, theme.Accent).Radius(theme.Radius).
				Label(tr("Move terminal here")).Children(func() {
				ui.Icon(c, terminalIcon).FontSize(22).TextColor(theme.Accent)
				ui.Text(c, tr("Move terminal here")).Bold().TextColor(theme.Accent)
			})
			return
		}
		a.terminalSearchView(c, session.id)
		a.terminalSessionContent(c, index, &session)
	})
}

func (a *app) terminalSessionMenu(m *ui.Menu, id uint64) {
	index, session := a.terminalSessionByID(id)
	if session == nil {
		return
	}
	if m.Item(tr("Rename terminal")).Chosen() {
		a.openTerminalRename(id)
	}
	m.Separator()
	if m.Item(tr("Close terminal")).Chosen() {
		a.requestCloseTerminalAt(index)
	}
}

func (a *app) terminalSessionContent(c *ui.Context, index int, session *terminalSessionState) {
	theme := c.Theme()
	if session.err != "" {
		ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Text(c, session.err).TextColor(theme.Danger)
			if ui.Button(c, tr("Try again")).Clicked() {
				a.term.active = index
				a.startTerminalAt(index)
			}
		})
		return
	}
	if session.loading {
		ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Starting the shell"))
			ui.Text(c, tr("Starting the shell…")).TextColor(theme.TextMuted)
		})
		return
	}
	if session.pane != nil {
		ui.Box(c).Grow(1).ObserveInput(func(ev ui.InputEvent) {
			if ev.Kind == ui.InputPointerDown || ev.Kind == ui.InputKeyDown {
				a.term.active = index
			}
		}).Children(func() {
			view := terminal.View(c, session.pane).Grow(1)
			// A find bar opening above the shell owns the initial focus; giving
			// the terminal AutoFocus in that same frame would steal it back.
			if !session.searchOpen {
				view = view.AutoFocus()
			}
			a.keyboardPane(c, fmt.Sprintf("terminal-%d", session.id), view)
		})
	}
}
