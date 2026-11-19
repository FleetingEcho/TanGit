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

func workspaceRecoveryFixture(t *testing.T) (string, FullSession) {
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

func discardWorkspaceRecoveryFiles(t *testing.T, dir string, session interface {
	Session
	ChangesSession
}) OperationRecord {
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
	reopened, err := openFullRepositorySessionForTest((Opener{}).Open(context.Background(), dir))
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

func TestOperationStoresPostOperationWorkspaceSnapshot(t *testing.T) {
	_, session := workspaceRecoveryFixture(t)
	discardWorkspaceRecoveryFiles(t, "", session)
	snapshots, err := session.WorkSnapshots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 {
		record := onlyOperation(t, session)
		t.Fatalf("operation has %d durable workspace snapshots, want before and post-operation restore points; status=%s error=%s before=%s after=%s", len(snapshots), record.Status, record.Error, record.WorkspaceGuardBefore, record.WorkspaceGuardAfter)
	}
}

func TestOperationUndoRejectsRawContentHiddenByCleanFilters(t *testing.T) {
	dir, session := recoveryRepository(t)
	runGit(t, dir, "config", "filter.casefold.clean", "tr A-Z a-z")
	runGit(t, dir, "config", "filter.casefold.smudge", "tr a-z A-Z")
	if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("payload.txt filter=casefold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "payload.txt"), []byte("BASE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".gitattributes", "payload.txt")
	runGit(t, dir, "commit", "-m", "configured filter payload")
	if err := os.WriteFile(filepath.Join(dir, "payload.txt"), []byte("WIP\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var target File
	for _, file := range sessionSnapshotForTest(t, session).Files {
		if file.Path == "payload.txt" {
			target = file
		}
	}
	if target.Path == "" {
		t.Fatal("payload fixture is not present as a changed file")
	}
	if err := session.DiscardFiles(context.Background(), []File{target}); err != nil {
		t.Fatal(err)
	}
	record := onlyOperation(t, session)
	// Different raw bytes have the same clean representation, and refreshing
	// the index does not alter its semantic stage. These are still newer work.
	if err := os.WriteFile(filepath.Join(dir, "payload.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "--renormalize", "payload.txt")
	if err := session.UndoOperation(context.Background(), record.ID); err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("undo hid a raw-content change behind Git's clean filter: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "payload.txt")); string(got) != "base\n" {
		t.Fatalf("rejected undo overwrote newer filtered content: %q", got)
	}
}

func TestOperationUndoRejectsNewerOperationRefs(t *testing.T) {
	dir, session := workspaceRecoveryFixture(t)
	record := discardWorkspaceRecoveryFiles(t, dir, session)
	if err := os.WriteFile(filepath.Join(dir, "newer.txt"), []byte("new stash, not part of the earlier discard\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "stash", "push", "--include-untracked", "-m", "newer independent stash")
	if err := session.UndoOperation(context.Background(), record.ID); err == nil || !strings.Contains(err.Error(), "worktree changed") {
		t.Fatalf("undo could overwrite a newer operation ref: %v", err)
	}
	if got := gitOutput(t, dir, "stash", "list", "--format=%s"); !strings.Contains(got, "newer independent stash") {
		t.Fatalf("newer stash was overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "newer.txt")); !os.IsNotExist(err) {
		t.Fatalf("fixture no longer matches the operation's post-state: %v", err)
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
