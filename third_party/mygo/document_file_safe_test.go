package mygo

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileSafelyCheckedRechecksBeforeReplacement(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "document.txt")
	if err := os.WriteFile(filename, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed := errors.New("changed while preparing replacement")
	check := func() error {
		content, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		if !bytes.Equal(content, []byte("old")) {
			return changed
		}
		return nil
	}
	err := WriteFileSafelyChecked(filename, check, func(w io.Writer) error {
		if _, err := io.WriteString(w, "replacement"); err != nil {
			return err
		}
		return os.WriteFile(filename, []byte("external"), 0o600)
	})
	if !errors.Is(err, changed) {
		t.Fatalf("checked replacement error = %v", err)
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "external" {
		t.Fatalf("checked replacement overwrote external contents with %q", content)
	}
	entries, err := os.ReadDir(filepath.Dir(filename))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("checked replacement left temporary files: %v", entries)
	}
}
