package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDropCommitRemovesItFromHistory(t *testing.T) {
	dir, session := newThreeCommitHistory(t)
	snapshot := historySnapshot(t, session)
	middle := snapshot.Commits[1]

	if err := session.DropCommit(context.Background(), middle); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(historySubjects(t, dir), ", "); got != "tip, initial" {
		t.Fatalf("history after drop = %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "two.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the dropped commit's file remains: %v", err)
	}
}

func TestMoveCommitSwapsWithItsNeighbor(t *testing.T) {
	tests := []struct {
		name      string
		direction CommitMove
		want      string
	}{
		{name: "toward HEAD", direction: MoveCommitNewer, want: "middle, tip, initial"},
		{name: "toward older history", direction: MoveCommitOlder, want: "tip, initial, middle"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir, session := newThreeCommitHistory(t)
			middle := historySnapshot(t, session).Commits[1]
			if err := session.MoveCommit(context.Background(), middle, test.direction); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(historySubjects(t, dir), ", "); got != test.want {
				t.Fatalf("history after move = %s, want %s", got, test.want)
			}
			for _, file := range []string{"one.txt", "two.txt", "three.txt"} {
				if _, err := os.Stat(filepath.Join(dir, file)); err != nil {
					t.Fatalf("%s is missing after move: %v", file, err)
				}
			}
		})
	}
}

func TestCombineCommitSquashesOrFixesUpIntoOlderCommit(t *testing.T) {
	tests := []struct {
		name         string
		mode         CombineMode
		wantTipInMsg bool
	}{
		{name: "squash keeps both messages", mode: CombineSquash, wantTipInMsg: true},
		{name: "fixup drops the selected message", mode: CombineFixup, wantTipInMsg: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir, session := newThreeCommitHistory(t)
			tip := historySnapshot(t, session).Commits[0]
			if err := session.CombineCommit(context.Background(), tip, test.mode); err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(historySubjects(t, dir), ", "); got != "middle, initial" {
				t.Fatalf("history after combine = %s", got)
			}
			message := gitOutput(t, dir, "log", "-1", "--format=%B")
			if strings.Contains(message, "tip") != test.wantTipInMsg {
				t.Fatalf("combined message = %q, want tip included = %v", message, test.wantTipInMsg)
			}
			if got := gitOutput(t, dir, "show", "HEAD:three.txt"); got != "three\n" {
				t.Fatalf("combined commit lost the selected changes: %q", got)
			}
		})
	}
}

func TestAmendCommitAddsStagedChangesToHeadOrOlderCommit(t *testing.T) {
	for _, target := range []struct {
		name  string
		index int
	}{
		{name: "HEAD", index: 0},
		{name: "older commit", index: 1},
	} {
		t.Run(target.name, func(t *testing.T) {
			dir, session := newThreeCommitHistory(t)
			before := historySnapshot(t, session)
			commit := before.Commits[target.index]
			if err := os.WriteFile(filepath.Join(dir, "one.txt"), []byte("amended content\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			runGit(t, dir, "add", "one.txt")

			if err := session.AmendCommit(context.Background(), commit); err != nil {
				t.Fatal(err)
			}
			after := historySnapshot(t, session)
			if len(after.Commits) != 3 {
				t.Fatalf("amend left %d commits, want 3", len(after.Commits))
			}
			if got := gitOutput(t, dir, "show", after.Commits[target.index].Hash+":one.txt"); got != "amended content\n" {
				t.Fatalf("selected commit has %q", got)
			}
			if got := strings.TrimSpace(gitOutput(t, dir, "diff", "--cached")); got != "" {
				t.Fatalf("amend left staged changes: %s", got)
			}
			for _, subject := range historySubjects(t, dir) {
				if strings.HasPrefix(subject, "fixup!") {
					t.Fatalf("amend left its implementation commit behind: %v", historySubjects(t, dir))
				}
			}
		})
	}
}

func TestCreateAndApplyFixupCommit(t *testing.T) {
	dir, session := newThreeCommitHistory(t)
	before := historySnapshot(t, session)
	middle := before.Commits[1]
	if err := os.WriteFile(filepath.Join(dir, "two.txt"), []byte("fixed content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "two.txt")

	if err := session.CreateFixupCommit(context.Background(), middle); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(historySubjects(t, dir), ", "); got != "fixup! middle, tip, middle, initial" {
		t.Fatalf("history after fixup creation = %s", got)
	}
	if got := gitOutput(t, dir, "show", middle.Hash+":two.txt"); got != "two\n" {
		t.Fatalf("creating the fixup rewrote its target immediately: %q", got)
	}

	if err := session.ApplyFixupCommits(context.Background(), middle); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(historySubjects(t, dir), ", "); got != "tip, middle, initial" {
		t.Fatalf("history after autosquash = %s", got)
	}
	after := historySnapshot(t, session)
	if got := gitOutput(t, dir, "show", after.Commits[1].Hash+":two.txt"); got != "fixed content\n" {
		t.Fatalf("autosquash target has %q", got)
	}
	if after.Commits[2].Hash != before.Commits[2].Hash {
		t.Fatal("autosquash rewrote the commit before its selected base")
	}
}

func TestHistoryEditsRejectInvalidBoundariesAndEmptyIndex(t *testing.T) {
	dir, session := newThreeCommitHistory(t)
	commits := historySnapshot(t, session).Commits
	head, root := commits[0], commits[len(commits)-1]

	if err := session.MoveCommit(context.Background(), head, MoveCommitNewer); err == nil || !strings.Contains(err.Error(), "cannot move any newer") {
		t.Fatalf("moving HEAD newer returned %v", err)
	}
	if err := session.MoveCommit(context.Background(), root, MoveCommitOlder); err == nil || !strings.Contains(err.Error(), "cannot move any older") {
		t.Fatalf("moving root older returned %v", err)
	}
	if err := session.CombineCommit(context.Background(), root, CombineSquash); err == nil || !strings.Contains(err.Error(), "no older commit") {
		t.Fatalf("squashing root returned %v", err)
	}
	if err := session.AmendCommit(context.Background(), head); err == nil || !strings.Contains(err.Error(), "stage changes first") {
		t.Fatalf("amending without staged changes returned %v", err)
	}
	if got := strings.Join(historySubjects(t, dir), ", "); got != "tip, middle, initial" {
		t.Fatalf("rejected operations changed history: %s", got)
	}
}

func newThreeCommitHistory(t *testing.T) (string, Session) {
	t.Helper()
	dir, session := newHistoryEditingSession(t)
	writeHistoryCommit(t, dir, "one.txt", "one\n", "initial")
	writeHistoryCommit(t, dir, "two.txt", "two\n", "middle")
	writeHistoryCommit(t, dir, "three.txt", "three\n", "tip")
	return dir, session
}

func historySnapshot(t *testing.T, session Session) Snapshot {
	t.Helper()
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func historySubjects(t *testing.T, dir string) []string {
	t.Helper()
	output := strings.TrimSpace(gitOutput(t, dir, "log", "--format=%s"))
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}
