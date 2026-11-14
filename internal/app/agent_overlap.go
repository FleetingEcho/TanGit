package app

import (
	"context"
	"fmt"
	"strings"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

// agentOverlapUIState is one read-only, task-bound analysis. Results are hints,
// not conflict predictions and never authorize integration or change review state.
type agentOverlapUIState struct {
	open    bool
	loading bool
	token   uint64
	cancel  context.CancelFunc
	report  agentwork.OverlapReport
	errors  []string
}

func (a *app) resetAgentOverlap() {
	a.agents.overlap.token++
	if a.agents.overlap.cancel != nil {
		a.agents.overlap.cancel()
	}
	a.agents.overlap = agentOverlapUIState{token: a.agents.overlap.token}
}

func (a *app) beginAgentOverlapAnalysis() {
	if a.agents.overlap.loading {
		return
	}
	tasks := make([]agentwork.Task, 0, len(a.agents.tasks))
	for _, task := range a.agents.tasks {
		if agentTaskActive(task) {
			tasks = append(tasks, task.Clone())
		}
	}
	if len(tasks) < 2 {
		a.notify("warning", "At least two active agent tasks are required", "Create another task before analyzing parallel change risks.")
		return
	}
	state := &a.agents.overlap
	state.token++
	state.open = true
	state.loading = true
	state.report = agentwork.OverlapReport{}
	state.errors = nil
	if state.cancel != nil {
		state.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	token, generation, repositoryID := state.token, a.generation, a.agentRepositoryID()
	go func() {
		defer a.recoverBackground()
		defer cancel()
		inputs := make([]agentwork.OverlapTaskSnapshot, 0, len(tasks))
		failures := make([]string, 0)
		for _, task := range tasks {
			if ctx.Err() != nil {
				break
			}
			input := agentwork.OverlapTaskSnapshot{TaskID: task.ID, Title: task.Title, BaseCommit: task.BaseCommit}
			err := a.loadAgentOverlapTask(ctx, task, &input)
			if err != nil {
				failures = append(failures, task.Title+": "+safeError(err))
				continue
			}
			inputs = append(inputs, input)
		}
		report := agentwork.AnalyzeOverlaps(inputs)
		a.update(func() {
			current := &a.agents.overlap
			if current.cancel != nil && token == current.token {
				current.cancel = nil
			}
			if generation != a.generation || token != current.token || repositoryID != a.agentRepositoryID() {
				return
			}
			current.loading = false
			current.report = report
			current.errors = failures
		})
	}()
}

// loadAgentOverlapTask reads one explicit target without Snapshot or the
// foreground repository session. Zero context keeps hunk bounds on actual edits.
func (a *app) loadAgentOverlapTask(ctx context.Context, task agentwork.Task, input *agentwork.OverlapTaskSnapshot) error {
	target, err := a.opener.Open(ctx, a.worktreeOpenPath(task.WorktreePath))
	if err != nil {
		return err
	}
	defer closeRepositorySession(target)
	options := repository.DiffOptions{ContextLines: 0}
	files, err := target.FinalFiles(ctx, task.BaseCommit, options)
	if err != nil {
		return err
	}
	input.Files = make([]agentwork.OverlapFileSnapshot, 0, len(files))
	for _, file := range files {
		diff, err := target.FinalFileDiff(ctx, task.BaseCommit, file, options)
		if err != nil {
			return fmt.Errorf("read %s: %w", file.Path, err)
		}
		input.Files = append(input.Files, agentwork.OverlapFileSnapshot{
			Path: file.Path, OldPath: file.PreviousPath, Status: file.Status, Patch: diff.Patch,
		})
	}
	return nil
}

func agentOverlapKindLabel(kind agentwork.OverlapKind) string {
	switch kind {
	case agentwork.OverlapDeleteModify:
		return "Delete/modify hazard"
	case agentwork.OverlapHunkExact:
		return "Overlapping hunks"
	case agentwork.OverlapHunkNearby:
		return "Nearby hunks"
	case agentwork.OverlapGeneratedFile:
		return "Generated-file risk"
	case agentwork.OverlapMigrationFile:
		return "Migration-file risk"
	default:
		return "Shared file"
	}
}

func agentOverlapFindingTitle(finding agentwork.OverlapFinding) string {
	return fmt.Sprintf("%s · %s · %s", finding.TaskA.Title, finding.TaskB.Title, agentOverlapKindLabel(finding.Kind))
}

func agentOverlapFindingDetail(finding agentwork.OverlapFinding) string {
	var details []string
	if finding.FileA.Path != finding.Path || finding.FileA.OldPath != "" {
		details = append(details, "First: "+finding.FileA.Path+" (from "+finding.FileA.OldPath+")")
	}
	if finding.FileB.Path != finding.Path || finding.FileB.OldPath != "" {
		details = append(details, "Second: "+finding.FileB.Path+" (from "+finding.FileB.OldPath+")")
	}
	if finding.HunkA != nil && finding.HunkB != nil {
		details = append(details, finding.HunkA.Header, finding.HunkB.Header)
	}
	if !finding.BaseComparable {
		details = append(details, "Different Base SHAs: hunk positions were not compared.")
	}
	return strings.Join(details, "\n")
}

func agentOverlapDiagnostic(diagnostic agentwork.OverlapDiagnostic) string {
	location := diagnostic.Title
	if diagnostic.Path != "" {
		location += " · " + diagnostic.Path
	}
	return location + ": " + diagnostic.Message
}
