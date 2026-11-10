package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// commandLogModal is the command log, as Lazygit's log panel: every Git
// command the session ran, newest first, with the output of the ones that
// print any and the error of the ones that failed. It is where a failed
// action's toast sends the user.
func (a *app) commandLogModal(c *ui.Context) {
	if !a.commandLogOpen {
		return
	}
	theme := c.Theme()
	entries := a.commandLogVisibleEntries()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(820).MaxWidthPercent(94).HeightPercent(92).MaxHeightPercent(92).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Command log")).
				Children(func() {
					a.commandLogHeader(c, len(entries))
					ui.Divider(c)
					a.commandLogTools(c)
					ui.Divider(c)
					ui.Scroll(c).Grow(1).Children(func() {
						a.commandLogBody(c, entries)
					})
					ui.Divider(c)
					a.commandLogFooter(c, entries)
				})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCommandLog()
			}
		})
	})
}

// commandLogEntries reads the session's record of commands. The record is
// written by the goroutine running Git, so it is copied before the frame
// walks it.
func (a *app) commandLogEntries() []repository.CommandEntry {
	if a.commandLog == nil {
		return nil
	}
	return a.commandLog.Entries()
}

func (a *app) commandLogHeader(c *ui.Context, count int) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Icon(c, logIcon).FontSize(20).TextColor(theme.Accent)
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, tr("Command log")).FontSize(19).Bold()
			ui.Text(c, tr("Git, hosting and custom commands run in this workspace, with what they printed.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
		ui.Badge(c, fmt.Sprintf("%d", count))
		iconButton(c, "Close command log", "Escape", closeIcon, false, a.closeCommandLog)
	})
}

// commandLogBody lists the commands newest first, so the action just taken
// and the reason it failed are what the pane shows without scrolling.
func (a *app) commandLogBody(c *ui.Context, entries []repository.CommandEntry) {
	theme := c.Theme()
	if len(entries) == 0 {
		query := strings.TrimSpace(a.commandLogQuery)
		message := tr("No commands in this section yet.")
		hint := tr("Your configured and one-off commands appear in Your commands.")
		if !a.commandLogUser {
			hint = tr("Git actions from TanGit appear in Background Git commands.")
		}
		if query != "" {
			message = tr("No commands match your search.")
			hint = tr("Try a different search or switch sections.")
		}
		ui.Column(c).Grow(1).Center().Gap(8).Padding(30).Children(func() {
			ui.Icon(c, logIcon).FontSize(26).TextColor(theme.TextMuted)
			ui.Text(c, message).Bold()
			ui.Text(c, hint).TextColor(theme.TextMuted).TextAlign(ui.Center).MaxWidth(360)
			if query != "" {
				if ui.Button(c, tr("Clear search")).Clicked() {
					a.commandLogQuery = ""
				}
			}
		})
		return
	}
	// The record is a document, not a picture of one: any command or
	// line of output can be selected, with the pointer or the arrows, and
	// copied, as Git's own message is worth pasting elsewhere.
	ui.Column(c).FillWidth().Gap(4).Selectable().Children(func() {
		for i := len(entries) - 1; i >= 0; i-- {
			a.commandLogEntry(c, entries[i])
			ui.Divider(c)
		}
	})
}

func (a *app) commandLogEntry(c *ui.Context, entry repository.CommandEntry) {
	theme := c.Theme()
	color := theme.TextMuted
	if entry.Failed {
		color = theme.Danger
	}
	ui.Column(c).FillWidth().Gap(4).Padding(6, 4).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			if !entry.At.IsZero() {
				ui.Text(c, entry.At.Format("15:04:05")).TextColor(theme.TextMuted).
					FontSize(theme.FontSize - 1).Width(60)
			}
			if entry.Running {
				ui.Text(c, tr("running")).TextColor(theme.Warning).Bold().
					FontSize(theme.FontSize - 1)
			}
			if entry.Failed {
				ui.Text(c, tr("failed")).TextColor(theme.Danger).Bold().
					FontSize(theme.FontSize - 1)
			}
			commandLogHighlighted(c, "$ "+entry.Command, a.commandLogQuery, theme.Text)
		})
		if output := strings.TrimRight(entry.Output, "\n"); output != "" {
			ui.Box(c).FillWidth().Padding(7, 9).Radius(theme.Radius-1).
				Background(theme.Surface).Border(1, theme.Border).Children(func() {
				for line := range strings.SplitSeq(output, "\n") {
					commandLogHighlighted(c, line, a.commandLogQuery, color)
				}
			})
		}
	})
}

func (a *app) commandLogFooter(c *ui.Context, entries []repository.CommandEntry) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, commandCount(len(entries))).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		if ui.Button(c, tr("Copy log")).Disabled(len(entries) == 0).
			Tooltip(tr("Copy every command and its output")).Clicked() {
			c.Services().WriteClipboard(formatCommandLog(entries))
			a.notify("success", "Copied command log",
				trf("%d commands", len(entries)))
		}
		if ui.Button(c, tr("Clear")).Disabled(len(entries) == 0).Clicked() {
			if a.commandLog != nil {
				a.commandLog.Clear()
			}
			c.Invalidate()
		}
		if ui.PrimaryButton(c, tr("Done")).Clicked() {
			a.closeCommandLog()
		}
	})
}

// commandCount names how many commands the log holds, in the singular when it
// holds one.
func commandCount(count int) string {
	if count == 1 {
		return tr("1 command")
	}
	return trf("%d commands", count)
}

// formatCommandLog writes the log as the text the Copy log button puts on the
// clipboard.
func formatCommandLog(entries []repository.CommandEntry) string {
	var text strings.Builder
	for _, entry := range entries {
		if !entry.At.IsZero() {
			fmt.Fprintf(&text, "[%s] ", entry.At.Format("15:04:05"))
		}
		fmt.Fprintf(&text, "$ %s\n", entry.Command)
		if output := strings.TrimRight(entry.Output, "\n"); output != "" {
			text.WriteString(output)
			text.WriteString("\n")
		}
	}
	return text.String()
}
