package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/sshworkspace"
)

// sshClientSession is deliberately a local assertion. Local repository
// sessions do not need to know about SSH, while a remote session owns exactly
// the client captured for each background operation.
type sshClientSession interface {
	SSHClient() *sshworkspace.Client
}

// remoteWorkspaceClient is the subset used by the Editor. Keeping the small
// interface here also makes POSIX path and versioning tests independent of a
// live SSH server. Lstat is asserted separately below until it is exposed by
// sshworkspace.Client; never substitute Stat, which follows links.
type remoteWorkspaceClient interface {
	Run(context.Context, string, string, ...string) (sshworkspace.ExecResult, error)
	RunStreaming(context.Context, sshworkspace.ExecOptions, string, ...string) (sshworkspace.ExecResult, error)
	ReadFile(string) ([]byte, error)
	ReadDir(string) ([]os.FileInfo, error)
	WriteFileAtomic(string, []byte, os.FileMode) error
	Remove(string) error
	RemoveAll(string) error
	Rename(string, string) error
}

type remoteWorkspaceLstat interface {
	Lstat(string) (os.FileInfo, error)
}

func remoteWorkspaceClientFor(session repository.Session, remote bool) (remoteWorkspaceClient, error) {
	if !remote {
		return nil, nil
	}
	provider, ok := session.(sshClientSession)
	if !ok || provider == nil {
		return nil, errors.New("remote repository session does not provide an SSH client")
	}
	client := provider.SSHClient()
	if client == nil {
		return nil, errors.New("remote repository SSH client is not connected")
	}
	return client, nil
}

func (a *app) remoteWorkspaceClient() (remoteWorkspaceClient, error) {
	return remoteWorkspaceClientFor(a.session, a.location.IsRemote())
}

func remoteLstat(client remoteWorkspaceClient, filename string) (os.FileInfo, error) {
	provider, ok := client.(remoteWorkspaceLstat)
	if !ok {
		return nil, errors.New("SSH workspace does not expose SFTP Lstat; refusing to follow symbolic links")
	}
	return provider.Lstat(filename)
}

func remoteWorkspacePath(root, relative string) (string, error) {
	if root == "" || !path.IsAbs(root) || strings.ContainsRune(root, '\x00') {
		return "", errors.New("invalid remote repository root")
	}
	if !validCodeManagedPath(relative) {
		return "", fmt.Errorf("invalid remote repository path %q", relative)
	}
	return path.Join(root, relative), nil
}

func remoteCheckParentDirectories(client remoteWorkspaceClient, root, name string) error {
	if !validCodeManagedPath(name) {
		return fmt.Errorf("invalid remote repository path %q", name)
	}
	parts := strings.Split(name, "/")
	for index := 1; index < len(parts); index++ {
		parent := strings.Join(parts[:index], "/")
		filename, _ := remoteWorkspacePath(root, parent)
		info, err := remoteLstat(client, filename)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%q is not a real directory inside the remote repository", parent)
		}
	}
	return nil
}

func remoteRegularFile(client remoteWorkspaceClient, root, name string) (string, os.FileInfo, error) {
	filename, err := remoteWorkspacePath(root, name)
	if err != nil {
		return "", nil, err
	}
	if err := remoteCheckParentDirectories(client, root, name); err != nil {
		return "", nil, err
	}
	info, err := remoteLstat(client, filename)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("%q is not a regular remote file", name)
	}
	return filename, info, nil
}

func parseRemoteGitNames(output []byte) []string {
	seen := make(map[string]bool)
	var names []string
	for _, name := range strings.Split(string(output), "\x00") {
		name = strings.TrimSuffix(name, "/")
		if name == "" || !validCodeRelativePath(name) || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func listRemoteCodeFiles(ctx context.Context, client remoteWorkspaceClient, root string) ([]string, error) {
	result, err := client.Run(ctx, root, "git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	names := parseRemoteGitNames(result.Output)
	paths := make([]string, 0, len(names))
	for _, name := range names {
		if _, _, statErr := remoteRegularFile(client, root, name); statErr == nil {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func listRemoteCodeEmptyDirectories(ctx context.Context, client remoteWorkspaceClient, root string) []string {
	result, err := client.Run(ctx, root, "git", "ls-files", "-z", "--others", "--exclude-standard", "--directory")
	if err != nil {
		return nil
	}
	var directories []string
	for _, name := range parseRemoteGitNames(result.Output) {
		filename, pathErr := remoteWorkspacePath(root, name)
		if pathErr != nil {
			continue
		}
		info, statErr := remoteLstat(client, filename)
		if statErr != nil || !info.IsDir() {
			continue
		}
		entries, readErr := client.ReadDir(filename)
		if readErr == nil && len(entries) == 0 {
			directories = append(directories, name)
		}
	}
	sort.Strings(directories)
	return directories
}

type remoteFileStamp struct {
	size    int64
	modTime int64
	mode    os.FileMode
}

func stampRemoteFile(info os.FileInfo) remoteFileStamp {
	return remoteFileStamp{size: info.Size(), modTime: info.ModTime().UnixNano(), mode: info.Mode()}
}

func sameRemoteFileStamp(left, right remoteFileStamp) bool {
	return left.size == right.size && left.modTime == right.modTime && left.mode == right.mode
}

var errRemoteFileModified = errors.New("file was modified remotely; reopen it before saving")

func readRemoteWorkingFile(client remoteWorkspaceClient, root, name string) ([]byte, remoteFileStamp, error) {
	filename, before, err := remoteRegularFile(client, root, name)
	if err != nil {
		return nil, remoteFileStamp{}, err
	}
	content, err := client.ReadFile(filename)
	if err != nil {
		return nil, remoteFileStamp{}, err
	}
	_, after, err := remoteRegularFile(client, root, name)
	if err != nil {
		return nil, remoteFileStamp{}, err
	}
	beforeStamp, afterStamp := stampRemoteFile(before), stampRemoteFile(after)
	if !sameRemoteFileStamp(beforeStamp, afterStamp) || int64(len(content)) != afterStamp.size {
		return nil, remoteFileStamp{}, errRemoteFileModified
	}
	return content, afterStamp, nil
}

func writeRemoteWorkingFile(client remoteWorkspaceClient, root, name string, content []byte, previous remoteFileStamp, previousKnown bool) (remoteFileStamp, error) {
	filename, current, err := remoteRegularFile(client, root, name)
	if err != nil {
		return remoteFileStamp{}, err
	}
	currentStamp := stampRemoteFile(current)
	if !previousKnown || !sameRemoteFileStamp(previous, currentStamp) {
		return remoteFileStamp{}, errRemoteFileModified
	}
	permission := current.Mode().Perm()
	if permission == 0 {
		permission = 0o644
	}
	if err := client.WriteFileAtomic(filename, content, permission); err != nil {
		return remoteFileStamp{}, err
	}
	_, after, err := remoteRegularFile(client, root, name)
	if err != nil {
		return remoteFileStamp{}, err
	}
	stamp := stampRemoteFile(after)
	if int64(len(content)) != stamp.size {
		return remoteFileStamp{}, errRemoteFileModified
	}
	return stamp, nil
}

func performRemoteCodeFileOperation(ctx context.Context, client remoteWorkspaceClient, root string, kind codeFileOperationKind, source, destination string) error {
	if !validCodeManagedPath(destination) {
		return fmt.Errorf("invalid destination path %q", destination)
	}
	switch kind {
	case codeFileCreateFile, codeFileCreateFolder:
		if source != "" {
			return fmt.Errorf("create operation has source path %q", source)
		}
	case codeFileRename, codeFileMove, codeFileDelete:
		if !validCodeManagedPath(source) {
			return fmt.Errorf("invalid source path %q", source)
		}
	default:
		return fmt.Errorf("unknown code file operation %d", kind)
	}
	if source != "" {
		if err := remoteCheckParentDirectories(client, root, source); err != nil {
			return err
		}
	}
	if err := remoteCheckParentDirectories(client, root, destination); err != nil {
		return err
	}
	destinationPath, _ := remoteWorkspacePath(root, destination)
	_, destinationErr := remoteLstat(client, destinationPath)
	destinationExists := destinationErr == nil
	if destinationErr != nil && !errors.Is(destinationErr, fs.ErrNotExist) && !os.IsNotExist(destinationErr) {
		return destinationErr
	}

	switch kind {
	case codeFileCreateFile:
		if destinationExists {
			return fmt.Errorf("destination %q already exists", destination)
		}
		return createRemoteCodeFile(ctx, client, root, destinationPath)
	case codeFileCreateFolder:
		if destinationExists {
			return fmt.Errorf("destination %q already exists", destination)
		}
		_, err := client.Run(ctx, root, "mkdir", "-m", "0755", destinationPath)
		return err
	case codeFileRename, codeFileMove:
		sourcePath, _ := remoteWorkspacePath(root, source)
		info, err := remoteLstat(client, sourcePath)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("%q is not a regular remote file or directory", source)
		}
		if destinationExists {
			return fmt.Errorf("destination %q already exists", destination)
		}
		if kind == codeFileMove && info.IsDir() && codePathWithin(destination, source) {
			return fmt.Errorf("cannot move directory %q into itself", source)
		}
		return client.Rename(sourcePath, destinationPath)
	case codeFileDelete:
		sourcePath, _ := remoteWorkspacePath(root, source)
		info, err := remoteLstat(client, sourcePath)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("%q is not a regular remote file or directory", source)
		}
		if info.IsDir() {
			return client.RemoveAll(sourcePath)
		}
		return client.Remove(sourcePath)
	default:
		panic("unreachable remote code file operation")
	}
}

// createRemoteCodeFile links a uniquely named temporary regular file into its
// final location. POSIX rename would clobber an existing destination; hard
// link creation is instead atomic and fails when the destination exists.
func createRemoteCodeFile(ctx context.Context, client remoteWorkspaceClient, root, destination string) error {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := path.Join(path.Dir(destination), "."+path.Base(destination)+".create-"+hex.EncodeToString(random[:])+".tmp")
	if err := client.WriteFileAtomic(temporary, nil, 0o644); err != nil {
		return err
	}
	defer client.Remove(temporary)
	if _, err := client.Run(ctx, root, "ln", temporary, destination); err != nil {
		return err
	}
	return nil
}

// remoteEditorState is shared by the remote document and in-pane editors. It
// performs only the UI-independent buffer decoding, leaving each caller's local
// loading path completely unchanged.
func remoteEditorState(name string, content []byte) *editor.State {
	buffer := editor.NewBuffer(string(content))
	buffer.SetLanguage(editor.LanguageFor(name))
	return &editor.State{Buffer: buffer, SoftWrap: true, LineNumbers: editor.LineNumbersOn, Label: name}
}
