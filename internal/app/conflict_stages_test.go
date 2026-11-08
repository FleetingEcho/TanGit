package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestThreeWayConflictTabsKeepStagesImmutable(t *testing.T) {
	const result = "<<<<<<< HEAD\ncurrent\n=======\nincoming\n>>>>>>> feature\n"
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() { readWorkingFile, writeWorkingFile = originalRead, originalWrite })
	readWorkingFile = func(string) ([]byte, error) { return []byte(result), nil }
	writeWorkingFile = func(string, []byte) error { return nil }
	stages := repository.ConflictStages{
		Base:     repository.ConflictStage{Present: true, Content: "base\n"},
		Current:  repository.ConflictStage{Present: true, Content: "current\n"},
		Incoming: repository.ConflictStage{Present: true, Content: "incoming\n"},
	}
	a := newApp(fakeOpener{
		result: openResult{snapshot: repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{{
			Path: "conflict.txt", Status: "UU", Tracked: true, Unstaged: true, HasMergeConflicts: true,
		}}}},
		conflictStages: stages,
	})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the three-way conflict", func() bool { return a.conflictState != nil })
	if a.conflictStageTab != 3 || a.conflictView.ReadOnly {
		t.Fatal("Result is not the initial editable tab")
	}

	for _, test := range []struct {
		label string
		tab   int
		text  string
	}{
		{"Base", 0, "base\n"}, {"Current", 1, "current\n"}, {"Incoming", 2, "incoming\n"},
	} {
		if err := h.tester.Click(test.label); err != nil {
			t.Fatal(err)
		}
		h.tester.Frame()
		view := a.conflictStageViews[test.tab]
		if view == nil || !view.ReadOnly || view.Buffer.String() != test.text {
			t.Fatalf("%s view = %#v", test.label, view)
		}
		if a.conflictView.Buffer.String() != result {
			t.Fatalf("viewing %s changed Result to %q", test.label, a.conflictView.Buffer.String())
		}
	}
	if err := h.tester.Click("Result"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Use current"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if a.conflictState.text() != "current\n" {
		t.Fatalf("Result was not resolved: %q", a.conflictState.text())
	}
	if a.conflictStages.Base.Content != "base\n" || a.conflictStages.Incoming.Content != "incoming\n" {
		t.Fatal("editing Result mutated an index-stage buffer")
	}
}

func TestThreeWayConflictMissingStageExplainsDeletion(t *testing.T) {
	originalRead := readWorkingFile
	t.Cleanup(func() { readWorkingFile = originalRead })
	readWorkingFile = func(string) ([]byte, error) { return []byte("incoming\n"), nil }
	a := newApp(fakeOpener{
		result: openResult{snapshot: repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{{
			Path: "gone.txt", Status: "DU", Tracked: true, Unstaged: true, HasMergeConflicts: true,
		}}}},
		conflictStages: repository.ConflictStages{
			Base: repository.ConflictStage{Present: true, Content: "base\n"}, Incoming: repository.ConflictStage{Present: true, Content: "incoming\n"},
		},
	})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "the delete/modify conflict", func() bool { return a.conflictSides != nil })
	if err := h.tester.Click("Current"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if !h.tester.HasText("No version at this stage") || !h.tester.HasText("This side deleted the file, or never added it.") {
		t.Fatalf("missing-stage explanation is absent: %q", h.texts())
	}
}
