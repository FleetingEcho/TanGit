package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestAddSubmoduleFormStagesRealGitSubmodule(t *testing.T) {
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	commit := func(dir, message string) {
		t.Helper()
		runGitIn(t, dir, "-c", "user.name=Submodule Test", "-c", "user.email=submodule@example.com",
			"-c", "commit.gpgSign=false", "commit", "--allow-empty", "-m", message)
	}
	library := t.TempDir()
	runGitIn(t, library, "init", "--initial-branch=main")
	const contents = "library contents\n"
	if err := os.WriteFile(filepath.Join(library, "library.txt"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, library, "add", "library.txt")
	commit(library, "library")
	head := strings.TrimSpace(runGitIn(t, library, "rev-parse", "HEAD"))

	root := t.TempDir()
	runGitIn(t, root, "init", "--initial-branch=main")
	commit(root, "superproject")
	a := newApp(repository.Opener{})
	a.pathUserData = t.TempDir()
	a.path = root
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	t.Cleanup(a.closeRepository)
	openFixture(t, h.tester)
	h.waitFor(t, "the superproject", func() bool {
		return a.state == stateReady && !a.repositoryDetailsLoading
	})
	click := func(label string) {
		t.Helper()
		if err := h.tester.Click(label); err != nil {
			t.Fatalf("click %q: %v", label, err)
		}
		h.tester.Frame()
	}
	checkSubmit := func(disabled bool) {
		t.Helper()
		button, found := h.tester.AccessByRole(ui.RoleButton, "Add submodule")
		if !found || button.Disabled != disabled {
			t.Fatalf("Add submodule button found=%t disabled=%t, want disabled=%t", found, button.Disabled, disabled)
		}
	}
	click("Submodules")
	click("Add submodule…")
	checkSubmit(true)
	const name, path = "shared-library", "vendor/library"
	url := filepath.ToSlash(library)
	for _, field := range []struct{ label, value string }{
		{"Submodule name", name},
		{"Checkout path", path},
		{"Repository URL", url},
	} {
		click(field.label)
		h.tester.Type(field.value)
		h.tester.Frame()
		checkSubmit(field.label != "Repository URL")
	}
	click("Add submodule")
	h.waitFor(t, "the add submodule action and details refresh", func() bool {
		return !a.actionBusy && !a.repositoryDetailsLoading
	})
	if a.actionError != "" || a.showSubmoduleForm {
		t.Fatalf("adding the submodule left the form open=%t with error %q", a.showSubmoduleForm, a.actionError)
	}
	want := repository.Submodule{Name: name, Path: path, URL: url}
	if got := a.snapshot.Submodules; len(got) != 1 || got[0] != want {
		t.Fatalf("submodules = %#v, want %#v", got, want)
	}
	if a.selectedSubmodule != 0 || !h.tester.HasText(url) {
		t.Fatal("the new submodule was not displayed in the details pane")
	}
	for _, setting := range []struct{ key, value string }{{"path", path}, {"url", url}} {
		key := "submodule." + name + "." + setting.key
		if got := strings.TrimSpace(runGitIn(t, root, "config", "--blob=:.gitmodules", "--get", key)); got != setting.value {
			t.Fatalf("staged %s = %q, want %q", key, got, setting.value)
		}
	}
	if got := strings.TrimSpace(runGitIn(t, root, "ls-files", "--stage", "--", path)); got != fmt.Sprintf("160000 %s 0\t%s", head, path) {
		t.Fatalf("staged gitlink at %s = %q, want commit %s", path, got, head)
	}
	if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path), "library.txt")); err != nil || string(data) != contents {
		t.Fatalf("checked-out submodule file = %q, error %v", data, err)
	}
}
