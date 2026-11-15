package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"lazygitapp/internal/sshworkspace"
)

// OperationStepStatus is the step's position in Git's live rebase metadata.
type OperationStepStatus string

const (
	OperationStepDone    OperationStepStatus = "done"
	OperationStepCurrent OperationStepStatus = "current"
	OperationStepPending OperationStepStatus = "pending"
)

// OperationStep is one executable line of a rebase plan. Raw retains Git's
// original line so non-commit instructions never need to be reconstructed for
// display or editing. Hash and Subject come from Git, not from the abbreviated
// hash and reminder text printed in a todo file.
type OperationStep struct {
	Action    string
	Hash      string
	Subject   string
	UpdateRef string
	Status    OperationStepStatus
	Raw       string
}

// RebasePlan is an immutable snapshot used to edit a draft or an active todo.
// Text and Fingerprint always describe the original snapshot; edits are passed
// separately to StartRebasePlan or EditRebasePlan. HeadSHA binds a draft to the
// history from which Git generated it.
type RebasePlan struct {
	Text                 string
	Fingerprint          string
	Steps                []OperationStep
	TargetRef            string
	TargetSHA            string
	MarkedBaseSHA        string
	HeadSHA              string
	Active               bool
	OperationFingerprint string
}

// RebasePlanOptions chooses the commits a draft replays. MarkedBase may be any
// revision when preparing a draft; Git resolves it to a full commit ID before
// the plan is returned and StartRebasePlan only accepts that fixed full ID.
type RebasePlanOptions struct {
	TargetRef  string
	MarkedBase string
}

var (
	// ErrRebasePlanChanged means an external writer changed the todo or its
	// operation after the user loaded it. No replacement file is installed.
	ErrRebasePlanChanged = errors.New("rebase plan changed")
	// ErrRebasePlanInvalid rejects unknown instructions and edits which copy,
	// remove, or replace plan entries instead of explicitly changing actions.
	ErrRebasePlanInvalid = errors.New("invalid rebase plan")
)

// RebasePlanSession is the optional visual-rebase extension. Existing Session
// implementations remain source-compatible and legacy history actions remain
// available when a session cannot expose live Git rebase metadata.
type RebasePlanSession interface {
	OperationSession
	PrepareRebasePlan(context.Context, RebasePlanOptions) (RebasePlan, error)
	ReadRebasePlan(context.Context) (RebasePlan, error)
	StartRebasePlan(context.Context, RebasePlan, string) error
	EditRebasePlan(context.Context, RebasePlan, string) error
	RebaseOntoBranchFromBase(context.Context, string, string) error
}

var rebaseActionAliases = map[string]string{
	"p": "pick", "pick": "pick",
	"r": "reword", "reword": "reword",
	"e": "edit", "edit": "edit",
	"s": "squash", "squash": "squash",
	"f": "fixup", "fixup": "fixup",
	"d": "drop", "drop": "drop",
	"x": "exec", "exec": "exec",
	"b": "break", "break": "break",
	"l": "label", "label": "label",
	"t": "reset", "reset": "reset",
	"m": "merge", "merge": "merge",
	"u": "update-ref", "update-ref": "update-ref",
	"noop": "noop",
}

func isRebaseCommitAction(action string) bool {
	switch action {
	case "pick", "reword", "edit", "squash", "fixup", "drop":
		return true
	default:
		return false
	}
}

func splitRebaseInstruction(line string) (command, rest string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", ""
	}
	if index := strings.IndexAny(line, " \t"); index >= 0 {
		return line[:index], strings.TrimSpace(line[index+1:])
	}
	return line, ""
}

// parseRebasePlanLine parses exactly one executable Git todo instruction. It
// deliberately does not shell-split exec: everything after exec is Git's
// command and is retained verbatim in Raw.
func parseRebasePlanLine(line string) (OperationStep, error) {
	command, rest := splitRebaseInstruction(line)
	action, ok := rebaseActionAliases[command]
	if !ok {
		return OperationStep{}, fmt.Errorf("%w: unsupported instruction %q", ErrRebasePlanInvalid, command)
	}
	step := OperationStep{Action: action, Raw: line}
	fields := strings.Fields(rest)
	switch {
	case isRebaseCommitAction(action):
		if len(fields) < 1 {
			return OperationStep{}, fmt.Errorf("%w: %s requires a commit", ErrRebasePlanInvalid, action)
		}
		if action == "fixup" && (fields[0] == "-C" || fields[0] == "-c") {
			flag := fields[0]
			fields = fields[1:]
			if len(fields) == 0 {
				return OperationStep{}, fmt.Errorf("%w: fixup %s requires a commit", ErrRebasePlanInvalid, flag)
			}
		}
		step.Hash = fields[0]
	case action == "update-ref":
		if len(fields) != 1 || fields[0] == "" {
			return OperationStep{}, fmt.Errorf("%w: update-ref requires exactly one ref", ErrRebasePlanInvalid)
		}
		step.UpdateRef = fields[0]
	case action == "label" || action == "reset":
		// Git's --root backend uses the synthetic label "[new root]".
		// Labels are opaque tokens; retain the complete remainder verbatim.
		if rest == "" {
			return OperationStep{}, fmt.Errorf("%w: %s requires a label", ErrRebasePlanInvalid, action)
		}
	case action == "exec":
		if rest == "" {
			return OperationStep{}, fmt.Errorf("%w: exec requires a command", ErrRebasePlanInvalid)
		}
	case action == "break" || action == "noop":
		if rest != "" {
			return OperationStep{}, fmt.Errorf("%w: %s takes no arguments", ErrRebasePlanInvalid, action)
		}
	case action == "merge":
		// merge [-C <commit> | -c <commit>] <label> [# <subject>]
		if len(fields) >= 2 && (fields[0] == "-C" || fields[0] == "-c") {
			step.Hash = fields[1]
			fields = fields[2:]
		} else if rest == "-C" || rest == "-c" {
			return OperationStep{}, fmt.Errorf("%w: merge %s requires a commit", ErrRebasePlanInvalid, rest)
		}
		if len(fields) < 1 {
			return OperationStep{}, fmt.Errorf("%w: merge requires a label", ErrRebasePlanInvalid)
		}
	}
	return step, nil
}

// ParseRebasePlan parses all executable lines and rejects every unknown or
// malformed instruction instead of silently discarding it. Comments and blank
// lines are retained in Text by the repository editor, although they are not
// operation steps.
func ParseRebasePlan(text string) ([]OperationStep, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var steps []OperationStep
	for lineNumber, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		step, err := parseRebasePlanLine(raw)
		if err != nil {
			return nil, fmt.Errorf("rebase todo line %d: %w", lineNumber+1, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// rebasePlanIdentity excludes the action of commit steps, so pick can become
// reword/edit/squash/fixup/drop without changing which line it identifies.
// Every directive, including exec/update-ref/reset/label, retains its exact
// trimmed text. Counts make copied or removed executable lines fail closed.
func rebasePlanIdentity(step OperationStep) (string, error) {
	if isRebaseCommitAction(step.Action) {
		return "commit\x00" + step.Hash, nil
	}
	// A merge's -C commit is not a pick whose action may be changed either;
	// like every other directive, its whole trimmed instruction is identity.
	return "directive\x00" + strings.TrimSpace(step.Raw), nil
}

func rebasePlanCounts(text string) (map[string]int, error) {
	steps, err := ParseRebasePlan(text)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(steps))
	for _, step := range steps {
		identity, err := rebasePlanIdentity(step)
		if err != nil {
			return nil, err
		}
		counts[identity]++
	}
	return counts, nil
}

func validateRebasePlanEdit(original, edited string) error {
	if strings.TrimSpace(edited) == "" {
		return fmt.Errorf("%w: the plan cannot be empty; use drop for unwanted commits", ErrRebasePlanInvalid)
	}
	before, err := rebasePlanCounts(original)
	if err != nil {
		return err
	}
	after, err := rebasePlanCounts(edited)
	if err != nil {
		return err
	}
	if len(before) != len(after) {
		return fmt.Errorf("%w: entries cannot be copied, removed, or replaced; change their action to drop instead", ErrRebasePlanInvalid)
	}
	for identity, count := range before {
		if after[identity] != count {
			return fmt.Errorf("%w: entries cannot be copied, removed, or replaced; change their action to drop instead", ErrRebasePlanInvalid)
		}
	}
	return nil
}

func rebasePlanFingerprint(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}

// rebaseExecutableText is the exact ordered instruction stream, with Git's
// generated help comments removed. Git --edit-todo may add those comments
// before invoking its sequence editor; this representation distinguishes that
// normalization from an actual copied, removed or reordered instruction.
func (s *gitSession) rebaseExecutableText(text string) (string, error) {
	steps, err := ParseRebasePlan(text)
	if err != nil {
		return "", err
	}
	if err := s.hydrateRebaseSteps(steps); err != nil {
		return "", err
	}
	var result strings.Builder
	for _, step := range steps {
		raw := strings.TrimSpace(step.Raw)
		_, rest := splitRebaseInstruction(raw)
		if step.Hash != "" {
			flag := ""
			if (step.Action == "fixup" || step.Action == "merge") && (strings.HasPrefix(rest, "-C ") || strings.HasPrefix(rest, "-c ")) {
				flag, rest = splitRebaseInstruction(rest)
			}
			_, subject := splitRebaseInstruction(rest)
			raw = step.Action + " "
			if flag != "" {
				raw += flag + " "
			}
			raw += step.Hash
			if subject != "" {
				raw += " " + subject
			}
		} else {
			raw = step.Action
			if rest != "" {
				raw += " " + rest
			}
		}
		result.WriteString(raw)
		result.WriteByte('\n')
	}
	return result.String(), nil
}

func (s *gitSession) operationMetadataPath(parts ...string) string {
	return s.joinPath(append([]string{s.git.RepoPaths.WorktreeGitDirPath()}, parts...)...)
}

func (s *gitSession) hydrateRebaseSteps(steps []OperationStep) error {
	type commitInfo struct{ hash, subject string }
	cache := map[string]commitInfo{}
	for index := range steps {
		if steps[index].Hash == "" {
			continue
		}
		info, ok := cache[steps[index].Hash]
		if !ok {
			output, err := s.gitText("show", "-s", "--format=%H%x1f%s", steps[index].Hash)
			if err != nil {
				return fmt.Errorf("resolve rebase step %s: %w", steps[index].Hash, err)
			}
			hash, subject, found := strings.Cut(strings.TrimSpace(output), "\x1f")
			if !found || hash == "" {
				return fmt.Errorf("resolve rebase step %s: Git returned an invalid commit identity", steps[index].Hash)
			}
			info = commitInfo{hash: strings.TrimSpace(hash), subject: subject}
			cache[steps[index].Hash] = info
		}
		steps[index].Hash = info.hash
		steps[index].Subject = info.subject
	}
	return nil
}

func (s *gitSession) rebaseOperationSteps(doneText, todoText string) ([]OperationStep, error) {
	done, err := ParseRebasePlan(doneText)
	if err != nil {
		return nil, fmt.Errorf("read completed rebase steps: %w", err)
	}
	pending, err := ParseRebasePlan(todoText)
	if err != nil {
		return nil, fmt.Errorf("read pending rebase steps: %w", err)
	}
	for index := range done {
		done[index].Status = OperationStepDone
	}
	if len(done) > 0 {
		// Git appends the stopped instruction to done before it waits. Earlier
		// entries are complete; the final one is the user's current step.
		done[len(done)-1].Status = OperationStepCurrent
	}
	for index := range pending {
		pending[index].Status = OperationStepPending
	}
	steps := append(done, pending...)
	if err := s.hydrateRebaseSteps(steps); err != nil {
		return nil, err
	}
	return steps, nil
}

func (s *gitSession) activeRebaseTodoPath() (string, error) {
	path := s.operationMetadataPath("rebase-merge", "git-rebase-todo")
	if _, err := s.fileService().Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("active rebase has no interactive todo file")
		}
		return "", err
	}
	return path, nil
}

// ReadRebasePlan returns the exact active todo and a plan/operation CAS pair.
// A second operation read rejects a state which changed while the file read.
func (s *gitSession) ReadRebasePlan(ctx context.Context) (RebasePlan, error) {
	if err := ctx.Err(); err != nil {
		return RebasePlan{}, err
	}
	state, err := s.OperationState(ctx)
	if err != nil {
		return RebasePlan{}, err
	}
	if state.Type != OperationTypeRebase && state.Type != OperationTypePullRebase {
		return RebasePlan{}, fmt.Errorf("no interactive rebase is active")
	}
	path, err := s.activeRebaseTodoPath()
	if err != nil {
		return RebasePlan{}, err
	}
	content, err := s.fileService().ReadFile(path)
	if err != nil {
		return RebasePlan{}, err
	}
	checked, err := s.OperationState(ctx)
	if err != nil {
		return RebasePlan{}, err
	}
	if checked.Fingerprint != state.Fingerprint {
		return RebasePlan{}, fmt.Errorf("%w: operation advanced while loading its todo", ErrRebasePlanChanged)
	}
	steps, err := ParseRebasePlan(string(content))
	if err != nil {
		return RebasePlan{}, err
	}
	for index := range steps {
		steps[index].Status = OperationStepPending
	}
	if err := s.hydrateRebaseSteps(steps); err != nil {
		return RebasePlan{}, err
	}
	return RebasePlan{
		Text: string(content), Fingerprint: rebasePlanFingerprint(string(content)), Steps: steps,
		HeadSHA: state.OrigSHA, Active: true, OperationFingerprint: state.Fingerprint,
	}, nil
}

func (s *gitSession) resolveRebaseRevision(revision, role string) (string, error) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "", nil
	}
	output, err := s.gitText("rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve %s %s: %w", role, revision, err)
	}
	full := strings.TrimSpace(output)
	if full == "" {
		return "", fmt.Errorf("resolve %s %s: empty commit ID", role, revision)
	}
	return full, nil
}

func (s *gitSession) resolveRebaseBase(revision string) (string, error) {
	return s.resolveRebaseRevision(revision, "marked rebase base")
}

func rebaseArgs(targetRef, markedBaseSHA string, interactive bool) []string {
	args := []string{"git", "rebase"}
	if interactive {
		args = append(args, "--interactive", "--keep-empty", "--rebase-merges")
	}
	args = append(args, "--autostash")
	if markedBaseSHA != "" {
		args = append(args, "--onto", targetRef, markedBaseSHA)
	} else {
		args = append(args, targetRef)
	}
	return args
}

// captureRebasePlanText asks Git to generate its own plan, copies the exact
// bytes, exits the sequence editor, and aborts the capture-only operation.
// This works for local and SSH sessions through the same command/file boundary.
func (s *gitSession) captureRebasePlanText(args []string) (text string, returnErr error) {
	captureName, removeCapture, err := s.temporaryFile("tangit-rebase-plan-capture-", nil, 0o600)
	if err != nil {
		return "", err
	}
	defer removeCapture()
	script := "#!/bin/sh\ncp \"$1\" " + sshworkspace.ShellQuote(captureName) + "\nexit 1\n"
	editorName, removeEditor, err := s.temporaryFile("tangit-rebase-plan-capture-editor-", []byte(script), 0o700)
	if err != nil {
		return "", err
	}
	defer removeEditor()
	_, captureErr := s.cmd.New(args).AddEnvVars(remoteRebaseEnvironment(":", editorName)...).RunWithOutput()
	// Once Git has attempted the capture, every subsequent return passes
	// through --abort, including a failed capture-file read. Successful
	// cleanup leaves no rebase active; a genuine abort failure is surfaced.
	defer func() {
		_, abortErr := s.cmd.New([]string{"git", "rebase", "--abort"}).AddEnvVars("GIT_EDITOR=:").RunWithOutput()
		if abortErr != nil && s.workingTreeState().Rebasing {
			returnErr = errors.Join(returnErr, fmt.Errorf("abort capture-only rebase: %w", abortErr))
		}
	}()
	content, readErr := s.fileService().ReadFile(captureName)
	if readErr != nil || len(content) == 0 {
		if captureErr == nil {
			captureErr = errors.New("Git did not generate a rebase todo")
		}
		return "", errors.Join(captureErr, readErr)
	}
	if captureErr == nil {
		return "", errors.New("capture-only rebase unexpectedly succeeded")
	}
	return string(content), nil
}

// PrepareRebasePlan generates a draft without applying any picks. In
// particular, the ordinary form has no --onto argument; only a marked base
// adds it, with the base already resolved to a full commit ID.
func (s *gitSession) PrepareRebasePlan(ctx context.Context, options RebasePlanOptions) (RebasePlan, error) {
	if err := ctx.Err(); err != nil {
		return RebasePlan{}, err
	}
	options.TargetRef = strings.TrimSpace(options.TargetRef)
	if options.TargetRef == "" {
		return RebasePlan{}, fmt.Errorf("choose a target ref for the rebase plan")
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	if state := s.workingTreeState(); state.Any() {
		return RebasePlan{}, fmt.Errorf("finish or abort the %s in progress first", state.Effective().CommandName())
	}
	targetSHA, err := s.resolveRebaseRevision(options.TargetRef, "rebase target")
	if err != nil {
		return RebasePlan{}, err
	}
	baseSHA, err := s.resolveRebaseBase(options.MarkedBase)
	if err != nil {
		return RebasePlan{}, err
	}
	head, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return RebasePlan{}, fmt.Errorf("resolve HEAD for rebase plan: %w", err)
	}
	text, err := s.captureRebasePlanText(rebaseArgs(options.TargetRef, baseSHA, true))
	if err != nil {
		return RebasePlan{}, fmt.Errorf("generate rebase plan: %w", err)
	}
	steps, err := ParseRebasePlan(text)
	if err != nil {
		return RebasePlan{}, err
	}
	if err := s.hydrateRebaseSteps(steps); err != nil {
		return RebasePlan{}, err
	}
	return RebasePlan{
		Text: text, Fingerprint: rebasePlanFingerprint(text), Steps: steps,
		TargetRef: options.TargetRef, TargetSHA: targetSHA, MarkedBaseSHA: baseSHA, HeadSHA: strings.TrimSpace(head),
	}, nil
}

func (s *gitSession) runRebasePlan(args []string, text string) error {
	planName, removePlan, err := s.temporaryFile("tangit-rebase-visual-plan-", []byte(text), 0o600)
	if err != nil {
		return err
	}
	defer removePlan()
	script := "#!/bin/sh\nset -eu\ncat " + sshworkspace.ShellQuote(planName) + " > \"$1\"\n"
	editorName, removeEditor, err := s.temporaryFile("tangit-rebase-visual-editor-", []byte(script), 0o700)
	if err != nil {
		return err
	}
	defer removeEditor()
	return s.cmd.New(args).AddEnvVars(remoteRebaseEnvironment(":", editorName)...).Run()
}

// StartRebasePlan applies the edited draft only when HEAD and all original
// entries still match. It uses the established journal/recovery path shared by
// ordinary branch rebases.
func (s *gitSession) StartRebasePlan(ctx context.Context, plan RebasePlan, edited string) error {
	if plan.Active {
		return fmt.Errorf("use EditRebasePlan for an active todo")
	}
	if plan.Fingerprint != rebasePlanFingerprint(plan.Text) || plan.HeadSHA == "" || plan.TargetSHA == "" || strings.TrimSpace(plan.TargetRef) == "" {
		return fmt.Errorf("%w: draft identity is incomplete", ErrRebasePlanInvalid)
	}
	if err := validateRebasePlanEdit(plan.Text, edited); err != nil {
		return err
	}
	// rebaseOntoBranch owns preflight, full-SHA validation, journaling and the
	// mutation lock. The expected HEAD is checked again while that lock is held.
	return s.rebaseOntoBranch(ctx, plan.TargetRef, plan.MarkedBaseSHA, plan.HeadSHA, plan.TargetSHA, &edited)
}

// editActiveRebaseTodoViaGit makes Git perform the final edit through its own
// interactive-rebase lock and sequence-editor handoff. The script compares
// every expected byte inside that handoff before installing the complete new
// file, closing the process-side check-then-rename race with another Git
// process continuing or editing the same operation.
func (s *gitSession) editActiveRebaseTodoViaGit(expected, edited string) error {
	expectedExecutable, err := s.rebaseExecutableText(expected)
	if err != nil {
		return err
	}
	expectedName, removeExpected, err := s.temporaryFile("tangit-rebase-expected-", []byte(expectedExecutable), 0o600)
	if err != nil {
		return err
	}
	defer removeExpected()
	editedName, removeEdited, err := s.temporaryFile("tangit-rebase-edited-", []byte(edited), 0o600)
	if err != nil {
		return err
	}
	defer removeEdited()
	script := "#!/bin/sh\nset -eu\n" +
		"sed -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$$/d' \"$1\" | " +
		"cmp -s " + sshworkspace.ShellQuote(expectedName) + " - || { echo 'rebase todo changed outside TanGit' >&2; exit 75; }\n" +
		"cat " + sshworkspace.ShellQuote(editedName) + " > \"$1\"\n"
	editorName, removeEditor, err := s.temporaryFile("tangit-rebase-cas-editor-", []byte(script), 0o700)
	if err != nil {
		return err
	}
	defer removeEditor()
	abbrev := "40"
	if format, formatErr := s.gitText("rev-parse", "--show-object-format"); formatErr == nil && strings.TrimSpace(format) == "sha256" {
		abbrev = "64"
	}
	err = s.cmd.New([]string{"git", "-c", "core.abbrev=" + abbrev, "rebase", "--edit-todo"}).
		AddEnvVars(remoteRebaseEnvironment(":", editorName)...).Run()
	if err != nil {
		latestPlan, planErr := s.ReadRebasePlan(context.Background())
		if planErr == nil {
			latestExecutable, executableErr := s.rebaseExecutableText(latestPlan.Text)
			if executableErr == nil && latestExecutable != expectedExecutable {
				return fmt.Errorf("%w: todo changed before Git's editor handoff", ErrRebasePlanChanged)
			}
		}
		return fmt.Errorf("Git rebase todo editor: %w", err)
	}
	return nil
}

// EditRebasePlan compares both the entire todo bytes and the rich operation
// fingerprint before Git's serialized editor handoff compares them once more.
// Directives such as exec/update-ref/reset/label survive exact and cannot be
// dropped or duplicated by a visual/raw edit.
func (s *gitSession) EditRebasePlan(ctx context.Context, plan RebasePlan, edited string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !plan.Active || plan.Fingerprint != rebasePlanFingerprint(plan.Text) || plan.OperationFingerprint == "" {
		return fmt.Errorf("%w: active todo identity is incomplete", ErrRebasePlanInvalid)
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	state, err := s.OperationState(ctx)
	if err != nil {
		return err
	}
	if state.Fingerprint != plan.OperationFingerprint || (state.Type != OperationTypeRebase && state.Type != OperationTypePullRebase) {
		return fmt.Errorf("%w: refresh the rebase and review its new state", ErrRebasePlanChanged)
	}
	path, err := s.activeRebaseTodoPath()
	if err != nil {
		return err
	}
	current, err := s.fileService().ReadFile(path)
	if err != nil {
		return err
	}
	if string(current) != plan.Text || rebasePlanFingerprint(string(current)) != plan.Fingerprint {
		return fmt.Errorf("%w: todo changed outside TanGit", ErrRebasePlanChanged)
	}
	if err := validateRebasePlanEdit(string(current), edited); err != nil {
		return err
	}
	checked, err := s.OperationState(ctx)
	if err != nil {
		return err
	}
	if checked.Fingerprint != state.Fingerprint {
		return fmt.Errorf("%w: operation changed during todo preflight", ErrRebasePlanChanged)
	}
	if string(current) == edited {
		return nil
	}
	return s.editActiveRebaseTodoViaGit(string(current), edited)
}

func (s *gitSession) RebaseOntoBranchFromBase(ctx context.Context, name, markedBase string) error {
	if strings.TrimSpace(markedBase) == "" {
		return fmt.Errorf("marked rebase base is required")
	}
	return s.rebaseOntoBranch(ctx, name, markedBase, "", "", nil)
}

// rebaseOntoBranch centralizes ordinary, marked-base and visual-plan rebases.
// expectedHead is nonempty only for a prepared visual plan. A nil planText
// keeps the existing non-interactive command unchanged.
func (s *gitSession) rebaseOntoBranch(ctx context.Context, name, markedBase, expectedHead, expectedTargetSHA string, planText *string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("rebase target is required")
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	current, err := s.currentBranch()
	if err != nil {
		return err
	}
	if current.Name == name && strings.TrimSpace(markedBase) == "" {
		return fmt.Errorf("rebase: cannot rebase %s onto itself", name)
	}
	if expectedHead != "" {
		head, readErr := s.gitText("rev-parse", "--verify", "HEAD")
		if readErr != nil {
			return readErr
		}
		if strings.TrimSpace(head) != expectedHead {
			return fmt.Errorf("%w: HEAD moved after the plan was prepared", ErrRebasePlanChanged)
		}
	}
	baseSHA, err := s.resolveRebaseBase(markedBase)
	if err != nil {
		return err
	}
	commandTarget := name
	if expectedTargetSHA != "" {
		currentTarget, resolveErr := s.resolveRebaseRevision(name, "rebase target")
		if resolveErr != nil {
			return resolveErr
		}
		if currentTarget != expectedTargetSHA {
			return fmt.Errorf("%w: target %s moved after the plan was prepared", ErrRebasePlanChanged, name)
		}
		// Run against the exact reviewed destination too, so a later external
		// ref update cannot redirect the command after this comparison.
		commandTarget = expectedTargetSHA
	}
	var snapshot *WorkSnapshot
	hasWork, err := s.repositoryHasWork()
	if err != nil {
		return err
	}
	if hasWork {
		captured, err := s.captureWorkSnapshot(ctx, RecoverySnapshotKind, "Before rebase onto "+name)
		if err != nil {
			return err
		}
		snapshot = &captured
	}
	record, err := s.beginOperation(OperationRebase, "Rebase onto "+name, UndoResetHead, snapshot)
	if err != nil {
		return err
	}
	if record, err = s.pinOperationBefore(record); err != nil {
		return err
	}
	if planText == nil {
		args := []string{"git", "rebase", "--autostash"}
		if baseSHA != "" {
			args = append(args, "--onto", name, baseSHA)
		} else {
			args = append(args, name)
		}
		err = s.cmd.New(args).AddEnvVars("GIT_EDITOR=:", "GIT_SEQUENCE_EDITOR=:").Run()
	} else {
		err = s.runRebasePlan(rebaseArgs(commandTarget, baseSHA, true), *planText)
	}
	if err != nil {
		return s.finishStartedJournalOperation(record, OperationTypeRebase, fmt.Errorf("rebase onto %s: %w", name, err))
	}
	return s.completeJournalOperation(record, nil)
}
