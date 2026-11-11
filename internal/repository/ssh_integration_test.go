package repository

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/sshintegration"
	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"

	"github.com/stefanhaller/git-todo-parser/todo"
)

func TestOpenSSHWorkspaceRealOpenSSH(t *testing.T) {
	server := sshintegration.Start(t)
	connector, err := sshworkspace.NewConnector(server.SecretStore, filepath.Join(t.TempDir(), "known_hosts"), func(sshworkspace.HostKeyInfo) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client, err := connector.Connect(ctx, server.Profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "TanGit Test")
	runGit(t, dir, "config", "user.email", "tangit@example.com")
	binary := []byte{0, 0xff, 0xfe, 'P', 'N', 'G', 0, 1, 2, '\n'}
	if err := os.WriteFile(filepath.Join(dir, "image.bin"), binary, 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "image.bin")
	runGit(t, dir, "commit", "-m", "root")
	for _, name := range []string{"middle", "head"} {
		if err := os.WriteFile(filepath.Join(dir, "history.txt"), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "history.txt")
		runGit(t, dir, "commit", "-m", name)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.bin"), []byte{0, 1, 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}

	// A GUI launched from another checkout must never send its repository
	// location through the SSH environment.
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "not-a-repository"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	session, err := OpenSSHWorkspace(ctx, client, workspace.Location{
		Kind: workspace.SSH, ProfileID: server.Profile.ID, Root: dir,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.(interface{ Close() error }).Close()
	if session.(interface{ SSHClient() *sshworkspace.Client }).SSHClient() != client {
		t.Fatal("session did not retain its SSH client")
	}
	if got := session.(interface{ Workspace() workspace.Location }).Workspace(); got.ProfileID != server.Profile.ID || got.Root != dir {
		t.Fatalf("Workspace() = %#v", got)
	}

	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Branch != "main" || len(snapshot.Commits) != 3 {
		t.Fatalf("snapshot = branch %q, commits %d", snapshot.Branch, len(snapshot.Commits))
	}
	var untracked File
	for _, file := range snapshot.Files {
		if file.Path == "untracked.bin" {
			untracked = file
		}
	}
	patch, err := session.WorkingTreeFilePatch(ctx, untracked)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "GIT binary patch") || !strings.Contains(patch, "untracked.bin") {
		t.Fatalf("untracked binary patch = %q", patch)
	}

	// Exercise the SSH session's byte stream directly with invalid UTF-8.
	blob, err := session.(*gitSession).gitBlobMedia("HEAD:image.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(blob.Data, binary) {
		t.Fatalf("binary blob = %v, want %v", blob.Data, binary)
	}
	var middle Commit
	for _, commit := range snapshot.Commits {
		if commit.Subject == "middle" {
			middle = commit
		}
	}
	if middle.Hash == "" {
		t.Fatal("middle commit not found")
	}
	if err := session.RewordCommit(ctx, middle, "middle reworded", "remote body"); err != nil {
		t.Fatal(err)
	}
	history, err := session.RecentCommits(ctx, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 || history[1].Subject != "middle reworded" {
		t.Fatalf("history after remote reword = %#v", history)
	}

	concrete := session.(*gitSession)
	if err := concrete.runRemoteRebase("HEAD~2", remoteRebaseOptions{
		transform: func(todos []todo.Todo) ([]todo.Todo, error) {
			for i := range todos {
				if todos[i].Commit != "" {
					todos[i].Command = todo.Edit
					break
				}
			}
			return todos, nil
		},
	}); err != nil {
		stdout, stderr, _ := concrete.cmd.New([]string{"git", "status", "--short", "--branch"}).RunWithOutputs()
		t.Fatalf("remote edit rebase: %v; state: %#v; status: %q / %q", err, concrete.workingTreeState(), stdout, stderr)
	}
	if !concrete.workingTreeState().Rebasing {
		t.Fatal("edit stop did not leave a rebase in progress")
	}
	withTodo, err := concrete.recentCommits(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(withTodo) == 0 || !withTodo[0].IsTODO() || withTodo[0].Name != "head" {
		t.Fatalf("remote pending rebase history = %#v", withTodo)
	}
	if err := concrete.finishGitOperation("rebase", "abort"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSSHWorkspaceRecoveryRealOpenSSH(t *testing.T) {
	server := sshintegration.Start(t)
	connector, err := sshworkspace.NewConnector(server.SecretStore, filepath.Join(t.TempDir(), "known_hosts"), func(sshworkspace.HostKeyInfo) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client, err := connector.Connect(ctx, server.Profile)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "TanGit Test")
	runGit(t, dir, "config", "user.email", "tangit@example.com")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "root")
	if err := os.WriteFile(filepath.Join(dir, "untracked.bin"), []byte{0, 1, 0xff}, 0o600); err != nil {
		t.Fatal(err)
	}

	session, err := OpenSSHWorkspace(ctx, client, workspace.Location{
		Kind: workspace.SSH, ProfileID: server.Profile.ID, Root: dir,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var untracked File
	for _, file := range snapshot.Files {
		if file.Path == "untracked.bin" {
			untracked = file
		}
	}
	if untracked.Path == "" {
		t.Fatal("untracked recovery fixture was not loaded")
	}
	if err := session.Discard(ctx, untracked); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Stat(filepath.Join(dir, "untracked.bin")); !os.IsNotExist(err) {
		t.Fatalf("remote untracked file still exists, Stat error = %v", err)
	}
	snapshots, err := session.WorkSnapshots(ctx)
	if err != nil || len(snapshots) == 0 {
		t.Fatalf("WorkSnapshots() = %#v, %v", snapshots, err)
	}
	if err := session.RestoreWorkSnapshot(ctx, snapshots[0]); err != nil {
		t.Fatal(err)
	}
	restored, err := client.ReadFile(filepath.Join(dir, "untracked.bin"))
	if err != nil || !bytes.Equal(restored, []byte{0, 1, 0xff}) {
		t.Fatalf("restored binary = %v, %v", restored, err)
	}

	exclude := filepath.Join(dir, ".git", "info", "exclude")
	if err := client.WriteFileAtomic(exclude, []byte("existing-without-newline"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.ExcludeFile(ctx, untracked); err != nil {
		t.Fatal(err)
	}
	excludeContent, err := client.ReadFile(exclude)
	if err != nil || string(excludeContent) != "existing-without-newline\n/untracked.bin\n" {
		t.Fatalf("remote exclude = %q, %v", excludeContent, err)
	}

	// Exercise the remote status replacement's conflict and attribute reads.
	runGit(t, dir, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("tracked.txt conflict-marker-size=12\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt", ".gitattributes")
	runGit(t, dir, "commit", "-m", "feature")
	runGit(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "main")
	mergeOutput, mergeErr := exec.Command("git", "-C", dir, "merge", "feature").CombinedOutput()
	if mergeErr == nil {
		t.Fatalf("conflicting merge unexpectedly succeeded: %s", mergeOutput)
	}
	conflictSnapshot, err := session.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundConflict := false
	for _, file := range conflictSnapshot.Files {
		if file.Path == "tracked.txt" && file.HasMergeConflicts && file.HasInlineMergeConflicts && file.ConflictMarkerSize == 12 {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Fatalf("remote conflict status = %#v", conflictSnapshot.Files)
	}
}
