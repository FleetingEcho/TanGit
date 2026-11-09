package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// terminalDefaultShellName mirrors the plugin's default command selection so
// a tab can identify its shell before the program reports an OSC title.
func terminalDefaultShellName() string {
	if runtime.GOOS == "windows" {
		return terminalWindowsShellName(os.Getenv("COMSPEC"), exec.LookPath)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
		if runtime.GOOS == "darwin" {
			shell = "/bin/zsh"
		}
	}
	return filepath.Base(shell)
}

// terminalWindowsShellName keeps the same discovery order as the terminal
// plugin. The lookup seam lets tests cover every Windows branch on any host.
func terminalWindowsShellName(comspec string, lookPath func(string) (string, error)) string {
	for _, name := range []string{"pwsh.exe", "powershell.exe", comspec, "cmd.exe"} {
		if path, err := lookPath(name); name != "" && err == nil {
			base := filepath.Base(path)
			return strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	return ""
}

// terminalDisplayDirectory abbreviates only the actual home-directory prefix,
// leaving repository names and other path components intact.
func terminalDisplayDirectory(directory string) string {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return ""
	}
	directory = filepath.Clean(directory)
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return directory
	}
	home = filepath.Clean(home)
	relative, err := filepath.Rel(home, directory)
	if err != nil {
		return directory
	}
	switch {
	case relative == ".":
		return "~"
	case relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)):
		return filepath.Join("~", relative)
	default:
		return directory
	}
}

func terminalSessionLabel(session *terminalSessionState) string {
	if session.name != "" {
		return session.name
	}
	if session.shell != "" {
		return session.shell
	}
	if title := strings.TrimSpace(session.title); title != "" {
		return title
	}
	return tr("Terminal")
}

func terminalSessionDetail(session *terminalSessionState) string {
	if title := strings.TrimSpace(session.title); title != "" && title != terminalSessionLabel(session) {
		return title
	}
	return session.directory
}

// terminalGroupLabel keeps a top-level tab's custom name separate from its
// splits. The default identity follows the selected shell but deliberately
// ignores that shell's custom name.
func (a *app) terminalGroupLabel(group uint64, session *terminalSessionState) string {
	if name := a.term.groupNames[group]; name != "" {
		return name
	}
	fallback := *session
	fallback.name = ""
	return terminalSessionLabel(&fallback)
}

// terminalGroupSession returns the shell whose default identity represents a
// top-level tab: its selected split, or the last split selected in that tab.
func (a *app) terminalGroupSession(group uint64) *terminalSessionState {
	if active := a.activeTerminal(); active != nil && active.groupID == group {
		return active
	}
	if activeID := a.term.activeSessions[group]; activeID != 0 {
		if _, session := a.terminalSessionByID(activeID); session != nil && session.groupID == group {
			return session
		}
	}
	for index := range a.term.sessions {
		if a.term.sessions[index].groupID == group {
			return &a.term.sessions[index]
		}
	}
	return nil
}
