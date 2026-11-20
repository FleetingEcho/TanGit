package app

import (
	"context"
	"slices"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/agentassist"
	"lazygitapp/internal/repository"
)

// conflictAssistUIState contains only source snapshots and display state. An
// accepted Result remains private inside agentassist.Preview until the explicit
// accept action revalidates it and the repository checks live Git state.
type conflictAssistUIState struct {
	open       bool
	running    bool
	generation uint64
	cancel     context.CancelFunc

	request     agentassist.AdviceRequest
	artifactID  string
	source      repository.ConflictResultRequest
	sourceAgent string
	status      string
	err         string
	before      string
	clone       *conflictState
	preview     *agentassist.Preview
	diff        *editor.DiffState
}

func cloneConflictStateForAssist(source *conflictState) *conflictState {
	clone := newConflictState(source.path, source.text(), source.markerSize)
	clone.originalLines = slices.Clone(source.originalLines)
	clone.selected = source.selected
	clone.undo = make([][]string, len(source.undo))
	for index := range source.undo {
		clone.undo[index] = slices.Clone(source.undo[index])
	}
	return clone
}

func (a *app) openConflictAssistant() {
	a.closeConflictAssistant()
	state := &a.assistance.conflict
	state.open = true
	state.sourceAgent = a.assistSourceBeforeRequest()
	state.status = "No suggestions requested"
}

func (a *app) closeConflictAssistant() {
	state := &a.assistance.conflict
	state.generation++
	if state.cancel != nil {
		state.cancel()
	}
	a.assistance.conflict = conflictAssistUIState{generation: state.generation}
}

// invalidateConflictAssistant keeps the dialog available but drops every
// preview built from an older Result. The accept button cannot survive this.
func (a *app) cancelConflictAdviceRequest() {
	state := &a.assistance.conflict
	if !state.open {
		return
	}
	state.generation++
	if state.cancel != nil {
		state.cancel()
	}
	state.cancel = nil
	state.running = false
	state.preview = nil
	state.clone = nil
	state.diff = nil
	state.status = "Request canceled; nothing was written"
	state.err = ""
}

func (a *app) invalidateConflictAssistant() {
	state := &a.assistance.conflict
	if !state.open {
		return
	}
	state.generation++
	if state.cancel != nil {
		state.cancel()
	}
	state.cancel = nil
	state.running = false
	state.preview = nil
	state.clone = nil
	state.diff = nil
	state.status = "Source changed"
	state.err = "The Result changed after this preview. Request suggestions again."
}

func (a *app) beginConflictAdvice() {
	assist := &a.assistance.conflict
	source := a.conflictState
	if assist.open == false || assist.running || source == nil || a.actionBusy {
		return
	}
	assist.err = ""
	assist.preview = nil
	assist.clone = nil
	assist.diff = nil
	before := source.text()
	request, err := agentassist.NewRequest(agentassist.KindConflict, []agentassist.Artifact{{
		Path: source.path, Text: before, MarkerSize: source.markerSize,
	}})
	if err != nil {
		assist.status = "Source unavailable"
		assist.err = safeError(err)
		return
	}
	advisor, sourceAgent, err := a.advisorForAssistRequest()
	if err != nil {
		assist.status = "Not configured"
		assist.err = safeError(err)
		return
	}

	assist.generation++
	generation := assist.generation
	if assist.cancel != nil {
		assist.cancel()
	}
	ctx, cancel := context.WithCancel(a.repositoryWork.Context())
	assist.cancel = cancel
	assist.running = true
	assist.sourceAgent = sourceAgent
	assist.status = "Requesting validated region suggestions"
	indexDigest := repository.ConflictStagesDigest(a.conflictStages)
	operationFingerprint := snapshotOperationFingerprint(a.snapshot)
	go func() {
		defer a.recoverBackground()
		advice, adviceErr := advisor.Propose(ctx, request.Clone())
		var preview agentassist.Preview
		if adviceErr == nil {
			preview, adviceErr = agentassist.BuildPreview(request, advice)
		}
		a.updateChanged(func() bool {
			current := &a.assistance.conflict
			if !current.open || current.generation != generation {
				return false
			}
			current.cancel = nil
			current.running = false
			if adviceErr != nil {
				current.status = "Advice rejected"
				current.err = safeError(adviceErr)
				return true
			}
			if a.conflictState != source || source.text() != before ||
				repository.ConflictStagesDigest(a.conflictStages) != indexDigest ||
				snapshotOperationFingerprint(a.snapshot) != operationFingerprint {
				current.status = "Source changed"
				current.err = "The path, index, or operation changed while advice was running. Request suggestions again."
				return true
			}
			if len(preview.Artifacts) != 1 {
				current.status = "Advice rejected"
				current.err = "The validated conflict preview does not contain exactly one artifact."
				return true
			}
			clone := cloneConflictStateForAssist(source)
			clone.adoptText(preview.Artifacts[0].Text)
			current.request = request
			current.artifactID = request.Artifacts[0].ID
			current.source = repository.ConflictResultRequest{
				Path: source.path, WorktreeDigest: request.Artifacts[0].Digest,
				IndexDigest: indexDigest, OperationFingerprint: operationFingerprint,
			}
			current.before = before
			current.clone = clone
			current.preview = &preview
			current.status = "Validated suggestions ready; no file or Git state changed"
			a.rebuildConflictAssistDiff()
			return true
		})
	}()
}

func (a *app) rebuildConflictAssistDiff() {
	assist := &a.assistance.conflict
	if assist.clone == nil {
		assist.diff = nil
		return
	}
	state, err := buildDiffState(a.diffLayout(), true, repository.File{Path: assist.source.Path}, repository.Diff{
		OldText: assist.before, NewText: assist.clone.text(),
	}, repository.DiffOptions{ContextLines: 3})
	if err != nil {
		assist.err = safeError(err)
		assist.diff = nil
		return
	}
	state.Font = selectedBufferFont(a.settings.BufferFont, a.settings.BufferFontSize)
	state.LineHeight = float32(a.settings.BufferLineHeight)
	assist.diff = state
}

func (a *app) conflictAdviceComplete() bool {
	assist := &a.assistance.conflict
	return assist.open && !assist.running && assist.preview != nil && assist.clone != nil &&
		len(assist.preview.Artifacts) == 1 && assist.preview.Artifacts[0].Complete && assist.err == ""
}

// acceptConflictAdvice writes only the private reviewed Result. Previewing used
// a clone, so source lines, originalLines and the ordinary undo stack remain
// untouched until Git publication succeeds and beginDiff reloads that Result.
func (a *app) acceptConflictAdvice() {
	assist := &a.assistance.conflict
	if !a.conflictAdviceComplete() || a.actionBusy || a.session == nil {
		return
	}
	result, err := assist.preview.ResolvedCopy(assist.artifactID, assist.request)
	if err != nil {
		assist.status = "Accept refused"
		assist.err = safeError(err)
		return
	}
	if _, ok := a.session.(repository.ConflictResultSession); !ok {
		assist.status = "Accept unavailable"
		assist.err = "This repository session cannot validate and save a conflict Result safely."
		return
	}
	request := assist.source
	request.Result = result
	var recovery repository.FileRecoveryRecord
	a.beginRepositoryActionMessage(tr("Accepted Conflict Assistant Result"), request.Path,
		func(ctx context.Context, session repository.Session) error {
			writer := session.(repository.ConflictResultSession)
			var err error
			recovery, err = writer.WriteConflictResult(ctx, request)
			return err
		}, func() {
			// beginDiff has retired this preview and reloaded the accepted file.
			// Keep only a durable success notification; no undo entry is invented.
			a.closeConflictAssistant()
			a.notify("success", tr("Conflict Result saved"), trf("Only Result changed; index and operation were preserved. V2 recovery: %s", recovery.OperationID))
		})
}
