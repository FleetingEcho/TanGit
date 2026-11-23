package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// indexIcon is a small database drum: the repository's search index.
var indexIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="8" cy="3.8" rx="5" ry="2"/><path d="M3 3.8v8.4c0 1.1 2.2 2 5 2s5-.9 5-2V3.8"/><path d="M3 8c0 1.1 2.2 2 5 2s5-.9 5-2"/></svg>`))

// codeSearchIndexButton shows whether this repository's search index exists, in
// the Code sidebar's header, and builds it when it does not. The icon's color
// is the answer: green for an index that is ready, amber for one that is too
// large, the text color while there is none.
func (a *app) codeSearchIndexButton(c *ui.Context) {
	if a.currentZoektRepositoryRoot() == "" || !codeSearchIndexingSupported() {
		return
	}
	theme := c.Theme()
	state := a.code.searchIndex
	color := theme.TextMuted
	var label, tooltip string
	var run func()
	disabled := false
	switch {
	case !a.zoektCodeSearchEnabled():
		label = tr("Code search index is off")
		tooltip = tr("Code search index is off for this repository. Click to turn it on and build it.")
		run = func() { a.enableZoekt(c) }
	case state.busy || state.status == codeSearchIndexChecking:
		color = theme.Warning
		label = tr("Building the code search index")
		tooltip = tr("Building the code search index… Search uses ripgrep until it is ready.")
	case state.status == codeSearchIndexReady:
		color = theme.Success
		label = tr("Code search index is ready")
		tooltip = trf("Indexed: %d files, %s on disk. Click to check that it is up to date.", state.documents, formatRemoteTransferBytes(state.bytes))
		run = a.startCodeSearchIndex
	case state.status == codeSearchIndexUnavailable:
		color = theme.Warning
		label = tr("Code search index is too large")
		tooltip = trf("This repository is over the %d GiB index limit, so search uses ripgrep. Click to change the limit in Settings.", codeSearchIndexSourceBytesLimit()>>30)
		run = func() { a.openSettingsSection(settingsPageCache) }
	default:
		label = tr("Code search is not indexed")
		tooltip = tr("This repository is not indexed, so search uses ripgrep. Click to build the index.")
		run = a.startCodeSearchIndex
	}
	if len(a.code.smartSkips) > 0 && !a.settings.SmartSearchSkipDisabled {
		tooltip += "\n" + trf("Skipping %d folders: %s", len(a.code.smartSkips), shorten(strings.Join(a.code.smartSkips, ", "), 160))
	}
	button := ui.ButtonBase(c).Padding(4).Radius(theme.Radius).Label(label).Tooltip(tooltip).Disabled(disabled)
	if button.Hovered() && !disabled {
		button.Background(theme.SurfaceHover)
	}
	building := state.busy || state.status == codeSearchIndexChecking
	button.Children(func() {
		const size = 18
		if !building {
			ui.Icon(c, indexIcon).FontSize(14).TextColor(color)
			return
		}
		// While the index is being built the drum fills from the bottom to the
		// top again and again, slowly, until the index is ready.
		fill := button.Keyframes("index-building", []ui.Keyframe{{At: 0, Value: 0}, {At: 2600 * time.Millisecond, Value: 1}},
			ui.KeyframeOptions{Repeat: ui.RepeatForever, NonMotion: true})
		ui.Box(c).Size(size, size).Radius(4).Clip().Children(func() {
			ui.Box(c).Absolute().Left(0).Right(0).Bottom(0).Height(size * fill).Background(color.Alpha(0.4))
			ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Center().Children(func() {
				ui.Icon(c, indexIcon).FontSize(14).TextColor(color)
			})
		})
	})
	if button.Clicked() && run != nil {
		run()
	}
}

// codeSearchIndexOpenDelay is how long a newly opened repository settles before
// its file list is read for the search index, so opening stays quick and
// whatever the user does first comes first.
const codeSearchIndexOpenDelay = 1500 * time.Millisecond

// scheduleCodeSearchIndexOnOpen runs the opening step after a delay. Tests
// replace it to run the step directly.
var scheduleCodeSearchIndexOnOpen = func(delay time.Duration, run func()) {
	time.AfterFunc(delay, run)
}

// startCodeSearchIndexOnOpen gets a newly opened local repository indexed in the
// background: shortly after it opens, its file list is read off the UI thread,
// and the end of that read starts the build. Nothing happens when Zoekt is off
// for the repository or when it was closed in the meantime.
func (a *app) startCodeSearchIndexOnOpen() {
	if !codeSearchIndexingSupported() || a.currentZoektRepositoryRoot() == "" || !a.zoektCodeSearchEnabled() {
		return
	}
	root, generation := a.snapshot.Root, a.generation
	scheduleCodeSearchIndexOnOpen(codeSearchIndexOpenDelay, func() {
		a.updateChanged(func() bool {
			if a.snapshot.Root != root || a.generation != generation || !a.zoektCodeSearchEnabled() {
				return false
			}
			a.ensureCodeFiles()
			return false
		})
	})
}

// startCodeSearchIndex builds the index now, or checks that the one on disk is
// current: it reads the file list first when that is not done yet.
func (a *app) startCodeSearchIndex() {
	state := &a.code.searchIndex
	if !a.code.filesLoaded {
		a.requestCodeFiles()
		return
	}
	if state.busy {
		return
	}
	state.requested = false
	if state.status == codeSearchIndexUnavailable {
		state.status = codeSearchIndexMissing
	}
	a.ensureCodeSearchIndex()
}
