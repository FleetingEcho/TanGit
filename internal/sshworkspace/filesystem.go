package sshworkspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
)

// All filesystem paths below use remote POSIX semantics. They are passed to
// path.Clean, never local filepath.Clean. Relative paths retain SFTP's remote
// home-directory semantics; use ResolvePath when an absolute path is required.

// Stat returns remote file metadata, following symbolic links.
func (c *Client) Stat(remotePath string) (os.FileInfo, error) {
	if c == nil || c.sftp == nil {
		return nil, errors.New("SFTP client is nil or uninitialized")
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	return c.sftp.Stat(cleaned)
}

// Lstat returns remote file metadata without following symbolic links.
func (c *Client) Lstat(remotePath string) (os.FileInfo, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	return c.sftp.Lstat(cleaned)
}

// Exists distinguishes nonexistence from other metadata errors.
func (c *Client) Exists(remotePath string) (bool, error) {
	_, err := c.Stat(remotePath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// ReadDir reads a remote directory.
func (c *Client) ReadDir(remotePath string) ([]os.FileInfo, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	return c.sftp.ReadDir(cleaned)
}

// ReadFile reads a complete remote regular file.
func (c *Client) ReadFile(remotePath string) ([]byte, error) {
	file, err := c.OpenReader(remotePath)
	if err != nil {
		return nil, err
	}
	contents, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read remote file %q: %w", remotePath, err)
	}
	return contents, nil
}

// WriteFileAtomic writes data to a unique temporary file in the destination
// directory, closes it, and atomically renames it over remotePath using the
// OpenSSH posix-rename extension. Temporary files are removed on failure.
// Permission zero preserves an existing file's mode and uses 0644 for a new one.
func (c *Client) WriteFileAtomic(remotePath string, data []byte, permission os.FileMode) error {
	if err := c.ready(); err != nil {
		return err
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return err
	}
	if permission == 0 {
		permission = 0o644
		if info, statErr := c.sftp.Stat(cleaned); statErr == nil {
			permission = info.Mode().Perm()
		}
	}
	directory := path.Dir(cleaned)
	base := path.Base(cleaned)
	if base == "." || base == "/" || cleaned == "/" {
		return fmt.Errorf("invalid atomic destination %q", remotePath)
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		suffix, randomErr := randomSuffix()
		if randomErr != nil {
			return randomErr
		}
		temporary := path.Join(directory, "."+base+".tmp-"+suffix)
		file, openErr := c.sftp.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
		if openErr != nil {
			lastErr = openErr
			if os.IsExist(openErr) {
				continue
			}
			return fmt.Errorf("create remote temporary file: %w", openErr)
		}
		writeErr := writeAll(file, data)
		chmodErr := file.Chmod(permission.Perm())
		closeErr := file.Close()
		if operationErr := errors.Join(writeErr, chmodErr, closeErr); operationErr != nil {
			_ = c.sftp.Remove(temporary)
			return fmt.Errorf("write remote temporary file: %w", operationErr)
		}
		if renameErr := c.sftp.PosixRename(temporary, cleaned); renameErr != nil {
			_ = c.sftp.Remove(temporary)
			return fmt.Errorf("atomically replace remote file %q: %w", cleaned, renameErr)
		}
		return nil
	}
	return fmt.Errorf("create unique remote temporary file: %w", lastErr)
}

// MkdirAll creates a remote directory and missing parents.
func (c *Client) MkdirAll(remotePath string) error {
	if err := c.ready(); err != nil {
		return err
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return err
	}
	return c.sftp.MkdirAll(cleaned)
}

// Remove removes a remote file or empty directory.
func (c *Client) Remove(remotePath string) error {
	if err := c.ready(); err != nil {
		return err
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return err
	}
	return c.sftp.Remove(cleaned)
}

// RemoveAll recursively removes a remote tree. Like os.RemoveAll, a missing
// path is not an error. Callers should resolve arbitrary user input before
// invoking this destructive operation.
func (c *Client) RemoveAll(remotePath string) error {
	if err := c.ready(); err != nil {
		return err
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return err
	}
	return c.sftp.RemoveAll(cleaned)
}

// Rename renames using the base SFTP protocol. For atomic overwrite semantics,
// use WriteFileAtomic or the server's posix-rename support.
func (c *Client) Rename(oldRemotePath, newRemotePath string) error {
	if err := c.ready(); err != nil {
		return err
	}
	oldCleaned, err := cleanRemotePath(oldRemotePath)
	if err != nil {
		return err
	}
	newCleaned, err := cleanRemotePath(newRemotePath)
	if err != nil {
		return err
	}
	return c.sftp.Rename(oldCleaned, newCleaned)
}

// RenameAtomically replaces the destination using OpenSSH's posix-rename
// extension. Unlike Rename, it is suitable for committing a staged upload over
// an existing destination without first deleting that destination.
func (c *Client) RenameAtomically(oldRemotePath, newRemotePath string) error {
	if err := c.ready(); err != nil {
		return err
	}
	oldCleaned, err := cleanRemotePath(oldRemotePath)
	if err != nil {
		return err
	}
	newCleaned, err := cleanRemotePath(newRemotePath)
	if err != nil {
		return err
	}
	return c.sftp.PosixRename(oldCleaned, newCleaned)
}

// OpenReader opens a remote file for reading. The caller owns the returned file.
func (c *Client) OpenReader(remotePath string) (*sftp.File, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	return c.sftp.Open(cleaned)
}

// OpenWriter creates or truncates a remote file and applies permission. The
// caller owns the returned file. Partial output remains if writing fails.
func (c *Client) OpenWriter(remotePath string, permission os.FileMode) (*sftp.File, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	cleaned, err := cleanRemotePath(remotePath)
	if err != nil {
		return nil, err
	}
	file, err := c.sftp.OpenFile(cleaned, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(permission.Perm()); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func (c *Client) ready() error {
	if c == nil || c.sftp == nil {
		return errors.New("SFTP client is nil or uninitialized")
	}
	return nil
}

func cleanRemotePath(remotePath string) (string, error) {
	if remotePath == "" {
		return "", errors.New("remote path is empty")
	}
	if strings.ContainsRune(remotePath, '\x00') {
		return "", errors.New("remote path contains NUL")
	}
	return path.Clean(remotePath), nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) != 0 {
		n, err := writer.Write(data)
		if n > 0 {
			data = data[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func randomSuffix() (string, error) {
	var value [12]byte
	if _, err := io.ReadFull(rand.Reader, value[:]); err != nil {
		return "", fmt.Errorf("generate remote temporary filename: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
