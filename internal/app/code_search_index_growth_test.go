package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reopening the same unchanged repository must not accumulate Zoekt shards.
func makeZoektGrowthRepository(t *testing.T) (string, []string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := strings.Repeat("package sample\nfunc RepeatedSearchTerm() string { return \"needle\" }\n", 80)
	if err := os.WriteFile(filepath.Join(root, "pkg", "sample.go"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, []string{"pkg/sample.go"}
}

// Reopening the same unchanged repository must not accumulate Zoekt shards.
func TestRepeatedZoektInspectionDoesNotGrowDiskUsage(t *testing.T) {
	root, paths := makeZoektGrowthRepository(t)
	directory := filepath.Join(t.TempDir(), "index")
	ctx := context.Background()

	var sizes []int64
	var statuses []codeSearchIndexStatus
	for generation := 0; generation < 6; generation++ {
		info, err := inspectCodeSearchIndex(ctx, root, paths, nil, directory)
		if err != nil {
			t.Fatalf("generation %d inspect: %v", generation, err)
		}
		if info.status == codeSearchIndexMissing || info.status == codeSearchIndexStale {
			info, err = buildCodeSearchIndex(ctx, root, paths, nil, directory)
			if err != nil {
				t.Fatalf("generation %d build: %v", generation, err)
			}
		}
		if info.status != codeSearchIndexReady {
			t.Fatalf("generation %d status = %v; info=%+v", generation, info.status, info)
		}
		statuses = append(statuses, info.status)
		sizes = append(sizes, codeSearchIndexDiskBytes(directory))
	}
	t.Logf("sizes=%v statuses=%v", sizes, statuses)
	if sizes[len(sizes)-1] > sizes[0] {
		t.Fatalf("unchanged repository index grew from %d to %d bytes across inspections", sizes[0], sizes[len(sizes)-1])
	}
}

func TestCodeSearchIndexReadsLegacyManifest(t *testing.T) {
	root, paths := makeZoektGrowthRepository(t)
	directory := filepath.Join(t.TempDir(), "index")
	ctx := context.Background()
	if _, err := buildCodeSearchIndex(ctx, root, paths, nil, directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(directory, codeSearchIndexManifestFile), filepath.Join(directory, "tangit-index.json")); err != nil {
		t.Fatal(err)
	}
	info, err := inspectCodeSearchIndex(ctx, root, paths, nil, directory)
	if err != nil || info.status != codeSearchIndexReady {
		t.Fatalf("legacy index status = %v, error = %v", info.status, err)
	}
}

func TestForcedZoektRebuildDoesNotAccumulateGenerations(t *testing.T) {
	root, paths := makeZoektGrowthRepository(t)
	directory := filepath.Join(t.TempDir(), "index")
	ctx := context.Background()
	var sizes []int64
	for generation := 0; generation < 6; generation++ {
		info, err := buildCodeSearchIndex(ctx, root, paths, nil, directory)
		if err != nil {
			t.Fatalf("generation %d build: %v", generation, err)
		}
		if info.status != codeSearchIndexReady {
			t.Fatalf("generation %d status = %v; info=%+v", generation, info.status, info)
		}
		sizes = append(sizes, codeSearchIndexDiskBytes(directory))
	}
	entries, _ := os.ReadDir(directory)
	var names []string
	shardCount := 0
	for _, entry := range entries {
		names = append(names, entry.Name())
		if strings.HasPrefix(entry.Name(), "working_") {
			shardCount++
		}
	}
	t.Logf("sizes=%v directory=%v", sizes, names)
	if shardCount != 1 || sizes[len(sizes)-1] > sizes[0]+64 {
		t.Fatalf("forced rebuilds accumulated index data: sizes=%v shards=%d directory=%v", sizes, shardCount, names)
	}
}

func growZoektFixturePastSourceBudget(t *testing.T, root, name string) {
	t.Helper()
	// A small budget keeps the fixture a small file, under the size at which a
	// file is left out of the index altogether.
	previous := codeSearchIndexLimit.Load()
	codeSearchIndexLimit.Store(64 << 10)
	t.Cleanup(func() { codeSearchIndexLimit.Store(previous) })
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte("oversized source fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, codeSearchIndexSourceBytesLimit()+1); err != nil {
		t.Fatal(err)
	}
}

func TestZoektSourceBudgetSkipsIndexWithoutLosingNativeSearch(t *testing.T) {
	root, paths := makeZoektGrowthRepository(t)
	growZoektFixturePastSourceBudget(t, root, "huge.bin")
	paths = append([]string{"huge.bin"}, paths...)
	directory := filepath.Join(t.TempDir(), "index")
	ctx := context.Background()

	info, err := buildCodeSearchIndex(ctx, root, paths, nil, directory)
	if !errors.Is(err, errCodeSearchIndexSourceBudget) || info.status != codeSearchIndexUnavailable {
		t.Fatalf("oversized build = info %+v, err %v", info, err)
	}
	if _, statErr := os.Stat(directory); !os.IsNotExist(statErr) {
		t.Fatalf("oversized repository left index data behind: %v", statErr)
	}
	options := workspaceSearchOptions{Query: "needle", IndexDirectory: directory}
	if _, indexed := searchWorkspaceFilesZoekt(ctx, root, paths, options, 10); indexed {
		t.Fatal("over-budget repository used a partial Zoekt index")
	}
	matches, err := searchWorkspaceFilesGoWithOptions(ctx, root, paths, options, 10)
	if err != nil || len(matches) == 0 {
		t.Fatalf("native search fallback lost matches: matches=%d err=%v", len(matches), err)
	}
}

func TestEnsureCodeSearchIndexRemovesLegacyOverBudgetIndex(t *testing.T) {
	root, originalPaths := makeZoektGrowthRepository(t)
	a := newApp(nil)
	a.pathCache = t.TempDir()
	directory := codeSearchIndexDirectory(a.pathCache, root)
	ctx := context.Background()
	if _, err := buildCodeSearchIndex(ctx, root, originalPaths, nil, directory); err != nil {
		t.Fatal(err)
	}
	growZoektFixturePastSourceBudget(t, root, "huge.bin")
	manifest, err := loadCodeSearchIndexManifest(directory)
	if err != nil {
		t.Fatal(err)
	}
	// Emulate the large manifest published by a release without the budget.
	manifest.SourceBytes = codeSearchIndexSourceBytesLimit() + 1
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, codeSearchIndexManifestFile), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	previousSchedule := scheduleCodeSearchIndexRefresh
	scheduleCodeSearchIndexRefresh = func(time.Duration, func()) func() bool { return func() bool { return true } }
	t.Cleanup(func() { scheduleCodeSearchIndexRefresh = previousSchedule })
	updated := newUpdates()
	a.update = updated.update
	a.snapshot.Root = root
	a.code.filesLoaded = true
	a.code.files = []string{"huge.bin", "pkg/sample.go"}

	a.ensureCodeSearchIndex()
	for attempts := 0; attempts < 4 && (a.code.searchIndex.busy || a.code.searchIndex.status == codeSearchIndexChecking); attempts++ {
		updated.waitForUpdate(t)
	}
	if a.code.searchIndex.status != codeSearchIndexUnavailable {
		t.Fatalf("over-budget status = %v, err=%q", a.code.searchIndex.status, a.code.searchIndex.err)
	}
	if _, statErr := os.Stat(directory); !os.IsNotExist(statErr) {
		t.Fatalf("legacy over-budget directory still occupies disk: %v", statErr)
	}
	// The terminal over-budget state must not rearm a new build every 30s.
	a.ensureCodeSearchIndex()
	if a.code.searchIndex.busy {
		t.Fatal("over-budget repository started another indexing pass")
	}
}

// Big or generated files stay out of the index and are left to the native
// complement; they must still be found, and cost the budget nothing.
func TestZoektIndexLeavesBigAndGeneratedFilesToTheNativeSearch(t *testing.T) {
	if codeSearchIndexSkipsFile("src/app.go", 10<<10) {
		t.Fatal("an ordinary source file is skipped")
	}
	for _, name := range []string{"dist/app.min.js", "web/package-lock.json", "icons/logo.svg", "build/app.js.map"} {
		if !codeSearchIndexSkipsFile(name, 10) {
			t.Errorf("%s should be left out of the index", name)
		}
	}
	if !codeSearchIndexSkipsFile("include/registers.h", codeSearchIndexMaxFileBytes+1) {
		t.Error("a file over the size limit should be left out of the index")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.go"), []byte("package app // findme in an indexed file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bundle.min.js"), []byte("var a = 'findme in a skipped file'"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := []string{"small.go", "bundle.min.js"}
	directory := filepath.Join(t.TempDir(), "index")
	info, err := buildCodeSearchIndex(context.Background(), root, paths, nil, directory)
	if err != nil || info.status != codeSearchIndexReady {
		t.Fatalf("build = %+v, %v", info, err)
	}
	if info.documents != 1 {
		t.Fatalf("indexed %d documents, want only the ordinary file", info.documents)
	}
	matches, ok := searchWorkspaceFilesZoekt(context.Background(), root, paths,
		workspaceSearchOptions{Query: "findme", IndexDirectory: directory}, 10)
	if !ok || len(matches) != 2 {
		t.Fatalf("matches = %+v, ok = %v: the skipped file was not searched", matches, ok)
	}
}
