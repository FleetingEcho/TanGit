package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestClearUnfinishedIndexesCancelsAndWaitsForBuilders(t *testing.T) {
	previousAsync := cacheCleanupAsync
	cacheCleanupAsync = func(fn func()) { go fn() }
	t.Cleanup(func() { cacheCleanupAsync = previousAsync })
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "one.txt"), []byte("index me"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newApp(nil)
	a.pathCache = filepath.Join(t.TempDir(), "cache")
	cacheRoot := filepath.Join(a.pathCache, codeSearchCacheDirName)
	staging := filepath.Join(cacheRoot, ".tangit-index-abandoned")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "partial.zoekt"), []byte("unfinished"), 0o600); err != nil {
		t.Fatal(err)
	}

	published := codeSearchIndexDirectory(a.pathCache, root)
	unlockDirectory := lockCodeSearchIndex(published)
	var unlockOnce sync.Once
	releaseDirectory := func() { unlockOnce.Do(unlockDirectory) }
	t.Cleanup(releaseDirectory)
	// A real build lease makes deletion wait independently of goroutine timing.
	releaseBuild, ok := acquireCodeSearchIndexBuild()
	if !ok {
		t.Fatal("a finished cleanup left the maintenance barrier closed")
	}
	var buildOnce sync.Once
	releaseLease := func() { buildOnce.Do(releaseBuild) }
	t.Cleanup(releaseLease)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	buildDone := make(chan error, 1)
	go func() {
		_, err := buildCodeSearchIndex(ctx, root, []string{"one.txt"}, nil, published)
		buildDone <- err
	}()
	clearDone := make(chan struct{})
	var clearOnce sync.Once
	a.update = func(fn func()) {
		fn()
		clearOnce.Do(func() { close(clearDone) })
	}
	canceled := make(chan struct{})
	var cancelOnce sync.Once
	a.onCachesCleared = func() { cancelOnce.Do(func() { close(canceled); cancel() }) }

	a.clearUnfinishedIndexes()
	<-canceled
	select {
	case <-clearDone:
		t.Fatal("cache cleanup did not wait for the in-flight build lease")
	case <-time.After(40 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(staging, "partial.zoekt")); err != nil {
		t.Fatalf("staging was removed while a builder still held the lease: %v", err)
	}
	if size := codeSearchIndexDiskBytes(root); size != int64(len("index me")) {
		t.Fatalf("fixture repository content changed size: %d", size)
	}

	releaseDirectory()
	releaseLease()
	if err := <-buildDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled build error = %v", err)
	}
	select {
	case <-clearDone:
	case <-time.After(10 * time.Second):
		t.Fatal("cache cleanup did not finish after the builder exited")
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("unfinished staging survived confirmed cleanup: %v", err)
	}
	if a.cacheCleanupBusy {
		t.Fatal("cache cleanup did not release its busy state")
	}

	release, ok := acquireCodeSearchIndexBuild()
	if !ok {
		t.Fatal("cleanup permanently disabled future index builds")
	}
	release()
	if _, err := buildCodeSearchIndex(context.Background(), root, []string{"one.txt"}, nil, published); err != nil {
		t.Fatalf("index cannot be rebuilt after cleanup: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "one.txt")); err != nil || string(data) != "index me" {
		t.Fatalf("index lifecycle modified repository contents: %q, %v", data, err)
	}
}
