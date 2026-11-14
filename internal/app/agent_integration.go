package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

const (
	agentIntegrationMerged  = "merged"
	agentIntegrationStopped = "stopped"
	agentIntegrationNotRun  = "not_run"
)

type agentIntegrationCapturedTask struct {
	Task         agentwork.Task
	CapturedHead string
	DirtySummary string
}

type agentIntegrationExclusion struct {
	TaskID string
	Title  string
	Reason string
}

type agentIntegrationStep struct {
	TaskID       string
	Title        string
	CapturedHead string
	Status       string
	Detail       string
}

// agentIntegrationUIState owns an explicit, isolated dry run. A captured HEAD
// is data, not task readiness: no task status, BaseCommit, branch, push, or
// foreground checkout is changed by this workflow.
type agentIntegrationUIState struct {
	open            bool
	running         bool
	cleaning        bool
	token           uint64
	cancel          context.CancelFunc
	repositoryID    string
	familyRoot      string
	familyOpenPath  string
	ordered         []agentwork.Task
	selected        map[string]*bool
	baseCommit      string
	worktreePath    string
	worktreeCreated bool
	cleanupDone     bool
	completed       bool
	succeeded       bool
	stopped         bool
	captured        []agentIntegrationCapturedTask
	exclusions      []agentIntegrationExclusion
	steps           []agentIntegrationStep
	err             string
}

type agentIntegrationTaskTarget struct {
	task     agentwork.Task
	openPath string
}

type agentIntegrationExecution struct {
	baseCommit      string
	worktreePath    string
	worktreeCreated bool
	completed       bool
	succeeded       bool
	stopped         bool
	captured        []agentIntegrationCapturedTask
	exclusions      []agentIntegrationExclusion
	steps           []agentIntegrationStep
	err             string
}

// resetAgentIntegration invalidates and cancels background work without
// removing the isolated worktree. Removal is always a separate confirmation.
func (a *app) resetAgentIntegration() {
	state := &a.agentIntegration
	state.token++
	if state.cancel != nil {
		state.cancel()
	}
	a.agentIntegration = agentIntegrationUIState{token: state.token}
}

func (a *app) agentIntegrationFamilyRoot() string {
	root := strings.TrimSpace(a.snapshot.Root)
	for _, worktree := range a.snapshot.Worktrees {
		if worktree.Main && strings.TrimSpace(worktree.Path) != "" {
			return strings.TrimSpace(worktree.Path)
		}
	}
	return root
}

func agentIntegrationOpenPath(location workspace.Location, nativePath string) string {
	if location.IsRemote() {
		location.Root = pathpkg.Clean(nativePath)
		return location.String()
	}
	return nativePath
}

// prepareAgentIntegration offers only active tasks. The order is computed from
// the complete repository family, including archived dependency nodes, while
// archived tasks themselves are never selected or merged implicitly.
func (a *app) prepareAgentIntegration(repositoryID string) error {
	family := agentTaskClones(a.agents.tasks)
	ordered, err := agentwork.TaskOrder(family)
	if err != nil {
		return err
	}
	active := 0
	for _, task := range ordered {
		if agentTaskActive(task) {
			active++
		}
	}
	if active == 0 {
		return errors.New("there are no active agent tasks to integrate")
	}
	state := &a.agentIntegration
	state.open = true
	state.repositoryID = repositoryID
	state.familyRoot = a.agentIntegrationFamilyRoot()
	state.familyOpenPath = agentIntegrationOpenPath(a.location, state.familyRoot)
	state.ordered = nil
	state.selected = make(map[string]*bool)
	state.baseCommit = ""
	state.worktreePath = ""
	state.worktreeCreated = false
	state.cleanupDone = false
	state.completed = false
	state.succeeded = false
	state.stopped = false
	state.captured = nil
	state.exclusions = nil
	state.steps = nil
	state.err = ""
	for _, task := range ordered {
		if !agentTaskActive(task) {
			continue
		}
		state.ordered = append(state.ordered, task.Clone())
		selected := true
		state.selected[task.ID] = &selected
	}
	return nil
}

// beginAgentIntegrationDryRun opens the selection dialog on its first call.
// The dialog calls it again after explicit checkbox selection; that call only
// opens an attention confirmation. No session is opened and no Git write occurs
// until that confirmation is accepted.
func (a *app) beginAgentIntegrationDryRun() {
	state := &a.agentIntegration
	if state.running || state.cleaning || a.actionBusy || a.session == nil {
		return
	}
	repositoryID := a.agentRepositoryID()
	if repositoryID == "" {
		return
	}
	if !state.open {
		// A retained result with a worktree remains inspectable when reopened.
		// Result-free and already-cleaned reports start a fresh selection instead.
		if state.worktreeCreated && state.repositoryID == repositoryID {
			state.open = true
			return
		}
		if err := a.prepareAgentIntegration(repositoryID); err != nil {
			a.notify("warning", "Could not prepare integration dry run", safeError(err))
			return
		}
		return
	}
	if state.completed {
		return
	}
	if state.repositoryID != repositoryID || len(state.ordered) == 0 {
		if err := a.prepareAgentIntegration(repositoryID); err != nil {
			a.notify("warning", "Could not prepare integration dry run", safeError(err))
			return
		}
		return
	}

	selected := make(map[string]bool)
	tasks := make([]agentwork.Task, 0, len(state.ordered))
	for _, task := range state.ordered {
		choice := state.selected[task.ID]
		if choice == nil || !*choice {
			continue
		}
		selected[task.ID] = true
		tasks = append(tasks, task.Clone())
	}
	if len(tasks) == 0 {
		a.notify("warning", "No integration tasks selected", "Select at least one active task explicitly.")
		return
	}
	familyRoot := state.familyRoot
	familyOpenPath := state.familyOpenPath
	generation := a.generation
	a.askConfirm(confirmRequest{
		title:        "Run isolated agent integration",
		message:      trf("Create a detached dry-run worktree and merge %d selected task results?", len(tasks)),
		detail:       tr("This captures each task HEAD as a commit SHA, then merges those SHAs in dependency order. It never merges branch names, pushes, changes task readiness/BaseCommit, or writes to the foreground/task worktrees. Stop and conflict results are kept for inspection."),
		confirmLabel: "Create isolated dry run",
		destructive:  true,
		run: func() {
			if generation != a.generation || repositoryID != a.agentRepositoryID() {
				return
			}
			a.startAgentIntegrationDryRun(repositoryID, familyRoot, familyOpenPath, tasks, selected)
		},
	})
}

func (a *app) startAgentIntegrationDryRun(repositoryID, familyRoot, familyOpenPath string, tasks []agentwork.Task, selected map[string]bool) {
	state := &a.agentIntegration
	if state.running || state.cleaning || state.completed || state.repositoryID != repositoryID {
		return
	}
	targets := make([]agentIntegrationTaskTarget, 0, len(tasks))
	for _, task := range tasks {
		targets = append(targets, agentIntegrationTaskTarget{
			task:     task.Clone(),
			openPath: agentIntegrationOpenPath(a.location, task.WorktreePath),
		})
	}
	state.token++
	state.running = true
	state.err = ""
	state.captured = nil
	state.exclusions = nil
	state.steps = nil
	state.baseCommit = ""
	state.worktreePath = ""
	state.worktreeCreated = false
	state.cleanupDone = false
	state.completed = false
	state.succeeded = false
	state.stopped = false
	if state.cancel != nil {
		state.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	token, generation := state.token, a.generation
	opener, location := a.opener, a.location
	go func() {
		defer a.recoverBackground()
		defer cancel()
		result := executeAgentIntegrationDryRun(ctx, opener, location, familyRoot, familyOpenPath, token, targets, selected)
		a.update(func() {
			current := &a.agentIntegration
			if token == current.token {
				current.cancel = nil
			}
			if generation != a.generation || token != current.token || repositoryID != a.agentRepositoryID() {
				return
			}
			current.running = false
			current.baseCommit = result.baseCommit
			current.worktreePath = result.worktreePath
			current.worktreeCreated = result.worktreeCreated
			current.completed = result.completed
			current.succeeded = result.succeeded
			current.stopped = result.stopped
			current.captured = result.captured
			current.exclusions = result.exclusions
			current.steps = result.steps
			current.err = result.err
		})
	}()
}

func agentIntegrationWorktreePath(root string, remote bool, token uint64) string {
	randomBytes := make([]byte, 6)
	_, _ = rand.Read(randomBytes)
	name := fmt.Sprintf(".tangit-agent-integration-%d-%d-%s", time.Now().UnixNano(), token, hex.EncodeToString(randomBytes))
	if remote {
		return pathpkg.Join(pathpkg.Dir(root), name)
	}
	return filepath.Join(filepath.Dir(root), name)
}

func agentIntegrationDirtySummary(files []repository.File) string {
	if len(files) == 0 {
		return ""
	}
	staged, unstaged, untracked := 0, 0, 0
	for _, file := range files {
		if file.Staged {
			staged++
		}
		if !file.Tracked {
			untracked++
		} else if file.Unstaged {
			unstaged++
		}
	}
	return fmt.Sprintf("dirty output: %d staged, %d unstaged, %d untracked", staged, unstaged, untracked)
}

func captureAgentIntegrationTask(ctx context.Context, opener repositoryOpener, target agentIntegrationTaskTarget) (agentIntegrationCapturedTask, error) {
	session, err := opener.Open(ctx, target.openPath)
	if err != nil {
		return agentIntegrationCapturedTask{}, err
	}
	defer closeRepositorySession(session)
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		return agentIntegrationCapturedTask{}, err
	}
	status, err := session.AgentTargetStatus(ctx, target.task.BaseCommit)
	if err != nil {
		return agentIntegrationCapturedTask{}, err
	}
	if status.HeadSHA == "" || status.BaseSHA == "" {
		return agentIntegrationCapturedTask{}, errors.New("task capture returned an empty HEAD or base SHA")
	}
	if snapshot.HeadHash == "" || snapshot.HeadHash != status.HeadSHA {
		return agentIntegrationCapturedTask{}, errors.New("task HEAD changed or was inconsistent during capture")
	}
	captured := target.task.Clone()
	captured.BaseCommit = status.BaseSHA
	return agentIntegrationCapturedTask{
		Task:         captured,
		CapturedHead: status.HeadSHA,
		DirtySummary: agentIntegrationDirtySummary(snapshot.Files),
	}, nil
}

func executeAgentIntegrationDryRun(ctx context.Context, opener repositoryOpener, location workspace.Location,
	familyRoot, familyOpenPath string, token uint64, targets []agentIntegrationTaskTarget, selected map[string]bool,
) agentIntegrationExecution {
	result := agentIntegrationExecution{}
	source, err := opener.Open(ctx, familyOpenPath)
	if err != nil {
		result.completed, result.stopped, result.err = true, true, safeError(err)
		return result
	}
	defer closeRepositorySession(source)
	if result.baseCommit, err = source.ResolveCommit(ctx, "HEAD"); err != nil {
		result.completed, result.stopped, result.err = true, true, safeError(err)
		return result
	}
	if result.baseCommit == "" {
		result.completed, result.stopped, result.err = true, true, "the integration base resolved to an empty SHA"
		return result
	}

	captures := make(map[string]agentIntegrationCapturedTask, len(targets))
	titles := make(map[string]string, len(targets))
	for _, target := range targets {
		titles[target.task.ID] = target.task.Title
		if err := ctx.Err(); err != nil {
			result.completed, result.stopped, result.err = true, true, safeError(err)
			return result
		}
		captured, captureErr := captureAgentIntegrationTask(ctx, opener, target)
		if captureErr != nil {
			result.exclusions = append(result.exclusions, agentIntegrationExclusion{
				TaskID: target.task.ID, Title: target.task.Title, Reason: "capture failed: " + safeError(captureErr),
			})
			continue
		}
		result.captured = append(result.captured, captured)
		if captured.DirtySummary != "" {
			result.exclusions = append(result.exclusions, agentIntegrationExclusion{
				TaskID: captured.Task.ID, Title: captured.Task.Title, Reason: captured.DirtySummary + "; captured HEAD was not merged",
			})
			continue
		}
		captures[captured.Task.ID] = captured
	}

	// A selected dependent cannot silently authorize or omit a prerequisite.
	// Only an explicit selection whose clean dependency was also selected runs.
	for _, target := range targets {
		if _, ok := captures[target.task.ID]; !ok {
			continue
		}
		for _, dependencyID := range target.task.DependsOn {
			if !selected[dependencyID] {
				result.exclusions = append(result.exclusions, agentIntegrationExclusion{
					TaskID: target.task.ID, Title: target.task.Title,
					Reason: fmt.Sprintf("dependency %s was not explicitly selected", agentShortHash(dependencyID)),
				})
				delete(captures, target.task.ID)
				break
			}
			if _, ok := captures[dependencyID]; !ok {
				result.exclusions = append(result.exclusions, agentIntegrationExclusion{
					TaskID: target.task.ID, Title: target.task.Title,
					Reason: fmt.Sprintf("dependency %s was excluded or unavailable", agentShortHash(dependencyID)),
				})
				delete(captures, target.task.ID)
				break
			}
		}
	}
	if len(captures) == 0 {
		result.completed = true
		result.err = "no selected tasks with explicit clean dependencies remain to integrate"
		return result
	}
	if err := ctx.Err(); err != nil {
		result.completed, result.stopped, result.err = true, true, safeError(err)
		return result
	}

	result.worktreePath = agentIntegrationWorktreePath(familyRoot, location.IsRemote(), token)
	createOptions := repository.WorktreeCreateOptions{Path: result.worktreePath, Base: result.baseCommit, Detach: true}
	if err := source.CreateWorktree(ctx, createOptions); err != nil {
		result.completed, result.stopped, result.err = true, true, safeError(err)
		return result
	}
	result.worktreeCreated = true
	integrationPath := agentIntegrationOpenPath(location, result.worktreePath)
	integration, err := opener.Open(ctx, integrationPath)
	if err != nil {
		result.completed, result.stopped, result.err = true, true, safeError(err)
		return result
	}
	defer closeRepositorySession(integration)

	stopped := false
	for _, target := range targets {
		captured, ok := captures[target.task.ID]
		if !ok {
			continue
		}
		step := agentIntegrationStep{TaskID: target.task.ID, Title: target.task.Title, CapturedHead: captured.CapturedHead}
		if stopped {
			step.Status = agentIntegrationNotRun
			step.Detail = "not attempted after the first conflict or error"
			result.steps = append(result.steps, step)
			continue
		}
		if err := ctx.Err(); err != nil {
			step.Status, step.Detail = agentIntegrationStopped, safeError(err)
			result.steps = append(result.steps, step)
			result.err = safeError(err)
			stopped = true
			continue
		}
		// The argument is deliberately the immutable capture, never Task.Branch.
		if err := integration.MergeBranch(ctx, captured.CapturedHead); err != nil {
			step.Status, step.Detail = agentIntegrationStopped, safeError(err)
			result.err = "merge stopped at " + target.task.Title + ": " + safeError(err)
			stopped = true
		} else {
			step.Status = agentIntegrationMerged
			step.Detail = "merged captured commit " + captured.CapturedHead
		}
		result.steps = append(result.steps, step)
	}
	result.completed = true
	result.stopped = stopped
	result.succeeded = !stopped && len(result.steps) > 0
	return result
}

// confirmAgentIntegrationCleanup is separate from dry-run confirmation. Force
// removal is explicit because a conflicted integration contains Git's conflict
// state; successful and conflicted worktrees otherwise remain inspectable.
func (a *app) confirmAgentIntegrationCleanup() {
	state := &a.agentIntegration
	if state.running || state.cleaning || !state.worktreeCreated || state.worktreePath == "" {
		return
	}
	path := state.worktreePath
	repositoryID := state.repositoryID
	familyOpenPath := state.familyOpenPath
	a.askConfirm(confirmRequest{
		title:        "Remove isolated integration worktree",
		message:      trf("Force-remove only the dry-run worktree at %s?", path),
		detail:       tr("This deletes the isolated checkout, including any conflict state or commits that exist only there. Task worktrees, branches, statuses and BaseCommit values are untouched."),
		confirmLabel: "Force remove dry run",
		destructive:  true,
		run: func() {
			a.beginAgentIntegrationCleanup(repositoryID, familyOpenPath, path)
		},
	})
}

func (a *app) beginAgentIntegrationCleanup(repositoryID, familyOpenPath, path string) {
	state := &a.agentIntegration
	if state.running || state.cleaning || !state.worktreeCreated || state.repositoryID != repositoryID || state.worktreePath != path {
		return
	}
	state.token++
	state.cleaning = true
	state.err = ""
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	token, generation := state.token, a.generation
	opener := a.opener
	go func() {
		defer a.recoverBackground()
		defer cancel()
		source, err := opener.Open(ctx, familyOpenPath)
		if err == nil {
			defer closeRepositorySession(source)
			err = source.DeleteWorktree(ctx, path, true)
		}
		a.update(func() {
			current := &a.agentIntegration
			if token == current.token {
				current.cancel = nil
			}
			if generation != a.generation || token != current.token || repositoryID != a.agentRepositoryID() {
				return
			}
			current.cleaning = false
			if err != nil {
				current.err = "could not remove isolated worktree: " + safeError(err)
				return
			}
			current.worktreeCreated = false
			current.cleanupDone = true
		})
	}()
}
