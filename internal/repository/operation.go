package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
)

// OperationType identifies the Git command whose metadata describes an
// interrupted operation. PullRebase is used only when TanGit's own journal
// proves that a rebase came from its pull-with-rebase action.
type OperationType string

const (
	OperationTypeNone       OperationType = ""
	OperationTypeMerge      OperationType = "merge"
	OperationTypeRebase     OperationType = "rebase"
	OperationTypePullRebase OperationType = "pull-rebase"
	OperationTypeAM         OperationType = "am"
	OperationTypeCherryPick OperationType = "cherry-pick"
	OperationTypeRevert     OperationType = "revert"
)

// OperationAction is one transition Git can perform on an active operation.
type OperationAction string

const (
	OperationActionContinue OperationAction = "continue"
	OperationActionSkip     OperationAction = "skip"
	OperationActionAbort    OperationAction = "abort"
)

// ErrOperationChanged means the operation or its unmerged index stages no
// longer match the state the caller reviewed. The driver does not run Git in
// this case; the caller must refresh and review the new state.
var ErrOperationChanged = errors.New("operation changed")

// ErrOperationUnsupported marks a rare transition for which a session cannot
// reproduce Git/Lazygit's safety algorithm. It is never silently approximated.
var ErrOperationUnsupported = errors.New("operation transition unsupported")

// OperationState is an immutable view of Git's own operation metadata. Refs
// are full refnames and SHAs are read from the corresponding state files,
// never inferred from the display branch or recent-history rows.
type OperationState struct {
	Type        OperationType
	FullRefs    []string
	OntoSHA     string
	OrigSHA     string
	IncomingSHA string
	StoppedSHA  string
	Step        int
	Done        int
	Total       int
	// Steps is Git's actual rebase plan: completed/current entries from done
	// followed by pending entries from git-rebase-todo. It is empty for
	// non-interactive backends and for operation types which have no todo.
	Steps []OperationStep
	// StepsError keeps plan corruption visible without making the underlying
	// operation (and especially Abort) unreadable.
	StepsError  string
	Actions     []OperationAction
	Fingerprint string
	// HistoryPatch links a rebase stop to its durable Custom Patch state. It is
	// also populated for the post-rebase index/stash stop, where Git itself has
	// no rebase metadata left.
	HistoryPatch *HistoryPatchState
}

// Can reports whether the active operation supports action.
func (s OperationState) Can(action OperationAction) bool {
	return slices.Contains(s.Actions, action)
}

// OperationDriveOptions binds an action to the exact state the user reviewed.
// An empty ExpectedFingerprint preserves the legacy Continue/Abort behavior;
// new UI code should always supply the fingerprint from its snapshot.
type OperationDriveOptions struct {
	Action              OperationAction
	ExpectedFingerprint string
}

// OperationSession is an optional extension of Session. Existing Session
// implementations remain source-compatible; callers can use type assertion
// and fall back to ContinueOperation and AbortOperation when necessary.
type OperationSession interface {
	OperationState(context.Context) (OperationState, error)
	DriveOperation(context.Context, OperationDriveOptions) error
}

func (s *gitSession) operationMetadataFile(parts ...string) (string, error) {
	name := s.joinPath(append([]string{s.git.RepoPaths.WorktreeGitDirPath()}, parts...)...)
	content, err := s.fileService().ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read operation metadata %s: %w", s.joinPath(parts...), err)
	}
	return strings.TrimSpace(string(content)), nil
}

func (s *gitSession) optionalOperationMetadataFile(parts ...string) (string, error) {
	name := s.joinPath(append([]string{s.git.RepoPaths.WorktreeGitDirPath()}, parts...)...)
	content, err := s.fileService().ReadFile(name)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read operation metadata %s: %w", s.joinPath(parts...), err)
	}
	return strings.TrimSpace(string(content)), nil
}

func (s *gitSession) operationHead() (ref string, sha string, err error) {
	ref, refErr := s.gitText("symbolic-ref", "--quiet", "HEAD")
	if refErr != nil {
		if code, ok := commandExitCode(refErr); !ok || code != 1 {
			return "", "", fmt.Errorf("read operation HEAD ref: %w", refErr)
		}
	}
	sha, err = s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("read operation HEAD: %w", err)
	}
	return strings.TrimSpace(ref), strings.TrimSpace(sha), nil
}

func appendOperationRef(refs []string, ref string) []string {
	if strings.HasPrefix(ref, "refs/") && !slices.Contains(refs, ref) {
		return append(refs, ref)
	}
	return refs
}

func operationActions(state models.WorkingTreeState) []OperationAction {
	actions := []OperationAction{OperationActionContinue}
	if state.CanSkip() {
		actions = append(actions, OperationActionSkip)
	}
	return append(actions, OperationActionAbort)
}

// operationType maps through Lazygit's effective-state precedence so a
// cherry-pick or revert started inside a rebase is not mistaken for the rebase.
func operationType(state models.WorkingTreeState) OperationType {
	switch state.Effective() {
	case models.WORKING_TREE_STATE_MERGING:
		return OperationTypeMerge
	case models.WORKING_TREE_STATE_REBASING:
		return OperationTypeRebase
	case models.WORKING_TREE_STATE_APPLYING_PATCHES:
		return OperationTypeAM
	case models.WORKING_TREE_STATE_CHERRY_PICKING:
		return OperationTypeCherryPick
	case models.WORKING_TREE_STATE_REVERTING:
		return OperationTypeRevert
	default:
		return OperationTypeNone
	}
}

func (s *gitSession) unmergedStages() (string, error) {
	output, err := s.cmd.New([]string{"git", "ls-files", "-u", "-z"}).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("read unmerged index stages: %w", err)
	}
	return output, nil
}

func operationFingerprint(state OperationState, headSHA string, unmerged string, metadata []string) (string, error) {
	state.Fingerprint = ""
	payload := struct {
		State    OperationState
		HeadSHA  string
		Unmerged string
		Metadata []string
	}{State: state, HeadSHA: headSHA, Unmerged: unmerged, Metadata: metadata}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("fingerprint operation: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (s *gitSession) isOwnPullRebase(ctx context.Context, state OperationState) (bool, error) {
	records, err := s.Operations(ctx)
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.ActiveOperation != OperationTypePullRebase || record.Worktree != s.git.RepoPaths.WorktreePath() || record.HeadBefore != state.OrigSHA {
			continue
		}
		if record.Branch != "" && !slices.Contains(state.FullRefs, "refs/heads/"+record.Branch) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func mailboxCommitSHA(patch string) string {
	line, _, _ := strings.Cut(patch, "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "From" || (len(fields[1]) != 40 && len(fields[1]) != 64) {
		return ""
	}
	if _, err := hex.DecodeString(fields[1]); err != nil {
		return ""
	}
	return fields[1]
}

// OperationState reads the active operation and its index without changing
// either. The zero state means no supported operation is active.
func (s *gitSession) OperationState(ctx context.Context) (OperationState, error) {
	if err := ctx.Err(); err != nil {
		return OperationState{}, err
	}
	workingTreeState := s.workingTreeState()
	type_ := operationType(workingTreeState)
	if type_ == OperationTypeNone {
		// Unborn HEAD is a valid empty operation, just as it was before the
		// durable post-rebase stop was introduced. A Custom Patch history
		// journal still gets a chance to identify itself if one exists.
		head, _ := s.gitText("rev-parse", "--verify", "HEAD")
		unmerged, unmergedErr := s.unmergedStages()
		if unmergedErr != nil {
			return OperationState{}, unmergedErr
		}
		empty := OperationState{}
		attached, attachErr := s.attachHistoryPatchState(ctx, empty, strings.TrimSpace(head), unmerged)
		if attachErr != nil {
			return OperationState{}, attachErr
		}
		if attached.HistoryPatch != nil {
			return attached, nil
		}
		return OperationState{}, nil
	}
	state := OperationState{Type: type_, Actions: operationActions(workingTreeState)}
	headRef, headSHA, err := s.operationHead()
	if err != nil {
		return OperationState{}, err
	}
	state.FullRefs = appendOperationRef(state.FullRefs, headRef)

	metadata := []string{}
	readRequired := func(parts ...string) (string, error) {
		value, readErr := s.operationMetadataFile(parts...)
		if readErr == nil {
			metadata = append(metadata, s.joinPath(parts...)+"\x00"+value)
		}
		return value, readErr
	}
	readOptional := func(parts ...string) (string, error) {
		value, readErr := s.optionalOperationMetadataFile(parts...)
		if readErr == nil {
			metadata = append(metadata, s.joinPath(parts...)+"\x00"+value)
		}
		return value, readErr
	}

	switch type_ {
	case OperationTypeMerge:
		state.OntoSHA = headSHA
		state.OrigSHA, err = readRequired("ORIG_HEAD")
		if err == nil {
			state.IncomingSHA, err = readRequired("MERGE_HEAD")
		}
		if err == nil {
			_, err = readOptional("MERGE_MODE")
		}
		if err == nil {
			_, err = readOptional("MERGE_MSG")
		}
	case OperationTypeAM:
		directory := "rebase-apply"
		var next, last string
		var headName string
		if headName, err = readOptional(directory, "head-name"); err != nil {
			break
		}
		state.FullRefs = appendOperationRef(state.FullRefs, headName)
		if state.OrigSHA, err = readOptional(directory, "initial-commit"); err != nil {
			break
		}
		if state.OrigSHA == "" {
			if state.OrigSHA, err = readRequired(directory, "abort-safety"); err != nil {
				break
			}
		}
		state.OntoSHA = state.OrigSHA
		if state.IncomingSHA, err = readOptional(directory, "original-commit"); err != nil {
			break
		}
		if state.IncomingSHA == "" {
			var patch string
			if patch, err = readRequired(directory, "patch"); err != nil {
				break
			}
			state.IncomingSHA = mailboxCommitSHA(patch)
		}
		state.StoppedSHA = state.IncomingSHA
		if next, err = readRequired(directory, "next"); err != nil {
			break
		}
		if last, err = readRequired(directory, "last"); err != nil {
			break
		}
		state.Step, err = strconv.Atoi(next)
		if err == nil {
			state.Total, err = strconv.Atoi(last)
		}
		if err != nil {
			err = fmt.Errorf("read am progress: %w", err)
			break
		}
		state.Done = max(0, state.Step-1)
		if _, err = readOptional(directory, "message"); err != nil {
			break
		}
		if _, err = readOptional(directory, "patch-num"); err != nil {
			break
		}
	case OperationTypeCherryPick, OperationTypeRevert:
		state.OntoSHA = headSHA
		headName := "CHERRY_PICK_HEAD"
		if type_ == OperationTypeRevert {
			headName = "REVERT_HEAD"
		}
		if state.IncomingSHA, err = readOptional(headName); err != nil {
			break
		}
		state.StoppedSHA = state.IncomingSHA
		if state.OrigSHA, err = readOptional("sequencer", "head"); err != nil {
			break
		}
		if _, err = readOptional("MERGE_MSG"); err != nil {
			break
		}
		if _, err = readOptional("sequencer", "todo"); err != nil {
			break
		}
	case OperationTypeRebase:
		directory := "rebase-merge"
		var doneText, todoText string
		if _, statErr := s.fileService().Stat(s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), directory)); os.IsNotExist(statErr) {
			directory = "rebase-apply"
		} else if statErr != nil {
			err = statErr
			break
		}
		var headName, upstream, messageNumber, end string
		if headName, err = readRequired(directory, "head-name"); err != nil {
			break
		}
		state.FullRefs = appendOperationRef(state.FullRefs, headName)
		if state.OntoSHA, err = readRequired(directory, "onto"); err != nil {
			break
		}
		if state.OrigSHA, err = readRequired(directory, "orig-head"); err != nil {
			break
		}
		if upstream, err = readOptional(directory, "upstream"); err != nil {
			break
		}
		state.FullRefs = appendOperationRef(state.FullRefs, upstream)
		if directory == "rebase-merge" {
			messageNumber, err = readRequired(directory, "msgnum")
			if err == nil {
				end, err = readRequired(directory, "end")
			}
			if err == nil {
				state.StoppedSHA, err = readOptional(directory, "stopped-sha")
			}
			if err == nil {
				doneText, err = readOptional(directory, "done")
			}
			if err == nil {
				todoText, err = readOptional(directory, "git-rebase-todo")
			}
		} else {
			messageNumber, err = readRequired(directory, "next")
			if err == nil {
				end, err = readRequired(directory, "last")
			}
		}
		if err != nil {
			break
		}
		state.Step, err = strconv.Atoi(messageNumber)
		if err == nil {
			state.Total, err = strconv.Atoi(end)
		}
		if err != nil {
			err = fmt.Errorf("read rebase progress: %w", err)
			break
		}
		state.Done = max(0, state.Step-1)
		state.IncomingSHA = state.StoppedSHA
		if state.IncomingSHA == "" {
			state.IncomingSHA, err = readOptional("CHERRY_PICK_HEAD")
			if err != nil {
				break
			}
		}
		if directory == "rebase-merge" {
			steps, stepsErr := s.rebaseOperationSteps(doneText, todoText)
			state.Steps = steps
			if stepsErr != nil {
				state.StepsError = stepsErr.Error()
			}
		}
	}
	if err != nil {
		return OperationState{}, err
	}
	if state.Type == OperationTypeRebase {
		ownPull, ownErr := s.isOwnPullRebase(ctx, state)
		if ownErr != nil {
			return OperationState{}, fmt.Errorf("read pull-rebase provenance: %w", ownErr)
		}
		if ownPull {
			state.Type = OperationTypePullRebase
		}
	}
	unmerged, err := s.unmergedStages()
	if err != nil {
		return OperationState{}, err
	}
	state.Fingerprint, err = operationFingerprint(state, headSHA, unmerged, metadata)
	if err != nil {
		return OperationState{}, err
	}
	return s.attachHistoryPatchState(ctx, state, headSHA, unmerged)
}

// remoteOperationPrecheck identifies transitions where Lazygit mutates Git's
// metadata before continuing. Ordinary conflict stops do not take these paths;
// refusing the exceptional ones is safer than skipping a commit over SSH.
func (s *gitSession) remoteOperationPrecheck(state OperationState) error {
	if state.Type == OperationTypeRebase || state.Type == OperationTypePullRebase {
		cherry, err := s.optionalOperationMetadataFile("CHERRY_PICK_HEAD")
		if err != nil {
			return err
		}
		message, err := s.optionalOperationMetadataFile("rebase-merge", "message")
		if err != nil {
			return err
		}
		if cherry != "" && message == "" {
			return fmt.Errorf("%w: continuing a rebase pick stopped before its commit message was written is not supported over SSH", ErrOperationUnsupported)
		}
	}
	if state.Type != OperationTypeCherryPick && state.Type != OperationTypeRevert {
		return nil
	}
	headName := "CHERRY_PICK_HEAD"
	if state.Type == OperationTypeRevert {
		headName = "REVERT_HEAD"
	}
	stopped, err := s.optionalOperationMetadataFile(headName)
	if err != nil || stopped != "" {
		return err
	}
	todos, err := s.optionalOperationMetadataFile("sequencer", "todo")
	if err != nil || todos == "" {
		return err
	}
	abortSafety, err := s.optionalOperationMetadataFile("sequencer", "abort-safety")
	if err != nil {
		return err
	}
	if abortSafety == state.OntoSHA {
		return fmt.Errorf("%w: continuing a sequencer todo which Git has not started is not supported over SSH; use skip or abort", ErrOperationUnsupported)
	}
	return nil
}

func operationAdvanced(before, after OperationState) bool {
	return before.Type == after.Type &&
		(before.Step != after.Step || before.Done != after.Done || before.Total != after.Total ||
			before.IncomingSHA != after.IncomingSHA || before.StoppedSHA != after.StoppedSHA)
}

// DriveOperation validates both operation metadata and unmerged index stages
// immediately before running Git. Compare-and-swap failures never fall back to
// a best-effort action against a different operation.
func (s *gitSession) DriveOperation(ctx context.Context, options OperationDriveOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()

	current, err := s.OperationState(ctx)
	if err != nil {
		return err
	}
	if options.ExpectedFingerprint != "" && current.Fingerprint != options.ExpectedFingerprint {
		return fmt.Errorf("%w: refresh the operation and review it again", ErrOperationChanged)
	}
	if current.Type == OperationTypeNone {
		return fmt.Errorf("no merge, rebase, cherry-pick, revert, or am operation is in progress")
	}
	if !current.Can(options.Action) {
		return fmt.Errorf("%s does not support %s", current.Type, options.Action)
	}
	if current.HistoryPatch != nil {
		if current.Type != OperationTypeRebase {
			return fmt.Errorf("history patch: unexpected operation type %s", current.Type)
		}
		return s.driveHistoryOperation(ctx, current, options)
	}
	unmerged, err := s.unmergedStages()
	if err != nil {
		return err
	}
	// Recheck after the index read: this also closes the gap between the first
	// metadata read and the action preflight.
	checked, err := s.OperationState(ctx)
	if err != nil {
		return err
	}
	if checked.Fingerprint != current.Fingerprint {
		return fmt.Errorf("%w: operation metadata or index stages changed during preflight", ErrOperationChanged)
	}
	if options.Action == OperationActionContinue && unmerged != "" {
		return fmt.Errorf("resolve all conflicts before continuing the %s", current.Type)
	}

	if supportsOperationTransitionRecovery(current.Type) {
		label := map[OperationAction]string{OperationActionContinue: "Continue", OperationActionSkip: "Skip", OperationActionAbort: "Abort"}[options.Action]
		summary := fmt.Sprintf("%s %s", label, current.Type)
		return s.runWorkspaceMutation(ctx, OperationTransition, summary, true, func() error {
			return s.driveCheckedOperation(ctx, current, options)
		})
	}
	return s.driveCheckedOperation(ctx, current, options)
}

func (s *gitSession) driveCheckedOperation(ctx context.Context, current OperationState, options OperationDriveOptions) error {
	// Full-workspace capture is deliberately more expensive than the original
	// preflight Read. Recheck the accepted immutable operation immediately
	// before the step, so it cannot apply to newly arrived conflicts or edits.
	latest, err := s.OperationState(ctx)
	if err != nil {
		return err
	}
	if latest.Fingerprint != current.Fingerprint {
		return fmt.Errorf("%w: operation changed before the step could run", ErrOperationChanged)
	}
	workingTreeState := s.workingTreeState()
	action := string(options.Action)
	if s.sshClient == nil {
		err = s.git.Rebase.GenericMergeOrRebaseAction(workingTreeState.Effective(), action)
	} else {
		// Lazygit's lock-recovery callbacks inspect host files. SSH sessions use
		// repository reads through sessionFiles and retry only an entirely
		// unchanged operation. Progress followed by a lock error is left intact.
		if options.Action == OperationActionContinue {
			if err = s.remoteOperationPrecheck(current); err != nil {
				return err
			}
		}
		commandOperation := current.Type
		if commandOperation == OperationTypePullRebase {
			commandOperation = OperationTypeRebase
		}
		command := s.cmd.New([]string{"git", string(commandOperation), "--" + action})
		command.AddEnvVars("GIT_EDITOR=:", "GIT_SEQUENCE_EDITOR=:")
		command = command.RecoverFromLockErrorWith(func() *oscommands.CmdObj {
			latest, readErr := s.OperationState(context.Background())
			if readErr == nil && latest.Fingerprint == current.Fingerprint {
				return command
			}
			return nil
		})
		err = command.Run()
	}
	after, stateErr := s.OperationState(ctx)
	if stateErr != nil {
		return errors.Join(err, stateErr)
	}
	if err != nil {
		if after.Type != OperationTypeNone && operationAdvanced(current, after) {
			var remaining string
			remaining, stateErr = s.unmergedStages()
			if stateErr == nil && remaining != "" {
				// Git reached a later pick and stopped on its conflicts. This
				// is ordinary progress, not an exceptional driver failure.
				return nil
			}
		}
		if stateErr != nil {
			return errors.Join(err, stateErr)
		}
		return fmt.Errorf("%s %s: %w", options.Action, current.Type, err)
	}
	if after.Type == OperationTypeNone {
		return s.finalizeActiveOperationJournal(ctx, current.Type, action)
	}
	return nil
}
