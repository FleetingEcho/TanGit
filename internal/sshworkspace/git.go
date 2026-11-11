package sshworkspace

import (
	"context"
	"fmt"
	"strings"
)

// GitRoot checks repository membership, including subfolders and linked
// worktrees. Only Git's explicit not-a-repository diagnostic means absence;
// permission, missing executable and transport failures remain errors.
func (c *Client) GitRoot(ctx context.Context, dir string) (string, bool, error) {
	result, err := c.Run(ctx, dir, "env", "LC_ALL=C", "git", "rev-parse", "--show-toplevel")
	output := strings.TrimSpace(string(result.Output))
	if err != nil {
		if result.ExitCode == 128 && strings.Contains(output, "fatal: not a git repository") {
			return "", false, nil
		}
		return "", false, fmt.Errorf("check remote Git repository: %s: %w", output, err)
	}
	if !strings.HasPrefix(output, "/") || strings.ContainsAny(output, "\r\n\x00") {
		return "", false, fmt.Errorf("Git returned an invalid repository root %q", output)
	}
	return output, true, nil
}
