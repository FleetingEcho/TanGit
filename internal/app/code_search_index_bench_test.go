//go:build linux || darwin || freebsd || netbsd || openbsd || illumos

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// benchmarkCodeSearchIndex creates a fixed repository once. The candidate
// query matches only a few files, making freshness-check and full-scan costs
// easy to compare as the number of repository files grows.
func benchmarkCodeSearchIndex(b *testing.B, fileCount int) (root string, paths []string, directory string) {
	b.Helper()
	root = b.TempDir()
	paths = make([]string, 0, fileCount)
	for index := range fileCount {
		name := fmt.Sprintf("file-%04d.txt", index)
		content := fmt.Sprintf("ordinary source line %d\n", index)
		if index%97 == 0 {
			content += "needle on the matching line\n"
		}
		filename := filepath.Join(root, name)
		if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
		paths = append(paths, name)
	}
	directory = filepath.Join(b.TempDir(), "index")
	info, err := buildCodeSearchIndex(context.Background(), root, paths, nil, directory)
	if err != nil || info.status != codeSearchIndexReady {
		b.Fatalf("build benchmark index: %+v, %v", info, err)
	}
	return root, paths, directory
}

func BenchmarkCodeSearchIndexQueryReuse(b *testing.B) {
	for _, fileCount := range []int{200, 2000} {
		b.Run(fmt.Sprintf("%d_files", fileCount), func(b *testing.B) {
			root, paths, directory := benchmarkCodeSearchIndex(b, fileCount)
			options := workspaceSearchOptions{Query: "needle", CaseSensitive: true, IndexDirectory: directory}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				matches, indexed := searchWorkspaceFilesZoekt(context.Background(), root, paths, options, codeSearchPageSize)
				if !indexed || len(matches) == 0 {
					b.Fatalf("indexed candidate search = %d matches, available=%v", len(matches), indexed)
				}
			}
		})
	}
}

func BenchmarkCodeSearchNativeFullScan(b *testing.B) {
	for _, fileCount := range []int{200, 2000} {
		b.Run(fmt.Sprintf("%d_files", fileCount), func(b *testing.B) {
			root, paths, directory := benchmarkCodeSearchIndex(b, fileCount)
			_ = directory // the native comparison deliberately does not use the index.
			options := workspaceSearchOptions{Query: "needle", CaseSensitive: true}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				matches, err := searchWorkspaceFilesGoWithOptions(context.Background(), root, paths, options, codeSearchPageSize)
				if err != nil || len(matches) == 0 {
					b.Fatalf("native full scan = %d matches, %v", len(matches), err)
				}
			}
		})
	}
}

func BenchmarkCodeSearchIndexFullRebuild(b *testing.B) {
	for _, fileCount := range []int{200, 2000} {
		b.Run(fmt.Sprintf("%d_files", fileCount), func(b *testing.B) {
			root, paths, directory := benchmarkCodeSearchIndex(b, fileCount)
			b.ResetTimer()
			for b.Loop() {
				info, err := buildCodeSearchIndex(context.Background(), root, paths, nil, directory)
				if err != nil || info.status != codeSearchIndexReady {
					b.Fatalf("rebuild benchmark index: %+v, %v", info, err)
				}
			}
		})
	}
}
