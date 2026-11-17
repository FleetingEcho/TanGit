package repository

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitpatch "github.com/jesseduffield/lazygit/pkg/commands/patch"
)

type historyPatchFixture struct {
	dir       string
	session   Session
	base      string
	source    string
	later     string
	head      string
	patch     string
	sourceSHA string
}

func newHistoryPatchFixture(t *testing.T) historyPatchFixture {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	writePatchImportFile(t, dir, "a.txt", []byte("one\ntwo\nthree\nfour\n"))
	writePatchImportFile(t, dir, "b.txt", []byte("base other\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writePatchImportFile(t, dir, "a.txt", []byte("one\nSELECTED\nthree\nfour\n"))
	writePatchImportFile(t, dir, "b.txt", []byte("source other\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "source")
	source := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writePatchImportFile(t, dir, "later.txt", []byte("later\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "later")
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	opened, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := opened.CommitFilePatch(context.Background(), Commit{Hash: source, Parents: []string{base}}, CommitFile{Path: "a.txt", Status: "M"})
	if err != nil {
		t.Fatal(err)
	}
	return historyPatchFixture{dir: dir, session: opened, base: base, source: source, head: head, patch: patch, sourceSHA: source}
}

func (f historyPatchFixture) request(kind HistoryPatchKind) HistoryPatchRequest {
	return HistoryPatchRequest{Kind: kind, Patch: f.patch, SourceSHA: f.source, ExpectedHeadSHA: f.head, ExpectedHeadRef: "refs/heads/main"}
}

func (f historyPatchFixture) apply(t *testing.T, request HistoryPatchRequest) {
	t.Helper()
	if err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request); err != nil {
		records, _ := f.session.Operations(context.Background())
		t.Fatalf("%v\ndir=%s\nlog=%s\nrecords=%#v", err, f.dir, gitOutput(t, f.dir, "log", "--oneline", "--decorate", "-5"), records)
	}
}

func TestHistoryPatchRemoveReplaysRealHistoryAndJournalsDigest(t *testing.T) {
	f := newHistoryPatchFixture(t)
	f.apply(t, f.request(HistoryPatchRemove))
	if got := gitOutput(t, f.dir, "log", "--reverse", "--format=%s"); got != "base\nsource\nlater\n" {
		t.Fatalf("history subjects = %q", got)
	}
	source := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^"))
	if source == f.source {
		t.Fatal("source was not rewritten")
	}
	if got := gitOutput(t, f.dir, "show", source+":a.txt"); got != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("source retained the removed patch: %q", got)
	}
	if got := gitOutput(t, f.dir, "show", source+":b.txt"); got != "source other\n" {
		t.Fatalf("unselected source change was lost: %q", got)
	}
	if got := gitOutput(t, f.dir, "show", "HEAD:later.txt"); got != "later\n" {
		t.Fatalf("later commit was not replayed: %q", got)
	}
	records, err := f.session.Operations(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatalf("journal records = %d, %v", len(records), err)
	}
	history := records[0].HistoryPatch
	if history == nil || history.SourceSHA != f.source || history.PatchDigest != historyPatchDigest(f.patch) || history.Step != HistoryStepComplete {
		t.Fatalf("journal history = %#v", history)
	}
	if history.BeforeSHA == "" || history.AfterSHA == "" || records[0].Status != OperationSucceeded {
		t.Fatalf("journal did not record durable step/result: %#v", records[0])
	}
}

func TestHistoryPatchMoveToOlderAndNewerCommits(t *testing.T) {
	t.Run("older uses synthetic source parent", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		request := f.request(HistoryPatchMoveCommit)
		request.DestinationSHA = f.base
		f.apply(t, request)
		base := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^^"))
		source := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^"))
		if got := gitOutput(t, f.dir, "show", base+":a.txt"); got != "one\nSELECTED\nthree\nfour\n" {
			t.Fatalf("older destination lacks patch: %q", got)
		}
		if got := gitOutput(t, f.dir, "diff", base, source, "--", "a.txt"); strings.Contains(got, "SELECTED") {
			t.Fatalf("source still contains the moved patch in its diff:\n%s", got)
		}
		if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\nSELECTED\nthree\nfour\n" {
			t.Fatalf("final content changed: %q", got)
		}
	})
	t.Run("newer uses compensation patch", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		request := f.request(HistoryPatchMoveCommit)
		request.DestinationSHA = f.head
		f.apply(t, request)
		source := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^"))
		if got := gitOutput(t, f.dir, "show", source+":a.txt"); got != "one\ntwo\nthree\nfour\n" {
			t.Fatalf("newer-move source kept patch: %q", got)
		}
		if got := gitOutput(t, f.dir, "diff", "HEAD^", "HEAD", "--", "a.txt"); !strings.Contains(got, "+SELECTED") {
			t.Fatalf("newer destination did not receive compensation patch:\n%s", got)
		}
		if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\nSELECTED\nthree\nfour\n" {
			t.Fatalf("final content changed: %q", got)
		}
	})
}

func TestHistoryPatchNewAfterAndBeforePreserveHistoryContent(t *testing.T) {
	t.Run("after", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		request := f.request(HistoryPatchNewAfter)
		request.Summary = "extracted patch"
		request.Description = "body"
		f.apply(t, request)
		if got := gitOutput(t, f.dir, "log", "--reverse", "--format=%s"); got != "base\nsource\nextracted patch\nlater\n" {
			t.Fatalf("new-after order = %q", got)
		}
		if got := gitOutput(t, f.dir, "show", "HEAD^^:a.txt"); got != "one\ntwo\nthree\nfour\n" {
			t.Fatalf("new-after source kept selected patch: %q", got)
		}
		if got := gitOutput(t, f.dir, "diff", "HEAD^^", "HEAD^", "--", "a.txt"); !strings.Contains(got, "+SELECTED") {
			t.Fatalf("new-after commit lacks compensation patch:\n%s", got)
		}
		if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\nSELECTED\nthree\nfour\n" {
			t.Fatalf("new-after final content = %q", got)
		}
	})
	t.Run("before", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		request := f.request(HistoryPatchNewBefore)
		request.Summary = "prerequisite"
		f.apply(t, request)
		if got := gitOutput(t, f.dir, "log", "--reverse", "--format=%s"); got != "base\nprerequisite\nsource\nlater\n" {
			t.Fatalf("new-before order = %q", got)
		}
		extracted := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^^"))
		source := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^"))
		if got := gitOutput(t, f.dir, "show", extracted+":a.txt"); got != "one\nSELECTED\nthree\nfour\n" {
			t.Fatalf("new-before extracted tree = %q", got)
		}
		if got := gitOutput(t, f.dir, "diff", extracted, source, "--", "a.txt"); strings.Contains(got, "+SELECTED") || strings.Contains(got, "-SELECTED") {
			t.Fatalf("source did not compensate synthetic parent exactly once:\n%s", got)
		}
		if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\nSELECTED\nthree\nfour\n" {
			t.Fatalf("new-before final content = %q", got)
		}
	})
}

func historyPatchReplayConflictFixture(t *testing.T) historyPatchFixture {
	t.Helper()
	f := newHistoryPatchFixture(t)
	writePatchImportFile(t, f.dir, "a.txt", []byte("one\nLATER\nthree\nfour\n"))
	runGit(t, f.dir, "add", "a.txt")
	runGit(t, f.dir, "commit", "--amend", "--no-edit")
	f.head = strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD"))
	return f
}

func historyPatchStateAfterRestart(t *testing.T, dir string) (Session, OperationState) {
	t.Helper()
	reopened, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := reopened.(OperationSession).OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Type != OperationTypeRebase || state.HistoryPatch == nil || state.Fingerprint == "" {
		t.Fatalf("restarted operation state = %#v", state)
	}
	return reopened, state
}

func TestHistoryPatchMoveNewerSeparatesDestinationPickAndCompensationConflicts(t *testing.T) {
	f := historyPatchReplayConflictFixture(t)
	request := f.request(HistoryPatchMoveCommit)
	request.DestinationSHA = f.head
	if err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request); err == nil {
		t.Fatal("destination pick conflict was expected")
	}
	reopened, state := historyPatchStateAfterRestart(t, f.dir)
	if state.HistoryPatch.Step != HistoryStepAwaitDestination || !state.HistoryPatch.Conflict {
		t.Fatalf("destination pick was mistaken for patch apply: %#v", state.HistoryPatch)
	}
	writePatchImportFile(t, f.dir, "a.txt", []byte("one\nLATER\nthree\nfour\n"))
	runGit(t, f.dir, "add", "a.txt")
	state, err := reopened.(OperationSession).OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantFinal := "SELECTED"
	err = reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
	})
	if err != nil {
		reopened, state = historyPatchStateAfterRestart(t, f.dir)
		if state.HistoryPatch.Step != HistoryStepApplyDestination || state.HistoryPatch.Stage != historyStageApply || !state.HistoryPatch.Conflict {
			t.Fatalf("compensation stop was skipped: %#v", state.HistoryPatch)
		}
		writePatchImportFile(t, f.dir, "a.txt", []byte("one\nFINAL\nthree\nfour\n"))
		runGit(t, f.dir, "add", "a.txt")
		state, err = reopened.(OperationSession).OperationState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		wantFinal = "FINAL"
		if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
			Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
		}); err != nil {
			t.Fatal(err)
		}
	}
	source := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^"))
	if got := gitOutput(t, f.dir, "show", source+":a.txt"); got != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("move-newer source = %q", got)
	}
	if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\n"+wantFinal+"\nthree\nfour\n" {
		latest, _ := reopened.(OperationSession).OperationState(context.Background())
		t.Fatalf("destination after compensation = %q, want %s; state=%#v; history=%+v; err=%v", got, wantFinal, latest, latest.HistoryPatch, err)
	}
	if got := gitOutput(t, f.dir, "status", "--porcelain"); got != "" {
		t.Fatalf("destination replay left work: %q", got)
	}
}

func TestHistoryPatchMoveNewerReconcilesExternallyReachedBreak(t *testing.T) {
	f := historyPatchReplayConflictFixture(t)
	request := f.request(HistoryPatchMoveCommit)
	request.DestinationSHA = f.head
	if err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request); err == nil {
		t.Fatal("destination pick conflict was expected")
	}
	writePatchImportFile(t, f.dir, "a.txt", []byte("one\nLATER\nthree\nfour\n"))
	runGit(t, f.dir, "add", "a.txt")
	command := exec.Command("git", "-C", f.dir, "rebase", "--continue")
	command.Env = append(os.Environ(), "GIT_EDITOR=:")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("external rebase --continue: %v: %s", err, output)
	}
	reopened, state := historyPatchStateAfterRestart(t, f.dir)
	if state.HistoryPatch.Step != HistoryStepAwaitDestination || state.StoppedSHA != "" {
		t.Fatalf("test premise: external break not reached: %#v (rebase stop %q)", state.HistoryPatch, state.StoppedSHA)
	}
	err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
	})
	if err != nil {
		state, err = reopened.(OperationSession).OperationState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if state.HistoryPatch.Step != HistoryStepApplyDestination {
			t.Fatalf("external break was consumed without compensation: %#v", state.HistoryPatch)
		}
		writePatchImportFile(t, f.dir, "a.txt", []byte("one\nFINAL\nthree\nfour\n"))
		runGit(t, f.dir, "add", "a.txt")
		state, err = reopened.(OperationSession).OperationState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
			Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\nSELECTED\nthree\nfour\n" && got != "one\nFINAL\nthree\nfour\n" {
		t.Fatalf("external continue skipped compensation: %q", got)
	}
}

func TestHistoryPatchReplayConflictCASContinueAndAbortAfterRestart(t *testing.T) {
	for _, abort := range []bool{false, true} {
		name := "continue"
		if abort {
			name = "abort"
		}
		t.Run(name, func(t *testing.T) {
			f := historyPatchReplayConflictFixture(t)
			err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), f.request(HistoryPatchRemove))
			if err == nil {
				t.Fatal("descendant replay unexpectedly had no conflict")
			}
			reopened, state := historyPatchStateAfterRestart(t, f.dir)
			if state.HistoryPatch.Step != HistoryStepReplay || state.HistoryPatch.SourceSHA != f.source || !state.HistoryPatch.Conflict {
				t.Fatalf("persisted replay stop = %#v", state.HistoryPatch)
			}
			if !abort {
				writePatchImportFile(t, f.dir, "a.txt", []byte("one\nLATER\nthree\nfour\n"))
				runGit(t, f.dir, "add", "a.txt")
			}
			if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
				Action: OperationActionContinue, ExpectedFingerprint: "stale",
			}); !errors.Is(err, ErrOperationChanged) {
				t.Fatalf("stale fingerprint was accepted: %v", err)
			}
			current, err := reopened.(OperationSession).OperationState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			action := OperationActionContinue
			if abort {
				action = OperationActionAbort
			}
			if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
				Action: action, ExpectedFingerprint: current.Fingerprint,
			}); err != nil {
				t.Fatalf("drive %s: %v", action, err)
			}
			finished, err := reopened.(OperationSession).OperationState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if finished.Type != OperationTypeNone {
				t.Fatalf("%s left operation active: %#v", action, finished)
			}
			if abort {
				if got := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD")); got != f.head {
					t.Fatalf("abort HEAD = %s, want original %s", got, f.head)
				}
			} else {
				source := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^"))
				if got := gitOutput(t, f.dir, "show", source+":a.txt"); got != "one\ntwo\nthree\nfour\n" {
					t.Fatalf("continued source = %q", got)
				}
				if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\nLATER\nthree\nfour\n" {
					t.Fatalf("continued final content = %q", got)
				}
			}
		})
	}
}

func TestHistoryPatchMoveIndexFinalConflictIsDurable(t *testing.T) {
	for _, abort := range []bool{false, true} {
		name := "continue"
		if abort {
			name = "abort"
		}
		t.Run(name, func(t *testing.T) {
			f := historyPatchReplayConflictFixture(t)
			err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), f.request(HistoryPatchMoveIndex))
			if err == nil {
				t.Fatal("first replay conflict was expected")
			}
			reopened, _ := historyPatchStateAfterRestart(t, f.dir)
			writePatchImportFile(t, f.dir, "a.txt", []byte("one\nLATER\nthree\nfour\n"))
			runGit(t, f.dir, "add", "a.txt")
			state, err := reopened.(OperationSession).OperationState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			// Continue the descendant conflict. Move-out then immediately hits
			// its separate final compensation-patch conflict after Git's rebase
			// metadata is already gone.
			err = reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
				Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
			})
			if err == nil {
				t.Fatal("final move-to-index conflict was expected")
			}
			reopened, state = historyPatchStateAfterRestart(t, f.dir)
			if state.HistoryPatch.Step != HistoryStepMoveIndex || state.HistoryPatch.CompensationSHA == "" {
				t.Fatalf("final stop was not persisted: %#v", state.HistoryPatch)
			}
			if s := f.session.(*gitSession).workingTreeState(); s.Any() {
				t.Fatalf("test premise: final conflict should have no native Git operation: %#v", s)
			}
			action := OperationActionAbort
			if !abort {
				writePatchImportFile(t, f.dir, "a.txt", []byte("one\nFINAL\nthree\nfour\n"))
				runGit(t, f.dir, "add", "a.txt")
				state, err = reopened.(OperationSession).OperationState(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				action = OperationActionContinue
			}
			if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
				Action: action, ExpectedFingerprint: state.Fingerprint,
			}); err != nil {
				t.Fatalf("final %s: %v", action, err)
			}
			if abort {
				if got := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD")); got != f.head {
					t.Fatalf("final abort HEAD = %s, want %s", got, f.head)
				}
				if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\nLATER\nthree\nfour\n" {
					t.Fatalf("final abort changed original content: %q", got)
				}
			} else {
				if got := gitOutput(t, f.dir, "show", ":a.txt"); got != "one\nFINAL\nthree\nfour\n" {
					t.Fatalf("final resolved index = %q", got)
				}
				if got := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD")); got == f.head {
					t.Fatal("final continue did not preserve rewritten history")
				}
			}
		})
	}
}

func TestHistoryPatchDestinationApplyConflictCanFinishAfterRestart(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	writePatchImportFile(t, dir, "a.txt", []byte("one\ntwo\nthree\n"))
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "base-destination")
	destination := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writePatchImportFile(t, dir, "a.txt", []byte("one\nMID\nthree\n"))
	runGit(t, dir, "commit", "-am", "middle-context")
	mid := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writePatchImportFile(t, dir, "a.txt", []byte("one\nSELECTED\nthree\n"))
	runGit(t, dir, "commit", "-am", "source")
	source := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	text, err := session.CommitFilePatch(context.Background(), Commit{Hash: source, Parents: []string{mid}}, CommitFile{Path: "a.txt", Status: "M"})
	if err != nil {
		t.Fatal(err)
	}
	request := HistoryPatchRequest{Kind: HistoryPatchMoveCommit, Patch: text, SourceSHA: source, DestinationSHA: destination, ExpectedHeadSHA: source, ExpectedHeadRef: "refs/heads/main"}
	if err := session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request); err == nil {
		t.Fatal("destination patch conflict was expected")
	}
	reopened, stored := historyPatchStateAfterRestart(t, dir)
	if stored.HistoryPatch.Step != HistoryStepApplyDestination || !stored.HistoryPatch.Conflict {
		t.Fatalf("destination conflict state = %#v", stored.HistoryPatch)
	}
	writePatchImportFile(t, dir, "a.txt", []byte("one\nSELECTED\nthree\n"))
	runGit(t, dir, "add", "a.txt")
	stored, err = reopened.(OperationSession).OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: stored.Fingerprint,
	}); err == nil {
		t.Fatal("replaying the independent middle commit should also conflict on the amended destination")
	}
	stored, err = reopened.(OperationSession).OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	writePatchImportFile(t, dir, "a.txt", []byte("one\nSELECTED\nthree\n"))
	runGit(t, dir, "add", "a.txt")
	stored, err = reopened.(OperationSession).OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: stored.Fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, dir, "show", "HEAD:a.txt"); got != "one\nSELECTED\nthree\n" {
		t.Fatalf("continued destination conflict result = %q", got)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD^")); got == destination {
		t.Fatal("destination was not amended")
	}
	if got := gitOutput(t, dir, "status", "--porcelain"); got != "" {
		t.Fatalf("continued destination left work: %q", got)
	}
}

func TestHistoryPatchNewBeforeDoesNotDuplicateAdjacentAddedLines(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	writePatchImportFile(t, dir, "a.txt", []byte("one\nfour\n"))
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "base")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writePatchImportFile(t, dir, "a.txt", []byte("one\nSELECTED\nNEARBY\nfour\n"))
	runGit(t, dir, "commit", "-am", "source")
	source := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	whole, err := session.CommitFilePatch(context.Background(), Commit{Hash: source, Parents: []string{base}}, CommitFile{Path: "a.txt", Status: "M"})
	if err != nil {
		t.Fatal(err)
	}
	parsed := gitpatch.Parse(whole)
	indices := gitpatch.ChangeLineIndicesForLines(parsed, []gitpatch.LineIdentity{{LineNumber: 2}})
	partial := parsed.Transform(gitpatch.TransformOpts{IncludedLineIndices: indices}).FormatPlain()
	if !strings.Contains(partial, "+SELECTED") || strings.Contains(partial, "+NEARBY") {
		t.Fatalf("test partial selection =\n%s", partial)
	}
	err = session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), HistoryPatchRequest{
		Kind: HistoryPatchNewBefore, Patch: partial, SourceSHA: source,
		ExpectedHeadSHA: source, ExpectedHeadRef: "refs/heads/main", Summary: "prerequisite",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, dir, "show", "HEAD^:a.txt"); got != "one\nSELECTED\nfour\n" {
		t.Fatalf("adjacent new-before prerequisite = %q", got)
	}
	if got := gitOutput(t, dir, "show", "HEAD:a.txt"); got != "one\nSELECTED\nNEARBY\nfour\n" || strings.Count(got, "SELECTED") != 1 {
		t.Fatalf("adjacent lines were duplicated or lost: %q", got)
	}
	if got := gitOutput(t, dir, "diff", "HEAD^", "HEAD", "--", "a.txt"); strings.Contains(got, "+SELECTED") || strings.Contains(got, "-SELECTED") || !strings.Contains(got, "+NEARBY") {
		t.Fatalf("source diff did not retain only the unselected adjacent line:\n%s", got)
	}
}

func TestHistoryPatchAmendFailureRetriesAmendWithoutReapply(t *testing.T) {
	f := newHistoryPatchFixture(t)
	allow := filepath.Join(t.TempDir(), "allow-commit")
	hook := "#!/bin/sh\ntest -f " + allow + "\n"
	if err := os.WriteFile(filepath.Join(f.dir, ".git", "hooks", "pre-commit"), []byte(hook), 0o700); err != nil {
		t.Fatal(err)
	}
	request := f.request(HistoryPatchNewAfter)
	request.Summary = "extracted after retry"
	if err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request); err == nil {
		t.Fatal("the intentional pre-commit rejection was expected")
	}
	reopened, state := historyPatchStateAfterRestart(t, f.dir)
	if state.HistoryPatch.Step != HistoryStepReverseSource || state.HistoryPatch.Stage != historyStageAmend {
		t.Fatalf("failed amend lost its durable stage: %#v", state.HistoryPatch)
	}
	// Simulate a process stop after apply succeeded but before Stage=amend
	// became durable. Exact index-tree reconciliation must advance without a
	// second reverse apply.
	records, err := reopened.Operations(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatalf("amend failure records = %#v, %v", records, err)
	}
	records[0].HistoryPatch.Stage = historyStageApply
	if err := reopened.(*gitSession).writeHistoryPatchRecord(records[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allow, []byte("allowed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err = reopened.(OperationSession).OperationState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	if got := gitOutput(t, f.dir, "log", "--reverse", "--format=%s"); got != "base\nsource\nextracted after retry\nlater\n" {
		t.Fatalf("amend retry history = %q", got)
	}
	if got := gitOutput(t, f.dir, "show", "HEAD:a.txt"); got != "one\nSELECTED\nthree\nfour\n" || strings.Count(got, "SELECTED") != 1 {
		t.Fatalf("amend retry repeated the reverse patch: %q", got)
	}
}

func TestHistoryPatchSyntheticRecoveryChecksBranchAndExternalAbort(t *testing.T) {
	t.Run("changed branch has no actions", func(t *testing.T) {
		f := historyPatchReplayConflictFixture(t)
		err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), f.request(HistoryPatchMoveIndex))
		if err == nil {
			t.Fatal("replay conflict expected")
		}
		reopened, state := historyPatchStateAfterRestart(t, f.dir)
		writePatchImportFile(t, f.dir, "a.txt", []byte("one\nLATER\nthree\nfour\n"))
		runGit(t, f.dir, "add", "a.txt")
		state, err = reopened.(OperationSession).OperationState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
			Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
		}); err == nil {
			t.Fatal("final move-index conflict expected")
		}
		finalHead := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD"))
		runGit(t, f.dir, "checkout", "-b", "other")
		state, err = reopened.(OperationSession).OperationState(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(state.Actions) != 0 || len(state.FullRefs) == 0 || state.FullRefs[0] != "refs/heads/other" {
			t.Fatalf("changed-branch synthetic state = %#v", state)
		}
		if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
			Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint,
		}); err == nil {
			t.Fatal("abort on another branch was accepted")
		}
		if got := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD")); got != finalHead {
			t.Fatalf("foreign branch was reset to %s", got)
		}
	})
	t.Run("external abort cannot become success", func(t *testing.T) {
		f := historyPatchReplayConflictFixture(t)
		err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), f.request(HistoryPatchRemove))
		if err == nil {
			t.Fatal("replay conflict expected")
		}
		runGit(t, f.dir, "rebase", "--abort")
		reopened, state := historyPatchStateAfterRestart(t, f.dir)
		if len(state.Actions) != 1 || !state.Can(OperationActionAbort) || state.Can(OperationActionContinue) {
			t.Fatalf("external abort exposed Continue: %#v", state.Actions)
		}
		if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
			Action: OperationActionContinue, ExpectedFingerprint: state.Fingerprint,
		}); err == nil {
			t.Fatal("externally aborted rebase was converted into success")
		}
		if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
			Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint,
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestHistoryPatchDroppedStashIsNotAcceptedAsRestored(t *testing.T) {
	f := newHistoryPatchFixture(t)
	writePatchImportFile(t, f.dir, "a.txt", []byte("one\nUNSTAGED ORIGINAL\nthree\nfour\n"))
	session := f.session.(*gitSession)
	state := &HistoryPatchState{Version: historyPatchJournalVersion, Kind: HistoryPatchMoveIndex, Step: HistoryStepRestoreStash, NeedsStash: true, PatchDigest: historyPatchDigest(f.patch), CompensationSHA: historyPatchDigest(f.patch)}
	record, err := session.beginOperation(OperationHistoryPatch, "stash restoration proof", UndoResetHead, nil)
	if err != nil {
		t.Fatal(err)
	}
	record.HistoryPatch = state
	record.ActiveOperation = OperationTypeRebase
	record, err = session.ensureHistoryStash(record)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, f.dir, "stash", "drop", "stash@{0}")
	if err := session.restoreHistoryStash(record); err == nil || !strings.Contains(err.Error(), "without a recorded pop") {
		t.Fatalf("external stash drop was accepted as restoration: %v", err)
	}
	if got := string(readPatchImportFile(t, f.dir, "a.txt")); got != "one\nSELECTED\nthree\nfour\n" {
		t.Fatalf("test premise: stash push failed to clean worktree: %q", got)
	}
}

func TestHistoryPatchMoveOutRestoresDedicatedStashAndDirtyAbort(t *testing.T) {
	t.Run("successful restore", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		writePatchImportFile(t, f.dir, "dirty.txt", []byte("original dirty work\n"))
		runGit(t, f.dir, "add", "dirty.txt")
		f.apply(t, f.request(HistoryPatchMoveIndex))
		if got := string(readPatchImportFile(t, f.dir, "dirty.txt")); got != "original dirty work\n" {
			t.Fatalf("dedicated stash content = %q", got)
		}
		if got := gitOutput(t, f.dir, "show", ":a.txt"); got != "one\nSELECTED\nthree\nfour\n" {
			t.Fatalf("custom patch index = %q", got)
		}
		if got := gitOutput(t, f.dir, "stash", "list", "--format=%H"); got != "" {
			t.Fatalf("dedicated stash was not removed: %q", got)
		}
	})
	t.Run("staged and unstaged survive separately", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		writePatchImportFile(t, f.dir, "dirty.txt", []byte("BASE\n"))
		runGit(t, f.dir, "add", "dirty.txt")
		runGit(t, f.dir, "commit", "-m", "dirty base")
		f.head = strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD"))
		writePatchImportFile(t, f.dir, "dirty.txt", []byte("STAGED\n"))
		runGit(t, f.dir, "add", "dirty.txt")
		writePatchImportFile(t, f.dir, "dirty.txt", []byte("WORKTREE\n"))
		f.apply(t, f.request(HistoryPatchMoveIndex))
		if got := gitOutput(t, f.dir, "show", ":dirty.txt"); got != "STAGED\n" {
			t.Fatalf("original staged index = %q", got)
		}
		if got := string(readPatchImportFile(t, f.dir, "dirty.txt")); got != "WORKTREE\n" {
			t.Fatalf("original unstaged worktree = %q", got)
		}
	})
	t.Run("abort restores exact original work", func(t *testing.T) {
		f := historyPatchReplayConflictFixture(t)
		writePatchImportFile(t, f.dir, "dirty.txt", []byte("original dirty work\n"))
		runGit(t, f.dir, "add", "dirty.txt")
		err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), f.request(HistoryPatchMoveIndex))
		if err == nil {
			t.Fatal("replay conflict was expected")
		}
		reopened, state := historyPatchStateAfterRestart(t, f.dir)
		if err := reopened.(OperationSession).DriveOperation(context.Background(), OperationDriveOptions{
			Action: OperationActionAbort, ExpectedFingerprint: state.Fingerprint,
		}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD")); got != f.head {
			t.Fatalf("dirty abort HEAD = %s, want %s", got, f.head)
		}
		if got := string(readPatchImportFile(t, f.dir, "dirty.txt")); got != "original dirty work\n" {
			t.Fatalf("dirty abort content = %q", got)
		}
		if got := gitOutput(t, f.dir, "show", ":dirty.txt"); got != "original dirty work\n" {
			t.Fatalf("dirty abort did not restore the original index: %q", got)
		}
		if got := gitOutput(t, f.dir, "stash", "list", "--format=%H"); got != "" {
			t.Fatalf("dirty abort left its dedicated stash: %q", got)
		}
	})
}

func TestHistoryPatchRejectsStaleHEADAndUnsupportedSourcesBeforeMutation(t *testing.T) {
	t.Run("stale HEAD", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		request := f.request(HistoryPatchRemove)
		request.ExpectedHeadSHA = f.base
		before := importRepositoryFingerprint(t, f.dir)
		err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request)
		if !errors.Is(err, ErrOperationChanged) {
			t.Fatalf("stale HEAD error = %v", err)
		}
		if after := importRepositoryFingerprint(t, f.dir); after != before {
			t.Fatal("stale HEAD changed the repository")
		}
	})
	t.Run("same SHA different branch", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		runGit(t, f.dir, "checkout", "-b", "other")
		request := f.request(HistoryPatchRemove)
		before := importRepositoryFingerprint(t, f.dir)
		err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request)
		if !errors.Is(err, ErrOperationChanged) {
			t.Fatalf("same-SHA branch error = %v", err)
		}
		if after := importRepositoryFingerprint(t, f.dir); after != before {
			t.Fatal("same-SHA branch mismatch changed the repository")
		}
	})
	t.Run("new before root", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		request := f.request(HistoryPatchNewBefore)
		request.SourceSHA = f.base
		request.Summary = "not allowed"
		before := importRepositoryFingerprint(t, f.dir)
		err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request)
		if err == nil || !strings.Contains(err.Error(), "root") {
			t.Fatalf("root new-before error = %v", err)
		}
		if after := importRepositoryFingerprint(t, f.dir); after != before {
			t.Fatal("unsupported root new-before changed the repository")
		}
	})
	t.Run("merge", func(t *testing.T) {
		f := newHistoryPatchFixture(t)
		runGit(t, f.dir, "checkout", "-b", "side", f.source)
		writePatchImportFile(t, f.dir, "side.txt", []byte("side\n"))
		runGit(t, f.dir, "add", "side.txt")
		runGit(t, f.dir, "commit", "-m", "side")
		runGit(t, f.dir, "checkout", "main")
		writePatchImportFile(t, f.dir, "main.txt", []byte("main\n"))
		runGit(t, f.dir, "add", "main.txt")
		runGit(t, f.dir, "commit", "-m", "main-only")
		runGit(t, f.dir, "merge", "--no-ff", "side", "-m", "merge")
		merge := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD"))
		request := f.request(HistoryPatchRemove)
		request.SourceSHA, request.ExpectedHeadSHA = merge, merge
		before := importRepositoryFingerprint(t, f.dir)
		err := f.session.(HistoryPatchSession).ApplyHistoryPatch(context.Background(), request)
		if err == nil || !strings.Contains(err.Error(), "merge") {
			t.Fatalf("merge source error = %v", err)
		}
		if after := importRepositoryFingerprint(t, f.dir); after != before {
			t.Fatal("merge rejection changed the repository")
		}
	})
}

func TestHistoryPatchMoveOutLeavesCompensationInIndices(t *testing.T) {
	f := newHistoryPatchFixture(t)
	f.apply(t, f.request(HistoryPatchMoveIndex))
	source := strings.TrimSpace(gitOutput(t, f.dir, "rev-parse", "HEAD^"))
	if got := gitOutput(t, f.dir, "show", source+":a.txt"); got != "one\ntwo\nthree\nfour\n" {
		t.Fatalf("move-out source = %q", got)
	}
	if got := gitOutput(t, f.dir, "show", ":a.txt"); got != "one\nSELECTED\nthree\nfour\n" {
		t.Fatalf("moved patch is not staged: %q", got)
	}
	content := gitOutput(t, f.dir, "show", ":a.txt")
	data, err := os.ReadFile(filepath.Join(f.dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("index/worktree differ: index %q, worktree %q", content, data)
	}
	if got := gitOutput(t, f.dir, "diff", "HEAD", "--", "a.txt"); !strings.Contains(got, "+SELECTED") {
		t.Fatalf("moved patch is absent from worktree: %s", got)
	}
}
