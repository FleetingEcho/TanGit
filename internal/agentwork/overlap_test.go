package agentwork

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAnalyzeOverlapsUsesOneDirectBaseToFinalPatch(t *testing.T) {
	// The first patch includes both committed and dirty edits in one direct
	// base-to-final view. The second models an untracked file without special
	// overlap semantics.
	left := OverlapTaskSnapshot{
		TaskID: "task-a", Title: "Combined work", BaseCommit: "base-1",
		Files: []OverlapFileSnapshot{
			{Path: "combined.go", Status: "M", Patch: "@@ -20,2 +20,3 @@ combined\n-old\n+new\n+extra\n"},
			{Path: "new.go", Status: "??", Patch: "@@ -0,0 +1,2 @@\n+package sample\n+\n"},
		},
	}
	right := OverlapTaskSnapshot{
		TaskID: "task-b", Title: "Independent edits", BaseCommit: "base-1",
		Files: []OverlapFileSnapshot{
			{Path: "combined.go", Status: "M", Patch: "@@ -20,2 +20,2 @@ other\n-old\n+other\n"},
			{Path: "new.go", Status: "A", Patch: "@@ -0,0 +1 @@\n+package other\n"},
		},
	}

	report := AnalyzeOverlaps([]OverlapTaskSnapshot{right, left})
	if len(report.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", report.Diagnostics)
	}
	exact := findingsOfKind(report.Findings, OverlapHunkExact)
	if len(exact) != 2 {
		t.Fatalf("exact hunk findings = %#v, want combined and untracked patches", exact)
	}
	if exact[0].Path != "combined.go" || exact[1].Path != "new.go" {
		t.Fatalf("exact finding paths = %q, %q", exact[0].Path, exact[1].Path)
	}
	if exact[0].TaskA.TaskID != "task-a" || exact[0].TaskA.Title != "Combined work" ||
		exact[0].TaskB.TaskID != "task-b" || exact[0].TaskB.Title != "Independent edits" ||
		!exact[0].BaseComparable {
		t.Fatalf("finding lost task or base identity: %#v", exact[0])
	}
	untracked := exact[1]
	if untracked.HunkA == nil || untracked.HunkA.OldStart != 0 || untracked.HunkA.OldCount != 0 ||
		untracked.HunkA.NewStart != 1 || untracked.HunkA.NewCount != 2 {
		t.Fatalf("zero-count untracked hunk was parsed incorrectly: %#v", untracked.HunkA)
	}
}

func TestAnalyzeOverlapsClassifiesSameNearbyAndDisjointHunks(t *testing.T) {
	files := func(nearbyStart int) []OverlapFileSnapshot {
		return []OverlapFileSnapshot{
			{Path: "a-same.go", Status: "M", Patch: "@@ -10,2 +10,2 @@\n"},
			{Path: "b-near.go", Status: "M", Patch: "@@ -40,2 +40,2 @@\n"},
			{Path: "c-disjoint.go", Status: "M", Patch: "@@ -100,2 +100,2 @@\n"},
		}
	}
	left := OverlapTaskSnapshot{TaskID: "a", BaseCommit: "base", Files: files(40)}
	right := OverlapTaskSnapshot{TaskID: "b", BaseCommit: "base"}
	right.Files = []OverlapFileSnapshot{
		{Path: "c-disjoint.go", Status: "M", Patch: "@@ -120,2 +120,2 @@\n"},
		{Path: "b-near.go", Status: "M", Patch: "@@ -44,2 +44,2 @@\n"},
		{Path: "a-same.go", Status: "M", Patch: "@@ -10,2 +11,2 @@\n"},
	}

	report := AnalyzeOverlaps([]OverlapTaskSnapshot{left, right})
	if got := len(findingsOfKind(report.Findings, OverlapSamePath)); got != 3 {
		t.Fatalf("same-path finding count = %d, want 3", got)
	}
	exact := findingsOfKind(report.Findings, OverlapHunkExact)
	if len(exact) != 1 || exact[0].Path != "a-same.go" ||
		exact[0].HunkA.OldCount != 2 || exact[0].HunkB.NewStart != 11 {
		t.Fatalf("same-hunk findings = %#v", exact)
	}
	nearby := findingsOfKind(report.Findings, OverlapHunkNearby)
	if len(nearby) != 1 || nearby[0].Path != "b-near.go" {
		t.Fatalf("nearby-hunk findings = %#v", nearby)
	}
	for _, finding := range report.Findings {
		if finding.Path == "c-disjoint.go" && (finding.Kind == OverlapHunkExact || finding.Kind == OverlapHunkNearby) {
			t.Fatalf("disjoint hunks produced a risk: %#v", finding)
		}
	}
}

func TestAnalyzeOverlapsTreatsDefaultCountsAndZeroCountRanges(t *testing.T) {
	tests := []struct {
		name               string
		patch              string
		oldStart, oldCount int64
		newStart, newCount int64
	}{
		{name: "omitted counts", patch: "@@ -8 +9 @@ section\n", oldStart: 8, oldCount: 1, newStart: 9, newCount: 1},
		{name: "addition", patch: "@@ -0,0 +1,2 @@\n", oldStart: 0, oldCount: 0, newStart: 1, newCount: 2},
		{name: "deletion", patch: "@@ -8,2 +0,0 @@\n", oldStart: 8, oldCount: 2, newStart: 0, newCount: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tasks := []OverlapTaskSnapshot{
				{TaskID: "a", BaseCommit: "base", Files: []OverlapFileSnapshot{{Path: "range.txt", Status: "M", Patch: test.patch}}},
				{TaskID: "b", BaseCommit: "base", Files: []OverlapFileSnapshot{{Path: "range.txt", Status: "M", Patch: test.patch}}},
			}
			report := AnalyzeOverlaps(tasks)
			exact := findingsOfKind(report.Findings, OverlapHunkExact)
			if len(exact) != 1 {
				t.Fatalf("exact findings = %#v; diagnostics = %#v", exact, report.Diagnostics)
			}
			hunk := exact[0].HunkA
			if hunk.OldStart != test.oldStart || hunk.OldCount != test.oldCount ||
				hunk.NewStart != test.newStart || hunk.NewCount != test.newCount || hunk.Header != strings.TrimSuffix(test.patch, "\n") {
				t.Fatalf("hunk bounds = %#v", hunk)
			}
		})
	}
}

func TestAnalyzeOverlapsDoesNotCompareCoordinatesAcrossBases(t *testing.T) {
	patch := "@@ -50,3 +50,3 @@ identical coordinates\n"
	report := AnalyzeOverlaps([]OverlapTaskSnapshot{
		{TaskID: "b", Title: "Second", BaseCommit: "base-b", Files: []OverlapFileSnapshot{{Path: "same.go", Status: "M", Patch: patch}}},
		{TaskID: "a", Title: "First", BaseCommit: "base-a", Files: []OverlapFileSnapshot{{Path: "same.go", Status: "M", Patch: patch}}},
	})
	if len(report.Findings) != 1 || report.Findings[0].Kind != OverlapSamePath {
		t.Fatalf("different-base findings = %#v, want only same-path review", report.Findings)
	}
	finding := report.Findings[0]
	if finding.BaseComparable || finding.TaskA.BaseCommit != "base-a" || finding.TaskB.BaseCommit != "base-b" {
		t.Fatalf("different bases were not preserved as incomparable: %#v", finding)
	}
}

func TestAnalyzeOverlapsReportsDeleteModifyWithoutCallingItAConflict(t *testing.T) {
	report := AnalyzeOverlaps([]OverlapTaskSnapshot{
		{TaskID: "a", BaseCommit: "base", Files: []OverlapFileSnapshot{{Path: "remove.go", Status: "D", Patch: "@@ -1,2 +0,0 @@\n"}}},
		{TaskID: "b", BaseCommit: "base", Files: []OverlapFileSnapshot{{Path: "remove.go", Status: "M", Patch: "@@ -1,2 +1,2 @@\n"}}},
	})
	if len(report.Findings) != 2 || report.Findings[0].Kind != OverlapDeleteModify || report.Findings[1].Kind != OverlapSamePath {
		t.Fatalf("delete/modify findings = %#v", report.Findings)
	}
	if report.Findings[0].FileA.Status != "D" || report.Findings[0].FileB.Status != "M" {
		t.Fatalf("delete/modify file details = %#v", report.Findings[0])
	}

	bothDelete := AnalyzeOverlaps([]OverlapTaskSnapshot{
		{TaskID: "a", BaseCommit: "base", Files: []OverlapFileSnapshot{{Path: "same.go", Status: "D"}}},
		{TaskID: "b", BaseCommit: "base", Files: []OverlapFileSnapshot{{Path: "same.go", Status: "D"}}},
	})
	if len(bothDelete.Findings) != 1 || bothDelete.Findings[0].Kind != OverlapSamePath {
		t.Fatalf("delete/delete findings = %#v", bothDelete.Findings)
	}
}

func TestAnalyzeOverlapsMatchesBothRenamePathIdentities(t *testing.T) {
	report := AnalyzeOverlaps([]OverlapTaskSnapshot{
		{
			TaskID: "a", BaseCommit: "base",
			Files: []OverlapFileSnapshot{{Path: "docs/new.md", OldPath: "docs/old.md", Status: "R100"}},
		},
		{
			TaskID: "b", BaseCommit: "base",
			Files: []OverlapFileSnapshot{
				{Path: "docs/new.md", Status: "M"},
				{Path: "docs/old.md", Status: "M"},
			},
		},
	})
	shared := findingsOfKind(report.Findings, OverlapSamePath)
	if len(shared) != 2 || shared[0].Path != "docs/new.md" || shared[1].Path != "docs/old.md" {
		t.Fatalf("rename identity findings = %#v", shared)
	}
	for _, finding := range shared {
		if finding.FileA.OldPath != "docs/old.md" || finding.FileA.Path != "docs/new.md" {
			t.Fatalf("rename file detail = %#v", finding)
		}
	}
}

func TestAnalyzeOverlapsRetainsValidHunksAfterMalformedHeaders(t *testing.T) {
	valid := "@@ -30,2 +30,2 @@ usable"
	malformed := strings.Join([]string{
		"@@ -1,no +2 @@",
		valid,
		"@@ -999999999999999999999999,2 +1,2 @@",
		"@@@ -1,1 +1,1 @@@",
	}, "\n")
	report := AnalyzeOverlaps([]OverlapTaskSnapshot{
		{TaskID: "a", Title: "Partially parsed", BaseCommit: "base", Files: []OverlapFileSnapshot{{Path: "partial.go", Status: "M", Patch: malformed}}},
		{TaskID: "b", BaseCommit: "base", Files: []OverlapFileSnapshot{{Path: "partial.go", Status: "M", Patch: valid}}},
	})
	if len(report.Diagnostics) != 3 {
		t.Fatalf("diagnostics = %#v, want three bounded parse errors", report.Diagnostics)
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.TaskID != "a" || diagnostic.Path != "partial.go" || diagnostic.Line == 0 ||
			!strings.Contains(diagnostic.Message, "hunk header") {
			t.Fatalf("unhelpful diagnostic: %#v", diagnostic)
		}
	}
	exact := findingsOfKind(report.Findings, OverlapHunkExact)
	if len(exact) != 1 || exact[0].HunkA.Header != valid {
		t.Fatalf("valid hunk was discarded with malformed headers: %#v", report.Findings)
	}
}

func TestAnalyzeOverlapsReportsGeneratedAndMigrationPathHints(t *testing.T) {
	files := []OverlapFileSnapshot{
		{Path: "api/client.gen.go", Status: "M"},
		{Path: "db/migrations/001_initial.sql", Status: "A"},
	}
	otherFiles := slices.Clone(files)
	report := AnalyzeOverlaps([]OverlapTaskSnapshot{
		{TaskID: "a", BaseCommit: "base", Files: files},
		{TaskID: "b", BaseCommit: "base", Files: otherFiles},
	})
	if len(report.Findings) != 4 {
		t.Fatalf("path hint findings = %#v, want same-path and one hint per file", report.Findings)
	}
	generated := findingsOfKind(report.Findings, OverlapGeneratedFile)
	migration := findingsOfKind(report.Findings, OverlapMigrationFile)
	if len(generated) != 1 || generated[0].Path != "api/client.gen.go" {
		t.Fatalf("generated-file hints = %#v", generated)
	}
	if len(migration) != 1 || migration[0].Path != "db/migrations/001_initial.sql" {
		t.Fatalf("migration-file hints = %#v", migration)
	}
}

func TestAnalyzeOverlapsIsDeterministicAndDoesNotMutateInput(t *testing.T) {
	original := []OverlapTaskSnapshot{
		{TaskID: "z", Title: "Last ID", BaseCommit: "same", Files: []OverlapFileSnapshot{
			{Path: "z.go", Status: "M", Patch: "@@ -80 +80 @@\n"},
			{Path: "a.go", Status: "M", Patch: "@@ -10 +10 @@\n"},
		}},
		{TaskID: "a", Title: "First ID", BaseCommit: "same", Files: []OverlapFileSnapshot{
			{Path: "a.go", Status: "M", Patch: "@@ -11 +11 @@\n"},
			{Path: "z.go", Status: "M", Patch: "@@ -80 +80 @@\n"},
		}},
	}
	before := slices.Clone(original)
	before[0].Files = slices.Clone(original[0].Files)
	before[1].Files = slices.Clone(original[1].Files)
	first := AnalyzeOverlaps(original)
	if !reflect.DeepEqual(original, before) {
		t.Fatalf("analysis mutated its input:\n got %#v\nwant %#v", original, before)
	}

	reversed := []OverlapTaskSnapshot{before[1], before[0]}
	reversed[0].Files = slices.Clone(reversed[0].Files)
	reversed[1].Files = slices.Clone(reversed[1].Files)
	slices.Reverse(reversed[0].Files)
	slices.Reverse(reversed[1].Files)
	second := AnalyzeOverlaps(reversed)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("analysis depends on input order:\nfirst %#v\nsecond %#v", first, second)
	}
	for index, finding := range first.Findings {
		if finding.TaskA.TaskID != "a" || finding.TaskB.TaskID != "z" {
			t.Fatalf("finding %d task pair is not canonical: %#v", index, finding)
		}
		if index > 0 && compareFindings(first.Findings[index-1], finding) > 0 {
			t.Fatalf("findings are not sorted at %d: %#v", index, first.Findings)
		}
	}
}

func findingsOfKind(findings []OverlapFinding, kind OverlapKind) []OverlapFinding {
	var result []OverlapFinding
	for _, finding := range findings {
		if finding.Kind == kind {
			result = append(result, finding)
		}
	}
	return result
}
