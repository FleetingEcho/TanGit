package sshpool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lazygitapp/internal/sshworkspace"
)

type fakeDialer struct {
	calls atomic.Int32
	dial  func(context.Context, sshworkspace.Profile) (*sshworkspace.Client, error)
}

func (d *fakeDialer) Connect(ctx context.Context, profile sshworkspace.Profile) (*sshworkspace.Client, error) {
	d.calls.Add(1)
	return d.dial(ctx, profile)
}

func testProfile(id string) sshworkspace.Profile {
	return sshworkspace.Profile{
		ID: id, Host: "example.test", Username: "git", AuthMode: sshworkspace.AuthAgent,
	}
}

func acquireResult(t *testing.T, manager *Manager, ctx context.Context, profile sshworkspace.Profile) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		lease, err := manager.Acquire(ctx, profile)
		if lease != nil {
			defer lease.Close()
		}
		result <- err
	}()
	return result
}

func receive(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Minute):
		t.Fatal("acquisition did not complete")
		return nil
	}
}

func TestManagerSharesOneConfiguration(t *testing.T) {
	dialer := &fakeDialer{dial: func(context.Context, sshworkspace.Profile) (*sshworkspace.Client, error) {
		return nil, nil
	}}
	manager := NewManager(dialer)
	defer manager.Close()
	profile := testProfile("one")
	first, err := manager.Acquire(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Acquire(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if dialer.calls.Load() != 1 || first.entry != second.entry {
		t.Fatalf("calls=%d entries=%p/%p, want one shared connection", dialer.calls.Load(), first.entry, second.entry)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	refs := first.entry.refs
	manager.mu.Unlock()
	if refs != 1 {
		t.Fatalf("references after idempotent close = %d, want 1", refs)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	_, retained := manager.entries[profile]
	manager.mu.Unlock()
	if retained {
		t.Fatal("transport retained after final lease")
	}

	other := testProfile("one")
	other.Port = 2222
	otherLease, err := manager.Acquire(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	defer otherLease.Close()
	if dialer.calls.Load() != 2 {
		t.Fatalf("calls=%d, want two configurations to use different transports", dialer.calls.Load())
	}
}

func TestCanceledWaiterDoesNotCancelOthers(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	dialer := &fakeDialer{dial: func(ctx context.Context, _ sshworkspace.Profile) (*sshworkspace.Client, error) {
		close(started)
		select {
		case <-finish:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	manager := NewManager(dialer)
	defer manager.Close()
	profile := testProfile("one")
	canceled, cancel := context.WithCancel(context.Background())
	first := acquireResult(t, manager, canceled, profile)
	second := acquireResult(t, manager, context.Background(), profile)
	<-started
	cancel()
	if err := receive(t, first); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquisition error = %v", err)
	}
	close(finish)
	if err := receive(t, second); err != nil {
		t.Fatalf("remaining acquisition failed: %v", err)
	}
}

func TestLastWaiterCancelsDial(t *testing.T) {
	started := make(chan struct{})
	dialCanceled := make(chan error, 1)
	dialer := &fakeDialer{dial: func(ctx context.Context, _ sshworkspace.Profile) (*sshworkspace.Client, error) {
		close(started)
		<-ctx.Done()
		dialCanceled <- ctx.Err()
		return nil, ctx.Err()
	}}
	manager := NewManager(dialer)
	defer manager.Close()
	ctx, cancel := context.WithCancel(context.Background())
	result := acquireResult(t, manager, ctx, testProfile("one"))
	<-started
	cancel()
	if err := receive(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquisition error = %v", err)
	}
	select {
	case err := <-dialCanceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dial cancellation = %v", err)
		}
	case <-time.After(time.Minute):
		t.Fatal("dial was not canceled")
	}
}

func TestManagerCloseDuringDial(t *testing.T) {
	started := make(chan struct{})
	dialer := &fakeDialer{dial: func(ctx context.Context, _ sshworkspace.Profile) (*sshworkspace.Client, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	manager := NewManager(dialer)
	result := acquireResult(t, manager, context.Background(), testProfile("one"))
	<-started
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, result); !errors.Is(err, ErrClosed) && !errors.Is(err, context.Canceled) {
		t.Fatalf("acquisition after close = %v", err)
	}
	if _, err := manager.Acquire(context.Background(), testProfile("one")); !errors.Is(err, ErrClosed) {
		t.Fatalf("new acquisition after close = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerConcurrentAcquisitionStress(t *testing.T) {
	dialer := &fakeDialer{dial: func(context.Context, sshworkspace.Profile) (*sshworkspace.Client, error) {
		return nil, nil
	}}
	manager := NewManager(dialer)
	profile := testProfile("one")
	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				lease, err := manager.Acquire(context.Background(), profile)
				if err != nil {
					t.Error(err)
					return
				}
				_ = lease.Close()
			}
		}()
	}
	wait.Wait()
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	manager.mu.Lock()
	remaining := len(manager.entries)
	manager.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("retained %d connections after stress test", remaining)
	}
}
