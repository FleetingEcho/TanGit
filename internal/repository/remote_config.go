package repository

import (
	"context"
	"strings"
	"sync"

	"github.com/jesseduffield/lazygit/pkg/commands/git_config"

	"lazygitapp/internal/sshworkspace"
)

// remoteGitConfig satisfies Lazygit's string-only config interface without its
// standard implementation's host exec.Command calls. A fresh read also avoids
// retaining remote config indefinitely between independent refresh operations.
type remoteGitConfig struct {
	client *sshworkspace.Client
	mu     sync.RWMutex
	dir    string
}

var _ git_config.IGitConfig = (*remoteGitConfig)(nil)

func (c *remoteGitConfig) SetDir(dir string) {
	c.mu.Lock()
	c.dir = dir
	c.mu.Unlock()
}

func (c *remoteGitConfig) DropCache() {}

func (c *remoteGitConfig) get(args ...string) string {
	c.mu.RLock()
	dir := c.dir
	c.mu.RUnlock()
	argv := []string{"git"}
	if dir != "" {
		argv = append(argv, "-C", dir)
	}
	argv = append(argv, "config")
	argv = append(argv, args...)
	result, err := c.client.Run(context.Background(), "", argv[0], argv[1:]...)
	if err != nil {
		return ""
	}
	return string(result.Output)
}

func (c *remoteGitConfig) Get(key string) string {
	return strings.TrimSuffix(c.get("--get", "--null", key), "\x00")
}

func (c *remoteGitConfig) GetGeneral(args string) string {
	return c.get(strings.Fields(args)...)
}

func (c *remoteGitConfig) GetBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(c.get("--bool", "--get", key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
