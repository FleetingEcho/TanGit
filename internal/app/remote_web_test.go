package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDeriveRemoteWebURL(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		want   string
	}{
		{name: "https", remote: "https://example.com/team/project.git", want: "https://example.com/team/project"},
		{name: "http custom port and base path", remote: "http://localhost:8080/git/team/project.git/", want: "http://localhost:8080/git/team/project"},
		{name: "https strips query and fragment", remote: "https://example.com/team/project.git?private=1#clone", want: "https://example.com/team/project"},
		{name: "scp syntax", remote: "git@example.com:team/project.git", want: "https://example.com/team/project"},
		{name: "scp absolute path", remote: "git@example.com:/team/project.git", want: "https://example.com/team/project"},
		{name: "ssh syntax", remote: "ssh://git@example.com/team/project.git", want: "https://example.com/team/project"},
		{name: "ssh port is not a web port", remote: "ssh://git@example.com:2222/team/project.git", want: "https://example.com/team/project"},
		{name: "git protocol", remote: "git://example.com/team/project.git", want: "https://example.com/team/project"},
		{name: "git ssh protocol", remote: "git+ssh://git@example.com/team/project.git", want: "https://example.com/team/project"},
		{name: "local path", remote: "/srv/team/project.git", want: ""},
		{name: "file URL", remote: "file:///srv/team/project.git", want: ""},
		{name: "unsupported scheme", remote: "javascript:alert(1)", want: ""},
		{name: "credential bearing URL", remote: "https://user:secret@example.com/team/project.git", want: ""},
		{name: "redacted credential URL", remote: "https://[redacted]@example.com/team/project.git", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deriveRemoteWebURL(test.remote); got != test.want {
				t.Fatalf("deriveRemoteWebURL(%q) = %q, want %q", test.remote, got, test.want)
			}
		})
	}
}

func TestNormalizeRemoteWebURLsRejectsUnsafeValues(t *testing.T) {
	got := normalizeRemoteWebURLs(map[string]map[string]string{
		" /work/project/ ": {
			" origin ": " HTTPS://example.com/team/project ",
			"script":   "javascript:alert(1)",
			"password": "https://user:secret@example.com/team/project",
			"empty":    "",
		},
		"": {"origin": "https://example.com/ignored"},
	})
	want := map[string]map[string]string{
		"/work/project": {"origin": "https://example.com/team/project"},
	}
	if len(got) != 1 || len(got["/work/project"]) != 1 || got["/work/project"]["origin"] != want["/work/project"]["origin"] {
		t.Fatalf("normalized overrides = %#v, want %#v", got, want)
	}
	for _, value := range []string{"", "example.com/team/project", "file:///tmp/project", "https://user@example.com/team/project", "https://example.com/\nproject"} {
		if normalized, ok := normalizeRemoteWebURL(value); ok {
			t.Errorf("normalizeRemoteWebURL(%q) accepted %q", value, normalized)
		}
	}
}

func TestRemoteWebMenuKeyboardAndPersistence(t *testing.T) {
	h := menuTestApp(t)
	h.app.pathUserData = t.TempDir()
	h.app.workspaceTab = sectionRemotes
	h.tester.Frame()

	h.rightClick(t, "origin")
	h.requireMenuLabels(t, "Open in browser", "Copy web URL", "Edit web URL…")
	h.chooseMenuItem(t, "Copy web URL")
	const derived = "https://example.com/team/project"
	if got := h.tester.Clipboard(); got != derived {
		t.Fatalf("copied web URL = %q, want %q", got, derived)
	}

	h.rightClick(t, "origin")
	h.chooseMenuItem(t, "Open in browser")
	if got := h.tester.OpenedURLs(); !slices.Equal(got, []string{derived}) {
		t.Fatalf("opened URLs = %q, want %q", got, []string{derived})
	}

	h.press(t, ui.KeyC)
	if got := h.tester.Clipboard(); got != derived {
		t.Fatalf("keyboard copied web URL = %q, want %q", got, derived)
	}
	h.press(t, ui.KeyO)
	if got := h.tester.OpenedURLs(); len(got) != 2 || got[1] != derived {
		t.Fatalf("keyboard opened URLs = %q", got)
	}

	h.tester.Key(ui.Shift, ui.KeyO)
	h.tester.Frame()
	if !h.app.showRemoteWebURLForm || h.app.remoteWebURL != derived {
		t.Fatalf("web URL form = open %v, value %q", h.app.showRemoteWebURLForm, h.app.remoteWebURL)
	}
	if err := h.tester.Click("Remote web URL"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(ui.Ctrl, ui.KeyA)
	h.tester.Type(" https://git.example.internal/repositories/desktop ")
	if err := h.tester.Click("Save web URL"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.showRemoteWebURLForm {
		t.Fatal("valid web URL did not close its form")
	}
	const override = "https://git.example.internal/repositories/desktop"
	loaded := loadAppSettings(h.app.pathUserData)
	if got := loaded.RemoteWebURLs["/work/project"]["origin"]; got != override {
		t.Fatalf("persisted web URL = %q, want %q", got, override)
	}
	h.press(t, ui.KeyO)
	if got := h.tester.OpenedURLs(); len(got) != 3 || got[2] != override {
		t.Fatalf("override opened URLs = %q", got)
	}

	h.app.settings.RemoteWebURLs["/work/project"]["upstream"] = "https://example.net/another/project"
	h.tester.Key(ui.Shift, ui.KeyO)
	h.tester.Frame()
	if err := h.tester.Click("Restore automatic"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.showRemoteWebURLForm || h.app.remoteWebAddress(h.app.snapshot.Remotes[0]) != derived {
		t.Fatal("restore did not return to the derived web URL")
	}
	loaded = loadAppSettings(h.app.pathUserData)
	if _, ok := loaded.RemoteWebURLs["/work/project"]["origin"]; ok {
		t.Fatal("restore left the manual origin override in settings")
	}
	if got := loaded.RemoteWebURLs["/work/project"]["upstream"]; got != "https://example.net/another/project" {
		t.Fatalf("restore changed another remote's override: %q", got)
	}
	h.press(t, ui.KeyO)
	if got := h.tester.OpenedURLs(); len(got) != 4 || got[3] != derived {
		t.Fatalf("restored automatic URL was not opened: %q", got)
	}
}

func TestRemoteWebManualOverrideForUnknownTransport(t *testing.T) {
	h := menuTestApp(t)
	h.app.workspaceTab = sectionRemotes
	h.app.snapshot.Remotes[0].URLs = []string{"/srv/project.git"}
	h.tester.Frame()
	if got := h.app.remoteWebAddress(h.app.snapshot.Remotes[0]); got != "" {
		t.Fatalf("local transport derived %q", got)
	}

	h.rightClick(t, "origin")
	h.chooseMenuItem(t, "Edit web URL…")
	h.tester.Frame()
	if err := h.tester.Click("Remote web URL"); err != nil {
		t.Fatal(err)
	}
	h.tester.Type("javascript:alert(1)")
	if err := h.tester.Click("Save web URL"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.app.showRemoteWebURLForm || h.app.remoteWebURLError == "" {
		t.Fatal("unsafe URL was not rejected in the form")
	}
	if err := h.tester.Click("Remote web URL"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(ui.Ctrl, ui.KeyA)
	h.tester.Type("http://code.example.local/team/project")
	if err := h.tester.Click("Save web URL"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.showRemoteWebURLForm || h.app.remoteWebAddress(h.app.snapshot.Remotes[0]) != "http://code.example.local/team/project" {
		t.Fatal("manual web URL did not replace the unknown transport")
	}
}

func TestRemoteWebRendersInLightAndDark(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) {
			h := menuTestApp(t)
			h.app.settings.Theme = "System"
			h.tester.SetDark(dark)
			h.app.workspaceTab = sectionRemotes
			h.app.openEditRemoteWebURL(h.app.snapshot.Remotes[0])
			h.tester.Frame()
			if h.app.crash != "" {
				t.Fatalf("render crashed: %s", h.app.crash)
			}
			for _, label := range []string{"Edit web URL", "Web URL", "Save web URL", "Only HTTP(S) addresses without credentials are allowed."} {
				if !h.tester.HasText(label) {
					t.Errorf("missing %q in %q", label, h.texts())
				}
			}
		})
	}
}

func TestRemoteWebOverrideFollowsRenameAndRemoval(t *testing.T) {
	a := &app{path: "/work/project", settings: appSettings{RemoteWebURLs: map[string]map[string]string{
		"/work/project": {"origin": "https://example.com/team/project"},
	}}}
	a.moveRemoteWebURLOverride("origin", "upstream")
	if got := a.settings.RemoteWebURLs["/work/project"]["upstream"]; got != "https://example.com/team/project" {
		t.Fatalf("renamed override = %q", got)
	}
	if _, ok := a.settings.RemoteWebURLs["/work/project"]["origin"]; ok {
		t.Fatal("old remote override survived rename")
	}
	a.moveRemoteWebURLOverride("upstream", "")
	if got := normalizeRemoteWebURLs(a.settings.RemoteWebURLs); got != nil {
		t.Fatalf("removed remote left overrides: %#v", got)
	}
}

func TestRemoteWebHelpIncludesActions(t *testing.T) {
	commands := keyboardCommands()
	for _, label := range []string{"Open the remote in a browser", "Copy the remote's web URL", "Edit the remote's web URL…"} {
		if !slices.ContainsFunc(commands, func(command keyboardCommand) bool {
			return command.group == "Remotes" && command.label == label
		}) {
			t.Errorf("keyboard command reference omits %q", label)
		}
	}
}
