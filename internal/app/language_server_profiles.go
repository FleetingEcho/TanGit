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
	// ProtocolProbe is for servers whose published CLI has no version flag.
	// Their Test action performs a bounded real LSP initialize instead.
	ProtocolProbe    bool
	PullDiagnostics  bool
	Extensions       map[string]string
	Filenames        map[string]string
	EnabledByDefault bool
	DownloadURL      string
	Custom           bool
}

// languageServerDownloadURLs identifies the upstream package or release used
// by each managed installer, so users can inspect it before downloading.
var languageServerDownloadURLs = map[string]string{
	"typescript":   "https://www.npmjs.com/package/typescript",
	"html":         "https://www.npmjs.com/package/@t1ckbase/vscode-langservers-extracted",
	"css":          "https://www.npmjs.com/package/@t1ckbase/vscode-langservers-extracted",
	"json":         "https://www.npmjs.com/package/@t1ckbase/vscode-langservers-extracted",
	"tailwind":     "https://www.npmjs.com/package/@tailwindcss/language-server",
	"eslint":       "https://www.npmjs.com/package/@t1ckbase/vscode-langservers-extracted",
	"rust":         "https://rust-analyzer.github.io/",
	"go":           "https://pkg.go.dev/golang.org/x/tools/gopls",
	"ty":           "https://pypi.org/project/ty/",
	"basedpyright": "https://www.npmjs.com/package/basedpyright",
	"ruff":         "https://pypi.org/project/ruff/",
	"yaml":         "https://www.npmjs.com/package/yaml-language-server",
	"oxlint":       "https://www.npmjs.com/package/oxlint",
	"vue":          "https://www.npmjs.com/package/@vue/language-server",
	"svelte":       "https://www.npmjs.com/package/svelte-language-server",
	"astro":        "https://www.npmjs.com/package/@astrojs/language-server",
	"bash":         "https://www.npmjs.com/package/bash-language-server",
	"docker":       "https://www.npmjs.com/package/dockerfile-language-server-nodejs",
	"marksman":     "https://github.com/artempyanykh/marksman/releases/latest",
	"clangd":       "https://github.com/clangd/clangd/releases/latest",
	"lua":          "https://github.com/LuaLS/lua-language-server/releases/latest",
	"zls":          "https://github.com/zigtools/zls/releases/latest",
	"tombi":        "https://github.com/tombi-toml/tombi/releases/latest",
	"neocmake":     "https://github.com/neocmakelsp/neocmakelsp/releases/latest",
	"terraform":    "https://github.com/hashicorp/terraform-ls",
	"intelephense": "https://www.npmjs.com/package/intelephense",
	"prisma":       "https://www.npmjs.com/package/@prisma/language-server",
	"ruby":         "https://rubygems.org/gems/ruby-lsp",
	"csharp":       "https://www.nuget.org/packages/csharp-ls",
}

var languageServerProfiles = []languageServerProfile{
	{ID: "typescript", Name: "TypeScript Language Server", Category: "core", Description: "Native TypeScript 7 navigation, hover, and completion", Program: "tsc", Arguments: []string{"--lsp", "--stdio"}, VersionArguments: []string{"--version"}, EnabledByDefault: true, Extensions: map[string]string{
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
	{ID: "ty", Name: "ty", Category: "core", Description: "Python type checking, navigation, and completion; pairs with Ruff", Program: "ty", Arguments: []string{"server"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".py": "python", ".pyi": "python"}},
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
	{ID: "clangd", Name: "clangd", Category: "core", Description: "C and C++ completion, navigation, and diagnostics", Program: "clangd", VersionArguments: []string{"--version"}, Extensions: map[string]string{
		".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".c++": "cpp", ".hh": "cpp", ".hpp": "cpp", ".hxx": "cpp", ".h++": "cpp", ".m": "objective-c", ".mm": "objective-cpp", ".cu": "cuda-cpp",
	}},
	{ID: "lua", Name: "Lua Language Server", Category: "core", Description: "Lua completion, hover, diagnostics, and workspace symbols", Program: "lua-language-server", VersionArguments: []string{"--version"}, Extensions: map[string]string{".lua": "lua"}},
	{ID: "zls", Name: "Zig Language Server", Category: "optional", Description: "Zig completion, navigation, and diagnostics", Program: "zls", VersionArguments: []string{"--version"}, Extensions: map[string]string{".zig": "zig", ".zon": "zig"}},
	{ID: "tombi", Name: "Tombi", Category: "core", Description: "TOML schemas, completion, hover, and validation", Program: "tombi", Arguments: []string{"lsp"}, VersionArguments: []string{"--version"}, Extensions: map[string]string{".toml": "toml"}},
	{ID: "neocmake", Name: "NeoCMakeLSP", Category: "optional", Description: "CMake completion, hover, and diagnostics", Program: "neocmakelsp", Arguments: []string{"stdio"}, VersionArguments: []string{"--version"}, PullDiagnostics: true, Extensions: map[string]string{".cmake": "cmake"}, Filenames: map[string]string{"cmakelists.txt": "cmake"}},
	{ID: "terraform", Name: "Terraform Language Server", Category: "optional", Description: "Terraform completion, hover, and validation", Program: "terraform-ls", Arguments: []string{"serve"}, VersionArguments: []string{"-v"}, Extensions: map[string]string{".tf": "terraform", ".tfvars": "terraform-vars"}},
	{ID: "intelephense", Name: "Intelephense", Category: "core", Description: "PHP completion, navigation, and diagnostics; some features require a license", Program: "intelephense", Arguments: []string{"--stdio"}, ProtocolProbe: true, Extensions: map[string]string{".php": "php", ".phtml": "php"}},
	{ID: "prisma", Name: "Prisma Language Server", Category: "optional", Description: "Prisma schema completion, hover, formatting, and diagnostics", Program: "prisma-language-server", Arguments: []string{"--stdio"}, ProtocolProbe: true, Extensions: map[string]string{".prisma": "prisma"}},
	{ID: "ruby", Name: "Ruby LSP", Category: "optional", Description: "Ruby navigation, completion, diagnostics, and inline hints", Program: "ruby-lsp", VersionArguments: []string{"--version"}, Extensions: map[string]string{".rb": "ruby", ".rake": "ruby", ".gemspec": "ruby", ".ru": "ruby"}, Filenames: map[string]string{"gemfile": "ruby", "rakefile": "ruby", "guardfile": "ruby", "vagrantfile": "ruby"}},
	{ID: "csharp", Name: "C# Language Server", Category: "optional", Description: "C# project navigation, completion, and diagnostics; requires .NET", Program: "csharp-ls", VersionArguments: []string{"--version"}, Extensions: map[string]string{".cs": "csharp", ".csx": "csharp"}},
}

// languageServerProfileDescription is the presentation boundary for catalog
// descriptions. Built-in descriptions are stable English message keys; custom
// descriptions are user text and must pass through unchanged. Keep the profile
// itself canonical so localization never mutates IDs, settings, or launch data.
func languageServerProfileDescription(profile languageServerProfile) string {
	if profile.Custom {
		return profile.Description
	}
	return tr(profile.Description)
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
	for _, profile := range languageServerProfilesWithSettings(settings) {
		if _, matches := languageServerProfileForPath(profile, path); matches && languageServerProfileEnabled(settings, profile) {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

// languageServerExecutableCandidates returns configured executables in the
// order the user wrote them. A blank list retains the profile's PATH default.
func languageServerExecutableCandidates(configured, fallback string) []string {
	var programs []string
	for _, program := range strings.Split(configured, ",") {
		if program = strings.TrimSpace(program); program != "" {
			programs = append(programs, program)
		}
	}
	if len(programs) == 0 {
		return []string{fallback}
	}
	return programs
}

func languageServerProgram(settings map[string]languageServerSettings, profile languageServerProfile) (string, []string) {
	server := settings[profile.ID]
	program := languageServerExecutableCandidates(server.Path, profile.Program)[0]
	arguments := server.Arguments
	if len(arguments) == 0 {
		arguments = profile.Arguments
	}
	return program, append([]string(nil), arguments...)
}
