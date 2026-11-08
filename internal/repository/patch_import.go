package repository

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

const patchPreviewTextLimit = 256 * 1024

// PatchPreview describes the exact patch file contents that were checked.
// Its private fields bind ApplyPatch to those contents, this repository, and
// the repository status observed during the preview.
type PatchPreview struct {
	Path          string
	Text          string
	TextTruncated bool
	Clean         bool
	Diagnostic    string
	Files         []PatchFile
	Additions     int
	Deletions     int

	digest          string
	repositoryRoot  string
	repositoryState string
	sourcePath      string
	patchBytes      []byte
	byteBacked      bool
}

// PatchFile is one path affected by an imported patch. OldPath is populated
// for renames and copies. Binary patches have no textual line counts.
type PatchFile struct {
	Path      string
	OldPath   string
	Additions int
	Deletions int
	Binary    bool
	Summary   string
}

type patchSource struct {
	path        string
	temporary   string
	digest      string
	text        string
	textLimited bool
	patchBytes  []byte
	byteBacked  bool
}

// readPatchSource takes a private, immutable copy for Git to consume. Apart
// from making preview reads consistent, this closes the source-replacement
// race between hashing a patch and asking Git to apply it. The source itself
// is only ever opened for reading.
func readPatchSource(path string) (patchSource, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return patchSource{}, fmt.Errorf("resolve patch path: %w", err)
	}
	source, err := os.Open(absolute)
	if err != nil {
		return patchSource{}, fmt.Errorf("open patch: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return patchSource{}, fmt.Errorf("inspect patch: %w", err)
	}
	if !info.Mode().IsRegular() {
		return patchSource{}, fmt.Errorf("patch is not a regular file: %s", absolute)
	}

	temporary, err := os.CreateTemp("", "tangit-import-*.patch")
	if err != nil {
		return patchSource{}, fmt.Errorf("copy patch for inspection: %w", err)
	}
	result := patchSource{path: absolute, temporary: temporary.Name()}
	keep := false
	defer func() {
		temporary.Close()
		if !keep {
			os.Remove(result.temporary)
		}
	}()

	hash := sha256.New()
	text := &patchTextCollector{limit: patchPreviewTextLimit}
	if _, err := io.Copy(io.MultiWriter(temporary, hash, text), source); err != nil {
		return patchSource{}, fmt.Errorf("read patch: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return patchSource{}, fmt.Errorf("copy patch for inspection: %w", err)
	}
	result.digest = fmt.Sprintf("%x", hash.Sum(nil))
	result.text = string(text.data)
	result.textLimited = text.truncated
	keep = true
	return result, nil
}

// newBytePatchSource owns a private copy of content and a private temporary
// file containing exactly those bytes. It is used both for text previews and
// for reconstructing the approved bytes when an in-memory preview is applied.
func newBytePatchSource(content []byte) (patchSource, error) {
	data := bytes.Clone(content)
	temporary, err := os.CreateTemp("", "tangit-import-*.patch")
	if err != nil {
		return patchSource{}, fmt.Errorf("copy patch for inspection: %w", err)
	}
	result := patchSource{temporary: temporary.Name()}
	keep := false
	defer func() {
		temporary.Close()
		if !keep {
			os.Remove(result.temporary)
		}
	}()
	if _, err := io.Copy(temporary, bytes.NewReader(data)); err != nil {
		return patchSource{}, fmt.Errorf("copy patch for inspection: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return patchSource{}, fmt.Errorf("copy patch for inspection: %w", err)
	}
	text := &patchTextCollector{limit: patchPreviewTextLimit}
	_, _ = text.Write(data)
	result.digest = fmt.Sprintf("%x", sha256.Sum256(data))
	result.text = string(text.data)
	result.textLimited = text.truncated
	result.patchBytes = data
	result.byteBacked = true
	keep = true
	return result, nil
}

func (p patchSource) close() {
	if p.temporary != "" {
		os.Remove(p.temporary)
	}
}

// Bytes returns a complete, independent copy of the approved patch, even when
// Text was truncated for display. In-memory patches are checked against their
// private digest; file-backed previews re-read and verify their original file.
func (p PatchPreview) Bytes() ([]byte, error) {
	if p.digest == "" {
		return nil, fmt.Errorf("read patch bytes: a complete preview is required")
	}
	if p.byteBacked {
		if digest := fmt.Sprintf("%x", sha256.Sum256(p.patchBytes)); digest != p.digest {
			return nil, fmt.Errorf("read patch bytes: patch bytes failed their integrity check")
		}
		return bytes.Clone(p.patchBytes), nil
	}
	if p.sourcePath == "" || p.sourcePath != p.Path {
		return nil, fmt.Errorf("read patch bytes: a complete file-backed preview is required")
	}
	source, err := readPatchSource(p.sourcePath)
	if err != nil {
		return nil, fmt.Errorf("read patch bytes: %w", err)
	}
	defer source.close()
	if source.digest != p.digest {
		return nil, fmt.Errorf("read patch bytes: patch file changed since preview; preview it again")
	}
	data, err := os.ReadFile(source.temporary)
	if err != nil {
		return nil, fmt.Errorf("read patch bytes: %w", err)
	}
	return data, nil
}

type patchTextCollector struct {
	data      []byte
	limit     int
	truncated bool
}

func (c *patchTextCollector) Write(p []byte) (int, error) {
	original := len(p)
	room := c.limit - len(c.data)
	if room < 0 {
		room = 0
	}
	if len(p) > room {
		c.truncated = true
		p = p[:room]
	}
	c.data = append(c.data, p...)
	return original, nil
}

func patchCommandDiagnostic(output string, err error) string {
	if text := strings.TrimSpace(RedactSecrets(output)); text != "" {
		return text
	}
	if err != nil {
		return RedactSecrets(err.Error())
	}
	return "Git could not read the patch"
}

// PreviewPatch inspects a private copy of path with git apply's read-only
// reporting and checking modes. An inapplicable or malformed patch is a
// successful preview with Clean false so the UI can show Git's diagnostic.
func (s *gitSession) PreviewPatch(ctx context.Context, path string) (PatchPreview, error) {
	if err := ctx.Err(); err != nil {
		return PatchPreview{}, err
	}
	source, err := readPatchSource(path)
	if err != nil {
		return PatchPreview{}, err
	}
	defer source.close()

	return s.previewPatchSource(source, PatchPreview{
		Path:           source.path,
		Text:           source.text,
		TextTruncated:  source.textLimited,
		digest:         source.digest,
		repositoryRoot: s.git.RepoPaths.WorktreePath(),
		sourcePath:     source.path,
	})
}

// PreviewPatchText is the in-memory counterpart of PreviewPatch. The approved
// bytes stay private, so changing the public display metadata cannot change a
// later application and a large patch can still be exported in full.
func (s *gitSession) PreviewPatchText(ctx context.Context, text string) (PatchPreview, error) {
	if err := ctx.Err(); err != nil {
		return PatchPreview{}, err
	}
	source, err := newBytePatchSource([]byte(text))
	if err != nil {
		return PatchPreview{}, err
	}
	defer source.close()

	return s.previewPatchSource(source, PatchPreview{
		Text:           source.text,
		TextTruncated:  source.textLimited,
		digest:         source.digest,
		repositoryRoot: s.git.RepoPaths.WorktreePath(),
		patchBytes:     source.patchBytes,
		byteBacked:     true,
	})
}

func (s *gitSession) previewPatchSource(source patchSource, preview PatchPreview) (PatchPreview, error) {
	state, err := s.patchRepositoryState()
	if err != nil {
		return PatchPreview{}, fmt.Errorf("preview patch: %w", err)
	}
	preview.repositoryState = state

	// Successful Git commands can still write warnings to stderr. Keep it out
	// of the NUL-delimited numstat stream (RunWithOutput combines both).
	numstat, numstatStderr, statErr := s.cmd.New([]string{
		"git", "--no-optional-locks", "apply", "--numstat", "-z", source.temporary,
	}).RunWithOutputs()
	if statErr == nil {
		preview.Files, preview.Additions, preview.Deletions, err = parsePatchNumstat(numstat)
		if err != nil {
			statErr = fmt.Errorf("read patch statistics: %w", err)
		}
	}

	summary, summaryErr := s.cmd.New([]string{
		"git", "--no-optional-locks", "apply", "--summary", source.temporary,
	}).RunWithOutput()
	if summaryErr == nil && statErr == nil {
		preview.Files = applyPatchSummaries(preview.Files, summary)
		if err := applyPatchHeaderMetadata(preview.Files, source.temporary); err != nil {
			return PatchPreview{}, fmt.Errorf("preview patch metadata: %w", err)
		}
		preview.Additions, preview.Deletions = 0, 0
		for _, file := range preview.Files {
			preview.Additions += file.Additions
			preview.Deletions += file.Deletions
		}
	}

	checkOutput, checkErr := s.cmd.New([]string{
		"git", "--no-optional-locks", "apply", "--check", source.temporary,
	}).RunWithOutput()
	blocked, err := s.patchOperationDiagnostic()
	if err != nil {
		return PatchPreview{}, fmt.Errorf("preview patch: %w", err)
	}

	switch {
	case blocked != "":
		preview.Diagnostic = blocked
	case statErr != nil:
		preview.Diagnostic = patchCommandDiagnostic(numstat+numstatStderr, statErr)
	case summaryErr != nil:
		preview.Diagnostic = patchCommandDiagnostic(summary, summaryErr)
	case checkErr != nil:
		preview.Diagnostic = patchCommandDiagnostic(checkOutput, checkErr)
	default:
		preview.Clean = true
	}
	return preview, nil
}

func parsePatchNumstat(output string) ([]PatchFile, int, int, error) {
	if output == "" {
		return nil, 0, 0, nil
	}
	records := strings.Split(output, "\x00")
	if records[len(records)-1] != "" {
		return nil, 0, 0, fmt.Errorf("unterminated NUL record")
	}
	records = records[:len(records)-1]
	files := make([]PatchFile, 0, len(records))
	additions, deletions := 0, 0
	for index := 0; index < len(records); index++ {
		record := records[index]
		columns := strings.SplitN(record, "\t", 3)
		if len(columns) != 3 {
			return nil, 0, 0, fmt.Errorf("malformed numstat record %q", record)
		}
		file := PatchFile{Path: columns[2]}
		if file.Path == "" {
			// This is the same rename layout as git diff --numstat -z.
			// Current git apply normally emits only the new name instead, but
			// accepting both keeps the parser correct for either producer.
			if index+2 >= len(records) {
				return nil, 0, 0, fmt.Errorf("incomplete rename numstat record")
			}
			file.OldPath = records[index+1]
			file.Path = records[index+2]
			index += 2
		}
		if file.Path == "" {
			return nil, 0, 0, fmt.Errorf("empty path in numstat record")
		}
		file.Binary = columns[0] == "-" || columns[1] == "-"
		var err error
		if columns[0] != "-" {
			file.Additions, err = strconv.Atoi(columns[0])
			if err != nil || file.Additions < 0 {
				return nil, 0, 0, fmt.Errorf("invalid addition count %q", columns[0])
			}
		}
		if columns[1] != "-" {
			file.Deletions, err = strconv.Atoi(columns[1])
			if err != nil || file.Deletions < 0 {
				return nil, 0, 0, fmt.Errorf("invalid deletion count %q", columns[1])
			}
		}
		additions += file.Additions
		deletions += file.Deletions
		files = append(files, file)
	}
	return files, additions, deletions, nil
}

// Summary output supplies create/delete and mode-only records, including
// empty-file changes for which numstat has no row. Rename paths are read from
// the unambiguous extended patch headers below rather than Git's compact
// brace-and-arrow display syntax.
func applyPatchSummaries(files []PatchFile, output string) []PatchFile {
	byPath := make(map[string]int, len(files))
	for index := range files {
		byPath[files[index].Path] = index
	}
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(strings.TrimPrefix(line, " "), " ", 4)
		if len(fields) != 4 {
			continue
		}
		kind := fields[0] + " " + fields[1]
		if kind != "create mode" && kind != "delete mode" && kind != "mode change" {
			continue
		}
		path := decodeGitPatchPath(fields[3])
		if path == "" {
			continue
		}
		if index, ok := byPath[path]; ok {
			files[index].Summary = strings.TrimSpace(line)
			continue
		}
		byPath[path] = len(files)
		files = append(files, PatchFile{Path: path, Summary: strings.TrimSpace(line)})
	}
	return files
}

type patchHeader struct {
	path    string
	oldPath string
	action  string
}

func applyPatchHeaderMetadata(files []PatchFile, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	byPath := make(map[string]int, len(files))
	for index := range files {
		byPath[files[index].Path] = index
	}
	current := patchHeader{}
	flush := func() {
		if current.path != "" && current.oldPath != "" {
			if index, ok := byPath[current.path]; ok {
				files[index].OldPath = current.oldPath
				action := current.action
				if action == "" {
					action = "rename"
				}
				files[index].Summary = action + " " + current.oldPath + " => " + current.path
			}
		}
		current = patchHeader{}
	}

	scanner := bufio.NewScanner(file)
	// Binary literals can contain very long lines. Metadata headers are small;
	// if an unusually long literal reaches the limit, numstat and Git's check
	// remain authoritative and no path metadata is guessed.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
		case strings.HasPrefix(line, "rename from "):
			current.oldPath = decodeGitPatchPath(strings.TrimPrefix(line, "rename from "))
			current.action = "rename"
		case strings.HasPrefix(line, "rename to "):
			current.path = decodeGitPatchPath(strings.TrimPrefix(line, "rename to "))
			current.action = "rename"
		case strings.HasPrefix(line, "copy from "):
			current.oldPath = decodeGitPatchPath(strings.TrimPrefix(line, "copy from "))
			current.action = "copy"
		case strings.HasPrefix(line, "copy to "):
			current.path = decodeGitPatchPath(strings.TrimPrefix(line, "copy to "))
			current.action = "copy"
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		// A long binary line is not a metadata error; actual I/O errors are.
		if strings.Contains(err.Error(), "token too long") {
			return nil
		}
		return err
	}
	return nil
}

func decodeGitPatchPath(path string) string {
	if len(path) >= 2 && path[0] == '"' && path[len(path)-1] == '"' {
		if decoded, err := strconv.Unquote(path); err == nil {
			return decoded
		}
	}
	return path
}

func (s *gitSession) patchRepositoryState() (string, error) {
	status, stderr, err := s.cmd.New([]string{
		"git", "--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z",
		"--untracked-files=all", "--",
	}).RunWithOutputs()
	if err != nil {
		return "", fmt.Errorf("read repository state: %w: %s", err, strings.TrimSpace(status+stderr))
	}

	// Porcelain names statuses, not contents. Include exact index/worktree diffs
	// and the bytes of every changed or untracked file so an edit that leaves the
	// same " M" or "??" status still invalidates patch previews and journal Undo.
	base := "HEAD"
	if err := s.cmd.New([]string{"git", "rev-parse", "--verify", "--quiet", "HEAD"}).Run(); err != nil {
		if symbolicErr := s.cmd.New([]string{"git", "symbolic-ref", "--quiet", "HEAD"}).Run(); symbolicErr != nil {
			return "", fmt.Errorf("read repository state: read HEAD: %w", err)
		}
		base = models.EmptyTreeCommitHash
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "status\x00%d\x00", len(status))
	hash.Write([]byte(status))
	for _, args := range [][]string{
		{"git", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--submodule=diff", "--cached", base, "--"},
		{"git", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--submodule=diff", "--"},
	} {
		output, stderr, err := s.cmd.New(args).RunWithOutputs()
		if err != nil {
			return "", fmt.Errorf("read repository content state: %w: %s", err, strings.TrimSpace(output+stderr))
		}
		fmt.Fprintf(hash, "diff\x00%d\x00", len(output))
		hash.Write([]byte(output))
	}

	listing, err := s.cmd.New([]string{
		"git", "ls-files", "--modified", "--others", "--exclude-standard", "-z", "--",
	}).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("list repository content state: %w: %s", err, strings.TrimSpace(listing))
	}
	paths := strings.Split(strings.TrimSuffix(listing, "\x00"), "\x00")
	if listing == "" {
		paths = nil
	}
	sort.Strings(paths)
	if err := s.hashWorktreeFiles(hash, paths); err != nil {
		return "", fmt.Errorf("read repository content state: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (s *gitSession) hashWorktreeFiles(hash io.Writer, paths []string) error {
	for _, path := range paths {
		name, err := worktreeFilePath(s.git.RepoPaths.WorktreePath(), path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "path\x00%s\x00", path)
		info, err := os.Lstat(name)
		if os.IsNotExist(err) {
			hash.Write([]byte("missing\x00"))
			continue
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00", info.Mode())
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "link\x00%d\x00%s\x00", len(target), target)
		case info.Mode().IsRegular():
			content, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			fmt.Fprintf(hash, "file\x00%d\x00", len(content))
			hash.Write(content)
			hash.Write([]byte{0})
		}
	}
	return nil
}

func (s *gitSession) resetRepositoryState(before, after string) (string, error) {
	state, err := s.patchRepositoryState()
	if err != nil {
		return "", err
	}
	ignored, err := s.resetIgnoredPaths(before, after)
	if err != nil || len(ignored) == 0 {
		return state, err
	}
	hash := sha256.New()
	fmt.Fprintf(hash, "reset\x00%s\x00", state)
	if err := s.hashWorktreeFiles(hash, ignored); err != nil {
		return "", fmt.Errorf("read reset content state: %w", err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (s *gitSession) patchOperationDiagnostic() (string, error) {
	state := s.git.Status.WorkingTreeState()
	if state.Any() {
		return fmt.Sprintf("cannot import a patch while %s is in progress", state.Effective().CommandName()), nil
	}
	output, stderr, err := s.cmd.New([]string{
		"git", "--no-optional-locks", "ls-files", "--unmerged", "-z",
	}).RunWithOutputs()
	if err != nil {
		return "", fmt.Errorf("check unresolved conflicts: %w: %s", err, strings.TrimSpace(output+stderr))
	}
	if output != "" {
		return "resolve the repository's unresolved conflicts before importing a patch", nil
	}
	return "", nil
}

func (s *gitSession) checkImportedPatch(source patchSource) error {
	output, err := s.cmd.New([]string{
		"git", "--no-optional-locks", "apply", "--check", source.temporary,
	}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("repository changed since patch preview; preview the patch again: %s", patchCommandDiagnostic(output, err))
	}
	return nil
}

func (s *gitSession) validateImportedPatch(preview PatchPreview, source patchSource) error {
	blocked, err := s.patchOperationDiagnostic()
	if err != nil {
		return err
	}
	if blocked != "" {
		return fmt.Errorf("%s", blocked)
	}
	state, err := s.patchRepositoryState()
	if err != nil {
		return err
	}
	if state != preview.repositoryState {
		return fmt.Errorf("repository changed since patch preview; preview the patch again")
	}
	return s.checkImportedPatch(source)
}

func (s *gitSession) repositoryHasWork() (bool, error) {
	output, stderr, err := s.cmd.New([]string{
		"git", "--no-optional-locks", "status", "--porcelain=v1", "-z",
		"--untracked-files=all", "--",
	}).RunWithOutputs()
	if err != nil {
		return false, fmt.Errorf("check work before recovery capture: %w: %s", err, strings.TrimSpace(output+stderr))
	}
	return output != "", nil
}

// ApplyPatch applies the exact bytes approved by PreviewPatch or
// PreviewPatchText. Ordinary git apply writes only working-tree files; it never
// stages the imported changes.
func (s *gitSession) ApplyPatch(ctx context.Context, preview PatchPreview) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if preview.digest == "" || preview.repositoryRoot == "" || preview.repositoryState == "" ||
		(!preview.byteBacked && preview.sourcePath == "") || (preview.byteBacked && preview.sourcePath != "") {
		return fmt.Errorf("apply patch: a complete PreviewPatch result is required")
	}
	if preview.repositoryRoot != s.git.RepoPaths.WorktreePath() ||
		(!preview.byteBacked && preview.sourcePath != preview.Path) {
		return fmt.Errorf("apply patch: preview belongs to a different repository or patch path")
	}
	if !preview.Clean {
		diagnostic := strings.TrimSpace(preview.Diagnostic)
		if diagnostic == "" {
			diagnostic = "the patch is not applicable"
		}
		return fmt.Errorf("apply patch: %s", diagnostic)
	}

	// Serialize validation through mutation across every session for this
	// worktree, including Change Courier's independently opened destination.
	unlock := s.lockWorktreeMutation()
	defer unlock()

	var source patchSource
	var err error
	if preview.byteBacked {
		source, err = newBytePatchSource(preview.patchBytes)
	} else {
		source, err = readPatchSource(preview.sourcePath)
	}
	if err != nil {
		return fmt.Errorf("apply patch: %w", err)
	}
	defer source.close()
	if source.digest != preview.digest {
		if preview.byteBacked {
			return fmt.Errorf("apply patch: patch bytes failed their integrity check")
		}
		return fmt.Errorf("apply patch: patch file changed since preview; preview it again")
	}
	if err := s.validateImportedPatch(preview, source); err != nil {
		return fmt.Errorf("apply patch: %w", err)
	}

	record, err := s.beginOperation(OperationPatchApply, "Apply patch", UndoReversePatch, nil)
	if err != nil {
		return fmt.Errorf("apply patch: %w", err)
	}
	patchName := filepath.Join(s.journalDir(), record.ID+".patch")
	if err := os.MkdirAll(filepath.Dir(patchName), 0o700); err != nil {
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}
	data, err := os.ReadFile(source.temporary)
	if err != nil {
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}
	temporary, err := os.CreateTemp(s.journalDir(), "recovery-*.patch")
	if err != nil {
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}
	if err := os.Rename(temporaryName, patchName); err != nil {
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}
	if err := syncDirectory(s.journalDir()); err != nil {
		return fmt.Errorf("apply patch: save recovery patch: %w", err)
	}

	hasWork, err := s.repositoryHasWork()
	if err != nil {
		return fmt.Errorf("apply patch: %w", err)
	}
	var snapshot WorkSnapshot
	captured := false
	if hasWork {
		snapshot, err = s.captureWorkSnapshot(ctx, RecoverySnapshotKind, "Before importing patch")
		if err != nil {
			return fmt.Errorf("apply patch: %w", err)
		}
		captured = true
		// Recovery construction runs several Git commands. Close the window
		// between the first check and mutation, and remove our just-created
		// ref if this final preflight fails.
		if err := s.validateImportedPatch(preview, source); err != nil {
			cleanupErr := s.DeleteWorkSnapshot(context.Background(), snapshot)
			if cleanupErr != nil {
				return fmt.Errorf("apply patch: %w (also could not remove unused recovery entry: %v)", err, cleanupErr)
			}
			return fmt.Errorf("apply patch: %w", err)
		}
	}

	output, err := s.cmd.New([]string{
		"git", "--no-optional-locks", "apply", source.temporary,
	}).RunWithOutput()
	if err != nil {
		return s.failJournalOperation(record, fmt.Errorf("apply patch: %w: %s", err, strings.TrimSpace(RedactSecrets(output))))
	}
	if err := s.completeJournalOperation(record, nil); err != nil {
		return err
	}
	if captured {
		if err := s.pruneRecovery(ctx); err != nil {
			return err
		}
	}
	return nil
}
