package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func onlyOperation(t *testing.T, session Session) OperationRecord {
	t.Helper()
	records, err := session.Operations(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatalf("journal records = %#v, %v; want one", records, err)
	}
	return records[0]
}

func TestDiscardJournalUndoRestoresExactWorkAndPersists(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Discard(ctx, core.Files[0]); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if record.Kind != OperationDiscard || !record.CanUndo() || record.Snapshot == nil {
		t.Fatalf("unexpected discard record: %#v", record)
	}
	reopened, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	persisted := onlyOperation(t, reopened)
	if persisted.ID != record.ID || persisted.AfterState == "" {
		t.Fatalf("record did not persist: %#v", persisted)
	}
	if err := reopened.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(content) != "staged\nunstaged\n" {
		t.Fatalf("restored content = %q, %v", content, err)
	}
	if got := gitOutput(t, dir, "show", ":a.txt"); got != "staged\n" {
		t.Fatalf("restored index = %q", got)
	}
	records, err := reopened.Operations(ctx)
	if err != nil || records[0].UndoneAt == 0 || records[0].CanUndo() {
		t.Fatalf("undone record = %#v, %v", records, err)
	}
}

func TestDiscardJournalUndoPreservesUnselectedExistingWork(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("discard only this file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("unselected existing work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var selected File
	for _, file := range core.Files {
		if file.Path == "a.txt" {
			selected = file
		}
	}
	if err := session.Discard(ctx, selected); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "discard only this file\n" {
		t.Fatalf("restored selected file = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "keep.txt")); string(got) != "unselected existing work\n" {
		t.Fatalf("unselected work changed = %q", got)
	}
}

func TestPartialDiscardJournalUndoRestoresCompletedFiles(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "first.txt"), []byte("discarded before the failure\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("unselected existing work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := []File{
		{Path: "first.txt", Added: true, Unstaged: true},
		{Path: "../outside", Added: true, Unstaged: true},
	}
	if err := session.DiscardFiles(ctx, files); err == nil {
		t.Fatal("invalid second path unexpectedly discarded")
	}
	if _, err := os.Stat(filepath.Join(dir, "first.txt")); !os.IsNotExist(err) {
		t.Fatal("fixture did not complete the first discard before failing")
	}
	record := onlyOperation(t, session)
	if record.Status != OperationFailed || !record.Partial || !record.CanUndo() {
		t.Fatalf("partial discard is not recoverable: %#v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "first.txt")); string(got) != "discarded before the failure\n" {
		t.Fatalf("restored partial discard = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "keep.txt")); string(got) != "unselected existing work\n" {
		t.Fatalf("unselected work changed = %q", got)
	}
}

func TestJournalUndoRejectsNewerWork(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("discard me\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Discard(ctx, core.Files[0]); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if err := os.WriteFile(filepath.Join(dir, "newer.txt"), []byte("must survive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(ctx, record.ID); err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("stale undo error = %v", err)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "newer.txt")); string(content) != "must survive\n" {
		t.Fatal("stale undo changed newer work")
	}
}

func TestDiscardJournalUndoRejectsNewerContentsWithSameUntrackedStatus(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("discard this file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("original surviving work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var selected File
	for _, file := range core.Files {
		if file.Path == "a.txt" {
			selected = file
		}
	}
	if err := session.Discard(ctx, selected); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("newer work with the same ?? status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(ctx, record.ID); err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("same-status content edit was accepted for Undo: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "keep.txt")); string(got) != "newer work with the same ?? status\n" {
		t.Fatalf("stale Undo overwrote newer untracked contents = %q", got)
	}
}

func TestMixedResetJournalUndoRejectsFurtherTrackedEdits(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("second commit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "second")
	older := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD^"))
	if err := session.ResetToCommit(ctx, Commit{Hash: older, ShortHash: older[:7]}, ResetMixed); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("newer work with the same M status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(ctx, record.ID); err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("same-status tracked edit was accepted for Undo: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "newer work with the same M status\n" {
		t.Fatalf("stale Undo overwrote newer tracked contents = %q", got)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != older {
		t.Fatalf("stale Undo moved HEAD = %s, want %s", got, older)
	}
}

func TestHardResetJournalUndoRestoresHeadIndexAndWorktree(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "second")
	before, err := session.Snapshot(ctx)
	if err != nil || len(before.Commits) < 2 {
		t.Fatalf("history = %#v, %v", before.Commits, err)
	}
	newest, older := before.Commits[0], before.Commits[1]
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.ResetToCommit(ctx, older, ResetHard); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if record.Kind != OperationReset || record.HeadBefore != newest.Hash || record.Snapshot == nil {
		t.Fatalf("unexpected reset record: %#v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != newest.Hash {
		t.Fatalf("restored HEAD = %s, want %s", got, newest.Hash)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(content) != "staged\nunstaged\n" {
		t.Fatalf("restored worktree = %q", content)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "new.txt")); string(content) != "new\n" {
		t.Fatalf("restored untracked file = %q", content)
	}
	if got := gitOutput(t, dir, "show", ":a.txt"); got != "staged\n" {
		t.Fatalf("restored index = %q", got)
	}
}

func TestDeletedBranchJournalUndoPinsTipAndRecreatesBranch(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	runGit(t, dir, "branch", "feature")
	tip := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "feature"))
	if err := session.DeleteBranch(ctx, "feature", true); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if record.Kind != OperationBranchDelete || record.ObjectID != tip || record.RefName == "" {
		t.Fatalf("unexpected branch record: tip=%q record=%#v", tip, record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/heads/feature")); got != tip {
		t.Fatalf("recreated branch = %s, want %s", got, tip)
	}
}

func TestDroppedStashJournalUndoRestoresSavedObject(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("stashed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(ctx, "precious work", false); err != nil {
		t.Fatal(err)
	}
	stashes, err := session.Snapshot(ctx)
	if err != nil || len(stashes.Stashes) != 1 {
		t.Fatalf("stashes = %#v, %v", stashes.Stashes, err)
	}
	hash := stashes.Stashes[0].Hash
	if err := session.DropStash(ctx, 0); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	list := nativeStashList(t, dir)
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "stash@{0}")); !strings.Contains(list, "precious work") || got != hash {
		t.Fatalf("restored stash = %q (%s), want %s; record=%#v", list, got, hash, record)
	}
}

func TestIndependentSessionsSerializePatchValidationAndApply(t *testing.T) {
	dir, first := patchImportRepository(t)
	second, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nTWO\nthree\n"))
	})
	data, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatal(err)
	}
	firstPreview, err := first.PreviewPatchText(context.Background(), string(data))
	if err != nil || !firstPreview.Clean {
		t.Fatalf("first preview = %#v, %v", firstPreview, err)
	}
	secondPreview, err := second.PreviewPatchText(context.Background(), string(data))
	if err != nil || !secondPreview.Clean {
		t.Fatalf("second preview = %#v, %v", secondPreview, err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, test := range []struct {
		session Session
		preview PatchPreview
	}{{first, firstPreview}, {second, secondPreview}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- test.session.ApplyPatch(context.Background(), test.preview)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent applies succeeded %d times; want exactly one", successes)
	}
	if got := string(readPatchImportFile(t, dir, "a.txt")); got != "one\nTWO\nthree\n" {
		t.Fatalf("concurrent apply content = %q", got)
	}
	if got := gitOutput(t, dir, "diff", "--cached"); got != "" {
		t.Fatalf("concurrent apply staged changes: %s", got)
	}
	onlyOperation(t, first)
}

func TestIndependentSessionsSerializeConcurrentUndo(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("restore exactly once\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	core, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Discard(ctx, core.Files[0]); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	first, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, session := range []Session{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- session.UndoOperation(ctx, record.ID)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent Undo succeeded %d times; want exactly one", successes)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "restore exactly once\n" {
		t.Fatalf("concurrent Undo content = %q", got)
	}
	if persisted := onlyOperation(t, first); persisted.UndoneAt == 0 || persisted.CanUndo() {
		t.Fatalf("concurrent Undo did not persist completion: %#v", persisted)
	}
}

func TestAppliedPatchJournalUndoReversesOnlyPatch(t *testing.T) {
	dir, session := patchImportRepository(t)
	ctx := context.Background()
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nTWO\nthree\nfour\n"))
	})
	preview, err := session.PreviewPatch(ctx, patchPath)
	if err != nil || !preview.Clean {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	if err := session.ApplyPatch(ctx, preview); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if record.Kind != OperationPatchApply || !record.CanUndo() {
		t.Fatalf("unexpected patch record: %#v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if got := string(readPatchImportFile(t, dir, "a.txt")); got != "one\ntwo\nthree\n" {
		t.Fatalf("reverse-applied content = %q", got)
	}
	if got := gitOutput(t, dir, "diff", "--cached"); got != "" {
		t.Fatalf("undo staged changes: %s", got)
	}
}

func TestHardResetJournalUndoRestoresUntrackedFileOverwrittenByReset(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-b", "older-with-tracked-file")
	if err := os.WriteFile(filepath.Join(dir, "collision.txt"), []byte("tracked at reset target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "collision.txt")
	runGit(t, dir, "commit", "-m", "tracked collision")
	older := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "collision.txt"), []byte("precious untracked contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.ResetToCommit(ctx, Commit{Hash: older, ShortHash: older[:7]}, ResetHard); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "collision.txt")); string(got) != "tracked at reset target\n" {
		t.Fatalf("fixture reset did not replace the untracked file: %q", got)
	}
	record := onlyOperation(t, session)
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != before {
		t.Fatalf("restored HEAD = %s, want %s", got, before)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "collision.txt")); string(got) != "precious untracked contents\n" {
		t.Fatalf("restored overwritten untracked file = %q", got)
	}
	if got := gitOutput(t, dir, "status", "--porcelain"); got != "?? collision.txt\n" {
		t.Fatalf("restored untracked status = %q", got)
	}
}

func TestHardResetJournalUndoRestoresIgnoredFileOverwrittenByReset(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-b", "older-with-tracked-ignored-collision")
	if err := os.WriteFile(filepath.Join(dir, "collision.txt"), []byte("tracked at reset target\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "collision.txt")
	runGit(t, dir, "commit", "-m", "tracked ignored collision")
	older := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("collision.txt\nnode_modules/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "cache.txt"), []byte("cache before reset\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "collision.txt"), []byte("precious ignored contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.ResetToCommit(ctx, Commit{Hash: older, ShortHash: older[:7]}, ResetHard); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "collision.txt")); string(got) != "tracked at reset target\n" {
		t.Fatalf("fixture reset did not replace the ignored file: %q", got)
	}
	record := onlyOperation(t, session)
	if record.Snapshot == nil {
		t.Fatal("hard reset did not capture the ignored file it overwrote")
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "cache.txt"), []byte("unrelated newer cache\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != before {
		t.Fatalf("restored HEAD = %s, want %s", got, before)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "collision.txt")); string(got) != "precious ignored contents\n" {
		t.Fatalf("restored overwritten ignored file = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "node_modules", "cache.txt")); string(got) != "unrelated newer cache\n" {
		t.Fatalf("Undo captured or reverted an unrelated ignored cache = %q", got)
	}
}

func TestResetJournalUndoRejectsNewerIgnoredFile(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "collision.txt"), []byte("tracked before reset\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "collision.txt")
	runGit(t, dir, "commit", "-m", "tracked before ignored reset")
	older := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD^"))
	if err := session.ResetToCommit(ctx, Commit{Hash: older, ShortHash: older[:7]}, ResetHard); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("collision.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "collision.txt"), []byte("new precious ignored work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(ctx, record.ID); err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("newer ignored work was accepted for Undo: %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != older {
		t.Fatalf("stale Undo moved HEAD to %s, want %s", got, older)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "collision.txt")); string(got) != "new precious ignored work\n" {
		t.Fatalf("stale Undo overwrote newer ignored contents = %q", got)
	}
}

func TestSoftAndMixedResetJournalUndoRestoreExactHead(t *testing.T) {
	for _, mode := range []ResetMode{ResetSoft, ResetMixed} {
		t.Run(string(mode), func(t *testing.T) {
			dir, session := recoveryRepository(t)
			ctx := context.Background()
			if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("second\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", "a.txt")
			runGit(t, dir, "commit", "-m", "second")
			before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
			older := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD^"))
			if err := session.ResetToCommit(ctx, Commit{Hash: older, ShortHash: older[:7]}, mode); err != nil {
				t.Fatal(err)
			}
			record := onlyOperation(t, session)
			if err := session.UndoOperation(ctx, record.ID); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != before {
				t.Fatalf("restored HEAD = %s, want %s", got, before)
			}
			if got := gitOutput(t, dir, "status", "--porcelain"); got != "" {
				t.Fatalf("restored clean pre-reset state = %q", got)
			}
		})
	}
}

func TestResetJournalUndoRejectsNewerHead(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	older := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "second")
	newest := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := session.ResetToCommit(ctx, Commit{Hash: older, ShortHash: older[:7]}, ResetSoft); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	runGit(t, dir, "commit", "--allow-empty", "-m", "newer history")
	if err := session.UndoOperation(ctx, record.ID); err == nil || !strings.Contains(err.Error(), "newer history") {
		t.Fatalf("newer HEAD undo error = %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got == newest {
		t.Fatal("stale undo discarded the newer commit")
	}
}

func TestRebaseJournalUndoRestoresPreRebaseHead(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "feature.txt")
	runGit(t, dir, "commit", "-m", "feature")
	featureTip := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "feature"))
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "main.txt")
	runGit(t, dir, "commit", "-m", "main work")
	before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := session.RebaseOntoBranch(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if record.Kind != OperationRebase || record.HeadBefore != before || record.HeadAfter == before {
		t.Fatalf("unexpected rebase record: %#v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != before {
		t.Fatalf("restored pre-rebase HEAD = %s, want %s", got, before)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "feature")); got != featureTip {
		t.Fatalf("rebase undo moved feature = %s, want %s", got, featureTip)
	}
}

func TestDroppedStashJournalUndoRejectsNewerStashState(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("first stash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(ctx, "first", false); err != nil {
		t.Fatal(err)
	}
	if err := session.DropStash(ctx, 0); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("newer stash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(ctx, "newer", false); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(ctx, record.ID); err == nil || !strings.Contains(err.Error(), "stash") {
		t.Fatalf("newer stash undo error = %v", err)
	}
	if list := nativeStashList(t, dir); strings.Contains(list, "first") || !strings.Contains(list, "newer") {
		t.Fatalf("stale undo changed the newer stash list: %q", list)
	}
}

func TestRecoveryPruningKeepsWorkNeededByJournalUndo(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	var first OperationRecord
	for index := 0; index <= recoveryKeep; index++ {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Repeat("change\n", index+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := session.Snapshot(ctx)
		if err != nil || len(snapshot.Files) != 1 {
			t.Fatalf("snapshot %d = %#v, %v", index, snapshot.Files, err)
		}
		if err := session.Discard(ctx, snapshot.Files[0]); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = onlyOperation(t, session)
		}
	}
	snapshots, err := session.WorkSnapshots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == first.Snapshot.ID {
			return
		}
	}
	t.Fatalf("recovery pruning removed the first journal's saved work %s among %d snapshots", first.Snapshot.ID, len(snapshots))
}

func TestPendingOperationIsInterruptedAndNotUndoable(t *testing.T) {
	_, session := recoveryRepository(t)
	concrete := session.(*gitSession)
	record, err := concrete.beginOperation(OperationReset, "unfinished", UndoResetHead, nil)
	if err != nil {
		t.Fatal(err)
	}
	records, err := session.Operations(context.Background())
	if err != nil || len(records) != 1 || records[0].Status != OperationInterrupted || records[0].CanUndo() {
		t.Fatalf("pending record = %#v, %v", records, err)
	}
	if err := session.UndoOperation(context.Background(), record.ID); err == nil {
		t.Fatal("interrupted operation offered undo")
	}
}

func TestOperationsIgnoreUnpublishedTemporaryRecords(t *testing.T) {
	_, session := recoveryRepository(t)
	concrete := session.(*gitSession)
	record, err := concrete.beginOperation(OperationReset, "published", UndoResetHead, nil)
	if err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(concrete.journalDir(), "operation-partial.json")
	if err := os.WriteFile(temporary, []byte(`{"version":1,"id":`), 0o600); err != nil {
		t.Fatal(err)
	}
	records, err := session.Operations(context.Background())
	if err != nil || len(records) != 1 || records[0].ID != record.ID {
		t.Fatalf("operations included an in-progress temporary file: %#v, %v", records, err)
	}
}

func TestCorruptJournalRecordDoesNotBlockRepositoryDetailsOrOfferUndo(t *testing.T) {
	dir, session := recoveryRepository(t)
	concrete := session.(*gitSession)
	published, err := concrete.beginOperation(OperationReset, "healthy", UndoResetHead, nil)
	if err != nil {
		t.Fatal(err)
	}
	corruptID := strings.Repeat("f", 32)
	if err := os.WriteFile(filepath.Join(concrete.journalDir(), corruptID+".json"), []byte(`{"version":1,"status":"succeeded",`), 0o600); err != nil {
		t.Fatal(err)
	}

	records, err := session.Operations(context.Background())
	if err != nil || len(records) != 2 {
		t.Fatalf("corrupt record blocked journal loading: %#v, %v", records, err)
	}
	var healthy, corrupt *OperationRecord
	for index := range records {
		switch records[index].ID {
		case published.ID:
			healthy = &records[index]
		case corruptID:
			corrupt = &records[index]
		}
	}
	if healthy == nil || healthy.Status != OperationInterrupted || healthy.Summary != "healthy" {
		t.Fatalf("healthy record was not preserved: %#v", healthy)
	}
	if corrupt == nil || corrupt.Status != OperationInterrupted || corrupt.CanUndo() || corrupt.UndoKind != "" || corrupt.Error == "" {
		t.Fatalf("corrupt record was not represented safely: %#v", corrupt)
	}
	details, err := session.(ProgressiveSession).DetailsSnapshot(context.Background())
	if err != nil || len(details.Commits) == 0 || len(details.Worktrees) == 0 || len(details.Operations) != 2 {
		t.Fatalf("corrupt record blocked unrelated details: commits=%d worktrees=%d operations=%d, %v",
			len(details.Commits), len(details.Worktrees), len(details.Operations), err)
	}
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := session.UndoOperation(context.Background(), corruptID); err == nil {
		t.Fatal("corrupt record offered a direct undo")
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != head {
		t.Fatalf("corrupt undo moved HEAD to %s, want %s", got, head)
	}
}

func assertDeletedJournalSnapshotStopsUndo(t *testing.T, dir string, session Session, record OperationRecord) {
	t.Helper()
	if record.Snapshot == nil {
		t.Fatalf("operation has no recovery snapshot to delete: %#v", record)
	}
	headAfter := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	statusAfter := gitOutput(t, dir, "status", "--porcelain")
	ref, err := workSnapshotRef(record.Snapshot.Kind, record.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "update-ref", "-d", ref)
	if err := session.UndoOperation(context.Background(), record.ID); err == nil || !strings.Contains(err.Error(), "saved work was deleted or changed") {
		t.Fatalf("deleted-snapshot undo error = %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != headAfter {
		t.Fatalf("undo with deleted snapshot moved HEAD to %s, want %s", got, headAfter)
	}
	if got := gitOutput(t, dir, "status", "--porcelain"); got != statusAfter {
		t.Fatalf("undo with deleted snapshot changed worktree:\nbefore: %q\nafter: %q", statusAfter, got)
	}
}

func TestResetJournalUndoRefusesDeletedRecoverySnapshotBeforeMovingHead(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "second")
	older := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD^"))
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("work saved before reset\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.ResetToCommit(ctx, Commit{Hash: older, ShortHash: older[:7]}, ResetHard); err != nil {
		t.Fatal(err)
	}
	assertDeletedJournalSnapshotStopsUndo(t, dir, session, onlyOperation(t, session))
}

func TestRebaseJournalUndoRefusesDeletedRecoverySnapshotBeforeMovingHead(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "feature.txt")
	runGit(t, dir, "commit", "-m", "feature")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "main.txt")
	runGit(t, dir, "commit", "-m", "main work")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("work saved before rebase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.RebaseOntoBranch(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	assertDeletedJournalSnapshotStopsUndo(t, dir, session, onlyOperation(t, session))
}
