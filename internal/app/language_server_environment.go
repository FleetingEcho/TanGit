package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// Finder-launched macOS apps may not inherit the user's shell PATH. Use
// common tool locations as fallbacks without starting a shell or changing
// the process environment. Explicit paths and existing PATH entries win.
func languageServerToolDirectories(goos, home string) []string {
	if goos != "darwin" {
		return nil
	}
	dirs := []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/local/go/bin", "/opt/local/bin"}
	if home != "" {
		for _, relative := range []string{"go/bin", ".cargo/bin", ".local/bin"} {
			dirs = append(dirs, filepath.Join(home, relative))
		}
	}
	return dirs
}

func currentLanguageServerToolDirectories() []string {
	home, _ := os.UserHomeDir()
	return languageServerToolDirectories(runtime.GOOS, home)
}

func lookPathLanguageServerProgram(program string) (string, error) {
	return findLanguageServerProgram(program, currentLanguageServerToolDirectories())
}

func findLanguageServerProgram(program string, directories []string) (string, error) {
	path, err := exec.LookPath(program)
	if err == nil {
		return path, nil
	}
	if program == "" || strings.ContainsAny(program, `/\`) || errors.Is(err, exec.ErrDot) {
		return "", err
	}
	for _, directory := range directories {
		if path, candidateErr := exec.LookPath(filepath.Join(directory, program)); candidateErr == nil {
			return path, nil
		}
	}
	return "", err
}

// Language servers also spawn tools: gopls needs go, npm needs node, etc.
// Give only these child processes the same fallback directories used for
// executable discovery, including the selected executable's own directory.
func languageServerCommandEnvironment(base []string, program string, overrides []string) []string {
	environment := append(slices.Clone(base), overrides...)
	if runtime.GOOS != "darwin" {
		return environment
	}
	var path string
	for _, entry := range environment {
		if value, ok := strings.CutPrefix(entry, "PATH="); ok {
			path = value
		}
	}
	dirs := filepath.SplitList(path)
	fallbacks := currentLanguageServerToolDirectories()
	if filepath.IsAbs(program) {
		fallbacks = append([]string{filepath.Dir(program)}, fallbacks...)
	}
	for _, directory := range fallbacks {
		if !slices.Contains(dirs, directory) {
			dirs = append(dirs, directory)
		}
	}
	return append(environment, "PATH="+strings.Join(dirs, string(os.PathListSeparator)))
}
