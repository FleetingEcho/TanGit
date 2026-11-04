package app

import "github.com/egoist/mygo/ui"

// changedFileNavigation moves through actual file rows, leaving section and
// directory headers out. It wraps so the same buttons work at either end.
func (a *app) changedFileNavigation(direction int) {
	if len(a.changeRows) == 0 || direction == 0 {
		return
	}
	for step := 1; step <= len(a.changeRows); step++ {
		index := (a.selectedFile + direction*step) % len(a.changeRows)
		if index < 0 {
			index += len(a.changeRows)
		}
		if !a.changeRows[index].isFile() {
			continue
		}
		a.selectedFile = index
		a.resetFileDiff()
		return
	}
}

// changedFileNavigationControls stays available while the newly chosen diff
// loads, so repeated next/previous actions never depend on network-speed work.
func (a *app) changedFileNavigationControls(c *ui.Context) {
	ui.Row(c).Justify(ui.End).Children(func() {
		flatIconButton(c, "Previous changed file", "Go to the previous changed file (Alt+K)", chevronUpIcon, 14, 4, false, func() {
			a.changedFileNavigation(-1)
		})
		flatIconButton(c, "Next changed file", "Go to the next changed file (Alt+J)", chevronDownIcon, 14, 4, false, func() {
			a.changedFileNavigation(1)
		})
	})
}
