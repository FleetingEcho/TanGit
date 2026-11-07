package repository

import (
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

// workingTreeState is the remote counterpart of Lazygit's private filesystem
// probes. Local sessions retain the upstream implementation unchanged; an SSH
// session performs the same small reads over SFTP instead of accidentally
// inspecting a same-named repository on the GUI machine.
func (s *gitSession) workingTreeState() models.WorkingTreeState {
	if s.sshClient == nil {
		return s.git.Status.WorkingTreeState()
	}
	var result models.WorkingTreeState
	gitDir := s.git.RepoPaths.WorktreeGitDirPath()
	exists := func(parts ...string) bool {
		_, err := s.fileService().Stat(s.joinPath(append([]string{gitDir}, parts...)...))
		return err == nil
	}
	result.ApplyingPatches = exists("rebase-apply", "applying")
	result.Rebasing = exists("rebase-merge") || exists("rebase-apply") && !result.ApplyingPatches
	result.Merging = exists("MERGE_HEAD")
	result.SquashMerging = exists("SQUASH_MSG")
	result.CherryPicking = exists("CHERRY_PICK_HEAD")
	if result.CherryPicking && result.Rebasing {
		// Rebase leaves this file after applying a pick. stopped-sha and the
		// last done entry distinguish that state from standalone cherry-pick.
		content, _ := s.fileService().ReadFile(s.joinPath(gitDir, "CHERRY_PICK_HEAD"))
		cherry := strings.TrimSpace(string(content))
		stopped, stoppedErr := s.fileService().ReadFile(s.joinPath(gitDir, "rebase-merge", "stopped-sha"))
		done, doneErr := s.fileService().ReadFile(s.joinPath(gitDir, "rebase-merge", "done"))
		last := ""
		if doneErr == nil {
			lines := strings.Split(strings.TrimSpace(string(done)), "\n")
			if len(lines) > 0 {
				fields := strings.Fields(lines[len(lines)-1])
				if len(fields) > 1 {
					last = fields[1]
				}
			}
		}
		if cherry != "" && (stoppedErr == nil && strings.HasPrefix(cherry, strings.TrimSpace(string(stopped))) ||
			doneErr == nil && last != "" && strings.HasPrefix(cherry, last)) {
			result.CherryPicking = false
		}
	}
	result.Reverting = exists("REVERT_HEAD")
	if !result.CherryPicking || !result.Reverting {
		sequence, err := s.fileService().ReadFile(s.joinPath(gitDir, "sequencer", "todo"))
		if err == nil {
			for _, line := range strings.Split(string(sequence), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
					continue
				}
				switch fields[0] {
				case "pick", "p":
					result.CherryPicking = true
				case "revert":
					result.Reverting = true
				}
				break
			}
		}
	}
	return result
}
