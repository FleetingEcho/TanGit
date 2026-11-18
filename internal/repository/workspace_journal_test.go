package repository

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func workspaceRecoveryFixture(t *testing.T) (string, Session) {
	t.Helper()
	dir, session := recoveryRepository(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".env\nignored/\n.env.extra\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("token=before-discard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored", "data.bin"), []byte{0, 1, 0xff, 2, 0, 3}, 0o711); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join("ignored", "data.bin"), filepath.Join(dir, "ignored", "link")); err != nil {
			t.Fatal(err)
		}
	}
	return dir, session
}

func discardWorkspaceRecoveryFiles(t *testing.T, dir string, session Session) OperationRecord {
	t.Helper()
	var sessionFiles []File
	for _, state := range sessionSnapshotForTest(t, session).Files {
		if state.Path == ".env" || state.Path == ".gitignore" || state.Path == "ignored/data.bin" ||
			(runtime.GOOS != "windows" && state.Path == "ignored/link") {
			sessionFiles = append(sessionFiles, state)
		}
	}
	sessionFiles = append(sessionFiles,
		File{Path: ".env", Unstaged: true, Added: true},
		File{Path: "ignored/data.bin", Unstaged: true, Added: true},
	)
	if runtime.GOOS != "windows" {
		sessionFiles = append(sessionFiles, File{Path: "ignored/link", Unstaged: true, Added: true})
	}
	if err := session.DiscardFiles(context.Background(), sessionFiles); err != nil {
		t.Fatal(err)
	}
	return onlyOperation(t, session)
}

func sessionSnapshotForTest(t *testing.T, session Session) Snapshot {
	t.Helper()
	state, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestOperationWorkspaceUndoRestoresIgnoredPayloadAndIndex(t *testing.T) {
	dir, session := workspaceRecoveryFixture(t)
	if runtime.GOOS == "windows" {
		t.Skip("the regression includes symbolic links and POSIX executable permissions")
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.extra"), []byte("preserve-extra-ignore\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "a.txt"), 0o751); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("staged\nunstaged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := discardWorkspaceRecoveryFiles(t, dir, session)
	reopened, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.UndoOperation(context.Background(), record.ID); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil || string(content) != "token=before-discard\n" {
		t.Fatalf("ignored payload after restore = %q, %v", content, err)
	}
	info, err := os.Stat(filepath.Join(dir, "ignored", "data.bin"))
	if err != nil || info.Mode().Perm() != 0o711 {
		t.Fatalf("ignored permissions were not restored exactly: %v, %v", info, err)
	}
	binary, err := os.ReadFile(filepath.Join(dir, "ignored", "data.bin"))
	if err != nil || !bytes.Equal(binary, []byte{0, 1, 0xff, 2, 0, 3}) {
		t.Fatalf("ignored binary after restore = %v, %v", binary, err)
	}
	info, err = os.Stat(filepath.Join(dir, "a.txt"))
	if err != nil || info.Mode().Perm() != 0o751 {
		t.Fatalf("tracked permissions were not restored exactly: %v, %v", info, err)
	}
	target, err := os.Readlink(filepath.Join(dir, "ignored", "link"))
	if err != nil || target != filepath.Join("ignored", "data.bin") {
		t.Fatalf("restored symlink target = %q, %v", target, err)
	}
	if got := gitOutput(t, dir, "show", ":a.txt"); got != "staged\n" {
		t.Fatalf("restored index = %q, want staged", got)
	}
	if content, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(content) != "staged\nunstaged\n" {
		t.Fatalf("restored staged/unstaged split = %q", content)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, ".env.extra")); string(got) != "preserve-extra-ignore\n" {
		t.Fatalf("unselected ignored work was changed: %q", got)
	}
}

func TestOperationUndoRejectsChangedIgnoredPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the regression checks POSIX permission-bit compare-and-swap")
	}
	dir, session := workspaceRecoveryFixture(t)
	unselected := filepath.Join(dir, ".env.extra")
	if err := os.WriteFile(unselected, []byte("leave-this-newer-change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := discardWorkspaceRecoveryFiles(t, dir, session)
	if err := os.Chmod(unselected, 0o711); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(context.Background(), record.ID); err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("undo ignored a newer permission change: %v", err)
	}
	info, err := os.Stat(unselected)
	if err != nil || info.Mode().Perm() != 0o711 {
		t.Fatalf("rejected undo changed ignored permissions: %v, %v", info, err)
	}
}

func TestOperationUndoRejectsChangedIgnoredWorkWithoutDiscarding(t *testing.T) {
	dir, session := workspaceRecoveryFixture(t)
	record := discardWorkspaceRecoveryFiles(t, dir, session)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("newer-ignored-work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(context.Background(), record.ID); err == nil {
		t.Fatal("undo overwrote newer ignored work instead of rejecting the state mismatch")
	}
	content, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil || string(content) != "newer-ignored-work\n" {
		t.Fatalf("rejected undo changed newer ignored work: %q, %v", content, err)
	}
}
