package app

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestNormalizedCodeSearchFolder(t *testing.T) {
	for _, test := range []struct {
		value  string
		folder string
		valid  bool
	}{
		{value: "", valid: true},
		{value: ".", valid: true},
		{value: "/", valid: true},
		{value: "nested/deep/", folder: "nested/deep", valid: true},
		{value: "./nested/../source", folder: "source", valid: true},
		{value: "../outside", valid: false},
		{value: "nested/../../outside", valid: false},
		{value: "/outside", valid: false},
	} {
		folder, valid := normalizedCodeSearchFolder(test.value)
		if folder != test.folder || valid != test.valid {
			t.Errorf("normalizedCodeSearchFolder(%q) = %q, %v; want %q, %v", test.value, folder, valid, test.folder, test.valid)
		}
	}
}

func TestCodeDeepWorkspaceShowsFiles(t *testing.T) {
	h := menuTestApp(t)
	useRealCodeWorkspace(t, h, map[string]string{
		"example/server/src/web_new/main.go":   "package main\n",
		"example/server/src/web_new/README.md": "# web_new\n",
		"example/server/src/other/skip.go":     "package other\n",
	})
	if !h.app.openCodeWorkspaceFolder("example/server/src/web_new") {
		t.Fatal("the reported workspace folder was rejected")
	}
	h.tester.Frame()
	if len(h.app.code.visiblePaths) != 2 || len(h.app.code.rows) == 0 {
		t.Fatalf("the opened workspace is empty: paths=%q rows=%+v files=%q",
			h.app.code.visiblePaths, h.app.code.rows, h.app.code.files)
	}
	if h.tester.HasText("This repository has no files") {
		t.Fatal("the Explorer claims the non-empty workspace has no files")
	}
}

func TestCodeBrowseSearchFolderUsesRepositoryRelativeScope(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{
		"root.go":             "package root // needle\n",
		"nested/deep/file.go": "package deep // needle\n",
	})
	deep := filepath.Join(root, "nested", "deep")
	originalChoose := chooseCodeSearchFolder
	t.Cleanup(func() { chooseCodeSearchFolder = originalChoose })
	var options mygo.OpenDialogOptions
	chooseCodeSearchFolder = func(opts mygo.OpenDialogOptions) ([]string, error) {
		options = opts
		return []string{deep}, nil
	}
	h.app.code.searchQuery = "needle"
	h.app.browseCodeSearchFolder()
	h.waitFor(t, "the native folder selection", func() bool {
		return !h.app.code.searchRunning && h.app.code.searchFolder == "nested/deep" && len(h.app.code.searchResults) == 1
	})
	if !options.Directory || options.DefaultPath != root || options.ButtonLabel != tr("Open folder") {
		t.Fatalf("native folder options = %+v", options)
	}
	if got := h.app.code.searchResults[0].Path; got != "nested/deep/file.go" {
		t.Fatalf("picked-folder search returned %q", got)
	}
}

func TestCodeBrowseSearchFolderRejectsOutsideAndLateAnswers(t *testing.T) {
	h := menuTestApp(t)
	root := useRealCodeWorkspace(t, h, map[string]string{"root.go": "package root\n"})
	outside := t.TempDir()
	if got, err := codeSearchFolderFromDisk(root, outside); err == nil || got != "" {
		t.Fatalf("outside folder = %q, %v; want rejection", got, err)
	}

	answer := make(chan []string)
	originalChoose := chooseCodeSearchFolder
	t.Cleanup(func() { chooseCodeSearchFolder = originalChoose })
	chooseCodeSearchFolder = func(mygo.OpenDialogOptions) ([]string, error) { return <-answer, nil }
	h.app.browseCodeSearchFolder()
	// A newer choice invalidates the first native dialog's eventual answer.
	h.app.code.searchFolder = "current"
	h.app.code.searchFolderSelectionSeq++
	answer <- []string{root}
	h.tester.Frame()
	if h.app.code.searchFolder != "current" {
		t.Fatalf("late folder answer changed the scope to %q", h.app.code.searchFolder)
	}
}
