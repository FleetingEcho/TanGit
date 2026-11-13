package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/sshworkspace"
)

type fakeSSHKeychain struct {
	values map[string][]byte
	fail   bool
}

func (f *fakeSSHKeychain) Set(account string, value []byte) error {
	if f.fail {
		return errors.New("keychain write failed")
	}
	if f.values == nil {
		f.values = map[string][]byte{}
	}
	f.values[account] = append([]byte(nil), value...)
	return nil
}

func (f *fakeSSHKeychain) Get(account string) ([]byte, error) {
	if f.fail {
		return nil, errors.New("keychain read failed")
	}
	value, ok := f.values[account]
	if !ok {
		return nil, errors.New("not found")
	}
	return append([]byte(nil), value...), nil
}

func (f *fakeSSHKeychain) Delete(account string) error {
	if f.fail {
		return errors.New("keychain delete failed")
	}
	delete(f.values, account)
	return nil
}

func TestNormalizeSSHProfilesStableIDsAndDefaults(t *testing.T) {
	profiles := []sshworkspace.Profile{
		{ID: "Team One", Name: " Team One ", Host: " one.example.com ", Username: " git "},
		{ID: "Team One", Name: "Duplicate", Host: "two.example.com", Username: "git", Port: 65536},
		{ID: "Build_7", Name: "Build", Host: "build.example.com", Username: "builder", Port: 2222, AuthMode: sshworkspace.AuthKey, PrivateKeyPath: " ~/.ssh/build "},
	}
	got := normalizeSSHProfiles(profiles)
	if got[0].ID != "team-one" || got[1].ID != "team-one-2" || got[2].ID != "Build_7" {
		t.Fatalf("normalized IDs = %q, %q, %q", got[0].ID, got[1].ID, got[2].ID)
	}
	for i, profile := range got {
		if !sshworkspace.ValidProfileID(profile.ID) {
			t.Errorf("profile %d has invalid ID %q", i, profile.ID)
		}
	}
	if got[0].Port != sshworkspace.DefaultPort || got[1].Port != sshworkspace.DefaultPort || got[2].Port != 2222 {
		t.Fatalf("normalized ports = %d, %d, %d", got[0].Port, got[1].Port, got[2].Port)
	}
	if got[0].Host != "one.example.com" || got[0].Username != "git" || got[0].AuthMode != sshworkspace.AuthAgent {
		t.Fatalf("first profile was not cleaned: %+v", got[0])
	}
	if got[2].PrivateKeyPath != "~/.ssh/build" {
		t.Fatalf("key path = %q", got[2].PrivateKeyPath)
	}
	if again := normalizeSSHProfiles(got); !reflect.DeepEqual(again, got) {
		t.Fatalf("normalization was not stable:\nfirst:  %+v\nsecond: %+v", got, again)
	}
}

func TestSSHProfileValidationTranslatesKnownMessagesOnly(t *testing.T) {
	const unknown = "an arbitrary backend or user payload"
	catalog := map[string]string{
		unknown:                                "must not be used",
		"Enter an SSH port from 1 to 65535.":   "translated port requirement",
		"Enter an SSH password.":               "translated password requirement",
		"invalid SSH host %q":                  "translated host %q",
		"SSH host must not include a port: %q": "translated host port %q",
		"invalid SSH port %d":                  "translated numeric port %d",
	}
	stubScopedTranslationCatalog(t, catalog)
	for _, test := range []struct {
		message string
		want    string
	}{
		{"Enter an SSH port from 1 to 65535.", "translated port requirement"},
		{"Enter an SSH password.", "translated password requirement"},
		{`invalid SSH host "bad host"`, `translated host "bad host"`},
		{`SSH host must not include a port: "example.com:22"`, `translated host port "example.com:22"`},
		{"invalid SSH port 70000", "translated numeric port 70000"},
		{unknown, unknown},
	} {
		if got := sshProfileValidationMessage(errors.New(test.message)); got != test.want {
			t.Errorf("validation message %q = %q, want %q", test.message, got, test.want)
		}
	}
}

func TestSSHProfileValidationUsesRealCatalogs(t *testing.T) {
	messages := []struct {
		key       string
		formatted bool
		argument  any
	}{
		{key: "Enter an SSH port from 1 to 65535."},
		{key: "Enter an SSH password."},
		{key: "SSH username must be nonempty and contain no whitespace or control characters"},
		{key: "private-key authentication requires a private key path"},
		{key: "private key path is only valid with private-key authentication"},
		{key: "SSH host must be nonempty and have no surrounding whitespace"},
		{key: "SSH default root contains a control character"},
		{key: "SSH default root must be absolute or home-relative"},
		{key: "invalid SSH profile ID %q", formatted: true, argument: "bad id"},
		{key: "invalid SSH host %q", formatted: true, argument: "bad host"},
		{key: "invalid bracketed SSH host %q", formatted: true, argument: "[bad"},
		{key: "SSH host must not include a port: %q", formatted: true, argument: "example.com:22"},
		{key: "unsupported SSH auth mode %q", formatted: true, argument: "unknown"},
		{key: "invalid SSH port %d", formatted: true, argument: 70000},
	}
	if len(messages) != 14 {
		t.Fatalf("real SSH validation catalog fixture has %d messages, want 14", len(messages))
	}
	t.Cleanup(func() { currentLanguage = defaultLanguage })
	for _, choice := range languageChoices[1:] {
		t.Run(choice.Code, func(t *testing.T) {
			currentLanguage = choice.Code
			catalog := translations[choice.Code]
			for _, test := range messages {
				message, want := test.key, catalog[test.key]
				if want == "" {
					t.Errorf("%s has no real-catalog translation for SSH validation %q", choice.Code, test.key)
					continue
				}
				if test.formatted {
					message = fmt.Sprintf(test.key, test.argument)
					want = fmt.Sprintf(want, test.argument)
				}
				if got := sshProfileValidationMessage(errors.New(message)); got != want {
					t.Errorf("%s SSH validation %q = %q, want %q", choice.Code, test.key, got, want)
				}
			}
		})
	}
}

func TestSSHConnectionNoticesTranslateWithoutTranslatingPayloads(t *testing.T) {
	const payload = "dial tcp: server-specific failure"
	stubScopedTranslationCatalog(t, map[string]string{
		"SSH connection testing is not wired yet.": "translated unavailable notice",
		"SSH connection failed: %s":                "translated failure: %s",
		payload:                                    "must not be used",
	})
	a := newApp(fakeOpener{})
	updates := newUpdates()
	a.update = updates.update
	a.sshTestConnection = nil
	a.testSSHProfile()
	if got := a.sshProfileFormNotice; got != "translated unavailable notice" {
		t.Fatalf("test notice = %q", got)
	}
	a.sshSecretBackend = &fakeSSHKeychain{}
	a.sshProfileDraft = sshProfileDraft{
		Host: "example.com", Username: "git", Port: "22", AuthMode: sshAuthPassword, Password: "secret",
	}
	a.sshTestConnection = func(context.Context, sshworkspace.Profile, sshProfileSecrets) error {
		return errors.New(payload)
	}
	a.testSSHProfile()
	updates.waitForUpdate(t)
	if got, want := a.sshProfileFormError, "translated failure: "+payload; got != want {
		t.Fatalf("connection error = %q, want %q", got, want)
	}
}

func TestSSHProfilePersistenceContainsNoSecrets(t *testing.T) {
	dir := t.TempDir()
	const password = "correct-horse-battery-staple"
	const passphrase = "a-private-key-passphrase"
	a := newApp(fakeOpener{})
	a.pathUserData = dir
	a.sshSecretBackend = &fakeSSHKeychain{}
	a.sshProfileDraft = sshProfileDraft{
		Name: "Production", Host: "prod.example.com", Username: "git", Port: "2222",
		AuthMode: sshAuthPassword, DefaultRoot: "/srv/git/project",
		Password: password, Passphrase: passphrase,
	}
	a.submitSSHProfileForm(nil)
	if len(a.settings.SSHProfiles) != 1 {
		t.Fatalf("saved profiles = %+v", a.settings.SSHProfiles)
	}
	if a.sshProfileDraft.Password != "" || a.sshProfileDraft.Passphrase != "" {
		t.Fatal("sensitive form drafts survived Save")
	}
	data, err := os.ReadFile(settingsStorePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{password, passphrase} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("settings.json contains a secret:\n%s", data)
		}
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	profiles := generic["sshProfiles"].([]any)[0].(map[string]any)
	for _, forbidden := range []string{"password", "passphrase"} {
		if _, exists := profiles[forbidden]; exists {
			t.Fatalf("profile JSON gained sensitive field %q: %v", forbidden, profiles)
		}
	}
	loaded := loadAppSettings(dir)
	if !reflect.DeepEqual(loaded.SSHProfiles, a.settings.SSHProfiles) {
		t.Fatalf("profiles did not round-trip: got %+v want %+v", loaded.SSHProfiles, a.settings.SSHProfiles)
	}
	backend := a.sshSecretBackend.(*fakeSSHKeychain)
	profileID := a.settings.SSHProfiles[0].ID
	if got := string(backend.values[string(sshworkspace.PasswordSecretRef(profileID))]); got != password {
		t.Fatalf("keychain password = %q", got)
	}
}

func TestSSHKeychainMessagesTranslateAtTheirSource(t *testing.T) {
	stubScopedTranslationCatalog(t, map[string]string{
		"invalid SSH secret reference":       "translated invalid reference",
		"SSH system keychain is unavailable": "translated unavailable keychain",
	})
	a := &app{sshSecretBackend: &fakeSSHKeychain{fail: true}, sshSessionSecrets: map[string]string{}}
	if _, _, err := a.sshSecrets().Get(""); err == nil || err.Error() != "translated invalid reference" {
		t.Fatalf("invalid reference error = %v", err)
	}
	ref := sshworkspace.PasswordSecretRef("prod")
	if _, _, err := a.sshSecrets().Get(ref); err == nil || err.Error() != "translated unavailable keychain" {
		t.Fatalf("keychain error = %v", err)
	}
}

func TestSSHSecretStoreFallsBackWithoutLeakingKeychainErrors(t *testing.T) {
	const secret = "do-not-leak-this-password"
	backend := &fakeSSHKeychain{fail: true}
	a := &app{sshSecretBackend: backend, sshSessionSecrets: map[string]string{}}
	store := a.sshSecrets()
	ref := sshworkspace.PasswordSecretRef("prod")
	if err := store.Set(ref, secret); !errors.Is(err, errSSHSecretSessionOnly) {
		t.Fatalf("Set error = %v, want session-only notice", err)
	} else if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "keychain write failed") {
		t.Fatalf("Set leaked backend details: %v", err)
	}
	got, found, err := store.Get(ref)
	if err != nil || !found || got != secret {
		t.Fatalf("fallback Get = %q, %v, %v", got, found, err)
	}
	if err := store.Delete(ref); !errors.Is(err, errSSHSecretSessionOnly) {
		t.Fatalf("Delete error = %v, want session-only notice", err)
	}
	if got, found, err := store.Get(ref); err != nil || found || got != "" {
		t.Fatalf("deleted fallback secret = %q, %v, %v", got, found, err)
	}
}

func TestSSHSessionSecretsAreSharedByTabs(t *testing.T) {
	const secret = "one-window-credential"
	opener := newSSHWorkspaceOpener(fakeOpener{})
	defer opener.Close()
	backend := &fakeSSHKeychain{fail: true}
	first, second := newApp(opener), newApp(opener)
	first.sshSecretBackend = backend
	second.sshSecretBackend = backend
	ref := sshworkspace.PasswordSecretRef("shared")
	if err := first.sshSecrets().Set(ref, secret); !errors.Is(err, errSSHSecretSessionOnly) {
		t.Fatal(err)
	}
	value, found, err := second.sshSecrets().Get(ref)
	if err != nil || !found || value != secret {
		t.Fatalf("second tab secret = %q, %v, %v", value, found, err)
	}
	if err := first.sshSecrets().Delete(ref); !errors.Is(err, errSSHSecretSessionOnly) {
		t.Fatal(err)
	}
	if value, found, err = second.sshSecrets().Get(ref); err != nil || found || value != "" {
		t.Fatalf("second tab did not share deletion tombstone: %q, %v, %v", value, found, err)
	}
}

func TestHomeHasVisibleSSHWorkspaceEntryAndInlineProfileForm(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settingsLoaded = true
	tester := ui.NewTester(a.view, 1280, 800)
	tester.Frame()
	if !tester.HasText("Open SSH") {
		t.Fatalf("Home has no SSH workspace entry: %q", tester.Texts())
	}
	if err := tester.Click("Open SSH"); err != nil {
		t.Fatalf("open SSH workspace picker: %v", err)
	}
	tester.Frame()
	if !a.sshWorkspaceOpen || !a.sshProfileFormOpen {
		t.Fatalf("empty Home picker did not open its inline profile form: open=%v form=%v", a.sshWorkspaceOpen, a.sshProfileFormOpen)
	}
	for _, label := range []string{"Add SSH profile", "SSH host", "SSH username", "SSH port", "SSH password"} {
		if !tester.HasText(label) {
			t.Errorf("inline profile form is missing %q: %q", label, tester.Texts())
		}
	}
}

func TestSettingsRailOpensSSHProfileForm(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settingsLoaded = true
	a.openSettings()
	tester := ui.NewTester(a.view, 1280, 800)
	tester.Frame()
	if err := tester.Click("SSH profiles settings"); err != nil {
		t.Fatalf("open SSH profile settings: %v", err)
	}
	tester.Frame()
	if !tester.HasText("No SSH profiles yet") {
		t.Fatalf("SSH profile page is not visible: %q", tester.Texts())
	}
	if err := tester.Click("Add SSH profile…"); err != nil {
		t.Fatalf("open Add SSH profile form: %v", err)
	}
	tester.Frame()
	if !a.sshProfileFormOpen {
		t.Fatal("Add did not open the SSH profile form")
	}
	for _, label := range []string{"Host", "Username", "Port", "Authentication", "Default remote folder"} {
		if !tester.HasText(label) {
			t.Errorf("SSH profile form is missing %q: %q", label, tester.Texts())
		}
	}
}

func TestOpenSelectedSSHWorkspaceUsesCanonicalLocation(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, _ string, _ bool) (sshFolderListing, error) {
		return sshFolderListing{Root: "/srv/git/project", Repository: true}, nil
	}
	a.settings.SSHProfiles = []sshworkspace.Profile{{
		ID: "Build_7", Name: "Build", Host: "build.example.com", Username: "git",
		Port: 22, AuthMode: sshworkspace.AuthAgent, DefaultRoot: "/srv/git/project",
	}}
	a.sshWorkspaceOpen = true
	a.sshWorkspaceProfileID = "Build_7"
	a.sshWorkspaceRoot = "/srv/git/./project/../project"
	var claimed string
	a.claimOpen = func(path string) bool {
		claimed = path
		return true
	}
	a.openSelectedSSHWorkspace()
	applySSHBrowserUpdate(t, updates)
	const want = "ssh://Build_7/srv/git/project"
	if a.path != want || claimed != want {
		t.Fatalf("open path = %q, claimed = %q; want %q", a.path, claimed, want)
	}
	if a.sshWorkspaceOpen {
		t.Fatal("SSH picker remained open after beginning an open")
	}
}
