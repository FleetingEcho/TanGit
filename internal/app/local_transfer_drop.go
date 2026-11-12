package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	mygotransfer "github.com/egoist/mygo/transfer"
)

const localTransferBufferSize = 256 * 1024

var performLocalTransferDropForUI = performLocalTransferDropContext

// localTransferUIState belongs to one repository page. Cancellation functions
// are safe to call from the UI while workers copy off the UI goroutine; every
// other job field is UI-owned.
type localTransferUIState struct {
	nextID uint64
	jobs   map[uint64]*localTransferJob
}

type localTransferJob struct {
	cancel     context.CancelFunc
	progress   localTransferProgress
	cancelling bool
}

type localTransferProgress struct {
	Preparing      bool
	CompletedItems int
	TotalItems     int
	CompletedBytes int64
	TotalBytes     int64
}

// receiveLocalTransferDrop copies native file drops into a local working tree.
// Progress uses one replace-in-place notification and never polls. Filesystem
// work happens away from the UI goroutine; cancellation and every publication
// retain their originating workspace identity.
func (a *app) receiveLocalTransferDrop(data mygotransfer.Data, directory string) {
	root := a.snapshot.Root
	if a.state != stateReady || root == "" || a.location.IsRemote() ||
		(directory != "" && !validCodeManagedPath(directory)) {
		return
	}
	sources, err := data.Files()
	if err != nil {
		a.notify("error", "Could not copy files", safeError(err))
		return
	}
	if len(sources) == 0 {
		return
	}

	generation := a.generation
	ctx, cancel := context.WithCancel(context.Background())
	state := &a.localTransfers
	state.nextID++
	id := state.nextID
	if state.jobs == nil {
		state.jobs = make(map[uint64]*localTransferJob)
	}
	job := &localTransferJob{cancel: cancel, progress: localTransferProgress{Preparing: true}}
	state.jobs[id] = job
	a.publishLocalTransfer(generation, root, id)

	go func() {
		defer a.recoverBackground()
		defer cancel()
		var lastPublished time.Time
		publish := func(progress localTransferProgress, force bool) {
			// Ten updates per second are enough for responsive feedback without
			// turning a many-file import into a repaint storm. Preparation and
			// terminal results are always published immediately.
			if !force && !lastPublished.IsZero() && time.Since(lastPublished) < 100*time.Millisecond {
				return
			}
			lastPublished = time.Now()
			a.update(func() {
				if !a.currentLocalTransfer(generation, root, id) {
					return
				}
				job.progress = progress
				a.publishLocalTransfer(generation, root, id)
			})
		}
		destinations, copyErr := performLocalTransferDropForUI(ctx, root, directory, sources, func(progress localTransferProgress) {
			publish(progress, !progress.Preparing && progress.CompletedItems == 0 && progress.CompletedBytes == 0)
		})
		a.update(func() {
			if !a.currentLocalTransfer(generation, root, id) {
				return
			}
			delete(a.localTransfers.jobs, id)
			toast := appToast{id: localTransferToastID(id), timeout: 7 * time.Second}
			switch {
			case errors.Is(copyErr, context.Canceled):
				toast.title = "Copy cancelled"
				toast.description = "Incomplete copied items were removed."
			case copyErr != nil:
				toast.kind = "error"
				toast.title = "Copy failed"
				toast.description = safeError(copyErr)
			default:
				toast.kind = "success"
				toast.title = "Copy complete"
				toast.description = strings.Join(destinations, ", ")
				// Reads already in flight predate the copy. Replace them rather
				// than allowing an old file list or snapshot to land afterward.
				a.code.filesToken++
				a.code.filesLoading = false
				a.code.filesLoaded = false
				a.code.filesOperationRefresh = true
				a.snapshotSeq++
				a.requestCodeFiles()
				a.requestQuietRefresh()
			}
			a.notifyToast(toast)
		})
	}()
}

func localTransferToastID(id uint64) string {
	return fmt.Sprintf("local-transfer:%d", id)
}

func (a *app) currentLocalTransfer(generation uint64, root string, id uint64) bool {
	return generation == a.generation && root == a.snapshot.Root && a.localTransfers.jobs[id] != nil
}

func (a *app) publishLocalTransfer(generation uint64, root string, id uint64) {
	job := a.localTransfers.jobs[id]
	if job == nil {
		return
	}
	title := "Preparing copy"
	if !job.progress.Preparing {
		title = "Copy in progress"
	}
	if job.cancelling {
		title = "Cancelling copy"
	}
	toast := appToast{
		id:          localTransferToastID(id),
		title:       title,
		description: localTransferProgressDescription(job.progress, job.cancelling),
		timeout:     -1,
	}
	if !job.cancelling {
		toast.action = "Cancel"
		toast.onAction = func() {
			if a.currentLocalTransfer(generation, root, id) {
				job.cancelling = true
				job.cancel()
				a.publishLocalTransfer(generation, root, id)
			}
		}
	}
	a.notifyToast(toast)
}

func localTransferProgressDescription(progress localTransferProgress, cancelling bool) string {
	if cancelling {
		return "Stopping after the current I/O chunk and removing incomplete items…"
	}
	if progress.Preparing {
		return "Counting files and bytes before copying…"
	}
	processed := progress.CompletedItems
	text := fmt.Sprintf("%d of %d items", processed, progress.TotalItems)
	if progress.TotalBytes > 0 {
		completed := min(progress.CompletedBytes, progress.TotalBytes)
		text = fmt.Sprintf("%s · %d%% · %s of %s", text, completed*100/progress.TotalBytes,
			formatRemoteTransferBytes(completed), formatRemoteTransferBytes(progress.TotalBytes))
	} else {
		text += " · " + formatRemoteTransferBytes(progress.CompletedBytes)
	}
	return text
}

// shutdownLocalTransfers stops work without waiting on the UI thread. The
// workspace generation makes all subsequent worker publications irrelevant.
func (a *app) shutdownLocalTransfers() {
	for _, job := range a.localTransfers.jobs {
		job.cancel()
	}
	a.localTransfers.jobs = nil
}

// performLocalTransferDrop preserves the synchronous backend used by focused
// filesystem tests and other callers that do not need progress.
func performLocalTransferDrop(root, directory string, sources []string) ([]string, error) {
	return performLocalTransferDropContext(context.Background(), root, directory, sources, nil)
}

// performLocalTransferDropContext validates every top-level destination before
// it creates anything. In particular, one collision in a multi-file drop cannot
// leave the earlier files copied beside the destination that was rejected.
func performLocalTransferDropContext(ctx context.Context, root, directory string, sources []string, report func(localTransferProgress)) ([]string, error) {
	if root == "" || (directory != "" && !validCodeManagedPath(directory)) {
		return nil, fmt.Errorf("invalid copy destination %q", directory)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, err
	}
	resolvedDirectory := filepath.Join(resolvedRoot, filepath.FromSlash(directory))

	type item struct {
		source      string
		destination string
		relative    string
	}
	items := make([]item, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !filepath.IsAbs(source) {
			return nil, fmt.Errorf("source %q is not an absolute path", source)
		}
		source = filepath.Clean(source)
		info, err := os.Lstat(source)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return nil, fmt.Errorf("%q is not a regular file, directory, or symbolic link", source)
		}
		relative := joinCodePath(directory, filepath.Base(source))
		if !validCodeManagedPath(relative) {
			return nil, fmt.Errorf("invalid copy destination %q", relative)
		}
		if seen[relative] {
			return nil, fmt.Errorf("multiple sources have destination %q", relative)
		}
		seen[relative] = true
		if err := checkCodeParentDirectories(absoluteRoot, relative); err != nil {
			return nil, err
		}
		destination := filepath.Join(absoluteRoot, filepath.FromSlash(relative))
		if _, err := os.Lstat(destination); err == nil {
			return nil, fmt.Errorf("destination %q already exists", relative)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if info.IsDir() {
			resolvedSource, err := filepath.EvalSymlinks(source)
			if err != nil {
				return nil, err
			}
			inside, err := localTransferPathWithin(resolvedDirectory, resolvedSource)
			if err != nil {
				return nil, err
			}
			if inside {
				return nil, fmt.Errorf("cannot copy directory %q into itself", source)
			}
		}
		items = append(items, item{source: source, destination: destination, relative: relative})
	}

	progress := localTransferProgress{Preparing: true}
	if report != nil {
		report(progress)
	}
	for _, item := range items {
		if err := measureLocalTransferEntry(ctx, item.source, &progress); err != nil {
			return nil, err
		}
	}
	progress.Preparing = false
	if report != nil {
		report(progress)
	}

	destinations := make([]string, 0, len(items))
	createdDestinations := make([]string, 0, len(items))
	for _, item := range items {
		created := false
		err := copyLocalTransferEntryContext(ctx, item.source, item.destination, &progress, report, &created)
		if created {
			createdDestinations = append(createdDestinations, item.destination)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				var cleanupErr error
				// Cleanup is strictly limited to top-level outputs this operation
				// successfully created. A destination that appeared independently,
				// including one belonging to another input, is never removed.
				for _, copied := range createdDestinations {
					cleanupErr = errors.Join(cleanupErr, os.RemoveAll(copied))
				}
				if cleanupErr != nil {
					return nil, fmt.Errorf("%w; could not remove every incomplete item: %v", err, cleanupErr)
				}
			}
			return nil, err
		}
		destinations = append(destinations, item.relative)
	}
	return destinations, nil
}

func measureLocalTransferEntry(ctx context.Context, source string, progress *localTransferProgress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	switch {
	case info.Mode().IsRegular():
		progress.TotalItems++
		progress.TotalBytes += info.Size()
	case info.Mode()&os.ModeSymlink != 0:
		progress.TotalItems++
	case info.IsDir():
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := measureLocalTransferEntry(ctx, filepath.Join(source, entry.Name()), progress); err != nil {
				return err
			}
		}
	}
	return nil
}

// localTransferPathWithin reports whether name is directory itself or its
// descendant. Both paths have already been cleaned by the caller.
func localTransferPathWithin(name, directory string) (bool, error) {
	relative, err := filepath.Rel(directory, name)
	if err != nil {
		return false, err
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)), nil
}

// copyLocalTransferEntry uses Lstat at every level. Symbolic links are copied
// as links, never traversed, so a link cannot make the copy read outside its
// source tree (or recurse through a directory cycle).
func copyLocalTransferEntry(source, destination string) error {
	return copyLocalTransferEntryContext(context.Background(), source, destination, &localTransferProgress{}, nil, nil)
}

func copyLocalTransferEntryContext(ctx context.Context, source, destination string, progress *localTransferProgress, report func(localTransferProgress), created *bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, destination); err != nil {
			return err
		}
		if created != nil {
			*created = true
		}
		progress.CompletedItems++
		if report != nil {
			report(*progress)
		}
		return nil
	case info.IsDir():
		if err := os.Mkdir(destination, info.Mode().Perm()); err != nil {
			return err
		}
		if created != nil {
			*created = true
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyLocalTransferEntryContext(ctx, filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name()), progress, report, nil); err != nil {
				return err
			}
		}
		return nil
	case info.Mode().IsRegular():
		return copyLocalTransferFileContext(ctx, source, destination, info.Mode().Perm(), progress, report, created)
	default:
		return fmt.Errorf("%q is not a regular file, directory, or symbolic link", source)
	}
}

func copyLocalTransferFile(source, destination string, mode os.FileMode) error {
	return copyLocalTransferFileContext(context.Background(), source, destination, mode, &localTransferProgress{}, nil, nil)
}

func copyLocalTransferFileContext(ctx context.Context, source, destination string, mode os.FileMode, progress *localTransferProgress, report func(localTransferProgress), created *bool) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if created != nil {
		*created = true
	}
	defer output.Close()
	buffer := make([]byte, localTransferBufferSize)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := input.Read(buffer)
		if read > 0 {
			written, writeErr := output.Write(buffer[:read])
			if writeErr != nil {
				return writeErr
			}
			if written != read {
				return io.ErrShortWrite
			}
			progress.CompletedBytes += int64(written)
			if report != nil {
				report(*progress)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	progress.CompletedItems++
	if report != nil {
		report(*progress)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return output.Close()
}
