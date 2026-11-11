package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/sshworkspace"
)

func TestSSHHostKeyModalKeepsParentOpen(t *testing.T) {
	for _, parent := range []string{"workspace", "settings"} {
		for _, action := range []string{"trust", "cancel", "escape"} {
			t.Run(parent+"/"+action, func(t *testing.T) {
				a := newApp(fakeOpener{})
				a.settingsLoaded = true
				a.settings.SSHProfiles = []sshworkspace.Profile{{ID: "server", Name: "Server", Host: "example.test", Username: "git", AuthMode: sshworkspace.AuthAgent, DefaultRoot: "~"}}
				if parent == "workspace" {
					a.openSSHWorkspacePicker()
				} else {
					a.openSettings()
					a.settingsPage = settingsPageSSHProfiles
				}
				prompt := &sshHostKeyPrompt{info: sshworkspace.HostKeyInfo{Address: "example.test:22", FingerprintSHA256: "SHA256:test"}, response: make(chan bool, 1)}
				a.sshHostKeyPrompt = prompt
				tester := ui.NewTester(a.view, 1280, 800)
				tester.Frame()
				switch action {
				case "trust":
					if err := tester.Click("Trust new host"); err != nil {
						t.Fatal(err)
					}
				case "cancel":
					if err := tester.Click("Cancel connection"); err != nil {
						t.Fatal(err)
					}
				case "escape":
					tester.Key(0, ui.KeyEscape)
				}
				tester.Frame()
				select {
				case accepted := <-prompt.response:
					if accepted != (action == "trust") {
						t.Fatalf("accepted = %v for %s", accepted, action)
					}
				default:
					t.Fatal("host-key response was not delivered")
				}
				// The child disappears and the parent's controls return on later builds.
				tester.Frame()
				tester.Frame()
				if a.sshHostKeyPrompt != nil {
					t.Fatal("host-key dialog remained open")
				}
				if parent == "workspace" && !a.sshWorkspaceOpen {
					t.Fatal("host-key action closed the workspace picker")
				}
				if parent == "settings" && !a.settingsOpen {
					t.Fatal("host-key action closed Settings")
				}
			})
		}
	}
}
