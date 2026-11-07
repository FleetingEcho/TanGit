package app

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/transfer"
)

// EnqueueRemoteUpload enqueues one transfer item for each native local file or
// directory. Every source is placed directly in remoteDestinationDir under its
// local base name. remoteDestinationDir must be an absolute POSIX path.
//
// Source types are checked with Lstat both when planning and when executing an
// item. Existing remote destinations are resolved through onCollision. Regular
// files are uploaded to a unique sibling temporary file before being renamed.
// Directories are recursively uploaded to a temporary sibling and then moved
// into place. Overwriting a directory replaces the whole directory; the former
// directory is restored if the replacement cannot be committed.
//
// onCollision and onComplete may be nil. onCollision is required only if an
// item actually encounters a collision. Retry reuses the immutable batch and
// its callbacks, but repeats all source and collision checks.
func EnqueueRemoteUpload(
	manager *transfer.Manager,
	client *sshworkspace.Client,
	localSources []string,
	remoteDestinationDir string,
	onCollision func(transfer.CollisionPrompt),
	onComplete func(transfer.Result),
) (transfer.ID, error) {
	if client == nil {
		return 0, errors.New("remote upload: SSH client is nil")
	}
	return enqueueRemoteUpload(manager, client, localSources, remoteDestinationDir, onCollision, onComplete)
}

// EnqueueRemoteDownload enqueues one transfer item for each absolute remote
// source and its corresponding native local destination. Remote directories
// are downloaded as ZIP files at the supplied local destination, not extracted.
//
// Existing local destinations are resolved through onCollision. Every local
// output is written to a unique sibling temporary file and published only after
// the download and all close operations succeed. Failed and cancelled items
// remove their temporary output without modifying an existing destination.
// onCollision and onComplete may be nil, with the same semantics as
// EnqueueRemoteUpload.
func EnqueueRemoteDownload(
	manager *transfer.Manager,
	client *sshworkspace.Client,
	remoteSources []string,
	localDestinations []string,
	onCollision func(transfer.CollisionPrompt),
	onComplete func(transfer.Result),
) (transfer.ID, error) {
	if client == nil {
		return 0, errors.New("remote download: SSH client is nil")
	}
	return enqueueRemoteDownload(manager, client, remoteSources, localDestinations, onCollision, onComplete)
}

type remoteTransferClient interface {
	Lstat(string) (os.FileInfo, error)
	MkdirAll(string) error
	Remove(string) error
	RemoveAll(string) error
	Rename(string, string) error
	RenameAtomically(string, string) error
	UploadFile(context.Context, string, string, os.FileMode, sshworkspace.ProgressFunc) error
	UploadFolder(context.Context, string, string, sshworkspace.ProgressFunc) error
	DownloadFile(context.Context, string, string, os.FileMode, sshworkspace.ProgressFunc) error
	DownloadFolderZIP(context.Context, string, *zip.Writer, sshworkspace.ProgressFunc) error
}

type remoteUploadKind uint8

const (
	remoteUploadFile remoteUploadKind = iota + 1
	remoteUploadFolder
)

func enqueueRemoteUpload(
	manager *transfer.Manager,
	client remoteTransferClient,
	localSources []string,
	remoteDestinationDir string,
	onCollision func(transfer.CollisionPrompt),
	onComplete func(transfer.Result),
) (transfer.ID, error) {
	if manager == nil {
		return 0, errors.New("remote upload: transfer manager is nil")
	}
	if client == nil {
		return 0, errors.New("remote upload: SSH client is nil")
	}
	remoteDir, err := cleanRemoteTransferDirectory(remoteDestinationDir)
	if err != nil {
		return 0, fmt.Errorf("remote upload destination: %w", err)
	}
	items, kinds, err := planRemoteUploadItems(localSources, remoteDir)
	if err != nil {
		return 0, err
	}

	operation := func(ctx context.Context, request transfer.OperationRequest) error {
		kind, ok := kinds[request.Item]
		if !ok {
			return errors.New("remote upload: unknown item specification")
		}
		return performRemoteUpload(ctx, client, request, kind)
	}
	return manager.Enqueue(transfer.Batch{
		Items:       items,
		RemotePath:  remoteDir,
		Operation:   operation,
		OnCollision: onCollision,
		OnComplete:  onComplete,
	})
}

func planRemoteUploadItems(localSources []string, remoteDir string) ([]transfer.Item, map[transfer.Item]remoteUploadKind, error) {
	if len(localSources) == 0 {
		return nil, nil, fmt.Errorf("%w: remote upload has no sources", transfer.ErrInvalidBatch)
	}
	items := make([]transfer.Item, 0, len(localSources))
	kinds := make(map[transfer.Item]remoteUploadKind, len(localSources))
	for index, source := range localSources {
		cleaned, base, err := cleanNativeTransferPath(source)
		if err != nil {
			return nil, nil, fmt.Errorf("remote upload source %d: %w", index, err)
		}
		info, err := os.Lstat(cleaned)
		if err != nil {
			return nil, nil, fmt.Errorf("remote upload source %q: %w", cleaned, err)
		}
		var kind remoteUploadKind
		var size int64
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return nil, nil, fmt.Errorf("remote upload source %q is a symbolic link", cleaned)
		case info.Mode().IsRegular():
			kind = remoteUploadFile
			size = info.Size()
		case info.IsDir():
			kind = remoteUploadFolder
		default:
			return nil, nil, fmt.Errorf("remote upload source %q is not a regular file or directory", cleaned)
		}
		item := transfer.Item{Source: cleaned, Destination: path.Join(remoteDir, base), Size: size}
		items = append(items, item)
		kinds[item] = kind
	}
	return items, kinds, nil
}

func enqueueRemoteDownload(
	manager *transfer.Manager,
	client remoteTransferClient,
	remoteSources []string,
	localDestinations []string,
	onCollision func(transfer.CollisionPrompt),
	onComplete func(transfer.Result),
) (transfer.ID, error) {
	if manager == nil {
		return 0, errors.New("remote download: transfer manager is nil")
	}
	if client == nil {
		return 0, errors.New("remote download: SSH client is nil")
	}
	if len(remoteSources) == 0 || len(remoteSources) != len(localDestinations) {
		return 0, fmt.Errorf("%w: remote download sources and destinations must be nonempty and have equal lengths", transfer.ErrInvalidBatch)
	}
	items := make([]transfer.Item, 0, len(remoteSources))
	for index, source := range remoteSources {
		cleanedSource, err := cleanRemoteTransferSource(source)
		if err != nil {
			return 0, fmt.Errorf("remote download source %d: %w", index, err)
		}
		cleanedDestination, _, err := cleanNativeTransferPath(localDestinations[index])
		if err != nil {
			return 0, fmt.Errorf("remote download destination %d: %w", index, err)
		}
		items = append(items, transfer.Item{Source: cleanedSource, Destination: cleanedDestination})
	}
	operation := func(ctx context.Context, request transfer.OperationRequest) error {
		return performRemoteDownload(ctx, client, request)
	}
	return manager.Enqueue(transfer.Batch{
		Items:       items,
		Operation:   operation,
		OnCollision: onCollision,
		OnComplete:  onComplete,
	})
}

func cleanRemoteTransferDirectory(remoteDir string) (string, error) {
	if remoteDir == "" || !path.IsAbs(remoteDir) {
		return "", errors.New("remote path must be a nonempty absolute POSIX path")
	}
	if err := rejectTransferNUL(remoteDir); err != nil {
		return "", err
	}
	return path.Clean(remoteDir), nil
}

func cleanRemoteTransferSource(remoteSource string) (string, error) {
	cleaned, err := cleanRemoteTransferDirectory(remoteSource)
	if err != nil {
		return "", err
	}
	if !validRemoteTransferBase(path.Base(cleaned)) {
		return "", fmt.Errorf("remote source %q has an unsafe base name", remoteSource)
	}
	return cleaned, nil
}

func cleanNativeTransferPath(nativePath string) (cleaned, base string, err error) {
	if nativePath == "" {
		return "", "", errors.New("local path is empty")
	}
	if err := rejectTransferNUL(nativePath); err != nil {
		return "", "", err
	}
	cleaned = filepath.Clean(nativePath)
	base = filepath.Base(cleaned)
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "", "", fmt.Errorf("local path %q has an unsafe base name", nativePath)
	}
	return cleaned, base, nil
}

func validRemoteTransferBase(base string) bool {
	return base != "" && base != "." && base != ".." && base != "/"
}

func rejectTransferNUL(value string) error {
	for _, character := range value {
		if character == 0 {
			return errors.New("path contains NUL")
		}
	}
	return nil
}

func performRemoteUpload(ctx context.Context, client remoteTransferClient, request transfer.OperationRequest, expected remoteUploadKind) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(request.Item.Source)
	if err != nil {
		return fmt.Errorf("lstat local upload source: %w", err)
	}
	actual := remoteUploadKind(0)
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("remote upload source %q is a symbolic link", request.Item.Source)
	case info.Mode().IsRegular():
		actual = remoteUploadFile
	case info.IsDir():
		actual = remoteUploadFolder
	default:
		return fmt.Errorf("remote upload source %q is not a regular file or directory", request.Item.Source)
	}
	if actual != expected {
		return fmt.Errorf("remote upload source %q changed type after enqueue", request.Item.Source)
	}

	destinationInfo, destinationExists, err := statRemoteTransferDestination(client, request.Item.Destination)
	if err != nil {
		return fmt.Errorf("stat remote upload destination: %w", err)
	}
	overwrite, err := resolveRemoteTransferCollision(ctx, request, destinationExists)
	if err != nil {
		return err
	}
	if destinationExists {
		compatible := actual == remoteUploadFile && destinationInfo.Mode().IsRegular() || actual == remoteUploadFolder && destinationInfo.IsDir()
		if !compatible {
			return fmt.Errorf("remote upload destination %q has a different file type", request.Item.Destination)
		}
	}
	if err := client.MkdirAll(path.Dir(request.Item.Destination)); err != nil {
		return fmt.Errorf("create remote upload parent: %w", err)
	}
	if actual == remoteUploadFolder {
		return uploadRemoteFolder(ctx, client, request, info, destinationExists && overwrite)
	}
	return uploadRemoteFile(ctx, client, request, info, destinationExists && overwrite)
}

func resolveRemoteTransferCollision(ctx context.Context, request transfer.OperationRequest, exists bool) (overwrite bool, err error) {
	if !exists {
		return false, nil
	}
	choice, err := request.ResolveCollision(ctx)
	if err != nil {
		return false, err
	}
	switch choice.Decision {
	case transfer.CollisionSkip:
		return false, transfer.ErrSkip
	case transfer.CollisionOverwrite:
		return true, nil
	case transfer.CollisionCancel:
		return false, transfer.ErrCollisionCancelled
	default:
		return false, fmt.Errorf("remote transfer: invalid collision decision %d", choice.Decision)
	}
}

func statRemoteTransferDestination(client remoteTransferClient, remotePath string) (os.FileInfo, bool, error) {
	info, err := client.Lstat(remotePath)
	if err == nil {
		return info, true, nil
	}
	if os.IsNotExist(err) || errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

func uploadRemoteFile(ctx context.Context, client remoteTransferClient, request transfer.OperationRequest, info os.FileInfo, overwrite bool) (err error) {
	temporary, err := reserveRemoteTransferTemporary(client, request.Item.Destination)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			err = errors.Join(err, removeRemoteTransferTemporary(client.Remove, temporary))
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	progress := func(update sshworkspace.TransferProgress) {
		request.ReportProgress(update.Bytes)
	}
	if err := client.UploadFile(ctx, request.Item.Source, temporary, info.Mode().Perm(), progress); err != nil {
		return fmt.Errorf("upload remote temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	commit := client.Rename
	if overwrite {
		commit = client.RenameAtomically
	}
	if err := commit(temporary, request.Item.Destination); err != nil {
		return fmt.Errorf("commit remote uploaded file into place: %w", err)
	}
	committed = true
	return nil
}

func uploadRemoteFolder(ctx context.Context, client remoteTransferClient, request transfer.OperationRequest, _ os.FileInfo, overwrite bool) (err error) {
	temporary, err := reserveRemoteTransferTemporary(client, request.Item.Destination)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			err = errors.Join(err, removeRemoteTransferTemporary(client.RemoveAll, temporary))
		}
	}()
	progress := func(update sshworkspace.TransferProgress) {
		request.ReportProgress(update.Bytes)
	}
	if err := client.UploadFolder(ctx, request.Item.Source, temporary, progress); err != nil {
		return fmt.Errorf("upload remote temporary folder: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !overwrite {
		if err := client.Rename(temporary, request.Item.Destination); err != nil {
			return fmt.Errorf("rename remote uploaded folder into place: %w", err)
		}
		committed = true
		return nil
	}

	backup, err := reserveRemoteTransferTemporary(client, request.Item.Destination)
	if err != nil {
		return err
	}
	if err := client.Rename(request.Item.Destination, backup); err != nil {
		return fmt.Errorf("move existing remote folder aside: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return restoreRemoteUploadFolder(client, request.Item.Destination, backup, ctxErr)
	}
	if err := client.Rename(temporary, request.Item.Destination); err != nil {
		return restoreRemoteUploadFolder(client, request.Item.Destination, backup, fmt.Errorf("replace remote folder: %w", err))
	}
	committed = true
	if err := client.RemoveAll(backup); err != nil {
		return fmt.Errorf("remove previous remote folder backup %q: %w", backup, err)
	}
	return nil
}

func restoreRemoteUploadFolder(client remoteTransferClient, destination, backup string, cause error) error {
	if err := client.Rename(backup, destination); err != nil {
		return errors.Join(cause, fmt.Errorf("restore remote folder (original remains at %q): %w", backup, err))
	}
	return cause
}

func reserveRemoteTransferTemporary(client remoteTransferClient, destination string) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		suffix, err := newRemoteTransferSuffix()
		if err != nil {
			return "", err
		}
		temporary := path.Join(path.Dir(destination), ".tangit-transfer-"+suffix+".tmp")
		_, exists, err := statRemoteTransferDestination(client, temporary)
		if err != nil {
			return "", fmt.Errorf("check remote temporary path: %w", err)
		}
		if !exists {
			return temporary, nil
		}
	}
	return "", errors.New("could not allocate a unique remote temporary path")
}

func newRemoteTransferSuffix() (string, error) {
	var value [18]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate transfer temporary name: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func removeRemoteTransferTemporary(remove func(string) error, temporary string) error {
	if err := remove(temporary); err != nil && !os.IsNotExist(err) && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove transfer temporary path %q: %w", temporary, err)
	}
	return nil
}

func performRemoteDownload(ctx context.Context, client remoteTransferClient, request transfer.OperationRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sourceInfo, err := client.Lstat(request.Item.Source)
	if err != nil {
		return fmt.Errorf("stat remote download source: %w", err)
	}
	isFolder := sourceInfo.IsDir()
	if !sourceInfo.Mode().IsRegular() && !isFolder {
		return fmt.Errorf("remote download source %q is not a regular file or directory", request.Item.Source)
	}
	destinationInfo, destinationExists, err := lstatLocalTransferDestination(request.Item.Destination)
	if err != nil {
		return err
	}
	overwrite, err := resolveRemoteTransferCollision(ctx, request, destinationExists)
	if err != nil {
		return err
	}
	if destinationExists && destinationInfo.IsDir() {
		return fmt.Errorf("local download destination %q is a directory", request.Item.Destination)
	}
	replace := destinationExists && overwrite
	if isFolder {
		return downloadRemoteFolderZIP(ctx, client, request, replace)
	}
	return downloadRemoteFile(ctx, client, request, sourceInfo, replace)
}

func lstatLocalTransferDestination(destination string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(destination)
	if err == nil {
		return info, true, nil
	}
	if os.IsNotExist(err) || errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("lstat local download destination: %w", err)
}

func downloadRemoteFile(ctx context.Context, client remoteTransferClient, request transfer.OperationRequest, sourceInfo os.FileInfo, overwrite bool) (err error) {
	temporary, output, err := createLocalTransferTemporary(request.Item.Destination)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			err = errors.Join(err, removeLocalTransferTemporary(temporary))
		}
	}()
	// DownloadFile owns its output handle, so close the exclusive reservation
	// before handing it the unique path.
	if err := output.Close(); err != nil {
		return fmt.Errorf("close local temporary reservation: %w", err)
	}
	progress := func(update sshworkspace.TransferProgress) {
		request.ReportProgress(update.Bytes)
	}
	if err := client.DownloadFile(ctx, request.Item.Source, temporary, sourceInfo.Mode().Perm(), progress); err != nil {
		return fmt.Errorf("download to local temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := commitLocalTransferFile(temporary, request.Item.Destination, overwrite); err != nil {
		return fmt.Errorf("commit local download into place: %w", err)
	}
	committed = true
	return nil
}

func downloadRemoteFolderZIP(ctx context.Context, client remoteTransferClient, request transfer.OperationRequest, overwrite bool) (err error) {
	temporary, output, err := createLocalTransferTemporary(request.Item.Destination)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			err = errors.Join(err, removeLocalTransferTemporary(temporary))
		}
	}()
	archive := zip.NewWriter(output)
	progress := func(update sshworkspace.TransferProgress) {
		request.ReportProgress(update.Bytes)
	}
	downloadErr := client.DownloadFolderZIP(ctx, request.Item.Source, archive, progress)
	archiveCloseErr := archive.Close()
	outputCloseErr := output.Close()
	if ctxErr := ctx.Err(); ctxErr != nil {
		downloadErr = errors.Join(downloadErr, ctxErr)
	}
	if err := errors.Join(downloadErr, archiveCloseErr, outputCloseErr); err != nil {
		return fmt.Errorf("complete local temporary ZIP: %w", err)
	}
	if err := commitLocalTransferFile(temporary, request.Item.Destination, overwrite); err != nil {
		return fmt.Errorf("commit local ZIP download into place: %w", err)
	}
	committed = true
	return nil
}

func commitLocalTransferFile(temporary, destination string, overwrite bool) error {
	if overwrite {
		return os.Rename(temporary, destination)
	}
	// A hard link publishes the completed sibling at a previously absent path
	// without replacing a destination created while the transfer was running.
	// Both names are on the same filesystem because the sibling is its directory.
	if err := os.Link(temporary, destination); err != nil {
		return err
	}
	return os.Remove(temporary)
}

func createLocalTransferTemporary(destination string) (string, *os.File, error) {
	for attempt := 0; attempt < 8; attempt++ {
		suffix, err := newRemoteTransferSuffix()
		if err != nil {
			return "", nil, err
		}
		temporary := filepath.Join(filepath.Dir(destination), ".tangit-transfer-"+suffix+".tmp")
		output, err := os.OpenFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return temporary, output, nil
		}
		if os.IsExist(err) {
			continue
		}
		return "", nil, fmt.Errorf("create local transfer temporary file: %w", err)
	}
	return "", nil, errors.New("could not allocate a unique local temporary path")
}

func removeLocalTransferTemporary(temporary string) error {
	if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove local transfer temporary file %q: %w", temporary, err)
	}
	return nil
}
