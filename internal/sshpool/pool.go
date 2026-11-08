// Package sshpool shares authenticated SSH transports between workspaces.
package sshpool

import (
	"context"
	"errors"
	"sync"

	"lazygitapp/internal/sshworkspace"
)

// ErrClosed reports that the connection owner has shut down.
var ErrClosed = errors.New("SSH connection pool is closed")

// Dialer establishes one authenticated SSH/SFTP transport.
type Dialer interface {
	Connect(context.Context, sshworkspace.Profile) (*sshworkspace.Client, error)
}

// Manager owns transports until their final lease closes. Profile is the map
// key deliberately: editing any connection or authentication field creates a
// new configuration without disturbing existing workspaces.
type Manager struct {
	dialer Dialer

	mu      sync.Mutex
	entries map[sshworkspace.Profile]*entry
	closed  bool
}

type entry struct {
	ready        chan struct{}
	cancel       context.CancelFunc
	client       *sshworkspace.Client
	clientClosed bool
	err          error
	refs         int
	done         bool
}

// Lease is one workspace's ownership of a shared transport. Close is idempotent.
type Lease struct {
	manager *Manager
	profile sshworkspace.Profile
	entry   *entry
	once    sync.Once
}

// NewManager creates an empty pool. dialer is commonly an sshworkspace.Connector.
func NewManager(dialer Dialer) *Manager {
	return &Manager{dialer: dialer, entries: make(map[sshworkspace.Profile]*entry)}
}

// Acquire shares a connected transport or joins an in-flight connection. The
// dial has its own context so one caller abandoning a connection cannot cancel
// another caller that is waiting for it. The last waiter leaving cancels it.
func (m *Manager) Acquire(ctx context.Context, profile sshworkspace.Profile) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	e := m.entries[profile]
	if e == nil {
		dialContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
		e = &entry{ready: make(chan struct{}), cancel: cancel}
		m.entries[profile] = e
		e.refs = 1
		go m.connect(dialContext, profile, e)
	} else {
		e.refs++
	}
	m.mu.Unlock()

	select {
	case <-e.ready:
		m.mu.Lock()
		err := e.err
		if err == nil && m.closed {
			err = ErrClosed
		}
		m.mu.Unlock()
		if err != nil {
			m.release(profile, e)
			return nil, err
		}
		return &Lease{manager: m, profile: profile, entry: e}, nil
	case <-ctx.Done():
		m.release(profile, e)
		return nil, ctx.Err()
	}
}

func (m *Manager) connect(ctx context.Context, profile sshworkspace.Profile, e *entry) {
	client, err := m.dialer.Connect(ctx, profile)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	m.mu.Lock()
	e.client, e.err = client, err
	e.done = true
	close(e.ready)
	orphaned := e.refs == 0 || m.closed
	if orphaned {
		delete(m.entries, profile)
		if client != nil && !e.clientClosed {
			e.clientClosed = true
		} else {
			client = nil
		}
	} else {
		client = nil
	}
	e.cancel()
	m.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
}

// Client returns the shared transport. It remains valid until the lease closes.
func (l *Lease) Client() *sshworkspace.Client {
	if l == nil || l.entry == nil {
		return nil
	}
	return l.entry.client
}

// Close releases this lease and closes the transport after the final release.
func (l *Lease) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() { l.manager.release(l.profile, l.entry) })
	return nil
}

func (m *Manager) release(profile sshworkspace.Profile, e *entry) {
	m.mu.Lock()
	if e.refs > 0 {
		e.refs--
	}
	// A failed connection has no owner left to remove it once every waiter has
	// observed the error. Successful entries remain until their final release.
	unused := e.refs == 0 && e.done
	if unused && m.entries[profile] == e {
		delete(m.entries, profile)
	}
	cancel := e.cancel
	client := e.client
	cancelDial := e.refs == 0 && !e.done
	if unused && (client == nil || e.clientClosed) {
		client = nil
	} else if unused {
		e.clientClosed = true
	}
	m.mu.Unlock()
	if cancelDial {
		cancel()
	}
	if client != nil {
		_ = client.Close()
	}
}

// Close prevents new acquisitions and closes all connected transports. Leases
// may still be closed safely afterward. An in-flight dial is canceled and its
// late result is closed by connect.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	var clients []*sshworkspace.Client
	for profile, e := range m.entries {
		e.cancel()
		if e.done {
			delete(m.entries, profile)
			if e.client != nil && !e.clientClosed {
				e.clientClosed = true
				clients = append(clients, e.client)
			}
		}
	}
	m.mu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
	return nil
}
