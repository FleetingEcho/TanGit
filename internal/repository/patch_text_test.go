package repository

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewPatchTextCleanApplyUsesImmutableApprovedBytes(t *testing.T) {
	dir, session := patchImportRepository(t)
	ctx := context.Background()
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nTWO\nthree\nfour\n"))
	})
	patchBytes := readPatchImportFile(t, filepath.Dir(patchPath), filepath.Base(patchPath))
	preview, err := session.PreviewPatchText(ctx, string(patchBytes))
	if err != nil || !preview.Clean {
		t.Fatalf("in-memory preview = %#v, %v", preview, err)
	}
	if preview.Path != "" || preview.TextTruncated || preview.Text != string(patchBytes) {
		t.Fatalf("unexpected in-memory display metadata: path=%q truncated=%v length=%d", preview.Path, preview.TextTruncated, len(preview.Text))
	}
	exported, err := preview.Bytes()
	if err != nil || !bytes.Equal(exported, patchBytes) {
		t.Fatalf("full bytes = %d bytes, %v; want %d", len(exported), err, len(patchBytes))
	}
	exported[0] ^= 0xff
	again, err := preview.Bytes()
	if err != nil || !bytes.Equal(again, patchBytes) {
		t.Fatal("export did not return an independent copy")
	}

	// Neither public metadata nor the original source file participates in an
	// in-memory apply; only the private digest-checked bytes do.
	preview.Text = "changed display text"
	preview.Files = nil
	if err := os.Remove(patchPath); err != nil {
		t.Fatal(err)
	}
	if err := session.ApplyPatch(ctx, preview); err != nil {
		t.Fatal(err)
	}
	if got := string(readPatchImportFile(t, dir, "a.txt")); got != "one\nTWO\nthree\nfour\n" {
		t.Fatalf("applied content = %q", got)
	}
	if cached := gitOutput(t, dir, "diff", "--cached", "--binary"); cached != "" {
		t.Fatalf("in-memory patch application staged changes:\n%s", cached)
	}
	if refs := importRecoveryRefs(t, dir); refs != "" {
		t.Fatalf("clean in-memory application created recovery refs: %q", refs)
	}
}

func TestPreviewPatchTextTruncationStillExportsAndAppliesEverything(t *testing.T) {
	dir, session := patchImportRepository(t)
	ctx := context.Background()
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "large.txt", []byte(strings.Repeat("a repeated line\n", 30000)))
	})
	patchBytes := readPatchImportFile(t, filepath.Dir(patchPath), filepath.Base(patchPath))
	if len(patchBytes) <= patchPreviewTextLimit {
		t.Fatalf("test patch is only %d bytes", len(patchBytes))
	}
	preview, err := session.PreviewPatchText(ctx, string(patchBytes))
	if err != nil || !preview.Clean {
		t.Fatalf("large in-memory preview = %#v, %v", preview, err)
	}
	if !preview.TextTruncated || len(preview.Text) > patchPreviewTextLimit || preview.Additions != 30000 {
		t.Fatalf("truncated=%v length=%d additions=%d", preview.TextTruncated, len(preview.Text), preview.Additions)
	}
	exported, err := preview.Bytes()
	if err != nil || !bytes.Equal(exported, patchBytes) {
		t.Fatalf("truncated preview exported %d bytes, %v; want %d", len(exported), err, len(patchBytes))
	}
	if err := session.ApplyPatch(ctx, preview); err != nil {
		t.Fatal(err)
	}
	if got := readPatchImportFile(t, dir, "large.txt"); !bytes.Equal(got, []byte(strings.Repeat("a repeated line\n", 30000))) {
		t.Fatalf("applied large file has %d bytes", len(got))
	}
}

func TestPreviewPatchTextRejectsPrivateByteTampering(t *testing.T) {
	dir, session := patchImportRepository(t)
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nPATCH\nthree\n"))
	})
	patchBytes := readPatchImportFile(t, filepath.Dir(patchPath), filepath.Base(patchPath))
	preview, err := session.PreviewPatchText(context.Background(), string(patchBytes))
	if err != nil || !preview.Clean {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	preview.patchBytes[len(preview.patchBytes)-1] ^= 1
	before := importRepositoryFingerprint(t, dir)
	if _, err := preview.Bytes(); err == nil || !strings.Contains(err.Error(), "integrity check") {
		t.Fatalf("tampered export error = %v", err)
	}
	if err := session.ApplyPatch(context.Background(), preview); err == nil || !strings.Contains(err.Error(), "integrity check") {
		t.Fatalf("tampered apply error = %v", err)
	}
	if after := importRepositoryFingerprint(t, dir); after != before {
		t.Fatal("tampered patch altered repository or recovery refs")
	}
}

func TestPreviewPatchTextRejectsStalePreviewAndDifferentRepository(t *testing.T) {
	t.Run("stale repository", func(t *testing.T) {
		dir, session := patchImportRepository(t)
		patchPath := makeImportPatch(t, dir, func() {
			writePatchImportFile(t, dir, "a.txt", []byte("one\nPATCH\nthree\n"))
		})
		patchBytes := readPatchImportFile(t, filepath.Dir(patchPath), filepath.Base(patchPath))
		preview, err := session.PreviewPatchText(context.Background(), string(patchBytes))
		if err != nil || !preview.Clean {
			t.Fatalf("preview = %#v, %v", preview, err)
		}
		writePatchImportFile(t, dir, "unrelated.txt", []byte("external\n"))
		before := importRepositoryFingerprint(t, dir)
		if err := session.ApplyPatch(context.Background(), preview); err == nil || !strings.Contains(err.Error(), "repository changed since patch preview") {
			t.Fatalf("stale apply error = %v", err)
		}
		if after := importRepositoryFingerprint(t, dir); after != before {
			t.Fatal("stale application altered repository or recovery refs")
		}
	})

	t.Run("different repository", func(t *testing.T) {
		dir, session := patchImportRepository(t)
		_, otherSession := patchImportRepository(t)
		patchPath := makeImportPatch(t, dir, func() {
			writePatchImportFile(t, dir, "a.txt", []byte("one\nPATCH\nthree\n"))
		})
		patchBytes := readPatchImportFile(t, filepath.Dir(patchPath), filepath.Base(patchPath))
		preview, err := session.PreviewPatchText(context.Background(), string(patchBytes))
		if err != nil || !preview.Clean {
			t.Fatalf("preview = %#v, %v", preview, err)
		}
		if err := otherSession.ApplyPatch(context.Background(), preview); err == nil || !strings.Contains(err.Error(), "different repository") {
			t.Fatalf("cross-repository apply error = %v", err)
		}
		if got := string(readPatchImportFile(t, dir, "a.txt")); got != "one\ntwo\nthree\n" {
			t.Fatalf("source repository changed: %q", got)
		}
	})
}
