package repository

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/stefanhaller/git-todo-parser/todo"
)

// remoteRebaseCommits hydrates pending history through SFTP. The upstream
// CommitLoader performs these reads with the GUI host's os package, which must
// never inspect a same-named local checkout for an SSH session.
func (s *gitSession) remoteRebaseCommits() ([]*models.Commit, error) {
	gitDir := s.git.RepoPaths.WorktreeGitDirPath()
	for _, directory := range []string{"rebase-merge", "rebase-apply"} {
		name := s.joinPath(gitDir, directory, "git-rebase-todo")
		content, err := s.fileService().ReadFile(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		todos, err := todo.Parse(bytes.NewReader(content), s.git.Config.GetCoreCommentChar())
		if err != nil {
			return nil, fmt.Errorf("parse in-progress rebase todo: %w", err)
		}
		slices.Reverse(todos) // Git stores the plan oldest-first; history is newest-first.
		commits := make([]*models.Commit, 0, len(todos))
		for _, item := range todos {
			if item.Commit == "" {
				continue
			}
			status := models.StatusRebasing
			if directory == "rebase-apply" {
				status = models.StatusApplyingPatch
			}
			commits = append(commits, models.NewCommit(s.hashPool, models.NewCommitOpts{
				Hash: item.Commit, Name: item.Msg, Status: status,
				Action: item.Command, ActionFlag: item.Flag,
			}))
		}
		return commits, nil
	}

	// A non-interactive git-am has one mail file per remaining patch instead
	// of a rebase todo. Keep its subject and remote patch identity together.
	directory := s.joinPath(gitDir, "rebase-apply")
	entries, err := s.fileService().ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var patches []os.FileInfo
	for _, entry := range entries {
		if entry.Mode().IsRegular() && len(entry.Name()) == 4 && strings.Trim(entry.Name(), "0123456789") == "" {
			patches = append(patches, entry)
		}
	}
	slices.Reverse(patches)
	commits := make([]*models.Commit, 0, len(patches))
	for _, entry := range patches {
		name := s.joinPath(directory, entry.Name())
		content, err := s.fileService().ReadFile(name)
		if err != nil {
			return nil, err
		}
		subject := entry.Name()
		for line := range strings.SplitSeq(string(content), "\n") {
			if value, ok := strings.CutPrefix(line, "Subject:"); ok {
				subject = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "[PATCH]"))
				break
			}
		}
		commits = append(commits, models.NewCommit(s.hashPool, models.NewCommitOpts{
			Name: subject, Status: models.StatusApplyingPatch,
			Action: todo.Pick, PatchFile: name,
		}))
	}
	return commits, nil
}
