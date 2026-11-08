package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	mygotransfer "github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/transfer"
	"lazygitapp/internal/workspace"
)

type remoteTransferUITestSession struct {
	fakeSession
	client *sshworkspace.Client
}

func (s remoteTransferUITestSession) SSHClient() *sshworkspace.Client { return s.client }

func newRemoteTransferUITestApp(t *testing.T) (*app, *updates) {
	t.Helper()
	a := newApp(fakeOpener{})
	updated := newUpdates()
	a.update = updated.update
	a.state = stateReady
	a.generation = 7
	a.location = workspace.Location{Kind: workspace.SSH, ProfileID: "server", Root: "/srv/repo"}
	a.snapshot.Root = "/srv/repo"
	a.session = remoteTransferUITestSession{client: &sshworkspace.Client{}}
	t.Cleanup(func() {
		a.shutdownRemoteTransfers()
		waitForRemoteTransferClose()
	})
	return a, updated
}

func TestRemoteTransferManagerIsLazyAndClosedOnceWithStaleCompletionIgnored(t *testing.T) {
	a, updated := newRemoteTransferUITestApp(t)
	if a.remoteTransfers.manager != nil {
		t.Fatal("a remote manager was created before the first transfer")
	}
	originalClose := closeRemoteTransferManager
	closed := make(chan *transfer.Manager, 2)
	closeRemoteTransferManager = func(manager *transfer.Manager) {
		closed <- manager
		originalClose(manager)
	}
	t.Cleanup(func() { closeRemoteTransferManager = originalClose })

	manager, generation, _, err := a.remoteTransferManager()
	if err != nil {
		t.Fatal(err)
	}
	updated.waitForUpdate(t) // Run the manager's initial subscription snapshot.
	again, againGeneration, _, err := a.remoteTransferManager()
	if err != nil {
		t.Fatal(err)
	}
	if again != manager || againGeneration != generation {
		t.Fatal("one workspace generation did not reuse its manager")
	}
	a.shutdownRemoteTransfers()
	a.shutdownRemoteTransfers()
	select {
	case got := <-closed:
		if got != manager {
			t.Fatal("the wrong workspace manager was closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("manager close did not run")
	}
	select {
	case <-closed:
		t.Fatal("the same manager was closed twice")
	default:
	}

	before := len(a.pendingToasts)
	a.completeRemoteTransfer(manager, generation, transfer.Result{ID: 1, Status: transfer.StatusFailed}, "upload", nil, "", nil)
	updated.waitForUpdate(t)
	if len(a.pendingToasts) != before {
		t.Fatal("a completion from the closed session published a notification")
	}
}

func TestRemoteTransferNotificationsKeepIdentityAndTerminalActions(t *testing.T) {
	a, updated := newRemoteTransferUITestApp(t)
	manager, generation, _, err := a.remoteTransferManager()
	if err != nil {
		t.Fatal(err)
	}
	updated.waitForUpdate(t)
	job := a.ensureRemoteTransferJob(12, "download", nil, "", []string{"/tmp/report.zip"})
	a.publishRemoteTransfer(manager, generation, job, transfer.Result{
		ID: 12, Status: transfer.StatusRunning, TotalItems: 3, TotalBytes: 200, CompletedBytes: 50,
	})
	if len(a.pendingToasts) != 1 {
		t.Fatalf("active notifications = %d, want 1", len(a.pendingToasts))
	}
	active := a.pendingToasts[0]
	if active.id != a.remoteTransfers.toastPrefix+":12" || a.remoteTransfers.toastPrefix == "" {
		t.Fatalf("notification identity = %q, want one stable ID for this workspace batch", active.id)
	}
	if !strings.HasSuffix(active.id, ":12") || active.action != "Cancel" || active.timeout != -1 {
		t.Fatalf("active toast = %+v", active)
	}
	if !strings.Contains(active.description, "25%") {
		t.Fatalf("progress description = %q, want byte progress", active.description)
	}

	a.pendingToasts = nil
	a.publishRemoteTransfer(manager, generation, job, transfer.Result{
		ID: 12, Status: transfer.StatusPartial, TotalItems: 3, CompletedItems: 1,
		SkippedItems: 1, FailedItems: 1, TotalBytes: 200, CompletedBytes: 50,
		Errors: []transfer.ItemError{{ItemIndex: 2, Err: context.DeadlineExceeded}},
	})
	terminal := a.pendingToasts[0]
	if terminal.id != active.id {
		t.Fatalf("terminal ID %q replaced active ID %q", terminal.id, active.id)
	}
	if terminal.action != "Retry" || terminal.kind != "error" {
		t.Fatalf("partial toast = %+v", terminal)
	}
	if !strings.Contains(terminal.description, "1 completed") ||
		!strings.Contains(terminal.description, "1 skipped") ||
		!strings.Contains(terminal.description, "1 failed") ||
		!strings.Contains(terminal.description, "50 B of 200 B") {
		t.Fatalf("partial description = %q", terminal.description)
	}
	var actions []string
	for _, action := range terminal.moreActions {
		actions = append(actions, action.label)
	}
	if !reflect.DeepEqual(actions, []string{"Details", "Reveal"}) {
		t.Fatalf("terminal actions = %v", actions)
	}

	// A duplicate terminal snapshot cannot stack a second final toast.
	a.pendingToasts = nil
	a.publishRemoteTransfer(manager, generation, job, job.result)
	if len(a.pendingToasts) != 0 {
		t.Fatal("terminal result was notified more than once")
	}
}

func TestRemoteTransferCollisionIsAsynchronousNativeBatchChoice(t *testing.T) {
	a, updated := newRemoteTransferUITestApp(t)
	manager, _, _, err := a.remoteTransferManager()
	if err != nil {
		t.Fatal(err)
	}
	originalMessage := showRemoteTransferMessage
	asked := make(chan mygo.MessageOptions, 1)
	showRemoteTransferMessage = func(options mygo.MessageOptions) (mygo.MessageResult, error) {
		asked <- options
		return mygo.MessageResult{Button: 1, CheckboxChecked: true}, nil
	}
	t.Cleanup(func() { showRemoteTransferMessage = originalMessage })

	choice := make(chan transfer.CollisionChoice, 1)
	id, err := manager.Enqueue(transfer.Batch{
		Items: []transfer.Item{{Source: "/remote/a", Destination: "/local/a"}},
		Operation: func(ctx context.Context, request transfer.OperationRequest) error {
			resolved, err := request.ResolveCollision(ctx)
			if err != nil {
				return err
			}
			choice <- resolved
			return nil
		},
		OnCollision: func(prompt transfer.CollisionPrompt) {
			a.presentRemoteTransferCollision(manager, a.generation, prompt)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var options mygo.MessageOptions
	for {
		select {
		case options = <-asked:
			goto collisionShown
		default:
			updated.waitForUpdate(t)
		}
	}

collisionShown:
	if !reflect.DeepEqual(options.Buttons, []string{"Skip", "Overwrite", "Cancel"}) || options.CheckboxLabel != "Apply to all" {
		t.Fatalf("collision options = %+v", options)
	}
	select {
	case got := <-choice:
		if got.Decision != transfer.CollisionOverwrite || !got.ApplyToAll {
			t.Fatalf("collision choice = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker remained blocked after the asynchronous answer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := manager.Result(ctx, id)
	if err != nil || result.Status != transfer.StatusSucceeded {
		t.Fatalf("collision result = %+v, %v", result, err)
	}
	updated.waitForUpdate(t) // Retire the answered collision on the UI goroutine.
}

func TestRemoteNativeDropPassesFilesAndFoldersToRemoteFolder(t *testing.T) {
	a, updated := newRemoteTransferUITestApp(t)
	originalEnqueue := enqueueRemoteUploadForUI
	type uploadCall struct {
		sources     []string
		destination string
	}
	called := make(chan uploadCall, 1)
	enqueueRemoteUploadForUI = func(_ *transfer.Manager, _ *sshworkspace.Client, sources []string, destination string, _ func(transfer.CollisionPrompt), _ func(transfer.Result)) (transfer.ID, error) {
		called <- uploadCall{append([]string(nil), sources...), destination}
		return 9, nil
	}
	t.Cleanup(func() { enqueueRemoteUploadForUI = originalEnqueue })

	root := t.TempDir()
	sources := []string{filepath.Join(root, "note.txt"), filepath.Join(root, "folder")}
	data, err := mygotransfer.FileData(sources...)
	if err != nil {
		t.Fatal(err)
	}
	a.receiveWorkspaceTransferDrop(data, "src")
	select {
	case got := <-called:
		if !reflect.DeepEqual(got.sources, sources) || got.destination != "/srv/repo/src" {
			t.Fatalf("upload drop = %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native file/folder drop was not enqueued")
	}
	for a.remoteTransfers.jobs[9] == nil {
		updated.waitForUpdate(t)
	}

	local := newApp(fakeOpener{})
	local.state = stateReady
	local.location = workspace.Location{Kind: workspace.Local, Root: root}
	local.receiveRemoteTransferDrop(data, "src")
	select {
	case got := <-called:
		t.Fatalf("local workspace accepted a remote upload: %+v", got)
	default:
	}
}

func TestRemoteFolderDownloadChoosesLocalZIPName(t *testing.T) {
	a, updated := newRemoteTransferUITestApp(t)
	originalChoose := chooseRemoteTransferSavePath
	asked := make(chan mygo.SaveDialogOptions, 1)
	chooseRemoteTransferSavePath = func(options mygo.SaveDialogOptions) (string, error) {
		asked <- options
		return "", nil
	}
	t.Cleanup(func() { chooseRemoteTransferSavePath = originalChoose })

	a.beginRemoteDownload("docs", true)
	select {
	case options := <-asked:
		if options.DefaultPath != "docs.zip" || strings.HasPrefix(options.DefaultPath, "/srv/") {
			t.Fatalf("folder download destination = %q", options.DefaultPath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("folder download did not choose a local destination")
	}
	updated.waitForUpdate(t) // cancelled native save performs no enqueue
}

func TestLaunchPathPreservesSSHWorkspaceURI(t *testing.T) {
	const remote = "ssh://Build_7/srv/git/my%20project"
	if got := launchPath([]string{"-psn_0_12345", remote}); got != remote {
		t.Fatalf("SSH launch path = %q, want unchanged URI %q", got, remote)
	}
}

func TestShellCloseWaitsForRemoteSaveBeforeTransferAndSessionTeardown(t *testing.T) {
	s := newShell(pathOpener{})
	updated := newUpdates()
	s.update = updated.update
	s.invalidate = func() {}
	s.start("")
	page := readyShellTab(s, "/srv/repo")
	a := page.app
	a.location = workspace.Location{Kind: workspace.SSH, ProfileID: "test", Root: "/srv/repo"}
	a.snapshot.Root = "/srv/repo"
	session := &shutdownTrackingSession{}
	a.session = session
	client := newRemoteTestClient()
	filename := "/srv/repo/note.txt"
	client.infos[filename] = remoteTestRegular(filename, "original", 1)
	client.contents[filename] = []byte("original")
	state := &editor.State{Buffer: editor.NewBuffer("original")}
	state.Insert("unsaved")
	a.fileEditing = true
	a.fileEditKey = "note.txt"
	a.fileEditState = state
	a.code.remoteFileEdit.client = client
	a.code.remoteFileEdit.stamp = stampRemoteFile(client.infos[filename])
	a.code.remoteFileEdit.stampKnown = true
	manager := transfer.NewManager()
	a.remoteTransfers.manager = manager
	t.Cleanup(func() {
		a.shutdownRemoteTransfers()
		waitForRemoteTransferClose()
	})

	client.writeFile = func(string, []byte, os.FileMode) error {
		return errors.New("remote disk full")
	}
	s.closeTab(page)
	updated.waitForUpdate(t)
	if s.indexOf(page) < 0 || a.session != session || a.state != stateReady || !state.Buffer.Modified() {
		t.Fatal("failed remote save removed the tab, reset the editor, or closed the session")
	}
	if closes := session.closes.Load(); closes != 0 || manager.Snapshot().Closed {
		t.Fatalf("failed save started final teardown: session closes=%d manager closed=%v", closes, manager.Snapshot().Closed)
	}

	client.writeFile = nil
	state.Insert(" after correcting the remote failure")
	s.closeTab(page)
	updated.waitForUpdate(t)
	waitForRemoteTransferClose()
	if s.indexOf(page) >= 0 || len(s.tabs) != 1 || s.activeTab().repository() {
		t.Fatalf("successful save did not leave the repository picker: tabs=%d", len(s.tabs))
	}
	if closes := session.closes.Load(); closes != 1 || !manager.Snapshot().Closed {
		t.Fatalf("successful save did not finish teardown exactly once: session closes=%d manager closed=%v", closes, manager.Snapshot().Closed)
	}
}
