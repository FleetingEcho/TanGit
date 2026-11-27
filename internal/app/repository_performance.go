package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// performanceUIState is the Repository performance dialog. desired holds the
// checkboxes; report is the last read of the repository's real settings.
type performanceUIState struct {
	open     bool
	loading  bool
	applying bool
	report   *repository.RepositoryPerformance
	desired  repository.PerformanceSettings
	err      string
	token    uint64
	// suggested remembers the repositories already offered the settings in
	// this window, so reopening one does not repeat the notice.
	suggested map[string]bool
	// statusRead is how long the first snapshot, whose working-tree status is
	// the slow part in a big repository, took to read when this one was opened.
	statusRead time.Duration
}

// performanceSlowStatus is how long that first read must take before a repository
// is offered the settings: below it, the repository is not slow however many
// files it holds, and the notice would only be noise.
const performanceSlowStatus = 2 * time.Second

// performanceHintMuted says whether the user asked not to be reminded about
// this repository. Only its own recent entry is consulted.
func (a *app) performanceHintMuted(path string) bool {
	want := cleanRepoPath(path)
	for _, repo := range a.recentRepositories {
		if !repo.EditorOnly && cleanRecentPath(repo.Path) == want {
			return repo.PerformanceHintOff
		}
	}
	return false
}

// mutePerformanceHint stops the reminder for this repository alone and keeps
// the choice in its recent entry. A repository with no entry has nothing to keep.
func (a *app) mutePerformanceHint(path string) {
	want := cleanRepoPath(path)
	for i := range a.recentRepositories {
		if !a.recentRepositories[i].EditorOnly && cleanRecentPath(a.recentRepositories[i].Path) == want {
			a.recentRepositories[i].PerformanceHintOff = true
			a.historyRevision++
			_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
			return
		}
	}
}

func (s *performanceUIState) reset() {
	s.token++
	s.open = false
	s.loading = false
	s.applying = false
	s.report = nil
	s.err = ""
}

func (a *app) performanceSession() (repository.PerformanceSession, bool) {
	if a.session == nil {
		return nil, false
	}
	session, ok := a.session.(repository.PerformanceSession)
	return session, ok
}

func (a *app) openRepositoryPerformance() {
	if a.session == nil || a.performance.applying {
		return
	}
	a.performance.open = true
	a.performance.err = ""
	a.requestRepositoryPerformance(true)
}

func (a *app) closeRepositoryPerformance() {
	if a.performance.applying {
		return
	}
	a.performance.reset()
}

// requestRepositoryPerformance reads the settings in the background. With
// resetChoices the checkboxes start from the recommended state.
func (a *app) requestRepositoryPerformance(resetChoices bool) {
	session, ok := a.performanceSession()
	if !ok {
		a.performance.err = tr("This repository does not support performance settings.")
		return
	}
	generation := a.generation
	a.performance.token++
	token := a.performance.token
	a.performance.loading = true
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		report, err := session.Performance(ctx)
		a.updateChanged(func() bool {
			if generation != a.generation || token != a.performance.token {
				return false
			}
			a.performance.loading = false
			if err != nil {
				a.performance.err = safeError(err)
				return a.performance.open
			}
			a.performance.report = &report
			if resetChoices {
				a.performance.desired = report.Recommended()
			}
			return a.performance.open
		})
	}()
}

func (a *app) applyRepositoryPerformance() {
	report := a.performance.report
	if report == nil || a.performance.loading || a.performance.applying || a.performance.desired == report.Settings {
		return
	}
	session, ok := a.performanceSession()
	if !ok {
		return
	}
	desired := a.performance.desired
	generation := a.generation
	a.performance.token++
	token := a.performance.token
	a.performance.applying = true
	a.performance.err = ""
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		err := session.ApplyPerformance(ctx, desired)
		updated, readErr := session.Performance(ctx)
		a.updateChanged(func() bool {
			if generation != a.generation || token != a.performance.token {
				return false
			}
			a.performance.applying = false
			if readErr == nil {
				a.performance.report = &updated
				a.performance.desired = updated.Settings
			}
			if err != nil {
				a.performance.err = safeError(err)
				return true
			}
			if readErr != nil {
				a.performance.err = safeError(readErr)
				return true
			}
			a.notifyTranslated("success", "Repository performance", "Performance settings were updated.")
			a.performance.reset()
			return true
		})
	}()
}

// suggestRepositoryPerformance offers the settings once per repository and
// window, and only for a large repository that is missing some of them.
func (a *app) suggestRepositoryPerformance() {
	session, ok := a.performanceSession()
	if !ok || a.performance.suggested[a.path] {
		return
	}
	// Only a repository that really was slow to read is worth a notice, and only
	// while the user has not turned it off for this one.
	if a.performance.statusRead < performanceSlowStatus || a.performanceHintMuted(a.path) {
		return
	}
	if a.performance.suggested == nil {
		a.performance.suggested = map[string]bool{}
	}
	a.performance.suggested[a.path] = true
	generation := a.generation
	path := a.path
	ctx := a.repositoryWork.Context()
	go func() {
		defer a.recoverBackground()
		report, err := session.Performance(ctx)
		if err != nil || !report.Large() || report.Optimized() {
			return
		}
		a.updateChanged(func() bool {
			if generation != a.generation {
				return false
			}
			a.notifyToast(appToast{
				id:    "repository-performance",
				kind:  "info",
				title: "Large repository",
				description: trf("%d tracked files. Reading this repository's status was slow. Git's file system monitor, untracked cache and commit-graph can make status and history much faster.",
					report.TrackedFiles),
				action:   "Optimize…",
				onAction: a.openRepositoryPerformance,
				moreActions: []appToastAction{{label: "Don't remind me for this repository", onAction: func() {
					a.mutePerformanceHint(path)
				}}},
				timeout: 12 * time.Second,
			})
			return true
		})
	}()
}

type performanceOption struct {
	label       string
	description string
	value       *bool
	current     bool
	available   bool
	unavailable string
}

func (a *app) repositoryPerformanceModal(c *ui.Context) {
	if !a.performance.open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		c.Key("repository-performance-modal")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		back.Children(func() {
			panel := ui.Column(c).Width(640).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(20).Gap(12).Background(theme.Background).Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Repository performance")).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, lineChartIcon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Repository performance")).Bold().FontSize(17)
						ui.Text(c, tr("Git settings that keep status, history and fetch fast in large repositories.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close repository performance", "Escape", closeIcon, a.performance.applying, a.closeRepositoryPerformance)
				})
				a.repositoryPerformanceBody(c)
				ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
					if ui.Button(c, tr("Cancel")).Disabled(a.performance.applying).Clicked() {
						a.closeRepositoryPerformance()
					}
					report := a.performance.report
					unchanged := report == nil || a.performance.desired == report.Settings
					label := tr("Apply")
					if a.performance.applying {
						label = tr("Applying…")
					}
					if ui.PrimaryButton(c, label).
						Disabled(unchanged || a.performance.loading || a.performance.applying).Clicked() {
						a.applyRepositoryPerformance()
					}
				})
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeRepositoryPerformance()
			}
		})
	})
}

func (a *app) repositoryPerformanceBody(c *ui.Context) {
	theme := c.Theme()
	report := a.performance.report
	if report == nil {
		if a.performance.loading {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Label(tr("Reading performance settings"))
				ui.Text(c, tr("Reading performance settings…")).TextColor(theme.TextMuted)
			})
		}
		stackSyncMessageView(c, a.performance.err, true)
		return
	}
	summary := trf("%d tracked files · Git %s", report.TrackedFiles, report.GitVersion)
	if report.Large() {
		summary += " · " + tr("large repository")
	}
	ui.Text(c, summary).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1).Selectable()
	options := []performanceOption{
		{
			label:       "File system monitor",
			description: "Git's built-in daemon (core.fsmonitor) tells status which files changed, instead of checking every file.",
			value:       &a.performance.desired.FSMonitor, current: report.Settings.FSMonitor,
			available:   report.FSMonitorSupported || report.Settings.FSMonitor,
			unavailable: "Not available: this Git has no built-in monitor (Linux, SSH workspaces or Git before 2.37).",
		},
		{
			label:       "Untracked cache",
			description: "Remembers folders without new files (core.untrackedCache), so status skips them.",
			value:       &a.performance.desired.UntrackedCache, current: report.Settings.UntrackedCache, available: true,
		},
		{
			label:       "Commit-graph",
			description: "Writes a commit-graph with changed-path filters and keeps it current on fetch. Speeds up the graph, file history and blame.",
			value:       &a.performance.desired.CommitGraph, current: report.Settings.CommitGraph, available: true,
		},
		{
			label:       "Background maintenance",
			description: "Registers this repository with git maintenance start: hourly prefetch and commit-graph, daily loose objects and packs. Uses your system scheduler.",
			value:       &a.performance.desired.Maintenance, current: report.Settings.Maintenance,
			available:   report.MaintenanceSupported || report.Settings.Maintenance,
			unavailable: "Not available for SSH workspaces.",
		},
	}
	ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
		ui.Column(c).Gap(8).FillWidth().Children(func() {
			for _, option := range options {
				performanceOptionView(c, option, a.performance.loading || a.performance.applying)
			}
		})
	})
	stackSyncMessageView(c, a.performance.err, true)
}

func performanceOptionView(c *ui.Context, option performanceOption, busy bool) {
	theme := c.Theme()
	c.Key("performance-option:" + option.label)
	ui.Column(c).FillWidth().Gap(4).Padding(10, 12).Border(1, theme.Border).Radius(theme.Radius).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Checkbox(c, option.value, tr(option.label)).Disabled(busy || !option.available)
			ui.Box(c).Grow(1)
			status := tr("Off")
			if option.current {
				status = tr("On")
			}
			ui.Badge(c, status)
		})
		description := option.description
		if !option.available {
			description = option.unavailable
		}
		ui.Text(c, tr(description)).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
	})
}
