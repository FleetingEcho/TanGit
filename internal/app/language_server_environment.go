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

// languageServerPackageManagerDirectories lists where Node package managers
// put global executables (pnpm, npm, yarn, volta, bun). A window launched from
// a desktop shortcut often lacks the shell PATH that adds them, so a global
// typescript-language-server would otherwise look uninstalled.
func languageServerPackageManagerDirectories(goos, home string, getenv func(string) string) []string {
	var dirs []string
	add := func(dir string) {
		if dir != "" && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	// pnpm 10 and later keep executables in a bin folder inside PNPM_HOME;
	// earlier versions used PNPM_HOME itself. Offer both.
	if pnpmHome := getenv("PNPM_HOME"); pnpmHome != "" {
		add(filepath.Join(pnpmHome, "bin"))
		add(pnpmHome)
	}
	if home != "" {
		switch goos {
		case "darwin":
			add(filepath.Join(home, "Library", "pnpm", "bin"))
			add(filepath.Join(home, "Library", "pnpm"))
		case "windows":
		default:
			add(filepath.Join(home, ".local", "share", "pnpm", "bin"))
			add(filepath.Join(home, ".local", "share", "pnpm"))
			add(filepath.Join(home, ".local", "bin"))
			// Node itself is often a Homebrew install on Linux, and the
			// package managers' shims run it through PATH.
			add("/home/linuxbrew/.linuxbrew/bin")
			add(filepath.Join(home, ".linuxbrew", "bin"))
		}
		// nvm keeps one bin folder per Node version; offer each, newest first.
		if versions, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin")); len(versions) > 0 {
			for i := len(versions) - 1; i >= 0; i-- {
				add(versions[i])
			}
		}
		for _, relative := range []string{".npm-global/bin", ".npm/bin", ".yarn/bin", ".config/yarn/global/node_modules/.bin",
			".volta/bin", ".bun/bin", ".asdf/shims", ".local/share/mise/shims", ".local/share/fnm/aliases/default/bin"} {
			add(filepath.Join(home, filepath.FromSlash(relative)))
		}
	}
	if goos == "windows" {
		if local := getenv("LOCALAPPDATA"); local != "" {
			add(filepath.Join(local, "pnpm", "bin"))
			add(filepath.Join(local, "pnpm"))
		}
		if roaming := getenv("APPDATA"); roaming != "" {
			add(filepath.Join(roaming, "npm"))
		}
	}
	return dirs
}

func currentLanguageServerToolDirectories() []string {
	home, _ := os.UserHomeDir()
	dirs := languageServerToolDirectories(runtime.GOOS, home)
	for _, dir := range languageServerPackageManagerDirectories(runtime.GOOS, home, os.Getenv) {
		if !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}
	return dirs
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
