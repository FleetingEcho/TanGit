package app

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Keep customer and product-identifying fixture data out of the public tree.
// The needle is assembled so this guard does not violate its own rule.
func TestRepositoryTextSourcesAvoidSensitiveProductNames(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate the repository hygiene test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	needle := []byte("for" + "ti")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".go", ".md", ".txt", ".json", ".yaml", ".yml":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(bytes.ToLower(data), needle) {
			relative, _ := filepath.Rel(root, path)
			t.Errorf("sensitive product identifier in %s", relative)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repository text sources: %v", err)
	}
}
