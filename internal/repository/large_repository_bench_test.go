package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

// These benchmarks are opt-in: `go test` never builds the repository. The
// fixture is an ordinary Git worktree with tracked content, real history and
// working-tree edits, not an in-memory Session substitute. Keep all variants
// under one parent so its >10k-file setup is shared and excluded from timings.
const (
	perfFixtureFiles   = 12_000
	perfFixtureHistory = 512
	perfFixtureLines   = 8_000
)

type largeRepositoryFixture struct {
	root       string
	session    FullSession
	rootCommit Commit
	longCommit Commit
	longFile   CommitFile
	longDiff   Diff
	files      int
	history    int
}

func newLargeRepositoryFixture(b *testing.B) *largeRepositoryFixture {
	b.Helper()
	root := os.Getenv("LAZYGITAPP_PERF_REPO")
	if root == "" {
		root = b.TempDir()
		makeLargeGitRepository(b, root, perfFixtureFiles, perfFixtureHistory)
	} else if _, err := os.Stat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		// An explicit path makes the same fixture reusable by package-level
		// profiles and app benchmarks; an implicit temporary path is cleaned up.
		if err := os.MkdirAll(root, 0o755); err != nil {
			b.Fatal(err)
		}
		makeLargeGitRepository(b, root, perfFixtureFiles, perfFixtureHistory)
	} else if output := runPerfGit(b, root, "rev-list", "--count", "--all"); strings.TrimSpace(output) == "" {
		b.Fatal("LAZYGITAPP_PERF_REPO is not a readable Git repository")
	}
	session, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), root))
	if err != nil {
		b.Fatal(err)
	}
	if closer, ok := session.(interface{ Close() error }); ok {
		b.Cleanup(func() { _ = closer.Close() })
	}
	f := &largeRepositoryFixture{root: root, session: session, files: perfFixtureFiles, history: perfFixtureHistory}
	page, err := session.RecentCommits(context.Background(), 1, perfFixtureHistory+1)
	if err != nil || len(page) != 1 {
		b.Fatalf("read root commit: %d commits, %v", len(page), err)
	}
	f.rootCommit = page[0]
	page, err = session.RecentCommits(context.Background(), 1, 0)
	if err != nil || len(page) != 1 {
		b.Fatalf("read long-hunk commit: %d commits, %v", len(page), err)
	}
	f.longCommit = page[0]
	f.longFile = CommitFile{Path: "long.go"}
	f.longDiff, err = session.CommitFileDiff(context.Background(), f.longCommit, f.longFile, DiffOptions{ContextLines: 3})
	if err != nil || len(f.longDiff.Patch) < 100_000 {
		b.Fatalf("read substantial real patch: %d bytes, %v", len(f.longDiff.Patch), err)
	}
	b.Logf("real Git fixture: root=%s files=%d history_commits=%d total_commits=%d patch_bytes=%d",
		root, f.files, f.history, f.history+2, len(f.longDiff.Patch))
	return f
}

func makeLargeGitRepository(b *testing.B, root string, files, history int) {
	b.Helper()
	runPerfGit(b, root, "init", "--initial-branch=main")
	runPerfGit(b, root, "config", "user.name", "Large Repository Benchmark")
	runPerfGit(b, root, "config", "user.email", "perf@example.invalid")
	runPerfGit(b, root, "config", "commit.gpgsign", "false")
	for i := range files {
		path := filepath.Join(root, "tree", fmt.Sprintf("d%03d", i/256), fmt.Sprintf("f%05d.txt", i))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			b.Fatal(err)
		}
		content := fmt.Sprintf("tracked file %d\ncommon line 2\ncommon line 3\n", i)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "history.txt"), []byte("history 0\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	writeLongGoFile(b, root, 0)
	runPerfGit(b, root, "add", "--", ".")
	runPerfGit(b, root, "commit", "-m", "initial 12000-file tree")
	for i := 1; i <= history; i++ {
		content := fmt.Sprintf("history %d\n%s", i, strings.Repeat("stable context\n", i%17))
		if err := os.WriteFile(filepath.Join(root, "history.txt"), []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
		runPerfGit(b, root, "add", "--", "history.txt")
		runPerfGit(b, root, "commit", "-m", fmt.Sprintf("history %04d", i))
	}
	writeLongGoFile(b, root, 1)
	runPerfGit(b, root, "add", "--", "long.go")
	runPerfGit(b, root, "commit", "-m", "rewrite one long hunk")

	// CoreSnapshot must traverse and numstat a genuinely large dirty worktree.
	for i := range files {
		path := filepath.Join(root, "tree", fmt.Sprintf("d%03d", i/256), fmt.Sprintf("f%05d.txt", i))
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := fmt.Fprintln(file, "working-tree addition"); err != nil {
			_ = file.Close()
			b.Fatal(err)
		}
		if err := file.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func writeLongGoFile(b *testing.B, root string, version int) {
	b.Helper()
	var content strings.Builder
	content.WriteString("package longfixture\n\n")
	for i := range perfFixtureLines {
		fmt.Fprintf(&content, "const Value%05d = %d // fixture version %d with enough text to measure parsing\n", i, i, version)
	}
	if err := os.WriteFile(filepath.Join(root, "long.go"), []byte(content.String()), 0o644); err != nil {
		b.Fatal(err)
	}
}

func runPerfGit(b *testing.B, dir string, args ...string) string {
	b.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_DATE=1700000000 +0000",
		"GIT_COMMITTER_DATE=1700000000 +0000",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		b.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func (f *largeRepositoryFixture) clearCommitTotals(b *testing.B) {
	b.Helper()
	s, ok := f.session.(*gitSession)
	if !ok {
		b.Fatal("benchmark requires the real Git session")
	}
	s.commitTotalsMu.Lock()
	s.commitTotals = nil
	s.commitTotalsMu.Unlock()
}

// parseCommitLineStatsWithSplit is the pre-optimization implementation kept
// only in this benchmark. Both variants consume the same real 12k-file Git
// output; this is a component A/B, not a synthetic end-to-end speedup claim.
func parseCommitLineStatsWithSplit(output string) map[string]commitLineStats {
	stats := make(map[string]commitLineStats)
	fields := strings.Split(output, "\x00")
	for i := 0; i < len(fields); {
		columns := strings.SplitN(fields[i], "\t", 3)
		i++
		if len(columns) != 3 {
			continue
		}
		added, addedErr := strconv.Atoi(columns[0])
		deleted, deletedErr := strconv.Atoi(columns[1])
		if addedErr != nil {
			added = 0
		}
		if deletedErr != nil {
			deleted = 0
		}
		path := columns[2]
		if path == "" {
			if i+1 >= len(fields) {
				break
			}
			path = fields[i+1]
			i += 2
		}
		if path != "" {
			stats[path] = commitLineStats{added: added, deleted: deleted}
		}
	}
	return stats
}

var (
	perfSnapshotSink  Snapshot
	perfLineStatsSink map[string]commitLineStats
	perfCommitsSink   []Commit
	perfFilesSink     []CommitFile
	perfDiffSink      Diff
	perfPatchesSink   []editor.FilePatch
	perfStateSink     *editor.DiffState
)

func BenchmarkLargeRepository(b *testing.B) {
	f := newLargeRepositoryFixture(b)
	ctx := context.Background()

	numstat := runPerfGit(b, f.root, "diff", "--no-ext-diff", "--numstat", "-z",
		models.EmptyTreeCommitHash, f.rootCommit.Hash)
	b.Run("ParseNumstat12k", func(b *testing.B) {
		for _, variant := range []string{"split_original", "streaming"} {
			b.Run(variant, func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(numstat)))
				for b.Loop() {
					if variant == "split_original" {
						perfLineStatsSink = parseCommitLineStatsWithSplit(numstat)
					} else {
						perfLineStatsSink = parseCommitLineStats(numstat)
					}
				}
				b.ReportMetric(float64(len(numstat)), "diff_bytes")
				b.ReportMetric(float64(len(perfLineStatsSink)), "files")
			})
		}
	})

	b.Run("CoreSnapshot_git_status_and_12k_numstat", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var err error
			perfSnapshotSink, err = f.session.(ProgressiveSession).CoreSnapshot(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(perfSnapshotSink.Files)), "snapshot_files")
	})

	b.Run("DetailsSnapshot_cold_commit_numstat", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			f.clearCommitTotals(b)
			b.StartTimer()
			var err error
			perfSnapshotSink, err = f.session.(ProgressiveSession).DetailsSnapshot(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(perfSnapshotSink.Commits)), "page_commits")
	})

	for _, skip := range []int{0, 100, 300, 480} {
		b.Run("RecentCommits_warm_totals_skip_"+strconv.Itoa(skip), func(b *testing.B) {
			// Warm the immutable totals cache so this variant measures the Git
			// log/read itself rather than silently benchmarking numstat again.
			if _, err := f.session.RecentCommits(ctx, SnapshotCommitLimit, skip); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				var err error
				perfCommitsSink, err = f.session.RecentCommits(ctx, SnapshotCommitLimit, skip)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(skip), "skip")
			b.ReportMetric(float64(len(perfCommitsSink)), "returned_commits")
		})
	}

	b.Run("CommitFiles_root_12k_name_status_and_numstat", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			var err error
			perfFilesSink, err = f.session.CommitFiles(ctx, f.rootCommit)
			if err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(perfFilesSink)), "commit_files")
	})

	b.Run("LongHunk_GitReadOnly", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(f.longDiff.Patch)))
		for b.Loop() {
			var err error
			perfDiffSink, err = f.session.CommitFileDiff(ctx, f.longCommit, f.longFile, DiffOptions{ContextLines: 3})
			if err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(f.longDiff.Patch)), "diff_bytes")
	})

	patches, err := editor.ParsePatch(f.longDiff.Patch)
	if err != nil || len(patches) != 1 {
		b.Fatalf("parse long fixture: %d patches, %v", len(patches), err)
	}
	b.Run("LongHunk_ParsePatchOnly", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(f.longDiff.Patch)))
		for b.Loop() {
			var err error
			perfPatchesSink, err = editor.ParsePatch(f.longDiff.Patch)
			if err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(perfPatchesSink[0].Hunks)), "parsed_hunks")
	})

	b.Run("LongHunk_MyGoSetPatchOnly", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(f.longDiff.Patch)))
		for b.Loop() {
			state := &editor.DiffState{Layout: editor.DiffUnified, ContextLines: 3, Inline: editor.DiffInlineWordAlt}
			state.SetPatch(patches[0])
			perfStateSink = state
		}
	})

	state := &editor.DiffState{Layout: editor.DiffUnified, ContextLines: 3, Inline: editor.DiffInlineWordAlt}
	state.SetPatch(patches[0])
	b.Run("LongHunk_MyGoScrollingFrame", func(b *testing.B) {
		height := 1800 // deliberately taller than a short screen; thousands remain offscreen
		tester := ui.NewTester(func(c *ui.Context) {
			editor.DiffView(c, state).Fill()
		}, 1000, height)
		for range 3 {
			tester.Frame()
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; b.Loop(); i++ {
			state.SetScrollY(float64(200 + (i%7000)*37))
			tester.Frame()
		}
		b.StopTimer()
		b.ReportMetric(float64(height), "viewport_dip")
		b.ReportMetric(float64(perfFixtureLines), "hunk_lines_per_side")
	})

	// Cancellation is behavioral rather than a throughput target. This variant
	// supplies data for the report without asserting a machine-specific limit.
	b.Run("LongHunk_ParseCancelledContextCost", func(b *testing.B) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		start := time.Now()
		err := cancelled.Err()
		elapsed := time.Since(start)
		if err == nil {
			b.Fatal("cancelled context has no error")
		}
		b.ReportMetric(float64(elapsed.Nanoseconds()), "cancel_check_ns")
	})
}
