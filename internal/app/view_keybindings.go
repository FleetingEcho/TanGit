package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// keyBinding is one line of the shortcut reference: the keys to press and
// what they do. keys is display text, so a combination and a pair of
// alternatives both fit in the single key chip beside the description.
type keyBinding struct {
	keys  string
	label string
}

// keyBindingGroup keeps the reference readable by naming the part of the
// window each shortcut acts on: the same letter does different things in
// different sections.
type keyBindingGroup struct {
	title    string
	icon     *ui.SVG
	bindings []keyBinding
}

// keyBindingGroups is the shortcut reference in the order the app presents
// its parts: the gestures that work everywhere, the window-wide navigation,
// then each workspace section, and finally the forms.
func keyBindingGroups() []keyBindingGroup {
	return []keyBindingGroup{
		{
			title: "Everywhere",
			icon:  keyboardIcon,
			bindings: []keyBinding{
				{"F1", "Open the complete shortcut list"},
				{"?", "Show shortcuts for the current page"},
				{"Ctrl+,", "Open settings"},
				{"Ctrl+L", "Show the command log"},
				{"Ctrl+`", "Toggle the terminal"},
				{"F5", "Refresh the repository"},
				{"/", "Search the active panel"},
				{"F3 / Shift+F3", "Move to the next / previous quick-search result"},
				{"Ctrl+Z", "Undo the latest eligible Git operation"},
				{"Ctrl+Shift+Z", "Redo the latest undone Git operation"},
				{"Tab / Shift+Tab", "Move focus between controls"},
				{"Escape", "Close a dialog or cancel the current step"},
			},
		},
		{
			title: "Tabs",
			icon:  newWindowIcon,
			bindings: []keyBinding{
				{"Ctrl+T", "Open a new tab or the picker"},
				{"Ctrl+W", "Close the active tab"},
				{"Ctrl+Tab", "Switch to the next tab"},
				{"Ctrl+Shift+Tab", "Switch to the previous tab"},
			},
		},
		{
			title: "Workspace",
			icon:  folderIcon,
			bindings: []keyBinding{
				{"1 – 9", "Switch to a workspace section"},
				{"0", "Switch to the last section"},
				{"Up / Down", "Move through a list"},
				{"Home / End", "Jump to the first or last item"},
				{"PageUp / PageDown", "Move a page at a time"},
				{"Enter", "Open the chosen item"},
				{"W", "Compare two refs"},
				{"Alt+W", "Ignore or show whitespace-only diff changes"},
				{"Alt+U", "Cycle ordinary diff context lines"},
			},
		},
		{
			title: "Changes",
			icon:  stageIcon,
			bindings: []keyBinding{
				{"Space", "Stage or unstage the chosen file, or the lines picked in its diff"},
				{"C", "Write a commit message"},
				{"Ctrl+Enter", "Commit the staged changes"},
				{"B", "Blame the chosen file"},
				{"I / Shift+I", "Ignore file in .gitignore / .git/info/exclude"},
				{"D", "Discard the chosen file's changes"},
				{"F7 / Shift+F7", "Move to the next / previous conflict"},
				{"Alt+1 / Alt+2", "Keep the current / ancestor conflict hunk"},
				{"Alt+3 / Alt+4", "Keep the incoming conflict hunk / both hunks"},
				{"Alt+Z", "Undo the last conflict resolution"},
			},
		},
		{
			title: "History and graph",
			icon:  historyIcon,
			bindings: []keyBinding{
				{"H / Shift+H", "Compare commit with HEAD / first parent with commit"},
				{"Shift+R", "Restore the chosen historical file after confirmation"},
				{"P", "Cherry-pick the chosen commit"},
				{"V", "Revert the chosen commit"},
				{"R", "Reword the chosen commit"},
				{"A", "Amend the chosen commit with staged changes"},
				{"S", "Squash the chosen commit into its older neighbor"},
				{"F", "Fix up the chosen commit into its older neighbor"},
				{"D", "Drop the chosen commit"},
				{"Ctrl+K / Ctrl+J", "Move the chosen commit toward newer / older history"},
			},
		},
		{
			title: "Branches",
			icon:  branchesIcon,
			bindings: []keyBinding{
				{"H / Shift+H", "Compare branch commit with HEAD / its first parent"},
				{"Shift+R", "Restore the chosen branch-commit file after confirmation"},
				{"Space", "Check out the chosen branch"},
				{"N", "Create a branch"},
				{"M", "Merge the chosen branch into the current one"},
				{"U", "Unset the chosen branch's upstream"},
				{"D", "Delete the chosen branch"},
			},
		},
		{
			title: "Remotes",
			icon:  remotesIcon,
			bindings: []keyBinding{
				{"N", "Add a remote"},
				{"F", "Fetch the chosen remote"},
				{"E", "Edit the chosen remote's name or URL"},
				{"Space", "Check out the chosen remote branch as a local branch"},
				{"Shift+Space", "Check out the chosen remote branch detached"},
				{"M", "Merge the chosen remote branch into the current one"},
				{"R", "Rebase the current branch onto the chosen remote branch"},
				{"U", "Set the current branch to track the chosen remote branch"},
				{"C", "Copy the full remote branch name"},
				{"Shift+D", "Delete the chosen branch on its remote"},
				{"D", "Remove the chosen remote"},
			},
		},
		{
			title: "Stash",
			icon:  stashIcon,
			bindings: []keyBinding{
				{"Space", "Apply the chosen stash"},
				{"P", "Pop the chosen stash"},
				{"D", "Drop the chosen stash"},
			},
		},
		{
			title: "Tags",
			icon:  tagsIcon,
			bindings: []keyBinding{
				{"N", "Create a tag"},
				{"P", "Push the chosen tag"},
				{"D", "Delete the chosen tag"},
			},
		},
		{
			title: "Worktrees",
			icon:  worktreesIcon,
			bindings: []keyBinding{
				{"N", "Create a worktree"},
				{"O", "Open the chosen worktree"},
				{"D", "Remove the chosen worktree"},
			},
		},
		{
			title: "Submodules",
			icon:  submodulesIcon,
			bindings: []keyBinding{
				{"N", "Add a submodule"},
				{"U", "Initialize or update the chosen submodule"},
				{"D", "Delete the chosen submodule"},
			},
		},
		{
			title: "Forms",
			icon:  commitIcon,
			bindings: []keyBinding{
				{"Enter", "Submit the form"},
				{"Escape", "Cancel the form"},
			},
		},
	}
}

func (a *app) openKeybindings() {
	a.keybindingsCurrentOnly = false
	a.keybindingsOpen = true
}

func (a *app) openCurrentKeybindings() {
	a.keybindingFilter = ""
	a.keybindingsCurrentOnly = true
	a.keybindingsOpen = true
}

func (a *app) closeKeybindings() {
	a.keybindingsOpen = false
	a.keybindingsCurrentOnly = false
	a.keybindingFilter = ""
}

// keybindingsModal is the shortcut reference: the gestures the app answers
// to, grouped by where they act, a field to narrow them, and the switch that
// turns every shortcut off for users who would rather click.
func (a *app) keybindingsModal(c *ui.Context) {
	if !a.keybindingsOpen {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		back.Children(func() {
			panel := ui.Column(c).Width(760).MaxWidthPercent(94).MaxHeightPercent(96).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Keyboard shortcuts")).
				Children(func() {
					a.keybindingsHeader(c)
					ui.Divider(c)
					a.keybindingsFilterField(c)
					ui.Divider(c)
					ui.Scroll(c).Grow(1).Children(func() {
						a.keybindingsBody(c)
					})
					ui.Divider(c)
					a.keybindingsFooter(c)
				})
			// Overlay commands, unlike the window registrations, keep working in
			// the modal's focus scope. This makes ? and F1 respond immediately
			// after the dialog's switch re-enables the keyboard.
			if !a.settings.DisableShortcuts {
				if back.OverlayShortcut(ui.Shift, ui.KeySlash) {
					a.openCurrentKeybindings()
				}
				if back.OverlayShortcut(0, ui.KeyF1) {
					a.openKeybindings()
				}
			}
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeKeybindings()
			}
		})
	})
}

func (a *app) keybindingsHeader(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Icon(c, keyboardIcon).FontSize(20).TextColor(theme.Accent)
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, tr("Keyboard shortcuts")).FontSize(19).Bold()
			subtitle := tr("Every action has a key. Type below to find the one you want.")
			if a.keybindingsCurrentOnly && a.state == stateReady {
				subtitle = trf("Shortcuts for %s and the actions available everywhere.", tr(workspaceSections[a.workspaceTab]))
			}
			ui.Text(c, subtitle).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
		})
		iconButton(c, "Close keyboard shortcuts", "Escape", closeIcon, false, a.closeKeybindings)
	})
}

func (a *app) keybindingsFilterField(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
		ui.TextInput(c, &a.keybindingFilter).
			Label(tr("Filter shortcuts")).
			Placeholder(tr("Type to filter shortcuts")).
			Grow(1)
	})
}

// visibleKeyBindingGroups keeps ? focused on the current page. Navigation and
// global commands remain visible because they work alongside its row actions;
// F1 bypasses this filter and still lists every section.
func (a *app) visibleKeyBindingGroups() []keyBindingGroup {
	groups := keyBindingGroups()
	if !a.keybindingsCurrentOnly {
		return groups
	}
	active := ""
	if a.state == stateReady {
		switch a.workspaceTab {
		case sectionHistory, sectionGraph:
			active = "History and graph"
		case sectionReflog:
			active = "Reflog"
		default:
			active = workspaceSections[a.workspaceTab]
		}
	}
	visible := make([]keyBindingGroup, 0, 4)
	for i, group := range groups {
		if i < 2 || (a.state == stateReady && (i == 2 || group.title == active)) {
			visible = append(visible, group)
		}
	}
	return visible
}

// keybindingsBody draws the groups that still have a matching shortcut, and a
// quiet line when the filter matches nothing.
func (a *app) keybindingsBody(c *ui.Context) {
	theme := c.Theme()
	filter := strings.ToLower(strings.TrimSpace(a.keybindingFilter))
	shown := 0
	ui.Column(c).FillWidth().Gap(14).Children(func() {
		for _, group := range a.visibleKeyBindingGroups() {
			matches := matchingKeyBindings(group, filter)
			if len(matches) == 0 {
				continue
			}
			shown += len(matches)
			ui.Column(c).FillWidth().Gap(5).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Icon(c, group.icon).FontSize(14).TextColor(theme.Accent)
					ui.Text(c, tr(group.title)).Bold().Grow(1)
					ui.Badge(c, fmt.Sprintf("%d", len(matches)))
				})
				for _, binding := range matches {
					a.keyBindingRow(c, binding)
				}
			})
		}
		if shown == 0 {
			ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
				ui.Icon(c, searchIcon).FontSize(24).TextColor(theme.TextMuted)
				ui.Text(c, tr("No shortcut matches ")+strings.TrimSpace(a.keybindingFilter)).
					TextColor(theme.TextMuted)
			})
		}
	})
}

func matchingKeyBindings(group keyBindingGroup, filter string) []keyBinding {
	if filter == "" {
		return group.bindings
	}
	matches := make([]keyBinding, 0, len(group.bindings))
	for _, binding := range group.bindings {
		haystack := strings.ToLower(tr(group.title) + " " + binding.keys + " " + tr(binding.label))
		if strings.Contains(haystack, filter) {
			matches = append(matches, binding)
		}
	}
	return matches
}

// keyBindingRow is one shortcut: its keys in a chip, then what they do.
func (a *app) keyBindingRow(c *ui.Context, binding keyBinding) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(12).Padding(4, 6).Children(func() {
		ui.Box(c).Width(150).Padding(3, 8).Radius(theme.Radius-1).
			Background(theme.Surface).Border(1, theme.Border).Children(func() {
			ui.Text(c, binding.keys).FontSize(theme.FontSize - 1).
				TextColor(theme.Accent).SingleLine()
		})
		ui.Text(c, tr(binding.label)).TextColor(theme.TextMuted).Grow(1)
	})
}

func (a *app) keybindingsFooter(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		disable := ui.Checkbox(c, &a.settings.DisableShortcuts, tr("Disable all keyboard shortcuts")).
			Label(tr("Disable all keyboard shortcuts"))
		if disable.Changed() {
			a.saveSettings()
			c.Invalidate()
		}
		ui.Text(c, tr("The header icons keep every action reachable.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		if ui.PrimaryButton(c, tr("Done")).Clicked() {
			a.closeKeybindings()
		}
	})
}
