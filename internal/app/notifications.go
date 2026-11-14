package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// appToast is a notification queued by an action. Actions run while the
// window is neither building nor holding a context, so they queue the toast
// and the next frame adds it to MyGo's list, which owns its time and place.
type appToast struct {
	id          string
	kind        string
	title       string
	description string
	action      string
	onAction    func()
	moreActions []appToastAction
	timeout     time.Duration
}

// appToastAction adds a secondary action, such as Details beside Reveal,
// without hiding it in a notification's descriptive text.
type appToastAction struct {
	label    string
	onAction func()
}

// notifyToast queues a rich notification. A nonempty id replaces the visible
// notification in place, which keeps progress updates from accumulating.
// Descriptions must already be presentation-ready: translate known static text
// with tr/trf at the source, and leave dynamic paths and error payloads raw.
func (a *app) notifyToast(toast appToast) {
	toast.title = tr(toast.title)
	toast.description = shorten(repository.RedactSecrets(toast.description), 320)
	toast.action = tr(toast.action)
	for index := range toast.moreActions {
		toast.moreActions[index].label = tr(toast.moreActions[index].label)
	}
	a.pendingToasts = append(a.pendingToasts, toast)
}

// notify queues a toast as the result of what the user just did. kind picks
// the color and icon: "success", "error", or anything else for an aside.
// Descriptions stay as written: they may be Git, server, OS, or user payloads,
// which must never be offered to the translation catalog as message keys.
func (a *app) notify(kind, title, description string) {
	a.pendingToasts = append(a.pendingToasts, appToast{
		kind:        kind,
		title:       tr(title),
		description: shorten(repository.RedactSecrets(description), 220),
		timeout:     4 * time.Second,
	})
}

// notifyTranslated is the explicit boundary for a static, app-authored
// description. Formatted descriptions should use trf at their call site so
// only the known format string is translated, not its dynamic arguments.
func (a *app) notifyTranslated(kind, title, description string) {
	a.notify(kind, title, tr(description))
}

// notifyError reports a failed action, with the way to the command log that
// holds the command and the rest of its output. A known recovery becomes the
// primary action instead of leaving the user to find it in Git's output.
func (a *app) notifyError(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, repository.ErrPullRequestMissing) {
		a.notifyPullRequestMissing()
		return
	}
	if errors.Is(err, repository.ErrPullRequestSetup) {
		tool := "gh"
		if strings.Contains(strings.ToLower(err.Error()), "glab") {
			tool = "glab"
		}
		a.notifyToast(appToast{
			kind:        "error",
			title:       trf("Set up %s for pull requests", tool),
			description: trf("Install %s or sign in with '%s auth login', then retry. The CLI output is in the command log.", tool, tool),
			action:      "View log",
			onAction:    a.openGitCommandLog,
			timeout:     12 * time.Second,
		})
		return
	}
	if errors.Is(err, repository.ErrPullDiverged) {
		a.notifyToast(appToast{
			kind:        "error",
			title:       "Choose how to pull",
			description: tr("Your local and remote branches have diverged. Merge preserves both histories; rebase replays your local commits on top of the remote tip."),
			action:      "Merge",
			onAction:    func() { a.confirmPullStrategy(repository.PullMerge) },
			moreActions: []appToastAction{
				{label: "Rebase", onAction: func() { a.confirmPullStrategy(repository.PullRebase) }},
				{label: "View log", onAction: a.openGitCommandLog},
			},
			timeout: 12 * time.Second,
		})
		return
	}
	if errors.Is(err, repository.ErrPushRejected) {
		a.notifyToast(appToast{
			kind:        "error",
			title:       "Pull first, then push",
			description: tr("The remote branch has changes you do not have locally. Pull and resolve any conflicts, then push again."),
			action:      "Pull now",
			onAction:    a.confirmPull,
			moreActions: []appToastAction{{label: "View log", onAction: a.openGitCommandLog}},
			timeout:     12 * time.Second,
		})
		return
	}
	a.pendingToasts = append(a.pendingToasts, appToast{
		kind:        "error",
		title:       tr("Git action failed"),
		description: shorten(safeError(err), 220),
		action:      tr("View log"),
		onAction:    a.openGitCommandLog,
		timeout:     8 * time.Second,
	})
}

// safeError is the presentation boundary for errors from Git and the OS.
// Keeping it beside the notification boundary makes it difficult for a new
// pane or action to accidentally display a credential-bearing remote URL.
func safeError(err error) string {
	if err == nil {
		return ""
	}
	return repository.RedactSecrets(err.Error())
}

// notifyCopied writes text to the clipboard and says so, so a copy is never
// silent: what was copied is named, and the value itself shows for the ones
// that are hard to see (a hash, a branch name).
func (a *app) notifyCopied(services ui.Services, what, text string) {
	services.WriteClipboard(text)
	a.notify("success", tr("Copied ")+tr(what), text)
}

// shorten keeps a toast description to one readable line or two.
func shorten(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

// flushToasts turns the notifications actions queued into MyGo toasts, at the
// end of the build pass that has a context to add them with.
func (a *app) flushToasts(c *ui.Context) {
	for _, pending := range a.pendingToasts {
		a.toastSeq++
		id := pending.id
		if id == "" {
			id = fmt.Sprintf("app-toast-%d", a.toastSeq)
		}
		c.AddToast(ui.Toast{
			ID:          id,
			Title:       pending.title,
			Description: pending.description,
			Type:        pending.kind,
			Timeout:     pending.timeout,
			Action:      pending.action,
			OnAction:    pending.onAction,
			Data:        pending.moreActions,
		})
	}
	a.pendingToasts = nil
}

// toastViewport gives the notifications the app's look: a card at the top
// right, below the window's own chrome, colored by what it reports, with a
// close control and the action the notification offers, such as opening the
// command log after a failure. The newest sits highest, where a new one
// always shows in the same place, clear of the work the eye was reading.
func (a *app) toastViewport(c *ui.Context, topInset float32) {
	theme := c.Theme()
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(topInset+16, 16, 16, 16).
			AlignItems(ui.End).Justify(ui.Start).Gap(8)
		for i := len(toasts) - 1; i >= 0; i-- {
			toast := toasts[i]
			parts := ui.ToastBase(c, toast)
			icon, color := toastLook(theme, toast.Type)
			parts.Root.Row().AlignItems(ui.Start).Gap(10).Padding(10, 12).
				Radius(theme.Radius+2).MaxWidth(400).
				Background(theme.Surface).Border(1, color.Alpha(0.55)).
				Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.22))
			parts.Root.Children(func() {
				ui.Icon(c, icon).FontSize(15).TextColor(color).Padding(1, 0)
				ui.Column(c).Gap(3).Grow(1).Children(func() {
					ui.Text(c, toast.Title).Bold()
					if toast.Description != "" {
						ui.Text(c, toast.Description).TextColor(theme.TextMuted).
							FontSize(theme.FontSize - 1).MaxWidth(320)
					}
					extraActions, _ := toast.Data.([]appToastAction)
					if toast.Action != "" || len(extraActions) > 0 {
						ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
							if toast.Action != "" {
								action := ui.ButtonBase(c).Label(toast.Action).Padding(2, 0).Gap(4).
									AlignSelf(ui.Start)
								action.TextColor(color)
								action.Children(func() {
									ui.Text(c, toast.Action).FontSize(theme.FontSize - 1).Bold()
								})
								if action.Clicked() {
									if toast.OnAction != nil {
										toast.OnAction()
									}
									c.CloseToast(toast.ID)
								}
							}
							if actions, ok := toast.Data.([]appToastAction); ok {
								for _, extra := range actions {
									action := ui.ButtonBase(c).Label(extra.label).Padding(2, 0).AlignSelf(ui.Start)
									action.TextColor(color)
									action.Children(func() {
										ui.Text(c, extra.label).FontSize(theme.FontSize - 1).Bold()
									})
									if action.Clicked() {
										if extra.onAction != nil {
											extra.onAction()
										}
										c.CloseToast(toast.ID)
									}
								}
							}
						})
					}
				})
				dismiss := ui.ButtonBase(c).Label(tr("Dismiss notification")).Padding(2)
				dismiss.Children(func() {
					ui.Icon(c, closeIcon).FontSize(12).TextColor(theme.TextMuted)
				})
				if dismiss.Clicked() {
					c.CloseToast(toast.ID)
				}
			})
		}
	})
}

// toastLook picks the icon and color a notification's kind shows with.
func toastLook(theme *ui.Theme, kind string) (*ui.SVG, ui.Color) {
	switch kind {
	case "error":
		return conflictIcon, theme.Danger
	case "success":
		return commitIcon, theme.Success
	default:
		return appIcon, theme.Accent
	}
}

// notifyPullRequestMissing keeps a non-existent PR distinct from a missing
// or unauthenticated CLI and gives the user a useful next action.
func (a *app) notifyPullRequestMissing() {
	a.notifyToast(appToast{
		kind:        "info",
		title:       tr("No pull request for this branch"),
		description: tr("Create one to start a review and see its checks."),
		action:      "Create pull request",
		onAction:    a.openCreatePullRequestForm,
		moreActions: []appToastAction{{label: "View log", onAction: a.openGitCommandLog}},
		timeout:     10 * time.Second,
	})
}
