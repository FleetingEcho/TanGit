package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type agentIgnoredFixture struct {
	source  string
	dest    string
	session Session
}

func newAgentIgnoredFixture(t *testing.T) agentIgnoredFixture {
	t.Helper()
	fixture := agentIgnoredFixture{source: t.TempDir(), dest: t.TempDir()}
	for _, directory := range []string{fixture.source, fixture.dest} {
		runGit(t, directory, "init", "--initial-branch=main")
	}
	writeAgentIgnoredFixtureFile(t, fixture.source, ".gitignore", "*\n!visible.txt\n", 0o600)
	session, err := (Opener{}).Open(context.Background(), fixture.dest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.session = session
	return fixture
}

func writeAgentIgnoredFixtureFile(t *testing.T, root, relative, content string, mode os.FileMode) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, mode); err != nil {
		t.Fatal(err)
	}
}

func assertAgentIgnoredAbsent(t *testing.T, root, relative string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or could not be inspected: %v", relative, err)
	}
}

func TestAgentIgnoredFilesCopiesOnlyExplicitFiles(t *testing.T) {
	fixture := newAgentIgnoredFixture(t)
	writeAgentIgnoredFixtureFile(t, fixture.source, "nested/ignored.txt", "task configuration\n", 0o751)
	writeAgentIgnoredFixtureFile(t, fixture.source, ".env", "EXPLICIT_ONLY=1\n", 0o600)
	writeAgentIgnoredFixtureFile(t, fixture.source, "unlisted.txt", "must stay behind\n", 0o600)

	if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{"nested/ignored.txt"}); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(fixture.dest, "nested", "ignored.txt")
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "task configuration\n" {
		t.Fatalf("copied content = %q", content)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o751 {
		t.Fatalf("copied mode = %04o, want 0751", info.Mode().Perm())
	}
	assertAgentIgnoredAbsent(t, fixture.dest, ".env")
	assertAgentIgnoredAbsent(t, fixture.dest, "unlisted.txt")

	// .env has no blanket prohibition: its explicit, Git-ignored listing is
	// what authorizes this second copy.
	if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{".env"}); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(filepath.Join(fixture.dest, ".env"))
	if err != nil || string(content) != "EXPLICIT_ONLY=1\n" {
		t.Fatalf("explicit .env content = %q, err = %v", content, err)
	}
	assertAgentIgnoredAbsent(t, fixture.dest, "unlisted.txt")
}

func TestAgentIgnoredFilesPreflightCopiesNothing(t *testing.T) {
	for _, test := range []struct {
		name   string
		second string
	}{
		{name: "unignored", second: "visible.txt"},
		{name: "missing", second: "missing.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAgentIgnoredFixture(t)
			writeAgentIgnoredFixtureFile(t, fixture.source, "ignored.txt", "ignored\n", 0o600)
			if test.second == "visible.txt" {
				writeAgentIgnoredFixtureFile(t, fixture.source, test.second, "visible\n", 0o600)
			}
			err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{"ignored.txt", test.second})
			if err == nil || !strings.Contains(err.Error(), test.second) {
				t.Fatalf("error = %v, want path %q", err, test.second)
			}
			assertAgentIgnoredAbsent(t, fixture.dest, "ignored.txt")
			assertAgentIgnoredAbsent(t, fixture.dest, test.second)
		})
	}
}

func TestAgentIgnoredFilesRejectsUnsafeAndAmbiguousPaths(t *testing.T) {
	for _, relative := range []string{
		"../ignored.txt",
		"/absolute/ignored.txt",
		"C:/ignored.txt",
		"nested/../ignored.txt",
		"nested//ignored.txt",
		".git/config",
		"nested/.GIT/config",
	} {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			fixture := newAgentIgnoredFixture(t)
			writeAgentIgnoredFixtureFile(t, fixture.source, "ignored.txt", "ignored\n", 0o600)
			if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{relative}); err == nil {
				t.Fatalf("CopyAgentIgnoredFiles accepted %q", relative)
			}
			assertAgentIgnoredAbsent(t, fixture.dest, "ignored.txt")
		})
	}

	for _, paths := range [][]string{{"ignored.txt", "ignored.txt"}, {"ignored.txt", "IGNORED.txt"}} {
		fixture := newAgentIgnoredFixture(t)
		writeAgentIgnoredFixtureFile(t, fixture.source, "ignored.txt", "ignored\n", 0o600)
		if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, paths); err == nil || !strings.Contains(err.Error(), "duplicate or ambiguous") {
			t.Fatalf("duplicate paths %q: error = %v", paths, err)
		}
		assertAgentIgnoredAbsent(t, fixture.dest, "ignored.txt")
	}
}

func TestAgentIgnoredFilesNeverOverwritesDestination(t *testing.T) {
	for _, test := range []struct {
		name    string
		symlink bool
	}{
		{name: "regular file"},
		{name: "symlink", symlink: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAgentIgnoredFixture(t)
			writeAgentIgnoredFixtureFile(t, fixture.source, "occupied.txt", "new\n", 0o600)
			outside := filepath.Join(t.TempDir(), "outside.txt")
			if err := os.WriteFile(outside, []byte("keep\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(fixture.dest, "occupied.txt")
			if test.symlink {
				if err := os.Symlink(outside, destination); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(destination, []byte("keep\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{"occupied.txt"}); err == nil {
				t.Fatal("copy overwrote an existing destination")
			}
			content, err := os.ReadFile(outside)
			if err != nil || string(content) != "keep\n" {
				t.Fatalf("outside content = %q, err = %v", content, err)
			}
			if test.symlink {
				if info, err := os.Lstat(destination); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("destination symlink was replaced: info = %v, err = %v", info, err)
				}
			}
		})
	}
}

func TestAgentIgnoredFilesRejectsSymlinkSourcesAndParents(t *testing.T) {
	t.Run("source file symlink", func(t *testing.T) {
		fixture := newAgentIgnoredFixture(t)
		outside := filepath.Join(t.TempDir(), "outside.txt")
		if err := os.WriteFile(outside, []byte("secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(fixture.source, "linked.txt")); err != nil {
			t.Fatal(err)
		}
		if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{"linked.txt"}); err == nil {
			t.Fatal("copy accepted a source symlink")
		}
		assertAgentIgnoredAbsent(t, fixture.dest, "linked.txt")
	})

	t.Run("source parent symlink", func(t *testing.T) {
		fixture := newAgentIgnoredFixture(t)
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(fixture.source, "linked")); err != nil {
			t.Fatal(err)
		}
		if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{"linked/secret.txt"}); err == nil {
			t.Fatal("copy accepted a source parent symlink")
		}
		assertAgentIgnoredAbsent(t, fixture.dest, "linked/secret.txt")
	})

	t.Run("destination parent symlink", func(t *testing.T) {
		fixture := newAgentIgnoredFixture(t)
		writeAgentIgnoredFixtureFile(t, fixture.source, "nested/secret.txt", "secret\n", 0o600)
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(fixture.dest, "nested")); err != nil {
			t.Fatal(err)
		}
		if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{"nested/secret.txt"}); err == nil {
			t.Fatal("copy accepted a destination parent symlink")
		}
		assertAgentIgnoredAbsent(t, outside, "secret.txt")
	})
}

func TestAgentIgnoredFilesRejectsDirectoriesAndHonorsContext(t *testing.T) {
	fixture := newAgentIgnoredFixture(t)
	if err := os.Mkdir(filepath.Join(fixture.source, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fixture.session.CopyAgentIgnoredFiles(context.Background(), fixture.source, []string{"directory"}); err == nil {
		t.Fatal("copy accepted a directory")
	}
	writeAgentIgnoredFixtureFile(t, fixture.source, "ignored.txt", "ignored\n", 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.session.CopyAgentIgnoredFiles(ctx, fixture.source, []string{"ignored.txt"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled copy error = %v", err)
	}
	assertAgentIgnoredAbsent(t, fixture.dest, "ignored.txt")
}
