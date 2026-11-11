package app

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mygotransfer "github.com/egoist/mygo/transfer"
)

func writeLocalTransferTestFile(t *testing.T, root, name, content string) string {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return filename
}

func readLocalTransferTestFile(t *testing.T, filename string) string {
	t.Helper()
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func localTransferTestData(t *testing.T, sources ...string) mygotransfer.Data {
	t.Helper()
	data, err := mygotransfer.FileData(sources...)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func localTransferTestApp(t *testing.T, root string) (*app, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	originalList := listCodeFiles
	listCodeFiles = func(string) ([]string, error) { return nil, nil }
	t.Cleanup(func() { listCodeFiles = originalList })

	a := newApp(fakeOpener{})
	a.state = stateReady
	a.snapshot.Root = root
	completed := make(chan struct{}, 1)
	filesRead := make(chan struct{}, 1)
	a.update = func(fn func()) {
		fn()
		if len(a.localTransfers.jobs) == 0 {
			completed <- struct{}{}
		}
	}
	a.updateChanged = func(fn func() bool) {
		fn()
		filesRead <- struct{}{}
	}
	return a, completed, filesRead
}

func TestLocalTransferDropCopiesMultipleInputsRecursively(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	sources := t.TempDir()
	folder := filepath.Join(sources, "folder")
	writeLocalTransferTestFile(t, folder, "nested/deep/file.txt", "nested content")
	writeLocalTransferTestFile(t, folder, "top.txt", "top content")
	note := writeLocalTransferTestFile(t, sources, "note.txt", "note content")
	a, completed, filesRead := localTransferTestApp(t, root)

	a.receiveLocalTransferDrop(localTransferTestData(t, folder, note), "inbox")
	<-completed
	<-filesRead

	for name, want := range map[string]string{
		"inbox/folder/nested/deep/file.txt": "nested content",
		"inbox/folder/top.txt":              "top content",
		"inbox/note.txt":                    "note content",
	} {
		if got := readLocalTransferTestFile(t, filepath.Join(root, filepath.FromSlash(name))); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	// A native drop copies rather than moves every source.
	if got := readLocalTransferTestFile(t, filepath.Join(folder, "nested/deep/file.txt")); got != "nested content" {
		t.Fatalf("source directory changed to %q", got)
	}
	if got := readLocalTransferTestFile(t, note); got != "note content" {
		t.Fatalf("source file changed to %q", got)
	}
	if !a.code.filesLoaded || a.code.filesLoading || len(a.pendingToasts) == 0 {
		t.Fatalf("copy completion left inconsistent UI state: filesLoaded=%v filesLoading=%v toasts=%#v",
			a.code.filesLoaded, a.code.filesLoading, a.pendingToasts)
	}
	if toast := a.pendingToasts[len(a.pendingToasts)-1]; toast.kind != "success" || toast.title != "Copy complete" {
		t.Fatalf("final copy notification = %+v", toast)
	}
	progressFound := false
	for _, toast := range a.pendingToasts {
		if toast.title == "Copy in progress" && toast.action == "Cancel" && strings.Contains(toast.description, "0 of 3 items") {
			progressFound = true
		}
	}
	if !progressFound {
		t.Fatalf("recursive copy did not publish measured progress with Cancel: %+v", a.pendingToasts)
	}
}

func TestLocalTransferDropValidatesAllDestinationsBeforeCopying(t *testing.T) {
	root := t.TempDir()
	writeLocalTransferTestFile(t, root, "inbox/second.txt", "keep me")
	sources := t.TempDir()
	first := writeLocalTransferTestFile(t, sources, "first.txt", "first")
	second := writeLocalTransferTestFile(t, sources, "second.txt", "replacement")
	a, completed, _ := localTransferTestApp(t, root)

	a.receiveLocalTransferDrop(localTransferTestData(t, first, second), "inbox")
	<-completed

	if got := readLocalTransferTestFile(t, filepath.Join(root, "inbox/second.txt")); got != "keep me" {
		t.Fatalf("existing destination was clobbered with %q", got)
	}
	if _, err := os.Lstat(filepath.Join(root, "inbox/first.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("first input was copied before the second collision was rejected: %v", err)
	}
	if len(a.pendingToasts) == 0 {
		t.Fatal("collision produced no notification")
	}
	if toast := a.pendingToasts[len(a.pendingToasts)-1]; toast.kind != "error" || toast.title != "Copy failed" {
		t.Fatalf("collision notification = %+v, want one final error", toast)
	}
}

func TestPerformLocalTransferDropSafety(t *testing.T) {
	t.Run("directory into its subtree", func(t *testing.T) {
		root := t.TempDir()
		source := filepath.Join(root, "folder")
		writeLocalTransferTestFile(t, source, "file.txt", "content")
		if err := os.Mkdir(filepath.Join(source, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := performLocalTransferDrop(root, "folder/nested", []string{source}); err == nil {
			t.Fatal("copying a folder into its subtree succeeded")
		}
		if _, err := os.Lstat(filepath.Join(source, "nested/folder")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("rejected recursive copy created a destination: %v", err)
		}
	})

	t.Run("symlinked destination parent", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
			t.Fatal(err)
		}
		source := writeLocalTransferTestFile(t, t.TempDir(), "file.txt", "content")
		if _, err := performLocalTransferDrop(root, "escape", []string{source}); err == nil {
			t.Fatal("copying through a symlinked destination succeeded")
		}
		if _, err := os.Lstat(filepath.Join(outside, "file.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("copy escaped through the symlink: %v", err)
		}
	})

	t.Run("source symlinks are not followed", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "inbox"), 0o755); err != nil {
			t.Fatal(err)
		}
		sources := t.TempDir()
		source := filepath.Join(sources, "folder")
		writeLocalTransferTestFile(t, sources, "secret.txt", "outside the folder")
		if err := os.Mkdir(source, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../secret.txt", filepath.Join(source, "link")); err != nil {
			t.Fatal(err)
		}
		if _, err := performLocalTransferDrop(root, "inbox", []string{source}); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "inbox/folder/link")
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("source symlink was followed instead of copied as a link")
		}
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatal(err)
		}
		if target != "../secret.txt" {
			t.Fatalf("copied link target = %q", target)
		}
		if _, err := os.Stat(link); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("copied link unexpectedly resolves to copied external content: %v", err)
		}
	})

	t.Run("repository traversal", func(t *testing.T) {
		root := t.TempDir()
		source := writeLocalTransferTestFile(t, t.TempDir(), "file.txt", "content")
		if _, err := performLocalTransferDrop(root, "../escape", []string{source}); err == nil {
			t.Fatal("copy accepted a destination outside the repository")
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(root), "escape/file.txt")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("path traversal created an outside file: %v", err)
		}
	})
}

func TestLocalTransferDropCancellationStopsChunkedCopyAndCleansUp(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "large.bin")
	contents := bytes.Repeat([]byte{0x5a}, 3*localTransferBufferSize)
	if err := os.WriteFile(source, contents, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var measured localTransferProgress
	var copied int64
	_, err := performLocalTransferDropContext(ctx, root, "", []string{source}, func(progress localTransferProgress) {
		if !progress.Preparing && progress.TotalItems > 0 {
			measured = progress
		}
		copied = progress.CompletedBytes
		if progress.CompletedBytes >= localTransferBufferSize {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("large copy cancellation error = %v", err)
	}
	if measured.TotalItems != 1 || measured.TotalBytes != int64(len(contents)) {
		t.Fatalf("large copy totals = %+v", measured)
	}
	if copied == 0 || copied >= int64(len(contents)) {
		t.Fatalf("copy did not stop after a cancellable chunk: %d of %d", copied, len(contents))
	}
	if _, statErr := os.Lstat(filepath.Join(root, "large.bin")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("cancelled copy left an incomplete binary: %v", statErr)
	}
}

func TestLocalTransferCancellationNeverRemovesAnotherInputDestination(t *testing.T) {
	root := t.TempDir()
	sources := t.TempDir()
	first := writeLocalTransferTestFile(t, sources, "first.txt", "first")
	second := writeLocalTransferTestFile(t, sources, "second.txt", "source")
	independent := filepath.Join(root, "second.txt")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := performLocalTransferDropContext(ctx, root, "", []string{first, second}, func(progress localTransferProgress) {
		if progress.CompletedItems == 1 {
			// This destination was not created by the copy and must survive even
			// though it belongs to another top-level input in the same batch.
			if writeErr := os.WriteFile(independent, []byte("independent"), 0o644); writeErr != nil {
				t.Error(writeErr)
			}
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
	if got := readLocalTransferTestFile(t, independent); got != "independent" {
		t.Fatalf("unrelated second destination was changed to %q", got)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "first.txt")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("owned partial output survived cancellation: %v", statErr)
	}
}

func TestLocalTransferProgressCancelAndShutdown(t *testing.T) {
	a := newApp(fakeOpener{})
	a.state = stateReady
	a.snapshot.Root = t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	a.localTransfers.nextID = 1
	a.localTransfers.jobs = map[uint64]*localTransferJob{1: {cancel: cancel, progress: localTransferProgress{Preparing: true}}}
	a.publishLocalTransfer(a.generation, a.snapshot.Root, 1)
	if len(a.pendingToasts) != 1 || a.pendingToasts[0].action != "Cancel" {
		t.Fatalf("progress notification does not offer Cancel: %+v", a.pendingToasts)
	}
	toast := a.pendingToasts[0]
	if toast.id != "local-transfer:1" || toast.timeout != -1 {
		t.Fatalf("progress notification identity/lifetime = %+v", toast)
	}
	toast.onAction()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("Cancel did not cancel the copy context")
	}
	last := a.pendingToasts[len(a.pendingToasts)-1]
	if last.title != "Cancelling copy" || last.action != "" {
		t.Fatalf("cancellation feedback = %+v", last)
	}
	// A later terminal result can still replace the same visible card, while
	// workspace reset cancels and invalidates all callbacks without a wait.
	a.localTransfers.jobs[1].progress = localTransferProgress{CompletedItems: 3, TotalItems: 3, CompletedBytes: 1024, TotalBytes: 1024}
	a.publishLocalTransfer(a.generation, a.snapshot.Root, 1)
	if got := localTransferProgressDescription(a.localTransfers.jobs[1].progress, false); got != "3 of 3 items · 100% · 1.0 KiB of 1.0 KiB" {
		t.Fatalf("progress description = %q", got)
	}
	a.resetRepositoryState(a.snapshot)
	if a.localTransfers.jobs != nil {
		t.Fatal("workspace reset retained active local imports")
	}
}

func TestLocalTransferDropStaleGenerationHasNoUIEffect(t *testing.T) {
	root := t.TempDir()
	source := writeLocalTransferTestFile(t, t.TempDir(), "file.txt", "content")
	a := newApp(fakeOpener{})
	a.state = stateReady
	a.snapshot.Root = root
	a.generation = 10
	generation := a.generation
	entered := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	originalPerform := performLocalTransferDropForUI
	performLocalTransferDropForUI = func(ctx context.Context, root, directory string, sources []string, report func(localTransferProgress)) ([]string, error) {
		destinations, err := performLocalTransferDropContext(ctx, root, directory, sources, report)
		close(entered)
		<-release
		return destinations, err
	}
	t.Cleanup(func() { performLocalTransferDropForUI = originalPerform })
	a.update = func(fn func()) {
		fn()
		if a.generation != generation {
			close(completed)
		}
	}

	a.receiveLocalTransferDrop(localTransferTestData(t, source), "")
	<-entered
	// The filesystem copy has completed, but its publication belongs to an old
	// workspace and must not repaint, refresh, or notify its replacement.
	a.generation++
	close(release)
	<-completed

	if got := readLocalTransferTestFile(t, filepath.Join(root, "file.txt")); got != "content" {
		t.Fatalf("copied file = %q", got)
	}
	for _, toast := range a.pendingToasts {
		if toast.title == "Copy complete" || toast.title == "Copy failed" || toast.title == "Copy cancelled" {
			t.Fatalf("stale completion published %+v", toast)
		}
	}
	if a.snapshotSeq != 0 || a.code.filesToken != 0 || a.code.filesLoading {
		t.Fatalf("stale completion refreshed replacement workspace: toasts=%#v snapshotSeq=%d filesToken=%d loading=%v",
			a.pendingToasts, a.snapshotSeq, a.code.filesToken, a.code.filesLoading)
	}
}
