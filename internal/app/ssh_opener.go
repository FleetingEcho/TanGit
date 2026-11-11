package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/sshpool"
	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
)

// sshWorkspaceOpener is shared by the tabs of a window. Each app receives a
// lightweight binding containing its profile and secret stores, while the pool
// underneath shares only the authenticated transport.
type sshWorkspaceOpener struct {
	local repositoryOpener
	pool  *sshpool.Manager
	// Process-lifetime credentials are shared by every tab using this window's
	// profiles. They survive closing the tab that originally saved them.
	sshSecretsMu      sync.RWMutex
	sshSessionSecrets map[string]string
	// Only one unknown-host prompt can belong to a window at a time. A channel
	// makes waiting for that prompt cancelable when another handshake is asking.
	hostKeyGate chan struct{}
}

type appSSHRepositoryOpener struct {
	base *sshWorkspaceOpener
	app  *app
}

func newSSHWorkspaceOpener(local repositoryOpener) *sshWorkspaceOpener {
	return &sshWorkspaceOpener{
		local:       local,
		pool:        sshpool.NewManager(sshConnectorDialer{}),
		hostKeyGate: make(chan struct{}, 1),
	}
}

func (o *sshWorkspaceOpener) forApp(a *app) repositoryOpener {
	return appSSHRepositoryOpener{base: o, app: a}
}

// Open and Init keep the shared opener usable by the window shell. Applications
// normally receive forApp's binding; only a local path can be opened without
// that profile/credential context.
func (o *sshWorkspaceOpener) Open(ctx context.Context, reference string) (repository.Session, error) {
	location, err := workspace.Parse(reference)
	if err != nil {
		return nil, err
	}
	if location.IsRemote() {
		return nil, errors.New("open the SSH workspace through its owning application")
	}
	return o.local.Open(ctx, reference)
}

func (o *sshWorkspaceOpener) Init(ctx context.Context, reference string) error {
	location, err := workspace.Parse(reference)
	if err != nil {
		return err
	}
	if location.IsRemote() {
		return errors.New("initialize the SSH workspace through its owning application")
	}
	return o.local.Init(ctx, reference)
}

func (o *sshWorkspaceOpener) Close() error {
	return o.pool.Close()
}

type sshInteraction struct {
	secrets        sshworkspace.SecretStore
	knownHostsPath string
	confirm        sshworkspace.HostKeyConfirmation
}

type sshInteractionKey struct{}

type sshConnectorDialer struct{}

func (sshConnectorDialer) Connect(ctx context.Context, profile sshworkspace.Profile) (*sshworkspace.Client, error) {
	interaction, ok := ctx.Value(sshInteractionKey{}).(sshInteraction)
	if !ok {
		return nil, errors.New("SSH connection is missing its credential and host-key context")
	}
	connector, err := sshworkspace.NewConnector(interaction.secrets, interaction.knownHostsPath, interaction.confirm)
	if err != nil {
		return nil, err
	}
	return connector.Connect(ctx, profile)
}

func (o appSSHRepositoryOpener) interactionContext(ctx context.Context) (context.Context, error) {
	if o.app == nil {
		return nil, errors.New("SSH opener has no owning workspace")
	}
	knownHosts, err := o.app.sshKnownHostsPath()
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, sshInteractionKey{}, sshInteraction{
		secrets:        o.app.sshSecrets(),
		knownHostsPath: knownHosts,
		confirm: func(info sshworkspace.HostKeyInfo) (bool, error) {
			return o.app.confirmSSHHostKeyGated(ctx, info)
		},
	}), nil
}

type sshProfileResult struct {
	profile sshworkspace.Profile
	err     error
}

func (o appSSHRepositoryOpener) profile(ctx context.Context, id string) (sshworkspace.Profile, error) {
	if o.app == nil || o.app.update == nil {
		return sshworkspace.Profile{}, errors.New("SSH opener has no owning workspace")
	}
	// Settings are UI-owned and can be edited while an earlier open is still in
	// flight. Read the selected immutable value on that thread rather than
	// racing the form from the connection goroutine.
	result := make(chan sshProfileResult, 1)
	o.app.update(func() {
		settings := o.app.settings
		if !o.app.settingsLoaded {
			settings = loadAppSettings(o.app.pathUserData)
		}
		for _, profile := range settings.SSHProfiles {
			if profile.ID != id {
				continue
			}
			if err := profile.Validate(); err != nil {
				result <- sshProfileResult{err: fmt.Errorf("SSH profile %q is invalid: %w", id, err)}
			} else {
				result <- sshProfileResult{profile: profile}
			}
			return
		}
		result <- sshProfileResult{err: fmt.Errorf("SSH profile %q no longer exists", id)}
	})
	select {
	case resolved := <-result:
		return resolved.profile, resolved.err
	case <-ctx.Done():
		return sshworkspace.Profile{}, ctx.Err()
	}
}

func (o appSSHRepositoryOpener) acquire(ctx context.Context, location workspace.Location) (*sshpool.Lease, error) {
	profile, err := o.profile(ctx, location.ProfileID)
	if err != nil {
		return nil, err
	}
	connectionContext, err := o.interactionContext(ctx)
	if err != nil {
		return nil, err
	}
	lease, err := o.base.pool.Acquire(connectionContext, profile)
	if err != nil {
		return nil, fmt.Errorf("connect SSH workspace: %w", err)
	}
	return lease, nil
}

func (o appSSHRepositoryOpener) Open(ctx context.Context, reference string) (repository.Session, error) {
	location, err := workspace.Parse(reference)
	if err != nil {
		return nil, err
	}
	if !location.IsRemote() {
		return o.base.local.Open(ctx, reference)
	}
	lease, err := o.acquire(ctx, location)
	if err != nil {
		return nil, err
	}
	session, err := repository.OpenSSHWorkspace(ctx, lease.Client(), location, lease)
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	return session, nil
}

func (o appSSHRepositoryOpener) Init(ctx context.Context, reference string) error {
	location, err := workspace.Parse(reference)
	if err != nil {
		return err
	}
	if !location.IsRemote() {
		return o.base.local.Init(ctx, reference)
	}
	lease, err := o.acquire(ctx, location)
	if err != nil {
		return err
	}
	defer lease.Close()
	result, err := lease.Client().Run(ctx, "", "git", "init", "--", location.Root)
	if err != nil {
		if message := strings.TrimSpace(string(result.Output)); message != "" {
			return fmt.Errorf("initialize remote repository: %s", message)
		}
		return fmt.Errorf("initialize remote repository: %w", err)
	}
	return nil
}

func (a *app) resolveSSHRoot(ctx context.Context, profile sshworkspace.Profile, root string) (string, error) {
	opener, ok := a.opener.(appSSHRepositoryOpener)
	if !ok {
		return "", errors.New("home-relative SSH roots require the SSH workspace opener")
	}
	lease, err := opener.acquire(ctx, workspace.Location{Kind: workspace.SSH, ProfileID: profile.ID})
	if err != nil {
		return "", err
	}
	defer lease.Close()
	return lease.Client().ResolvePath(ctx, root)
}

func (a *app) sshKnownHostsPath() (string, error) {
	if a == nil || a.pathUserData == "" {
		return "", errors.New("SSH known_hosts directory is unavailable")
	}
	return filepath.Join(a.pathUserData, "ssh_known_hosts"), nil
}

// sshHostKeyPrompt lives on the UI side while a handshake waits on a buffered
// response. The handshake context cancels both the modal wait and late responses.
type sshHostKeyPrompt struct {
	info     sshworkspace.HostKeyInfo
	response chan bool
	once     sync.Once
}

func (a *app) confirmSSHHostKeyGated(ctx context.Context, info sshworkspace.HostKeyInfo) (bool, error) {
	opener, ok := a.opener.(appSSHRepositoryOpener)
	if !ok || opener.base == nil || opener.base.hostKeyGate == nil {
		return a.confirmSSHHostKey(ctx, info)
	}
	gate := opener.base.hostKeyGate
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return a.confirmSSHHostKey(ctx, info)
}

func (a *app) confirmSSHHostKey(ctx context.Context, info sshworkspace.HostKeyInfo) (bool, error) {
	prompt := &sshHostKeyPrompt{info: info, response: make(chan bool, 1)}
	presented := make(chan struct{})
	a.update(func() {
		a.sshHostKeyPrompt = prompt
		close(presented)
	})
	select {
	case <-presented:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case accepted := <-prompt.response:
		return accepted, nil
	case <-ctx.Done():
		a.update(func() {
			if a.sshHostKeyPrompt == prompt {
				a.sshHostKeyPrompt = nil
			}
		})
		return false, ctx.Err()
	}
}

func (a *app) respondSSHHostKey(accepted bool) {
	prompt := a.sshHostKeyPrompt
	if prompt == nil {
		return
	}
	a.sshHostKeyPrompt = nil
	prompt.once.Do(func() { prompt.response <- accepted })
}

// sshDraftSecretStore supplies a credential typed into a not-yet-saved Test
// Connection form, falling through to the stable profile store for other values.
type sshDraftSecretStore struct {
	base    sshworkspace.SecretStore
	profile sshworkspace.Profile
	secrets sshProfileSecrets
}

func (s sshDraftSecretStore) Get(ref sshworkspace.SecretRef) (string, bool, error) {
	if ref == sshworkspace.PasswordSecretRef(s.profile.ID) && s.secrets.Password != "" {
		return s.secrets.Password, true, nil
	}
	if ref == sshworkspace.PassphraseSecretRef(s.profile.ID) && s.secrets.Passphrase != "" {
		return s.secrets.Passphrase, true, nil
	}
	return s.base.Get(ref)
}

func (s sshDraftSecretStore) Set(ref sshworkspace.SecretRef, value string) error {
	return s.base.Set(ref, value)
}

func (s sshDraftSecretStore) Delete(ref sshworkspace.SecretRef) error {
	return s.base.Delete(ref)
}

func (a *app) testSSHConnection(ctx context.Context, profile sshworkspace.Profile, secrets sshProfileSecrets) (err error) {
	knownHosts, err := a.sshKnownHostsPath()
	if err != nil {
		return err
	}
	connector, err := sshworkspace.NewConnector(sshDraftSecretStore{base: a.sshSecrets(), profile: profile, secrets: secrets}, knownHosts, func(info sshworkspace.HostKeyInfo) (bool, error) {
		return a.confirmSSHHostKeyGated(ctx, info)
	})
	if err != nil {
		return err
	}
	client, err := connector.Connect(ctx, profile)
	if err != nil {
		return redactSSHProfileError(err, secrets)
	}
	defer func() { err = errors.Join(err, client.Close()) }()
	if _, err := client.HomeDirectory(ctx); err != nil {
		return redactSSHProfileError(err, secrets)
	}
	return nil
}
