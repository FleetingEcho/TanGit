package app

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	mygotransfer "github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/transfer"
)

// remoteTransferUIState belongs to one app/tab. A manager never follows a
// session into another tab or a replacement repository.
type remoteTransferUIState struct {
	manager     *transfer.Manager
	generation  uint64
	unsubscribe func()
	jobs        map[transfer.ID]*remoteTransferJob
	prompts     map[transfer.ID]transfer.CollisionPrompt
	toastPrefix string
}

type remoteTransferJob struct {
	kind             string
	localSources     []string
	remoteDirectory  string
	localOutputs     []string
	result           transfer.Result
	terminalNotified bool
	cancelling       bool
}

var (
	enqueueRemoteUploadForUI     = EnqueueRemoteUpload
	enqueueRemoteDownloadForUI   = EnqueueRemoteDownload
	chooseRemoteTransferSavePath = func(options mygo.SaveDialogOptions) (string, error) {
		return mygo.Dialog.Save(options)
	}
	showRemoteTransferMessage = func(options mygo.MessageOptions) (mygo.MessageResult, error) {
		return mygo.Dialog.Message(options)
	}
	revealRemoteTransferOutput = func(filename string) {
		mygo.Shell.ShowItemInFolder(filename)
	}
	closeRemoteTransferManager = func(manager *transfer.Manager) {
		_ = manager.Close()
	}
	remoteTransferCloses sync.WaitGroup
)

// shutdownRemoteTransfers invalidates every callback before asking the manager
// to close. Close waits for SFTP, cleanup and completion callbacks on its own
// goroutine, never on the UI thread. Main waits for these closes before closing
// the shared SSH opener after the event loop has stopped.
func (a *app) shutdownRemoteTransfers() {
	state := &a.remoteTransfers
	manager := state.manager
	if manager != nil {
		// Running cancellation only signals its context. Queued cancellation has
		// no SFTP work of its own, and completion callbacks merely schedule UI.
		for _, job := range manager.Snapshot().Jobs {
			if !job.Status.Terminal() {
				manager.Cancel(job.ID)
			}
		}
	}
	state.manager = nil
	state.generation = 0
	for _, prompt := range state.prompts {
		prompt.Respond(transfer.CollisionChoice{Decision: transfer.CollisionCancel})
	}
	state.prompts = nil
	state.jobs = nil
	if state.unsubscribe != nil {
		state.unsubscribe()
		state.unsubscribe = nil
	}
	if manager == nil {
		return
	}
	remoteTransferCloses.Add(1)
	go func() {
		defer remoteTransferCloses.Done()
		defer a.recoverBackground()
		closeRemoteTransferManager(manager)
	}()
}

func waitForRemoteTransferClose() {
	remoteTransferCloses.Wait()
}

func (a *app) currentRemoteTransfer(manager *transfer.Manager, generation uint64) bool {
	return manager != nil && a.remoteTransfers.manager == manager &&
		a.remoteTransfers.generation == generation && a.generation == generation && a.location.IsRemote()
}

func (a *app) remoteTransferManager() (*transfer.Manager, uint64, *sshworkspace.Client, error) {
	if a.state != stateReady || !a.location.IsRemote() || a.session == nil {
		return nil, 0, nil, errors.New(tr("transfers are available only in an open SSH workspace"))
	}
	provider, ok := a.session.(sshClientSession)
	if !ok {
		return nil, 0, nil, errors.New(tr("remote repository session does not provide an SSH client"))
	}
	client := provider.SSHClient()
	if client == nil {
		return nil, 0, nil, errors.New(tr("remote repository SSH client is not connected"))
	}
	state := &a.remoteTransfers
	if state.manager != nil {
		if state.generation == a.generation {
			return state.manager, state.generation, client, nil
		}
		a.shutdownRemoteTransfers()
	}
	manager := transfer.NewManager()
	generation := a.generation
	state.manager = manager
	state.generation = generation
	state.jobs = make(map[transfer.ID]*remoteTransferJob)
	state.prompts = make(map[transfer.ID]transfer.CollisionPrompt)
	state.toastPrefix = fmt.Sprintf("workspace-transfer:%p", a)
	state.unsubscribe = manager.Subscribe(func(snapshot transfer.Snapshot) {
		a.update(func() {
			a.applyRemoteTransferSnapshot(manager, generation, snapshot)
		})
	})
	return manager, generation, client, nil
}

func (a *app) remoteTransfersAvailable() bool {
	return a.state == stateReady && a.location.IsRemote()
}

var workspaceTransferDropOptions = mygotransfer.DropOptions{
	Formats:    []mygotransfer.Format{mygotransfer.FileList, mygotransfer.URIList},
	Operations: mygotransfer.Copy,
}

// workspaceTransferDropTarget accepts native files and folders in every open
// Explorer. Local workspaces import them locally; SSH workspaces upload them.
func (a *app) workspaceTransferDropTarget(element ui.Element, directory string, color ui.Color) {
	if a.state != stateReady || a.snapshot.Root == "" {
		return
	}
	if _, over := ui.DataDragOver(element, workspaceTransferDropOptions); over {
		element.Background(color)
	}
	if drop, ok := ui.DropData(element, workspaceTransferDropOptions); ok {
		a.receiveWorkspaceTransferDrop(drop.Data, directory)
	}
}

func (a *app) receiveWorkspaceTransferDrop(data mygotransfer.Data, directory string) {
	if a.state != stateReady || a.snapshot.Root == "" {
		return
	}
	if a.location.IsRemote() {
		a.receiveRemoteTransferDrop(data, directory)
		return
	}
	a.receiveLocalTransferDrop(data, directory)
}

func (a *app) receiveRemoteTransferDrop(data mygotransfer.Data, directory string) {
	if !a.remoteTransfersAvailable() {
		return
	}
	if sources, err := data.Files(); err == nil && len(sources) > 0 {
		a.beginRemoteUpload(sources, directory)
	}
}

func (a *app) remoteTransferExplorerMenu(menu *ui.Menu, item fileTreeRow) {
	if !a.remoteTransfersAvailable() || !validCodeManagedPath(item.path) {
		return
	}
	menu.Separator()
	label := "Download…"
	if item.isDirectory() {
		label = "Download as ZIP…"
	}
	if menu.Item(tr(label)).Chosen() {
		a.beginRemoteDownload(item.path, item.isDirectory())
	}
}

func (a *app) remoteTransferRootMenu(menu *ui.Menu) {
	if !a.remoteTransfersAvailable() {
		return
	}
	menu.Separator()
	if menu.Item(tr("Download repository as ZIP…")).Chosen() {
		a.beginRemoteDownload("", true)
	}
}

func (a *app) remoteTransferExplorerHover(c *ui.Context, parent ui.Element, item fileTreeRow) {
	if !a.remoteTransfersAvailable() || !parent.Hovered() {
		return
	}
	label := tr("Download") + " " + item.path
	button := compactIconButton(ui.ButtonBase(c)).Label(label).Tooltip(label)
	button.Children(func() {
		ui.Icon(c, downloadIcon).FontSize(13).TextColor(c.Theme().TextMuted)
	})
	if button.Clicked() {
		a.beginRemoteDownload(item.path, item.isDirectory())
	}
}

func (a *app) beginRemoteUpload(localSources []string, remoteDirectory string) {
	if len(localSources) == 0 {
		return
	}
	manager, generation, client, err := a.remoteTransferManager()
	if err != nil {
		a.notify("error", "Could not upload", safeError(err))
		return
	}
	destination, err := remoteWorkspacePath(a.snapshot.Root, remoteDirectory)
	if err != nil {
		a.notify("error", "Could not upload", safeError(err))
		return
	}
	sources := append([]string(nil), localSources...)
	go func() {
		defer a.recoverBackground()
		id, enqueueErr := enqueueRemoteUploadForUI(manager, client, sources, destination,
			func(prompt transfer.CollisionPrompt) {
				a.presentRemoteTransferCollision(manager, generation, prompt)
			},
			func(result transfer.Result) {
				a.completeRemoteTransfer(manager, generation, result, "upload", sources, destination, nil)
			})
		a.update(func() {
			if !a.currentRemoteTransfer(manager, generation) {
				return
			}
			if enqueueErr != nil {
				a.notify("error", "Could not enqueue upload", safeError(enqueueErr))
				return
			}
			a.ensureRemoteTransferJob(id, "upload", sources, destination, nil)
			a.applyRemoteTransferSnapshot(manager, generation, manager.Snapshot())
		})
	}()
}

func (a *app) beginRemoteDownload(relative string, directory bool) {
	if (relative != "" && !validCodeManagedPath(relative)) || !a.remoteTransfersAvailable() {
		return
	}
	manager, generation, client, err := a.remoteTransferManager()
	if err != nil {
		a.notify("error", "Could not download", safeError(err))
		return
	}
	source, err := remoteWorkspacePath(a.snapshot.Root, relative)
	if err != nil {
		a.notify("error", "Could not download", safeError(err))
		return
	}
	name := path.Base(source) + ".zip"
	title := "Download folder as ZIP"
	filters := []mygo.FileFilter{{Name: tr("ZIP archives"), Extensions: []string{"zip"}}}
	if !directory {
		name = path.Base(source)
		title = "Download file"
		filters = nil
	}
	go func() {
		defer a.recoverBackground()
		destination, dialogErr := chooseRemoteTransferSavePath(mygo.SaveDialogOptions{
			Parent:      a.window,
			Title:       tr(title),
			ButtonLabel: tr("Download"),
			DefaultPath: name,
			Filters:     filters,
		})
		a.update(func() {
			if !a.currentRemoteTransfer(manager, generation) {
				return
			}
			if dialogErr != nil {
				a.notify("error", "Could not download", safeError(dialogErr))
				return
			}
			if destination == "" {
				return
			}
			a.startRemoteDownload(manager, generation, client, source, destination)
		})
	}()
}

func (a *app) startRemoteDownload(manager *transfer.Manager, generation uint64, client *sshworkspace.Client, source, destination string) {
	sources := []string{source}
	outputs := []string{destination}
	go func() {
		defer a.recoverBackground()
		id, enqueueErr := enqueueRemoteDownloadForUI(manager, client, sources, outputs,
			func(prompt transfer.CollisionPrompt) {
				a.presentRemoteTransferCollision(manager, generation, prompt)
			},
			func(result transfer.Result) {
				a.completeRemoteTransfer(manager, generation, result, "download", nil, "", outputs)
			})
		a.update(func() {
			if !a.currentRemoteTransfer(manager, generation) {
				return
			}
			if enqueueErr != nil {
				a.notify("error", "Could not enqueue download", safeError(enqueueErr))
				return
			}
			a.ensureRemoteTransferJob(id, "download", nil, "", outputs)
			a.applyRemoteTransferSnapshot(manager, generation, manager.Snapshot())
		})
	}()
}

func (a *app) ensureRemoteTransferJob(id transfer.ID, kind string, localSources []string, remoteDirectory string, localOutputs []string) *remoteTransferJob {
	state := &a.remoteTransfers
	if state.jobs == nil {
		state.jobs = make(map[transfer.ID]*remoteTransferJob)
	}
	if job := state.jobs[id]; job != nil {
		return job
	}
	job := &remoteTransferJob{
		kind:            kind,
		localSources:    append([]string(nil), localSources...),
		remoteDirectory: remoteDirectory,
		localOutputs:    append([]string(nil), localOutputs...),
		result:          transfer.Result{ID: id, Status: transfer.StatusQueued},
	}
	state.jobs[id] = job
	return job
}

func (a *app) applyRemoteTransferSnapshot(manager *transfer.Manager, generation uint64, snapshot transfer.Snapshot) {
	if !a.currentRemoteTransfer(manager, generation) {
		return
	}
	for _, result := range snapshot.Jobs {
		job := a.remoteTransfers.jobs[result.ID]
		if job == nil && result.RetryOf != 0 {
			if previous := a.remoteTransfers.jobs[result.RetryOf]; previous != nil {
				job = a.ensureRemoteTransferJob(result.ID, previous.kind, previous.localSources, previous.remoteDirectory, previous.localOutputs)
			}
		}
		if job == nil {
			// A very fast operation can publish before Enqueue returns. Its
			// completion callback or the enqueue continuation supplies metadata.
			continue
		}
		a.publishRemoteTransfer(manager, generation, job, result)
	}
}

func (a *app) completeRemoteTransfer(manager *transfer.Manager, generation uint64, result transfer.Result, kind string, localSources []string, remoteDirectory string, localOutputs []string) {
	a.update(func() {
		if !a.currentRemoteTransfer(manager, generation) {
			return
		}
		job := a.ensureRemoteTransferJob(result.ID, kind, localSources, remoteDirectory, localOutputs)
		a.publishRemoteTransfer(manager, generation, job, result)
	})
}

func (a *app) publishRemoteTransfer(manager *transfer.Manager, generation uint64, job *remoteTransferJob, result transfer.Result) {
	job.result = result
	result = job.result
	terminal := result.Status.Terminal()
	if terminal && job.terminalNotified {
		return
	}
	if terminal {
		job.terminalNotified = true
		delete(a.remoteTransfers.prompts, result.ID)
	}
	jobTitle := tr("Upload")
	if job.kind == "download" {
		jobTitle = tr("Download")
	}
	toast := appToast{
		id:          fmt.Sprintf("%s:%d", a.remoteTransfers.toastPrefix, result.ID),
		title:       jobTitle,
		description: remoteTransferProgressDescription(job, result),
		timeout:     -1,
	}
	if !terminal {
		toast.title = trf("%s queued", jobTitle)
		if result.Status == transfer.StatusRunning {
			toast.title = trf("%s in progress", jobTitle)
		}
		if job.cancelling {
			toast.title = trf("Cancelling %s", strings.ToLower(jobTitle))
		}
		toast.action = tr("Cancel")
		toast.onAction = func() {
			a.cancelRemoteTransfer(manager, generation, result.ID)
		}
	} else {
		toast.timeout = 7 * time.Second
		toast.description = remoteTransferTerminalDescription(result)
		if result.TotalBytes > 0 {
			completed := min(result.CompletedBytes, result.TotalBytes)
			toast.description += " · " + trf("%s of %s", formatRemoteTransferBytes(completed), formatRemoteTransferBytes(result.TotalBytes))
		}
		switch result.Status {
		case transfer.StatusSucceeded:
			toast.kind = "success"
			toast.title = trf("%s complete", jobTitle)
		case transfer.StatusPartial:
			toast.kind = "error"
			toast.title = trf("%s partially completed", jobTitle)
		case transfer.StatusFailed:
			toast.kind = "error"
			toast.title = trf("%s failed", jobTitle)
		case transfer.StatusCancelled:
			toast.title = trf("%s cancelled", jobTitle)
		}
		if result.Status == transfer.StatusFailed || result.Status == transfer.StatusPartial || result.Status == transfer.StatusCancelled {
			toast.action = tr("Retry")
			toast.onAction = func() { a.retryRemoteTransfer(manager, generation, result.ID) }
		}
		toast.moreActions = append(toast.moreActions, appToastAction{
			label: tr("Details"),
			onAction: func() {
				a.showRemoteTransferDetails(manager, generation, result.ID)
			},
		})
		if job.kind == "download" && result.CompletedItems > 0 && len(job.localOutputs) > 0 {
			output := job.localOutputs[0]
			reveal := appToastAction{label: tr("Reveal"), onAction: func() {
				if a.currentRemoteTransfer(manager, generation) {
					revealRemoteTransferOutput(output)
				}
			}}
			if toast.action == "" {
				toast.action = reveal.label
				toast.onAction = reveal.onAction
			} else {
				toast.moreActions = append(toast.moreActions, reveal)
			}
		}
		if toast.action == "" {
			toast.action = tr("Details")
			toast.onAction = func() { a.showRemoteTransferDetails(manager, generation, result.ID) }
			toast.moreActions = nil
		}
	}
	a.notifyToast(toast)
	if terminal && job.kind == "upload" && (result.Status == transfer.StatusSucceeded || result.Status == transfer.StatusPartial) {
		a.code.filesLoaded = false
		a.requestCodeFiles()
	}
}

func remoteTransferProgressDescription(job *remoteTransferJob, result transfer.Result) string {
	if job.cancelling {
		return tr("Waiting for the current item to stop and clean up…")
	}
	processed := result.CompletedItems + result.SkippedItems + result.FailedItems
	text := trf("%d of %d items", processed, result.TotalItems)
	if result.TotalBytes > 0 {
		percent := result.CompletedBytes * 100 / result.TotalBytes
		text = trf("%s · %d%% · %s of %s", text, percent, formatRemoteTransferBytes(result.CompletedBytes), formatRemoteTransferBytes(result.TotalBytes))
	} else if result.CompletedBytes > 0 {
		text += " · " + formatRemoteTransferBytes(result.CompletedBytes)
	}
	return text
}

func remoteTransferTerminalDescription(result transfer.Result) string {
	parts := []string{trf("%d completed", result.CompletedItems)}
	if result.SkippedItems > 0 {
		parts = append(parts, trf("%d skipped", result.SkippedItems))
	}
	if result.FailedItems > 0 {
		parts = append(parts, trf("%d failed", result.FailedItems))
	}
	// The transfer error is a dynamic SSH, filesystem, server, or user payload.
	// Append it verbatim after redaction rather than treating it as a catalog key.
	if message := firstRemoteTransferError(result); message != "" && result.Status != transfer.StatusSucceeded {
		parts = append(parts, message)
	}
	return strings.Join(parts, " · ")
}

func firstRemoteTransferError(result transfer.Result) string {
	if len(result.Errors) > 0 && result.Errors[0].Err != nil {
		return safeError(result.Errors[0].Err)
	}
	return safeError(result.Err)
}

func formatRemoteTransferBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	divisor, exponent := int64(unit), 0
	for value := bytes / unit; value >= unit && exponent < 4; value /= unit {
		divisor *= unit
		exponent++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(divisor), "KMGTPE"[exponent])
}

func (a *app) cancelRemoteTransfer(manager *transfer.Manager, generation uint64, id transfer.ID) {
	if !a.currentRemoteTransfer(manager, generation) {
		return
	}
	job := a.remoteTransfers.jobs[id]
	if job == nil || job.result.Status.Terminal() {
		return
	}
	job.cancelling = true
	a.publishRemoteTransfer(manager, generation, job, job.result)
	go func() {
		defer a.recoverBackground()
		manager.Cancel(id)
	}()
}

func (a *app) retryRemoteTransfer(manager *transfer.Manager, generation uint64, id transfer.ID) {
	if !a.currentRemoteTransfer(manager, generation) {
		return
	}
	previous := a.remoteTransfers.jobs[id]
	if previous == nil {
		return
	}
	newID, err := manager.Retry(id)
	if err != nil {
		a.notify("error", "Could not retry transfer", safeError(err))
		return
	}
	job := a.ensureRemoteTransferJob(newID, previous.kind, previous.localSources, previous.remoteDirectory, previous.localOutputs)
	job.result.RetryOf = id
	a.applyRemoteTransferSnapshot(manager, generation, manager.Snapshot())
}

func (a *app) showRemoteTransferDetails(manager *transfer.Manager, generation uint64, id transfer.ID) {
	if !a.currentRemoteTransfer(manager, generation) {
		return
	}
	job := a.remoteTransfers.jobs[id]
	if job == nil {
		return
	}
	title := "Upload details"
	if job.kind == "download" {
		title = "Download details"
	}
	detail := remoteTransferJobDetails(job)
	go func() {
		defer a.recoverBackground()
		_, _ = showRemoteTransferMessage(mygo.MessageOptions{
			Parent:        a.window,
			Type:          mygo.MessageInfo,
			Title:         tr(title),
			Message:       trf("Transfer batch #%d", id),
			Detail:        detail,
			Buttons:       []string{tr("Close")},
			DefaultButton: 0,
			CancelButton:  0,
		})
	}()
}

func remoteTransferStatusText(status transfer.Status) string {
	switch status {
	case transfer.StatusQueued:
		return tr("queued")
	case transfer.StatusRunning:
		return tr("running")
	case transfer.StatusSucceeded:
		return tr("succeeded")
	case transfer.StatusPartial:
		return tr("partial")
	case transfer.StatusFailed:
		return tr("failed")
	case transfer.StatusCancelled:
		return tr("cancelled")
	default:
		return string(status)
	}
}

func remoteTransferJobDetails(job *remoteTransferJob) string {
	result := job.result
	var text strings.Builder
	fmt.Fprintf(&text, tr("Status: %s\n"), remoteTransferStatusText(result.Status))
	fmt.Fprintf(&text, tr("Items: %d total, %d completed, %d skipped, %d failed\n"),
		result.TotalItems, result.CompletedItems, result.SkippedItems, result.FailedItems)
	fmt.Fprintf(&text, tr("Bytes: %s of %s\n"), formatRemoteTransferBytes(result.CompletedBytes), formatRemoteTransferBytes(result.TotalBytes))
	if job.remoteDirectory != "" {
		fmt.Fprintf(&text, tr("Remote destination: %s\n"), job.remoteDirectory)
	}
	for _, filename := range job.localSources {
		fmt.Fprintf(&text, tr("Local source: %s\n"), filename)
	}
	for _, filename := range job.localOutputs {
		fmt.Fprintf(&text, tr("Local output: %s\n"), filename)
	}
	for _, itemErr := range result.Errors {
		if itemErr.ItemIndex >= 0 {
			fmt.Fprintf(&text, "\n%s → %s\n", itemErr.Item.Source, itemErr.Item.Destination)
		}
		if itemErr.Err != nil {
			fmt.Fprintf(&text, "%s\n", safeError(itemErr.Err))
		}
	}
	if len(result.Errors) == 0 && result.Err != nil {
		fmt.Fprintf(&text, "\n%s\n", safeError(result.Err))
	}
	return shorten(text.String(), 12000)
}

func (a *app) presentRemoteTransferCollision(manager *transfer.Manager, generation uint64, prompt transfer.CollisionPrompt) {
	a.update(func() {
		if !a.currentRemoteTransfer(manager, generation) {
			prompt.Respond(transfer.CollisionChoice{Decision: transfer.CollisionCancel})
			return
		}
		if a.remoteTransfers.prompts == nil {
			a.remoteTransfers.prompts = make(map[transfer.ID]transfer.CollisionPrompt)
		}
		a.remoteTransfers.prompts[prompt.ID] = prompt
		go func() {
			defer a.recoverBackground()
			result, err := showRemoteTransferMessage(mygo.MessageOptions{
				Parent:             a.window,
				Type:               mygo.MessageWarning,
				Title:              tr("Destination already exists"),
				Message:            prompt.Item.Destination,
				Detail:             tr("Skip this item, overwrite the destination, or cancel the rest of the batch."),
				Buttons:            []string{tr("Skip"), tr("Overwrite"), tr("Cancel")},
				DefaultButton:      0,
				CancelButton:       2,
				DestructiveButtons: []int{1},
				CheckboxLabel:      tr("Apply to all"),
			})
			choice := transfer.CollisionChoice{Decision: transfer.CollisionCancel}
			if err == nil {
				switch result.Button {
				case 0:
					choice.Decision = transfer.CollisionSkip
					choice.ApplyToAll = result.CheckboxChecked
				case 1:
					choice.Decision = transfer.CollisionOverwrite
					choice.ApplyToAll = result.CheckboxChecked
				}
			}
			prompt.Respond(choice)
			a.update(func() {
				if a.currentRemoteTransfer(manager, generation) {
					delete(a.remoteTransfers.prompts, prompt.ID)
				}
			})
		}()
	})
}
