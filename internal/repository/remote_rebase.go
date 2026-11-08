package repository

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"github.com/stefanhaller/git-todo-parser/todo"

	"lazygitapp/internal/sshworkspace"
)

type remoteRebaseOptions struct {
	autosquash bool
	message    []byte
	transform  func([]todo.Todo) ([]todo.Todo, error)
}

// interactiveRebase keeps Lazygit's local behavior and replaces its embedded
// local executable editor for SSH repositories.
func (s *gitSession) interactiveRebase(commits []*models.Commit, start, end int, action todo.TodoCommand, flag string) error {
	if s.sshClient == nil {
		return s.git.Rebase.InteractiveRebase(commits, start, end, action, flag)
	}
	baseIndex := end + 1
	if action == todo.Squash || action == todo.Fixup {
		baseIndex++
	}
	changes := make(map[string]bool)
	for _, commit := range commits[start : end+1] {
		if !commit.IsMerge() {
			changes[commit.Hash()] = true
		}
	}
	return s.runRemoteRebase(sessionRebaseBase(commits, baseIndex), remoteRebaseOptions{
		transform: func(todos []todo.Todo) ([]todo.Todo, error) {
			return changeSessionTodoActions(todos, changes, action, flag)
		},
	})
}

func (s *gitSession) moveCommits(commits []*models.Commit, start, end, offset int) error {
	if s.sshClient == nil {
		if offset < 0 {
			return s.git.Rebase.MoveCommitsUp(commits, start, end)
		}
		return s.git.Rebase.MoveCommitsDown(commits, start, end)
	}
	baseIndex := end + 1
	if offset > 0 {
		baseIndex += offset
	}
	hashes := make([]string, 0, end-start+1)
	for _, commit := range commits[start : end+1] {
		hashes = append(hashes, commit.Hash())
	}
	return s.runRemoteRebase(sessionRebaseBase(commits, baseIndex), remoteRebaseOptions{
		transform: func(todos []todo.Todo) ([]todo.Todo, error) {
			return moveSessionTodos(todos, hashes, offset)
		},
	})
}

func (s *gitSession) rewordCommit(commits []*models.Commit, index int, summary, description string) error {
	if s.sshClient == nil {
		return s.git.Rebase.RewordCommit(commits, index, summary, description)
	}
	message := summary
	if description != "" {
		message += "\n\n" + description
	}
	message += "\n"
	return s.runRemoteRebase(sessionRebaseBase(commits, index+1), remoteRebaseOptions{
		message: []byte(message),
		transform: func(todos []todo.Todo) ([]todo.Todo, error) {
			return changeSessionTodoActions(todos, map[string]bool{commits[index].Hash(): true}, todo.Reword, "")
		},
	})
}

func (s *gitSession) dropMergeCommit(commits []*models.Commit, index int) error {
	if s.sshClient == nil {
		return s.git.Rebase.DropMergeCommit(commits, index)
	}
	hash := commits[index].Hash()
	return s.runRemoteRebase(sessionRebaseBase(commits, index+1), remoteRebaseOptions{
		transform: func(todos []todo.Todo) ([]todo.Todo, error) {
			for i, item := range todos {
				if item.Command == todo.Merge && item.Flag == "-C" && sessionSameHash(item.Commit, hash) {
					return slices.Delete(todos, i, i+1), nil
				}
			}
			return nil, fmt.Errorf("Expected exactly one merge commit with hash %s", hash)
		},
	})
}

func (s *gitSession) amendToCommit(commits []*models.Commit, index int) error {
	if s.sshClient == nil {
		return s.git.Rebase.AmendTo(commits, index)
	}
	target := commits[index].Hash()
	if err := s.git.Commit.CreateFixupCommit(target); err != nil {
		return err
	}
	fixup, err := s.gitText("rev-parse", "--verify", "HEAD")
	if err != nil {
		return err
	}
	fixup = strings.TrimSpace(fixup)
	return s.runRemoteRebase(sessionRebaseBase(commits, index+1), remoteRebaseOptions{
		transform: func(todos []todo.Todo) ([]todo.Todo, error) {
			return moveSessionFixup(todos, target, fixup, true)
		},
	})
}

func (s *gitSession) applyFixupCommits(commit *models.Commit) error {
	if s.sshClient == nil {
		return s.git.Rebase.SquashAllAboveFixupCommits(commit)
	}
	base := commit.Hash() + "^"
	if commit.IsFirstCommit() {
		base = "--root"
	}
	return s.runRemoteRebase(base, remoteRebaseOptions{autosquash: true})
}

func sessionRebaseBase(commits []*models.Commit, index int) string {
	if index < len(commits) {
		return commits[index].Hash()
	}
	return "--root"
}

func sessionSameHash(a, b string) bool {
	common := min(len(a), len(b))
	return common > 0 && a[:common] == b[:common]
}

func changeSessionTodoActions(todos []todo.Todo, hashes map[string]bool, action todo.TodoCommand, flag string) ([]todo.Todo, error) {
	matched := 0
	for i := range todos {
		for hash := range hashes {
			if sessionSameHash(todos[i].Commit, hash) {
				todos[i].Command = action
				todos[i].Flag = flag
				matched++
				break
			}
		}
	}
	if matched != len(hashes) {
		return nil, errors.New("Some todos not found in git-rebase-todo")
	}
	return todos, nil
}

func findSessionTodo(todos []todo.Todo, hash string) (int, bool) {
	for i, item := range todos {
		if sessionSameHash(item.Commit, hash) {
			return i, true
		}
	}
	return 0, false
}

func moveSessionTodos(todos []todo.Todo, hashes []string, offset int) ([]todo.Todo, error) {
	for range max(offset, -offset) {
		selected := make(map[string]bool, len(hashes))
		for _, hash := range hashes {
			selected[hash] = true
		}
		var indices []int
		for i, item := range todos {
			for hash := range selected {
				if sessionSameHash(item.Commit, hash) {
					indices = append(indices, i)
					delete(selected, hash)
					break
				}
			}
		}
		if len(selected) != 0 {
			return nil, errors.New("Some todos not found in git-rebase-todo")
		}
		slices.Sort(indices)
		first, last := indices[0], indices[len(indices)-1]
		if offset > 0 {
			destination := -1
			for i := first - 1; i >= 0; i-- {
				if todos[i].Commit != "" {
					destination = i
					break
				}
			}
			if destination < 0 {
				return nil, errors.New("Destination position for moving todo is out of range")
			}
			item := todos[destination]
			todos = slices.Delete(todos, destination, destination+1)
			todos = slices.Insert(todos, last, item)
		} else {
			destination := -1
			for i := last + 1; i < len(todos); i++ {
				if todos[i].Commit != "" {
					destination = i
					break
				}
			}
			if destination < 0 {
				return nil, errors.New("Destination position for moving todo is out of range")
			}
			item := todos[destination]
			todos = slices.Delete(todos, destination, destination+1)
			todos = slices.Insert(todos, first, item)
		}
	}
	return todos, nil
}

func moveSessionFixup(todos []todo.Todo, originalHash, fixupHash string, changeToFixup bool) ([]todo.Todo, error) {
	original, fixup := -1, -1
	for i, item := range todos {
		if (item.Command == todo.Pick || item.Command == todo.Merge) && sessionSameHash(item.Commit, originalHash) {
			if original >= 0 {
				return nil, errors.New("Expected exactly one original hash")
			}
			original = i
		}
		if item.Command == todo.Pick && sessionSameHash(item.Commit, fixupHash) {
			if fixup >= 0 {
				return nil, errors.New("Expected exactly one fixup hash")
			}
			fixup = i
		}
	}
	if original < 0 || fixup < 0 {
		return nil, errors.New("Original or fixup commit not found in git-rebase-todo")
	}
	item := todos[fixup]
	todos = slices.Delete(todos, fixup, fixup+1)
	if fixup < original {
		original--
	}
	insert := original + 1
	todos = slices.Insert(todos, insert, item)
	if changeToFixup {
		todos[insert].Command = todo.Fixup
	}
	return todos, nil
}

// runRemoteRebase never asks Git to invoke TanGit or Lazygit. When a todo must
// be changed, a capture-only rebase generates Git's exact rebase-merges plan,
// copies it to a temporary file, and aborts before applying any picks. The
// transformed plan is then installed by a one-off POSIX shell editor.
func (s *gitSession) runRemoteRebase(base string, options remoteRebaseOptions) error {
	args := []string{"git", "rebase", "--interactive", "--autostash", "--keep-empty"}
	if options.autosquash {
		args = append(args, "--autosquash")
	} else {
		args = append(args, "--no-autosquash")
	}
	args = append(args, "--rebase-merges", base)
	editor := ":"
	if options.message != nil {
		name, cleanup, err := s.remoteRebaseEditor(options.message)
		if err != nil {
			return err
		}
		defer cleanup()
		editor = name
	}
	if options.transform == nil {
		command := s.cmd.New(args).AddEnvVars(remoteRebaseEnvironment(editor, ":")...)
		return command.Run()
	}

	todos, err := s.captureRemoteRebaseTodo(args)
	if err != nil {
		return err
	}
	todos, err = options.transform(todos)
	if err != nil {
		return err
	}
	var plan bytes.Buffer
	if err := todo.Write(&plan, todos, s.git.Config.GetCoreCommentChar()); err != nil {
		return err
	}
	planName, removePlan, err := s.temporaryFile("tangit-rebase-plan-", plan.Bytes(), 0o600)
	if err != nil {
		return err
	}
	defer removePlan()
	script := "#!/bin/sh\nset -eu\ncat " + sshworkspace.ShellQuote(planName) + " > \"$1\"\n"
	editorName, removeEditor, err := s.temporaryFile("tangit-rebase-sequence-editor-", []byte(script), 0o700)
	if err != nil {
		return err
	}
	defer removeEditor()
	return s.cmd.New(args).AddEnvVars(remoteRebaseEnvironment(editor, editorName)...).Run()
}

func remoteRebaseEnvironment(editor, sequenceEditor string) []string {
	return []string{
		"LANG=C", "LC_ALL=C", "LC_MESSAGES=C",
		"GIT_EDITOR=" + editor,
		"GIT_SEQUENCE_EDITOR=" + sequenceEditor,
	}
}

func (s *gitSession) remoteRebaseEditor(message []byte) (string, func(), error) {
	messageName, removeMessage, err := s.temporaryFile("tangit-rebase-message-", message, 0o600)
	if err != nil {
		return "", nil, err
	}
	script := "#!/bin/sh\nset -eu\ncat " + sshworkspace.ShellQuote(messageName) + " > \"$1\"\n"
	name, removeEditor, err := s.temporaryFile("tangit-rebase-message-editor-", []byte(script), 0o700)
	if err != nil {
		removeMessage()
		return "", nil, err
	}
	return name, func() { removeEditor(); removeMessage() }, nil
}

func (s *gitSession) captureRemoteRebaseTodo(args []string) ([]todo.Todo, error) {
	captureName, removeCapture, err := s.temporaryFile("tangit-rebase-capture-", nil, 0o600)
	if err != nil {
		return nil, err
	}
	defer removeCapture()
	script := "#!/bin/sh\ncp \"$1\" " + sshworkspace.ShellQuote(captureName) + "\nexit 1\n"
	editorName, removeEditor, err := s.temporaryFile("tangit-rebase-capture-editor-", []byte(script), 0o700)
	if err != nil {
		return nil, err
	}
	defer removeEditor()
	_, captureErr := s.cmd.New(args).AddEnvVars(remoteRebaseEnvironment(":", editorName)...).RunWithOutput()
	content, readErr := s.fileService().ReadFile(captureName)
	if readErr != nil || len(content) == 0 {
		if captureErr == nil {
			captureErr = errors.New("Git did not generate a rebase todo")
		}
		return nil, captureErr
	}
	// A sequence-editor failure leaves the rebase metadata available for
	// --abort on supported Git versions. On versions which clean it up
	// themselves, a failed abort is harmless if no operation remains.
	_, abortErr := s.cmd.New([]string{"git", "rebase", "--abort"}).AddEnvVars("GIT_EDITOR=:").RunWithOutput()
	if abortErr != nil && s.workingTreeState().Rebasing {
		return nil, errors.Join(captureErr, abortErr)
	}
	if captureErr == nil {
		return nil, errors.New("capture-only remote rebase unexpectedly succeeded")
	}
	todos, err := todo.Parse(bytes.NewReader(content), s.git.Config.GetCoreCommentChar())
	if err != nil {
		return nil, fmt.Errorf("parse remote rebase todo: %w", err)
	}
	return todos, nil
}
