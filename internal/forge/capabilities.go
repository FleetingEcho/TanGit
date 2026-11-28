package forge

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
)

// Capabilities says what the connected credentials may do, so the UI can hide
// controls that would only fail.
type Capabilities struct {
	// CanWrite is false only when the provider positively reported a
	// read-only token. Unknown stays true: a failing write then shows its error.
	CanWrite bool
	Scopes   []string
}

// Capabilities reads the token's scopes. GitLab reports them for the token in
// use; GitHub does not expose them through the CLI, so it is treated as writable.
func (c *cliClient) Capabilities(ctx context.Context) (Capabilities, error) {
	unknown := Capabilities{CanWrite: true}
	if c == nil || c.runner == nil {
		return unknown, errors.New("forge command runner is unavailable")
	}
	tool, err := c.runner.PullRequestTool(ctx)
	if err != nil || tool != "glab" {
		return unknown, err
	}
	output, err := c.runner.RunHostingCommand(ctx, tool, []string{"api", "personal_access_tokens/self"})
	if err != nil {
		return unknown, err
	}
	return parseGitLabCapabilities([]byte(output)), nil
}

func parseGitLabCapabilities(data []byte) Capabilities {
	var token struct {
		Scopes []string `json:"scopes"`
	}
	if json.Unmarshal(data, &token) != nil || len(token.Scopes) == 0 {
		return Capabilities{CanWrite: true}
	}
	return Capabilities{Scopes: token.Scopes, CanWrite: slices.Contains(token.Scopes, "api")}
}
