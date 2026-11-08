package app

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// customCommandSetting is one user-defined shell command bound to an
// optional key. Contexts names the workspace sections the key fires in;
// "everywhere" covers all of them. An empty Key makes the command
// reachable only from the commands menu and the settings list.
type customCommandSetting struct {
	Name     string   `json:"name"`
	Command  string   `json:"command"`
	Key      string   `json:"key,omitempty"`
	Contexts []string `json:"contexts,omitempty"`
}

// customCommandContexts lists the section ids a command can scope to, in the
// order the settings page presents them.
var customCommandContexts = []struct {
	id   string
	name string
}{
	{"everywhere", "Everywhere"},
	{"changes", "Changes"},
	{"history", "History and graph"},
	{"branches", "Branches"},
	{"remotes", "Remotes"},
	{"stash", "Stash"},
	{"tags", "Tags"},
	{"worktrees", "Worktrees"},
	{"agents", "Agent tasks"},
	{"submodules", "Submodules"},
	{"reflog", "Reflog"},
	{"compare", "Compare refs"},
	{"diff", "Diff review"},
	{"code", "Editor"},
}

// keyboardGroupContext maps keyboardCommands groups to custom command
// contexts so a command scoped to "history" fires in History and Graph.
func keyboardGroupContext(group string) string {
	switch group {
	case "Changes":
		return "changes"
	case "History and graph", "Historical files":
		return "history"
	case "Diff review":
		return "diff"
	case "Branches":
		return "branches"
	case "Remotes", "Remote branches":
		return "remotes"
	case "Stash":
		return "stash"
	case "Tags":
		return "tags"
	case "Worktrees":
		return "worktrees"
	case "Agent tasks":
		return "agents"
	case "Submodules":
		return "submodules"
	case "Reflog":
		return "reflog"
	case "Compare refs":
		return "compare"
	case "Code":
		return "code"
	default:
		return ""
	}
}

func normalizeCustomCommands(commands []customCommandSetting) []customCommandSetting {
	if len(commands) == 0 {
		return nil
	}
	known := map[string]bool{}
	for _, ctx := range customCommandContexts {
		known[ctx.id] = true
	}
	normalized := make([]customCommandSetting, 0, len(commands))
	seen := map[string]bool{}
	for _, command := range commands {
		command.Name = strings.TrimSpace(command.Name)
		command.Command = strings.TrimSpace(command.Command)
		if command.Command == "" {
			continue
		}
		if command.Name == "" {
			command.Name = firstLine(command.Command)
		}
		key := strings.ToLower(strings.TrimSpace(command.Key))
		if _, _, ok := parseCustomCommandKey(key); !ok && key != "" {
			key = ""
		}
		command.Key = key
		contexts := make([]string, 0, len(command.Contexts))
		seenContext := map[string]bool{}
		for _, contextID := range command.Contexts {
			contextID = strings.ToLower(strings.TrimSpace(contextID))
			// "everywhere" subsumes the rest, keep it alone.
			if contextID == "everywhere" {
				seenContext = map[string]bool{"everywhere": true}
				contexts = []string{"everywhere"}
				break
			}
			if !known[contextID] || seenContext[contextID] {
				continue
			}
			seenContext[contextID] = true
			contexts = append(contexts, contextID)
		}
		switch {
		case len(contexts) == 0:
			contexts = []string{"everywhere"}
		case len(contexts) > 1 && seenContext["everywhere"]:
			contexts = []string{"everywhere"}
		}
		command.Contexts = contexts
		dedupe := command.Name + "\x00" + command.Key + "\x00" + command.Command
		if seen[dedupe] {
			continue
		}
		seen[dedupe] = true
		normalized = append(normalized, command)
	}
	return normalized
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	line = strings.TrimSpace(line)
	if len(line) > 60 {
		line = line[:60] + "…"
	}
	return line
}

func customCommandInContext(command customCommandSetting, group string) bool {
	if len(command.Contexts) == 0 {
		return true
	}
	for _, contextID := range command.Contexts {
		if contextID == "everywhere" {
			return true
		}
		if contextID == keyboardGroupContext(group) {
			return true
		}
	}
	return false
}

// shellQuote single-quotes a value for POSIX shells. Commands run through
// sh -c (or cmd /C on Windows); Windows is quoted the way cmd expects.
func shellQuote(value string) string {
	if runtime.GOOS == "windows" {
		// cmd.exe: double quotes, escaping embedded quotes by doubling.
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// customCommandPlaceholders is deliberately a single-pass expansion. A
// placeholder-looking sequence inside a branch name, path, or other value is
// data, not another template instruction. Expanding replacement values again
// could turn a quoted value into executable text.
func customCommandPlaceholders(command string, values map[string]string) string {
	parts := make([]string, 0, 2*len(values))
	for key, value := range values {
		parts = append(parts, "{{"+key+"}}", shellQuote(value))
	}
	return strings.NewReplacer(parts...).Replace(command)
}

// commandContextValues collects the values a custom command can reference.
func (a *app) commandContextValues() map[string]string {
	values := map[string]string{
		"root":   a.snapshot.Root,
		"branch": a.snapshot.Branch,
	}
	if a.snapshot.HeadHash != "" {
		values["commit"] = a.snapshot.HeadHash
		values["head"] = a.snapshot.HeadHash
	}
	if commit, ok := a.selectedVisibleCommit(); ok {
		values["commit"] = commit.Hash
		values["commitSubject"] = commit.Subject
	}
	if len(a.snapshot.Files) > 0 {
		index := a.selectedFile
		if index >= 0 && index < len(a.changeRows) && a.changeRows[index].isFile() {
			values["file"] = a.changeRows[index].file.Path
		} else if len(a.snapshot.Files) == 1 {
			values["file"] = a.snapshot.Files[0].Path
		}
	}
	if a.selectedTag >= 0 && a.selectedTag < len(a.snapshot.Tags) {
		values["tag"] = a.snapshot.Tags[a.selectedTag].Name
	}
	if a.selectedStash >= 0 && a.selectedStash < len(a.snapshot.Stashes) {
		values["stash"] = fmt.Sprintf("stash@{%d}", a.snapshot.Stashes[a.selectedStash].Index)
	}
	if remote, ok := a.selectedRemoteName(); ok {
		values["remote"] = remote
	}
	return values
}

func (a *app) selectedRemoteName() (string, bool) {
	if a.selectedRemote >= 0 && a.selectedRemote < len(a.snapshot.Remotes) {
		return a.snapshot.Remotes[a.selectedRemote].Name, true
	}
	if len(a.snapshot.Remotes) == 1 {
		return a.snapshot.Remotes[0].Name, true
	}
	return "", false
}

// runCustomCommand runs a custom command, or an ad-hoc prompt command, in
// the repository root. Output goes to the command log; a toast reports the
// outcome. Custom commands are unavailable in SSH workspaces.
func (a *app) runCustomCommand(command, label string) {
	if strings.TrimSpace(command) == "" {
		return
	}
	if a.actionBusy || a.customCommandRunning {
		a.notify("warning", "A command is already running", tr("Wait for the current operation to finish, then try again."))
		return
	}
	if a.location.IsRemote() {
		a.notify("warning", "Commands run locally", tr("Custom commands run on this computer and cannot act on an SSH workspace yet."))
		return
	}
	if a.snapshot.Root == "" {
		return
	}
	root := a.snapshot.Root
	expanded := customCommandPlaceholders(command, a.commandContextValues())
	complete := a.commandLog.BeginUserCommand(expanded)
	commandLog := a.commandLog
	generation := a.generation
	a.customCommandSeq++
	job := a.customCommandSeq
	ctx, cancel := context.WithCancel(context.Background())
	a.customCommandCancel = cancel
	a.customCommandRunning = true
	a.customCommandDone = make(chan struct{})
	jobDone := a.customCommandDone
	// Publish the running entry immediately, including when the log modal was
	// already open; completion will replace Running with the command's result.
	a.update(func() {})
	done := func(output string, failed bool) {
		complete(output, failed)
		a.update(func() {
			if job == a.customCommandSeq {
				a.customCommandRunning = false
				a.customCommandCancel = nil
			}
			if generation != a.generation || a.commandLog != commandLog || a.snapshot.Root != root {
				return
			}
			if failed {
				a.notifyToast(appToast{
					kind:        "error",
					title:       tr("Command failed"),
					description: trf("%s exited with an error. See the command log for its output.", label),
					action:      tr("View log"),
					onAction:    a.openCommandLog,
					timeout:     12 * time.Second,
				})
				return
			}
			a.notify("success", label, tr("The command completed. Its output is in the command log."))
			a.requestQuietRefresh()
		})
	}
	go func() {
		defer a.recoverBackground()
		defer close(jobDone)
		defer cancel()
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(ctx, "cmd", "/C", expanded)
		} else {
			cmd = exec.CommandContext(ctx, "sh", "-c", expanded)
		}
		cmd.Dir = root
		repository.ConfigureBackgroundCommand(cmd)
		configureCustomCancellation(cmd)
		output, err := cmd.CombinedOutput()
		done(string(output), err != nil)
	}()
}

// runCustomCommandByMenu runs a stored command from the commands menu.
func (a *app) runCustomCommandByMenu(command customCommandSetting) {
	label := command.Name
	if label == "" {
		label = "Custom command"
	}
	a.runCustomCommand(command.Command, label)
}

// commandsMenuButton is the header entry point for commands: the one-off
// command prompt plus every configured custom command, so commands are
// reachable with the mouse as well as their keys.
func (a *app) commandsMenuButton(c *ui.Context) {
	theme := c.Theme()
	button := compactIconButton(ui.Button(c, "")).Center().Label(tr("Commands")).Tooltip(tr("Run a command (:)"))
	button.Children(func() {
		ui.Icon(c, commandIcon).FontSize(15).TextColor(theme.Text)
	})
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(tr("Run command…")).Shortcut(ui.Shift, ui.KeySemicolon).Chosen() {
			a.openCommandPrompt()
		}
		if len(a.settings.CustomCommands) > 0 {
			m.Separator()
			for _, command := range a.settings.CustomCommands {
				command := command
				label := command.Name
				if keys := customCommandDisplayKey(command.Key); keys != "" {
					label = command.Name + "  (" + keys + ")"
				}
				if m.Item(label).Chosen() {
					a.runCustomCommandByMenu(command)
				}
			}
		}
		m.Separator()
		if m.Item(tr("Customize commands…")).Chosen() {
			a.openSettingsSection(settingsPageCommands)
		}
	})
}

// openCommandPrompt opens the one-off command line, the same as pressing :.
func (a *app) openCommandPrompt() {
	if a.state != stateReady {
		return
	}
	a.commandPromptText = ""
	a.commandPromptOpen = true
}

func (a *app) closeCommandPrompt() {
	a.commandPromptOpen = false
	a.commandPromptText = ""
}

// submitCommandPrompt runs the entered line and closes the prompt.
func (a *app) submitCommandPrompt() {
	command := strings.TrimSpace(a.commandPromptText)
	a.closeCommandPrompt()
	if command == "" {
		return
	}
	a.runCustomCommand(command, commandTitle(command))
}

func commandTitle(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "Command"
	}
	if len(fields) == 1 {
		return fields[0]
	}
	return strings.Join(fields[:2], " ")
}

// commandPromptModal is the one-off command line opened with :. It runs in
// the repository root and leaves the output in the command log.
func (a *app) commandPromptModal(c *ui.Context) {
	if !a.commandPromptOpen {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Start).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal().Padding(0, 0, 0, 96)
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(620).MaxWidthPercent(92).
				Padding(14).Gap(8).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Run a command")).Children(func() {
				ui.Text(c, tr("Run a command")).Bold()
				input := ui.TextInput(c, &a.commandPromptText).Label(tr("Command")).
					Placeholder(tr("git log --oneline -10")).AutoFocus()
				if input.Submitted() {
					a.submitCommandPrompt()
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, tr("Runs in the repository root; output opens in the command log.")).
						TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Grow(1)
					if ui.Button(c, tr("Cancel")).Clicked() {
						a.closeCommandPrompt()
					}
					if ui.PrimaryButton(c, tr("Run")).Clicked() {
						a.submitCommandPrompt()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCommandPrompt()
			}
		})
	})
}
