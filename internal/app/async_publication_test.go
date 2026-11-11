package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo"

	"lazygitapp/internal/repository"
)

func acknowledgeAsyncPublications(a *app) <-chan bool {
	acknowledged := make(chan bool, 8)
	a.updateChanged = func(fn func() bool) { acknowledged <- fn() }
	return acknowledged
}

type controlledHistoryPublicationSession struct {
	fakeSession
	started chan int
	release chan []repository.Commit
}

func (s *controlledHistoryPublicationSession) RecentCommits(_ context.Context, _, skip int) ([]repository.Commit, error) {
	s.started <- skip
	return <-s.release, nil
}

func TestHistoryPageRepaintsOnlyWhenVisible(t *testing.T) {
	for _, test := range []struct {
		name    string
		tab     int
		repaint bool
	}{
		{name: "history", tab: sectionHistory, repaint: true},
		{name: "graph", tab: sectionGraph, repaint: true},
		{name: "hidden", tab: sectionChanges, repaint: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &controlledHistoryPublicationSession{
				started: make(chan int, 1),
				release: make(chan []repository.Commit, 1),
			}
			a := newApp(fakeOpener{})
			a.session = session
			a.workspaceTab = test.tab
			a.commitsMore = true
			a.snapshot.Commits = []repository.Commit{{Hash: "current"}}
			acknowledged := acknowledgeAsyncPublications(a)

			a.requestMoreCommits()
			if skip := <-session.started; skip != 1 {
				t.Fatalf("history page skip = %d, want 1", skip)
			}
			session.release <- []repository.Commit{{Hash: "older"}}
			if repaint := <-acknowledged; repaint != test.repaint {
				t.Fatalf("history page repaint = %v, want %v", repaint, test.repaint)
			}
			if a.commitsLoading || len(a.snapshot.Commits) != 2 || a.snapshot.Commits[1].Hash != "older" {
				t.Fatalf("history page did not publish its state: loading=%v commits=%#v", a.commitsLoading, a.snapshot.Commits)
			}
		})
	}
}

func TestStaleHistoryPageDoesNotRepaint(t *testing.T) {
	session := &controlledHistoryPublicationSession{
		started: make(chan int, 1),
		release: make(chan []repository.Commit, 1),
	}
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionHistory
	a.commitsMore = true
	a.snapshot.Commits = []repository.Commit{{Hash: "current"}}
	acknowledged := acknowledgeAsyncPublications(a)

	a.requestMoreCommits()
	<-session.started
	a.commitsToken++
	a.commitsLoading = false
	session.release <- []repository.Commit{{Hash: "stale"}}
	if <-acknowledged {
		t.Fatal("a stale history page requested a repaint")
	}
	if len(a.snapshot.Commits) != 1 || a.snapshot.Commits[0].Hash != "current" {
		t.Fatalf("a stale history page replaced commits: %#v", a.snapshot.Commits)
	}
}

type controlledCommitDetailPublicationSession struct {
	fakeSession
	started chan string
	release chan []repository.CommitFile
}

func (s *controlledCommitDetailPublicationSession) CommitFiles(_ context.Context, commit repository.Commit) ([]repository.CommitFile, error) {
	s.started <- commit.Hash
	return <-s.release, nil
}

func TestCommitDetailRetryStillRunsAfterGenerationCleanup(t *testing.T) {
	session := &controlledCommitDetailPublicationSession{
		started: make(chan string, 1),
		release: make(chan []repository.CommitFile, 1),
	}
	a := newApp(fakeOpener{})
	a.session = session
	published := make(chan struct{}, 2)
	a.update = func(fn func()) {
		fn()
		published <- struct{}{}
	}
	commit := repository.Commit{Hash: "commit"}

	a.requestCommitDetails(commit)
	<-session.started
	a.generation++
	session.release <- []repository.CommitFile{{Path: "stale.txt"}}
	<-published
	if a.commitFilesHash != "" || a.commitFilesLoading || len(a.commitFiles) != 0 {
		t.Fatalf("stale commit details did not reset retry state: hash=%q loading=%v files=%#v",
			a.commitFilesHash, a.commitFilesLoading, a.commitFiles)
	}

	a.requestCommitDetails(commit)
	<-session.started
	session.release <- []repository.CommitFile{{Path: "current.txt"}}
	<-published
	if len(a.commitFiles) != 1 || a.commitFiles[0].Path != "current.txt" || a.commitFilesLoading {
		t.Fatalf("retried commit details were not installed: loading=%v files=%#v", a.commitFilesLoading, a.commitFiles)
	}
}

type controlledStashPublicationSession struct {
	fakeSession
	started chan int
	release chan []repository.CommitFile
}

func (s *controlledStashPublicationSession) StashFiles(_ context.Context, index int) ([]repository.CommitFile, error) {
	s.started <- index
	return <-s.release, nil
}

func TestStaleStashGenerationRepaintsRetryCleanup(t *testing.T) {
	session := &controlledStashPublicationSession{
		started: make(chan int, 1),
		release: make(chan []repository.CommitFile, 1),
	}
	a := newApp(fakeOpener{})
	a.session = session
	a.workspaceTab = sectionStash
	acknowledged := acknowledgeAsyncPublications(a)
	stash := repository.Stash{Index: 0, Hash: "stash"}

	a.requestStashDetails(stash)
	<-session.started
	a.generation++
	session.release <- []repository.CommitFile{{Path: "stale.txt"}}
	if !<-acknowledged {
		t.Fatal("visible retry cleanup did not request a repaint")
	}
	if a.stashFilesHash != "" || a.stashFilesLoading || len(a.stashFiles) != 0 {
		t.Fatalf("stale stash read did not reset retry state: hash=%q loading=%v files=%#v",
			a.stashFilesHash, a.stashFilesLoading, a.stashFiles)
	}

	a.requestStashDetails(stash)
	<-session.started
	session.release <- []repository.CommitFile{{Path: "current.txt"}}
	if !<-acknowledged {
		t.Fatal("retried stash details did not request a repaint")
	}
	if len(a.stashFiles) != 1 || a.stashFiles[0].Path != "current.txt" || a.stashFilesLoading {
		t.Fatalf("retried stash details were not installed: loading=%v files=%#v", a.stashFilesLoading, a.stashFiles)
	}
}

type controlledChangePublicationSession struct {
	fakeSession
	diffStarted  chan struct{}
	diffRelease  chan struct{}
	blameStarted chan struct{}
	blameRelease chan struct{}
}

func (s *controlledChangePublicationSession) Diff(context.Context, repository.File, repository.DiffOptions) (repository.Diff, error) {
	close(s.diffStarted)
	<-s.diffRelease
	return repository.Diff{}, nil
}

func (s *controlledChangePublicationSession) BlameFile(context.Context, repository.File) ([]repository.BlameLine, error) {
	close(s.blameStarted)
	<-s.blameRelease
	return nil, nil
}

func TestStaleWorkingTreeDiffDoesNotRepaint(t *testing.T) {
	h := menuTestApp(t)
	session := &controlledChangePublicationSession{diffStarted: make(chan struct{}), diffRelease: make(chan struct{})}
	h.app.session = session
	acknowledged := acknowledgeAsyncPublications(h.app)
	h.app.beginDiff()
	<-session.diffStarted
	h.app.diffGeneration++
	close(session.diffRelease)
	if <-acknowledged {
		t.Fatal("a stale working-tree diff requested a repaint")
	}
}

func TestStaleWorkingTreeBlameDoesNotRepaint(t *testing.T) {
	h := menuTestApp(t)
	session := &controlledChangePublicationSession{blameStarted: make(chan struct{}), blameRelease: make(chan struct{})}
	h.app.session = session
	acknowledged := acknowledgeAsyncPublications(h.app)
	h.app.beginBlame()
	<-session.blameStarted
	h.app.blameGeneration++
	close(session.blameRelease)
	if <-acknowledged {
		t.Fatal("a stale working-tree blame requested a repaint")
	}
}

type controlledClonePublicationOpener struct {
	fakeOpener
	reported chan func(repository.CloneProgress)
	release  chan error
}

func (o *controlledClonePublicationOpener) Clone(_ context.Context, _ repository.CloneOptions, report func(repository.CloneProgress)) error {
	o.reported <- report
	return <-o.release
}

func TestClonePublicationsSkipDuplicateAndStaleProgress(t *testing.T) {
	opener := &controlledClonePublicationOpener{
		reported: make(chan func(repository.CloneProgress), 1),
		release:  make(chan error, 1),
	}
	a := newApp(opener)
	acknowledged := acknowledgeAsyncPublications(a)
	options, _ := cloneOptionsFixture()
	a.openCloneForm()
	a.cloneURL = options.URL
	a.cloneDestination = options.Destination
	a.beginClone()
	report := <-opener.reported
	progress := repository.CloneProgress{Phase: "Receiving objects", Current: 3, Total: 12, Percent: 25, Fraction: 0.25, Known: true}

	report(progress)
	if !<-acknowledged {
		t.Fatal("new clone progress did not request a repaint")
	}
	report(progress)
	if <-acknowledged {
		t.Fatal("duplicate clone progress requested a repaint")
	}

	a.cancelCloneOperation()
	report(repository.CloneProgress{Phase: "Stale progress"})
	if <-acknowledged {
		t.Fatal("stale clone progress requested a repaint")
	}
	opener.release <- context.Canceled
	if <-acknowledged {
		t.Fatal("a cancelled clone completion requested a repaint")
	}
	if a.state != stateEmpty || a.cloneOpen || a.cloneProgress.Known {
		t.Fatalf("a cancelled clone changed visible state: state=%v open=%v progress=%#v",
			a.state, a.cloneOpen, a.cloneProgress)
	}
}

func TestEmptyCloneParentSelectionDoesNotRepaint(t *testing.T) {
	started := make(chan mygo.OpenDialogOptions, 1)
	release := make(chan []string, 1)
	restore := chooseCloneParent
	chooseCloneParent = func(options mygo.OpenDialogOptions) ([]string, error) {
		started <- options
		return <-release, nil
	}
	t.Cleanup(func() { chooseCloneParent = restore })

	a := newApp(fakeOpener{})
	acknowledged := acknowledgeAsyncPublications(a)
	a.openCloneForm()
	a.cloneURL = "git@example.com:owner/project.git"
	destination := a.cloneDestination
	a.browseCloneParent()
	<-started
	release <- nil
	if <-acknowledged {
		t.Fatal("an empty clone parent selection requested a repaint")
	}
	if !a.cloneOpen || a.cloneDestination != destination || a.cloneError != "" {
		t.Fatalf("an empty clone parent selection changed the form: open=%v destination=%q error=%q",
			a.cloneOpen, a.cloneDestination, a.cloneError)
	}
}
