package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentUIState struct {
	review         agentReviewState
	tasks          []agentwork.Task
	list           ui.ListState
	selectedRow    int
	selectedTaskID string
	loadedRepo     string
	loading        bool
	loadToken      uint64
	err            string
	mutating       map[string]bool

	formOpen       bool
	title          string
	request        string
	base           string
	branch         string
	path           string
	launchCommand  string
	resumeCommand  string
	setupCommand   string
	testCommand    string
	launchAfterAdd bool
}

func (a *app) agentRepositoryID() string {
	root := strings.TrimSpace(a.snapshot.Root)
	for _, worktree := range a.snapshot.Worktrees {
		if worktree.Main && strings.TrimSpace(worktree.Path) != "" {
			root = strings.TrimSpace(worktree.Path)
			break
		}
	}
	location := a.location
	location.Root = root
	if location.Valid() {
		return location.String()
	}
	return root
}

func (a *app) ensureAgentStore() (*agentwork.Store, error) {
	if a.agentStore != nil {
		return a.agentStore, nil
	}
	store, err := agentwork.OpenStore(a.dataDir())
	if err != nil {
		return nil, err
	}
	a.agentStore = store
	return store, nil
}

// refreshAgentTasks reads durable records off the UI thread. Repository and
// generation checks stop a slow read from one repository landing in another.
func (a *app) refreshAgentTasks() {
	repositoryID := a.agentRepositoryID()
	if repositoryID == "" {
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.agents.err = safeError(err)
		return
	}
	a.agents.loadToken++
	token, generation := a.agents.loadToken, a.generation
	a.agents.loading = true
	a.agents.err = ""
	go func() {
		defer a.recoverBackground()
		tasks, err := store.List(repositoryID)
		a.updateChanged(func() bool {
			if token != a.agents.loadToken || generation != a.generation || repositoryID != a.agentRepositoryID() {
				return false
			}
			a.agents.loading = false
			a.agents.loadedRepo = repositoryID
			if err != nil {
				a.agents.err = safeError(err)
				return true
			}
			a.agents.tasks = agentTaskClones(tasks)
			if a.agents.selectedTaskID == "" && len(tasks) > 0 {
				a.agents.selectedTaskID = tasks[0].ID
			}
			return a.workspaceTab == sectionAgents
		})
	}()
}

func agentTaskClones(tasks []agentwork.Task) []agentwork.Task {
	result := make([]agentwork.Task, len(tasks))
	for index, task := range tasks {
		result[index] = task.Clone()
	}
	return result
}

func (a *app) beginNewAgentTask() {
	a.agents.title = ""
	a.agents.request = ""
	a.agents.launchCommand = ""
	a.agents.resumeCommand = ""
	a.agents.setupCommand = ""
	a.agents.testCommand = ""
	a.agents.base = "HEAD"
	if a.snapshot.Branch != "" {
		a.agents.base = a.snapshot.Branch
	}
	a.agents.branch = "agent/"
	a.agents.path = ""
	a.agents.launchAfterAdd = true
	a.agents.formOpen = true
}

func (a *app) suggestedAgentTaskPath() string {
	a.worktreeBranch = a.agents.branch
	result := a.suggestedWorktreePath()
	a.worktreeBranch = ""
	return result
}

// beginCreateAgentTask freezes the base and creates the checkout and durable
// task in the same background action. If persistence fails, the error says the
// checkout already exists instead of pretending the entire action rolled back.
func (a *app) beginCreateAgentTask() {
	if a.actionBusy || a.session == nil {
		return
	}
	title := strings.TrimSpace(a.agents.title)
	request := a.agents.request
	base := strings.TrimSpace(a.agents.base)
	branch := strings.TrimSpace(a.agents.branch)
	path := strings.TrimSpace(a.agents.path)
	if title == "" || base == "" || branch == "" || path == "" {
		return
	}
	launchAfterAdd := a.agents.launchAfterAdd
	profile := agentwork.Profile{
		LaunchCommand: strings.TrimSpace(a.agents.launchCommand),
		ResumeCommand: strings.TrimSpace(a.agents.resumeCommand),
		SetupCommand:  strings.TrimSpace(a.agents.setupCommand),
		TestCommand:   strings.TrimSpace(a.agents.testCommand),
	}
	repositoryID := a.agentRepositoryID()
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not create agent task", safeError(err))
		return
	}
	var created agentwork.Task
	a.beginRepositoryActionMessage("", "", func(ctx context.Context, session repository.Session) error {
		baseCommit, err := session.ResolveCommit(ctx, base)
		if err != nil {
			return err
		}
		if err := session.CreateWorktree(ctx, repository.WorktreeCreateOptions{Path: path, Base: base, Branch: branch}); err != nil {
			return err
		}
		created, err = store.Create(agentwork.Task{
			RepositoryID: repositoryID,
			Title:        title, Request: request,
			BaseRef: base, BaseCommit: baseCommit,
			Branch: branch, WorktreePath: path, Profile: profile,
		})
		if err != nil {
			return fmt.Errorf("worktree %s was created, but the agent task could not be saved: %w", path, err)
		}
		return nil
	}, func() {
		a.agents.tasks = append([]agentwork.Task{created.Clone()}, a.agents.tasks...)
		a.agents.selectedTaskID = created.ID
		a.agents.formOpen = false
		a.agents.err = ""
		a.notify("success", "Agent task created", created.Title)
		if launchAfterAdd && created.Profile.LaunchCommand != "" {
			a.launchAgentTask(created.ID, false)
		}
	})
}

func (a *app) selectedAgentTask() (agentwork.Task, bool) {
	for _, task := range a.agents.tasks {
		if task.ID == a.agents.selectedTaskID {
			return task.Clone(), true
		}
	}
	return agentwork.Task{}, false
}

func (a *app) mutateAgentTask(taskID string, mutate func(*agentwork.Task) error, success ...func(agentwork.Task)) {
	if taskID == "" || a.agents.mutating[taskID] {
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not update agent task", safeError(err))
		return
	}
	if a.agents.mutating == nil {
		a.agents.mutating = map[string]bool{}
	}
	a.agents.mutating[taskID] = true
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		updated, err := store.Update(taskID, mutate)
		a.update(func() {
			delete(a.agents.mutating, taskID)
			if generation != a.generation {
				return
			}
			if err != nil {
				a.notify("error", "Could not update agent task", safeError(err))
				return
			}
			found := false
			for index := range a.agents.tasks {
				if a.agents.tasks[index].ID == taskID {
					a.agents.tasks[index] = updated.Clone()
					found = true
					break
				}
			}
			if !found {
				a.agents.tasks = append([]agentwork.Task{updated.Clone()}, a.agents.tasks...)
			}
			if len(success) > 0 && success[0] != nil {
				success[0](updated.Clone())
			}
		})
	}()
}

func (a *app) setAgentTaskStatus(task agentwork.Task, status agentwork.Status) {
	a.mutateAgentTask(task.ID, func(current *agentwork.Task) error {
		current.Status = status
		return nil
	})
}

// agentTaskCommand expands values once and shell-quotes each substitution. A
// task request containing {worktree} or shell syntax remains data, never a
// second template or program fragment.
func (a *app) expandAgentTaskCommand(task agentwork.Task, template, requestPath string) string {
	values := map[string]string{
		"worktree": task.WorktreePath, "task": task.Title, "request": task.Request,
		"promptFile": requestPath, "base": task.BaseCommit, "branch": task.Branch,
		"sessionId": task.SessionID,
	}
	parts := make([]string, 0, len(values)*4)
	for key, value := range values {
		quoted := shellQuote(value)
		parts = append(parts, "{"+key+"}", quoted, "{{"+key+"}}", quoted)
	}
	return strings.NewReplacer(parts...).Replace(template)
}

func (a *app) agentTaskCommand(task agentwork.Task, resume bool, requestPath string) []string {
	template := task.Profile.LaunchCommand
	if resume && strings.TrimSpace(task.Profile.ResumeCommand) != "" {
		template = task.Profile.ResumeCommand
	}
	expanded := a.expandAgentTaskCommand(task, template, requestPath)
	if task.Profile.SetupCommand != "" && !resume {
		expanded = "(" + task.Profile.SetupCommand + ") && (" + expanded + ")"
	}
	return shellCommandForTerminal(expanded)
}

func (a *app) launchAgentTask(taskID string, resume bool) {
	task, ok := a.selectedAgentTask()
	if !ok || task.ID != taskID || task.Status == agentwork.StatusArchived {
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not launch agent", safeError(err))
		return
	}
	requestPath, err := store.RequestFile(task.ID)
	if err != nil {
		return
	}
	command := a.agentTaskCommand(task, resume, requestPath)
	if len(command) == 0 {
		a.notify("warning", "Agent command is not configured", "Set a launch command for this task in Settings → Commands.")
		return
	}
	if !a.openAgentTerminal(task, command, requestPath) {
		return
	}
	a.mutateAgentTask(task.ID, func(current *agentwork.Task) error {
		current.Status = agentwork.StatusRunning
		current.ExitCode = nil
		return nil
	})
}

func (a *app) openSelectedAgentWorktree() {
	task, ok := a.selectedAgentTask()
	if !ok {
		return
	}
	for _, worktree := range a.snapshot.Worktrees {
		if worktree.Path == task.WorktreePath && !worktree.Missing {
			a.openWorktree(worktree)
			return
		}
	}
	a.notify("warning", "Worktree is unavailable", task.WorktreePath)
}

func agentTaskStatusLabel(status agentwork.Status) string {
	switch status {
	case agentwork.StatusReady:
		return "Ready"
	case agentwork.StatusRunning:
		return "Running"
	case agentwork.StatusNeedsInput:
		return "Needs input"
	case agentwork.StatusTesting:
		return "Testing"
	case agentwork.StatusExited:
		return "Exited"
	case agentwork.StatusReadyForReview:
		return "Ready for review"
	case agentwork.StatusBlocked:
		return "Blocked"
	case agentwork.StatusFailed:
		return "Failed"
	case agentwork.StatusDone:
		return "Done"
	case agentwork.StatusArchived:
		return "Archived"
	default:
		return string(status)
	}
}

func agentTaskActive(task agentwork.Task) bool { return task.Status != agentwork.StatusArchived }

func agentShortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

func agentTaskUpdated(task agentwork.Task) string {
	when := task.UpdatedAt
	if when.IsZero() {
		return ""
	}
	return when.Local().Format(time.DateTime)
}
