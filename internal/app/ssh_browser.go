package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
)

type sshFolderListing struct {
	Root       string
	Entries    []os.FileInfo
	Repository bool
}

type sshBrowserState struct {
	Listing         sshFolderListing
	List            ui.ListState
	AllowInit       bool
	CreateOpen      bool
	CreateDirectory bool
	CreateName      string
	CreateRoot      string
}

func (a *app) readSSHFolder(ctx context.Context, profile sshworkspace.Profile, root string, initialize bool) (sshFolderListing, error) {
	opener, ok := a.opener.(appSSHRepositoryOpener)
	if !ok {
		return sshFolderListing{}, errors.New("SSH folder browsing requires the SSH workspace opener")
	}
	lease, err := opener.acquire(ctx, workspace.Location{Kind: workspace.SSH, ProfileID: profile.ID})
	if err != nil {
		return sshFolderListing{}, err
	}
	defer lease.Close()
	return readSSHFolder(ctx, lease.Client(), root, initialize)
}

func readSSHFolder(ctx context.Context, client *sshworkspace.Client, root string, initialize bool) (sshFolderListing, error) {
	resolved, err := client.ResolvePath(ctx, root)
	if err != nil {
		return sshFolderListing{}, err
	}
	entries, err := client.ReadDirContext(ctx, resolved)
	if err != nil {
		return sshFolderListing{}, err
	}
	_, managed, err := client.GitRoot(ctx, resolved)
	if err != nil {
		return sshFolderListing{}, err
	}
	if initialize && !managed {
		result, err := client.Run(ctx, resolved, "git", "init")
		if err != nil {
			return sshFolderListing{}, fmt.Errorf("initialize remote repository: %s: %w", strings.TrimSpace(string(result.Output)), err)
		}
		_, managed, err = client.GitRoot(ctx, resolved)
		if err != nil {
			return sshFolderListing{}, err
		}
		if !managed {
			return sshFolderListing{}, errors.New("remote folder is still not a Git repository after initialization")
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})
	return sshFolderListing{Root: resolved, Entries: entries, Repository: managed}, nil
}

func (a *app) loadSSHFolder(root string, open, initialize bool) {
	a.startSSHFolderJob(root, open, initialize, 0, nil)
}

func (a *app) editSSHFolderPath() {
	a.sshBrowser.Listing = sshFolderListing{}
	a.sshBrowser.List = ui.ListState{}
	a.sshBrowser.CreateOpen = false
	a.startSSHFolderJob(a.sshWorkspaceRoot, false, false, 350*time.Millisecond, nil)
}

func (a *app) startSSHFolderJob(root string, open, initialize bool, delay time.Duration, read func(context.Context, sshworkspace.Profile, string, bool) (sshFolderListing, error)) {
	profile, ok := a.selectedSSHProfile()
	if !ok {
		a.sshWorkspaceError = tr("Choose an SSH profile first.")
		return
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = "/home"
	}
	a.cancelSSHWorkspaceResolve()
	a.sshWorkspaceRoot = root
	if a.sshBrowser.Listing.Root != root {
		a.sshBrowser.Listing = sshFolderListing{}
		a.sshBrowser.List = ui.ListState{}
	}
	a.sshBrowser.CreateOpen = false
	a.sshWorkspaceError = ""
	if !validSSHRoot(root) {
		a.sshWorkspaceError = tr("Enter an absolute remote folder, ~, or a path beginning with ~/.")
		return
	}
	if initialize && !a.sshBrowser.AllowInit {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.sshWorkspaceResolveCancel = cancel
	a.sshWorkspaceResolving = true
	sequence := a.sshWorkspaceResolveSeq
	a.sshWorkspaceError = ""
	if read == nil {
		read = a.sshBrowseFolder
	}
	if read == nil {
		read = a.readSSHFolder
	}
	go func() {
		defer a.recoverBackground()
		defer cancel()
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
		listing, err := read(ctx, profile, root, initialize)
		a.updateChanged(func() bool {
			if sequence != a.sshWorkspaceResolveSeq || !a.sshWorkspaceOpen {
				return false
			}
			a.sshWorkspaceResolveCancel = nil
			a.sshWorkspaceResolving = false
			if err != nil {
				if os.IsNotExist(err) {
					a.sshWorkspaceError = tr("Remote folder does not exist.")
				} else {
					a.sshWorkspaceError = statusErrorLine(repository.RedactSecrets(err.Error()))
				}
				return true
			}
			if !path.IsAbs(listing.Root) {
				a.sshWorkspaceError = tr("The server returned an invalid remote folder.")
				return true
			}
			a.sshBrowser.Listing = listing
			a.sshWorkspaceRoot = listing.Root
			if open && !listing.Repository && !initialize {
				a.notify("info", "This folder is not a Git repository", tr("Choose a Git-managed folder or enable repository creation, then click Open."))
			}
			if open && listing.Repository {
				a.path = (workspace.Location{Kind: workspace.SSH, ProfileID: profile.ID, Root: listing.Root}).String()
				a.closeSSHWorkspacePicker()
				a.beginOpen()
			}
			return true
		})
	}()
}

func (a *app) beginSSHBrowserCreate(directory bool) {
	if a.sshWorkspaceResolving || a.sshBrowser.Listing.Root == "" || a.sshWorkspaceRoot != a.sshBrowser.Listing.Root {
		return
	}
	a.sshBrowser.CreateOpen = true
	a.sshBrowser.CreateDirectory = directory
	a.sshBrowser.CreateRoot = a.sshBrowser.Listing.Root
	a.sshBrowser.CreateName = ""
	a.sshWorkspaceError = ""
}

func (a *app) submitSSHBrowserCreate() {
	if !a.sshBrowser.CreateOpen || a.sshWorkspaceResolving {
		return
	}
	name, ok := cleanCodeEntryName(a.sshBrowser.CreateName)
	if !ok {
		a.sshWorkspaceError = tr("Enter a valid name without path separators.")
		return
	}
	root, directory := a.sshBrowser.CreateRoot, a.sshBrowser.CreateDirectory
	if root != a.sshWorkspaceRoot || root != a.sshBrowser.Listing.Root {
		return
	}
	create := a.sshCreateEntry
	if create == nil {
		create = a.createSSHBrowserEntry
	}
	a.startSSHFolderJob(root, false, false, 0, func(ctx context.Context, profile sshworkspace.Profile, root string, _ bool) (sshFolderListing, error) {
		return create(ctx, profile, root, name, directory)
	})
}

func (a *app) createSSHBrowserEntry(ctx context.Context, profile sshworkspace.Profile, root, name string, directory bool) (sshFolderListing, error) {
	opener, ok := a.opener.(appSSHRepositoryOpener)
	if !ok {
		return sshFolderListing{}, errors.New("SSH folder browsing requires the SSH workspace opener")
	}
	lease, err := opener.acquire(ctx, workspace.Location{Kind: workspace.SSH, ProfileID: profile.ID})
	if err != nil {
		return sshFolderListing{}, err
	}
	defer lease.Close()
	return createSSHBrowserEntry(ctx, lease.Client(), root, name, directory)
}

func createSSHBrowserEntry(ctx context.Context, client *sshworkspace.Client, root, name string, directory bool) (sshFolderListing, error) {
	if _, ok := cleanCodeEntryName(name); !ok {
		return sshFolderListing{}, errors.New("invalid file or folder name")
	}
	if directory {
		result, err := client.Run(ctx, root, "mkdir", "--", name)
		if err != nil {
			return sshFolderListing{}, fmt.Errorf("create remote folder: %s: %w", strings.TrimSpace(string(result.Output)), err)
		}
	} else if err := client.CreateFileExclusive(ctx, path.Join(root, name)); err != nil {
		return sshFolderListing{}, err
	}
	return readSSHFolder(ctx, client, root, false)
}

func (a *app) refreshSSHBrowserOnSelection() {
	if _, ok := a.opener.(appSSHRepositoryOpener); ok && a.sshWorkspaceOpen && !a.sshProfileFormOpen {
		a.loadSSHFolder(a.sshWorkspaceRoot, false, false)
	}
}
