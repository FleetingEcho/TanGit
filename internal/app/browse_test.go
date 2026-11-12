package app

import (
	"testing"

	"github.com/egoist/mygo"
)

func holdRepositoryFolderDialog(t *testing.T) (chan mygo.OpenDialogOptions, chan []string) {
	t.Helper()
	started := make(chan mygo.OpenDialogOptions, 1)
	release := make(chan []string)
	restore := chooseRepositoryFolder
	chooseRepositoryFolder = func(options mygo.OpenDialogOptions) ([]string, error) {
		started <- options
		return <-release, nil
	}
	t.Cleanup(func() { chooseRepositoryFolder = restore })
	return started, release
}

func observeBrowseRepaints(a *app) chan bool {
	repaints := make(chan bool, 1)
	a.updateChanged = func(fn func() bool) { repaints <- fn() }
	return repaints
}

func TestBrowseRejectsCancelledDialogCompletion(t *testing.T) {
	a := newApp(fakeOpener{})
	repaints := observeBrowseRepaints(a)
	var opened []string
	a.claimOpen = func(path string) bool {
		opened = append(opened, path)
		return true
	}
	a.path = "/work/original"
	started, release := holdRepositoryFolderDialog(t)
	a.browse()
	<-started

	a.path = "/work/current"
	a.cancelLoading()
	release <- []string{"/work/stale"}
	if <-repaints {
		t.Fatal("a cancelled dialog requested a repaint")
	}
	if len(opened) != 0 {
		t.Fatalf("a cancelled dialog opened repositories: %q", opened)
	}
	if a.path != "/work/current" || a.state != stateEmpty || a.errMessage != "" {
		t.Fatalf("a cancelled dialog changed state: path=%q state=%v error=%q",
			a.path, a.state, a.errMessage)
	}
}

func TestBrowseEmptySelectionDoesNotRepaint(t *testing.T) {
	a := newApp(fakeOpener{})
	repaints := observeBrowseRepaints(a)
	var opened []string
	a.claimOpen = func(path string) bool {
		opened = append(opened, path)
		return true
	}
	a.path = "/work/current"
	started, release := holdRepositoryFolderDialog(t)
	a.browse()
	<-started

	release <- nil
	if <-repaints {
		t.Fatal("an empty selection requested a repaint")
	}
	if len(opened) != 0 {
		t.Fatalf("an empty selection opened repositories: %q", opened)
	}
	if a.path != "/work/current" || a.state != stateEmpty || a.errMessage != "" {
		t.Fatalf("an empty selection changed state: path=%q state=%v error=%q",
			a.path, a.state, a.errMessage)
	}
}

func TestBrowseCurrentSelectionUsesCapturedOptionsAndOpens(t *testing.T) {
	a := newApp(fakeOpener{})
	repaints := observeBrowseRepaints(a)
	var opened []string
	a.claimOpen = func(path string) bool {
		opened = append(opened, path)
		return true
	}
	parent := &mygo.Window{}
	a.window = parent
	a.path = "/work/default"
	started, release := holdRepositoryFolderDialog(t)
	a.browse()
	options := <-started
	if options.Parent != parent || options.DefaultPath != "/work/default" ||
		options.Title != "Open Git Repository" || options.ButtonLabel != "Open" || !options.Directory {
		t.Fatalf("repository dialog options = %+v", options)
	}

	release <- []string{"/work/selected"}
	if !<-repaints {
		t.Fatal("a current selection did not request a repaint")
	}
	if len(opened) != 1 || opened[0] != "/work/selected" || a.path != "/work/selected" {
		t.Fatalf("current selection opened %q with path %q", opened, a.path)
	}
}
