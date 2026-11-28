package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// resolvedPlaceholders returns the paths shown as Resolved in the merge
// section of rows.
func resolvedPlaceholders(rows []changeRow) []string {
	var paths []string
	for _, row := range rows {
		if row.isFile() && row.resolved {
			paths = append(paths, row.file.Path)
		}
	}
	return paths
}

func rowsInSection(rows []changeRow, merge, staged bool) []changeRow {
	var out []changeRow
	for _, row := range rows {
		if row.isFile() && row.merge == merge && row.staged == staged {
			out = append(out, row)
		}
	}
	return out
}

func newResolvedRowsApp(t *testing.T) *app {
	t.Helper()
	a := newApp(fileRecoveryOpener{})
	a.snapshot.OperationState = &repository.OperationState{Type: repository.OperationTypeMerge, Fingerprint: "merge"}
	return a
}

func TestResolvedPlaceholderLivesInMergeSectionBesideRealStagedRow(t *testing.T) {
	files := []repository.File{
		{Path: "a.txt", Resolved: true},
		{Path: "a.txt", Status: "M ", Staged: true, Tracked: true},
		{Path: "b.txt", Status: "UU", HasMergeConflicts: true},
		{Path: "c.txt", Status: " M", Unstaged: true, Tracked: true},
	}
	rows := buildChangeRows(files, "", false, true, true, true)

	merge := rowsInSection(rows, true, false)
	if len(merge) != 2 {
		t.Fatalf("merge rows = %#v, want the placeholder and the conflict", merge)
	}
	if !merge[0].resolved || merge[0].file.Path != "a.txt" || merge[0].file.HasMergeConflicts {
		t.Fatalf("placeholder row = %#v", merge[0])
	}
	if merge[1].resolved || !merge[1].file.HasMergeConflicts {
		t.Fatalf("a real conflict must not look resolved: %#v", merge[1])
	}
	if staged := rowsInSection(rows, false, true); len(staged) != 1 || staged[0].file.Path != "a.txt" {
		t.Fatalf("the real staged row was lost: %#v", staged)
	}
	if changes := rowsInSection(rows, false, false); len(changes) != 1 || changes[0].file.Path != "c.txt" {
		t.Fatalf("changes rows = %#v", changes)
	}
	if rows[0].header != "Merge Changes" || rows[0].count != 2 {
		t.Fatalf("merge header = %#v", rows[0])
	}
}

func TestResolvedPlaceholderIsNeverTheChosenOrActionableRow(t *testing.T) {
	files := []repository.File{
		{Path: "a.txt", Resolved: true},
		{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true},
	}
	a := newResolvedRowsApp(t)
	a.changeRows = buildChangeRows(files, "", false, true, true, true)

	first := firstChangeFile(a.changeRows)
	if first < 0 || a.changeRows[first].resolved {
		t.Fatalf("first selectable row = %d (%#v), must skip the placeholder", first, a.changeRows)
	}
	// Restoring a choice after an action must land on the file Git reports, not
	// on the placeholder that shares its path and side.
	index := changeRowIndex(a.changeRows, "a.txt", false)
	if index < 0 || a.changeRows[index].resolved {
		t.Fatalf("changeRowIndex chose the placeholder: %d %#v", index, a.changeRows)
	}
	for i, row := range a.changeRows {
		if row.resolved {
			a.selectedFile = i
			if _, ok := a.selectedChange(); ok {
				t.Fatal("a Resolved placeholder offered file actions (stage, discard, diff)")
			}
		}
	}
}

func TestResolvedPlaceholderTreeModeKeepsItsMarker(t *testing.T) {
	files := []repository.File{
		{Path: "dir/a.txt", Resolved: true},
		{Path: "dir/b.txt", Status: "UU", HasMergeConflicts: true},
	}
	rows := buildChangeTreeRows(files, "", false, true, true, true, nil)
	if got := resolvedPlaceholders(rows); len(got) != 1 || got[0] != "dir/a.txt" {
		t.Fatalf("tree rows lost the Resolved marker: %#v", rows)
	}
}

func TestResolvedPlaceholdersEndWithTheOperationOrWhenConflictedAgain(t *testing.T) {
	a := newResolvedRowsApp(t)
	a.markConflictResolved(repository.File{Path: "a.txt", Status: "UU", HasMergeConflicts: true})
	a.markConflictResolved(repository.File{Path: "b.txt", Status: "UU", HasMergeConflicts: true})
	a.snapshot.Files = []repository.File{{Path: "c.txt", Status: "UU", HasMergeConflicts: true}}

	rows := a.changeRowsFrom(a.snapshot.Files)
	if got := resolvedPlaceholders(rows); strings.Join(got, ",") != "a.txt,b.txt" {
		t.Fatalf("placeholders = %v, want a.txt and b.txt in path order", got)
	}
	// The recorded placeholder never carries the conflict, so no conflict
	// action can run from it.
	for _, row := range rows {
		if row.resolved && (row.file.HasMergeConflicts || row.file.Staged || row.file.Unstaged) {
			t.Fatalf("placeholder keeps live state: %#v", row.file)
		}
	}

	// Restore unresolved: Git reports a.txt as conflicted again.
	a.snapshot.Files = append(a.snapshot.Files, repository.File{Path: "a.txt", Status: "UU", HasMergeConflicts: true})
	rows = a.changeRowsFrom(a.snapshot.Files)
	if got := resolvedPlaceholders(rows); strings.Join(got, ",") != "b.txt" {
		t.Fatalf("a conflict that returned still shows Resolved: %v", got)
	}
	if _, kept := a.resolvedConflicts["a.txt"]; kept {
		t.Fatal("the stale placeholder was not forgotten")
	}

	// Continue or Abort ends the operation.
	a.snapshot.OperationState = nil
	rows = a.changeRowsFrom(a.snapshot.Files)
	if got := resolvedPlaceholders(rows); len(got) != 0 || len(a.resolvedConflicts) != 0 {
		t.Fatalf("placeholders outlived the operation: %v %v", got, a.resolvedConflicts)
	}
}

func TestResolvedPlaceholderRespectsSearchAndLeavesCountsHonest(t *testing.T) {
	a := newResolvedRowsApp(t)
	a.markConflictResolved(repository.File{Path: "keep.txt"})
	a.markConflictResolved(repository.File{Path: "other.txt"})
	a.fileSearch = "keep"
	rows := a.changeRowsFrom(nil)
	if got := resolvedPlaceholders(rows); len(got) != 1 || got[0] != "keep.txt" {
		t.Fatalf("search did not filter placeholders: %v", got)
	}
}

func newAddAddConflict(t *testing.T) (string, repository.Session) {
	t.Helper()
	return newFileRecoveryConflict(t, conflictVersions{
		name: "conflict.txt", base: []byte("base\n"), current: []byte("current\n"), incoming: []byte("incoming\n"),
	})
}

// A file resolved to HEAD's own content leaves Git's status entirely. It must
// stay visible as Resolved, and nothing about the user's work may be lost: the
// original conflict is restorable byte for byte, and untracked work survives
// an Abort.
func TestRealAppUseCurrentKeepsResolvedRowAndNothingIsLost(t *testing.T) {
	dir, session := newAddAddConflict(t)
	path := "conflict.txt"
	originalFile := readRecoveryFile(t, dir, path)
	originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path)
	untracked := filepath.Join(dir, "abort-should-keep-untracked.txt")
	if err := os.WriteFile(untracked, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := newFileRecoveryApp(t, dir, session)
	h.app.confirm = nil
	if err := h.tester.Click("Use current " + path); err != nil {
		t.Fatalf("Use current: %v; texts=%q", err, h.texts())
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "Use current finished", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && !hasConflictedFiles(h.app.snapshot.Files)
	})
	h.tester.Frame()

	// The status no longer lists the path; the list still does, as Resolved.
	for _, file := range h.app.snapshot.Files {
		if file.Path == path {
			t.Fatalf("Use current == HEAD should leave Git's status, got %#v", file)
		}
	}
	if got := resolvedPlaceholders(h.app.changeRows); len(got) != 1 || got[0] != path {
		t.Fatalf("resolved file vanished from Merge Changes: rows=%#v texts=%q", h.app.changeRows, h.texts())
	}
	found := false
	for _, text := range h.texts() {
		if text == "Resolved" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Resolved label drawn: %q", h.texts())
	}
	// Clicking the placeholder must not open an action or a diff.
	for i, row := range h.app.changeRows {
		if row.resolved {
			h.app.selectedFile = i
			if _, ok := h.app.selectedChange(); ok {
				t.Fatal("placeholder is selectable as a real change")
			}
		}
	}
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, []byte("current\n")) {
		t.Fatalf("worktree = %q", got)
	}

	// Undo: restore the original unresolved conflict.
	record := onlyRecoveryRecord(t, session)
	if err := session.(repository.FileRecoverySession).RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("restored bytes = %q, want %q", got, originalFile)
	}
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path); got != originalIndex {
		t.Fatalf("restored index = %q, want %q", got, originalIndex)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h.app.snapshot = snapshot
	rows := h.app.changeRowsFrom(snapshot.Files)
	if got := resolvedPlaceholders(rows); len(got) != 0 {
		t.Fatalf("a restored conflict still reads Resolved: %v", got)
	}
	if got := rowsInSection(rows, true, false); len(got) != 1 || !got[0].file.HasMergeConflicts {
		t.Fatalf("restored conflict is not offered for resolution again: %#v", got)
	}

	// Abort ends the operation: the placeholder goes, untracked work stays.
	runGitIn(t, dir, "merge", "--abort")
	snapshot, err = session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	h.app.snapshot = snapshot
	if got := resolvedPlaceholders(h.app.changeRowsFrom(snapshot.Files)); len(got) != 0 {
		t.Fatalf("placeholder outlived Abort: %v", got)
	}
	if got := readRecoveryFile(t, dir, "abort-should-keep-untracked.txt"); string(got) != "mine\n" {
		t.Fatalf("untracked work lost: %q", got)
	}
}

// Mark resolved stages the file exactly as it is, markers and all: nothing is
// rewritten, the file is staged and still listed under Merge Changes as
// Resolved, and the original conflict can be restored.
func TestRealAppMarkResolvedKeepsBytesStagesAndStaysUndoable(t *testing.T) {
	dir, session := newAddAddConflict(t)
	path := "conflict.txt"
	originalFile := readRecoveryFile(t, dir, path)
	originalIndex := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path)

	h := newFileRecoveryApp(t, dir, session)
	h.app.confirm = nil
	if err := h.tester.Click("Mark resolved " + path); err != nil {
		t.Fatalf("Mark resolved: %v; texts=%q", err, h.texts())
	}
	h.waitFor(t, "Mark resolved finished", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && !hasConflictedFiles(h.app.snapshot.Files)
	})
	h.tester.Frame()

	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("Mark resolved rewrote the file: %q", got)
	}
	if got := resolvedPlaceholders(h.app.changeRows); len(got) != 1 || got[0] != path {
		t.Fatalf("Merge Changes lost the resolved file: %#v", h.app.changeRows)
	}
	staged := rowsInSection(h.app.changeRows, false, true)
	if len(staged) != 1 || staged[0].file.Path != path {
		t.Fatalf("file did not reach Staged Changes: %#v", h.app.changeRows)
	}

	record := onlyRecoveryRecord(t, session)
	if err := session.(repository.FileRecoverySession).RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("restored bytes = %q", got)
	}
	if got := runGitIn(t, dir, "ls-files", "--stage", "-z", "--", path); got != originalIndex {
		t.Fatalf("restored index = %q, want %q", got, originalIndex)
	}
}

// Use incoming replaces the file with the other side. The original must stay
// recoverable, the result is staged, and the row reads Resolved.
func TestRealAppUseIncomingStagesAndKeepsResolvedRow(t *testing.T) {
	dir, session := newAddAddConflict(t)
	path := "conflict.txt"
	originalFile := readRecoveryFile(t, dir, path)

	h := newFileRecoveryApp(t, dir, session)
	h.app.confirm = nil
	if err := h.tester.Click("Use incoming " + path); err != nil {
		t.Fatalf("Use incoming: %v; texts=%q", err, h.texts())
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "Use incoming finished", func() bool {
		return !h.app.actionBusy && h.app.actionError == "" && !hasConflictedFiles(h.app.snapshot.Files)
	})
	h.tester.Frame()

	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, []byte("incoming\n")) {
		t.Fatalf("Use incoming wrote %q", got)
	}
	if got := resolvedPlaceholders(h.app.changeRows); len(got) != 1 {
		t.Fatalf("resolved row missing: %#v", h.app.changeRows)
	}
	if staged := rowsInSection(h.app.changeRows, false, true); len(staged) != 1 {
		t.Fatalf("incoming result is not staged: %#v", h.app.changeRows)
	}
	record := onlyRecoveryRecord(t, session)
	if err := session.(repository.FileRecoverySession).RestoreFileRecovery(context.Background(), record.OperationID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := readRecoveryFile(t, dir, path); !bytes.Equal(got, originalFile) {
		t.Fatalf("restored bytes = %q", got)
	}
}
