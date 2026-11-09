package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/junegunn/go-shellwords"
)

const codeFormatterTimeout = 10 * time.Second

var (
	errCodeFormatterTimedOut    = errors.New("formatter timed out")
	errCodeFormatterEmptyOutput = errors.New("formatter returned empty output")
)

type externalCodeFormatter struct {
	name    string
	command string
}

// externalCodeFormatter deliberately ignores the language server's enabled
// state: a user-installed formatter is an independent language preference.
func (a *app) externalCodeFormatter(document *codeDocument) (externalCodeFormatter, bool) {
	if document == nil || a.location.IsRemote() {
		return externalCodeFormatter{}, false
	}
	for _, profile := range languageServerProfilesWithSettings(a.settings.LanguageServers) {
		if _, matches := languageServerProfileForPath(profile, document.path); !matches {
			continue
		}
		if command := strings.TrimSpace(a.settings.LanguageServers[profile.ID].Formatter); command != "" {
			return externalCodeFormatter{name: profile.Name, command: command}, true
		}
	}
	return externalCodeFormatter{}, false
}

// runCodeFormatter parses and executes a command directly, never through a
// shell. The variable is a test seam; production always uses the bounded
// stdin/stdout process below.
var runCodeFormatter = runCodeFormatterCommand

func runCodeFormatterCommand(commandLine, filePath, root, input string) (string, error) {
	return runCodeFormatterCommandWithinTimeout(commandLine, filePath, root, input, codeFormatterTimeout)
}

func runCodeFormatterCommandWithinTimeout(commandLine, filePath, root, input string, timeout time.Duration) (string, error) {
	arguments, err := parseCodeFormatterCommand(commandLine, filePath)
	if err != nil {
		return "", err
	}
	program, err := languageServerLookPath(arguments[0])
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, program, arguments[1:]...)
	command.Dir = root
	command.Stdin = strings.NewReader(input)
	command.Env = languageServerCommandEnvironment(command.Environ(), program, nil)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("%w after %s", errCodeFormatterTimedOut, timeout)
	}
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return "", fmt.Errorf("%s: %w", message, err)
		}
		return "", err
	}
	if input != "" && stdout.Len() == 0 {
		return "", errCodeFormatterEmptyOutput
	}
	return stdout.String(), nil
}

func parseCodeFormatterCommand(commandLine, filePath string) ([]string, error) {
	// Explicitly disable the parser's optional environment and command
	// expansion, even if another importer changes its package-level defaults.
	parser := &shellwords.Parser{ParseEnv: false, ParseBacktick: false, ParseComment: false}
	arguments, err := parser.Parse(commandLine)
	if err != nil {
		return nil, fmt.Errorf("parse formatter command: %w", err)
	}
	// shellwords stops at unquoted shell control operators. Reject the rest
	// instead of silently executing only the first part of the user's command.
	if parser.Position >= 0 {
		return nil, errors.New("parse formatter command: shell operators are not supported")
	}
	if len(arguments) == 0 {
		return nil, errors.New("parse formatter command: no executable configured")
	}
	for index := range arguments {
		arguments[index] = strings.ReplaceAll(arguments[index], "{{file}}", filePath)
	}
	return arguments, nil
}
