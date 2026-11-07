package app

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// fileVisual is how a changed file shows its type in a row: the letters of
// its language on a small badge, or the glyph of a family that has no short
// name (data, docs, images, binaries). The colors are mid tones, readable on
// the light and the dark theme alike.
type fileVisual struct {
	icon  *ui.SVG
	label string
	color ui.Color
}

// The colors of the file types. One place, so a language keeps its color
// wherever a file is listed.
var (
	jsColor      = ui.Hex("#d4b106")
	tsColor      = ui.Hex("#3f87c4")
	pyColor      = ui.Hex("#3f7fbf")
	goColor      = ui.Hex("#22a2b8")
	rustColor    = ui.Hex("#c47a3d")
	javaColor    = ui.Hex("#b5563f")
	rubyColor    = ui.Hex("#c43d3d")
	phpColor     = ui.Hex("#6f63b8")
	cColor       = ui.Hex("#6f86c9")
	csharpColor  = ui.Hex("#5c9e5c")
	swiftColor   = ui.Hex("#d1662f")
	kotlinColor  = ui.Hex("#8a6fd1")
	luaColor     = ui.Hex("#4156a3")
	dartColor    = ui.Hex("#2f9bb3")
	shellColor   = ui.Hex("#4f9e5a")
	htmlColor    = ui.Hex("#d1662f")
	cssColor     = ui.Hex("#3f87c4")
	scssColor    = ui.Hex("#c25b8f")
	dataColor    = ui.Hex("#b99a2e")
	docColor     = ui.Hex("#4f8fc4")
	imageColor   = ui.Hex("#9b6fc4")
	archiveColor = ui.Hex("#b8862e")
	dbColor      = ui.Hex("#3f9e86")
	lockColor    = ui.Hex("#b8862e")
	plainColor   = ui.Hex("#8b93a7")
)

// fileVisualsByExt maps a lowercase extension to the badge or glyph of that
// file type. A type with letters wears them; the rest carry an icon.
var fileVisualsByExt = map[string]fileVisual{
	// Scripts and languages.
	"js": {label: "JS", color: jsColor}, "jsx": {label: "JS", color: jsColor},
	"mjs": {label: "JS", color: jsColor}, "cjs": {label: "JS", color: jsColor},
	"ts": {label: "TS", color: tsColor}, "tsx": {label: "TS", color: tsColor},
	"mts": {label: "TS", color: tsColor}, "cts": {label: "TS", color: tsColor},
	"py": {label: "PY", color: pyColor}, "pyw": {label: "PY", color: pyColor},
	"pyi":  {label: "PY", color: pyColor},
	"go":   {label: "GO", color: goColor},
	"rs":   {label: "RS", color: rustColor},
	"java": {label: "JA", color: javaColor}, "scala": {label: "SL", color: javaColor},
	"sc": {label: "SL", color: javaColor},
	"kt": {label: "KT", color: kotlinColor}, "kts": {label: "KT", color: kotlinColor},
	"rb":  {label: "RB", color: rubyColor},
	"php": {label: "PH", color: phpColor},
	"c":   {label: "C", color: cColor}, "h": {label: "C", color: cColor},
	"cc": {label: "C+", color: cColor}, "cpp": {label: "C+", color: cColor},
	"cxx": {label: "C+", color: cColor}, "hpp": {label: "C+", color: cColor},
	"hh": {label: "C+", color: cColor}, "m": {label: "C", color: cColor},
	"mm":    {label: "C+", color: cColor},
	"cs":    {label: "C#", color: csharpColor},
	"swift": {label: "SW", color: swiftColor},
	"lua":   {label: "LU", color: luaColor}, "dart": {label: "DA", color: dartColor},
	"ex": {label: "EX", color: phpColor}, "exs": {label: "EX", color: phpColor},
	"erl": {label: "ER", color: javaColor}, "hrl": {label: "ER", color: javaColor},
	"hs": {label: "HS", color: kotlinColor}, "lhs": {label: "HS", color: kotlinColor},
	"clj": {label: "CL", color: shellColor}, "cljs": {label: "CL", color: shellColor},
	"cljc": {label: "CL", color: shellColor},
	"pl":   {label: "PL", color: cssColor}, "pm": {label: "PL", color: cssColor},
	"r": {label: "R", color: cssColor}, "vim": {label: "VI", color: shellColor},
	"groovy": {label: "GR", color: shellColor},
	// Shell scripts wear the letters of the shell they run in.
	"sh": {label: "SH", color: shellColor}, "bash": {label: "SH", color: shellColor},
	"zsh": {label: "SH", color: shellColor}, "fish": {label: "SH", color: shellColor},
	"ksh": {label: "SH", color: shellColor}, "ps1": {label: "PS", color: shellColor},
	"bat": {label: "CM", color: shellColor}, "cmd": {label: "CM", color: shellColor},
	// Markup and styles.
	"html": {label: "HT", color: htmlColor}, "htm": {label: "HT", color: htmlColor},
	"vue": {label: "VU", color: htmlColor}, "svelte": {label: "SV", color: htmlColor},
	"css": {label: "CS", color: cssColor}, "scss": {label: "SC", color: scssColor},
	"sass": {label: "SC", color: scssColor}, "less": {label: "LE", color: cssColor},
	"styl": {label: "ST", color: scssColor},
	// Data and configuration.
	"json": {icon: fileBracesIcon, color: dataColor}, "jsonc": {icon: fileBracesIcon, color: dataColor},
	"json5": {icon: fileBracesIcon, color: dataColor}, "yaml": {icon: fileBracesIcon, color: dataColor},
	"yml": {icon: fileBracesIcon, color: dataColor}, "toml": {icon: fileBracesIcon, color: dataColor},
	"xml": {icon: fileBracesIcon, color: dataColor}, "xsd": {icon: fileBracesIcon, color: dataColor},
	"xsl": {icon: fileBracesIcon, color: dataColor}, "csv": {icon: fileBracesIcon, color: dataColor},
	"tsv": {icon: fileBracesIcon, color: dataColor},
	"ini": {icon: fileBracesIcon, color: plainColor}, "cfg": {icon: fileBracesIcon, color: plainColor},
	"conf": {icon: fileBracesIcon, color: plainColor}, "cnf": {icon: fileBracesIcon, color: plainColor},
	"properties": {icon: fileBracesIcon, color: plainColor}, "env": {icon: fileBracesIcon, color: plainColor},
	"plist": {icon: fileBracesIcon, color: plainColor}, "reg": {icon: fileBracesIcon, color: plainColor},
	// Docs.
	"md": {icon: fileDocIcon, color: docColor}, "markdown": {icon: fileDocIcon, color: docColor},
	"mdx": {icon: fileDocIcon, color: docColor}, "rst": {icon: fileDocIcon, color: docColor},
	"adoc": {icon: fileDocIcon, color: docColor}, "asciidoc": {icon: fileDocIcon, color: docColor},
	"txt": {icon: fileDocIcon, color: plainColor}, "text": {icon: fileDocIcon, color: plainColor},
	"log": {icon: fileDocIcon, color: plainColor},
	// Images, archives, databases, and binaries.
	"png": {icon: fileImageIcon, color: imageColor}, "jpg": {icon: fileImageIcon, color: imageColor},
	"jpeg": {icon: fileImageIcon, color: imageColor}, "gif": {icon: fileImageIcon, color: imageColor},
	"bmp": {icon: fileImageIcon, color: imageColor}, "webp": {icon: fileImageIcon, color: imageColor},
	"ico": {icon: fileImageIcon, color: imageColor}, "tif": {icon: fileImageIcon, color: imageColor},
	"tiff": {icon: fileImageIcon, color: imageColor}, "psd": {icon: fileImageIcon, color: imageColor},
	"svg": {icon: fileImageIcon, color: imageColor}, "xcf": {icon: fileImageIcon, color: imageColor},
	"zip": {icon: fileArchiveIcon, color: archiveColor}, "tar": {icon: fileArchiveIcon, color: archiveColor},
	"gz": {icon: fileArchiveIcon, color: archiveColor}, "tgz": {icon: fileArchiveIcon, color: archiveColor},
	"bz2": {icon: fileArchiveIcon, color: archiveColor}, "xz": {icon: fileArchiveIcon, color: archiveColor},
	"7z": {icon: fileArchiveIcon, color: archiveColor}, "rar": {icon: fileArchiveIcon, color: archiveColor},
	"zst": {icon: fileArchiveIcon, color: archiveColor}, "cab": {icon: fileArchiveIcon, color: archiveColor},
	"iso": {icon: fileArchiveIcon, color: archiveColor}, "jar": {icon: fileArchiveIcon, color: archiveColor},
	"war": {icon: fileArchiveIcon, color: archiveColor}, "ear": {icon: fileArchiveIcon, color: archiveColor},
	"apk": {icon: fileArchiveIcon, color: archiveColor}, "deb": {icon: fileArchiveIcon, color: archiveColor},
	"rpm": {icon: fileArchiveIcon, color: archiveColor},
	"sql": {label: "SQ", color: dbColor}, "db": {icon: fileDBIcon, color: dbColor},
	"sqlite": {icon: fileDBIcon, color: dbColor}, "sqlite3": {icon: fileDBIcon, color: dbColor},
	"exe": {icon: fileBinaryIcon, color: plainColor}, "dll": {icon: fileBinaryIcon, color: plainColor},
	"so": {icon: fileBinaryIcon, color: plainColor}, "dylib": {icon: fileBinaryIcon, color: plainColor},
	"a": {icon: fileBinaryIcon, color: plainColor}, "o": {icon: fileBinaryIcon, color: plainColor},
	"obj": {icon: fileBinaryIcon, color: plainColor}, "lib": {icon: fileBinaryIcon, color: plainColor},
	"bin": {icon: fileBinaryIcon, color: plainColor}, "dat": {icon: fileBinaryIcon, color: plainColor},
	"class": {icon: fileBinaryIcon, color: plainColor}, "pyc": {icon: fileBinaryIcon, color: plainColor},
	"pyo": {icon: fileBinaryIcon, color: plainColor}, "wasm": {icon: fileBinaryIcon, color: plainColor},
	"node": {icon: fileBinaryIcon, color: plainColor}, "ko": {icon: fileBinaryIcon, color: plainColor},
	"elf":  {icon: fileBinaryIcon, color: plainColor},
	"lock": {icon: fileLockIcon, color: lockColor},
}

// fileVisualsByName maps the files whose name, not extension, names their
// type: build files, dependency manifests, and the like.
var fileVisualsByName = map[string]fileVisual{
	"dockerfile":     {label: "DK", color: cssColor},
	"makefile":       {label: "MK", color: archiveColor},
	"gnumakefile":    {label: "MK", color: archiveColor},
	"cmakelists.txt": {label: "CM", color: cssColor},
	"go.mod":         {label: "GO", color: goColor},
	"go.sum":         {label: "GO", color: goColor},
	"gemfile":        {label: "RB", color: rubyColor},
	"rakefile":       {label: "RB", color: rubyColor},
	"pipfile":        {label: "PY", color: pyColor},
	"gradlew":        {label: "SH", color: shellColor},
	"license":        {icon: fileDocIcon, color: docColor},
	"licence":        {icon: fileDocIcon, color: docColor},
	"readme":         {icon: fileDocIcon, color: docColor},
	"changelog":      {icon: fileDocIcon, color: docColor},
	".gitignore":     {icon: fileIcon, color: plainColor},
	".gitattributes": {icon: fileIcon, color: plainColor},
	".dockerignore":  {icon: fileIcon, color: plainColor},
	".editorconfig":  {icon: fileBracesIcon, color: plainColor},
}

// fileVisualFor names the badge or glyph a changed file wears, from its name
// or its extension. A file whose type is not known wears the plain page in
// the muted color, so the row stays quiet rather than guessing.
func fileVisualFor(path string) fileVisual {
	name := strings.ToLower(filepath.Base(path))
	if visual, ok := fileVisualsByName[name]; ok {
		return visual
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if visual, ok := fileVisualsByExt[ext]; ok {
		return visual
	}
	return fileVisual{icon: fileIcon, color: plainColor}
}

// fileTypeMark draws a file's type where a row leads: a small badge with the
// language's letters, or the glyph of a family that has no such name. The
// badge is tinted from its own color so it reads as one mark rather than as
// part of the path.
//
// Every mark takes the same width, the badge's, with the glyph centered in it,
// so the paths of a list start at one edge whether their rows lead with
// letters or a glyph.
//
// On a chosen row the mark takes the text color of the selection, as the path
// does, so it stays legible on the accent fill rather than carrying a color
// meant for the surface behind it.
func fileTypeMark(c *ui.Context, path string, size float32, selected bool) {
	visual := fileVisualFor(path)
	color := visual.color
	if selected {
		color = c.Theme().AccentText
	}
	slot := ui.Box(c).Size(size+4, size).Center()
	// The chip's tint belongs to the surface behind it, so a chosen row shows
	// the letters alone in the selection's text color, and a glyph never takes
	// a chip at all.
	if !selected && visual.label != "" {
		slot = slot.Radius(3).Background(visual.color.Alpha(0.16))
	}
	slot.Children(func() {
		if visual.label == "" {
			ui.Icon(c, visual.icon).FontSize(size).TextColor(color)
			return
		}
		ui.Text(c, visual.label).FontSize(size * 0.58).Bold().TextColor(color)
	})
}
