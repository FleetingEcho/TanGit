package sshintegration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/sshintegration"
	"lazygitapp/internal/sshworkspace"
)

func TestServerProvidesAuthenticatedOpenSSHConnection(t *testing.T) {
	server := sshintegration.Start(t)
	if err := server.Profile.Validate(); err != nil {
		t.Fatalf("generated profile is invalid: %v", err)
	}
	if server.SecretStore == nil {
		t.Fatal("generated secret store is nil")
	}
	for _, name := range []string{server.HostKeyPath, server.Profile.PrivateKeyPath} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("private key %s mode = %#o, want 0600", name, got)
		}
	}
	stateInfo, err := os.Stat(filepath.Dir(server.HostKeyPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := stateInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("state directory mode = %#o, want 0700", got)
	}

	connector, err := sshworkspace.NewConnector(server.SecretStore, filepath.Join(t.TempDir(), "known_hosts"), func(info sshworkspace.HostKeyInfo) (bool, error) {
		if !strings.HasPrefix(info.FingerprintSHA256, "SHA256:") {
			t.Errorf("host-key fingerprint = %q", info.FingerprintSHA256)
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := connector.Connect(ctx, server.Profile)
	if err != nil {
		logContents, _ := os.ReadFile(filepath.Join(filepath.Dir(server.HostKeyPath), "sshd.log"))
		t.Fatalf("connect with generated key: %v\nsshd log:\n%s", err, logContents)
	}
	t.Cleanup(func() { _ = client.Close() })
	result, err := client.Run(ctx, "", "printf", "%s", "helper-ok")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || string(result.Output) != "helper-ok" {
		t.Fatalf("command result = %+v", result)
	}
}
