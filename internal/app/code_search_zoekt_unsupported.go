//go:build !(linux || darwin || freebsd || netbsd || openbsd || illumos)

package app

import "context"

func codeSearchIndexingSupported() bool { return false }

func newCodeSearchIndexBuilder(string) (codeSearchIndexBuilder, error) {
	return nil, errCodeSearchIndexUnavailable
}

func codeSearchIndexCandidates(context.Context, string, int, workspaceSearchOptions) (map[string]bool, error) {
	return nil, errCodeSearchIndexUnavailable
}
