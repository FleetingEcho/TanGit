package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestParseLanguageServerWorkspaceEditForms(t *testing.T) {
	raw := json.RawMessage(`{
		"changes":{"file:///work/b.go":[{"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":2}},"newText":"B"}]},
		"documentChanges":[{"textDocument":{"uri":"file:///work/a.go","version":7},"edits":[{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":0}},"newText":"A"}]}]
	}`)
	edit, err := parseLanguageServerWorkspaceEdit(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(edit.Documents) != 2 || edit.Documents[0].URI != "file:///work/a.go" || edit.Documents[0].Version == nil || *edit.Documents[0].Version != 7 ||
		edit.Documents[1].URI != "file:///work/b.go" {
		t.Fatalf("workspace edit = %+v", edit)
	}
}

func TestParseLanguageServerWorkspaceEditRejectsResourceOperations(t *testing.T) {
	_, err := parseLanguageServerWorkspaceEdit(json.RawMessage(`{"documentChanges":[{"kind":"rename","oldUri":"file:///a","newUri":"file:///b"}]}`))
	if !errors.Is(err, errLanguageServerWorkspaceResourceOperation) {
		t.Fatalf("error = %v", err)
	}
}

func TestPlanLanguageServerTextEditsUTF16AndOverlap(t *testing.T) {
	text := "a😀b\nsecond"
	edits := []languageServerTextEdit{
		{Range: languageServerRange{Start: languageServerPosition{Line: 1, Character: 0}, End: languageServerPosition{Line: 1, Character: 6}}, NewText: "2"},
		{Range: languageServerRange{Start: languageServerPosition{Line: 0, Character: 1}, End: languageServerPosition{Line: 0, Character: 3}}, NewText: "emoji"},
	}
	planned, err := planLanguageServerTextEdits(text, edits)
	if err != nil {
		t.Fatal(err)
	}
	want := []editor.Edit{{Start: 1, End: 5, Text: "emoji"}, {Start: 7, End: 13, Text: "2"}}
	if !reflect.DeepEqual(planned, want) {
		t.Fatalf("planned = %+v, want %+v", planned, want)
	}
	_, err = planLanguageServerTextEdits("abc", []languageServerTextEdit{
		{Range: languageServerRange{Start: languageServerPosition{}, End: languageServerPosition{Character: 2}}},
		{Range: languageServerRange{Start: languageServerPosition{Character: 1}, End: languageServerPosition{Character: 3}}},
	})
	if !errors.Is(err, errLanguageServerWorkspaceEditOverlap) {
		t.Fatalf("overlap error = %v", err)
	}
}

func TestLanguageServerExactOffsetRejectsInvalidPositions(t *testing.T) {
	for _, position := range []languageServerPosition{{Line: 2}, {Character: 3}, {Character: 1}} {
		text := "ab"
		if position.Character == 1 {
			text = "😀"
		}
		if _, ok := languageServerExactOffsetAt(text, position); ok {
			t.Fatalf("accepted %+v in %q", position, text)
		}
	}
}

func TestLanguageServerWorkspaceEditRejectsGitMetadata(t *testing.T) {
	h := menuTestApp(t)
	root := t.TempDir()
	h.app.snapshot.Root = root
	h.app.code.documents = make(map[string]*codeDocument)
	filename := filepath.Join(root, ".git", "config")
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("[core]\n\trepositoryformatversion = 0\n")
	if err := os.WriteFile(filename, original, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"changes": map[string][]languageServerTextEdit{
		languageServerFileURI(filename): {{
			Range: languageServerRange{End: languageServerPosition{Character: 1}}, NewText: "X",
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	done, succeeded := false, true
	h.app.applyLanguageServerWorkspaceEdit(raw, "", func(ok bool) { done, succeeded = true, ok })
	waitForUpdates(t, h.updates, "the unsafe workspace edit to finish", func() bool { return done })
	if succeeded {
		t.Fatal("language server edited Git administrative metadata")
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(content, original) {
		t.Fatalf("Git metadata changed: got %q want %q", content, original)
	}
}

func TestValidateLanguageServerWorkspaceTargetRejectsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available in the Windows test environment")
	}
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.go")
	if err := os.WriteFile(outsideFile, []byte("package outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"linked/outside.go", "linked.go"} {
		filename := filepath.Join(root, filepath.FromSlash(path))
		if err := validateLanguageServerWorkspaceTarget(root, path, filename); err == nil {
			t.Errorf("accepted symlinked workspace edit target %q", path)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "regular.go"), []byte("package regular\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateLanguageServerWorkspaceTarget(root, "regular.go", filepath.Join(root, "regular.go")); err != nil {
		t.Fatalf("regular workspace file was rejected: %v", err)
	}
}

func TestLanguageServerWorkspaceRollbackPreservesExternalChanges(t *testing.T) {
	h := menuTestApp(t)
	root := t.TempDir()
	h.app.snapshot.Root = root
	for path, content := range map[string]string{"a.go": "original a", "b.go": "original b"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	targets := []languageServerWorkspaceTarget{
		{path: "a.go", filename: filepath.Join(root, "a.go"), original: "original a", result: "edited a", stamp: stampLocalFile([]byte("original a"))},
		{path: "b.go", filename: filepath.Join(root, "b.go"), original: "original b", result: "edited b", stamp: stampLocalFile([]byte("original b"))},
	}
	current := map[string]string{"a.go": "original a", "b.go": "original b"}
	originalCheckedWrite := writeWorkingFileChecked
	t.Cleanup(func() { writeWorkingFileChecked = originalCheckedWrite })
	writeWorkingFileChecked = func(path string, data []byte, expected localFileStamp, known bool) error {
		name := filepath.Base(path)
		if name == "a.go" && string(data) == "original a" {
			current[name] = "external edit"
		}
		if known && stampLocalFile([]byte(current[name])) != expected {
			return errLocalFileModified
		}
		if name == "b.go" {
			return errors.New("stop the transaction")
		}
		current[name] = string(data)
		return nil
	}
	h.app.code.languageEditSeq = 1
	h.app.code.languageEditRunning = true
	done, succeeded := false, true
	h.app.commitLanguageServerWorkspaceEdit(1, h.app.generation, root, "", targets, func(ok bool) {
		done, succeeded = true, ok
	})
	h.updates.waitForUpdate(t)
	if !done || succeeded {
		t.Fatalf("rollback outcome = done:%v success:%v", done, succeeded)
	}
	if current["a.go"] != "external edit" || current["b.go"] != "original b" {
		t.Fatalf("rollback overwrote external work: a=%q b=%q", current["a.go"], current["b.go"])
	}
}
