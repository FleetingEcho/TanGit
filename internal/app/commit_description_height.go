package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const commitDescriptionMaxAutoHeight = float32(320)

// commitDescriptionMeasurement is the natural height of one real commit
// message render. No Element is retained beyond the build that produced it.
type commitDescriptionMeasurement struct {
	height float32
	width  float32
}

func (m commitDescriptionMeasurement) valid() bool {
	return m.height > 0 && m.width > 0
}

func commitDescriptionAutomaticHeight(container ui.Element, measured commitDescriptionMeasurement) float32 {
	maximum := commitDescriptionMaxAutoHeight
	if height := container.Bounds().H; height > 0 {
		maximum = min(maximum, max(40, height*0.4))
	}
	minimum := min(commitMessageDefaultHeight, maximum)
	return min(max(measured.height, minimum), maximum)
}

// preferredCommitDescriptionHeight renders one completed message using the
// same raw/Markdown path as the visible pane, then measures its natural size.
// The previous frame's geometry is opt-in only: a new document has no useful
// measurement until the following build has laid it out.
func (a *app) preferredCommitDescriptionHeight(c *ui.Context, commit repository.Commit, state commitDetailViewState, scrollState *ui.ScrollState) commitDescriptionMeasurement {
	state.requestMessage(commit)
	message := state.message
	var measured commitDescriptionMeasurement
	switch {
	case message.showLoading:
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Loading the full commit message…"))
			ui.Text(c, tr("Loading the full commit message…")).TextColor(c.Theme().TextMuted)
		})
		return measured
	case message.loading:
		return measured
	case message.error != "":
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, message.error).TextColor(c.Theme().Danger).SingleLine().Tooltip(message.error).Grow(1).MinWidth(0)
			if ui.Button(c, tr("Retry")).Clicked() {
				message.hash = ""
				state.requestMessage(commit)
			}
		})
		return measured
	case message.message.Description == "":
		return measured
	}
	headerHeight, contentHeight, contentWidth := float32(0), float32(0), float32(0)
	fullscreen := scrollState == &a.diffFullscreenMessageScroll
	bodyColumn := ui.Column(c).Fill().Gap(4)
	if fullscreen && a.diffFullscreenMessageHeight > 0 {
		// The outer metadata scroll does not give its children a height
		// limit. Give the Markdown view its own measured viewport instead,
		// so a long message keeps its established internal scrolling.
		bodyColumn.Height(a.diffFullscreenMessageHeight)
	}
	bodyColumn.Children(func() {
		header := ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, tr("Description")).Bold().TextColor(c.Theme().TextMuted).Grow(1)
			label, icon := "Show raw commit message", fileCodeIcon
			if message.raw {
				label, icon = "Show rendered commit message", fileDocIcon
			}
			flatIconButton(c, label, label, icon, 14, 3, false, func() {
				message.raw = !message.raw
			})
		})
		headerHeight = header.Bounds().H
		mode := "markdown"
		if message.raw {
			mode = "raw"
		}
		c.Key("commit-description:" + commit.Hash + "/" + mode)
		body := ui.Scroll(c).Grow(1).FillWidth().Label(tr("Commit message"))
		if scrollState != nil {
			body.TrackScroll(scrollState)
		}
		body.Children(func() {
			if message.raw {
				ui.Text(c, message.message.Description).Selectable().FillWidth()
				return
			}
			if message.markdown == nil {
				message.markdown = &markdown.Options{WrapCode: true}
			}
			document := markdown.View(c, message.message.Description, message.markdown)
			contentHeight = document.Bounds().H
		})
		contentWidth = body.Bounds().W
		if message.raw && contentWidth > 0 {
			_, contentHeight = c.MeasureText(contentWidth, ui.Span{Text: message.message.Description, Size: c.Theme().FontSize})
		}
	})
	if headerHeight > 0 && contentHeight > 0 {
		measured.height = headerHeight + 4 + contentHeight + commitDetailsPaneGap/2
		measured.width = contentWidth
	}
	if fullscreen && measured.valid() {
		desired := min(max(measured.height, commitMessageDefaultHeight), commitDescriptionMaxAutoHeight)
		if a.diffFullscreenMessageHeight != desired {
			a.diffFullscreenMessageHeight = desired
			c.Invalidate()
		}
	}
	return measured
}

// commitDescriptionSplit keeps manual height changes by commit identity while
// allowing an untouched commit to pick its initial comfortable message size.
func (a *app) commitDescriptionSplit(c *ui.Context, commit repository.Commit, state commitDetailViewState, filesAndDiff func()) ui.Element {
	message := state.message
	if override, ok := message.heightByCommit[commit.Hash]; ok {
		message.height = override
	}
	before := message.height
	var measured commitDescriptionMeasurement
	element := paneSplitVerticalReserve(c, &message.height, diffRegionMinHeight, commitDetailsPaneGap/2,
		func() {
			measured = a.preferredCommitDescriptionHeight(c, commit, state, &message.scroll)
		}, filesAndDiff)
	if message.height != before {
		if message.heightByCommit == nil {
			message.heightByCommit = make(map[string]float32)
		}
		message.heightByCommit[commit.Hash] = message.height
		return element
	}
	if custom := message.heightByCommit[commit.Hash]; custom > 0 {
		return element
	}
	if !measured.valid() {
		return element
	}
	desired := commitDescriptionAutomaticHeight(element, measured)
	if difference := desired - message.height; difference > 0.5 || difference < -0.5 {
		// Bounds belong to the previous frame; this frame is only valid after
		// a second layout. Refresh rather than replacing or storing Elements.
		message.height = desired
		c.Invalidate()
	}
	return element
}
