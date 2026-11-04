package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mediaRepository(t *testing.T) (string, Session) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "Desktop Test")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	if err := os.WriteFile(filepath.Join(dir, "old.png"), []byte("old\x00image"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "old.png")
	runGit(t, dir, "commit", "-m", "base image")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, session
}

func TestMediaReadsWorkingTreeIndexAndAddDeleteSides(t *testing.T) {
	dir, session := mediaRepository(t)
	ctx := context.Background()
	newData := []byte("new\x00image")
	if err := os.WriteFile(filepath.Join(dir, "old.png"), newData, 0o600); err != nil {
		t.Fatal(err)
	}
	pair, err := session.Media(ctx, MediaRequest{Source: MediaWorkingTree, File: File{Path: "old.png", Tracked: true, Unstaged: true}})
	if err != nil {
		t.Fatal(err)
	}
	if string(pair.Old.Data) != "old\x00image" || string(pair.New.Data) != string(newData) {
		t.Fatalf("unstaged media = %#v", pair)
	}
	runGit(t, dir, "add", "old.png")
	pair, err = session.Media(ctx, MediaRequest{Source: MediaWorkingTree, File: File{Path: "old.png", Tracked: true, Staged: true}})
	if err != nil || string(pair.Old.Data) != "old\x00image" || string(pair.New.Data) != string(newData) {
		t.Fatalf("staged media = %#v, %v", pair, err)
	}
	if err := os.Remove(filepath.Join(dir, "old.png")); err != nil {
		t.Fatal(err)
	}
	pair, err = session.Media(ctx, MediaRequest{Source: MediaWorkingTree, File: File{Path: "old.png", Tracked: true, Unstaged: true, Deleted: true}})
	if err != nil || !pair.Old.Present || pair.New.Present {
		t.Fatalf("deleted media sides = %#v, %v", pair, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "untracked.png"), []byte("untracked"), 0o600); err != nil {
		t.Fatal(err)
	}
	pair, err = session.Media(ctx, MediaRequest{Source: MediaWorkingTree, File: File{Path: "untracked.png", Unstaged: true}})
	if err != nil || pair.Old.Present || string(pair.New.Data) != "untracked" {
		t.Fatalf("untracked media sides = %#v, %v", pair, err)
	}
}

func TestMediaReadsCommitRenameAndRootAddition(t *testing.T) {
	dir, session := mediaRepository(t)
	runGit(t, dir, "mv", "old.png", "renamed.png")
	runGit(t, dir, "commit", "-m", "rename image")
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	parent := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD^"))
	pair, err := session.Media(context.Background(), MediaRequest{
		Source: MediaCommit, Commit: Commit{Hash: head, Parents: []string{parent}},
		CommitFile: CommitFile{Path: "renamed.png", PreviousPath: "old.png", Status: "R"},
	})
	if err != nil || string(pair.Old.Data) != "old\x00image" || string(pair.New.Data) != "old\x00image" {
		t.Fatalf("renamed commit media = %#v, %v", pair, err)
	}
	root := strings.TrimSpace(gitOutput(t, dir, "rev-list", "--max-parents=0", "HEAD"))
	pair, err = session.Media(context.Background(), MediaRequest{
		Source: MediaCommit, Commit: Commit{Hash: root}, CommitFile: CommitFile{Path: "old.png", Status: "A"},
	})
	if err != nil || pair.Old.Present || string(pair.New.Data) != "old\x00image" {
		t.Fatalf("root addition media = %#v, %v", pair, err)
	}
}

func TestMediaReadsTrackedAndUntrackedStashVersions(t *testing.T) {
	dir, session := mediaRepository(t)
	if err := os.WriteFile(filepath.Join(dir, "old.png"), []byte("stash tracked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.png"), []byte("stash untracked"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "stash", "push", "--include-untracked", "-m", "media")
	for _, test := range []struct {
		file CommitFile
		old  string
		new  string
	}{
		{CommitFile{Path: "old.png", Status: "M"}, "old\x00image", "stash tracked"},
		{CommitFile{Path: "new.png", Status: "A"}, "", "stash untracked"},
	} {
		pair, err := session.Media(context.Background(), MediaRequest{Source: MediaStash, StashIndex: 0, CommitFile: test.file})
		if err != nil || string(pair.Old.Data) != test.old || string(pair.New.Data) != test.new {
			t.Errorf("stash %s media = %#v, %v", test.file.Path, pair, err)
		}
	}
}

func TestMediaResolvesOnlyVerifiedLocalLFSObjects(t *testing.T) {
	dir, session := mediaRepository(t)
	content := []byte("local LFS image bytes")
	digest := sha256.Sum256(content)
	oid := fmt.Sprintf("%x", digest)
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, len(content))
	if err := os.WriteFile(filepath.Join(dir, "lfs.png"), []byte(pointer), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "lfs.png")
	runGit(t, dir, "commit", "-m", "LFS pointer")
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	parent := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD^"))
	request := MediaRequest{Source: MediaCommit, Commit: Commit{Hash: head, Parents: []string{parent}},
		CommitFile: CommitFile{Path: "lfs.png", Status: "A"}}
	pair, err := session.Media(context.Background(), request)
	if err != nil || pair.New.Unavailable != "Git LFS object is not available locally" || len(pair.New.Data) != 0 {
		t.Fatalf("missing local LFS state = %#v, %v", pair.New, err)
	}
	object := filepath.Join(dir, ".git", "lfs", "objects", oid[:2], oid[2:4], oid)
	if err := os.MkdirAll(filepath.Dir(object), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(object, content, 0o600); err != nil {
		t.Fatal(err)
	}
	pair, err = session.Media(context.Background(), request)
	if err != nil || string(pair.New.Data) != string(content) || pair.New.Unavailable != "" {
		t.Fatalf("verified local LFS media = %#v, %v", pair.New, err)
	}
	if err := os.WriteFile(object, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	pair, err = session.Media(context.Background(), request)
	if err != nil || pair.New.Unavailable != "Local Git LFS object has the wrong size" {
		t.Fatalf("corrupt local LFS state = %#v, %v", pair.New, err)
	}
}
