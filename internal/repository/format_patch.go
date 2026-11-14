package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ExportPatches writes one mailbox-format patch per commit to directory,
// creating it if needed. Commits are written oldest first, named with a
// sequence number and a sanitized subject, matching git format-patch output.
// The returned paths are in the order they were written.
func (s *gitSession) ExportPatches(ctx context.Context, commits []Commit, directory string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("export patches: no commits selected")
	}
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("export patches: a target folder is required")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("export patches: %w", err)
	}
	// The app hands commits newest first; patches apply oldest first.
	ordered := make([]Commit, len(commits))
	for index, commit := range commits {
		ordered[len(commits)-1-index] = commit
	}
	paths := make([]string, 0, len(ordered))
	for index, commit := range ordered {
		slug := sanitizePatchSubject(commit.Subject)
		name := fmt.Sprintf("%04d-%s.patch", index+1, slug)
		path := uniquePatchPath(directory, name)
		output, err := s.cmd.New([]string{"git", "format-patch", "-1", "--stdout", commit.Hash}).DontLog().RunWithOutput()
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", commit.ShortHash, err)
		}
		if err := writeNewPatchFile(path, []byte(output)); err != nil {
			return nil, fmt.Errorf("export patches: %w", err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// AmPatch applies a mailbox-format patch (git format-patch output or a series
// in one file) with a three-way fallback and authorship preserved, exactly
// like git am --3way. The content travels over stdin so it works the same
// locally and over SSH.
func (s *gitSession) AmPatch(ctx context.Context, name string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(content) == 0 {
		return fmt.Errorf("apply patch: %s is empty", name)
	}
	if _, err := s.cmd.New([]string{"git", "am", "--3way"}).SetStdin(string(content)).RunWithOutput(); err != nil {
		return fmt.Errorf("apply patch: %w", err)
	}
	return nil
}

// sanitizePatchSubject reduces a commit subject to a safe filename component.
func sanitizePatchSubject(subject string) string {
	subject = strings.ToLower(strings.TrimSpace(subject))
	var builder strings.Builder
	lastDash := false
	for _, r := range subject {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case r == '-' || r == ' ' || r == '_' || r == '/':
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
		if builder.Len() >= 60 {
			break
		}
	}
	name := strings.Trim(builder.String(), "-")
	if name == "" {
		name = "patch"
	}
	return name
}

// uniquePatchPath reserves distinct filenames for repeated exports so an
// existing user patch is never silently overwritten.
func uniquePatchPath(directory, name string) string {
	candidate := filepath.Join(directory, name)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	ext := filepath.Ext(name)
	for n := 1; n < 10000; n++ {
		candidate := filepath.Join(directory, fmt.Sprintf("%s-%d%s", base, n, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(directory, name)
}

func writeNewPatchFile(name string, content []byte) error {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
