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
const agentTestOperationSuffix = ":tests"

func agentTestOperationID(taskID string) string { return taskID + agentTestOperationSuffix }

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

// openAgentOperationTerminal returns false without changing task state when
// the terminal limit or remote execution prevents startup.
func (a *app) openAgentOperationTerminal(task agentwork.Task, command []string, requestPath string, tests bool) bool {
	if len(command) == 0 || task.ID == "" || strings.TrimSpace(task.WorktreePath) == "" {
		return false
	}
	if a.location.IsRemote() {
		a.notify("warning", "Remote agent launch is not available yet", tr("This generic agent adapter currently requires a local worktree. The durable task and remote worktree are preserved."))
		return false
	}
	operationID := task.ID
	if tests {
		operationID = agentTestOperationID(task.ID)
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
	if tests {
		name, shell = "Tests: "+task.Title, "Tests"
	}
	a.term.sessions = append(a.term.sessions, terminalSessionState{
		id: a.term.nextID, groupID: a.term.nextGroupID,
		agentTaskID: operationID, worktree: task.WorktreePath,
		name: name, shell: shell, command: command,
		environment: []string{
			"TANGIT_AGENT_TASK_ID=" + task.ID,
			"TANGIT_AGENT_WORKTREE=" + task.WorktreePath,
			"TANGIT_AGENT_REQUEST_FILE=" + requestPath,
		},
	})
	a.term.active = index
	a.startTerminalAt(index)
	a.term.layout = &terminalLayoutNode{sessionID: a.term.nextID}
	a.storeTerminalLayout()
	return true
}

// agentTerminalExited records process completion only. A zero agent exit is
// not treated as task completion or review approval. Test exits update only
// LastTest and leave both the agent outcome and task lifecycle untouched.
func (a *app) agentTerminalExited(operationID string, code int) {
	taskID, tests := agentOperationTaskID(operationID)
	if tests {
		a.agentTestTerminalExited(taskID, code)
		return
	}
	a.mutateAgentTask(taskID, func(task *agentwork.Task) error {
		task.ExitCode = &code
		if code == 0 {
			task.Status = agentwork.StatusExited
		} else {
			task.Status = agentwork.StatusFailed
		}
		return nil
	})
}
