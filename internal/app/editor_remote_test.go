package app

import (
	"os"
	"testing"

	"lazygitapp/internal/workspace"
)

func TestRemoteGitFileOpensBuiltInWithoutLocalLstat(t *testing.T) {
	h := menuTestApp(t)
	const root = "/srv/remote/repository"
	h.app.snapshot.Root = root
	h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "server", Root: root}
	opened := stubEditor(t, nil)

	originalLstat := editorFileLstat
	editorFileLstat = func(name string) (os.FileInfo, error) {
		t.Fatalf("remote built-in open called local Lstat for %q", name)
		return nil, nil
	}
	t.Cleanup(func() { editorFileLstat = originalLstat })

	h.app.openGitFileInBuiltInEditor(root, "internal/app/main.go")

	if h.app.workspaceTab != sectionCode || h.app.code.active != "internal/app/main.go" {
		t.Fatalf("remote file did not open in Code: tab=%d active=%q toasts=%+v",
			h.app.workspaceTab, h.app.code.active, h.app.pendingToasts)
	}
	document := h.app.code.documents["internal/app/main.go"]
	if document == nil || document.diskPath != "" || document.readOnly {
		t.Fatalf("remote document = %#v, want a repository-relative editable document", document)
	}
	if len(*opened) != 0 {
		t.Fatalf("built-in remote open launched an external editor: %+v", *opened)
	}
}

func TestRemoteExternalEditorCommandsNeverLaunchLocalProcesses(t *testing.T) {
	h := menuTestApp(t)
	const root = "/srv/remote/repository"
	h.app.snapshot.Root = root
	h.app.location = workspace.Location{Kind: workspace.SSH, ProfileID: "server", Root: root}
	opened := stubEditor(t, nil)

	for _, command := range []string{vsCodeEditorCommand, editorPresets[2].Command, "custom {{filename}}"} {
		h.app.launchEditorCommand(command, root+"/internal/app/main.go", 0)
	}
	if len(*opened) != 0 {
		t.Fatalf("remote external editor actions launched local commands: %+v", *opened)
	}
}
