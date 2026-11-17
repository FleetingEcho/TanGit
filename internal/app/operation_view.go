package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func shortOperationSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func operationProgress(state repository.OperationState) string {
	if state.Step > 0 && state.Total > 0 {
		return trf("Step %d of %d", state.Step, state.Total)
	}
	return ""
}

func operationTooltip(state repository.OperationState) string {
	var lines []string
	if len(state.FullRefs) > 0 {
		lines = append(lines, strings.Join(state.FullRefs, "\n"))
	}
	for _, item := range []struct {
		label string
		sha   string
	}{{"onto", state.OntoSHA}, {"orig", state.OrigSHA}, {"incoming", state.IncomingSHA}, {"stopped", state.StoppedSHA}} {
		if item.sha != "" {
			lines = append(lines, item.label+": "+item.sha)
		}
	}
	if state.HistoryPatch != nil {
		lines = append(lines,
			"history kind: "+string(state.HistoryPatch.Kind),
			"history step: "+string(state.HistoryPatch.Step),
			"source: "+state.HistoryPatch.SourceSHA,
			"destination: "+state.HistoryPatch.DestinationSHA,
			"patch digest: "+state.HistoryPatch.PatchDigest,
			"compensation digest: "+state.HistoryPatch.CompensationSHA)
	}
	return strings.Join(lines, "\n")
}

// operationContextView is the compact recovery card above Changes. Every ref,
// SHA and count comes from Git's live operation metadata; no commit list or
// short branch name is used to fill in missing facts.
func (a *app) operationContextView(c *ui.Context) {
	state := a.snapshot.OperationState
	if state == nil {
		return
	}
	theme := c.Theme()
	tooltip := operationTooltip(*state)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(8, 10).
		Background(theme.Accent.Alpha(0.09)).Border(1, theme.Accent.Alpha(0.28)).Radius(theme.Radius).
		Tooltip(tooltip).Children(func() {
		ui.Icon(c, branchIcon).FontSize(16).TextColor(theme.Accent)
		context := strings.Join(state.FullRefs, " · ")
		if context == "" {
			context = shortOperationSHA(state.StoppedSHA)
			if context == "" {
				context = shortOperationSHA(state.OntoSHA)
			}
		}
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
				ui.Text(c, strings.ToUpper(string(state.Type))).Bold().TextColor(theme.Accent)
				if progress := operationProgress(*state); progress != "" {
					ui.Badge(c, progress)
				}
				if state.HistoryPatch != nil {
					ui.Badge(c, tr("Custom Patch")+" · "+string(state.HistoryPatch.Step))
				}
				if state.Done > 0 {
					ui.Text(c, trf("%d completed", state.Done)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				}
			})
			if context != "" {
				ui.Text(c, context).SingleLine().TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Tooltip(tooltip)
			}
			if conflicts := conflictedFileCount(a.snapshot.Files); conflicts > 0 {
				ui.Text(c, fmt.Sprintf(tr("%d conflict%s to resolve"), conflicts, plural(conflicts))+" · "+tr("Pick a side, then Continue.")).
					TextColor(theme.Danger).FontSize(theme.FontSize - 1).SingleLine()
			}
		})
		if a.hasRestorableFileRecovery() {
			rowIconButton(c, tr("Conflict recovery"), tr("Restore an original unresolved file and its index stages"), softResetIcon, func() {
				a.savedWorkPanel = 0
				a.showRecoveryModal = true
			})
		}
	})
}
