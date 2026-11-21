package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"lazygitapp/internal/repository"
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
				ID: commandID(fmt.Sprintf("tabs/switch/%d", index)), Label: "Switch to tab: " + tab.title(), Detail: tab.path(),
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
	mode, rest := commandQueryMode(s.palette.query)
	a := ctx.App
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
				ID: "commands/run-hint", Label: "Type a shell command after !", Group: commandGroupApplication, Prefix: '!', Glyph: terminalIcon,
				Resolve: func(commandContext) commandState {
					return commandState{Visible: true, DisabledReason: "Placeholders: {{branch}} {{commit}} {{file}} {{root}} {{tag}} {{stash}} {{remote}}"}
				},
			})
		}
		if rest != "" {
			specs = append(specs, commandSpec{
				ID: "commands/run-line", Label: rest, Detail: "Run in the repository root; output goes to the command log",
				Group: commandGroupApplication, Prefix: '!', Glyph: terminalIcon, KeepOpen: true,
				Resolve: func(ctx commandContext) commandState {
					state := localCommandState(ctx)
					if state.Enabled && ctx.App.customCommandRunning {
						state.Enabled = false
						state.DisabledReason = "A command is already running"
					}
					return state
				},
				Run: func(ctx commandContext) {
					if id := ctx.App.startCustomCommand(rest, commandTitle(rest)); id != 0 {
						ctx.Shell.palette.shellRun = paletteShellRun{app: ctx.App, entryID: id, line: rest}
						ctx.Shell.rememberShellLine(rest)
					}
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
	case '#':
		commits := a.visibleCommits()
		if len(commits) > dynamicCommitLimit {
			commits = commits[:dynamicCommitLimit]
		}
		for index, commit := range commits {
			commit := commit
			score := dynamicCommitLimit - index
			specs = append(specs, commandSpec{
				ID: commandID("history/commit/" + commit.Hash), Label: commit.Subject,
				Detail:   strings.TrimSpace(commit.ShortHash + " · " + commit.AuthorName),
				Keywords: []string{commit.Hash, commit.ShortHash, commit.AuthorName}, Group: commandGroupHistory, Prefix: '#', Glyph: historyIcon,
				Resolve: func(ctx commandContext) commandState {
					state := repositoryStateAllowBusy(ctx)
					state.ContextScore = score
					return state
				},
				Run:     func(ctx commandContext) { ctx.App.revealCommit(commit.Hash) },
				Actions: commitActions(commit),
			})
		}
	case '/':
		specs = append(specs, workspaceFileSpecs(a, rest)...)
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
				ID: commandID(fmt.Sprintf("stash/apply/%d", stash.Index)), Label: "Apply stash: " + stash.Name, Detail: "Apply the saved changes and keep the stash",
				Keywords: keywords, Group: commandGroupStash, Glyph: stashIcon, Resolve: repositoryState,
				Run: func(ctx commandContext) { ctx.App.applyStash(stash) },
			},
			commandSpec{
				ID: commandID(fmt.Sprintf("stash/pop/%d", stash.Index)), Label: "Pop stash: " + stash.Name, Detail: "Apply the saved changes and remove the stash",
				Keywords: keywords, Group: commandGroupStash, Glyph: stashIcon, Resolve: repositoryState,
				Run: func(ctx commandContext) { ctx.App.confirmStashMutationFor(stash, true) },
			})
	}
	for _, remote := range a.snapshot.Remotes {
		remote := remote
		specs = append(specs, commandSpec{
			ID: commandID("remotes/fetch/" + remote.Name), Label: "Fetch remote: " + remote.Name, Detail: "Download new commits from this remote only",
			Keywords: []string{"fetch", "remote", remote.Name}, Group: commandGroupRemotes, Glyph: fetchIcon, Resolve: repositoryState,
			Run: func(ctx commandContext) { ctx.App.beginFetchRemote(remote) },
		})
		if len(remote.URLs) > 0 {
			specs = append(specs, commandSpec{
				ID: commandID("remotes/open-web/" + remote.Name), Label: "Open remote in the browser: " + remote.Name, Detail: remote.URLs[0],
				Keywords: []string{"browser", "web", "remote", remote.Name}, Group: commandGroupRemotes, Glyph: externalLinkIcon, Resolve: repositoryStateAllowBusy,
				Run: func(ctx commandContext) { ctx.App.openRemoteWebURL(ctx.App.services, remote) },
			})
		}
	}
	return specs
}

const workspaceFileMatchLimit = 100

// workspaceFileSpecs searches every file the Editor has indexed for the '/'
// mode, so it reaches beyond the changed files. Matching happens here, before
// any spec exists, to keep large workspaces from building a spec per file.
func workspaceFileSpecs(a *app, query string) []commandSpec {
	if query == "" || len(a.code.files) == 0 {
		return nil
	}
	needle := strings.ToLower(query)
	var specs []commandSpec
	for _, path := range a.code.files {
		if _, ok := subsequenceRanges(strings.ToLower(path), needle); !ok {
			continue
		}
		path := path
		specs = append(specs, commandSpec{
			ID: commandID("files/open/" + path), Label: path, Detail: "Open in the Editor",
			Group: commandGroupNavigation, Prefix: '/', Glyph: editIcon, Resolve: readyAppState,
			Run: func(ctx commandContext) { ctx.App.openCodeFile(path, 0) },
		})
		if len(specs) == workspaceFileMatchLimit {
			break
		}
	}
	return specs
}

// branchActions are the alternatives to checking a branch out. Merging into,
// rebasing onto or deleting the branch that is already checked out makes no
// sense, so those are left out for it.
func branchActions(branch repository.Branch) []commandAction {
	actions := []commandAction{{Label: "Copy branch name", Run: func(ctx commandContext) {
		ctx.App.notifyCopied(ctx.App.services, "branch name", branch.Name)
	}}}
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
