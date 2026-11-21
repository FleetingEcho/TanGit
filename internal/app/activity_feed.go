package app

import (
	"container/heap"
	"sort"
	"strconv"
	"strings"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

const (
	activityPerAppCandidateLimit = 500
	activityGlobalCandidateLimit = 2000
	activityDefaultResultLimit   = 100
	activityCommandSummaryLimit  = 240
)

type activitySource string

const (
	activitySourceCommand  activitySource = "command"
	activitySourceJournal  activitySource = "journal"
	activitySourceAgent    activitySource = "agent"
	activitySourceTransfer activitySource = "transfer"
	activitySourceNotice   activitySource = "notice"
)

type activityKind string

const (
	activityKindCommand  activityKind = "command"
	activityKindJournal  activityKind = "journal"
	activityKindAgent    activityKind = "agent"
	activityKindTransfer activityKind = "transfer"
	activityKindNotice   activityKind = "notice"
)

type activitySeverity string

const (
	activitySeverityInfo    activitySeverity = "info"
	activitySeveritySuccess activitySeverity = "success"
	activitySeverityWarning activitySeverity = "warning"
	activitySeverityError   activitySeverity = "error"
)

type activityState string

const (
	activityStateRunning    activityState = "running"
	activityStateSucceeded  activityState = "succeeded"
	activityStateFailed     activityState = "failed"
	activityStateNeedsInput activityState = "needs input"
	activityStateBlocked    activityState = "blocked"
)

type activityPersistence string

const (
	activityPersistenceSession activityPersistence = "session"
	activityPersistenceDurable activityPersistence = "durable"
	activityPersistenceCurrent activityPersistence = "current state"
)

type activityTargetKind string

const (
	activityTargetCommand   activityTargetKind = "command"
	activityTargetJournal   activityTargetKind = "journal"
	activityTargetAgent     activityTargetKind = "agent"
	activityTargetTransfer  activityTargetKind = "transfer"
	activityTargetWorkspace activityTargetKind = "workspace"
	activityTargetNone      activityTargetKind = "none"
)

// activityID is the projection identity. Repo is the canonical repository
// family for durable rows and the repository/location/session family for
// session rows. Value is stable within that family and source.
type activityID struct {
	Source activitySource
	Repo   string
	Value  string
}

var activityIDComponentEscaper = strings.NewReplacer("%", "%25", ":", "%3A")

// String returns a stable, source-namespaced representation. Escaping percent
// and colon keeps different Repo/Value boundaries from producing the same ID.
func (id activityID) String() string {
	return string(id.Source) + ":" +
		activityIDComponentEscaper.Replace(id.Repo) + ":" +
		activityIDComponentEscaper.Replace(id.Value)
}

type activityTarget struct {
	Kind         activityTargetKind
	RepositoryID string
	PrimaryID    string
	SecondaryID  string
	Command      commandID
}

type activityItem struct {
	ID          activityID
	OccurredAt  time.Time
	UpdatedAt   time.Time
	Repository  string
	Source      activitySource
	Kind        activityKind
	Severity    activitySeverity
	State       activityState
	Persistence activityPersistence
	Title       string
	Description string
	Target      activityTarget
}

type activityNotice struct {
	ID          uint64
	OccurredAt  time.Time
	Severity    activitySeverity
	Title       string
	Description string
	Target      activityTarget
}

type activityTransfer struct {
	ID          string
	OccurredAt  time.Time
	UpdatedAt   time.Time
	Title       string
	Description string
	State       activityState
	Severity    activitySeverity
}

type activityAppInput struct {
	Repository string
	Location   string
	Session    string
	Commands   []repository.CommandEntry
	Journal    []repository.OperationRecord
	Agents     []agentwork.Task
	Transfers  []activityTransfer
	Notices    []activityNotice
}

type activityInputs struct {
	Apps []activityAppInput
}

type activityQuery struct {
	Repository string
	Sources    map[activitySource]bool
	Severities map[activitySeverity]bool
	Text       string
	Limit      int
}

type activityFeed struct{}

// Project builds a read-only, in-memory view of existing activity. Candidate
// bounds are applied before filtering so an expensive or overly specific
// filter cannot turn projection into an unbounded history scan.
func (activityFeed) Project(inputs activityInputs, query activityQuery) []activityItem {
	var candidates activityCandidateHeap
	for _, appInput := range inputs.Apps {
		var perApp activityCandidateHeap
		add := func(item activityItem) {
			item.Title = strings.TrimSpace(repository.RedactSecrets(item.Title))
			item.Description = strings.TrimSpace(repository.RedactSecrets(item.Description))
			activityCandidateHeapAdd(&perApp, activityPerAppCandidateLimit, item)
		}
		activityProjectCommands(appInput, add)
		activityProjectJournal(appInput, add)
		activityProjectAgents(appInput, add)
		activityProjectTransfers(appInput, add)
		activityProjectNotices(appInput, add)
		for _, item := range perApp {
			activityCandidateHeapAdd(&candidates, activityGlobalCandidateLimit, item)
		}
	}

	// Merge only after the global candidate bound. Durable rows share their
	// canonical family; session rows include location and manager generation.
	merged := make(map[activityID]activityItem, len(candidates))
	for _, candidate := range candidates {
		current, exists := merged[candidate.ID]
		if !exists || activityPreferUpdate(candidate, current) {
			merged[candidate.ID] = candidate
		}
	}
	items := make([]activityItem, 0, len(merged))
	for _, item := range merged {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return activityCandidateOrder(items[i], items[j]) < 0
	})

	limit := query.Limit
	if limit <= 0 || limit > activityGlobalCandidateLimit {
		if limit <= 0 {
			limit = activityDefaultResultLimit
		} else {
			limit = activityGlobalCandidateLimit
		}
	}
	filtered := make([]activityItem, 0, min(limit, len(items)))
	for _, item := range items {
		if !activityMatchesQuery(item, query) {
			continue
		}
		filtered = append(filtered, item)
		if len(filtered) == limit {
			break
		}
	}
	return filtered
}

func activityProjectCommands(appInput activityAppInput, add func(activityItem)) {
	family := activitySessionFamily(appInput)
	for _, entry := range appInput.Commands {
		id := strconv.FormatUint(entry.ID(), 10)
		state := activityStateSucceeded
		severity := activitySeveritySuccess
		if entry.Running {
			state = activityStateRunning
			severity = activitySeverityInfo
		} else if entry.Failed {
			state = activityStateFailed
			severity = activitySeverityError
		}
		title := strings.TrimSpace(entry.Command)
		if title == "" {
			title = "Command"
		}
		add(activityItem{
			ID:          activityID{Source: activitySourceCommand, Repo: family, Value: id},
			OccurredAt:  entry.At,
			UpdatedAt:   entry.At,
			Repository:  appInput.Repository,
			Source:      activitySourceCommand,
			Kind:        activityKindCommand,
			Severity:    severity,
			State:       state,
			Persistence: activityPersistenceSession,
			Title:       title,
			Description: activityCommandOutputSummary(entry.Output),
			Target: activityTarget{
				Kind: activityTargetCommand, RepositoryID: appInput.Location, PrimaryID: id,
			},
		})
	}
}

func activityCommandOutputSummary(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > activityCommandSummaryLimit {
			line = string(runes[:activityCommandSummaryLimit-1]) + "…"
		}
		return line
	}
	return ""
}

func activityProjectJournal(appInput activityAppInput, add func(activityItem)) {
	for _, record := range appInput.Journal {
		occurredAt := activityUnixTimestamp(record.CreatedAt)
		updatedAt := occurredAt
		for _, timestamp := range []int64{record.FinishedAt, record.UndoStartedAt, record.UndoneAt} {
			candidate := activityUnixTimestamp(timestamp)
			if !candidate.IsZero() && (updatedAt.IsZero() || candidate.After(updatedAt)) {
				updatedAt = candidate
			}
		}

		state := activityStateSucceeded
		severity := activitySeverityInfo
		switch record.Status {
		case repository.OperationPending, repository.OperationUndoing:
			state = activityStateRunning
		case repository.OperationFailed, repository.OperationInterrupted:
			state = activityStateFailed
			severity = activitySeverityError
		case repository.OperationSucceeded:
			severity = activitySeveritySuccess
		default:
			if record.Error != "" {
				state = activityStateFailed
				severity = activitySeverityError
			}
		}
		if record.Partial {
			// A failed but partially applied operation needs recovery attention,
			// rather than the same treatment as a failure with no workspace change.
			severity = activitySeverityWarning
		}

		title := strings.TrimSpace(record.Summary)
		if title == "" {
			title = strings.TrimSpace(record.Kind)
		}
		if title == "" {
			title = "Repository operation"
		}
		description := strings.TrimSpace(record.Error)
		if description == "" && record.Partial {
			description = "The operation left partial changes; recovery may be available."
		}
		add(activityItem{
			ID:          activityID{Source: activitySourceJournal, Repo: appInput.Repository, Value: record.ID},
			OccurredAt:  occurredAt,
			UpdatedAt:   updatedAt,
			Repository:  appInput.Repository,
			Source:      activitySourceJournal,
			Kind:        activityKindJournal,
			Severity:    severity,
			State:       state,
			Persistence: activityPersistenceDurable,
			Title:       title,
			Description: description,
			Target: activityTarget{
				Kind: activityTargetJournal, RepositoryID: appInput.Location, PrimaryID: record.ID,
			},
		})
	}
}

func activityUnixTimestamp(seconds int64) time.Time {
	if seconds == 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

func activityProjectAgents(appInput activityAppInput, add func(activityItem)) {
	for _, inputTask := range appInput.Agents {
		// Clone at the adapter boundary: projection never retains or mutates the
		// caller's task Activity, LastTest, checkpoint, or profile storage.
		task := inputTask.Clone()
		for _, activity := range task.Activity {
			// Checkpoints deliberately have no independent projection adapter.
			// Task.Activity is the sole durable timeline, including checkpoints.
			title, state, severity := activityAgentProjection(task, activity)
			var details []string
			if task.Title != "" {
				details = append(details, task.Title)
			}
			if activity.Command != "" {
				details = append(details, "Command: "+activity.Command)
			}
			if activity.Reference != "" {
				details = append(details, "Reference: "+activity.Reference)
			}
			if activity.CheckpointID != "" {
				details = append(details, "Checkpoint: "+activity.CheckpointID)
			}
			add(activityItem{
				ID: activityID{
					Source: activitySourceAgent,
					Repo:   appInput.Repository,
					Value:  task.ID + "/" + activity.ID,
				},
				OccurredAt:  activity.CreatedAt,
				UpdatedAt:   activity.CreatedAt,
				Repository:  appInput.Repository,
				Source:      activitySourceAgent,
				Kind:        activityKindAgent,
				Severity:    severity,
				State:       state,
				Persistence: activityPersistenceDurable,
				Title:       title,
				Description: strings.Join(details, "\n"),
				Target: activityTarget{
					Kind: activityTargetAgent, RepositoryID: appInput.Location,
					PrimaryID: task.ID, SecondaryID: activity.ID,
				},
			})
		}
	}
}

func activityAgentProjection(task agentwork.Task, activity agentwork.Activity) (string, activityState, activitySeverity) {
	switch activity.Kind {
	case agentwork.ActivityLaunch:
		return "Launch agent", activityStateSucceeded, activitySeveritySuccess
	case agentwork.ActivityResume:
		return "Resume agent", activityStateSucceeded, activitySeveritySuccess
	case agentwork.ActivityCheckpoint:
		return "Checkpoint saved", activityStateSucceeded, activitySeveritySuccess
	case agentwork.ActivityStatus:
		title := "Agent status"
		if activity.StatusTo != "" {
			title += ": " + activityAgentStatusLabel(agentwork.Status(activity.StatusTo))
		}
		if activity.StatusFrom != "" && activity.StatusTo != "" {
			title = "Agent status: " + activityAgentStatusLabel(agentwork.Status(activity.StatusFrom)) +
				" → " + activityAgentStatusLabel(agentwork.Status(activity.StatusTo))
		}
		state, severity := activityAgentStatusProjection(agentwork.Status(activity.StatusTo))
		return title, state, severity
	case agentwork.ActivityTest:
		status := agentwork.TestStatus(activity.StatusTo)
		title := "Tests"
		if activity.StatusTo != "" {
			title += ": " + activityAgentTestLabel(status)
			if activity.StatusFrom != "" {
				title = "Tests: " + activityAgentTestLabel(agentwork.TestStatus(activity.StatusFrom)) +
					" → " + activityAgentTestLabel(status)
			}
		}
		switch status {
		case agentwork.TestStatusRunning:
			return title, activityStateRunning, activitySeverityInfo
		case agentwork.TestStatusFailed:
			return title, activityStateFailed, activitySeverityError
		default:
			return title, activityStateSucceeded, activitySeveritySuccess
		}
	default:
		title := "Agent activity"
		if activity.Kind != "" {
			title += ": " + string(activity.Kind)
		}
		return title, activityStateSucceeded, activitySeverityInfo
	}
}

func activityAgentStatusLabel(status agentwork.Status) string {
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

func activityAgentTestLabel(status agentwork.TestStatus) string {
	switch status {
	case agentwork.TestStatusRunning:
		return "Running"
	case agentwork.TestStatusPassed:
		return "Passed"
	case agentwork.TestStatusFailed:
		return "Failed"
	default:
		return string(status)
	}
}

func activityAgentStatusProjection(status agentwork.Status) (activityState, activitySeverity) {
	switch status {
	case agentwork.StatusRunning, agentwork.StatusTesting:
		return activityStateRunning, activitySeverityInfo
	case agentwork.StatusNeedsInput:
		return activityStateNeedsInput, activitySeverityWarning
	case agentwork.StatusBlocked:
		return activityStateBlocked, activitySeverityWarning
	case agentwork.StatusFailed:
		return activityStateFailed, activitySeverityError
	case agentwork.StatusDone, agentwork.StatusReadyForReview:
		return activityStateSucceeded, activitySeveritySuccess
	default:
		return activityStateSucceeded, activitySeverityInfo
	}
}

func activityProjectTransfers(appInput activityAppInput, add func(activityItem)) {
	family := activitySessionFamily(appInput)
	for _, transfer := range appInput.Transfers {
		add(activityItem{
			ID:          activityID{Source: activitySourceTransfer, Repo: family, Value: transfer.ID},
			OccurredAt:  transfer.OccurredAt,
			UpdatedAt:   transfer.UpdatedAt,
			Repository:  appInput.Repository,
			Source:      activitySourceTransfer,
			Kind:        activityKindTransfer,
			Severity:    transfer.Severity,
			State:       transfer.State,
			Persistence: activityPersistenceSession,
			Title:       transfer.Title,
			Description: transfer.Description,
			Target: activityTarget{
				Kind: activityTargetTransfer, RepositoryID: appInput.Location, PrimaryID: transfer.ID,
			},
		})
	}
}

func activityProjectNotices(appInput activityAppInput, add func(activityItem)) {
	family := activitySessionFamily(appInput)
	for _, notice := range appInput.Notices {
		target := notice.Target
		if target.Kind == "" {
			target.Kind = activityTargetNone
		}
		target.RepositoryID = appInput.Location
		add(activityItem{
			ID: activityID{
				Source: activitySourceNotice,
				Repo:   family,
				Value:  strconv.FormatUint(notice.ID, 10),
			},
			OccurredAt:  notice.OccurredAt,
			UpdatedAt:   notice.OccurredAt,
			Repository:  appInput.Repository,
			Source:      activitySourceNotice,
			Kind:        activityKindNotice,
			Severity:    notice.Severity,
			State:       activityStateSucceeded,
			Persistence: activityPersistenceSession,
			Title:       notice.Title,
			Description: notice.Description,
			Target:      target,
		})
	}
}

// activitySessionFamily makes reset loggers and identical paths in different
// sessions distinct without weakening durable journal/agent deduplication.
func activitySessionFamily(appInput activityAppInput) string {
	var builder strings.Builder
	activityAppendIdentityPart(&builder, appInput.Repository)
	activityAppendIdentityPart(&builder, appInput.Location)
	activityAppendIdentityPart(&builder, appInput.Session)
	return builder.String()
}

func activityAppendIdentityPart(builder *strings.Builder, value string) {
	builder.WriteString(strconv.Itoa(len(value)))
	builder.WriteByte(':')
	builder.WriteString(value)
}

// activityCandidateHeap is a min-heap of the worst retained candidates. The
// root is therefore the row replaced when a newer candidate arrives.
type activityCandidateHeap []activityItem

func (h activityCandidateHeap) Len() int           { return len(h) }
func (h activityCandidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h activityCandidateHeap) Less(i, j int) bool { return activityCandidateOrder(h[i], h[j]) > 0 }
func (h *activityCandidateHeap) Push(value any)    { *h = append(*h, value.(activityItem)) }
func (h *activityCandidateHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	*h = old[:last]
	return value
}

func activityCandidateHeapAdd(h *activityCandidateHeap, limit int, item activityItem) {
	if limit <= 0 {
		return
	}
	if h.Len() < limit {
		heap.Push(h, item)
		return
	}
	if activityCandidateOrder(item, (*h)[0]) < 0 {
		(*h)[0] = item
		heap.Fix(h, 0)
	}
}

// activityCandidateOrder implements the public ordering, then deterministic
// duplicate/update tie-breakers used only while bounding and merging.
func activityCandidateOrder(left, right activityItem) int {
	if left.OccurredAt.After(right.OccurredAt) {
		return -1
	}
	if left.OccurredAt.Before(right.OccurredAt) {
		return 1
	}
	leftPriority := activitySourcePriority(left.Source)
	rightPriority := activitySourcePriority(right.Source)
	if leftPriority != rightPriority {
		return leftPriority - rightPriority
	}
	if comparison := strings.Compare(left.ID.String(), right.ID.String()); comparison != 0 {
		return comparison
	}
	if left.UpdatedAt.After(right.UpdatedAt) {
		return -1
	}
	if left.UpdatedAt.Before(right.UpdatedAt) {
		return 1
	}
	leftState := activityStateProgress(left.State)
	rightState := activityStateProgress(right.State)
	if leftState != rightState {
		return rightState - leftState
	}
	return strings.Compare(activityDeterministicKey(left), activityDeterministicKey(right))
}

func activitySourcePriority(source activitySource) int {
	switch source {
	case activitySourceCommand:
		return 0
	case activitySourceJournal:
		return 1
	case activitySourceAgent:
		return 2
	case activitySourceTransfer:
		return 3
	case activitySourceNotice:
		return 4
	default:
		return 5
	}
}

func activityPreferUpdate(candidate, current activityItem) bool {
	if candidate.UpdatedAt.After(current.UpdatedAt) {
		return true
	}
	if candidate.UpdatedAt.Before(current.UpdatedAt) {
		return false
	}
	if candidate.OccurredAt.After(current.OccurredAt) {
		return true
	}
	if candidate.OccurredAt.Before(current.OccurredAt) {
		return false
	}
	candidateState := activityStateProgress(candidate.State)
	currentState := activityStateProgress(current.State)
	if candidateState != currentState {
		return candidateState > currentState
	}
	return activityDeterministicKey(candidate) < activityDeterministicKey(current)
}

func activityStateProgress(state activityState) int {
	switch state {
	case activityStateNeedsInput, activityStateBlocked:
		return 1
	case activityStateSucceeded, activityStateFailed:
		return 2
	case activityStateRunning:
		return 0
	default:
		return -1
	}
}

func activityDeterministicKey(item activityItem) string {
	var builder strings.Builder
	for _, value := range []string{
		item.OccurredAt.String(), item.UpdatedAt.String(), item.Repository,
		string(item.Source), string(item.Kind), string(item.Severity),
		string(item.State), string(item.Persistence), item.Title, item.Description,
		string(item.ID.Source), item.ID.Repo, item.ID.Value,
		string(item.Target.Kind), item.Target.RepositoryID, item.Target.PrimaryID,
		item.Target.SecondaryID, string(item.Target.Command),
	} {
		activityAppendIdentityPart(&builder, value)
	}
	return builder.String()
}

func activityMatchesQuery(item activityItem, query activityQuery) bool {
	if query.Repository != "" && item.Repository != query.Repository && item.Target.RepositoryID != query.Repository {
		return false
	}
	if len(query.Sources) != 0 && !query.Sources[item.Source] {
		return false
	}
	if len(query.Severities) != 0 && !query.Severities[item.Severity] {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(query.Text))
	if text == "" {
		return true
	}
	for _, value := range []string{
		item.Title, item.Description, item.Repository, item.Target.RepositoryID,
		item.Target.PrimaryID, item.Target.SecondaryID, item.ID.String(),
		string(item.Source), string(item.Kind), string(item.State),
	} {
		if strings.Contains(strings.ToLower(value), text) {
			return true
		}
	}
	return false
}
