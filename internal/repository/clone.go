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
	// Mode trades history or object completeness for clone speed. The zero
	// value is an ordinary full clone.
	Mode CloneMode
	// Depth is the number of commits a shallow clone fetches. It is ignored
	// by the other modes; zero means one commit.
	Depth int
	// SingleBranch fetches only the cloned branch's history.
	SingleBranch bool
	// SparsePaths, when present, checks out only these directories with
	// cone-mode sparse checkout. Every other path stays in history only.
	SparsePaths []string
}

// CloneMode selects how much of the remote's history and objects Git fetches.
type CloneMode int

const (
	// CloneFull fetches every commit, tree and file.
	CloneFull CloneMode = iota
	// CloneBlobless fetches every commit and tree but downloads file contents
	// on demand (--filter=blob:none). History, blame and log stay complete.
	CloneBlobless
	// CloneTreeless fetches commits only and downloads trees and files on
	// demand (--filter=tree:0). Fastest for build machines; history walks that
	// read old trees become slow.
	CloneTreeless
	// CloneShallow truncates history to Depth commits (--depth).
	CloneShallow
)

const maxCloneDepth = 1 << 20

// cloneArguments returns the git clone options for the chosen mode, before
// the URL and destination.
func cloneArguments(options CloneOptions) []string {
	args := []string{"clone", "--progress"}
	if options.Branch != "" {
		args = append(args, "--branch", options.Branch)
	}
	switch options.Mode {
	case CloneBlobless:
		args = append(args, "--filter=blob:none")
	case CloneTreeless:
		args = append(args, "--filter=tree:0")
	case CloneShallow:
		depth := options.Depth
		if depth <= 0 {
			depth = 1
		}
		args = append(args, "--depth", strconv.Itoa(depth))
	}
	if options.SingleBranch {
		args = append(args, "--single-branch")
	} else if options.Mode == CloneShallow {
		// --depth implies --single-branch; keep the other branches visible
		// unless the user asked for one branch.
		args = append(args, "--no-single-branch")
	}
	if len(options.SparsePaths) > 0 {
		args = append(args, "--sparse")
	}
	return args
}

// ParseSparsePaths splits a comma-, space- or newline-separated list of
// repository directories and validates each one.
func ParseSparsePaths(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	paths := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		path := strings.Trim(filepath.ToSlash(strings.TrimSpace(field)), "/")
		if path == "" {
			continue
		}
		if err := validateSparsePath(path); err != nil {
			return nil, err
		}
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func validateSparsePath(path string) error {
	if strings.ContainsAny(path, "\x00\r\n*?[]!\\") {
		return fmt.Errorf("sparse checkout folder %q contains unsupported characters", path)
	}
	if strings.HasPrefix(path, "-") || strings.HasPrefix(path, "/") || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return fmt.Errorf("sparse checkout folder %q must be a relative repository folder", path)
	}
	for _, part := range strings.Split(path, "/") {
		if part == "." || part == ".." {
			return fmt.Errorf("sparse checkout folder %q must not contain . or ..", path)
		}
	}
	return nil
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

// ErrCloneSparseCheckout reports that the clone succeeded but narrowing it to
// the chosen folders failed. The destination is a usable repository.
var ErrCloneSparseCheckout = errors.New("the repository was cloned, but its sparse checkout folders could not be set")

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
	if options.Mode < CloneFull || options.Mode > CloneShallow {
		return CloneOptions{}, fmt.Errorf("unknown clone mode %d", options.Mode)
	}
	if options.Mode == CloneShallow {
		if options.Depth < 0 || options.Depth > maxCloneDepth {
			return CloneOptions{}, errors.New("shallow clone depth must be a positive number of commits")
		}
		if options.Depth == 0 {
			options.Depth = 1
		}
	} else {
		options.Depth = 0
	}
	sparse := make([]string, 0, len(options.SparsePaths))
	for _, path := range options.SparsePaths {
		path = strings.Trim(filepath.ToSlash(strings.TrimSpace(path)), "/")
		if path == "" {
			continue
		}
		if err := validateSparsePath(path); err != nil {
			return CloneOptions{}, err
		}
		sparse = append(sparse, path)
	}
	options.SparsePaths = sparse
	if len(sparse) == 0 {
		options.SparsePaths = nil
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
	args := append(cloneArguments(options), "--", options.URL, options.Destination)
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
	if err != nil {
		return cloneFailure("clone repository", err, diagnostics.String())
	}
	if len(options.SparsePaths) == 0 {
		return nil
	}
	return setSparseCheckout(ctx, options.Destination, options.SparsePaths)
}

// setSparseCheckout narrows a --sparse clone to the chosen directories. The
// clone itself already succeeded, so a failure here leaves a usable
// repository with only the top-level files checked out.
func setSparseCheckout(ctx context.Context, destination string, paths []string) error {
	args := append([]string{"-C", destination, "sparse-checkout", "set", "--cone"}, paths...)
	command := exec.CommandContext(ctx, "git", args...)
	configureBackgroundCommand(command)
	configureCloneCommand(command)
	command.WaitDelay = 2 * time.Second
	diagnostics := &boundedDiagnostic{limit: cloneDiagnosticLimit}
	command.Stdout = diagnostics
	command.Stderr = diagnostics
	err := command.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCloneSparseCheckout, cloneFailure("git sparse-checkout", err, diagnostics.String()))
	}
	return nil
}

func cloneFailure(action string, err error, output string) error {
	diagnostic := sanitizeCloneDiagnostic(output)
	if diagnostic == "" {
		return fmt.Errorf("%s: %w", action, err)
	}
	return fmt.Errorf("%s: %w: %s", action, err, diagnostic)
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
