package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func continueReviewSession(t *testing.T, session Session) ContinueReviewSession {
	t.Helper()
	review, ok := session.(ContinueReviewSession)
	if !ok {
		t.Fatal("repository session does not implement ContinueReviewSession")
	}
	return review
}

func TestContinueSourceHashesCanonicalIndexAndWorkingTreeBytes(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ResolveConflict(context.Background(), fileNamed(t, snapshot), false); err != nil {
		t.Fatal(err)
	}
	review := continueReviewSession(t, session)
	before, err := review.CaptureContinueSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if before.Operation.Fingerprint == "" || before.IndexHash == "" || before.WorkingTreeHash == "" {
		t.Fatalf("incomplete Continue source: %+v", before)
	}

	// "Feature\n" and "feature\n" have exactly the same length and line
	// count. Content identity, not either display statistic, must change.
	path := filepath.Join(dir, "file.txt")
	oldContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	newContent := []byte("Feature\n")
	if len(oldContent) != len(newContent) {
		t.Fatalf("fixture is not length-invariant: %d != %d", len(oldContent), len(newContent))
	}
	if err := os.WriteFile(path, newContent, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := review.CaptureContinueSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if after.WorkingTreeHash == before.WorkingTreeHash {
		t.Fatal("same-length working-tree edit did not change its canonical hash")
	}
	if after.IndexHash != before.IndexHash {
		t.Fatal("a working-tree-only edit changed the index hash")
	}
	delta := CompareContinueSources(before, after)
	if !delta.ContentChanged() || delta.OperationChanged || len(delta.ChangedFiles) != 1 || delta.ChangedFiles[0] != "file.txt" {
		t.Fatalf("unexpected same-length edit delta: %+v", delta)
	}

	runGit(t, dir, "add", "file.txt")
	staged, err := review.CaptureContinueSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if staged.IndexHash == after.IndexHash {
		t.Fatal("staging the resolved content did not change the canonical index hash")
	}
	if staged.WorkingTreeHash != after.WorkingTreeHash {
		t.Fatal("staging alone changed the working-tree hash")
	}
}

func TestContinueSourceIncludesUntrackedContentAndOperationIdentity(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	review := continueReviewSession(t, session)
	before, err := review.CaptureContinueSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	untracked := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(untracked, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withUntracked, err := review.CaptureContinueSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(untracked, []byte("bravo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := review.CaptureContinueSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if withUntracked.WorkingTreeHash == before.WorkingTreeHash || changed.WorkingTreeHash == withUntracked.WorkingTreeHash {
		t.Fatal("untracked files or their same-length edits are absent from the working-tree hash")
	}
	delta := CompareContinueSources(withUntracked, changed)
	if len(delta.ChangedFiles) != 1 || delta.ChangedFiles[0] != "notes.txt" {
		t.Fatalf("untracked content delta = %+v", delta)
	}

	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.ResolveConflict(context.Background(), fileNamed(t, snapshot), false); err != nil {
		t.Fatal(err)
	}
	resolved, err := review.CaptureContinueSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !CompareContinueSources(changed, resolved).OperationChanged {
		t.Fatal("resolving index stages did not invalidate the captured operation")
	}
}

func TestContinueSourceCaptureIsReadOnlyAndContextAware(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	status := gitOutput(t, dir, "status", "--porcelain=v2", "--untracked-files=all")
	index := gitOutput(t, dir, "ls-files", "--stage")
	content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	review := continueReviewSession(t, session)
	if _, err := review.CaptureContinueSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, dir, "status", "--porcelain=v2", "--untracked-files=all"); got != status {
		t.Fatalf("capture changed status:\nbefore %q\nafter %q", status, got)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage"); got != index {
		t.Fatal("capture staged, committed, or restored index content")
	}
	gotContent, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotContent) != string(content) {
		t.Fatal("capture changed working-tree bytes")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := review.CaptureContinueSource(ctx); err == nil {
		t.Fatal("cancelled source capture succeeded")
	}
}
