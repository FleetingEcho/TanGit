package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Conflict stages are immutable snapshots: reading them must not resolve or
// otherwise touch the working tree, and an empty stage differs from absence.
func TestConflictStagesReadBaseCurrentIncomingWithoutMutation(t *testing.T) {
	dir := conflictedRepository(t)
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	file := fileNamed(t, snapshot)
	beforeWork, err := os.ReadFile(filepath.Join(dir, file.Path))
	if err != nil {
		t.Fatal(err)
	}
	beforeIndex := gitOutput(t, dir, "ls-files", "-u")
	stages, err := session.ConflictStages(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		stage   ConflictStage
		content string
	}{
		"base":     {stages.Base, "base\n"},
		"current":  {stages.Current, "main\n"},
		"incoming": {stages.Incoming, "feature\n"},
	} {
		if !test.stage.Present || test.stage.Content != test.content {
			t.Errorf("%s stage = %#v, want %q", name, test.stage, test.content)
		}
	}
	afterWork, err := os.ReadFile(filepath.Join(dir, file.Path))
	if err != nil {
		t.Fatal(err)
	}
	if string(afterWork) != string(beforeWork) || gitOutput(t, dir, "ls-files", "-u") != beforeIndex {
		t.Fatal("reading conflict stages changed the working tree or index")
	}
}

func TestConflictStagesDistinguishDeletedSides(t *testing.T) {
	for _, kind := range []string{"DU", "UD"} {
		t.Run(kind, func(t *testing.T) {
			dir := conflictRepository(t, kind)
			session, err := (Opener{}).Open(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := session.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			stages, err := session.ConflictStages(context.Background(), conflictFile(t, snapshot))
			if err != nil {
				t.Fatal(err)
			}
			if kind == "DU" && stages.Current.Present {
				t.Fatal("DU current deletion was reported as an empty file")
			}
			if kind == "UD" && stages.Incoming.Present {
				t.Fatal("UD incoming deletion was reported as an empty file")
			}
			if !stages.Base.Present {
				t.Fatal("delete/modify conflict has no base stage")
			}
		})
	}
}
