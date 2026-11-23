package agentassist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	maxCommandDiagnosticBytes = 32 * 1024
	helperWaitDelay           = 2 * time.Second
)

// CommandConfig describes a user-trusted helper program, not a model provider.
// TempDir is only a parent for one private per-run directory; it is normally empty.
type CommandConfig struct {
	Template string
	Timeout  time.Duration
	TempDir  string
}

// CommandAdvisor adapts an external JSON-file helper to Advisor. Each run gets
// a private working directory and is unconditionally removed on return.
type CommandAdvisor struct {
	template string
	timeout  time.Duration
	tempDir  string
}

// NewCommandAdvisor validates command structure without running it.
func NewCommandAdvisor(config CommandConfig) (*CommandAdvisor, error) {
	if config.Timeout < 0 || config.Timeout > MaxHelperTimeout {
		return nil, limitError("helper timeout %s is outside 1ns..%s", config.Timeout, MaxHelperTimeout)
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultHelperTimeout
	}
	// Validate twice: here for early feedback, and per run with real path values.
	if _, err := expandHelperCommand(config.Template, map[string]string{
		"requestFile": "/request", "responseFile": "/response", "artifactDir": "/artifacts",
	}); err != nil {
		return nil, err
	}
	return &CommandAdvisor{template: config.Template, timeout: config.Timeout, tempDir: config.TempDir}, nil
}

var helperPlaceholder = regexp.MustCompile(`\{[A-Za-z][A-Za-z0-9]*\}`)

// expandHelperCommand performs one single-pass substitution. Values inserted
// into the result are never scanned for further placeholders. The only paths
// exposed to a helper are the three per-run files/directories below.
func expandHelperCommand(template string, values map[string]string) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", unsafeCommandError("helper command template is empty")
	}
	if strings.IndexByte(template, 0) >= 0 {
		return "", unsafeCommandError("helper command contains NUL")
	}
	allowed := map[string]bool{"requestFile": true, "responseFile": true, "artifactDir": true}
	for _, match := range helperPlaceholder.FindAllString(template, -1) {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
		if !allowed[name] {
			return "", unsafeCommandError("unsupported placeholder %s; only {requestFile}, {responseFile} and {artifactDir} are available", match)
		}
	}
	for name := range allowed {
		if strings.Contains(template, "{{"+name+"}}") {
			return "", unsafeCommandError("placeholder {%s} must use single braces", name)
		}
	}
	if strings.Count(template, "{requestFile}") != 1 {
		return "", unsafeCommandError("helper command must contain {requestFile} exactly once")
	}
	if strings.Count(template, "{responseFile}") != 1 {
		return "", unsafeCommandError("helper command must contain {responseFile} exactly once")
	}
	if strings.Count(template, "{artifactDir}") > 1 {
		return "", unsafeCommandError("helper command may contain {artifactDir} at most once")
	}
	parts := make([]string, 0, 6)
	for _, name := range []string{"requestFile", "responseFile", "artifactDir"} {
		value, found := values[name]
		if !found || value == "" {
			return "", unsafeCommandError("missing value for {%s}", name)
		}
		parts = append(parts, "{"+name+"}", shellQuoteHelper(value))
	}
	return strings.NewReplacer(parts...).Replace(template), nil
}

// shellQuoteHelper follows the command shells already used by lazygitGo profiles.
// In particular, POSIX single quotes make shell syntax in a generated or test
// temp path data rather than a second program fragment.
func shellQuoteHelper(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// Propose runs the helper in its private artifact directory and verifies every
// source/response binding before returning advice. It has no repository input.
func (a *CommandAdvisor) Propose(ctx context.Context, request AdviceRequest) (Advice, error) {
	if ctx == nil {
		return Advice{}, protocolError("nil context")
	}
	if err := ctx.Err(); err != nil {
		return Advice{}, err
	}
	prepared, err := PrepareRequest(request, request.Limits)
	if err != nil {
		return Advice{}, err
	}
	requestJSON, err := json.Marshal(prepared)
	if err != nil {
		return Advice{}, fmt.Errorf("encode agent advice request: %w", err)
	}
	requestJSON = append(requestJSON, '\n')
	if len(requestJSON) > prepared.Limits.MaxRequestBytes {
		return Advice{}, limitError("encoded request has %d bytes, maximum is %d", len(requestJSON), prepared.Limits.MaxRequestBytes)
	}

	jobDir, err := os.MkdirTemp(a.tempDir, "lazygitgo-agentassist-*")
	if err != nil {
		return Advice{}, fmt.Errorf("create private agent advice directory: %w", err)
	}
	defer os.RemoveAll(jobDir)
	artifactDir := filepath.Join(jobDir, "artifacts")
	if err := os.Mkdir(artifactDir, 0o700); err != nil {
		return Advice{}, fmt.Errorf("create agent advice artifact directory: %w", err)
	}
	requestFile := filepath.Join(jobDir, "request.json")
	responseFile := filepath.Join(jobDir, "response.json")
	if err := writeNewPrivateFile(requestFile, requestJSON); err != nil {
		return Advice{}, fmt.Errorf("write agent advice request: %w", err)
	}
	if err := os.Chmod(requestFile, 0o400); err != nil {
		return Advice{}, fmt.Errorf("protect agent advice request: %w", err)
	}
	expanded, err := expandHelperCommand(a.template, map[string]string{
		"requestFile": requestFile, "responseFile": responseFile, "artifactDir": artifactDir,
	})
	if err != nil {
		return Advice{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	var command *exec.Cmd
	if runtime.GOOS == "windows" {
		command = exec.CommandContext(runCtx, "cmd", "/C", expanded)
	} else {
		command = exec.CommandContext(runCtx, "sh", "-c", expanded)
	}
	command.Dir = artifactDir
	configureHelperCommand(command)
	command.WaitDelay = helperWaitDelay
	diagnostics := &boundedDiagnostic{limit: maxCommandDiagnosticBytes}
	command.Stdout, command.Stderr = diagnostics, diagnostics
	runErr := command.Run()
	if runCtxErr := runCtx.Err(); runCtxErr != nil {
		return Advice{}, fmt.Errorf("agent advice helper: %w", runCtxErr)
	}
	if runErr != nil {
		detail := strings.TrimSpace(strings.ToValidUTF8(diagnostics.String(), "�"))
		if detail != "" {
			return Advice{}, fmt.Errorf("agent advice helper failed: %w: %s", runErr, detail)
		}
		return Advice{}, fmt.Errorf("agent advice helper failed: %w", runErr)
	}

	responseJSON, err := readRegularFileBounded(responseFile, prepared.Limits.MaxResponseBytes)
	if err != nil {
		return Advice{}, fmt.Errorf("read agent advice response: %w", err)
	}
	var advice Advice
	if err := decodeStrictJSON(responseJSON, &advice); err != nil {
		return Advice{}, fmt.Errorf("decode agent advice response: %w", err)
	}
	if err := runCtx.Err(); err != nil {
		return Advice{}, fmt.Errorf("agent advice helper: %w", err)
	}
	validated, err := validatePreparedAdvice(prepared, advice)
	if err != nil {
		return Advice{}, err
	}
	return validated, nil
}

func writeNewPrivateFile(name string, data []byte) error {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	writeErr := error(nil)
	if _, err := file.Write(data); err != nil {
		writeErr = err
	}
	if err := file.Close(); writeErr == nil {
		writeErr = err
	}
	return writeErr
}

func readRegularFileBounded(name string, maximum int) ([]byte, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, protocolError("response path is not a regular non-symlink file")
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, protocolError("response path changed while opening it")
	}
	if opened.Size() > int64(maximum) {
		return nil, limitError("response exceeds %d bytes", maximum)
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maximum {
		return nil, limitError("response exceeds %d bytes", maximum)
	}
	return data, nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return protocolError("invalid versioned JSON: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return protocolError("response contains trailing JSON")
		}
		return protocolError("invalid trailing response content: %v", err)
	}
	return nil
}

type boundedDiagnostic struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (b *boundedDiagnostic) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	if len(p) >= b.limit {
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		return original, nil
	}
	if overflow := len(b.data) + len(p) - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return original, nil
}

func (b *boundedDiagnostic) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
