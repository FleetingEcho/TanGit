package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUndoingAPartialStageAndUnstage(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	writeSafetyFile(t, dir, "a.txt", "one\ntwo\nthree\n")
	runGit(t, dir, "commit", "-am", "three lines")
	writeSafetyFile(t, dir, "a.txt", "ONE\ntwo\nTHREE\n")
	patch := gitOutput(t, dir, "diff", "--", "a.txt")
	before := fullDigest(t, dir)

	if err := session.ApplyCachedPatch(ctx, patch, false); err != nil {
		t.Fatalf("stage selection: %v", err)
	}
	staged := fullDigest(t, dir)
	if staged == before {
		t.Fatal("staging the selection changed nothing")
	}
	record := latestOfKind(t, session, OperationStage)
	if record.Summary != "Stage selected changes" || !record.CanUndo() {
		t.Fatalf("selection stage record = %#v", record)
	}
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo stage selection: %v", err)
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatal("undo did not restore the exact state before the partial stage")
	}

	if err := session.ApplyCachedPatch(ctx, patch, false); err != nil {
		t.Fatal(err)
	}
	if err := session.ApplyCachedPatch(ctx, patch, true); err != nil {
		t.Fatalf("unstage selection: %v", err)
	}
	unstage := latestOfKind(t, session, OperationUnstage)
	if unstage.Summary != "Unstage selected changes" {
		t.Fatalf("unstage record = %#v", unstage)
	}
	if err := session.UndoOperation(ctx, unstage.ID); err != nil {
		t.Fatalf("undo unstage selection: %v", err)
	}
	if got := fullDigest(t, dir); got != staged {
		t.Fatal("undo did not bring back the partially staged index")
	}
}

func TestUndoingIgnoreAndExclude(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name, kind, file string
		ignore           func(Session, File) error
	}{
		{"ignore", OperationIgnore, ".gitignore", func(s Session, f File) error { return s.(FullSession).IgnoreFile(ctx, f) }},
		{"exclude", OperationExclude, filepath.Join(".git", "info", "exclude"), func(s Session, f File) error { return s.(FullSession).ExcludeFile(ctx, f) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, session := richWorkspace(t)
			target := filepath.Join(dir, test.file)
			existed := false
			if _, err := os.Stat(target); err == nil {
				existed = true
			}
			original, _ := os.ReadFile(target)
			before := fullDigest(t, dir)
			if err := test.ignore(session, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
				t.Fatal(err)
			}
			record := latestOfKind(t, session, test.kind)
			if !record.CanUndo() || !strings.Contains(record.Summary, "new.txt") {
				t.Fatalf("record = %#v", record)
			}
			if err := session.UndoOperation(ctx, record.ID); err != nil {
				t.Fatalf("undo: %v", err)
			}
			got, err := os.ReadFile(target)
			if existed {
				if err != nil || string(got) != string(original) {
					t.Fatalf("rule file after undo = %q, %v; want %q", got, err, original)
				}
			} else if err == nil {
				t.Fatalf("a file that did not exist before is still there: %q", got)
			}
			if test.name == "ignore" {
				if now := fullDigest(t, dir); now != before {
					t.Fatalf("undo ignore did not restore the exact state\n got: %s\nwant: %s", now, before)
				}
			}
			if _, ok := indexedPath(t, session, "new.txt"); !ok {
				t.Fatal("new.txt must be listed as untracked again")
			}
		})
	}
}

func indexedPath(t *testing.T, session Session, path string) (File, bool) {
	t.Helper()
	for _, file := range snapshotFiles(t, session) {
		if file.Path == path {
			return file, true
		}
	}
	return File{}, false
}

func TestUndoingIgnoreRefusesAfterTheRuleFileWasEdited(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	if err := session.IgnoreFile(ctx, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
		t.Fatal(err)
	}
	record := latestOfKind(t, session, OperationIgnore)
	writeSafetyFile(t, dir, ".gitignore", "my own rules\n")
	err := session.UndoOperation(ctx, record.ID)
	if err == nil || !strings.Contains(err.Error(), "edited after") {
		t.Fatalf("undo over an edited .gitignore = %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); string(got) != "my own rules\n" {
		t.Fatalf("a refused undo changed .gitignore: %q", got)
	}
}

func TestUndoingIgnorePreservesRulesThatWereThereBefore(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	writeSafetyFile(t, dir, ".gitignore", "*.log\nbuild/\n")
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "commit", "-m", "ignore rules")
	before := fullDigest(t, dir)
	if err := session.IgnoreFile(ctx, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(ctx, latestOfKind(t, session, OperationIgnore).ID); err != nil {
		t.Fatal(err)
	}
	if got := fullDigest(t, dir); got != before {
		t.Fatalf("existing rules were not restored exactly\n got: %s\nwant: %s", got, before)
	}
}

func TestUndoingTagCreateAndDelete(t *testing.T) {
	ctx := context.Background()
	for _, annotated := range []bool{false, true} {
		name := "light"
		if annotated {
			name = "annotated"
		}
		t.Run(name, func(t *testing.T) {
			dir, session := richWorkspace(t)
			head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
			before := fullDigest(t, dir)
			if err := session.CreateTag(ctx, "v1", head, "release", annotated); err != nil {
				t.Fatal(err)
			}
			created := latestOfKind(t, session, OperationTagCreate)
			if created.Summary != "Create tag v1" || !created.CanUndo() {
				t.Fatalf("create record = %#v", created)
			}
			tagObject := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/tags/v1"))
			if err := session.UndoOperation(ctx, created.ID); err != nil {
				t.Fatalf("undo create: %v", err)
			}
			if out := gitOutput(t, dir, "tag", "--list", "v1"); strings.TrimSpace(out) != "" {
				t.Fatalf("tag survived undo: %q", out)
			}
			if got := fullDigest(t, dir); got != before {
				t.Fatal("undoing a tag changed something else")
			}

			if err := session.CreateTag(ctx, "v1", head, "release", annotated); err != nil {
				t.Fatal(err)
			}
			if err := session.DeleteTag(ctx, "v1"); err != nil {
				t.Fatal(err)
			}
			deleted := latestOfKind(t, session, OperationTagDelete)
			if err := session.UndoOperation(ctx, deleted.ID); err != nil {
				t.Fatalf("undo delete: %v", err)
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/tags/v1")); got == "" {
				t.Fatal("deleted tag did not come back")
			}
			if annotated {
				_ = tagObject
				if kind := strings.TrimSpace(gitOutput(t, dir, "cat-file", "-t", "refs/tags/v1")); kind != "tag" {
					t.Fatalf("the annotated tag came back as a %q", kind)
				}
			}
		})
	}
}

func TestUndoingATagRefusesWhenItMovedAfterwards(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := session.CreateTag(ctx, "v1", head, "", false); err != nil {
		t.Fatal(err)
	}
	record := latestOfKind(t, session, OperationTagCreate)
	runGit(t, dir, "commit", "-m", "later", "--allow-empty")
	runGit(t, dir, "tag", "-f", "v1")
	moved := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/tags/v1"))
	err := session.UndoOperation(ctx, record.ID)
	if err == nil || !strings.Contains(err.Error(), "changed after") {
		t.Fatalf("undo of a moved tag = %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "refs/tags/v1")); got != moved {
		t.Fatal("a refused undo moved the tag")
	}
}

func TestSmallActionsLeaveNoEntryWhenNothingChanged(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	if err := session.DeleteTag(ctx, "missing"); err == nil {
		t.Fatal("deleting a missing tag should fail")
	}
	if got := operationsOfKind(t, session, OperationTagDelete); len(got) != 0 {
		t.Fatalf("a failed delete left a record: %#v", got)
	}
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if err := session.CreateTag(ctx, "v1", head, "", false); err != nil {
		t.Fatal(err)
	}
	if err := session.CreateTag(ctx, "v1", head, "", false); err == nil {
		t.Fatal("creating a duplicate tag should fail")
	}
	if got := operationsOfKind(t, session, OperationTagCreate); len(got) != 1 {
		t.Fatalf("a failed duplicate create left an extra record: %d", len(got))
	}
}

func TestDeletingAJournalEntryRemovesItsSavedFileCopy(t *testing.T) {
	dir, session := richWorkspace(t)
	ctx := context.Background()
	writeSafetyFile(t, dir, ".gitignore", "x\n")
	if err := session.IgnoreFile(ctx, fileByPath(t, snapshotFiles(t, session), "new.txt")); err != nil {
		t.Fatal(err)
	}
	record := latestOfKind(t, session, OperationIgnore)
	saved := filepath.Join(dir, ".git", "tangit", "journal", record.ID+".before")
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("saved copy missing: %v", err)
	}
	if err := session.DeleteOperation(ctx, record.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(saved); err == nil {
		t.Fatal("the saved copy outlived its journal entry")
	}
}
