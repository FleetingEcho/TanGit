package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
	"lazygitapp/internal/sshworkspace"
)

func TestParseSessionGitmodules(t *testing.T) {
	parent := &models.SubmoduleConfig{Name: "parent", Path: "vendor/parent"}
	configs, err := parseSessionGitmodules([]byte("[submodule \"one\"]\n path = nested/one\n url = https://example.com/one.git\n[submodule \"two\"]\n path=two\nurl=git@example.com:two.git\n"), parent)
	if err != nil || len(configs) != 2 {
		t.Fatalf("configs = %#v, %v", configs, err)
	}
	if configs[0].Name != "one" || configs[0].Path != "nested/one" || configs[0].Url != "https://example.com/one.git" || configs[0].ParentModule != parent {
		t.Fatalf("first config = %#v", configs[0])
	}
	if configs[1].FullName() != "parent/two" || configs[1].FullPath() != "vendor/parent/two" {
		t.Fatalf("second config = %#v", configs[1])
	}
}

func TestSuspiciousSessionSubmoduleName(t *testing.T) {
	for _, name := range []string{"", "../escape", "safe/../../escape", `safe\..\escape`} {
		if !suspiciousSessionSubmoduleName(name) {
			t.Errorf("accepted name %q", name)
		}
	}
	for _, name := range []string{"ordinary", "nested/name", ".." + "safe"} {
		if suspiciousSessionSubmoduleName(name) {
			t.Errorf("rejected name %q", name)
		}
	}
}

func TestRemoteSubmodulePathRejectsTraversalAndSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "actual"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "actual"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	session := &gitSession{sshClient: &sshworkspace.Client{}, files: localSessionFiles{}}
	for _, value := range []string{".", "../escape", "/absolute", "link", "file/child"} {
		if value == "file/child" {
			if err := os.WriteFile(filepath.Join(root, "file"), []byte("file"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if !session.suspiciousRemoteSubmodulePath(root, value) {
			t.Errorf("accepted path %q", value)
		}
	}
	if session.suspiciousRemoteSubmodulePath(root, "actual/child") {
		t.Fatal("rejected a path below a real directory")
	}
	if session.suspiciousRemoteSubmodulePath(root, "not-checked-out/child") {
		t.Fatal("rejected a path which does not exist yet")
	}
}

func TestParseSessionWorktrees(t *testing.T) {
	worktrees := parseSessionWorktrees("worktree /repo/main\nHEAD 111111\nbranch refs/heads/main\n\nworktree /repo/linked topic\nHEAD 222222\ndetached\n\n")
	if len(worktrees) != 2 {
		t.Fatalf("worktrees = %#v", worktrees)
	}
	if worktrees[0].Path != "/repo/main" || worktrees[0].Head != "111111" || worktrees[0].Branch != "main" {
		t.Fatalf("main = %#v", worktrees[0])
	}
	if worktrees[1].Path != "/repo/linked topic" || worktrees[1].Head != "222222" || worktrees[1].Branch != "" {
		t.Fatalf("linked = %#v", worktrees[1])
	}
	named := []*models.Worktree{{Path: "/one/shared"}, {Path: "/two/shared"}, {Path: "/other"}}
	uniqueSessionWorktreeNames(named)
	if named[0].Name != "shared" || named[1].Name != "shared-2" || named[2].Name != "other" {
		t.Fatalf("unique names = %q, %q, %q", named[0].Name, named[1].Name, named[2].Name)
	}

	metadata := parseWorktreeMetadata("worktree /repo/main\nHEAD 111111\nbranch refs/heads/main\n\nworktree /repo/locked\nHEAD 222222\nlocked External drive\n\nworktree /repo/gone\nHEAD 333333\nprunable gitdir file points to non-existent location\n\n")
	if got := metadata["/repo/locked"]; !got.Locked || got.LockReason != "External drive" {
		t.Fatalf("locked metadata = %#v", got)
	}
	if got := metadata["/repo/gone"]; !got.Prunable || got.PruneReason != "gitdir file points to non-existent location" {
		t.Fatalf("prunable metadata = %#v", got)
	}
	if got := metadata["/repo/main"]; got != (worktreeMetadata{}) {
		t.Fatalf("ordinary metadata = %#v", got)
	}
}
