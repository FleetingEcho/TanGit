package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
)

type countingCloseOwner struct {
	calls int
	err   error
}

func (o *countingCloseOwner) Close() error {
	o.calls++
	return o.err
}

func TestOpenSSHWorkspaceFailureClosesOwnerOnce(t *testing.T) {
	owner := &countingCloseOwner{}
	location := workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/remote/repository"}
	session, err := OpenSSHWorkspace(context.Background(), nil, location, owner)
	if err == nil || session != nil {
		t.Fatalf("OpenSSHWorkspace() = %#v, %v", session, err)
	}
	if owner.calls != 1 {
		t.Fatalf("owner Close calls = %d", owner.calls)
	}
}

func TestOpenSSHWorkspaceHonoursCancelledContextAndClosesOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	owner := &countingCloseOwner{}
	location := workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/remote/repository"}
	_, err := OpenSSHWorkspace(ctx, &sshworkspace.Client{}, location, owner)
	if !errors.Is(err, context.Canceled) || owner.calls != 1 {
		t.Fatalf("OpenSSHWorkspace() error = %v, owner closes = %d", err, owner.calls)
	}
}

func TestGitSessionCloseIsIdempotent(t *testing.T) {
	closeErr := errors.New("close failed")
	owner := &countingCloseOwner{err: closeErr}
	session := &gitSession{owner: owner}
	if err := session.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("first Close() = %v", err)
	}
	if err := session.Close(); !errors.Is(err, closeErr) || owner.calls != 1 {
		t.Fatalf("second Close() = %v, owner closes = %d", err, owner.calls)
	}
	var nilSession *gitSession
	if err := nilSession.Close(); err != nil {
		t.Fatalf("nil Close() = %v", err)
	}
	shared := &gitSession{}
	if err := shared.Close(); err != nil {
		t.Fatalf("nil-owner Close() = %v", err)
	}
}

func TestRemoteWatchIsIdleAndClosesWithContext(t *testing.T) {
	session := &gitSession{sshClient: &sshworkspace.Client{}}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := session.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-events:
		t.Fatalf("unexpected remote watch event, open = %v", ok)
	default:
	}
	cancel()
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("remote watch emitted an event while closing")
		}
	case <-time.After(time.Second):
		t.Fatal("remote watch did not close after cancellation")
	}
}

func TestSSHSessionInterfaces(t *testing.T) {
	var session Session = (*gitSession)(nil)
	if _, ok := session.(interface{ SSHClient() *sshworkspace.Client }); !ok {
		t.Fatal("session does not expose SSHClient")
	}
	if _, ok := session.(interface{ Workspace() workspace.Location }); !ok {
		t.Fatal("session does not expose Workspace")
	}
	if _, ok := session.(ProgressiveSession); !ok {
		t.Fatal("session does not implement ProgressiveSession")
	}
}
