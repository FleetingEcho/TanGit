package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/stefanhaller/git-todo-parser/todo"
)

// HistoryPatchKind identifies one single-source Custom Patch history edit.
// Unlike direct Apply, these operations rewrite commits and never use the
// direct request's worktree/index/reverse options as implicit history policy.
type HistoryPatchKind string

const (
	HistoryPatchRemove         HistoryPatchKind = "remove"
	HistoryPatchMoveIndex      HistoryPatchKind = "move-index"
	HistoryPatchMoveCommit     HistoryPatchKind = "move-commit"
	HistoryPatchNewBefore      HistoryPatchKind = "new-before"
	HistoryPatchNewAfter       HistoryPatchKind = "new-after"
	OperationHistoryPatch                       = "history-patch"
	historyPatchJournalVersion                  = 1
	historyStageApply                           = "apply"
	historyStageAmend                           = "amend"
	historyStageCommit                          = "commit"
	historyStageCompensate                      = "compensate"
)

// HistoryPatchStep is persisted before every potentially stopping action.
// In particular, apply-and-amend is not encoded in a Go callback which would
// disappear when the app exits.
type HistoryPatchStep string

const (
	HistoryStepPrepare          HistoryPatchStep = "prepare"
	HistoryStepReverseSource    HistoryPatchStep = "reverse-source"
	HistoryStepApplyDestination HistoryPatchStep = "apply-destination"
	HistoryStepNewBefore        HistoryPatchStep = "new-before"
	HistoryStepAwaitDestination HistoryPatchStep = "await-destination"
	HistoryStepNewAfter         HistoryPatchStep = "new-after"
	HistoryStepReplay           HistoryPatchStep = "replay"
	HistoryStepMoveIndex        HistoryPatchStep = "move-index"
	HistoryStepRestoreStash     HistoryPatchStep = "restore-stash"
	HistoryStepComplete         HistoryPatchStep = "complete"
	HistoryStepAborted          HistoryPatchStep = "aborted"
)

// HistoryPatchRequest binds a history rewrite to exact immutable inputs.
// ExpectedHeadSHA is mandatory: a stale history page cannot rewrite a branch
// which has moved since the user reviewed the patch and destination.
type HistoryPatchRequest struct {
	Kind            HistoryPatchKind
	Patch           string
	SourceSHA       string
	DestinationSHA  string
	Summary         string
	Description     string
	ExpectedHeadSHA string
	ExpectedHeadRef string
}

// HistoryPatchState is embedded in the durable safety journal. BeforeSHA and
// AfterSHA describe the latest completed durable step; Error is informational
// and never replaces the real Git/index validation done on recovery.
type HistoryPatchState struct {
	Version          int              `json:"version"`
	Kind             HistoryPatchKind `json:"kind"`
	SourceSHA        string           `json:"source_sha"`
	DestinationSHA   string           `json:"destination_sha,omitempty"`
	PatchDigest      string           `json:"patch_digest"`
	CompensationSHA  string           `json:"compensation_digest,omitempty"`
	Step             HistoryPatchStep `json:"step"`
	BeforeSHA        string           `json:"step_before_sha,omitempty"`
	AfterSHA         string           `json:"step_after_sha,omitempty"`
	Conflict         bool             `json:"conflict,omitempty"`
	Error            string           `json:"error,omitempty"`
	SourceCopySHA    string           `json:"source_copy_sha,omitempty"`
	BaseSHA          string           `json:"base_sha,omitempty"`
	KeepEmpty        bool             `json:"keep_empty,omitempty"`
	SourceBeforeDest bool             `json:"source_before_destination,omitempty"`
	Summary          string           `json:"summary,omitempty"`
	Description      string           `json:"description,omitempty"`
	Stage            string           `json:"stage,omitempty"`
	FinalHeadSHA     string           `json:"final_head_sha,omitempty"`
	NeedsStash       bool             `json:"needs_stash,omitempty"`
	StashSHA         string           `json:"stash_sha,omitempty"`
	StashBefore      string           `json:"stash_before,omitempty"`
	StashPrepared    bool             `json:"stash_prepared,omitempty"`
	IndexResolved    bool             `json:"index_resolved,omitempty"`
	StashPopStarted  bool             `json:"stash_pop_started,omitempty"`
	StashPopped      bool             `json:"stash_popped,omitempty"`
}

// HistoryPatchSession is optional so existing Session test doubles and remote
// wrappers remain source-compatible. The real gitSession implements it.
type HistoryPatchSession interface {
	ApplyHistoryPatch(context.Context, HistoryPatchRequest) error
}

func historyPatchDigest(text string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
}

func validHistoryPatchSHA(sha string) bool {
	if len(sha) != 40 && len(sha) != 64 {
		return false
	}
	_, err := hex.DecodeString(sha)
	return err == nil
}

func historyPatchSourceCopy(kind HistoryPatchKind, sourceBeforeDest bool) bool {
	return kind == HistoryPatchNewBefore || kind == HistoryPatchMoveCommit && sourceBeforeDest
}

func historyPatchReverseSource(kind HistoryPatchKind, sourceBeforeDest bool) bool {
	if kind == HistoryPatchMoveCommit && sourceBeforeDest {
		return false
	}
	return kind == HistoryPatchRemove || kind == HistoryPatchMoveIndex || kind == HistoryPatchMoveCommit || kind == HistoryPatchNewAfter
}

func (s *gitSession) historyPatchRecordName(record OperationRecord) string {
	name, _ := s.operationPath(record.ID)
	return name
}

func (s *gitSession) writeHistoryPatchRecord(record OperationRecord) error {
	return s.writeOperationRecord(s.historyPatchRecordName(record), record)
}

func (s *gitSession) saveActiveHistoryPatch(record OperationRecord, actionErr error) error {
	record.ActiveOperation = OperationTypeRebase
	record.RebasePaused = true
	record.Status = OperationSucceeded
	if actionErr != nil && record.HistoryPatch != nil {
		record.HistoryPatch.Error = RedactSecrets(actionErr.Error())
	}
	return s.completeJournalOperation(record, nil)
}

func (s *gitSession) activeHistoryPatchRecord(ctx context.Context) (*OperationRecord, error) {
	records, err := s.Operations(ctx)
	if err != nil {
		return nil, err
	}
	var found *OperationRecord
	for i := range records {
		record := &records[i]
		if record.Worktree != s.git.RepoPaths.WorktreePath() || record.HistoryPatch == nil ||
			record.ActiveOperation != OperationTypeRebase || record.HistoryPatch.Step == HistoryStepComplete || record.HistoryPatch.Step == HistoryStepAborted {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("multiple active Custom Patch history journals are present; resolve the repository state manually")
		}
		copy := *record
		found = &copy
	}
	return found, nil
}

func (s *gitSession) historyPatchOperationState(record OperationRecord, headSHA, unmerged string) (OperationState, error) {
	history := *record.HistoryPatch
	state := OperationState{
		Type: OperationTypeRebase, OntoSHA: record.HeadAfter, OrigSHA: record.HeadBefore,
		StoppedSHA: headSHA, HistoryPatch: &history,
	}
	currentRef, _ := s.gitText("symbolic-ref", "--quiet", "HEAD")
	state.FullRefs = appendOperationRef(state.FullRefs, strings.TrimSpace(currentRef))
	expectedRef := "refs/heads/" + record.Branch
	recoveryError := ""
	switch {
	case strings.TrimSpace(currentRef) != expectedRef:
		recoveryError = "current HEAD ref changed; return to " + expectedRef + " before driving this history edit"
	case history.Step == HistoryStepPrepare && headSHA == record.HeadBefore:
		state.Actions = []OperationAction{OperationActionContinue, OperationActionAbort}
	case (history.Step == HistoryStepMoveIndex || history.Step == HistoryStepRestoreStash) && history.FinalHeadSHA != "" && history.FinalHeadSHA != record.HeadBefore && headSHA == history.FinalHeadSHA:
		state.Actions = []OperationAction{OperationActionContinue, OperationActionAbort}
	case history.Step == HistoryStepReplay && history.FinalHeadSHA != "" && history.FinalHeadSHA != record.HeadBefore && headSHA == history.FinalHeadSHA:
		// Only verified final rebase heads can be completed synthetically (for
		// example after an autostash conflict). An externally aborted rebase at
		// original HEAD must never be marked as a successful history rewrite.
		state.Actions = []OperationAction{OperationActionContinue, OperationActionAbort}
	case headSHA == record.HeadBefore:
		// An external git rebase --abort restores original HEAD at any
		// intermediate patch stage. Only close/restore this journal; none of
		// those stages may be treated as a synthetic successful rewrite.
		state.Actions = []OperationAction{OperationActionAbort}
	default:
		recoveryError = "native rebase disappeared at an intermediate or unverified history-patch step; automatic continue is unavailable"
	}
	if recoveryError != "" && history.Error == "" {
		history.Error = recoveryError
	}
	var err error
	state.Fingerprint, err = operationFingerprint(state, headSHA, unmerged, []string{"history-patch\x00" + record.ID})
	return state, err
}

// attachHistoryPatchState makes both Git-backed rebase stops and the final
// index/stash stop visible through the ordinary Operation driver after restart.
func (s *gitSession) attachHistoryPatchState(ctx context.Context, state OperationState, headSHA, unmerged string) (OperationState, error) {
	record, err := s.activeHistoryPatchRecord(ctx)
	if err != nil || record == nil {
		return state, err
	}
	if state.Type == OperationTypeNone {
		return s.historyPatchOperationState(*record, headSHA, unmerged)
	}
	if state.Type != OperationTypeRebase && state.Type != OperationTypePullRebase || state.OrigSHA != record.HeadBefore {
		return OperationState{}, fmt.Errorf("active Custom Patch history journal %s does not match Git's current operation", record.ID)
	}
	state.Type = OperationTypeRebase
	state.HistoryPatch = record.HistoryPatch
	state.Actions = []OperationAction{OperationActionContinue, OperationActionAbort}
	state.Fingerprint, err = operationFingerprint(state, headSHA, unmerged, []string{"history-patch\x00" + record.ID, state.Fingerprint})
	return state, err
}

func (s *gitSession) historyPatchInputs(ctx context.Context, request HistoryPatchRequest) ([]*models.Commit, int, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, -1, -1, err
	}
	if request.Patch == "" {
		return nil, -1, -1, fmt.Errorf("history patch: the patch is empty")
	}
	if !validHistoryPatchSHA(request.SourceSHA) || request.DestinationSHA != "" && !validHistoryPatchSHA(request.DestinationSHA) || !validHistoryPatchSHA(request.ExpectedHeadSHA) || !strings.HasPrefix(request.ExpectedHeadRef, "refs/heads/") {
		return nil, -1, -1, fmt.Errorf("history patch: source, destination and expected HEAD must be full commit hashes")
	}
	if request.Kind != HistoryPatchRemove && request.Kind != HistoryPatchMoveIndex && request.Kind != HistoryPatchMoveCommit && request.Kind != HistoryPatchNewBefore && request.Kind != HistoryPatchNewAfter {
		return nil, -1, -1, fmt.Errorf("history patch: unknown action %q", request.Kind)
	}
	if (request.Kind == HistoryPatchNewBefore || request.Kind == HistoryPatchNewAfter) && strings.TrimSpace(request.Summary) == "" {
		return nil, -1, -1, fmt.Errorf("history patch: a new commit summary is required")
	}
	if request.Kind != HistoryPatchMoveCommit && request.DestinationSHA != "" {
		return nil, -1, -1, fmt.Errorf("history patch: a destination is valid only for Move to selected commit")
	}
	if request.Kind == HistoryPatchMoveCommit && request.DestinationSHA == "" {
		return nil, -1, -1, fmt.Errorf("history patch: select a destination commit")
	}
	if request.SourceSHA == request.DestinationSHA && request.Kind == HistoryPatchMoveCommit {
		return nil, -1, -1, fmt.Errorf("history patch: source and destination are the same commit")
	}
	unmerged, err := s.unmergedStages()
	if err != nil {
		return nil, -1, -1, fmt.Errorf("history patch: inspect index: %w", err)
	}
	if unmerged != "" {
		return nil, -1, -1, fmt.Errorf("history patch: resolve the repository's unresolved index entries first")
	}
	// Signing during a multi-stop replay may require an external pinentry and
	// cannot be resumed reliably by the non-interactive driver. Refuse it before
	// creating a stash, object or rebase rather than failing halfway through.
	if signed, err := s.gitText("config", "--bool", "commit.gpgsign"); err == nil && strings.TrimSpace(signed) == "true" {
		return nil, -1, -1, fmt.Errorf("history patch: commit.gpgsign is not supported by the resumable driver")
	}
	commits, source, err := s.editableHeadHistory(request.SourceSHA)
	if err != nil {
		return nil, -1, -1, fmt.Errorf("history patch: %w", err)
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return nil, -1, -1, err
	}
	if branch == "" || head != request.ExpectedHeadSHA || "refs/heads/"+branch != request.ExpectedHeadRef {
		return nil, -1, -1, fmt.Errorf("history patch: %w: HEAD or its full branch ref changed; review the patch and destination again", ErrOperationChanged)
	}
	if active, err := s.activeHistoryPatchRecord(ctx); err != nil {
		return nil, -1, -1, err
	} else if active != nil {
		return nil, -1, -1, fmt.Errorf("history patch: finish or abort the active Custom Patch history edit first")
	}
	if commits[source].IsMerge() {
		return nil, -1, -1, fmt.Errorf("history patch: merge commits cannot be a patch source")
	}
	destination := -1
	if request.Kind == HistoryPatchMoveCommit {
		for i, commit := range commits {
			if commit.Hash() == request.DestinationSHA {
				destination = i
				break
			}
		}
		if destination < 0 {
			return nil, -1, -1, fmt.Errorf("history patch: destination is not in current HEAD history")
		}
		if commits[destination].IsMerge() {
			return nil, -1, -1, fmt.Errorf("history patch: merge commits cannot be a patch destination")
		}
	}
	if request.Kind == HistoryPatchNewBefore && len(commits[source].Parents()) == 0 {
		return nil, -1, -1, fmt.Errorf("history patch: new-before a root commit is not supported; select another source or apply the patch directly")
	}
	return commits, source, destination, nil
}

// checkHistorySource validates binary and textual changes against the exact
// historical index, not against whatever happens to be checked out now.
func (s *gitSession) checkHistorySource(patch, source string, reverse bool) error {
	historySHA, err := s.gitText("rev-parse", "--verify", source+"^{commit}")
	if err != nil || strings.TrimSpace(historySHA) != source {
		return fmt.Errorf("history patch: source commit is missing or changed")
	}
	indexName, removeIndex, err := s.temporaryFile("tangit-history-check-index-", nil, 0o600)
	if err != nil {
		return err
	}
	defer removeIndex()
	if err := s.fileService().Remove(indexName); err != nil && !os.IsNotExist(err) {
		return err
	}
	env := []string{"GIT_INDEX_FILE=" + indexName}
	if output, err := s.cmd.New([]string{"git", "read-tree", source}).AddEnvVars(env...).RunWithOutput(); err != nil {
		return fmt.Errorf("history patch: prepare source index: %w: %s", err, strings.TrimSpace(output))
	}
	approval, err := s.newBytePatchSource([]byte(patch))
	if err != nil {
		return err
	}
	defer approval.close()
	args := []string{"git", "apply", "--cached", "--check"}
	if reverse {
		args = append(args, "--reverse")
	}
	args = append(args, approval.temporary)
	if output, err := s.cmd.New(args).AddEnvVars(env...).RunWithOutput(); err != nil {
		return fmt.Errorf("history patch: source patch is not applicable (binary and conflict checks included): %w: %s", err, strings.TrimSpace(RedactSecrets(output)))
	}
	return nil
}

// commitObjectHeader reproduces Lazygit's synthetic-parent algorithm. Signature
// headers are dropped because both the tree and parent changed. Root creation
// is explicit: parent insertion is not silently skipped when no header exists.
func historyPatchCommitObjectHeader(commitObject, name, value string) string {
	headers, message, _ := strings.Cut(commitObject, "\n\n")
	newHeaders := make([]string, 0, 8)
	inSignature, replaced := false, false
	for _, line := range strings.Split(headers, "\n") {
		if inSignature && strings.HasPrefix(line, " ") {
			continue
		}
		inSignature = strings.HasPrefix(line, "gpgsig ") || strings.HasPrefix(line, "gpgsig-sha256 ")
		if inSignature {
			continue
		}
		if strings.HasPrefix(line, name+" ") {
			line = name + " " + value
			replaced = true
		}
		newHeaders = append(newHeaders, line)
	}
	if !replaced {
		insert := 0
		for i, line := range newHeaders {
			if strings.HasPrefix(line, "tree ") {
				insert = i + 1
				break
			}
		}
		newHeaders = append(newHeaders, "")
		copy(newHeaders[insert+1:], newHeaders[insert:])
		newHeaders[insert] = name + " " + value
	}
	return strings.Join(newHeaders, "\n") + "\n\n" + message
}

func (s *gitSession) writeHistoryCommitObject(object string) (string, error) {
	output, _, err := s.cmd.New([]string{"git", "hash-object", "-t", "commit", "-w", "--stdin"}).SetStdin(object).RunWithOutputs()
	if err != nil {
		return "", fmt.Errorf("history patch: write synthetic commit: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// copyHistorySourceWithPatchInParent makes two commit objects without touching
// the real index/worktree. The copy has the original tree and message, while
// its new parent already contains the selected patch. Replaying the copy thus
// removes exactly that patch from the source without duplicate adjacent lines.
func (s *gitSession) copyHistorySourceWithPatchInParent(commit *models.Commit, patch string) (string, error) {
	parent := models.EmptyTreeCommitHash
	if parents := commit.Parents(); len(parents) == 1 {
		parent = parents[0]
	} else if len(parents) != 0 {
		return "", fmt.Errorf("history patch: merge sources cannot have a synthetic parent")
	}
	indexName, removeIndex, err := s.temporaryFile("tangit-history-index-", nil, 0o600)
	if err != nil {
		return "", err
	}
	defer removeIndex()
	if err := s.fileService().Remove(indexName); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	env := []string{"GIT_INDEX_FILE=" + indexName}
	if output, err := s.cmd.New([]string{"git", "read-tree", parent}).AddEnvVars(env...).RunWithOutput(); err != nil {
		return "", fmt.Errorf("history patch: read synthetic parent: %w: %s", err, strings.TrimSpace(output))
	}
	approval, err := s.newBytePatchSource([]byte(patch))
	if err != nil {
		return "", err
	}
	defer approval.close()
	if output, err := s.cmd.New([]string{"git", "apply", "--cached", approval.temporary}).AddEnvVars(env...).RunWithOutput(); err != nil {
		return "", fmt.Errorf("history patch: apply patch in temporary index: %w: %s", err, strings.TrimSpace(RedactSecrets(output)))
	}
	tree, _, err := s.cmd.New([]string{"git", "write-tree"}).AddEnvVars(env...).RunWithOutputs()
	if err != nil {
		return "", fmt.Errorf("history patch: write synthetic parent tree: %w", err)
	}
	commitObject, _, err := s.cmd.New([]string{"git", "cat-file", "commit", commit.Hash()}).RunWithOutputs()
	if err != nil {
		return "", fmt.Errorf("history patch: read source commit object: %w", err)
	}
	parentHash, err := s.writeHistoryCommitObject(historyPatchCommitObjectHeader(commitObject, "tree", strings.TrimSpace(tree)))
	if err != nil {
		return "", err
	}
	return s.writeHistoryCommitObject(historyPatchCommitObjectHeader(commitObject, "parent", parentHash))
}

func historyPatchPlan(state HistoryPatchState) func([]todo.Todo) ([]todo.Todo, error) {
	return func(todos []todo.Todo) ([]todo.Todo, error) {
		if state.Kind == HistoryPatchNewBefore {
			todos = append([]todo.Todo{{Command: todo.Break}}, todos...)
		} else {
			hashes := map[string]bool{state.SourceSHA: true}
			if state.Kind == HistoryPatchMoveCommit {
				if state.SourceBeforeDest {
					hashes = map[string]bool{state.DestinationSHA: true}
				} else {
					hashes[state.DestinationSHA] = true
				}
			}
			var err error
			todos, err = changeSessionTodoActions(todos, hashes, todo.Edit, "")
			if err != nil {
				return nil, err
			}
		}
		if state.Kind == HistoryPatchMoveCommit && !state.SourceBeforeDest {
			// If the destination's edit pick conflicts, Git may consume it
			// after resolution without a second edit-stop. A following break is
			// the deterministic recovery stop; a normal edit removes it below.
			for i := range todos {
				if todos[i].Command == todo.Edit && sessionSameHash(todos[i].Commit, state.DestinationSHA) {
					todos = append(todos, todo.Todo{})
					copy(todos[i+2:], todos[i+1:])
					todos[i+1] = todo.Todo{Command: todo.Break}
					break
				}
			}
		}
		if state.SourceCopySHA != "" {
			matched := false
			for i := range todos {
				if todos[i].Command == todo.Pick && sessionSameHash(todos[i].Commit, state.SourceSHA) {
					todos[i].Commit = state.SourceCopySHA
					matched = true
					break
				}
			}
			if !matched {
				return nil, fmt.Errorf("history patch: source commit is missing from the replay plan")
			}
		}
		return todos, nil
	}
}

func (s *gitSession) removeHistoryDestinationBreak() error {
	planName := s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), "rebase-merge", "git-rebase-todo")
	plan, err := s.fileService().ReadFile(planName)
	if err != nil {
		return err
	}
	todos, err := todo.Parse(strings.NewReader(string(plan)), s.git.Config.GetCoreCommentChar())
	if err != nil {
		return err
	}
	filtered := todos[:0]
	removed := false
	for _, item := range todos {
		if item.Command == todo.Break {
			removed = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !removed {
		return nil
	}
	var output strings.Builder
	if err := todo.Write(&output, filtered, s.git.Config.GetCoreCommentChar()); err != nil {
		return err
	}
	return s.fileService().WriteFileAtomic(planName, []byte(output.String()), 0o600)
}

func (s *gitSession) historyAtDestination(state HistoryPatchState) (bool, error) {
	if s.workingTreeState().Effective().CommandName() != "rebase" {
		return false, nil
	}
	stopped, err := s.optionalOperationMetadataFile("rebase-merge", "stopped-sha")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(stopped) == state.DestinationSHA {
		return true, nil
	}
	if stopped != "" {
		return false, nil
	}
	done, err := s.operationMetadataFile("rebase-merge", "done")
	if err != nil {
		return false, err
	}
	doneTodos, err := todo.Parse(strings.NewReader(done), s.git.Config.GetCoreCommentChar())
	if err != nil {
		return false, err
	}
	return len(doneTodos) > 0 && doneTodos[len(doneTodos)-1].Command == todo.Break, nil
}

func (s *gitSession) advanceHistoryDestination(record OperationRecord) error {
	if err := s.removeHistoryDestinationBreak(); err != nil {
		return err
	}
	// When an edit pick itself conflicted, resolving it can leave the picked
	// result staged above the previous HEAD. Commit that resolved original
	// destination before applying our compensation, not the other way around.
	if output, err := s.cmd.New([]string{"git", "diff", "--cached", "--quiet"}).RunWithOutput(); err != nil {
		if code, ok := commandExitCode(err); !ok || code != 1 {
			return fmt.Errorf("history patch: inspect resolved destination pick: %w: %s", err, strings.TrimSpace(output))
		}
		if output, err := s.cmd.New([]string{"git", "commit", "--amend", "--no-edit"}).AddEnvVars("GIT_EDITOR=:").RunWithOutput(); err != nil {
			return fmt.Errorf("history patch: commit resolved destination pick: %w: %s", err, strings.TrimSpace(output))
		}
	}
	head, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	state := record.HistoryPatch
	state.Step = HistoryStepApplyDestination
	state.Stage = historyStageApply
	state.BeforeSHA = strings.TrimSpace(head)
	return nil
}

func (s *gitSession) startHistoryPatchRebase(record OperationRecord) error {
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return err
	}
	if branch != record.Branch || head != record.HeadBefore {
		return fmt.Errorf("history patch: %w: HEAD changed after recovery snapshot construction", ErrOperationChanged)
	}
	state := record.HistoryPatch
	if err := s.runRemoteRebase(state.BaseSHA, remoteRebaseOptions{
		keepEmpty: state.KeepEmpty, transform: historyPatchPlan(*state),
	}); err != nil {
		return fmt.Errorf("history patch: start replay: %w", err)
	}
	head, err = s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	head = strings.TrimSpace(head)
	expected := state.SourceSHA
	switch {
	case state.Kind == HistoryPatchMoveCommit && state.SourceBeforeDest:
		expected = state.DestinationSHA
	case state.Kind == HistoryPatchNewBefore:
		source, err := s.gitText("rev-parse", "--verify", state.SourceSHA+"^")
		if err != nil {
			return err
		}
		expected = strings.TrimSpace(source)
	}
	if head != expected {
		return fmt.Errorf("history patch: replay stopped at %s instead of the planned commit %s", head, expected)
	}
	if state.Kind == HistoryPatchMoveCommit && !state.SourceBeforeDest {
		planName := s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), "rebase-merge", "git-rebase-todo")
		plan, readErr := s.fileService().ReadFile(planName)
		if readErr != nil {
			return fmt.Errorf("history patch: validate destination stop: %w", readErr)
		}
		parsed, parseErr := todo.Parse(strings.NewReader(string(plan)), s.git.Config.GetCoreCommentChar())
		if parseErr != nil {
			return fmt.Errorf("history patch: parse destination stop: %w", parseErr)
		}
		index, found := findSessionTodo(parsed, state.DestinationSHA)
		if !found || parsed[index].Command != todo.Edit {
			return fmt.Errorf("history patch: destination edit-stop is missing from replay plan:\n%s", plan)
		}
	}
	state.BeforeSHA = head
	return s.writeHistoryPatchRecord(record)
}

func historyStashEntryAdded(before, current string) (string, bool) {
	var older, actual []string
	if before != "" {
		older = strings.Split(before, "\n")
	}
	if current != "" {
		actual = strings.Split(current, "\n")
	}
	if len(actual) != len(older)+1 {
		return "", false
	}
	for i := range older {
		if actual[i+1] != older[i] {
			return "", false
		}
	}
	return actual[0], true
}

func historyStashWithTop(state HistoryPatchState) string {
	if state.StashBefore == "" {
		return state.StashSHA
	}
	return state.StashSHA + "\n" + state.StashBefore
}

func (s *gitSession) ensureHistoryStash(record OperationRecord) (OperationRecord, error) {
	state := record.HistoryPatch
	if !state.NeedsStash || state.StashSHA != "" {
		return record, nil
	}
	if !state.StashPrepared {
		before, err := s.stashOperationState()
		if err != nil {
			return record, err
		}
		state.StashBefore = before
		state.StashPrepared = true
		// Publish the old exact sequence before the first stash mutation. If
		// the process stops after push, one strict sequence delta identifies it.
		if err := s.writeHistoryPatchRecord(record); err != nil {
			return record, err
		}
	}
	current, err := s.stashOperationState()
	if err != nil {
		return record, err
	}
	if current != state.StashBefore {
		if sha, ok := historyStashEntryAdded(state.StashBefore, current); ok {
			state.StashSHA = sha
			return record, s.writeHistoryPatchRecord(record)
		}
		return record, fmt.Errorf("history patch: the stash list changed while preparing saved work")
	}
	if err := s.git.Stash.Push("TanGit Custom Patch history work"); err != nil {
		return record, fmt.Errorf("history patch: save existing work: %w", err)
	}
	current, err = s.stashOperationState()
	if err != nil {
		return record, err
	}
	sha, ok := historyStashEntryAdded(state.StashBefore, current)
	if !ok {
		return record, fmt.Errorf("history patch: could not identify its dedicated stash without touching other entries")
	}
	state.StashSHA = sha
	return record, s.writeHistoryPatchRecord(record)
}

func (s *gitSession) historyPatchFile(record OperationRecord, compensation bool) (patchSource, error) {
	suffix := ".patch"
	if compensation {
		suffix = ".compensation.patch"
	}
	data, err := s.fileService().ReadFile(s.joinPath(s.journalDir(), record.ID+suffix))
	if err != nil {
		return patchSource{}, fmt.Errorf("history patch: read saved patch: %w", err)
	}
	expected := record.HistoryPatch.PatchDigest
	if compensation {
		expected = record.HistoryPatch.CompensationSHA
	}
	if expected == "" || historyPatchDigest(string(data)) != expected {
		return patchSource{}, fmt.Errorf("history patch: saved patch failed its journal digest check")
	}
	return s.newBytePatchSource(data)
}

func (s *gitSession) writeHistoryCompensation(record OperationRecord) error {
	output, err := s.cmd.New([]string{"git", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", "--unified=3", "HEAD.." + record.HistoryPatch.SourceSHA}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("history patch: create compensation patch: %w: %s", err, strings.TrimSpace(output))
	}
	if output == "" {
		return fmt.Errorf("history patch: the reverse amendment produced an empty compensation patch")
	}
	name := s.joinPath(s.journalDir(), record.ID+".compensation.patch")
	if err := s.fileService().WriteFileAtomic(name, []byte(output), 0o600); err != nil {
		return fmt.Errorf("history patch: save compensation patch: %w", err)
	}
	if err := s.syncDirectory(s.journalDir()); err != nil {
		return err
	}
	record.HistoryPatch.CompensationSHA = historyPatchDigest(output)
	return nil
}

func (s *gitSession) expectedHistoryAppliedTree(record OperationRecord, compensation, reverse bool) (string, error) {
	base := record.HistoryPatch.BeforeSHA
	if base == "" {
		return "", fmt.Errorf("history patch: apply stage has no persisted base commit")
	}
	indexName, removeIndex, err := s.temporaryFile("tangit-history-reconcile-index-", nil, 0o600)
	if err != nil {
		return "", err
	}
	defer removeIndex()
	if err := s.fileService().Remove(indexName); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	env := []string{"GIT_INDEX_FILE=" + indexName}
	if output, err := s.cmd.New([]string{"git", "read-tree", base}).AddEnvVars(env...).RunWithOutput(); err != nil {
		return "", fmt.Errorf("history patch: prepare reconciliation index: %w: %s", err, strings.TrimSpace(output))
	}
	source, err := s.historyPatchFile(record, compensation)
	if err != nil {
		return "", err
	}
	defer source.close()
	args := []string{"git", "apply", "--cached", "--3way"}
	if reverse {
		args = append(args, "--reverse")
	}
	args = append(args, source.temporary)
	if output, err := s.cmd.New(args).AddEnvVars(env...).RunWithOutput(); err != nil {
		return "", fmt.Errorf("history patch: compute expected applied tree: %w: %s", err, strings.TrimSpace(RedactSecrets(output)))
	}
	tree, _, err := s.cmd.New([]string{"git", "write-tree"}).AddEnvVars(env...).RunWithOutputs()
	if err != nil {
		return "", fmt.Errorf("history patch: write expected applied tree: %w", err)
	}
	return strings.TrimSpace(tree), nil
}

// applyHistoryPatchRecord is idempotent across stage publication and conflict
// recording. It proves an untouched base, or the exact expected post-apply
// index, in a temporary index before allowing a repeat. Anything else is an
// external/index divergence which needs review rather than another git apply.
func (s *gitSession) applyHistoryPatchRecord(record OperationRecord, compensation, reverse bool) error {
	current, err := s.cmd.New([]string{"git", "write-tree"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("history patch: read current index for reconciliation: %w", err)
	}
	current = strings.TrimSpace(current)
	base := record.HistoryPatch.BeforeSHA
	baseTree, err := s.gitText("rev-parse", "--verify", base+"^{tree}")
	if err != nil {
		return fmt.Errorf("history patch: reconcile against unknown base %s: %w", base, err)
	}
	if current == strings.TrimSpace(baseTree) {
		source, err := s.historyPatchFile(record, compensation)
		if err != nil {
			return err
		}
		defer source.close()
		return s.applyCheckedPatch(source, PatchRequest{Target: PatchBoth, Reverse: reverse, ThreeWay: true})
	}
	expected, err := s.expectedHistoryAppliedTree(record, compensation, reverse)
	if err != nil {
		return err
	}
	if current == expected {
		if output, diffErr := s.cmd.New([]string{"git", "diff", "--quiet"}).RunWithOutput(); diffErr != nil {
			return fmt.Errorf("history patch: index has the expected tree but worktree differs; review it before continuing: %s", strings.TrimSpace(output))
		}
		return nil
	}
	head, _ := s.gitText("rev-parse", "--verify", "HEAD")
	return fmt.Errorf("history patch: %w: index tree %s differs from base %s and expected %s at HEAD %s (recorded base %s)", ErrOperationChanged, current, strings.TrimSpace(baseTree), expected, strings.TrimSpace(head), base)
}

func historyCommitParents(output string) []string {
	fields := strings.Fields(output)
	if len(fields) < 2 {
		return nil
	}
	return fields[1:]
}

func (s *gitSession) finishedHistoryAmend(record OperationRecord, compensation, reverse bool) (bool, error) {
	state := record.HistoryPatch
	head, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return false, err
	}
	head = strings.TrimSpace(head)
	if head == state.BeforeSHA {
		return false, nil
	}
	beforeParents, err := s.gitText("rev-list", "--parents", "-n", "1", state.BeforeSHA)
	if err != nil {
		return false, err
	}
	headParents, err := s.gitText("rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil {
		return false, err
	}
	original := state.SourceSHA
	if state.Step == HistoryStepApplyDestination {
		original = state.DestinationSHA
	}
	beforeMessage, err := s.gitText("show", "-s", "--format=%B", original)
	if err != nil {
		return false, err
	}
	headMessage, err := s.gitText("show", "-s", "--format=%B", "HEAD")
	if err != nil {
		return false, err
	}
	expectedTree, err := s.expectedHistoryAppliedTree(record, compensation, reverse)
	if err != nil {
		return false, err
	}
	headTree, err := s.gitText("rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return false, err
	}
	clean, err := s.cmd.New([]string{"git", "status", "--porcelain=v1", "--untracked-files=no"}).RunWithOutput()
	if err != nil {
		return false, err
	}
	if !slices.Equal(historyCommitParents(beforeParents), historyCommitParents(headParents)) || strings.TrimSpace(beforeMessage) != strings.TrimSpace(headMessage) || strings.TrimSpace(headTree) != expectedTree || clean != "" {
		return false, fmt.Errorf("history patch: %w: HEAD changed but does not match the exact amend result", ErrOperationChanged)
	}
	state.AfterSHA = head
	return true, nil
}

func (s *gitSession) amendHistoryHead(record OperationRecord, compensation, reverse bool) error {
	done, err := s.finishedHistoryAmend(record, compensation, reverse)
	if err != nil || done {
		return err
	}
	if _, err := s.cmd.New([]string{"git", "commit", "--amend", "--no-edit"}).AddEnvVars("GIT_EDITOR=:").RunWithOutput(); err != nil {
		return fmt.Errorf("history patch: amend stopped commit: %w", err)
	}
	head, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	record.HistoryPatch.AfterSHA = strings.TrimSpace(head)
	return nil
}

func (s *gitSession) commitHistoryNew(record OperationRecord) error {
	state := record.HistoryPatch
	if head, err := s.gitText("rev-parse", "--verify", "HEAD"); err == nil && strings.TrimSpace(head) != state.BeforeSHA {
		parent, parentErr := s.gitText("rev-parse", "--verify", "HEAD^")
		message, messageErr := s.gitText("show", "-s", "--format=%B", "HEAD")
		tree, treeErr := s.gitText("rev-parse", "--verify", "HEAD^{tree}")
		expectedTree := ""
		if state.Step == HistoryStepNewAfter {
			expectedTree, err = s.gitText("rev-parse", "--verify", state.SourceSHA+"^{tree}")
		} else {
			expectedTree, err = s.gitText("rev-parse", "--verify", state.SourceCopySHA+"^{tree}")
		}
		expectedMessage := strings.TrimSpace(state.Summary)
		if strings.TrimSpace(state.Description) != "" {
			expectedMessage = strings.TrimSpace(state.Summary + "\n\n" + state.Description)
		}
		clean, statusErr := s.cmd.New([]string{"git", "status", "--porcelain=v1", "--untracked-files=no"}).RunWithOutput()
		if parentErr != nil || messageErr != nil || treeErr != nil || err != nil || statusErr != nil || strings.TrimSpace(parent) != state.BeforeSHA || strings.TrimSpace(message) != expectedMessage || strings.TrimSpace(tree) != strings.TrimSpace(expectedTree) || clean != "" {
			return fmt.Errorf("history patch: %w: HEAD does not match the exact completed new commit", ErrOperationChanged)
		}
		state.AfterSHA = strings.TrimSpace(head)
		return nil
	}
	args := []string{"git", "commit", "-m", state.Summary}
	if strings.TrimSpace(state.Description) != "" {
		args = append(args, "-m", state.Description)
	}
	if output, err := s.cmd.New(args).AddEnvVars("GIT_EDITOR=:").RunWithOutput(); err != nil {
		return fmt.Errorf("history patch: create new commit: %w: %s", err, strings.TrimSpace(output))
	}
	head, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	state.AfterSHA = strings.TrimSpace(head)
	return nil
}

func (s *gitSession) continueHistoryRebase() error {
	// Do not use Lazygit's process-local onSuccessfulContinue callback here.
	// Every post-continue phase is already persisted below. The raw command
	// also avoids running an obsolete callback when a journal is reopened in a
	// new process; ordinary pick conflicts remain Git's own rebase state.
	planName := s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), "rebase-merge", "git-rebase-todo")
	before, _ := s.fileService().ReadFile(planName)
	command := s.cmd.New([]string{"git", "rebase", "--continue"}).AddEnvVars(remoteRebaseEnvironment(":", ":")...)
	output, err := command.RunWithOutput()
	if err != nil {
		return fmt.Errorf("rebase --continue: %w: %s\nplan before continue:\n%s", err, strings.TrimSpace(output), before)
	}
	return nil
}

func (s *gitSession) historyHasConflicts() (bool, error) {
	unmerged, err := s.unmergedStages()
	return unmerged != "", err
}

func (s *gitSession) completeHistoryPatch(record OperationRecord) error {
	record.HistoryPatch.Step = HistoryStepComplete
	record.HistoryPatch.Stage = ""
	record.HistoryPatch.Conflict = false
	record.HistoryPatch.Error = ""
	record.ActiveOperation = OperationTypeNone
	record.RebasePaused = false
	return s.completeJournalOperation(record, nil)
}

func (s *gitSession) dropHistoryStash(state HistoryPatchState, allowMissing bool) error {
	if !state.StashPrepared {
		return nil
	}
	current, err := s.stashOperationState()
	if err != nil {
		return err
	}
	if current == state.StashBefore {
		if allowMissing || state.StashPopped {
			return nil
		}
		return fmt.Errorf("history patch: dedicated stash disappeared before a recorded pop; refusing to treat dropped work as restored")
	}
	if state.StashSHA == "" {
		// A stop between push and recording its SHA is recoverable only when
		// exactly one new entry sits above the reviewed pre-operation list.
		sha, ok := historyStashEntryAdded(state.StashBefore, current)
		if !ok {
			return fmt.Errorf("history patch: cannot identify its dedicated stash during cleanup")
		}
		state.StashSHA = sha
	}
	if current != historyStashWithTop(state) {
		return fmt.Errorf("history patch: the stash list changed; preserve it and restore saved work manually")
	}
	if _, err := s.cmd.New([]string{"git", "stash", "drop", "refs/stash@{0}"}).RunWithOutput(); err != nil {
		return fmt.Errorf("history patch: drop its dedicated stash: %w", err)
	}
	after, err := s.stashOperationState()
	if err != nil {
		return err
	}
	if after != state.StashBefore {
		return fmt.Errorf("history patch: stash sequence differs after its dedicated drop")
	}
	return nil
}

func (s *gitSession) restoreHistoryStash(record OperationRecord) error {
	state := record.HistoryPatch
	if state.StashSHA == "" {
		return nil
	}
	current, err := s.stashOperationState()
	if err != nil {
		return err
	}
	if state.StashPopped {
		return nil
	}
	if current == state.StashBefore {
		return fmt.Errorf("history patch: dedicated stash is absent without a recorded pop; external stash removal is not proof of restoration")
	}
	if current != historyStashWithTop(*state) {
		return fmt.Errorf("history patch: the stash list changed before restoring saved work")
	}
	if !state.StashPopStarted {
		state.StashPopStarted = true
		if err := s.writeHistoryPatchRecord(record); err != nil {
			return err
		}
	}
	output, err := s.cmd.New([]string{"git", "stash", "pop", "refs/stash@{0}"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("history patch: restore saved work: %w: %s", err, strings.TrimSpace(output))
	}
	current, err = s.stashOperationState()
	if err != nil {
		return err
	}
	if current != state.StashBefore {
		return fmt.Errorf("history patch: dedicated stash is still present after its pop")
	}
	state.StashPopped = true
	// Publish proof of the completed pop before reconstructing its I-parent or
	// dropping anything. A disappeared entry without this proof never counts
	// as a successful restoration on restart.
	return s.writeHistoryPatchRecord(record)
}

// restoreHistoryStagedIndex applies the dedicated stash's original I-parent
// delta in a temporary index. A plain pop --index would compare the old index
// with newly rewritten HEAD and falsely reject even unrelated staged work.
// The real working tree and any user-resolved final index remain in charge.
func (s *gitSession) restoreHistoryStagedIndex(record OperationRecord) error {
	state := record.HistoryPatch
	if state.StashSHA == "" {
		return nil
	}
	baseTree := state.FinalHeadSHA
	if state.IndexResolved {
		tree, err := s.cmd.New([]string{"git", "write-tree"}).RunWithOutput()
		if err != nil {
			return fmt.Errorf("history patch: read resolved index tree: %w", err)
		}
		baseTree = strings.TrimSpace(tree)
	}
	indexName, removeIndex, err := s.temporaryFile("tangit-history-restore-index-", nil, 0o600)
	if err != nil {
		return err
	}
	defer removeIndex()
	if err := s.fileService().Remove(indexName); err != nil && !os.IsNotExist(err) {
		return err
	}
	env := []string{"GIT_INDEX_FILE=" + indexName}
	if output, err := s.cmd.New([]string{"git", "read-tree", baseTree}).AddEnvVars(env...).RunWithOutput(); err != nil {
		return fmt.Errorf("history patch: prepare final index: %w: %s", err, strings.TrimSpace(output))
	}
	apply := func(name string) error {
		output, err := s.cmd.New([]string{"git", "apply", "--cached", "--3way", name}).AddEnvVars(env...).RunWithOutput()
		if err != nil {
			return fmt.Errorf("history patch: rebuild staged index: %w: %s", err, strings.TrimSpace(output))
		}
		return nil
	}
	if !state.IndexResolved {
		compensation, err := s.historyPatchFile(record, true)
		if err != nil {
			return err
		}
		if err := apply(compensation.temporary); err != nil {
			compensation.close()
			return err
		}
		compensation.close()
	}
	delta, err := s.cmd.New([]string{"git", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", record.HeadBefore, state.StashSHA + "^2"}).RunWithOutput()
	if err != nil {
		return fmt.Errorf("history patch: reconstruct original staged delta: %w", err)
	}
	if delta != "" {
		staged, err := s.newBytePatchSource([]byte(delta))
		if err != nil {
			return err
		}
		if err := apply(staged.temporary); err != nil {
			staged.close()
			return err
		}
		staged.close()
	}
	tree, _, err := s.cmd.New([]string{"git", "write-tree"}).AddEnvVars(env...).RunWithOutputs()
	if err != nil {
		return fmt.Errorf("history patch: write final staged tree: %w", err)
	}
	if output, err := s.cmd.New([]string{"git", "read-tree", strings.TrimSpace(tree)}).RunWithOutput(); err != nil {
		return fmt.Errorf("history patch: install final staged index: %w: %s", err, strings.TrimSpace(output))
	}
	return nil
}

func (s *gitSession) abortHistoryPatch(record OperationRecord) error {
	actualRebase := s.workingTreeState().Effective().CommandName() == "rebase"
	if !actualRebase {
		branch, head, err := s.currentOperationIdentity()
		if err != nil {
			return err
		}
		state := record.HistoryPatch
		valid := branch == record.Branch
		switch state.Step {
		case HistoryStepPrepare:
			valid = valid && head == record.HeadBefore
		case HistoryStepReplay:
			valid = valid && head == record.HeadBefore || valid && state.FinalHeadSHA != "" && head == state.FinalHeadSHA
		case HistoryStepMoveIndex, HistoryStepRestoreStash:
			valid = valid && state.FinalHeadSHA != "" && state.FinalHeadSHA != record.HeadBefore && head == state.FinalHeadSHA
		default:
			valid = valid && head == record.HeadBefore
		}
		if !valid {
			return fmt.Errorf("history patch: %w: refusing to reset a changed branch or intermediate HEAD", ErrOperationChanged)
		}
	}
	var actionErr error
	if actualRebase {
		command := s.cmd.New([]string{"git", "rebase", "--abort"}).AddEnvVars(remoteRebaseEnvironment(":", ":")...)
		_, actionErr = command.RunWithOutput()
	}
	state := record.HistoryPatch
	// The dedicated Move-out stash is deliberately restored from the V2
	// snapshot, not popped over conflicts. The final synthetic stop likewise
	// has no Git rebase left to abort, so restore its original pinned HEAD/work.
	if actionErr == nil && (state.NeedsStash || !actualRebase) {
		if err := s.dropHistoryStash(*state, true); err != nil {
			actionErr = err
		} else if _, err := s.cmd.New([]string{"git", "reset", "--hard", record.HeadBefore}).RunWithOutput(); err != nil {
			actionErr = fmt.Errorf("history patch: restore original HEAD: %w", err)
		} else if record.Snapshot != nil {
			actionErr = s.restoreResetWorkSnapshot(context.Background(), *record.Snapshot)
		}
	}
	if actionErr != nil {
		return actionErr
	}
	state.Step = HistoryStepAborted
	state.Stage = ""
	state.Conflict = false
	state.Error = "aborted"
	record.ActiveOperation = OperationTypeNone
	record.RebasePaused = false
	record.UndoKind = ""
	record.Status = OperationInterrupted
	record.FinishedAt = time.Now().Unix()
	record.HeadAfter = record.HeadBefore
	record.BranchAfter = record.Branch
	return s.writeHistoryPatchRecord(record)
}

// resumeHistoryPatch runs only transitions whose preconditions are persisted.
// resolved is true only after DriveOperation has CAS-checked the exact conflict
// stop and verified that all unmerged index stages are gone.
func (s *gitSession) resumeHistoryPatch(ctx context.Context, record OperationRecord, resolved bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := record.HistoryPatch
		resolvedStep := resolved
		resolved = false
		stepErr := error(nil)
		switch state.Step {
		case HistoryStepPrepare:
			var err error
			record, err = s.ensureHistoryStash(record)
			if err == nil {
				err = s.startHistoryPatchRebase(record)
			}
			if err != nil {
				stepErr = err
				break
			}
			state = record.HistoryPatch
			state.Stage = ""
			if historyPatchReverseSource(state.Kind, state.SourceBeforeDest) {
				state.Step = HistoryStepReverseSource
			} else if state.Kind == HistoryPatchNewBefore {
				state.Step = HistoryStepNewBefore
			} else {
				state.Step = HistoryStepApplyDestination
			}
		case HistoryStepReverseSource:
			switch state.Stage {
			case "":
				state.Stage = historyStageApply
			case historyStageApply:
				if !resolvedStep {
					stepErr = s.applyHistoryPatchRecord(record, false, true)
				}
				if stepErr == nil {
					state.Stage = historyStageAmend
				}
			case historyStageAmend:
				stepErr = s.amendHistoryHead(record, false, true)
				if stepErr == nil && state.Kind == HistoryPatchMoveCommit && s.workingTreeState().Effective().CommandName() != "rebase" {
					stepErr = fmt.Errorf("reverse amendment unexpectedly ended the replay before the destination stop")
				}
				if stepErr == nil {
					if state.Kind == HistoryPatchRemove {
						state.Step, state.Stage = HistoryStepReplay, ""
					} else {
						state.Stage = historyStageCompensate
					}
				}
			case historyStageCompensate:
				stepErr = s.writeHistoryCompensation(record)
				if stepErr == nil {
					state.Stage = ""
					switch state.Kind {
					case HistoryPatchNewAfter:
						state.BeforeSHA = state.AfterSHA
						state.Step = HistoryStepNewAfter
					case HistoryPatchMoveCommit:
						state.Step = HistoryStepAwaitDestination
					default:
						state.Step = HistoryStepReplay
					}
				}
			default:
				stepErr = fmt.Errorf("unsupported reverse-source stage %q", state.Stage)
			}
		case HistoryStepApplyDestination:
			switch state.Stage {
			case "":
				state.Stage = historyStageApply
			case historyStageApply:
				if !resolvedStep {
					// Moving to a newer descendant applies HEAD..original-source
					// compensation after the reverse amend, exactly as upstream does;
					// the older-destination/synthetic-parent path uses the original.
					compensation := state.Kind == HistoryPatchMoveCommit && !state.SourceBeforeDest
					stepErr = s.applyHistoryPatchRecord(record, compensation, false)
				}
				if stepErr == nil {
					state.Stage = historyStageAmend
				}
			case historyStageAmend:
				stepErr = s.amendHistoryHead(record, state.Kind == HistoryPatchMoveCommit && !state.SourceBeforeDest, false)
				if stepErr == nil {
					state.Step, state.Stage = HistoryStepReplay, ""
				}
			default:
				stepErr = fmt.Errorf("unsupported destination stage %q", state.Stage)
			}
		case HistoryStepNewBefore, HistoryStepNewAfter:
			switch state.Stage {
			case "":
				state.Stage = historyStageApply
			case historyStageApply:
				if !resolvedStep {
					stepErr = s.applyHistoryPatchRecord(record, state.Step == HistoryStepNewAfter, false)
				}
				if stepErr == nil {
					state.Stage = historyStageCommit
				}
			case historyStageCommit:
				stepErr = s.commitHistoryNew(record)
				if stepErr == nil {
					state.Step, state.Stage = HistoryStepReplay, ""
				}
			default:
				stepErr = fmt.Errorf("unsupported new-commit stage %q", state.Stage)
			}
		case HistoryStepAwaitDestination:
			// Reconcile Git before doing anything: the user may already have
			// continued to our destination edit/break outside this process.
			arrived, err := s.historyAtDestination(*state)
			conflicts, conflictErr := s.historyHasConflicts()
			if conflictErr != nil {
				stepErr = conflictErr
			}
			switch {
			case err != nil:
				stepErr = err
			case arrived && conflicts:
				stepErr = fmt.Errorf("resolve the destination pick conflicts before applying the Custom Patch compensation")
			case arrived:
				stepErr = s.advanceHistoryDestination(record)
			default:
				stepErr = s.continueHistoryRebase()
				head, headErr := s.gitText("rev-parse", "--verify", "HEAD")
				if stepErr != nil {
					stepErr = fmt.Errorf("continue toward destination %s at HEAD %s (%s): %w", state.DestinationSHA, strings.TrimSpace(head), s.workingTreeState().Effective().CommandName(), stepErr)
				}
				if headErr != nil && stepErr == nil {
					stepErr = headErr
				}
				arrived, err = s.historyAtDestination(*state)
				if err != nil && stepErr == nil {
					stepErr = err
				}
				conflicts, conflictErr = s.historyHasConflicts()
				if conflictErr != nil && stepErr == nil {
					stepErr = conflictErr
				}
				if stepErr == nil && !conflicts {
					switch {
					case arrived:
						stepErr = s.advanceHistoryDestination(record)
					case s.workingTreeState().Effective().CommandName() != "rebase":
						stepErr = fmt.Errorf("%w: destination replay ended without its persistent edit/break stop", ErrOperationUnsupported)
					default:
						return s.writeHistoryPatchRecord(record)
					}
				}
			}
		case HistoryStepReplay:
			stepErr = s.continueHistoryRebase()
			state = record.HistoryPatch
			if s.workingTreeState().Effective().CommandName() != "rebase" {
				head, headErr := s.gitText("rev-parse", "--verify", "HEAD")
				if headErr == nil {
					state.FinalHeadSHA = strings.TrimSpace(head)
					state.AfterSHA = state.FinalHeadSHA
				} else if stepErr == nil {
					stepErr = headErr
				}
				if stepErr != nil && strings.Contains(strings.ToLower(stepErr.Error()), "no rebase in progress") {
					stepErr = nil
				}
				if stepErr == nil {
					if state.Kind == HistoryPatchMoveIndex {
						state.Step, state.Stage = HistoryStepMoveIndex, historyStageApply
						state.BeforeSHA = state.FinalHeadSHA
					} else {
						return s.completeHistoryPatch(record)
					}
				}
			} else if stepErr == nil {
				return s.writeHistoryPatchRecord(record)
			}
		case HistoryStepMoveIndex:
			switch state.Stage {
			case "":
				state.Stage = historyStageApply
			case historyStageApply:
				if !resolvedStep {
					stepErr = s.applyHistoryPatchRecord(record, true, false)
				}
				if stepErr == nil {
					state.IndexResolved = resolvedStep
					if state.NeedsStash {
						state.Step, state.Stage = HistoryStepRestoreStash, ""
					} else {
						return s.completeHistoryPatch(record)
					}
				}
			default:
				stepErr = fmt.Errorf("unsupported move-index stage %q", state.Stage)
			}
		case HistoryStepRestoreStash:
			if resolvedStep {
				// A conflict during pop was manually resolved by the user. The
				// resulting index/worktree are proof of their chosen merge result.
				state.StashPopped = true
			} else {
				stepErr = s.restoreHistoryStash(record)
				if stepErr == nil {
					stepErr = s.restoreHistoryStagedIndex(record)
				}
			}
			if stepErr == nil {
				stepErr = s.dropHistoryStash(*state, false)
			}
			if stepErr == nil {
				return s.completeHistoryPatch(record)
			}
		default:
			stepErr = fmt.Errorf("history patch: unsupported persisted step %q", state.Step)
		}

		if stepErr != nil {
			conflicts, conflictErr := s.historyHasConflicts()
			if conflictErr != nil {
				return errors.Join(stepErr, conflictErr)
			}
			state.Conflict = conflicts
			state.Error = RedactSecrets(stepErr.Error())
			if saveErr := s.saveActiveHistoryPatch(record, stepErr); saveErr != nil {
				return errors.Join(stepErr, saveErr)
			}
			return fmt.Errorf("history patch step %s: %w", state.Step, stepErr)
		}
		state.Conflict = false
		state.Error = ""
		// Persist every new stage before running it on the next loop. In
		// particular apply-destination/new-after never live only in memory.
		if err := s.writeHistoryPatchRecord(record); err != nil {
			return err
		}
	}
}

func (s *gitSession) validateHistoryPatchPins(record OperationRecord) error {
	state := record.HistoryPatch
	if state == nil || state.Version != historyPatchJournalVersion {
		return fmt.Errorf("history patch: unsupported or missing persistent state")
	}
	if err := s.validateJournalPin(record.RefName, record.HeadBefore); err != nil {
		return fmt.Errorf("history patch: original HEAD pin: %w", err)
	}
	if state.SourceCopySHA != "" && state.Step != HistoryStepPrepare {
		ref := journalRefPrefix + record.ID + "/copy"
		if err := s.validateJournalPin(ref, state.SourceCopySHA); err != nil {
			return fmt.Errorf("history patch: synthetic source pin: %w", err)
		}
	}
	return nil
}

func (s *gitSession) ensureHistoryPatchCopyPin(record OperationRecord) error {
	state := record.HistoryPatch
	if state.SourceCopySHA == "" || state.Step != HistoryStepPrepare {
		return nil
	}
	ref := journalRefPrefix + record.ID + "/copy"
	if current, err := s.gitText("rev-parse", "--verify", ref); err == nil {
		if strings.TrimSpace(current) != state.SourceCopySHA {
			return fmt.Errorf("history patch: synthetic source pin names a different object")
		}
		return nil
	}
	if _, err := s.gitText("cat-file", "-e", state.SourceCopySHA+"^{commit}"); err != nil {
		return fmt.Errorf("history patch: synthetic source object is unavailable: %w", err)
	}
	if _, err := s.cmd.New([]string{"git", "update-ref", ref, state.SourceCopySHA}).RunWithOutput(); err != nil {
		return fmt.Errorf("history patch: restore synthetic source pin: %w", err)
	}
	return nil
}

// ApplyHistoryPatch validates all immutable request fields before publishing a
// journal. Every later Continue/Abort is routed by DriveOperation using this
// record, including after opening a brand-new Session.
func (s *gitSession) ApplyHistoryPatch(ctx context.Context, request HistoryPatchRequest) error {
	unlock := s.lockWorktreeMutation()
	defer unlock()
	commits, source, destination, err := s.historyPatchInputs(ctx, request)
	if err != nil {
		return err
	}
	sourceBeforeDest := request.Kind == HistoryPatchMoveCommit && source < destination
	if historyPatchReverseSource(request.Kind, sourceBeforeDest) {
		if err := s.checkHistorySource(request.Patch, request.SourceSHA, true); err != nil {
			return err
		}
	}
	state := &HistoryPatchState{
		Version: historyPatchJournalVersion, Kind: request.Kind,
		SourceSHA: request.SourceSHA, DestinationSHA: request.DestinationSHA,
		PatchDigest: historyPatchDigest(request.Patch), Step: HistoryStepPrepare,
		SourceBeforeDest: sourceBeforeDest, Summary: strings.TrimSpace(request.Summary), Description: request.Description,
	}
	if historyPatchSourceCopy(request.Kind, sourceBeforeDest) {
		state.SourceCopySHA, err = s.copyHistorySourceWithPatchInParent(commits[source], request.Patch)
		if err != nil {
			return err
		}
	}
	baseIndex := source + 1
	if request.Kind == HistoryPatchMoveCommit && sourceBeforeDest {
		baseIndex = destination + 1
		state.KeepEmpty = true
	}
	if request.Kind == HistoryPatchNewBefore {
		state.KeepEmpty = true
	}
	state.BaseSHA = sessionRebaseBase(commits, baseIndex)
	if request.Kind == HistoryPatchMoveIndex {
		dirty, dirtyErr := s.cmd.New([]string{"git", "status", "--porcelain=v1", "-z", "--untracked-files=no"}).RunWithOutput()
		if dirtyErr != nil {
			return fmt.Errorf("history patch: inspect existing work: %w", dirtyErr)
		}
		state.NeedsStash = dirty != ""
	}
	var snapshot *WorkSnapshot
	if hasWork, err := s.repositoryHasWork(); err != nil {
		return err
	} else if hasWork {
		captured, err := s.captureWorkSnapshot(ctx, RecoverySnapshotKind, "Before Custom Patch history edit")
		if err != nil {
			return err
		}
		snapshot = &captured
	}
	record, err := s.beginOperation(OperationHistoryPatch, string(request.Kind)+" Custom Patch", UndoResetHead, snapshot)
	if err != nil {
		return err
	}
	branch, head, err := s.currentOperationIdentity()
	if err != nil {
		return err
	}
	if branch == "" || head != request.ExpectedHeadSHA || "refs/heads/"+branch != request.ExpectedHeadRef {
		return fmt.Errorf("history patch: %w: HEAD changed while recovery was prepared", ErrOperationChanged)
	}
	record.HistoryPatch = state
	data := []byte(request.Patch)
	if err := s.fileService().WriteFileAtomic(s.joinPath(s.journalDir(), record.ID+".patch"), data, 0o600); err != nil {
		return fmt.Errorf("history patch: save approved patch: %w", err)
	}
	if state.SourceCopySHA != "" {
		if _, err := s.cmd.New([]string{"git", "update-ref", journalRefPrefix + record.ID + "/copy", state.SourceCopySHA}).RunWithOutput(); err != nil {
			return fmt.Errorf("history patch: pin synthetic source copy: %w", err)
		}
	}
	// Publish active provenance only after the original patch and synthetic
	// copy are durable. A missing copy ref can be repaired during prepare; an
	// active record never lacks the approved bytes needed to resume it.
	record.ActiveOperation = OperationTypeRebase
	record.RebasePaused = true
	if record, err = s.pinOperationBefore(record); err != nil {
		return err
	}
	if err := s.syncDirectory(s.journalDir()); err != nil {
		return err
	}
	return s.resumeHistoryPatch(ctx, record, false)
}

// driveHistoryOperation is called by DriveOperation while holding the same
// worktree lock and after checking the user's expected operation fingerprint.
func (s *gitSession) driveHistoryOperation(ctx context.Context, current OperationState, options OperationDriveOptions) error {
	record, err := s.activeHistoryPatchRecord(ctx)
	if err != nil {
		return err
	}
	if record == nil || record.HistoryPatch == nil {
		return fmt.Errorf("history patch: its persistent journal is missing")
	}
	checked, checkErr := s.OperationState(ctx)
	if checkErr != nil {
		return checkErr
	}
	if checked.Fingerprint != current.Fingerprint {
		return fmt.Errorf("%w: history patch journal or operation changed during preflight", ErrOperationChanged)
	}
	if err := s.validateHistoryPatchPins(*record); err != nil {
		return err
	}
	if options.Action == OperationActionAbort {
		return s.abortHistoryPatch(*record)
	}
	if options.Action != OperationActionContinue {
		return fmt.Errorf("history patch: skip is not supported")
	}
	if err := s.ensureHistoryPatchCopyPin(*record); err != nil {
		return err
	}
	unmerged, err := s.unmergedStages()
	if err != nil {
		return err
	}
	if unmerged != "" {
		return fmt.Errorf("resolve all conflicts before continuing the Custom Patch history edit")
	}
	checked, checkErr = s.OperationState(ctx)
	if checkErr != nil {
		return checkErr
	}
	if checked.Fingerprint != current.Fingerprint {
		return fmt.Errorf("%w: conflicts or journal changed during preflight", ErrOperationChanged)
	}
	resolved := record.HistoryPatch.Conflict
	// Keep the conflict marker durable until the resolved stage finishes. If
	// the process stops before that commit, a later driver still knows not to
	// repeat an already-resolved apply. Success clears it with the next stage.
	return s.resumeHistoryPatch(ctx, *record, resolved)
}
