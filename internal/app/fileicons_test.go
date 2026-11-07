package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// A file's mark names its language when there is a short name for it, so a
// script reads as JS, TS, PY, or GO at a glance. Families without such a name
// (data, docs, images, archives) keep their glyph, in the color of the type.
func TestFileVisualsFollowTheFileType(t *testing.T) {
	cases := []struct {
		path  string
		label string
		icon  string
		color ui.Color
	}{
		{"app.js", "JS", "", jsColor},
		{"App.TSX", "TS", "", tsColor},
		{"tool.py", "PY", "", pyColor},
		{"main.go", "GO", "", goColor},
		{"lib.rs", "RS", "", rustColor},
		{"run.sh", "SH", "", shellColor},
		{"index.html", "HT", "", htmlColor},
		{"style.scss", "SC", "", scssColor},
		{"query.sql", "SQ", "", dbColor},
		{"data.json", "", "braces", dataColor},
		{"config.yaml", "", "braces", dataColor},
		{"notes.md", "", "doc", docColor},
		{"LICENSE", "", "doc", docColor},
		{"logo.png", "", "image", imageColor},
		{"bundle.tar.gz", "", "archive", archiveColor},
		{"server.bin", "", "binary", plainColor},
		{"Makefile", "MK", "", archiveColor},
		{"go.mod", "GO", "", goColor},
		{"Dockerfile", "DK", "", cssColor},
		{"yarn.lock", "", "lock", lockColor},
	}
	for _, test := range cases {
		t.Run(test.path, func(t *testing.T) {
			visual := fileVisualFor(test.path)
			if visual.label != test.label {
				t.Errorf("%s wears the %q badge, want %q", test.path, visual.label, test.label)
			}
			if got := iconName(visual.icon); got != test.icon {
				t.Errorf("%s wears the %s glyph, want %s", test.path, got, test.icon)
			}
			if visual.color != test.color {
				t.Errorf("%s is %v, want %v", test.path, visual.color, test.color)
			}
		})
	}

	// A type no rule names keeps the quiet page rather than guessing.
	unknown := fileVisualFor("archive.weird")
	if unknown.label != "" || iconName(unknown.icon) != "file" || unknown.color != plainColor {
		t.Fatalf("an unknown type wears %q / %s in %v", unknown.label, iconName(unknown.icon), unknown.color)
	}
}

// A list's paths all start at the same edge, whether their rows lead with a
// language badge or a glyph: the mark's slot is the badge's width for both.
func TestFileTypeMarksShareOneWidth(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			for _, path := range []string{"main.go", "docs/notes.md"} {
				ui.Row(c).Gap(8).Children(func() {
					fileTypeMark(c, path, 13, false)
					ui.Text(c, path)
				})
			}
		})
	}, 400, 100)
	tt.Frame()
	badge, ok := tt.Find("main.go")
	if !ok {
		t.Fatalf("the badge row drew no path: %v", tt.Texts())
	}
	glyph, ok := tt.Find("docs/notes.md")
	if !ok {
		t.Fatalf("the glyph row drew no path: %v", tt.Texts())
	}
	if badge.X != glyph.X {
		t.Errorf("paths start at %v with a badge and %v with a glyph", badge.X, glyph.X)
	}
	if badge.W <= 0 || glyph.W <= 0 {
		t.Errorf("paths have no width: %v and %v", badge, glyph)
	}
}

// A badge and a glyph are mutually exclusive: a file shows one mark, not two.
func TestFileVisualHasOneMark(t *testing.T) {
	for _, path := range []string{"a.js", "a.json", "a.md", "a.png", "a.zip", "a.bin", "a.lock", "a.weird"} {
		visual := fileVisualFor(path)
		if visual.label == "" && visual.icon == nil {
			t.Errorf("%s has no mark", path)
		}
		if visual.label != "" && visual.icon != nil {
			t.Errorf("%s has both a badge and a glyph", path)
		}
	}
}

// iconName names the glyph for the test's messages, so a failure reads as the
// family rather than a pointer.
func iconName(icon *ui.SVG) string {
	switch icon {
	case nil:
		return ""
	case fileCodeIcon:
		return "code"
	case fileBracesIcon:
		return "braces"
	case fileDocIcon:
		return "doc"
	case fileImageIcon:
		return "image"
	case fileArchiveIcon:
		return "archive"
	case fileBinaryIcon:
		return "binary"
	case fileDBIcon:
		return "db"
	case fileStyleIcon:
		return "style"
	case fileLockIcon:
		return "lock"
	case terminalIcon:
		return "terminal"
	case fileIcon:
		return "file"
	}
	return "unknown"
}
