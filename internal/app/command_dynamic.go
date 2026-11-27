package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const (
	dynamicCommitLimit = 300
	dynamicFileLimit   = 500
)

// dynamicCommandSpecs builds the commands that depend on live state: the open
// tabs always, and the branches ('@'), commits ('#'), changed files ('/') and
// the typed shell line ('!') only while the palette is in that mode, so the
// default list is never flooded with them.
func (s *shell) dynamicCommandSpecs(ctx commandContext) []commandSpec {
	if s == nil {
		return nil
	}
	var specs []commandSpec
	if len(s.tabs) > 1 {
		for index, tab := range s.tabs {
			index, tab := index, tab
			if index == s.active {
				continue
			}
			specs = append(specs, commandSpec{
				ID: commandID(fmt.Sprintf("tabs/switch/%d", index)), Label: "Switch to tab: " + tab.title(), Format: "Switch to tab: %s", FormatArgs: []string{tab.title()}, Detail: tab.path(),
				Keywords: []string{"tab", "switch", "repository", tab.path()}, Group: commandGroupApplication, Glyph: folderOpenIcon,
				Resolve: shellState,
				Run:     func(ctx commandContext) { ctx.Shell.request(func() { ctx.Shell.activate(index) }) },
			})
		}
	}
	if ctx.App != nil && ctx.App.state == stateReady && ctx.App.session != nil {
		specs = append(specs, workspaceObjectSpecs(ctx.App)...)
	}
	if !s.palette.open || ctx.App == nil {
		return specs
	}
	if s.palette.target != nil {
		return commitTargetSpecs(s.palette.target)
	}
	mode, rest := commandQueryMode(s.palette.query)
	a := ctx.App
	if mode == 0 && isCommitHash(rest) {
		// A pasted hash finds its commit without a prefix.
		specs = append(specs, s.commitSearchSpecs(a, strings.TrimSpace(rest), 0)...)
	}
	switch mode {
	case '~':
		for _, recent := range a.recentRepositories {
			recent := recent
			name := recent.Name
			if name == "" {
				name = filepath.Base(recent.Path)
			}
			specs = append(specs, commandSpec{
				ID: commandID("recent/open/" + recent.Path), Label: name, Detail: recent.Path,
				Keywords: []string{recent.Path, recent.Branch}, Group: commandGroupNavigation, Prefix: '~', Glyph: folderOpenIcon,
				Resolve: shellState,
				Run: func(ctx commandContext) {
					policy := openGitOnly
					if recent.EditorOnly {
						policy = openAllowPlainEditor
					}
					ctx.Shell.request(func() { ctx.Shell.openTabWithPolicy(recent.Path, policy) })
				},
			})
		}
	case '!':
		if rest == "" {
			specs = append(specs, commandSpec{
				ID: "commands/run-hint", Label: "any command", Group: commandGroupApplication, Prefix: '!', Glyph: terminalIcon,
				Resolve: func(commandContext) commandState {
					return commandState{Visible: true, ContextScore: 1000, DisabledReason: "{{branch}} {{commit}} {{file}} … stand for what you have selected"}
				},
			})
			// Examples: choosing one puts it in the box, ready to run or edit.
			values := a.commandContextValues()
			for index, example := range shellExamples {
				example, score := example, 900-index
				// Say what the placeholder stands for right now.
				badge, empty := shellExampleValue(values, example.key)
				specs = append(specs, commandSpec{
					ID: commandID("commands/run-example/" + example.command), Label: example.command, Detail: tr(example.detail), Badges: []commandBadge{{Text: badge, Muted: empty}},
					Group: commandGroupApplication, Prefix: '!', Glyph: terminalIcon, KeepOpen: true, NoFilter: true,
					Resolve: func(commandContext) commandState {
						return commandState{Visible: true, Enabled: true, ContextScore: score}
					},
					Run: func(ctx commandContext) {
						ctx.Shell.palette.query = "!" + example.command
						ctx.Shell.palette.selected = 0
					},
				})
			}
		}
		if rest != "" {
			// Two clearly different ways to run the line. A Git command is
			// non-interactive, so the command log comes first; anything else may
			// ask for a password or a code, so the terminal comes first.
			isGit := strings.EqualFold(firstWord(rest), "git")
			logScore, terminalScore := 10, 0
			if !isGit {
				logScore, terminalScore = 0, 10
			}
			specs = append(specs, commandSpec{
				ID: "commands/run-line", Label: tr("Run it here"), Detail: rest, NoFilter: true,
				Badges: []commandBadge{{Text: "Command log"}},
				Group:  commandGroupApplication, Prefix: '!', Glyph: terminalIcon, KeepOpen: true,
				Resolve: func(ctx commandContext) commandState {
					state := localCommandState(ctx)
					if state.Enabled && ctx.App.customCommandRunning {
						state.Enabled = false
						state.DisabledReason = "A command is already running"
					}
					state.ContextScore = logScore
					return state
				},
				Run: func(ctx commandContext) {
					if id := ctx.App.startCustomCommand(rest, commandTitle(rest)); id != 0 {
						ctx.Shell.palette.shellRun = paletteShellRun{app: ctx.App, entryID: id, line: rest}
						ctx.Shell.rememberShellLine(rest)
					}
				},
			})
			specs = append(specs, commandSpec{
				ID: "commands/run-line-terminal", Label: tr("Run in terminal"), Detail: rest, NoFilter: true,
				Badges: []commandBadge{{Text: "Terminal"}},
				Group:  commandGroupApplication, Prefix: '!', Glyph: terminalIcon,
				Resolve: func(ctx commandContext) commandState {
					state := localCommandState(ctx)
					state.ContextScore = terminalScore
					return state
				},
				Run: func(ctx commandContext) {
					ctx.Shell.rememberShellLineAs(rest, "commands/run-line-terminal")
					ctx.App.runLineInTerminal(rest)
				},
			})
		}
	case '@':
		for _, branch := range a.snapshot.Branches {
			branch := branch
			name := branchDisplay(branch)
			score := 0
			detail := "Check out this branch"
			if branch.Head {
				score = 1000
				detail = "Current branch"
			}
			specs = append(specs, commandSpec{
				ID: commandID("branches/checkout/" + branch.Name), Label: name, Detail: detail,
				Keywords: []string{branch.Name}, Group: commandGroupBranches, Prefix: '@', Glyph: branchIcon,
				Resolve: func(ctx commandContext) commandState {
					state := repositoryState(ctx)
					state.ContextScore = score
					if state.Enabled && branch.Head {
						state.Enabled = false
						state.DisabledReason = "This branch is already checked out"
					}
					return state
				},
				Run:     func(ctx commandContext) { ctx.App.checkoutBranch(branch.Name) },
				Actions: branchActions(branch),
			})
		}
		for _, remote := range a.snapshot.Remotes {
			remote := remote
			for _, remoteBranch := range remote.Branches {
				remoteBranch := remoteBranch
				qualified := remote.Name + "/" + remoteBranch.Name
				specs = append(specs, commandSpec{
					ID: commandID("branches/remote/" + qualified), Label: qualified, Detail: "Remote branch: check out a local branch that tracks it",
					Keywords: []string{remote.Name, remoteBranch.Name}, Group: commandGroupRemotes, Prefix: '@', Glyph: remotesIcon,
					Resolve: repositoryState,
					Run:     func(ctx commandContext) { ctx.App.confirmCheckoutRemoteBranch(remote, remoteBranch) },
					Actions: []commandAction{
						{Label: "List this branch's commits", KeepOpen: true, Run: func(ctx commandContext) {
							ctx.Shell.palette.query = "#branch:" + qualified + " "
							ctx.Shell.palette.selected = 0
							ctx.Shell.palette.actionIndex = 0
						}},
						{Label: "Copy branch name", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "branch name", qualified) }},
					},
				})
			}
		}
	case '#':
		if query, ok := parseHistoryChangesPalette(rest); ok {
			specs = append(specs, commandSpec{
				ID: "history/search-typed-code-changes", Label: historyChangesLabel, Detail: query.Text, Group: commandGroupHistory,
				Prefix: '#', Glyph: searchIcon, NoFilter: true, Resolve: historyChangesCommandState,
				Run: func(ctx commandContext) { ctx.App.openHistoryChanges(&query) },
			})
			break
		}
		if before, typed, ok := authorSuggestion(strings.TrimLeft(s.palette.query, " ")[1:]); ok {
			specs = append(specs, s.authorSuggestionSpecs(a, before, typed)...)
			break
		}
		if before, typed, ok := branchSuggestion(strings.TrimLeft(s.palette.query, " ")[1:]); ok {
			specs = append(specs, s.branchSuggestionSpecs(a, before, typed)...)
			break
		}
		if strings.TrimSpace(rest) == "" {
			// Nothing typed yet: say what can be searched, not list commits.
			specs = append(specs, s.commitSearchGuideSpecs()...)
			break
		}
		specs = append(specs, s.commitSearchSpecs(a, strings.TrimSpace(rest), '#')...)
	case '^':
		// Every file, fuzzy-ranked like the Editor's quick open; choosing one
		// opens the file history drawer for it.
		if !a.code.filesLoaded && !a.code.filesLoading {
			a.requestCodeFiles()
		}
		specs = append(specs, fileHistoryFileSpecs(a, rest)...)
		if len(specs) == 0 && a.code.filesLoading {
			specs = append(specs, commandSpec{
				ID: "files/history-loading", Label: "Reading the files…", Group: commandGroupNavigation, Prefix: '^', Glyph: historyIcon, NoFilter: true,
				Resolve: func(commandContext) commandState {
					return commandState{Visible: true, DisabledReason: "The list of files is being read"}
				},
			})
		}
	case '/':
		if !s.palette.filesChangedOnly {
			// Every file of the repository, ranked as the Editor's quick open
			// ranks them; choosing one opens it in the Editor.
			if !a.code.filesLoaded && !a.code.filesLoading {
				a.requestCodeFiles()
			}
			specs = append(specs, workspaceFileSpecs(a, rest)...)
			if len(specs) == 0 && a.code.filesLoading {
				specs = append(specs, commandSpec{
					ID: "files/loading", Label: "Reading the files…", Group: commandGroupNavigation, Prefix: '/', Glyph: editIcon, NoFilter: true,
					Resolve: func(commandContext) commandState {
						return commandState{Visible: true, DisabledReason: "The list of files is being read"}
					},
				})
			}
			break
		}
		count := 0
		for _, file := range a.snapshot.Files {
			if count == dynamicFileLimit {
				break
			}
			count++
			file := file
			specs = append(specs, commandSpec{
				ID: commandID("files/select/" + file.Path), Label: file.Path, Detail: "Show in Changes",
				Group: commandGroupChanges, Prefix: '/', Glyph: editIcon,
				Resolve: repositoryStateAllowBusy,
				Run:     func(ctx commandContext) { ctx.App.revealChangedFile(file.Path) },
				Actions: []commandAction{
					{Label: "Stage or unstage this file", Run: func(ctx commandContext) {
						ctx.App.revealChangedFile(file.Path)
						ctx.App.toggleStageSelected()
					}},
					{Label: "Copy file path", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "file path", file.Path) }},
				},
			})
		}
	}
	return specs
}

// revealCommit selects a commit in History when that list currently shows it.
func (a *app) revealCommit(hash string) {
	a.setWorkspaceTab(sectionHistory)
	for index, commit := range a.visibleCommits() {
		if commit.Hash == hash {
			a.selectedCommit = index
			return
		}
	}
	a.notify("warning", "Commit not shown", tr("The History filter hides this commit. Clear the filter to see it."))
}

// revealChangedFile selects a changed file in the Changes list.
func (a *app) revealChangedFile(path string) {
	a.setWorkspaceTab(sectionChanges)
	for index, row := range a.changeRows {
		if row.isFile() && row.file.Path == path {
			a.selectedFile = index
			return
		}
	}
}

// workspaceObjectSpecs offers the stashes and remotes of the repository as
// commands: apply or pop a stash, fetch or browse a remote. They resolve like
// any command, so a busy repository disables them with the usual reason.
func workspaceObjectSpecs(a *app) []commandSpec {
	var specs []commandSpec
	stashes := a.snapshot.Stashes
	if len(stashes) > 20 {
		stashes = stashes[:20]
	}
	for _, stash := range stashes {
		stash := stash
		keywords := []string{"stash", stash.Name, stash.Recency}
		specs = append(specs,
			commandSpec{
				ID: commandID(fmt.Sprintf("stash/apply/%d", stash.Index)), Label: "Apply stash: " + stash.Name, Format: "Apply stash: %s", FormatArgs: []string{stash.Name}, Detail: "Apply the saved changes and keep the stash",
				Keywords: keywords, Group: commandGroupStash, Glyph: stashIcon, Resolve: repositoryState,
				Run: func(ctx commandContext) { ctx.App.applyStash(stash) },
			},
			commandSpec{
				ID: commandID(fmt.Sprintf("stash/pop/%d", stash.Index)), Label: "Pop stash: " + stash.Name, Format: "Pop stash: %s", FormatArgs: []string{stash.Name}, Detail: "Apply the saved changes and remove the stash",
				Keywords: keywords, Group: commandGroupStash, Glyph: stashIcon, Resolve: repositoryState,
				Run: func(ctx commandContext) { ctx.App.confirmStashMutationFor(stash, true) },
			})
	}
	for _, remote := range a.snapshot.Remotes {
		remote := remote
		specs = append(specs, commandSpec{
			ID: commandID("remotes/fetch/" + remote.Name), Label: "Fetch remote: " + remote.Name, Format: "Fetch remote: %s", FormatArgs: []string{remote.Name}, Detail: "Download new commits from this remote only",
			Keywords: []string{"fetch", "remote", remote.Name}, Group: commandGroupRemotes, Glyph: fetchIcon, Resolve: repositoryState,
			Run: func(ctx commandContext) { ctx.App.beginFetchRemote(remote) },
		})
		if len(remote.URLs) > 0 {
			specs = append(specs, commandSpec{
				ID: commandID("remotes/open-web/" + remote.Name), Label: "Open remote in the browser: " + remote.Name, Format: "Open remote in the browser: %s", FormatArgs: []string{remote.Name}, Detail: remote.URLs[0],
				Keywords: []string{"browser", "web", "remote", remote.Name}, Group: commandGroupRemotes, Glyph: externalLinkIcon, Resolve: repositoryStateAllowBusy,
				Run: func(ctx commandContext) { ctx.App.openRemoteWebURL(ctx.App.services, remote) },
			})
		}
	}
	return specs
}

const workspaceFileMatchLimit = 100

type paletteFileMatches struct {
	query string
	specs []commandSpec
}

// workspaceFileSpecs searches every file the Editor has indexed for the '/'
// mode. Keep just the latest ranked results so scrolling and hover redraws
// reuse the same matches. Command availability is still evaluated on each read.
func workspaceFileSpecs(a *app, query string) []commandSpec {
	query = strings.TrimSpace(query)
	if cached := a.code.paletteFiles; cached.specs != nil && cached.query == query {
		return cached.specs
	}
	if len(a.code.files) == 0 {
		return nil
	}
	// fzf's ranking, the same as the Editor's quick open; an empty query lists
	// the files in order.
	paths := a.code.files
	if query != "" {
		paths = filterCodePathsFzf(paths, query)
	}
	if len(paths) > workspaceFileMatchLimit {
		paths = paths[:workspaceFileMatchLimit]
	}
	specs := make([]commandSpec, 0, len(paths))
	for index, path := range paths {
		path, score := path, workspaceFileMatchLimit-index
		specs = append(specs, commandSpec{
			ID: commandID("files/open/" + path), Label: path, Detail: "Open in the Editor", NoFilter: true,
			Group: commandGroupNavigation, Prefix: '/', Glyph: editIcon,
			Resolve: func(ctx commandContext) commandState {
				state := readyAppState(ctx)
				state.ContextScore = score
				return state
			},
			// The Editor page has to be the one shown, or the file opens unseen.
			Run: func(ctx commandContext) {
				ctx.App.setWorkspaceTab(sectionCode)
				ctx.App.openCodeFile(path, 0)
			},
		})
	}
	a.code.paletteFiles = paletteFileMatches{query: query, specs: specs}
	return specs
}

// fileHistoryFileSpecs is workspaceFileSpecs for the '^' mode: the same ranked
// files, each opening its history drawer instead of the Editor.
func fileHistoryFileSpecs(a *app, query string) []commandSpec {
	query = strings.TrimSpace(query)
	if cached := a.code.paletteHistoryFiles; cached.specs != nil && cached.query == query {
		return cached.specs
	}
	if len(a.code.files) == 0 {
		return nil
	}
	paths := a.code.files
	if query != "" {
		paths = filterCodePathsFzf(paths, query)
	}
	if len(paths) > workspaceFileMatchLimit {
		paths = paths[:workspaceFileMatchLimit]
	}
	specs := make([]commandSpec, 0, len(paths))
	for index, path := range paths {
		path, score := path, workspaceFileMatchLimit-index
		specs = append(specs, commandSpec{
			ID: commandID("files/history/" + path), Label: path, Detail: "Open file history", NoFilter: true,
			Group: commandGroupHistory, Prefix: '^', Glyph: historyIcon,
			Resolve: func(ctx commandContext) commandState {
				state := repositoryStateAllowBusy(ctx)
				state.ContextScore = score
				return state
			},
			Run: func(ctx commandContext) { ctx.App.openFileHistory(path) },
			Actions: []commandAction{
				{Label: "Open in the Editor", Run: func(ctx commandContext) {
					ctx.App.setWorkspaceTab(sectionCode)
					ctx.App.openCodeFile(path, 0)
				}},
				{Label: "Copy file path", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "file path", path) }},
			},
		})
	}
	a.code.paletteHistoryFiles = paletteFileMatches{query: query, specs: specs}
	return specs
}

// branchActions are the alternatives to checking a branch out. Merging into,
// rebasing onto or deleting the branch that is already checked out makes no
// sense, so those are left out for it.
func branchActions(branch repository.Branch) []commandAction {
	actions := []commandAction{
		{Label: "List this branch's commits", KeepOpen: true, Run: func(ctx commandContext) {
			ctx.Shell.palette.query = "#branch:" + branch.Name + " "
			ctx.Shell.palette.selected = 0
			ctx.Shell.palette.actionIndex = 0
		}},
		{Label: "Copy branch name", Run: func(ctx commandContext) {
			ctx.App.notifyCopied(ctx.App.services, "branch name", branch.Name)
		}},
	}
	if branch.Head {
		return actions
	}
	return append(actions,
		commandAction{Label: "Merge into the current branch…", Run: func(ctx commandContext) { ctx.App.confirmMergeBranch(branch) }},
		commandAction{Label: "Rebase the current branch onto this…", Run: func(ctx commandContext) { ctx.App.confirmRebaseOntoBranch(branch) }},
		commandAction{Label: "Delete this branch…", Run: func(ctx commandContext) { ctx.App.confirmDeleteBranch(branch) }},
	)
}

// commitActions are what a commit row can do besides revealing the commit.
func commitActions(commit repository.Commit) []commandAction {
	return []commandAction{
		{Label: "Check out this commit", Run: func(ctx commandContext) { ctx.App.confirmCheckoutCommit(commit) }},
		{Label: "Cherry-pick this commit…", Run: func(ctx commandContext) { ctx.App.confirmCommitActionFor(commit, false) }},
		{Label: "Revert this commit…", Run: func(ctx commandContext) { ctx.App.confirmCommitActionFor(commit, true) }},
		{Label: "Create a branch here…", Run: func(ctx commandContext) { ctx.App.openCommitBranchForm(commit) }},
		{Label: "Copy commit hash", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "commit hash", commit.Hash) }},
		{Label: "Copy commit subject", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "commit subject", commit.Subject) }},
	}
}

// shellExamples are the sample commands a bare ! offers.
var shellExamples = []struct{ command, detail, key string }{
	{"git show {{commit}}", "Show the selected commit", "commit"},
	{"git log --oneline {{branch}}", "List the commits of the current branch", "branch"},
	{"git diff {{file}}", "Show the changes of the selected file", "file"},
	{"git stash show -p {{stash}}", "Show the selected stash", "stash"},
	{"git fetch {{remote}}", "Fetch from the selected remote", "remote"},
}

// shellExampleValue is what a placeholder stands for now: the selected commit
// as its short hash and subject, the other values as they are.
func shellExampleValue(values map[string]string, key string) (string, bool) {
	value := strings.TrimSpace(values[key])
	if value == "" {
		return tr("nothing selected"), true
	}
	if key == "commit" {
		short := value
		if len(short) > 7 {
			short = short[:7]
		}
		if subject := strings.TrimSpace(values["commitSubject"]); subject != "" {
			return short + " " + shorten(subject, 48), false
		}
		return short, false
	}
	return shorten(value, 60), false
}

// firstWord is the command a shell line starts with.
func firstWord(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
