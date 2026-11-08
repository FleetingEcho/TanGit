package repository

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands"
	"github.com/jesseduffield/lazygit/pkg/commands/git_commands"
	"github.com/jesseduffield/lazygit/pkg/commands/git_config"
	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/jesseduffield/lazygit/pkg/commands/oscommands"
	"github.com/jesseduffield/lazygit/pkg/common"
	"github.com/jesseduffield/lazygit/pkg/config"
)

// newGitCommand builds the library's command layer for the repository at dir.
// The library's own constructor reads the process's working directory and
// changes it, and the window keeps several repositories open at once, so this
// variant leaves the process alone: every command is pinned to dir by the
// builder the layer is given.
func newGitCommand(
	dir string,
	cmn *common.Common,
	version *git_commands.GitVersion,
	osCommand *oscommands.OSCommand,
	gitConfig git_config.IGitConfig,
	diffConfig *config.DiffRendererConfigManager,
) (*commands.GitCommand, error) {
	repoPaths, err := git_commands.GetRepoPathsForDir(dir, osCommand.Cmd)
	if err != nil {
		return nil, err
	}
	if repoPaths.IsBareRepo() {
		return nil, fmt.Errorf("%s", cmn.Tr.BareRepoNotSupported)
	}
	// Pin the config reads to the repository, as the library's constructor
	// does: they run outside the command builder.
	gitConfig.SetDir(repoPaths.WorktreePath())
	return commands.NewGitCommandAux(cmn, version, osCommand, gitConfig, repoPaths, diffConfig), nil
}

// recentCommits reads up to limit commits reachable from any ref, newest
// first. A repository whose HEAD has no commit yet has nothing to read, and
// the log command would fail on the unborn HEAD, so it reports an empty list.
func (s *gitSession) recentCommits(limit int) ([]*models.Commit, error) {
	if limit <= 0 {
		return nil, nil
	}
	if !s.headHasCommit() {
		return nil, nil
	}
	commits, err := s.git.Loaders.CommitLoader.GetCommits(git_commands.GetCommitsOptions{
		Limit:                limit,
		IncludeRebaseCommits: s.sshClient == nil,
		// Internal recovery objects are reachable from refs, but are not user
		// history. Git's exclusion applies together with --all below.
		RefName:      "--exclude=refs/tangit/*",
		All:          true,
		MainBranches: s.commitMainBranches,
		HashPool:     s.hashPool,
	})
	if err != nil || s.sshClient == nil {
		return commits, err
	}
	pending, err := s.remoteRebaseCommits()
	if err != nil {
		return nil, err
	}
	return append(pending, commits...), nil
}

// headHasCommit reports whether HEAD names a commit. A repository Git has
// just initialized has none yet.
func (s *gitSession) headHasCommit() bool {
	_, err := s.cmd.New([]string{"git", "rev-parse", "--verify", "--quiet", "HEAD"}).RunWithOutput()
	return err == nil
}

// localBranches reads the local branches with their upstream, ahead and
// behind counts. The extra base-branch work is left out: the window does not
// show it, and it needs a UI to report progress to.
func (s *gitSession) localBranches() ([]*models.Branch, error) {
	return s.git.Loaders.BranchLoader.Load(
		nil,
		s.mainBranches,
		nil,
		false,
		func(work func() error) { _ = work() },
		func() {},
	)
}

// recentReflog reads the newest reflog entries, up to limit; zero reads all
// for undo/redo replay. The display bound belongs on Git's command line:
// Lazygit's loader otherwise reads the whole log before its caller trims it.
func (s *gitSession) recentReflog(limit int) ([]*models.Commit, error) {
	if !s.headHasCommit() {
		return nil, nil
	}
	args := []string{"git", "log", "-g"}
	if limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", limit))
	}
	args = append(args, "--format=%H%x00%ct%x00%gs%x00%P")
	output, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return nil, err
	}
	commits := make([]*models.Commit, 0, max(limit, 0))
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		fields := strings.SplitN(line, "\x00", 4)
		if len(fields) != 4 {
			continue
		}
		timestamp, _ := strconv.ParseInt(fields[1], 10, 64)
		var parents []string
		if fields[3] != "" {
			parents = strings.Split(fields[3], " ")
		}
		commits = append(commits, models.NewCommit(s.hashPool, models.NewCommitOpts{
			Hash:          fields[0],
			UnixTimestamp: timestamp,
			Name:          fields[2],
			Parents:       parents,
		}))
	}
	return commits, nil
}

// branchCommits reads the commits reachable from branch, newest first, as
// `git log branch` does, so a branch's history is readable without checking
// it out.
func (s *gitSession) branchCommits(branch string, limit int) ([]*models.Commit, error) {
	if limit <= 0 {
		return nil, nil
	}
	return s.git.Loaders.CommitLoader.GetCommits(git_commands.GetCommitsOptions{
		Limit:        limit,
		RefName:      branch,
		MainBranches: s.commitMainBranches,
		HashPool:     s.hashPool,
	})
}

// fileCommits reads the commits that changed path, newest first, following
// the file across renames.
func (s *gitSession) fileCommits(path string, limit int) ([]*models.Commit, error) {
	if limit <= 0 {
		return nil, nil
	}
	if !s.headHasCommit() {
		return nil, nil
	}
	return s.git.Loaders.CommitLoader.GetCommits(git_commands.GetCommitsOptions{
		Limit:        limit,
		FilterPath:   path,
		RefName:      "HEAD",
		MainBranches: s.commitMainBranches,
		HashPool:     s.hashPool,
	})
}

// amendHeadWithMessage rewrites the tip commit with the given message, taking
// whatever is staged into it. Hooks run unless forceSkipHooks asks otherwise.
func (s *gitSession) amendHeadWithMessage(summary string, description string, forceSkipHooks bool) error {
	args := []string{"git", "commit", "--amend", "--allow-empty"}
	if forceSkipHooks {
		args = append(args, "--no-verify")
	}
	args = append(args, "-m", summary)
	if description != "" {
		args = append(args, "-m", description)
	}
	return s.cmd.New(args).Run()
}

// resolveFileConflict takes one side of a conflicted file and stages it, which
// marks the conflict resolved. current chooses the checked-out side, otherwise
// the incoming one. A side that does not have the file chose to delete it, so
// the file is removed instead: Git records that resolution by taking the path
// out of both the working tree and the index.
func (s *gitSession) resolveFileConflict(path string, current bool) error {
	stage, side := 3, "--theirs"
	if current {
		stage, side = 2, "--ours"
	}
	present, err := s.indexHasStage(path, stage)
	if err != nil {
		return err
	}
	if !present {
		return s.cmd.New([]string{"git", "rm", "-q", "--", path}).Run()
	}
	if err := s.cmd.New([]string{"git", "checkout", side, "--", path}).Run(); err != nil {
		return err
	}
	return s.cmd.New([]string{"git", "add", "--", path}).Run()
}

// indexHasStage reports whether the index has an entry for stage at path. A
// conflicted file has up to three: the common ancestor, the current side and
// the incoming one. A side that deleted the file has none.
func (s *gitSession) indexHasStage(path string, stage int) (bool, error) {
	output, err := s.cmd.New([]string{"git", "ls-files", "-u", "-z", "--", path}).RunWithOutput()
	if err != nil {
		return false, err
	}
	want := strconv.Itoa(stage)
	for entry := range strings.SplitSeq(output, "\x00") {
		meta, _, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		if fields := strings.Fields(meta); len(fields) == 3 && fields[2] == want {
			return true, nil
		}
	}
	return false, nil
}

// finishOperation continues or aborts the merge, rebase, cherry-pick or revert
// in progress. The editor is pointed at a no-op so a message Git wants to
// confirm is taken as it stands rather than opening an editor.
func (s *gitSession) finishGitOperation(operation string, action string) error {
	cmd := s.cmd.New([]string{"git", operation, "--" + action})
	cmd.AddEnvVars("GIT_EDITOR=:", "GIT_SEQUENCE_EDITOR=:")
	return cmd.Run()
}
