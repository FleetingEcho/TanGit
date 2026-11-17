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
	"github.com/stefanhaller/git-todo-parser/todo"
)

// ErrStackSyncChanged means that a reviewed plan no longer describes the
// repository. Apply never reinterprets an old plan against new source state.
var ErrStackSyncChanged = errors.New("stack sync source changed")

// StackSyncMode selects how a checked-out branch reconciles local and remote
// histories. Fast-forwards always use merge --ff-only in either mode.
type StackSyncMode string

const (
	StackSyncRebase StackSyncMode = "rebase"
	StackSyncMerge  StackSyncMode = "merge"
)

// StackSyncRelation is the exact relationship between the frozen local and
// remote OIDs. Diverged is deliberately distinct from RemoteRewritten: a
// rewritten upstream has no local-only patches, while a diverged branch does.
type StackSyncRelation string

const (
	StackSyncUpToDate        StackSyncRelation = "up-to-date"
	StackSyncFastForward     StackSyncRelation = "fast-forward"
	StackSyncLocalAheadOnly  StackSyncRelation = "local-ahead-only"
	StackSyncRemoteRewritten StackSyncRelation = "remote-rewritten"
	StackSyncDiverged        StackSyncRelation = "diverged"
	StackSyncNoUpstream      StackSyncRelation = "no-upstream"
	StackSyncUpstreamMissing StackSyncRelation = "upstream-missing"
)

// StackSyncAction is the local operation previewed for one branch.
type StackSyncAction string

const (
	StackSyncActionNone      StackSyncAction = "none"
	StackSyncActionUpdateRef StackSyncAction = "update-ref"
	StackSyncActionMerge     StackSyncAction = "merge"
	StackSyncActionRebase    StackSyncAction = "rebase"
	StackSyncActionSkip      StackSyncAction = "skip"
)

// StackSyncOptions controls plan construction. Fetch happens before the source
// is frozen; Apply never fetches. Publish uses an explicit expected-OID lease
// for every branch, including ordinary fast-forward publishes.
type StackSyncOptions struct {
	MainBranch string
	Fetch      bool
	Publish    bool
	Mode       StackSyncMode
}

// StackSyncUpstream freezes Git's configured upstream, not a name inferred
// from the selected branch or a stale UI snapshot.
type StackSyncUpstream struct {
	Remote       string
	RemoteBranch string
	Ref          string
	OID          string
}

// StackSyncBranch is an immutable per-branch preview. LocalOID and RemoteOID
// (in Upstream.OID) are always full object IDs when present.
type StackSyncBranch struct {
	Name             string
	Ref              string
	LocalOID         string
	Upstream         StackSyncUpstream
	Parent           string
	ParentOID        string
	ParentTargetOID  string
	MergeBaseOID     string
	Relation         StackSyncRelation
	Ahead            int
	Behind           int
	Action           StackSyncAction
	TargetOID        string
	RebaseFromOID    string
	CheckedOut       bool
	Current          bool
	WorktreePath     string
	WorktreeHeadOID  string
	WorktreeDirty    bool
	LiveOperation    string
	Publish          bool
	PublishAllowed   bool
	MayUseUpdateRefs bool
	Warnings         []string
	Error            string
}

// StackSyncMergedBranch records why a local branch was omitted from the plan.
// Keeping its OID in the source fingerprint makes a later unmerge invalidate
// an already-reviewed plan.
type StackSyncMergedBranch struct {
	Name     string
	Ref      string
	LocalOID string
}

// StackSyncPlan freezes every source fact used by Apply. SourceID binds the
// complete structure, including topology and worktree safety observations.
type StackSyncPlan struct {
	Repository       string
	MainBranch       string
	MainOID          string
	CurrentBranch    string
	CurrentOID       string
	Mode             StackSyncMode
	Publish          bool
	RebaseUpdateRefs bool
	Branches         []StackSyncBranch
	Merged           []StackSyncMergedBranch
	SourceID         string
}

// Actionable reports whether Apply has local work to perform on this branch.
func (b StackSyncBranch) Actionable() bool {
	return b.Action == StackSyncActionUpdateRef || b.Action == StackSyncActionMerge || b.Action == StackSyncActionRebase
}

// CanApply reports whether the preview contains local work or a publication.
func (p StackSyncPlan) CanApply() bool {
	for _, branch := range p.Branches {
		if branch.Actionable() || branch.Publish {
			return true
		}
	}
	return false
}

// StackSyncBranchResult reports the operation which actually ran. Driver is
// "git update-ref transaction", "git merge", "git rebase", or
// "git rebase.updateRefs". UpdateRefsTODO is non-empty only when Git's real,
// captured interactive-rebase TODO drove the ref update.
type StackSyncBranchResult struct {
	Name           string
	Action         StackSyncAction
	Driver         string
	UpdateRefsTODO string
	OldOID         string
	NewOID         string
	Updated        bool
	Skipped        bool
	Warnings       []string
	Error          string
}

// StackSyncPushResult is one independent remote ref publication. Published
// and Error are mutually exclusive. No group of pushes is remotely atomic.
type StackSyncPushResult struct {
	Remote            string
	Branch            string
	RemoteBranch      string
	LocalOID          string
	ExpectedRemoteOID string
	Published         bool
	Rewriting         bool
	ForceWithLease    bool
	Error             string
}

// StackSyncResult preserves partial local progress and every attempted push.
// A local preflight/CAS failure returns ErrStackSyncChanged and no pushes.
type StackSyncResult struct {
	Branches []StackSyncBranchResult
	Pushes   []StackSyncPushResult
}

// StackSyncSession is optional so existing Session implementations do not
// need new stubs. The concrete Git session always implements it.
type StackSyncSession interface {
	Session
	ProvideStackSync(context.Context, StackSyncOptions) (StackSyncPlan, error)
	PlanStackSync(context.Context, StackSyncOptions) (StackSyncPlan, error)
	ApplyStackSync(context.Context, StackSyncPlan) (StackSyncResult, error)
}

// ProvideStackSync always constructs a fresh plan. It is intentionally a
// separate convenience entry point for UI providers which recompute on every
// open/refresh instead of caching a plan keyed by branch name.
func (s *gitSession) ProvideStackSync(ctx context.Context, options StackSyncOptions) (StackSyncPlan, error) {
	return s.PlanStackSync(ctx, options)
}

type stackSyncSourceBranch struct {
	name      string
	ref       string
	localOID  string
	upstream  StackSyncUpstream
	parent    string
	parentOID string
	depth     int
}

func stackSyncFullOID(oid string) bool {
	if len(oid) != 40 && len(oid) != 64 {
		return false
	}
	_, err := hex.DecodeString(oid)
	return err == nil
}

func stackSyncCleanPath(path string) string {
	return strings.TrimSuffix(strings.TrimSpace(path), "/")
}

func stackSyncPlanID(plan StackSyncPlan) (string, error) {
	plan.SourceID = ""
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("fingerprint stack sync plan: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (s *gitSession) stackSyncText(ctx context.Context, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	stdout, stderr, err := s.cmd.New(append([]string{"git"}, args...)).DontLog().RunWithOutputs()
	if err != nil {
		return "", fmt.Errorf("git %s: %w%s", strings.Join(args, " "), err, commandOutputSummary(stdout+stderr))
	}
	return strings.TrimSpace(stdout), nil
}

func (s *gitSession) stackSyncAncestor(ctx context.Context, ancestor, descendant string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	err := s.cmd.New([]string{"git", "merge-base", "--is-ancestor", ancestor, descendant}).DontLog().Run()
	if err == nil {
		return true, nil
	}
	if code, ok := commandExitCode(err); ok && code == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %w", err)
}

func (s *gitSession) stackSyncRevCount(ctx context.Context, left, right string) (int, error) {
	output, err := s.stackSyncText(ctx, "rev-list", "--count", left+".."+right)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(output)
	if err != nil {
		return 0, fmt.Errorf("parse revision count %q: %w", output, err)
	}
	return count, nil
}

func (s *gitSession) stackSyncBranches(ctx context.Context) ([]stackSyncSourceBranch, error) {
	output, err := s.stackSyncText(ctx, "for-each-ref",
		"--format=%(refname)%09%(objectname)%09%(upstream)%09%(upstream:remotename)%09%(upstream:remoteref)%09%(objecttype)",
		"refs/heads")
	if err != nil {
		return nil, err
	}
	var branches []stackSyncSourceBranch
	for line := range strings.SplitSeq(output, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 || fields[5] != "commit" {
			return nil, fmt.Errorf("malformed for-each-ref output %q", line)
		}
		branch := stackSyncSourceBranch{
			name: strings.TrimPrefix(fields[0], "refs/heads/"), ref: fields[0], localOID: fields[1],
			upstream: StackSyncUpstream{Remote: fields[3], Ref: fields[2]},
		}
		if !stackSyncFullOID(branch.localOID) {
			return nil, fmt.Errorf("branch %s has non-full object ID %q", branch.name, branch.localOID)
		}
		if branch.upstream.Ref != "" {
			branch.upstream.RemoteBranch = strings.TrimPrefix(fields[4], "refs/heads/")
			branch.upstream.OID, err = s.stackSyncText(ctx, "rev-parse", "--verify", branch.upstream.Ref+"^{commit}")
			if err != nil {
				// A configured upstream whose tracking ref is gone remains in
				// the preview, with no remote OID and no mutation authority.
				branch.upstream.OID = ""
			}
			if branch.upstream.OID != "" && !stackSyncFullOID(branch.upstream.OID) {
				return nil, fmt.Errorf("upstream %s has non-full object ID %q", branch.upstream.Ref, branch.upstream.OID)
			}
		}
		branches = append(branches, branch)
	}
	slices.SortFunc(branches, func(a, b stackSyncSourceBranch) int { return strings.Compare(a.name, b.name) })
	return branches, nil
}

func (s *gitSession) stackSyncMain(options StackSyncOptions, branches []stackSyncSourceBranch) (stackSyncSourceBranch, error) {
	names := []string{options.MainBranch}
	if options.MainBranch == "" {
		names = []string{"main", "master"}
	}
	for _, name := range names {
		for _, branch := range branches {
			if branch.name == name {
				return branch, nil
			}
		}
	}
	if options.MainBranch != "" {
		return stackSyncSourceBranch{}, fmt.Errorf("stack sync main branch %s does not exist", options.MainBranch)
	}
	return stackSyncSourceBranch{}, fmt.Errorf("stack sync requires a main or master branch")
}

func (s *gitSession) stackSyncFetch(ctx context.Context) error {
	output, err := s.stackSyncText(ctx, "remote")
	if err != nil {
		return err
	}
	for remote := range strings.SplitSeq(output, "\n") {
		remote = strings.TrimSpace(remote)
		if remote == "" {
			continue
		}
		if err := s.FetchRemote(ctx, remote); err != nil {
			return fmt.Errorf("fetch %s for stack sync: %w", remote, err)
		}
	}
	return nil
}

func (s *gitSession) stackSyncUpdateRefs(ctx context.Context) (bool, error) {
	stdout, stderr, err := s.cmd.New([]string{"git", "config", "--bool", "--get", "rebase.updateRefs"}).DontLog().RunWithOutputs()
	if err == nil {
		value, parseErr := strconv.ParseBool(strings.TrimSpace(stdout))
		if parseErr != nil {
			return false, fmt.Errorf("parse rebase.updateRefs: %w", parseErr)
		}
		return value, nil
	}
	if code, ok := commandExitCode(err); ok && code == 1 {
		return false, nil
	}
	return false, fmt.Errorf("read rebase.updateRefs: %w%s", err, commandOutputSummary(stderr))
}

func (s *gitSession) stackSyncWorktreeOperation(worktree *models.Worktree) (string, error) {
	parts := []struct {
		name string
		kind string
	}{
		{"rebase-merge", "rebase"}, {"rebase-apply", "rebase/am"},
		{"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"}, {"BISECT_LOG", "bisect"},
	}
	for _, part := range parts {
		_, err := s.fileService().Stat(s.joinPath(worktree.GitDir, part.name))
		if err == nil {
			return part.kind, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect %s operation in %s: %w", part.kind, worktree.Path, err)
		}
	}
	return "", nil
}

func (s *gitSession) stackSyncWorktreeDirty(ctx context.Context, path string) (bool, error) {
	stdout, stderr, err := s.cmd.New([]string{"git", "-C", path, "status", "--porcelain=v1", "--untracked-files=all"}).DontLog().RunWithOutputs()
	if err != nil {
		return false, fmt.Errorf("read worktree status %s: %w%s", path, err, commandOutputSummary(stderr))
	}
	return strings.TrimSpace(stdout) != "", nil
}

func (s *gitSession) stackSyncRelation(ctx context.Context, branch stackSyncSourceBranch) (StackSyncRelation, int, int, string, error) {
	if branch.upstream.Ref == "" {
		return StackSyncNoUpstream, 0, 0, "", nil
	}
	if branch.upstream.OID == "" {
		return StackSyncUpstreamMissing, 0, 0, "", nil
	}
	local, remote := branch.localOID, branch.upstream.OID
	if local == remote {
		return StackSyncUpToDate, 0, 0, local, nil
	}
	ahead, err := s.stackSyncRevCount(ctx, remote, local)
	if err != nil {
		return "", 0, 0, "", err
	}
	behind, err := s.stackSyncRevCount(ctx, local, remote)
	if err != nil {
		return "", 0, 0, "", err
	}
	base, err := s.stackSyncText(ctx, "merge-base", local, remote)
	if err != nil {
		return "", 0, 0, "", err
	}
	localBehind, err := s.stackSyncAncestor(ctx, local, remote)
	if err != nil {
		return "", 0, 0, "", err
	}
	if localBehind {
		return StackSyncFastForward, ahead, behind, base, nil
	}
	remoteBehind, err := s.stackSyncAncestor(ctx, remote, local)
	if err != nil {
		return "", 0, 0, "", err
	}
	if remoteBehind {
		return StackSyncLocalAheadOnly, ahead, behind, base, nil
	}

	// git cherry compares patches, not just topology. Every local-only patch
	// being present upstream identifies a rewritten remote; a single '+' is
	// local work which must be reconciled rather than discarded as "rewrite".
	cherry, err := s.stackSyncText(ctx, "cherry", remote, local)
	if err != nil {
		return "", 0, 0, "", err
	}
	rewritten := true
	for line := range strings.SplitSeq(cherry, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "-") {
			rewritten = false
			break
		}
	}
	if rewritten {
		return StackSyncRemoteRewritten, ahead, behind, base, nil
	}
	return StackSyncDiverged, ahead, behind, base, nil
}

func stackSyncOrder(branches []StackSyncBranch) {
	byName := make(map[string]StackSyncBranch, len(branches))
	for _, branch := range branches {
		byName[branch.Name] = branch
	}
	depth := func(name string) int {
		seen := map[string]bool{}
		result := 0
		for name != "" && !seen[name] {
			seen[name] = true
			parent, ok := byName[name]
			if !ok {
				break
			}
			name = parent.Parent
			if name != "" {
				result++
			}
		}
		return result
	}
	slices.SortStableFunc(branches, func(a, b StackSyncBranch) int {
		if difference := depth(a.Name) - depth(b.Name); difference != 0 {
			return difference
		}
		return strings.Compare(a.Name, b.Name)
	})
}

// PlanStackSync reads Git directly and freezes a complete operation plan. No
// local branch or remote is changed except for an explicitly requested fetch.
func (s *gitSession) PlanStackSync(ctx context.Context, options StackSyncOptions) (StackSyncPlan, error) {
	if err := ctx.Err(); err != nil {
		return StackSyncPlan{}, err
	}
	if options.Mode == "" {
		options.Mode = StackSyncRebase
	}
	if options.Mode != StackSyncRebase && options.Mode != StackSyncMerge {
		return StackSyncPlan{}, fmt.Errorf("unsupported stack sync mode %q", options.Mode)
	}
	if options.Fetch {
		if err := s.stackSyncFetch(ctx); err != nil {
			return StackSyncPlan{}, err
		}
	}
	return s.readStackSyncPlan(ctx, options)
}

func (s *gitSession) readStackSyncPlan(ctx context.Context, options StackSyncOptions) (StackSyncPlan, error) {
	branches, err := s.stackSyncBranches(ctx)
	if err != nil {
		return StackSyncPlan{}, err
	}
	main, err := s.stackSyncMain(options, branches)
	if err != nil {
		return StackSyncPlan{}, err
	}
	currentBranch, err := s.stackSyncText(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		// Interactive rebases deliberately detach HEAD. Git's operation
		// metadata still names the branch, so preserve a per-branch live
		// operation refusal instead of reducing it to a detached-HEAD error.
		operation, operationErr := s.OperationState(ctx)
		if operationErr != nil {
			return StackSyncPlan{}, operationErr
		}
		if operation.Type != OperationTypeNone {
			for _, ref := range operation.FullRefs {
				if strings.HasPrefix(ref, "refs/heads/") {
					currentBranch = strings.TrimPrefix(ref, "refs/heads/")
					break
				}
			}
		}
		if currentBranch == "" {
			return StackSyncPlan{}, fmt.Errorf("stack sync requires an attached HEAD: %w", err)
		}
	}
	currentOID, err := s.stackSyncText(ctx, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return StackSyncPlan{}, err
	}
	updateRefs, err := s.stackSyncUpdateRefs(ctx)
	if err != nil {
		return StackSyncPlan{}, err
	}
	plan := StackSyncPlan{
		Repository: s.repositoryIdentity(), MainBranch: main.name, MainOID: main.localOID,
		CurrentBranch: currentBranch, CurrentOID: currentOID, Mode: options.Mode,
		Publish: options.Publish, RebaseUpdateRefs: updateRefs,
	}

	merged := map[string]bool{}
	candidates := []stackSyncSourceBranch{main}
	for _, branch := range branches {
		if branch.name == main.name {
			continue
		}
		isMerged, err := s.stackSyncAncestor(ctx, branch.localOID, main.localOID)
		if err != nil {
			return StackSyncPlan{}, err
		}
		if isMerged {
			merged[branch.name] = true
			plan.Merged = append(plan.Merged, StackSyncMergedBranch{Name: branch.name, Ref: branch.ref, LocalOID: branch.localOID})
			continue
		}
		candidates = append(candidates, branch)
	}

	// A branch's parent is the nearest other unmerged local tip which is a
	// strict ancestor. Both names and OIDs are retained in the fingerprint.
	for index := range candidates {
		branch := &candidates[index]
		if branch.name == main.name {
			continue
		}
		branch.parent = main.name
		branch.parentOID = main.localOID
		bestDepth := -1
		for _, possible := range candidates {
			if possible.name == branch.name || possible.localOID == branch.localOID {
				continue
			}
			ancestor, err := s.stackSyncAncestor(ctx, possible.localOID, branch.localOID)
			if err != nil {
				return StackSyncPlan{}, err
			}
			if !ancestor {
				continue
			}
			depth, err := s.stackSyncRevCount(ctx, possible.localOID, branch.localOID)
			if err != nil {
				return StackSyncPlan{}, err
			}
			if depth > 0 && (bestDepth < 0 || depth < bestDepth || depth == bestDepth && possible.name < branch.parent) {
				bestDepth, branch.parent, branch.parentOID, branch.depth = depth, possible.name, possible.localOID, depth
			}
		}
	}

	worktrees, _, err := s.sessionWorktrees()
	if err != nil {
		return StackSyncPlan{}, fmt.Errorf("list stack sync worktrees: %w", err)
	}
	worktreeByBranch := map[string]*models.Worktree{}
	for _, worktree := range worktrees {
		if worktree.Branch != "" {
			worktreeByBranch[worktree.Branch] = worktree
		}
	}

	branchIndex := map[string]int{}
	for _, source := range candidates {
		if source.name == main.name {
			continue
		}
		branch := StackSyncBranch{
			Name: source.name, Ref: source.ref, LocalOID: source.localOID, Upstream: source.upstream,
			Parent: source.parent, ParentOID: source.parentOID, ParentTargetOID: source.parentOID,
		}
		branch.Relation, branch.Ahead, branch.Behind, branch.MergeBaseOID, err = s.stackSyncRelation(ctx, source)
		if err != nil {
			return StackSyncPlan{}, fmt.Errorf("classify %s: %w", source.name, err)
		}
		if worktree := worktreeByBranch[source.name]; worktree != nil {
			branch.CheckedOut = true
			branch.Current = stackSyncCleanPath(worktree.Path) == stackSyncCleanPath(s.git.RepoPaths.WorktreePath())
			branch.WorktreePath = worktree.Path
			branch.WorktreeHeadOID = worktree.Head
			if !worktree.IsPathMissing {
				branch.LiveOperation, err = s.stackSyncWorktreeOperation(worktree)
				if err != nil {
					return StackSyncPlan{}, err
				}
				branch.WorktreeDirty, err = s.stackSyncWorktreeDirty(ctx, worktree.Path)
				if err != nil {
					return StackSyncPlan{}, err
				}
			} else {
				branch.Error = "checked-out worktree is missing"
			}
		}
		branchIndex[branch.Name] = len(plan.Branches)
		plan.Branches = append(plan.Branches, branch)
	}

	// Parents precede children, so ParentTargetOID is the actual planned
	// target (or the unchanged local OID), not a second live Git read.
	stackSyncOrder(plan.Branches)
	branchIndex = map[string]int{}
	for index, branch := range plan.Branches {
		branchIndex[branch.Name] = index
	}
	for index := range plan.Branches {
		branch := &plan.Branches[index]
		if parent, ok := branchIndex[branch.Parent]; ok {
			target := plan.Branches[parent].TargetOID
			if target == "" {
				target = plan.Branches[parent].LocalOID
			}
			branch.ParentTargetOID = target
		}
		parentChanged := branch.Parent != "" && branch.ParentTargetOID != "" && branch.ParentTargetOID != branch.ParentOID
		if parentChanged {
			ancestor, ancestorErr := s.stackSyncAncestor(ctx, branch.ParentOID, branch.LocalOID)
			if ancestorErr != nil {
				return StackSyncPlan{}, ancestorErr
			}
			parentChanged = ancestor
			if !ancestor {
				branch.Warnings = append(branch.Warnings, "parent is not an ancestor of this branch; no parent-driven rebase is planned")
			}
		}
		// A live operation is itself a refusal to apply this branch's stack
		// plan, even when its local ref otherwise looks up to date.
		requiresLocal := parentChanged || branch.LiveOperation != "" || branch.Relation == StackSyncFastForward || branch.Relation == StackSyncRemoteRewritten || branch.Relation == StackSyncDiverged
		branch.PublishAllowed = options.Publish && branch.Upstream.OID != ""
		branch.Publish = branch.PublishAllowed && (branch.Relation == StackSyncLocalAheadOnly || branch.Relation == StackSyncDiverged || parentChanged)
		if branch.Relation == StackSyncUpstreamMissing {
			branch.PublishAllowed, branch.Publish = false, false
			branch.Error = "upstream remote-tracking ref is missing; fetch and review the upstream"
		}
		if branch.Relation == StackSyncNoUpstream {
			branch.Warnings = append(branch.Warnings, "no upstream is configured")
		}
		switch {
		case !requiresLocal:
			branch.Action = StackSyncActionNone
		case branch.LiveOperation != "":
			branch.Action = StackSyncActionSkip
			branch.Publish = false
			branch.Error = "a live " + branch.LiveOperation + " operation must be finished or aborted first"
		case branch.Relation == StackSyncDiverged && !branch.Current:
			branch.Action = StackSyncActionSkip
			branch.Publish = false
			branch.Warnings = append(branch.Warnings, "diverged local work must be merged or rebased in its checked-out worktree")
		case parentChanged && !branch.Current && branch.Relation != StackSyncFastForward && branch.Relation != StackSyncRemoteRewritten:
			branch.Action = StackSyncActionSkip
			branch.Publish = false
			branch.Warnings = append(branch.Warnings, "the parent moved; checkout this branch to replay its local commits")
		case branch.CheckedOut && !branch.Current:
			branch.Action = StackSyncActionSkip
			if branch.Relation != StackSyncLocalAheadOnly && branch.Relation != StackSyncUpToDate {
				branch.Publish = false
			}
			branch.Warnings = append(branch.Warnings, "checked out in another worktree; local ref update skipped: "+branch.WorktreePath)
		case branch.Current && branch.WorktreeDirty:
			branch.Action = StackSyncActionSkip
			branch.Publish = false
			branch.Error = "checked-out worktree has uncommitted changes"
		case branch.Relation == StackSyncDiverged || branch.Relation == StackSyncRemoteRewritten || parentChanged:
			if branch.Current {
				branch.TargetOID = branch.Upstream.OID
				branch.RebaseFromOID = branch.MergeBaseOID
				if parentChanged && (branch.Relation == StackSyncUpToDate || branch.Relation == StackSyncLocalAheadOnly || branch.Relation == StackSyncNoUpstream) {
					branch.TargetOID = branch.ParentTargetOID
					branch.RebaseFromOID = branch.ParentOID
				}
				if branch.TargetOID == "" {
					branch.Action = StackSyncActionSkip
					branch.Error = "no frozen target is available for reconciliation"
					break
				}
				if options.Mode == StackSyncMerge && !parentChanged && branch.Relation == StackSyncDiverged {
					branch.Action = StackSyncActionMerge
				} else {
					branch.Action = StackSyncActionRebase
				}
			} else if branch.Relation == StackSyncFastForward || branch.Relation == StackSyncRemoteRewritten {
				branch.Action = StackSyncActionUpdateRef
				branch.TargetOID = branch.Upstream.OID
			} else {
				branch.Action = StackSyncActionSkip
				branch.Publish = false
			}
		case branch.Relation == StackSyncFastForward:
			branch.TargetOID = branch.Upstream.OID
			if branch.Current {
				branch.Action = StackSyncActionMerge
			} else {
				branch.Action = StackSyncActionUpdateRef
			}
		}
		if branch.Action == StackSyncActionRebase && updateRefs {
			branch.MayUseUpdateRefs = true
		}
		// An unchanged or skipped parent leaves its old OID as its child's
		// base. Actual rebase.updateRefs results are read after Git runs.
		if branch.TargetOID == "" {
			branch.TargetOID = branch.LocalOID
		}
	}
	plan.SourceID, err = stackSyncPlanID(plan)
	if err != nil {
		return StackSyncPlan{}, err
	}
	return plan, nil
}

func (s *gitSession) validateStackSyncPlan(ctx context.Context, plan StackSyncPlan) error {
	id, err := stackSyncPlanID(plan)
	if err != nil {
		return err
	}
	if plan.SourceID == "" || id != plan.SourceID {
		return fmt.Errorf("%w: plan fingerprint is invalid; provide a fresh plan", ErrStackSyncChanged)
	}
	if plan.Repository != s.repositoryIdentity() {
		return fmt.Errorf("%w: repository identity differs", ErrStackSyncChanged)
	}
	fresh, err := s.readStackSyncPlan(ctx, StackSyncOptions{MainBranch: plan.MainBranch, Mode: plan.Mode, Publish: plan.Publish})
	if err != nil {
		return fmt.Errorf("re-read stack sync source: %w", err)
	}
	if fresh.SourceID != plan.SourceID {
		return fmt.Errorf("%w: local/remote OIDs, topology, upstream, worktree, or live operation changed; provide a fresh plan", ErrStackSyncChanged)
	}
	return nil
}

type stackSyncRefUpdate struct {
	branch StackSyncBranch
}

func stackSyncRefTransaction(updates []stackSyncRefUpdate) string {
	var input strings.Builder
	input.WriteString("start\x00")
	for _, update := range updates {
		input.WriteString("update ")
		input.WriteString(update.branch.Ref)
		input.WriteByte(0)
		input.WriteString(update.branch.TargetOID)
		input.WriteByte(0)
		input.WriteString(update.branch.LocalOID)
		input.WriteByte(0)
	}
	input.WriteString("prepare\x00commit\x00")
	return input.String()
}

func stackSyncUpdateRefsFromTODO(todos []todo.Todo) map[string]string {
	refs := map[string]string{}
	for _, item := range todos {
		if item.Command == todo.UpdateRef && strings.HasPrefix(item.Ref, "refs/heads/") {
			refs[item.Ref] = "update-ref " + item.Ref
		}
	}
	return refs
}

func (s *gitSession) captureStackSyncRebase(args []string) (map[string]string, error) {
	todos, err := s.captureRemoteRebaseTodo(args)
	if err != nil {
		return nil, fmt.Errorf("capture Git's rebase.updateRefs TODO: %w", err)
	}
	return stackSyncUpdateRefsFromTODO(todos), nil
}

func (s *gitSession) stackSyncRebaseArgs(plan StackSyncPlan, branch StackSyncBranch) []string {
	args := []string{"git", "rebase", "--interactive", "--keep-empty"}
	if plan.RebaseUpdateRefs {
		args = append(args, "--update-refs")
	} else {
		args = append(args, "--no-update-refs")
	}
	args = append(args, "--onto", branch.TargetOID, branch.RebaseFromOID, branch.CurrentName(plan))
	return args
}

func (b StackSyncBranch) CurrentName(plan StackSyncPlan) string {
	if b.Current {
		return plan.CurrentBranch
	}
	return b.Name
}

func (s *gitSession) applyStackSyncCurrent(ctx context.Context, plan StackSyncPlan, branch StackSyncBranch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch branch.Action {
	case StackSyncActionMerge:
		args := []string{"git", "merge", "--no-edit"}
		if branch.Relation == StackSyncFastForward {
			args = append(args, "--ff-only")
		}
		args = append(args, branch.TargetOID)
		stdout, stderr, err := s.cmd.New(args).RunWithOutputs()
		if err != nil {
			return fmt.Errorf("git merge %s: %w%s", branch.Name, err, commandOutputSummary(stdout+stderr))
		}
		return nil
	case StackSyncActionRebase:
		args := s.stackSyncRebaseArgs(plan, branch)
		command := s.cmd.New(args).AddEnvVars(remoteRebaseEnvironment(":", ":")...)
		command.RecoverFromLockErrorWith(func() *oscommands.CmdObj { return nil })
		stdout, stderr, err := command.RunWithOutputs()
		if err != nil {
			return fmt.Errorf("git rebase %s: %w%s", branch.Name, err, commandOutputSummary(stdout+stderr))
		}
		return nil
	default:
		return nil
	}
}

func stackSyncResultFor(branch StackSyncBranch) StackSyncBranchResult {
	result := StackSyncBranchResult{
		Name: branch.Name, Action: branch.Action, OldOID: branch.LocalOID,
		NewOID: branch.LocalOID, Warnings: slices.Clone(branch.Warnings), Error: branch.Error,
		Skipped: branch.Action == StackSyncActionSkip,
	}
	return result
}

// ApplyStackSync validates the reviewed source under the mutation lock. A
// failed CAS transaction rejects the whole update-ref batch before the real
// checked-out merge/rebase starts. Independent pushes then report their own
// outcomes; they are never described as a remote transaction.
func (s *gitSession) ApplyStackSync(ctx context.Context, plan StackSyncPlan) (StackSyncResult, error) {
	result := StackSyncResult{}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	unlock := s.lockWorktreeMutation()
	defer unlock()
	if err := s.validateStackSyncPlan(ctx, plan); err != nil {
		return result, err
	}
	byName := make(map[string]int, len(plan.Branches))
	for index, branch := range plan.Branches {
		byName[branch.Name] = index
		result.Branches = append(result.Branches, stackSyncResultFor(branch))
	}

	// If updateRefs is enabled, ask Git for its real TODO once, abort that
	// capture-only operation, and exclude precisely those refs from the later
	// update-ref transaction. The final rebase uses the same unmodified TODO.
	updatedByRebase := map[string]string{}
	if plan.RebaseUpdateRefs {
		for _, branch := range plan.Branches {
			if !branch.Current || branch.Action != StackSyncActionRebase {
				continue
			}
			args := s.stackSyncRebaseArgs(plan, branch)
			refs, err := s.captureStackSyncRebase(args)
			if err != nil {
				return result, err
			}
			for ref, line := range refs {
				if branchIndex, ok := byName[strings.TrimPrefix(ref, "refs/heads/")]; ok {
					other := plan.Branches[branchIndex]
					if other.CheckedOut && !other.Current {
						return result, fmt.Errorf("Git's rebase.updateRefs TODO would update %s checked out in %s; disable updateRefs or sync that worktree separately", other.Name, other.WorktreePath)
					}
				}
				updatedByRebase[ref] = line
			}
		}
		// Capturing and aborting must not hide an external source change.
		if err := s.validateStackSyncPlan(ctx, plan); err != nil {
			return result, err
		}
	}

	for ref, line := range updatedByRebase {
		name := strings.TrimPrefix(ref, "refs/heads/")
		if index, ok := byName[name]; ok {
			result.Branches[index].Driver = "git rebase.updateRefs"
			result.Branches[index].UpdateRefsTODO = line
		}
	}

	var updates []stackSyncRefUpdate
	for _, branch := range plan.Branches {
		if branch.Action == StackSyncActionUpdateRef && branch.Error == "" {
			if _, driven := updatedByRebase[branch.Ref]; !driven {
				updates = append(updates, stackSyncRefUpdate{branch: branch})
			}
		}
	}
	if len(updates) > 0 {
		command := s.cmd.New([]string{"git", "update-ref", "-z", "--stdin"}).SetStdin(stackSyncRefTransaction(updates))
		output, err := s.combinedOutput(command)
		if err != nil {
			return result, fmt.Errorf("%w: git update-ref transaction rejected an expected old value; no update in that transaction ran: %v%s", ErrStackSyncChanged, err, commandOutputSummary(string(output)))
		}
		for _, update := range updates {
			index := byName[update.branch.Name]
			result.Branches[index].Driver = "git update-ref transaction"
			result.Branches[index].NewOID = update.branch.TargetOID
			result.Branches[index].Updated = update.branch.TargetOID != update.branch.LocalOID
		}
	}
	for _, branch := range plan.Branches {
		if !branch.Current || !branch.Actionable() {
			continue
		}
		if err := s.applyStackSyncCurrent(ctx, plan, branch); err != nil {
			index := byName[branch.Name]
			result.Branches[index].Error = err.Error()
			oid, _ := s.stackSyncText(context.Background(), "rev-parse", "--verify", branch.Ref)
			result.Branches[index].NewOID = oid
			return result, err
		}
		index := byName[branch.Name]
		if branch.Action == StackSyncActionMerge {
			result.Branches[index].Driver = "git merge"
		} else {
			result.Branches[index].Driver = "git rebase"
		}
	}

	// Read actual local results, including extra updateRefs labels that had no
	// independent update-ref action in the preview.
	localOIDs := map[string]string{}
	for index, branch := range plan.Branches {
		oid, err := s.stackSyncText(ctx, "rev-parse", "--verify", branch.Ref)
		if err != nil {
			return result, err
		}
		localOIDs[branch.Name] = oid
		result.Branches[index].NewOID = oid
		result.Branches[index].Updated = oid != branch.LocalOID
		if branch.Current && result.Branches[index].Driver == "" && oid != branch.LocalOID {
			result.Branches[index].Driver = "git rebase.updateRefs"
		}
	}

	if plan.Publish {
		for index, branch := range plan.Branches {
			if !branch.PublishAllowed || branch.Error != "" {
				continue
			}
			localOID := localOIDs[branch.Name]
			if localOID == branch.Upstream.OID {
				continue
			}
			// A skipped rewrite/divergence may not publish the old unresolved
			// state. Ordinary local-ahead branches remain publishable even when
			// checked out in another worktree.
			if branch.Action == StackSyncActionSkip && branch.Relation != StackSyncLocalAheadOnly && branch.Relation != StackSyncUpToDate {
				continue
			}
			remoteAncestor, err := s.stackSyncAncestor(ctx, branch.Upstream.OID, localOID)
			if err != nil {
				return result, err
			}
			push := StackSyncPushResult{
				Remote: branch.Upstream.Remote, Branch: branch.Name, RemoteBranch: branch.Upstream.RemoteBranch,
				LocalOID: localOID, ExpectedRemoteOID: branch.Upstream.OID,
				Rewriting: !remoteAncestor, ForceWithLease: true,
			}
			refspec := branch.Ref + ":" + "refs/heads/" + branch.Upstream.RemoteBranch
			remoteRef := "refs/heads/" + branch.Upstream.RemoteBranch
			args := []string{"git", "push", "--force-with-lease=" + remoteRef + ":" + branch.Upstream.OID, branch.Upstream.Remote, refspec}
			stdout, stderr, pushErr := s.cmd.New(args).PromptOnCredentialRequest(nil).RunWithOutputs()
			if pushErr != nil {
				push.Error = safeNetworkError(fmt.Errorf("%w%s", pushErr, commandOutputSummary(stdout+stderr)), true).Error()
				result.Branches[index].Warnings = append(result.Branches[index].Warnings, "publication failed; local updates remain: "+push.Error)
			} else {
				push.Published = true
			}
			result.Pushes = append(result.Pushes, push)
		}
	}
	return result, nil
}
