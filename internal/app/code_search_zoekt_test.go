//go:build linux || darwin || freebsd || netbsd || openbsd || illumos

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lazygitapp/internal/workspace"
)

func buildZoektSearchTestIndex(t *testing.T, root string, paths, excluded []string) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "index")
	info, err := buildCodeSearchIndex(context.Background(), root, paths, excluded, directory)
	if err != nil {
		t.Fatalf("build Zoekt index: %v", err)
	}
	if info.status != codeSearchIndexReady {
		t.Fatalf("built index status = %v, want ready", info.status)
	}
	inspected, err := inspectCodeSearchIndex(context.Background(), root, paths, excluded, directory)
	if err != nil || inspected.status != codeSearchIndexReady {
		t.Fatalf("inspect new index = %+v, %v; want ready", inspected, err)
	}
	return directory
}

func TestZoektWorkspaceSearchMatchesNativeSemantics(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceSearchTestFile(t, root, "second.txt", "short NEEDLE\r\nlast needle\nκόσμε cafe\n")
	writeWorkspaceSearchTestFile(t, root, "profiles/web/first.go", "Alpha needle needle\nécat cat\né Kelvin\nfinal ς\nra_playbook(\"one\")\n")
	writeWorkspaceSearchTestFile(t, root, "tiny.go", "ab")
	writeWorkspaceSearchTestFile(t, root, "profiles/other/skip.go", "needle\n")
	writeWorkspaceSearchTestFile(t, root, "binary.dat", "needle before\n\x00needle after")
	paths := []string{"second.txt", "./profiles/web/first.go", "tiny.go", "profiles/other/skip.go", "binary.dat", "missing.txt"}
	directory := buildZoektSearchTestIndex(t, root, paths, nil)

	for _, test := range []struct {
		name    string
		options workspaceSearchOptions
		limit   int
	}{
		{name: "literal and CRLF", options: workspaceSearchOptions{Query: "needle", CaseSensitive: true}, limit: 20},
		{name: "ASCII case folding", options: workspaceSearchOptions{Query: "NEEDLE"}, limit: 20},
		{name: "short file complement", options: workspaceSearchOptions{Query: "ab", CaseSensitive: true}, limit: 20},
		{name: "Unicode whole word", options: workspaceSearchOptions{Query: "cat", CaseSensitive: true, WholeWord: true}, limit: 20},
		{name: "Unicode regex classes", options: workspaceSearchOptions{Query: `\w+`, RegularExpr: true, CaseSensitive: true}, limit: 100},
		{name: "regex and case", options: workspaceSearchOptions{Query: `RA_[A-Z]+`, RegularExpr: true}, limit: 20},
		{name: "zero-width line start", options: workspaceSearchOptions{Query: `^`, RegularExpr: true, CaseSensitive: true}, limit: 100},
		{name: "zero-width line end", options: workspaceSearchOptions{Query: `$`, RegularExpr: true, CaseSensitive: true}, limit: 100},
		{name: "zero-width word anchor", options: workspaceSearchOptions{Query: `\bneedle\b`, RegularExpr: true, CaseSensitive: true}, limit: 20},
		{name: "zero-width repetition", options: workspaceSearchOptions{Query: `x*`, RegularExpr: true, CaseSensitive: true}, limit: 100},
		{name: "Unicode fold through ASCII query", options: workspaceSearchOptions{Query: "k"}, limit: 20},
		{name: "scope and globs", options: workspaceSearchOptions{Query: "needle", Folder: "profiles", Include: "**/*.go", Exclude: "**/other/**"}, limit: 20},
		{name: "page limit and path order", options: workspaceSearchOptions{Query: "needle", CaseSensitive: true}, limit: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			nativeOptions := test.options
			want, err := searchWorkspaceFilesGoWithOptions(context.Background(), root, paths, nativeOptions, test.limit)
			if err != nil {
				t.Fatal(err)
			}
			test.options.IndexDirectory = directory
			got, backend, err := searchWorkspaceFilesWithOptions(context.Background(), root, paths, test.options, test.limit)
			if err != nil {
				t.Fatal(err)
			}
			if backend != workspaceSearchBackendZoekt {
				t.Fatalf("backend = %v, want actual Zoekt; matches %+v, native %+v", backend, got, want)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("Zoekt matches = %+v, want native %+v", got, want)
			}
		})
	}
}

func TestZoektWorkspaceSearchPaginationReachesFilesAfterMatchHeavyResults(t *testing.T) {
	root := t.TempDir()
	paths := []string{"heavy.txt", "later.txt"}
	writeWorkspaceSearchTestFile(t, root, "heavy.txt", strings.Repeat("needle\n", 300))
	writeWorkspaceSearchTestFile(t, root, "later.txt", "needle at the end\n")
	directory := buildZoektSearchTestIndex(t, root, paths, nil)

	for _, limit := range []int{1, codeSearchPageSize, 2 * codeSearchPageSize} {
		t.Run(fmt.Sprintf("limit %d", limit), func(t *testing.T) {
			options := workspaceSearchOptions{Query: "needle", CaseSensitive: true}
			want, err := searchWorkspaceFilesGoWithOptions(context.Background(), root, paths, options, limit)
			if err != nil {
				t.Fatal(err)
			}
			options.IndexDirectory = directory
			got, backend, err := searchWorkspaceFilesWithOptions(context.Background(), root, paths, options, limit)
			if err != nil || backend != workspaceSearchBackendZoekt {
				t.Fatalf("indexed page backend = %v, err %v; want Zoekt", backend, err)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("indexed page = %+v, want native %+v", got, want)
			}
			if limit == 2*codeSearchPageSize {
				last := got[len(got)-1]
				if last.Path != "later.txt" || last.Line != 1 {
					t.Fatalf("load more did not reach the later file: %+v", got)
				}
			}
		})
	}
}

func TestZoektWorkspaceSearchFallsBackForUnicodeLiteralAndStaleFiles(t *testing.T) {
	originalLookPath := workspaceSearchLookPath
	t.Cleanup(func() { workspaceSearchLookPath = originalLookPath })
	workspaceSearchLookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	root := t.TempDir()
	paths := []string{"source.txt"}
	writeWorkspaceSearchTestFile(t, root, "source.txt", "final ς\nneedle\n")
	directory := buildZoektSearchTestIndex(t, root, paths, nil)
	options := workspaceSearchOptions{Query: "Σ", IndexDirectory: directory}
	got, backend, err := searchWorkspaceFilesWithOptions(context.Background(), root, paths, options, 20)
	if err != nil || backend != workspaceSearchBackendGo {
		t.Fatalf("non-ASCII literal fallback = backend %v, err %v", backend, err)
	}
	want := []workspaceSearchMatch{{Path: "source.txt", Line: 1, Column: 7, Text: "final ς"}}
	if !slices.Equal(got, want) {
		t.Fatalf("non-ASCII fallback matches = %+v, want %+v", got, want)
	}

	// Introducing a match after indexing must not be hidden by the old shard.
	writeWorkspaceSearchTestFile(t, root, "source.txt", "unused\nΣ\n")
	got, backend, err = searchWorkspaceFilesWithOptions(context.Background(), root, paths, options, 20)
	if err != nil || backend != workspaceSearchBackendGo {
		t.Fatalf("stale introduced-match fallback = backend %v, err %v", backend, err)
	}
	want = []workspaceSearchMatch{{Path: "source.txt", Line: 2, Column: 1, Text: "Σ"}}
	if !slices.Equal(got, want) {
		t.Fatalf("stale introduced-match results = %+v, want current native %+v", got, want)
	}
}

func TestZoektWorkspaceSearchFallsBackForCorruptIndex(t *testing.T) {
	originalLookPath := workspaceSearchLookPath
	t.Cleanup(func() { workspaceSearchLookPath = originalLookPath })
	workspaceSearchLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	root := t.TempDir()
	paths := []string{"source.txt"}
	writeWorkspaceSearchTestFile(t, root, "source.txt", "needle\ninfo\n")
	directory := buildZoektSearchTestIndex(t, root, paths, nil)
	if err := os.WriteFile(filepath.Join(directory, codeSearchIndexManifestFile), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	indexed, ok := searchWorkspaceFilesZoekt(context.Background(), root, paths, workspaceSearchOptions{Query: "needle", IndexDirectory: directory}, 20)
	if ok || indexed != nil {
		t.Fatalf("direct corrupt indexed search = indexed %v, ok %v; want neither", indexed, ok)
	}
	got, backend, err := searchWorkspaceFilesWithOptions(context.Background(), root, paths, workspaceSearchOptions{Query: "needle", IndexDirectory: directory}, 20)
	if err != nil || backend != workspaceSearchBackendGo || len(got) != 1 {
		t.Fatalf("corrupt-index fallback = %+v, backend %v, err %v", got, backend, err)
	}
}

func TestZoektIndexCancellationDoesNotPublish(t *testing.T) {
	root := t.TempDir()
	paths := []string{"first.txt", "second.txt"}
	writeWorkspaceSearchTestFile(t, root, "first.txt", "needle\n")
	writeWorkspaceSearchTestFile(t, root, "second.txt", "needle\n")
	directory := filepath.Join(t.TempDir(), "index")
	originalRead := workspaceSearchReadFile
	t.Cleanup(func() { workspaceSearchReadFile = originalRead })
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	workspaceSearchReadFile = func(ctx context.Context, filename string) ([]byte, bool) {
		data, ok := originalRead(ctx, filename)
		if strings.HasSuffix(filename, "first.txt") {
			select {
			case <-started:
			default:
				close(started)
			}
			<-ctx.Done()
			return nil, false
		}
		return data, ok
	}
	done := make(chan error, 1)
	go func() {
		_, err := buildCodeSearchIndex(ctx, root, paths, nil, directory)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled build error = %v, want context cancellation", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled build published %q: %v", directory, err)
	}
}

func TestZoektIndexCacheIdentityUsesCanonicalRepositoryRoot(t *testing.T) {
	cache := t.TempDir()
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("cannot create repository symlink: %v", err)
	}
	first := codeSearchIndexDirectory(cache, root)
	second := codeSearchIndexDirectory(cache, alias)
	if first == "" || first != second {
		t.Fatalf("canonical repository cache directories = %q and %q, want one identity", first, second)
	}
	if strings.HasPrefix(first, root+string(filepath.Separator)) {
		t.Fatalf("index directory %q is inside working tree %q", first, root)
	}
}

func TestCodeSearchIndexBuildsAndRefreshesAutomatically(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	root := useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\ninfo\n"})
	h.app.code.sidebar = codeSidebarSearch
	h.waitFor(t, "automatic background index build", func() bool {
		return h.app.code.searchIndex.status == codeSearchIndexReady && !h.app.code.searchIndex.busy
	})
	h.tester.Frame()
	state := h.app.code.searchIndex
	if state.directory == "" || state.documents != 1 || state.bytes <= 0 || state.err != "" {
		t.Fatalf("automatically built index state = %+v", state)
	}
	for _, detail := range []string{"Build index", "Update index", "Delete search index", "Zoekt", "No local search index"} {
		if h.tester.HasText(detail) {
			t.Fatalf("automatic indexing exposes %q: %q", detail, h.texts())
		}
	}

	h.app.code.searchQuery = "needle"
	h.app.beginCodeSearch()
	h.waitFor(t, "indexed repository search", func() bool {
		return !h.app.code.searchRunning && h.app.code.searchBackend == workspaceSearchBackendZoekt
	})
	if len(h.app.code.searchResults) != 1 || h.app.code.searchSubmittedOptions.IndexDirectory != state.directory {
		t.Fatalf("indexed search did not capture its ready index: results=%+v options=%+v", h.app.code.searchResults, h.app.code.searchSubmittedOptions)
	}

	writeWorkspaceSearchTestFile(t, root, "source.txt", "info\nneedle introduced by a later edit\n")
	h.app.invalidateCodeSearchIndex()
	h.waitFor(t, "automatic stale-index rebuild", func() bool {
		current := h.app.code.searchIndex
		return !current.busy && current.status == codeSearchIndexReady && current.updated.After(state.updated)
	})
	if content, err := os.ReadFile(filepath.Join(root, "source.txt")); err != nil || string(content) != "info\nneedle introduced by a later edit\n" {
		t.Fatalf("automatic indexing changed repository content %q, %v", content, err)
	}
}

func TestRemoteWorkspaceNeverUsesOrCreatesLocalSearchIndex(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "unused-cache")
	a := newApp(fakeOpener{})
	a.pathCache = cache
	a.location.Root = "/srv/repository"
	a.location.Kind = "ssh"
	a.snapshot.Root = "/srv/repository"
	a.code.filesLoaded = true
	a.code.searchIndex = codeSearchIndexState{status: codeSearchIndexReady, directory: filepath.Join(cache, "index")}
	if got := a.codeSearchIndexDirectory(); got != "" {
		t.Fatalf("remote search captured local index %q", got)
	}
	a.ensureCodeSearchIndex()
	a.invalidateCodeSearchIndex()
	if a.code.searchIndex.busy || a.code.searchIndex.requested {
		t.Fatal("SSH workspace started local index inspection or construction")
	}
	if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SSH workspace created local cache %q: %v", cache, err)
	}
}

func TestCodeSearchIndexCleanupOnlyRemovesAbandonedStaging(t *testing.T) {
	parent := t.TempDir()
	now := time.Now()
	for _, name := range []string{".tangit-index-abandoned", ".tangit-index-active", "published-generation"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeWorkspaceSearchTestFile(t, filepath.Join(parent, ".tangit-index-abandoned"), "partial.shard", "incomplete")
	staleFile := filepath.Join(parent, ".tangit-index-not-a-directory")
	if err := os.WriteFile(staleFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".tangit-index-abandoned", ".tangit-index-not-a-directory"} {
		if err := os.Chtimes(filepath.Join(parent, name), now.Add(-48*time.Hour), now.Add(-48*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	cleanupCodeSearchIndexStaging(parent, now)
	if _, err := os.Stat(filepath.Join(parent, ".tangit-index-abandoned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned staging directory was not removed: %v", err)
	}
	for _, name := range []string{".tangit-index-active", "published-generation", ".tangit-index-not-a-directory"} {
		if _, err := os.Stat(filepath.Join(parent, name)); err != nil {
			t.Fatalf("cleanup removed active or non-staging content %q: %v", name, err)
		}
	}
}

func TestCodeSearchQueriesDoNotInvalidateReadyIndex(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\ninfo\n"})
	h.waitFor(t, "automatic background index build", func() bool {
		return h.app.code.searchIndex.status == codeSearchIndexReady && !h.app.code.searchIndex.busy
	})
	state := h.app.code.searchIndex

	for _, query := range []string{"needle", "info", "not-present"} {
		h.app.code.searchQuery = query
		h.app.beginCodeSearch()
		h.waitFor(t, "indexed query "+query, func() bool {
			return !h.app.code.searchRunning && h.app.code.searchSubmitted == query
		})
		if h.app.code.searchBackend != workspaceSearchBackendZoekt {
			t.Fatalf("query %q used backend %v instead of its ready index", query, h.app.code.searchBackend)
		}
		current := h.app.code.searchIndex
		if current.busy || current.requested != state.requested || current.token != state.token ||
			!current.updated.Equal(state.updated) || current.directory != state.directory {
			t.Fatalf("query-only edit invalidated its ready index: before=%+v after=%+v", state, current)
		}
		if h.app.code.searchSubmittedOptions.IndexDirectory != state.directory {
			t.Fatalf("query %q did not reuse the ready index", query)
		}
	}
}

func TestCodeSearchIndexBusyBuildDoesNotDelaySearch(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\n"})
	h.waitFor(t, "automatic background index build", func() bool {
		state := h.app.code.searchIndex
		return state.status == codeSearchIndexReady && !state.busy
	})

	unlock := lockCodeSearchIndex(h.app.code.searchIndex.directory)
	defer unlock()
	// Simulate a generation build holding the shard lock. Search must select
	// a fallback instead of waiting for this lock to be released.
	h.app.code.searchIndex.busy = true
	if got := h.app.codeSearchIndexDirectory(); got != "" {
		t.Fatalf("busy index was offered to a new query: %q", got)
	}
	h.app.code.searchQuery = "needle"
	h.app.beginCodeSearch()
	h.waitFor(t, "query completed while index is busy", func() bool { return !h.app.code.searchRunning })
	if h.app.code.searchBackend == workspaceSearchBackendZoekt {
		t.Fatal("a query reached Zoekt while its generation was rebuilding")
	}
	if len(h.app.code.searchResults) != 1 {
		t.Fatalf("busy-index fallback returned %+v, want the match", h.app.code.searchResults)
	}
}

func TestCodeSearchIndexChangesCancelAndCoalesceBuilds(t *testing.T) {
	originalRead := workspaceSearchReadFile
	originalSchedule := scheduleCodeSearchIndexRebuild
	t.Cleanup(func() {
		workspaceSearchReadFile = originalRead
		scheduleCodeSearchIndexRebuild = originalSchedule
	})
	var delays []time.Duration
	var rebuilds []func()
	scheduleCodeSearchIndexRebuild = func(delay time.Duration, run func()) {
		delays = append(delays, delay)
		rebuilds = append(rebuilds, run)
	}
	firstStarted := make(chan struct{})
	firstCanceled := make(chan struct{})
	var mu sync.Mutex
	readCalls := 0
	workspaceSearchReadFile = func(ctx context.Context, filename string) ([]byte, bool) {
		mu.Lock()
		readCalls++
		call := readCalls
		mu.Unlock()
		if call == 1 {
			close(firstStarted)
			<-ctx.Done()
			close(firstCanceled)
			return nil, false
		}
		return originalRead(ctx, filename)
	}
	getReadCalls := func() int {
		mu.Lock()
		defer mu.Unlock()
		return readCalls
	}

	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	root := useRealCodeWorkspace(t, h, map[string]string{"source.txt": "first needle version\n"})
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("the first index build did not start reading files")
	}
	if !h.app.code.searchIndex.busy {
		t.Fatal("the index state does not reflect its in-flight build")
	}

	// A new file version cancels the obsolete read immediately and schedules
	// its replacement instead of starting it inline with the save.
	writeWorkspaceSearchTestFile(t, root, "source.txt", "second needle version\n")
	h.app.invalidateCodeSearchIndex()
	select {
	case <-firstCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("file changes did not cancel the obsolete staging build")
	}
	if len(delays) != 1 || delays[0] != codeSearchIndexRebuildDelay {
		t.Fatalf("rebuild delays = %v, want one %s debounce", delays, codeSearchIndexRebuildDelay)
	}
	if !h.app.code.searchIndex.rebuildScheduled || h.app.code.searchIndex.status != codeSearchIndexStale {
		t.Fatalf("invalidation left the old index available: %+v", h.app.code.searchIndex)
	}
	h.waitFor(t, "obsolete build retirement", func() bool { return !h.app.code.searchIndex.busy })
	if got := getReadCalls(); got != 1 {
		t.Fatalf("a new build started before its debounce: %d reads", got)
	}
	rebuilds[0]()
	h.updates.waitForUpdate(t)
	h.waitFor(t, "debounced replacement build", func() bool {
		state := h.app.code.searchIndex
		return !state.busy && state.status == codeSearchIndexReady
	})
	if got := getReadCalls(); got != 2 {
		t.Fatalf("replacement build read files %d times, want once", got-1)
	}

	// Three notifications share one timer. The superseded callbacks cannot
	// start a build even if a test or scheduler fires them later.
	before := len(rebuilds)
	for version := 3; version <= 5; version++ {
		writeWorkspaceSearchTestFile(t, root, "source.txt", fmt.Sprintf("version %d needle\n", version))
		h.app.invalidateCodeSearchIndex()
	}
	if len(rebuilds) != before+3 {
		t.Fatalf("three changes scheduled %d callbacks, want %d", len(rebuilds), before+3)
	}
	rebuilds[before]()
	h.updates.waitForUpdate(t)
	rebuilds[before+1]()
	h.updates.waitForUpdate(t)
	if got := getReadCalls(); got != 2 {
		t.Fatalf("a superseded debounce callback started work: %d reads", got)
	}
	rebuilds[before+2]()
	h.updates.waitForUpdate(t)
	h.waitFor(t, "the one coalesced rebuild", func() bool {
		state := h.app.code.searchIndex
		return !state.busy && state.status == codeSearchIndexReady
	})
	if got := getReadCalls(); got != 3 {
		t.Fatalf("coalesced changes caused %d builds, want one", got-2)
	}
}

func TestCodeSearchIndexPeriodicRefreshRebuildsStaleIndex(t *testing.T) {
	originalSchedule := scheduleCodeSearchIndexRefresh
	t.Cleanup(func() { scheduleCodeSearchIndexRefresh = originalSchedule })
	var intervals []time.Duration
	var refreshes []func()
	scheduleCodeSearchIndexRefresh = func(delay time.Duration, run func()) func() bool {
		intervals = append(intervals, delay)
		refreshes = append(refreshes, run)
		return nil
	}

	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	root := useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\n"})
	h.waitFor(t, "automatic background index build", func() bool {
		state := h.app.code.searchIndex
		return state.status == codeSearchIndexReady && !state.busy
	})
	// The first build advances the index generation, so its initial timer is
	// superseded by the current generation's timer. Every timer uses the same
	// periodic interval; only the newest callback can affect the live state.
	if len(refreshes) == 0 {
		t.Fatal("periodic refresh was not scheduled")
	}
	for _, interval := range intervals {
		if interval != codeSearchIndexRefreshInterval {
			t.Fatalf("periodic refresh intervals = %v, want %s", intervals, codeSearchIndexRefreshInterval)
		}
	}
	updated := h.app.code.searchIndex.updated
	scheduledBefore := len(intervals)

	// An outside editor rewrites the file; the index does not know yet.
	writeWorkspaceSearchTestFile(t, root, "source.txt", "needle rewritten by another editor\n")

	// Fire the current generation's periodic freshness check the way the
	// 30 second timer would.
	refreshes[len(refreshes)-1]()
	h.updates.waitForUpdate(t)
	h.waitFor(t, "periodic rebuild after the file changed", func() bool {
		current := h.app.code.searchIndex
		return !current.busy && current.status == codeSearchIndexReady && current.updated.After(updated)
	})
	if len(intervals) <= scheduledBefore || intervals[len(intervals)-1] != codeSearchIndexRefreshInterval {
		t.Fatalf("periodic refresh did not re-arm itself: %v", intervals)
	}
}

func TestCodeSearchIndexUnchangedRefreshDoesNotRepaint(t *testing.T) {
	originalSchedule := scheduleCodeSearchIndexRefresh
	t.Cleanup(func() { scheduleCodeSearchIndexRefresh = originalSchedule })
	var refreshes []func()
	scheduleCodeSearchIndexRefresh = func(_ time.Duration, run func()) func() bool {
		refreshes = append(refreshes, run)
		return nil
	}

	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	useRealCodeWorkspace(t, h, map[string]string{"source.txt": "unchanged needle\n"})
	h.waitFor(t, "the initial index", func() bool {
		state := h.app.code.searchIndex
		return state.status == codeSearchIndexReady && !state.busy
	})
	if len(refreshes) == 0 {
		t.Fatal("periodic refresh was not scheduled")
	}
	h.app.workspaceTab = sectionCode
	acknowledged := make(chan bool, 4)
	h.app.updateChanged = func(fn func() bool) { acknowledged <- fn() }

	refreshes[len(refreshes)-1]()
	if <-acknowledged {
		t.Fatal("the periodic timer callback itself requested a repaint")
	}
	if <-acknowledged {
		t.Fatal("an unchanged index inspection requested a repaint")
	}
	if state := h.app.code.searchIndex; state.status != codeSearchIndexReady || state.busy || state.err != "" {
		t.Fatalf("the unchanged index did not remain ready: %+v", state)
	}
}

func TestCodeSearchIndexPeriodicRefreshPausesForRemoteWorkspaces(t *testing.T) {
	originalSchedule := scheduleCodeSearchIndexRefresh
	t.Cleanup(func() { scheduleCodeSearchIndexRefresh = originalSchedule })
	var intervals []time.Duration
	var refresh func()
	scheduleCodeSearchIndexRefresh = func(delay time.Duration, run func()) func() bool {
		intervals = append(intervals, delay)
		refresh = run
		return nil
	}

	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\n"})
	h.waitFor(t, "automatic background index build", func() bool {
		state := h.app.code.searchIndex
		return state.status == codeSearchIndexReady && !state.busy
	})
	if len(intervals) == 0 || refresh == nil {
		t.Fatalf("periodic refresh was not scheduled: %v", intervals)
	}
	scheduledBefore := len(intervals)

	// While the workspace is remote the local index must not be touched, and
	// the timer must not re-arm itself into a remote session.
	h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/repo"}
	refresh()
	h.updates.waitForUpdate(t)
	if h.app.code.searchIndex.busy {
		t.Fatal("the periodic check rebuilt the index while the workspace is remote")
	}
	if len(intervals) != scheduledBefore {
		t.Fatalf("the periodic refresh re-armed while remote: %v", intervals)
	}
	if h.app.code.searchIndex.refreshScheduled {
		t.Fatal("the periodic timer slot stayed occupied while remote")
	}

	// Returning to the local workspace re-arms the periodic check.
	h.app.location = workspace.Location{Kind: workspace.Local, Root: h.app.snapshot.Root}
	h.app.ensureCodeSearchIndex()
	if len(intervals) != scheduledBefore+1 || intervals[len(intervals)-1] != codeSearchIndexRefreshInterval {
		t.Fatalf("returning to the local workspace did not re-arm the periodic check: %v", intervals)
	}
	h.waitFor(t, "the re-armed freshness check settles", func() bool {
		return !h.app.code.searchIndex.busy
	})
}

func TestCodeSearchResumeOverridesPendingRebuildDebounce(t *testing.T) {
	originalSchedule := scheduleCodeSearchIndexRebuild
	t.Cleanup(func() { scheduleCodeSearchIndexRebuild = originalSchedule })
	var pending []func()
	scheduleCodeSearchIndexRebuild = func(_ time.Duration, run func()) {
		pending = append(pending, run)
	}
	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\n"})
	h.waitFor(t, "automatic background index build", func() bool {
		state := h.app.code.searchIndex
		return state.status == codeSearchIndexReady && !state.busy
	})

	h.app.invalidateCodeSearchIndex()
	if len(pending) != 1 || !h.app.code.searchIndex.rebuildScheduled {
		t.Fatal("invalidation did not create a pending rebuild")
	}
	h.app.resumeCodeSearchIndex()
	if !h.app.code.searchIndex.busy || h.app.code.searchIndex.rebuildScheduled {
		t.Fatalf("activation waited for the debounce: %+v", h.app.code.searchIndex)
	}
	h.waitFor(t, "activation-driven freshness check", func() bool {
		return !h.app.code.searchIndex.busy && h.app.code.searchIndex.status == codeSearchIndexReady
	})
	pending[0]()
	h.updates.waitForUpdate(t)
	if h.app.code.searchIndex.busy {
		t.Fatal("the superseded debounce restarted work after activation")
	}
}

func TestCodeSearchIndexBackgroundBackoffAndImmediateResume(t *testing.T) {
	originalSchedule := scheduleCodeSearchIndexRefresh
	t.Cleanup(func() { scheduleCodeSearchIndexRefresh = originalSchedule })
	var intervals []time.Duration
	var latestRefresh func()
	scheduleCodeSearchIndexRefresh = func(delay time.Duration, run func()) func() bool {
		intervals = append(intervals, delay)
		latestRefresh = run
		return func() bool { return true }
	}

	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	active := true
	h.app.code.searchIndexActive = func() bool { return active }
	root := useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\n"})
	h.waitFor(t, "automatic background index build", func() bool {
		state := h.app.code.searchIndex
		return state.status == codeSearchIndexReady && !state.busy
	})
	if latestRefresh == nil || intervals[len(intervals)-1] != codeSearchIndexRefreshInterval {
		t.Fatalf("active index refresh interval = %v", intervals)
	}
	previous := h.app.code.searchIndex.updated

	// While inactive, a periodic tick only re-arms the slower timer. It does
	// not walk the changed source tree or start a rebuild.
	active = false
	writeWorkspaceSearchTestFile(t, root, "source.txt", "written while the window is inactive\n")
	backgroundTicks := intervals
	latestRefresh()
	h.updates.waitForUpdate(t)
	if !h.app.code.searchIndex.updated.Equal(previous) || h.app.code.searchIndex.busy {
		t.Fatalf("inactive polling inspected or rebuilt the index: %+v", h.app.code.searchIndex)
	}
	if len(intervals) != len(backgroundTicks)+1 || intervals[len(intervals)-1] != codeSearchIndexBackgroundRefreshInterval {
		t.Fatalf("inactive polling did not back off: %v", intervals)
	}
	backgroundTicks = append([]time.Duration(nil), intervals...)

	// Activation revalidates immediately, discovers the outside edit and
	// restores the active interval instead of waiting for the slow timer.
	active = true
	h.app.resumeCodeSearchIndex()
	h.waitFor(t, "immediate index validation after activation", func() bool {
		current := h.app.code.searchIndex
		return !current.busy && current.status == codeSearchIndexReady && current.updated.After(previous)
	})
	if len(intervals) <= len(backgroundTicks) || intervals[len(intervals)-1] != codeSearchIndexRefreshInterval {
		t.Fatalf("activation did not restore active polling: %v", intervals)
	}
}

func TestCodeSearchIndexPeriodicRefreshQueuesBehindABuild(t *testing.T) {
	originalSchedule := scheduleCodeSearchIndexRefresh
	t.Cleanup(func() { scheduleCodeSearchIndexRefresh = originalSchedule })
	var refresh func()
	scheduleCodeSearchIndexRefresh = func(_ time.Duration, run func()) func() bool {
		refresh = run
		return nil
	}

	h := menuTestApp(t)
	h.app.pathCache = t.TempDir()
	useRealCodeWorkspace(t, h, map[string]string{"source.txt": "needle\n"})
	h.waitFor(t, "automatic background index build", func() bool {
		state := h.app.code.searchIndex
		return state.status == codeSearchIndexReady && !state.busy
	})
	if refresh == nil {
		t.Fatal("periodic refresh was not scheduled")
	}

	// A build is in flight when the timer fires: the check is queued behind
	// it instead of overlapping a second build.
	h.app.code.searchIndex.busy = true
	refresh()
	h.updates.waitForUpdate(t)
	if !h.app.code.searchIndex.refreshQueued {
		t.Fatal("periodic refresh did not queue behind the in-flight build")
	}
	h.app.code.searchIndex.busy = false
	h.app.code.searchIndex.refreshQueued = false
}
