package app

import (
	"os"
	"runtime"
	"strings"

	"lazygitapp/internal/agentwork"
)

// shellCommandForTerminal runs the user's trusted template as an interactive
// login-shell command. Substitutions were quoted before this single shell pass.
func shellCommandForTerminal(command string) []string {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/S", "/C", command}
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell, "-lc", command}
}

// Agent and test terminals have different operation identities while sharing
// the task's stable ID in their environment. This lets both processes run in
// the same explicit worktree without reopening or replacing each other.
const (
	agentTestOperationSuffix = ":tests"
	agentRunOperationSuffix  = ":run"
)

func agentTestOperationID(taskID string) string { return taskID + agentTestOperationSuffix }
func agentRunOperationID(taskID string) string  { return taskID + agentRunOperationSuffix }

func agentOperationTaskID(operationID string) (string, bool) {
	taskID, tests := strings.CutSuffix(operationID, agentTestOperationSuffix)
	return taskID, tests
}

func (a *app) selectAgentOperationTerminal(operationID string) bool {
	for index := range a.term.sessions {
		session := &a.term.sessions[index]
		if session.agentTaskID != operationID {
			continue
		}
		a.term.active = index
		a.selectTerminalGroup(session.groupID)
		a.term.panelOpen = true
		return true
	}
	return false
}

// openAgentTerminal starts or selects the one live agent terminal owned by task.
func (a *app) openAgentTerminal(task agentwork.Task, command []string, requestPath string) bool {
	return a.openAgentOperationTerminal(task, command, requestPath, false)
}

func (a *app) openAgentTestTerminal(task agentwork.Task, command []string, requestPath string) bool {
	return a.openAgentOperationTerminal(task, command, requestPath, true)
}

func (a *app) openAgentRunTerminal(task agentwork.Task, command []string, requestPath string) bool {
	return a.openAgentOperationTerminalKind(task, command, requestPath, "run")
}

// openAgentOperationTerminal returns false without changing task state when
// the terminal limit or remote execution prevents startup.
func (a *app) openAgentOperationTerminal(task agentwork.Task, command []string, requestPath string, tests bool) bool {
	kind := "agent"
	if tests {
		kind = "tests"
	}
	return a.openAgentOperationTerminalKind(task, command, requestPath, kind)
}

func (a *app) openAgentOperationTerminalKind(task agentwork.Task, command []string, requestPath, kind string) bool {
	if len(command) == 0 || task.ID == "" || strings.TrimSpace(task.WorktreePath) == "" {
		return false
	}
	if a.location.IsRemote() {
		a.notify("warning", "Remote agent launch is not available yet", tr("This generic agent adapter currently requires a local worktree. The durable task and remote worktree are preserved."))
		return false
	}
	operationID := task.ID
	switch kind {
	case "tests":
		operationID = agentTestOperationID(task.ID)
	case "run":
		operationID = agentRunOperationID(task.ID)
	}
	if a.selectAgentOperationTerminal(operationID) {
		return true
	}
	a.term.panelOpen = true
	if a.term.percent <= 0 || a.term.percent >= 100 {
		a.term.percent = terminalPanelPercent
	}
	if a.terminalAtLimit() {
		return false
	}
	if a.activeTerminal() != nil {
		a.storeTerminalLayout()
	}
	a.term.nextID++
	a.term.nextGroupID++
	index := len(a.term.sessions)
	name, shell := "Agent: "+task.Title, "Agent"
	switch kind {
	case "tests":
		name, shell = "Tests: "+task.Title, "Tests"
	case "run":
		name, shell = "Run: "+task.Title, "Run"
	}
	a.term.sessions = append(a.term.sessions, terminalSessionState{
		id: a.term.nextID, groupID: a.term.nextGroupID,
		agentTaskID: operationID, worktree: task.WorktreePath,
		name: name, shell: shell, command: command,
		environment: agentTaskEnvironment(task, requestPath, operationID),
	})
	a.term.active = index
	a.startTerminalAt(index)
	a.term.layout = &terminalLayoutNode{sessionID: a.term.nextID}
	a.storeTerminalLayout()
	return true
}

// agentProcessCompletion distinguishes a real process exit from terminal
// startup failure or manual termination. It is queued while a task mutation is
// active so even an immediately exiting agent cannot leave its state Running.
type agentProcessCompletion struct {
	generation uint64
	exitCode   *int
}

func (a *app) recordAgentProcessCompletion(taskID string, completion agentProcessCompletion) {
	a.recordAgentProcessCompletionAtGeneration(taskID, a.generation, completion)
}

func (a *app) recordAgentProcessCompletionAtGeneration(taskID string, generation uint64, completion agentProcessCompletion) {
	completion.generation = generation
	if a.agents.mutating[taskID] {
		if a.agents.processCompletions == nil {
			a.agents.processCompletions = map[string]agentProcessCompletion{}
		}
		if _, pending := a.agents.processCompletions[taskID]; !pending {
			a.agents.processCompletions[taskID] = completion
		}
		return
	}
	a.persistAgentProcessCompletion(taskID, completion)
}

func (a *app) persistAgentProcessCompletion(taskID string, completion agentProcessCompletion) {
	a.mutateAgentTaskAtGeneration(taskID, completion.generation, func(task *agentwork.Task) error {
		task.ExitCode = nil
		task.Status = agentwork.StatusFailed
		if completion.exitCode != nil {
			code := *completion.exitCode
			task.ExitCode = &code
			if code == 0 {
				task.Status = agentwork.StatusExited
			}
		}
		return nil
	})
}

func (a *app) flushAgentProcessCompletion(taskID string) {
	if a.agents.mutating[taskID] {
		return
	}
	completion, pending := a.agents.processCompletions[taskID]
	if !pending {
		return
	}
	delete(a.agents.processCompletions, taskID)
	a.persistAgentProcessCompletion(taskID, completion)
}

// agentTerminalExited records process completion only. A zero agent exit is
// not treated as task completion or review approval. Test exits update only
// LastTest and leave both the agent outcome and task lifecycle untouched.
func (a *app) agentTerminalExited(operationID string, code int) {
	a.agentTerminalExitedAtGeneration(operationID, a.generation, code)
}

func (a *app) agentTerminalExitedAtGeneration(operationID string, generation uint64, code int) {
	// Development servers and other Run commands are observations only. Their
	// exit codes never alter the coding agent's lifecycle or test result.
	if strings.HasSuffix(operationID, agentRunOperationSuffix) {
		return
	}
	taskID, tests := agentOperationTaskID(operationID)
	if tests {
		a.recordAgentTestCompletionAtGeneration(taskID, generation, agentTestExitCompletion(code))
		return
	}
	a.recordAgentProcessCompletionAtGeneration(taskID, generation, agentProcessCompletion{exitCode: &code})
}

// agentTerminalFailed finishes an operation whose terminal closed or failed to
// start before producing a trustworthy process exit code.
func (a *app) agentTerminalFailed(operationID, detail string) {
	a.agentTerminalFailedAtGeneration(operationID, a.generation, detail)
}

func (a *app) agentTerminalFailedAtGeneration(operationID string, generation uint64, detail string) {
	if strings.HasSuffix(operationID, agentRunOperationSuffix) {
		return
	}
	taskID, tests := agentOperationTaskID(operationID)
	if tests {
		a.recordAgentTestCompletionAtGeneration(taskID, generation, agentTestFailureCompletion(detail))
		return
	}
	a.recordAgentProcessCompletionAtGeneration(taskID, generation, agentProcessCompletion{})
}
