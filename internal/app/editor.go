package app

import (
	"errors"
	"os"
	"os/exec"
	pathpkg "path"
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
	Name    string `json:"name"`
	Command string `json:"command"`
}

// editorPresets are the editors Settings offers, in the order they
// draw them. They follow the commands their own installers write: VS Code's
// `code`, Zed's `zed` and Neovim's `nvim`.
var editorPresets = []editorPreset{
	{Name: builtInEditorCommand, Command: builtInEditorCommand},
	{Name: "VS Code", Command: vsCodeEditorCommand},
	{Name: "Zed", Command: "zed " + filenamePlaceholder},
	{Name: "Neovim", Command: "nvim +" + linePlaceholder + " " + filenamePlaceholder},
}

// Built-in uses TanGit's Code page. The external command remains a plain
// setting so existing VS Code, Zed, Neovim, and custom configurations keep working.
const (
	builtInEditorCommand = "Built-in"
	vsCodeEditorCommand  = "code --wait " + filenamePlaceholder
	defaultEditorCommand = builtInEditorCommand
)

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

// allEditorPresets combines the built-in choices with the user's saved external
// editors. Custom names never alter the editor command's documented placeholders.
func (a *app) allEditorPresets() []editorPreset {
	presets := make([]editorPreset, 0, len(editorPresets)+len(a.settings.ExternalEditors))
	presets = append(presets, editorPresets...)
	presets = append(presets, a.settings.ExternalEditors...)
	return presets
}

// normalizeExternalEditors discards unusable or ambiguous persisted entries.
// Names are unique across all presets so every menu action remains unambiguous.
func normalizeExternalEditors(editors []editorPreset) []editorPreset {
	if len(editors) == 0 {
		return nil
	}
	names := make(map[string]bool, len(editorPresets)+len(editors))
	for _, preset := range editorPresets {
		names[strings.ToLower(preset.Name)] = true
	}
	normalized := make([]editorPreset, 0, len(editors))
	for _, editor := range editors {
		editor.Name = strings.TrimSpace(editor.Name)
		editor.Command = strings.TrimSpace(editor.Command)
		key := strings.ToLower(editor.Name)
		if !validExternalEditor(editor) || names[key] {
			continue
		}
		names[key] = true
		normalized = append(normalized, editor)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func validExternalEditor(editor editorPreset) bool {
	if editor.Name == "" || editor.Command == "" ||
		strings.ContainsAny(editor.Name, "\r\n\x00") || strings.ContainsAny(editor.Command, "\r\n\x00") ||
		strings.EqualFold(editor.Command, builtInEditorCommand) {
		return false
	}
	_, err := splitEditorCommand(editor.Command)
	return err == nil
}

// effectiveEditorCommand is the command the app launches: the setting, or the
// default when it is empty.
func effectiveEditorCommand(command string) string {
	if trimmed := strings.TrimSpace(command); trimmed != "" {
		return trimmed
	}
	return defaultEditorCommand
}

// launchEditor runs the configured editor command for path, at line.
func (a *app) launchEditor(path string, line int) {
	a.launchEditorCommand(effectiveEditorCommand(a.settings.EditorCommand), path, line)
}

// launchEditorCommand opens one explicit command without changing the saved
// editor preference, and says what to do when the machine cannot launch it.
func (a *app) launchEditorCommand(command, path string, line int) {
	if command == builtInEditorCommand {
		a.openBuiltInEditor(path, line)
		return
	}
	// An SSH path belongs to the remote host, not to a locally installed editor.
	// Keep this guard beside the process boundary as well as hiding the actions.
	if a.location.IsRemote() {
		return
	}
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

// openBuiltInEditor opens Code for the current repository and, when path names
// a file in it, reveals that file in the explorer. From Home, the repository is
// opened first; Code is already its destination when loading finishes.
func (a *app) openBuiltInEditor(path string, line int) {
	if a.snapshot.Root == "" {
		if !a.sectionVisible(sectionCode) {
			a.setSectionVisible(sectionCode, true)
			a.saveSettings()
		}
		a.workspaceTab = sectionCode
		a.openRecent(path)
		return
	}
	var relative string
	var ok bool
	if a.location.IsRemote() {
		if a.location.Valid() && a.location.Root == a.snapshot.Root {
			relative, ok = codeEditorRemoteRelativePath(a.snapshot.Root, path)
		}
	} else {
		relative, ok = codeEditorRelativePath(a.snapshot.Root, path)
	}
	if !ok {
		a.notify("error", tr("Could not open the file"), tr("The built-in editor can only open files in this repository."))
		return
	}
	if !a.sectionVisible(sectionCode) {
		a.setSectionVisible(sectionCode, true)
		a.saveSettings()
	}
	a.setWorkspaceTab(sectionCode)
	if relative == "" {
		return
	}
	if a.location.IsRemote() {
		a.openCodeDocumentLocation(relative, "", false, line, 0, false)
	} else {
		a.openCodeFile(relative, line)
	}
	a.revealCodeFile(relative)
}

// editorFileLstat is a test seam for keeping local-only filesystem validation
// out of the SSH document-opening path.
var editorFileLstat = os.Lstat

// openGitFileInBuiltInEditor validates a working-tree path at action time.
// Git row state may be stale, and a historical file is not necessarily still
// present, so every failure is reported without switching the Code workspace.
func (a *app) openGitFileInBuiltInEditor(root, relative string) {
	fail := func(description string) {
		a.notify("error", tr("Could not open the file"), description)
	}
	if root == "" || a.snapshot.Root != root {
		fail(tr("The editor is showing another repository. Reopen the file's repository and try again."))
		return
	}
	if !validCodeRelativePath(relative) {
		fail(tr("The built-in editor can only open files in this repository."))
		return
	}
	if a.location.IsRemote() {
		// The remote reader performs its own Lstat; a remote POSIX path must
		// never be checked on the local filesystem.
		filename, err := remoteWorkspacePath(root, relative)
		if err != nil {
			fail(tr("The built-in editor can only open files in this repository."))
			return
		}
		a.openBuiltInEditor(filename, 0)
		return
	}
	filename := filepath.Join(root, filepath.FromSlash(relative))
	info, err := editorFileLstat(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fail(tr("The file has been deleted or no longer exists."))
		} else {
			fail(safeError(err))
		}
		return
	}
	// Match the Explorer's file contract. In particular this rejects folders,
	// submodule gitlinks, device files, and links that could leave the repo.
	if !info.Mode().IsRegular() {
		fail(tr("Only regular files in this repository can be opened in the built-in editor."))
		return
	}
	a.openBuiltInEditor(filename, 0)
}

func codeEditorRelativePath(root, name string) (string, bool) {
	root, rootErr := filepath.Abs(root)
	name, nameErr := filepath.Abs(name)
	if rootErr != nil || nameErr != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, name)
	if err != nil {
		return "", false
	}
	if relative == "." {
		return "", true
	}
	relative = filepath.ToSlash(relative)
	if !validCodeRelativePath(relative) {
		return "", false
	}
	return relative, true
}

func codeEditorRemoteRelativePath(root, name string) (string, bool) {
	if root == "" || !pathpkg.IsAbs(root) || !pathpkg.IsAbs(name) {
		return "", false
	}
	root, name = pathpkg.Clean(root), pathpkg.Clean(name)
	if name == root {
		return "", true
	}
	prefix := strings.TrimSuffix(root, "/") + "/"
	if !strings.HasPrefix(name, prefix) {
		return "", false
	}
	relative := strings.TrimPrefix(name, prefix)
	if _, err := remoteWorkspacePath(root, relative); err != nil {
		return "", false
	}
	return relative, true
}

// editorLaunchButton opens the path immediately with the configured command.
// Editor selection belongs in Settings, not in the launch action.
func (a *app) editorLaunchButton(c *ui.Context, label, tooltip, path string, line int) {
	button := compactIconButton(ui.Button(c, ""))
	button.Label(tr(label)).Tooltip(tr(tooltip) + "\n" + tr("Configure your editor in Settings → Editor."))
	button.Children(func() {
		ui.Icon(c, fileCodeIcon).FontSize(14)
	})
	if button.Clicked() {
		a.launchEditor(path, line)
	}
}

// editorButton opens the repository in the chosen editor, where its files are
// edited before they are staged here.
func (a *app) editorButton(c *ui.Context) {
	if a.snapshot.Root == "" || a.location.IsRemote() && effectiveEditorCommand(a.settings.EditorCommand) != builtInEditorCommand {
		return
	}
	a.editorLaunchButton(c, "Open in editor", "Open this repository in your editor", a.snapshot.Root, 0)
}

// repositoryExternalEditorButton offers direct workspace launches independently
// of the configured editor. Picking an IDE here is an action, not a preference.
func (a *app) repositoryExternalEditorButton(c *ui.Context) {
	a.repositoryExternalEditorButtonWithText(c, "")
}

func (a *app) repositoryExternalEditorButtonWithText(c *ui.Context, text string) {
	if a.snapshot.Root == "" || a.location.IsRemote() {
		return
	}
	workspacePath := a.snapshot.Root
	if folder, ok := normalizedCodeSearchFolder(a.code.searchFolder); ok && folder != "" {
		workspacePath = filepath.Join(workspacePath, filepath.FromSlash(folder))
	}
	theme := c.Theme()
	button := ui.ButtonBase(c).Padding(4).Radius(theme.Radius)
	button.Label(tr("Open repository in external editor")).
		Tooltip(tr("Open this repository in VS Code, Zed, or another editor"))
	if text != "" {
		button.Label(tr(text))
	}
	button.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
			ui.Icon(c, externalLinkIcon).FontSize(14).TextColor(theme.TextMuted)
			if text != "" {
				ui.Text(c, tr(text))
			}
		})
	})
	if button.Hovered() {
		button.Background(theme.SurfaceHover)
	}
	button.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item("VS Code").Chosen() {
			a.launchEditorCommand(vsCodeEditorCommand, workspacePath, 0)
		}
		if m.Item("Zed").Chosen() {
			a.launchEditorCommand(editorPresets[2].Command, workspacePath, 0)
		}
		for _, editor := range a.settings.ExternalEditors {
			if m.Item(editor.Name).Chosen() {
				a.launchEditorCommand(editor.Command, workspacePath, 0)
			}
		}
		m.Separator()
		if m.Item(tr("Customize editor…")).Chosen() {
			a.settingsPage = settingsPageEditor
			a.openSettings()
		}
	})
}

// fileEditorButton opens the selected file in the chosen editor, from the
// working tree, so hand edits beside the diff land in the same file.
func (a *app) fileEditorButton(c *ui.Context, file repository.File) {
	if a.location.IsRemote() && effectiveEditorCommand(a.settings.EditorCommand) != builtInEditorCommand {
		return
	}
	path := a.workingFilePath(file.Path)
	if path == "" {
		return
	}
	a.editorLaunchButton(c, "Open file in editor", "Open this file in your editor", path, 0)
}
