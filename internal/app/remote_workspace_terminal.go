package app

import (
	"context"
	"errors"
	"math"
	"path"
	"strings"

	"github.com/FleetingEcho/lazygitGo/internal/sshworkspace"
)

// remoteTerminalConn adapts the SSH uint32 PTY API to MyGo's signed cell
// dimensions. Every terminal owns its own conn; Close and Resize never touch
// another terminal channel on the shared SSH client.
type remoteTerminalConn struct {
	terminal *sshworkspace.Terminal
}

func (c *remoteTerminalConn) Read(data []byte) (int, error)  { return c.terminal.Read(data) }
func (c *remoteTerminalConn) Write(data []byte) (int, error) { return c.terminal.Write(data) }
func (c *remoteTerminalConn) Close() error                   { return c.terminal.Close() }
func (c *remoteTerminalConn) Resize(columns, rows int) error {
	columns, rows = max(1, columns), max(1, rows)
	columns, rows = min(columns, math.MaxUint32), min(rows, math.MaxUint32)
	return c.terminal.Resize(uint32(columns), uint32(rows))
}

type remoteTerminalOpener interface {
	OpenTerminal(context.Context, sshworkspace.TerminalOptions) (*sshworkspace.Terminal, error)
}

func openRemoteTerminal(ctx context.Context, client remoteWorkspaceClient, root string) (*remoteTerminalConn, error) {
	opener, ok := client.(remoteTerminalOpener)
	if !ok {
		return nil, errors.New("SSH workspace does not provide interactive terminals")
	}
	// The login shell is started in the repository directly by an exec request:
	// no line is typed into a shell, so it cannot reach the remote history, be
	// swallowed by a startup file's prompt, or flash on screen. -l keeps the
	// environment a login shell would have had.
	command := "cd -- " + sshworkspace.ShellQuote(root) + " && exec ${SHELL:-/bin/sh} -l"
	terminal, err := opener.OpenTerminal(ctx, sshworkspace.TerminalOptions{Columns: 80, Rows: 24, Command: command})
	if err != nil {
		return nil, err
	}
	connection := &remoteTerminalConn{terminal: terminal}
	return connection, nil
}

func remoteTerminalFileLink(root, link string) (string, int, bool) {
	link = strings.TrimSpace(link)
	if root == "" || link == "" || !path.IsAbs(root) {
		return "", 0, false
	}
	name, line := splitTerminalLine(link)
	if terminalLinkHasScheme(name) || name == "~" || strings.HasPrefix(name, "~/") {
		return "", 0, false
	}
	var relative string
	if path.IsAbs(name) {
		cleanRoot, cleanName := path.Clean(root), path.Clean(name)
		switch {
		case cleanName == cleanRoot:
			return "", 0, false
		case strings.HasPrefix(cleanName, cleanRoot+"/"):
			relative = strings.TrimPrefix(cleanName, cleanRoot+"/")
		default:
			return "", 0, false
		}
	} else {
		relative = path.Clean(name)
	}
	if !validCodeManagedPath(relative) {
		return "", 0, false
	}
	return relative, line, true
}
