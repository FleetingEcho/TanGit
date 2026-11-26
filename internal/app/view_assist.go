package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// assistDialog shares the existing read-only patch dialog's bounded frame.
// Both actions keep their body scrollable and footer wrapping, so narrow
// windows never move Accept/Apply outside the viewport.
func (a *app) assistDialog(c *ui.Context, open bool, title, subtitle string, icon *ui.SVG, close func(), body, footer func()) {
	if !open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		c.Key(title)
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(920).Height(700).MaxWidthPercent(94).MaxHeightPercent(94).
				Padding(20).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr(title)).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, icon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr(title)).FontSize(17).Bold()
						ui.Text(c, tr(subtitle)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, tr("Close")+" "+tr(title), tr("Cancel and discard this suggestion preview"), closeIcon, a.actionBusy, close)
				})
				ui.Divider(c)
				ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Label(tr("Suggestion preview")).Children(body)
				ui.Divider(c)
				footer()
			})
			if !a.actionBusy && (panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape)) {
				close()
			}
		})
	})
}

func (a *app) assistProfileEditor(c *ui.Context) {
	ui.Column(c).Gap(6).FillWidth().Children(func() {
		ui.TextInput(c, &a.agents.assistCommand).Label(tr("AssistCommand (optional)")).Placeholder(tr("advisor {requestFile} {responseFile} {artifactDir}"))
		ui.Text(c, tr("Conflict Assistant runs this JSON-file helper only after an explicit request. It receives only isolated request, response, and artifact paths; it never returns a Git patch.")).
			TextColor(c.Theme().TextMuted).FontSize(c.Theme().FontSize - 1)
	})
}

func assistStatus(c *ui.Context, sourceAgent, status string) {
	theme := c.Theme()
	ui.Column(c).Gap(4).Padding(9, 10).Background(theme.Surface).Radius(theme.Radius).Children(func() {
		ui.Textf(c, "%s %s", tr("Suggestion source:"), tr(sourceAgent)).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
		ui.Text(c, tr(status)).FontSize(theme.FontSize - 1)
	})
}

func (a *app) conflictAssistantModal(c *ui.Context) {
	assist := &a.assistance.conflict
	a.assistDialog(c, assist.open, "Conflict Assistant", "Preview region substitutions on a clone. Accept writes Result only and saves V2 recovery.", commandIcon,
		a.closeConflictAssistant, func() {
			assist = &a.assistance.conflict
			theme := c.Theme()
			ui.Column(c).Gap(12).FillWidth().Children(func() {
				a.assistProfileEditor(c)
				assistStatus(c, assist.sourceAgent, assist.status)
				if a.conflictState != nil {
					ui.Textf(c, "%s %s", tr("Conflicted path:"), a.conflictState.path).Selectable()
				}
				if assist.running {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10).Children(func() {
						ui.Spinner(c).Label(tr("Requesting suggestions…"))
						ui.Text(c, tr("The source Result and Git state are read-only while the helper runs.")).TextColor(theme.TextMuted)
					})
				}
				if assist.err != "" {
					ui.Column(c).Gap(5).Padding(10).Background(theme.Danger.Alpha(0.08)).Border(1, theme.Danger.Alpha(0.4)).Radius(theme.Radius).Children(func() {
						ui.Text(c, tr("Suggestions unavailable")).Bold().TextColor(theme.Danger)
						ui.Text(c, assist.err).Selectable()
					})
				}
				if assist.preview == nil && !assist.running && assist.err == "" {
					ui.Column(c).Gap(6).Padding(20).Center().Children(func() {
						ui.Text(c, tr("No suggestions yet")).Bold()
						ui.Text(c, tr("Choose Get region suggestions explicitly. Opening this dialog never runs external AI or changes your files.")).TextColor(theme.TextMuted).TextAlign(ui.Center)
					})
				}
				if assist.preview != nil && len(assist.preview.Artifacts) == 1 {
					artifact := assist.preview.Artifacts[0]
					ui.Text(c, tr("Region suggestions and reasons")).Bold()
					for index, region := range artifact.Regions {
						ui.Column(c).Gap(4).Padding(8, 10).Background(theme.Surface).Radius(theme.Radius).Children(func() {
							ui.Text(c, trf("Region %d · original lines %d–%d", index+1, region.StartLine, region.EndLine)).Bold()
							if region.Excluded {
								ui.Text(c, tr("Excluded; Accept is disabled for this incomplete Result")).TextColor(theme.Warning)
								if region.Reason != "" {
									ui.Textf(c, "%s %s", tr("Reason:"), region.Reason).Selectable()
								}
							} else {
								if region.Summary == "" {
									ui.Text(c, tr("No suggestion reason was supplied.")).TextColor(theme.TextMuted)
								} else {
									ui.Text(c, region.Summary).Selectable()
								}
								ui.Textf(c, "%s %s", tr("Substitution:"), string(region.Substitution)).TextColor(theme.TextMuted)
							}
						})
					}
					ui.Column(c).Gap(5).Children(func() {
						ui.Text(c, tr("Result before/after preview")).Bold()
						ui.Text(c, tr("Computed in memory from a cloned conflict state. No working-tree write, undo edit, stage, continue, commit, or hook has run.")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						if assist.diff != nil {
							editor.DiffView(c, assist.diff).Height(280).FillWidth()
						}
					})
					if !artifact.Complete {
						ui.Text(c, tr("Accept requires a suggestion for every original region; explicit exclusions are preserved only for review.")).TextColor(theme.Warning)
					}
				}
			})
		}, func() {
			assist = &a.assistance.conflict
			ui.Row(c).Justify(ui.End).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
				if ui.Button(c, tr("Close")).Tooltip(tr("Cancel and discard this suggestion preview")).Disabled(a.actionBusy).Clicked() {
					a.closeConflictAssistant()
				}
				if assist.running {
					if ui.Button(c, tr("Cancel request")).Tooltip(tr("Retire this generation; a late response cannot be accepted")).Clicked() {
						a.cancelConflictAdviceRequest()
					}
					return
				}
				getDisabled := a.actionBusy || a.conflictState == nil || !a.assistCommandConfigured()
				getTooltip := tr("Request region suggestions from the configured JSON-file helper")
				if !a.assistCommandConfigured() {
					getTooltip = tr("Add an AssistCommand above; no external helper runs without one")
				}
				if ui.Button(c, tr("Get region suggestions")).Tooltip(getTooltip).Disabled(getDisabled).Clicked() {
					a.beginConflictAdvice()
				}
				acceptTooltip := tr("Validate path, raw bytes, index stages, and operation fingerprint; then write Result only")
				if assist.preview != nil && len(assist.preview.Artifacts) == 1 && !assist.preview.Artifacts[0].Complete {
					acceptTooltip = tr("Accept is unavailable while any region is explicitly excluded")
				}
				if ui.PrimaryButton(c, tr("Accept Result only")).Tooltip(acceptTooltip).Disabled(!a.conflictAdviceComplete() || a.actionBusy).Clicked() {
					a.acceptConflictAdvice()
				}
			})
		})
}
