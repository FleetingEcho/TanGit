package app

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui/editor"
	gitpatch "github.com/jesseduffield/lazygit/pkg/commands/patch"

	"lazygitapp/internal/agentassist"
	"lazygitapp/internal/repository"
)

type changeSplitGroup struct {
	id           string
	summary      string
	patch        string
	local        bool
	preview      *repository.PatchPreview
	previewError string
}

type changeSplitUIState struct {
	open       bool
	running    bool
	generation uint64
	cancel     context.CancelFunc

	file        repository.File
	request     agentassist.AdviceRequest
	groups      []changeSplitGroup
	exclusions  []agentassist.Exclusion
	sourceAgent string
	status      string
	err         string
}

func (a *app) selectedChangeSplitFile() (repository.File, bool) {
	change, ok := a.selectedChange()
	if !ok || change.staged || change.merge || !change.file.Unstaged || change.file.HasMergeConflicts {
		return repository.File{}, false
	}
	file := change.diffFile()
	return file, true
}

func (a *app) openChangeSplitter() {
	a.closeChangeSplitter()
	state := &a.assistance.splitter
	state.open = true
	state.sourceAgent = a.assistSourceBeforeRequest()
	state.status = "No split suggestions requested"
	if file, ok := a.selectedChangeSplitFile(); ok {
		state.file = file
	} else {
		state.err = "Choose the unstaged side of a non-conflicted changed file."
	}
}

func (a *app) closeChangeSplitter() {
	state := &a.assistance.splitter
	state.generation++
	if state.cancel != nil {
		state.cancel()
	}
	a.assistance.splitter = changeSplitUIState{generation: state.generation}
}

func (a *app) cancelChangeSplitRequest() {
	state := &a.assistance.splitter
	if !state.open {
		return
	}
	state.generation++
	if state.cancel != nil {
		state.cancel()
	}
	state.cancel = nil
	state.running = false
	state.groups = nil
	state.exclusions = nil
	state.status = "Request canceled; nothing was staged"
	state.err = ""
}

func binaryDiffPatch(text string) (bool, error) {
	parsed, err := editor.ParsePatch(text)
	if err != nil {
		return false, err
	}
	if len(parsed) != 1 {
		return false, fmt.Errorf("canonical file diff contains %d files instead of one", len(parsed))
	}
	return parsed[0].Binary, nil
}

// changeSplitPatch transforms only identities that survived agentassist's
// exhaustive coverage and duplicate checks. An empty hunk set represents a
// whole-file/mode-only selection; a line/hunk selection never falls back to the
// original whole patch unless it genuinely covers every change.
func changeSplitPatch(source repository.File, sourcePatch string, group agentassist.SplitPreview) (string, error) {
	if len(group.Files) != 1 {
		return "", fmt.Errorf("split group %q covers %d files; expected only %s", group.SuggestionID, len(group.Files), source.Path)
	}
	file := group.Files[0]
	if file.Path != source.Path {
		return "", fmt.Errorf("split group %q path %q does not match source %q", group.SuggestionID, file.Path, source.Path)
	}
	identities := make([]gitpatch.LineIdentity, 0)
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case agentassist.LineAdd:
				identities = append(identities, gitpatch.LineIdentity{LineNumber: line.NewLine})
			case agentassist.LineDelete:
				identities = append(identities, gitpatch.LineIdentity{LineNumber: line.OldLine, IsDeletion: true})
			}
		}
	}
	if len(file.Hunks) == 0 {
		return sourcePatch, nil
	}
	generated, whole, err := partialPatchForIdentities(sourcePatch, source, identities, false)
	if err != nil {
		return "", err
	}
	if whole {
		return sourcePatch, nil
	}
	if generated == "" {
		return "", fmt.Errorf("split group %q generated no apply-ready patch", group.SuggestionID)
	}
	return generated, nil
}

func buildChangeSplitGroups(source repository.File, sourcePatch string, preview agentassist.Preview) []changeSplitGroup {
	groups := make([]changeSplitGroup, len(preview.Splits))
	for index, split := range preview.Splits {
		groups[index] = changeSplitGroup{id: split.SuggestionID, summary: split.Summary}
		patch, err := changeSplitPatch(source, sourcePatch, split)
		if err != nil {
			groups[index].previewError = safeError(err)
			continue
		}
		groups[index].patch = patch
	}
	return groups
}

func (a *app) beginChangeSplitAdvice() {
	assist := &a.assistance.splitter
	if !assist.open || assist.running || a.actionBusy || a.session == nil {
		return
	}
	file := assist.file
	selected, ok := a.selectedChangeSplitFile()
	if !ok || selected.Path != file.Path || selected.Staged != file.Staged {
		assist.err = "The selected unstaged source changed. Reopen Change Splitter."
		return
	}
	file = selected
	assist.file = file
	assist.err = ""
	assist.groups = nil
	assist.exclusions = nil
	assist.generation++
	generation := assist.generation
	if assist.cancel != nil {
		assist.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	assist.cancel = cancel
	assist.running = true
	assist.status = "Loading canonical source diff"
	session := a.session
	injected, factory, command := a.assistance.advisor, a.assistance.advisorFactory, a.agents.assistCommand
	go func() {
		defer a.recoverBackground()
		sourcePatch, runErr := session.WorkingTreeFilePatch(ctx, file)
		binary := false
		localOnly := false
		var request agentassist.AdviceRequest
		var groups []changeSplitGroup
		var exclusions []agentassist.Exclusion
		sourceAgent := "TanGit local preview"
		if runErr == nil && sourcePatch == "" {
			runErr = fmt.Errorf("the selected file has no unstaged diff to split")
		}
		if runErr == nil {
			binary, runErr = binaryDiffPatch(sourcePatch)
		}
		if runErr == nil && binary {
			// Binary bytes stay local. No helper/configuration is consulted and
			// the canonical binary patch is one indivisible whole-file group.
			localOnly = true
			sourceAgent = "TanGit local whole-file; helper not run"
			groups = []changeSplitGroup{{
				id: "binary-whole-file", summary: "Whole binary file (local, not sent to advisor)",
				patch: sourcePatch, local: true,
			}}
		}
		if runErr == nil && !binary {
			request, runErr = agentassist.NewRequest(agentassist.KindChangeSplit, []agentassist.Artifact{
				agentassist.NewDiffArtifact(sourcePatch),
			})
		}
		if runErr == nil && !binary {
			var advisor agentassist.Advisor
			advisor, sourceAgent, runErr = newAssistAdvisor(injected, factory, command)
			var advice agentassist.Advice
			if runErr == nil {
				advice, runErr = advisor.Propose(ctx, request.Clone())
			}
			var preview agentassist.Preview
			if runErr == nil {
				preview, runErr = agentassist.BuildPreview(request, advice)
			}
			if runErr == nil {
				groups = buildChangeSplitGroups(file, sourcePatch, preview)
				exclusions = append([]agentassist.Exclusion(nil), preview.Exclusions...)
			}
		}

		// Every group is preflighted by Git against the same frozen source.
		// Apply still repeats preview+state validation immediately before its
		// explicit index-only mutation; these previews never grant broad writes.
		if runErr == nil {
			for index := range groups {
				if groups[index].previewError != "" {
					continue
				}
				if groups[index].patch == "" {
					continue
				}
				preview, previewErr := session.PreviewPatchRequest(ctx, repository.PatchRequest{
					Text: groups[index].patch, Target: repository.PatchIndex,
				})
				if previewErr != nil {
					groups[index].previewError = safeError(previewErr)
					continue
				}
				groups[index].preview = &preview
			}
		}
		a.updateChanged(func() bool {
			current := &a.assistance.splitter
			if !current.open || current.generation != generation {
				return false
			}
			current.cancel = nil
			current.running = false
			current.sourceAgent = sourceAgent
			if runErr != nil {
				current.status = "Split advice unavailable"
				current.err = safeError(runErr)
				return true
			}
			selected, selectedOK := a.selectedChangeSplitFile()
			if !selectedOK || selected.Path != file.Path || selected.Staged != file.Staged {
				current.status = "Source changed"
				current.err = "The selected file changed while suggestions were running. Reopen Change Splitter."
				return true
			}
			current.request = request
			current.groups = groups
			current.exclusions = exclusions
			if localOnly {
				current.status = "Local whole-file preview ready; no external helper ran"
			} else if len(groups) == 0 {
				current.status = "No groups; every covered source was explicitly excluded"
			} else {
				current.status = "Validated groups ready; no index or working-tree change made"
			}
			return true
		})
	}()
}

func (a *app) applyChangeSplitGroup(index int) {
	assist := &a.assistance.splitter
	if a.actionBusy || a.session == nil || !assist.open || assist.running || index < 0 || index >= len(assist.groups) {
		return
	}
	group := assist.groups[index]
	if group.patch == "" || group.previewError != "" || group.preview == nil || !group.preview.Clean {
		return
	}
	// Re-preview regardless of how old the displayed preview is. ApplyPatch's
	// private CAS bytes and state digest then authorize only this exact group.
	a.beginRepositoryActionMessage(tr("Staged Change Splitter group"), assist.file.Path,
		func(ctx context.Context, session repository.Session) error {
			return previewAndApplyPatch(ctx, session, repository.PatchRequest{
				Text: group.patch, Target: repository.PatchIndex,
			})
		}, func() {
			a.closeChangeSplitter()
			a.notify("success", tr("Change group staged"), tr("Only this group's index entries changed. The working tree was not changed and no commit was created."))
		})
}
