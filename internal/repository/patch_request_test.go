package repository

import (
	"context"
	"os"
	"strings"
	"testing"
)

func readRequestedPatch(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestPatchPreviewUsesPrivateRequestInsteadOfPublicMetadata(t *testing.T) {
	dir, session := patchImportRepository(t)
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nAPPROVED\nthree\n"))
	})
	preview, err := session.PreviewPatchRequest(context.Background(), PatchRequest{
		Text: readRequestedPatch(t, patchPath), Target: PatchWorktree,
	})
	if err != nil || !preview.Clean {
		t.Fatalf("preview = %#v, %v", preview, err)
	}

	// All of these fields are display/test-double metadata. The real mutation
	// must still be the forward, worktree-only proposal that was checked.
	preview.Text = "not the approved patch"
	preview.Target = PatchIndex
	preview.Reverse = true
	preview.ThreeWay = true
	if err := session.ApplyPatch(context.Background(), preview); err != nil {
		t.Fatal(err)
	}
	if got := string(readPatchImportFile(t, dir, "a.txt")); got != "one\nAPPROVED\nthree\n" {
		t.Fatalf("approved worktree content = %q", got)
	}
	if staged := gitOutput(t, dir, "diff", "--cached", "--binary"); staged != "" {
		t.Fatalf("public Target=PatchIndex redirected the mutation:\n%s", staged)
	}
}

func TestPatchPreviewRejectsPrivateOptionTampering(t *testing.T) {
	dir, session := patchImportRepository(t)
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nAPPROVED\nthree\n"))
	})
	patchText := readRequestedPatch(t, patchPath)
	preview, err := session.PreviewPatchRequest(context.Background(), PatchRequest{Text: patchText, Target: PatchIndex})
	if err != nil || !preview.Clean {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	preview.requestReverse = true
	before := importRepositoryFingerprint(t, dir)
	if err := session.ApplyPatch(context.Background(), preview); err == nil || !strings.Contains(err.Error(), "options failed their integrity check") {
		t.Fatalf("private option tampering error = %v", err)
	}
	if after := importRepositoryFingerprint(t, dir); after != before {
		t.Fatal("private option tampering changed the repository")
	}
}

func TestPatchPreviewRequestRejectsStaleStateDigest(t *testing.T) {
	dir, session := patchImportRepository(t)
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nSTAGED\nthree\n"))
	})
	patchText := readRequestedPatch(t, patchPath)
	preview, err := session.PreviewPatchRequest(context.Background(), PatchRequest{Text: patchText, Target: PatchIndex})
	if err != nil || !preview.Clean {
		t.Fatalf("preview = %#v, %v", preview, err)
	}

	// Preserve status shape while changing content, proving the CAS is over
	// private file/index content digests rather than only porcelain letters.
	writePatchImportFile(t, dir, "a.txt", []byte("one\nEXTERNAL\nthree\n"))
	before := importRepositoryFingerprint(t, dir)
	if err := session.ApplyPatch(context.Background(), preview); err == nil || !strings.Contains(err.Error(), "repository changed since patch preview") {
		t.Fatalf("stale preview error = %v", err)
	}
	if after := importRepositoryFingerprint(t, dir); after != before {
		t.Fatal("digest CAS failure changed the repository")
	}
}

func TestPatchRequestBothAndThreeWayApplyTheCheckedOptions(t *testing.T) {
	dir, session := patchImportRepository(t)
	patchPath := makeImportPatch(t, dir, func() {
		writePatchImportFile(t, dir, "a.txt", []byte("one\nBOTH\nthree\n"))
	})
	patchText := readRequestedPatch(t, patchPath)
	preview, err := session.PreviewPatchRequest(context.Background(), PatchRequest{
		Text: patchText, Target: PatchBoth, ThreeWay: true,
	})
	if err != nil || !preview.Clean {
		t.Fatalf("combined three-way preview = %#v, %v", preview, err)
	}
	if err := session.ApplyPatch(context.Background(), preview); err != nil {
		t.Fatal(err)
	}
	if got := string(readPatchImportFile(t, dir, "a.txt")); got != "one\nBOTH\nthree\n" {
		t.Fatalf("combined worktree content = %q", got)
	}
	if staged := gitOutput(t, dir, "diff", "--cached", "--binary"); !strings.Contains(staged, "+BOTH") {
		t.Fatalf("combined target did not update the index:\n%s", staged)
	}

	if _, err := session.PreviewPatchRequest(context.Background(), PatchRequest{
		Text: patchText, Target: PatchWorktree, ThreeWay: true,
	}); err == nil || !strings.Contains(err.Error(), "requires the index and working tree") {
		t.Fatalf("ambiguous three-way request error = %v", err)
	}
}
