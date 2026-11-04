package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func TestNeovimTerminalCommandPreservesPathsAndLineNumbers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the macOS shell command is exercised with a POSIX shell")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "a repo's $(touch UNEXPECTED); folder")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Use a recorder as the editor, so the actual shell quoting and cwd are
	// exercised without opening a GUI window or running a user's editor.
	program := filepath.Join(root, "my nvim")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n{ pwd -P; printf '%s\\n' \"$@\"; } > \"$TANGIT_TERMINAL_EDITOR_ARGS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "arguments")
	t.Setenv("TANGIT_TERMINAL_EDITOR_ARGS", output)
	for _, file := range []bool{false, true} {
		path, line := dir, 0
		if file {
			path, line = filepath.Join(dir, "file's `touch UNEXPECTED` name.txt"), 23
		}
		args, err := editorCommandArgs(editorPresets[2].Command, path, line)
		if err != nil {
			t.Fatal(err)
		}
		cmd, err := terminalEditorCommand("darwin", args, path, func(string) (string, error) { return program, nil })
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Path != "/usr/bin/osascript" || cmd.Args[2] != terminalEditorAppleScript {
			t.Fatalf("Neovim does not launch through Terminal.app: %v", cmd.Args)
		}
		// A command after quitting the editor must still run in the same
		// shell and directory, rather than losing the session to exec.
		returned := filepath.Join(root, "returned-to-shell")
		shell := exec.Command("/bin/sh", "-c", cmd.Args[3]+"; printf 'ready' > "+quoteEditorShellArg(returned))
		shell.Dir = root
		if out, err := shell.CombinedOutput(); err != nil {
			t.Fatalf("launch: %v: %s", err, out)
		}
		if ready, err := os.ReadFile(returned); err != nil || string(ready) != "ready" {
			t.Fatalf("quitting Neovim did not return to the shell: %q, %v", ready, err)
		}
		if err := os.Remove(returned); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		wantDir, err := filepath.EvalSymlinks(dir)
		if err != nil {
			t.Fatal(err)
		}
		want := wantDir + "\n" + args[1] + "\n" + path + "\n"
		if string(data) != want {
			t.Fatalf("editor received %q, want %q", data, want)
		}
		for _, location := range []string{root, dir} {
			if _, err := os.Stat(filepath.Join(location, "UNEXPECTED")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("a path was interpreted as shell code")
			}
		}
	}
}

func TestNeovimUsesInteractiveTerminalOnEachPlatform(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		for _, terminalAvailable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_terminal_%v", goos, terminalAvailable), func(t *testing.T) {
				args := []string{"nvim", "+17", "/repo/file with spaces.txt"}
				lookup := func(name string) (string, error) {
					if name == "nvim" || terminalAvailable && (name == "wt.exe" || name == "gnome-terminal") {
						return "/bin/" + name, nil
					}
					return "", exec.ErrNotFound
				}
				cmd, err := terminalEditorCommand(goos, args, args[2], lookup)
				if goos == "linux" && !terminalAvailable {
					if err == nil {
						t.Fatal("a missing terminal was not reported")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"/bin/gnome-terminal", "--", "/bin/nvim", "+17", args[2]}
				if goos == "windows" {
					want = []string{"/bin/wt.exe", "new-tab", "-d", filepath.Dir(args[2]), "--", "/bin/nvim", "+17", args[2]}
					if !terminalAvailable {
						want = []string{"/bin/nvim", "+17", args[2]}
					}
				}
				if !slices.Equal(cmd.Args, want) {
					t.Fatalf("launch command %q, want %q", cmd.Args, want)
				}
			})
		}
	}
}

func TestNeovimMissingReportsEditorErrorBeforeLaunchingTerminal(t *testing.T) {
	_, err := terminalEditorCommand("darwin", []string{"nvim", "+1", "/repo"}, "/repo",
		func(string) (string, error) { return "", exec.ErrNotFound })
	if !errors.Is(err, errEditorMissing) {
		t.Fatalf("missing editor: %v", err)
	}
}

func TestNeovimLauncherChecksTheEditorBeforeOpeningATerminal(t *testing.T) {
	program := filepath.Join(t.TempDir(), "nvim")
	if err := runEditorTemplate(program+" {{filename}}", "/repo", 0); !errors.Is(err, errEditorMissing) {
		t.Fatalf("Neovim did not use the terminal launcher's executable check: %v", err)
	}
}

func TestMacOSNeovimDiscoveryWithoutShellPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("checks the installed macOS Homebrew executable")
	}
	t.Setenv("PATH", t.TempDir())
	program := resolveEditorProgram("nvim")
	if program == "nvim" {
		t.Skip("Neovim is not installed in a standard macOS location")
	}
	if _, err := exec.LookPath(program); err != nil {
		t.Fatalf("Finder cannot launch the discovered Neovim: %v", err)
	}
}
