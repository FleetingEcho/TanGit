package repository

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// repositoryEventDelay joins the small storm of notifications produced by one
// editor save or Git operation into one refresh request. The polling fallback
// remains in place for filesystems that cannot deliver watcher events.
const repositoryEventDelay = 120 * time.Millisecond

// WatchingSession reports coalesced working-tree and Git-metadata changes.
// Implementations close the returned channel when ctx is cancelled. Watching
// is optional: sessions without it keep the app's timer-based refresh.
type WatchingSession interface {
	Session
	Watch(context.Context) (<-chan struct{}, error)
}

// Watch observes both the working tree and the repository's metadata. A linked
// worktree has separate private and common Git directories, so both are watched;
// object storage is deliberately left out because refs and the index describe
// every change the UI reads without an event for every object Git writes.
func (s *gitSession) Watch(ctx context.Context) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	watcher, err := fsnotify.NewBufferedWatcher(64)
	if err != nil {
		return nil, err
	}

	paths := s.git.RepoPaths
	worktreeGitDir := cleanWatchPath(paths.WorktreeGitDirPath())
	commonGitDir := cleanWatchPath(paths.RepoGitDirPath())
	if worktree := cleanWatchPath(paths.WorktreePath()); worktree != "" {
		err = addWatchTree(ctx, watcher, worktree, func(path string, entry fs.DirEntry) bool {
			clean := filepath.Clean(path)
			return clean == worktreeGitDir || clean == commonGitDir || entry.Name() == ".git"
		})
	}
	if err == nil && worktreeGitDir != "" {
		err = addWatchTree(ctx, watcher, worktreeGitDir, gitMetadataWatchExcluded(worktreeGitDir))
	}
	if err == nil && commonGitDir != "" && commonGitDir != worktreeGitDir {
		err = addWatchTree(ctx, watcher, commonGitDir, gitMetadataWatchExcluded(commonGitDir))
	}
	if err != nil {
		_ = watcher.Close()
		return nil, err
	}

	notifications := make(chan struct{}, 1)
	go runRepositoryWatcher(ctx, watcher, notifications, func(path string, entry fs.DirEntry) bool {
		clean := filepath.Clean(path)
		return entry.Name() == ".git" ||
			(worktreeGitDir != "" && clean == filepath.Join(worktreeGitDir, "objects")) ||
			(commonGitDir != "" && clean == filepath.Join(commonGitDir, "objects"))
	})
	return notifications, nil
}

func cleanWatchPath(path string) string {
	if path == "" {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}

// addWatchTree includes existing directories and skips excluded subtrees. New
// directories are added by runRepositoryWatcher as their Create events arrive.
// Checking the context at every entry keeps closing a repository responsive,
// even when its initial walk reaches a large directory tree.
func addWatchTree(
	ctx context.Context,
	watcher *fsnotify.Watcher,
	root string,
	excluded func(string, fs.DirEntry) bool,
) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if path != root && excluded(path, entry) {
			return filepath.SkipDir
		}
		return watcher.Add(path)
	})
}

func gitMetadataWatchExcluded(root string) func(string, fs.DirEntry) bool {
	objects := filepath.Join(root, "objects")
	return func(path string, entry fs.DirEntry) bool {
		return entry.IsDir() && filepath.Clean(path) == objects
	}
}

// runRepositoryWatcher owns watcher after Watch returns. Notification errors
// only lose the immediate path: the app's existing timer remains the fallback.
func runRepositoryWatcher(
	ctx context.Context,
	watcher *fsnotify.Watcher,
	notifications chan<- struct{},
	excluded func(string, fs.DirEntry) bool,
) {
	defer close(notifications)
	defer watcher.Close()

	events := watcher.Events
	errors := watcher.Errors
	var timer *time.Timer
	var delayed <-chan time.Time
	stopTimer := func() {
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
	defer stopTimer()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			// Chmod matters too: on filesystems with executable-bit support,
			// it can be the only notification for a change to Git's status.
			if event.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = addWatchTree(ctx, watcher, event.Name, excluded)
				}
			}
			if timer == nil {
				timer = time.NewTimer(repositoryEventDelay)
			} else {
				stopTimer()
				timer.Reset(repositoryEventDelay)
			}
			delayed = timer.C
		case <-delayed:
			// A full channel already represents this pending change. Keeping one
			// seat also prevents an inactive tab from accumulating notifications.
			select {
			case notifications <- struct{}{}:
			default:
			}
			delayed = nil
		case _, ok := <-errors:
			if !ok {
				errors = nil
			}
		}
	}
}
