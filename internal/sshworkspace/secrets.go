package sshworkspace

import (
	"errors"
	"fmt"
	"sync"
)

// SecretRef is a stable, nonsecret key used to look up a profile credential.
type SecretRef string

const (
	passwordSecretKind   = "password"
	passphraseSecretKind = "private-key-passphrase"
)

// PasswordSecretRef identifies a profile's SSH password. It depends only on
// profileID, so host and username changes do not orphan a stored secret.
func PasswordSecretRef(profileID string) SecretRef {
	return secretRef(profileID, passwordSecretKind)
}

// PassphraseSecretRef identifies the optional passphrase for the private key
// configured by a profile. The private key itself is never put in SecretStore.
func PassphraseSecretRef(profileID string) SecretRef {
	return secretRef(profileID, passphraseSecretKind)
}

func secretRef(profileID, kind string) SecretRef {
	if !ValidProfileID(profileID) {
		return ""
	}
	return SecretRef("sshworkspace/" + profileID + "/" + kind)
}

// SecretStore stores credential values separately from JSON-friendly Profiles.
// found=false indicates that no value exists. Implementations may persist to a
// system keyring or keep values only for the current application session.
type SecretStore interface {
	Get(ref SecretRef) (value string, found bool, err error)
	Set(ref SecretRef, value string) error
	Delete(ref SecretRef) error
}

// MemorySecretStore is a concurrency-safe session-only SecretStore. Values are
// retained in process memory until overwritten, deleted, or the store is
// discarded.
type MemorySecretStore struct {
	mu      sync.RWMutex
	secrets map[SecretRef]string
}

// NewMemorySecretStore creates an empty session-only store.
func NewMemorySecretStore() *MemorySecretStore {
	return &MemorySecretStore{secrets: make(map[SecretRef]string)}
}

func (s *MemorySecretStore) Get(ref SecretRef) (string, bool, error) {
	if s == nil {
		return "", false, errors.New("nil memory secret store")
	}
	s.mu.RLock()
	value, found := s.secrets[ref]
	s.mu.RUnlock()
	return value, found, nil
}

func (s *MemorySecretStore) Set(ref SecretRef, value string) error {
	if s == nil {
		return errors.New("nil memory secret store")
	}
	if ref == "" {
		return errors.New("empty secret reference")
	}
	s.mu.Lock()
	if s.secrets == nil {
		s.secrets = make(map[SecretRef]string)
	}
	s.secrets[ref] = value
	s.mu.Unlock()
	return nil
}

func (s *MemorySecretStore) Delete(ref SecretRef) error {
	if s == nil {
		return errors.New("nil memory secret store")
	}
	s.mu.Lock()
	delete(s.secrets, ref)
	s.mu.Unlock()
	return nil
}

func loadSecret(store SecretStore, ref SecretRef, description string) (string, error) {
	if store == nil {
		return "", fmt.Errorf("%s requires a secret store", description)
	}
	if ref == "" {
		return "", errors.New("invalid secret reference")
	}
	value, found, err := store.Get(ref)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", description, err)
	}
	if !found {
		return "", fmt.Errorf("%s is not available in the secret store", description)
	}
	return value, nil
}
