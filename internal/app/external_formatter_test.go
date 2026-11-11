package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/workspace"
)

type externalFormatterCall struct {
	command string
	path    string
	root    string
	input   string
}

func stubExternalFormatter(t *testing.T, run func(externalFormatterCall) (string, error)) {
	t.Helper()
	original := runCodeFormatter
	runCodeFormatter = func(command, path, root, input string) (string, error) {
		return run(externalFormatterCall{command: command, path: path, root: root, input: input})
	}
	t.Cleanup(func() { runCodeFormatter = original })
}

func TestParseCodeFormatterCommandQuotesAndFilePlaceholder(t *testing.T) {
	arguments, err := parseCodeFormatterCommand(
		`prettier --stdin-filepath "{{file}}" --parser 'go fmt' "$HOME" "$(pwd)"`,
		`/work/a project/main.go`,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"prettier", "--stdin-filepath", "/work/a project/main.go", "--parser", "go fmt", "$HOME", "$(pwd)"}
	if !reflect.DeepEqual(arguments, want) {
		t.Fatalf("formatter arguments = %#v, want %#v", arguments, want)
	}
	for _, command := range []string{"", "   ", `prettier "`, "gofmt; touch forbidden", "gofmt && touch forbidden", "gofmt > output"} {
		if _, err := parseCodeFormatterCommand(command, "main.go"); err == nil {
			t.Errorf("unsafe or empty formatter command %q was accepted", command)
		}
	}
}

func TestCodeFormatterHelperProcess(t *testing.T) {
	mode := os.Getenv("TANGIT_CODE_FORMATTER_HELPER")
	if mode == "" {
		return
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(12)
	}
	switch mode {
	case "format":
		separator := -1
		for index, argument := range os.Args {
			if argument == "--" {
				separator = index
				break
			}
		}
		want := []string{"--tag", "$HOME literal", "--stdin-filepath", os.Getenv("TANGIT_CODE_FORMATTER_EXPECT_FILE"), "--", "value with spaces"}
		if separator < 0 || !reflect.DeepEqual(os.Args[separator+1:], want) {
			fmt.Fprintf(os.Stderr, "arguments = %#v, want %#v", os.Args, want)
			os.Exit(13)
		}
		fmt.Print("formatted:" + string(data))
		os.Exit(0)
	case "failure":
		fmt.Fprint(os.Stderr, "invalid formatter configuration")
		os.Exit(7)
	case "empty":
		os.Exit(0)
	case "hang":
		time.Sleep(30 * time.Second)
	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode")
		os.Exit(14)
	}
}

func TestRunCodeFormatterCommandArgumentsStdinStdoutFailuresAndTimeout(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    string
		input   string
		timeout time.Duration
		want    string
		wantErr error
	}{
		{name: "arguments stdin and stdout", mode: "format", input: "package  main\n", want: "formatted:package  main\n"},
		{name: "nonzero failure includes stderr", mode: "failure", input: "source", wantErr: errors.New("invalid formatter configuration")},
		{name: "empty stdout is rejected", mode: "empty", input: "source", wantErr: errCodeFormatterEmptyOutput},
		{name: "timeout kills a stuck formatter", mode: "hang", input: "source", timeout: 40 * time.Millisecond, wantErr: errCodeFormatterTimedOut},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TANGIT_CODE_FORMATTER_HELPER", test.mode)
			root := t.TempDir()
			filePath := filepath.Join(root, "file with spaces.go")
			t.Setenv("TANGIT_CODE_FORMATTER_EXPECT_FILE", filePath)
			commandLine := strconv.Quote(os.Args[0]) +
				` -test.run=^TestCodeFormatterHelperProcess$ -- --tag "$HOME literal" --stdin-filepath "{{file}}" -- "value with spaces"`
			timeout := test.timeout
			if timeout == 0 {
				timeout = 2 * time.Second
			}
			started := time.Now()
			output, err := runCodeFormatterCommandWithinTimeout(commandLine, filePath, root, test.input, timeout)
			if output != test.want {
				t.Fatalf("formatter stdout = %q, want %q", output, test.want)
			}
			if test.wantErr == nil && err != nil {
				t.Fatal(err)
			}
			if test.wantErr != nil {
				if err == nil || !strings.Contains(err.Error(), test.wantErr.Error()) {
					t.Fatalf("formatter error = %v, want text %q", err, test.wantErr)
				}
				if errors.Is(test.wantErr, errCodeFormatterTimedOut) {
					if !errors.Is(err, errCodeFormatterTimedOut) {
						t.Fatalf("timeout error = %v", err)
					}
					if elapsed := time.Since(started); elapsed > 5*time.Second {
						t.Fatalf("timed-out formatter took %s", elapsed)
					}
				}
			}
		})
	}
}

func TestFormatDocumentUsesExternalFormatterWhenLanguageServerDisabled(t *testing.T) {
	const original = "package main\nvar 界=\"😀\"\n"
	const formatted = "package main\n\nvar 界 = \"😀\"\n"
	called := make(chan externalFormatterCall, 1)
	stubExternalFormatter(t, func(call externalFormatterCall) (string, error) {
		called <- call
		return formatted, nil
	})
	lspFormatting := false
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{"main.go": original},
		func(method string, _ any, _ func(json.RawMessage, error)) {
			if method == "textDocument/formatting" || method == "textDocument/rangeFormatting" {
				lspFormatting = true
			}
		})
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"go": {Disabled: true, Formatter: `gofmt --stdin-filepath "{{file}}"`},
	}
	h.app.formatCodeDocument(document)
	h.waitFor(t, "external document formatting", func() bool { return !document.formatting })
	call := <-called
	if call.command != `gofmt --stdin-filepath "{{file}}"` || call.path != "/work/project/main.go" ||
		call.root != "/work/project" || call.input != original {
		t.Fatalf("external formatter invocation = %#v", call)
	}
	if lspFormatting {
		t.Fatal("the language server ran despite the configured external formatter")
	}
	if got := document.state.Buffer.String(); got != formatted {
		t.Fatalf("external document formatting = %q, want %q", got, formatted)
	}
	if !document.state.Buffer.Undo() || document.state.Buffer.String() != original ||
		!document.state.Buffer.Redo() || document.state.Buffer.String() != formatted {
		t.Fatal("external document formatting was not one undo/redo step")
	}
}

func TestFormatSelectionContextMenuEnablementAndExternalReplacement(t *testing.T) {
	const original = "var 界=\"😀\"\nvar other=2\n"
	const selected = "var 界=\"😀\""
	const formattedSelection = "var 界 = \"😀\""
	called := make(chan externalFormatterCall, 1)
	stubExternalFormatter(t, func(call externalFormatterCall) (string, error) {
		called <- call
		return formattedSelection, nil
	})
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{"main.go": original}, nil)
	h.app.settings.LanguageServers = map[string]languageServerSettings{
		"go": {Disabled: true, Formatter: "external-selection-formatter"},
	}
	bounds, found := h.tester.Find("Code editor")
	if !found {
		t.Fatal("the code editor is not visible")
	}
	openMenu := func() {
		// Right-click inside the first-line selection. MyGo intentionally
		// collapses a selection when the secondary click lands elsewhere.
		h.tester.RightClickAt(bounds.X+80, bounds.Y+10)
		h.tester.Frame()
	}
	openMenu()
	if err := h.tester.ChooseMenuItem("Format Selection"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("a caret enabled Format Selection: %v", err)
	}
	h.tester.CloseMenu()
	start, end := strings.Index(original, selected), strings.Index(original, selected)+len(selected)
	other := strings.Index(original, "var other=2")
	document.state.SetSelections(editor.Selection{Anchor: start, Head: end}, editor.Selection{Anchor: other, Head: other + 3})
	h.tester.Frame()
	openMenu()
	if got := len(document.state.Selections()); got != 2 {
		t.Fatalf("the menu pointer changed the multiple-selection fixture: %d selections", got)
	}
	if err := h.tester.ChooseMenuItem("Format Selection"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("multiple selections enabled Format Selection: %v", err)
	}
	h.tester.CloseMenu()
	document.state.Select(start, end)
	h.tester.Frame()
	openMenu()
	menu := h.tester.Menu()
	if selections := document.state.Selections(); len(selections) != 1 || selections[0].Empty() {
		t.Fatalf("the menu pointer changed the selection fixture: %#v", selections)
	}
	if !slices.Contains(menu, "Format Document") || !slices.Contains(menu, "Format Selection") {
		t.Fatalf("formatting context menu = %q", menu)
	}
	if err := h.tester.ChooseMenuItem("Format Selection"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "external selection formatting", func() bool { return !document.formatting })
	call := <-called
	if call.input != selected {
		t.Fatalf("external selection stdin = %q, want %q", call.input, selected)
	}
	formatted := strings.Replace(original, selected, formattedSelection, 1)
	if got := document.state.Buffer.String(); got != formatted {
		t.Fatalf("external selection formatting = %q, want %q", got, formatted)
	}
	if !document.state.Buffer.Undo() || document.state.Buffer.String() != original ||
		!document.state.Buffer.Redo() || document.state.Buffer.String() != formatted {
		t.Fatal("external selection formatting was not one undo/redo step")
	}
}

func TestExternalFormattingFailuresAndEmptyOutputPreserveDocument(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		err    error
	}{
		{name: "process failure", output: "must not be applied", err: errors.New("formatter failed")},
		{name: "timeout", output: "must not be applied", err: errCodeFormatterTimedOut},
		{name: "unexpected empty output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			const original = "package main\nvar x=1\n"
			stubExternalFormatter(t, func(externalFormatterCall) (string, error) { return test.output, test.err })
			h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{"main.go": original}, nil)
			h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Formatter: "broken-formatter"}}
			h.app.formatCodeDocument(document)
			h.waitFor(t, "failed external formatting", func() bool { return !document.formatting })
			if got := document.state.Buffer.String(); got != original {
				t.Fatalf("failed formatting changed the document to %q", got)
			}
			if document.formatting || !h.tester.HasText("Go (gopls) formatting failed") {
				t.Fatalf("failed formatting state = busy %v, texts %q", document.formatting, h.texts())
			}
		})
	}
}

func TestExternalFormattingDiscardsResultAfterFurtherTyping(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan struct{})
	stubExternalFormatter(t, func(externalFormatterCall) (string, error) {
		close(started)
		<-finish
		return "package overwrite\n", nil
	})
	h, _, document := openGoDocumentWithLanguageServer(t, map[string]string{"main.go": "package main\nvar x=1\n"}, nil)
	h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Formatter: "slow-formatter"}}
	h.app.formatCodeDocument(document)
	<-started
	document.state.Insert("// keep this edit\n")
	want := document.state.Buffer.String()
	close(finish)
	h.waitFor(t, "stale external formatting", func() bool { return !document.formatting })
	if got := document.state.Buffer.String(); got != want {
		t.Fatalf("stale external formatting replaced new typing: %q, want %q", got, want)
	}
	if document.formatting {
		t.Fatal("a stale external response did not clear the busy state")
	}
}

func TestExternalFormatterNeverLaunchesForRemoteWorkspace(t *testing.T) {
	h := menuTestApp(t)
	h.app.location = workspace.Location{Kind: workspace.SSH, Root: "/remote/project"}
	document := &codeDocument{path: "main.go", state: &editor.State{Buffer: editor.NewBuffer("package main\n")}}
	h.app.settings.LanguageServers = map[string]languageServerSettings{"go": {Formatter: "gofmt"}}
	if _, ok := h.app.externalCodeFormatter(document); ok {
		t.Fatal("a remote workspace offered a local external formatter")
	}
}
