package app

import (
	"fmt"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// newTerminal is a seam so tests can drive the pane without a real shell.
var newTerminal = terminal.New

// terminalPanelPercent is the share of the window height the terminal opens
// at: one third, sitting at the bottom and pushing the workspace up. Its
// divider moves it from there.
const terminalPanelPercent = 100.0 / 3.0

const terminalSessionLimit = 6

// terminalSessionState is one independent shell. Its attempt rejects startup
// and exit callbacks after Close, including a Close while the shell starts.
// name is the user's label; title remains the shell's OSC-reported title.
type terminalSessionState struct {
	id      uint64
	pane    *terminal.Terminal
	loading bool
	err     string
	name    string
	title   string
	attempt uint64
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

// terminalState is the shell pane the header button opens. Hiding it keeps
// every shell running; only an explicit close or a shell's own exit ends one.
type terminalState struct {
	sessions []terminalSessionState
	nextID   uint64
	active   int
	open     bool
	// percent is the share of the window the pane takes when open, which its
	// divider drags. shown is that share as the frame being built lays it out,
	// which a drag moves before the app adopts it.
	percent float32
	shown   float32
	// layout remembers each local split's direction and size independently.
	layout *terminalLayoutNode
	// A drop is applied before the next build, never while its cards are
	// being constructed: moving a session midway through a build would bind
	// that terminal's persistent handle to two views in the same frame.
	pendingMoveID     uint64
	pendingMoveTarget int
	// renameID identifies the terminal in the app-owned rename modal. Stable
	// identity keeps reordering and shell-title callbacks out of the form.
	renameID   uint64
	renameName string
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

func terminalSessionLabel(session *terminalSessionState) string {
	if session.name != "" {
		return session.name
	}
	return fmt.Sprintf("%s %d", tr("Terminal"), terminalSessionNumber(session))
}

func (a *app) terminalSessionCount() int { return len(a.term.sessions) }

// toggleTerminal shows the pane, starting the shell the first time, or hides
// it while keeping every shell and its scrollback for the next time.
func (a *app) toggleTerminal() {
	if a.term.open {
		a.term.open = false
		return
	}
	a.term.open = true
	if a.activeTerminal() == nil {
		a.term.sessions = append(a.term.sessions, terminalSessionState{})
		a.term.active = 0
	}
	if a.activeTerminal().pane == nil {
		a.startTerminal()
	}
	if a.term.layout == nil {
		a.term.layout = &terminalLayoutNode{sessionID: a.activeTerminal().id}
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

func (a *app) splitTerminalRight() { a.splitTerminal(false) }
func (a *app) splitTerminalBelow() { a.splitTerminal(true) }

// splitTerminal opens another independent shell in the requested direction.
// The buttons stay available at the limit so selecting one explains the limit.
func (a *app) splitTerminal(below bool) {
	active := a.activeTerminal()
	if !a.term.open || active == nil || active.pane == nil {
		return
	}
	if a.terminalSessionCount() >= terminalSessionLimit {
		a.notify("error", "Terminal limit reached",
			trf("TanGit keeps up to %d terminal cards open at once. Close a terminal before splitting again.", terminalSessionLimit))
		return
	}
	activeIndex := a.term.active
	activeID := active.id
	insert := activeIndex + 1
	a.term.nextID++
	newID := a.term.nextID
	a.term.sessions = append(a.term.sessions, terminalSessionState{})
	copy(a.term.sessions[insert+1:], a.term.sessions[insert:])
	a.term.sessions[insert] = terminalSessionState{id: newID}
	if a.term.layout == nil {
		a.term.layout = &terminalLayoutNode{sessionID: activeID}
	}
	if !a.term.layout.split(activeID, newID, below) {
		a.term.sessions = append(a.term.sessions[:insert], a.term.sessions[insert+1:]...)
		a.term.active = activeIndex
		return
	}
	a.term.active = insert
	a.startTerminalAt(insert)
}

// closeTerminal exits only the active shell. Hiding is deliberately separate:
// the last Close hides the empty panel, but Hide never terminates a process.
func (a *app) closeTerminal() { a.closeTerminalAt(a.term.active) }

func (a *app) closeTerminalAt(index int) {
	if index < 0 || index >= len(a.term.sessions) {
		return
	}
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
	a.term.sessions = append(a.term.sessions[:index], a.term.sessions[index+1:]...)
	a.term.layout = removeTerminalLayout(a.term.layout, session.id)
	if len(a.term.sessions) == 0 {
		a.term.open = false
		a.term.active = 0
		a.term.pendingMoveID = 0
		a.term.pendingMoveTarget = 0
		return
	}
	if session.id == activeID {
		a.term.active = min(index, len(a.term.sessions)-1)
		return
	}
	if activeIndex, _ := a.terminalSessionByID(activeID); activeIndex >= 0 {
		a.term.active = activeIndex
	}
}

// requestCloseAllTerminals makes a multi-shell exit explicit. Closing one tab
// remains immediate; this guard is only for terminating every shell at once.
func (a *app) requestCloseAllTerminals() {
	count := a.terminalSessionCount()
	if count == 0 {
		a.term.open = false
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
	if source < 0 || session == nil || target < 0 || target >= len(a.term.sessions) || source == target {
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
// repository opens without a pane, title or error left from the one before.
// Its outer height is a preference and stays; the local layout belongs to
// the shells being closed.
func (a *app) resetTerminal() {
	for len(a.term.sessions) > 0 {
		a.closeTerminalAt(0)
	}
	a.term.open = false
	a.term.active = 0
	a.term.pendingMoveID = 0
	a.term.pendingMoveTarget = 0
	a.closeTerminalRename()
}

// terminalButton is the status-bar icon that toggles the pane, in the accent
// color while the pane is open.
func (a *app) terminalButton(c *ui.Context) {
	var button ui.Element
	if a.term.open {
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
		a.toggleTerminal()
	}
}

// terminalPaneView keeps page-wide actions separate from each shell's island.
// Every card owns its title, drag handle, rename and immediate-close buttons.
func (a *app) terminalPaneView(c *ui.Context) {
	if a.term.pendingMoveID != 0 {
		a.moveTerminalTab(a.term.pendingMoveID, a.term.pendingMoveTarget)
		a.term.pendingMoveID = 0
		a.term.pendingMoveTarget = 0
	}
	if len(a.term.sessions) == 0 {
		return
	}
	ui.Column(c).Grow(1).Gap(0).Label(tr("Terminal pane")).Children(func() {
		islandCard(c, ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(4, 8)).Label(tr("Terminal actions")).Children(func() {
			ui.Icon(c, terminalIcon).FontSize(13).TextColor(c.Theme().TextMuted)
			ui.Text(c, tr("Terminal")).Bold()
			if count := a.terminalSessionCount(); count > 1 {
				ui.Badge(c, fmt.Sprintf("%d", count))
			}
			ui.Spacer(c).Grow(1)
			active := a.activeTerminal()
			splitDisabled := active == nil || active.pane == nil
			flatIconButton(c, "Search terminal", "Search the active terminal (Ctrl+F)", searchIcon, 15, 5, splitDisabled, a.openTerminalSearch)
			flatIconButton(c, "Split terminal right", "Open another independent terminal to the right", splitIcon, 15, 5, splitDisabled, a.splitTerminalRight)
			flatIconButton(c, "Split terminal below", "Open another independent terminal below", splitBelowIcon, 15, 5, splitDisabled, a.splitTerminalBelow)
			// Hide never closes a terminal: all processes and their scrollback
			// stay in app state for the next time the pane opens.
			flatIconButton(c, "Hide terminal", "Keep terminal processes running", chevronDownIcon, 15, 5, false, func() {
				a.term.open = false
			})
			closeLabel := "Close terminal"
			if a.terminalSessionCount() > 1 {
				closeLabel = "Close all terminals"
			}
			flatIconButton(c, closeLabel, closeLabel, closeIcon, 15, 5, false, a.requestCloseAllTerminals)
		})
		a.terminalSessionsView(c)
	})
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
				button.Children(func() {
					ui.Icon(c, terminalIcon).FontSize(13).TextColor(theme.TextMuted)
					ui.Text(c, title).Bold().SingleLine()
					if session.title != "" {
						ui.Text(c, session.title).TextColor(theme.TextMuted).SingleLine().Grow(1)
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
					a.closeTerminalAt(index)
				})
			})
		})
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
