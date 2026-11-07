package sshworkspace

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
)

// HomeDirectory asks the remote login shell for its initial absolute working
// directory. It does not cache the value because each call is independent.
func (c *Client) HomeDirectory(ctx context.Context) (string, error) {
	result, err := c.Run(ctx, "", "pwd")
	if err != nil {
		return "", fmt.Errorf("resolve remote home directory: %w", err)
	}
	home := strings.TrimSuffix(string(result.Output), "\n")
	home = strings.TrimSuffix(home, "\r")
	if home == "" || !path.IsAbs(home) || strings.ContainsRune(home, '\x00') || strings.Contains(home, "\n") {
		return "", fmt.Errorf("remote pwd returned an invalid absolute path %q", home)
	}
	return path.Clean(home), nil
}

// BrowserRoot returns an absolute remote starting directory. An empty profile
// root means the remote home directory.
func (c *Client) BrowserRoot(ctx context.Context) (string, error) {
	return c.ResolvePath(ctx, c.profile.DefaultRoot)
}

// ResolvePath expands "~" and makes relative paths absolute against the remote
// home directory. An empty path resolves to DefaultRoot, then the home directory.
// SFTP methods themselves do not implicitly expand paths.
func (c *Client) ResolvePath(ctx context.Context, remotePath string) (string, error) {
	if remotePath == "" {
		remotePath = c.profile.DefaultRoot
	}
	home, err := c.HomeDirectory(ctx)
	if err != nil {
		return "", err
	}
	return ResolveRemotePath(home, remotePath)
}

// ResolveRemotePath is the pure POSIX-path helper used by ResolvePath.
func ResolveRemotePath(home, remotePath string) (string, error) {
	if home == "" || !path.IsAbs(home) || strings.ContainsRune(home, '\x00') {
		return "", errors.New("remote home must be a nonempty absolute POSIX path without NUL")
	}
	home = path.Clean(home)
	if strings.ContainsRune(remotePath, '\x00') {
		return "", errors.New("remote path contains NUL")
	}
	switch {
	case remotePath == "", remotePath == "~":
		return home, nil
	case strings.HasPrefix(remotePath, "~/"):
		return path.Join(home, strings.TrimPrefix(remotePath, "~/")), nil
	case strings.HasPrefix(remotePath, "~"):
		return "", fmt.Errorf("remote path %q uses unsupported ~user expansion", remotePath)
	case path.IsAbs(remotePath):
		return path.Clean(remotePath), nil
	default:
		return path.Join(home, remotePath), nil
	}
}
