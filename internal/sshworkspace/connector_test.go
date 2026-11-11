package sshworkspace

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestKnownHostsAcceptNewPersistAndAlwaysRejectChanged(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "nested", "known_hosts")
	confirmCalls := 0
	connector, err := NewConnector(nil, knownHosts, func(info HostKeyInfo) (bool, error) {
		confirmCalls++
		if info.Address != "example.test:22" || !strings.HasPrefix(info.FingerprintSHA256, "SHA256:") {
			t.Fatalf("unexpected confirmation info: %+v", info)
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.ensureKnownHosts(); err != nil {
		t.Fatal(err)
	}
	callback := connector.hostKeyCallback()
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}
	original := testPublicKey(t)
	changed := testPublicKey(t)

	if err := callback("example.test:22", remote, original); err != nil {
		t.Fatalf("accept new host: %v", err)
	}
	if confirmCalls != 1 {
		t.Fatalf("confirmation calls = %d, want 1", confirmCalls)
	}
	if err := callback("example.test:22", remote, original); err != nil {
		t.Fatalf("known host was not accepted: %v", err)
	}
	if confirmCalls != 1 {
		t.Fatalf("known host prompted again: %d calls", confirmCalls)
	}
	if err := callback("example.test:22", remote, changed); err == nil || !strings.Contains(err.Error(), "host key changed") {
		t.Fatalf("changed host key error = %v", err)
	}
	if confirmCalls != 1 {
		t.Fatalf("changed host key reached confirmation callback: %d calls", confirmCalls)
	}
	persisted, err := os.ReadFile(knownHosts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(persisted), "example.test ssh-ed25519 ") {
		t.Fatalf("known_hosts does not contain normalized host entry: %q", persisted)
	}
	if strings.Count(string(persisted), "\n") != 1 {
		t.Fatalf("changed or duplicate key was persisted: %q", persisted)
	}
}

func TestKnownHostsDeclinedNewKeyIsNotPersisted(t *testing.T) {
	connector, err := NewConnector(nil, filepath.Join(t.TempDir(), "known_hosts"), func(HostKeyInfo) (bool, error) {
		return false, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := connector.ensureKnownHosts(); err != nil {
		t.Fatal(err)
	}
	callback := connector.hostKeyCallback()
	err = callback("new.example:22", &net.TCPAddr{IP: net.ParseIP("192.0.2.2"), Port: 22}, testPublicKey(t))
	if err == nil || !strings.Contains(err.Error(), "not accepted") {
		t.Fatalf("declined key error = %v", err)
	}
	contents, readErr := os.ReadFile(connector.knownHostsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(contents) != 0 {
		t.Fatalf("declined key was persisted: %q", contents)
	}
}

func TestEncryptedPrivateKeyUsesStablePassphraseRef(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "test", []byte("correct horse"))
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewMemorySecretStore()
	profile := Profile{ID: "key-server", Host: "example.test", Username: "alice", AuthMode: AuthKey, PrivateKeyPath: keyPath}
	connector, err := NewConnector(store, filepath.Join(t.TempDir(), "known_hosts"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := connector.authMethod(context.Background(), profile); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("missing passphrase error = %v", err)
	}
	if err := store.Set(PassphraseSecretRef(profile.ID), "correct horse"); err != nil {
		t.Fatal(err)
	}
	method, closeAuth, err := connector.authMethod(context.Background(), profile)
	if err != nil {
		t.Fatalf("load encrypted key: %v", err)
	}
	if method == nil || closeAuth != nil {
		t.Fatal("private key authentication returned an unexpected method or cleanup function")
	}
}

func TestExpandPrivateKeyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	resolved, err := expandPrivateKeyPath("~/.ssh/id_ed25519")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".ssh", "id_ed25519"); resolved != want {
		t.Fatalf("resolved path = %q, want %q", resolved, want)
	}
	if _, err := expandPrivateKeyPath("~other/.ssh/id_ed25519"); err == nil {
		t.Fatal("~user expansion was unexpectedly accepted")
	}
}

func TestAgentAuthenticationRequiresSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	connector, err := NewConnector(nil, filepath.Join(t.TempDir(), "known_hosts"), nil)
	if err != nil {
		t.Fatal(err)
	}
	profile := Profile{ID: "agent", Host: "example.test", Username: "alice", AuthMode: AuthAgent}
	if _, _, err := connector.authMethod(context.Background(), profile); err == nil || !strings.Contains(err.Error(), "SSH_AUTH_SOCK") {
		t.Fatalf("agent error = %v", err)
	}
}

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
