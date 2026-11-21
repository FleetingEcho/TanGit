package app

import (
	"strconv"
	"strings"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/transfer"
)

type activityOpenResult struct {
	Opened bool
	Reason string
}

func (s *shell) activityTargetTab(target activityTarget) *tab {
	if s == nil {
		return nil
	}
	location := strings.TrimSpace(target.RepositoryID)
	if location == "" {
		return s.activeTab()
	}
	for _, tab := range s.tabs {
		if tab != nil && activityLocation(tab.app) == location {
			return tab
		}
	}
	return nil
}

func (a *app) activityTargetUnavailable(title, description string) {
	if a == nil {
		return
	}
	a.recordActivityNotice(activitySeverityWarning, title, description, activityTarget{})
	a.notify("warning", title, description)
}

// Open resolves only stable IDs and queues cross-tab work through shell.request.
// A record lookup occurs after the selected tab is active, never from a saved
// row index, command string or closure.
func (activityFeed) Open(ctx commandContext, target activityTarget) activityOpenResult {
	if target.Kind == activityTargetWorkspace || target.Command != "" {
		id := target.Command
		if id == "" {
			id = commandID(target.PrimaryID)
		}
		if id == "" || ctx.Shell == nil {
			return activityOpenResult{Reason: "This activity has no available navigation target"}
		}
		ctx.Shell.request(func() {
			ctx.Shell.invokeCommand(id)
		})
		return activityOpenResult{Opened: true}
	}
	tab := ctx.Shell.activityTargetTab(target)
	if tab == nil || tab.app == nil {
		return activityOpenResult{Reason: "The repository tab for this activity is no longer open"}
	}
	ctx.Shell.request(func() {
		if ctx.Shell.indexOf(tab) < 0 {
			ctx.Shell.activeApp().activityTargetUnavailable("Activity target unavailable", "The repository tab closed before navigation completed.")
			return
		}
		ctx.Shell.activateTab(tab)
		switch target.Kind {
		case activityTargetCommand:
			tab.app.openActivityCommand(target.PrimaryID)
		case activityTargetJournal:
			tab.app.openJournalOperation(target.PrimaryID)
		case activityTargetAgent:
			tab.app.openAgentActivity(target.PrimaryID, target.SecondaryID)
		case activityTargetTransfer:
			if !tab.app.openActivityTransfer(target.PrimaryID) {
				ctx.Shell.activeApp().activityTargetUnavailable("Transfer details unavailable", "That transfer is no longer available in this session.")
			}
		default:
			tab.app.activityTargetUnavailable("Activity target unavailable", "This activity has no supported navigation target.")
		}
	})
	return activityOpenResult{Opened: true}
}

func (a *app) openActivityCommand(value string) {
	a.openGitCommandLog()
	id, err := strconv.ParseUint(value, 10, 64)
	if err == nil && id != 0 && a.commandLog != nil {
		for _, entry := range a.commandLog.Entries() {
			if entry.ID() != id {
				continue
			}
			a.commandLogUser = entry.Origin == repository.CommandOriginUser
			target := commandLogFailureTarget{log: a.commandLog, id: id}
			a.commandLogReveal = &target
			return
		}
	}
	a.activityTargetUnavailable("Command record unavailable", "That command was cleared or evicted from the session log. Showing the current Command Log instead.")
}

func (a *app) openJournalOperation(id string) {
	a.openActivityRecovery()
	a.journalNotice = ""
	a.journalReveal = ""
	for _, record := range a.snapshot.Operations {
		if record.ID == id && id != "" {
			a.journalReveal = id
			return
		}
	}
	a.journalNotice = tr("That journal operation is no longer available. Showing the latest records instead.")
	a.activityTargetUnavailable("Journal record unavailable", a.journalNotice)
}

func (a *app) openAgentActivity(taskID, activityID string) {
	a.setWorkspaceTab(sectionAgents)
	a.agents.activityNotice = ""
	a.agents.activityReveal = ""
	for index, task := range a.agents.tasks {
		if task.ID != taskID {
			continue
		}
		a.agents.selectedRow = index
		a.agents.selectedTaskID = task.ID
		if activityID == "" {
			return
		}
		for _, activity := range task.Activity {
			if activity.ID == activityID {
				a.agents.activityReveal = activityID
				return
			}
		}
		a.agents.activityNotice = tr("That Agent activity is no longer in the task history. Showing the latest activity instead.")
		a.activityTargetUnavailable("Agent activity unavailable", a.agents.activityNotice)
		return
	}
	a.agents.activityNotice = tr("That Agent task is not loaded in this repository.")
	a.activityTargetUnavailable("Agent task unavailable", a.agents.activityNotice)
}

func (a *app) openActivityTransfer(value string) bool {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return false
	}
	transferID := transfer.ID(id)
	if a.remoteTransfers.manager == nil || a.remoteTransfers.jobs[transferID] == nil {
		return false
	}
	a.showRemoteTransferDetails(a.remoteTransfers.manager, a.remoteTransfers.generation, transferID)
	return true
}
