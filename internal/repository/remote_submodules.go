package repository

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	pathpkg "path"
	"regexp"
	"strings"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

var (
	submoduleSectionPattern = regexp.MustCompile(`^\s*\[submodule "(.*)"\]`)
	submodulePathPattern    = regexp.MustCompile(`^\s*path\s*=\s*(.*)\s*`)
	submoduleURLPattern     = regexp.MustCompile(`^\s*url\s*=\s*(.*)\s*`)
)

// submoduleConfigs is the remote-safe counterpart of Lazygit's .gitmodules
// loader. Its local implementation uses os.Open/os.Lstat directly, which must
// never inspect a same-named path on the GUI machine for an SSH repository.
func (s *gitSession) submoduleConfigs(parent *models.SubmoduleConfig) ([]*models.SubmoduleConfig, error) {
	if s.sshClient == nil {
		return s.git.Submodule.GetConfigs(parent)
	}
	dir := s.git.RepoPaths.WorktreePath()
	if parent != nil {
		dir = s.joinPath(dir, parent.FullPath())
	}
	name := s.joinPath(dir, ".gitmodules")
	content, err := s.fileService().ReadFile(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	parsed, err := parseSessionGitmodules(content, parent)
	if err != nil {
		return nil, err
	}
	configs := make([]*models.SubmoduleConfig, 0, len(parsed))
	for _, config := range parsed {
		if suspiciousSessionSubmoduleName(config.Name) || s.suspiciousRemoteSubmodulePath(dir, config.Path) {
			continue
		}
		configs = append(configs, config)
		nested, nestedErr := s.submoduleConfigs(config)
		if nestedErr == nil {
			configs = append(configs, nested...)
		}
	}
	return configs, nil
}

func parseSessionGitmodules(content []byte, parent *models.SubmoduleConfig) ([]*models.SubmoduleConfig, error) {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	configs := []*models.SubmoduleConfig{}
	for scanner.Scan() {
		line := scanner.Text()
		if match := submoduleSectionPattern.FindStringSubmatch(line); len(match) != 0 {
			configs = append(configs, &models.SubmoduleConfig{Name: match[1], ParentModule: parent})
			continue
		}
		if len(configs) == 0 {
			continue
		}
		config := configs[len(configs)-1]
		if match := submodulePathPattern.FindStringSubmatch(line); len(match) != 0 {
			config.Path = match[1]
		} else if match := submoduleURLPattern.FindStringSubmatch(line); len(match) != 0 {
			config.Url = match[1]
		}
	}
	return configs, scanner.Err()
}

func suspiciousSessionSubmoduleName(name string) bool {
	if name == "" {
		return true
	}
	for _, component := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if component == ".." {
			return true
		}
	}
	return false
}

func (s *gitSession) suspiciousRemoteSubmodulePath(parentDir, value string) bool {
	clean := pathpkg.Clean(value)
	if clean == "." || pathpkg.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return true
	}
	dir := parentDir
	for _, component := range strings.Split(pathpkg.Clean(value), "/") {
		dir = s.joinPath(dir, component)
		info, err := s.fileService().Lstat(dir)
		if os.IsNotExist(err) {
			return false
		}
		if err != nil || !info.IsDir() {
			return true
		}
	}
	return false
}

func (s *gitSession) deleteSubmodule(config *models.SubmoduleConfig) error {
	if s.sshClient == nil {
		return s.git.Submodule.Delete(config)
	}
	gitDir := s.remoteSubmoduleGitDir(config)
	modulesDir := s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), "modules")
	cleanModules := pathpkg.Clean(modulesDir) + "/"
	cleanGitDir := pathpkg.Clean(gitDir)
	if cleanGitDir == pathpkg.Clean(modulesDir) || !strings.HasPrefix(cleanGitDir, cleanModules) || strings.Contains(strings.TrimPrefix(cleanGitDir, cleanModules), "../") {
		return fmt.Errorf("refusing to delete %s because it is not inside %s", gitDir, modulesDir)
	}
	dir := s.git.RepoPaths.WorktreePath()
	if config.ParentModule != nil {
		dir = s.joinPath(dir, config.ParentModule.FullPath())
	}
	run := func(args ...string) error {
		argv := []string{"git", "-C", dir}
		argv = append(argv, args...)
		_, err := s.cmd.New(argv).RunWithOutput()
		return err
	}
	if err := run("submodule", "deinit", "--force", "--", config.Path); err != nil {
		if !strings.Contains(err.Error(), "did not match any file(s) known to git") {
			return err
		}
		if err := run("config", "--file", ".gitmodules", "--remove-section", "submodule."+config.Path); err != nil {
			return err
		}
		if err := run("config", "--remove-section", "submodule."+config.Path); err != nil {
			return err
		}
	}
	// Like Lazygit, an already-absent working directory does not prevent the
	// private Git metadata from being removed.
	_ = run("rm", "--force", "-r", "--", config.Path)
	return s.fileService().RemoveAll(gitDir)
}

func (s *gitSession) remoteSubmoduleGitDir(config *models.SubmoduleConfig) string {
	if config.ParentModule != nil {
		return s.joinPath(s.remoteSubmoduleGitDir(config.ParentModule), "modules", config.Name)
	}
	return s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), "modules", config.Name)
}
