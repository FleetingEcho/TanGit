package app

import (
	"errors"
	"io/fs"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// A two-way conflict is found from its four markers, and each side splices
// over the whole conflict: markers included, nothing else touched.
func TestConflictStatePicksASide(t *testing.T) {
	content := "first\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	state := newConflictState("file.txt", content, 0)
	if len(state.regions) != 1 {
		t.Fatalf("regions = %#v, want one", state.regions)
	}
	region := state.regions[0]
	if region.start != 1 || region.split != 3 || region.end != 5 || region.hasBase() {
		t.Fatalf("region = %#v", region)
	}

	if !state.resolve(conflictCurrent) {
		t.Fatal("picking the current side changed nothing")
	}
	if got, want := state.text(), "first\nours\nlast\n"; got != want {
		t.Fatalf("current side = %q, want %q", got, want)
	}
	if len(state.regions) != 0 {
		t.Fatalf("the conflict survived its resolution: %#v", state.regions)
	}

	if !state.undoResolution() {
		t.Fatal("undo changed nothing")
	}
	if state.text() != content {
		t.Fatalf("undo = %q, want the original", state.text())
	}

	if !state.resolve(conflictIncoming) {
		t.Fatal("picking the incoming side changed nothing")
	}
	if got, want := state.text(), "first\ntheirs\nlast\n"; got != want {
		t.Fatalf("incoming side = %q, want %q", got, want)
	}
}

// The diff3 style adds the common ancestor between the sides; both drops it,
// and picking the ancestor is only offered while it is there.
func TestConflictStateDiff3(t *testing.T) {
	content := "<<<<<<< HEAD\nours\n||||||| base\ncommon\n=======\ntheirs\n>>>>>>> topic\n"
	state := newConflictState("file.txt", content, 0)
	if len(state.regions) != 1 {
		t.Fatalf("regions = %#v, want one", state.regions)
	}
	if !state.regions[0].hasBase() {
		t.Fatal("the ancestor marker was not found")
	}

	if !state.resolve(conflictBoth) {
		t.Fatal("picking both changed nothing")
	}
	if got, want := state.text(), "ours\ntheirs\n"; got != want {
		t.Fatalf("both = %q, want %q", got, want)
	}

	state = newConflictState("file.txt", content, 0)
	if !state.resolve(conflictBase) {
		t.Fatal("picking the ancestor changed nothing")
	}
	if got, want := state.text(), "common\n"; got != want {
		t.Fatalf("base = %q, want %q", got, want)
	}
}

// The two editable sides of a conflict carry the same deletion/addition
// colors as a diff; unchanged lines keep the ordinary editor background.
func TestConflictLineBackgroundsTintBothSides(t *testing.T) {
	content := "before\n<<<<<<< HEAD\nours one\nours two\n||||||| base\nancestor\n=======\ntheirs\n>>>>>>> topic\nafter\n"
	state := newConflictState("file.go", content, 0)
	theme := ui.DarkTheme()
	backgrounds := state.lineBackgrounds(theme)
	at := func(line int) (ui.Color, bool) {
		for _, background := range backgrounds {
			if line >= background.First && line <= background.Last {
				return background.Color, true
			}
		}
		return ui.Color{}, false
	}
	assertTint := func(lines []int, want ui.Color) {
		t.Helper()
		for _, line := range lines {
			got, ok := at(line)
			if !ok || got.A == 0 || got.R != want.R || got.G != want.G || got.B != want.B {
				t.Errorf("line %d background = %#v, %v; want an opaque tint of %#v", line, got, ok, want)
			}
		}
	}
	assertTint([]int{2, 3}, theme.Danger)
	assertTint([]int{5}, theme.Warning)
	assertTint([]int{7}, theme.Success)
	for _, line := range []int{0, 9} {
		if got, ok := at(line); ok {
			t.Errorf("unchanged line %d unexpectedly has background %#v", line, got)
		}
	}
}

// The side-by-side review exposes the familiar + and - gutters rather than
// MyGo's default color bars alone.
func TestConflictReviewUsesClassicDiffIndicators(t *testing.T) {
	a := &app{conflictState: newConflictState("file.go", "<<<<<<< HEAD\nold\n=======\nnew\n>>>>>>> topic\n", 0)}
	a.refreshConflictDiff()
	if a.conflictDiffState == nil {
		t.Fatal("conflict review has no diff state")
	}
	if got := a.conflictDiffState.Indicators; got != editor.DiffClassic {
		t.Fatalf("diff indicators = %v, want DiffClassic (%v)", got, editor.DiffClassic)
	}
}

// A success badge is used over a bright green fill in the dark theme. Its
// label must choose a contrasting foreground instead of the ordinary text.
func TestConflictSuccessBadgeTextContrast(t *testing.T) {
	theme := ui.DarkTheme()
	foreground := conflictSuccessBadgeText(theme)
	luminance := func(color ui.Color) float64 {
		linear := func(channel uint8) float64 {
			value := float64(channel) / 255
			if value <= 0.04045 {
				return value / 12.92
			}
			return math.Pow((value+0.055)/1.055, 2.4)
		}
		return 0.2126*linear(color.R) + 0.7152*linear(color.G) + 0.0722*linear(color.B)
	}
	fg, bg := luminance(foreground), luminance(theme.Success)
	ratio := (math.Max(fg, bg) + 0.05) / (math.Min(fg, bg) + 0.05)
	if ratio < 4.5 {
		t.Fatalf("badge contrast = %.2f:1, want at least 4.5:1 (foreground %#v, background %#v)", ratio, foreground, theme.Success)
	}
}

// A file without markers has no conflicts, and an ordinary line of equals
// signs is text, not a separator.
func TestConflictStateIgnoresNonMarkers(t *testing.T) {
	for _, content := range []string{
		"",
		"plain\ntext\n",
		"=======\nnot a marker run\n",
		"<<<<<<< HEAD\nours\n=======\n",
	} {
		state := newConflictState("file.txt", content, 0)
		if len(state.regions) != 0 {
			t.Fatalf("%q gave regions %#v", content, state.regions)
		}
		if state.resolve(conflictCurrent) {
			t.Fatalf("%q resolved something", content)
		}
	}
}

// A resolved file keeps the line ending and the final newline it had.
func TestConflictStateKeepsLineEndings(t *testing.T) {
	content := "first\r\n<<<<<<< HEAD\r\nours\r\n=======\r\ntheirs\r\n>>>>>>> topic"
	state := newConflictState("file.txt", content, 0)
	if !state.resolve(conflictCurrent) {
		t.Fatal("picking the current side changed nothing")
	}
	if got, want := state.text(), "first\r\nours"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

// Choosing another conflict moves through the file and stops at its ends.
func TestConflictStateMovesBetweenConflicts(t *testing.T) {
	content := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"
	state := newConflictState("file.txt", content, 0)
	if len(state.regions) != 2 {
		t.Fatalf("regions = %#v, want two", state.regions)
	}
	if !state.moveSelection(1) {
		t.Fatal("moving down did nothing")
	}
	if state.selected != 1 {
		t.Fatalf("selected = %d, want 1", state.selected)
	}
	if state.moveSelection(1) {
		t.Fatal("moving past the last conflict worked")
	}
	if state.moveSelection(-2) {
		t.Fatal("moving before the first conflict worked")
	}
	if state.selected != 1 {
		t.Fatalf("a refused move changed the selection to %d", state.selected)
	}
	if !state.moveSelection(-1) || state.selected != 0 {
		t.Fatalf("moving up did not reach the first conflict")
	}
}

// The pane resolves the working-tree file: a pick writes the file and undo
// puts the original content back.
func TestConflictPaneResolvesTheFile(t *testing.T) {
	content := "first\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	var written []byte
	writeWorkingFile = func(_ string, data []byte) error {
		written = append([]byte(nil), data...)
		return nil
	}

	h, calls := changesTestAppIn(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	}, 1600, 900)
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()

	badge, found := h.tester.Find("1 / 1")
	if !found {
		t.Fatal("compact conflict counter is missing")
	}
	right := badge.X + badge.W
	var navigationIcon ui.Rect
	for _, label := range []string{
		"Previous conflict", "Next conflict",
		"Base", "Current", "Incoming", "Result",
		"Use current", "Use incoming", "Use both", "Restore initial conflict", "Mark resolved",
	} {
		button, found := h.tester.Find(label)
		if !found {
			t.Fatalf("conflict action %q is missing", label)
		}
		if label != "Mark resolved" {
			buttonCenter, badgeCenter := button.Y+button.H/2, badge.Y+badge.H/2
			if buttonCenter != badgeCenter {
				t.Errorf("%s center = %.1f, conflict counter center = %.1f; group actions should share one row", label, buttonCenter, badgeCenter)
			}
		}
		if button.X < right {
			t.Errorf("%s starts at %.1f, before the previous group ends at %.1f", label, button.X, right)
		}
		switch label {
		case "Previous conflict":
			navigationIcon = button
		case "Use current", "Use incoming", "Use both", "Restore initial conflict":
			if button.W != navigationIcon.W || button.H != navigationIcon.H {
				t.Errorf("Apply action %q is %.1f×%.1f, want the Hunk icon-button dimensions %.1f×%.1f", label, button.W, button.H, navigationIcon.W, navigationIcon.H)
			}
		}
		right = button.X + button.W
	}
	if _, found := h.tester.Find("Undo resolution"); found {
		t.Fatal("Undo should not reserve space before there is a resolution to undo")
	}
	currentButton, _ := h.tester.Find("Use current")
	h.tester.Move(currentButton.X+currentButton.W/2, currentButton.Y+currentButton.H/2)
	h.tester.Advance(time.Second)
	if !h.tester.HasText("Alt+1") || !h.tester.HasText("Keep the current side's hunk") {
		t.Fatalf("Use current icon does not show its complete tooltip: %q", h.texts())
	}
	h.tester.Move(0, 0)
	h.tester.Advance(2 * time.Second)

	for _, text := range []string{
		"Hunk", "View", "Apply", "1 / 1", "Use current", "Use incoming", "Use both",
		"Restore initial conflict", "Open file in editor", "Mark resolved",
	} {
		if !slices.Contains(h.texts(), text) {
			t.Fatalf("conflict control %q missing from %q", text, h.texts())
		}
	}

	if err := h.tester.Click("Use incoming"); err != nil {
		t.Fatalf("pick the incoming side: %v", err)
	}
	h.tester.Frame()
	if got, want := string(written), "first\ntheirs\nlast\n"; got != want {
		t.Fatalf("written = %q, want %q", got, want)
	}
	if len(h.app.conflictState.regions) != 0 {
		t.Fatalf("the resolved file still has conflicts: %#v", h.app.conflictState.regions)
	}
	if !slices.Contains(h.texts(), "No conflict markers") {
		t.Fatalf("the toolbar does not say the conflicts are gone: %q", h.texts())
	}
	undo, found := h.tester.Find("Undo resolution")
	if !found {
		t.Fatal("Undo did not appear after a resolution")
	}
	if undo.W != navigationIcon.W || undo.H != navigationIcon.H {
		t.Fatalf("Undo is %.1f×%.1f, want the Hunk icon-button dimensions %.1f×%.1f", undo.W, undo.H, navigationIcon.W, navigationIcon.H)
	}

	if err := h.tester.Click("Undo resolution"); err != nil {
		t.Fatalf("undo the pick: %v", err)
	}
	h.tester.Frame()
	if string(written) != content {
		t.Fatalf("undo wrote %q, want the original", written)
	}
	if len(h.app.conflictState.regions) != 1 {
		t.Fatalf("undo restored no conflict: %#v", h.app.conflictState.regions)
	}

	// Mark resolved stages the file, which is what tells Git the conflict
	// is settled.
	if err := h.tester.Click("Mark resolved"); err != nil {
		t.Fatalf("mark the file resolved: %v", err)
	}
	h.tester.Frame()
	h.settle(t)
	if !calls.has("stage:conflict.txt") {
		t.Fatalf("mark resolved did not stage the file: %#v", calls.all())
	}
}

// A selected tab changes only its visual role. Constructing both a normal and
// a primary button would draw two copies, as the Result regression showed.
func TestConflictViewSelectorRendersOneButtonPerTab(t *testing.T) {
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	h, _ := changesTestApp(t, []repository.File{{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true}})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	for tab := range 4 {
		h.app.conflictStageTab = tab
		h.tester.Frame()
		for _, label := range []string{"Base", "Current", "Incoming", "Result"} {
			count := 0
			for _, text := range h.texts() {
				if text == label {
					count++
				}
			}
			if count != 2 {
				t.Errorf("tab %d renders %q %d times, want one visible text and one accessible label", tab, label, count)
			}
		}
	}
}

// Switching an inline file's View changes only the content below the toolbar.
// The Hunk, View and Apply groups and Mark resolved never change position.
func TestConflictToolbarStaysFixedAcrossViewTabs(t *testing.T) {
	content := "<<<<<<< HEAD\nours\n||||||| base\ncommon\n=======\ntheirs\n>>>>>>> topic\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	writes := 0
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	writeWorkingFile = func(string, []byte) error { writes++; return nil }
	h, _ := changesTestAppIn(t, []repository.File{{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true}}, 1600, 900)
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()

	const (
		count = "1 / 1"
		mark  = "Mark resolved"
	)
	stable := []string{count, "Hunk", "View", "Apply", mark}
	baseline := make(map[string]ui.Rect, len(stable))
	for _, label := range stable {
		rect, found := h.tester.Find(label)
		if !found {
			t.Fatalf("initial Result toolbar is missing %q", label)
		}
		baseline[label] = rect
	}
	for tab := range 4 {
		h.app.conflictStageTab = tab
		h.tester.Frame()
		for _, label := range stable {
			rect, found := h.tester.Find(label)
			if !found {
				t.Errorf("View tab %d lost %q", tab, label)
				continue
			}
			if want := baseline[label]; rect.X != want.X || rect.Y != want.Y {
				t.Errorf("View tab %d moved %q from (%.1f, %.1f) to (%.1f, %.1f)", tab, label, want.X, want.Y, rect.X, rect.Y)
			}
		}
		for _, action := range []string{"Base", "Current", "Incoming", "Result", "Use current", "Use incoming", "Use both", "Use base", "Restore initial conflict"} {
			if _, found := h.tester.Find(action); !found {
				t.Errorf("View tab %d lost action %q", tab, action)
			}
		}
		if tab != 3 {
			before := h.app.conflictState.text()
			_ = h.tester.Click("Use current") // disabled outside Result
			h.tester.Frame()
			if got := h.app.conflictState.text(); got != before || writes != 0 {
				t.Fatalf("read-only View tab %d changed Result or wrote %d files", tab, writes)
			}
		}
	}
	h.app.conflictStageTab = 3
	h.tester.Frame()
	_ = h.tester.Click("Use current")
	h.tester.Frame()
	if writes != 1 {
		t.Fatalf("returning to Result left Apply disabled; got %d writes, want 1", writes)
	}
}

// Separators stay about two thirds of a toolbar group's height, centered
// beside its actions, even when Apply wraps; they never span the container.
func TestConflictToolbarSeparatorsKeepContentHeight(t *testing.T) {
	content := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	h, _ := changesTestApp(t, []repository.File{{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true}})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	for _, width := range []int{1280, 1120, 1040, 960, 880, 800, 720} {
		h.tester.SetSize(width, 800)
		h.tester.Frame()
		for _, label := range []string{"Hunk separator", "View separator", "Apply separator"} {
			separator, found := h.tester.Find(label)
			if !found {
				t.Fatalf("width %d: %s is missing", width, label)
			}
			if separator.H != 31 {
				t.Errorf("width %d: %s height = %.1f, want its two-thirds size of 31", width, label, separator.H)
			}
		}
	}
}

// A refresh that began before Undo may finish afterward with the previously
// resolved bytes. That obsolete answer must not overwrite the restored file.
func TestConflictUndoRejectsInflightResolvedRead(t *testing.T) {
	original := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	var mu sync.Mutex
	current := original
	blockRead := false
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce, releaseOnce sync.Once
	releaseReads := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseReads)
	readWorkingFile = func(string) ([]byte, error) {
		mu.Lock()
		content, block := current, blockRead
		mu.Unlock()
		if block {
			startOnce.Do(func() { close(started) })
			<-release
		}
		return []byte(content), nil
	}
	writeWorkingFile = func(_ string, data []byte) error {
		mu.Lock()
		current = string(data)
		mu.Unlock()
		return nil
	}

	h, _ := changesTestApp(t, []repository.File{{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true}})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.app.resolveConflictSide(conflictIncoming)
	h.tester.Frame()
	resolved := h.app.conflictState.text()
	if len(h.app.conflictState.regions) != 1 {
		t.Fatalf("one hunk pick left %d conflicts, want 1", len(h.app.conflictState.regions))
	}

	mu.Lock()
	blockRead = true
	mu.Unlock()
	h.app.refreshRemoteQuietDetails()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the stale quiet read did not start")
	}
	h.app.undoConflictResolution()
	if got := h.app.conflictState.text(); got != original {
		t.Fatalf("Undo before the stale reply produced %q, want the original conflict", got)
	}
	releaseReads()
	h.waitFor(t, "the stale quiet reply", func() bool {
		return !h.app.diffLoading && h.app.cancelDiff == nil
	})
	if got := h.app.conflictState.text(); got != original {
		t.Fatalf("stale reply overwrote Undo with %q (previous resolution %q), want the original conflict", got, resolved)
	}
	if len(h.app.conflictState.regions) != 2 {
		t.Fatalf("stale reply left %d conflicts, want both original conflicts", len(h.app.conflictState.regions))
	}
}

// Restore initial conflict is a file-level reset, not just one undo: after
// several picks it brings every original marker back. The reset is itself
// undoable, so recovery never silently throws away the user's resolved state.
func TestConflictPaneRestoresInitialConflict(t *testing.T) {
	content := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	var written []byte
	writeWorkingFile = func(_ string, data []byte) error {
		written = append([]byte(nil), data...)
		return nil
	}

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	for range 2 {
		if err := h.tester.Click("Use incoming"); err != nil {
			t.Fatalf("resolve one hunk: %v", err)
		}
		h.tester.Frame()
	}
	resolved := "a\n2\nb\n4\n"
	if got := h.app.conflictState.text(); got != resolved || len(h.app.conflictState.regions) != 0 {
		t.Fatalf("two picks left text %q and regions %#v, want the fully resolved file", got, h.app.conflictState.regions)
	}

	if err := h.tester.Click("Restore initial conflict"); err != nil {
		t.Fatalf("restore the initial conflict: %v", err)
	}
	h.tester.Frame()
	if got := string(written); got != content {
		t.Fatalf("restored file = %q, want the original conflict", got)
	}
	if len(h.app.conflictState.regions) != 2 || h.app.conflictState.selected != 0 {
		t.Fatalf("restore left regions %#v and selected %d, want two regions at the first", h.app.conflictState.regions, h.app.conflictState.selected)
	}

	if err := h.tester.Click("Undo resolution"); err != nil {
		t.Fatalf("undo the restore: %v", err)
	}
	h.tester.Frame()
	if got := string(written); got != resolved {
		t.Fatalf("undoing restore wrote %q, want the state before restore %q", got, resolved)
	}
}

// A failed restore must leave both the resolved Result and its undo stack
// intact; the UI must not claim an initial conflict that was never written.
func TestConflictRestoreFailureKeepsCurrentResult(t *testing.T) {
	content := "first\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	writeWorkingFile = func(string, []byte) error { return nil }
	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	if err := h.tester.Click("Use incoming"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	resolved := "first\ntheirs\nlast\n"
	state := h.app.conflictState
	if got := state.text(); got != resolved {
		t.Fatalf("resolved Result = %q, want %q", got, resolved)
	}
	undoLen := len(state.undo)
	writeWorkingFile = func(string, []byte) error { return fs.ErrPermission }
	if err := h.tester.Click("Restore initial conflict"); err != nil {
		t.Fatalf("attempt restore: %v", err)
	}
	h.tester.Frame()
	if got := state.text(); got != resolved {
		t.Fatalf("failed restore changed Result to %q, want %q", got, resolved)
	}
	if len(state.undo) != undoLen {
		t.Fatalf("failed restore changed undo length from %d to %d", undoLen, len(state.undo))
	}
}

// Ctrl+Z (Cmd+Z on macOS) is the conventional edit undo and should invoke
// the same hunk-resolution undo as the toolbar, not an empty replacement
// buffer's text history. Alt+Z remains available for compatibility.
func TestConflictShortcutUndoResolution(t *testing.T) {
	for _, modifiers := range []struct {
		name string
		mod  ui.Modifiers
	}{{"command", ui.Cmd}, {"control", ui.Ctrl}} {
		t.Run(modifiers.name, func(t *testing.T) {
			content := "first\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nlast\n"
			originalRead, originalWrite := readWorkingFile, writeWorkingFile
			t.Cleanup(func() {
				readWorkingFile, writeWorkingFile = originalRead, originalWrite
			})
			readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
			writeWorkingFile = func(string, []byte) error { return nil }
			h, _ := changesTestApp(t, []repository.File{{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true}})
			h.waitFor(t, "the conflict read", func() bool {
				return !h.app.diffLoading && h.app.conflictState != nil
			})
			if err := h.tester.Click("Use incoming"); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			h.tester.Key(modifiers.mod, ui.KeyZ)
			h.tester.Frame()
			if got := h.app.conflictState.text(); got != content {
				t.Fatalf("undo shortcut wrote %q, want the original conflict", got)
			}
		})
	}
}

// The toolbar walks the file's conflicts, and each pick moves the pane on to
// the next one that is left.
func TestConflictPaneMovesBetweenConflicts(t *testing.T) {
	content := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	writeWorkingFile = func(string, []byte) error { return nil }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "1 / 2") {
		t.Fatalf("the toolbar starts on the wrong conflict: %q", h.texts())
	}

	if err := h.tester.Click("Next conflict"); err != nil {
		t.Fatalf("move to the next conflict: %v", err)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "2 / 2") {
		t.Fatalf("moving down did not select the second conflict: %q", h.texts())
	}
	if h.app.conflictState.selected != 1 {
		t.Fatalf("selected = %d, want 1", h.app.conflictState.selected)
	}

	if err := h.tester.Click("Previous conflict"); err != nil {
		t.Fatalf("move to the previous conflict: %v", err)
	}
	h.tester.Frame()
	if !slices.Contains(h.texts(), "1 / 2") {
		t.Fatalf("moving up did not select the first conflict: %q", h.texts())
	}
}

// The complete hunk workflow is available without leaving the marker editor:
// navigate, pick both/incoming/current, and undo the last pick. Modified
// choices leave ordinary letters and digits available to the editor.
func TestConflictKeyboardResolvesHunks(t *testing.T) {
	content := "a\n<<<<<<< HEAD\n1\n=======\n2\n>>>>>>> t\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() {
		readWorkingFile, writeWorkingFile = originalRead, originalWrite
	})
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	var written string
	writeWorkingFile = func(_ string, data []byte) error {
		written = string(data)
		return nil
	}

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflict read", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()

	h.tester.Key(0, ui.KeyF7)
	h.tester.Frame()
	if h.app.conflictState.selected != 1 {
		t.Fatalf("F7 selected conflict %d, want the second", h.app.conflictState.selected)
	}
	h.tester.Key(ui.Shift, ui.KeyF7)
	h.tester.Frame()
	if h.app.conflictState.selected != 0 {
		t.Fatalf("Shift+F7 selected conflict %d, want the first", h.app.conflictState.selected)
	}

	h.tester.Key(ui.Alt, ui.Key4)
	h.tester.Frame()
	if got, want := written, "a\n1\n2\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"; got != want {
		t.Fatalf("Alt+4 wrote %q, want both sides of the first hunk %q", got, want)
	}
	h.tester.Key(ui.Alt, ui.Key3)
	h.tester.Frame()
	if got, want := written, "a\n1\n2\nb\n4\n"; got != want {
		t.Fatalf("Alt+3 wrote %q, want the incoming second hunk %q", got, want)
	}
	if len(h.app.conflictState.regions) != 0 {
		t.Fatalf("keyboard picks left conflicts: %#v", h.app.conflictState.regions)
	}

	h.tester.Key(ui.Alt, ui.KeyZ)
	h.tester.Frame()
	if got, want := written, "a\n1\n2\nb\n<<<<<<< HEAD\n3\n=======\n4\n>>>>>>> t\n"; got != want {
		t.Fatalf("Alt+Z wrote %q, want the incoming pick undone %q", got, want)
	}
	h.tester.Key(ui.Alt, ui.Key1)
	h.tester.Frame()
	if got, want := written, "a\n1\n2\nb\n3\n"; got != want {
		t.Fatalf("Alt+1 wrote %q, want the current second hunk %q", got, want)
	}
}

// An interrupted merge cannot be continued while Git still reports a
// conflict. The disabled header action must not call the repository at all.
func TestContinueStaysDisabledWithUnresolvedConflicts(t *testing.T) {
	h, calls := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.app.snapshot.Operation = "merge"
	h.tester.Frame()

	if err := h.tester.Click("Continue"); err != nil {
		t.Fatalf("click disabled Continue: %v", err)
	}
	h.tester.Frame()
	if calls.has("continue") {
		t.Fatal("Continue ran with an unresolved conflict")
	}
}

// A file that cannot be read says so in the pane instead of pretending it
// has no conflicts.
func TestConflictPaneReportsAnUnreadableFile(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the failed read", func() bool {
		return !h.app.diffLoading && h.app.diffError != ""
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "The conflicted file could not be read.") {
		t.Fatalf("the pane does not explain the read: %q", h.texts())
	}
	if !slices.Contains(h.texts(), "permission denied") {
		t.Fatalf("the pane does not show the error: %q", h.texts())
	}
}

// The repository can write markers of its own size through the
// conflict-marker-size attribute; the parser takes the file's size, and the
// default does not mistake a wider marker for text.
func TestConflictStateUsesTheFilesMarkerSize(t *testing.T) {
	content := strings.Repeat("<", 12) + " HEAD\nours\n" + strings.Repeat("=", 12) + "\ntheirs\n" + strings.Repeat(">", 12) + " topic\n"
	state := newConflictState("file.txt", content, 12)
	if len(state.regions) != 1 {
		t.Fatalf("regions = %#v, want one", state.regions)
	}
	if region := state.regions[0]; region.start != 0 || region.split != 2 || region.end != 4 {
		t.Fatalf("region = %#v", region)
	}
	if !state.resolve(conflictIncoming) {
		t.Fatal("picking the incoming side changed nothing")
	}
	if got, want := state.text(), "theirs\n"; got != want {
		t.Fatalf("incoming side = %q, want %q", got, want)
	}
	if regions := newConflictState("file.txt", content, 0).regions; len(regions) != 0 {
		t.Fatalf("the default size found the 12-character markers: %#v", regions)
	}
}

// Modify/delete has no text hunks, but its resolution toolbar must use the
// same Hunk, View, Apply and right-side Mark resolved layout as inline text.
func TestConflictSidesUsesGroupedResolutionToolbar(t *testing.T) {
	content := "first\nsecond changed\nthird\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }
	h, _ := changesTestAppIn(t, []repository.File{{Path: "conflict.txt", Status: "UD", Tracked: true, Staged: true, Unstaged: true, HasMergeConflicts: true}}, 1600, 900)
	h.waitFor(t, "the modify/delete conflict", func() bool {
		return !h.app.diffLoading && h.app.conflictSides != nil
	})
	h.tester.Frame()

	stable := []string{"Deleted by them", "Hunk", "View", "Apply", "Mark resolved"}
	baseline := make(map[string]ui.Rect, len(stable))
	for _, label := range stable {
		rect, found := h.tester.Find(label)
		if !found {
			t.Fatalf("modify/delete did not use the grouped toolbar: missing %q", label)
		}
		baseline[label] = rect
	}
	for _, action := range []string{"Use current", "Use incoming"} {
		count := 0
		for _, text := range h.texts() {
			if text == action {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%q appears %d times, want one icon button with an accessible label", action, count)
		}
	}
	if _, found := h.tester.Find("Use both"); found {
		t.Fatal("a modify/delete resolution offers the meaningless Use both action")
	}
	for tab := range 4 {
		h.app.conflictStageTab = tab
		h.tester.Frame()
		for _, label := range stable {
			rect, found := h.tester.Find(label)
			if !found {
				t.Errorf("View tab %d lost %q for modify/delete", tab, label)
				continue
			}
			if want := baseline[label]; rect.X != want.X || rect.Y != want.Y {
				t.Errorf("View tab %d moved %q from (%.1f, %.1f) to (%.1f, %.1f)", tab, label, want.X, want.Y, rect.X, rect.Y)
			}
		}
	}
}

// Git leaves no markers when one side deleted the file: it keeps the other
// side in the tree, and the pane shows that side rather than an empty merge.
func TestConflictSidesPaneShowsTheKeptSide(t *testing.T) {
	const content = "line1\nline2 changed\nline3\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, calls := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "DU", Staged: true, Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the kept side", func() bool {
		return !h.app.diffLoading && h.app.conflictSides != nil
	})
	h.tester.Frame()

	if h.app.conflictState != nil {
		t.Fatal("a conflict without markers opened the marker editor")
	}
	sides := h.app.conflictSides
	if sides.content != content || sides.current || !sides.incoming {
		t.Fatalf("sides = %+v, want the incoming version alone", sides)
	}
	for _, text := range []string{
		"Deleted by us",
		"The current side deleted this file. The incoming version Git kept is below.",
		"Use current",
		"Use incoming",
		"This file was deleted in this branch and changed in the incoming branch. Git kept the incoming version.",
	} {
		if !slices.Contains(h.texts(), text) {
			t.Fatalf("the pane is missing %q: %q", text, h.texts())
		}
	}
	if slices.Contains(h.texts(), "Git left conflict markers in this file. Choose a side, or edit them away and mark it resolved.") {
		t.Fatalf("the pane claims markers that do not exist: %q", h.texts())
	}

	if err := h.tester.Click("Use current"); err != nil {
		t.Fatalf("resolve with the current side: %v", err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the resolution", func() bool { return calls.has("resolve:conflict.txt:current") })
}

// A conflict both sides deleted has no text at all: the pane names the state
// and still offers both resolutions.
func TestConflictSidesPaneForABothDeletedFile(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return nil, fs.ErrNotExist }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "gone.txt", Status: "DD", Staged: true, Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the missing file", func() bool {
		return !h.app.diffLoading && h.app.conflictSides != nil
	})
	h.tester.Frame()

	if h.app.conflictSides.content != "" {
		t.Fatalf("a both-deleted conflict carries %q", h.app.conflictSides.content)
	}
	for _, text := range []string{"Deleted by both", "Both sides deleted this file.", "Use current", "Use incoming"} {
		if !slices.Contains(h.texts(), text) {
			t.Fatalf("the pane is missing %q: %q", text, h.texts())
		}
	}
}

// The review of a conflict compares the file as each side would have it:
// the current side's hunks and the incoming side's, with the ancestor
// dropped and the text outside the conflicts unchanged.
func TestConflictSidesTexts(t *testing.T) {
	content := "first\n<<<<<<< HEAD\nours\n||||||| base\ncommon\n=======\ntheirs\n>>>>>>> topic\nlast\n"
	state := newConflictState("file.txt", content, 0)
	current, incoming := state.sidesTexts()
	if got, want := current, "first\nours\nlast\n"; got != want {
		t.Fatalf("current side = %q, want %q", got, want)
	}
	if got, want := incoming, "first\ntheirs\nlast\n"; got != want {
		t.Fatalf("incoming side = %q, want %q", got, want)
	}

	// A resolved conflict leaves both sides at the resolved text.
	state.resolve(conflictCurrent)
	current, incoming = state.sidesTexts()
	if current != "first\nours\nlast\n" || current != incoming {
		t.Fatalf("resolved sides = %q and %q", current, incoming)
	}
}

// A conflicted file can be reviewed like any other change: the toggle shows
// the two sides as a diff, with the layout, wrapping, saving and expanding
// the other diffs offer, and the way back to editing the markers.
func TestConflictReviewShowsBothSidesAsADiff(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, calls := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Review both sides") {
		t.Fatalf("the pane offers no review: %q", h.texts())
	}

	if err := h.tester.Click("Review both sides"); err != nil {
		t.Fatalf("open the review: %v", err)
	}
	h.tester.Frame()

	if !h.app.conflictReview {
		t.Fatal("the review did not open")
	}
	// The way back to the markers remains: the review is a diff, the markers
	// are the file, and the Code action replaces the old pencil.
	for _, label := range []string{"Show conflict markers"} {
		if !slices.Contains(h.texts(), label) {
			t.Fatalf("the review is missing %q: %q", label, h.texts())
		}
	}
	for _, label := range []string{
		"Toggle diff layout",
		"Toggle diff line wrapping",
		"Save diff",
		"Expand diff",
	} {
		if !slices.Contains(h.texts(), label) {
			t.Fatalf("the review is missing %q: %q", label, h.texts())
		}
	}
	review := h.app.conflictDiffState
	if review == nil {
		t.Fatal("the review has no diff state")
	}
	if got, want := review.Old.String(), "one\nours\nlast\n"; got != want {
		t.Fatalf("current side = %q, want %q", got, want)
	}
	if got, want := review.New.String(), "one\ntheirs\nlast\n"; got != want {
		t.Fatalf("incoming side = %q, want %q", got, want)
	}
	shown, ok := h.app.activeDiff()
	if !ok || shown.state != review {
		t.Fatalf("the review is not the active diff: %+v, %v", shown, ok)
	}
	if shown.path != "conflict.txt" {
		t.Fatalf("the active diff names %q, want conflict.txt", shown.path)
	}
	if !calls.has("diff:conflict.txt:unstaged") {
		t.Fatalf("the combined diff was never read: %v", calls.all())
	}

	if err := h.tester.Click("Show conflict markers"); err != nil {
		t.Fatalf("return to the editor: %v", err)
	}
	h.tester.Frame()
	if h.app.conflictReview {
		t.Fatal("the editor did not come back")
	}
	if !slices.Contains(h.texts(), "Review both sides") {
		t.Fatalf("the pane does not offer the review again: %q", h.texts())
	}
}

// A conflicted file's header carries what a changed file's does: the way to
// the review of its sides, then the controls of a diff, since the review is
// one, and its own menu last.
func TestAConflictedFileHeaderCarriesTheDiffControls(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()

	for _, label := range []string{
		"Open file in editor",
		"Review both sides",
		"Toggle diff line wrapping",
		"Save diff",
		"Expand diff",
		"File actions",
	} {
		if !slices.Contains(h.texts(), label) {
			t.Fatalf("the conflicted file is missing %q: %q", label, h.texts())
		}
	}
	// The layout toggle belongs to the review: the markers are one buffer,
	// with no two sides to lay out until the review shows them.
	if slices.Contains(h.texts(), "Toggle diff layout") {
		t.Fatalf("the markers offer a layout to switch: %q", h.texts())
	}
	// The menu comes last on the controls row; expansion sits above it in the
	// title's top-right corner.
	more, _ := h.tester.Find("File actions")
	expand, _ := h.tester.Find("Expand diff")
	save, _ := h.tester.Find("Save diff")
	if more.X <= save.X || expand.X <= more.X || more.Y < expand.Y+expand.H {
		t.Fatalf("the conflict header is not split into title and controls rows: expand=%+v save=%+v more=%+v",
			expand, save, more)
	}
}

// Wrapping the pane wraps the conflict's markers too, so a long conflicted
// line reads without scrolling sideways, as a diff's long line does.
func TestWrappingThePaneWrapsTheConflictMarkers(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictView != nil
	})
	h.tester.Frame()
	if h.app.conflictView.SoftWrap {
		t.Fatal("the markers wrap before the toggle was used")
	}
	if err := h.tester.Click("Toggle diff line wrapping"); err != nil {
		t.Fatalf("wrap the pane: %v", err)
	}
	h.tester.Frame()
	if !h.app.conflictView.SoftWrap {
		t.Fatal("the toggle left the markers unwrapped")
	}
}

// The conflicted file's menu holds the resolution actions and the two ways to
// copy its path, and none of the staging, blaming or discarding that would act
// on a file whose sides are not decided yet.
func TestAConflictedFileMenuHoldsTheResolutionActions(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	if err := h.tester.Click("File actions"); err != nil {
		t.Fatalf("open the conflicted file's menu: %v", err)
	}
	h.tester.Frame()

	for _, label := range []string{
		"Use current",
		"Use incoming",
		"Mark resolved (stage as is)",
		"Copy relative path",
		"Copy absolute path",
	} {
		if !slices.Contains(h.texts(), label) {
			t.Fatalf("the menu is missing %q: %q", label, h.texts())
		}
	}
	for _, label := range []string{"Stage", "Unstage", "Blame", "Discard changes…"} {
		if slices.Contains(h.texts(), label) {
			t.Fatalf("the conflicted file's menu offers %q: %q", label, h.texts())
		}
	}
}

// The legacy pane editor remains covered through its internal action after its
// redundant header button was removed. Done still rereads the conflict markers.
func TestEditingAConflictedFileWholeReturnsToItsMarkers(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	// The pane reads and writes the same text, as it does on disk, so the
	// read after Done shows what was typed.
	var mu sync.Mutex
	text := content
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() { readWorkingFile, writeWorkingFile = originalRead, originalWrite })
	readWorkingFile = func(string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		return []byte(text), nil
	}
	writeWorkingFile = func(_ string, data []byte) error {
		mu.Lock()
		defer mu.Unlock()
		text = string(data)
		return nil
	}

	h, _ := changesTestApp(t, []repository.File{
		{Path: "conflict.txt", Status: "UU", Unstaged: true, HasMergeConflicts: true},
	})
	h.waitFor(t, "the conflicted file", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil
	})
	h.tester.Frame()
	h.app.openFileEdit()
	h.tester.Frame()
	h.waitFor(t, "the file read", func() bool {
		return !h.app.fileEditLoading && h.app.fileEditState != nil
	})
	h.tester.Frame()
	if !h.app.fileEditing {
		t.Fatal("the internal edit action did not open the editor")
	}
	// The editor owns the file whole: the conflict actions step aside while
	// it shows, so a click meant for the file cannot mark it resolved half
	// edited.
	for _, gone := range []string{"Mark resolved", "Use current", "File actions", "Review both sides"} {
		if slices.Contains(h.texts(), gone) {
			t.Fatalf("%q still shows while the editor is open: %q", gone, h.texts())
		}
	}

	clickEditor(t, h, "Done")
	h.tester.Type("X")
	h.tester.Frame()
	h.tester.Frame()
	mu.Lock()
	wrote := text
	mu.Unlock()
	if !strings.Contains(wrote, "X") {
		t.Fatalf("typed text was not written back: %q", wrote)
	}

	if err := h.tester.Click("Done"); err != nil {
		t.Fatalf("leave the editor: %v", err)
	}
	h.waitFor(t, "the conflict read again", func() bool {
		return !h.app.diffLoading && h.app.conflictState != nil && !h.app.fileEditing
	})
	h.tester.Frame()
	if !slices.Contains(h.texts(), "Review both sides") {
		t.Fatalf("the markers did not come back: %q", h.texts())
	}
	if !strings.Contains(h.app.conflictState.text(), "X") {
		t.Fatalf("the markers show the file as it was before the edit: %q", h.app.conflictState.text())
	}
}

// A conflicted file's header is a changed file's bar, with the way to review
// its sides beside the Code action, and the way to mark it
// resolved on the conflict toolbar under it rather than beside the name. At
// the window's default width every control must still show: none may be laid
// out past the pane's edge with no width, and the name must keep room to read.
func TestAConflictedFileFitsItsControlsInTheDefaultWindow(t *testing.T) {
	const content = "one\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nlast\n"
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte(content), nil }

	const path = "internal/repository/session.go"
	calls := &callRecorder{}
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root:   "/work/project",
		Branch: "main",
		Files: []repository.File{
			{Path: path, Status: "UU", Unstaged: true, Tracked: true, HasMergeConflicts: true},
		},
	}}, calls: calls})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	opts := appWindowOptions(nil)
	h.tester = ui.NewTester(a.view, opts.Width, opts.Height)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the conflicted file", func() bool {
		return !a.diffLoading && a.conflictState != nil
	})
	h.tester.Frame()

	for _, label := range []string{
		"Open file in editor",
		"Review both sides",
		"Toggle diff line wrapping",
		"Save diff",
		"Expand diff",
		"File actions",
		"Mark resolved",
		"Previous conflict",
		"Next conflict",
		"Use current",
		"Use incoming",
		"Use both",
		"Restore initial conflict",
	} {
		r, ok := h.tester.Find(label)
		if !ok {
			t.Fatalf("the conflicted file is missing %q: %q", label, h.texts())
		}
		if r.W <= 0 {
			t.Fatalf("%q is laid out past the pane's edge, with no width", label)
		}
	}
	name, ok := h.tester.Find(path)
	if !ok || name.W < 120 {
		t.Fatalf("the file's name is squeezed to %0.f wide", name.W)
	}
	// Mark resolved ends the resolution, so it shares the conflict toolbar's
	// row rather than the header beside the name: the header is then the bar
	// a changed file has, on one row.
	editor, _ := h.tester.Find("Open file in editor")
	mark, _ := h.tester.Find("Mark resolved")
	if mark.Y <= editor.Y {
		t.Fatalf("Mark resolved sits on the header row at %.0f, as the editor action at %.0f", mark.Y, editor.Y)
	}
}
