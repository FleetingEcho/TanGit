package app

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// commandsSettings is the settings page for user-defined shell commands.
// Commands run from the repository root with the user's shell; the listed
// placeholders expand to the current selection.
func (a *app) commandsSettings(c *ui.Context) {
	theme := c.Theme()
	settingsSection(c, func() {
		settingsGroup(c, commandIcon, "Custom commands", "Your own keys for shell commands in this repository", func() {
			flatIconButton(c, "Add command", "Add a custom command", plusIcon, 15, 5,
				a.settingsUI.customCommandFormOpen, func() { a.openCustomCommandForm(-1) })
		})
		a.customCommandForm(c)

		// Placeholders are small chips; what they stand for is in each tooltip.
		ui.Row(c).AlignItems(ui.Center).Gap(6).Wrap().Children(func() {
			ui.Text(c, tr("Placeholders")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			for _, placeholder := range customCommandPlaceholderHelp() {
				c.Key("placeholder:" + placeholder.name)
				ui.Text(c, "{{"+placeholder.name+"}}").FontSize(theme.FontSize-2).TextColor(theme.Accent).SingleLine().
					Padding(1, 8).Radius(999).Background(theme.Accent.Alpha(0.12)).
					Tooltip(placeholder.help)
			}
		})

		if len(a.settings.CustomCommands) == 0 {
			ui.Column(c).FillWidth().Gap(8).Padding(28).AlignItems(ui.Center).
				Background(theme.Surface.Alpha(0.4)).Border(1, theme.Border).Radius(theme.Radius + 4).Children(func() {
				ui.Icon(c, commandIcon).FontSize(24).TextColor(theme.TextMuted)
				ui.Text(c, tr("No commands yet")).Bold()
				ui.Text(c, tr("Run a command from the palette with !, then save it, or add one here.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).TextAlign(ui.Center).MaxWidth(380)
				ui.Row(c).Gap(8).Children(func() {
					iconTextButton(c, "Add command", "Add a custom command", plusIcon, false, func() { a.openCustomCommandForm(-1) })
				})
			})
		}
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for i := range a.settings.CustomCommands {
				i := i
				command := a.settings.CustomCommands[i]
				c.Key(fmt.Sprintf("custom-command:%d", i))
				ui.Row(c).FillWidth().MinWidth(0).Gap(12).AlignItems(ui.Center).Padding(9, 12).
					Background(theme.Surface.Alpha(0.5)).Border(1, theme.Border).Radius(theme.Radius + 2).Children(func() {
					ui.Column(c).Width(0).Grow(1).MinWidth(0).Gap(3).Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(8).MinWidth(0).Children(func() {
							ui.Text(c, command.Name).Bold().SingleLine()
							if key := customCommandDisplayKey(command.Key); key != "" {
								ui.Text(c, key).FontSize(theme.FontSize-2).TextColor(theme.Text).SingleLine().
									Padding(0, 7).Radius(4).Background(theme.SurfaceHover).Border(1, theme.Border).Shrink(0)
							}
							for _, label := range customCommandContextLabels(command) {
								ui.Text(c, label).FontSize(theme.FontSize-2).TextColor(theme.TextMuted).SingleLine().
									Padding(0, 7).Radius(999).Background(theme.TextMuted.Alpha(0.12)).Shrink(0)
							}
						})
						ui.Text(c, command.Command).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).
							SingleLine().Tooltip(command.Command)
					})
					flatIconButton(c, "Edit command", "Edit this command", editIcon, 14, 5, false, func() {
						a.openCustomCommandForm(i)
					})
					flatIconButton(c, "Remove command", "Remove this command", trashIcon, 14, 5, false, func() {
						a.settings.CustomCommands = append(a.settings.CustomCommands[:i], a.settings.CustomCommands[i+1:]...)
						a.settingsChanged(c)
					})
				})
			}
		})
		ui.Text(c, tr("Commands run in the repository root with your shell, never through a remote. Press : anywhere for a one-off command; its output goes to the command log (Ctrl+L).")).
			TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).MaxWidth(620)
	})
}

// customCommandPlaceholderHelp is what each placeholder stands for; every
// value is shell-quoted when it is put into a command.
func customCommandPlaceholderHelp() []struct{ name, help string } {
	return []struct{ name, help string }{
		{"branch", tr("The current branch")},
		{"commit", tr("The selected commit, else HEAD")},
		{"file", tr("The selected changed file")},
		{"tag", tr("The selected tag")},
		{"stash", tr("The selected stash")},
		{"remote", tr("The selected remote")},
		{"root", tr("The repository root")},
	}
}

// customCommandContextLabels names where a command is active, nothing for the
// usual "everywhere".
func customCommandContextLabels(command customCommandSetting) []string {
	var labels []string
	for _, id := range command.Contexts {
		if id == "everywhere" {
			return nil
		}
		for _, context := range customCommandContexts {
			if context.id == id {
				labels = append(labels, tr(context.name))
			}
		}
	}
	return labels
}

func keyCapsule(c *ui.Context, key string) {
	theme := c.Theme()
	ui.Row(c).Height(22).Padding(0, 8).AlignItems(ui.Center).
		Background(theme.SurfaceHover).Border(1, theme.Border).Radius(4).Width(54).Justify(ui.Center).Children(func() {
		ui.Text(c, key).FontSize(theme.FontSize - 2).TextColor(theme.Text)
	})
}

func (a *app) openCustomCommandForm(index int) {
	a.settingsUI.customCommandFormEdit = index
	a.settingsUI.customCommandFormError = ""
	if index < 0 || index >= len(a.settings.CustomCommands) {
		a.settingsUI.customCommandFormEdit = -1
		a.settingsUI.customCommandFormName = ""
		a.settingsUI.customCommandFormCommand = ""
		a.settingsUI.customCommandFormKey = ""
		a.settingsUI.customCommandFormContexts = []string{"everywhere"}
	} else {
		command := a.settings.CustomCommands[index]
		a.settingsUI.customCommandFormName = command.Name
		a.settingsUI.customCommandFormCommand = command.Command
		a.settingsUI.customCommandFormKey = command.Key
		a.settingsUI.customCommandFormContexts = append([]string{}, command.Contexts...)
	}
	a.settingsUI.customCommandFormOpen = true
}

func (a *app) closeCustomCommandForm() {
	a.settingsUI.closeCustomCommandForm()
}

func (a *app) customCommandFormContextChecked(id string) bool {
	for _, contextID := range a.settingsUI.customCommandFormContexts {
		if contextID == id {
			return true
		}
	}
	return false
}

func (a *app) toggleCustomCommandFormContext(id string, on bool) {
	if id == "everywhere" {
		if on {
			a.settingsUI.customCommandFormContexts = []string{"everywhere"}
			return
		}
		a.settingsUI.customCommandFormContexts = nil
		return
	}
	contexts := make([]string, 0, len(a.settingsUI.customCommandFormContexts)+1)
	for _, contextID := range a.settingsUI.customCommandFormContexts {
		if contextID != "everywhere" && contextID != id {
			contexts = append(contexts, contextID)
		}
	}
	if on {
		contexts = append(contexts, id)
	}
	if len(contexts) == 0 {
		contexts = []string{"everywhere"}
	}
	a.settingsUI.customCommandFormContexts = contexts
}

func (a *app) submitCustomCommandForm(c *ui.Context) {
	name := strings.TrimSpace(a.settingsUI.customCommandFormName)
	command := strings.TrimSpace(a.settingsUI.customCommandFormCommand)
	key := strings.ToLower(strings.TrimSpace(a.settingsUI.customCommandFormKey))
	if command == "" {
		a.settingsUI.customCommandFormError = tr("Enter the command to run.")
		return
	}
	if key != "" {
		if _, _, ok := parseCustomCommandKey(key); !ok {
			a.settingsUI.customCommandFormError = tr("That shortcut is not recognized. Use keys like ctrl+g, shift+f2, or f9.")
			return
		}
	}
	if name == "" {
		name = firstLine(command)
	}
	for index, other := range a.settings.CustomCommands {
		if index != a.settingsUI.customCommandFormEdit && strings.EqualFold(strings.TrimSpace(other.Name), name) {
			a.settingsUI.customCommandFormError = trf("A command named “%s” already exists. Choose another name.", other.Name)
			return
		}
	}
	contexts := a.settingsUI.customCommandFormContexts
	if len(contexts) == 0 {
		contexts = []string{"everywhere"}
	}
	draft := customCommandSetting{Name: name, Command: command, Key: key, Contexts: contexts}
	if a.settingsUI.customCommandFormEdit >= 0 && a.settingsUI.customCommandFormEdit < len(a.settings.CustomCommands) {
		a.settings.CustomCommands[a.settingsUI.customCommandFormEdit] = draft
	} else {
		a.settings.CustomCommands = append(a.settings.CustomCommands, draft)
	}
	a.settings.CustomCommands = normalizeCustomCommands(a.settings.CustomCommands)
	a.closeCustomCommandForm()
	a.settingsChanged(c)
}

func (a *app) customCommandForm(c *ui.Context) {
	if !a.settingsUI.customCommandFormOpen {
		return
	}
	theme := c.Theme()
	title := "Add command"
	if a.settingsUI.customCommandFormEdit >= 0 {
		title = "Edit command"
	}
	ui.Column(c).Gap(10).Padding(15).Background(theme.Surface.Alpha(0.55)).
		Border(1, theme.Border).Radius(theme.Radius + 5).Children(func() {
		ui.Text(c, tr(title)).Bold()
		settingRow(c, "Name", "Shown in the commands menu and the shortcut list", func() {
			ui.TextInput(c, &a.settingsUI.customCommandFormName).Label(tr("Name")).
				Placeholder(tr("Deploy preview")).Grow(1)
		})
		for _, warning := range a.customCommandFormConflicts() {
			ui.Text(c, warning).TextColor(theme.Warning).FontSize(theme.FontSize - 1).MaxWidth(560)
		}
		settingRow(c, "Command", "Run with your shell in the repository root", func() {
			ui.TextInput(c, &a.settingsUI.customCommandFormCommand).Label(tr("Command")).
				Placeholder(tr("git push origin HEAD:refs/for/main")).Grow(1).AutoFocus()
		})
		settingRow(c, "Shortcut", "Optional. A key like ctrl+g, shift+f2 or f9", func() {
			ui.TextInput(c, &a.settingsUI.customCommandFormKey).Label(tr("Shortcut")).
				Placeholder(tr("ctrl+g")).Grow(1)
		})
		ui.Text(c, tr("Active in")).Bold()
		// The same chips as the sidebar's page choices, Everywhere first.
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, ctx := range customCommandContexts {
				ctx := ctx
				checked := a.customCommandFormContextChecked(ctx.id)
				c.Key("command-context:" + ctx.id)
				chip := settingsChoiceChip(c, tr(ctx.name), tr("Click to toggle"), checked)
				chip.Children(func() {
					ui.Icon(c, customCommandContextIcon(ctx.id)).FontSize(14)
					ui.Text(c, tr(ctx.name)).SingleLine()
				})
				chip.OnClick(func() { a.toggleCustomCommandFormContext(ctx.id, !checked) })
			}
		})
		if a.settingsUI.customCommandFormError != "" {
			ui.Text(c, a.settingsUI.customCommandFormError).TextColor(theme.Danger)
		}
		ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
			if ui.Button(c, tr("Cancel")).Clicked() {
				a.closeCustomCommandForm()
			}
			label := "Add command"
			if a.settingsUI.customCommandFormEdit >= 0 {
				label = "Save command"
			}
			if ui.PrimaryButton(c, tr(label)).Clicked() {
				a.submitCustomCommandForm(c)
			}
		})
	})
}

// customCommandFormConflicts lists what the command being edited clashes with:
// another command of the same name, one with the same shortcut, or the very
// same command line saved under another name.
func (a *app) customCommandFormConflicts() []string {
	name := strings.ToLower(strings.TrimSpace(a.settingsUI.customCommandFormName))
	command := strings.TrimSpace(a.settingsUI.customCommandFormCommand)
	key := strings.ToLower(strings.TrimSpace(a.settingsUI.customCommandFormKey))
	var warnings []string
	for index, other := range a.settings.CustomCommands {
		if index == a.settingsUI.customCommandFormEdit {
			continue
		}
		switch {
		case name != "" && strings.ToLower(strings.TrimSpace(other.Name)) == name:
			warnings = append(warnings, trf("A command named “%s” already exists. Choose another name.", other.Name))
		case key != "" && strings.ToLower(strings.TrimSpace(other.Key)) == key:
			warnings = append(warnings, trf("The shortcut %s is already used by “%s”.", key, other.Name))
		case command != "" && strings.TrimSpace(other.Command) == command:
			warnings = append(warnings, trf("The same command is already saved as “%s”.", other.Name))
		}
	}
	return warnings
}

// customCommandContextIcon is the icon of a place a command can be active in:
// the page's own icon, as the sidebar shows it.
func customCommandContextIcon(id string) *ui.SVG {
	switch id {
	case "everywhere":
		return appIcon
	case "diff":
		return diffUnifiedIcon
	}
	if section := slices.Index(workspaceSectionIDs, id); section >= 0 {
		return sectionIcons[section]
	}
	return appIcon
}
