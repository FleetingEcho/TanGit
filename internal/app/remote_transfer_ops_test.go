package app

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/transfer"
)

type remoteTransferFakeInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (i remoteTransferFakeInfo) Name() string       { return i.name }
func (i remoteTransferFakeInfo) Size() int64        { return i.size }
func (i remoteTransferFakeInfo) Mode() os.FileMode  { return i.mode }
func (i remoteTransferFakeInfo) ModTime() time.Time { return time.Time{} }
func (i remoteTransferFakeInfo) IsDir() bool        { return i.mode.IsDir() }
func (i remoteTransferFakeInfo) Sys() any           { return nil }

type remoteTransferFake struct {
	mu sync.Mutex

	info map[string]os.FileInfo

	uploadFileErr       error
	uploadFolderErr     error
	downloadFileErr     error
	downloadFolderErr   error
	downloadFileContent string
	downloadFileFunc    func(context.Context, string, string, os.FileMode, sshworkspace.ProgressFunc) error

	calls              []string
	fileUploads        []string
	folderUploads      []string
	fileDownloads      []string
	folderDownloads    []string
	renames            [][2]string
	removed            []string
	removedAll         []string
	progressTheseCalls []int64
}

func newRemoteTransferFake() *remoteTransferFake {
	return &remoteTransferFake{info: make(map[string]os.FileInfo), downloadFileContent: "new contents"}
}

func (f *remoteTransferFake) record(call string) {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
}

func (f *remoteTransferFake) Lstat(remotePath string) (os.FileInfo, error) {
	f.record("lstat:" + remotePath)
	f.mu.Lock()
	defer f.mu.Unlock()
	if info, ok := f.info[remotePath]; ok {
		return info, nil
	}
	return nil, &os.PathError{Op: "lstat", Path: remotePath, Err: fs.ErrNotExist}
}

func (f *remoteTransferFake) MkdirAll(remotePath string) error {
	f.record("mkdir-all:" + remotePath)
	return nil
}

func (f *remoteTransferFake) Remove(remotePath string) error {
	f.record("remove:" + remotePath)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, remotePath)
	if _, ok := f.info[remotePath]; !ok {
		return &os.PathError{Op: "remove", Path: remotePath, Err: fs.ErrNotExist}
	}
	delete(f.info, remotePath)
	return nil
}

func (f *remoteTransferFake) RemoveAll(remotePath string) error {
	f.record("remove-all:" + remotePath)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.info, remotePath)
	f.removedAll = append(f.removedAll, remotePath)
	return nil
}

func (f *remoteTransferFake) Rename(oldRemotePath, newRemotePath string) error {
	f.record("rename:" + oldRemotePath + ":" + newRemotePath)
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.info[oldRemotePath]
	if !ok {
		return &os.PathError{Op: "rename", Path: oldRemotePath, Err: fs.ErrNotExist}
	}
	delete(f.info, oldRemotePath)
	f.info[newRemotePath] = info
	f.renames = append(f.renames, [2]string{oldRemotePath, newRemotePath})
	return nil
}

func (f *remoteTransferFake) RenameAtomically(oldRemotePath, newRemotePath string) error {
	f.record("rename-atomically:" + oldRemotePath + ":" + newRemotePath)
	return f.Rename(oldRemotePath, newRemotePath)
}

func (f *remoteTransferFake) UploadFile(_ context.Context, localPath, remotePath string, _ os.FileMode, progress sshworkspace.ProgressFunc) error {
	f.record("upload-file:" + remotePath)
	contents, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.fileUploads = append(f.fileUploads, remotePath)
	err = f.uploadFileErr
	f.mu.Unlock()
	if progress != nil && len(contents) > 0 {
		progress(sshworkspace.TransferProgress{Bytes: int64(len(contents))})
	}
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.info[remotePath] = remoteTransferFakeInfo{name: path.Base(remotePath), size: int64(len(contents)), mode: 0o644}
	f.mu.Unlock()
	return nil
}

func (f *remoteTransferFake) UploadFolder(_ context.Context, _ string, remotePath string, progress sshworkspace.ProgressFunc) error {
	f.record("upload-folder:" + remotePath)
	f.mu.Lock()
	f.folderUploads = append(f.folderUploads, remotePath)
	err := f.uploadFolderErr
	f.info[remotePath] = remoteTransferFakeInfo{name: path.Base(remotePath), mode: os.ModeDir | 0o755}
	f.mu.Unlock()
	if progress != nil {
		progress(sshworkspace.TransferProgress{Bytes: 12})
	}
	return err
}

func (f *remoteTransferFake) DownloadFile(ctx context.Context, _ string, localPath string, permission os.FileMode, progress sshworkspace.ProgressFunc) error {
	f.record("download-file:" + localPath)
	f.mu.Lock()
	f.fileDownloads = append(f.fileDownloads, localPath)
	custom := f.downloadFileFunc
	f.mu.Unlock()
	if custom != nil {
		return custom(ctx, "", localPath, permission, progress)
	}
	f.mu.Lock()
	contents := f.downloadFileContent
	err := f.downloadFileErr
	f.mu.Unlock()
	if writeErr := os.WriteFile(localPath, []byte(contents), permission); writeErr != nil {
		return writeErr
	}
	if progress != nil {
		progress(sshworkspace.TransferProgress{Bytes: int64(len(contents))})
	}
	return err
}

func (f *remoteTransferFake) DownloadFolderZIP(_ context.Context, _ string, writer *zip.Writer, progress sshworkspace.ProgressFunc) error {
	f.record("download-folder")
	f.mu.Lock()
	f.folderDownloads = append(f.folderDownloads, "downloaded")
	err := f.downloadFolderErr
	f.mu.Unlock()
	entry, createErr := writer.Create("folder/data.txt")
	if createErr != nil {
		return createErr
	}
	if _, writeErr := entry.Write([]byte("zip contents")); writeErr != nil {
		return writeErr
	}
	if progress != nil {
		progress(sshworkspace.TransferProgress{Bytes: 12})
	}
	return err
}

func (f *remoteTransferFake) fileUploadPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.fileUploads...)
}

func (f *remoteTransferFake) removedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

func (f *remoteTransferFake) removedAllPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removedAll...)
}

func (f *remoteTransferFake) folderUploadPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.folderUploads...)
}

func (f *remoteTransferFake) setUploadFileError(err error) {
	f.mu.Lock()
	f.uploadFileErr = err
	f.mu.Unlock()
}

func (f *remoteTransferFake) setDownloadFileError(err error) {
	f.mu.Lock()
	f.downloadFileErr = err
	f.mu.Unlock()
}

func remoteTransferResultWithin(t *testing.T, manager *transfer.Manager, id transfer.ID) transfer.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := manager.Result(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestCommitLocalTransferFilePreservesUnexpectedDestination(t *testing.T) {
	directory := t.TempDir()
	for _, overwrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("overwrite-%v", overwrite), func(t *testing.T) {
			temporary := filepath.Join(directory, fmt.Sprintf("temporary-%v", overwrite))
			destination := filepath.Join(directory, fmt.Sprintf("destination-%v", overwrite))
			if err := os.WriteFile(temporary, []byte("completed download"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, []byte("created concurrently"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := commitLocalTransferFile(temporary, destination, overwrite)
			contents, readErr := os.ReadFile(destination)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !overwrite {
				if err == nil || string(contents) != "created concurrently" {
					t.Fatalf("no-clobber commit error=%v contents=%q", err, contents)
				}
			} else if err != nil || string(contents) != "completed download" {
				t.Fatalf("overwrite commit error=%v contents=%q", err, contents)
			}
		})
	}
}

func TestRemoteUploadNameMappingAndSourceValidation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	localFile := filepath.Join(root, "nested", "report.txt")
	if err := os.Mkdir(filepath.Dir(localFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localFile, []byte("report"), 0o640); err != nil {
		t.Fatal(err)
	}
	localFolder := filepath.Join(root, "assets")
	if err := os.Mkdir(localFolder, 0o755); err != nil {
		t.Fatal(err)
	}

	items, kinds, err := planRemoteUploadItems([]string{localFile, localFolder}, "/srv/incoming")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := items[0].Destination, "/srv/incoming/report.txt"; got != want {
		t.Fatalf("file destination = %q, want %q", got, want)
	}
	if got, want := items[1].Destination, "/srv/incoming/assets"; got != want {
		t.Fatalf("folder destination = %q, want %q", got, want)
	}
	if items[0].Size != int64(len("report")) || items[1].Size != 0 || kinds[items[0]] != remoteUploadFile || kinds[items[1]] != remoteUploadFolder {
		t.Fatalf("unexpected upload plan: %+v %+v", items, kinds)
	}

	link := filepath.Join(root, "linked-report")
	if err := os.Symlink(localFile, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, _, err := planRemoteUploadItems([]string{link}, "/srv/incoming"); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink source error = %v", err)
	}
	if _, err := cleanRemoteTransferDirectory("relative/destination"); err == nil {
		t.Fatal("expected an absolute remote destination error")
	}
	if _, err := cleanRemoteTransferSource("/"); err == nil {
		t.Fatal("expected remote root source to be rejected")
	}
	if _, _, err := cleanNativeTransferPath(string(filepath.Separator)); err == nil {
		t.Fatal("expected local filesystem root to be rejected")
	}
}

func TestRemoteUploadCollisionApplyToAll(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	localPaths := []string{filepath.Join(root, "one.txt"), filepath.Join(root, "two.txt")}
	fake := newRemoteTransferFake()
	for _, localPath := range localPaths {
		if err := os.WriteFile(localPath, []byte("local"), 0o644); err != nil {
			t.Fatal(err)
		}
		remotePath := fmt.Sprintf("/remote/%s", filepath.Base(localPath))
		fake.info[remotePath] = remoteTransferFakeInfo{name: filepath.Base(localPath), mode: 0o644}
	}
	manager := transfer.NewManager()
	defer manager.Close()
	prompts := 0
	id, err := enqueueRemoteUpload(manager, fake, localPaths, "/remote", func(prompt transfer.CollisionPrompt) {
		prompts++
		prompt.Respond(transfer.CollisionChoice{Decision: transfer.CollisionSkip, ApplyToAll: true})
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := remoteTransferResultWithin(t, manager, id)
	if result.Status != transfer.StatusSucceeded || result.SkippedItems != 2 || prompts != 1 {
		t.Fatalf("apply-to-all collision result = %+v, prompts = %d", result, prompts)
	}
	if uploads := fake.fileUploadPaths(); len(uploads) != 0 {
		t.Fatalf("skipped uploads reached transport: %v", uploads)
	}
}

func TestRemoteUploadRetryUsesFreshTemporaryAndCleansFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	localFile := filepath.Join(root, "payload.bin")
	if err := os.WriteFile(localFile, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newRemoteTransferFake()
	fake.setUploadFileError(errors.New("temporary transport failure"))
	manager := transfer.NewManager()
	defer manager.Close()
	id, err := enqueueRemoteUpload(manager, fake, []string{localFile}, "/remote", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	failed := remoteTransferResultWithin(t, manager, id)
	if failed.Status != transfer.StatusFailed {
		t.Fatalf("first result = %+v", failed)
	}
	uploads := fake.fileUploadPaths()
	removed := fake.removedPaths()
	if len(uploads) != 1 || len(removed) != 1 || uploads[0] != removed[0] {
		t.Fatalf("failed upload cleanup: uploads=%v removals=%v", uploads, removed)
	}
	if uploads[0] == "/remote/payload.bin" || path.Dir(uploads[0]) != "/remote" {
		t.Fatalf("upload did not use a sibling temporary path: %q", uploads[0])
	}

	fake.setUploadFileError(nil)
	retryID, err := manager.Retry(id)
	if err != nil {
		t.Fatal(err)
	}
	succeeded := remoteTransferResultWithin(t, manager, retryID)
	if succeeded.Status != transfer.StatusSucceeded || succeeded.RetryOf != id {
		t.Fatalf("retry result = %+v", succeeded)
	}
	uploads = fake.fileUploadPaths()
	if len(uploads) != 2 || uploads[0] == uploads[1] {
		t.Fatalf("retry did not allocate a fresh temporary path: %v", uploads)
	}
	fake.mu.Lock()
	_, finalExists := fake.info["/remote/payload.bin"]
	_, temporaryExists := fake.info[uploads[1]]
	fake.mu.Unlock()
	if !finalExists || temporaryExists {
		t.Fatalf("retry was not committed by rename: final=%v temporary=%v", finalExists, temporaryExists)
	}
}

func TestRemoteUploadFolderFailureRemovesTemporaryTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	localFolder := filepath.Join(root, "folder")
	if err := os.Mkdir(localFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localFolder, "data"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newRemoteTransferFake()
	fake.uploadFolderErr = errors.New("folder stream failed")
	manager := transfer.NewManager()
	defer manager.Close()
	id, err := enqueueRemoteUpload(manager, fake, []string{localFolder}, "/remote", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := remoteTransferResultWithin(t, manager, id)
	if result.Status != transfer.StatusFailed {
		t.Fatalf("folder result = %+v", result)
	}
	uploads := fake.folderUploadPaths()
	removals := fake.removedAllPaths()
	if len(uploads) != 1 || len(removals) != 1 || uploads[0] != removals[0] {
		t.Fatalf("temporary folder cleanup: uploads=%v removals=%v", uploads, removals)
	}
	if uploads[0] == "/remote/folder" || path.Dir(uploads[0]) != "/remote" {
		t.Fatalf("folder upload did not use a temporary sibling: %q", uploads[0])
	}
}

func TestRemoteDownloadOverwriteFailureIsAtomicAndRetryable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	destination := filepath.Join(root, "downloaded.txt")
	if err := os.WriteFile(destination, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := newRemoteTransferFake()
	fake.info["/remote/source.txt"] = remoteTransferFakeInfo{name: "source.txt", size: int64(len("new contents")), mode: 0o640}
	fake.setDownloadFileError(errors.New("download interrupted"))
	manager := transfer.NewManager()
	defer manager.Close()
	collisions := 0
	onCollision := func(prompt transfer.CollisionPrompt) {
		collisions++
		prompt.Respond(transfer.CollisionChoice{Decision: transfer.CollisionOverwrite, ApplyToAll: true})
	}
	id, err := enqueueRemoteDownload(manager, fake, []string{"/remote/source.txt"}, []string{destination}, onCollision, nil)
	if err != nil {
		t.Fatal(err)
	}
	result := remoteTransferResultWithin(t, manager, id)
	if result.Status != transfer.StatusFailed {
		t.Fatalf("failed download result = %+v", result)
	}
	assertRemoteTransferDirectoryContents(t, root, map[string]string{"downloaded.txt": "original"})

	fake.setDownloadFileError(nil)
	retryID, err := manager.Retry(id)
	if err != nil {
		t.Fatal(err)
	}
	result = remoteTransferResultWithin(t, manager, retryID)
	if result.Status != transfer.StatusSucceeded || result.RetryOf != id {
		t.Fatalf("download retry result = %+v", result)
	}
	assertRemoteTransferDirectoryContents(t, root, map[string]string{"downloaded.txt": "new contents"})
	if collisions != 2 {
		t.Fatalf("collision callbacks across retry = %d, want 2", collisions)
	}
}

func TestRemoteDownloadFolderZIPAndCollisionSkip(t *testing.T) {
	t.Parallel()
	t.Run("ZIP commits after close", func(t *testing.T) {
		root := t.TempDir()
		destination := filepath.Join(root, "archive.zip")
		fake := newRemoteTransferFake()
		fake.info["/remote/folder"] = remoteTransferFakeInfo{name: "folder", mode: os.ModeDir | 0o755}
		manager := transfer.NewManager()
		defer manager.Close()
		id, err := enqueueRemoteDownload(manager, fake, []string{"/remote/folder"}, []string{destination}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result := remoteTransferResultWithin(t, manager, id); result.Status != transfer.StatusSucceeded {
			t.Fatalf("ZIP result = %+v", result)
		}
		archive, err := zip.OpenReader(destination)
		if err != nil {
			t.Fatal(err)
		}
		defer archive.Close()
		if len(archive.File) != 1 || archive.File[0].Name != "folder/data.txt" {
			t.Fatalf("ZIP entries = %+v", archive.File)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "archive.zip" {
			t.Fatalf("temporary ZIP output remains: %+v", entries)
		}
	})

	t.Run("skip leaves destination untouched", func(t *testing.T) {
		root := t.TempDir()
		destination := filepath.Join(root, "archive.zip")
		if err := os.WriteFile(destination, []byte("original"), 0o644); err != nil {
			t.Fatal(err)
		}
		fake := newRemoteTransferFake()
		fake.info["/remote/folder"] = remoteTransferFakeInfo{name: "folder", mode: os.ModeDir | 0o755}
		manager := transfer.NewManager()
		defer manager.Close()
		id, err := enqueueRemoteDownload(manager, fake, []string{"/remote/folder"}, []string{destination}, func(prompt transfer.CollisionPrompt) {
			prompt.Respond(transfer.CollisionChoice{Decision: transfer.CollisionSkip})
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		result := remoteTransferResultWithin(t, manager, id)
		if result.Status != transfer.StatusSucceeded || result.SkippedItems != 1 {
			t.Fatalf("skip result = %+v", result)
		}
		fake.mu.Lock()
		downloads := len(fake.folderDownloads)
		fake.mu.Unlock()
		if downloads != 0 {
			t.Fatal("skipped remote folder reached transport")
		}
		assertRemoteTransferDirectoryContents(t, root, map[string]string{"archive.zip": "original"})
	})
}

func TestRemoteDownloadCancellationCleansTemporaryFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	destination := filepath.Join(root, "cancelled.txt")
	fake := newRemoteTransferFake()
	fake.info["/remote/source.txt"] = remoteTransferFakeInfo{name: "source.txt", size: 10, mode: 0o644}
	started := make(chan struct{})
	fake.downloadFileFunc = func(ctx context.Context, _ string, localPath string, _ os.FileMode, _ sshworkspace.ProgressFunc) error {
		if err := os.WriteFile(localPath, []byte("partial"), 0o600); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	manager := transfer.NewManager()
	defer manager.Close()
	id, err := enqueueRemoteDownload(manager, fake, []string{"/remote/source.txt"}, []string{destination}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	if !manager.Cancel(id) {
		t.Fatal("cancel was not accepted")
	}
	result := remoteTransferResultWithin(t, manager, id)
	if result.Status != transfer.StatusCancelled {
		t.Fatalf("cancel result = %+v", result)
	}
	assertRemoteTransferDirectoryContents(t, root, map[string]string{})
}

func assertRemoteTransferDirectoryContents(t *testing.T, directory string, expected map[string]string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(expected) {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("directory entries = %v, want %v", names, expected)
	}
	for name, contents := range expected {
		actual, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(actual) != contents {
			t.Fatalf("%s contents = %q, want %q", name, actual, contents)
		}
	}
}
