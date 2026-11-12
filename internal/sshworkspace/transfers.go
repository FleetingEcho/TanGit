package sshworkspace

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// TransferProgress reports cumulative remote bytes and completed regular files.
// Current is the slash-separated remote path associated with the latest update.
// Progress callbacks run synchronously on the transferring goroutine.
type TransferProgress struct {
	Bytes   int64
	Items   int64
	Current string
}

// ProgressFunc observes transfer progress without crossing goroutines.
type ProgressFunc func(TransferProgress)

type transferState struct {
	ctx      context.Context
	progress ProgressFunc
	value    TransferProgress
}

// UploadFile streams a local regular file to a remote POSIX path. permission
// zero preserves the local permission bits. Cancellation closes the opened
// files before returning. A truncated/partial remote destination may remain on
// failure; callers needing replacement guarantees should upload to a temporary
// remote path and rename it themselves.
func (c *Client) UploadFile(ctx context.Context, localPath, remotePath string, permission os.FileMode, progress ProgressFunc) error {
	state := transferState{ctx: ctx, progress: progress}
	return c.uploadFile(&state, localPath, remotePath, permission)
}

// UploadFolder recursively streams a local directory to a remote POSIX path.
// It creates remote directories, transfers regular files only, and rejects
// symbolic links and special files instead of following or silently skipping
// them. Partial destinations may remain on failure or cancellation.
func (c *Client) UploadFolder(ctx context.Context, localRoot, remoteRoot string, progress ProgressFunc) error {
	if err := c.ready(); err != nil {
		return err
	}
	rootInfo, err := os.Stat(localRoot)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("upload source %q is not a directory", localRoot)
	}
	cleanedRoot, err := cleanRemotePath(remoteRoot)
	if err != nil {
		return err
	}
	if err := c.sftp.MkdirAll(cleanedRoot); err != nil {
		return fmt.Errorf("create remote upload root: %w", err)
	}
	state := transferState{ctx: ctx, progress: progress}
	return filepath.WalkDir(localRoot, func(localPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(localRoot, localPath)
		if err != nil {
			return err
		}
		remotePath := cleanedRoot
		if relative != "." {
			remotePath = path.Join(cleanedRoot, filepath.ToSlash(relative))
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			if err := c.sftp.MkdirAll(remotePath); err != nil {
				return fmt.Errorf("create remote directory %q: %w", remotePath, err)
			}
			return nil
		case !info.Mode().IsRegular():
			return fmt.Errorf("upload source %q is not a regular file or directory", localPath)
		default:
			return c.uploadFile(&state, localPath, remotePath, info.Mode().Perm())
		}
	})
}

func (c *Client) uploadFile(state *transferState, localPath, remotePath string, permission os.FileMode) error {
	if err := state.ctx.Err(); err != nil {
		return err
	}
	local, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local upload source: %w", err)
	}
	info, statErr := local.Stat()
	if statErr != nil {
		_ = local.Close()
		return statErr
	}
	if !info.Mode().IsRegular() {
		_ = local.Close()
		return fmt.Errorf("upload source %q is not a regular file", localPath)
	}
	if permission == 0 {
		permission = info.Mode().Perm()
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		_ = local.Close()
		return err
	}
	remote, err := c.OpenWriter(cleaned, permission)
	if err != nil {
		_ = local.Close()
		return fmt.Errorf("open remote upload destination: %w", err)
	}
	stopCancellation := context.AfterFunc(state.ctx, func() {
		_ = remote.Close()
		_ = local.Close()
	})
	copyErr := state.copy(remote, local, cleaned)
	stopCancellation()
	remoteCloseErr := remote.Close()
	localCloseErr := local.Close()
	if ctxErr := state.ctx.Err(); ctxErr != nil {
		copyErr = ctxErr
	}
	if err := errors.Join(copyErr, remoteCloseErr, localCloseErr); err != nil {
		return fmt.Errorf("upload %q to %q: %w", localPath, cleaned, err)
	}
	state.item(cleaned)
	return nil
}

// DownloadFile streams one remote regular file to a local path. permission zero
// preserves remote permission bits. The caller is responsible for removing a
// partial local file after failure or cancellation.
func (c *Client) DownloadFile(ctx context.Context, remotePath, localPath string, permission os.FileMode, progress ProgressFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	remote, err := c.OpenReader(remotePath)
	if err != nil {
		return err
	}
	cleaned := path.Clean(remotePath)
	info, statErr := remote.Stat()
	if statErr != nil {
		_ = remote.Close()
		return statErr
	}
	if !info.Mode().IsRegular() {
		_ = remote.Close()
		return fmt.Errorf("download source %q is not a regular file", cleaned)
	}
	if permission == 0 {
		permission = info.Mode().Perm()
	}
	local, err := os.OpenFile(localPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, permission.Perm())
	if err != nil {
		_ = remote.Close()
		return fmt.Errorf("open local download destination: %w", err)
	}
	if err := local.Chmod(permission.Perm()); err != nil {
		_ = local.Close()
		_ = remote.Close()
		return fmt.Errorf("set local download permissions: %w", err)
	}
	state := transferState{ctx: ctx, progress: progress}
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = local.Close()
		_ = remote.Close()
	})
	copyErr := state.copy(local, remote, cleaned)
	stopCancellation()
	localCloseErr := local.Close()
	remoteCloseErr := remote.Close()
	if ctxErr := ctx.Err(); ctxErr != nil {
		copyErr = ctxErr
	}
	if err := errors.Join(copyErr, localCloseErr, remoteCloseErr); err != nil {
		return fmt.Errorf("download %q to %q: %w", cleaned, localPath, err)
	}
	state.item(cleaned)
	return nil
}

// DownloadFolderZIP recursively enumerates remoteDir through SFTP and streams
// regular files into a caller-owned local ZIP writer. It neither creates a
// server-side archive nor loads the tree into memory. Archive names contain the
// directory contents relative to remoteDir and always use '/'. Empty directory
// entries are included; symlinks and special files are rejected.
//
// The caller must close zipWriter and its underlying output. On failure or
// cancellation, the caller is responsible for closing/removing any temporary
// local output; DownloadFolderZIP deliberately does not own either resource.
func (c *Client) DownloadFolderZIP(ctx context.Context, remoteDir string, zipWriter *zip.Writer, progress ProgressFunc) error {
	if zipWriter == nil {
		return errors.New("ZIP writer is nil")
	}
	cleanedRoot, err := cleanRemotePath(remoteDir)
	if err != nil {
		return err
	}
	info, err := c.Stat(cleanedRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("download source %q is not a directory", cleanedRoot)
	}
	state := transferState{ctx: ctx, progress: progress}
	return c.downloadZIPDirectory(&state, zipWriter, cleanedRoot, "")
}

func (c *Client) downloadZIPDirectory(state *transferState, zipWriter *zip.Writer, remoteDir, relativeDir string) error {
	if err := state.ctx.Err(); err != nil {
		return err
	}
	entries, err := c.sftp.ReadDirContext(state.ctx, remoteDir)
	if err != nil {
		return fmt.Errorf("read remote directory %q: %w", remoteDir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	if len(entries) == 0 && relativeDir != "" {
		header := &zip.FileHeader{Name: strings.TrimSuffix(relativeDir, "/") + "/", Method: zip.Store}
		if _, err := zipWriter.CreateHeader(header); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if err := state.ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if name == "" || name == "." || name == ".." || path.Base(name) != name || strings.ContainsRune(name, '\x00') {
			return fmt.Errorf("remote directory returned unsafe entry name %q", name)
		}
		remotePath := path.Join(remoteDir, name)
		relativePath := path.Join(relativeDir, name)
		switch {
		case entry.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("remote ZIP source %q is a symbolic link", remotePath)
		case entry.IsDir():
			if err := c.downloadZIPDirectory(state, zipWriter, remotePath, relativePath); err != nil {
				return err
			}
		case !entry.Mode().IsRegular():
			return fmt.Errorf("remote ZIP source %q is not a regular file", remotePath)
		default:
			if err := c.downloadZIPFile(state, zipWriter, remotePath, relativePath, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) downloadZIPFile(state *transferState, zipWriter *zip.Writer, remotePath, relativePath string, info os.FileInfo) error {
	remote, err := c.OpenReader(remotePath)
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		_ = remote.Close()
		return err
	}
	header.Name = relativePath
	header.Method = zip.Deflate
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		_ = remote.Close()
		return fmt.Errorf("create ZIP entry %q: %w", relativePath, err)
	}
	stopCancellation := context.AfterFunc(state.ctx, func() { _ = remote.Close() })
	copyErr := state.copy(writer, remote, remotePath)
	stopCancellation()
	closeErr := remote.Close()
	if ctxErr := state.ctx.Err(); ctxErr != nil {
		copyErr = ctxErr
	}
	if err := errors.Join(copyErr, closeErr); err != nil {
		return fmt.Errorf("download ZIP entry %q: %w", remotePath, err)
	}
	state.item(remotePath)
	return nil
}

func (s *transferState) copy(destination io.Writer, source io.Reader, remotePath string) error {
	buffer := make([]byte, 128*1024)
	for {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			written, writeErr := destination.Write(buffer[:n])
			if written > 0 {
				s.value.Bytes += int64(written)
				s.value.Current = remotePath
				s.notify()
			}
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
		if n == 0 {
			// Avoid spinning forever on a broken Reader while permitting the
			// occasional legal zero-length read.
			if err := s.ctx.Err(); err != nil {
				return err
			}
		}
	}
}

func (s *transferState) item(remotePath string) {
	s.value.Items++
	s.value.Current = remotePath
	s.notify()
}

func (s *transferState) notify() {
	if s.progress != nil {
		s.progress(s.value)
	}
}
