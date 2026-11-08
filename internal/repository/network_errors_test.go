package repository

import (
	"context"
	"errors"
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
	for _, kind := range []error{ErrAuthentication, ErrPushRejected, ErrPushLeaseRejected, ErrPromptCancelled, ErrHostKeyVerification} {
		if errors.Is(err, kind) {
			t.Fatalf("DNS failure was classified as %v", kind)
		}
	}
	if got, want := err.Error(), "could not resolve host: example.invalid"; got != want {
		t.Fatalf("error %q, want %q", got, want)
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
