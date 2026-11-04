package app

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

// defaultConflictMarkerSize is the number of characters Git writes in a
// conflict marker. The conflict-marker-size attribute can change it, and the
// file's own size is used when it does.
const defaultConflictMarkerSize = 7

// conflictRegion is one conflict of a file, by line index, from 0: the
// <<<<<<< marker, the ||||||| ancestor Git writes with the diff3 style (-1
// without it), the ======= separator and the >>>>>>> marker.
type conflictRegion struct {
	start int
	base  int
	split int
	end   int
}

// hasBase reports whether Git wrote the common ancestor's hunk, which the
// diff3 conflict style does.
func (r conflictRegion) hasBase() bool {
	return r.base >= 0
}

// conflictSide is a hunk of a conflict the user can keep: the current side,
// the ancestor the diff3 style shows, or the incoming side. conflictBoth keeps
// the current and incoming hunks and drops the ancestor between them, as
// lazygit's pick-both does.
type conflictSide int

const (
	conflictCurrent conflictSide = iota
	conflictBase
	conflictIncoming
	conflictBoth
)

// conflictState is one conflicted file open in the pane: its text, the
// conflicts still in it, the one the toolbar acts on, and the contents the
// picks before it replaced, so a pick can be undone.
type conflictState struct {
	path    string
	lines   []string
	regions []conflictRegion
	// selected is the index of the conflict the toolbar acts on.
	selected int
	undo     [][]string
	// crlf and trailingNewline are how the file was written, kept so a
	// resolved file is written back the same way.
	crlf            bool
	trailingNewline bool
	// markerSize is how many characters this file's markers are, from its
	// conflict-marker-size attribute, or Git's default when it is unset.
	markerSize int
}

// newConflictState reads content as the file at path. The text is stored by
// line so a pick can splice a hunk out without splitting the file again.
func newConflictState(path, content string, markerSize int) *conflictState {
	state := &conflictState{path: path, markerSize: markerSizeOrDefault(markerSize)}
	if strings.Contains(content, "\r\n") {
		state.crlf = true
		content = strings.ReplaceAll(content, "\r\n", "\n")
	}
	state.trailingNewline = strings.HasSuffix(content, "\n")
	state.lines = splitConflictLines(content)
	state.regions = findConflictRegions(state.lines, state.markerSize)
	return state
}

// markerSizeOrDefault is how many characters a marker has when the file does
// not set conflict-marker-size.
func markerSizeOrDefault(size int) int {
	if size > 0 {
		return size
	}
	return defaultConflictMarkerSize
}

// splitConflictLines splits text into lines, dropping the empty line a final
// newline leaves behind.
func splitConflictLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// adoptText replaces the file's lines with text typed into the pane, keeping
// the line ending the file was written with, and finds the markers left after
// the edit. The selection stays on the conflict at the same index where it
// still exists.
func (s *conflictState) adoptText(text string) {
	s.trailingNewline = strings.HasSuffix(text, "\n")
	s.lines = splitConflictLines(text)
	s.regions = findConflictRegions(s.lines, s.markerSize)
	s.clampSelection()
}

// text is the file's content as it should be written: the lines with the
// file's own line ending and its final newline.
func (s *conflictState) text() string {
	separator, ending := "\n", "\n"
	if s.crlf {
		separator, ending = "\r\n", "\r\n"
	}
	text := strings.Join(s.lines, separator)
	if s.trailingNewline && text != "" {
		text += ending
	}
	return text
}

// sidesTexts reads the file as each side of the merge would have it: the
// current side's hunks in place of every conflict for current, and the
// incoming side's for incoming, with the common ancestor dropped. Text
// outside the conflicts is the same on both sides. The review the pane shows
// compares the two.
func (s *conflictState) sidesTexts() (current, incoming string) {
	var cur, inc []string
	at := 0
	for _, region := range s.regions {
		cur = append(cur, s.lines[at:region.start]...)
		inc = append(inc, s.lines[at:region.start]...)
		currentEnd := region.split
		if region.hasBase() {
			currentEnd = region.base
		}
		cur = append(cur, s.lines[region.start+1:currentEnd]...)
		inc = append(inc, s.lines[region.split+1:region.end]...)
		at = region.end + 1
	}
	cur = append(cur, s.lines[at:]...)
	inc = append(inc, s.lines[at:]...)
	return joinConflictLines(cur, s.trailingNewline), joinConflictLines(inc, s.trailingNewline)
}

// joinConflictLines turns lines into text with the final newline the file was
// written with. The review compares lines by their text, so the separator is
// always "\n" even when the file on disk uses CRLF.
func joinConflictLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		return ""
	}
	text := strings.Join(lines, "\n")
	if trailingNewline {
		text += "\n"
	}
	return text
}

// conflictLineType is what a line is to the parser: a marker of one of the
// four kinds, or ordinary text.
type conflictLineType int

const (
	conflictLineStart conflictLineType = iota
	conflictLineBase
	conflictLineSplit
	conflictLineEnd
	conflictLineText
)

// conflictLineKind says whether line is a conflict marker. A start, ancestor
// or end marker may carry a label after a space, as "<<<<<<< HEAD"; the
// separator never does. Git's --combined output prefixes markers with "++",
// which is stripped first as lazygit does.
func conflictLineKind(line string, size int) conflictLineType {
	trimmed := strings.TrimPrefix(line, "++")
	switch {
	case conflictMarkerWithLabel(trimmed, '<', size):
		return conflictLineStart
	case conflictMarkerWithLabel(trimmed, '|', size):
		return conflictLineBase
	case len(trimmed) == size && strings.Repeat("=", size) == trimmed:
		return conflictLineSplit
	case conflictMarkerWithLabel(trimmed, '>', size):
		return conflictLineEnd
	default:
		return conflictLineText
	}
}

// conflictMarkerWithLabel reports whether line begins with size copies of
// marker, alone or followed by a space and a label.
func conflictMarkerWithLabel(line string, marker byte, size int) bool {
	if len(line) < size {
		return false
	}
	if line[:size] != strings.Repeat(string(marker), size) {
		return false
	}
	return len(line) == size || line[size] == ' '
}

// findConflictRegions reads the conflicts out of lines. A start marker opens
// a conflict, the separators fill it in, and an end marker closes it; a
// malformed run without its separator is left as ordinary text.
func findConflictRegions(lines []string, markerSize int) []conflictRegion {
	regions := make([]conflictRegion, 0, 2)
	open := conflictRegion{base: -1, split: -1, end: -1}
	opened := false
	for i, line := range lines {
		switch conflictLineKind(line, markerSizeOrDefault(markerSize)) {
		case conflictLineStart:
			open = conflictRegion{start: i, base: -1, split: -1, end: -1}
			opened = true
		case conflictLineBase:
			if opened && open.split < 0 {
				open.base = i
			}
		case conflictLineSplit:
			if opened && open.split < 0 {
				open.split = i
			}
		case conflictLineEnd:
			if opened && open.split >= 0 {
				open.end = i
				regions = append(regions, open)
			}
			opened = false
		}
	}
	return regions
}

// current returns the conflict the toolbar acts on.
func (s *conflictState) current() (conflictRegion, bool) {
	if s.selected < 0 || s.selected >= len(s.regions) {
		return conflictRegion{}, false
	}
	return s.regions[s.selected], true
}

// clampSelection keeps the selection inside the conflicts left after a pick
// or an undo.
func (s *conflictState) clampSelection() {
	if len(s.regions) == 0 {
		s.selected = 0
		return
	}
	if s.selected < 0 {
		s.selected = 0
	}
	if s.selected >= len(s.regions) {
		s.selected = len(s.regions) - 1
	}
}

// moveSelection moves the toolbar to the conflict delta places away, staying
// on the file's first or last conflict at the ends.
func (s *conflictState) moveSelection(delta int) bool {
	if len(s.regions) == 0 || delta == 0 {
		return false
	}
	next := s.selected + delta
	if next < 0 || next >= len(s.regions) {
		return false
	}
	s.selected = next
	return true
}

// selectedLine is the first line of the selected conflict, for scrolling it
// into view; 0 when the file has none.
func (s *conflictState) selectedLine() int {
	if region, ok := s.current(); ok {
		return region.start
	}
	return 0
}

// resolve keeps one hunk of the selected conflict in place of its markers and
// returns whether it changed anything. The content it replaced is kept so undo
// can put it back.
func (s *conflictState) resolve(side conflictSide) bool {
	region, ok := s.current()
	if !ok {
		return false
	}
	if side == conflictBase && !region.hasBase() {
		return false
	}

	kept := make([]string, 0, region.end-region.start)
	// With the diff3 style the ancestor sits between the current side and
	// the separator, so the current hunk ends at its marker.
	currentEnd := region.split
	if region.hasBase() {
		currentEnd = region.base
	}
	switch side {
	case conflictCurrent:
		kept = append(kept, s.lines[region.start+1:currentEnd]...)
	case conflictBase:
		kept = append(kept, s.lines[region.base+1:region.split]...)
	case conflictIncoming:
		kept = append(kept, s.lines[region.split+1:region.end]...)
	case conflictBoth:
		kept = append(kept, s.lines[region.start+1:currentEnd]...)
		kept = append(kept, s.lines[region.split+1:region.end]...)
	}

	next := make([]string, 0, len(s.lines)-(region.end-region.start)+len(kept))
	next = append(next, s.lines[:region.start]...)
	next = append(next, kept...)
	next = append(next, s.lines[region.end+1:]...)

	s.undo = append(s.undo, s.lines)
	s.lines = next
	s.regions = findConflictRegions(next, s.markerSize)
	s.clampSelection()
	return true
}

// undoResolution puts back the content before the last pick, and reports
// whether there was one.
func (s *conflictState) undoResolution() bool {
	if len(s.undo) == 0 {
		return false
	}
	previous := s.undo[len(s.undo)-1]
	s.undo = s.undo[:len(s.undo)-1]
	s.lines = previous
	s.regions = findConflictRegions(previous, s.markerSize)
	s.clampSelection()
	return true
}

// lineBackgrounds tints the marker rows of every conflict, the selected one
// more strongly, so the eye lands on the conflict the toolbar acts on.
func (s *conflictState) lineBackgrounds(theme *ui.Theme) []editor.LineBackground {
	backgrounds := make([]editor.LineBackground, 0, len(s.regions)*2)
	for i, region := range s.regions {
		color := theme.Warning.Alpha(0.16)
		if i == s.selected {
			color = theme.Danger.Alpha(0.24)
		}
		for _, line := range []int{region.start, region.base, region.split, region.end} {
			if line < 0 {
				continue
			}
			backgrounds = append(backgrounds, editor.LineBackground{First: line, Last: line, Color: color})
		}
	}
	return backgrounds
}

// requestConflictFile reads the chosen conflicted file into the pane, replacing
// the diff read the pane otherwise shows. A file with markers is read as the
// merged text the picks work on; a conflict Git wrote without markers is read
// as the side it kept. The read is generation-guarded as a diff is, so choosing
// another file drops its answer.
func (a *app) requestConflictFile(file repository.File, generation uint64) {
	path := file.Path
	inline := hasInlineConflict(file)
	session := a.session
	go func() {
		defer a.recoverBackground()
		content, err := readWorkingFile(a.workingFilePath(path))
		var state *conflictState
		var sides *conflictSides
		if inline {
			if err == nil {
				state = newConflictState(path, string(content), file.ConflictMarkerSize)
			}
		} else if err == nil || errors.Is(err, fs.ErrNotExist) {
			// Git leaves no markers when one side has no version of the
			// file; it keeps the other side in the tree instead, and a
			// both-deleted conflict leaves nothing at all.
			sides = newConflictSides(file, string(content))
		}
		// The combined diff git prints for an unmerged file is the patch the
		// review saves; it holds both sides' lines and the markers, which the
		// side-by-side review shows as the changes between the sides.
		patch := ""
		if session != nil {
			if diff, derr := session.Diff(context.Background(), file); derr == nil {
				patch = diff.Patch
			}
		}
		a.update(func() {
			if generation != a.diffGeneration {
				return
			}
			a.cancelDiff = nil
			a.diffLoading = false
			a.conflictSaveError = ""
			a.conflictSaveVersion = 0
			if err != nil && sides == nil {
				a.diffError = err.Error()
				return
			}
			a.conflictState = state
			a.conflictSides = sides
			a.conflictDiffPatch = patch
			a.conflictScrollTo = -1
			if state != nil {
				a.conflictScrollTo = state.selectedLine()
			}
			a.refreshConflictViewer()
			a.refreshConflictDiff()
		})
	}()
}

// refreshConflictViewer gives the pane's editor the resolved file's text. The
// editor is kept across picks so its scroll and selection survive them; only
// its buffer and language change. It is editable: markers left after a pick can
// be typed away as well.
func (a *app) refreshConflictViewer() {
	if a.conflictState == nil {
		a.conflictView = nil
		return
	}
	state := a.conflictView
	if state == nil {
		state = &editor.State{LineNumbers: editor.LineNumbersOn}
		a.conflictView = state
	}
	// The markers wrap as the diffs do: the header's wrap control changes
	// both, so a long conflicted line reads without scrolling sideways.
	state.SoftWrap = a.diffWrap
	state.Buffer = editor.NewBuffer(a.conflictState.text())
	state.Buffer.SetLanguage(editor.LanguageFor(a.conflictState.path))
	a.refreshConflictDiff()
}

// refreshConflictDiff rebuilds the review of the conflict's two sides from the
// file as it stands. The pane keeps the review's scroll and layout, replacing
// only its buffers, so a pick does not move the reader.
func (a *app) refreshConflictDiff() {
	path := ""
	var oldText, newText string
	switch {
	case a.conflictState != nil:
		path = a.conflictState.path
		oldText, newText = a.conflictState.sidesTexts()
	case a.conflictSides != nil:
		sides := a.conflictSides
		path = sides.file.Path
		switch {
		case !sides.current && !sides.incoming:
			// Both sides deleted the file: there is no change to review.
		case !sides.current:
			newText = sides.content
		case !sides.incoming:
			oldText = sides.content
		default:
			oldText, newText = sides.content, sides.content
		}
	default:
		a.conflictDiffState = nil
		return
	}
	state := a.conflictDiffState
	if state == nil {
		state = &editor.DiffState{ContextLines: 3}
		a.conflictDiffState = state
	}
	state.Old = editor.NewBuffer(oldText)
	state.New = editor.NewBuffer(newText)
	language := editor.LanguageFor(path)
	state.Old.SetLanguage(language)
	state.New.SetLanguage(language)
	state.Layout = a.diffLayout()
	state.Wrap = a.diffWrap
}

// conflictSides is a conflict Git wrote without markers: one side deleted the
// file, so Git kept the other side in the working tree instead of a merged
// text. current and incoming say which sides have a version of the path, kept
// is whose version is in the tree, and content is that version.
type conflictSides struct {
	file     repository.File
	current  bool
	incoming bool
	kept     conflictSide
	content  string
	view     *editor.State
}

// newConflictSides reads the sides of a conflict from its status, whose first
// letter is the current side of Git's two-character code and second the
// incoming one. A side with no version of the file is the side that deleted
// it, or the side that never had it because this branch added the file.
func newConflictSides(file repository.File, content string) *conflictSides {
	sides := &conflictSides{file: file, content: content, current: true, incoming: true}
	switch file.Status {
	case "DU", "UA", "DD":
		sides.current = false
	}
	switch file.Status {
	case "UD", "AU", "DD":
		sides.incoming = false
	}
	switch {
	case sides.current && !sides.incoming:
		sides.kept = conflictCurrent
	case sides.incoming && !sides.current:
		sides.kept = conflictIncoming
	default:
		sides.kept = conflictCurrent
	}
	return sides
}

// viewState is the read-only editor of the version Git kept, made once so its
// scroll survives the frames that draw it.
func (s *conflictSides) viewState() *editor.State {
	if s.view == nil {
		s.view = &editor.State{LineNumbers: editor.LineNumbersOn, ReadOnly: true}
		s.view.Buffer = editor.NewBuffer(s.content)
		s.view.Buffer.SetLanguage(editor.LanguageFor(s.file.Path))
	}
	return s.view
}

// hasInlineConflict reports whether Git merged the file as text, which is the
// case when both sides have a version of it: Git then writes markers into the
// working tree. The status is checked as well as the flag, so a reader that
// does not fill the flag in still gets the merge pane for a text conflict.
func hasInlineConflict(file repository.File) bool {
	return file.HasInlineMergeConflicts || file.Status == "UU" || file.Status == "AA"
}

// conflictExplanation says what Git did with a conflicted file. Git writes
// markers when both sides kept a version; when one side deleted the file it
// keeps the other side instead, and claiming markers would send the user
// looking for text that is not there.
func conflictExplanation(file repository.File) string {
	if hasInlineConflict(file) {
		return "Git left conflict markers in this file. Choose a side, or edit them away and mark it resolved."
	}
	switch file.Status {
	case "DU":
		return "This file was deleted in this branch and changed in the incoming branch. Git kept the incoming version."
	case "UD":
		return "This file was changed in this branch and deleted in the incoming branch. Git kept this branch's version."
	case "AU":
		return "This file was added in this branch; the incoming branch has no version of it."
	case "UA":
		return "This file was added in the incoming branch; this branch has no version of it."
	case "DD":
		return "Both branches deleted this file."
	}
	return "Git kept one side of this file. Choose which side to resolve it."
}

// conflictSidesNote says what happened to the file on the side that has no
// version of it, as the pane explains why it shows only one side.
func conflictSidesNote(status string, current bool) string {
	switch status {
	case "DU":
		if current {
			return "The current side deleted this file. The incoming version Git kept is below."
		}
	case "UD":
		if !current {
			return "The incoming side deleted this file. The current version Git kept is below."
		}
	case "AU":
		if !current {
			return "The incoming side has no version of this file. The current version Git kept is below."
		}
	case "UA":
		if current {
			return "The current side has no version of this file. The incoming version Git kept is below."
		}
	case "DD":
		return "Both sides deleted this file."
	}
	return ""
}

// conflictStatusLabel names Git's unmerged status in the pane's badge, as
// "deleted by us" says more than the two letters of the code.
func conflictStatusLabel(status string) string {
	switch status {
	case "DU":
		return "Deleted by us"
	case "UD":
		return "Deleted by them"
	case "AU":
		return "Added by us"
	case "UA":
		return "Added by them"
	case "DD":
		return "Deleted by both"
	}
	return "Merge conflict"
}

// syncConflictEdits adopts what was typed into the conflict editor, finds the
// markers left by the edit and writes the file. It runs at the top of the
// pane's frame, after the editor applied the last key. A failed write stays on
// screen and is tried again when the text changes.
func (a *app) syncConflictEdits() {
	state := a.conflictState
	view := a.conflictView
	if state == nil || view == nil || view.Buffer == nil || !view.Buffer.Modified() {
		return
	}
	version := view.Buffer.Version()
	if a.conflictSaveError != "" && version == a.conflictSaveVersion {
		return
	}
	a.conflictSaveVersion = version

	state.adoptText(view.Buffer.String())
	if err := a.writeConflictFileToDisk(); err != nil {
		if a.conflictSaveError != err.Error() {
			a.conflictSaveError = err.Error()
			a.notify("error", "Could not save the file", err.Error())
		}
		return
	}
	a.conflictSaveError = ""
	view.Buffer.MarkSaved()
}

// writeConflictFileToDisk writes the resolved text of the open conflict over
// its file in the working tree.
func (a *app) writeConflictFileToDisk() error {
	if a.conflictState == nil {
		return nil
	}
	return writeWorkingFile(a.workingFilePath(a.conflictState.path), []byte(a.conflictState.text()))
}

// resolveConflictSide keeps one hunk of the selected conflict, writes the file
// and moves the pane to the next conflict. A failed write puts the previous
// content back, so the pane never claims a resolution the file does not hold.
func (a *app) resolveConflictSide(side conflictSide) {
	if a.conflictState == nil || a.actionBusy {
		return
	}
	if !a.conflictState.resolve(side) {
		return
	}
	if err := a.writeConflictFileToDisk(); err != nil {
		a.conflictState.undoResolution()
		a.notify("error", "Could not write the resolved file", err.Error())
		return
	}
	a.scrollToSelectedConflict()
	a.refreshConflictViewer()
}

// scrollToSelectedConflict brings the conflict the toolbar acts on into view,
// leaving the scroll alone once none is left.
func (a *app) scrollToSelectedConflict() {
	if len(a.conflictState.regions) == 0 {
		return
	}
	a.conflictScrollTo = a.conflictState.selectedLine()
}

// undoConflictResolution puts back the content before the last pick.
func (a *app) undoConflictResolution() {
	if a.conflictState == nil || a.actionBusy {
		return
	}
	state := a.conflictState
	resolved := state.lines
	if !state.undoResolution() {
		return
	}
	if err := a.writeConflictFileToDisk(); err != nil {
		// The file still holds the resolved text, so the pane must keep
		// showing it rather than the content the undo could not write.
		state.lines = resolved
		state.regions = findConflictRegions(resolved, state.markerSize)
		state.clampSelection()
		a.notify("error", "Could not write the resolved file", err.Error())
		return
	}
	a.scrollToSelectedConflict()
	a.refreshConflictViewer()
}

// moveConflictSelection moves the toolbar's conflict and scrolls it into view.
func (a *app) moveConflictSelection(delta int) {
	if a.conflictState == nil {
		return
	}
	if !a.conflictState.moveSelection(delta) {
		return
	}
	a.conflictScrollTo = a.conflictState.selectedLine()
}
