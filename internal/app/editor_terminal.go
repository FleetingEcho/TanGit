package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Terminal editors need a real console; a GUI app's detached subprocess has
// neither an interactive screen nor keyboard input.
func terminalEditorProgram(program string) bool {
	switch strings.TrimSuffix(strings.ToLower(filepath.Base(program)), ".exe") {
	case "nvim", "vim", "vi":
		return true
	}
	return false
}

const terminalEditorAppleScript = `on run argv
    set editorCommand to item 1 of argv
    tell application "Terminal"
        do script editorCommand
        activate
    end tell
end run`

// terminalEditorCommand keeps argv intact on Linux and Windows. Terminal.app
// accepts shell text, so every argument (including the directory) is quoted
// independently and is passed as data to a static AppleScript.
func terminalEditorCommand(goos string, args []string, path string, lookPath func(string) (string, error)) (*exec.Cmd, error) {
	program, err := lookPath(args[0])
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, errEditorMissing
		}
		return nil, err
	}
	args = append([]string{program}, args[1:]...)
	dir := filepath.Dir(path)
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		dir = path
	}
	switch goos {
	case "darwin":
		quoted := make([]string, len(args))
		for i, arg := range args {
			quoted[i] = quoteEditorShellArg(arg)
		}
		// Keep Terminal's interactive shell alive after the editor exits.
		command := "cd " + quoteEditorShellArg(dir) + " && " + strings.Join(quoted, " ")
		return exec.Command("/usr/bin/osascript", "-e", terminalEditorAppleScript, command), nil
	case "windows":
		terminal, err := lookPath("wt.exe")
		if err == nil {
			return exec.Command(terminal, append([]string{"new-tab", "-d", dir, "--"}, args...)...), nil
		}
		cmd := exec.Command(program, args[1:]...)
		cmd.Dir = dir
		editorNewConsole(cmd)
		return cmd, nil
	default:
		for _, terminal := range []struct {
			name string
			flag string
		}{
			{"x-terminal-emulator", "-e"}, {"gnome-terminal", "--"},
			{"konsole", "-e"}, {"xfce4-terminal", "-x"},
			{"kitty", "--"}, {"alacritty", "-e"}, {"xterm", "-e"},
		} {
			if executable, err := lookPath(terminal.name); err == nil {
				cmd := exec.Command(executable, append([]string{terminal.flag}, args...)...)
				cmd.Dir = dir
				return cmd, nil
			}
		}
		return nil, fmt.Errorf("no terminal emulator was found to open %s", filepath.Base(program))
	}
}

func quoteEditorShellArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
