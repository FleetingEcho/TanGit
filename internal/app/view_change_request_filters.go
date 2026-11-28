package app

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/forge"
)

type changeRequestSortChoice struct {
	label string
	field forge.SortField
}

func changeRequestSortChoices(provider forge.Provider) []changeRequestSortChoice {
	choices := []changeRequestSortChoice{
		{"Created date", forge.SortCreated},
		{"Updated date", forge.SortUpdated},
	}
	if provider == forge.GitLab {
		choices = append(choices,
			changeRequestSortChoice{"Merged date", forge.SortMerged},
			changeRequestSortChoice{"Milestone due date", forge.SortMilestoneDue},
			changeRequestSortChoice{"Popularity", forge.SortPopularity},
			changeRequestSortChoice{"Label priority", forge.SortLabelPriority},
			changeRequestSortChoice{"Priority", forge.SortPriority},
			changeRequestSortChoice{"Title", forge.SortTitle},
		)
	}
	return choices
}

func (a *app) openChangeRequestFilters() {
	a.changeRequests.filterDraft = a.changeRequests.filters
	a.changeRequests.filterErr = ""
	a.changeRequests.afterAt = parseFilterDate(a.changeRequests.filters.After)
	a.changeRequests.beforeAt = parseFilterDate(a.changeRequests.filters.Before)
	a.changeRequests.filterOpen = true
	a.loadChangeRequestFacets()
}

// loadChangeRequestFacets fetches the people and labels once per repository, in
// the background, so the dialog opens at once and its fields fill in as they come.
func (a *app) loadChangeRequestFacets() {
	state := &a.changeRequests
	if state.facetsLoaded || state.facetsLoading || a.session == nil {
		return
	}
	runner, ok := a.session.(forgeCommandSession)
	if !ok {
		return
	}
	a.applyHostingHints(a.session)
	state.facetsLoading = true
	state.facetsToken++
	token, generation := state.facetsToken, a.generation
	client := forge.NewCLIClient(runner)
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		facets, err := client.Facets(ctx)
		a.update(func() {
			if token != a.changeRequests.facetsToken || generation != a.generation {
				return
			}
			a.changeRequests.facetsLoading = false
			a.changeRequests.facetsLoaded = err == nil
			a.changeRequests.facets = facets
		})
	}()
}

// changeRequestSuggestions merges what the provider listed with what is
// already known: people and branches seen in the loaded requests, and this
// repository's own branches.
func (a *app) changeRequestSuggestions() (people, branches, labels []string) {
	add := func(list *[]string, value string) {
		if value = strings.TrimSpace(value); value != "" && !slices.Contains(*list, value) {
			*list = append(*list, value)
		}
	}
	for _, name := range a.changeRequests.facets.People {
		add(&people, name)
	}
	for _, name := range a.changeRequests.facets.Labels {
		add(&labels, name)
	}
	for _, item := range a.changeRequests.items {
		add(&people, item.Author)
		add(&branches, item.SourceBranch)
		add(&branches, item.TargetBranch)
		for _, label := range item.Labels {
			add(&labels, label)
		}
	}
	for _, branch := range a.snapshot.Branches {
		if !branch.DetachedHead {
			add(&branches, branch.Name)
		}
	}
	for _, remote := range a.snapshot.Remotes {
		for _, branch := range remote.Branches {
			add(&branches, strings.TrimPrefix(branch.Name, remote.Name+"/"))
		}
	}
	slices.Sort(people)
	slices.Sort(branches)
	slices.Sort(labels)
	return people, branches, labels
}

func (a *app) closeChangeRequestFilters() { a.changeRequests.filterOpen = false }

func parseFilterDate(value string) time.Time {
	at, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Time{}
	}
	return at
}

func formatFilterDate(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format("2006-01-02")
}

// filterDateField is an unset field that reads "Any date" until clicked, then a
// date picker with a button that unsets it again.
func filterDateField(c *ui.Context, label string, value *time.Time, extra func()) {
	theme := c.Theme()
	if value.IsZero() {
		field := ui.ButtonBase(c).Grow(1).Padding(7, 10).Gap(8).
			Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius)
		clicked := field.Label(trf("Set %s", label)).Tooltip(tr("Choose a date")).Clicked()
		field.Children(func() {
			ui.Icon(c, historyIcon).FontSize(13).TextColor(theme.TextMuted)
			ui.Text(c, tr("Any date")).TextColor(theme.TextMuted).Grow(1)
		})
		if clicked {
			*value = parseFilterDate(time.Now().Format("2006-01-02"))
		}
	} else {
		ui.DateInput(c, value).Grow(1).Label(label)
		iconButton(c, trf("Clear %s", label), "Leave this date unset", closeIcon, false, func() {
			*value = time.Time{}
		})
	}
	if extra != nil {
		extra()
	}
}

func (a *app) applyChangeRequestFilters() {
	v := a.changeRequests.filterDraft
	v.After = formatFilterDate(a.changeRequests.afterAt)
	v.Before = formatFilterDate(a.changeRequests.beforeAt)
	if !forge.ValidDate(v.After) || !forge.ValidDate(v.Before) {
		a.changeRequests.filterErr = tr("Dates use the form YYYY-MM-DD.")
		return
	}
	if v.After != "" && v.Before != "" && v.After > v.Before {
		a.changeRequests.filterErr = tr("The start date is after the end date.")
		return
	}
	a.changeRequests.filters = v
	a.changeRequests.filterOpen = false
	a.chooseChangeRequestFilter()
}

func (a *app) clearChangeRequestFilters() {
	a.changeRequests.filters = changeRequestFilterValues{}
	a.changeRequests.filterDraft = changeRequestFilterValues{}
	a.changeRequests.search = ""
	a.changeRequests.filterOpen = false
	a.chooseChangeRequestFilter()
}

// recentDate is today minus days, for the quick date buttons.
func recentDate(days int) string {
	return time.Now().AddDate(0, 0, -days).Format("2006-01-02")
}

func (a *app) changeRequestFilterModal(c *ui.Context) {
	if !a.changeRequests.filterOpen {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(600).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(22).Gap(10).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 8).
				Role(ui.RoleDialog).Label(tr("Filter pull requests"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Icon(c, filterIcon).FontSize(18).TextColor(theme.Accent)
					ui.Text(c, tr("Filter pull requests")).FontSize(18).Bold().Grow(1)
					iconButton(c, "Close", "Escape", closeIcon, false, a.closeChangeRequestFilters)
				})
				draft := &a.changeRequests.filterDraft
				ui.Scroll(c).Shrink(1).MinHeight(0).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						people, branches, labels := a.changeRequestSuggestions()
						field := func(label, hint string, value *string, placeholder string, options []string) {
							hostingSettingRow(c, label, hint, func() {
								a.pickerField(c, label, value, options, placeholder)
							})
						}
						ui.Row(c).Height(20).AlignItems(ui.Center).Children(func() {
							if a.changeRequests.facetsLoading {
								ui.Text(c, tr("Loading people and labels…")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
							}
						})
						field("Author", "Username of who opened it", &draft.Author, tr("Type or pick a username"), people)
						field("Assignee", "Username it is assigned to", &draft.Assignee, tr("Type or pick a username"), people)
						field("Reviewer", "Username asked to review", &draft.Reviewer, tr("Type or pick a username"), people)
						field("Source branch", "The branch with the changes", &draft.Source, tr("Type or pick a branch"), branches)
						field("Target branch", "The branch it merges into", &draft.Target, tr("Type or pick a branch"), branches)
						field("Label", "One label name", &draft.Label, tr("Type or pick a label"), labels)
						hostingSettingRow(c, "Updated after", "Only requests updated on or after this date. Leave unset for no limit.", func() {
							filterDateField(c, tr("Updated after"), &a.changeRequests.afterAt, func() {
								for _, days := range []int{7, 30, 90} {
									days := days
									c.Key("recent:" + strconv.Itoa(days))
									if ui.Button(c, strconv.Itoa(days)+"d").Tooltip(trf("Last %d days", days)).Clicked() {
										a.changeRequests.afterAt = parseFilterDate(recentDate(days))
									}
								}
							})
						})
						hostingSettingRow(c, "Updated before", "Only requests updated on or before this date. Leave unset for no limit.", func() {
							filterDateField(c, tr("Updated before"), &a.changeRequests.beforeAt, nil)
						})
						hostingSettingRow(c, "Drafts", "Include or leave out drafts", func() {
							names := []string{tr("Any"), tr("Only drafts"), tr("Exclude drafts")}
							chosen := names[draft.Draft]
							ui.Select(c, &chosen, names).Label(tr("Draft filter")).Grow(1)
							for index, name := range names {
								if name == chosen {
									draft.Draft = index
								}
							}
						})
						hostingSettingRow(c, "Sort by", "Order returned by the hosting provider", func() {
							choices := changeRequestSortChoices(a.changeRequests.provider)
							field := draft.OrderBy
							if field == "" {
								field = forge.SortUpdated
							}
							fieldNames := make([]string, len(choices))
							chosenField := ""
							for i, choice := range choices {
								fieldNames[i] = tr(choice.label)
								if choice.field == field {
									chosenField = tr(choice.label)
								}
							}
							if chosenField == "" {
								chosenField = tr("Updated date")
							}
							ui.Select(c, &chosenField, fieldNames).Label(tr("Sort field")).Grow(1)
							for _, choice := range choices {
								if tr(choice.label) == chosenField {
									draft.OrderBy = choice.field
								}
							}
							directions := []string{tr("Descending"), tr("Ascending")}
							chosenDirection := directions[0]
							if draft.Sort == forge.SortAscending {
								chosenDirection = directions[1]
							}
							ui.Select(c, &chosenDirection, directions).Label(tr("Sort direction")).Width(150)
							if chosenDirection == directions[1] {
								draft.Sort = forge.SortAscending
							} else {
								draft.Sort = forge.SortDescending
							}
						})
					})
				})
				if a.changeRequests.filterErr != "" {
					ui.Text(c, a.changeRequests.filterErr).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
				}
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Clear all")).Clicked() {
						a.clearChangeRequestFilters()
					}
					if ui.Button(c, tr("Cancel")).Clicked() {
						a.closeChangeRequestFilters()
					}
					if ui.PrimaryButton(c, tr("Apply")).Clicked() {
						a.applyChangeRequestFilters()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeChangeRequestFilters()
			}
		})
	})
}

// pickerField is a text field with a drop-down list: a click or Down shows
// every option, typing narrows them, and any text can still be entered.
func (a *app) pickerField(c *ui.Context, label string, value *string, options []string, placeholder string) {
	theme := c.Theme()
	box := ui.ComboboxBase(c, value)
	box.Input.Label(tr(label)).Placeholder(placeholder).Grow(1).Padding(7, 10).
		Border(1, theme.Border).Radius(theme.Radius).Background(theme.Surface)
	typed := strings.ToLower(strings.TrimSpace(*value))
	var shown []string
	for _, option := range options {
		if typed == "" || strings.Contains(strings.ToLower(option), typed) {
			shown = append(shown, option)
		}
	}
	if len(shown) > 80 {
		shown = shown[:80]
	}
	box.Popup(func(panel ui.Element) {
		panel.Padding(4).Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius).MaxHeight(260)
		if len(shown) == 0 {
			ui.Text(c, tr("No suggestions")).TextColor(theme.TextMuted).Padding(6, 10)
			return
		}
		ui.Scroll(c).Children(func() {
			ui.Column(c).Children(func() {
				for _, option := range shown {
					item := box.Item(option).Padding(6, 10).Radius(theme.Radius - 2)
					if item.Highlighted() {
						item.Background(theme.Accent).TextColor(theme.AccentText)
					}
					item.Children(func() { ui.Text(c, option).SingleLine() })
				}
			})
		})
	})
	if chosen, ok := box.Chosen(); ok {
		*value = chosen
	}
}
