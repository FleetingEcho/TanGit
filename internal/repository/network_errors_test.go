package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNetworkErrorCategories(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		lease bool
		want  error
	}{
		{name: "HTTP authentication", err: errors.New("fatal: Authentication failed for 'https://user:secret@example.com/repo.git'"), want: ErrAuthentication},
		{name: "SSH authentication", err: errors.New("git@example.com: Permission denied (publickey)."), want: ErrAuthentication},
		{name: "host key", err: errors.New("Host key verification failed."), want: ErrHostKeyVerification},
		{name: "changed host key", err: errors.New("REMOTE HOST IDENTIFICATION HAS CHANGED!"), want: ErrHostKeyVerification},
		{name: "cancelled prompt", err: errors.New("credential prompt cancelled by user"), want: ErrPromptCancelled},
		{name: "diverged pull", err: errors.New("fatal: Need to specify how to reconcile divergent branches."), want: ErrPullDiverged},
		{name: "push rejected", err: errors.New("! [rejected] main -> main (non-fast-forward)"), want: ErrPushRejected},
		{name: "lease rejected", err: errors.New("! [rejected] main -> main (stale info)"), lease: true, want: ErrPushLeaseRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := safeNetworkError(test.err, test.lease)
			if !errors.Is(got, test.want) {
				t.Fatalf("error %q has category %v, want %v", got, got, test.want)
			}
			if strings.Contains(got.Error(), "secret") || strings.Contains(got.Error(), "user:") {
				t.Fatalf("classified error retained credentials: %q", got)
			}
		})
	}
}

func TestNetworkErrorDoesNotClassifyOrdinaryFailures(t *testing.T) {
	err := safeNetworkError(errors.New("could not resolve host: example.invalid"), false)
	for _, kind := range []error{ErrAuthentication, ErrPullDiverged, ErrPushRejected, ErrPushLeaseRejected, ErrPromptCancelled, ErrHostKeyVerification} {
		if errors.Is(err, kind) {
			t.Fatalf("DNS failure was classified as %v", kind)
		}
	}
	if got, want := err.Error(), "could not resolve host: example.invalid"; got != want {
		t.Fatalf("error %q, want %q", got, want)
	}
}

func TestPrePushHookFailureIsNotRemoteRejection(t *testing.T) {
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "file.txt", "first\n", "initial")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, dir, "init", "--bare", remote)
	runGit(t, dir, "remote", "add", "origin", remote)
	hook := "#!/bin/sh\n" +
		"echo 'Try linting on changed files...'\n" +
		"echo '  × eslint(no-unused-vars): Identifier getGuiServerHandlerURL is imported but never used.' >&2\n" +
		"echo 'Found 98 warnings and 2 errors.' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", "pre-push"), []byte(hook), 0o700); err != nil {
		t.Fatal(err)
	}

	err := session.Push(context.Background())
	if err == nil {
		t.Fatal("push with a failing pre-push hook unexpectedly succeeded")
	}
	if errors.Is(err, ErrPushRejected) || errors.Is(err, ErrPushLeaseRejected) {
		t.Fatalf("pre-push lint failure was misreported as a remote rejection: %v", err)
	}
	if !strings.Contains(err.Error(), "getGuiServerHandlerURL") || !strings.Contains(err.Error(), "2 errors") {
		t.Fatalf("push error lost the lint diagnostics: %v", err)
	}
	entries := session.CommandLog().Entries()
	if len(entries) == 0 || !strings.Contains(entries[len(entries)-1].Command, "git push") {
		t.Fatalf("push command missing from the command log: %+v", entries)
	}
	output := entries[len(entries)-1].Output
	if !strings.Contains(output, "Try linting") || !strings.Contains(output, "getGuiServerHandlerURL") {
		t.Fatalf("command log lost mixed stdout/stderr hook diagnostics: %q", output)
	}
}

func TestPullWithoutUpstreamHasTypedError(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Pull(context.Background()); !errors.Is(err, ErrNoUpstream) {
		t.Fatalf("pull error = %v, want ErrNoUpstream", err)
	}
}
