package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// BenchmarkPrincipalCommitListFrame measures the real app view with 20k
// commits. The distinct-scroll variant is the decision benchmark; the
// same-position control demonstrates why a short, cached frame is not enough.
func BenchmarkPrincipalCommitListFrame(b *testing.B) {
	const commitsN = 20_000
	for _, variant := range []string{"same_position_control", "scroll_distinct"} {
		b.Run(variant, func(b *testing.B) {
			a := newApp(fakeOpener{})
			a.path = "/work/large-repository"
			a.state = stateReady
			a.workspaceTab = sectionHistory
			a.snapshot = repository.Snapshot{Root: a.path, Branch: "main", Commits: make([]repository.Commit, commitsN)}
			a.session = fakeSession{snapshot: a.snapshot}
			a.selectedCommit = -1
			for i := range a.snapshot.Commits {
				a.snapshot.Commits[i] = repository.Commit{
					Hash:          fmt.Sprintf("%040x", i+1),
					ShortHash:     fmt.Sprintf("%07x", i+1),
					Subject:       fmt.Sprintf("A representative commit subject with enough text to shape %05d", i),
					AuthorName:    fmt.Sprintf("Author %02d", i%97),
					AuthorEmail:   fmt.Sprintf("author-%02d@example.invalid", i%97),
					UnixTimestamp: 1_700_000_000 - int64(i)*60,
					LinesAdded:    i%113 + 1,
					LinesDeleted:  i % 37,
				}
			}
			tester := ui.NewTester(a.view, 1280, 1800)
			for range 3 {
				tester.Frame()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				if variant == "scroll_distinct" {
					// Co-prime stride visits thousands of non-adjacent screens instead
					// of hitting one retained short-screen cache repeatedly.
					a.commitList.ScrollTo((i*997+123)%(commitsN-30), ui.Start)
				}
				tester.Frame()
			}
			b.StopTimer()
			first, last := a.commitList.Visible()
			b.ReportMetric(float64(commitsN), "total_commits")
			b.ReportMetric(float64(max(0, last-first+1)), "lazy_visible_rows")
			b.ReportMetric(float64(first), "last_first_row")
			b.ReportMetric(float64(last), "last_last_row")
		})
	}
}

// BenchmarkCommitDiffLongHunk uses a real git-diff patch. GitReadOnly is in
// internal/repository's BenchmarkLargeRepository; these variants isolate
// ParsePatch from ParsePatch+MyGo construction in the app's actual builder.
func BenchmarkCommitDiffLongHunk(b *testing.B) {
	commit, file, diff := newAppLongDiffFixture(b)
	options := repository.DiffOptions{ContextLines: 3}
	b.Logf("real Git long-hunk patch: commit=%s file=%s diff_bytes=%d", commit.ShortHash, file.Path, len(diff.Patch))
	b.Run("ParsePatchOnly", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(diff.Patch)))
		for b.Loop() {
			patches, err := editor.ParsePatch(diff.Patch)
			if err != nil || len(patches) != 1 {
				b.Fatalf("patches=%d err=%v", len(patches), err)
			}
			appBenchPatchSink = patches
		}
	})
	b.Run("BuildDiffState_ParsePlusMyGo", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(diff.Patch)))
		for b.Loop() {
			state, err := buildDiffState(editor.DiffUnified, false, repository.File{Path: file.Path}, diff, options)
			if err != nil || state == nil {
				b.Fatalf("state=%p err=%v", state, err)
			}
			appBenchDiffStateSink = state
		}
		b.ReportMetric(float64(len(appBenchDiffStateSink.Hunks())), "diff_state_hunks")
	})
	b.Run("RepeatedBuildSamePatch", func(b *testing.B) {
		// This models the work the original quiet refresh did before it
		// discovered that the already displayed raw patch had not changed.
		b.ReportAllocs()
		b.SetBytes(int64(len(diff.Patch)))
		for b.Loop() {
			for range 2 {
				state, err := buildDiffState(editor.DiffUnified, false, repository.File{Path: file.Path}, diff, options)
				if err != nil {
					b.Fatal(err)
				}
				appBenchDiffStateSink = state
			}
		}
		b.ReportMetric(2, "builds/op")
	})
	b.Run("QuietRefreshSamePatch_GitExcluded", func(b *testing.B) {
		// The real app path still performs its Session.Diff call; an immediate
		// controlled response excludes Git wall time, which GitReadOnly covers.
		session := &controlledRemoteDiffSession{Session: fakeSession{}, response: diff}
		a := newApp(fakeOpener{})
		a.session = session
		a.workspaceTab = sectionChanges
		a.snapshot.Files = []repository.File{changedFile(file.Path)}
		a.changeRows = a.changeRowsFrom(a.snapshot.Files)
		a.selectedFile = changeRowIndex(a.changeRows, file.Path, false)
		a.diffOptions = options
		published := make(chan bool, 1)
		a.updateChanged = func(fn func() bool) { published <- fn() }
		a.requestFileDiff()
		if !<-published {
			b.Fatal("initial diff did not publish")
		}
		state := a.diffState
		if state == nil || a.diffPatch != diff.Patch {
			b.Fatalf("quiet benchmark could not load its real patch: selected=%d state=%p patch=%d/%d kind=%q error=%q",
				a.selectedFile, state, len(a.diffPatch), len(diff.Patch), a.diffKind, a.diffError)
		}
		b.ReportAllocs()
		// No byte stream is processed on the reuse path: report diff_bytes as
		// context, not a fictitious ParsePatch throughput.
		b.ResetTimer()
		for b.Loop() {
			a.requestQuietFileDiff()
			if <-published {
				b.Fatal("identical quiet patch requested a repaint")
			}
		}
		b.StopTimer()
		if a.diffState != state || a.diffPatch != diff.Patch || a.cancelDiff != nil {
			b.Fatal("quiet refresh changed the displayed state or left work in flight")
		}
		b.ReportMetric(1, "same_state")
		b.ReportMetric(float64(len(diff.Patch)), "diff_bytes")
	})
}

var (
	appBenchPatchSink     []editor.FilePatch
	appBenchDiffStateSink *editor.DiffState
)

func newAppLongDiffFixture(b *testing.B) (repository.Commit, repository.CommitFile, repository.Diff) {
	b.Helper()
	root := os.Getenv("LAZYGITAPP_PERF_REPO")
	if root == "" {
		root = b.TempDir()
		runAppPerfGit(b, root, "init", "--initial-branch=main")
		runAppPerfGit(b, root, "config", "user.name", "Diff Benchmark")
		runAppPerfGit(b, root, "config", "user.email", "diff@example.invalid")
		for version := range 2 {
			writeAppLongDiffFile(b, root, version)
			runAppPerfGit(b, root, "add", "long.go")
			runAppPerfGit(b, root, "commit", "-m", fmt.Sprintf("long version %d", version))
		}
	}
	session, err := (repository.Opener{}).Open(context.Background(), root)
	if err != nil {
		b.Fatal(err)
	}
	commits, err := session.RecentCommits(context.Background(), 1, 0)
	if err != nil || len(commits) != 1 {
		b.Fatalf("commits=%d err=%v", len(commits), err)
	}
	file := repository.CommitFile{Path: "long.go"}
	diff, err := session.CommitFileDiff(context.Background(), commits[0], file, repository.DiffOptions{ContextLines: 3})
	if err != nil || len(diff.Patch) < 100_000 {
		b.Fatalf("diff_bytes=%d err=%v", len(diff.Patch), err)
	}
	return commits[0], file, diff
}

func writeAppLongDiffFile(b *testing.B, root string, version int) {
	b.Helper()
	var content strings.Builder
	content.WriteString("package longfixture\n\n")
	for i := range 8_000 {
		fmt.Fprintf(&content, "const Value%05d = %d // fixture version %d with enough text to measure parsing\n", i, i, version)
	}
	if err := os.WriteFile(filepath.Join(root, "long.go"), []byte(content.String()), 0o644); err != nil {
		b.Fatal(err)
	}
}

func runAppPerfGit(b *testing.B, dir string, args ...string) {
	b.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if output, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
