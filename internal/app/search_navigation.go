package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// wrappedSearchRow moves through filtered results, wrapping at either end. If
// a search has just invalidated the old selection, forward starts at the first
// match and backward at the last; an empty result has no destination.
func wrappedSearchRow(current, count, direction int) (int, bool) {
	if count == 0 || direction == 0 {
		return -1, false
	}
	if current < 0 || current >= count {
		if direction < 0 {
			return count - 1, true
		}
		return 0, true
	}
	if direction < 0 {
		direction = -1
	} else {
		direction = 1
	}
	return (current + direction + count) % count, true
}

func (a *app) navigateBranchSearch(direction int) {
	if strings.TrimSpace(a.branchSearch) == "" {
		return
	}
	rows := a.visibleBranches()
	if row, ok := wrappedSearchRow(a.selectedBranchRow, len(rows), direction); ok {
		a.selectBranchRow(row)
		a.resetBranchCommits()
		a.showRenameForm = false
		a.branchList.ScrollIntoView(row)
	}
}

func (a *app) navigateRemoteSearch(direction int) {
	if strings.TrimSpace(a.remoteFilter) == "" {
		return
	}
	rows := a.visibleRemotes()
	if row, ok := wrappedSearchRow(a.selectedRemoteRow, len(rows), direction); ok {
		a.selectRemoteRow(row)
		a.remoteList.ScrollIntoView(row)
	}
}

func (a *app) navigateRemoteBranchSearch(direction int) {
	if strings.TrimSpace(a.remoteBranchFilter) == "" {
		return
	}
	rows := a.visibleRemoteBranches()
	if row, ok := wrappedSearchRow(a.selectedRemoteBranchRow, len(rows), direction); ok {
		a.selectRemoteBranchRow(row)
		a.showRemoteCheckoutForm = false
		a.remoteBranches.ScrollIntoView(row)
	}
}

func (a *app) navigateStashSearch(direction int) {
	if strings.TrimSpace(a.stashFilter) == "" {
		return
	}
	rows := a.visibleStashes()
	if row, ok := wrappedSearchRow(a.selectedStashRow, len(rows), direction); ok {
		a.selectStashRow(row)
		a.stashList.ScrollIntoView(row)
	}
}

func (a *app) navigateTagSearch(direction int) {
	if strings.TrimSpace(a.tagFilter) == "" {
		return
	}
	rows := a.visibleTags()
	if row, ok := wrappedSearchRow(a.selectedTagRow, len(rows), direction); ok {
		a.selectTagRow(row)
		a.showTagPushForm = false
		a.tagList.ScrollIntoView(row)
	}
}

func (a *app) navigateWorktreeSearch(direction int) {
	if strings.TrimSpace(a.worktreeFilter) == "" {
		return
	}
	rows := a.visibleWorktrees()
	if row, ok := wrappedSearchRow(a.selectedWorktreeRow, len(rows), direction); ok {
		a.selectWorktreeRow(row)
		a.worktreeList.ScrollIntoView(row)
	}
}

func (a *app) navigateSubmoduleSearch(direction int) {
	if strings.TrimSpace(a.submoduleFilter) == "" {
		return
	}
	rows := a.visibleSubmodules()
	if row, ok := wrappedSearchRow(a.selectedSubmoduleRow, len(rows), direction); ok {
		a.selectSubmoduleRow(row)
		a.submoduleList.ScrollIntoView(row)
	}
}

func (a *app) navigateReflogSearch(direction int) {
	if strings.TrimSpace(a.reflogFilter) == "" {
		return
	}
	rows := a.visibleReflog()
	if row, ok := wrappedSearchRow(a.selectedReflogRow, len(rows), direction); ok {
		a.selectReflogRow(row)
		a.reflogList.ScrollIntoView(row)
	}
}

func (a *app) navigateChangeSearch(direction int) {
	if strings.TrimSpace(a.fileSearch) == "" {
		return
	}
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	files := make([]int, 0, len(a.changeRows))
	for row, change := range a.changeRows {
		if change.isFile() {
			files = append(files, row)
		}
	}
	current := -1
	for i, row := range files {
		if row == a.selectedFile {
			current = i
			break
		}
	}
	match, ok := wrappedSearchRow(current, len(files), direction)
	if !ok {
		return
	}
	row := files[match]
	a.selectedFile = row
	sections := splitChangeRows(a.changeRows)
	switch {
	case sections.mergeBase >= 0 && row >= sections.mergeBase && row < sections.mergeBase+len(sections.merge):
		a.mergeList.ScrollIntoView(row - sections.mergeBase)
	case sections.stagedBase >= 0 && row >= sections.stagedBase && row < sections.stagedBase+len(sections.staged):
		a.stagedList.ScrollIntoView(row - sections.stagedBase)
	case sections.changesBase >= 0:
		a.fileList.ScrollIntoView(row - sections.changesBase)
	}
}

func (a *app) navigateCompareSearch(direction int) {
	if strings.TrimSpace(a.compare.fileSearch) == "" {
		return
	}
	a.compare.fileRows = a.syncCompareFileRows()
	files := make([]int, 0, len(a.compare.fileRows))
	for row, item := range a.compare.fileRows {
		if !item.isDirectory() {
			files = append(files, row)
		}
	}
	current := -1
	for index, row := range files {
		if row == a.compare.selectedRow {
			current = index
			break
		}
	}
	match, ok := wrappedSearchRow(current, len(files), direction)
	if !ok {
		return
	}
	row := files[match]
	a.selectCompareFileRow(row)
	a.compare.list.ScrollIntoView(row)
}

// navigateActiveSearch routes F3 to the quick search on the current page. The
// nested remote-branch search wins when it has text; History and Graph's
// structured filters are intentionally not treated as quick-search results.
func (a *app) navigateActiveSearch(direction int) {
	if a.fileHistoryOpen {
		return
	}
	switch a.workspaceTab {
	case sectionChanges:
		a.navigateChangeSearch(direction)
	case sectionBranches:
		a.navigateBranchSearch(direction)
	case sectionRemotes:
		if strings.TrimSpace(a.remoteBranchFilter) != "" {
			a.navigateRemoteBranchSearch(direction)
		} else {
			a.navigateRemoteSearch(direction)
		}
	case sectionStash:
		a.navigateStashSearch(direction)
	case sectionTags:
		a.navigateTagSearch(direction)
	case sectionWorktrees:
		a.navigateWorktreeSearch(direction)
	case sectionSubmodules:
		a.navigateSubmoduleSearch(direction)
	case sectionReflog:
		a.navigateReflogSearch(direction)
	case sectionCode:
		if a.code.sidebar == codeSidebarSearch {
			a.navigateCodeSearch(direction)
		}
	case sectionCompare:
		a.navigateCompareSearch(direction)
	}
}

// searchNavigationButtons is shared with Changes, whose filter row also holds
// the untracked-files checkbox rather than using listSearchField.
func (a *app) searchNavigationButtons(c *ui.Context, enabled bool) {
	if !enabled {
		return
	}
	commitHeaderButton(c, "Previous file search result", "Shift+F3", chevronUpIcon, func() {
		if a.workspaceTab == sectionCompare {
			a.navigateCompareSearch(-1)
		} else {
			a.navigateChangeSearch(-1)
		}
	})
	commitHeaderButton(c, "Next file search result", "F3", chevronDownIcon, func() {
		if a.workspaceTab == sectionCompare {
			a.navigateCompareSearch(1)
		} else {
			a.navigateChangeSearch(1)
		}
	})
}
