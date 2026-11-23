package repository

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// LanguageShare is one language's part of the repository's source.
type LanguageShare struct {
	Name  string
	Bytes int64
	Files int
}

// LanguageStats is the source of the checked-out commit counted by language,
// the largest first.
type LanguageStats struct {
	Languages  []LanguageShare
	TotalBytes int64
	TotalFiles int
}

// languageByExtension names the language of a file by its extension. Data,
// images, lock files and the like are deliberately left out: the chart is about
// the code.
var languageByExtension = map[string]string{
	".go": "Go", ".py": "Python", ".pyi": "Python", ".js": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript",
	".jsx": "JavaScript", ".ts": "TypeScript", ".tsx": "TypeScript", ".rs": "Rust", ".rb": "Ruby", ".php": "PHP",
	".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin", ".scala": "Scala", ".swift": "Swift", ".m": "Objective-C",
	".mm": "Objective-C", ".c": "C", ".h": "C", ".cc": "C++", ".cpp": "C++", ".cxx": "C++", ".hpp": "C++",
	".hh": "C++", ".cs": "C#", ".fs": "F#", ".dart": "Dart", ".lua": "Lua", ".pl": "Perl", ".pm": "Perl",
	".r": "R", ".jl": "Julia", ".ex": "Elixir", ".exs": "Elixir", ".erl": "Erlang", ".hs": "Haskell",
	".clj": "Clojure", ".sh": "Shell", ".bash": "Shell", ".zsh": "Shell", ".ps1": "PowerShell", ".bat": "Batchfile",
	".html": "HTML", ".htm": "HTML", ".css": "CSS", ".scss": "SCSS", ".sass": "SCSS", ".less": "Less",
	".vue": "Vue", ".svelte": "Svelte", ".sql": "SQL", ".proto": "Protocol Buffers", ".zig": "Zig",
	".nim": "Nim", ".v": "V", ".sol": "Solidity", ".tf": "HCL", ".md": "Markdown", ".mdx": "Markdown",
	".yml": "YAML", ".yaml": "YAML", ".toml": "TOML", ".json": "JSON", ".xml": "XML", ".gradle": "Gradle",
	".mk": "Makefile", ".cmake": "CMake", ".vim": "Vim Script", ".groovy": "Groovy", ".coffee": "CoffeeScript",
}

// languageByName names the language of a few files that have no extension.
var languageByName = map[string]string{"Makefile": "Makefile", "Dockerfile": "Dockerfile", "CMakeLists.txt": "CMake"}

// skippedLanguageDirs hold other people's or generated code.
var skippedLanguageDirs = map[string]bool{
	"node_modules": true, "vendor": true, "third_party": true, "dist": true, "build": true,
	"__pycache__": true, ".git": true, "testdata": true, "bower_components": true,
}

// languageOfPath is the language a repository path counts as, or "" when it is
// not counted.
func languageOfPath(file string) string {
	parts := strings.Split(file, "/")
	for _, part := range parts[:len(parts)-1] {
		if skippedLanguageDirs[part] {
			return ""
		}
	}
	base := parts[len(parts)-1]
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".min.js") || strings.HasSuffix(lower, ".min.css") || strings.HasSuffix(lower, ".pb.go") ||
		strings.HasSuffix(lower, ".lock") || strings.HasSuffix(lower, "-lock.json") || lower == "package-lock.json" {
		return ""
	}
	if name, ok := languageByName[base]; ok {
		return name
	}
	return languageByExtension[strings.ToLower(path.Ext(base))]
}

// LanguageStats counts the source files of HEAD by language and size, reading
// every blob's size in one `git ls-tree` call.
func (s *gitSession) LanguageStats(ctx context.Context) (LanguageStats, error) {
	if err := ctx.Err(); err != nil {
		return LanguageStats{}, err
	}
	if _, err := s.cmd.New([]string{"git", "rev-parse", "--verify", "--quiet", "HEAD"}).RunWithOutput(); err != nil {
		// No commit yet: nothing to count.
		return LanguageStats{}, nil
	}
	output, err := s.cmd.New([]string{"git", "ls-tree", "-r", "-l", "-z", "HEAD"}).RunWithOutput()
	if err != nil {
		return LanguageStats{}, fmt.Errorf("read languages: %w", err)
	}
	return summarizeLanguages(output), nil
}

// summarizeLanguages aggregates `git ls-tree -r -l -z` output.
func summarizeLanguages(output string) LanguageStats {
	totals := map[string]*LanguageShare{}
	for _, entry := range strings.Split(output, "\x00") {
		meta, file, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 4 || fields[1] != "blob" {
			continue
		}
		size, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || size <= 0 {
			continue
		}
		name := languageOfPath(file)
		if name == "" {
			continue
		}
		share := totals[name]
		if share == nil {
			share = &LanguageShare{Name: name}
			totals[name] = share
		}
		share.Bytes += size
		share.Files++
	}
	stats := LanguageStats{}
	for _, share := range totals {
		stats.Languages = append(stats.Languages, *share)
		stats.TotalBytes += share.Bytes
		stats.TotalFiles += share.Files
	}
	sort.Slice(stats.Languages, func(i, j int) bool {
		if stats.Languages[i].Bytes != stats.Languages[j].Bytes {
			return stats.Languages[i].Bytes > stats.Languages[j].Bytes
		}
		return stats.Languages[i].Name < stats.Languages[j].Name
	})
	return stats
}
