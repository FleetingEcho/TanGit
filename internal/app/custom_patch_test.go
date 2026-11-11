package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
	gitpatch "github.com/jesseduffield/lazygit/pkg/commands/patch"

	"lazygitapp/internal/repository"
)

const customPatchFixture = `diff --git a/changed.txt b/changed.txt
index 1111111..2222222 100644
--- a/changed.txt
+++ b/changed.txt
@@ -1,4 +1,4 @@
 one
-two
+TWO
 three
-four
+FOUR
`

func customPatchTestEntry(hash, path string) customPatchEntry {
	return customPatchEntry{
		commit:      repository.Commit{Hash: hash, ShortHash: hash[:7]},
		file:        repository.CommitFile{Path: path, Status: "M"},
		sourcePatch: strings.ReplaceAll(customPatchFixture, "changed.txt", path),
		whole:       true,
	}
}

func TestCustomPatchMergesPartialsAndWholeFileDeterministically(t *testing.T) {
	entry := customPatchTestEntry("1234567890abcdef", "changed.txt")
	entry.whole = false
	entry.lines = []gitpatch.LineIdentity{{LineNumber: 2, IsDeletion: true}}
	next, order, err := mergeCustomPatchEntries(nil, []customPatchEntry{entry}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if order != 5 || next[0].order != 4 {
		t.Fatalf("insertion order = %d, next = %d", next[0].order, order)
	}
	whole := entry
	whole.whole = true
	merged, _, err := mergeCustomPatchEntries(next, []customPatchEntry{whole}, order)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 || !merged[0].whole || len(merged[0].lines) != 0 {
		t.Fatalf("whole file did not supersede the partial: %+v", merged[0])
	}
	text, err := customPatchText(merged)
	if err != nil || text != customPatchFixture {
		t.Fatalf("whole patch mismatch: %v\n%s", err, text)
	}
}

func TestCustomPatchRejectsCrossCommitPathCollisionsAtomically(t *testing.T) {
	existing := customPatchTestEntry("1234567890abcdef", "same.txt")
	batch := []customPatchEntry{
		customPatchTestEntry("abcdef1234567890", "other.txt"),
		customPatchTestEntry("abcdef1234567890", "same.txt"),
	}
	merged, _, err := mergeCustomPatchEntries([]customPatchEntry{existing}, batch, 1)
	if err == nil || merged != nil {
		t.Fatalf("cross-commit collision was accepted: %+v, %v", merged, err)
	}

	rename := customPatchTestEntry("abcdef1234567890", "new.txt")
	rename.file.PreviousPath = "same.txt"
	if _, _, err := mergeCustomPatchEntries([]customPatchEntry{existing}, []customPatchEntry{rename}, 1); err == nil {
		t.Fatal("rename old-path collision was accepted")
	}
}

func TestCustomPatchTransformsOnlySelectedLineIdentities(t *testing.T) {
	entry := customPatchTestEntry("1234567890abcdef", "changed.txt")
	entry.whole = false
	parsed, err := editor.ParsePatch(customPatchFixture)
	if err != nil || len(parsed) != 1 {
		t.Fatalf("parse fixture: %v", err)
	}
	// Selecting an addition includes its paired deletion, as the line picker does.
	entry.lines = selectedLineIdentities(parsed[0], editor.DiffLineRange{Side: editor.DiffNew, Start: 1, End: 1})
	text, err := customPatchText([]customPatchEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "-two\n+TWO") || strings.Contains(text, "FOUR") {
		t.Fatalf("partial patch kept the wrong lines:\n%s", text)
	}
	if !strings.HasSuffix(text, "\n") {
		t.Fatal("patch does not end in one newline")
	}
}

func TestCustomPatchPartialRejectsModeChangesWithoutDroppingWholeFileModes(t *testing.T) {
	entry := customPatchTestEntry("1234567890abcdef", "changed.txt")
	entry.sourcePatch = strings.Replace(entry.sourcePatch, "index 1111111..2222222 100644", "old mode 100644\nnew mode 100755\nindex 1111111..2222222", 1)
	entry.whole = false
	entry.lines = []gitpatch.LineIdentity{{LineNumber: 2, IsDeletion: true}}
	if _, err := customPatchText([]customPatchEntry{entry}); err == nil {
		t.Fatal("partial selection silently included an unselected mode change")
	}
	entry.whole = true
	text, err := customPatchText([]customPatchEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "old mode 100644\nnew mode 100755\n") {
		t.Fatalf("whole-file selection lost its mode change:\n%s", text)
	}
}

func TestCustomPatchHunkIdentitiesUseOldAndNewLineNumbers(t *testing.T) {
	parsed, err := editor.ParsePatch(customPatchFixture)
	if err != nil || len(parsed) != 1 {
		t.Fatalf("parse fixture: %v", err)
	}
	lines := customPatchHunkIdentities(parsed[0], 0)
	want := []gitpatch.LineIdentity{
		{LineNumber: 2, IsDeletion: true}, {LineNumber: 2},
		{LineNumber: 4, IsDeletion: true}, {LineNumber: 4},
	}
	if len(lines) != len(want) {
		t.Fatalf("identities = %+v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("identity %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
}
