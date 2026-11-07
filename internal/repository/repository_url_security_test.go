package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotRedactsRepositoryURLsAndEditPreservesOriginal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	fetchURL := "https://fetch-user:fetch-secret@example.com/private/repo.git"
	pushURL := "https://push-user:push-secret@example.com/private/repo.git?access_token=push-token"
	runGit(t, dir, "remote", "add", "origin", fetchURL)
	runGit(t, dir, "config", "remote.origin.pushurl", pushURL)
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte(
		"[submodule \"library\"]\n\tpath = vendor/library\n\turl = https://sub-user:sub-secret@example.com/library.git\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	opened, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := opened.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Remotes) != 1 {
		t.Fatalf("unexpected remotes %#v", snapshot.Remotes)
	}
	remote := snapshot.Remotes[0]
	if len(remote.URLs) != 1 || remote.URLs[0] != "https://[redacted]@example.com/private/repo.git" {
		t.Fatalf("redacted fetch URLs = %#v", remote.URLs)
	}
	if len(remote.PushURLs) != 1 || strings.Contains(remote.PushURLs[0], "push-secret") ||
		strings.Contains(remote.PushURLs[0], "push-token") || !strings.Contains(remote.PushURLs[0], "access_token=[redacted]") {
		t.Fatalf("redacted push URLs = %#v", remote.PushURLs)
	}
	if len(snapshot.Submodules) != 1 || snapshot.Submodules[0].URL != "https://[redacted]@example.com/library.git" {
		t.Fatalf("redacted submodules = %#v", snapshot.Submodules)
	}

	if err := opened.UpdateRemote(ctx, "origin", "backup", remote.URLs[0]); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "remote", "get-url", "backup")); got != fetchURL {
		t.Fatalf("saving the redacted form changed the original URL to %q", got)
	}
}

func TestRepositoryURLsCannotPersistNewCredentials(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	opened, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	credentialURL := "https://user:secret@example.com/private/repo.git"
	if err := opened.AddRemote(ctx, "origin", credentialURL); !errors.Is(err, ErrCredentialsInURL) {
		t.Fatalf("add remote error = %v, want ErrCredentialsInURL", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "remote")); got != "" {
		t.Fatalf("credential-bearing remote was persisted: %q", got)
	}
	if err := opened.AddSubmodule(ctx, "library", "vendor/library", credentialURL); !errors.Is(err, ErrCredentialsInURL) {
		t.Fatalf("add submodule error = %v, want ErrCredentialsInURL", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitmodules")); !os.IsNotExist(err) {
		t.Fatalf("credential-bearing submodule wrote .gitmodules: %v", err)
	}

	runGit(t, dir, "remote", "add", "origin", "https://example.com/public/repo.git")
	if err := opened.UpdateRemote(ctx, "origin", "origin", credentialURL); !errors.Is(err, ErrCredentialsInURL) {
		t.Fatalf("update remote error = %v, want ErrCredentialsInURL", err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "remote", "get-url", "origin")); got != "https://example.com/public/repo.git" {
		t.Fatalf("rejected edit changed the URL to %q", got)
	}
}
