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
	a.ssh.browser.Listing = sshFolderListing{}
	a.ssh.browser.List = ui.ListState{}
	a.ssh.browser.CreateOpen = false
	a.startSSHFolderJob(a.ssh.workspaceRoot, false, false, 350*time.Millisecond, nil)
}

func (a *app) startSSHFolderJob(root string, open, initialize bool, delay time.Duration, read func(context.Context, sshworkspace.Profile, string, bool) (sshFolderListing, error)) {
	profile, ok := a.selectedSSHProfile()
	if !ok {
		a.ssh.workspaceError = tr("Choose an SSH profile first.")
		return
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = "/home"
	}
	a.cancelSSHWorkspaceResolve()
	a.ssh.workspaceRoot = root
	if a.ssh.browser.Listing.Root != root {
		a.ssh.browser.Listing = sshFolderListing{}
		a.ssh.browser.List = ui.ListState{}
	}
	a.ssh.browser.CreateOpen = false
	a.ssh.workspaceError = ""
	if !validSSHRoot(root) {
		a.ssh.workspaceError = tr("Enter an absolute remote folder, ~, or a path beginning with ~/.")
		return
	}
	if initialize && !a.ssh.browser.AllowInit {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.ssh.workspaceResolveCancel = cancel
	a.ssh.workspaceResolving = true
	sequence := a.ssh.workspaceResolveSeq
	a.ssh.workspaceError = ""
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
			if sequence != a.ssh.workspaceResolveSeq || !a.ssh.workspaceOpen {
				return false
			}
			a.ssh.workspaceResolveCancel = nil
			a.ssh.workspaceResolving = false
			if err != nil {
				if os.IsNotExist(err) {
					a.ssh.workspaceError = tr("Remote folder does not exist.")
				} else {
					a.ssh.workspaceError = statusErrorLine(repository.RedactSecrets(err.Error()))
				}
				return true
			}
			if !path.IsAbs(listing.Root) {
				a.ssh.workspaceError = tr("The server returned an invalid remote folder.")
				return true
			}
			a.ssh.browser.Listing = listing
			a.ssh.workspaceRoot = listing.Root
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
	if a.ssh.workspaceResolving || a.ssh.browser.Listing.Root == "" || a.ssh.workspaceRoot != a.ssh.browser.Listing.Root {
		return
	}
	a.ssh.browser.CreateOpen = true
	a.ssh.browser.CreateDirectory = directory
	a.ssh.browser.CreateRoot = a.ssh.browser.Listing.Root
	a.ssh.browser.CreateName = ""
	a.ssh.workspaceError = ""
}

func (a *app) submitSSHBrowserCreate() {
	if !a.ssh.browser.CreateOpen || a.ssh.workspaceResolving {
		return
	}
	name, ok := cleanCodeEntryName(a.ssh.browser.CreateName)
	if !ok {
		a.ssh.workspaceError = tr("Enter a valid name without path separators.")
		return
	}
	root, directory := a.ssh.browser.CreateRoot, a.ssh.browser.CreateDirectory
	if root != a.ssh.workspaceRoot || root != a.ssh.browser.Listing.Root {
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
	if _, ok := a.opener.(appSSHRepositoryOpener); ok && a.ssh.workspaceOpen && !a.ssh.profileFormOpen {
		a.loadSSHFolder(a.ssh.workspaceRoot, false, false)
	}
}
