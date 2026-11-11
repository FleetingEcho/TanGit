package app

import (
	"os"
	"path/filepath"
	"testing"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

func TestRemoteRefreshIgnoresHostStatAndReloadsSameStatusDiff(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "a.txt")
	if err := os.WriteFile(filename, []byte("first local version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := repository.Snapshot{Root: root, Branch: "main", Files: []repository.File{changedFile("a.txt")}}
	beforeLocal := signSnapshotForWorkspace(snapshot, false)
	beforeRemote := signSnapshotForWorkspace(snapshot, true)
	// A same-Git-status edit substantially changes the host size/mtime. The SSH
	// signature must not acquire either fact, even though this exact remote path
	// also exists and is readable on the host.
	if err := os.WriteFile(filename, []byte("a much longer second local version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterLocal := signSnapshotForWorkspace(snapshot, false)
	afterRemote := signSnapshotForWorkspace(snapshot, true)
	if beforeLocal.files == afterLocal.files {
		t.Fatal("test did not change the local size/mtime signature")
	}
	if beforeRemote != afterRemote {
		t.Fatalf("remote signature read host metadata:\n%q\n%q", beforeRemote, afterRemote)
	}

	opener := &liveOpener{snapshot: snapshot}
	s, _, updated := newLiveShell(t, opener)
	a := openLiveRepo(t, s, updated, root)
	a.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: root}
	a.changeRows = a.changeRowsFrom(a.snapshot.Files)
	a.selectedFile = changeRowIndex(a.changeRows, "a.txt", false)
	if _, ok := a.selectedChange(); !ok {
		t.Fatal("test did not select the remote change")
	}
	if signSnapshotForWorkspace(a.snapshot, true) != signSnapshotForWorkspace(opener.current(), true) {
		t.Fatal("test did not arrange equal remote signatures")
	}
	generation := a.diffGeneration
	a.requestQuietRefresh()
	if !a.refreshBusy {
		t.Fatal("same-status remote quiet refresh did not start")
	}
	drainRefresh(t, updated, 1)
	if a.diffGeneration == generation {
		t.Fatalf("equal Git status left the remote diff stale at generation %d", generation)
	}
	// Finish the replacement read so no background work outlives the test.
	for a.diffLoading {
		updated.waitForUpdate(t)
	}
	local, err := os.ReadFile(filename)
	if err != nil || string(local) != "a much longer second local version\n" {
		t.Fatalf("remote refresh altered the unrelated host file: %q %v", local, err)
	}
}
