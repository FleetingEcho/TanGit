package app

import (
	"strconv"
	"strings"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type attentionSeverity string

const (
	attentionSeverityInfo    attentionSeverity = "info"
	attentionSeveritySuccess attentionSeverity = "success"
	attentionSeverityWarning attentionSeverity = "warning"
	attentionSeverityError   attentionSeverity = "error"
)

type cockpitRepository struct {
	Name           string
	Root           string
	Branch         string
	Remote         bool
	Clean          bool
	DetailsLoading bool
}

type cockpitWorkingTree struct {
	Staged        int
	Unstaged      int
	Untracked     int
	Conflicted    int
	Reviewed      int
	ReviewTotal   int
	ReviewLoading bool
}

type cockpitOperation struct {
	Name        string
	Description string
	Command     commandID
}

type cockpitCollaboration struct {
	Ahead           int
	Behind          int
	DivergenceKnown bool
	Upstream        string
	ChecksCached    bool
	ChecksLoading   bool
	ChecksSummary   string
}

type cockpitAgents struct {
	Loaded      bool
	Loading     bool
	Error       string
	Running     int
	NeedsInput  int
	Testing     int
	ReadyReview int
	Blocked     int
	FailedTests int
}

type cockpitBackground struct {
	TransfersRunning int
	TransfersFailed  int
	Indexing         bool
	LSPBusy          bool
}

type cockpitAttention struct {
	ID          string
	Severity    attentionSeverity
	Title       string
	Description string
	Command     commandID
}

type cockpitSummary struct {
	Repository    cockpitRepository
	Attention     []cockpitAttention
	WorkingTree   cockpitWorkingTree
	Operation     *cockpitOperation
	Collaboration cockpitCollaboration
	Agents        cockpitAgents
	Background    cockpitBackground
	Recent        []activityItem
}

type cockpitInput struct {
	Snapshot       repository.Snapshot
	DetailsLoading bool
	Crash          string
	ActionError    string
	CommandFailure string
	Reviewed       int
	ReviewTotal    int
	ReviewLoading  bool
	DirtyEditors   int
	Agents         []agentwork.Task
	AgentsLoaded   bool
	AgentsLoading  bool
	AgentsError    string
	ChecksText     string
	ChecksError    string
	ChecksLoading  bool
	Transfers      []activityTransfer
	Indexing       bool
	LSPBusy        bool
	Recent         []activityItem
	Remote         bool
}

func cockpitErrorSummary(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return shorten(repository.RedactSecrets(statusErrorLine(value)), 240)
		}
	}
	return ""
}

func buildCockpitSummary(input cockpitInput) cockpitSummary {
	files := input.Snapshot.Files
	summary := cockpitSummary{
		Repository: cockpitRepository{
			Name: repoName(input.Snapshot.Root), Root: input.Snapshot.Root,
			Branch: input.Snapshot.Branch, Remote: input.Remote,
			Clean: len(files) == 0, DetailsLoading: input.DetailsLoading,
		},
		WorkingTree: cockpitWorkingTree{
			Reviewed: input.Reviewed, ReviewTotal: input.ReviewTotal, ReviewLoading: input.ReviewLoading,
		},
		Agents: cockpitAgents{
			Loaded: input.AgentsLoaded, Loading: input.AgentsLoading,
			Error: cockpitErrorSummary(input.AgentsError),
		},
		Background: cockpitBackground{Indexing: input.Indexing, LSPBusy: input.LSPBusy},
		Recent:     append([]activityItem(nil), input.Recent...),
	}
	if summary.Repository.Name == "" {
		summary.Repository.Name = input.Snapshot.Root
	}
	for _, file := range files {
		if file.HasMergeConflicts {
			summary.WorkingTree.Conflicted++
			continue
		}
		if file.Staged {
			summary.WorkingTree.Staged++
		}
		if !file.Tracked {
			summary.WorkingTree.Untracked++
		} else if file.Unstaged {
			summary.WorkingTree.Unstaged++
		}
	}
	for _, branch := range input.Snapshot.Branches {
		if !branch.Head {
			continue
		}
		summary.Collaboration.Upstream = upstreamName(branch)
		ahead, behind, known := aheadBehind(branch)
		if known {
			summary.Collaboration.DivergenceKnown = true
			summary.Collaboration.Ahead, _ = strconv.Atoi(ahead)
			summary.Collaboration.Behind, _ = strconv.Atoi(behind)
		}
		break
	}
	if input.ChecksLoading || strings.TrimSpace(input.ChecksText) != "" || strings.TrimSpace(input.ChecksError) != "" {
		summary.Collaboration.ChecksCached = true
	}
	summary.Collaboration.ChecksLoading = input.ChecksLoading
	summary.Collaboration.ChecksSummary = cockpitErrorSummary(input.ChecksError, input.ChecksText)
	if input.ChecksLoading && summary.Collaboration.ChecksSummary == "" {
		summary.Collaboration.ChecksSummary = "Checks are loading only because their existing dialog requested them."
	}
	if summary.Collaboration.ChecksSummary == "" && !input.ChecksLoading {
		summary.Collaboration.ChecksSummary = "No checks have been requested in this session."
	}
	for _, transfer := range input.Transfers {
		switch transfer.State {
		case activityStateRunning:
			summary.Background.TransfersRunning++
		case activityStateFailed:
			summary.Background.TransfersFailed++
		}
	}
	for _, task := range input.Agents {
		switch task.Status {
		case agentwork.StatusRunning:
			summary.Agents.Running++
		case agentwork.StatusNeedsInput:
			summary.Agents.NeedsInput++
		case agentwork.StatusTesting:
			summary.Agents.Testing++
		case agentwork.StatusReadyForReview:
			summary.Agents.ReadyReview++
		case agentwork.StatusBlocked:
			summary.Agents.Blocked++
		}
		if task.LastTest != nil && task.LastTest.Status == agentwork.TestStatusFailed {
			summary.Agents.FailedTests++
		}
	}
	if operation := cockpitOperationFor(input); operation != nil {
		summary.Operation = operation
	}
	summary.Attention = cockpitAttentions(input, summary)
	return summary
}

func cockpitOperationFor(input cockpitInput) *cockpitOperation {
	if strings.TrimSpace(input.Snapshot.Operation) != "" {
		return &cockpitOperation{
			Name:        input.Snapshot.Operation,
			Description: "Git has an interrupted operation that needs a safe next step.",
			Command:     commandContinue,
		}
	}
	if input.Snapshot.Bisect != nil && input.Snapshot.Bisect.Active {
		return &cockpitOperation{
			Name: "bisect", Description: "A bisect is active. Review the current test commit before marking it.",
			Command: "workspace/history",
		}
	}
	return nil
}

func cockpitAttentions(input cockpitInput, summary cockpitSummary) []cockpitAttention {
	candidates := make([]cockpitAttention, 0, 10)
	add := func(id string, severity attentionSeverity, title, description string, command commandID) {
		candidates = append(candidates, cockpitAttention{ID: id, Severity: severity, Title: title, Description: description, Command: command})
	}
	if failure := cockpitErrorSummary(input.Crash, input.ActionError, input.CommandFailure); failure != "" {
		command := commandOpenLog
		if strings.TrimSpace(input.Crash) != "" {
			command = commandOpenRefresh
		}
		add("failure", attentionSeverityError, "An action needs attention", failure, command)
	}
	if summary.Operation != nil {
		add("operation", attentionSeverityWarning, trf("Continue the interrupted %s", summary.Operation.Name),
			summary.Operation.Description, summary.Operation.Command)
	}
	if summary.WorkingTree.Conflicted > 0 {
		add("conflicts", attentionSeverityError, trf("Resolve %d conflicted files", summary.WorkingTree.Conflicted),
			"Unresolved conflicts block ordinary staged actions and Continue.", commandOpenChanges)
	}
	if input.DirtyEditors > 0 {
		add("editor-buffers", attentionSeverityWarning, trf("Save %d unsaved editor buffers", input.DirtyEditors),
			"Unsaved buffers are kept separate from Git working-tree changes.", "workspace/code")
	}
	switch {
	case summary.Agents.NeedsInput > 0:
		add("agent-input", attentionSeverityWarning, trf("%d Agent tasks need input", summary.Agents.NeedsInput),
			"Open Agent tasks to answer the pending request.", commandOpenAgents)
	case summary.Agents.FailedTests > 0:
		add("agent-tests", attentionSeverityError, trf("%d Agent tasks have failed tests", summary.Agents.FailedTests),
			"Review test output before marking a result ready.", commandOpenAgents)
	case summary.Agents.Blocked > 0:
		add("agent-blocked", attentionSeverityWarning, trf("%d Agent tasks are blocked", summary.Agents.Blocked),
			"Inspect the blocked task and its latest durable activity.", commandOpenAgents)
	case summary.Agents.ReadyReview > 0:
		add("agent-ready", attentionSeveritySuccess, trf("%d Agent results are ready for review", summary.Agents.ReadyReview),
			"Review the explicit Base-to-result difference before marking work done.", commandOpenAgents)
	}
	if summary.Background.TransfersFailed > 0 {
		add("transfer-failed", attentionSeverityError, trf("%d transfers failed or completed partially", summary.Background.TransfersFailed),
			"Open Remotes to inspect the failed or partial transfer.", "workspace/remotes")
	}
	if summary.Collaboration.Ahead > 0 {
		add("branch-ahead", attentionSeverityInfo, trf("Push %d local commits", summary.Collaboration.Ahead),
			"The current branch has commits that are not published upstream.", commandOpenPush)
	} else if summary.Collaboration.Behind > 0 {
		add("branch-behind", attentionSeverityInfo, trf("Pull %d upstream commits", summary.Collaboration.Behind),
			"Review and integrate upstream changes with the existing Pull flow.", commandOpenPull)
	}
	if summary.WorkingTree.Staged > 0 {
		add("staged", attentionSeverityInfo, trf("Commit %d staged files", summary.WorkingTree.Staged),
			"Staged changes are ready for a commit message.", commandCommitStaged)
	} else if summary.WorkingTree.Unstaged+summary.WorkingTree.Untracked > 0 {
		add("dirty", attentionSeverityInfo, "Review working-tree changes",
			"Open Changes to review modified and untracked files.", commandOpenChanges)
	}
	if len(candidates) == 0 {
		add("clean", attentionSeveritySuccess, "Repository is clean and ready",
			"No current action needs attention. Continue with Changes, History or a sync action.", commandOpenChanges)
	}
	if input.DetailsLoading {
		candidates = append(candidates, cockpitAttention{
			ID: "details-loading", Severity: attentionSeverityInfo, Title: "Repository details are still loading",
			Description: "Core status is available; ancillary history and operation details are not being reported as empty.",
			Command:     commandOpenRefresh,
		})
	}
	if len(candidates) > 3 {
		candidates = candidates[:3]
	}
	return candidates
}

// repositoryStartsOnOverview is the side-effect-free startup preference
// decision. A loaded fresh profile may opt in; legacy profiles retain their
// previous landing section, and editor-only folders always use Code.
func (a *app) repositoryStartsOnOverview() bool {
	return a != nil && a.settingsLoaded && a.settings.OpenRepositoriesOnOverview &&
		!a.editorOnlyWorkspace() && a.sectionVisible(sectionOverview)
}

// prepareCockpit runs only on an explicit section transition or repository
// installation. Rendering calls only the pure summary and projection modules.
func (a *app) prepareCockpit() {
	if a == nil || a.state != stateReady || a.editorOnlyWorkspace() || a.workspaceTab != sectionOverview {
		return
	}
	repositoryID := a.agentRepositoryID()
	if a.agents.loadedRepo != repositoryID && !a.agents.loading {
		a.refreshAgentTasks()
	}
	a.loadCockpitContributors()
}
