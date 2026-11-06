package app

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// appSettings is what the settings modal edits and the app stores between
// launches, beside the recent repositories.
type appSettings struct {
	// Theme is a name from themeNames, "System" following the desktop's
	// light or dark appearance.
	Theme string `json:"theme"`
	// Accent is a name from accentNames or a custom #rrggbb color, painting
	// buttons and highlights in place of the palette's own; empty keeps it.
	Accent string `json:"accent"`
	// FontSize, Spacing and Radius are the theme metrics; 0 keeps the
	// palette's own value (except Radius, where 0 is a sharp corner).
	FontSize float64 `json:"fontSize"`
	// Font is the interface family. The Buffer settings independently control
	// diffs and code buffers; System means the platform's monospaced family.
	Font             string  `json:"font"`
	BufferFont       string  `json:"bufferFont"`
	BufferFontSize   float64 `json:"bufferFontSize"`
	BufferLineHeight float64 `json:"bufferLineHeight"`
	Spacing          float64 `json:"spacing"`
	Radius           float64 `json:"radius"`
	// DiffSplit opens every diff side by side, DiffWrap wraps long diff
	// lines instead of scrolling them sideways, and IgnoreUntracked leaves
	// untracked files out of the Changes list.
	DiffSplit       bool `json:"diffSplit"`
	DiffWrap        bool `json:"diffWrap"`
	IgnoreUntracked bool `json:"ignoreUntracked"`
	// DisableShortcuts switches the window's keyboard shortcuts off. The
	// header icons keep working, so every action stays reachable.
	DisableShortcuts bool `json:"disableShortcuts"`
	// Language is the code of the language the interface is drawn in, from
	// languageChoices; English is the default.
	Language string `json:"language"`
	// EditorCommand is the command the editor buttons run, with
	// {{filename}} where the file goes and {{line}} where the line does.
	// An empty value keeps the default editor.
	EditorCommand string `json:"editorCommand"`
	// ExternalEditors contains additional named commands offered by Settings
	// and the Editor header's direct-launch menu.
	ExternalEditors []editorPreset `json:"externalEditors,omitempty"`
	// SearchExcludedDirectories contains folder names skipped by workspace
	// content search. Nil in an older settings file selects the
	// defaults; a saved empty slice deliberately disables every exclusion.
	SearchExcludedDirectories []string `json:"searchExcludedDirectories"`
	// LanguageServers keeps per-language executable choices. An absent entry
	// means the built-in default: enabled and discovered on PATH.
	LanguageServers map[string]languageServerSettings `json:"languageServers,omitempty"`
	// HomeBackgroundPath is the image behind the repository picker. Empty
	// keeps the theme's plain gradient; the image itself stays where the
	// user chose it rather than being copied into the settings file.
	HomeBackgroundPath string `json:"homeBackgroundPath"`
	// HomeBackgroundDisabled hides the image without forgetting its path or
	// opacity. False keeps images from older settings files enabled.
	HomeBackgroundDisabled bool `json:"homeBackgroundDisabled"`
	// HomeBackgroundOpacity is the image's strength, from 5% to 100%. Zero
	// in an older file takes the original subtle default.
	HomeBackgroundOpacity float64 `json:"homeBackgroundOpacity"`
	// VisibleSections names the workspace sections the sidebar lists, by the
	// ids of workspaceSectionIDs. A file without the choice, or one naming no
	// known section, starts from defaultVisibleSections.
	VisibleSections []string `json:"visibleSections"`
}

type languageServerSettings struct {
	Disabled       bool     `json:"disabled,omitempty"`
	Path           string   `json:"path,omitempty"`
	Arguments      []string `json:"arguments,omitempty"`
	ProbeArguments []string `json:"probeArguments,omitempty"`
	// Custom is present only for user-defined servers. Keeping the definition
	// with its launch settings preserves the profile while it is disabled.
	Custom *customLanguageServerDefinition `json:"custom,omitempty"`
}

// settingsFile is the name of the settings inside MyGo's user data directory,
// beside the recent repositories.
const settingsFile = "settings.json"

// defaultTheme is the palette a fresh install starts on and the fallback for
// a stored name no theme answers to.
const defaultTheme = "Tokyo Night"

const defaultHomeBackgroundOpacity = 0.24

const (
	defaultBufferFontSize   = 13.0
	defaultBufferLineHeight = 1.5
)

// defaultSettings starts on the Tokyo Night palette, the appearance the app
// ships with; the settings modal tunes the theme and its metrics from there.
func defaultSettings() appSettings {
	base := themeChoices[defaultTheme].build()
	return appSettings{
		Theme:                     defaultTheme,
		Accent:                    "",
		FontSize:                  float64(base.FontSize),
		Font:                      "System",
		BufferFont:                "System",
		BufferFontSize:            defaultBufferFontSize,
		BufferLineHeight:          defaultBufferLineHeight,
		Spacing:                   float64(base.Spacing),
		Radius:                    float64(base.Radius),
		Language:                  defaultLanguage,
		EditorCommand:             defaultEditorCommand,
		SearchExcludedDirectories: defaultWorkspaceSearchExcludedDirectories(),
		HomeBackgroundOpacity:     defaultHomeBackgroundOpacity,
		VisibleSections:           slices.Clone(defaultVisibleSections),
	}
}

// normalizeSettings fills what a damaged or older file leaves empty and
// rejects values no theme can use, so the app starts with something sane.
func normalizeSettings(s appSettings) appSettings {
	if _, ok := themeChoices[s.Theme]; !ok {
		s.Theme = defaultTheme
	}
	if _, ok := accentChoices[s.Accent]; !ok {
		if color, custom := parseCustomAccent(s.Accent); custom {
			s.Accent = formatCustomAccent(color)
		} else {
			s.Accent = ""
		}
	}
	if s.FontSize < 9 || s.FontSize > 26 {
		s.FontSize = defaultSettings().FontSize
	}
	if strings.TrimSpace(s.Font) == "" {
		s.Font = "System"
	}
	if strings.TrimSpace(s.BufferFont) == "" {
		s.BufferFont = "System"
	}
	if s.BufferFontSize < 9 || s.BufferFontSize > 26 {
		s.BufferFontSize = defaultBufferFontSize
	}
	if s.BufferLineHeight < 1 || s.BufferLineHeight > 2.5 {
		s.BufferLineHeight = defaultBufferLineHeight
	}
	if s.Spacing < 2 || s.Spacing > 10 {
		s.Spacing = defaultSettings().Spacing
	}
	if s.Radius < 0 || s.Radius > 20 {
		s.Radius = defaultSettings().Radius
	}
	s.Language = normalizeLanguage(s.Language)
	s.EditorCommand = effectiveEditorCommand(s.EditorCommand)
	s.ExternalEditors = normalizeExternalEditors(s.ExternalEditors)
	if s.SearchExcludedDirectories == nil {
		s.SearchExcludedDirectories = defaultWorkspaceSearchExcludedDirectories()
	} else {
		s.SearchExcludedDirectories = normalizeWorkspaceSearchExcludedDirectories(s.SearchExcludedDirectories)
	}
	s.LanguageServers = normalizeLanguageServerSettings(s.LanguageServers)
	s.HomeBackgroundOpacity = normalizeHomeBackgroundOpacity(s.HomeBackgroundOpacity)
	s.VisibleSections = normalizeVisibleSections(s.VisibleSections)
	return s
}

func normalizeLanguageServerSettings(servers map[string]languageServerSettings) map[string]languageServerSettings {
	if len(servers) == 0 {
		return nil
	}
	normalized := make(map[string]languageServerSettings, len(servers))
	for language, server := range servers {
		language = strings.ToLower(strings.TrimSpace(language))
		if language == "" {
			continue
		}
		server.Path = strings.TrimSpace(server.Path)
		arguments := make([]string, 0, len(server.Arguments))
		for _, argument := range server.Arguments {
			if argument = strings.TrimSpace(argument); argument != "" {
				arguments = append(arguments, argument)
			}
		}
		if len(arguments) == 0 {
			arguments = nil
		}
		server.Arguments = arguments
		probeArguments := make([]string, 0, len(server.ProbeArguments))
		for _, argument := range server.ProbeArguments {
			if argument = strings.TrimSpace(argument); argument != "" {
				probeArguments = append(probeArguments, argument)
			}
		}
		if len(probeArguments) == 0 {
			probeArguments = nil
		}
		server.ProbeArguments = probeArguments
		if server.Custom != nil {
			server.Custom.Name = strings.TrimSpace(server.Custom.Name)
			server.Custom.Description = strings.TrimSpace(server.Custom.Description)
			server.Custom.DownloadURL = strings.TrimSpace(server.Custom.DownloadURL)
			extensions := make(map[string]string, len(server.Custom.Extensions))
			for extension, id := range server.Custom.Extensions {
				extension, id = strings.ToLower(strings.TrimSpace(extension)), strings.ToLower(strings.TrimSpace(id))
				if strings.HasPrefix(extension, ".") && len(extension) > 1 && id != "" {
					extensions[extension] = id
				}
			}
			server.Custom.Extensions = extensions
			if server.Custom.Name == "" || server.Path == "" || len(extensions) == 0 || !validCustomLanguageServerDownload(server.Custom.DownloadURL) {
				continue
			}
		}
		profile, knownProfile := languageServerProfileByID(language)
		if server.Custom == nil && !server.Disabled && server.Path == "" && len(server.Arguments) == 0 &&
			(!knownProfile || profile.EnabledByDefault) {
			continue
		}
		normalized[language] = server
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// normalizeHomeBackgroundOpacity accepts only useful visible strengths. Zero
// is not offered by the slider, so it also identifies files from before this
// preference existed; NaN is rejected explicitly because comparisons miss it.
func normalizeHomeBackgroundOpacity(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0.05 || value > 1 {
		return defaultHomeBackgroundOpacity
	}
	return value
}

// normalizeVisibleSections keeps the known section ids once each, in tab
// order. A file without the choice, or one that would hide every section,
// starts from the default set: the workspace must always have a pane.
func normalizeVisibleSections(visible []string) []string {
	show := make(map[string]bool, len(visible))
	for _, id := range visible {
		show[id] = true
	}
	kept := make([]string, 0, len(visible))
	for _, id := range workspaceSectionIDs {
		if show[id] {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		return slices.Clone(defaultVisibleSections)
	}
	return kept
}

func settingsStorePath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, settingsFile)
}

// loadAppSettings reads the settings stored in dir. A missing or damaged
// file gives the defaults rather than an error.
func loadAppSettings(dir string) appSettings {
	path := settingsStorePath(dir)
	if path == "" {
		return defaultSettings()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultSettings()
	}
	var s appSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultSettings()
	}
	return normalizeSettings(s)
}

// saveAppSettings writes the settings to dir atomically, as the history is
// written, so a crash mid-write leaves the last good file.
func saveAppSettings(dir string, s appSettings) error {
	path := settingsStorePath(dir)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadSettings reads the stored settings once the data directory is known,
// and applies the toggles they carry to the running app.
func (a *app) loadSettings() {
	if a.settingsLoaded {
		return
	}
	dir := a.dataDir()
	if dir == "" {
		return
	}
	a.settings = normalizeSettings(loadAppSettings(dir))
	a.settingsLoaded = true
	currentLanguage = normalizeLanguage(a.settings.Language)
	a.diffSplit = a.settings.DiffSplit
	a.diffWrap = a.settings.DiffWrap
	a.ignoreUntracked = a.settings.IgnoreUntracked
	a.ensureVisibleSection()
}

// saveSettings stores the settings. A failure is silent: the app keeps the
// choices for this run and simply starts with the old file next time.
func (a *app) saveSettings() {
	a.settingsLoaded = true
	_ = saveAppSettings(a.dataDir(), a.settings)
}

// applySettingsChange mirrors the toggles a settings change carries into the
// running app and stores the new file.
func (a *app) applySettingsChange() {
	a.diffSplit = a.settings.DiffSplit
	a.diffWrap = a.settings.DiffWrap
	a.ignoreUntracked = a.settings.IgnoreUntracked
	a.ensureVisibleSection()
	currentLanguage = normalizeLanguage(a.settings.Language)
	refreshTrayMenu()
	a.applyDiffOptions()
	a.saveSettings()
}

// themeChoice is one entry of the theme menu: the name the settings store,
// and the palette it builds over a light or dark base.
type themeChoice struct {
	// build returns a fresh palette, so a frame's metrics never leak into
	// the next one.
	build func() *ui.Theme
}

// themeNames are the theme menu's options, System first and then the
// palettes, dark before light within a family.
var themeNames = []string{
	"System",
	"Tokyo Night",
	"Dracula",
	"Gruvbox Dark",
	"GitHub Dark",
	"Nord",
	"One Dark",
	"Solarized Dark",
	"Catppuccin Frappé",
	"Catppuccin Macchiato",
	"Catppuccin Mocha",
	"Rosé Pine",
	"Kanagawa Wave",
	"Everforest Dark",
	"Ayu Mirage",
	"Gruvbox Light",
	"GitHub Light",
	"Solarized Light",
	"Catppuccin Latte",
	"One Light",
	"Rosé Pine Dawn",
}

var themeChoices = map[string]themeChoice{
	"System": {build: func() *ui.Theme { return ui.LightTheme() }},
	"Tokyo Night": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#1a1b26", surface: "#24283b", surfaceHover: "#2f3549",
			surfacePressed: "#3b4261", border: "#414868", text: "#c0caf5",
			textMuted: "#787c99", accent: "#7aa2f7", accentHover: "#8fb3ff",
			accentPressed: "#5f7bd0", accentText: "#1a1b26", danger: "#f7768e",
			warning: "#e0af68", success: "#9ece6a",
			selection: ui.RGBA(122, 162, 247, 0.3), focus: ui.RGBA(122, 162, 247, 0.6),
		})
	}},
	"Dracula": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#282a36", surface: "#343746", surfaceHover: "#424450",
			surfacePressed: "#4d5065", border: "#44475a", text: "#f8f8f2",
			textMuted: "#8b8fa8", accent: "#bd93f9", accentHover: "#caa9fa",
			accentPressed: "#a67cf0", accentText: "#282a36", danger: "#ff5555",
			warning: "#ffb86c", success: "#50fa7b",
			selection: ui.RGBA(189, 147, 249, 0.35), focus: ui.RGBA(189, 147, 249, 0.6),
		})
	}},
	"Gruvbox Dark": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#282828", surface: "#3c3836", surfaceHover: "#504945",
			surfacePressed: "#665c54", border: "#504945", text: "#ebdbb2",
			textMuted: "#a89984", accent: "#fabd2f", accentHover: "#ffd75f",
			accentPressed: "#d79921", accentText: "#282828", danger: "#fb4934",
			warning: "#fe8019", success: "#b8bb26",
			selection: ui.RGBA(250, 189, 47, 0.3), focus: ui.RGBA(250, 189, 47, 0.55),
		})
	}},
	"GitHub Dark": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#0d1117", surface: "#161b22", surfaceHover: "#21262d",
			surfacePressed: "#30363d", border: "#30363d", text: "#e6edf3",
			textMuted: "#8b949e", accent: "#2f81f7", accentHover: "#58a6ff",
			accentPressed: "#1f6feb", accentText: "#ffffff", danger: "#f85149",
			warning: "#d29922", success: "#3fb950",
			selection: ui.RGBA(47, 129, 247, 0.35), focus: ui.RGBA(47, 129, 247, 0.6),
		})
	}},
	"Nord": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#2e3440", surface: "#3b4252", surfaceHover: "#434c5e",
			surfacePressed: "#4c566a", border: "#434c5e", text: "#eceff4",
			textMuted: "#a3aab8", accent: "#88c0d0", accentHover: "#9fd3e0",
			accentPressed: "#6ba8b8", accentText: "#2e3440", danger: "#bf616a",
			warning: "#ebcb8b", success: "#a3be8c",
			selection: ui.RGBA(136, 192, 208, 0.3), focus: ui.RGBA(136, 192, 208, 0.55),
		})
	}},
	"One Dark": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#282c34", surface: "#31353f", surfaceHover: "#3a3f4b",
			surfacePressed: "#444a57", border: "#3e4451", text: "#abb2bf",
			textMuted: "#7f848e", accent: "#61afef", accentHover: "#7bc0f5",
			accentPressed: "#4a93cf", accentText: "#282c34", danger: "#e06c75",
			warning: "#e5c07b", success: "#98c379",
			selection: ui.RGBA(97, 175, 239, 0.3), focus: ui.RGBA(97, 175, 239, 0.55),
		})
	}},
	"Solarized Dark": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#002b36", surface: "#073642", surfaceHover: "#0b4652",
			surfacePressed: "#12525e", border: "#586e75", text: "#839496",
			textMuted: "#657b83", accent: "#268bd2", accentHover: "#3ca0e6",
			accentPressed: "#1b6f9f", accentText: "#fdf6e3", danger: "#dc322f",
			warning: "#b58900", success: "#859900",
			selection: ui.RGBA(38, 139, 210, 0.28), focus: ui.RGBA(38, 139, 210, 0.55),
		})
	}},
	"Catppuccin Frappé": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#303446", surface: "#414559", surfaceHover: "#51576d",
			surfacePressed: "#626880", border: "#51576d", text: "#c6d0f5",
			textMuted: "#a5adce", accent: "#8caaee", accentHover: "#a0b9f2",
			accentPressed: "#708bca", accentText: "#303446", danger: "#e78284",
			warning: "#e5c890", success: "#a6d189",
			selection: ui.RGBA(140, 170, 238, 0.30), focus: ui.RGBA(140, 170, 238, 0.58),
		})
	}},
	"Catppuccin Macchiato": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#24273a", surface: "#363a4f", surfaceHover: "#494d64",
			surfacePressed: "#5b6078", border: "#494d64", text: "#cad3f5",
			textMuted: "#a5adcb", accent: "#8aadf4", accentHover: "#9dbbf7",
			accentPressed: "#6d8fce", accentText: "#24273a", danger: "#ed8796",
			warning: "#eed49f", success: "#a6da95",
			selection: ui.RGBA(138, 173, 244, 0.30), focus: ui.RGBA(138, 173, 244, 0.58),
		})
	}},
	"Catppuccin Mocha": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#1e1e2e", surface: "#313244", surfaceHover: "#45475a",
			surfacePressed: "#585b70", border: "#45475a", text: "#cdd6f4",
			textMuted: "#a6adc8", accent: "#89b4fa", accentHover: "#9cc2ff",
			accentPressed: "#6c94d8", accentText: "#1e1e2e", danger: "#f38ba8",
			warning: "#fab387", success: "#a6e3a1",
			selection: ui.RGBA(137, 180, 250, 0.32), focus: ui.RGBA(137, 180, 250, 0.6),
		})
	}},
	"Rosé Pine": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#191724", surface: "#1f1d2e", surfaceHover: "#26233a",
			surfacePressed: "#403d52", border: "#403d52", text: "#e0def4",
			textMuted: "#908caa", accent: "#c4a7e7", accentHover: "#d2b8ed",
			accentPressed: "#a78bc8", accentText: "#191724", danger: "#eb6f92",
			warning: "#f6c177", success: "#95b1ac",
			selection: ui.RGBA(196, 167, 231, 0.30), focus: ui.RGBA(196, 167, 231, 0.58),
		})
	}},
	"Kanagawa Wave": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#1f1f28", surface: "#2a2a37", surfaceHover: "#363646",
			surfacePressed: "#54546d", border: "#54546d", text: "#dcd7ba",
			textMuted: "#938aa9", accent: "#7e9cd8", accentHover: "#9cacca",
			accentPressed: "#637eaf", accentText: "#16161d", danger: "#e46876",
			warning: "#dca561", success: "#98bb6c",
			selection: ui.RGBA(45, 79, 103, 0.75), focus: ui.RGBA(126, 156, 216, 0.55),
		})
	}},
	"Everforest Dark": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#2d353b", surface: "#343f44", surfaceHover: "#3d484d",
			surfacePressed: "#475258", border: "#4f585e", text: "#d3c6aa",
			textMuted: "#9da9a0", accent: "#7fbbb3", accentHover: "#95cac3",
			accentPressed: "#649b94", accentText: "#232a2e", danger: "#e67e80",
			warning: "#dbbc7f", success: "#a7c080",
			selection: ui.RGBA(127, 187, 179, 0.28), focus: ui.RGBA(127, 187, 179, 0.55),
		})
	}},
	"Ayu Mirage": {build: func() *ui.Theme {
		return palette(ui.DarkTheme(), paletteColors{
			background: "#1f2430", surface: "#242936", surfaceHover: "#343d4b",
			surfacePressed: "#3d4757", border: "#46505c", text: "#cccac2",
			textMuted: "#707a8c", accent: "#ffcc66", accentHover: "#ffd786",
			accentPressed: "#d7a94c", accentText: "#1f2430", danger: "#f28779",
			warning: "#ffad66", success: "#bae67e",
			selection: ui.RGBA(255, 204, 102, 0.24), focus: ui.RGBA(255, 204, 102, 0.50),
		})
	}},
	"Gruvbox Light": {build: func() *ui.Theme {
		return palette(ui.LightTheme(), paletteColors{
			background: "#fbf1c7", surface: "#f2e5bc", surfaceHover: "#ebdbb2",
			surfacePressed: "#d5c4a1", border: "#d5c4a1", text: "#3c3836",
			textMuted: "#7c6f64", accent: "#b57614", accentHover: "#d79921",
			accentPressed: "#8f5a0a", accentText: "#fbf1c7", danger: "#9d0006",
			warning: "#af3a03", success: "#79740e",
			selection: ui.RGBA(181, 118, 20, 0.25), focus: ui.RGBA(181, 118, 20, 0.5),
		})
	}},
	"GitHub Light": {build: func() *ui.Theme {
		return palette(ui.LightTheme(), paletteColors{
			background: "#ffffff", surface: "#f6f8fa", surfaceHover: "#eef1f4",
			surfacePressed: "#e7ebef", border: "#d0d7de", text: "#1f2328",
			textMuted: "#656d76", accent: "#0969da", accentHover: "#1f6feb",
			accentPressed: "#0550ae", accentText: "#ffffff", danger: "#cf222e",
			warning: "#9a6700", success: "#1a7f37",
			selection: ui.RGBA(9, 105, 218, 0.2), focus: ui.RGBA(9, 105, 218, 0.5),
		})
	}},
	"Solarized Light": {build: func() *ui.Theme {
		return palette(ui.LightTheme(), paletteColors{
			background: "#fdf6e3", surface: "#eee8d5", surfaceHover: "#e4ddc8",
			surfacePressed: "#d9d2bd", border: "#d3cbb7", text: "#073642",
			textMuted: "#657b83", accent: "#268bd2", accentHover: "#1f6fa8",
			accentPressed: "#1a5f8f", accentText: "#fdf6e3", danger: "#dc322f",
			warning: "#b58900", success: "#859900",
			selection: ui.RGBA(38, 139, 210, 0.25), focus: ui.RGBA(38, 139, 210, 0.5),
		})
	}},
	"Catppuccin Latte": {build: func() *ui.Theme {
		return palette(ui.LightTheme(), paletteColors{
			background: "#eff1f5", surface: "#e6e9ef", surfaceHover: "#dce0e8",
			surfacePressed: "#ccd0da", border: "#ccd0da", text: "#4c4f69",
			textMuted: "#8c8fa1", accent: "#1e66f5", accentHover: "#4a7ff7",
			accentPressed: "#1652c9", accentText: "#ffffff", danger: "#d20f39",
			warning: "#df8e1d", success: "#40a02b",
			selection: ui.RGBA(30, 102, 245, 0.22), focus: ui.RGBA(30, 102, 245, 0.5),
		})
	}},
	"One Light": {build: func() *ui.Theme {
		return palette(ui.LightTheme(), paletteColors{
			background: "#fafafa", surface: "#f0f0f0", surfaceHover: "#e8e8e8",
			surfacePressed: "#dfdfdf", border: "#e0e0e0", text: "#383a42",
			textMuted: "#a0a1a7", accent: "#4078f2", accentHover: "#5a8cf3",
			accentPressed: "#2f60c9", accentText: "#ffffff", danger: "#e45649",
			warning: "#c18401", success: "#50a14f",
			selection: ui.RGBA(64, 120, 242, 0.2), focus: ui.RGBA(64, 120, 242, 0.5),
		})
	}},
	"Rosé Pine Dawn": {build: func() *ui.Theme {
		return palette(ui.LightTheme(), paletteColors{
			background: "#faf4ed", surface: "#fffaf3", surfaceHover: "#f2e9e1",
			surfacePressed: "#dfdad9", border: "#cecacd", text: "#464261",
			textMuted: "#797593", accent: "#907aa9", accentHover: "#7b668f",
			accentPressed: "#665475", accentText: "#ffffff", danger: "#b4637a",
			warning: "#ea9d34", success: "#6d8f89",
			selection: ui.RGBA(144, 122, 169, 0.22), focus: ui.RGBA(144, 122, 169, 0.50),
		})
	}},
}

// paletteColors are the colors one palette overrides on a base theme; the
// metrics (spacing, radius, font) come from the settings afterwards.
type paletteColors struct {
	background, surface, surfaceHover, surfacePressed, border string
	text, textMuted                                           string
	accent, accentHover, accentPressed, accentText            string
	danger, warning, success                                  string
	selection, focus                                          ui.Color
}

func palette(base *ui.Theme, c paletteColors) *ui.Theme {
	base.Background = ui.Hex(c.background)
	base.Surface = ui.Hex(c.surface)
	base.SurfaceHover = ui.Hex(c.surfaceHover)
	base.SurfacePressed = ui.Hex(c.surfacePressed)
	base.Border = ui.Hex(c.border)
	base.Text = ui.Hex(c.text)
	base.TextMuted = ui.Hex(c.textMuted)
	base.Accent = ui.Hex(c.accent)
	base.AccentHover = ui.Hex(c.accentHover)
	base.AccentPressed = ui.Hex(c.accentPressed)
	base.AccentText = ui.Hex(c.accentText)
	base.Danger = ui.Hex(c.danger)
	base.Warning = ui.Hex(c.warning)
	base.Success = ui.Hex(c.success)
	base.Selection = c.selection
	base.Focus = c.focus
	return base
}

// accentNames are the accent colors the settings offer, in the order the row
// draws them: around the color wheel, from blue to slate. The palette's own
// accent is the setting's empty value and is not among them.
var accentNames = []string{
	"Blue", "Sky", "Cyan", "Teal", "Emerald", "Green", "Lime", "Yellow",
	"Amber", "Orange", "Red", "Rose", "Pink", "Violet", "Purple", "Indigo",
	"Slate",
}

// accentChoices are the colors behind accentNames: muted, low-chroma tones
// that read as a soft wash rather than a neon one, while still holding their
// own as text and icons on a dark or a light page.
var accentChoices = map[string]ui.Color{
	"Blue":    ui.Hex("#6b8fd6"),
	"Sky":     ui.Hex("#6690b5"),
	"Cyan":    ui.Hex("#5d95a3"),
	"Teal":    ui.Hex("#5a9792"),
	"Emerald": ui.Hex("#5d9a86"),
	"Green":   ui.Hex("#6b9a78"),
	"Lime":    ui.Hex("#83954b"),
	"Yellow":  ui.Hex("#9c8f3f"),
	"Amber":   ui.Hex("#a68b45"),
	"Orange":  ui.Hex("#bf7c50"),
	"Red":     ui.Hex("#c56f6f"),
	"Rose":    ui.Hex("#c26f7f"),
	"Pink":    ui.Hex("#bf74a4"),
	"Violet":  ui.Hex("#987bc4"),
	"Purple":  ui.Hex("#8f78bd"),
	"Indigo":  ui.Hex("#7d82cf"),
	"Slate":   ui.Hex("#7d8aa0"),
}

// parseCustomAccent accepts only the opaque six-digit hex form saved by the
// picker. Settings files are untrusted input, so malformed values never reach
// ui.Hex, which intentionally panics on programmer errors.
func parseCustomAccent(value string) (ui.Color, bool) {
	if len(value) != 7 || value[0] != '#' {
		return ui.Color{}, false
	}
	rgb, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		return ui.Color{}, false
	}
	return ui.RGB(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb)), true
}

func formatCustomAccent(color ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", color.R, color.G, color.B)
}

func accentColor(value string) (ui.Color, bool) {
	if color, ok := accentChoices[value]; ok {
		return color, true
	}
	return parseCustomAccent(value)
}

// accentText is the text drawn on an accent fill. Arbitrary picker colors need
// a contrast comparison, rather than a hue-specific threshold, to remain legible.
func accentText(color ui.Color) ui.Color {
	linear := func(channel uint8) float64 {
		value := float64(channel) / 255
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	luminance := 0.2126*linear(color.R) + 0.7152*linear(color.G) + 0.0722*linear(color.B)
	if luminance < 0.179 {
		return ui.Hex("#ffffff")
	}
	return ui.Hex("#15171f")
}

// applyAccent paints a palette with the chosen accent color: the button
// fill, its hover and press, the text on it, and the selection and focus
// washes that follow the accent elsewhere.
func applyAccent(t *ui.Theme, color ui.Color) {
	white, black := ui.RGB(255, 255, 255), ui.RGB(0, 0, 0)
	color = ui.RGB(color.R, color.G, color.B)
	t.Accent = color
	t.AccentText = accentText(color)
	if t.Dark {
		t.AccentHover = color.Mix(white, 0.16)
		t.AccentPressed = color.Mix(black, 0.20)
		t.Selection = color.Alpha(0.30)
	} else {
		// A light page keeps the accent readable by darkening it under the
		// pointer rather than washing it out.
		t.AccentHover = color.Mix(black, 0.10)
		t.AccentPressed = color.Mix(black, 0.22)
		t.Selection = color.Alpha(0.22)
	}
	t.Focus = color.Alpha(0.6)
}

// themeForSettings builds the theme of a frame: the chosen palette, or, for
// System, the light or dark base matching the desktop's appearance, with the
// settings' metrics and accent layered on it.
func themeForSettings(s appSettings, systemDark bool) *ui.Theme {
	var t *ui.Theme
	choice, ok := themeChoices[s.Theme]
	if !ok || s.Theme == "System" {
		t = ui.LightTheme()
		if systemDark {
			t = ui.DarkTheme()
		}
	} else {
		t = choice.build()
	}
	if s.FontSize > 0 {
		t.FontSize = float32(s.FontSize)
	}
	if f := strings.TrimSpace(s.Font); f != "" && f != "System" {
		t.Font = f
	}
	if s.Spacing > 0 {
		t.Spacing = float32(s.Spacing)
	}
	t.Radius = float32(s.Radius)
	if color, ok := accentColor(s.Accent); ok {
		applyAccent(t, color)
	}
	return t
}

// fontChoices lists every family the text stack can use. Keep the saved
// family when it is no longer installed so opening Settings does not silently
// change it to another font.
func fontChoices(selected string) []string {
	return fontChoicesFrom(ui.FontFamilies(), selected)
}

func fontChoicesFrom(families []ui.FontFamily, selected string) []string {
	choices := make([]string, 1, len(families)+2)
	choices[0] = "System"
	selected = strings.TrimSpace(selected)
	if selected != "" && selected != "System" {
		choices = append(choices, selected)
	}
	for _, family := range families {
		if name := strings.TrimSpace(family.Name); name != "" {
			choices = append(choices, name)
		}
	}
	sort.SliceStable(choices[1:], func(i, j int) bool {
		return strings.ToLower(choices[i+1]) < strings.ToLower(choices[j+1])
	})
	return slices.Compact(choices)
}
