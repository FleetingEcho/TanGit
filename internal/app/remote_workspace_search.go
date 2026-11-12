package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path"
	"strings"

	"lazygitapp/internal/sshworkspace"
)

// searchRemoteWorkspaceFiles never invokes the local LookPath, Stat, ReadFile,
// EvalSymlinks, or exec seams. Ripgrep runs in the captured SSH session and
// falls back to the same session's SFTP reads when rg is unavailable.
func searchRemoteWorkspaceFiles(ctx context.Context, client remoteWorkspaceClient, root string, paths []string, options workspaceSearchOptions, limit int) ([]workspaceSearchMatch, workspaceSearchBackend, error) {
	if options.Query == "" || limit <= 0 || ctx.Err() != nil {
		return nil, workspaceSearchBackendNone, nil
	}
	if _, err := options.regularExpression(); err != nil {
		return nil, workspaceSearchBackendNone, err
	}
	if !options.PreferNative {
		if matches, ok := searchRemoteWorkspaceRipgrep(ctx, client, root, paths, options, limit); ok {
			return matches, workspaceSearchBackendRipgrep, nil
		}
	}
	matches, err := searchRemoteWorkspaceFilesystem(ctx, client, root, paths, options, limit)
	return matches, workspaceSearchBackendGo, err
}

func searchRemoteWorkspaceFilesystem(ctx context.Context, client remoteWorkspaceClient, root string, paths []string, options workspaceSearchOptions, limit int) ([]workspaceSearchMatch, error) {
	expression, err := options.regularExpression()
	if err != nil {
		return nil, err
	}
	var matches []workspaceSearchMatch
	for _, candidate := range paths {
		if ctx.Err() != nil {
			return matches, nil
		}
		if !options.pathIncluded(candidate) {
			continue
		}
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		filename, info, statErr := remoteRegularFile(client, root, cleaned)
		if statErr != nil || info.Size() > workspaceSearchMaxFileSize {
			continue
		}
		data, readErr := client.ReadFile(filename)
		if readErr != nil || ctx.Err() != nil || len(data) == 0 || len(data) > workspaceSearchMaxFileSize || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		lines := bytes.Split(data, []byte{'\n'})
		for lineIndex, rawLine := range lines {
			if lineIndex == len(lines)-1 && len(rawLine) == 0 && len(data) > 0 {
				continue
			}
			if ctx.Err() != nil {
				return matches, nil
			}
			line := strings.TrimSuffix(string(rawLine), "\r")
			column, _, found := workspaceSearchLineRange(line, options, expression)
			if !found {
				continue
			}
			matches = append(matches, workspaceSearchMatch{
				Path: cleaned, Line: lineIndex + 1, Column: column + 1,
				Text: workspaceSearchSnippet(line, column),
			})
			if len(matches) == limit {
				return matches, nil
			}
		}
	}
	return matches, nil
}

func searchRemoteWorkspaceRipgrep(ctx context.Context, client remoteWorkspaceClient, root string, paths []string, options workspaceSearchOptions, limit int) ([]workspaceSearchMatch, bool) {
	if _, err := options.regularExpression(); err != nil {
		return nil, false
	}
	paths = filterWorkspaceSearchPaths(paths, options)
	var filenames []string
	logicalPaths := make(map[string]string, len(paths))
	for _, candidate := range paths {
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		filenames = append(filenames, cleaned)
		logicalPaths[cleaned] = cleaned
	}
	if len(filenames) == 0 {
		return nil, true
	}
	baseArgumentSize := workspaceSearchArgumentSize(workspaceSearchRipgrepArguments(options, limit, nil))
	var matches []workspaceSearchMatch
	for start := 0; start < len(filenames); {
		argumentSize, end := baseArgumentSize, start
		for end < len(filenames) {
			pathSize := len(filenames[end]) + 1
			if end > start && argumentSize+pathSize > workspaceSearchMaxArgumentSize {
				break
			}
			argumentSize += pathSize
			end++
		}
		batch, ok := searchRemoteWorkspaceRipgrepBatch(ctx, client, root, options, filenames[start:end], logicalPaths, limit-len(matches))
		matches = append(matches, batch...)
		if !ok {
			return nil, false
		}
		if ctx.Err() != nil || len(matches) == limit {
			return matches, true
		}
		start = end
	}
	return matches, true
}

type remoteRipgrepResult struct {
	result sshworkspace.ExecResult
	err    error
}

func searchRemoteWorkspaceRipgrepBatch(ctx context.Context, client remoteWorkspaceClient, root string, options workspaceSearchOptions, filenames []string, logicalPaths map[string]string, limit int) ([]workspaceSearchMatch, bool) {
	expression, err := options.regularExpression()
	if err != nil || limit <= 0 {
		return nil, false
	}
	commandContext, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	resultChannel := make(chan remoteRipgrepResult, 1)
	go func() {
		result, runErr := client.RunStreaming(commandContext, sshworkspace.ExecOptions{
			Dir: root, Stdout: writer, Stderr: io.Discard,
		}, "rg", workspaceSearchRipgrepArguments(options, limit, filenames)...)
		_ = writer.Close()
		resultChannel <- remoteRipgrepResult{result: result, err: runErr}
	}()

	decoder := json.NewDecoder(reader)
	var matches []workspaceSearchMatch
	var matchFiles []string
	searchableFiles := make(map[string]bool)
	valid, limited := true, false
	for !limited {
		var message workspaceSearchRipgrepMessage
		if decodeErr := decoder.Decode(&message); decodeErr != nil {
			if decodeErr != io.EOF {
				valid = false
			}
			break
		}
		filename := path.Clean(message.Data.Path.String())
		logicalPath, knownPath := logicalPaths[filename]
		switch message.Type {
		case "match":
			if !knownPath || message.Data.LineNumber <= 0 || len(message.Data.Submatches) == 0 {
				continue
			}
			searchable, checked := searchableFiles[filename]
			if !checked {
				_, info, statErr := remoteRegularFile(client, root, logicalPath)
				searchable = statErr == nil && info.Size() <= workspaceSearchMaxFileSize
				searchableFiles[filename] = searchable
			}
			if !searchable {
				continue
			}
			line := strings.TrimSuffix(message.Data.Lines.String(), "\n")
			line = strings.TrimSuffix(line, "\r")
			column, _, found := workspaceSearchLineRange(line, options, expression)
			if !found {
				continue
			}
			matches = append(matches, workspaceSearchMatch{
				Path: logicalPath, Line: message.Data.LineNumber, Column: column + 1,
				Text: workspaceSearchSnippet(line, column),
			})
			matchFiles = append(matchFiles, filename)
		case "end":
			if !knownPath {
				continue
			}
			if message.Data.BinaryOffset != nil {
				keptMatches := matches[:0]
				keptFiles := matchFiles[:0]
				for index, matchFile := range matchFiles {
					if matchFile == filename {
						continue
					}
					keptMatches = append(keptMatches, matches[index])
					keptFiles = append(keptFiles, matchFile)
				}
				matches, matchFiles = keptMatches, keptFiles
			}
			limited = len(matches) >= limit
		}
	}
	if limited {
		cancel()
	}
	_ = reader.Close()
	completed := <-resultChannel
	if ctx.Err() != nil {
		return matches, true
	}
	if limited {
		if len(matches) > limit {
			matches = matches[:limit]
		}
		return matches, true
	}
	if !valid {
		return nil, false
	}
	if completed.err != nil {
		// rg uses 1 for no matches and 2 for an incomplete/partially missing
		// operand set. Any valid JSON result is useful in those cases; command
		// not found, transport errors, and unreadable output use SFTP fallback.
		if completed.result.ExitCode != 1 && completed.result.ExitCode != 2 {
			return nil, false
		}
		if len(matches) == 0 && completed.result.ExitCode == 2 {
			return nil, false
		}
	}
	return matches, true
}
