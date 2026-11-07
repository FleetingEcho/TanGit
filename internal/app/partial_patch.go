package app

import (
	"fmt"

	"github.com/egoist/mygo/ui/editor"
	"github.com/jesseduffield/lazygit/pkg/commands/patch"

	"lazygitapp/internal/repository"
)

// diffSupportsLineStaging reports whether the pane may offer to stage or
// unstage a selection of this diff. A staged, unstaged or untracked diff of a
// file is a patch the index can take lines out of; a merge conflict has a
// combined diff neither side of which a partial patch can address.
func diffSupportsLineStaging(file repository.File, kind string) bool {
	if file.HasMergeConflicts {
		return false
	}
	switch kind {
	case "Unstaged", "Untracked":
		return true
	case "Staged":
		// A file deleted in the index has no entry there to take lines back
		// into, so a partial unstage can only fail; undoing a deletion is
		// whole-file work.
		return !file.Deleted
	}
	return false
}

// partialPatch builds the diff holding only the lines the user selected,
// so the index can take just those. reverse builds it for taking the lines
// back out of the index; it is how the staged side is addressed.
//
// The second result says the selection covers every change of the file, in
// which case the file is staged or unstaged as a whole instead: the diff of a
// file that is wholly added or deleted describes the file itself, and applying
// it line by line would leave an empty file in the index rather than an entry
// missing or an entry added.
//
// The selection numbers the lines of the pane's side as the viewer lays them
// out, which is the patch's hunk lines without the hunks' headers and without
// the contexts the pane folds away. A line is named to the patch by the line
// it has in the file — an addition by its line in the new file, a deletion by
// its line in the old one — which tells the two halves of a changed line
// apart. Context lines are not changes, so selecting them alone builds
// nothing.
func partialPatch(patchText string, file repository.File, selection editor.DiffLineRange, reverse bool) (string, bool, error) {
	parsed, err := editor.ParsePatch(patchText)
	if err != nil {
		return "", false, fmt.Errorf("read the file diff: %w", err)
	}
	if len(parsed) != 1 {
		return "", false, fmt.Errorf("the file diff covers %d files, not one", len(parsed))
	}
	lines := selectedLineIdentities(parsed[0], selection)
	if len(lines) == 0 {
		return "", false, nil
	}
	filePatch := patch.Parse(patchText)
	indices := patch.ChangeLineIndicesForLines(filePatch, lines)
	if len(indices) == 0 {
		return "", false, nil
	}
	if len(indices) >= changeLineCount(filePatch) {
		return "", true, nil
	}
	return filePatch.Transform(patch.TransformOpts{
		Reverse:             reverse,
		IncludedLineIndices: indices,
		FileNameOverride:    file.Path,
	}).FormatPlain(), false, nil
}

// changeLineCount is how many of a patch's lines change the file, rather than
// being context or header.
func changeLineCount(filePatch *patch.Patch) int {
	count := 0
	for _, line := range filePatch.Lines() {
		if line.IsAddition() || line.IsDeletion() {
			count++
		}
	}
	return count
}

// selectedLineIdentities names the change lines a selection covers, by where
// they sit in the file. The pane shows the patch's hunk lines in order, on
// each side only those the side has, so walking the hunks the same way maps a
// selected line back to the line of the file it is.
//
// A selected addition brings its block's deletions with it: the pane draws a
// changed line as the old line deleted and the new one added, and staging only
// the addition would leave the old line beside it. Selecting only deletions
// stages those alone, which is how lines are taken away.
func selectedLineIdentities(filePatch editor.FilePatch, selection editor.DiffLineRange) []patch.LineIdentity {
	// changeLine is one line of a run of changes: where the patch names it,
	// and where the pane shows it on its side.
	type changeLine struct {
		identity patch.LineIdentity
		partial  int
		side     editor.DiffSide
	}
	var lines []patch.LineIdentity
	oldPartial, newPartial := 0, 0 // each side's line in the pane, from 0
	for _, hunk := range filePatch.Hunks {
		oldLine, newLine := hunk.OldStart, hunk.NewStart
		i := 0
		for i < len(hunk.Lines) {
			line := hunk.Lines[i]
			if line == "" || line[0] == '\\' {
				i++
				continue
			}
			if line[0] == ' ' {
				oldPartial++
				newPartial++
				oldLine++
				newLine++
				i++
				continue
			}
			// A run of changes, which git writes as its deletions then its
			// additions.
			run := make([]changeLine, 0, 4)
			for i < len(hunk.Lines) {
				l := hunk.Lines[i]
				if l == "" || l[0] == '\\' {
					i++
					continue
				}
				if l[0] != '-' && l[0] != '+' {
					break
				}
				if l[0] == '-' {
					run = append(run, changeLine{
						identity: patch.LineIdentity{LineNumber: oldLine, IsDeletion: true},
						partial:  oldPartial,
						side:     editor.DiffOld,
					})
					oldPartial++
					oldLine++
				} else {
					run = append(run, changeLine{
						identity: patch.LineIdentity{LineNumber: newLine},
						partial:  newPartial,
						side:     editor.DiffNew,
					})
					newPartial++
					newLine++
				}
				i++
			}
			takesAdditions := false
			for _, change := range run {
				if change.side == selection.Side &&
					change.partial >= selection.Start && change.partial <= selection.End {
					if change.side == editor.DiffNew {
						takesAdditions = true
					}
					lines = append(lines, change.identity)
				}
			}
			if takesAdditions {
				for _, change := range run {
					if change.side == editor.DiffOld {
						lines = append(lines, change.identity)
					}
				}
			}
		}
	}
	return lines
}
