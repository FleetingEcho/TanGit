package app

import (
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/forge"
)

var (
	requestOpenIcon   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4"><circle cx="8" cy="8" r="6"/><circle cx="8" cy="8" r="2" fill="currentColor"/></svg>`))
	requestClosedIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><circle cx="8" cy="8" r="6"/><path d="m5.8 5.8 4.4 4.4M10.2 5.8l-4.4 4.4"/></svg>`))
	requestDraftIcon  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><path d="M3 11.5 3.5 9l6-6 2.5 2.5-6 6z"/></svg>`))
	issueIcon         = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><circle cx="8" cy="8" r="6"/><path d="M8 4.5v4"/><circle cx="8" cy="11.5" r=".75" fill="currentColor" stroke="none"/></svg>`))
)

// pill is a rounded, tinted badge: the color names the meaning (green open,
// blue merged, red closed), with an optional glyph in front of the text.
func pill(c *ui.Context, text string, color ui.Color, icon *ui.SVG) {
	ui.Row(c).AlignItems(ui.Center).Gap(3).Padding(0, 7).Shrink(0).Radius(9).
		Background(color.Alpha(0.16)).Border(1, color.Alpha(0.5)).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).FontSize(11).TextColor(color)
		}
		ui.Text(c, text).Bold().SingleLine().FontSize(c.Theme().FontSize - 2).TextColor(color)
	})
}

func requestStateIcon(state forge.State) *ui.SVG {
	switch state {
	case forge.StateMerged:
		return mergeIcon
	case forge.StateClosed:
		return requestClosedIcon
	}
	return requestOpenIcon
}

// requestStatePill is the Open / Merged / Closed badge with its glyph.
func (a *app) requestStatePill(c *ui.Context, item forge.ChangeRequest) {
	pill(c, requestStateLabel(item), a.requestStateColor(c, item.State), requestStateIcon(item.State))
}

// labelColor gives a label the same theme color every time it is drawn.
func labelColor(theme *ui.Theme, name string) ui.Color {
	if scope, value, scoped := strings.Cut(name, "::"); scoped && scope != "" && value != "" {
		return theme.Accent
	}
	return theme.Success
}

// labelPill draws a label as GitLab does: "scope::value" is a solid scope
// beside a tinted value, any other label a single tinted pill.
func labelPill(c *ui.Context, name string) {
	color := labelColor(c.Theme(), name)
	size := c.Theme().FontSize - 2
	scope, value, scoped := strings.Cut(name, "::")
	theme := c.Theme()
	if !scoped || scope == "" || value == "" {
		// A plain label is one solid pill.
		ui.Row(c).AlignItems(ui.Center).Shrink(0).Radius(9).Padding(1, 8).Background(color).Children(func() {
			ui.Text(c, name).Bold().FontSize(size).SingleLine().TextColor(theme.AccentText)
		})
		return
	}
	// A scoped label is a tinted scope beside a plain value, in one outline.
	ui.Row(c).AlignItems(ui.Center).Shrink(0).Radius(9).Border(1, color.Alpha(0.6)).Clip().Children(func() {
		ui.Box(c).Padding(1, 7).Background(color.Alpha(0.22)).Children(func() {
			ui.Text(c, scope).Bold().FontSize(size).SingleLine().TextColor(theme.Text)
		})
		ui.Box(c).Padding(1, 7).Background(theme.Surface).Children(func() {
			ui.Text(c, value).Bold().FontSize(size).SingleLine().TextColor(theme.Text)
		})
	})
}

// decisionLabel is the wording shown for a review or merge status.
func decisionLabel(status string) string {
	if status == "Not approved" {
		return "Not approved yet"
	}
	return status
}

// requestWebBase is the repository page a request URL sits under:
// https://host/group/project/-/merge_requests/7 or https://host/owner/repo/pull/7.
func requestWebBase(requestURL string) (base string, gitlab bool) {
	for _, marker := range []string{"/-/merge_requests/", "/pull/"} {
		if i := strings.Index(requestURL, marker); i > 0 {
			return requestURL[:i], marker != "/pull/"
		}
	}
	return "", false
}

func escapePathKeepingSlashes(value string) string {
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

// requestBranchURL is the page of a branch on the hosting site. The source
// branch of a fork's request lives in the fork, not in the repository the
// request is in: its address comes from the provider, and is left empty when
// the provider did not say, rather than pointing at the wrong repository.
func requestBranchURL(item forge.ChangeRequest, branch string, source bool) string {
	base, gitlab := requestWebBase(item.URL)
	if source && item.CrossRepo {
		base = item.SourceRepoURL
	}
	if base == "" || branch == "" {
		return ""
	}
	if gitlab {
		return base + "/-/tree/" + escapePathKeepingSlashes(branch)
	}
	return base + "/tree/" + escapePathKeepingSlashes(branch)
}

func requestCommitURL(item forge.ChangeRequest, sha string) string {
	base, gitlab := requestWebBase(item.URL)
	if base == "" || sha == "" {
		return ""
	}
	if gitlab {
		return base + "/-/commit/" + url.PathEscape(sha)
	}
	return base + "/commit/" + url.PathEscape(sha)
}

// requestSectionURL is the request's own commits or changes page.
func requestSectionURL(item forge.ChangeRequest, section string) string {
	if item.URL == "" {
		return ""
	}
	_, gitlab := requestWebBase(item.URL)
	switch {
	case section == "commits" && gitlab:
		return item.URL + "/commits"
	case section == "commits":
		return item.URL + "/commits"
	case gitlab:
		return item.URL + "/diffs"
	}
	return item.URL + "/files"
}

// branchChip is a branch name in a tinted, monospace chip. Clicking opens the
// branch on the hosting site; the button beside it copies the name.
func (a *app) branchChip(c *ui.Context, item forge.ChangeRequest, name string, source bool) {
	theme := c.Theme()
	blue := theme.Accent
	target := requestBranchURL(item, name, source)
	chip := ui.ButtonBase(c).AlignItems(ui.Center).Padding(0, 7).Shrink(0).Radius(theme.Radius).
		Background(blue.Alpha(0.14)).Border(1, blue.Alpha(0.35)).Label(trf("Open branch %s", name)).
		Tooltip(tr("Open branch in browser")).Disabled(target == "" || a.openExternalURL == nil)
	if chip.Hovered() {
		chip.Background(blue.Alpha(0.26))
	}
	chip.Children(func() {
		ui.Text(c, name).SingleLine().Font(a.settings.BufferFont).FontSize(theme.FontSize - 2).TextColor(blue)
	})
	if chip.Clicked() && target != "" && a.openExternalURL != nil {
		a.openExternalURL(target)
	}
	flatIconButton(c, "Copy branch name", "Copy branch name", copyIcon, 11, 2, false, func() {
		a.notifyCopied(c.Services(), "branch name", name)
	})
}

func decisionColor(c *ui.Context, text string) ui.Color {
	theme := c.Theme()
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "not approved"), strings.Contains(lower, "required"), strings.Contains(lower, "changes"),
		strings.Contains(lower, "blocked"), strings.Contains(lower, "discussions"), strings.Contains(lower, "checking"):
		return theme.Warning
	case strings.Contains(lower, "conflict"), strings.Contains(lower, "failed"), strings.Contains(lower, "cannot"):
		return theme.Danger
	case strings.Contains(lower, "approved"), strings.Contains(lower, "mergeable"):
		return theme.Success
	}
	return theme.TextMuted
}

// relativeTime says how long ago t was, as "2 days ago".
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	age := time.Since(t)
	unit := func(n int, one, many string) string {
		if n == 1 {
			return trf("%d "+one+" ago", n)
		}
		return trf("%d "+many+" ago", n)
	}
	switch {
	case age < time.Minute:
		return tr("just now")
	case age < time.Hour:
		return unit(int(age.Minutes()), "minute", "minutes")
	case age < 24*time.Hour:
		return unit(int(age.Hours()), "hour", "hours")
	case age < 30*24*time.Hour:
		return unit(int(age.Hours()/24), "day", "days")
	case age < 365*24*time.Hour:
		return unit(int(age.Hours()/24/30), "month", "months")
	}
	return unit(int(age.Hours()/24/365), "year", "years")
}

// monoChip is a branch name in a small tinted, monospace chip.
func (a *app) monoChip(c *ui.Context, name string, color ui.Color) {
	theme := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Padding(0, 6).Shrink(1).MinWidth(0).Radius(theme.Radius).
		Background(color.Alpha(0.14)).Border(1, color.Alpha(0.35)).Children(func() {
		ui.Text(c, name).SingleLine().Shrink(1).MinWidth(0).Font(a.settings.BufferFont).FontSize(theme.FontSize - 3).TextColor(color)
	})
}

// branchBadge is a branch (or worktree) name as a rounded, tinted badge with the
// branch glyph, in the theme's accent (success for a worktree).
func (a *app) branchBadge(c *ui.Context, name string, worktree bool) {
	theme := c.Theme()
	color := theme.Accent
	if worktree {
		color = theme.Success
	}
	ui.Row(c).AlignItems(ui.Center).Gap(4).Padding(0, 8).MinWidth(0).MaxWidth(300).Shrink(1).Radius(9).
		Background(color.Alpha(0.15)).Border(1, color.Alpha(0.45)).Tooltip(name).Children(func() {
		ui.Icon(c, branchIcon).FontSize(11).TextColor(color).Shrink(0)
		ui.Text(c, name).MinWidth(0).Shrink(1).SingleLine().Bold().FontSize(theme.FontSize - 2).TextColor(color)
	})
}
