package repository

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const mergeUnsupportedIgnoredName = "server/data/log-utils/session/.logcluster\\events.d"

func mergeIgnoredRecoveryFixture(t *testing.T, collide bool) (dir string, session FullSession, ignoredFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this repository name is a legal POSIX filename, not a Windows path")
	}
	dir, session = recoveryRepository(t)
	runGit(t, dir, "checkout", "-b", "AAA")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.d\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".gitignore")
	runGit(t, dir, "commit", "-m", "ignore recovery fixture")
	ignoredFile = filepath.Join(dir, filepath.FromSlash(mergeUnsupportedIgnoredName))
	if err := os.MkdirAll(filepath.Dir(ignoredFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignoredFile, []byte("unrelated user data\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "checkout", "-b", "incoming")
	if collide {
		runGit(t, dir, "add", "-f", "--", mergeUnsupportedIgnoredName)
	} else {
		if err := os.WriteFile(filepath.Join(dir, "incoming.txt"), []byte("incoming\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "incoming.txt")
	}
	runGit(t, dir, "commit", "-m", "incoming changes")
	runGit(t, dir, "checkout", "AAA")
	if collide {
		// Checking out AAA removes the newly tracked incoming file. Recreate
		// it as ignored user data so the incoming tree really collides with it.
		if err := os.MkdirAll(filepath.Dir(ignoredFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ignoredFile, []byte("unrelated user data\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, session, ignoredFile
}

func TestMergeDoesNotBackupUnrelatedUnsupportedIgnoredFile(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "ambiguous branch and tag"}[ambiguous], func(t *testing.T) {
			dir, session, ignoredFile := mergeIgnoredRecoveryFixture(t, false)
			if ambiguous {
				runGit(t, dir, "tag", "incoming")
			}
			before := gitOutput(t, dir, "rev-parse", "AAA")
			target, err := session.(*gitSession).gitText("rev-parse", "--verify", "incoming^{commit}")
			if err != nil {
				t.Fatal(err)
			}
			target = strings.TrimSpace(target)
			if err := session.MergeBranch(context.Background(), "incoming"); err != nil {
				t.Fatalf("unrelated ignored data blocked merge before Git could run: %v", err)
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")); got != target {
				t.Fatalf("merge reached %q, want target %q", got, target)
			}
			record := onlyOperation(t, session)
			scope, enabled, err := storedWorkspaceScope(session.(*gitSession), record.Snapshot)
			if err != nil || !enabled || scope.AllIgnored || len(scope.IgnoredPaths) != 0 {
				t.Fatalf("merge captured an unnecessarily broad ignored scope: scope=%+v enabled=%v err=%v", scope, enabled, err)
			}
			// Unscoped user data does not stop Undo, and Undo must not clobber
			// changes made after the operation it is restoring.
			if err := os.WriteFile(ignoredFile, []byte("new work after merge\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := session.UndoOperation(context.Background(), record.ID); err != nil {
				t.Fatalf("scoped merge recovery cannot be undone: %v", err)
			}
			if got := gitOutput(t, dir, "rev-parse", "HEAD"); got != before {
				t.Fatalf("Undo reached %s, want original HEAD %s", got, before)
			}
			if got, _ := os.ReadFile(ignoredFile); string(got) != "new work after merge\n" {
				t.Fatalf("Undo touched unrelated ignored user data: %q", got)
			}
		})
	}
}

func TestMergeStillRejectsUnsupportedIgnoredCollisionBeforeMutation(t *testing.T) {
	dir, session, ignoredFile := mergeIgnoredRecoveryFixture(t, true)
	before := gitOutput(t, dir, "rev-parse", "HEAD")
	err := session.MergeBranch(context.Background(), "incoming")
	if err == nil || !strings.Contains(err.Error(), "recovery path") {
		t.Fatalf("an actual recovery collision was not rejected: %v", err)
	}
	if got := gitOutput(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("blocked merge nevertheless moved HEAD to %s, want %s", got, before)
	}
	if got, _ := os.ReadFile(ignoredFile); string(got) != "unrelated user data\n" {
		t.Fatalf("blocked merge modified the user's ignored file: %q", got)
	}
}

func TestCommitDoesNotRequireIrrelevantIgnoredSnapshot(t *testing.T) {
	dir, session, ignoredFile := mergeIgnoredRecoveryFixture(t, false)
	before := gitOutput(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "next.txt"), []byte("next\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "next.txt")
	if err := session.Commit(context.Background(), "only tracked content", "", false); err != nil {
		t.Fatalf("irrelevant ignored data blocked an ordinary commit: %v", err)
	}
	if got := gitOutput(t, dir, "rev-parse", "HEAD"); got == before {
		t.Fatal("commit did not advance HEAD")
	}
	if got, _ := os.ReadFile(ignoredFile); string(got) != "unrelated user data\n" {
		t.Fatalf("commit changed unrelated ignored data: %q", got)
	}
}
