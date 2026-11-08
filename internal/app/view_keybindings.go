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
func keyBindingGroups(customCommands ...customCommandSetting) []keyBindingGroup {
	groups := []keyBindingGroup{
		{
			title: "Everywhere",
			icon:  keyboardIcon,
			bindings: []keyBinding{
				{"F1", "Open the complete shortcut list"},
				{"?", "Show shortcuts for the current page"},
				{"Ctrl+,", "Open settings"},
				{":", "Run a one-off shell command"},
				{"Ctrl+L", "Show the command log"},
				{"Ctrl+` / Ctrl+~", "Toggle the terminal"},
				{"Ctrl+F", "Search the active terminal"},
				{"F5", "Refresh the repository"},
				{"/", "Search the active panel"},
				{"F3 / Shift+F3", "Move to the next / previous quick-search result"},
				{"Ctrl+Z", "Undo the latest eligible Git operation"},
				{"Ctrl+Shift+Z", "Redo the latest undone Git operation"},
				{"Tab / Shift+Tab", "Move between panels"},
				{"F6 / Shift+F6", "Move between panels"},
				{"j / k", "Move through a list"},
				{"Shift+F10", "Open the selected item’s context menu"},
				{"Escape", "Close a dialog or cancel the current step"},
			},
		},
		{
			title: "Tabs",
			icon:  newWindowIcon,
			bindings: []keyBinding{
				{"Cmd/Ctrl+T", "Open a new tab or the picker"},
				{"Cmd/Ctrl+W", "Close the active tab"},
				{"Ctrl+Tab", "Switch to the next tab"},
				{"Ctrl+Shift+Tab", "Switch to the previous tab"},
			},
		},
		{
			title: "Home",
			icon:  folderOpenIcon,
			bindings: []keyBinding{
				{"C", "Clone a remote repository"},
				{"Escape", "Cancel cloning or close the clone form"},
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
		{title: "Changes", icon: stageIcon, bindings: []keyBinding{
			{"F7 / Shift+F7", "Move to the next / previous conflict"},
			{"Alt+1 / Alt+2", "Keep the current / ancestor conflict hunk"},
			{"Alt+3 / Alt+4", "Keep the incoming conflict hunk / both hunks"},
			{"Alt+Z", "Undo the last conflict resolution"},
		}},
		{title: "History and graph", icon: historyIcon},
		{title: "Branches", icon: branchesIcon},
		{title: "Remotes", icon: remotesIcon},
		{title: "Stash", icon: stashIcon},
		{title: "Tags", icon: tagsIcon},
		{title: "Worktrees", icon: worktreesIcon},
		{title: "Agent tasks", icon: fileBracesIcon},
		{title: "Submodules", icon: submodulesIcon},

		{
			title: "Forms",
			icon:  commitIcon,
			bindings: []keyBinding{
				{"Tab / Shift+Tab", "Move focus between controls"},
				{"Enter", "Submit the form"},
				{"Escape", "Cancel the form"},
			},
		},
	}
	// Operation keys come from the same definitions that dispatch them.
	replacements := map[string][]keyBinding{}
	for _, command := range keyboardCommands() {
		replacements[command.group] = append(replacements[command.group], keyBinding{command.keys, command.label})
	}
	for i := range groups {
		if bindings, ok := replacements[groups[i].title]; ok {
			if groups[i].title == "Everywhere" {
				groups[i].bindings = append(groups[i].bindings, bindings...)
			} else {
				if groups[i].title == "Changes" {
					bindings = append(bindings, groups[i].bindings...)
				}
				groups[i].bindings = bindings
			}
			delete(replacements, groups[i].title)
		}
	}
	// Diff selection has its own focused-pane scope. Current-page help changes
	// Space's label for Custom Patch and hides its commands elsewhere.
	diffBindings := make([]keyBinding, 0, len(diffKeyboardCommands())+2)
	for _, command := range diffKeyboardCommands() {
		diffBindings = append(diffBindings, keyBinding{command.keys, command.label})
	}
	diffBindings = append(diffBindings, keyBinding{"A", "Add hunk"}, keyBinding{"B", "Review Custom Patch"})
	// Remote branches have their own scope rather than acting on the selected remote.
	groups = append(groups,
		keyBindingGroup{title: "Remote branches", icon: remotesIcon, bindings: replacements["Remote branches"]},
		keyBindingGroup{title: "Historical files", icon: historyIcon, bindings: replacements["Historical files"]},
		keyBindingGroup{title: "Diff review", icon: selectLinesIcon, bindings: diffBindings},
	)
	if len(customCommands) > 0 {
		bindings := make([]keyBinding, 0, len(customCommands))
		for _, command := range customCommands {
			keys := customCommandDisplayKey(command.Key)
			if keys == "" {
				keys = "menu"
			}
			bindings = append(bindings, keyBinding{keys, command.Name})
		}
		groups = append(groups, keyBindingGroup{title: "Custom commands", icon: commandIcon, bindings: bindings})
	}
	return groups

}

func (a *app) openKeybindings() {
	a.keybindingsCurrentOnly = false
	a.keybindingsOpen = true
}

func (a *app) openCurrentKeybindings() {
	a.keybindingFilter = ""
	a.keybindingsCurrentOnly = true
	a.keybindingsScope = ""
	if a.state == stateReady {
		a.keybindingsScope = a.keyboardCurrentGroup
	}
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
		a.registerTerminalModalShortcuts(back)
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
			if !a.settings.DisableShortcuts && !c.FocusTakesText() {
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
	groups := keyBindingGroups(a.settings.CustomCommands...)
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
	if a.keybindingsScope != "" {
		active = a.keybindingsScope
	}
	visible := make([]keyBindingGroup, 0, 4)
	for i, group := range groups {
		if group.title == "Custom commands" {
			// Custom commands can fire on any page; keep them visible in the
			// current-page view too.
			visible = append(visible, group)
			continue
		}
		if i < 2 || (a.state != stateReady && group.title == "Home") ||
			(a.state == stateReady && (group.title == "Workspace" || group.title == active)) {
			if group.title == "Diff review" {
				custom := a.customPatchActive()
				group.bindings = nil
				for _, command := range diffKeyboardCommands() {
					if command.selection && !a.keyboardDiffCanSelect {
						continue
					}
					label := command.label
					if custom && command.key == ui.KeySpace {
						label = "Add selected lines"
					}
					group.bindings = append(group.bindings, keyBinding{command.keys, label})
				}
				if custom {
					group.bindings = append(group.bindings, keyBinding{"A", "Add hunk"}, keyBinding{"B", "Review Custom Patch"})
				}
			}
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
		a.hotkeysSwitch(c)
		ui.Text(c, tr("The header icons keep every action reachable.")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
		if ui.PrimaryButton(c, tr("Done")).Clicked() {
			a.closeKeybindings()
		}
	})
}
