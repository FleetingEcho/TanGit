package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/sshpool"
	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
)

func TestSSHWorkspaceOpenerWithoutAppOnlyOpensLocalPaths(t *testing.T) {
	calls := &callRecorder{}
	root := filepath.Join(t.TempDir(), "project")
	local := fakeOpener{calls: calls, result: openResult{snapshot: repository.Snapshot{Root: root}}}
	opener := newSSHWorkspaceOpener(local)
	defer opener.Close()
	session, err := opener.Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Root != root {
		t.Fatalf("local root = %q, want %q", snapshot.Root, root)
	}
	remote := workspace.Location{Kind: workspace.SSH, ProfileID: "server", Root: "/srv/project"}.String()
	if _, err := opener.Open(context.Background(), remote); err == nil {
		t.Fatal("an SSH open without its application unexpectedly started a connection")
	}
}

func TestSSHProfileLookupReadsSettingsThroughTheUIUpdate(t *testing.T) {
	profile := sshworkspace.Profile{
		ID: "server", Host: "example.test", Username: "git", AuthMode: sshworkspace.AuthAgent,
	}
	updates := make(chan func(), 1)
	a := &app{
		settings:       appSettings{SSHProfiles: []sshworkspace.Profile{profile}},
		settingsLoaded: true,
		update:         func(fn func()) { updates <- fn },
	}
	opener := appSSHRepositoryOpener{base: &sshWorkspaceOpener{}, app: a}
	result := make(chan sshProfileResult, 1)
	go func() {
		resolved, err := opener.profile(context.Background(), profile.ID)
		result <- sshProfileResult{profile: resolved, err: err}
	}()
	apply, ok := <-updates
	if !ok {
		t.Fatal("profile lookup did not schedule its settings read")
	}
	select {
	case premature := <-result:
		t.Fatalf("profile lookup completed before its UI read: %+v", premature)
	default:
	}
	apply()
	resolved := <-result
	if resolved.err != nil || resolved.profile != profile {
		t.Fatalf("profile lookup = %+v, %v; want %+v", resolved.profile, resolved.err, profile)
	}
}

func TestSSHHostKeyPromptResponseAndCancellation(t *testing.T) {
	for _, test := range []struct {
		name     string
		cancel   bool
		accepted bool
		wantErr  error
	}{
		{name: "accepted", accepted: true},
		{name: "rejected"},
		{name: "cancelled", cancel: true, wantErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &app{}
			presented := make(chan struct{})
			a.update = func(fn func()) {
				fn()
				select {
				case <-presented:
				default:
					close(presented)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			var accepted bool
			go func() {
				var err error
				accepted, err = a.confirmSSHHostKey(ctx, sshworkspace.HostKeyInfo{Address: "example.test:22"})
				result <- err
			}()
			<-presented
			if test.cancel {
				cancel()
			} else {
				a.respondSSHHostKey(test.accepted)
			}
			if err := <-result; !errors.Is(err, test.wantErr) {
				t.Fatalf("confirmation error = %v, want %v", err, test.wantErr)
			}
			if test.cancel {
				// The cancellation cleanup itself is delivered through Update.
				<-presented
			}
			if !test.cancel && accepted != test.accepted {
				t.Fatalf("accepted = %v, want %v", accepted, test.accepted)
			}
			a.update(func() {
				if a.sshHostKeyPrompt != nil {
					t.Fatal("host-key prompt remained after its response")
				}
			})
			cancel()
		})
	}
}

func TestSSHHostKeyGateWaitHonorsCancellation(t *testing.T) {
	base := &sshWorkspaceOpener{pool: sshpool.NewManager(nil), hostKeyGate: make(chan struct{}, 1)}
	defer base.pool.Close()
	base.hostKeyGate <- struct{}{}
	a := &app{update: func(fn func()) { fn() }}
	a.opener = appSSHRepositoryOpener{base: base, app: a}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	accepted, err := a.confirmSSHHostKeyGated(ctx, sshworkspace.HostKeyInfo{})
	if accepted || !errors.Is(err, context.Canceled) {
		t.Fatalf("gated confirmation = %v, %v; want cancellation", accepted, err)
	}
	if a.sshHostKeyPrompt != nil {
		t.Fatal("a cancelled waiter presented a second host-key prompt")
	}
}
