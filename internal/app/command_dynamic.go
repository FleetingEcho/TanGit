package app

import (
	"fmt"
	"strings"
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
	if !s.palette.open || ctx.App == nil {
		return specs
	}
	mode, rest := commandQueryMode(s.palette.query)
	a := ctx.App
	switch mode {
	case '!':
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
						ctx.Shell.palette.shellRun = paletteShellRun{app: ctx.App, entryID: id}
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
				Actions: []commandAction{{Label: "Copy branch name", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "branch name", branch.Name) }}},
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
				Run: func(ctx commandContext) { ctx.App.revealCommit(commit.Hash) },
				Actions: []commandAction{
					{Label: "Check out this commit", Run: func(ctx commandContext) { ctx.App.confirmCheckoutCommit(commit) }},
					{Label: "Copy commit hash", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "commit hash", commit.Hash) }},
					{Label: "Copy commit subject", Run: func(ctx commandContext) { ctx.App.notifyCopied(ctx.App.services, "commit subject", commit.Subject) }},
				},
			})
		}
	case '/':
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
