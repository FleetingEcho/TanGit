package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// commandsSettings is the settings page for user-defined shell commands.
// Commands run from the repository root with the user's shell; the listed
// placeholders expand to the current selection.
func (a *app) commandsSettings(c *ui.Context) {
	theme := c.Theme()
	settingsSection(c, func() {
		settingsGroup(c, commandIcon, "Custom commands", "Your own keys for shell commands in this repository")
		ui.Column(c).Gap(12).Padding(15).Background(theme.Surface.Alpha(0.55)).
			Border(1, theme.Border).Radius(theme.Radius + 5).Children(func() {
			ui.Text(c, tr("Commands run in the repository root with your shell. They see the repository through the working directory, never through a remote.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(560)
			ui.Text(c, tr("Placeholders: {{branch}} {{commit}} {{file}} {{tag}} {{stash}} {{remote}} {{root}} — each is shell-quoted for you.")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(560).Selectable()

			if len(a.settings.CustomCommands) == 0 {
				ui.Text(c, tr("No commands yet. Add one to put it on a key.")).TextColor(theme.TextMuted)
			}
			for i := range a.settings.CustomCommands {
				command := &a.settings.CustomCommands[i]
				ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
					if key := customCommandDisplayKey(command.Key); key != "" {
						keyCapsule(c, key)
					} else {
						ui.Text(c, tr("menu")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 2).
							Width(54).TextAlign(ui.Center)
					}
					ui.Column(c).Grow(1).Gap(1).Children(func() {
						ui.Text(c, command.Name).SingleLine()
						ui.Text(c, command.Command).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1).SingleLine()
					})
					flatIconButton(c, "Edit command", "Edit this command", editIcon, 14, 4, false, func() {
						a.openCustomCommandForm(i)
					})
					flatIconButton(c, "Remove command", "Remove this command", trashIcon, 14, 4, false, func() {
						a.settings.CustomCommands = append(a.settings.CustomCommands[:i], a.settings.CustomCommands[i+1:]...)
						a.settingsChanged(c)
					})
				})
			}
			flatIconButton(c, "Add command…", "Add a custom command", plusIcon, 14, 4,
				a.customCommandFormOpen, func() { a.openCustomCommandForm(-1) })

			ui.Divider(c)
			ui.Text(c, tr("Press : anywhere in the workspace to run a one-off command. The output goes to the command log (Ctrl+L).")).
				TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).MaxWidth(560)
		})
		a.customCommandForm(c)
	})
}

func keyCapsule(c *ui.Context, key string) {
	theme := c.Theme()
	ui.Row(c).Height(22).Padding(0, 8).AlignItems(ui.Center).
		Background(theme.SurfaceHover).Border(1, theme.Border).Radius(4).Width(54).Justify(ui.Center).Children(func() {
		ui.Text(c, key).FontSize(theme.FontSize - 2).TextColor(theme.Text)
	})
}

func (a *app) openCustomCommandForm(index int) {
	a.customCommandFormEdit = index
	a.customCommandFormError = ""
	if index < 0 || index >= len(a.settings.CustomCommands) {
		a.customCommandFormEdit = -1
		a.customCommandFormName = ""
		a.customCommandFormCommand = ""
		a.customCommandFormKey = ""
		a.customCommandFormContexts = []string{"everywhere"}
	} else {
		command := a.settings.CustomCommands[index]
		a.customCommandFormName = command.Name
		a.customCommandFormCommand = command.Command
		a.customCommandFormKey = command.Key
		a.customCommandFormContexts = append([]string{}, command.Contexts...)
	}
	a.customCommandFormOpen = true
}

func (a *app) closeCustomCommandForm() {
	a.customCommandFormOpen = false
	a.customCommandFormError = ""
}

func (a *app) customCommandFormContextChecked(id string) bool {
	for _, contextID := range a.customCommandFormContexts {
		if contextID == id {
			return true
		}
	}
	return false
}

func (a *app) toggleCustomCommandFormContext(id string, on bool) {
	if id == "everywhere" {
		if on {
			a.customCommandFormContexts = []string{"everywhere"}
			return
		}
		a.customCommandFormContexts = nil
		return
	}
	contexts := make([]string, 0, len(a.customCommandFormContexts)+1)
	for _, contextID := range a.customCommandFormContexts {
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
	a.customCommandFormContexts = contexts
}

func (a *app) submitCustomCommandForm(c *ui.Context) {
	name := strings.TrimSpace(a.customCommandFormName)
	command := strings.TrimSpace(a.customCommandFormCommand)
	key := strings.ToLower(strings.TrimSpace(a.customCommandFormKey))
	if command == "" {
		a.customCommandFormError = tr("Enter the command to run.")
		return
	}
	if key != "" {
		if _, _, ok := parseCustomCommandKey(key); !ok {
			a.customCommandFormError = tr("That shortcut is not recognized. Use keys like ctrl+g, shift+f2, or f9.")
			return
		}
	}
	if name == "" {
		name = firstLine(command)
	}
	contexts := a.customCommandFormContexts
	if len(contexts) == 0 {
		contexts = []string{"everywhere"}
	}
	draft := customCommandSetting{Name: name, Command: command, Key: key, Contexts: contexts}
	if a.customCommandFormEdit >= 0 && a.customCommandFormEdit < len(a.settings.CustomCommands) {
		a.settings.CustomCommands[a.customCommandFormEdit] = draft
	} else {
		a.settings.CustomCommands = append(a.settings.CustomCommands, draft)
	}
	a.settings.CustomCommands = normalizeCustomCommands(a.settings.CustomCommands)
	a.closeCustomCommandForm()
	a.settingsChanged(c)
}

func (a *app) customCommandForm(c *ui.Context) {
	if !a.customCommandFormOpen {
		return
	}
	theme := c.Theme()
	title := "Add command"
	if a.customCommandFormEdit >= 0 {
		title = "Edit command"
	}
	ui.Column(c).Gap(10).Padding(15).Background(theme.Surface.Alpha(0.55)).
		Border(1, theme.Border).Radius(theme.Radius + 5).Children(func() {
		ui.Text(c, tr(title)).Bold()
		settingRow(c, "Name", "Shown in the commands menu and the shortcut list", func() {
			ui.TextInput(c, &a.customCommandFormName).Label(tr("Name")).
				Placeholder(tr("Deploy preview")).Grow(1)
		})
		settingRow(c, "Command", "Run with your shell in the repository root", func() {
			ui.TextInput(c, &a.customCommandFormCommand).Label(tr("Command")).
				Placeholder(tr("git push origin HEAD:refs/for/main")).Grow(1).AutoFocus()
		})
		settingRow(c, "Shortcut", "Optional. A key like ctrl+g, shift+f2 or f9", func() {
			ui.TextInput(c, &a.customCommandFormKey).Label(tr("Shortcut")).
				Placeholder(tr("ctrl+g")).Grow(1)
		})
		ui.Text(c, tr("Active in")).Bold()
		ui.Row(c).Gap(6).Wrap().Children(func() {
			for _, ctx := range customCommandContexts {
				checked := a.customCommandFormContextChecked(ctx.id)
				var button ui.Element
				if checked {
					button = ui.PrimaryButton(c, tr(ctx.name))
				} else {
					button = ui.Button(c, tr(ctx.name))
				}
				_ = button
				if button.Clicked() {
					a.toggleCustomCommandFormContext(ctx.id, !checked)
				}
			}
		})
		if a.customCommandFormError != "" {
			ui.Text(c, a.customCommandFormError).TextColor(theme.Danger)
		}
		ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
			if ui.Button(c, tr("Cancel")).Clicked() {
				a.closeCustomCommandForm()
			}
			label := "Add command"
			if a.customCommandFormEdit >= 0 {
				label = "Save command"
			}
			if ui.PrimaryButton(c, tr(label)).Clicked() {
				a.submitCustomCommandForm(c)
			}
		})
	})
}
