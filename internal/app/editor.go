package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// filenamePlaceholder and linePlaceholder are what an editor command writes
// where it wants the file and the line. A command that names no file has the
// path appended to it, so `code --wait` still opens the file it was asked to.
const (
	filenamePlaceholder = "{{filename}}"
	linePlaceholder     = "{{line}}"
)

// editorPreset is one editor Settings offers, with the command its own
// installer puts on the PATH; choosing it stores the command, so the setting
// stays one plain string the user can edit as they like.
type editorPreset struct {
	Name    string
	Command string
}

// editorPresets are the editors Settings offers, in the order they
// draw them. They follow the commands their own installers write: VS Code's
// `code`, Zed's `zed` and Neovim's `nvim`.
var editorPresets = []editorPreset{
	{Name: "VS Code", Command: "code --wait " + filenamePlaceholder},
	{Name: "Zed", Command: "zed " + filenamePlaceholder},
	{Name: "Neovim", Command: "nvim +" + linePlaceholder + " " + filenamePlaceholder},
}

// defaultEditorCommand is what a fresh install opens files with, and the
// fallback for an emptied setting.
const defaultEditorCommand = "code --wait " + filenamePlaceholder

var (
	// errEditorMissing is what the launcher reports when the editor's
	// command is not on the PATH, so the app can tell the user to install
	// it rather than pass on the shell's own wording.
	errEditorMissing = errors.New("the editor's command is not on the PATH")
	// errEditorQuote is an editor command whose quoting never closes.
	errEditorQuote = errors.New("the editor command has an unclosed quote")
	// errEditorCommandEmpty is an editor command with nothing in it.
	errEditorCommandEmpty = errors.New("no editor command is set")
)

// editorGOOS selects platform-specific editor discovery; tests replace it to
// reproduce a Finder-launched macOS app's environment on another host.
var editorGOOS = runtime.GOOS

// runEditorTemplate starts the configured editor for path, at line (0 when
// the file is opened whole). It is a seam so a test can check the command and
// arguments without launching an editor.
var runEditorTemplate = func(command, path string, line int) error {
	args, err := editorCommandArgs(command, path, line)
	if err != nil {
		return err
	}
	cmd := exec.Command(resolveEditorProgram(args[0]), args[1:]...)
	if terminalEditorProgram(args[0]) {
		args[0] = resolveEditorProgram(args[0])
		cmd, err = terminalEditorCommand(editorGOOS, args, path, exec.LookPath)
		if err != nil {
			return err
		}
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errEditorMissing
		}
		return err
	}
	// The command hands the file to the editor and returns; it is waited
	// for on its own so it leaves no zombie behind.
	go func() { _ = cmd.Wait() }()
	return nil
}

// resolveEditorProgram keeps PATH as the first choice. Finder does not give a
// macOS app every PATH entry from the user's shell, so installed editors also
// get their app-bundle or Homebrew executables as fallbacks. Explicit paths and
// every other platform stay exactly as configured.
func resolveEditorProgram(program string) string {
	if editorGOOS != "darwin" || strings.ContainsAny(program, `/\`) {
		return program
	}
	if _, err := exec.LookPath(program); err == nil {
		return program
	}
	var candidates []string
	if program == "code" {
		const bundleCLI = "Visual Studio Code.app/Contents/Resources/app/bin/code"
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			candidates = append(candidates, filepath.Join(home, "Applications", bundleCLI))
		}
		candidates = append(candidates, filepath.Join("/Applications", bundleCLI))
	} else if terminalEditorProgram(program) {
		for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"} {
			candidates = append(candidates, filepath.Join(dir, program))
		}
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate
		}
	}
	return program
}

// editorCommandArgs fills the placeholders of an editor command and splits it
// into the program and its arguments. The command is split before the path is
// filled in, so a path with spaces stays one argument without being quoted.
func editorCommandArgs(command, path string, line int) ([]string, error) {
	args, err := splitEditorCommand(command)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, errEditorCommandEmpty
	}
	if line <= 0 {
		line = 1
	}
	named := false
	for i, arg := range args {
		if strings.Contains(arg, filenamePlaceholder) {
			named = true
		}
		arg = strings.ReplaceAll(arg, filenamePlaceholder, path)
		arg = strings.ReplaceAll(arg, linePlaceholder, strconv.Itoa(line))
		args[i] = arg
	}
	if !named {
		args = append(args, path)
	}
	return args, nil
}

// splitEditorCommand splits a command line on spaces, keeping runs the user
// quoted together, so an argument may hold a space of its own.
func splitEditorCommand(command string) ([]string, error) {
	var args []string
	var current strings.Builder
	var quote rune
	started := false
	flush := func() {
		if started {
			args = append(args, current.String())
			current.Reset()
			started = false
		}
	}
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			current.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, errEditorQuote
	}
	flush()
	return args, nil
}

// editorProgram names the program an editor command runs, to tell the user
// which command a missing binary is.
func editorProgram(command string) string {
	args, err := splitEditorCommand(command)
	if err != nil || len(args) == 0 {
		return strings.TrimSpace(command)
	}
	return args[0]
}

// effectiveEditorCommand is the command the app launches: the setting, or the
// default when it is empty.
func effectiveEditorCommand(command string) string {
	if trimmed := strings.TrimSpace(command); trimmed != "" {
		return trimmed
	}
	return defaultEditorCommand
}

// launchEditor runs the configured editor command for path, at line, and says
// what to do when the machine cannot.
func (a *app) launchEditor(path string, line int) {
	command := effectiveEditorCommand(a.settings.EditorCommand)
	if err := runEditorTemplate(command, path, line); err != nil {
		description := safeError(err)
		switch {
		case errors.Is(err, errEditorMissing):
			description = trf("%s was not found on the PATH. Install it, or choose another editor command in Settings.",
				editorProgram(command))
		case errors.Is(err, errEditorQuote), errors.Is(err, errEditorCommandEmpty):
			description = tr("Set an editor command in Settings, with {{filename}} where the file should go.")
		}
		a.notify("error", tr("Could not open the editor"), description)
	}
}

// editorLaunchButton opens the path immediately with the configured command.
// Editor selection belongs in Settings, not in the launch action.
func (a *app) editorLaunchButton(c *ui.Context, label, tooltip, path string, line int) {
	button := compactIconButton(ui.Button(c, ""))
	button.Label(tr(label)).Tooltip(tr(tooltip) + "\n" + tr("Configure your editor in Settings → Editor."))
	button.Children(func() {
		ui.Icon(c, vscodeIcon).FontSize(14)
	})
	if button.Clicked() {
		a.launchEditor(path, line)
	}
}

// editorButton opens the repository in the chosen editor, where its files are
// edited before they are staged here.
func (a *app) editorButton(c *ui.Context) {
	if a.snapshot.Root == "" {
		return
	}
	a.editorLaunchButton(c, "Open in editor", "Open this repository in your editor", a.snapshot.Root, 0)
}

// fileEditorButton opens the selected file in the chosen editor, from the
// working tree, so hand edits beside the diff land in the same file.
func (a *app) fileEditorButton(c *ui.Context, file repository.File) {
	path := a.workingFilePath(file.Path)
	if path == "" {
		return
	}
	a.editorLaunchButton(c, "Open file in editor", "Open this file in your editor", path, 0)
}
