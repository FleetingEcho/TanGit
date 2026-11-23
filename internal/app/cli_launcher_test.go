package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The Windows installer makes lazygitGo available on PATH.
// Keep the packager hook, its install/uninstall halves, and the environment
// notification together: a missing half leaves either no command or a stale
// PATH entry.
func TestWindowsCommandLineLauncherPackaging(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(source), "..", "..")
	data, err := os.ReadFile(filepath.Join(root, "mygo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Windows struct {
			InstallerInclude string `json:"installerInclude"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Windows.InstallerInclude == "" {
		t.Fatal("the Windows package does not install lazygitGo on PATH")
	}
	include, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(config.Windows.InstallerInclude)))
	if err != nil {
		t.Fatal(err)
	}
	script := string(include)
	for _, want := range []string{"!macro customInstall", "!macro customUnInstall", "LAZYGITGO_INSTALL_DIR", "lazygitgo-path.ps1", "WM_SETTINGCHANGE"} {
		if !strings.Contains(script, want) {
			t.Errorf("the Windows installer hook lacks %q", want)
		}
	}
	helper, err := os.ReadFile(filepath.Join(root, "resources", "windows", "lazygitgo-path.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ValidateSet('Add', 'Remove')", "SetEnvironmentVariable('Path'"} {
		if !strings.Contains(string(helper), want) {
			t.Errorf("the Windows PATH helper lacks %q", want)
		}
	}
}

// Exercise the distributed launcher with a recorder standing in for macOS
// LaunchServices, so CI verifies argument delivery without opening a GUI.
func TestMacOSCommandLineLauncher(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the macOS launcher is a POSIX shell script")
	}
	_, source, _, _ := runtime.Caller(0)
	launcher := filepath.Join(filepath.Dir(source), "..", "..", "resources", "darwin", "lazygitGo")
	root := t.TempDir()
	app := filepath.Join(root, "TanGit.app")
	if err := os.Mkdir(app, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo's $(touch UNEXPECTED) folder")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "open-arguments")
	open := filepath.Join(root, "open")
	if err := os.WriteFile(open, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$LAZYGITGO_LAUNCHER_TEST_ARGS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LAZYGITGO_APP", app)
	t.Setenv("LAZYGITGO_LAUNCHER_TEST_ARGS", output)
	physicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{repo}, {"./" + filepath.Base(repo)}, {"--", "./" + filepath.Base(repo)}} {
		cmd := exec.Command("/bin/sh", append([]string{launcher}, args...)...)
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("lazygitGo %q: %v: %s", args, err, data)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Join([]string{"-n", "-a", app, "--args", physicalRepo, ""}, "\n")
		if string(data) != want {
			t.Fatalf("LaunchServices received %q, want %q", data, want)
		}
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", launcher, filepath.Join(root, "missing"))
	if err := cmd.Run(); err == nil {
		t.Fatal("the launcher accepted a nonexistent directory")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("an invalid path launched the application")
	}
	if _, err := os.Stat(filepath.Join(root, "UNEXPECTED")); !os.IsNotExist(err) {
		t.Fatal("a repository path was interpreted as shell code")
	}
	executable := filepath.Join(app, "Contents", "MacOS", "lazygitGo")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if data, err := exec.Command("/bin/sh", launcher, "agent", "help").CombinedOutput(); err != nil || string(data) != "agent\nhelp\n" {
		t.Fatalf("headless launch: %v, output %q", err, data)
	}
	if runtime.GOOS == "linux" {
		// Use a temporary home and keep system Applications out of the test.
		// A macOS developer may have one of the real apps installed there.
		t.Setenv("HOME", root)
		t.Setenv("LAZYGITGO_APP", "")
		t.Setenv("TANGIT_APP", "")
		for _, name := range []string{"TanGit.app", "lazygitGo.app"} {
			bundle := filepath.Join(root, "Applications", name)
			if err := os.MkdirAll(bundle, 0o755); err != nil {
				t.Fatal(err)
			}
			if data, err := exec.Command("/bin/sh", launcher).CombinedOutput(); err != nil {
				t.Fatalf("launch %s: %v: %s", name, err, data)
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "-a\n"+bundle+"\n" {
				t.Fatalf("launch %s: arguments = %q, error = %v", name, data, err)
			}
		}
	}
}

func TestLinuxUpgradeRegistersNewCommand(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exercises the Linux compatibility launcher")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	_, source, _, _ := runtime.Caller(0)
	launcher := filepath.Join(filepath.Dir(source), "..", "..", "resources", "linux", "tangit")
	script, err := os.ReadFile(launcher)
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(root, ".local", "tangit.app")
	bin := filepath.Join(root, ".local", "bin")
	for _, dir := range []string{installed, bin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	legacy := filepath.Join(installed, "tangit")
	if err := os.WriteFile(legacy, script, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installed, "lazygitgo"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldCommand, newCommand := filepath.Join(bin, "tangit"), filepath.Join(bin, "lazygitGo")
	if err := os.Symlink(legacy, oldCommand); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{oldCommand, newCommand} {
		if data, err := exec.Command(command, "agent", "example with spaces").CombinedOutput(); err != nil || string(data) != "agent\nexample with spaces\n" {
			t.Fatalf("%s: %v, output %q", command, err, data)
		}
	}
}
