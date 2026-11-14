package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func agentReviewRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Agent Review Test")
	runGit(t, dir, "config", "user.email", "agent-review@example.com")
	return dir
}

func openAgentReviewSession(t *testing.T, dir string) Session {
	t.Helper()
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func writeAgentReviewFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func agentReviewFile(t *testing.T, files []CommitFile, path string) CommitFile {
	t.Helper()
	for _, file := range files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("final files do not contain %q: %#v", path, files)
	return CommitFile{}
}

func TestAgentReviewCommitRangeUsesCapturedHeadAndPages(t *testing.T) {
	dir := agentReviewRepository(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "commit", "--allow-empty", "-m", "first review commit")
	first := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "commit", "--allow-empty", "-m", "second review commit")
	capturedHead := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "commit", "--allow-empty", "-m", "later unrelated commit")
	session := openAgentReviewSession(t, dir)

	newest, err := session.CommitRange(context.Background(), base, capturedHead, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(newest) != 1 || newest[0].Hash != capturedHead || newest[0].Subject != "second review commit" {
		t.Fatalf("newest review page = %#v, want captured second commit", newest)
	}
	older, err := session.CommitRange(context.Background(), base, capturedHead, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(older) != 1 || older[0].Hash != first || older[0].Subject != "first review commit" {
		t.Fatalf("older review page = %#v, want first commit", older)
	}
	pastEnd, err := session.CommitRange(context.Background(), base, capturedHead, 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(pastEnd) != 0 {
		t.Fatalf("page past range = %#v, want none", pastEnd)
	}
	same, err := session.CommitRange(context.Background(), capturedHead, capturedHead, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(same) != 0 {
		t.Fatalf("empty review range = %#v, want none", same)
	}
}

func TestAgentReviewFinalFilesIncludeCommitsStagedUnstagedAndUntracked(t *testing.T) {
	dir := agentReviewRepository(t)
	writeAgentReviewFile(t, dir, "tracked.txt", []byte("one\ntwo\nthree\n"))
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))

	writeAgentReviewFile(t, dir, "tracked.txt", []byte("one\ntwo\nthree\ncommitted\n"))
	writeAgentReviewFile(t, dir, "committed.txt", []byte("committed file\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "captured head")
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writeAgentReviewFile(t, dir, "tracked.txt", []byte("one\ntwo\nthree\ncommitted\nstaged\n"))
	runGit(t, dir, "add", "tracked.txt")
	writeAgentReviewFile(t, dir, "tracked.txt", []byte("one\ntwo\nthree\ncommitted\nstaged\nworktree\n"))
	writeAgentReviewFile(t, dir, "untracked.txt", []byte("untracked\n"))
	session := openAgentReviewSession(t, dir)

	files, err := session.FinalFiles(context.Background(), base, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("final files = %#v, want tracked, committed and untracked", files)
	}
	tracked := agentReviewFile(t, files, "tracked.txt")
	if tracked.Status != "M" || tracked.LinesAdded != 3 || tracked.LinesDeleted != 0 {
		t.Errorf("tracked final stats = %#v, want +3 -0", tracked)
	}
	committed := agentReviewFile(t, files, "committed.txt")
	if committed.Status != "A" || committed.LinesAdded != 1 || committed.LinesDeleted != 0 {
		t.Errorf("committed final stats = %#v, want +1 -0", committed)
	}
	untracked := agentReviewFile(t, files, "untracked.txt")
	if untracked.Status != "??" || untracked.LinesAdded != 1 || untracked.LinesDeleted != 0 {
		t.Errorf("untracked final stats = %#v, want ?? +1 -0", untracked)
	}

	diff, err := session.FinalFileDiff(context.Background(), base, tracked, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if diff.Kind != "Final" {
		t.Errorf("final diff kind = %q, want Final", diff.Kind)
	}
	for _, line := range []string{"+committed\n", "+staged\n", "+worktree\n"} {
		if !strings.Contains(diff.Patch, line) {
			t.Errorf("final patch lacks %q: %q", line, diff.Patch)
		}
	}

	// With base at the captured HEAD, committed work is already in the base;
	// only the dirty index, worktree and untracked file remain.
	dirtyOnly, err := session.FinalFiles(context.Background(), head, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(dirtyOnly) != 2 {
		t.Fatalf("base-equals-head files = %#v, want tracked and untracked only", dirtyOnly)
	}
	tracked = agentReviewFile(t, dirtyOnly, "tracked.txt")
	if tracked.LinesAdded != 2 || tracked.LinesDeleted != 0 {
		t.Errorf("base-equals-head stats = %#v, want +2 -0", tracked)
	}
	diff, err = session.FinalFileDiff(context.Background(), head, tracked, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(diff.Patch, "+committed\n") ||
		!strings.Contains(diff.Patch, "+staged\n") || !strings.Contains(diff.Patch, "+worktree\n") {
		t.Errorf("base-equals-head patch is not dirty-only: %q", diff.Patch)
	}
}

func TestAgentReviewFinalFilesOmitWorktreeReversionOfCommittedEdit(t *testing.T) {
	dir := agentReviewRepository(t)
	writeAgentReviewFile(t, dir, "reverted.txt", []byte("base\n"))
	runGit(t, dir, "add", "reverted.txt")
	runGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writeAgentReviewFile(t, dir, "reverted.txt", []byte("committed\n"))
	runGit(t, dir, "commit", "-am", "change later reverted in worktree")
	writeAgentReviewFile(t, dir, "reverted.txt", []byte("base\n"))
	session := openAgentReviewSession(t, dir)

	files, err := session.FinalFiles(context.Background(), base, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("reverted final files = %#v, want none", files)
	}
	diff, err := session.FinalFileDiff(context.Background(), base,
		CommitFile{Path: "reverted.txt", Status: "M"}, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if diff.Patch != "" {
		t.Errorf("reverted final patch = %q, want empty", diff.Patch)
	}
}

func TestAgentReviewFinalFilesDoNotAddOverlappingSideStats(t *testing.T) {
	dir := agentReviewRepository(t)
	writeAgentReviewFile(t, dir, "overlap.txt", []byte("one\ntwo\nthree\n"))
	runGit(t, dir, "add", "overlap.txt")
	runGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writeAgentReviewFile(t, dir, "overlap.txt", []byte("one\ncommitted\nthree\n"))
	runGit(t, dir, "commit", "-am", "committed edit")
	writeAgentReviewFile(t, dir, "overlap.txt", []byte("one\nstaged\nthree\n"))
	runGit(t, dir, "add", "overlap.txt")
	writeAgentReviewFile(t, dir, "overlap.txt", []byte("one\nfinal\nthree\n"))
	session := openAgentReviewSession(t, dir)

	files, err := session.FinalFiles(context.Background(), base, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	file := agentReviewFile(t, files, "overlap.txt")
	if file.LinesAdded != 1 || file.LinesDeleted != 1 {
		t.Fatalf("overlapping final stats = %#v, want net +1 -1", file)
	}
	diff, err := session.FinalFileDiff(context.Background(), base, file, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "-two\n+final\n") ||
		strings.Contains(diff.Patch, "committed") || strings.Contains(diff.Patch, "staged") {
		t.Errorf("overlapping final patch is not base-to-final: %q", diff.Patch)
	}
}

func TestAgentReviewFinalFilesHandleUntrackedEmptyBinaryAndSpecialNames(t *testing.T) {
	dir := agentReviewRepository(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	special := "odd [review]\t\n.txt"
	writeAgentReviewFile(t, dir, "empty.txt", nil)
	writeAgentReviewFile(t, dir, "binary.dat", []byte{0, 1, 2})
	writeAgentReviewFile(t, dir, special, []byte("first\nsecond\n"))
	session := openAgentReviewSession(t, dir)

	files, err := session.FinalFiles(context.Background(), base, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("untracked final files = %#v, want exactly three unique paths", files)
	}
	for _, path := range []string{"empty.txt", "binary.dat", special} {
		file := agentReviewFile(t, files, path)
		if file.Status != "??" {
			t.Errorf("%q status = %q, want ??", path, file.Status)
		}
		wantAdded := 0
		if path == special {
			wantAdded = 2
		}
		if file.LinesAdded != wantAdded || file.LinesDeleted != 0 {
			t.Errorf("%q stats = +%d -%d, want +%d -0", path, file.LinesAdded, file.LinesDeleted, wantAdded)
		}
	}

	empty := agentReviewFile(t, files, "empty.txt")
	diff, err := session.FinalFileDiff(context.Background(), base, empty, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "new file mode") || diff.NewText != "" {
		t.Errorf("empty untracked diff = %#v, want an empty-file addition", diff)
	}
	diff, err = session.FinalFileDiff(context.Background(), base, agentReviewFile(t, files, special), DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "+first\n+second\n") {
		t.Errorf("special-name untracked patch = %q", diff.Patch)
	}
	diff, err = session.FinalFileDiff(context.Background(), base, agentReviewFile(t, files, "binary.dat"), DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "Binary files") {
		t.Errorf("binary untracked patch = %q", diff.Patch)
	}
}

func TestAgentReviewFinalFileDiffUsesLiteralPathsAndRenamePreviousPath(t *testing.T) {
	dir := agentReviewRepository(t)
	literal := "tracked [review].txt"
	writeAgentReviewFile(t, dir, literal, []byte("old\n"))
	writeAgentReviewFile(t, dir, "old-name.txt", []byte("one\ntwo\nthree\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writeAgentReviewFile(t, dir, literal, []byte("new\n"))
	runGit(t, dir, "mv", "old-name.txt", "new-name.txt")
	session := openAgentReviewSession(t, dir)

	files, err := session.FinalFiles(context.Background(), base, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	literalFile := agentReviewFile(t, files, literal)
	diff, err := session.FinalFileDiff(context.Background(), base, literalFile, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "-old\n+new\n") {
		t.Errorf("literal-path final patch = %q", diff.Patch)
	}
	rename := agentReviewFile(t, files, "new-name.txt")
	if !strings.HasPrefix(rename.Status, "R") || rename.PreviousPath != "old-name.txt" {
		t.Fatalf("rename final file = %#v, want old-name previous path", rename)
	}
	diff, err = session.FinalFileDiff(context.Background(), base, rename, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Patch, "rename from old-name.txt") ||
		!strings.Contains(diff.Patch, "rename to new-name.txt") {
		t.Errorf("rename final patch = %q", diff.Patch)
	}
}

func TestAgentReviewFinalDiffHonorsWhitespaceAndContextOptions(t *testing.T) {
	dir := agentReviewRepository(t)
	writeAgentReviewFile(t, dir, "code.txt", []byte("one\ntwo\nthree\n"))
	writeAgentReviewFile(t, dir, "space.txt", []byte("left value\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writeAgentReviewFile(t, dir, "code.txt", []byte("one\ntwo changed\nthree\n"))
	writeAgentReviewFile(t, dir, "space.txt", []byte("left   value\n"))
	session := openAgentReviewSession(t, dir)

	code := CommitFile{Path: "code.txt", Status: "M"}
	zero, err := session.FinalFileDiff(context.Background(), base, code, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(zero.Patch, "\n one\n") || strings.Contains(zero.Patch, "\n three\n") {
		t.Errorf("zero-context final patch contains neighbors: %q", zero.Patch)
	}
	three, err := session.FinalFileDiff(context.Background(), base, code, DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(three.Patch, "\n one\n") || !strings.Contains(three.Patch, "\n three\n") {
		t.Errorf("three-context final patch lacks neighbors: %q", three.Patch)
	}

	ignored := DiffOptions{ContextLines: 3, IgnoreWhitespace: true}
	files, err := session.FinalFiles(context.Background(), base, ignored)
	if err != nil {
		t.Fatal(err)
	}
	space := agentReviewFile(t, files, "space.txt")
	if space.LinesAdded != 0 || space.LinesDeleted != 0 {
		t.Errorf("whitespace-ignoring final stats = %#v, want zero", space)
	}
	diff, err := session.FinalFileDiff(context.Background(), base, space, ignored)
	if err != nil {
		t.Fatal(err)
	}
	if diff.Patch != "" {
		t.Errorf("whitespace-ignoring final patch = %q, want empty", diff.Patch)
	}
}

func TestAgentReviewRejectsInvalidRefsWithoutEmptyTreeFallback(t *testing.T) {
	dir := agentReviewRepository(t)
	runGit(t, dir, "commit", "--allow-empty", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	session := openAgentReviewSession(t, dir)
	ctx := context.Background()

	if _, err := session.FinalFiles(ctx, "missing", DiffOptions{ContextLines: 3}); err == nil {
		t.Error("FinalFiles accepted an invalid base")
	}
	if _, err := session.FinalFileDiff(ctx, "missing", CommitFile{Path: "file.txt"}, DiffOptions{ContextLines: 3}); err == nil {
		t.Error("FinalFileDiff accepted an invalid base")
	}
	if _, err := session.CommitRange(ctx, "missing", base, 10, 0); err == nil {
		t.Error("CommitRange accepted an invalid base")
	}
	if _, err := session.CommitRange(ctx, base, "missing", 10, 0); err == nil {
		t.Error("CommitRange accepted an invalid captured head")
	}
	if _, err := session.CommitRange(ctx, base, base, 0, 0); err == nil {
		t.Error("CommitRange accepted a zero limit")
	}
	if _, err := session.FinalFiles(ctx, base, DiffOptions{ContextLines: -1}); err == nil {
		t.Error("FinalFiles accepted negative context")
	}
	injectedOutput := filepath.Join(t.TempDir(), "agent-review-injection")
	if _, err := session.FinalFiles(ctx, "--output="+injectedOutput, DiffOptions{ContextLines: 3}); err == nil {
		t.Error("FinalFiles accepted an option-like base")
	}
	if _, err := os.Stat(injectedOutput); !os.IsNotExist(err) {
		t.Errorf("option-like base created an output file: %v", err)
	}

	unborn := agentReviewRepository(t)
	writeAgentReviewFile(t, unborn, "untracked.txt", []byte("must not become an empty-tree diff\n"))
	unbornSession := openAgentReviewSession(t, unborn)
	if files, err := unbornSession.FinalFiles(ctx, "HEAD", DiffOptions{ContextLines: 3}); err == nil {
		t.Errorf("unborn HEAD fell back to the empty tree: %#v", files)
	}
	if commits, err := unbornSession.CommitRange(ctx, "HEAD", "HEAD", 10, 0); err == nil {
		t.Errorf("unborn review range fell back to the empty tree: %#v", commits)
	}
}
