package app

import (
	"github.com/egoist/mygo/ui"
)

const agentOverlapVisibleFindingLimit = 100

func (a *app) agentOverlapModal(c *ui.Context) {
	a.repositoryFormModal(c, &a.agents.overlap.open, 860, "Parallel change risks",
		"Base-to-final results, including committed and dirty output. Findings are review hints, not predicted merge conflicts.", fileBracesIcon, func() {
			theme := c.Theme()
			state := &a.agents.overlap
			if state.loading {
				ui.Text(c, tr("Analyzing explicit task worktrees…")).TextColor(theme.TextMuted)
				return
			}
			if len(state.errors) > 0 {
				ui.Text(c, tr("Some tasks could not be analyzed; findings below are partial.")).TextColor(theme.Warning)
				ui.Scroll(c).MaxHeight(90).FillWidth().Children(func() {
					ui.Column(c).Gap(4).Children(func() {
						for _, failure := range state.errors {
							ui.Text(c, failure).TextColor(theme.Warning).Selectable()
						}
					})
				})
			}
			if len(state.report.Findings) == 0 {
				ui.Text(c, tr("No shared-path risks found in the analyzed results.")).TextColor(theme.Success)
			} else {
				ui.Textf(c, tr("%d review findings"), len(state.report.Findings)).Bold()
				ui.Scroll(c).MaxHeight(360).FillWidth().Children(func() {
					ui.Column(c).Gap(12).Children(func() {
						for index, finding := range state.report.Findings {
							if index == agentOverlapVisibleFindingLimit {
								ui.Textf(c, tr("Showing the first %d findings; narrow the task set for a smaller report."), agentOverlapVisibleFindingLimit).TextColor(theme.TextMuted)
								break
							}
							ui.Column(c).Gap(3).Children(func() {
								ui.Text(c, agentOverlapFindingTitle(finding)).Bold().Selectable()
								ui.Text(c, finding.Path).TextColor(theme.Accent).Selectable()
								if detail := agentOverlapFindingDetail(finding); detail != "" {
									ui.Text(c, detail).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted).Selectable()
								}
							})
						}
					})
				})
			}
			if len(state.report.Diagnostics) > 0 {
				ui.Text(c, tr("Some patch details could not be interpreted.")).TextColor(theme.Warning)
				ui.Scroll(c).MaxHeight(80).FillWidth().Children(func() {
					ui.Column(c).Gap(3).Children(func() {
						for _, diagnostic := range state.report.Diagnostics {
							ui.Text(c, agentOverlapDiagnostic(diagnostic)).TextColor(theme.TextMuted).Selectable()
						}
					})
				})
			}
			ui.Row(c).Justify(ui.End).Children(func() {
				ui.Button(c, tr("Analyze again")).OnClick(a.beginAgentOverlapAnalysis)
			})
		})
}
