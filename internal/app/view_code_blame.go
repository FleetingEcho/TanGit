package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// codeBlameDetailsCard fills editor.State.ShowHover's native interactive
// hover. MyGo anchors it to the active line and keeps it open while the pointer
// or keyboard focus is inside; it neither blocks editing nor opens a modal.
func (a *app) codeBlameDetailsCard(c *ui.Context) {
	hover := &a.codeBlameHover
	if !hover.open {
		return
	}
	theme := c.Theme()
	ui.Column(c).Gap(9).MaxWidth(480).Children(func() {
		ui.Text(c, fmt.Sprintf("%s:%d", hover.path, hover.line)).TextColor(theme.TextMuted).
			FontSize(theme.FontSize - 2).SingleLine().Ellipsis("…").Tooltip(hover.path)
		if uncommittedBlameHash(hover.blame.Hash) {
			ui.Text(c, tr("Uncommitted changes")).Bold()
			ui.Text(c, tr("This line has not been committed yet.")).TextColor(theme.TextMuted)
			ui.Row(c).Justify(ui.End).Children(func() {
				iconTextButtonState(c, "Open in History", tr("This line has not been committed yet."), historyIcon, false,
					true, a.openCodeBlameCommitInHistory)
			})
			return
		}
		subject := hover.commit.Subject
		if subject == "" {
			subject = hover.blame.Subject
		}
		if subject == "" {
			subject = strings.TrimSpace(shortBlameHash(hover.blame.Hash))
		}
		ui.Text(c, subject).Bold()
		ui.Text(c, shortBlameHash(hover.blame.Hash)).TextColor(theme.TextMuted).
			SingleLine().Tooltip(hover.blame.Hash)

		author := hover.commit.AuthorName
		if author == "" {
			author = hover.blame.Author
		}
		email := hover.commit.AuthorEmail
		if email == "" {
			email = hover.blame.AuthorEmail
		}
		if email != "" {
			author += " <" + email + ">"
		}
		date := hover.blame.Date
		if hover.commit.UnixTimestamp > 0 {
			authored := time.Unix(hover.commit.UnixTimestamp, 0)
			date = authored.Format("2006-01-02 15:04") + " · " + codeBlameRelativeTime(hover.commit.UnixTimestamp)
		} else if hover.blame.UnixTimestamp > 0 {
			authored := time.Unix(hover.blame.UnixTimestamp, 0)
			date = authored.Format("2006-01-02 15:04") + " · " + codeBlameRelativeTime(hover.blame.UnixTimestamp)
		}
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, tr("Author")).TextColor(theme.TextMuted).Width(52)
			ui.Text(c, author)
		})
		ui.Row(c).Gap(6).Children(func() {
			ui.Text(c, tr("Date")).TextColor(theme.TextMuted).Width(52)
			ui.Text(c, date)
		})
		if hover.loading {
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Spinner(c).Label(tr("Loading commit details"))
				ui.Text(c, tr("Loading commit details…")).TextColor(theme.TextMuted)
			})
		}
		if hover.loadError != "" {
			ui.Text(c, hover.loadError).TextColor(theme.Warning).Tooltip(hover.loadError)
		}
		historyTooltip := tr("Open this commit in History")
		if !a.sectionVisible(sectionHistory) {
			historyTooltip = tr("History is hidden in Workspace settings")
		}
		ui.Row(c).Justify(ui.End).Children(func() {
			iconTextButtonState(c, "Open in History", historyTooltip, historyIcon, false,
				!a.sectionVisible(sectionHistory), a.openCodeBlameCommitInHistory)
		})
	})
}
