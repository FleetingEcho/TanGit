package app

import (
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

func TestDiffStateUsesInlineWordsAndAnnotatesMovedLines(t *testing.T) {
	patch := "diff --git a/code.txt b/code.txt\n" +
		"--- a/code.txt\n+++ b/code.txt\n" +
		"@@ -1,5 +1,5 @@\n" +
		" start\n" +
		"-move this line\n" +
		" middle\n" +
		"-old changed value\n" +
		"+new changed value\n" +
		" tail\n" +
		"+move this line\n"
	state, err := buildDiffState(editor.DiffUnified, false, repository.File{Path: "code.txt"},
		repository.Diff{Patch: patch}, repository.DiffOptions{ContextLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if state.Inline != editor.DiffInlineWordAlt {
		t.Fatalf("inline mode = %v, want changed-word highlighting", state.Inline)
	}
	if state.RenderAnnotation == nil || len(state.Annotations) != 2 {
		t.Fatalf("moved annotations = %#v", state.Annotations)
	}
	old, new := state.Annotations[0], state.Annotations[1]
	if old.Side != editor.DiffOld || old.Line != 1 || new.Side != editor.DiffNew || new.Line != 4 {
		t.Fatalf("annotation positions = %#v", state.Annotations)
	}
	for _, annotation := range state.Annotations {
		pair, ok := annotation.Key.(movedLine)
		if !ok || pair.From != 2 || pair.To != 5 {
			t.Errorf("move pair = %#v", annotation.Key)
		}
	}
}

func TestMovedLineDetectionRejectsEditsBlankAndDuplicateLines(t *testing.T) {
	patch, err := editor.ParsePatch("diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1,4 +1,4 @@\n-old edited line\n+new edited line\n-repeated line\n-repeated line\n+repeated line\n+repeated line\n-x\n+y\n")
	if err != nil {
		t.Fatal(err)
	}
	if annotations := movedLineAnnotations(patch[0]); len(annotations) != 0 {
		t.Fatalf("unrelated edits or ambiguous lines were marked moved: %#v", annotations)
	}
}
