package agentwork

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// NearbyHunkLineGap is the largest number of unchanged base lines between two
// hunks that still produces a nearby-hunk finding. It is a review heuristic,
// not a prediction that applying either patch will fail.
const NearbyHunkLineGap = 3

// OverlapKind identifies why a pair of task results deserves review.
type OverlapKind string

const (
	OverlapSamePath      OverlapKind = "same_path"
	OverlapDeleteModify  OverlapKind = "delete_modify"
	OverlapHunkExact     OverlapKind = "hunk_exact"
	OverlapHunkNearby    OverlapKind = "hunk_nearby"
	OverlapGeneratedFile OverlapKind = "generated_file"
	OverlapMigrationFile OverlapKind = "migration_file"
)

// OverlapTaskSnapshot is a task's complete BaseCommit-to-final-result view.
// Committed and uncommitted work must already be combined into Files by the
// caller. TaskID must uniquely identify the task within the input. The input
// must contain tasks from only one repository.
type OverlapTaskSnapshot struct {
	TaskID     string
	Title      string
	BaseCommit string
	Files      []OverlapFileSnapshot
}

// OverlapFileSnapshot describes one direct BaseCommit-to-final-result change.
// OldPath is populated for renames and copies. Status is the change status
// (for example M, A, D, R100, or ??); an untracked patch needs no special case.
// Patch may contain metadata and multiple unified-diff hunks for this file.
type OverlapFileSnapshot struct {
	Path    string
	OldPath string
	Status  string
	Patch   string
}

// OverlapTaskRef preserves the task identity and base used for a finding.
type OverlapTaskRef struct {
	TaskID     string
	Title      string
	BaseCommit string
}

// OverlapFileRef identifies the file metadata behind a finding.
type OverlapFileRef struct {
	Path    string
	OldPath string
	Status  string
}

// OverlapHunk contains the parsed bounds from a unified-diff hunk header.
// Old coordinates refer to BaseCommit and New coordinates refer to the final
// result. A zero count represents an insertion or deletion position, not a
// one-line range.
type OverlapHunk struct {
	Header   string
	OldStart int64
	OldCount int64
	NewStart int64
	NewCount int64
}

// OverlapFinding is a review hint shared by two tasks. A finding never means
// that a merge conflict is certain. BaseComparable is true only when both
// tasks have the same non-empty, immutable BaseCommit.
type OverlapFinding struct {
	TaskA          OverlapTaskRef
	TaskB          OverlapTaskRef
	FileA          OverlapFileRef
	FileB          OverlapFileRef
	Path           string
	Kind           OverlapKind
	BaseComparable bool
	HunkA          *OverlapHunk
	HunkB          *OverlapHunk
}

// OverlapDiagnostic records input that could not be fully interpreted. Other
// valid hunks and path-level findings remain available in the report.
type OverlapDiagnostic struct {
	TaskID  string
	Title   string
	Path    string
	Line    int
	Message string
}

// OverlapReport contains deterministic findings and non-fatal parse or input
// diagnostics. Findings sort by task pair, shared path, kind, file metadata,
// and finally hunk bounds.
type OverlapReport struct {
	Findings    []OverlapFinding
	Diagnostics []OverlapDiagnostic
}

type analyzedFile struct {
	snapshot OverlapFileSnapshot
	hunks    []OverlapHunk
	paths    []string
}

type analyzedTask struct {
	snapshot OverlapTaskSnapshot
	files    []analyzedFile
	byPath   map[string][]int
}

// AnalyzeOverlaps compares every distinct task pair without running Git or
// consulting app/UI state. The caller supplies only same-repository tasks.
// Inputs and their slices are not modified. Different-base tasks retain path
// findings, but their hunk coordinates are deliberately never compared.
func AnalyzeOverlaps(tasks []OverlapTaskSnapshot) OverlapReport {
	prepared := make([]analyzedTask, len(tasks))
	report := OverlapReport{}
	seenTaskIDs := make(map[string]bool, len(tasks))
	for index, task := range tasks {
		prepared[index] = analyzeTask(task, &report)
		if seenTaskIDs[task.TaskID] {
			report.Diagnostics = append(report.Diagnostics, OverlapDiagnostic{
				TaskID: task.TaskID, Title: task.Title,
				Message: "duplicate task ID; pair identity may be ambiguous",
			})
		}
		seenTaskIDs[task.TaskID] = true
	}
	slices.SortStableFunc(prepared, compareAnalyzedTasks)

	for left := 0; left < len(prepared); left++ {
		for right := left + 1; right < len(prepared); right++ {
			analyzeTaskPair(prepared[left], prepared[right], &report)
		}
	}

	slices.SortFunc(report.Findings, compareFindings)
	report.Findings = compactFindings(report.Findings)
	slices.SortFunc(report.Diagnostics, compareDiagnostics)
	return report
}

func analyzeTask(task OverlapTaskSnapshot, report *OverlapReport) analyzedTask {
	prepared := analyzedTask{
		snapshot: task,
		files:    make([]analyzedFile, 0, len(task.Files)),
		byPath:   make(map[string][]int),
	}
	if task.BaseCommit == "" {
		report.Diagnostics = append(report.Diagnostics, OverlapDiagnostic{
			TaskID: task.TaskID, Title: task.Title,
			Message: "base commit is empty; hunk coordinates are not comparable",
		})
	}
	for _, file := range task.Files {
		if file.Path == "" {
			report.Diagnostics = append(report.Diagnostics, OverlapDiagnostic{
				TaskID: task.TaskID, Title: task.Title,
				Message: "file with an empty final path was ignored",
			})
			continue
		}
		item := analyzedFile{snapshot: file}
		item.paths = filePaths(file)
		item.hunks = parseUnifiedHunks(file.Patch, func(line int, err error) {
			report.Diagnostics = append(report.Diagnostics, OverlapDiagnostic{
				TaskID: task.TaskID, Title: task.Title, Path: file.Path, Line: line,
				Message: fmt.Sprintf("invalid unified-diff hunk header: %v", err),
			})
		})
		prepared.files = append(prepared.files, item)
	}
	slices.SortFunc(prepared.files, compareAnalyzedFiles)
	for index, file := range prepared.files {
		for _, filePath := range file.paths {
			prepared.byPath[filePath] = append(prepared.byPath[filePath], index)
		}
	}
	return prepared
}

func filePaths(file OverlapFileSnapshot) []string {
	paths := []string{file.Path}
	if file.OldPath != "" && file.OldPath != file.Path {
		paths = append(paths, file.OldPath)
		slices.Sort(paths)
	}
	return paths
}

func compareAnalyzedTasks(left, right analyzedTask) int {
	if compared := strings.Compare(left.snapshot.TaskID, right.snapshot.TaskID); compared != 0 {
		return compared
	}
	if compared := strings.Compare(left.snapshot.Title, right.snapshot.Title); compared != 0 {
		return compared
	}
	if compared := strings.Compare(left.snapshot.BaseCommit, right.snapshot.BaseCommit); compared != 0 {
		return compared
	}
	return slices.CompareFunc(left.files, right.files, compareAnalyzedFiles)
}

func compareAnalyzedFiles(left, right analyzedFile) int {
	if compared := strings.Compare(left.snapshot.Path, right.snapshot.Path); compared != 0 {
		return compared
	}
	if compared := strings.Compare(left.snapshot.OldPath, right.snapshot.OldPath); compared != 0 {
		return compared
	}
	if compared := strings.Compare(left.snapshot.Status, right.snapshot.Status); compared != 0 {
		return compared
	}
	return strings.Compare(left.snapshot.Patch, right.snapshot.Patch)
}

func analyzeTaskPair(left, right analyzedTask, report *OverlapReport) {
	paths := sharedPaths(left.byPath, right.byPath)
	baseComparable := left.snapshot.BaseCommit != "" && left.snapshot.BaseCommit == right.snapshot.BaseCommit
	for _, sharedPath := range paths {
		for _, leftIndex := range left.byPath[sharedPath] {
			for _, rightIndex := range right.byPath[sharedPath] {
				fileA := left.files[leftIndex]
				fileB := right.files[rightIndex]
				finding := OverlapFinding{
					TaskA: taskRef(left.snapshot), TaskB: taskRef(right.snapshot),
					FileA: fileRef(fileA.snapshot), FileB: fileRef(fileB.snapshot),
					Path: sharedPath, BaseComparable: baseComparable,
				}
				finding.Kind = OverlapSamePath
				report.Findings = append(report.Findings, finding)

				deleteModify := isDeletion(fileA.snapshot.Status) != isDeletion(fileB.snapshot.Status)
				if deleteModify {
					finding.Kind = OverlapDeleteModify
					report.Findings = append(report.Findings, finding)
				}
				if generatedFilePath(sharedPath) {
					finding.Kind = OverlapGeneratedFile
					report.Findings = append(report.Findings, finding)
				}
				if migrationFilePath(sharedPath) {
					finding.Kind = OverlapMigrationFile
					report.Findings = append(report.Findings, finding)
				}
				if !baseComparable || deleteModify {
					continue
				}
				appendHunkFindings(finding, fileA.hunks, fileB.hunks, report)
			}
		}
	}
}

func taskRef(task OverlapTaskSnapshot) OverlapTaskRef {
	return OverlapTaskRef{TaskID: task.TaskID, Title: task.Title, BaseCommit: task.BaseCommit}
}

func fileRef(file OverlapFileSnapshot) OverlapFileRef {
	return OverlapFileRef{Path: file.Path, OldPath: file.OldPath, Status: file.Status}
}

func sharedPaths(left, right map[string][]int) []string {
	paths := make([]string, 0)
	for filePath := range left {
		if _, ok := right[filePath]; ok {
			paths = append(paths, filePath)
		}
	}
	slices.Sort(paths)
	return paths
}

func isDeletion(status string) bool {
	status = strings.TrimSpace(status)
	return status != "" && (status[0] == 'D' || status[0] == 'd')
}

func appendHunkFindings(base OverlapFinding, left, right []OverlapHunk, report *OverlapReport) {
	for leftIndex := range left {
		for rightIndex := range right {
			kind, ok := hunkRisk(left[leftIndex], right[rightIndex])
			if !ok {
				continue
			}
			finding := base
			finding.Kind = kind
			hunkA := left[leftIndex]
			hunkB := right[rightIndex]
			finding.HunkA = &hunkA
			finding.HunkB = &hunkB
			report.Findings = append(report.Findings, finding)
		}
	}
}

func hunkRisk(left, right OverlapHunk) (OverlapKind, bool) {
	leftStart, rightStart := uint64(left.OldStart), uint64(right.OldStart)
	leftEnd := leftStart + uint64(left.OldCount)
	rightEnd := rightStart + uint64(right.OldCount)
	leftEmpty, rightEmpty := left.OldCount == 0, right.OldCount == 0

	exact := false
	switch {
	case leftEmpty && rightEmpty:
		exact = leftStart == rightStart
	case leftEmpty:
		exact = leftStart >= rightStart && leftStart < rightEnd
	case rightEmpty:
		exact = rightStart >= leftStart && rightStart < leftEnd
	default:
		exact = leftStart < rightEnd && rightStart < leftEnd
	}
	if exact {
		return OverlapHunkExact, true
	}
	if hunkLineGap(leftStart, leftEnd, rightStart, rightEnd, leftEmpty, rightEmpty) <= NearbyHunkLineGap {
		return OverlapHunkNearby, true
	}
	return "", false
}

func hunkLineGap(leftStart, leftEnd, rightStart, rightEnd uint64, leftEmpty, rightEmpty bool) uint64 {
	switch {
	case leftEmpty && rightEmpty:
		if leftStart < rightStart {
			return rightStart - leftStart
		}
		return leftStart - rightStart
	case leftEmpty:
		return pointRangeGap(leftStart, rightStart, rightEnd)
	case rightEmpty:
		return pointRangeGap(rightStart, leftStart, leftEnd)
	case leftEnd <= rightStart:
		return rightStart - leftEnd
	default:
		return leftStart - rightEnd
	}
}

func pointRangeGap(point, start, end uint64) uint64 {
	if point < start {
		return start - point
	}
	// At end, the point is immediately after the final line in the range.
	return point - end + 1
}

var unifiedHunkHeader = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@(?:[ \t].*)?$`)

func parseUnifiedHunks(patch string, onMalformed func(int, error)) []OverlapHunk {
	lines := strings.Split(patch, "\n")
	hunks := make([]OverlapHunk, 0)
	for index, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if !strings.HasPrefix(line, "@@") {
			continue
		}
		matches := unifiedHunkHeader.FindStringSubmatch(line)
		if matches == nil {
			onMalformed(index+1, fmt.Errorf("expected @@ -old[,count] +new[,count] @@"))
			continue
		}
		values := [4]int64{}
		for group, valueIndex := range []int{1, 3, 2, 4} {
			text := matches[valueIndex]
			if text == "" && (valueIndex == 2 || valueIndex == 4) {
				values[group] = 1
				continue
			}
			value, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				onMalformed(index+1, fmt.Errorf("line number or count is outside the supported range: %w", err))
				values = [4]int64{-1, -1, -1, -1}
				break
			}
			values[group] = value
		}
		if values[0] < 0 {
			continue
		}
		hunks = append(hunks, OverlapHunk{
			Header:   line,
			OldStart: values[0], NewStart: values[1],
			OldCount: values[2], NewCount: values[3],
		})
	}
	return hunks
}

func generatedFilePath(filePath string) bool {
	segments, base := normalizedPathParts(filePath)
	for _, segment := range segments[:len(segments)-1] {
		if segment == "generated" || segment == "__generated__" || segment == "gen" || segment == "dist" {
			return true
		}
	}
	switch base {
	case "go.sum", "cargo.lock", "composer.lock", "gemfile.lock", "package-lock.json",
		"pnpm-lock.yaml", "yarn.lock", "poetry.lock", "pipfile.lock":
		return true
	}
	for _, suffix := range []string{
		".pb.go", ".pb.cc", ".pb.h", ".gen.go", "_gen.go", ".generated.go",
		".generated.js", ".generated.ts", ".generated.tsx", ".min.js", ".min.css",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func migrationFilePath(filePath string) bool {
	segments, base := normalizedPathParts(filePath)
	for _, segment := range segments[:len(segments)-1] {
		if segment == "migration" || segment == "migrations" || segment == "migrate" {
			return true
		}
	}
	return strings.HasPrefix(base, "migration_") || strings.HasPrefix(base, "migrate_") ||
		strings.Contains(base, ".migration.")
}

func normalizedPathParts(filePath string) ([]string, string) {
	cleaned := strings.ReplaceAll(strings.ToLower(filePath), `\`, "/")
	cleaned = path.Clean(cleaned)
	segments := strings.Split(cleaned, "/")
	return segments, segments[len(segments)-1]
}

type findingIdentity struct {
	taskA, taskB       OverlapTaskRef
	fileA, fileB       OverlapFileRef
	path               string
	kind               OverlapKind
	baseComparable     bool
	hunkA, hunkB       OverlapHunk
	hasHunkA, hasHunkB bool
}

func identityOfFinding(finding OverlapFinding) findingIdentity {
	identity := findingIdentity{
		taskA: finding.TaskA, taskB: finding.TaskB,
		fileA: finding.FileA, fileB: finding.FileB,
		path: finding.Path, kind: finding.Kind, baseComparable: finding.BaseComparable,
	}
	if finding.HunkA != nil {
		identity.hunkA, identity.hasHunkA = *finding.HunkA, true
	}
	if finding.HunkB != nil {
		identity.hunkB, identity.hasHunkB = *finding.HunkB, true
	}
	return identity
}

func compactFindings(findings []OverlapFinding) []OverlapFinding {
	if len(findings) == 0 {
		return findings
	}
	result := findings[:1]
	previous := identityOfFinding(findings[0])
	for _, finding := range findings[1:] {
		identity := identityOfFinding(finding)
		if identity == previous {
			continue
		}
		result = append(result, finding)
		previous = identity
	}
	return result
}

func compareFindings(left, right OverlapFinding) int {
	leftIdentity, rightIdentity := identityOfFinding(left), identityOfFinding(right)
	for _, compared := range []int{
		strings.Compare(left.TaskA.TaskID, right.TaskA.TaskID),
		strings.Compare(left.TaskB.TaskID, right.TaskB.TaskID),
		strings.Compare(left.Path, right.Path),
		strings.Compare(string(left.Kind), string(right.Kind)),
		strings.Compare(left.TaskA.Title, right.TaskA.Title),
		strings.Compare(left.TaskB.Title, right.TaskB.Title),
		strings.Compare(left.TaskA.BaseCommit, right.TaskA.BaseCommit),
		strings.Compare(left.TaskB.BaseCommit, right.TaskB.BaseCommit),
		compareFileRefs(left.FileA, right.FileA),
		compareFileRefs(left.FileB, right.FileB),
		compareOptionalHunks(left.HunkA, right.HunkA),
		compareOptionalHunks(left.HunkB, right.HunkB),
	} {
		if compared != 0 {
			return compared
		}
	}
	if leftIdentity.baseComparable != rightIdentity.baseComparable {
		if leftIdentity.baseComparable {
			return 1
		}
		return -1
	}
	return 0
}

func compareFileRefs(left, right OverlapFileRef) int {
	for _, compared := range []int{
		strings.Compare(left.Path, right.Path),
		strings.Compare(left.OldPath, right.OldPath),
		strings.Compare(left.Status, right.Status),
	} {
		if compared != 0 {
			return compared
		}
	}
	return 0
}

func compareOptionalHunks(left, right *OverlapHunk) int {
	if left == nil || right == nil {
		switch {
		case left == nil && right != nil:
			return -1
		case left != nil && right == nil:
			return 1
		default:
			return 0
		}
	}
	return compareHunks(*left, *right)
}

func compareHunks(left, right OverlapHunk) int {
	for _, compared := range []int{
		compareInt64(left.OldStart, right.OldStart),
		compareInt64(left.OldCount, right.OldCount),
		compareInt64(left.NewStart, right.NewStart),
		compareInt64(left.NewCount, right.NewCount),
		strings.Compare(left.Header, right.Header),
	} {
		if compared != 0 {
			return compared
		}
	}
	return 0
}

func compareInt64(left, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareDiagnostics(left, right OverlapDiagnostic) int {
	for _, compared := range []int{
		strings.Compare(left.TaskID, right.TaskID),
		strings.Compare(left.Path, right.Path),
		compareInt64(int64(left.Line), int64(right.Line)),
		strings.Compare(left.Message, right.Message),
		strings.Compare(left.Title, right.Title),
	} {
		if compared != 0 {
			return compared
		}
	}
	return 0
}
