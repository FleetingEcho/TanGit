package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// launchedEditor is one run of the editor command seam.
type launchedEditor struct {
	command string
	path    string
	line    int
}

// stubEditor replaces the editor launcher for one test and remembers what it
// was asked to open. It puts the real launcher back afterwards.
func stubEditor(t *testing.T, run func(command, path string, line int) error) *[]launchedEditor {
	t.Helper()
	var opened []launchedEditor
	restore := runEditorTemplate
	runEditorTemplate = func(command, path string, line int) error {
		opened = append(opened, launchedEditor{command: command, path: path, line: line})
		if run != nil {
			return run(command, path, line)
		}
		return nil
	}
	t.Cleanup(func() { runEditorTemplate = restore })
	return &opened
}

// The top bar's editor button opens the repository itself, not a file in it,
// so one click launches the configured editor without opening a chooser or
// changing the saved editor command.
func TestEditorButtonOpensTheRepositoryWithTheChosenEditor(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	opened := stubEditor(t, nil)
	h.app.settings.EditorCommand = editorPresets[2].Command

	if err := h.tester.Click("Open in editor"); err != nil {
		t.Fatalf("the header has no editor button: %v", err)
	}
	h.tester.Frame()
	for _, unwanted := range []string{"VS Code", "Zed", "Neovim", "Custom command…"} {
		if slices.Contains(h.texts(), unwanted) {
			t.Fatalf("clicking the launch action opened an editor chooser: %q", h.texts())
		}
	}
	h.settle(t)

	if !slices.Equal(*opened, []launchedEditor{{command: "zed {{filename}}", path: "/work/project"}}) {
		t.Fatalf("opened %+v, want the repository root in Zed", *opened)
	}
	if got := h.app.settings.EditorCommand; got != "zed {{filename}}" {
		t.Fatalf("launching changed the setting to %q, want Zed's command", got)
	}
	button, _ := h.tester.Find("Open in editor")
	h.tester.Move(0, 0)
	h.tester.Frame()
	h.tester.Move(button.X+button.W/2, button.Y+button.H/2)
	h.tester.Advance(time.Second)
	if !h.tester.HasText("Open this repository in your editor\nConfigure your editor in Settings → Editor.") {
		t.Fatal("the editor tooltip does not explain where to configure the editor")
	}
}

func TestEditorButtonOpensBuiltInCodeByDefault(t *testing.T) {
	h := menuTestApp(t)
	stubCodeWorkspace(t, nil)
	opened := stubEditor(t, nil)
	if got := h.app.settings.EditorCommand; got != builtInEditorCommand {
		t.Fatalf("fresh editor setting = %q, want Built-in", got)
	}
	if err := h.tester.Click("Open in editor"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.workspaceTab != sectionCode {
		t.Fatalf("the header editor button opened section %d, want Code", h.app.workspaceTab)
	}
	if len(*opened) != 0 {
		t.Fatalf("Built-in launched an external editor: %+v", *opened)
	}
}

// A Home card can open its repository in the configured editor without
// opening that repository in TanGit; its context menu offers the same action.
func TestRecentCardOpensTheRepositoryWithTheConfiguredEditor(t *testing.T) {
	a, tt, _ := recentTester(t, recentRepository{Path: "/work/project", LastOpened: timeNow()})
	opened := stubEditor(t, nil)
	a.settings.EditorCommand = editorPresets[2].Command
	want := launchedEditor{command: editorPresets[2].Command, path: "/work/project"}

	if err := tt.Click("Open /work/project in editor"); err != nil {
		t.Fatalf("the recent card has no editor button: %v", err)
	}
	if !slices.Equal(*opened, []launchedEditor{want}) {
		t.Fatalf("card editor opened %+v, want %+v", *opened, want)
	}
	if a.state != stateEmpty {
		t.Fatalf("opening the editor must not open the TanGit repository; state=%v", a.state)
	}

	tt.Frame()
	if err := tt.RightClick("/work/project"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Open in editor"); err != nil {
		t.Fatalf("the card menu has no editor command: %v", err)
	}
	if !slices.Equal(*opened, []launchedEditor{want, want}) {
		t.Fatalf("menu editor opened %+v, want a second launch of %+v", *opened, want)
	}
}

func TestRecentCardOpensItsRepositoryInBuiltInCode(t *testing.T) {
	stubCodeWorkspace(t, nil)
	a, tt, updated := recentTester(t, recentRepository{Path: "/work/project", LastOpened: timeNow()})
	a.workspaceTab = sectionChanges
	opened := stubEditor(t, nil)
	if err := tt.Click("Open /work/project in editor"); err != nil {
		t.Fatalf("the recent card has no editor button: %v", err)
	}
	h := &harness{app: a, tester: tt, updates: updated}
	h.settle(t)
	if a.state != stateReady || a.workspaceTab != sectionCode {
		t.Fatalf("Built-in opened state=%v section=%d, want the repository's Code page", a.state, a.workspaceTab)
	}
	if len(*opened) != 0 {
		t.Fatalf("Built-in launched an external editor: %+v", *opened)
	}
}

func TestRecentCardEditorSitsAtTheTopRight(t *testing.T) {
	_, tt, _ := recentTester(t, recentRepository{Path: "/work/project", LastOpened: timeNow()})
	editor, ok := tt.Find("Open /work/project in editor")
	if !ok {
		t.Fatal("the recent card has no editor shortcut")
	}
	name, ok := tt.Find("project")
	if !ok {
		t.Fatal("the recent card has no repository name")
	}
	rename, ok := tt.Find("Rename /work/project")
	if !ok {
		t.Fatal("the recent card has no rename action")
	}
	if editor.X <= name.X+name.W || editor.Y > name.Y+name.H || editor.Y >= rename.Y {
		t.Fatalf("editor bounds %+v are not at the top-right of name %+v and footer action %+v",
			editor, name, rename)
	}
	if editor.W < 28 || editor.H < 28 {
		t.Fatalf("the editor target is only %.0f×%.0f DIP", editor.W, editor.H)
	}
}

func TestFilePaneEditorButtonOpensAndRevealsTheFileInBuiltInCode(t *testing.T) {
	files := unstagedTrackedFile()
	files[0].Path = "internal/app/changed.txt"
	stubCodeWorkspace(t, map[string]string{"internal/app/changed.txt": "changed\n"})
	h, _ := changesTestApp(t, files)
	opened := stubEditor(t, nil)
	if _, found := h.tester.Find("Edit"); found {
		t.Fatal("the file header still has the redundant pencil edit button")
	}

	if err := h.tester.Click("Open file in editor"); err != nil {
		t.Fatalf("the file pane has no editor button: %v", err)
	}
	h.tester.Frame()
	document := waitForCodeDocument(t, h, "internal/app/changed.txt")

	if len(*opened) != 0 {
		t.Fatalf("Built-in launched an external editor: %+v", *opened)
	}
	if h.app.workspaceTab != sectionCode || document != h.app.activeCodeDocument() {
		t.Fatalf("the action did not open the file in Code: tab=%d active=%q", h.app.workspaceTab, h.app.code.active)
	}
	if got := h.app.selectedCodePath(); got != "internal/app/changed.txt" {
		t.Fatalf("the explorer selected %q, want the opened file", got)
	}
	if h.app.fileTreeCollapsed[fileTreeCollapseKey(codeFileTreeScope, "internal/app")] {
		t.Fatal("the opened file's parent directory stayed collapsed")
	}
}

// The file pane keeps the overflow menu last, after the controls that change
// only what the pane shows of the file.
func TestFileActionsMenuComesLast(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.tester.Frame()

	more, ok := h.tester.Find("File actions")
	if !ok {
		t.Fatalf("the file pane has no overflow menu: %q", h.texts())
	}
	expand, ok := h.tester.Find("Expand diff")
	if !ok {
		t.Fatalf("the file pane has no expand control: %q", h.texts())
	}
	if more.X <= expand.X {
		t.Fatalf("the overflow menu sits at %.0f, before Expand diff at %.0f", more.X, expand.X)
	}
}

// A machine without the editor's command gets an explanation and the one thing
// to change, not the shell's own wording.
func TestMissingEditorSaysHowToInstallIt(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.app.settings.EditorCommand = vsCodeEditorCommand
	stubEditor(t, func(string, string, int) error { return errEditorMissing })

	if err := h.tester.Click("Open in editor"); err != nil {
		t.Fatalf("the header has no editor button: %v", err)
	}
	h.tester.Frame()
	h.settle(t)

	texts := h.texts()
	if !slices.Contains(texts, "Could not open the editor") {
		t.Fatalf("the failure showed no notification: %q", texts)
	}
	hint := trf("%s was not found on the PATH. Install it, or choose another editor command in Settings.", "code")
	if !slices.Contains(texts, hint) {
		t.Fatalf("the notification does not say how to install the editor: %q", texts)
	}
}

// A Finder-launched macOS app does not inherit every PATH entry a terminal's
// shell added. An installed VS Code app still provides its documented CLI, so
// the launcher falls back to that executable without changing the arguments.
func TestEditorFindsVSCodeOutsideTheMacOSGUIPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	wasGOOS := editorGOOS
	editorGOOS = "darwin"
	t.Cleanup(func() { editorGOOS = wasGOOS })

	executable := filepath.Join(home, "Applications", "Visual Studio Code.app",
		"Contents", "Resources", "app", "bin", "code")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TANGIT_EDITOR_TEST_ARGS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	argumentsFile := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("TANGIT_EDITOR_TEST_ARGS", argumentsFile)

	if err := runEditorTemplate(vsCodeEditorCommand, "/work/project/one.txt", 0); err != nil {
		t.Fatalf("an installed VS Code app did not open when its command was outside the GUI PATH: %v", err)
	}
	for range 100 {
		arguments, err := os.ReadFile(argumentsFile)
		if err == nil {
			if got, want := string(arguments), "--wait\n/work/project/one.txt\n"; got != want {
				t.Fatalf("VS Code received %q, want %q", got, want)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the VS Code fallback did not pass on the file arguments")
}

// The editor command remains reachable through the Editor settings page.
func TestEditorConfigurationLivesInSettings(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.app.settings.EditorCommand = vsCodeEditorCommand
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Editor settings"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if h.app.settingsPage != settingsPageEditor || !h.tester.HasText("Editor command") {
		t.Fatal("the Editor settings page does not show its command")
	}
}

// The settings' presets fill in the command they stand for, and the settings
// store one plain command string.
func TestSettingsPresetsFillTheEditorCommand(t *testing.T) {
	h, _ := changesTestApp(t, unstagedTrackedFile())
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Editor settings"); err != nil {
		t.Fatalf("open Editor settings: %v", err)
	}
	h.tester.Frame()

	if err := h.tester.Click("Neovim"); err != nil {
		t.Fatalf("the settings offer no Neovim preset: %v", err)
	}
	h.tester.Frame()
	if got, want := h.app.settings.EditorCommand, editorPresets[3].Command; got != want {
		t.Fatalf("the preset stored %q, want %q", got, want)
	}
}

// The editor button is only in the workspace header: the picker has no
// repository to open yet.
func TestEditorButtonIsHiddenWithoutARepository(t *testing.T) {
	h := menuTestApp(t)
	h.app.closeRepository()
	h.tester.Frame()
	if _, ok := h.tester.Find("Open in editor"); ok {
		t.Fatal("the picker offers an editor button")
	}
}

// An editor command fills the placeholders with the file and line it is
// given, keeps a quoted run together, and appends a file the command does not
// name.
func TestEditorCommandFillsPlaceholders(t *testing.T) {
	tests := []struct {
		name    string
		command string
		path    string
		line    int
		want    []string
	}{
		{
			name:    "the default command",
			command: "code --wait {{filename}}",
			path:    "/repo/file.txt",
			want:    []string{"code", "--wait", "/repo/file.txt"},
		},
		{
			name:    "a path with spaces stays one argument",
			command: "code --wait {{filename}}",
			path:    "/repo/a file.txt",
			want:    []string{"code", "--wait", "/repo/a file.txt"},
		},
		{
			name:    "a line fused with the file",
			command: "code --wait --goto {{filename}}:{{line}}",
			path:    "/repo/file.txt",
			line:    42,
			want:    []string{"code", "--wait", "--goto", "/repo/file.txt:42"},
		},
		{
			name:    "an unknown line is the first",
			command: "nvim +{{line}} {{filename}}",
			path:    "/repo/file.txt",
			want:    []string{"nvim", "+1", "/repo/file.txt"},
		},
		{
			name:    "a quoted argument",
			command: `sh -c "echo hello" {{filename}}`,
			path:    "/repo/file.txt",
			want:    []string{"sh", "-c", "echo hello", "/repo/file.txt"},
		},
		{
			name:    "a command that names no file gets it appended",
			command: "code --wait",
			path:    "/repo/file.txt",
			want:    []string{"code", "--wait", "/repo/file.txt"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := editorCommandArgs(tc.command, tc.path, tc.line)
			if err != nil {
				t.Fatalf("editorCommandArgs: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("command %q gave %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

// A command whose quote never closes is reported rather than run split wrong.
func TestEditorCommandRejectsAnUnclosedQuote(t *testing.T) {
	if _, err := editorCommandArgs(`code "{{filename}}`, "/repo/file.txt", 0); !errors.Is(err, errEditorQuote) {
		t.Fatalf("unclosed quote gave %v, want errEditorQuote", err)
	}
	if _, err := editorCommandArgs("   ", "/repo/file.txt", 0); !errors.Is(err, errEditorCommandEmpty) {
		t.Fatalf("an empty command gave %v, want errEditorCommandEmpty", err)
	}
}
