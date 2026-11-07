package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLanguageServerProgramLookupPreservesConfiguredPaths(t *testing.T) {
	preferred, fallback := t.TempDir(), t.TempDir()
	program := executableName("example-lsp")
	for _, directory := range []string{preferred, fallback} {
		if err := os.WriteFile(filepath.Join(directory, program), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", preferred)
	if got, err := findLanguageServerProgram(program, []string{fallback}); err != nil || got != filepath.Join(preferred, program) {
		t.Fatalf("PATH preference = %q, %v", got, err)
	}
	t.Setenv("PATH", t.TempDir())
	if got, err := findLanguageServerProgram(program, []string{fallback}); err != nil || got != filepath.Join(fallback, program) {
		t.Fatalf("GUI fallback = %q, %v", got, err)
	}
	if got, err := findLanguageServerProgram(filepath.Join(t.TempDir(), program), []string{fallback}); err == nil {
		t.Fatalf("missing explicit path silently fell back to %q", got)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(fallback, program), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, err := findLanguageServerProgram(program, []string{fallback}); err == nil {
			t.Fatalf("non-executable fallback was accepted: %q", got)
		}
	}
}

func TestLanguageServerToolDirectoriesCoverMacInstallLocations(t *testing.T) {
	home := t.TempDir()
	dirs := languageServerToolDirectories("darwin", home)
	for _, want := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/local/go/bin", filepath.Join(home, "go", "bin"), filepath.Join(home, ".cargo", "bin")} {
		if !slices.Contains(dirs, want) {
			t.Errorf("missing macOS tool directory %q", want)
		}
	}
	for _, goos := range []string{"linux", "windows"} {
		if dirs := languageServerToolDirectories(goos, home); len(dirs) != 0 {
			t.Errorf("%s received macOS fallback paths: %q", goos, dirs)
		}
	}
}

func TestLanguageServerChildProcessesFindSiblingTools(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS GUI environment regression")
	}
	bin := t.TempDir()
	program := filepath.Join(bin, "test-server")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s\\n' \"$GOBIN\"\ncommand -v tangit-test-go\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(bin, "tangit-test-go")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	t.Setenv("GOBIN", "original")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := runLanguageServerInstallCommand(ctx, program, nil, []string{"GOBIN=managed"})
	if err != nil || string(output) != "managed\n"+tool+"\n" {
		t.Fatalf("installer environment = %q, %v", output, err)
	}
	version, err := runLanguageServerVersionArguments(program, nil, []string{"--version"})
	if err != nil || version != "original\n"+tool {
		t.Fatalf("version probe environment = %q, %v", version, err)
	}
	if os.Getenv("PATH") != "/usr/bin:/bin:/usr/sbin:/sbin" || os.Getenv("GOBIN") != "original" {
		t.Fatal("language server subprocesses changed the app environment")
	}
}

func TestGoplsInstallerFindsGoWithGUIPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS GUI environment regression")
	}
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	if path, err := exec.LookPath("go"); err == nil {
		t.Skipf("system PATH already contains Go: %s", path)
	}
	goTool, err := lookPathLanguageServerProgram("go")
	if err != nil {
		t.Skip("Go is not installed in a standard macOS location")
	}
	original := runLanguageServerInstallCommand
	t.Cleanup(func() { runLanguageServerInstallCommand = original })
	dataDir := t.TempDir()
	binDir := filepath.Join(dataDir, "tools", "bin")
	calls := 0
	runLanguageServerInstallCommand = func(ctx context.Context, program string, arguments, environment []string) ([]byte, error) {
		calls++
		if program != goTool || !reflect.DeepEqual(arguments, []string{"install", "golang.org/x/tools/gopls@latest"}) ||
			!reflect.DeepEqual(environment, []string{"GOBIN=" + binDir}) {
			t.Errorf("gopls installer invocation = %q %q, env=%q", program, arguments, environment)
		}
		// Exercise the installed Go binary without downloading packages.
		return original(ctx, program, []string{"version"}, environment)
	}
	profile, _ := languageServerProfileByID("go")
	installation, err := installLanguageServerTool(profile, dataDir)
	if err != nil || calls != 1 || installation.Path != filepath.Join(binDir, "gopls") {
		t.Fatalf("gopls install with GUI PATH = %#v, %v (%d calls)", installation, err, calls)
	}
	if !strings.HasPrefix(goTool, "/") {
		t.Fatalf("Go fallback is not absolute: %q", goTool)
	}
	t.Logf("GUI PATH resolved Go to %s", goTool)
}
