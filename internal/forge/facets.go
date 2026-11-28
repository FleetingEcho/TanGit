package forge

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// Facets are the names a filter can pick from: people who can author, be
// assigned or review, and the labels the project defines.
type Facets struct {
	People []string
	Labels []string
}

// Facets reads people and labels from the provider. A part that cannot be read
// (a token without membership access, say) comes back empty rather than failing
// the other, so filters still offer what is known.
func (c *cliClient) Facets(ctx context.Context) (Facets, error) {
	if c == nil || c.runner == nil {
		return Facets{}, errors.New("forge command runner is unavailable")
	}
	tool, err := c.runner.PullRequestTool(ctx)
	if err != nil {
		return Facets{}, err
	}
	var peoplePath, labelsPath string
	var personKeys []string
	switch tool {
	case "gh":
		peoplePath, labelsPath, personKeys = "repos/{owner}/{repo}/assignees?per_page=100", "repos/{owner}/{repo}/labels?per_page=100", []string{"login"}
	case "glab":
		peoplePath, labelsPath, personKeys = "projects/:fullpath/members/all?per_page=100", "projects/:fullpath/labels?per_page=100", []string{"username"}
	default:
		return Facets{}, errors.New("unsupported hosting tool " + tool)
	}
	var facets Facets
	var firstErr error
	if output, err := c.runner.RunHostingCommand(ctx, tool, []string{"api", peoplePath}); err == nil {
		facets.People = namesFrom([]byte(output), personKeys)
	} else {
		firstErr = err
	}
	if output, err := c.runner.RunHostingCommand(ctx, tool, []string{"api", labelsPath}); err == nil {
		facets.Labels = namesFrom([]byte(output), []string{"name"})
	} else if firstErr == nil {
		firstErr = err
	}
	if len(facets.People) == 0 && len(facets.Labels) == 0 {
		return facets, firstErr
	}
	return facets, nil
}

func namesFrom(data []byte, keys []string) []string {
	var items []map[string]json.RawMessage
	if json.Unmarshal(data, &items) != nil {
		return nil
	}
	var names []string
	for _, item := range items {
		if name := strings.TrimSpace(stringField(item, keys...)); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
