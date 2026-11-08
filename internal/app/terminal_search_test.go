package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

func settleTerminalSearch(t *testing.T, h *harness, index int, want int) {
	t.Helper()
	for range 12 {
		h.tester.Frame()
		h.settle(t)
		if h.app.term.sessions[index].pane.SearchStatus().Total == want {
			return
		}
	}
	t.Fatalf("terminal %d search status = %+v, want %d matches; text=%q open=%v texts=%q", index+1,
		h.app.term.sessions[index].pane.SearchStatus(), want, h.app.term.sessions[index].searchText,
		h.app.term.sessions[index].searchOpen, h.texts())
}

func TestTerminalSearchIsIndependentAndKeyboardDriven(t *testing.T) {
	h, peers, first := monitoredTerminalTestApp(t)
	second := splitMonitoredTerminal(t, h, peers, false)
	if _, err := first.Write([]byte("first needle\r\nfirst needle\r\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Write([]byte("second-only\r\n")); err != nil {
		t.Fatal(err)
	}

	if err := h.tester.Click("Focus terminal 1"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Key(ui.Ctrl, ui.KeyF)
	h.tester.Frame()
	if !h.app.term.sessions[0].searchOpen || h.app.term.sessions[1].searchOpen {
		t.Fatalf("Ctrl+F did not open only the active terminal's search: %+v", h.app.term.sessions)
	}
	h.tester.Type("needle")
	settleTerminalSearch(t, h, 0, 2)
	if got := h.app.term.sessions[0].searchText; got != "needle" {
		t.Fatalf("first search text = %q", got)
	}

	h.tester.Key(0, ui.KeyF3)
	settleTerminalSearch(t, h, 0, 2)
	if got := h.app.term.sessions[0].pane.SearchStatus().Selected; got != 2 {
		t.Fatalf("F3 selected match %d, want the older second match", got)
	}
	h.tester.Key(ui.Shift, ui.KeyF3)
	h.tester.Frame()
	if got := h.app.term.sessions[0].pane.SearchStatus().Selected; got != 1 {
		t.Fatalf("Shift+F3 selected match %d, want the first match", got)
	}

	if err := h.tester.Click("Focus terminal 2"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.tester.Key(ui.Ctrl, ui.KeyF)
	h.tester.Frame()
	h.tester.Type("second")
	settleTerminalSearch(t, h, 1, 1)
	if got := h.app.term.sessions[0].pane.SearchStatus(); got.Text != "needle" || got.Total != 2 {
		t.Fatalf("searching the second terminal disturbed the first: %+v", got)
	}

	// Escape closes only the active find bar and clears only its highlights.
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.term.sessions[1].searchOpen || h.app.term.sessions[1].searchText != "" ||
		h.app.term.sessions[1].pane.SearchStatus().Text != "" {
		t.Fatalf("Escape did not close the second terminal's search: %+v", h.app.term.sessions[1])
	}
	if !h.app.term.sessions[0].searchOpen || h.app.term.sessions[0].searchText != "needle" ||
		h.app.term.sessions[0].pane.SearchStatus().Total != 2 {
		t.Fatalf("closing the second search disturbed the first: %+v", h.app.term.sessions[0])
	}
	requireTerminalRunning(t, first)
	requireTerminalRunning(t, second)
}

func TestTerminalOpenLinkUsesEditorForRepositoryFilesAndLeavesURLsAlone(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "--quiet", root).CombinedOutput(); err != nil {
		t.Fatalf("initialize the real test repository: %v: %s", err, output)
	}
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("package main\n\n// 3\n// 4\n// 5\n// 6\n// 7\n// 8\n// 9\n// 10\n// 11\n// 12\n")
	for _, name := range []string{"src/main.go", "space name.go"} {
		if err := os.WriteFile(filepath.Join(root, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	original := newTerminal
	peers := make(chan monitoredTerminalPeer, 1)
	links := make(chan func(string) bool, 1)
	newTerminal = func(options terminal.Options) (*terminal.Terminal, error) {
		links <- options.OpenLink
		return newMonitoredTerminal(options, peers)
	}
	t.Cleanup(func() { newTerminal = original })
	h := menuTestApp(t)
	h.app.snapshot.Root = root
	h.app.settings.EditorCommand = vsCodeEditorCommand
	h.app.openTerminal()
	h.tester.Frame()
	h.waitFor(t, "the terminal", func() bool {
		return len(h.app.term.sessions) > 0 && h.app.term.sessions[0].pane != nil
	})
	peer := <-peers
	t.Cleanup(func() { _ = peer.Close() })
	openLink := <-links
	if openLink == nil {
		t.Fatal("the terminal has no OpenLink callback")
	}
	opened := stubEditor(t, nil)

	for _, link := range []string{"src/main.go:12:3", "space name.go(7,2)", filepath.Join(root, "src/main.go#L9C4")} {
		if !openLink(link) {
			t.Errorf("repository file link %q was not handled", link)
		}
	}
	want := []launchedEditor{
		{command: vsCodeEditorCommand, path: filepath.Join(root, "src/main.go"), line: 12},
		{command: vsCodeEditorCommand, path: filepath.Join(root, "space name.go"), line: 7},
		{command: vsCodeEditorCommand, path: filepath.Join(root, "src/main.go"), line: 9},
	}
	if !slices.Equal(*opened, want) {
		t.Fatalf("editor launches = %+v, want %+v", *opened, want)
	}

	before := len(*opened)
	for _, link := range []string{
		"https://example.com/src/main.go:12",
		"mailto:developer@example.com",
		"file://" + filepath.Join(root, "src/main.go"),
		outside + ":3",
		"../outside.go:4",
		"src/missing.go:2",
		"src:1",
	} {
		if openLink(link) {
			t.Errorf("non-repository-file link %q was unexpectedly handled", link)
		}
	}
	if len(*opened) != before {
		t.Fatalf("rejected links launched the editor: %+v", (*opened)[before:])
	}

	h.app.settings.EditorCommand = fmt.Sprintf("zed %s", filenamePlaceholder)
	if !openLink("src/main.go:21") || len(*opened) != before+1 {
		t.Fatal("the link did not use the current configured editor")
	}
	if got := (*opened)[len(*opened)-1]; got.command != "zed {{filename}}" || got.line != 21 {
		t.Fatalf("configured editor launch = %+v", got)
	}

	builtInBefore := len(*opened)
	h.app.settings.EditorCommand = builtInEditorCommand
	if !openLink("src/main.go:12:3") {
		t.Fatal("Built-in did not handle the repository file link")
	}
	h.waitFor(t, "the terminal link in Code", func() bool {
		document := h.app.code.documents["src/main.go"]
		return document != nil && document.state != nil && h.app.selectedCodePath() == "src/main.go"
	})
	if h.app.workspaceTab != sectionCode || h.app.code.active != "src/main.go" {
		t.Fatalf("the terminal link opened section=%d file=%q", h.app.workspaceTab, h.app.code.active)
	}
	if got := h.app.activeCodeDocument().state.CursorPos().Line; got != 11 {
		t.Fatalf("the terminal link opened line %d, want zero-based line 11", got)
	}
	if len(*opened) != builtInBefore {
		t.Fatalf("Built-in terminal link launched an external editor: %+v", (*opened)[builtInBefore:])
	}
}
