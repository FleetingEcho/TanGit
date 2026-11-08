package app

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

// homeWorktreePickerState owns an on-demand read of one repository from Home.
// Nothing scans every recent repository: the user explicitly chooses a source
// before a session is opened, and the token keeps a closed/replaced picker from
// accepting an older result.
type homeWorktreePickerState struct {
	open      bool
	dropdown  bool
	menuOpen  bool
	source    string
	location  workspace.Location
	worktrees []repository.Worktree
	loading   bool
	err       string
	token     uint64
}

func (a *app) openHomeWorktrees(source string) {
	source = strings.TrimSpace(source)
	if source == "" || a.opener == nil {
		return
	}
	state := &a.homeWorktrees
	state.token++
	token, generation := state.token, a.generation
	state.open = true
	state.dropdown = false
	state.source = source
	state.location = workspace.Location{}
	state.worktrees = nil
	state.loading = true
	state.err = ""
	go func() {
		defer a.recoverBackground()
		session, err := a.opener.Open(context.Background(), source)
		var snapshot repository.Snapshot
		var location workspace.Location
		if err == nil {
			snapshot, err = session.Snapshot(context.Background())
			location = openedLocation(session, snapshot, source)
			closeRepositorySession(session)
		}
		a.update(func() {
			if token != a.homeWorktrees.token || generation != a.generation || !a.homeWorktrees.open ||
				(a.homeWorktrees.dropdown && !a.homeWorktrees.menuOpen) {
				return
			}
			a.homeWorktrees.loading = false
			if err != nil {
				if a.homeWorktrees.dropdown && errors.Is(err, os.ErrNotExist) {
					a.homeWorktrees.err = tr("Repository folder no longer exists")
					return
				}
				a.homeWorktrees.err = safeError(err)
				return
			}
			a.homeWorktrees.location = location
			a.homeWorktrees.worktrees = append([]repository.Worktree(nil), snapshot.Worktrees...)
		})
	}()
}

func (a *app) closeHomeWorktrees() {
	a.homeWorktrees.open = false
	a.homeWorktrees.dropdown = false
	a.homeWorktrees.menuOpen = false
	a.homeWorktrees.token++
}

// hasOpenableLinkedWorktree distinguishes a useful quick-open menu from a
// repository whose records contain only its main or unavailable worktrees.
func hasOpenableLinkedWorktree(worktrees []repository.Worktree) bool {
	for _, worktree := range worktrees {
		if !worktree.Main && !worktree.Missing && strings.TrimSpace(worktree.Path) != "" {
			return true
		}
	}
	return false
}

// homeWorktreeMenu fills a recent card's compact dropdown. Its first build
// starts the read; later frames replace the busy item with the results while
// the menu stays open. Dismissing and reopening it reads the repository again.
func (a *app) homeWorktreeMenu(m *ui.Menu, source string) {
	state := &a.homeWorktrees
	reopen := !state.menuOpen || !state.dropdown || state.source != source
	state.dropdown = true
	// The first open is temporarily uncontrolled so sibling cards cannot steal
	// the shared binding. Record that their owner's menu is already opening.
	state.menuOpen = true
	if reopen {
		a.openHomeWorktrees(source)
		state.dropdown = true
	}
	m.MaxWidth(460)
	switch {
	case state.loading:
		m.Item(tr("Opening repository")).Disabled(true)
		return
	case state.err != "":
		m.Item(state.err).Disabled(true).Tooltip(state.err)
		return
	case !hasOpenableLinkedWorktree(state.worktrees):
		m.Item(tr("No linked worktrees")).Disabled(true)
		return
	}
	for _, worktree := range state.worktrees {
		worktree := worktree
		name := worktree.Name
		if strings.TrimSpace(name) == "" {
			name = repoName(worktree.Path)
		}
		label := name
		if worktree.Main {
			label += " · " + tr("Main worktree")
		} else if worktree.Branch != "" {
			label += " · " + worktree.Branch
		}
		if worktree.Missing {
			label += " · " + tr("Missing")
		}
		path := a.homeWorktreePath(worktree)
		if m.Item(label).ID(path).Tooltip(path).Disabled(worktree.Missing || path == "").Chosen() {
			a.openHomeWorktree(worktree)
		}
	}
}

func (a *app) homeWorktreePath(worktree repository.Worktree) string {
	location := a.homeWorktrees.location
	location.Root = strings.TrimSpace(worktree.Path)
	if location.Valid() {
		return location.String()
	}
	return strings.TrimSpace(worktree.Path)
}

func (a *app) openHomeWorktree(worktree repository.Worktree) {
	path := a.homeWorktreePath(worktree)
	if path == "" || worktree.Missing {
		return
	}
	a.closeHomeWorktrees()
	a.openRecent(path)
}

// homeWorktreesModal opens an existing main or linked worktree without first
// replacing Home with its source repository. Remote paths retain their profile.
func (a *app) homeWorktreesModal(c *ui.Context) {
	state := &a.homeWorktrees
	if state.dropdown {
		return
	}
	a.repositoryFormModal(c, &state.open, 720, "Open worktree",
		"Choose a main or linked worktree to open", worktreesIcon, func() {
			ui.Text(c, state.source).FontSize(c.Theme().FontSize - 1).
				TextColor(c.Theme().TextMuted).SingleLine().Tooltip(state.source)
			switch {
			case state.loading:
				ui.Row(c).AlignItems(ui.Center).Gap(10).Padding(18).Children(func() {
					ui.Spinner(c).Label(tr("Opening repository"))
					ui.Text(c, tr("Opening repository")).TextColor(c.Theme().TextMuted)
				})
			case state.err != "":
				ui.Text(c, state.err).TextColor(c.Theme().Danger).Selectable()
			case len(state.worktrees) == 0:
				ui.Text(c, tr("No worktrees found")).TextColor(c.Theme().TextMuted)
			default:
				ui.Scroll(c).Height(300).Children(func() {
					ui.Column(c).FillWidth().Gap(6).Children(func() {
						for _, worktree := range state.worktrees {
							worktree := worktree
							row := ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(10).
								Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(c.Theme().Radius)
							row.Children(func() {
								ui.Icon(c, worktreesIcon).FontSize(18).TextColor(c.Theme().Accent)
								ui.Column(c).Gap(2).Grow(1).Children(func() {
									ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
										name := worktree.Name
										if strings.TrimSpace(name) == "" {
											name = repoName(worktree.Path)
										}
										ui.Text(c, name).Bold().SingleLine()
										if worktree.Main {
											ui.Badge(c, tr("Main worktree"))
										}
										if worktree.Branch != "" {
											ui.Badge(c, worktree.Branch)
										}
										if worktree.Missing {
											ui.Badge(c, tr("Missing"))
										}
									})
									ui.Text(c, worktree.Path).FontSize(c.Theme().FontSize - 2).
										TextColor(c.Theme().TextMuted).SingleLine().Tooltip(worktree.Path)
								})
								button := ui.PrimaryButton(c, tr("Open")).Label(tr("Open worktree") + " " + worktree.Path).
									Disabled(worktree.Missing || strings.TrimSpace(worktree.Path) == "")
								if button.Clicked() {
									a.openHomeWorktree(worktree)
								}
							})
						}
					})
				})
			}
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.Button(c, tr("Cancel")).Clicked() {
					a.closeHomeWorktrees()
				}
			})
		})
}
