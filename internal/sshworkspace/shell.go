package sshworkspace

import (
	"errors"
	"fmt"
	"strings"
)

// ShellQuote returns one POSIX-shell word with no expansion or command
// interpolation. It is intended for an SSH server's /bin/sh command line.
func ShellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// ShellCommand quotes an executable name and every argument independently.
func ShellCommand(name string, args ...string) (string, error) {
	if name == "" {
		return "", errors.New("remote command name is empty")
	}
	words := make([]string, 0, len(args)+1)
	for i, value := range append([]string{name}, args...) {
		if strings.ContainsRune(value, '\x00') {
			if i == 0 {
				return "", errors.New("remote command name contains NUL")
			}
			return "", fmt.Errorf("remote command argument %d contains NUL", i-1)
		}
		words = append(words, ShellQuote(value))
	}
	command := strings.Join(words, " ")
	return command, nil
}

func commandInDirectory(dir, name string, args ...string) (string, error) {
	command, err := ShellCommand(name, args...)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return command, nil
	}
	if strings.ContainsRune(dir, '\x00') {
		return "", errors.New("remote working directory contains NUL")
	}
	// The -- guard prevents a relative directory beginning with '-' from being
	// interpreted as shell options. ShellQuote protects all other characters.
	return "cd -- " + ShellQuote(dir) + " && " + command, nil
}
