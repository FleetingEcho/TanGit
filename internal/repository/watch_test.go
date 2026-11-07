package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func waitRepositoryEvent(t *testing.T, events <-chan struct{}) {
	t.Helper()
	select {
	case _, ok := <-events:
		if !ok {
			t.Fatal("the repository watcher closed unexpectedly")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("the filesystem change did not produce a repository event")
	}
}

func TestWatchReportsWorkingTreeAndGitMetadataChanges(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "first")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := session.(WatchingSession).Watch(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() {
		cancel()
		for {
			select {
			case _, ok := <-events:
				if !ok {
					return
				}
			case <-time.After(4 * time.Second):
				t.Error("cancelling did not close the repository watcher")
				return
			}
		}
	}()

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("changed elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitRepositoryEvent(t, events)

	runGit(t, dir, "add", "file.txt")
	waitRepositoryEvent(t, events)

	runGit(t, dir, "commit", "-m", "made elsewhere")
	waitRepositoryEvent(t, events)

	// A source directory named objects is not Git's own object store. Its
	// existing and newly created subtrees must remain observable.
	objects := filepath.Join(dir, "objects")
	if err := os.Mkdir(objects, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(objects, "source.txt")
	if err := os.WriteFile(source, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitRepositoryEvent(t, events) // the directory creation registers its watch
	if err := os.WriteFile(source, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitRepositoryEvent(t, events)
}

func TestWatchReportsPermissionOnlyChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Git's executable-bit permission changes")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "script.sh")
	runGit(t, dir, "commit", "-m", "add script")

	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := session.(WatchingSession).Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	waitRepositoryEvent(t, events)
}

func TestAddWatchTreeHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	watcher, err := fsnotify.NewBufferedWatcher(1)
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = addWatchTree(ctx, watcher, root, func(string, os.DirEntry) bool { return false })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled walk returned %v, want context.Canceled", err)
	}
	if paths := watcher.WatchList(); len(paths) != 0 {
		t.Fatalf("the cancelled walk installed watches: %v", paths)
	}
}
