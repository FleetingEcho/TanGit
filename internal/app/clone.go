package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
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
	return a.cloneURLError == "" && a.cloneDestinationError == "" && a.cloneBranchError == ""
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
	options, err := repository.NormalizeCloneOptions(repository.CloneOptions{
		URL: a.cloneURL, Destination: a.cloneDestination, Branch: a.cloneBranch,
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
							ui.Text(c, tr("Copy an HTTPS or SSH repository, then open it in TanGit.")).
								TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
						})
						iconButton(c, "Close clone form", "Escape", closeIcon, false, a.closeCloneForm)
					})
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
						if ui.Button(c, tr("Choose parent folder…")).
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
					ui.Text(c, tr("Cancellation stops Git. TanGit never deletes the destination folder.")).
						FontSize(theme.FontSize - 1).TextColor(theme.TextMuted)
					ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
						if ui.Button(c, tr("Cancel")).Tooltip(tr("Cancel (Escape)")).Clicked() {
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
			if ui.Button(c, tr("Cancel")).Tooltip(tr("Cancel cloning (Escape); the destination is not deleted")).Clicked() {
				a.cancelCloneOperation()
			}
		})
	})
}
