package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// repositoryCloner is deliberately optional. Existing opener fakes implement
// only repositoryOpener and continue to work; a real Opener also clones.
type repositoryCloner interface {
	Clone(context.Context, repository.CloneOptions, func(repository.CloneProgress)) error
}

// chooseCloneParent is the native directory chooser. Tests answer it without
// opening a desktop dialog.
var chooseCloneParent = func(options mygo.OpenDialogOptions) ([]string, error) {
	return mygo.Dialog.Open(options)
}

func cloneErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return tr(repository.RedactSecrets(err.Error()))
}

func (a *app) resetCloneValidation() {
	a.cloneValidationShown = false
	a.cloneURLError = ""
	a.cloneBranchError = ""
	a.cloneDestinationError = ""
	a.cloneDepthError = ""
	a.cloneSparseError = ""
}

// cloneModeLabels are the clone form's mode choices, in CloneMode order.
var cloneModeLabels = []string{
	"Full clone",
	"Blobless: full history, files on demand",
	"Treeless: commits only, fastest",
	"Shallow: recent commits only",
}

var cloneModeHints = []string{
	"Downloads every commit and file. Best for small and medium repositories.",
	"Recommended for large repositories. History, blame and log stay complete; file contents download when Git first needs them.",
	"For builds and one-off checkouts. Browsing old history downloads trees on demand and can be slow.",
	"Downloads only the newest commits. Older history, blame and merge bases are unavailable until you fetch more.",
}

func cloneModeOptions() []string {
	options := make([]string, len(cloneModeLabels))
	for index, label := range cloneModeLabels {
		options[index] = tr(label)
	}
	return options
}

// cloneMode maps the translated choice back to the repository mode. An
// unknown label, for example after a language change, is a full clone.
func (a *app) cloneMode() repository.CloneMode {
	for index, label := range cloneModeLabels {
		if a.cloneModeChoice == tr(label) {
			return repository.CloneMode(index)
		}
	}
	return repository.CloneFull
}

func (a *app) validateCloneDepthField() (int, bool) {
	a.cloneDepthError = ""
	if a.cloneMode() != repository.CloneShallow {
		return 0, true
	}
	raw := strings.TrimSpace(a.cloneDepth)
	if raw == "" {
		return 1, true
	}
	depth, err := strconv.Atoi(raw)
	if err != nil || depth <= 0 || depth > 1<<20 {
		a.cloneDepthError = tr("Enter a positive number of commits.")
		return 0, false
	}
	return depth, true
}

func (a *app) validateCloneSparseField() ([]string, bool) {
	paths, err := repository.ParseSparsePaths(a.cloneSparse)
	a.cloneSparseError = cloneErrorMessage(err)
	return paths, err == nil
}

func (a *app) validateCloneURLField() {
	if strings.TrimSpace(a.cloneURL) == "" {
		a.cloneURLError = tr("Remote repository URL is required.")
		return
	}
	a.cloneURLError = cloneErrorMessage(repository.ValidateCloneURL(a.cloneURL))
}

func (a *app) validateCloneDestinationField() {
	if strings.TrimSpace(a.cloneDestination) == "" {
		a.cloneDestinationError = tr("Destination folder is required.")
		return
	}
	a.cloneDestinationError = cloneErrorMessage(repository.ValidateCloneDestination(a.cloneDestination))
}

func (a *app) validateCloneBranchField() {
	a.cloneBranchError = cloneErrorMessage(repository.ValidateCloneBranch(a.cloneBranch))
}

func (a *app) validateCloneFields() bool {
	a.cloneValidationShown = true
	a.validateCloneURLField()
	a.validateCloneDestinationField()
	a.validateCloneBranchField()
	_, depthOK := a.validateCloneDepthField()
	_, sparseOK := a.validateCloneSparseField()
	return a.cloneURLError == "" && a.cloneDestinationError == "" && a.cloneBranchError == "" && depthOK && sparseOK
}

func (a *app) cloneFieldChanged() {
	a.cloneError = ""
	if a.cloneValidationShown {
		a.validateCloneFields()
	}
}

func (a *app) openCloneForm() {
	if a.state != stateEmpty && a.state != stateError {
		return
	}
	a.cloneGeneration++
	a.cloneOpen = true
	a.cloneURL = ""
	a.cloneBranch = ""
	a.cloneDestination = ""
	a.cloneDestinationManual = false
	a.cloneModeChoice = tr(cloneModeLabels[repository.CloneFull])
	a.cloneDepth = "1"
	a.cloneSingleBranch = false
	a.cloneSparse = ""
	a.resetCloneValidation()
	a.cloneError = ""
	a.cloneProgress = repository.CloneProgress{}
	a.offerInitPath = ""
	a.errMessage = ""
	if a.cloneParent == "" {
		a.cloneParent, _ = os.UserHomeDir()
	}
}

func (a *app) closeCloneForm() {
	a.cloneGeneration++
	a.cloneOpen = false
	a.resetCloneValidation()
	a.cloneError = ""
	a.cloneProgress = repository.CloneProgress{}
}

func (a *app) cancelCloneOperation() {
	if a.state != stateCloning {
		return
	}
	if a.cancelClone != nil {
		a.cancelClone()
		a.cancelClone = nil
	}
	a.cloneGeneration++
	a.state = stateEmpty
	a.cloneOpen = false
	a.resetCloneValidation()
	a.cloneError = ""
	a.cloneProgress = repository.CloneProgress{}
}

func (a *app) updateCloneDestination() {
	if a.cloneDestinationManual {
		return
	}
	name, err := repository.CloneRepositoryName(a.cloneURL)
	if err != nil {
		return
	}
	parent := a.cloneParent
	if parent == "" && a.cloneDestination != "" {
		parent = filepath.Dir(a.cloneDestination)
	}
	if parent == "" {
		a.cloneDestination = name
	} else {
		a.cloneDestination = filepath.Join(parent, name)
	}
}

// browseCloneParent chooses only the parent. The repository name remains
// derived from the remote unless the user edits the full destination field.
func (a *app) browseCloneParent() {
	if !a.cloneOpen || a.state != stateEmpty && a.state != stateError {
		return
	}
	a.cloneGeneration++
	generation := a.cloneGeneration
	repositoryGeneration := a.generation
	parentWindow := a.window
	defaultPath := a.cloneParent
	if a.cloneDestination != "" {
		defaultPath = filepath.Dir(a.cloneDestination)
	}
	remote := a.cloneURL
	go func() {
		defer a.recoverBackground()
		paths, err := chooseCloneParent(mygo.OpenDialogOptions{
			Parent:            parentWindow,
			Title:             tr("Choose clone parent folder"),
			DefaultPath:       defaultPath,
			ButtonLabel:       tr("Choose folder"),
			Directory:         true,
			CreateDirectories: true,
		})
		a.updateChanged(func() bool {
			if generation != a.cloneGeneration || repositoryGeneration != a.generation ||
				!a.cloneOpen || a.state == stateCloning {
				return false
			}
			if err != nil {
				a.cloneError = cloneErrorMessage(err)
				return true
			}
			if len(paths) == 0 {
				return false
			}
			name, nameErr := repository.CloneRepositoryName(remote)
			if nameErr != nil {
				a.cloneValidationShown = true
				a.cloneURLError = cloneErrorMessage(nameErr)
				a.cloneError = ""
				return true
			}
			a.cloneParent = paths[0]
			a.cloneDestinationManual = false
			a.cloneDestination = filepath.Join(paths[0], name)
			a.cloneFieldChanged()
			return true
		})
	}()
}

func (a *app) beginClone() {
	if !a.cloneOpen || a.state == stateCloning {
		return
	}
	a.cloneError = ""
	if !a.validateCloneFields() {
		return
	}
	depth, _ := a.validateCloneDepthField()
	sparse, _ := a.validateCloneSparseField()
	options, err := repository.NormalizeCloneOptions(repository.CloneOptions{
		URL: a.cloneURL, Destination: a.cloneDestination, Branch: a.cloneBranch,
		Mode: a.cloneMode(), Depth: depth, SingleBranch: a.cloneSingleBranch, SparsePaths: sparse,
	})
	if err != nil {
		a.cloneError = cloneErrorMessage(err)
		return
	}
	cloner, ok := a.opener.(repositoryCloner)
	if !ok {
		a.cloneError = tr("Cloning is unavailable in this window.")
		return
	}

	a.cloneGeneration++
	generation := a.cloneGeneration
	repositoryGeneration := a.generation
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelClone = cancel
	a.cloneOpen = false
	a.cloneError = ""
	a.cloneProgress = repository.CloneProgress{}
	a.state = stateCloning
	go func() {
		defer a.recoverBackground()
		err := cloner.Clone(ctx, options, func(progress repository.CloneProgress) {
			a.updateChanged(func() bool {
				if generation != a.cloneGeneration || repositoryGeneration != a.generation || a.state != stateCloning {
					return false
				}
				progress.Detail = repository.RedactSecrets(progress.Detail)
				if a.cloneProgress == progress {
					return false
				}
				a.cloneProgress = progress
				return true
			})
		})
		a.updateChanged(func() bool {
			if generation != a.cloneGeneration || repositoryGeneration != a.generation || a.state != stateCloning {
				return false
			}
			a.cancelClone = nil
			if err != nil && errors.Is(err, repository.ErrCloneSparseCheckout) {
				// The repository exists; open it and keep the reason visible
				// rather than offering a retry that would hit a non-empty folder.
				a.state = stateEmpty
				a.path = options.Destination
				a.beginOpen()
				a.notify("warning", "Sparse checkout was not applied", repository.RedactSecrets(err.Error()))
				return true
			}
			if err != nil {
				a.state = stateEmpty
				a.cloneOpen = true
				a.cloneError = cloneErrorMessage(err)
				return true
			}
			// A clone only produces the path. The ordinary opener remains the
			// single owner of snapshots, recents, tabs and progressive loading.
			a.state = stateEmpty
			a.path = options.Destination
			a.beginOpen()
			return true
		})
	}()
}

// cloneModal is the editable preflight form. During the operation the Home
// page switches to cloneProgressView, whose Cancel controls real cancellation.
func (a *app) cloneModal(c *ui.Context) {
	if !a.cloneOpen || a.state == stateCloning {
		return
	}
	theme := c.Theme()
	ui.Overlay(c, func() {
		c.Key("clone-remote-modal")
		back := ui.Row(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Justify(ui.Center).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.35)).Modal()
		back.Children(func() {
			panel := ui.Column(c).Width(620).MaxWidthPercent(94).MaxHeightPercent(94).
				Padding(22).Gap(14).Background(theme.Background).
				Border(1, theme.Border).Radius(theme.Radius + 4).
				Role(ui.RoleDialog).Label(tr("Clone remote repository")).
				Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
						ui.Box(c).Size(34, 34).Radius(9).Center().
							Background(theme.Accent.Alpha(0.14)).Children(func() {
							ui.Icon(c, downloadIcon).FontSize(18).TextColor(theme.Accent)
						})
						ui.Column(c).Gap(2).Grow(1).Children(func() {
							ui.Text(c, tr("Clone remote repository")).FontSize(18).Bold()
							ui.Text(c, tr("Copy an HTTPS or SSH repository, then open it in lazygitGo.")).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close clone form", "Escape", closeIcon, false, a.closeCloneForm)
					})
					// The fields scroll so the actions stay reachable in short windows.
					ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
						ui.Column(c).Gap(14).FillWidth().Children(func() {
							ui.Text(c, tr("Use HTTPS, SSH, or scp-like syntax. URLs containing credentials or tokens are rejected; Git uses your credential helper or SSH agent.")).
								FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
							ui.Field(c, tr("Remote repository URL")+" *", func() {
								remote := ui.TextInput(c, &a.cloneURL).
									Label(tr("Remote repository URL")).
									Placeholder(tr("https://example.com/owner/repository.git or git@example.com:owner/repository.git")).
									AutoFocus().FillWidth()
								if remote.Changed() {
									a.updateCloneDestination()
									a.cloneFieldChanged()
								}
								if remote.Submitted() {
									a.beginClone()
								}
							}).Error(a.cloneURLError)
							ui.Field(c, tr("Branch (optional)"), func() {
								branch := ui.TextInput(c, &a.cloneBranch).
									Label(tr("Branch (optional)")).
									Placeholder(tr("Default branch")).FillWidth()
								if branch.Changed() {
									a.cloneFieldChanged()
								}
								if branch.Submitted() {
									a.beginClone()
								}
							}).Error(a.cloneBranchError)
							a.cloneSpeedOptions(c)
							ui.Field(c, tr("Destination folder")+" *", func() {
								destination := ui.TextInput(c, &a.cloneDestination).
									Label(tr("Destination folder")).
									Placeholder(tr("Parent folder / repository name")).FillWidth()
								if destination.Changed() {
									a.cloneDestinationManual = true
									a.cloneFieldChanged()
								}
								if destination.Submitted() {
									a.beginClone()
								}
							}).Error(a.cloneDestinationError)
							ui.Row(c).Justify(ui.End).Children(func() {
								if ui.Button(c, tr("Choose parent folder")).
									Tooltip(tr("Choose a parent folder; the repository name comes from the remote URL")).Clicked() {
									a.browseCloneParent()
								}
							})
							if a.cloneError != "" {
								ui.Column(c).Gap(5).Padding(10).Background(theme.Danger.Alpha(0.1)).
									Border(1, theme.Danger.Alpha(0.45)).Radius(theme.Radius).Children(func() {
									ui.Text(c, tr("Could not clone repository")).Bold().TextColor(theme.Danger)
									ui.Text(c, a.cloneError).Selectable().FontSize(theme.FontSize - 1)
								})
							}
							ui.Text(c, tr("Cancellation stops Git. lazygitGo never deletes the destination folder.")).
								FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
						})
					})
					ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
						if cloneCancel(c, a, tr("Cancel"), "", "Esc") {
							a.closeCloneForm()
						}
						if ui.PrimaryButton(c, tr("Clone and open")).Tooltip(tr("Clone and open (C opens this form)")).Clicked() {
							a.beginClone()
						}
					})
				})
			if back.OverlayShortcut(0, ui.KeyF1) {
				a.openKeybindings()
			}
			if !c.FocusTakesText() && back.OverlayShortcut(ui.Shift, ui.KeySlash) {
				a.openCurrentKeybindings()
			}
			if panel.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.closeCloneForm()
			}
		})
	})
}

// cloneProgressView shows only parsed Git progress, never synthetic counts or
// timer-based percentages. Unknown phases retain an indeterminate spinner.
func (a *app) cloneProgressView(c *ui.Context) {
	theme := c.Theme()
	ui.Box(c).Fill().Background(theme.Background).Children(func() {
		a.homeBackgroundView(c)
		ui.Column(c).Fill().Center().Gap(16).Padding(28).Children(func() {
			ui.Box(c).Size(64, 64).Radius(18).Center().
				Background(theme.Accent.Alpha(0.16)).Children(func() {
				ui.Icon(c, downloadIcon).FontSize(30).TextColor(theme.Accent)
			})
			ui.Text(c, tr("Cloning repository")).FontSize(22).Bold()
			redactedURL := repository.RedactSecrets(strings.TrimSpace(a.cloneURL))
			ui.Text(c, redactedURL).
				TextColor(theme.TextMuted).MaxWidth(660).TextAlign(ui.Center).SingleLine().Tooltip(redactedURL)
			ui.Text(c, a.cloneDestination).TextColor(theme.TextMuted).
				MaxWidth(660).TextAlign(ui.Center).SingleLine().Tooltip(a.cloneDestination)
			progress := a.cloneProgress
			if progress.Phase != "" {
				phase := tr(progress.Phase)
				if progress.Total > 0 {
					phase = trf("%s · %d/%d", phase, progress.Current, progress.Total)
				}
				ui.Text(c, phase).Bold()
			}
			if progress.Known {
				ui.Progress(c, progress.Fraction).Label(tr("Clone progress")).Width(440).MaxWidthPercent(90)
				ui.Text(c, trf("%d%%", progress.Percent)).TextColor(theme.TextMuted)
			} else {
				ui.Spinner(c).Label(tr("Cloning repository"))
				ui.Text(c, tr("Waiting for Git progress…")).TextColor(theme.TextMuted)
			}
			if cloneCancel(c, a, tr("Cancel cloning"), tr("The destination is not deleted."), "Esc") {
				a.cancelCloneOperation()
			}
		})
	})
}

// cloneSpeedOptions chooses how much history and how many files the clone
// downloads. Each choice maps to one documented git clone option.
func (a *app) cloneSpeedOptions(c *ui.Context) {
	theme := c.Theme()
	mode := a.cloneMode()
	ui.Row(c).AlignItems(ui.End).Gap(12).Wrap().Children(func() {
		before := a.cloneModeChoice
		ui.Field(c, tr("Clone mode"), func() {
			ui.Select(c, &a.cloneModeChoice, cloneModeOptions()).Label(tr("Clone mode")).Width(330)
		})
		if before != a.cloneModeChoice {
			a.cloneFieldChanged()
			mode = a.cloneMode()
		}
		if mode == repository.CloneShallow {
			ui.Field(c, tr("Commits to fetch"), func() {
				depth := ui.TextInput(c, &a.cloneDepth).Label(tr("Commits to fetch")).Placeholder("1").Width(130)
				if depth.Changed() {
					a.cloneFieldChanged()
				}
				if depth.Submitted() {
					a.beginClone()
				}
			}).Error(a.cloneDepthError)
		}
	})
	ui.Text(c, tr(cloneModeHints[mode])).FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
	before := a.cloneSingleBranch
	ui.Checkbox(c, &a.cloneSingleBranch, tr("Fetch only the cloned branch")).
		Tooltip(tr("Skips other branches and their history (--single-branch)"))
	if before != a.cloneSingleBranch {
		a.cloneFieldChanged()
	}
	ui.Field(c, tr("Sparse checkout folders (optional)"), func() {
		sparse := ui.TextInput(c, &a.cloneSparse).Label(tr("Sparse checkout folders (optional)")).
			Placeholder(tr("app, docs/api — empty checks out everything")).FillWidth()
		if sparse.Changed() {
			a.cloneFieldChanged()
		}
		if sparse.Submitted() {
			a.beginClone()
		}
	}).Error(a.cloneSparseError)
}

// cloneCancel is the clone form's Cancel button with a tooltip that names its
// shortcut and, while cloning, what Cancel leaves behind.
func cloneCancel(c *ui.Context, a *app, title, body, shortcut string) bool {
	button := ui.Button(c, tr("Cancel"))
	richTooltip(c, button, richTip{Title: title, Body: body, Shortcut: shortcut})
	return button.Clicked()
}
