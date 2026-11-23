package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/FleetingEcho/lazygitGo/internal/transfer"
)

// activityLocation and activitySessionID identify the current repository and
// process without persisting an additional activity database.
func activityLocation(a *app) string {
	if a == nil {
		return ""
	}
	if location := strings.TrimSpace(a.location.String()); location != "" {
		return location
	}
	return strings.TrimSpace(a.snapshot.Root)
}

func activitySessionID(a *app) string {
	if a == nil {
		return ""
	}
	return fmt.Sprintf("%p:%d", a, a.generation)
}

func activityTimestamp(created, updated time.Time) time.Time {
	if !updated.IsZero() {
		return updated
	}
	return created
}

// activityInput snapshots canonical records for the Overview while composing
// a frame. Transfer timestamps are view model metadata owned by this package.
func (a *app) activityInput() (activityAppInput, bool) {
	if a == nil || a.state != stateReady || a.editorOnlyWorkspace() {
		return activityAppInput{}, false
	}
	repositoryID := strings.TrimSpace(a.agentRepositoryID())
	location := activityLocation(a)
	if repositoryID == "" {
		repositoryID = location
	}
	if repositoryID == "" && location == "" {
		return activityAppInput{}, false
	}
	input := activityAppInput{
		Repository: repositoryID, Location: location, Session: activitySessionID(a),
		Commands: a.commandLogEntries(),
		Journal:  append([]repository.OperationRecord(nil), a.snapshot.Operations...),
		Agents:   agentTaskClones(a.agents.tasks),
		Notices:  append([]activityNotice(nil), a.activityNotices...),
	}
	for id, job := range a.remoteTransfers.jobs {
		if job == nil {
			continue
		}
		title := tr("Upload")
		if job.kind == "download" {
			title = tr("Download")
		}
		state, severity := activityStateSucceeded, activitySeveritySuccess
		switch job.result.Status {
		case transfer.StatusQueued, transfer.StatusRunning:
			state, severity = activityStateRunning, activitySeverityInfo
		case transfer.StatusPartial:
			state, severity = activityStateFailed, activitySeverityWarning
		case transfer.StatusFailed, transfer.StatusCancelled:
			state, severity = activityStateFailed, activitySeverityError
		}
		input.Transfers = append(input.Transfers, activityTransfer{
			ID: strconv.FormatUint(uint64(id), 10), OccurredAt: job.occurredAt, UpdatedAt: job.updatedAt,
			Title: title, Description: remoteTransferProgressDescription(job, job.result),
			State: state, Severity: severity,
		})
	}
	return input, true
}

func activitySourceLabel(source activitySource) string {
	switch source {
	case activitySourceCommand:
		return tr("Commands")
	case activitySourceJournal:
		return tr("Safety Journal")
	case activitySourceAgent:
		return tr("Agent activity")
	case activitySourceTransfer:
		return tr("Transfers")
	case activitySourceNotice:
		return tr("Notices")
	default:
		return tr("Activity")
	}
}

func activityStateLabel(state activityState) string {
	switch state {
	case activityStateRunning:
		return tr("Running")
	case activityStateSucceeded:
		return tr("Succeeded")
	case activityStateFailed:
		return tr("Failed")
	case activityStateNeedsInput:
		return tr("Needs input")
	case activityStateBlocked:
		return tr("Blocked")
	default:
		return string(state)
	}
}

func activityPersistenceLabel(persistence activityPersistence) string {
	if persistence == activityPersistenceDurable {
		return tr("Durable")
	}
	if persistence == activityPersistenceCurrent {
		return tr("Current state")
	}
	return tr("Session")
}

// recordActivityNotice is intentionally explicit: ordinary toasts and every
// progress tick do not become a second notification database.
func (a *app) recordActivityNotice(severity activitySeverity, title, description string, target activityTarget) {
	if a == nil || strings.TrimSpace(title) == "" {
		return
	}
	a.activityNoticeSeq++
	if target.RepositoryID == "" {
		target.RepositoryID = activityLocation(a)
	}
	notice := activityNotice{
		ID: a.activityNoticeSeq, OccurredAt: time.Now(), Severity: severity,
		Title: title, Description: description, Target: target,
	}
	a.activityNotices = append(a.activityNotices, notice)
	if len(a.activityNotices) > 200 {
		a.activityNotices = append([]activityNotice(nil), a.activityNotices[len(a.activityNotices)-200:]...)
	}
}
