package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

const overviewPeriodLayout = "2006-01-02 15:04"

func overviewPresetLabel(days int) string {
	switch days {
	case 1:
		return tr("1 day")
	case 7:
		return tr("1 week")
	case 14:
		return tr("2 weeks")
	case 30:
		return tr("1 month")
	default:
		return trf("%d days", days)
	}
}

func (o *overviewState) overviewReadPeriod(now time.Time) overviewPeriod {
	if o.period != nil {
		return overviewPeriod{From: o.period.From, To: o.period.To.Add(time.Minute - time.Second)}
	}
	days := overviewRanges[o.rangeIdx]
	from := now.AddDate(0, 0, -days)
	if days == 30 {
		lastDay := time.Date(now.Year(), now.Month(), 0, 0, 0, 0, 0, now.Location()).Day()
		from = time.Date(now.Year(), now.Month()-1, min(now.Day(), lastDay),
			now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
	}
	return overviewPeriod{From: from, To: now}
}

func (a *app) selectOverviewPreset(o *overviewState, index int) {
	o.rangeIdx, o.period = index, nil
	a.saveOverviewRange()
	a.reloadOverview()
}

func (a *app) openOverviewPeriod(o *overviewState) {
	period := o.overviewReadPeriod(time.Now())
	if o.period != nil {
		period = *o.period
	}
	period.From = period.From.Local().Truncate(time.Minute)
	period.To = period.To.Local().Truncate(time.Minute)
	o.periodDraft = &period
}

func (a *app) overviewRangeControl(c *ui.Context, o *overviewState) {
	theme := c.Theme()
	if o.searching() {
		ui.Text(c, tr("All time")).Padding(4, 8).SingleLine().TextColor(theme.TextMuted)
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		if period := o.period; period != nil {
			label := period.From.Local().Format(overviewPeriodLayout) + " – " + period.To.Local().Format(overviewPeriodLayout)
			button := compactTextButton(ui.Button(c, "")).Label(label).Tooltip(tr("Custom period")).
				OnClick(func() { a.openOverviewPeriod(o) })
			button.Children(func() { ui.Text(c, label).SingleLine().FontSize(theme.FontSize - 1) })
		}
		dropdown := compactTextButton(ui.Button(c, "")).Label(tr("Time range")).Gap(6)
		if o.period != nil {
			dropdown = compactIconButton(dropdown)
		}
		dropdown.PopoverMenuButton(func(menu *ui.Menu) {
			for index, days := range overviewRanges {
				if menu.Item(overviewPresetLabel(days)).Checked(o.period == nil && index == o.rangeIdx).Chosen() {
					a.selectOverviewPreset(o, index)
				}
			}
			menu.Separator()
			if menu.Item(tr("Custom period")).Chosen() {
				a.openOverviewPeriod(o)
			}
		}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd})
		dropdown.Children(func() {
			if o.period == nil {
				ui.Text(c, overviewPresetLabel(overviewRanges[o.rangeIdx])).SingleLine()
			}
			ui.Icon(c, chevronDownIcon).FontSize(13)
		})
	})
}

func (a *app) overviewPeriodModal(c *ui.Context, o *overviewState) {
	draft := o.periodDraft
	if draft == nil {
		return
	}
	theme := c.Theme()
	closeModal := func() { o.periodDraft = nil }
	apply := func() {
		if !draft.valid() {
			return
		}
		period := *draft
		o.period = &period
		closeModal()
		a.saveOverviewRange()
		a.reloadOverview()
	}
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().
			Background(ui.RGBA(0, 0, 0, 0.45)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(460).MaxWidthPercent(94).Padding(18).Gap(12).
				Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Custom period"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
					ui.Text(c, tr("Custom period")).FontSize(16).Bold().Grow(1)
					iconButton(c, "Close", "Close (Esc)", closeIcon, false, closeModal)
				})
				overviewPeriodField(c, "From", &draft.From)
				overviewPeriodField(c, "To", &draft.To)
				if !draft.valid() {
					ui.Text(c, tr("From must be on or before To.")).TextColor(theme.Danger)
				}
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					ui.Button(c, tr("Cancel")).OnClick(closeModal)
					ui.PrimaryButton(c, tr("Apply")).Disabled(!draft.valid()).OnClick(apply)
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				closeModal()
			}
		})
	})
}

func overviewPeriodField(c *ui.Context, caption string, value *time.Time) {
	label := tr(caption)
	ui.Column(c).FillWidth().Gap(5).Children(func() {
		ui.Text(c, label).Bold()
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			ui.DateInput(c, value).Grow(3).Label(trf("%s date", label))
			ui.TimeInput(c, value).Grow(2).Label(trf("%s time", label))
		})
	})
}
