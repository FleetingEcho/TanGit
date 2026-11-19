package repository

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type checkoutUndoFixture struct {
	dir        string
	session    FullSession
	mainSHA    string
	featureSHA string
	mainText   string
}

func newCheckoutUndoFixture(t *testing.T) checkoutUndoFixture {
	t.Helper()
	dir, session := recoveryRepository(t)
	mainSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("feature contents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "commit", "-am", "feature edit")
	featureSHA := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "main")
	mainText, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local.bin"), []byte{0, 0xff, 7}, 0o711); err != nil {
		t.Fatal(err)
	}
	return checkoutUndoFixture{dir: dir, session: session, mainSHA: mainSHA, featureSHA: featureSHA, mainText: string(mainText)}
}

func assertCheckoutUndo(t *testing.T, fixture checkoutUndoFixture, operationID, wantBranch string, wantDetached bool) {
	t.Helper()
	if err := fixture.session.UndoOperation(context.Background(), operationID); err != nil {
		t.Fatalf("undo checkout: %v", err)
	}
	head := strings.TrimSpace(gitOutput(t, fixture.dir, "rev-parse", "HEAD"))
	if head != fixture.mainSHA {
		t.Fatalf("checkout Undo HEAD = %s, want %s", head, fixture.mainSHA)
	}
	err := exec.Command("git", "-C", fixture.dir, "symbolic-ref", "--quiet", "HEAD").Run()
	if wantDetached && err == nil {
		t.Fatal("checkout Undo left HEAD attached")
	}
	if !wantDetached {
		if err != nil {
			t.Fatalf("checkout Undo left HEAD detached: %v", err)
		}
		if got := strings.TrimSpace(gitOutput(t, fixture.dir, "symbolic-ref", "--short", "HEAD")); got != wantBranch {
			t.Fatalf("checkout Undo branch = %s, want %s", got, wantBranch)
		}
	}
	if got, err := os.ReadFile(filepath.Join(fixture.dir, "a.txt")); err != nil || string(got) != fixture.mainText {
		t.Fatalf("checkout Undo restored wrong file: %q, %v", got, err)
	}
	binary, err := os.ReadFile(filepath.Join(fixture.dir, "local.bin"))
	if err != nil || !bytes.Equal(binary, []byte{0, 0xff, 7}) {
		t.Fatalf("checkout Undo changed untracked binary: %v, %v", binary, err)
	}
	info, err := os.Stat(filepath.Join(fixture.dir, "local.bin"))
	if err != nil || info.Mode().Perm() != 0o711 {
		t.Fatalf("checkout Undo changed untracked permissions: %v, %v", info, err)
	}
}

func TestUndoCheckoutHeadAndWorkspaceShapes(t *testing.T) {
	t.Run("branch to branch", func(t *testing.T) {
		f := newCheckoutUndoFixture(t)
		if err := f.session.CheckoutBranch(context.Background(), "feature"); err != nil {
			t.Fatal(err)
		}
		assertCheckoutUndo(t, f, onlyOperation(t, f.session).ID, "main", false)
	})
	t.Run("branch to detached", func(t *testing.T) {
		f := newCheckoutUndoFixture(t)
		if err := f.session.CheckoutCommit(context.Background(), Commit{Hash: f.featureSHA, ShortHash: f.featureSHA[:7]}); err != nil {
			t.Fatal(err)
		}
		assertCheckoutUndo(t, f, onlyOperation(t, f.session).ID, "main", false)
	})
	t.Run("detached to branch", func(t *testing.T) {
		f := newCheckoutUndoFixture(t)
		runGit(t, f.dir, "checkout", "--detach")
		if err := f.session.CheckoutBranch(context.Background(), "feature"); err != nil {
			t.Fatal(err)
		}
		assertCheckoutUndo(t, f, onlyOperation(t, f.session).ID, "", true)
	})
	t.Run("detached to detached", func(t *testing.T) {
		f := newCheckoutUndoFixture(t)
		runGit(t, f.dir, "checkout", "--detach")
		if err := f.session.CheckoutCommit(context.Background(), Commit{Hash: f.featureSHA, ShortHash: f.featureSHA[:7]}); err != nil {
			t.Fatal(err)
		}
		assertCheckoutUndo(t, f, onlyOperation(t, f.session).ID, "", true)
	})
	t.Run("force checkout preserves discarded work", func(t *testing.T) {
		f := newCheckoutUndoFixture(t)
		if err := os.WriteFile(filepath.Join(f.dir, "a.txt"), []byte("precious staged\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, f.dir, "add", "a.txt")
		index := gitOutput(t, f.dir, "ls-files", "--stage")
		if err := os.WriteFile(filepath.Join(f.dir, "a.txt"), []byte("precious unstaged\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := f.session.ForceCheckoutBranch(context.Background(), "feature"); err != nil {
			t.Fatal(err)
		}
		if err := f.session.UndoOperation(context.Background(), onlyOperation(t, f.session).ID); err != nil {
			t.Fatalf("undo force checkout: %v", err)
		}
		if got, _ := os.ReadFile(filepath.Join(f.dir, "a.txt")); string(got) != "precious unstaged\n" {
			t.Fatalf("force checkout Undo lost work: %q", got)
		}
		if got := gitOutput(t, f.dir, "ls-files", "--stage"); got != index {
			t.Fatalf("force checkout Undo did not restore staged index: %q", got)
		}
		if got := strings.TrimSpace(gitOutput(t, f.dir, "symbolic-ref", "--short", "HEAD")); got != "main" {
			t.Fatalf("force checkout Undo restored branch %q", got)
		}
	})
}

func TestUndoCheckoutRemoteBranchRestoresLocalRefsAndConfig(t *testing.T) {
	f := newCheckoutUndoFixture(t)
	remote := filepath.Join(t.TempDir(), "origin.git")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, remote, "init", "--bare")
	runGit(t, f.dir, "remote", "add", "origin", remote)
	runGit(t, f.dir, "push", "--set-upstream", "origin", "main")
	runGit(t, f.dir, "push", "origin", "feature")
	runGit(t, f.dir, "branch", "-D", "feature")
	beforeConfig, err := os.ReadFile(filepath.Join(f.dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.session.CheckoutRemoteBranch(context.Background(), "origin", "feature", "review-copy"); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, f.session)
	if len(record.RemoteEffects) != 0 {
		t.Fatal("a local checkout was incorrectly classified as a server-side effect")
	}
	if err := f.session.UndoOperation(context.Background(), record.ID); err != nil {
		t.Fatalf("undo remote checkout: %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, f.dir, "symbolic-ref", "--short", "HEAD")); got != "main" {
		t.Fatalf("remote checkout Undo restored %q, want main", got)
	}
	if err := exec.Command("git", "-C", f.dir, "show-ref", "--verify", "--quiet", "refs/heads/review-copy").Run(); err == nil {
		t.Fatal("new local branch survived Undo")
	}
	afterConfig, err := os.ReadFile(filepath.Join(f.dir, ".git", "config"))
	if err != nil || !bytes.Equal(afterConfig, beforeConfig) {
		t.Fatalf("remote checkout leaked upstream configuration: %s, %v", afterConfig, err)
	}
	if got := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "refs/remotes/origin/feature")); got != f.featureSHA {
		t.Fatalf("remote-tracking ref was incorrectly undone: %s", got)
	}
}

func TestUndoRenameBranchRestoresSymbolicHeadAndConfig(t *testing.T) {
	dir, session := recoveryRepository(t)
	runGit(t, dir, "checkout", "-b", "feature")
	runGit(t, dir, "config", "branch.feature.remote", "origin")
	runGit(t, dir, "config", "branch.feature.merge", "refs/heads/feature")
	beforeConfig, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RenameBranch(context.Background(), "feature", "renamed"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "symbolic-ref", "--short", "HEAD")); got != "renamed" {
		t.Fatalf("rename did not move symbolic HEAD: %s", got)
	}
	if err := session.UndoOperation(context.Background(), onlyOperation(t, session).ID); err != nil {
		t.Fatalf("undo branch rename: %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "symbolic-ref", "--short", "HEAD")); got != "feature" {
		t.Fatalf("rename Undo restored %q, want feature", got)
	}
	if err := exec.Command("git", "-C", dir, "show-ref", "--verify", "--quiet", "refs/heads/renamed").Run(); err == nil {
		t.Fatal("rename Undo retained the new branch ref")
	}
	afterConfig, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil || !bytes.Equal(afterConfig, beforeConfig) {
		t.Fatalf("rename Undo changed branch configuration: %s, %v", afterConfig, err)
	}
}
