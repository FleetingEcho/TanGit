package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// comparisonState owns the dialog's drafts, applied read options, and results.
// Applied values never alias the editable form: a background read keeps the
// refs and options it started with even when the user changes the next one.
type comparisonState struct {
	open    bool
	from    string
	to      string
	context string
	options repository.DiffOptions

	appliedFrom    string
	appliedTo      string
	appliedOptions repository.DiffOptions
	files          []repository.CommitFile
	filesLoading   bool
	filesError     string

	selected  int
	list      ui.ListState
	listWidth float32

	diffKey     string
	diffState   *editor.DiffState
	diffLoading bool
	diffError   string
	token       uint64
}

// openCompare opens the ref picker without disturbing the selection or diff
// underneath it. The latest commit is a useful default only when it has a
// parent; a root-only repository asks for its starting ref explicitly.
func (a *app) openCompare() {
	if a.state != stateReady || a.session == nil {
		return
	}
	a.compare.open = true
	if a.compare.context == "" {
		a.compare.context = "3"
	}
	if a.compare.from == "" && a.compare.to == "" && len(a.snapshot.Commits) > 0 {
		a.compare.to = "HEAD"
		if len(a.snapshot.Commits[0].Parents) > 0 {
			a.compare.from = "HEAD~1"
		}
	}
}

// closeCompare hides the dialog and rejects anything its reads still have in
// flight. Completed results remain so reopening the dialog is inexpensive.
func (a *app) closeCompare() {
	a.compare.open = false
	a.compare.token++
	a.compare.filesLoading = false
	a.compare.diffLoading = false
	a.compare.diffKey = ""
}

// resetComparison drops every ref and result belonging to the old repository.
func (a *app) resetComparison() {
	a.closeCompare()
	a.compare.from = ""
	a.compare.to = ""
	a.compare.context = "3"
	a.compare.options = repository.DiffOptions{ContextLines: 3}
	a.compare.appliedFrom = ""
	a.compare.appliedTo = ""
	a.compare.appliedOptions = repository.DiffOptions{}
	a.compare.files = nil
	a.compare.filesError = ""
	a.compare.selected = -1
	a.compare.diffState = nil
	a.compare.diffError = ""
}

// beginCompare applies the current drafts and starts a new file-list read.
// Changing either refs or an option invalidates both the list and file diff.
func (a *app) beginCompare() {
	if a.session == nil || !a.compare.open || a.compare.filesLoading {
		return
	}
	from, to := strings.TrimSpace(a.compare.from), strings.TrimSpace(a.compare.to)
	if from == "" || to == "" {
		a.compare.filesError = tr("Choose both refs to compare.")
		return
	}
	contextLines, err := strconv.Atoi(a.compare.context)
	if err != nil || contextLines < 0 {
		a.compare.filesError = tr("Choose a valid diff context.")
		return
	}
	options := repository.DiffOptions{
		IgnoreWhitespace: a.compare.options.IgnoreWhitespace,
		ContextLines:     contextLines,
	}
	a.compare.from, a.compare.to = from, to
	a.compare.options = options
	a.compare.appliedFrom, a.compare.appliedTo = from, to
	a.compare.appliedOptions = options
	a.compare.files = nil
	a.compare.filesLoading = true
	a.compare.filesError = ""
	a.compare.selected = -1
	a.compare.diffKey = ""
	a.compare.diffState = nil
	a.compare.diffLoading = false
	a.compare.diffError = ""
	a.compare.token++

	session := a.session
	generation := a.generation
	token := a.compare.token
	go func() {
		defer a.recoverBackground()
		files, err := session.CompareFiles(context.Background(), from, to, options)
		a.update(func() {
			if generation != a.generation || token != a.compare.token || !a.compare.open {
				return
			}
			a.compare.filesLoading = false
			if err != nil {
				a.compare.filesError = safeError(err)
				return
			}
			if files == nil {
				files = []repository.CommitFile{}
			}
			a.compare.files = files
			if len(files) > 0 {
				a.compare.selected = 0
			}
		})
	}()
}

// refreshAppliedCompare reloads after an option change only once a comparison
// has been applied; editing a fresh form does not start reads on every click.
func (a *app) refreshAppliedCompare() {
	if a.compare.appliedFrom != "" && !a.compare.filesLoading {
		a.beginCompare()
		return
	}
	if a.compare.appliedFrom != "" {
		// A new choice supersedes the read in flight. Allow the replacement.
		a.compare.filesLoading = false
		a.beginCompare()
	}
}

// swapCompare reverses the drafts and immediately reverses an applied result.
func (a *app) swapCompare() {
	a.compare.from, a.compare.to = a.compare.to, a.compare.from
	if a.compare.appliedFrom != "" {
		if a.compare.filesLoading {
			a.compare.filesLoading = false
		}
		a.beginCompare()
	}
}

// requestCompareFileDiff reads the selected file with exactly the options that
// produced its list. diffKey rejects a slower response for a previous file;
// token and generation reject responses for a previous comparison or repo.
func (a *app) requestCompareFileDiff() {
	if !a.compare.open || a.compare.filesLoading || a.compare.filesError != "" ||
		a.compare.selected < 0 || a.compare.selected >= len(a.compare.files) {
		return
	}
	file := a.compare.files[a.compare.selected]
	if a.compare.diffKey == file.Path {
		return
	}
	a.compare.diffKey = file.Path
	a.compare.diffState = nil
	a.compare.diffLoading = true
	a.compare.diffError = ""

	session := a.session
	generation := a.generation
	token := a.compare.token
	from, to := a.compare.appliedFrom, a.compare.appliedTo
	options := a.compare.appliedOptions
	layout, wrap := a.diffLayout(), a.diffWrap
	go func() {
		defer a.recoverBackground()
		diff, err := session.CompareFileDiff(context.Background(), from, to, file, options)
		var state *editor.DiffState
		if err == nil && (diff.Patch != "" || diff.OldText != "" || diff.NewText != "") {
			state, err = buildDiffState(layout, wrap, repository.File{Path: file.Path}, diff, options)
		}
		a.update(func() {
			if generation != a.generation || token != a.compare.token || !a.compare.open ||
				a.compare.diffKey != file.Path {
				return
			}
			a.compare.diffLoading = false
			if err != nil {
				a.compare.diffError = safeError(err)
				return
			}
			a.compare.diffState = state
		})
	}()
}

// compareModal is a large, read-only worksheet: refs and display options above,
// changed files on the left, and the selected file's patch on the right.
func (a *app) compareModal(c *ui.Context) {
	if !a.compare.open {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		a.registerTerminalModalShortcuts(back)
		if !a.settings.DisableShortcuts {
			back.OnShortcut(0, ui.KeyF6, func() { a.cycleKeyboardPane(c, 1) })
			back.OnShortcut(ui.Shift, ui.KeyF6, func() { a.cycleKeyboardPane(c, -1) })
		}
		back.Children(func() {
			panel := ui.Column(c).Width(1100).Height(700).MaxWidthPercent(94).MaxHeightPercent(92).
				Padding(18).Gap(12).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Compare refs"))
			panel.Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Box(c).Size(34, 34).Radius(9).Center().
						Background(theme.Accent.Alpha(0.14)).Children(func() {
						ui.Icon(c, compareIcon).FontSize(18).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(2).Grow(1).Children(func() {
						ui.Text(c, tr("Compare refs")).FontSize(17).Bold()
						ui.Text(c, tr("Review two branches, tags, or commits without changing your working tree.")).
							TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
					})
					iconButton(c, "Close comparison", "Escape", closeIcon, false, a.closeCompare)
				})

				ui.Row(c).AlignItems(ui.End).Gap(8).Children(func() {
					from := a.compareRefInput(c, "From ref", "main or HEAD~1", &a.compare.from).Grow(1)
					to := a.compareRefInput(c, "To ref", "feature or HEAD", &a.compare.to).Grow(1)
					iconButton(c, "Swap refs", "Swap the comparison direction", swapIcon,
						strings.TrimSpace(a.compare.from) == "" || strings.TrimSpace(a.compare.to) == "", a.swapCompare)
					compare := ui.PrimaryButton(c, tr("Compare")).
						Tooltip(tr("Read the changed files and their diffs")).
						Disabled(strings.TrimSpace(a.compare.from) == "" || strings.TrimSpace(a.compare.to) == "" || a.compare.filesLoading)
					if compare.Clicked() || from.Submitted() || to.Submitted() {
						a.beginCompare()
					}
				})
				ui.Row(c).AlignItems(ui.Center).Gap(12).Wrap().Children(func() {
					ignore := ui.Checkbox(c, &a.compare.options.IgnoreWhitespace, tr("Ignore whitespace")).
						Label(tr("Ignore whitespace")).Tooltip(tr("Hide whitespace-only changes inside each file"))
					ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
						ui.Text(c, tr("Context lines")).TextColor(theme.TextMuted)
						contextChoice := ui.Select(c, &a.compare.context, []string{"0", "1", "3", "5", "10", "20"}).
							Label(tr("Context lines")).Tooltip(tr("Unchanged lines shown around each change"))
						if contextChoice.Changed() {
							a.refreshAppliedCompare()
						}
					})
					if ignore.Changed() {
						a.refreshAppliedCompare()
					}
					ui.Spacer(c).Grow(1)
					a.diffLayoutButton(c)
					a.diffWrapButton(c)
				})

				if a.compare.appliedFrom != "" {
					ui.Textf(c, "%s → %s", a.compare.appliedFrom, a.compare.appliedTo).
						TextColor(theme.TextMuted).SingleLine().Tooltip(a.compare.appliedFrom + " → " + a.compare.appliedTo)
				}
				a.compareResults(c, theme)
			})
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCompare()
			}
		})
	})
}

// compareRefInput keeps revisions editable while offering known branches and
// tags. Unlike a closed combobox, an unmatched value is a valid Git revision.
func (a *app) compareRefInput(c *ui.Context, label, placeholder string, value *string) ui.Element {
	theme := c.Theme()
	combo := ui.ComboboxBase(c, value)
	input := combo.Input.Grow(1).Padding(7, 10).
		Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius).
		Label(tr(label)).Placeholder(tr(placeholder)).AutoComplete(ui.AutoCompleteList)
	combo.Popup(func(panel ui.Element) {
		panel.Padding(4).Background(theme.Surface).Border(1, theme.Border).Radius(theme.Radius)
		choices := a.compareRefChoices(*value, combo.Filtering())
		filterPopupList(c, theme, func() {
			if len(choices) == 0 {
				ui.Row(c).Padding(7, 10).Children(func() {
					ui.Text(c, tr("No matches")).TextColor(theme.TextMuted)
				})
				return
			}
			for _, ref := range choices {
				item := combo.Item(ref).Padding(7, 10)
				if item.Highlighted() {
					item.Background(theme.SurfaceHover)
				}
				item.Children(func() { ui.Text(c, ref).SingleLine().Tooltip(ref) })
			}
		})
	})
	if ref, ok := combo.Chosen(); ok {
		*value = ref
	}
	return input
}

// compareRefChoices lists familiar revisions on click and narrows them while
// the user types. Keep the list bounded for repositories with many refs.
func (a *app) compareRefChoices(typed string, filtering bool) []string {
	choices := make([]string, 0, 1+len(a.snapshot.Branches)+len(a.snapshot.Tags))
	seen := make(map[string]bool)
	add := func(ref string) {
		if ref != "" && !seen[ref] {
			seen[ref] = true
			choices = append(choices, ref)
		}
	}
	add("HEAD")
	for _, branch := range a.snapshot.Branches {
		add(branch.Name)
	}
	for _, remote := range a.snapshot.Remotes {
		for _, branch := range remote.Branches {
			name := branch.RemoteName
			if name == "" {
				name = remote.Name
			}
			if name != "" && branch.Name != "" {
				add(name + "/" + branch.Name)
			}
		}
	}
	for _, tag := range a.snapshot.Tags {
		add(tag.Name)
	}
	if filtering {
		needle := strings.ToLower(strings.TrimSpace(typed))
		if needle != "" {
			matched := choices[:0]
			for _, ref := range choices {
				if strings.Contains(strings.ToLower(ref), needle) {
					matched = append(matched, ref)
				}
			}
			choices = matched
		}
	}
	if len(choices) > 200 {
		choices = choices[:200]
	}
	return choices
}

func (a *app) compareResults(c *ui.Context, theme *ui.Theme) {
	switch {
	case a.compare.filesLoading:
		ui.Row(c).Grow(1).Center().Gap(8).Children(func() {
			ui.Spinner(c).Label(tr("Loading comparison"))
			ui.Text(c, tr("Loading comparison…")).TextColor(theme.TextMuted)
		})
		return
	case a.compare.filesError != "":
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Text(c, tr("The comparison could not be read.")).Bold()
			ui.Text(c, a.compare.filesError).TextColor(theme.Danger).TextAlign(ui.Center)
			if ui.Button(c, tr("Try again")).Clicked() {
				a.beginCompare()
			}
		})
		return
	case a.compare.files == nil:
		ui.Column(c).Grow(1).Center().Gap(6).Children(func() {
			ui.Text(c, tr("Choose two refs, then Compare.")).Bold()
			ui.Text(c, tr("Use branch names, tags, commit hashes, or revisions such as HEAD~3.")).
				TextColor(theme.TextMuted)
		})
		return
	case len(a.compare.files) == 0:
		ui.Column(c).Grow(1).Center().Children(func() {
			ui.Text(c, tr("No changes between these refs.")).TextColor(theme.TextMuted)
		})
		return
	}

	paneSplit(c, &a.compare.listWidth, func() {
		ui.Column(c).Grow(1).FillHeight().Gap(7).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
				paneTitle(c, folderIcon, "Changed files")
				ui.Spacer(c).Grow(1)
				ui.Badge(c, strconv.Itoa(len(a.compare.files)))
			})
			list := a.keyboardList(c, &a.compare.list, len(a.compare.files)).Grow(1).Rows(func(row ui.ListRow) {
				file := a.compare.files[row.Index]
				ui.Row(row.Context).Gap(8).AlignItems(ui.Center).Padding(7, 8).Children(func() {
					fileTypeMark(row.Context, file.Path, 13, row.Selected())
					ui.Text(row.Context, file.Path).Grow(1).SingleLine().Tooltip(file.Path)
					fileDiffStatCounts(row.Context, file.Path, file.LinesAdded, file.LinesDeleted)
					statusSlot(row.Context, statusLetter(file.Status), theme.Warning)
				})
			})
			if a.keyboardListChanged(&a.compare.list, list) {
				a.compare.diffKey = ""
				a.compare.diffState = nil
				a.compare.diffLoading = false
				a.compare.diffError = ""
			}
		})
	}, func() {
		ui.Column(c).Grow(1).FillHeight().Gap(7).Children(func() {
			if a.compare.selected < 0 || a.compare.selected >= len(a.compare.files) {
				ui.Column(c).Grow(1).Center().Children(func() {
					ui.Text(c, tr("Select a file to see its diff.")).TextColor(theme.TextMuted)
				})
				return
			}
			file := a.compare.files[a.compare.selected]
			ui.Text(c, file.Path).Bold().SingleLine().Tooltip(file.Path)
			a.requestCompareFileDiff()
			switch {
			case a.compare.diffLoading:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Label(tr("Loading diff"))
					ui.Text(c, tr("Loading diff…")).TextColor(theme.TextMuted)
				})
			case a.compare.diffError != "":
				ui.Text(c, a.compare.diffError).TextColor(theme.Danger)
			case a.compare.diffState != nil:
				a.keyboardDiffViewer(c, a.compare.diffState)
			default:
				ui.Column(c).Grow(1).Center().Children(func() {
					ui.Text(c, tr("No textual diff")).TextColor(theme.TextMuted)
				})
			}
		})
	}).Grow(1)
}
