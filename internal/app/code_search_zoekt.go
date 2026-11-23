//go:build linux || darwin || freebsd || netbsd || openbsd || illumos

package app

import (
	"context"
	"errors"

	"github.com/sourcegraph/zoekt"
	"github.com/sourcegraph/zoekt/index"
	"github.com/sourcegraph/zoekt/query"
	zoektsearch "github.com/sourcegraph/zoekt/search"
)

type zoektCodeSearchIndexBuilder struct {
	builder *index.Builder
}

func (b zoektCodeSearchIndexBuilder) Add(name string, content []byte) error {
	return b.builder.Add(index.Document{Name: name, Content: content})
}

func (b zoektCodeSearchIndexBuilder) Finish() error {
	return b.builder.Finish()
}

func codeSearchIndexingSupported() bool { return true }

func newCodeSearchIndexBuilder(directory string) (codeSearchIndexBuilder, error) {
	options := index.Options{
		IndexDir:            directory,
		ShardPrefixOverride: "working",
		SizeMax:             workspaceSearchMaxFileSize,
		// One worker avoids multiplying memory and disk pressure for large
		// repositories. Queries themselves remain parallel inside Zoekt.
		Parallelism:  1,
		DisableCTags: true,
		RepositoryDescription: zoekt.Repository{
			ID: 1, Name: "lazygitGo working tree",
		},
		// lazygitGo has already applied its own size and binary filtering. Do not
		// let Zoekt silently substitute NOT-INDEXED for large/trigram-heavy
		// text. The under-three-byte exception is handled by the native complement.
		LargeFiles: []string{"**"},
	}
	options.SetDefaults()
	// SetDefaults fills zero fields; preserve the explicit integration values.
	options.IndexDir = directory
	options.ShardPrefixOverride = "working"
	options.SizeMax = workspaceSearchMaxFileSize
	options.Parallelism = 1
	options.DisableCTags = true
	options.LargeFiles = []string{"**"}
	builder, err := index.NewBuilder(options)
	if err != nil {
		return nil, err
	}
	return zoektCodeSearchIndexBuilder{builder: builder}, nil
}

func codeSearchIndexCandidates(ctx context.Context, directory string, pathCount int, options workspaceSearchOptions) (map[string]bool, error) {
	if !options.CaseSensitive && !options.RegularExpr && !workspaceSearchASCII(options.Query) {
		// Zoekt and Go do not guarantee identical Unicode case-fold candidate
		// sets for arbitrary non-ASCII literals. Retain the established engines
		// for this uncommon case rather than risking a missed match.
		return nil, errors.New("non-ASCII case-insensitive literal is not index eligible")
	}
	var (
		zoektQuery query.Q
		err        error
	)
	if options.RegularExpr {
		pattern := options.Query
		if !options.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		zoektQuery, err = query.RegexpQuery("(?m)"+pattern, true, false)
	} else {
		zoektQuery = &query.Substring{Pattern: options.Query, CaseSensitive: options.CaseSensitive, Content: true}
	}
	if err != nil {
		return nil, err
	}
	searcher, err := zoektsearch.NewDirectorySearcher(directory)
	if err != nil {
		return nil, err
	}
	defer searcher.Close()
	result, err := searcher.Search(ctx, zoektQuery, &zoekt.SearchOptions{MaxDocDisplayCount: pathCount})
	if err != nil {
		return nil, err
	}
	// Candidate truncation or a crashed/skipped shard can hide matches. Never
	// publish a partial indexed answer as if it were complete.
	if result.Stats.Crashes != 0 || result.Stats.FilesSkipped != 0 || result.Stats.ShardsSkipped != 0 {
		return nil, errors.New("Zoekt returned partial candidates")
	}
	candidates := make(map[string]bool, len(result.Files))
	for _, file := range result.Files {
		candidates[file.FileName] = true
	}
	return candidates, nil
}
