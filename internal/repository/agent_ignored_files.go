package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"

	"lazygitapp/internal/sshworkspace"
)

// A task may need a small local configuration file, but ignored files must
// never become an implicit worktree-discovery channel. The caller supplies the
// complete allowlist and each source must independently pass check-ignore.
const maxAgentIgnoredFileSize = 16 << 20

type agentIgnoredCopy struct {
	relative    string
	source      string
	destination string
	mode        os.FileMode
}

// CopyAgentIgnoredFiles copies exactly relativePaths from sourceWorktree into
// this session's worktree. It preflights every path before writing anything.
// An I/O failure during the copy itself can leave earlier files copied; its
// error identifies both the current path and those earlier files.
func (s *gitSession) CopyAgentIgnoredFiles(ctx context.Context, sourceWorktree string, relativePaths []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(relativePaths) == 0 {
		return nil
	}
	if sourceWorktree == "" {
		return fmt.Errorf("copy agent ignored files: source worktree is empty")
	}

	files := s.fileService()
	destinationWorktree := s.git.RepoPaths.WorktreePath()
	if err := validateAgentIgnoredRoot(files, sourceWorktree, "source"); err != nil {
		return err
	}
	if err := validateAgentIgnoredRoot(files, destinationWorktree, "destination"); err != nil {
		return err
	}

	copies := make([]agentIgnoredCopy, 0, len(relativePaths))
	seen := make(map[string]struct{}, len(relativePaths))
	for _, supplied := range relativePaths {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := validAgentIgnoredRelativePath(supplied)
		if err != nil {
			return fmt.Errorf("copy agent ignored files: %w", err)
		}
		// Reject case-only aliases too: a list validated on Linux must not turn
		// into two writes to one file on a case-insensitive worktree.
		key := strings.ToLower(relative)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("copy agent ignored files: duplicate or ambiguous path %q", relative)
		}
		seen[key] = struct{}{}

		source, err := s.worktreeFilePath(sourceWorktree, relative)
		if err != nil {
			return fmt.Errorf("copy agent ignored file %q: %w", relative, err)
		}
		destination, err := s.worktreeFilePath(destinationWorktree, relative)
		if err != nil {
			return fmt.Errorf("copy agent ignored file %q: %w", relative, err)
		}
		if err := s.validateAgentIgnoredParents(ctx, files, sourceWorktree, relative, false); err != nil {
			return fmt.Errorf("copy agent ignored file %q: source: %w", relative, err)
		}
		info, err := files.Lstat(source)
		switch {
		case os.IsNotExist(err):
			return fmt.Errorf("copy agent ignored file %q: source is missing", relative)
		case err != nil:
			return fmt.Errorf("copy agent ignored file %q: inspect source: %w", relative, err)
		case !info.Mode().IsRegular():
			return fmt.Errorf("copy agent ignored file %q: source is not a regular file", relative)
		case info.Size() < 0 || info.Size() > maxAgentIgnoredFileSize:
			return fmt.Errorf("copy agent ignored file %q: source exceeds the %d-byte limit", relative, maxAgentIgnoredFileSize)
		}
		if err := s.checkAgentIgnored(ctx, sourceWorktree, relative); err != nil {
			return err
		}
		if err := s.validateAgentIgnoredParents(ctx, files, destinationWorktree, relative, true); err != nil {
			return fmt.Errorf("copy agent ignored file %q: destination: %w", relative, err)
		}
		if _, err := files.Lstat(destination); err == nil {
			return fmt.Errorf("copy agent ignored file %q: destination already exists", relative)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("copy agent ignored file %q: inspect destination: %w", relative, err)
		}
		copies = append(copies, agentIgnoredCopy{
			relative: relative, source: source, destination: destination, mode: info.Mode(),
		})
	}

	copied := make([]string, 0, len(copies))
	for _, copy := range copies {
		if err := ctx.Err(); err != nil {
			return agentIgnoredCopyError(copy.relative, copied, err)
		}
		content, err := readAgentIgnoredSource(ctx, files, copy.source)
		if err != nil {
			return agentIgnoredCopyError(copy.relative, copied, err)
		}
		parent := copy.destination
		if directory := pathpkg.Dir(copy.relative); directory != "." {
			parent, err = s.worktreeFilePath(destinationWorktree, directory)
			if err != nil {
				return agentIgnoredCopyError(copy.relative, copied, err)
			}
		} else {
			parent = destinationWorktree
		}
		if err := files.MkdirAll(parent, 0o700); err != nil {
			return agentIgnoredCopyError(copy.relative, copied, fmt.Errorf("create destination parent: %w", err))
		}
		// Check the chain again after creating missing directories. This also
		// catches an existing parent replaced by a link since preflight.
		if err := s.validateAgentIgnoredParents(ctx, files, destinationWorktree, copy.relative, true); err != nil {
			return agentIgnoredCopyError(copy.relative, copied, fmt.Errorf("destination parent: %w", err))
		}
		if err := s.writeAgentIgnoredDestination(ctx, copy.destination, content, copy.mode); err != nil {
			return agentIgnoredCopyError(copy.relative, copied, err)
		}
		copied = append(copied, copy.relative)
	}
	return nil
}

func validAgentIgnoredRelativePath(value string) (string, error) {
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf("ignored-file path is empty or contains NUL")
	}
	// Repository paths use slash on every host. Reject backslash and drive
	// forms rather than giving one string different meanings on SSH/Windows.
	if strings.Contains(value, `\`) || pathpkg.IsAbs(value) || filepath.IsAbs(value) ||
		len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' {
		return "", fmt.Errorf("ignored-file path %q is absolute or ambiguous", value)
	}
	if pathpkg.Clean(value) != value || value == "." {
		return "", fmt.Errorf("ignored-file path %q is not a canonical relative path", value)
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || component == "." || component == ".." {
			return "", fmt.Errorf("ignored-file path %q contains an unsafe component", value)
		}
		if strings.EqualFold(component, ".git") {
			return "", fmt.Errorf("ignored-file path %q contains .git", value)
		}
	}
	return value, nil
}

func validateAgentIgnoredRoot(files sessionFiles, root, kind string) error {
	info, err := files.Lstat(root)
	if err != nil {
		return fmt.Errorf("copy agent ignored files: inspect %s worktree: %w", kind, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("copy agent ignored files: %s worktree is not a real directory", kind)
	}
	return nil
}

// validateAgentIgnoredParents uses Lstat for every component. Source parents
// must exist; destination parents may be absent, but every existing one must be
// a real directory rather than a symlink or another file type.
func (s *gitSession) validateAgentIgnoredParents(ctx context.Context, files sessionFiles, root, relative string, allowMissing bool) error {
	directory := pathpkg.Dir(relative)
	if directory == "." {
		return nil
	}
	current := root
	for _, component := range strings.Split(directory, "/") {
		if err := ctx.Err(); err != nil {
			return err
		}
		current = s.joinPath(current, component)
		info, err := files.Lstat(current)
		if os.IsNotExist(err) && allowMissing {
			return nil
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("parent %q is not a real directory", current)
		}
	}
	return nil
}

func (s *gitSession) checkAgentIgnored(ctx context.Context, sourceWorktree, relative string) error {
	var err error
	if s.sshClient != nil {
		_, err = s.sshClient.Run(ctx, sourceWorktree, "git", "check-ignore", "--quiet", "--", relative)
	} else {
		command := exec.CommandContext(ctx, "git", "-C", sourceWorktree, "check-ignore", "--quiet", "--", relative)
		err = command.Run()
	}
	if err == nil {
		return ctx.Err()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if code, ok := commandExitCode(err); ok && code == 1 {
		return fmt.Errorf("copy agent ignored file %q: source is not Git-ignored", relative)
	}
	return fmt.Errorf("copy agent ignored file %q: check Git ignore status: %w", relative, err)
}

func readAgentIgnoredSource(ctx context.Context, files sessionFiles, name string) ([]byte, error) {
	reader, err := files.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open source: %w", err)
	}
	content, readErr := io.ReadAll(io.LimitReader(reader, maxAgentIgnoredFileSize+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read source: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close source: %w", closeErr)
	}
	if len(content) > maxAgentIgnoredFileSize {
		return nil, fmt.Errorf("source exceeds the %d-byte limit", maxAgentIgnoredFileSize)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

func agentIgnoredCopyError(relative string, copied []string, err error) error {
	if len(copied) == 0 {
		return fmt.Errorf("copy agent ignored file %q (no files copied): %w", relative, err)
	}
	return fmt.Errorf("copy agent ignored file %q (already copied %q): %w", relative, copied, err)
}

func (s *gitSession) writeAgentIgnoredDestination(ctx context.Context, name string, content []byte, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	permissions := mode.Perm()
	if s.sshClient != nil {
		// POSIX noclobber reserves a previously absent file without following
		// or replacing an existing one, including a dangling symlink. Only
		// install the cleanup trap after that reservation succeeds.
		const script = `set -e
set -C
umask 077
: > "$1"
trap 'rm -f -- "$1"' 0 1 2 15
cat >> "$1"
chmod "$2" "$1"
trap - 0 1 2 15
`
		result, err := s.sshClient.RunStreaming(ctx, sshworkspace.ExecOptions{
			Stdin: bytes.NewReader(content),
		}, "sh", "-c", script, "tangit-copy", name, fmt.Sprintf("%04o", permissions))
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("create destination exclusively on SSH host (status %d): %w", result.ExitCode, err)
		}
		return nil
	}

	// WriteFileAtomic intentionally replaces its target. Agent files instead
	// require exclusive creation so a late-arriving destination is never lost.
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create destination exclusively: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(name)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}
	if err := file.Chmod(permissions); err != nil {
		return fmt.Errorf("preserve destination permissions: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}
	complete = true
	return nil
}
