package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestRealAppUndoCommitThroughSafetyJournal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the extra recovery assertions use POSIX permissions and symbolic links")
	}
	dir := t.TempDir()
	runGitIn(t, dir, "init", "--initial-branch=main")
	runGitIn(t, dir, "config", "user.name", "Undo UI Test")
	runGitIn(t, dir, "config", "user.email", "undo-ui@example.com")
	runGitIn(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "a.txt")
	runGitIn(t, dir, "commit", "-m", "base for journal Undo")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD"))
	binary := []byte{0, 1, 0xff, 2}
	if err := os.WriteFile(filepath.Join(dir, ".git", "info", "exclude"), []byte("ignored.bin\nignored-link\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.bin"), binary, 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "ignored.bin"), 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ignored.bin", filepath.Join(dir, "ignored-link")); err != nil {
		t.Fatal(err)
	}
	session, err := (repository.Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	h := newFileRecoveryApp(t, dir, session)
	ctx := context.Background()

	// Use the actual commit shortcut and real form submission. The baseline
	// contains separate staged bytes and an unstaged working-file suffix.
	h.press(t, ui.KeyC)
	if !h.app.showCommitForm {
		t.Fatalf("C did not open the real commit form: %q", h.texts())
	}
	h.tester.Type("Undo from the safety journal")
	h.tester.Frame()
	if err := h.tester.Click("Commit"); err != nil {
		t.Fatalf("submit the real commit: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "a recoverable commit in the safety journal", func() bool {
		for _, record := range h.app.snapshot.Operations {
			if record.Kind == repository.OperationCommit && record.CanUndo() {
				return true
			}
		}
		return false
	})
	var record repository.OperationRecord
	for _, current := range h.app.snapshot.Operations {
		if current.Kind == repository.OperationCommit {
			record = current
			break
		}
	}
	after := strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD"))
	if after == before {
		t.Fatal("the real commit did not move HEAD")
	}

	entries := h.app.commandLogEntries()
	provenance := 0
	for _, entry := range entries {
		if entry.Shortcut != nil && entry.Shortcut.ID == "git/Changes/Write a commit message" {
			provenance++
			if entry.Shortcut.Chord != "c" || entry.Shortcut.Label != "Write a commit message" {
				t.Fatalf("commit shortcut provenance = %+v", entry.Shortcut)
			}
		}
		if entry.Shortcut != nil && entry.Shortcut.Chord == "Enter" {
			t.Fatalf("typing in the form or mouse approval was mislabeled as a shortcut: %+v", entry.Shortcut)
		}
	}
	if provenance != 1 {
		t.Fatalf("commit keyboard provenance records = %d, want exactly 1", provenance)
	}

	h.app.setWorkspaceTab(sectionStash)
	h.tester.Frame()
	openRecoveryForTest(t, h)
	if err := h.tester.Click("Show safety journal"); err != nil {
		t.Fatalf("open Safety Journal: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if err := h.tester.Click("Undo " + record.ID); err != nil {
		t.Fatalf("choose journal Undo: %v; texts=%q", err, h.texts())
	}
	h.tester.Frame()
	if h.app.confirm == nil || h.app.confirm.message != "Undo "+record.Summary+"?" {
		t.Fatalf("Undo did not open its exact confirmation: %#v; texts=%q", h.app.confirm, h.texts())
	}
	if got := strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD")); got != after {
		t.Fatal("Undo ran before the confirmation was accepted")
	}
	if err := h.tester.Click("Undo"); err != nil {
		t.Fatalf("accept Undo: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "the confirmed Undo to restore the parent", func() bool {
		if strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD")) != before {
			return false
		}
		records, err := session.Operations(ctx)
		if err != nil {
			return false
		}
		for _, current := range records {
			if current.ID == record.ID {
				return current.UndoneAt != 0
			}
		}
		return false
	})
	if got := strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD")); got != before {
		t.Fatalf("confirmed Undo moved HEAD to %s, want %s", got, before)
	}
	if got := runGitIn(t, dir, "show", ":a.txt"); got != "staged\n" {
		t.Fatalf("Undo restored the wrong staged payload: %q", got)
	}
	if got := readRecoveryFile(t, dir, "a.txt"); string(got) != "staged\nunstaged\n" {
		t.Fatalf("Undo restored the wrong staged/unstaged split: %q", got)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "ignored.bin")); err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("Undo lost ignored binary data: %v, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dir, "ignored.bin"))
	if err != nil || info.Mode().Perm() != 0o751 {
		t.Fatalf("Undo restored the wrong ignored permissions: %v, %v", info, err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "ignored-link")); err != nil || target != "ignored.bin" {
		t.Fatalf("Undo restored the wrong symbolic link: %q, %v", target, err)
	}

}
