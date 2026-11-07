package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
)

type remoteTestFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (i remoteTestFileInfo) Name() string       { return i.name }
func (i remoteTestFileInfo) Size() int64        { return i.size }
func (i remoteTestFileInfo) Mode() os.FileMode  { return i.mode }
func (i remoteTestFileInfo) ModTime() time.Time { return i.modTime }
func (i remoteTestFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i remoteTestFileInfo) Sys() any           { return nil }

type remoteTestClient struct {
	mu             sync.Mutex
	infos          map[string]os.FileInfo
	contents       map[string][]byte
	run            func(context.Context, string, string, ...string) (sshworkspace.ExecResult, error)
	runStreaming   func(context.Context, sshworkspace.ExecOptions, string, ...string) (sshworkspace.ExecResult, error)
	runDir         string
	runName        string
	runArgs        []string
	runEnvironment map[string]string
	writeFile      func(string, []byte, os.FileMode) error
	atomicWrites   [][]byte
	readFileCalls  int
	remove         map[string]bool
	renameOld      string
	renameNew      string
	missingMethods error
}

func newRemoteTestClient() *remoteTestClient {
	return &remoteTestClient{
		infos: map[string]os.FileInfo{}, contents: map[string][]byte{}, remove: map[string]bool{},
		missingMethods: errors.New("remote test method not implemented"),
	}
}

func (c *remoteTestClient) Run(ctx context.Context, dir, name string, args ...string) (sshworkspace.ExecResult, error) {
	if c.run != nil {
		return c.run(ctx, dir, name, args...)
	}
	return sshworkspace.ExecResult{}, c.missingMethods
}

func (c *remoteTestClient) RunWithEnv(ctx context.Context, dir string, environment map[string]string, name string, args ...string) (sshworkspace.ExecResult, error) {
	c.runDir, c.runName = dir, name
	c.runArgs = append([]string(nil), args...)
	c.runEnvironment = environment
	return sshworkspace.ExecResult{}, nil
}

func (c *remoteTestClient) RunStreaming(ctx context.Context, options sshworkspace.ExecOptions, name string, args ...string) (sshworkspace.ExecResult, error) {
	if c.runStreaming != nil {
		return c.runStreaming(ctx, options, name, args...)
	}
	return sshworkspace.ExecResult{ExitCode: 127}, c.missingMethods
}

func (c *remoteTestClient) ReadFile(name string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readFileCalls++
	content, ok := c.contents[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), content...), nil
}

func (c *remoteTestClient) ReadDir(string) ([]os.FileInfo, error) { return nil, c.missingMethods }

func (c *remoteTestClient) WriteFileAtomic(name string, data []byte, mode os.FileMode) error {
	if c.writeFile != nil {
		if err := c.writeFile(name, data, mode); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.atomicWrites = append(c.atomicWrites, append([]byte(nil), data...))
	c.contents[name] = append([]byte(nil), data...)
	c.infos[name] = remoteTestFileInfo{name: name, size: int64(len(data)), mode: mode, modTime: time.Unix(0, int64(len(c.atomicWrites)+10))}
	return nil
}

func (c *remoteTestClient) Remove(name string) error { c.remove[name] = true; return nil }
func (c *remoteTestClient) RemoveAll(name string) error {
	c.remove[name] = true
	return nil
}
func (c *remoteTestClient) Rename(oldName, newName string) error {
	c.renameOld, c.renameNew = oldName, newName
	return nil
}
func (c *remoteTestClient) Lstat(name string) (os.FileInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	info, ok := c.infos[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return info, nil
}

func remoteTestRegular(name, content string, version int64) remoteTestFileInfo {
	return remoteTestFileInfo{name: name, size: int64(len(content)), mode: 0o644, modTime: time.Unix(0, version)}
}

type sshProviderSession struct {
	repository.Session
	client *sshworkspace.Client
}

func (s sshProviderSession) SSHClient() *sshworkspace.Client { return s.client }

func TestRemoteWorkspaceClientDispatchIsScopedToRemoteSessions(t *testing.T) {
	client, err := remoteWorkspaceClientFor(sshProviderSession{}, false)
	if err != nil || client != nil {
		t.Fatalf("local dispatch returned client=%v err=%v", client, err)
	}
	if _, err := remoteWorkspaceClientFor(nil, true); err == nil {
		t.Fatal("remote session without SSHClient was accepted")
	}
	sshClient := &sshworkspace.Client{}
	client, err = remoteWorkspaceClientFor(sshProviderSession{client: sshClient}, true)
	if err != nil || client != sshClient {
		t.Fatalf("remote dispatch did not retain the actual client: %v %v", client, err)
	}
}

func TestRemoteWorkspacePathsAndTerminalLinksStayPOSIX(t *testing.T) {
	got, err := remoteWorkspacePath("/srv/work/repo", "dir/file with space.txt")
	if err != nil || got != "/srv/work/repo/dir/file with space.txt" {
		t.Fatalf("remoteWorkspacePath = %q, %v", got, err)
	}
	for _, name := range []string{"", "../outside", "/tmp/outside", `C:\outside`, ".git/config"} {
		if _, err := remoteWorkspacePath("/srv/work/repo", name); err == nil {
			t.Errorf("unsafe remote path %q accepted", name)
		}
	}
	if _, err := remoteWorkspacePath(`C:\work\repo`, "main.go"); err == nil {
		t.Fatal("local Windows root accepted as a remote POSIX root")
	}
	for _, test := range []struct {
		link, want string
		line       int
	}{
		{"dir/main.go:12:4", "dir/main.go", 12},
		{"/srv/work/repo/README.md#L7", "README.md", 7},
		{"./README.md", "README.md", 0},
	} {
		name, line, ok := remoteTerminalFileLink("/srv/work/repo", test.link)
		if !ok || name != test.want || line != test.line {
			t.Errorf("remoteTerminalFileLink(%q) = %q, %d, %v; want %q, %d", test.link, name, line, ok, test.want, test.line)
		}
	}
	for _, link := range []string{"/outside/main.go:3", "../outside.go", "https://example.test/a.go"} {
		if _, _, ok := remoteTerminalFileLink("/srv/work/repo", link); ok {
			t.Errorf("unsafe or external terminal link %q accepted", link)
		}
	}
}

func TestRemoteWorkingFileUsesLstatAndRejectsStaleWrites(t *testing.T) {
	client := newRemoteTestClient()
	name := "/srv/repo/link.txt"
	client.infos[name] = remoteTestFileInfo{name: name, mode: os.ModeSymlink | 0o777, size: 4}
	client.contents[name] = []byte("data")
	if _, _, err := readRemoteWorkingFile(client, "/srv/repo", "link.txt"); err == nil {
		t.Fatal("symbolic link was opened for editing")
	}
	if client.readFileCalls != 0 {
		t.Fatal("symbolic link target was read")
	}

	name = "/srv/repo/note.txt"
	client.infos[name] = remoteTestRegular(name, "old", 1)
	client.contents[name] = []byte("old")
	_, stamp, err := readRemoteWorkingFile(client, "/srv/repo", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	client.infos[name] = remoteTestRegular(name, "remote", 2)
	client.contents[name] = []byte("remote")
	if _, err := writeRemoteWorkingFile(client, "/srv/repo", "note.txt", []byte("local"), stamp, true); !errors.Is(err, errRemoteFileModified) {
		t.Fatalf("stale save error = %v", err)
	}
	if len(client.atomicWrites) != 0 {
		t.Fatal("stale save reached atomic replacement")
	}
}

func TestRemoteSearchUsesRemoteRipgrepAndFSSeamIndependently(t *testing.T) {
	client := newRemoteTestClient()
	filename := "/srv/repo/main.go"
	client.infos[filename] = remoteTestRegular(filename, "package main\n// needle\n", 1)
	client.contents[filename] = []byte("package main\n// needle\n")
	client.runStreaming = func(_ context.Context, options sshworkspace.ExecOptions, name string, args ...string) (sshworkspace.ExecResult, error) {
		if name != "rg" || options.Dir != "/srv/repo" {
			t.Fatalf("remote rg command = %q in %q", name, options.Dir)
		}
		_, _ = io.WriteString(options.Stdout, `{"type":"match","data":{"path":{"text":"main.go"},"lines":{"text":"// needle\n"},"line_number":2,"submatches":[{"start":3}]}}`+"\n")
		_, _ = io.WriteString(options.Stdout, `{"type":"end","data":{"path":{"text":"main.go"}}}`+"\n")
		return sshworkspace.ExecResult{}, nil
	}
	matches, backend, err := searchRemoteWorkspaceFiles(context.Background(), client, "/srv/repo", []string{"main.go"}, workspaceSearchOptions{Query: "needle"}, 20)
	if err != nil || backend != workspaceSearchBackendRipgrep || len(matches) != 1 || matches[0].Line != 2 {
		t.Fatalf("remote rg result = %#v backend=%v err=%v", matches, backend, err)
	}

	client.runStreaming = nil
	matches, backend, err = searchRemoteWorkspaceFiles(context.Background(), client, "/srv/repo", []string{"main.go"}, workspaceSearchOptions{Query: "needle"}, 20)
	if err != nil || backend != workspaceSearchBackendGo || len(matches) != 1 {
		t.Fatalf("remote filesystem result = %#v backend=%v err=%v", matches, backend, err)
	}
}

func TestRemoteCodeSaveAwaitsPendingNewerBuffer(t *testing.T) {
	h := menuTestApp(t)
	h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/repo"}
	h.app.snapshot.Root = "/srv/repo"
	client := newRemoteTestClient()
	filename := "/srv/repo/note.txt"
	client.infos[filename] = remoteTestRegular(filename, "original", 1)
	client.contents[filename] = []byte("original")
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var once sync.Once
	client.writeFile = func(_ string, data []byte, _ os.FileMode) error {
		if len(client.atomicWrites) == 0 {
			once.Do(func() { close(firstStarted) })
			<-releaseFirst
		}
		if strings.Contains(string(data), "second-edit") {
			return fmt.Errorf("the second request must wait for the first")
		}
		return nil
	}
	state := &editor.State{Buffer: editor.NewBuffer("original")}
	state.Insert("first-edit\n")
	document := &codeDocument{
		path: "note.txt", state: state,
		remoteStamp: stampRemoteFile(client.infos[filename]), remoteStampKnown: true,
	}
	h.app.code.documents = map[string]*codeDocument{"note.txt": document}
	closed, closeResult := false, false
	h.app.saveRemoteCodeDocumentWithClient(client, document, func(ok bool) {
		closeResult = ok
		closed = true
	})
	h.waitFor(t, "the first remote save", func() bool {
		select {
		case <-firstStarted:
			return true
		default:
			return false
		}
	})
	state.Insert("second-edit\n")
	h.app.saveRemoteCodeDocumentWithClient(client, document, nil)
	// The second request deliberately fails if it is written. Replace its
	// blocker before releasing request one; success then proves ordering.
	client.writeFile = nil
	if closed {
		t.Fatal("Save-and-Close completed while the first request was blocked")
	}
	close(releaseFirst)
	h.waitFor(t, "both remote saves", func() bool { return closed })
	if !closeResult || state.Buffer.Modified() || document.saveError != "" {
		t.Fatalf("save completion ok=%v modified=%v error=%q", closeResult, state.Buffer.Modified(), document.saveError)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.atomicWrites) != 2 || !strings.Contains(string(client.atomicWrites[0]), "first-edit") ||
		strings.Contains(string(client.atomicWrites[0]), "second-edit") || !strings.Contains(string(client.atomicWrites[1]), "second-edit") {
		t.Fatalf("captured remote writes = %q", client.atomicWrites)
	}
}

func TestRemoteCodeSaveFailureRetainsModifiedBuffer(t *testing.T) {
	h := menuTestApp(t)
	h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/repo"}
	h.app.snapshot.Root = "/srv/repo"
	client := newRemoteTestClient()
	filename := "/srv/repo/note.txt"
	client.infos[filename] = remoteTestRegular(filename, "original", 1)
	client.writeFile = func(string, []byte, os.FileMode) error { return errors.New("remote disk full") }
	state := &editor.State{Buffer: editor.NewBuffer("original")}
	state.Insert("unsaved")
	document := &codeDocument{path: "note.txt", state: state, remoteStamp: stampRemoteFile(client.infos[filename]), remoteStampKnown: true}
	h.app.code.documents = map[string]*codeDocument{"note.txt": document}
	completed := false
	h.app.saveRemoteCodeDocumentWithClient(client, document, func(ok bool) {
		if ok {
			t.Error("failed save reported success")
		}
		completed = true
	})
	h.waitFor(t, "the failed remote save", func() bool { return completed })
	if !state.Buffer.Modified() || !strings.Contains(document.saveError, "remote disk full") {
		t.Fatalf("failed save lost dirty state/error: modified=%v error=%q", state.Buffer.Modified(), document.saveError)
	}
}
