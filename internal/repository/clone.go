package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cloneDiagnosticLimit = 32 * 1024

// CloneOptions describes one remote clone. Destination is the new working
// tree, not its parent. Branch is optional and is passed to Git as one
// argument, never through a shell.
type CloneOptions struct {
	URL         string
	Destination string
	Branch      string
}

// CloneProgress is one real progress record parsed from git clone --progress.
// Known distinguishes a real zero from a phase without a percentage yet.
type CloneProgress struct {
	Phase    string
	Current  int
	Total    int
	Percent  int
	Fraction float64
	Known    bool
	Detail   string
}

var ErrCloneCredentials = errors.New("repository URL contains credentials or a token; use a Git credential helper or SSH agent instead")

// NormalizeCloneOptions validates the remote and returns cleaned paths and
// fields. HTTPS and SSH/scp-like Git remotes are the only accepted transports.
func NormalizeCloneOptions(options CloneOptions) (CloneOptions, error) {
	return normalizeCloneOptions(options, false)
}

func normalizeCloneOptions(options CloneOptions, allowLocalTransport bool) (CloneOptions, error) {
	options.URL = strings.TrimSpace(options.URL)
	options.Destination = strings.TrimSpace(options.Destination)
	options.Branch = strings.TrimSpace(options.Branch)
	if err := validateCloneURL(options.URL, allowLocalTransport); err != nil {
		return CloneOptions{}, err
	}
	if err := ValidateCloneDestination(options.Destination); err != nil {
		return CloneOptions{}, err
	}
	if err := ValidateCloneBranch(options.Branch); err != nil {
		return CloneOptions{}, err
	}
	absolute, err := filepath.Abs(options.Destination)
	if err != nil {
		return CloneOptions{}, fmt.Errorf("resolve destination folder: %w", err)
	}
	options.Destination = filepath.Clean(absolute)
	return options, nil
}

// ValidateCloneDestination checks the new working-tree path before Git runs.
func ValidateCloneDestination(destination string) error {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return errors.New("choose a destination folder")
	}
	if strings.ContainsAny(destination, "\x00\r\n") {
		return errors.New("destination folder contains invalid control characters")
	}
	return nil
}

// ValidateCloneBranch checks the optional branch without contacting the remote.
func ValidateCloneBranch(branch string) error {
	if strings.ContainsAny(strings.TrimSpace(branch), "\x00\r\n") {
		return errors.New("branch contains invalid control characters")
	}
	return nil
}

// ValidateCloneURL accepts HTTPS, ssh://, and Git's [user@]host:path form.
// HTTPS userinfo is always rejected; SSH permits a login name but never a
// password or credential-bearing query parameter.
func ValidateCloneURL(raw string) error {
	return validateCloneURL(raw, false)
}

func validateCloneURL(raw string, allowLocalTransport bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return errors.New("enter a remote repository URL")
	}
	if strings.ContainsAny(raw, "\x00\r\n") {
		return errors.New("remote repository URL contains invalid control characters")
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid remote repository URL: %w", err)
		}
		switch strings.ToLower(parsed.Scheme) {
		case "https":
			if parsed.User != nil || urlContainsCredentials(raw) {
				return ErrCloneCredentials
			}
			query, queryErr := url.ParseQuery(parsed.RawQuery)
			if queryErr != nil {
				return ErrCloneCredentials
			}
			// Signed download URLs persist credentials in origin. Only Git's
			// ordinary ref parameter is known to be safe without a blocklist
			// that can miss the next forge's token spelling.
			for key := range query {
				if !strings.EqualFold(key, "ref") {
					return ErrCloneCredentials
				}
			}
		case "ssh":
			if parsed.User != nil {
				if _, hasPassword := parsed.User.Password(); hasPassword {
					return ErrCloneCredentials
				}
			}
			if urlQuerySecretPattern.MatchString(raw) {
				return ErrCloneCredentials
			}
		case "file":
			if !allowLocalTransport {
				return errors.New("remote repository must use HTTPS, SSH, or scp-like syntax")
			}
		default:
			return errors.New("remote repository must use HTTPS, SSH, or scp-like syntax")
		}
		if (parsed.Hostname() == "" && parsed.Scheme != "file") || strings.Trim(parsed.Path, "/") == "" {
			return errors.New("remote repository URL must include a host and repository path")
		}
		return nil
	}
	// SCP-like syntax has no scheme. The host and path are both required, and
	// the host cannot be a drive letter or an option-looking string.
	colon := strings.Index(raw, ":")
	if colon <= 0 || colon == len(raw)-1 || len(raw[:colon]) == 1 || strings.ContainsAny(raw[:colon], "/\\") ||
		strings.HasPrefix(raw, "-") || strings.Contains(raw[:colon], " ") {
		return errors.New("remote repository must use HTTPS, SSH, or scp-like syntax")
	}
	if urlQuerySecretPattern.MatchString(raw) {
		return ErrCloneCredentials
	}
	return nil
}

// CloneRepositoryName derives Git's default directory name from a validated
// remote. A trailing .git suffix is removed just as Git does.
func CloneRepositoryName(raw string) (string, error) {
	if err := ValidateCloneURL(raw); err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	path := raw
	if strings.Contains(raw, "://") {
		parsed, _ := url.Parse(raw)
		path = parsed.Path
	} else if colon := strings.Index(raw, ":"); colon >= 0 {
		path = raw[colon+1:]
	}
	path = strings.TrimRight(path, "/")
	name := filepath.Base(filepath.FromSlash(path))
	name = strings.TrimSuffix(name, ".git")
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", errors.New("remote repository URL does not contain a repository name")
	}
	return name, nil
}

// CloneRepository runs Git without replacing its environment, so configured
// credential helpers, askpass programs, and SSH agents work normally. It never
// removes the destination, including after cancellation or failure.
func CloneRepository(ctx context.Context, options CloneOptions, report func(CloneProgress)) error {
	return cloneRepository(ctx, options, report, false)
}

func cloneRepository(ctx context.Context, options CloneOptions, report func(CloneProgress), allowLocalTransport bool) error {
	options, err := normalizeCloneOptions(options, allowLocalTransport)
	if err != nil {
		return err
	}
	args := []string{"clone", "--progress"}
	if options.Branch != "" {
		args = append(args, "--branch", options.Branch)
	}
	args = append(args, "--", options.URL, options.Destination)
	command := exec.CommandContext(ctx, "git", args...)
	configureBackgroundCommand(command)
	configureCloneCommand(command)
	// A transport helper can inherit Git's output pipes. Bound their lifetime
	// as well as killing the process tree when cancellation is requested.
	command.WaitDelay = 2 * time.Second

	diagnostics := &boundedDiagnostic{limit: cloneDiagnosticLimit}
	progress := &cloneProgressWriter{report: report}
	command.Stdout = diagnostics
	command.Stderr = io.MultiWriter(diagnostics, progress)
	err = command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err == nil {
		return nil
	}
	diagnostic := sanitizeCloneDiagnostic(diagnostics.String())
	if diagnostic == "" {
		return fmt.Errorf("clone repository: %w", err)
	}
	return fmt.Errorf("clone repository: %w: %s", err, diagnostic)
}

// Clone lets the app reuse Opener for both opening and cloning. Tests whose
// openers predate cloning do not need to implement this optional interface.
func (Opener) Clone(ctx context.Context, options CloneOptions, report func(CloneProgress)) error {
	return CloneRepository(ctx, options, report)
}

// boundedDiagnostic retains only the newest output. Clone progress can be
// arbitrarily verbose, but neither memory nor a later error may grow with it.
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
	if excess := len(b.data) + len(p) - b.limit; excess > 0 {
		copy(b.data, b.data[excess:])
		b.data = b.data[:len(b.data)-excess]
	}
	b.data = append(b.data, p...)
	return original, nil
}

func (b *boundedDiagnostic) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}

var ansiEscapePattern = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)|.)`)

func sanitizeCloneDiagnostic(raw string) string {
	raw = ansiEscapePattern.ReplaceAllString(raw, "")
	raw = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f:
			return -1
		default:
			return r
		}
	}, raw)
	lines := strings.FieldsFunc(raw, func(r rune) bool { return r == '\r' || r == '\n' })
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return RedactSecrets(strings.TrimSpace(strings.Join(lines, "\n")))
}

type cloneProgressWriter struct {
	pending []byte
	report  func(CloneProgress)
}

func (w *cloneProgressWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		index := bytes.IndexAny(w.pending, "\r\n")
		if index < 0 {
			if len(w.pending) > 4096 {
				w.pending = append(w.pending[:0], w.pending[len(w.pending)-4096:]...)
			}
			return len(p), nil
		}
		w.consume(string(w.pending[:index]))
		w.pending = w.pending[index+1:]
	}
}

func (w *cloneProgressWriter) consume(line string) {
	if w.report == nil {
		return
	}
	progress, ok := parseCloneProgress(line)
	if ok {
		w.report(progress)
	}
}

var (
	clonePhasePattern   = regexp.MustCompile(`(?i)^(?:remote:\s*)?(enumerating objects|counting objects|compressing objects|receiving objects|resolving deltas|updating files|checking connectivity|filtering content)(?::\s*|\s+)(.*)$`)
	clonePercentPattern = regexp.MustCompile(`(\d+)%`)
	cloneCountPattern   = regexp.MustCompile(`\((\d+)/(\d+)\)`)
	cloneObjectsPattern = regexp.MustCompile(`^(\d+)(?:,|$)`)
)

func parseCloneProgress(line string) (CloneProgress, bool) {
	line = strings.TrimSpace(ansiEscapePattern.ReplaceAllString(line, ""))
	matches := clonePhasePattern.FindStringSubmatch(line)
	if matches == nil {
		return CloneProgress{}, false
	}
	phase := strings.ToLower(matches[1])
	phase = strings.ToUpper(phase[:1]) + phase[1:]
	progress := CloneProgress{Phase: phase, Detail: line}
	remainder := matches[2]
	if match := clonePercentPattern.FindStringSubmatch(remainder); match != nil {
		percent, err := strconv.Atoi(match[1])
		if err == nil && percent >= 0 && percent <= 100 {
			progress.Percent = percent
			progress.Fraction = float64(percent) / 100
			progress.Known = true
		}
	}
	if match := cloneCountPattern.FindStringSubmatch(remainder); match != nil {
		progress.Current, _ = strconv.Atoi(match[1])
		progress.Total, _ = strconv.Atoi(match[2])
	} else if match := cloneObjectsPattern.FindStringSubmatch(remainder); match != nil {
		progress.Current, _ = strconv.Atoi(match[1])
	}
	return progress, true
}
