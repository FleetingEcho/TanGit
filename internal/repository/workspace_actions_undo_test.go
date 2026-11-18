package repository

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func operationWithSummary(t *testing.T, session Session, prefix string) OperationRecord {
	t.Helper()
	records, err := session.Operations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if strings.HasPrefix(record.Summary, prefix) {
			return record
		}
	}
	t.Fatalf("no operation with summary prefix %q: %#v", prefix, records)
	return OperationRecord{}
}

func TestUndoOfficialHeadAmendAndReword(t *testing.T) {
	t.Run("amend staged work", func(t *testing.T) {
		dir, session := recoveryRepository(t)
		before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
		message := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%s"))
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "a.txt")
		index := gitOutput(t, dir, "ls-files", "--stage")
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if err := session.AmendCommit(ctx, Commit{Hash: before, ShortHash: before[:7]}); err != nil {
			t.Fatal(err)
		}
		record := operationWithSummary(t, session, "Amend ")
		if err := session.UndoOperation(ctx, record.ID); err != nil {
			t.Fatalf("undo amend: %v", err)
		}
		if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != before {
			t.Fatalf("Undo restore HEAD = %s, want %s", got, before)
		}
		if got := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%s")); got != message {
			t.Fatalf("Undo restored message = %q, want %q", got, message)
		}
		if got := gitOutput(t, dir, "ls-files", "--stage"); got != index {
			t.Fatalf("Undo restored index =\n%s\nwant\n%s", got, index)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "staged\nunstaged\n" {
			t.Fatalf("Undo restored staged/unstaged split = %q", got)
		}
	})
	t.Run("reword preserves separate binary work", func(t *testing.T) {
		dir, session := recoveryRepository(t)
		before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
		message := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%s"))
		payload := []byte{0, 1, 0xff, 2, 0}
		if err := os.WriteFile(filepath.Join(dir, "binary.untracked"), payload, 0o600); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		if err := session.RewordCommit(ctx, Commit{Hash: before, ShortHash: before[:7]}, "Reworded by Undo test", "details"); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%s")); got != "Reworded by Undo test" {
			t.Fatalf("reword fixture did not run: %q", got)
		}
		record := operationWithSummary(t, session, "Reword ")
		if err := session.UndoOperation(ctx, record.ID); err != nil {
			t.Fatalf("undo reword: %v", err)
		}
		if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != before {
			t.Fatalf("reword Undo moved HEAD to %s, want %s", got, before)
		}
		if got := strings.TrimSpace(gitOutput(t, dir, "log", "-1", "--format=%s")); got != message {
			t.Fatalf("reword Undo restored message = %q, want %q", got, message)
		}
		if got, err := os.ReadFile(filepath.Join(dir, "binary.untracked")); err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("reword Undo changed untracked binary: %v, %v", got, err)
		}
	})
}

func TestUndoStashSaveRestoresIndexAndStashReflog(t *testing.T) {
	dir, session := recoveryRepository(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("ignored.bin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.bin"), []byte{0, 0xff, 1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("older stash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(ctx, "older stash", true); err != nil {
		t.Fatal(err)
	}
	originalReflog := gitOutput(t, dir, "stash", "list", "--format=%H")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	index := gitOutput(t, dir, "ls-files", "--stage")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("untracked payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.StashChanges(ctx, "protect-me", true); err != nil {
		t.Fatal(err)
	}
	record := operationWithSummary(t, session, "Stash protect-me")
	if err := session.UndoOperation(ctx, record.ID); err != nil {
		t.Fatalf("undo stash save: %v", err)
	}
	if got := gitOutput(t, dir, "stash", "list", "--format=%H"); got != originalReflog {
		t.Fatalf("stash history changed = %q, want %q", got, originalReflog)
	}
	if got := gitOutput(t, dir, "ls-files", "--stage"); got != index {
		t.Fatalf("stash Undo restored index =\n%s\nwant\n%s", got, index)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "staged\nunstaged\n" {
		t.Fatalf("stash Undo restored staged/unstaged split = %q", got)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "new.txt")); err != nil || string(got) != "untracked payload\n" {
		t.Fatalf("stash Undo restored untracked file = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "ignored.bin")); err != nil || !bytes.Equal(got, []byte{0, 0xff, 1}) {
		t.Fatalf("stash Undo changed ignored binary: %v, %v", got, err)
	}
	scope, present, err := storedWorkspaceScope(session.(*gitSession), record.Snapshot)
	if err != nil || !present {
		t.Fatalf("original stash capture policy is unavailable: %v", err)
	}
	current, err := session.(*gitSession).currentWorkspaceScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	if current.GitStateGuard != record.GitStateBefore || current.WorkspaceGuard != record.WorkspaceGuardBefore {
		t.Fatal("Undo did not reproduce the exact pre-stash repository state")
	}
}

func TestUndoStashApplyAndPopPreservesOriginalStash(t *testing.T) {
	for _, pop := range []bool{false, true} {
		t.Run(map[bool]string{false: "apply", true: "pop"}[pop], func(t *testing.T) {
			dir, session := recoveryRepository(t)
			ctx := context.Background()
			if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("stash payload\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("untracked stash data\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := session.StashChanges(ctx, "restore me", true); err != nil {
				t.Fatal(err)
			}
			before := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
			stashes := gitOutput(t, dir, "stash", "list", "--format=%H")
			beforeContent, err := os.ReadFile(filepath.Join(dir, "a.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if err := session.ApplyStash(ctx, 0, pop); err != nil {
				t.Fatal(err)
			}
			prefix := "Apply "
			if pop {
				prefix = "Pop "
			}
			record := operationWithSummary(t, session, prefix)
			if err := session.UndoOperation(ctx, record.ID); err != nil {
				t.Fatalf("undo stash action: %v", err)
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != before {
				t.Fatalf("stash Undo moved HEAD to %s, want %s", got, before)
			}
			if got := gitOutput(t, dir, "stash", "list", "--format=%H"); got != stashes {
				t.Fatalf("stash selection changed: %q, want %q", got, stashes)
			}
			if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != string(beforeContent) {
				t.Fatalf("Undo restored the wrong pre-apply content: %q", got)
			}
			if _, err := os.Stat(filepath.Join(dir, "untracked.txt")); !os.IsNotExist(err) {
				t.Fatalf("Undo did not reconcile the applied untracked file: %v", err)
			}
		})
	}
}
