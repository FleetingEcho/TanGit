package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Exercise the distributed launcher with a recorder standing in for macOS
// LaunchServices, so CI verifies argument delivery without opening a GUI.
func TestMacOSCommandLineLauncher(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the macOS launcher is a POSIX shell script")
	}
	_, source, _, _ := runtime.Caller(0)
	launcher := filepath.Join(filepath.Dir(source), "..", "..", "resources", "darwin", "tangit")
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
	if err := os.WriteFile(open, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TANGIT_LAUNCHER_TEST_ARGS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TANGIT_APP", app)
	t.Setenv("TANGIT_LAUNCHER_TEST_ARGS", output)
	physicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{repo}, {"./" + filepath.Base(repo)}, {"--", "./" + filepath.Base(repo)}} {
		cmd := exec.Command("/bin/sh", append([]string{launcher}, args...)...)
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("tangit %q: %v: %s", args, err, data)
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
}
