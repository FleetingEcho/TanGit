package app

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The SSH settings page combines direct labels, translating helpers, and form
// controls. Render both of its states so a catalog omission cannot hide behind
// a successful tr call that silently falls back to English.
func TestLocalizedSSHProfileSettings(t *testing.T) {
	t.Cleanup(func() { currentLanguage = defaultLanguage })
	for _, choice := range languageChoices {
		if choice.Code == defaultLanguage {
			continue
		}
		t.Run(choice.Code, func(t *testing.T) {
			currentLanguage = choice.Code
			a := newApp(fakeOpener{})
			a.settingsLoaded = true
			a.settings.Language = choice.Code
			a.openSettings()
			tester := ui.NewTester(a.view, 1440, 960)
			tester.Frame()
			if err := tester.Click(tr("SSH profiles")); err != nil {
				t.Fatalf("open translated SSH profile settings: %v", err)
			}
			tester.Frame()
			assertLocalizedTexts(t, tester, choice.Code,
				"SSH profiles",
				"Remote Git workspaces, without credentials in settings",
				"Passwords and key passphrases use the system keychain when available.",
				"Add SSH profile…",
				"No SSH profiles yet",
				"Add a host to open a Git folder on it from Home.",
			)
			if err := tester.Click(tr("Add SSH profile…")); err != nil {
				t.Fatalf("open translated SSH profile form: %v", err)
			}
			tester.Frame()
			assertLocalizedTexts(t, tester, choice.Code,
				"Add SSH profile",
				"Profile name",
				"A label for this connection",
				"SSH host",
				"Host",
				"DNS name or IP address, without a port",
				"SSH username",
				"Username",
				"SSH port",
				"Port",
				"Authentication",
				"Agent, private key, or password",
				"Password",
				"Default remote folder",
				"Cancel",
				"Save SSH profile",
			)
			if tester.HasText("password") {
				t.Errorf("%s exposes the internal SSH password mode ID", choice.Code)
			}
			if err := tester.Click(tr("Password")); err != nil {
				t.Fatalf("open translated SSH authentication choices: %v", err)
			}
			tester.Frame()
			assertLocalizedTexts(t, tester, choice.Code, "SSH agent", "Private key", "Password")
			if err := tester.Click(tr("Private key")); err != nil {
				t.Fatalf("choose translated private-key authentication: %v", err)
			}
			tester.Frame()
			if got := a.sshProfileDraft.AuthMode; got != sshAuthKey {
				t.Fatalf("localized authentication choice stored %q, want %q", got, sshAuthKey)
			}
			assertLocalizedTexts(t, tester, choice.Code, "Private key", "A path on this machine", "SSH private-key path")
			if shots := os.Getenv("MYGO_SHOTS"); shots != "" && choice.Code == "zh-Hans" {
				saveScreenshot(t, shots, "ssh-profile-settings-zh-hans", tester)
			}
		})
	}
}

func assertLocalizedTexts(t *testing.T, tester *ui.Tester, language string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		want := translations[language][key]
		if want == "" || want == key {
			t.Errorf("%s has no translation for %q", language, key)
			continue
		}
		if !tester.HasText(want) {
			t.Errorf("%s does not render %q for %q: %q", language, want, key, tester.Texts())
		}
	}
}
