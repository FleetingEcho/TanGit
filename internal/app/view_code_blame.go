package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"lazygitapp/internal/repository"
)

// codeBlameDetailsCard stays inside the editor-owned interactive hover, so
// moving into the card keeps it open and Escape returns to the editor.
func (a *app) codeBlameDetailsCard(c *ui.Context) {
	hover := &a.codeBlameHover
	if !hover.open {
		return
	}
	theme := c.Theme()
	width, height := c.Size()
	ui.Scroll(c).Key(fmt.Sprintf("blame-card-scroll-%d", hover.token)).Label(tr("Commit details")).Width(min(float32(540), max(float32(180), width-48))).
		MaxHeight(max(float32(100), height-64)).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Padding(4).Children(func() {
			if uncommittedBlameHash(hover.blame.Hash) {
				ui.Text(c, tr("Uncommitted changes")).Bold()
				ui.Text(c, tr("This line has not been committed yet.")).TextColor(theme.TextMuted)
				ui.Textf(c, "%s:%d", hover.path, hover.line).TextColor(theme.TextMuted).SingleLine().Ellipsis("…")
				flatIconButton(c, "Open in History", "This line has not been committed yet.", historyIcon, 16, 5, true, a.openCodeBlameCommitInHistory)
				return
			}
			author, email := hover.commit.AuthorName, hover.commit.AuthorEmail
			if author == "" {
				author = hover.blame.Author
			}
			if email == "" {
				email = hover.blame.AuthorEmail
			}
			if author == "" {
				author = email
			}
			timestamp := hover.commit.UnixTimestamp
			if timestamp <= 0 {
				timestamp = hover.blame.UnixTimestamp
			}
			date := hover.blame.Date
			if timestamp > 0 {
				date = time.Unix(timestamp, 0).Format("Jan 2, 2006 · 15:04")
			}
			historyDisabled := !a.sectionVisible(sectionHistory)
			historyTooltip := "Open this commit in History"
			if historyDisabled {
				historyTooltip = "History is hidden in Workspace settings"
			}

			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				a.authorAvatar(c, repository.Commit{Hash: hover.blame.Hash, AuthorName: author, AuthorEmail: email}, 36)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Text(c, author).Bold().SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Tooltip(email)
						if hover.blame.Current {
							ui.Badge(c, tr("You"))
						}
					})
					metadata := shortBlameHash(hover.blame.Hash)
					if relative := codeBlameRelativeTime(timestamp); relative != "" {
						metadata += " · " + relative
					}
					ui.Text(c, metadata).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).SingleLine().Ellipsis("…").Tooltip(date)
				})
				services := c.Services()
				hash := hover.blame.Hash
				flatIconButton(c, "Copy commit hash", "Copy the full commit hash", copyIcon, 16, 5, false,
					func() { a.notifyCopied(services, "commit hash", hash) })
				flatIconButton(c, "Open in History", historyTooltip, historyIcon, 16, 5, historyDisabled, a.openCodeBlameCommitInHistory)
			})
			codeBlameDivider(c)
			subject := hover.commit.Subject
			if subject == "" {
				subject = hover.blame.Subject
			}
			if subject == "" {
				subject = shortBlameHash(hover.blame.Hash)
			}
			ui.Text(c, subject).Bold().FontSize(theme.FontSize + 2)
			if hover.description != "" {
				ui.Text(c, hover.description).TextColor(theme.TextMuted)
			}
			if a.snapshot.HeadHash == hover.blame.Hash || len(hover.commit.Tags) > 0 {
				ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
					if a.snapshot.HeadHash == hover.blame.Hash && a.snapshot.Branch != "" {
						ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, branchIcon).FontSize(13).TextColor(theme.TextMuted)
							ui.Badge(c, a.snapshot.Branch)
						})
					}
					for _, tag := range hover.commit.Tags {
						ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, tagsIcon).FontSize(13).TextColor(theme.TextMuted)
							ui.Badge(c, tag)
						})
					}
				})
			}
			if hover.loading {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Label(tr("Loading commit details"))
					ui.Text(c, tr("Loading commit details…")).TextColor(theme.TextMuted)
				})
			}
			if hover.loadError != "" {
				ui.Text(c, hover.loadError).TextColor(theme.Warning)
			}
			if hover.filesLoaded {
				codeBlameDivider(c)
				label := trf("Changes in this commit (%d)", len(hover.files))
				codeBlameDisclosure(c, label, !hover.filesCollapsed, func() { hover.filesCollapsed = !hover.filesCollapsed })
				if !hover.filesCollapsed {
					ui.Scroll(c).Key("blame-files").Label(tr("Changed files")).FillWidth().Height(144).Shrink(0).Children(func() {
						ui.Column(c).FillWidth().Gap(6).Children(func() {
							for _, file := range hover.files {
								ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
									ui.Icon(c, fileIcon).FontSize(15).TextColor(theme.Accent).Shrink(0)
									path := file.Path
									if file.PreviousPath != "" {
										path = file.PreviousPath + " → " + path
									}
									ui.Text(c, path).Grow(1).Shrink(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(path)
									fileDiffStatCounts(c, file.Path, file.LinesAdded, file.LinesDeleted)
								})
							}
						})
					})
				}
				added, deleted := 0, 0
				for _, file := range hover.files {
					added += file.LinesAdded
					deleted += file.LinesDeleted
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, tr("Files changed")).TextColor(theme.TextMuted).Grow(1)
					ui.Textf(c, "%d", len(hover.files)).TextColor(theme.TextMuted)
					fileDiffStatCounts(c, hover.blame.Hash, added, deleted)
				})
			}
			codeBlameDivider(c)
			codeBlameDisclosure(c, tr("More info"), hover.infoExpanded, func() { hover.infoExpanded = !hover.infoExpanded })
			if hover.infoExpanded {
				identity := author
				if email != "" {
					identity += " <" + email + ">"
				}
				metadata := func() {
					codeBlameInfoRow(c, tr("Author"), strings.TrimSpace(identity), identity)
					codeBlameInfoRow(c, tr("Date"), date, date)
					for _, parent := range hover.commit.Parents {
						codeBlameInfoRow(c, tr("Parent"), shortBlameHash(parent), parent)
					}
				}
				statistics := func() {
					if !hover.filesLoaded {
						return
					}
					added, deleted := 0, 0
					for _, file := range hover.files {
						added += file.LinesAdded
						deleted += file.LinesDeleted
					}
					codeBlameStatRow(c, tr("Files changed"), fmt.Sprint(len(hover.files)), theme.Text)
					codeBlameStatRow(c, tr("Insertions"), fmt.Sprintf("+%d", added), theme.Success)
					codeBlameStatRow(c, tr("Deletions"), fmt.Sprintf("−%d", deleted), theme.Danger)
				}
				if width >= 600 && hover.filesLoaded {
					ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Start).Children(func() {
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(metadata)
						ui.Box(c).Width(1).AlignSelf(ui.Stretch).Background(theme.Border)
						ui.Column(c).Width(170).Shrink(0).Gap(8).Children(statistics)
					})
				} else {
					metadata()
					statistics()
				}
			}
			ui.Textf(c, "%s:%d", hover.path, hover.line).TextColor(theme.TextMuted).
				FontSize(theme.FontSize - 2).SingleLine().Ellipsis("…").Tooltip(fmt.Sprintf("%s:%d", hover.path, hover.line))
		})
	})
}

func codeBlameDivider(c *ui.Context) {
	ui.Box(c).FillWidth().Height(1).Background(c.Theme().Border)
}

func codeBlameDisclosure(c *ui.Context, label string, expanded bool, toggle func()) {
	c.Key(label)
	button := ui.ButtonBase(c).FillWidth().Justify(ui.Start).Gap(7).Padding(0, 0, 2, 0).Label(label)
	button.Children(func() {
		icon := chevronDownIcon
		if !expanded {
			icon = chevronRightIcon
		}
		ui.Icon(c, icon).FontSize(13).TextColor(c.Theme().TextMuted)
		ui.Text(c, label).Bold()
	})
	if button.Clicked() {
		toggle()
	}
}

func codeBlameInfoRow(c *ui.Context, label, value, tooltip string) {
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		ui.Text(c, label).Width(64).TextColor(c.Theme().TextMuted)
		ui.Text(c, value).Shrink(1).MinWidth(0).Tooltip(tooltip).Selectable()
	})
}

func codeBlameStatRow(c *ui.Context, label, value string, color ui.Color) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, label).Grow(1).Shrink(1).MinWidth(0).TextColor(c.Theme().TextMuted)
		ui.Text(c, value).SingleLine().Shrink(0).TextColor(color)
	})
}
