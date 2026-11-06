package app

import (
	"path/filepath"
	"strings"
)

type languageServerProfile struct {
	ID               string
	Name             string
	Category         string
	Description      string
	Program          string
	Arguments        []string
	VersionArguments []string
	Extensions       map[string]string
	Filenames        map[string]string
	EnabledByDefault bool
}

var languageServerProfiles = []languageServerProfile{
	{ID: "typescript", Name: "TypeScript Language Server", Category: "core", Description: "JavaScript and TypeScript navigation, hover, and completion", Program: "typescript-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{
		".js": "javascript", ".jsx": "javascriptreact", ".mjs": "javascript", ".cjs": "javascript",
		".ts": "typescript", ".tsx": "typescriptreact", ".mts": "typescript", ".cts": "typescript",
	}},
	{ID: "html", Name: "VS Code HTML Language Server", Category: "core", Description: "HTML completion, hover, and validation", Program: "vscode-html-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".html": "html", ".htm": "html"}},
	{ID: "css", Name: "VS Code CSS Language Server", Category: "core", Description: "CSS, SCSS, and Less intelligence", Program: "vscode-css-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".css": "css", ".scss": "scss", ".less": "less"}},
	{ID: "json", Name: "VS Code JSON Language Server", Category: "core", Description: "JSON schemas, completion, and validation", Program: "vscode-json-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".json": "json", ".jsonc": "jsonc"}},
	{ID: "tailwind", Name: "Tailwind CSS Language Server", Category: "core", Description: "Tailwind classes, CSS previews, and diagnostics", Program: "tailwindcss-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{
		".html": "html", ".css": "css", ".scss": "scss", ".less": "less", ".vue": "vue",
		".svelte": "svelte", ".astro": "astro", ".js": "javascript", ".jsx": "javascriptreact",
		".ts": "typescript", ".tsx": "typescriptreact",
	}},
	{ID: "eslint", Name: "VS Code ESLint Language Server", Category: "core", Description: "ESLint diagnostics and fixes", Program: "vscode-eslint-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{
		".js": "javascript", ".jsx": "javascriptreact", ".mjs": "javascript", ".cjs": "javascript",
		".ts": "typescript", ".tsx": "typescriptreact", ".vue": "vue", ".svelte": "svelte", ".astro": "astro",
	}},
	{ID: "rust", Name: "Rust Analyzer", Category: "core", Description: "Rust navigation, completion, diagnostics, and refactoring", Program: "rust-analyzer", VersionArguments: []string{"--version"}, Extensions: map[string]string{".rs": "rust"}},
	{ID: "go", Name: "Go (gopls)", Category: "core", Description: "Official Go language server", Program: "gopls", VersionArguments: []string{"version"}, EnabledByDefault: true, Extensions: map[string]string{".go": "go"}},
	{ID: "basedpyright", Name: "BasedPyright", Category: "core", Description: "Python type checking, navigation, and completion", Program: "basedpyright-langserver", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".py": "python", ".pyi": "python"}},
	{ID: "ruff", Name: "Ruff", Category: "core", Description: "Fast Python linting, formatting, and fixes", Program: "ruff", Arguments: []string{"server"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".py": "python", ".pyi": "python"}},
	{ID: "yaml", Name: "YAML Language Server", Category: "core", Description: "YAML schemas, completion, and validation", Program: "yaml-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".yaml": "yaml", ".yml": "yaml"}},
	{ID: "oxlint", Name: "Oxc / Oxlint", Category: "optional", Description: "Oxc lint diagnostics and quick fixes", Program: "oxlint", Arguments: []string{"--lsp"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{
		".js": "javascript", ".jsx": "javascriptreact", ".mjs": "javascript", ".cjs": "javascript",
		".ts": "typescript", ".tsx": "typescriptreact", ".mts": "typescript", ".cts": "typescript",
		".vue": "vuejs", ".svelte": "svelte", ".astro": "astro",
	}},
	{ID: "vue", Name: "Vue Language Server", Category: "optional", Description: "Vue single-file component intelligence", Program: "vue-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".vue": "vue"}},
	{ID: "svelte", Name: "Svelte Language Server", Category: "optional", Description: "Svelte component intelligence", Program: "svelte-language-server", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".svelte": "svelte"}},
	{ID: "astro", Name: "Astro Language Server", Category: "optional", Description: "Astro component intelligence", Program: "astro-ls", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".astro": "astro"}},
	{ID: "bash", Name: "Bash Language Server", Category: "optional", Description: "Bash and shell script intelligence", Program: "bash-language-server", Arguments: []string{"start"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".sh": "shellscript", ".bash": "shellscript", ".zsh": "shellscript"}},
	{ID: "docker", Name: "Dockerfile Language Server", Category: "optional", Description: "Dockerfile completion and validation", Program: "docker-langserver", Arguments: []string{"--stdio"}, VersionArguments: []string{"--version"}, Filenames: map[string]string{"dockerfile": "dockerfile", "containerfile": "dockerfile"}},
	{ID: "marksman", Name: "Marksman", Category: "optional", Description: "Markdown links, symbols, and navigation", Program: "marksman", VersionArguments: []string{"--version"}, Extensions: map[string]string{".md": "markdown", ".markdown": "markdown"}},
}

func languageServerProfileByID(id string) (languageServerProfile, bool) {
	for _, profile := range languageServerProfiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return languageServerProfile{}, false
}

func languageServerProfileEnabled(settings map[string]languageServerSettings, profile languageServerProfile) bool {
	server, configured := settings[profile.ID]
	if !configured {
		return profile.EnabledByDefault
	}
	return !server.Disabled
}

func languageServerProfileForPath(profile languageServerProfile, path string) (string, bool) {
	if language, ok := profile.Filenames[strings.ToLower(filepath.Base(path))]; ok {
		return language, true
	}
	language, ok := profile.Extensions[strings.ToLower(filepath.Ext(path))]
	return language, ok
}

func configuredLanguageServerProfiles(settings map[string]languageServerSettings, path string) []languageServerProfile {
	profiles := make([]languageServerProfile, 0, 3)
	for _, profile := range languageServerProfiles {
		if _, matches := languageServerProfileForPath(profile, path); matches && languageServerProfileEnabled(settings, profile) {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

func languageServerProgram(settings map[string]languageServerSettings, profile languageServerProfile) (string, []string) {
	server := settings[profile.ID]
	program := strings.TrimSpace(server.Path)
	if program == "" {
		program = profile.Program
	}
	arguments := server.Arguments
	if len(arguments) == 0 {
		arguments = profile.Arguments
	}
	return program, append([]string(nil), arguments...)
}
