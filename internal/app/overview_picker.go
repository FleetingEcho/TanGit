package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	overviewPickerHeaderHeight = 76
	overviewPickerFooterHeight = 64
	overviewPickerListPadding  = 24
	overviewPickerRowHeight    = 56
	overviewPickerRowGap       = 8
)

// overviewPickerState is the home page's list of saved Overviews, opened by its
// Overview entry. The set is read from disk each time it opens, so it is never
// older than the file.
type overviewPickerState struct {
	open   bool
	config overviewConfig
}

// openOverviewPicker is what the home page's Overview entry does. With saved
// Overviews it lists them; with none there is nothing to choose from, so it goes
// straight to making the first one.
func (a *app) openOverviewPicker() {
	config := loadOverviewConfig(a.dataDir())
	if len(config.Overviews) == 0 {
		a.newOverviewFromPicker()
		return
	}
	a.overviewPicker = overviewPickerState{open: true, config: config}
}

func (a *app) closeOverviewPicker() { a.overviewPicker = overviewPickerState{} }

// chooseOverview opens the Overview tab on one of the saved Overviews.
func (a *app) chooseOverview(id string, intent overviewIntent) {
	a.closeOverviewPicker()
	if a.openOverviewTab != nil {
		a.openOverviewTab(id, intent)
	}
}

// confirmDeleteOverviewByID asks before dropping a saved Overview. Only the saved
// list of repositories goes; the repositories themselves are never touched.
func (a *app) confirmDeleteOverviewByID(id string) {
	def := a.overviewPicker.config.byID(id)
	if def == nil {
		return
	}
	name := def.Name
	a.askConfirm(confirmRequest{
		title:        "Delete Overview",
		message:      trf("Delete the Overview %s?", name),
		detail:       "Only this list of repositories is removed. The repositories themselves are not touched.",
		confirmLabel: "Delete",
		destructive:  true,
		run:          func() { a.deleteOverviewByID(id) },
	})
}

// deleteOverviewByID removes the Overview from the saved file and from the list
// on screen. The Overview tab, if open, reads the file again the next time it is
// shown, so it never keeps a deleted one.
func (a *app) deleteOverviewByID(id string) {
	config := loadOverviewConfig(a.dataDir())
	kept := config.Overviews[:0:0]
	for _, def := range config.Overviews {
		if def.ID != id {
			kept = append(kept, def)
		}
	}
	config.Overviews = kept
	if config.Active == id {
		config.Active = ""
	}
	_ = saveOverviewConfig(a.dataDir(), config)
	if !a.overviewPicker.open {
		return
	}
	a.overviewPicker.config = config
	if len(config.Overviews) == 0 {
		a.closeOverviewPicker()
	}
}

// overviewSummary lists the first few repositories and the total.
func overviewSummary(def overviewDef) string {
	if len(def.Repos) == 0 {
		return tr("No repositories")
	}
	const shown = 3
	names := make([]string, 0, shown)
	for i, repo := range def.Repos {
		if i >= shown {
			break
		}
		names = append(names, repo.displayName())
	}
	summary := strings.Join(names, ", ")
	if extra := len(def.Repos) - shown; extra > 0 {
		summary += " " + trf("+%d more", extra)
	}
	count := trf("%d repositories", len(def.Repos))
	if len(def.Repos) == 1 {
		count = tr("1 repository")
	}
	return summary + " · " + count
}

func (a *app) overviewPickerModal(c *ui.Context) {
	picker := &a.overviewPicker
	if !picker.open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.45)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			rows := len(picker.config.Overviews)
			listHeight := 2*overviewPickerListPadding + rows*overviewPickerRowHeight +
				max(0, rows-1)*overviewPickerRowGap
			// Give the scroll viewport a bounded height while keeping short lists compact.
			const chromeHeight = overviewPickerHeaderHeight + overviewPickerFooterHeight + 4 // border and dividers
			height := float32(chromeHeight + min(listHeight, 376))
			panel := ui.Column(c).Width(560).Height(height).MaxWidthPercent(94).MaxHeightPercent(90).
				Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius+8).
				Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.32)).Clip().
				Role(ui.RoleDialog).Label(tr("Overviews")).
				Children(func() {
					ui.Row(c).Height(overviewPickerHeaderHeight).Shrink(0).Padding(0, 18).
						AlignItems(ui.Center).Gap(12).Children(func() {
						ui.Box(c).Size(36, 36).Shrink(0).Radius(theme.Radius + 3).Center().
							Background(theme.Accent.Alpha(0.15)).Children(func() {
							ui.Icon(c, overviewIcon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Text(c, tr("Overviews")).Grow(1).MinWidth(0).SingleLine().FontSize(18).Bold()
						iconButton(c, "Close overviews", "Close (Esc)", closeIcon, a.confirm != nil,
							a.closeOverviewPicker)
					})
					ui.Divider(c)
					ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Label(tr("Saved Overviews")).Children(func() {
						ui.Column(c).FillWidth().Padding(overviewPickerListPadding, 18).Gap(overviewPickerRowGap).Children(func() {
							for _, def := range picker.config.Overviews {
								a.overviewPickerRow(c, def)
							}
						})
					})
					ui.Divider(c)
					ui.Row(c).Height(overviewPickerFooterHeight).Shrink(0).Padding(0, 18).
						AlignItems(ui.Center).Justify(ui.End).Children(func() {
						ui.PrimaryButton(c, "").Label(tr("New overview")).Height(36).Padding(0, 14).Gap(8).Radius(theme.Radius + 2).
							OnClick(a.newOverviewFromPicker).Children(func() {
							ui.Icon(c, plusIcon).FontSize(15)
							ui.Text(c, tr("New overview")).FontSize(12).SingleLine()
						})
					})
				})
			if a.confirm == nil && a.pickerEdit == nil && (panel.PressedOutside() || a.onOverlayShortcut(back, 0, ui.KeyEscape,
				shortcut("overview/picker-close", "Escape", tr("Close")), nil)) {
				a.closeOverviewPicker()
			}
		})
	})
}

// overviewPickerRow keeps opening, editing and deleting in one card with
// separate controls for each action.
func (a *app) overviewPickerRow(c *ui.Context, def overviewDef) {
	theme := c.Theme()
	c.Key("overview-pick:" + def.ID)
	summary := overviewSummary(def)
	item := ui.Row(c).FillWidth().Height(overviewPickerRowHeight).Shrink(0).
		AlignItems(ui.Center).Gap(4).Padding(5, 8).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius + 3)
	if item.Hovered() {
		item.Background(theme.SurfaceHover)
	}
	item.Children(func() {
		open := ui.ButtonBase(c).Grow(1).MinWidth(0).FillHeight().AlignItems(ui.Center).Justify(ui.Start).
			Gap(10).Padding(0, 8).Radius(theme.Radius).
			Label(trf("Open %s", def.Name)).Tooltip(def.Name + " · " + summary).
			OnClick(func() { a.chooseOverview(def.ID, overviewShow) })
		open.Children(func() {
			ui.Box(c).Size(32, 32).Shrink(0).Radius(theme.Radius).Center().
				Background(theme.Accent.Alpha(0.1)).Children(func() {
				ui.Icon(c, folderIcon).FontSize(17).TextColor(theme.Accent)
			})
			ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, def.Name).FontSize(13).Bold().SingleLine().MinWidth(0)
				ui.Text(c, summary).FontSize(11).TextColor(theme.TextMuted).SingleLine().MinWidth(0)
			})
		})
		flatIconButton(c, "Edit "+def.Name, "Rename this Overview or change its repositories", editIcon, 14, 6, a.confirm != nil,
			func() { a.editOverviewFromPicker(def.ID) })
		flatIconButton(c, "Delete "+def.Name, "Delete this Overview", trashIcon, 14, 6, a.confirm != nil,
			func() { a.confirmDeleteOverviewByID(def.ID) })
		// Open is the last control on the row, at its right edge.
		flatIconButton(c, trf("Open %s", def.Name), "Open this Overview", chevronRightIcon, 14, 6, a.confirm != nil,
			func() { a.chooseOverview(def.ID, overviewShow) })
	})
}
