package app

import (
	"strconv"
	"strings"
	"time"

	"lazygitapp/internal/agentwork"
)

// recordAgentActivity serializes an optional lifecycle mutation and its command
// event. Callers invoke it only after the terminal accepts the operation, so a
// refused start cannot leave a launch or resume record behind.
func (a *app) recordAgentActivity(taskID string, activity agentwork.Activity, mutate func(*agentwork.Task) error) {
	if taskID == "" || a.agents.mutating[taskID] {
		return
	}
	store, err := a.ensureAgentStore()
	if err != nil {
		a.notify("error", "Could not record agent activity", safeError(err))
		return
	}
	if a.agents.mutating == nil {
		a.agents.mutating = map[string]bool{}
	}
	a.agents.mutating[taskID] = true
	generation := a.generation
	go func() {
		defer a.recoverBackground()
		previous, err := store.Get(taskID)
		updated := previous
		if err == nil && mutate != nil {
			updated, err = store.Update(taskID, mutate)
		}
		if err == nil {
			updated, err = store.RecordActivity(taskID, activity)
		}
		a.update(func() {
			delete(a.agents.mutating, taskID)
			if generation != a.generation {
				return
			}
			if err != nil {
				a.notify("error", "Could not record agent activity", safeError(err))
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
			a.notifyAgentAttentions(agentTaskAttentions(previous, updated))
		})
	}()
}

func agentActivityCommand(command []string) string {
	if len(command) == 0 {
		return ""
	}
	// shellCommandForTerminal keeps the fully-expanded trusted command last for
	// both login shells and cmd.exe.
	return command[len(command)-1]
}

func (a *app) agentTerminalReference(taskID string) string {
	for _, session := range a.term.sessions {
		if session.agentTaskID == taskID {
			return "terminal:" + strconv.FormatUint(session.id, 10)
		}
	}
	return ""
}

func agentActivitySummary(activity agentwork.Activity) string {
	switch activity.Kind {
	case agentwork.ActivityLaunch:
		return tr("Launch agent")
	case agentwork.ActivityResume:
		return tr("Resume agent")
	case agentwork.ActivityCheckpoint:
		return tr("Checkpoint saved")
	case agentwork.ActivityStatus:
		from := agentTaskStatusLabel(agentwork.Status(activity.StatusFrom))
		to := agentTaskStatusLabel(agentwork.Status(activity.StatusTo))
		if activity.StatusFrom == "" {
			return tr("Status") + ": " + tr(to)
		}
		return tr("Status") + ": " + tr(from) + " → " + tr(to)
	case agentwork.ActivityTest:
		from := agentTestStatusLabel(agentwork.TestStatus(activity.StatusFrom))
		to := agentTestStatusLabel(agentwork.TestStatus(activity.StatusTo))
		if activity.StatusFrom == "" {
			return tr("Tests") + ": " + tr(to)
		}
		return tr("Tests") + ": " + tr(from) + " → " + tr(to)
	default:
		return string(activity.Kind)
	}
}

func agentActivityTimestamp(activity agentwork.Activity) string {
	if activity.CreatedAt.IsZero() {
		return ""
	}
	return activity.CreatedAt.Local().Format(time.DateTime)
}

func agentActivityTooltip(activity agentwork.Activity) string {
	var details []string
	if activity.Command != "" {
		details = append(details, tr("Command")+": "+activity.Command)
	}
	if activity.SessionID != "" {
		details = append(details, tr("Session")+": "+activity.SessionID)
	}
	if activity.Reference != "" {
		details = append(details, tr("Reference")+": "+activity.Reference)
	}
	if activity.CheckpointID != "" {
		details = append(details, tr("Checkpoint")+": "+activity.CheckpointID)
	}
	if activity.SnapshotID != "" {
		details = append(details, tr("Snapshot")+": "+activity.SnapshotID)
	}
	return strings.Join(details, "\n")
}
