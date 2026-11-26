package repository

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// LargeRepositoryFiles is the tracked-file count from which lazygitGo
// suggests the performance settings. Below it, plain Git is already fast.
const LargeRepositoryFiles = 20000

// PerformanceSession reads and changes the Git settings that make status,
// history and fetch fast in large repositories. Every setting is ordinary
// repository configuration that plain Git and other clients honor too.
type PerformanceSession interface {
	Performance(context.Context) (RepositoryPerformance, error)
	ApplyPerformance(context.Context, PerformanceSettings) error
}

// PerformanceSettings is the desired state of each optional setting.
type PerformanceSettings struct {
	// FSMonitor uses Git's built-in file system monitor daemon, so status
	// asks the operating system what changed instead of scanning every file.
	FSMonitor bool
	// UntrackedCache remembers directories without new files between runs.
	UntrackedCache bool
	// CommitGraph writes a commit-graph with changed-path Bloom filters and
	// keeps it current on fetch. History, graph, blame and file history use it.
	CommitGraph bool
	// Maintenance registers the repository for Git's scheduled background
	// maintenance (git maintenance start): prefetch, commit-graph, loose
	// objects and pack files.
	Maintenance bool
}

// RepositoryPerformance is a read of the repository's current settings.
type RepositoryPerformance struct {
	TrackedFiles int
	GitVersion   string
	// FSMonitorSupported is false where Git has no built-in daemon, such as
	// Linux and SSH workspaces, or Git older than 2.37.
	FSMonitorSupported bool
	// MaintenanceSupported is false for SSH workspaces, where scheduling
	// would install a timer on the remote host.
	MaintenanceSupported bool
	Settings             PerformanceSettings
}

// Large reports whether the repository is big enough for the settings to
// make a noticeable difference.
func (p RepositoryPerformance) Large() bool {
	return p.TrackedFiles >= LargeRepositoryFiles
}

// Recommended is the current settings with every supported repository
// setting turned on. Background maintenance installs a system timer, so it
// stays as it is: an explicit choice, never a default.
func (p RepositoryPerformance) Recommended() PerformanceSettings {
	return PerformanceSettings{
		FSMonitor:      p.FSMonitorSupported || p.Settings.FSMonitor,
		UntrackedCache: true,
		CommitGraph:    true,
		Maintenance:    p.Settings.Maintenance,
	}
}

// Optimized reports whether every recommended setting is already on.
func (p RepositoryPerformance) Optimized() bool {
	return p.Settings == p.Recommended()
}

func (s *gitSession) Performance(ctx context.Context) (RepositoryPerformance, error) {
	if err := ctx.Err(); err != nil {
		return RepositoryPerformance{}, err
	}
	var report RepositoryPerformance
	report.GitVersion = strings.TrimPrefix(s.gitOutputOrEmpty("version"), "git version ")
	report.TrackedFiles = s.indexEntryCount()
	report.Settings.FSMonitor = s.configBool("core.fsmonitor")
	report.Settings.UntrackedCache = s.configBool("core.untrackedCache")
	report.Settings.CommitGraph = s.configBool("fetch.writeCommitGraph") && s.commitGraphPresent()
	report.FSMonitorSupported = s.sshClient == nil && s.fsmonitorDaemonSupported()
	report.MaintenanceSupported = s.sshClient == nil
	report.Settings.Maintenance = s.maintenanceRegistered()
	if err := ctx.Err(); err != nil {
		return RepositoryPerformance{}, err
	}
	return report, nil
}

func (s *gitSession) ApplyPerformance(ctx context.Context, desired PerformanceSettings) error {
	current, err := s.Performance(ctx)
	if err != nil {
		return err
	}
	if desired.FSMonitor && !current.Settings.FSMonitor && !current.FSMonitorSupported {
		return fmt.Errorf("this Git does not include the built-in file system monitor")
	}
	if desired.Maintenance && !current.Settings.Maintenance && !current.MaintenanceSupported {
		return fmt.Errorf("background maintenance is not available for SSH workspaces")
	}
	steps := []struct {
		name    string
		want    bool
		have    bool
		enable  [][]string
		disable [][]string
	}{
		{"file system monitor", desired.FSMonitor, current.Settings.FSMonitor,
			[][]string{{"config", "core.fsmonitor", "true"}},
			[][]string{{"config", "core.fsmonitor", "false"}, {"fsmonitor--daemon", "stop"}}},
		{"untracked cache", desired.UntrackedCache, current.Settings.UntrackedCache,
			[][]string{{"config", "core.untrackedCache", "true"}},
			[][]string{{"config", "core.untrackedCache", "false"}}},
		{"commit-graph", desired.CommitGraph, current.Settings.CommitGraph,
			[][]string{{"config", "fetch.writeCommitGraph", "true"}, {"commit-graph", "write", "--reachable", "--changed-paths"}},
			[][]string{{"config", "fetch.writeCommitGraph", "false"}}},
		{"background maintenance", desired.Maintenance, current.Settings.Maintenance,
			[][]string{{"maintenance", "start"}},
			[][]string{{"maintenance", "unregister"}}},
	}
	for _, step := range steps {
		if step.want == step.have {
			continue
		}
		commands := step.enable
		if !step.want {
			commands = step.disable
		}
		for index, args := range commands {
			if err := ctx.Err(); err != nil {
				return err
			}
			output, runErr := s.cmd.New(append([]string{"git"}, args...)).RunWithOutput()
			if runErr == nil {
				continue
			}
			// Turning a setting off is complete once its local configuration
			// says false, which also overrides a global value. Stopping a
			// daemon that is not running is not a failure worth reporting.
			if !step.want && index > 0 {
				continue
			}
			return fmt.Errorf("%s: git %s: %w: %s", step.name, strings.Join(args, " "), runErr, strings.TrimSpace(output))
		}
	}
	return nil
}

func (s *gitSession) gitOutputOrEmpty(args ...string) string {
	output, err := s.gitText(args...)
	if err != nil {
		return ""
	}
	return output
}

func (s *gitSession) configBool(key string) bool {
	return s.gitOutputOrEmpty("config", "--type=bool", "--get", key) == "true"
}

// indexEntryCount reads the entry count from the index header instead of
// listing every path: the header is twelve bytes however large the index is.
func (s *gitSession) indexEntryCount() int {
	file, err := s.fileService().Open(s.joinPath(s.git.RepoPaths.WorktreeGitDirPath(), "index"))
	if err != nil {
		return 0
	}
	defer file.Close()
	return parseIndexEntryCount(file)
}

func parseIndexEntryCount(reader io.Reader) int {
	var header [12]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil || string(header[:4]) != "DIRC" {
		return 0
	}
	return int(binary.BigEndian.Uint32(header[8:12]))
}

func (s *gitSession) commitGraphPresent() bool {
	info := s.joinPath(s.git.RepoPaths.RepoGitDirPath(), "objects", "info")
	for _, name := range []string{"commit-graph", s.joinPath("commit-graphs", "commit-graph-chain")} {
		if _, err := s.fileService().Stat(s.joinPath(info, name)); err == nil {
			return true
		}
	}
	return false
}

// fsmonitorDaemonSupported asks Git itself. A Git built without the daemon
// says so; one that has it reports whether it is watching this worktree.
func (s *gitSession) fsmonitorDaemonSupported() bool {
	output, err := s.cmd.New([]string{"git", "fsmonitor--daemon", "status"}).RunWithOutput()
	if err == nil {
		return true
	}
	lower := strings.ToLower(output)
	return strings.Contains(lower, "not watching") || strings.Contains(lower, "is watching")
}

// maintenanceRegistered compares the global maintenance.repo list with this
// repository, after resolving symbolic links such as macOS's /tmp.
func (s *gitSession) maintenanceRegistered() bool {
	output := s.gitOutputOrEmpty("config", "--global", "--get-all", "maintenance.repo")
	if output == "" {
		return false
	}
	targets := map[string]bool{}
	for _, path := range []string{s.git.RepoPaths.RepoPath(), s.git.RepoPaths.WorktreePath()} {
		if path != "" {
			targets[canonicalMaintenancePath(path)] = true
		}
	}
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" && targets[canonicalMaintenancePath(line)] {
			return true
		}
	}
	return false
}

func canonicalMaintenancePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}
