package app

import (
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// newTerminal is a seam so tests can drive the pane without a real shell.
var newTerminal = terminal.New

// terminalPanelPercent is the share of the window height the terminal opens
// at: one third, sitting at the bottom and pushing the workspace up. Its
// divider moves it from there.
const terminalPanelPercent = 100.0 / 3.0

// terminalState is the shell pane the header button opens. The shell keeps
// running while the pane is hidden, so reopening returns to the same prompt.
type terminalState struct {
	pane    *terminal.Terminal
	open    bool
	loading bool
	err     string
	title   string
	// percent is the share of the window the pane takes when open, which its
	// divider drags. shown is that share as the frame being built lays it out,
	// which a drag moves before the app adopts it.
	percent float32
	shown   float32
}

// toggleTerminal shows the pane, starting the shell the first time, or hides
// it while keeping the shell alive.
func (a *app) toggleTerminal() {
	if a.term.open {
		a.term.open = false
		return
	}
	a.term.open = true
	if a.term.pane == nil {
		a.startTerminal()
	}
}

// startTerminal launches the user's shell in the repository being browsed.
// The repository's generation is carried along: a shell that starts after its
// repository was left is closed instead of being kept, and its callbacks say
// nothing about the repository that replaced it.
func (a *app) startTerminal() {
	if a.term.loading {
		return
	}
	a.term.loading = true
	a.term.err = ""
	dir := a.snapshot.Root
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		pane, err := newTerminal(terminal.Options{
			Dir: dir,
			OnTitle: func(title string) {
				a.update(func() {
					if generation != a.generation {
						return
					}
					a.term.title = title
				})
			},
			OnExit: func(int) {
				a.update(func() {
					if generation != a.generation {
						return
					}
					a.term.pane = nil
					a.term.open = false
					a.term.loading = false
				})
			},
		})
		a.update(func() {
			if generation != a.generation {
				// The repository changed while the shell started: end the
				// shell rather than keep it for a repository it does not
				// belong to.
				if pane != nil {
					_ = pane.Close()
				}
				return
			}
			a.term.loading = false
			if err != nil {
				a.term.err = err.Error()
				return
			}
			a.term.pane = pane
		})
	}()
}

// closeTerminal ends the shell, as closing the window must.
func (a *app) closeTerminal() {
	if a.term.pane != nil {
		a.term.pane.Close()
		a.term.pane = nil
	}
}

// resetTerminal drops the shell and anything still starting one, so a
// repository opens without a pane, title or error left from the one before.
// The pane's height is a preference and stays.
func (a *app) resetTerminal() {
	a.closeTerminal()
	a.term.open = false
	a.term.loading = false
	a.term.err = ""
	a.term.title = ""
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

// terminalPaneView draws the shell in the pane the divider sizes.
func (a *app) terminalPaneView(c *ui.Context) {
	theme := c.Theme()
	islandCard(c, ui.Column(c).Grow(1).Gap(6).Padding(6, 10)).Label(tr("Terminal pane")).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Icon(c, terminalIcon).FontSize(13).TextColor(theme.TextMuted)
			ui.Text(c, tr("Terminal")).Bold()
			if a.term.title != "" {
				ui.Text(c, a.term.title).TextColor(theme.TextMuted).SingleLine().Grow(1)
			} else {
				ui.Row(c).Grow(1)
			}
			if ui.Button(c, tr("Hide")).Clicked() {
				a.term.open = false
			}
		})
		if a.term.err != "" {
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Text(c, a.term.err).TextColor(theme.Danger)
				if ui.Button(c, tr("Try again")).Clicked() {
					a.startTerminal()
				}
			})
			return
		}
		if a.term.loading {
			ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Starting the shell"))
				ui.Text(c, tr("Starting the shell…")).TextColor(theme.TextMuted)
			})
			return
		}
		if a.term.pane != nil {
			a.keyboardPane(c, "terminal", terminal.View(c, a.term.pane).Grow(1).AutoFocus())
		}
	})
}
