package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"lazygitapp/internal/repository"
	"lazygitapp/internal/sshintegration"
	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
)

func sshBrowserTestApp(t *testing.T) (*app, chan func()) {
	t.Helper()
	a := newApp(fakeOpener{})
	a.settingsLoaded = true
	a.settings.SSHProfiles = []sshworkspace.Profile{{ID: "server", Name: "Server", Host: "example.test", Username: "git", AuthMode: sshworkspace.AuthAgent, DefaultRoot: "~"}}
	updates := make(chan func(), 10)
	a.update = func(fn func()) { updates <- fn }
	a.openSSHWorkspacePicker()
	a.selectSSHWorkspaceProfile("server")
	return a, updates
}

func applySSHBrowserUpdate(t *testing.T, updates chan func()) {
	t.Helper()
	select {
	case fn := <-updates:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("SSH browser update timed out")
	}
}

func TestSSHBrowserOpenInitializesOnlyWithOptIn(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	var initialized bool
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, initialize bool) (sshFolderListing, error) {
		initialized = initialize
		return sshFolderListing{Root: "/home/git", Repository: initialize, Entries: []os.FileInfo{remoteTestFileInfo{name: "project", mode: os.ModeDir}}}, nil
	}
	var opened string
	a.claimOpen = func(root string) bool { opened = root; return true }
	a.openSelectedSSHWorkspace()
	applySSHBrowserUpdate(t, updates)
	if initialized || opened != "" || !a.sshWorkspaceOpen || a.sshBrowser.AllowInit {
		t.Fatal("plain folder opened or initialized without permission")
	}
	tester := ui.NewTester(a.view, 1280, 900)
	tester.Frame()
	if !tester.HasText("This folder is not a Git repository") || !tester.HasText("Allow creating a Git repository") {
		t.Fatalf("missing plain-folder choices: %v", tester.Texts())
	}
	if tester.HasText("Initialize repository") {
		t.Fatal("separate initialize button remains")
	}
	if err := tester.Click("Allow creating a Git repository"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if err := tester.Click("Open selected SSH workspace"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if a.confirm != nil {
		t.Fatal("Open unexpectedly required a second confirmation")
	}
	applySSHBrowserUpdate(t, updates)
	if !initialized || opened != "ssh://server/home/git" {
		t.Fatalf("initialize=%v open=%q", initialized, opened)
	}
	a.openSSHWorkspacePicker()
	if a.sshBrowser.AllowInit {
		t.Fatal("initialization permission survived reopening")
	}
}

func TestSSHBrowserNavigationAndStaleResults(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	repaints := make(chan bool, 4)
	a.updateChanged = func(fn func() bool) {
		updates <- func() { repaints <- fn() }
	}
	requests := make(chan string, 4)
	release := make(chan struct{})
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, _ bool) (sshFolderListing, error) {
		requests <- root
		if root == "/old" {
			<-release
		}
		return sshFolderListing{Root: root}, nil
	}
	a.loadSSHFolder("/old", false, false)
	<-requests
	a.loadSSHFolder("/new", false, false)
	<-requests
	applySSHBrowserUpdate(t, updates)
	if !<-repaints {
		t.Fatal("current listing did not request its one repaint")
	}
	close(release)
	applySSHBrowserUpdate(t, updates)
	if <-repaints {
		t.Fatal("stale listing requested a repaint")
	}
	if a.sshBrowser.Listing.Root != "/new" {
		t.Fatal("stale listing replaced current folder")
	}
	a.loadSSHFolder("/closed", false, false)
	<-requests
	a.closeSSHWorkspacePicker()
	applySSHBrowserUpdate(t, updates)
	if <-repaints {
		t.Fatal("closed browser completion requested a repaint")
	}
	if a.sshWorkspaceOpen || a.sshWorkspaceResolving {
		t.Fatal("closed browser reopened from background result")
	}
}

func TestSSHNotRepositoryReturnsToBrowser(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.closeSSHWorkspacePicker()
	a.sshBrowseFolder = func(context.Context, sshworkspace.Profile, string, bool) (sshFolderListing, error) {
		return sshFolderListing{Root: "/root"}, nil
	}
	a.setOpenError("ssh://server/root", a.generation, repository.ErrNotRepository)
	applySSHBrowserUpdate(t, updates)
	if !a.sshWorkspaceOpen || a.sshBrowser.Listing.Root != "/root" || a.offerInitPath != "" || a.sshBrowser.AllowInit {
		t.Fatal("plain SSH folder did not return to safe browser")
	}
}

func TestSSHNotRepositoryRecoveryReadsOnlyRequestedRoot(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.opener = appSSHRepositoryOpener{}
	requests := make(chan string, 2)
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, _ bool) (sshFolderListing, error) {
		requests <- root
		return sshFolderListing{Root: root}, nil
	}
	a.setOpenError("ssh://server/custom/repository", a.generation, repository.ErrNotRepository)
	applySSHBrowserUpdate(t, updates)
	if got := <-requests; got != "/custom/repository" {
		t.Fatalf("recovery requested %q before or instead of its repository", got)
	}
	select {
	case root := <-requests:
		t.Fatalf("recovery made a redundant %q read", root)
	default:
	}
	if !a.sshWorkspaceOpen || a.sshBrowser.Listing.Root != "/custom/repository" {
		t.Fatal("recovery did not leave the requested repository in the browser")
	}
}

func TestSSHBrowserVirtualizesLargeFolder(t *testing.T) {
	a, _ := sshBrowserTestApp(t)
	entries := make([]os.FileInfo, 5000)
	for index := range entries {
		entries[index] = remoteTestFileInfo{name: fmt.Sprintf("entry-%04d", index), mode: os.ModeDir}
	}
	a.sshWorkspaceRoot = "/large"
	a.sshBrowser.Listing = sshFolderListing{Root: "/large", Entries: entries}
	tester := ui.NewTester(a.view, 1280, 900)
	tester.Frame()
	if tester.HasText("entry-4999") {
		t.Fatal("large folder built an entry thousands of rows outside its viewport")
	}
	tester.Frame()
	a.sshBrowser.List.ScrollTo(len(entries), ui.End)
	tester.Frame()
	if !tester.HasText("entry-4999") {
		t.Fatal("virtualized folder cannot scroll to its last entry")
	}
}

func TestSSHBrowserRealOpenSSH(t *testing.T) {
	server := sshintegration.Start(t)
	connector, err := sshworkspace.NewConnector(server.SecretStore, filepath.Join(t.TempDir(), "known_hosts"), func(sshworkspace.HostKeyInfo) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := connector.Connect(ctx, server.Profile)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "project"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := repository.OpenSSHWorkspace(ctx, client, workspace.Location{Kind: workspace.SSH, ProfileID: server.Profile.ID, Root: root}, nil); !errors.Is(err, repository.ErrNotRepository) {
		t.Fatalf("plain repository error=%v", err)
	}
	listing, err := readSSHFolder(ctx, client, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Repository || len(listing.Entries) != 2 || listing.Entries[0].Name() != "project" {
		t.Fatalf("plain folder = %+v", listing)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatal("browsing created .git")
	}

	if _, err := createSSHBrowserEntry(ctx, client, root, "created folder", true); err != nil {
		t.Fatal(err)
	}
	if _, err := createSSHBrowserEntry(ctx, client, root, "created file.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := createSSHBrowserEntry(ctx, client, root, "hello.txt", false); err == nil {
		t.Fatal("existing file was overwritten")
	}
	contents, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil || string(contents) != "hello" {
		t.Fatalf("original contents=%q error=%v", contents, err)
	}
	if _, err := createSSHBrowserEntry(ctx, client, root, "created folder", true); err == nil {
		t.Fatal("existing folder was silently reused")
	}
	if _, err := createSSHBrowserEntry(ctx, client, root, "../escape", false); err == nil {
		t.Fatal("path traversal accepted")
	}
	listing, err = readSSHFolder(ctx, client, root, true)
	if err != nil || !listing.Repository {
		t.Fatalf("initialized listing=%+v err=%v", listing, err)
	}
	nested, err := readSSHFolder(ctx, client, filepath.Join(root, "project"), true)
	if err != nil || !nested.Repository {
		t.Fatalf("nested listing=%+v err=%v", nested, err)
	}
	if _, err := os.Stat(filepath.Join(root, "project", ".git")); !os.IsNotExist(err) {
		t.Fatal("created a nested repository inside an existing worktree")
	}
	_, err = readSSHFolder(ctx, client, filepath.Join(root, "missing"), false)
	if err == nil || strings.Contains(err.Error(), "not a Git repository") {
		t.Fatalf("missing-directory error=%v", err)
	}
}

func TestSSHBrowserFolderButtons(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.sshWorkspaceRoot = "/home/git"
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, _ bool) (sshFolderListing, error) {
		return sshFolderListing{Root: root, Entries: []os.FileInfo{remoteTestFileInfo{name: "project", mode: os.ModeDir}, remoteTestFileInfo{name: "notes.txt"}}}, nil
	}
	tester := ui.NewTester(a.view, 1280, 900)
	tester.Frame()
	if err := tester.Click("Browse remote folder"); err != nil {
		t.Fatal(err)
	}
	applySSHBrowserUpdate(t, updates)
	tester.Frame()
	if !tester.HasText("notes.txt") {
		t.Fatal("file entries missing")
	}
	if err := tester.Click("Browse remote folder project"); err != nil {
		t.Fatal(err)
	}
	applySSHBrowserUpdate(t, updates)
	tester.Frame()
	if a.sshWorkspaceRoot != "/home/git/project" {
		t.Fatalf("child path=%q", a.sshWorkspaceRoot)
	}
	if !tester.HasText("..") {
		t.Fatal("parent folder entry is missing")
	}
	if a.sshBrowser.AllowInit {
		t.Fatal("browsing enabled repository creation")
	}
	if err := tester.Click("Parent folder"); err != nil {
		t.Fatal(err)
	}
	applySSHBrowserUpdate(t, updates)
	tester.Frame()
	if a.sshWorkspaceRoot != "/home/git" {
		t.Fatalf("parent path=%q", a.sshWorkspaceRoot)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "ssh-folder-browser", tester)
	}
}

func TestSSHBrowserEditedPathRefreshesAndWarns(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.sshWorkspaceRoot = ""
	requests := make(chan string, 4)
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, _ bool) (sshFolderListing, error) {
		requests <- root
		if root == "/missing" {
			return sshFolderListing{}, os.ErrNotExist
		}
		return sshFolderListing{Root: root}, nil
	}
	tester := ui.NewTester(a.view, 1280, 900)
	tester.Frame()
	if err := tester.Click("SSH workspace remote folder"); err != nil {
		t.Fatal(err)
	}
	tester.Type("/project")
	tester.Frame()
	applySSHBrowserUpdate(t, updates)
	tester.Frame()
	if a.sshBrowser.Listing.Root != "/project" || a.sshWorkspaceRoot != "/project" {
		t.Fatalf("typed path and listing disagree: path=%q listing=%q", a.sshWorkspaceRoot, a.sshBrowser.Listing.Root)
	}
	// A later edit cancels the earlier pending refresh and hides the old folder.
	a.sshWorkspaceRoot = "/superseded"
	a.editSSHFolderPath()
	a.sshWorkspaceRoot = "/missing"
	a.editSSHFolderPath()
	if a.sshBrowser.Listing.Root != "" {
		t.Fatal("old listing remained under an edited path")
	}
	applySSHBrowserUpdate(t, updates)
	tester.Frame()
	if !tester.HasText("Remote folder does not exist.") || a.sshBrowser.Listing.Root != "" {
		t.Fatalf("missing path has no warning: %v", tester.Texts())
	}
	close(requests)
	for root := range requests {
		if root == "/superseded" {
			t.Fatal("superseded path was fetched")
		}
	}
}

func TestSSHBrowserContextMenuCreatesInDisplayedFolder(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(fmt.Sprint(directory), func(t *testing.T) {
			a, updates := sshBrowserTestApp(t)
			a.sshWorkspaceRoot = "/home/git"
			a.sshBrowser.Listing = sshFolderListing{Root: "/home/git", Entries: []os.FileInfo{remoteTestFileInfo{name: "project", mode: os.ModeDir}}}
			var createdRoot, createdName string
			var createdDirectory bool
			a.sshCreateEntry = func(_ context.Context, _ sshworkspace.Profile, root, name string, isDir bool) (sshFolderListing, error) {
				createdRoot, createdName, createdDirectory = root, name, isDir
				return sshFolderListing{Root: root, Entries: []os.FileInfo{remoteTestFileInfo{name: name}}}, nil
			}
			tester := ui.NewTester(a.view, 1280, 900)
			tester.Frame()
			target := "Browse remote folder project"
			if directory {
				target = "Remote folder contents"
			}
			if err := tester.RightClick(target); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			menuLabel := "New File…"
			if directory {
				menuLabel = "New Folder…"
			}
			if err := tester.Click(menuLabel); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			if !a.sshWorkspaceOpen || !a.sshBrowser.CreateOpen {
				t.Fatal("context menu did not open the creation form")
			}
			if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
				saveScreenshot(t, dir, fmt.Sprintf("ssh-browser-create-%v", directory), tester)
			}
			tester.Type("new entry")
			tester.Frame()
			if err := tester.Click("Create remote entry"); err != nil {
				t.Fatal(err)
			}
			tester.Frame()
			applySSHBrowserUpdate(t, updates)
			tester.Frame()
			if createdRoot != "/home/git" || createdName != "new entry" || createdDirectory != directory {
				t.Fatalf("created %q/%q directory=%v", createdRoot, createdName, createdDirectory)
			}
			if !tester.HasText("new entry") || a.sshBrowser.CreateOpen {
				t.Fatal("creation did not refresh and close its form")
			}
		})
	}
}

func TestSSHBrowserLoadsSelectedProfileImmediately(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.opener = appSSHRepositoryOpener{}
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, _ bool) (sshFolderListing, error) {
		if root != "/home" {
			t.Errorf("initial root=%q", root)
		}
		return sshFolderListing{Root: "/home/git"}, nil
	}
	a.openSSHWorkspacePicker()
	if a.sshWorkspaceProfileID != "" || a.sshWorkspaceResolving {
		t.Fatal("picker connected before selection")
	}
	tester := ui.NewTester(a.view, 1280, 900)
	tester.Frame()
	if err := tester.Click("Use SSH profile Server"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	applySSHBrowserUpdate(t, updates)
	if a.sshWorkspaceRoot != "/home/git" || a.sshBrowser.Listing.Root != a.sshWorkspaceRoot {
		t.Fatal("selected profile did not populate and synchronize browser")
	}
}

func TestSSHBrowserListRemainsVisibleWhileLoadingAndOnError(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	tester := ui.NewTester(a.view, 1280, 900)
	tester.Frame()
	initial, ok := tester.Find("Remote folder contents")
	if !ok || initial.H < 180 {
		t.Fatalf("initial explorer is missing: %+v", initial)
	}
	release := make(chan struct{})
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, _ bool) (sshFolderListing, error) {
		if root == "/missing" {
			return sshFolderListing{}, os.ErrNotExist
		}
		<-release
		return sshFolderListing{Root: root}, nil
	}
	a.loadSSHFolder("/next", false, false)
	tester.Frame()
	loading, ok := tester.Find("Remote folder contents")
	if !ok || loading.H != initial.H || !tester.HasText("Loading remote folder…") {
		t.Fatalf("loading explorer collapsed or has no indicator: %+v texts=%v", loading, tester.Texts())
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "ssh-browser-loading", tester)
	}
	close(release)
	applySSHBrowserUpdate(t, updates)
	tester.Frame()
	loaded, _ := tester.Find("Remote folder contents")
	if loaded.H != initial.H {
		t.Fatal("loaded explorer height changed")
	}
	a.loadSSHFolder("/missing", false, false)
	applySSHBrowserUpdate(t, updates)
	tester.Frame()
	failed, ok := tester.Find("Remote folder contents")
	if !ok || failed.H != initial.H || !tester.HasText("Remote folder does not exist.") {
		t.Fatal("failed explorer disappeared or hid error")
	}
}

func TestSSHBrowserOpenNonRepositoryNotifies(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, initialize bool) (sshFolderListing, error) {
		if initialize {
			t.Error("creation was not enabled")
		}
		return sshFolderListing{Root: root}, nil
	}
	a.loadSSHFolder("/plain", false, false)
	applySSHBrowserUpdate(t, updates)
	if len(a.pendingToasts) != 0 {
		t.Fatal("browsing emitted a non-repository notification")
	}
	a.openSelectedSSHWorkspace()
	applySSHBrowserUpdate(t, updates)
	if !a.sshWorkspaceOpen || len(a.pendingToasts) != 1 || a.pendingToasts[0].title != "This folder is not a Git repository" {
		t.Fatalf("Open did not explain missing Git repository: %+v", a.pendingToasts)
	}
}

func TestSSHBrowserDefaultPathIsHome(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.settings.SSHProfiles[0].DefaultRoot = ""
	a.selectSSHWorkspaceProfile("server")
	if a.sshWorkspaceRoot != "/home" {
		t.Fatalf("default selected path=%q", a.sshWorkspaceRoot)
	}
	a.sshBrowseFolder = func(_ context.Context, _ sshworkspace.Profile, root string, _ bool) (sshFolderListing, error) {
		if root != "/home" {
			t.Errorf("default browse path=%q", root)
		}
		return sshFolderListing{Root: root}, nil
	}
	a.loadSSHFolder("", false, false)
	applySSHBrowserUpdate(t, updates)
	if a.sshWorkspaceRoot != "/home" || a.sshBrowser.Listing.Root != "/home" {
		t.Fatal("default explorer path is not /home")
	}
}

func TestSSHBrowserStartsAtHomeRegardlessOfSavedPath(t *testing.T) {
	for _, saved := range []string{"", "~", "/root", "/custom/repository"} {
		t.Run(saved, func(t *testing.T) {
			a, _ := sshBrowserTestApp(t)
			a.settings.SSHProfiles[0].DefaultRoot = saved
			a.selectSSHWorkspaceProfile("server")
			if a.sshWorkspaceRoot != "/home" {
				t.Fatalf("saved path %q overrode /home: %q", saved, a.sshWorkspaceRoot)
			}
			a.sshWorkspaceRoot = "/root"
			a.closeSSHWorkspacePicker()
			a.openSSHWorkspacePicker()
			if a.sshWorkspaceRoot != "/home" {
				t.Fatalf("reopening retained previous path %q", a.sshWorkspaceRoot)
			}
		})
	}
}

func TestSSHBrowserOpeningNeverSelectsAProfile(t *testing.T) {
	a, updates := sshBrowserTestApp(t)
	a.opener = appSSHRepositoryOpener{}
	requests := make(chan struct{}, 1)
	a.sshBrowseFolder = func(context.Context, sshworkspace.Profile, string, bool) (sshFolderListing, error) {
		requests <- struct{}{}
		return sshFolderListing{Root: "/home"}, nil
	}
	a.openSSHWorkspacePicker()
	tester := ui.NewTester(a.view, 1280, 900)
	for i := 0; i < 3; i++ {
		tester.Frame()
	}
	if a.sshWorkspaceProfileID != "" || a.sshWorkspaceResolving || a.sshWorkspaceRoot != "/home" {
		t.Fatal("opening auto-selected a profile")
	}
	select {
	case <-updates:
		t.Fatal("opening scheduled a connection")
	default:
	}
	select {
	case <-requests:
		t.Fatal("opening read remote credentials or files")
	default:
	}
}
