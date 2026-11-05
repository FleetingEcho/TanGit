package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type visibleTagRow struct {
	source int
	tag    repository.Tag
}

// visibleTags searches both the tag name and its annotation. Each visible
// row keeps its source index so actions still address snapshot.Tags.
func (a *app) visibleTags() []visibleTagRow {
	query := strings.ToLower(strings.TrimSpace(a.tagFilter))
	rows := make([]visibleTagRow, 0, len(a.snapshot.Tags))
	for i, tag := range a.snapshot.Tags {
		if query == "" || strings.Contains(strings.ToLower(tag.Name), query) ||
			strings.Contains(strings.ToLower(tag.Message), query) {
			rows = append(rows, visibleTagRow{source: i, tag: tag})
		}
	}
	return rows
}

func (a *app) tagAt(row int) (repository.Tag, bool) {
	rows := a.visibleTags()
	if row < 0 || row >= len(rows) {
		return repository.Tag{}, false
	}
	return rows[row].tag, true
}

func (a *app) selectTagRow(row int) {
	a.selectedTagRow = row
	a.selectedTag = -1
	rows := a.visibleTags()
	if row >= 0 && row < len(rows) {
		a.selectedTag = rows[row].source
	}
}

func (a *app) syncTagRow() {
	rows := a.visibleTags()
	for row, tag := range rows {
		if tag.source == a.selectedTag {
			a.selectedTagRow = row
			return
		}
	}
	a.selectTagRow(0)
}

func (a *app) beginCreateTag() {
	name := strings.TrimSpace(a.tagName)
	ref := strings.TrimSpace(a.tagRef)
	message := strings.TrimSpace(a.tagMessage)
	annotated := a.tagAnnotated
	detail := "The tag is lightweight."
	if annotated {
		detail = trf("The tag is annotated: %s", message)
	}
	a.askConfirm(confirmRequest{
		title:        "Create Tag",
		message:      trf("Create tag %s at %s?", name, ref),
		detail:       detail,
		confirmLabel: "Add tag",
		run: func() {
			a.beginRepositoryActionMessage(trf("Tagged %s as %s", ref, name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.CreateTag(ctx, name, ref, message, annotated)
				}, func() {
					a.selectedTag = selectedTagIndex(a.snapshot.Tags, name)
					a.syncTagRow()
					a.tagName = ""
					a.tagRef = ""
					a.tagMessage = ""
					a.tagAnnotated = false
					a.showTagForm = false
				})
		},
	})
}

func (a *app) beginPushTag() {
	if a.selectedTag < 0 || a.selectedTag >= len(a.snapshot.Tags) {
		return
	}
	name := a.snapshot.Tags[a.selectedTag].Name
	remote := strings.TrimSpace(a.tagRemote)
	a.confirmAttention("Push tag", name+" → "+remote, "This publishes a tag to the selected remote. Other users may rely on this published reference.", func() {
		a.beginRepositoryActionMessage(trf("Pushed tag %s to %s", name, remote), "",
			func(ctx context.Context, session repository.Session) error {
				return session.PushTag(ctx, remote, name)
			}, func() {
				a.showTagPushForm = false
			})
	})
}

func (a *app) confirmDeleteSelectedTag() {
	if a.selectedTag < 0 || a.selectedTag >= len(a.snapshot.Tags) {
		return
	}
	a.confirmDeleteTag(a.snapshot.Tags[a.selectedTag])
}

func (a *app) confirmDeleteTag(tag repository.Tag) {
	if a.actionBusy || a.session == nil {
		return
	}
	a.askConfirm(confirmRequest{
		title:        "Delete Tag",
		message:      trf("Delete local tag %s?", tag.Name),
		detail:       "A tag already published to a remote is not removed from that remote.",
		confirmLabel: "Delete",
		destructive:  true,
		run: func() {
			a.beginRepositoryActionMessage(trf("Deleted tag %s", tag.Name), "",
				func(ctx context.Context, session repository.Session) error {
					return session.DeleteTag(ctx, tag.Name)
				}, nil)
		},
	})
}

// tagRowMenu is the right-click menu for a tag row.
func (a *app) tagRowMenu(m *ui.Menu, row ui.ListRow, services ui.Services) {
	rows := a.visibleTags()
	if row.Index < 0 || row.Index >= len(rows) {
		return
	}
	item := rows[row.Index]
	tag := item.tag
	if m.Item(tr("Push tag…")).Shortcut(ui.Shift, ui.KeyP).Disabled(a.actionBusy || len(a.snapshot.Remotes) == 0).Chosen() {
		a.selectedTag, a.selectedTagRow = item.source, row.Index
		if a.tagRemote == "" && len(a.snapshot.Remotes) > 0 {
			a.tagRemote = a.snapshot.Remotes[0].Name
		}
		a.showTagPushForm = true
	}
	m.Separator()
	if m.Item(tr("Copy tag name")).Chosen() {
		a.notifyCopied(services, "tag name", tag.Name)
	}
	m.Separator()
	if m.Item(tr("Delete tag…")).Shortcut(0, ui.KeyD).Disabled(a.actionBusy).Chosen() {
		a.selectedTag, a.selectedTagRow = item.source, row.Index
		a.confirmDeleteTag(tag)
	}
}

func (a *app) tagFormModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.showTagForm, 560, "Create Tag",
		"Name a fixed point in repository history.", tagsIcon, func() {
			ui.Row(c).Gap(10).AlignItems(ui.End).Children(func() {
				ui.TextInput(c, &a.tagName).Label(tr("Tag name")).
					Placeholder(tr("v1.0.0")).Grow(1).AutoFocus()
				ui.TextInput(c, &a.tagRef).Label(tr("Target ref")).
					Placeholder(tr("HEAD")).Width(180)
			})
			ui.Checkbox(c, &a.tagAnnotated, tr("Annotated tag"))
			if a.tagAnnotated {
				ui.TextInput(c, &a.tagMessage).Label(tr("Tag message")).
					Placeholder(tr("Describe this release"))
			}
			ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c, tr("Cancel")).Disabled(a.actionBusy).Clicked() {
					a.showTagForm = false
				}
				invalid := strings.TrimSpace(a.tagName) == "" ||
					(a.tagAnnotated && strings.TrimSpace(a.tagMessage) == "")
				if ui.PrimaryButton(c, tr("Create tag")).Disabled(a.actionBusy || invalid).Clicked() {
					a.beginCreateTag()
				}
			})
		})
}

func (a *app) tagsView(c *ui.Context) {
	theme := c.Theme()
	ui.Column(c).Grow(1).Children(func() {
		a.collapsibleContentPaneSplit(c, &a.filePaneWidth, &a.workspacePaneCollapsed[sectionTags],
			"Expand list pane", func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(8).Padding(12)).Children(func() {
					paneTitleAction(c, tagsIcon, "Repository tags", "Create tag…", "Tag a commit",
						a.actionBusy, func() {
							a.showTagForm = true
							a.tagRef = "HEAD"
						}, func() {
							paneCollapseButton(c, &a.workspacePaneCollapsed[sectionTags], "Collapse list pane")
						})
					if len(a.snapshot.Tags) == 0 {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("No tags")).TextColor(theme.TextMuted)
						})
						return
					}
					visible := a.visibleTags()
					listSearchField(c, &a.sectionSearch[sectionTags], &a.tagFilter,
						"Search repository tags", "Filter by tag name or message", "Clear tag search",
						len(visible), len(a.snapshot.Tags), func() {
							a.selectTagRow(0)
							a.showTagPushForm = false
						}, a.navigateTagSearch)
					visible = a.visibleTags()
					if len(visible) == 0 {
						listSearchEmpty(c, "No repository tags match this search")
						return
					}
					list := a.keyboardList(c, &a.tagList, len(visible)).Grow(1).Rows(func(row ui.ListRow) {
						tag := visible[row.Index].tag
						services := row.Context.Services()
						ui.Column(row.Context).Gap(2).Padding(7, 8).Children(func() {
							ui.Text(row.Context, tag.Name)
							if tag.Message != "" {
								ui.Text(row.Context, tag.Message).TextColor(theme.TextMuted)
							}
						}).PopoverMenu(func(m *ui.Menu) { a.tagRowMenu(m, row, services) })
					})
					if a.keyboardListChanged(&a.tagList, list) {
						a.selectTagRow(a.selectedTagRow)
						a.showTagPushForm = false
					}
					if len(visible) > 0 && a.selectedTagRow < 0 {
						a.selectTagRow(0)
					}
				})
			}, func() {
				islandCard(c, ui.Column(c).Grow(1).Gap(10).Padding(16)).Children(func() {
					if a.selectedTag < 0 || a.selectedTag >= len(a.snapshot.Tags) {
						ui.Column(c).Grow(1).Center().Children(func() {
							ui.Text(c, tr("Select a tag")).TextColor(theme.TextMuted)
						})
						return
					}
					tag := a.snapshot.Tags[a.selectedTag]
					ui.Text(c, tag.Name).FontSize(18).Bold()
					if tag.Message != "" {
						ui.Text(c, tag.Message).TextColor(theme.TextMuted)
					}
					ui.Row(c).Gap(8).Children(func() {
						if ui.PrimaryButton(c, tr("Push tag…")).Disabled(a.actionBusy || len(a.snapshot.Remotes) == 0).Clicked() {
							a.showTagPushForm = true
							if a.tagRemote == "" && len(a.snapshot.Remotes) > 0 {
								a.tagRemote = a.snapshot.Remotes[0].Name
							}
						}
						if ui.Button(c, tr("Delete tag…")).Disabled(a.actionBusy).Clicked() {
							a.confirmDeleteSelectedTag()
						}
					})
					if a.showTagPushForm {
						ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
							ui.TextInput(c, &a.tagRemote).Label(tr("Remote name")).Grow(1)
							if ui.Button(c, tr("Cancel push")).Disabled(a.actionBusy).Clicked() {
								a.showTagPushForm = false
							}
							if ui.PrimaryButton(c, tr("Push selected tag")).Disabled(a.actionBusy || strings.TrimSpace(a.tagRemote) == "").Clicked() {
								a.beginPushTag()
							}
						})
					}
					ui.Column(c).Grow(1).Center().Children(func() {
						ui.Text(c, tr("Tags point to immutable repository history.")).TextColor(theme.TextMuted)
					})
				})
			})
	})
}
