package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/agentassist"
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
		ui.Text(c, tr("Conflict Assistant and Change Splitter run this JSON-file helper only after an explicit request. It receives only isolated request, response, and artifact paths; it never returns a Git patch.")).
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

func (a *app) changeSplitExclusionPath(exclusion agentassist.Exclusion) string {
	assist := &a.assistance.splitter
	for _, artifact := range assist.request.Artifacts {
		if artifact.ID != exclusion.ArtifactID {
			continue
		}
		for _, file := range artifact.Files {
			if file.ID == exclusion.FileID {
				return file.Path
			}
		}
	}
	return assist.file.Path
}

func (a *app) changeSplitterModal(c *ui.Context) {
	assist := &a.assistance.splitter
	a.assistDialog(c, assist.open, "Change Splitter", "Group original diff identities, preview TanGit-generated patches, and stage only one explicitly applied group.", diffSplitIcon,
		a.closeChangeSplitter, func() {
			assist = &a.assistance.splitter
			theme := c.Theme()
			ui.Column(c).Gap(12).FillWidth().Children(func() {
				a.assistProfileEditor(c)
				assistStatus(c, assist.sourceAgent, assist.status)
				if assist.file.Path != "" {
					ui.Textf(c, "%s %s", tr("Unstaged source:"), assist.file.Path).Selectable()
				}
				if assist.running {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(10).Children(func() {
						ui.Spinner(c).Label(tr("Preparing split previews…"))
						ui.Text(c, tr("Git is checking generated patches only; the index and working tree stay untouched.")).TextColor(theme.TextMuted)
					})
				}
				if assist.err != "" {
					ui.Column(c).Gap(5).Padding(10).Background(theme.Danger.Alpha(0.08)).Border(1, theme.Danger.Alpha(0.4)).Radius(theme.Radius).Children(func() {
						ui.Text(c, tr("Split suggestions unavailable")).Bold().TextColor(theme.Danger)
						ui.Text(c, assist.err).Selectable()
					})
				}
				if len(assist.groups) == 0 && !assist.running && assist.err == "" {
					ui.Column(c).Gap(6).Padding(20).Center().Children(func() {
						ui.Text(c, tr("No change groups yet")).Bold()
						ui.Text(c, tr("Request suggestions explicitly. No default AI runs, and uncovered identities must have visible exclusion reasons.")).TextColor(theme.TextMuted).TextAlign(ui.Center)
					})
				}
				for index := range assist.groups {
					group := &assist.groups[index]
					summary := group.summary
					if group.local {
						summary = tr(summary)
					}
					ui.Column(c).Gap(7).Padding(10).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
						ui.Text(c, summary).Bold().Selectable()
						if group.local {
							ui.Text(c, tr("Binary content is never sent to the text helper and can only be applied as a whole file.")).TextColor(theme.Warning)
						}
						switch {
						case group.previewError != "":
							ui.Text(c, group.previewError).TextColor(theme.Danger).Selectable()
						case group.preview == nil:
							ui.Text(c, tr("No CAS preview is available; Apply is disabled.")).TextColor(theme.Warning)
						case !group.preview.Clean:
							ui.Text(c, tr("Git rejected this group against the reviewed state; Apply is disabled.")).TextColor(theme.Danger)
							if group.preview.Diagnostic != "" {
								ui.Text(c, group.preview.Diagnostic).Selectable()
							}
						default:
							ui.Text(c, tr("Git CAS preview is clean · destination: index only")).TextColor(theme.Success)
						}
						if group.preview != nil {
							for _, file := range group.preview.Files {
								ui.Text(c, patchFileLabel(file)).TextColor(theme.TextMuted)
							}
							if group.preview.Text != "" {
								ui.Text(c, tr("TanGit-generated patch preview")).Bold()
								ui.Text(c, group.preview.Text).Selectable().FontSize(theme.FontSize - 1)
								if group.preview.TextTruncated {
									ui.Text(c, tr("The display is truncated; Apply still revalidates the complete internally generated patch.")).TextColor(theme.TextMuted)
								}
							}
						}
						applyDisabled := a.actionBusy || group.patch == "" || group.previewError != "" || group.preview == nil || !group.preview.Clean
						ui.Row(c).Justify(ui.End).Children(func() {
							if ui.PrimaryButton(c, tr("Apply group to index")).Tooltip(tr("Re-preview and stage only this group; keep the working tree unchanged and never commit automatically")).Disabled(applyDisabled).Clicked() {
								a.applyChangeSplitGroup(index)
							}
						})
					})
				}
				if len(assist.exclusions) > 0 {
					ui.Column(c).Gap(6).Children(func() {
						ui.Text(c, tr("Explicitly excluded source identities")).Bold()
						for _, exclusion := range assist.exclusions {
							ui.Column(c).Gap(2).Padding(7, 9).Background(theme.Surface).Radius(theme.Radius).Children(func() {
								ui.Text(c, a.changeSplitExclusionPath(exclusion)).TextColor(theme.TextMuted)
								ui.Text(c, exclusion.Reason).Selectable()
							})
						}
					})
				}
			})
		}, func() {
			assist = &a.assistance.splitter
			ui.Row(c).Justify(ui.End).AlignItems(ui.Center).Gap(8).Wrap().Children(func() {
				if ui.Button(c, tr("Close")).Tooltip(tr("Cancel and discard every unapplied group preview")).Disabled(a.actionBusy).Clicked() {
					a.closeChangeSplitter()
				}
				if assist.running {
					if ui.Button(c, tr("Cancel request")).Tooltip(tr("Retire this generation; late groups cannot become applicable")).Clicked() {
						a.cancelChangeSplitRequest()
					}
					return
				}
				needsConfiguredHelper := !a.assistCommandConfigured() && !a.changeSplitMayBeLocalBinary()
				getTooltip := tr("Load the original canonical diff and request identity groups explicitly")
				if !a.assistCommandConfigured() {
					getTooltip = tr("No external AI is configured. Binary whole-file preview remains available without a command.")
				}
				// A binary file needs no helper at all, so leave the explicit local
				// preview action enabled even with the default empty configuration.
				if ui.Button(c, tr("Get split suggestions")).Tooltip(getTooltip).
					Disabled(a.actionBusy || assist.file.Path == "" || needsConfiguredHelper).Clicked() {
					a.beginChangeSplitAdvice()
				}
			})
		})
}

// changeSplitMayBeLocalbinary determines enablement from an already loaded
// textual diff without doing I/O or invoking an advisor during rendering.
// Unknown/binary sources may always try the explicit canonical read; a known
// textual source still requires configuration.
func (a *app) changeSplitMayBeLocalBinary() bool {
	change, ok := a.selectedChange()
	if !ok || a.diffPatch == "" || change.file.Path != a.assistance.splitter.file.Path {
		return true
	}
	binary, err := binaryDiffPatch(a.diffPatch)
	return err == nil && binary
}
