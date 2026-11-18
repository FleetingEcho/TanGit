package app

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTreeGuideGroupsFollowActualVisibleBranches(t *testing.T) {
	rows := buildFileTreeRows([]string{
		"src/ui/tree/tree.go",
		"src/ui/app.go",
		"src/main.go",
		"other/nested/file.go",
		"other/sibling.go",
		"root.go",
	}, true, nil, "test")
	groups := treeGuideGroups(rows)
	indexes := make(map[string]int, len(rows))
	for index, row := range rows {
		indexes[row.path] = index
	}
	assertGroup := func(path, last string) {
		t.Helper()
		group, ok := groups[path]
		if !ok {
			t.Fatalf("missing parent branch %q in %#v", path, groups)
		}
		if got, want := group.parent, indexes[path]; got != want {
			t.Errorf("%q parent row = %d, want %d", path, got, want)
		}
		if got, want := group.end, indexes[last]; got != want {
			t.Errorf("%q ends at row %d, want %d (%q)", path, got, want, last)
		}
		if group.depth != rows[group.parent].depth {
			t.Errorf("%q group depth = %d, row depth = %d", path, group.depth, rows[group.parent].depth)
		}
	}
	assertGroup("src", "src/main.go")
	assertGroup("src/ui", "src/ui/app.go")
	assertGroup("src/ui/tree", "src/ui/tree/tree.go")
	assertGroup("other", "other/sibling.go")
	assertGroup("other/nested", "other/nested/file.go")

	// A real top-level directory has no incoming guide. It only starts an
	// outgoing rail when it has visible descendants.
	src := rows[indexes["src"]]
	segments := groups.segments(src, indexes["src"])
	wantOutgoing := treeGuideSegment{X1: 11, Y1: 0.5, X2: 11, Y2: 1}
	if !slices.Contains(segments, wantOutgoing) {
		t.Fatalf("root's outgoing branch = %+v, want %+v", segments, wantOutgoing)
	}
	for _, segment := range segments {
		if segment.Y1 == segment.Y2 {
			t.Fatalf("root acquired an incoming connector: %+v", segments)
		}
	}
	empty := buildFileTreeRowsWithDirectories(nil, []string{"empty"}, true, nil, "test")
	if groups := treeGuideGroups(empty); len(groups) != 0 || len(groups.segments(empty[0], 0)) != 0 {
		t.Fatalf("childless root acquired guide chrome: groups=%v rows=%v", groups, empty)
	}

	// The nested UI branch terminates independently while src continues to
	// main.go. Both the absolute parent range and the local end are explicit.
	uiIndex := indexes["src/ui"]
	uiSegments := groups.segments(rows[uiIndex], uiIndex)
	for _, want := range []treeGuideSegment{
		{X1: 11, X2: 11, Y2: 1},
		{X1: 12, Y1: 0.5, X2: 17, Y2: 0.5},
		{X1: 23, Y1: 0.5, X2: 23, Y2: 1},
	} {
		if !slices.Contains(uiSegments, want) {
			t.Errorf("adjacent nested directory missed %+v in %+v", want, uiSegments)
		}
	}
	lastUI := indexes["src/ui/app.go"]
	lastUISegments := groups.segments(rows[lastUI], lastUI)
	if !slices.Contains(lastUISegments, treeGuideSegment{X1: 11, X2: 11, Y2: 1}) {
		t.Errorf("src stopped with its nested sibling: %+v", lastUISegments)
	}
	if !slices.Contains(lastUISegments, treeGuideSegment{X1: 23, X2: 23, Y2: 0.5}) {
		t.Errorf("ui did not end at its own last descendant: %+v", lastUISegments)
	}
	for _, segment := range lastUISegments {
		if segment.X1 == 35 && segment.X2 == 35 {
			t.Errorf("already terminated tree sibling leaked into app.go: %+v", lastUISegments)
		}
	}
}

func TestTreeGuideGroupsCollapseOnlyTheirOwnBranch(t *testing.T) {
	paths := []string{
		"src/ui/tree/tree.go",
		"src/ui/app.go",
		"src/main.go",
		"other/nested/file.go",
		"root.go",
	}
	rows := buildFileTreeRows(paths, true, map[string]bool{
		fileTreeCollapseKey("test", "src/ui"): true,
	}, "test")
	if !slices.Contains(visibleCommitTreePaths(rows), "src/main.go") ||
		slices.Contains(visibleCommitTreePaths(rows), "src/ui/app.go") {
		t.Fatalf("collapsing ui hid the wrong rows: %v", visibleCommitTreePaths(rows))
	}
	groups := treeGuideGroups(rows)
	if _, ok := groups["src/ui"]; ok {
		t.Fatal("a collapsed parent retained a descendant rail")
	}
	srcIndex := slices.IndexFunc(rows, func(row fileTreeRow) bool { return row.path == "src" })
	mainIndex := slices.IndexFunc(rows, func(row fileTreeRow) bool { return row.path == "src/main.go" })
	if got := groups["src"]; got.parent != srcIndex || got.end != mainIndex {
		t.Fatalf("outer branch did not stop at its remaining child: %+v", got)
	}
	if _, ok := groups["other/nested"]; !ok {
		t.Fatal("collapsing ui hid an unrelated parent's guide")
	}

	rows = buildFileTreeRows(paths, true, map[string]bool{
		fileTreeCollapseKey("test", "src"): true,
	}, "test")
	visible := visibleCommitTreePaths(rows)
	if slices.Contains(visible, "src/main.go") || slices.Contains(visible, "src/ui") ||
		!slices.Contains(visible, "other/nested/file.go") {
		t.Fatalf("collapsing src hid more than its branch: %v", visible)
	}
	groups = treeGuideGroups(rows)
	if _, ok := groups["src"]; ok {
		t.Fatal("collapsed src retained a rail")
	}
	if _, ok := groups["other/nested"]; !ok {
		t.Fatal("collapsed src removed the other parent's rail")
	}
}

func TestTreeGuideColorKeepsSelectionContrast(t *testing.T) {
	theme := ui.DarkTheme()
	normal := treeGuideColor(theme, false)
	selected := treeGuideColor(theme, true)
	if selected.A <= normal.A {
		t.Fatalf("selected guides must be at least as legible as normal guides: normal=%v selected=%v", normal.A, selected.A)
	}
	if selected.R != theme.AccentText.R || selected.G != theme.AccentText.G || selected.B != theme.AccentText.B {
		t.Fatalf("selected guide = %v, want AccentText %v", selected, theme.AccentText)
	}
	ink := blendTreeGuidePixel(selected, theme.Accent)
	background := color.RGBA{R: theme.Accent.R, G: theme.Accent.G, B: theme.Accent.B, A: 255}
	if contrast := treeGuideContrast(ink, background); contrast < 1.25 {
		t.Fatalf("selected guide contrast = %.2f, want at least 1.25", contrast)
	}
}

const (
	treeGuideTestListX = 20
	treeGuideTestListY = 16
)

type treeGuideFixture struct {
	app      *app
	tester   *ui.Tester
	rows     []fileTreeRow
	selected int
	list     ui.ListState
}

func newTreeGuideFixture(paths []string, selected, width, listHeight int) *treeGuideFixture {
	f := &treeGuideFixture{app: newApp(fakeOpener{}), selected: selected}
	f.rows = buildFileTreeRows(paths, true, nil, "test")
	f.list.Selected = &f.selected
	f.list.Key = func(row int) any { return fileTreeKey(f.rows, row) }
	f.list.Label = func(row int) string { return fileTreeLabel(f.rows, row) }
	windowHeight := treeGuideTestListY + listHeight + 40
	f.tester = ui.NewTesterWith(func(c *ui.Context) {
		f.rows = buildFileTreeRows(paths, true, f.app.fileTreeCollapsed, "test")
		guides := treeGuideGroups(f.rows)
		ui.Box(c).Fill().Background(c.Theme().Background).Children(func() {
			ui.List(c, &f.list, len(f.rows)).Absolute().
				Left(treeGuideTestListX).Top(treeGuideTestListY).Size(float32(width-2*treeGuideTestListX), float32(listHeight)).
				Rows(func(row ui.ListRow) {
					item := f.rows[row.Index]
					var element ui.Element
					if item.isDirectory() {
						element = f.app.fileTreeDirectoryRow(row.Context, "test", item, row.Selected(), func() {
							f.selected = row.Index
						})
					} else {
						fileRow := ui.Row(row.Context).Gap(8).AlignItems(ui.Center).
							Padding(6, 8, 6, float32(8+treeIndentStep*item.depth)).Label("file " + item.path)
						fileRow.Children(func() {
							fileTypeMark(row.Context, item.path, 13, row.Selected())
							ui.Text(row.Context, item.name).TextColor(secondaryRowColor(row.Context.Theme(), row)).SingleLine()
						})
						element = fileRow
					}
					// Deliberately vary adjacent row heights: normalized intervals
					// must meet at the same absolute Y, not assume a fixed 64 DIP stroke.
					guides.decorate(element, item, row).Height(float32(20 + 4*(row.Index%3)))
				})
		})
	}, width, windowHeight, ui.TesterOptions{Dark: true})
	return f
}

func (f *treeGuideFixture) rowRect(t *testing.T, path string) ui.Rect {
	t.Helper()
	index := slices.IndexFunc(f.rows, func(row fileTreeRow) bool { return row.path == path })
	if index < 0 {
		t.Fatalf("row %q is not visible: %v", path, visibleCommitTreePaths(f.rows))
	}
	label := "file " + path
	if f.rows[index].isDirectory() {
		action := "Collapse directory"
		if f.app.fileTreeCollapsed[fileTreeCollapseKey("test", path)] {
			action = "Expand directory"
		}
		label = action + " " + path
	}
	r, ok := f.tester.Find(label)
	if !ok {
		t.Fatalf("could not find row %q by label %q", path, label)
	}
	return r
}

func rowCenter(r ui.Rect) int { return int(r.Y + r.H/2) }

func saveTreeGuidePNG(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	directory := os.Getenv("TANGIT_TREE_GUIDE_PNG_DIR")
	if directory == "" {
		return
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name+".png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, img); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("tree guide image: %s", path)
}

func treeGuidePixelDistance(a, b color.RGBA) int {
	difference := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return difference(a.R, b.R) + difference(a.G, b.G) + difference(a.B, b.B) + difference(a.A, b.A)
}

func blendTreeGuidePixel(ink, background ui.Color) color.RGBA {
	alpha := uint32(ink.A)
	blend := func(foreground, back uint8) uint8 {
		return uint8((uint32(foreground)*alpha + uint32(back)*(255-alpha) + 127) / 255)
	}
	return color.RGBA{R: blend(ink.R, background.R), G: blend(ink.G, background.G), B: blend(ink.B, background.B), A: 255}
}

func treeGuideLuminance(c color.RGBA) float64 {
	channel := func(v uint8) float64 {
		srgb := float64(v) / 255
		if srgb <= 0.04045 {
			return srgb / 12.92
		}
		return math.Pow((srgb+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

func treeGuideContrast(a, b color.RGBA) float64 {
	left, right := treeGuideLuminance(a), treeGuideLuminance(b)
	if left < right {
		left, right = right, left
	}
	return (left + 0.05) / (right + 0.05)
}

func TestTreeGuideRenderingHasAbsoluteYContinuityAndIndependentEnds(t *testing.T) {
	paths := []string{
		"src/ui/tree/tree.go",
		"src/ui/app.go",
		"src/main.go",
		"other/nested/file.go",
		"other/sibling.go",
		"root.go",
	}
	selectedRow := slices.IndexFunc(buildFileTreeRows(paths, true, nil, "test"), func(row fileTreeRow) bool {
		return row.path == "src/ui"
	})
	f := newTreeGuideFixture(paths, selectedRow, 280, 320)
	f.tester.Frame()
	img := f.tester.Image()
	saveTreeGuidePNG(t, "expanded-selected", img)

	srcRail := treeGuideTestListX + 11
	uiRail := treeGuideTestListX + 23
	backgroundX := 280 - treeGuideTestListX - 10
	srcRect := f.rowRect(t, "src")
	start := rowCenter(srcRect)
	srcEnd := rowCenter(f.rowRect(t, "src/main.go"))
	uiEnd := rowCenter(f.rowRect(t, "src/ui/app.go"))
	// Sample below the parent's chevron, which intentionally occupies the
	// same compact 12-DIP inset at the branch's midpoint.
	normalSampleY := start + 8
	normalBackground := img.RGBAAt(backgroundX, normalSampleY)
	normalInk := img.RGBAAt(srcRail, normalSampleY)
	if treeGuidePixelDistance(normalInk, normalBackground) < 12 {
		t.Fatalf("no normal guide at (%d,%d): ink=%v background=%v", srcRail, normalSampleY, normalInk, normalBackground)
	}
	for y := start; y <= srcEnd; y++ {
		ink, background := img.RGBAAt(srcRail, y), img.RGBAAt(backgroundX, y)
		if treeGuidePixelDistance(ink, background) < 12 {
			t.Fatalf("src rail breaks at absolute Y=%d: ink=%v background=%v", y, ink, background)
		}
		// Identical normal backgrounds must produce the exact same single-alpha
		// pixel; duplicate strokes at row boundaries would darken this value.
		if y >= normalSampleY && background == normalBackground && ink != normalInk {
			t.Fatalf("src rail overlaps at absolute Y=%d: ink=%v, one-stroke reference=%v", y, ink, normalInk)
		}
	}
	if y := uiEnd + 2; treeGuidePixelDistance(img.RGBAAt(uiRail, y), img.RGBAAt(backgroundX, y)) >= 12 {
		t.Fatalf("ui rail passed its last descendant at Y=%d", y)
	}
	if y := uiEnd + 2; treeGuidePixelDistance(img.RGBAAt(srcRail, y), img.RGBAAt(backgroundX, y)) < 12 {
		t.Fatalf("src incorrectly terminated with ui at Y=%d", y)
	}
	if y := srcEnd + 2; treeGuidePixelDistance(img.RGBAAt(srcRail, y), img.RGBAAt(backgroundX, y)) >= 12 {
		t.Fatalf("src rail passed its last descendant at Y=%d", y)
	}

	selected := f.rowRect(t, "src/ui")
	y := int(selected.Y+selected.H) - 2
	selectedInk, selectedBackground := img.RGBAAt(uiRail, y), img.RGBAAt(backgroundX, y)
	if contrast := treeGuideContrast(selectedInk, selectedBackground); contrast < 1.25 {
		t.Fatalf("selected guide contrast = %.2f at Y=%d: ink=%v background=%v", contrast, y, selectedInk, selectedBackground)
	}

	f.tester.SetScale(2)
	img = f.tester.Image()
	saveTreeGuidePNG(t, "expanded-selected-2x", img)
	y = normalSampleY * 2
	for dy := range 2 {
		for dx := range 2 {
			ink := img.RGBAAt(srcRail*2+dx, y+dy)
			background := img.RGBAAt(backgroundX*2, y+dy)
			if treeGuidePixelDistance(ink, background) < 12 {
				t.Fatalf("one-DIP rail lost device pixel (%d,%d): ink=%v background=%v", dx, dy, ink, background)
			}
		}
		if ink, background := img.RGBAAt(srcRail*2+2, y+dy), img.RGBAAt(backgroundX*2, y+dy); ink != background {
			t.Fatalf("one-DIP vertical rail is wider than two device pixels at dy=%d: %v != %v", dy, ink, background)
		}
	}
}

func TestTreeGuideDirectoryClickFoldsAndKeepsSelection(t *testing.T) {
	paths := []string{
		"src/ui/tree/tree.go",
		"src/ui/app.go",
		"src/main.go",
		"other/nested/file.go",
		"root.go",
	}
	f := newTreeGuideFixture(paths, 1, 280, 300)
	if err := f.tester.Click("Collapse directory src/ui"); err != nil {
		t.Fatal(err)
	}
	f.tester.Frame()
	visible := visibleCommitTreePaths(f.rows)
	if slices.Contains(visible, "src/ui/app.go") || slices.Contains(visible, "src/ui/tree") ||
		!slices.Contains(visible, "src/main.go") || !slices.Contains(visible, "other/nested/file.go") {
		t.Fatalf("directory control hid the wrong branch: %v", visible)
	}
	if got := selectedFileTreeDirectory(f.rows, f.selected); got != "src/ui" {
		t.Fatalf("directory click selection = %q, want src/ui", got)
	}
	saveTreeGuidePNG(t, "collapsed-ui", f.tester.Image())
	if err := f.tester.Click("Expand directory src/ui"); err != nil {
		t.Fatal(err)
	}
	f.tester.Frame()
	if !slices.Contains(visibleCommitTreePaths(f.rows), "src/ui/app.go") {
		t.Fatal("directory control did not restore its descendants")
	}
}

func TestTreeGuideRenderingSurvivesVirtualizationAndClipsToList(t *testing.T) {
	paths := []string{"root/child/grand/file.go"}
	for index := range 32 {
		paths = append(paths, fmt.Sprintf("root/item%02d.go", index))
	}
	paths = append(paths, "sibling/file.go", "top.go")
	f := newTreeGuideFixture(paths, 0, 280, 120)
	last := slices.IndexFunc(f.rows, func(row fileTreeRow) bool { return row.path == "root/item25.go" })
	if last < 0 {
		t.Fatal("scroll target missing")
	}
	f.list.ScrollTo(last, ui.Start)
	f.tester.Frame()
	if f.tester.HasText("child") {
		t.Fatal("scroll target did not virtualize the off-screen parent directories")
	}
	img := f.tester.Image()
	saveTreeGuidePNG(t, "scrolled-clipped", img)
	rail := treeGuideTestListX + 11
	background := img.RGBAAt(0, 0)
	bottom := treeGuideTestListY + 120
	inside := 0
	for y := treeGuideTestListY; y < bottom; y++ {
		if treeGuidePixelDistance(img.RGBAAt(rail, y), img.RGBAAt(260, y)) >= 12 {
			inside++
		}
	}
	if inside < 40 {
		t.Fatalf("off-screen parent's descendants lost their rail: only %d visible pixels", inside)
	}
	for y := 0; y < img.Bounds().Dy(); y++ {
		if y >= treeGuideTestListY && y < bottom {
			continue
		}
		if got := img.RGBAAt(rail, y); treeGuidePixelDistance(got, background) != 0 {
			t.Fatalf("guide escaped list content at (%d,%d): %v, background %v", rail, y, got, background)
		}
	}

	f.tester.SetSize(300, 210)
	img = f.tester.Image()
	for y := bottom; y < img.Bounds().Dy(); y++ {
		if got := img.RGBAAt(rail, y); treeGuidePixelDistance(got, img.RGBAAt(0, 0)) != 0 {
			t.Fatalf("resized window exposed guide below list at (%d,%d): %v", rail, y, got)
		}
	}
}
