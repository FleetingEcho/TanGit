package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jesseduffield/lazygit/pkg/commands"
	"github.com/jesseduffield/lazygit/pkg/commands/git_commands"
	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
	"github.com/jesseduffield/lazygit/pkg/common"
	"github.com/jesseduffield/lazygit/pkg/config"
	"github.com/jesseduffield/lazygit/pkg/utils"

	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
)

// SSHWorkspaceOpener opens repositories directly on a connected Linux host.
// It never mirrors or executes files from the remote repository on the GUI
// machine. Both the package function and Opener method are provided so app
// integration can use whichever shape matches its opener interface.
type SSHWorkspaceOpener struct{}

// OpenSSHWorkspace associates a session with one already-connected client.
//
// If opening fails, owner (when non-nil) is closed exactly once. Close on the
// returned session releases owner once; a nil owner deliberately leaves the
// shared client open. Workspace and SSHClient preserve the remote identity for
// the editor and connection-pool integration. The returned Session also
// implements ProgressiveSession and io.Closer.
func OpenSSHWorkspace(ctx context.Context, client *sshworkspace.Client, location workspace.Location, owner io.Closer) (session Session, err error) {
	closed := false
	closeOwner := func() {
		if owner != nil && !closed {
			closed = true
			_ = owner.Close()
		}
	}
	defer func() {
		if session == nil {
			closeOwner()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("open SSH workspace: SSH client is nil")
	}
	if location.Kind != workspace.SSH || !location.Valid() {
		return nil, errors.New("open SSH workspace: location is not a valid SSH workspace")
	}
	info, err := client.Stat(location.Root)
	if err != nil {
		return nil, fmt.Errorf("open SSH workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("open SSH workspace: %s is not a folder", location.Root)
	}

	if _, managed, err := client.GitRoot(ctx, location.Root); err != nil {
		return nil, err
	} else if !managed {
		return nil, ErrNotRepository
	}

	cmn := common.NewDummyCommon()
	appConfig := config.NewDummyAppConfig()
	commandLog := NewCommandLog()
	guiIO := oscommands.NewGuiIO(
		cmn.Log,
		commandLog.logCommand,
		commandLog.newWriter,
		func(oscommands.CredentialType) <-chan string { return nil },
	)
	osCommand := oscommands.NewOSCommand(cmn, appConfig, &oscommands.Platform{OS: "linux", Shell: "sh", ShellArg: "-c"}, guiIO)
	runner := &sshCmdObjRunner{
		client: client, log: cmn.Log,
		logCmd: commandLog.logCommand, newWriter: commandLog.newWriter,
	}
	osCommand.Cmd = osCommand.Cmd.CloneWithNewRunner(func(oscommands.ICmdObjRunner) oscommands.ICmdObjRunner {
		return runner
	})
	version, err := git_commands.GetGitVersion(osCommand)
	if err != nil {
		return nil, fmt.Errorf("read remote Git version: %w", err)
	}
	gitConfig := &remoteGitConfig{client: client}
	diffConfig := config.NewDiffRendererConfigManager(cmn.UserConfig)
	git, err := newGitCommand(location.Root, cmn, version, osCommand, gitConfig, diffConfig)
	if err != nil {
		if exit, ok := commandExitCode(err); ok && exit == 128 {
			return nil, ErrNotRepository
		}
		return nil, fmt.Errorf("open SSH workspace: %w", err)
	}
	repoPaths := git.RepoPaths
	runner.repositoryEnvironment = make(map[string]string)
	for _, entry := range repoPaths.GitLocationEnvVars() {
		if name, value, ok := strings.Cut(entry, "="); ok && (name == "GIT_DIR" || name == "GIT_WORK_TREE") {
			runner.repositoryEnvironment[name] = value
		}
	}
	cmd := commands.NewGitCmdObjBuilder(cmn.Log, osCommand.Cmd, repoPaths.WorktreePath(), repoPaths.GitLocationEnvVars())
	commitCommon := common.NewDummyCommon()
	commitCommon.UserConfig().Git.MainBranches = []string{}
	result := &gitSession{
		git: git, commandLog: commandLog, cmd: cmd,
		mainBranches:              git_commands.NewMainBranches(cmn, cmd),
		commitMainBranches:        git_commands.NewMainBranches(commitCommon, cmd),
		hashPool:                  &utils.StringPool{},
		renameSimilarityThreshold: cmn.UserConfig().Git.RenameSimilarityThreshold,
		sshClient:                 client, remote: runner, location: location, owner: owner,
	}
	result.location.Root = repoPaths.WorktreePath()
	result.files = &sshSessionFiles{
		client:      client,
		scratchRoot: path.Join(repoPaths.WorktreeGitDirPath(), "tangit", "tmp"),
	}
	session = result
	return result, nil
}

func (SSHWorkspaceOpener) OpenSSHWorkspace(ctx context.Context, client *sshworkspace.Client, location workspace.Location, owner io.Closer) (Session, error) {
	return OpenSSHWorkspace(ctx, client, location, owner)
}

func (Opener) OpenSSHWorkspace(ctx context.Context, client *sshworkspace.Client, location workspace.Location, owner io.Closer) (Session, error) {
	return OpenSSHWorkspace(ctx, client, location, owner)
}

func (s *gitSession) SSHClient() *sshworkspace.Client { return s.sshClient }
func (s *gitSession) Workspace() workspace.Location {
	if s.location.Valid() {
		return s.location
	}
	if s.git == nil || s.git.RepoPaths == nil {
		return workspace.Location{}
	}
	return workspace.Location{Kind: workspace.Local, Root: s.git.RepoPaths.WorktreePath()}
}

func (s *gitSession) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.owner != nil {
			s.closeErr = s.owner.Close()
		}
	})
	return s.closeErr
}

// sessionFiles gives all repository-owned file operations one local/SFTP
// boundary. The implementations deliberately use platform-native paths for a
// local session and only POSIX paths for SSH.
type sessionFiles interface {
	ReadFile(string) ([]byte, error)
	ReadDir(string) ([]os.FileInfo, error)
	Stat(string) (os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
	Readlink(string) (string, error)
	Open(string) (io.ReadCloser, error)
	WriteFileAtomic(string, []byte, os.FileMode) error
	MkdirAll(string, os.FileMode) error
	Remove(string) error
	RemoveAll(string) error
	Rename(string, string) error
	WriteTemp(string, []byte, os.FileMode) (string, error)
	OpenAppend(string, os.FileMode) (io.WriteCloser, error)
}

type localSessionFiles struct{}

func (localSessionFiles) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }
func (localSessionFiles) ReadDir(name string) ([]os.FileInfo, error) {
	entries, err := os.ReadDir(name)
	if err != nil {
		return nil, err
	}
	result := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		result = append(result, info)
	}
	return result, nil
}
func (localSessionFiles) Stat(name string) (os.FileInfo, error)   { return os.Stat(name) }
func (localSessionFiles) Lstat(name string) (os.FileInfo, error)  { return os.Lstat(name) }
func (localSessionFiles) Readlink(name string) (string, error)    { return os.Readlink(name) }
func (localSessionFiles) Open(name string) (io.ReadCloser, error) { return os.Open(name) }
func (localSessionFiles) WriteFileAtomic(name string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(name), ".tangit-write-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, name)
}
func (localSessionFiles) MkdirAll(name string, mode os.FileMode) error {
	return os.MkdirAll(name, mode)
}
func (localSessionFiles) Remove(name string) error             { return os.Remove(name) }
func (localSessionFiles) RemoveAll(name string) error          { return os.RemoveAll(name) }
func (localSessionFiles) Rename(oldName, newName string) error { return os.Rename(oldName, newName) }
func (localSessionFiles) WriteTemp(pattern string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}
func (localSessionFiles) OpenAppend(name string, mode os.FileMode) (io.WriteCloser, error) {
	return os.OpenFile(name, os.O_APPEND|os.O_RDWR|os.O_CREATE, mode)
}

type sshSessionFiles struct {
	client      *sshworkspace.Client
	scratchRoot string
}

func (f *sshSessionFiles) ReadFile(name string) ([]byte, error)       { return f.client.ReadFile(name) }
func (f *sshSessionFiles) ReadDir(name string) ([]os.FileInfo, error) { return f.client.ReadDir(name) }
func (f *sshSessionFiles) Stat(name string) (os.FileInfo, error)      { return f.client.Stat(name) }
func (f *sshSessionFiles) Lstat(name string) (os.FileInfo, error) {
	return f.client.Lstat(name)
}
func (f *sshSessionFiles) Readlink(name string) (string, error) {
	result, err := f.client.Run(context.Background(), "", "readlink", "--", name)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(result.Output), "\n"), nil
}
func (f *sshSessionFiles) Open(name string) (io.ReadCloser, error) { return f.client.OpenReader(name) }
func (f *sshSessionFiles) WriteFileAtomic(name string, data []byte, mode os.FileMode) error {
	if err := f.client.MkdirAll(path.Dir(name)); err != nil {
		return err
	}
	return f.client.WriteFileAtomic(name, data, mode)
}
func (f *sshSessionFiles) MkdirAll(name string, mode os.FileMode) error {
	if err := f.client.MkdirAll(name); err != nil {
		return err
	}
	if mode == 0 {
		return nil
	}
	_, err := f.client.Run(context.Background(), "", "chmod", fmt.Sprintf("%04o", mode.Perm()), "--", name)
	return err
}
func (f *sshSessionFiles) Remove(name string) error    { return f.client.Remove(name) }
func (f *sshSessionFiles) RemoveAll(name string) error { return f.client.RemoveAll(name) }
func (f *sshSessionFiles) Rename(oldName, newName string) error {
	return f.client.Rename(oldName, newName)
}
func (f *sshSessionFiles) WriteTemp(pattern string, data []byte, mode os.FileMode) (string, error) {
	if f.scratchRoot == "" {
		return "", errors.New("remote scratch directory is not configured")
	}
	if err := f.MkdirAll(f.scratchRoot, 0o700); err != nil {
		return "", err
	}
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	base := strings.NewReplacer("*", "", "/", "-", "\\", "-", "\x00", "").Replace(pattern)
	name := path.Join(f.scratchRoot, base+hex.EncodeToString(suffix[:]))
	if err := f.client.WriteFileAtomic(name, data, mode); err != nil {
		return "", err
	}
	return name, nil
}
func (f *sshSessionFiles) OpenAppend(name string, mode os.FileMode) (io.WriteCloser, error) {
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	umask := 0o777 &^ mode.Perm()
	go func() {
		var stderr lockedRemoteBuffer
		_, err := f.client.RunStreaming(ctx, sshworkspace.ExecOptions{
			Stdin: reader, Stderr: &stderr,
		}, "sh", "-c", fmt.Sprintf("umask %04o; cat >> %s", umask, sshworkspace.ShellQuote(name)))
		if err != nil && stderr.String() != "" {
			err = fmt.Errorf("append remote file: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		_ = reader.Close()
		done <- err
	}()
	return &remoteAppendWriter{writer: writer, cancel: cancel, done: done}, nil
}

type remoteAppendWriter struct {
	writer    *io.PipeWriter
	cancel    context.CancelFunc
	done      <-chan error
	closeOnce sync.Once
	closeErr  error
}

func (w *remoteAppendWriter) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *remoteAppendWriter) Close() error {
	w.closeOnce.Do(func() {
		w.closeErr = w.writer.Close()
		if w.closeErr != nil {
			w.cancel()
		}
		if err := <-w.done; w.closeErr == nil {
			w.closeErr = err
		}
		w.cancel()
	})
	return w.closeErr
}

func (s *gitSession) fileService() sessionFiles {
	if s.files != nil {
		return s.files
	}
	return localSessionFiles{}
}

func (s *gitSession) joinPath(elements ...string) string {
	if s.sshClient != nil {
		return path.Join(elements...)
	}
	return filepath.Join(elements...)
}

func (s *gitSession) absoluteRemoteOrLocalPath(root, value string) string {
	if value == "" {
		return root
	}
	if s.sshClient != nil {
		if path.IsAbs(value) {
			return path.Clean(value)
		}
		return path.Join(root, value)
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(root, value)
}

func (s *gitSession) temporaryFile(pattern string, data []byte, mode os.FileMode) (name string, cleanup func(), err error) {
	name, err = s.fileService().WriteTemp(pattern, data, mode)
	if err != nil {
		return "", nil, err
	}
	return name, func() { _ = s.fileService().Remove(name) }, nil
}
