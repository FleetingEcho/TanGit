package sshworkspace

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProfileValidationAndAddress(t *testing.T) {
	t.Parallel()
	profile := Profile{
		ID: "Work_1.2-test", Name: "Work", Host: "2001:db8::1",
		Username: "alice", Port: 2222, AuthMode: AuthAgent, DefaultRoot: "~/work",
	}
	if err := profile.Validate(); err != nil {
		t.Fatalf("valid profile: %v", err)
	}
	if got, want := profile.Address(), "[2001:db8::1]:2222"; got != want {
		t.Fatalf("Address() = %q, want %q", got, want)
	}
	profile.Host = "[2001:db8::1]"
	if err := profile.Validate(); err != nil {
		t.Fatalf("bracketed IPv6 host: %v", err)
	}
	profile.Port = 0
	if got, want := profile.Address(), "[2001:db8::1]:22"; got != want {
		t.Fatalf("default Address() = %q, want %q", got, want)
	}

	tests := []struct {
		name    string
		mutate  func(*Profile)
		wantErr string
	}{
		{"empty ID", func(p *Profile) { p.ID = "" }, "profile ID"},
		{"slash ID", func(p *Profile) { p.ID = "bad/id" }, "profile ID"},
		{"empty host", func(p *Profile) { p.Host = "" }, "host"},
		{"host with port", func(p *Profile) { p.Host = "example.com:22" }, "must not include a port"},
		{"mismatched host bracket", func(p *Profile) { p.Host = "[2001:db8::1" }, "bracketed"},
		{"bad port", func(p *Profile) { p.Port = -1 }, "port"},
		{"empty username", func(p *Profile) { p.Username = "" }, "username"},
		{"unknown auth", func(p *Profile) { p.AuthMode = "certificate" }, "auth mode"},
		{"missing key path", func(p *Profile) { p.AuthMode = AuthKey }, "key path"},
		{"unused key path", func(p *Profile) { p.PrivateKeyPath = "/tmp/key" }, "only valid"},
		{"relative root", func(p *Profile) { p.DefaultRoot = "work" }, "default root"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := profile
			test.mutate(&candidate)
			if err := candidate.Validate(); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Validate() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestProfileJSONAndSecretRefs(t *testing.T) {
	t.Parallel()
	profile := Profile{ID: "server", Name: "Server", Host: "example.test", Username: "u", AuthMode: AuthPassword}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"password", "passphrase", "privateKey"} {
		if _, found := fields[forbidden]; found {
			t.Fatalf("profile JSON %s unexpectedly contains field %q", encoded, forbidden)
		}
	}
	password := PasswordSecretRef("server")
	passphrase := PassphraseSecretRef("server")
	if password != "sshworkspace/server/password" {
		t.Fatalf("password ref = %q", password)
	}
	if passphrase != "sshworkspace/server/private-key-passphrase" {
		t.Fatalf("passphrase ref = %q", passphrase)
	}
	if PasswordSecretRef("bad/id") != "" || PassphraseSecretRef("") != "" {
		t.Fatal("invalid profile ID produced a secret reference")
	}
}

func TestMemorySecretStoreDistinguishesEmptyAndMissing(t *testing.T) {
	store := NewMemorySecretStore()
	ref := PasswordSecretRef("server")
	if _, found, err := store.Get(ref); err != nil || found {
		t.Fatalf("new store Get found=%v err=%v", found, err)
	}
	if err := store.Set(ref, ""); err != nil {
		t.Fatal(err)
	}
	if value, found, err := store.Get(ref); err != nil || !found || value != "" {
		t.Fatalf("stored empty secret value=%q found=%v err=%v", value, found, err)
	}
	if err := store.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := store.Get(ref); found {
		t.Fatal("deleted secret was found")
	}
}
