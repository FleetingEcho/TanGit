package repository

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/git_commands"
	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

// statusFiles keeps Lazygit's local status behavior while avoiding its
// directory and conflict-marker filesystem probes for SSH sessions.
func (s *gitSession) statusFiles(options git_commands.GetStatusFileOptions) ([]*models.File, error) {
	if s.sshClient == nil {
		return s.git.Loaders.FileLoader.GetStatusFiles(options), nil
	}
	return s.remoteStatusFiles(options)
}

func (s *gitSession) remoteStatusFiles(options git_commands.GetStatusFileOptions) ([]*models.File, error) {
	args := []string{"git", "status", "--untracked-files=all", "--porcelain", "-z"}
	if options.NoRenames {
		args = append(args, "--no-renames")
	} else {
		args = append(args, fmt.Sprintf("--find-renames=%d%%", s.renameSimilarityThreshold))
	}
	stdout, stderr, err := s.cmd.New(withoutLFSFilters(args)).RunWithOutputs()
	if isOptionalFilterStatsError(err) {
		configured, configErr := s.filterDisablingArgs()
		if configErr == nil {
			retryArgs := append([]string{args[0]}, configured...)
			retryArgs = append(retryArgs, args[1:]...)
			stdout, stderr, err = s.cmd.New(retryArgs).RunWithOutputs()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("read remote status: %w: %s", err, strings.TrimSpace(stdout+stderr))
	}
	files, err := parseRemoteStatusFiles(stdout)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.HasInlineMergeConflicts {
			file.ConflictMarkerSize = s.remoteConflictMarkerSize(file.Path)
		}
	}
	return files, nil
}

// filterDisablingArgs overrides every configured process/clean/smudge filter
// only for the child Git command that reads optional metadata. It does not
// write repository or user configuration. Keys are sorted for deterministic
// command lines and existing filters are disabled as required=false as well.
func (s *gitSession) filterDisablingArgs() ([]string, error) {
	output, err := s.cmd.New([]string{"git", "config", "--get-regexp", `^filter\..*\.(process|clean|smudge|required)$`}).RunWithOutput()
	if err != nil {
		if code, ok := commandExitCode(err); !ok || code != 1 {
			return nil, err
		}
	}
	drivers := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		key, _, ok := strings.Cut(line, " ")
		if !ok || key == "" {
			continue
		}
		name := strings.TrimPrefix(key, "filter.")
		end := strings.LastIndex(name, ".")
		if end > 0 {
			drivers["filter."+name[:end]] = true
		}
	}
	names := make([]string, 0, len(drivers))
	for name := range drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	args := make([]string, 0, len(names)*6)
	for _, name := range names {
		args = append(args,
			"-c", name+".required=false",
			"-c", name+".process=",
			"-c", name+".clean=",
			"-c", name+".smudge=",
		)
	}
	return args, nil
}

func parseRemoteStatusFiles(output string) ([]*models.File, error) {
	records := strings.Split(output, "\x00")
	files := make([]*models.File, 0, len(records))
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		if strings.HasPrefix(record, "warning") {
			continue
		}
		if len(record) < 4 || record[2] != ' ' {
			return nil, fmt.Errorf("read remote status: malformed porcelain record %q", record)
		}
		shortStatus := record[:2]
		name, isDirectory := strings.CutSuffix(record[3:], "/")
		file := &models.File{Path: name, ShortStatus: shortStatus, DisplayString: record}
		if shortStatus[0] == 'R' || shortStatus[0] == 'C' || shortStatus[1] == 'R' || shortStatus[1] == 'C' {
			index++
			if index >= len(records) || records[index] == "" {
				return nil, fmt.Errorf("read remote status: rename %q has no previous path", name)
			}
			file.PreviousPath = records[index]
		}
		if isDirectory {
			// Core snapshots do not expose Lazygit's more specific nested-repository
			// classification. Crucially, do not probe this POSIX path on the GUI host.
			file.DirectoryKind = models.PlainDirectory
		}
		models.SetStatusFields(file, shortStatus)
		files = append(files, file)
	}
	return files, nil
}

func (s *gitSession) remoteConflictMarkerSize(name string) int {
	stdout, _, err := s.cmd.New([]string{
		"git", "check-attr", "-z", "conflict-marker-size", "--", name,
	}).RunWithOutputs()
	if err != nil {
		return 0
	}
	fields := strings.Split(strings.TrimSuffix(stdout, "\x00"), "\x00")
	if len(fields) != 3 {
		return 0
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 {
		return 0
	}
	return size
}
