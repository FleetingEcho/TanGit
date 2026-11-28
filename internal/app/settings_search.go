package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// settingsSearchOption is navigation metadata, not a second copy of a
// control. Labels and hints stay in the same catalogs as the visible settings.
type settingsSearchOption struct {
	page  int
	label string
	hint  string
}

func settingsPageTitle(page int) string {
	switch page {
	case settingsPageAppearance:
		return "Appearance"
	case settingsPageTypography:
		return "Typography"
	case settingsPageBehavior:
		return "Behavior"
	case settingsPageEditor, settingsPageCodeEditor:
		return "Editor"
	case settingsPageLSP:
		return "LSP"
	case settingsPageWorkspace:
		return "Workspace"
	case settingsPageCache:
		return "Cache"
	case settingsPageCommands:
		return "Commands"
	case settingsPageSSHProfiles:
		return "SSH profiles"
	case settingsPageUpdates:
		return "Updates"
	default:
		return "Settings"
	}
}

func settingsPageGlyph(page int) *ui.SVG {
	switch page {
	case settingsPageAppearance:
		return themeIcon
	case settingsPageTypography:
		return fontIcon
	case settingsPageBehavior:
		return commitIcon
	case settingsPageEditor, settingsPageCodeEditor:
		return fileCodeIcon
	case settingsPageLSP:
		return terminalIcon
	case settingsPageWorkspace:
		return worktreesIcon
	case settingsPageCache:
		return trashIcon
	case settingsPageCommands:
		return commandIcon
	case settingsPageSSHProfiles:
		return remotesIcon
	case settingsPageUpdates:
		return updateIcon
	default:
		return settingsIcon
	}
}

// settingsSearchCatalog lists the stable preferences across all pages. Form
// rows generated from saved profiles or custom commands are represented by
// their field names; their concrete values are never copied into this index.
var settingsSearchCatalog = []settingsSearchOption{
	{settingsPageAppearance, "Language", "Language of the interface"},
	{settingsPageAppearance, "Theme", "Palettes for the whole window"},
	{settingsPageAppearance, "Customize theme", "Build your own theme with a color picker"},
	{settingsPageAppearance, "Accent color", "Buttons and highlights"},
	{settingsPageAppearance, "Home background", "An image behind the repository picker"},
	{settingsPageAppearance, "Background opacity", "How strongly the Home image shows"},

	{settingsPageTypography, "Interface font size", "Text size in controls and panes"},
	{settingsPageTypography, "Interface font", "System is the desktop's font"},
	{settingsPageTypography, "Interface spacing", "Padding inside and between controls"},
	{settingsPageTypography, "Buffer font size", "Text size in diffs and code buffers"},
	{settingsPageTypography, "Buffer font", "Diffs and code buffers; System is monospaced"},
	{settingsPageTypography, "Buffer line spacing", "Line height as a multiple of font size"},
	{settingsPageTypography, "Corner radius", "Button and panel rounding"},

	{settingsPageBehavior, "Conflict and hunk buttons", "Choose icons, icons with labels, or guided conflict resolution"},
	{settingsPageBehavior, "Show diffs side by side", "Diff reading"},
	{settingsPageBehavior, "Wrap long diff lines", "Diff reading"},
	{settingsPageBehavior, "Ignore untracked files", "Changes and file lists"},
	{settingsPageBehavior, "Keep running in the tray when the window is closed", "Closing the window"},
	{settingsPageBehavior, "Enable keyboard shortcuts", "Keyboard controls"},
	{settingsPageBehavior, "Safety journal", "Save a recovery point before actions so they can be undone"},
	{settingsPageBehavior, "Shortcut notifications", "Show shortcut acknowledgements while keeping Command Log and operation notifications visible."},
	{settingsPageBehavior, "Background fetch", "Fetches every remote while the window is open"},
	{settingsPageBehavior, "Fetch remotes in the background", "Background fetch"},
	{settingsPageBehavior, "Background fetch interval", "Choose a preset or enter a custom interval in minutes"},
	{settingsPageBehavior, "Prune stale remote-tracking branches on fetch", "Fetch pruning"},
	{settingsPageBehavior, "Continue preflight", "Required safety checks and an optional quality gate"},
	{settingsPageBehavior, "Required check command", "Blank disables Continue until configured"},
	{settingsPageBehavior, "Optional check command", "Blank is skipped; failure needs explicit override"},

	{settingsPageEditor, "Editor command", "Fills in {{filename}} and {{line}}"},
	{settingsPageEditor, "Presets", "Choose the editor buttons' destination"},
	{settingsPageEditor, "Add editor", "Add a custom external editor"},
	{settingsPageEditor, "Line numbers", "Absolute, relative, or hidden"},
	{settingsPageEditor, "Whitespace", "Choose which spaces and tabs are visible"},
	{settingsPageEditor, "Wrap long lines", "Built-in editor preferences"},
	{settingsPageEditor, "Auto-close brackets and quotes", "Built-in editor preferences"},
	{settingsPageEditor, "Inlay hints", "Built-in editor preferences"},
	{settingsPageEditor, "Semantic highlighting", "Built-in editor preferences"},
	{settingsPageEditor, "Minimap", "Built-in editor preferences"},
	{settingsPageEditor, "Sticky symbol header", "Built-in editor preferences"},
	{settingsPageEditor, "Use Zoekt code search", "Build a local full-text index for repository files"},
	{settingsPageEditor, "Search exclusions", "Folders skipped by content search and by the search index"},
	{settingsPageEditor, "Skip dependencies and generated folders automatically", "Search exclusions"},
	{settingsPageEditor, "Excluded folder", "Add a folder to the search exclusions"},
	{settingsPageEditor, "Keyboard shortcuts", "Customize built-in editor key bindings"},

	{settingsPageLSP, "Language servers", "Language Server Protocol detection, executable paths, and installation"},

	{settingsPageWorkspace, "Workspace sections", "The tabs the sidebar lists"},
	{settingsPageWorkspace, "Sidebar tabs", "Click to toggle; drag to reorder"},
	{settingsPageWorkspace, "Toolbar buttons", "Choose which global actions stay visible"},
	{settingsPageWorkspace, "Header toolbar", "Repository, editor, and application actions"},
	{settingsPageWorkspace, "Status bar", "Command, pull request, terminal, and help actions"},
	{settingsPageWorkspace, "Default terminal position", "Where new terminal docks open"},
	{settingsPageWorkspace, "Repository start", "Choose the first page for newly opened repositories"},
	{settingsPageWorkspace, "Start page", "The first page of a repository you open"},

	{settingsPageCache, "Cache", "Zoekt code-search indexes and downloaded author avatars"},
	{settingsPageCache, "Manage repositories", "Choose a repository's search index to clear"},
	{settingsPageCache, "Refresh cache size", "Remeasure disk usage"},
	{settingsPageCache, "Clear cache", "Delete Zoekt indexes and cached avatars"},
	{settingsPageCache, "Zoekt search indexes", "Full-text Code Search disk usage"},
	{settingsPageCache, "Cached avatars", "Commit-author images"},
	{settingsPageCache, "Index size limit", "The most source content one repository may put into its search index"},

	{settingsPageCommands, "Custom commands", "Your own keys for shell commands in this repository"},
	{settingsPageCommands, "Name", "Shown in the commands menu and the shortcut list"},
	{settingsPageCommands, "Command", "Run with your shell in the repository root"},
	{settingsPageCommands, "Shortcut", "Optional key binding for a custom command"},

	{settingsPageSSHProfiles, "SSH profiles", "Remote Git workspaces, without credentials in settings"},
	{settingsPageSSHProfiles, "Profile name", "A label for this connection"},
	{settingsPageSSHProfiles, "Host", "DNS name or IP address, without a port"},
	{settingsPageSSHProfiles, "Username", "Remote login name"},
	{settingsPageSSHProfiles, "Port", "SSH defaults to 22"},
	{settingsPageSSHProfiles, "Authentication", "Agent, private key, or password"},
	{settingsPageSSHProfiles, "Private key", "A path on this machine"},
	{settingsPageSSHProfiles, "Key passphrase", "Leave blank to keep the saved passphrase"},
	{settingsPageSSHProfiles, "Password", "Leave blank to keep the saved password"},
	{settingsPageSSHProfiles, "Default remote folder", "Absolute, ~, or ~/folder"},

	{settingsPageUpdates, "Updates", "Application updates"},
	{settingsPageUpdates, "Version", "Current application version"},
	{settingsPageUpdates, "Update status", "Check for a newer application version"},
}

// settingsSearchMatches matches every word against both source and
// localized strings, without touching or constructing any settings page.
func settingsSearchMatches(query string) []settingsSearchOption {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	if len(words) == 0 {
		return nil
	}
	matches := make([]settingsSearchOption, 0, len(settingsSearchCatalog))
	for _, option := range settingsSearchCatalog {
		page := settingsPageTitle(option.page)
		haystack := strings.ToLower(strings.Join([]string{
			option.label, option.hint, page,
			tr(option.label), tr(option.hint), tr(page),
		}, " "))
		matched := true
		for _, word := range words {
			if !strings.Contains(haystack, word) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, option)
		}
	}
	return matches
}

func (a *app) chooseSettingsSearchResult(option settingsSearchOption) {
	a.stopEditorShortcutRecording()
	a.settingsUI.page = option.page
	a.settingsUI.revealShortcuts = false
	a.settingsUI.searchQuery = ""
}

// settingsSearchInput lives in the dialog header so it never takes vertical
// room from the category rail or pushes its title into the next row.
func (a *app) settingsSearchInput(c *ui.Context) {
	theme := c.Theme()
	ui.Row(c).Width(285).MinWidth(0).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Icon(c, searchIcon).FontSize(14).TextColor(theme.TextMuted)
		ui.TextInput(c, &a.settingsUI.searchQuery).Bind(&a.settingsUI.searchInput).
			Label(tr("Search settings")).Placeholder(tr("Find an option by name or description")).Grow(1).MinWidth(0)
		if strings.TrimSpace(a.settingsUI.searchQuery) != "" {
			flatIconButton(c, "Clear settings search", "Show every item", closeIcon, 13, 3, false, func() {
				a.settingsUI.searchQuery = ""
			})
		}
	})
}

func (a *app) settingsSearchResults(c *ui.Context) {
	query := strings.TrimSpace(a.settingsUI.searchQuery)
	matches := settingsSearchMatches(query)
	if len(matches) == 0 {
		listSearchEmpty(c, "No settings match your search")
		return
	}
	theme := c.Theme()
	ui.Scroll(c).Grow(1).MinHeight(0).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(6).Padding(2, 8, 8, 2).Children(func() {
			for _, option := range matches {
				option := option
				c.Key("settings-search:" + settingsPageTitle(option.page) + ":" + option.label)
				page := settingsPageTitle(option.page)
				button := ui.ButtonBase(c).FillWidth().MinWidth(0).AlignItems(ui.Center).
					Gap(12).Padding(10, 12).Radius(theme.Radius).
					Label(tr(option.label) + " — " + tr(page))
				if option.hint != "" {
					button.Tooltip(tr(option.hint))
				}
				if button.Hovered() {
					button.Background(theme.SurfaceHover)
				}
				button.Children(func() {
					ui.Box(c).Size(30, 30).Radius(theme.Radius).Center().
						Background(theme.Accent.Alpha(0.1)).Children(func() {
						ui.Icon(c, settingsPageGlyph(option.page)).FontSize(15).TextColor(theme.Accent)
					})
					ui.Column(c).Gap(3).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, tr(option.label)).Bold().SingleLine()
						if option.hint != "" {
							ui.Text(c, tr(option.hint)).TextColor(theme.TextMuted).
								FontSize(theme.FontSize - 1).SingleLine()
						}
					})
					ui.Text(c, tr(page)).TextColor(theme.TextMuted).
						FontSize(theme.FontSize - 1).SingleLine()
					ui.Icon(c, chevronRightIcon).FontSize(14).TextColor(theme.TextMuted)
				})
				if button.Clicked() {
					a.chooseSettingsSearchResult(option)
				}
			}
		})
	})
}
