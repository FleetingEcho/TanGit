package app

import (
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestTerminalDefaultShellName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell selection")
	}
	t.Setenv("SHELL", "/usr/bin/fish")
	if got := terminalDefaultShellName(); got != "fish" {
		t.Fatalf("shell name = %q, want fish", got)
	}
}

func TestTerminalWindowsShellNameUsesPluginFallbackOrder(t *testing.T) {
	const comspec = "C:/Windows/System32/cmd.exe"
	for _, test := range []struct {
		name      string
		available map[string]string
		want      string
		wantCalls []string
	}{
		{name: "pwsh", available: map[string]string{"pwsh.exe": "/tools/pwsh.exe"}, want: "pwsh", wantCalls: []string{"pwsh.exe"}},
		{name: "powershell", available: map[string]string{"powershell.exe": "/tools/powershell.exe"}, want: "powershell", wantCalls: []string{"pwsh.exe", "powershell.exe"}},
		{name: "COMSPEC", available: map[string]string{comspec: comspec}, want: "cmd", wantCalls: []string{"pwsh.exe", "powershell.exe", comspec}},
		{name: "cmd fallback", available: map[string]string{"cmd.exe": "C:/Windows/System32/cmd.exe"}, want: "cmd", wantCalls: []string{"pwsh.exe", "powershell.exe", comspec, "cmd.exe"}},
		{name: "no shell", available: map[string]string{}, wantCalls: []string{"pwsh.exe", "powershell.exe", comspec, "cmd.exe"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			lookPath := func(name string) (string, error) {
				calls = append(calls, name)
				if path := test.available[name]; path != "" {
					return path, nil
				}
				return "", errors.New("not installed")
			}
			if got := terminalWindowsShellName(comspec, lookPath); got != test.want {
				t.Fatalf("shell = %q, want %q", got, test.want)
			}
			if !slices.Equal(calls, test.wantCalls) {
				t.Fatalf("lookup order = %q, want %q", calls, test.wantCalls)
			}
		})
	}
}

func TestTerminalDisplayDirectoryAbbreviatesOnlyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	child := filepath.Join(home, "work", "lazygitApp")
	if got, want := terminalDisplayDirectory(child), filepath.Join("~", "work", "lazygitApp"); got != want {
		t.Fatalf("home child = %q, want %q", got, want)
	}
	outside := filepath.Join(filepath.Dir(home), filepath.Base(home)+"-elsewhere")
	if got := terminalDisplayDirectory(outside); got != outside {
		t.Fatalf("unrelated path was abbreviated to %q, want %q", got, outside)
	}
}

func TestTerminalSessionIdentityPrefersCustomNameAndReportsContext(t *testing.T) {
	session := terminalSessionState{shell: "bash", directory: "~/work/lazygitApp"}
	if got := terminalSessionLabel(&session); got != "bash" {
		t.Fatalf("default label = %q, want bash", got)
	}
	if got := terminalSessionDetail(&session); got != "~/work/lazygitApp" {
		t.Fatalf("directory detail = %q", got)
	}
	session.title = "vim main.go"
	if got := terminalSessionLabel(&session); got != "bash" {
		t.Fatalf("OSC title replaced the shell label: %q", got)
	}
	if got := terminalSessionDetail(&session); got != "vim main.go" {
		t.Fatalf("OSC title detail = %q", got)
	}
	session.name = "Build shell"
	if got := terminalSessionLabel(&session); got != "Build shell" {
		t.Fatalf("custom label = %q", got)
	}
}
