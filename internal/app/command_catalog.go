package app

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode"

	"lazygitapp/internal/repository"
)

// commandID is the stable identity shared by the palette, Cockpit and
// activity targets. Candidates deliberately never expose an execution closure.
type commandID string

type commandGroup uint8

const (
	commandGroupApplication commandGroup = iota
	commandGroupNavigation
	commandGroupRepository
	commandGroupChanges
	commandGroupHistory
	commandGroupBranches
	commandGroupRemotes
	commandGroupStash
	commandGroupTags
	commandGroupWorktrees
	commandGroupAgents
	commandGroupSubmodules
	commandGroupCustom
)

var commandGroupLabels = [...]string{
	"Application", "Navigation", "Repository", "Changes", "History and graph",
	"Branches", "Remotes", "Stash", "Tags", "Worktrees", "Agent tasks",
	"Submodules", "Custom commands",
}

func (g commandGroup) label() string {
	if int(g) >= len(commandGroupLabels) {
		return "Commands"
	}
	return commandGroupLabels[g]
}

type commandKind uint8

const (
	commandIconDefault commandKind = iota
	commandIconNavigation
	commandIconFetch
	commandIconPull
	commandIconPush
	commandIconRefresh
	commandIconTerminal
	commandIconLog
	commandIconSettings
	commandIconHelp
	commandIconCommit
	commandIconContinue
	commandIconActivity
	commandIconAgent
	commandIconCustom
)

type commandContext struct {
	Shell *shell
	App   *app
}

type commandState struct {
	Visible        bool
	Enabled        bool
	DisabledReason string
	ContextScore   int
}

type commandSpec struct {
	ID       commandID
	Label    string
	Detail   string
	Keywords []string
	Group    commandGroup
	Chord    string
	Icon     commandKind
	Resolve  func(commandContext) commandState
	Run      func(commandContext)
}

type textRange struct {
	Start int
	End   int
}

type commandCandidate struct {
	ID             commandID
	Label          string
	Detail         string
	Group          commandGroup
	Chord          string
	Icon           commandKind
	Enabled        bool
	DisabledReason string
	MatchRanges    []textRange

	matchRank   int
	contextRank int
	recentRank  int
	emptyRank   int
}

type commandInvokeStatus uint8

const (
	commandInvokeUnknown commandInvokeStatus = iota
	commandInvokeHidden
	commandInvokeDisabled
	commandInvokeAccepted
)

type commandInvokeResult struct {
	Status commandInvokeStatus
	Reason string
}

func (r commandInvokeResult) Accepted() bool { return r.Status == commandInvokeAccepted }

type commandCatalog struct {
	specs []commandSpec
}

func newCommandCatalog(specs []commandSpec) *commandCatalog {
	return &commandCatalog{specs: append([]commandSpec(nil), specs...)}
}

// validate command IDs independently of the UI. Duplicate IDs would make an
// invoke-time revalidation resolve a different action than the selected row.
func (c *commandCatalog) validate() error {
	seen := make(map[commandID]struct{}, len(c.specs))
	for _, spec := range c.specs {
		if strings.TrimSpace(string(spec.ID)) == "" {
			return &commandCatalogError{"empty command ID"}
		}
		if _, ok := seen[spec.ID]; ok {
			return &commandCatalogError{"duplicate command ID: " + string(spec.ID)}
		}
		seen[spec.ID] = struct{}{}
	}
	return nil
}

type commandCatalogError struct{ message string }

func (e *commandCatalogError) Error() string { return e.message }

type commandMatch struct {
	rank   int
	ranges []textRange
}

func commandTextMatch(spec commandSpec, label, query string) (commandMatch, bool) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return commandMatch{}, true
	}
	localized := strings.ToLower(label)
	canonical := strings.ToLower(spec.Label)
	id := strings.ToLower(string(spec.ID))
	for _, candidate := range []struct {
		text  string
		label bool
	}{{localized, true}, {canonical, false}, {id, false}} {
		if candidate.text == query {
			match := commandMatch{rank: 0}
			if candidate.label {
				match.ranges = []textRange{{Start: 0, End: len(query)}}
			}
			return match, true
		}
	}
	for _, candidate := range []struct {
		text  string
		label bool
	}{{localized, true}, {canonical, false}, {id, false}} {
		if strings.HasPrefix(candidate.text, query) {
			match := commandMatch{rank: 1}
			if candidate.label {
				match.ranges = []textRange{{Start: 0, End: len(query)}}
			}
			return match, true
		}
	}
	for fieldIndex, field := range []string{localized, canonical} {
		for _, token := range strings.FieldsFunc(field, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune("/:_-–—", r)
		}) {
			if !strings.HasPrefix(token, query) {
				continue
			}
			match := commandMatch{rank: 2}
			if fieldIndex == 0 {
				index := strings.Index(field, token)
				match.ranges = []textRange{{Start: index, End: index + len(query)}}
			}
			return match, true
		}
	}
	for _, keyword := range append(append([]string(nil), spec.Keywords...), spec.Chord) {
		if strings.Contains(strings.ToLower(keyword), query) {
			return commandMatch{rank: 3}, true
		}
	}
	if index := strings.Index(localized, query); index >= 0 {
		return commandMatch{rank: 4, ranges: []textRange{{Start: index, End: index + len(query)}}}, true
	}
	if index := strings.Index(canonical, query); index >= 0 {
		return commandMatch{rank: 4}, true
	}
	if ranges, ok := subsequenceRanges(localized, query); ok {
		return commandMatch{rank: 5, ranges: ranges}, true
	}
	if _, ok := subsequenceRanges(canonical, query); ok {
		return commandMatch{rank: 5}, true
	}
	return commandMatch{}, false
}

func subsequenceRanges(text, query string) ([]textRange, bool) {
	if query == "" {
		return nil, true
	}
	var ranges []textRange
	remaining := query
	for index := 0; index < len(text) && remaining != ""; {
		// Search is ASCII case-folded by the callers for command labels. UTF-8
		// bytes that do not match are skipped as a complete rune.
		if text[index] == remaining[0] {
			start := index
			for index < len(text) && remaining != "" && text[index] == remaining[0] {
				index++
				remaining = remaining[1:]
			}
			ranges = append(ranges, textRange{Start: start, End: index})
			continue
		}
		_, size := utf8DecodeRuneInString(text[index:])
		index += size
	}
	return ranges, remaining == ""
}

// utf8DecodeRuneInString is a tiny seam that keeps match offsets in the
// original string without normalizing or reallocating its label.
func utf8DecodeRuneInString(value string) (rune, int) {
	for _, r := range value {
		return r, len(string(r))
	}
	return 0, 1
}

// Search resolves every candidate at read time. Resolve functions must only
// inspect published UI state; they never execute Git, disk or network work.
func (c *commandCatalog) Search(ctx commandContext, query string, recent []commandID) []commandCandidate {
	if c == nil {
		return nil
	}
	query = strings.TrimSpace(query)
	recentRanks := make(map[commandID]int, len(recent))
	for index, id := range recent {
		if _, ok := recentRanks[id]; !ok {
			recentRanks[id] = index
		}
	}
	candidates := make([]commandCandidate, 0, len(c.specs))
	for _, spec := range c.specs {
		state := commandState{}
		if spec.Resolve != nil {
			state = spec.Resolve(ctx)
		}
		if !state.Visible {
			continue
		}
		label := tr(spec.Label)
		match, matched := commandTextMatch(spec, label, query)
		if !matched {
			continue
		}
		recentRank, isRecent := recentRanks[spec.ID]
		if !isRecent {
			recentRank = len(recent) + 1
		}
		emptyRank := 3
		if query == "" {
			switch {
			case state.ContextScore >= 15:
				emptyRank = 0
			case isRecent:
				emptyRank = 1
			case spec.Group == commandGroupNavigation:
				emptyRank = 2
			default:
				continue
			}
		}
		candidates = append(candidates, commandCandidate{
			ID: spec.ID, Label: label, Detail: tr(spec.Detail), Group: spec.Group,
			Chord: spec.Chord, Icon: spec.Icon, Enabled: state.Enabled,
			DisabledReason: tr(state.DisabledReason), MatchRanges: match.ranges,
			matchRank: match.rank, contextRank: state.ContextScore,
			recentRank: recentRank, emptyRank: emptyRank,
		})
	}
	slices.SortStableFunc(candidates, func(a, b commandCandidate) int {
		for _, comparison := range []int{
			a.emptyRank - b.emptyRank,
			a.matchRank - b.matchRank,
			b.contextRank - a.contextRank,
			a.recentRank - b.recentRank,
			int(a.Group) - int(b.Group),
			strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label)),
			strings.Compare(string(a.ID), string(b.ID)),
		} {
			if comparison != 0 {
				return comparison
			}
		}
		return 0
	})
	if query == "" && len(candidates) > 40 {
		candidates = candidates[:40]
	}
	return candidates
}

// Invoke resolves the spec again immediately before running it. In particular,
// it never trusts a candidate created before a tab, selection or refresh change.
func (c *commandCatalog) Invoke(ctx commandContext, id commandID) commandInvokeResult {
	if c == nil {
		return commandInvokeResult{Status: commandInvokeUnknown, Reason: "Command is unavailable"}
	}
	for _, spec := range c.specs {
		if spec.ID != id {
			continue
		}
		state := commandState{}
		if spec.Resolve != nil {
			state = spec.Resolve(ctx)
		}
		if !state.Visible {
			return commandInvokeResult{Status: commandInvokeHidden, Reason: "Command is not available in this context"}
		}
		if !state.Enabled || spec.Run == nil {
			reason := state.DisabledReason
			if reason == "" {
				reason = "Command is currently unavailable"
			}
			return commandInvokeResult{Status: commandInvokeDisabled, Reason: reason}
		}
		if current := ctx.App; current != nil {
			current.commandLog.RecordPalette(string(spec.ID), tr(spec.Label))
		} else if ctx.Shell != nil {
			if current := ctx.Shell.activeApp(); current != nil {
				current.commandLog.RecordPalette(string(spec.ID), tr(spec.Label))
			}
		}
		spec.Run(ctx)
		return commandInvokeResult{Status: commandInvokeAccepted}
	}
	return commandInvokeResult{Status: commandInvokeUnknown, Reason: "Command is unavailable"}
}

const (
	commandOpenSettings   commandID = "settings/open"
	commandOpenHelp       commandID = "keybindings/open"
	commandOpenFetch      commandID = "repository/fetch"
	commandOpenPull       commandID = "repository/pull"
	commandOpenPush       commandID = "repository/push"
	commandOpenRefresh    commandID = "repository/refresh"
	commandOpenTerminal   commandID = "terminal/toggle"
	commandOpenLog        commandID = "command-log/open"
	commandCommitStaged   commandID = "changes/commit-staged"
	commandContinue       commandID = "operation/continue"
	commandOpenRecovery   commandID = "recovery/open"
	commandOpenPRChecks   commandID = "branches/pr-checks"
	commandOpenCockpit    commandID = "workspace/overview"
	commandOpenChanges    commandID = "workspace/changes"
	commandOpenAgents     commandID = "workspace/agents"
	commandNextUnreviewed commandID = "git/Changes/Go to the next unreviewed file"
)

var (
	staticSpecsOnce sync.Once
	staticSpecs     []commandSpec
	staticCatalog   *commandCatalog
)

func staticCommandSpecs() []commandSpec {
	staticSpecsOnce.Do(func() {
		staticSpecs = buildStaticCommandSpecs()
		staticCatalog = newCommandCatalog(staticSpecs)
	})
	return staticSpecs
}

func buildStaticCommandSpecs() []commandSpec {
	specs := []commandSpec{
		{ID: "tabs/new-home", Label: "Open a new Home tab", Detail: "Open the repository picker in a new tab", Keywords: []string{"picker", "home", "new tab"}, Group: commandGroupApplication, Chord: cmdShortcutChord() + "+T", Icon: commandIconNavigation, Resolve: shellState, Run: func(ctx commandContext) { ctx.Shell.request(ctx.Shell.showHome) }},
		{ID: "tabs/close-active", Label: "Close the active tab", Group: commandGroupApplication, Chord: cmdShortcutChord() + "+W", Icon: commandIconNavigation, Resolve: func(ctx commandContext) commandState {
			state := shellState(ctx)
			if ctx.Shell == nil || ctx.Shell.activeTab() == nil || !ctx.Shell.canClose(ctx.Shell.activeTab()) {
				state.Enabled = false
				state.DisabledReason = "The last Home tab cannot be closed"
			}
			return state
		}, Run: func(ctx commandContext) {
			ctx.Shell.request(func() {
				if tab := ctx.Shell.activeTab(); tab != nil {
					ctx.Shell.closeTab(tab)
				}
			})
		}},
		{ID: "tabs/cycle-next", Label: "Switch to the next tab", Group: commandGroupApplication, Chord: "Ctrl+Tab", Icon: commandIconNavigation, Resolve: shellState, Run: func(ctx commandContext) { ctx.Shell.request(func() { ctx.Shell.cycleTab(1) }) }},
		{ID: "tabs/cycle-previous", Label: "Switch to the previous tab", Group: commandGroupApplication, Chord: "Ctrl+Shift+Tab", Icon: commandIconNavigation, Resolve: shellState, Run: func(ctx commandContext) { ctx.Shell.request(func() { ctx.Shell.cycleTab(-1) }) }},
		{ID: commandOpenSettings, Label: "Open Settings", Detail: "Preferences, workspace sections and custom commands", Keywords: []string{"preferences", "settings"}, Group: commandGroupApplication, Chord: "Ctrl+,", Icon: commandIconSettings, Resolve: appState, Run: func(ctx commandContext) { ctx.App.openSettings() }},
		{ID: commandOpenHelp, Label: "Open Keyboard Shortcuts", Keywords: []string{"help", "keys", "shortcut help"}, Group: commandGroupApplication, Chord: "F1", Icon: commandIconHelp, Resolve: appState, Run: func(ctx commandContext) { ctx.App.openKeybindings() }},
		{ID: commandOpenFetch, Label: "Fetch from the remotes", Keywords: []string{"fetch", "download", "sync"}, Group: commandGroupRepository, Chord: "f", Icon: commandIconFetch, Resolve: fetchState, Run: func(ctx commandContext) { ctx.App.beginFetch() }},
		{ID: commandOpenPull, Label: "Pull from the upstream branch", Keywords: []string{"pull", "update", "integrate"}, Group: commandGroupRepository, Chord: "p", Icon: commandIconPull, Resolve: repositoryState, Run: func(ctx commandContext) { ctx.App.confirmPull() }},
		{ID: commandOpenPush, Label: "Push to the remote", Keywords: []string{"push", "publish", "upload"}, Group: commandGroupRepository, Chord: "Shift+P", Icon: commandIconPush, Resolve: repositoryState, Run: func(ctx commandContext) { ctx.App.confirmPush() }},
		{ID: commandOpenRefresh, Label: "Refresh the repository", Keywords: []string{"reload", "refresh", "status"}, Group: commandGroupRepository, Chord: "F5", Icon: commandIconRefresh, Resolve: repositoryState, Run: func(ctx commandContext) { ctx.App.beginRefresh() }},
		{ID: commandOpenTerminal, Label: "Toggle Terminal", Keywords: []string{"terminal", "shell", "console"}, Group: commandGroupApplication, Chord: "Ctrl+`", Icon: commandIconTerminal, Resolve: readyAppState, Run: func(ctx commandContext) { ctx.App.toggleTerminalPanel() }},
		{ID: commandOpenLog, Label: "Open Command Log", Keywords: []string{"commands", "output", "errors", "git log"}, Group: commandGroupApplication, Chord: "Ctrl+L", Icon: commandIconLog, Resolve: repositoryStateAllowBusy, Run: func(ctx commandContext) { ctx.App.openCommandLog() }},
		{ID: commandCommitStaged, Label: "Commit staged changes", Detail: "Write a commit message for the current staged changes", Keywords: []string{"commit", "save", "staged"}, Group: commandGroupChanges, Chord: "c", Icon: commandIconCommit, Resolve: commitState, Run: func(ctx commandContext) { ctx.App.showCommitForm = true }},
		{ID: commandContinue, Label: "Continue the interrupted operation", Keywords: []string{"continue", "merge", "rebase", "cherry-pick"}, Group: commandGroupRepository, Icon: commandIconContinue, Resolve: continueState, Run: func(ctx commandContext) { ctx.App.confirmContinueOperation() }},
		{ID: commandOpenRecovery, Label: "Open Safety Journal", Keywords: []string{"recovery", "undo", "journal", "operations"}, Group: commandGroupRepository, Icon: commandIconActivity, Resolve: repositoryStateAllowBusy, Run: func(ctx commandContext) { ctx.App.openActivityRecovery() }},
		{ID: commandOpenPRChecks, Label: "Open Pull Request Checks", Keywords: []string{"checks", "ci", "pull request", "github"}, Group: commandGroupBranches, Icon: commandIconActivity, Resolve: repositoryStateAllowBusy, Run: func(ctx commandContext) { ctx.App.openPullRequestChecks() }},
	}
	for section := 0; section < workspaceSectionCount; section++ {
		section := section
		id := commandID("workspace/" + workspaceSectionIDs[section])
		if id == commandOpenCockpit || id == commandOpenChanges || id == commandOpenAgents {
			// The constants above name these same destinations; only one spec is
			// allowed for each stable ID.
			continue
		}
		specs = append(specs, commandSpec{
			ID: id, Label: "Open " + workspaceSections[section], Keywords: []string{workspaceSectionIDs[section], workspaceSections[section], "section", "navigate"}, Group: commandGroupNavigation, Icon: commandIconNavigation,
			Resolve: workspaceState(section), Run: func(ctx commandContext) { ctx.App.setWorkspaceTab(section) },
		})
	}
	for _, section := range []struct {
		id    commandID
		index int
	}{{commandOpenCockpit, sectionOverview}, {commandOpenChanges, sectionChanges}, {commandOpenAgents, sectionAgents}} {
		specs = append(specs, commandSpec{ID: section.id, Label: "Open " + workspaceSections[section.index], Keywords: []string{workspaceSectionIDs[section.index], workspaceSections[section.index], "section", "navigate"}, Group: commandGroupNavigation, Icon: commandIconNavigation, Resolve: workspaceState(section.index), Run: func(ctx commandContext) { ctx.App.setWorkspaceTab(section.index) }})
	}
	seenKeyboardIDs := map[commandID]int{}
	for _, command := range keyboardCommands() {
		if command.label == "Fetch from the remotes" || command.label == "Pull from the upstream branch" || command.label == "Push to the remote" {
			continue
		}
		command := command
		id := commandID("git/" + command.group + "/" + command.label)
		seenKeyboardIDs[id]++
		if seenKeyboardIDs[id] > 1 {
			// A few legacy actions intentionally share a label but have distinct
			// chords (for example both Worktree open bindings). Preserve the
			// first stable ID and namespace later variants by their chord.
			id += commandID("/" + command.keys)
		}
		specs = append(specs, commandSpec{
			ID: id, Label: command.label,
			Group: commandGroupForKeyboard(command.group), Chord: command.keys, Icon: commandIconDefault,
			Keywords: []string{command.group, command.label},
			Resolve: func(ctx commandContext) commandState {
				if id == commandNextUnreviewed && ctx.App != nil && ctx.App.workspaceTab == sectionOverview {
					state := repositoryState(ctx)
					reviewed, total := ctx.App.reviewProgress()
					if state.Enabled && reviewed >= total {
						state.Enabled = false
						state.DisabledReason = "Every changed file has been reviewed"
					}
					return state
				}
				return keyboardState(ctx, command)
			},
			Run: func(ctx commandContext) {
				if id == commandNextUnreviewed && ctx.App.workspaceTab == sectionOverview {
					ctx.App.setWorkspaceTab(sectionChanges)
				}
				command.run(ctx.App)
			},
		})
	}
	return specs
}

func shellState(ctx commandContext) commandState {
	if ctx.Shell == nil {
		return commandState{Visible: true, DisabledReason: "The window is unavailable"}
	}
	return commandState{Visible: true, Enabled: true, ContextScore: 15}
}

func appState(ctx commandContext) commandState {
	if ctx.App == nil {
		return commandState{Visible: true, DisabledReason: "No workspace is active"}
	}
	return commandState{Visible: true, Enabled: true, ContextScore: 15}
}

func readyAppState(ctx commandContext) commandState {
	state := appState(ctx)
	if ctx.App == nil || ctx.App.state != stateReady {
		state.Enabled = false
		state.DisabledReason = "Open a workspace first"
	}
	return state
}

func repositoryStateAllowBusy(ctx commandContext) commandState {
	state := readyAppState(ctx)
	if ctx.App != nil && ctx.App.state == stateReady && ctx.App.editorOnlyWorkspace() {
		state.Enabled = false
		state.DisabledReason = "This editor-only workspace has no Git repository"
	}
	if state.Enabled {
		state.ContextScore = 15
	}
	return state
}

func repositoryState(ctx commandContext) commandState {
	state := repositoryStateAllowBusy(ctx)
	if state.Enabled && ctx.App.actionBusy {
		state.Enabled = false
		state.DisabledReason = "Wait for the current repository action to finish"
	}
	if state.Enabled && ctx.App.session == nil {
		state.Enabled = false
		state.DisabledReason = "No repository session is available"
	}
	return state
}

func fetchState(ctx commandContext) commandState {
	state := repositoryState(ctx)
	if state.Enabled && len(ctx.App.snapshot.Remotes) == 0 {
		state.Enabled = false
		state.DisabledReason = "No remotes are configured"
	}
	return state
}

func commitState(ctx commandContext) commandState {
	state := repositoryState(ctx)
	state.Visible = true
	if state.Enabled && !hasStagedFiles(ctx.App.snapshot.Files) {
		state.Enabled = false
		state.DisabledReason = "No staged changes to commit"
	}
	return state
}

func continueState(ctx commandContext) commandState {
	state := repositoryState(ctx)
	state.Visible = true
	if ctx.App == nil || ctx.App.state != stateReady {
		return state
	}
	if state.Enabled && ctx.App.snapshot.Operation == "" {
		state.Enabled = false
		state.DisabledReason = "No merge, rebase or other interrupted operation is in progress"
	} else if state.Enabled && hasConflictedFiles(ctx.App.snapshot.Files) {
		state.Enabled = false
		state.DisabledReason = "Resolve all conflicts before continuing"
	} else if state.Enabled && !ctx.App.operationCan(repository.OperationActionContinue) {
		state.Enabled = false
		state.DisabledReason = "The current operation cannot be continued"
	} else if state.Enabled && ctx.App.location.IsRemote() {
		state.Enabled = false
		state.DisabledReason = "Continue checks are disabled for SSH workspaces"
	}
	return state
}

func workspaceState(section int) func(commandContext) commandState {
	return func(ctx commandContext) commandState {
		state := readyAppState(ctx)
		if ctx.App == nil || ctx.App.state != stateReady || section < 0 || section >= workspaceSectionCount || !slices.Contains(ctx.App.visibleSections(), section) {
			state.Visible = false
			return state
		}
		state.Enabled = true
		if ctx.App.workspaceTab == section {
			state.ContextScore = 20
		} else {
			state.ContextScore = 10
		}
		return state
	}
}

func commandGroupForKeyboard(group string) commandGroup {
	switch group {
	case "Changes":
		return commandGroupChanges
	case "History and graph", "Historical files":
		return commandGroupHistory
	case "Branches":
		return commandGroupBranches
	case "Remotes", "Remote branches":
		return commandGroupRemotes
	case "Stash":
		return commandGroupStash
	case "Tags":
		return commandGroupTags
	case "Worktrees":
		return commandGroupWorktrees
	case "Agent tasks":
		return commandGroupAgents
	case "Submodules":
		return commandGroupSubmodules
	default:
		return commandGroupRepository
	}
}

func currentKeyboardGroup(a *app) string {
	if a == nil {
		return ""
	}
	switch a.workspaceTab {
	case sectionChanges:
		return "Changes"
	case sectionHistory, sectionGraph, sectionCompare, sectionReflog:
		return "History and graph"
	case sectionBranches:
		return "Branches"
	case sectionRemotes:
		return "Remotes"
	case sectionStash:
		return "Stash"
	case sectionTags:
		return "Tags"
	case sectionWorktrees:
		return "Worktrees"
	case sectionAgents:
		return "Agent tasks"
	case sectionSubmodules:
		return "Submodules"
	default:
		return ""
	}
}

func keyboardState(ctx commandContext, command keyboardCommand) commandState {
	state := repositoryState(ctx)
	group := currentKeyboardGroup(ctx.App)
	if command.group != "Everywhere" && command.group != group {
		state.Visible = false
		return state
	}
	if group == "Remote branches" && command.group == "Remotes" {
		state.Visible = false
		return state
	}
	if state.Enabled && (command.label == "Write a commit message" || command.label == "Commit the staged changes" || command.label == "Amend the chosen commit with staged changes") && !hasStagedFiles(ctx.App.snapshot.Files) {
		state.Enabled = false
		state.DisabledReason = "No staged changes to commit"
	}
	if command.group == group || command.group == "Everywhere" {
		state.ContextScore = 20
	}
	return state
}

func (s *shell) catalogFor(ctx commandContext) *commandCatalog {
	_ = staticCommandSpecs()
	actions := workflowActionSpecs(ctx)
	if ctx.App == nil || (len(ctx.App.settings.CustomCommands) == 0 && len(actions) == 0) {
		return staticCatalog
	}
	base := append([]commandSpec(nil), staticSpecs...)
	base = append(base, actions...)
	used := make(map[commandID]bool, len(base)+len(ctx.App.settings.CustomCommands))
	for _, spec := range base {
		used[spec.ID] = true
	}
	if ctx.App != nil {
		group := currentKeyboardGroup(ctx.App)
		for _, custom := range ctx.App.settings.CustomCommands {
			custom := custom
			id := commandID(customShortcutDefinition(custom).id)
			for suffix := 2; used[id]; suffix++ {
				id = commandID(fmt.Sprintf("%s/%d", customShortcutDefinition(custom).id, suffix))
			}
			used[id] = true
			base = append(base, commandSpec{
				ID: id, Label: customShortcutDefinition(custom).label,
				Detail: custom.Command, Keywords: []string{"custom", "shell", custom.Command}, Group: commandGroupCustom,
				Chord: customCommandDisplayKey(custom.Key), Icon: commandIconCustom,
				Resolve: func(ctx commandContext) commandState {
					state := repositoryState(ctx)
					if state.Enabled && !customCommandInContext(custom, group) {
						state.Enabled = false
						state.DisabledReason = "This custom command is restricted to another workspace section"
					}
					if state.Enabled && ctx.App.location.IsRemote() {
						state.Enabled = false
						state.DisabledReason = "Custom shell commands are unavailable in SSH workspaces"
					}
					return state
				},
				Run: func(ctx commandContext) { ctx.App.runCustomCommandByMenu(custom) },
			})
		}
	}
	return newCommandCatalog(base)
}
