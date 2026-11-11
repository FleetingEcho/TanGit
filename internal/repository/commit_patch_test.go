package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestCommitFilePatchRootCommitHasCanonicalBinaryAndFullIndex(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "desktop@example.com")
	runGit(t, dir, "config", "user.name", "Desktop")
	writePatchImportFile(t, dir, "a.txt", []byte("one\ntwo\nthree\n"))
	writePatchImportFile(t, dir, "binary.dat", []byte{0, 1, 2, 3})
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "root")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	commit := Commit{Hash: strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))}

	textPatch, err := session.CommitFilePatch(context.Background(), commit, CommitFile{Path: "a.txt", Status: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textPatch, "--- /dev/null\n+++ b/a.txt\n") ||
		!strings.Contains(textPatch, "@@ -0,0 +1,3 @@\n+one\n+two\n+three\n") {
		t.Fatalf("root text patch does not add the complete file:\n%s", textPatch)
	}
	if matched := regexp.MustCompile(`(?m)^index [0-9a-f]{40}\.\.[0-9a-f]{40}( 100644)?$`).MatchString(textPatch); !matched {
		t.Fatalf("text patch lacks full object IDs:\n%s", textPatch)
	}

	binaryPatch, err := session.CommitFilePatch(context.Background(), commit, CommitFile{Path: "binary.dat", Status: "A"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(binaryPatch, "GIT binary patch\n") || !strings.Contains(binaryPatch, "literal 4\n") {
		t.Fatalf("root binary patch does not contain a binary literal:\n%s", binaryPatch)
	}
	if matched := regexp.MustCompile(`(?m)^index [0-9a-f]{40}\.\.[0-9a-f]{40}$`).MatchString(binaryPatch); !matched {
		t.Fatalf("binary patch lacks full object IDs:\n%s", binaryPatch)
	}
}

func TestCommitFilePatchUsesFirstParentAndFixedContext(t *testing.T) {
	dir, session := patchImportRepository(t)
	writePatchImportFile(t, dir, "a.txt", []byte("zero\none\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n"))
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "more context")
	base := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	writePatchImportFile(t, dir, "a.txt", []byte("zero\none\ntwo\nthree\nFOUR\nfive\nsix\nseven\neight\nnine\n"))
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "edit")
	commit := Commit{
		Hash:    strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD")),
		Parents: []string{base},
	}

	patch, err := session.CommitFilePatch(context.Background(), commit, CommitFile{Path: "a.txt", Status: "M"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "@@ -2,7 +2,7 @@ zero\n one\n two\n three\n-four\n+FOUR\n five\n six\n seven\n") {
		t.Fatalf("patch does not use exactly three context lines against the first parent:\n%s", patch)
	}
	if strings.Contains(patch, "\n zero\n") || strings.Contains(patch, "\n eight\n") {
		t.Fatalf("patch contains display-independent extra context:\n%s", patch)
	}

	// Exercise the repository's textconv setting as well: a custom patch must
	// contain the committed bytes rather than a filter's display output.
	runGit(t, dir, "config", "diff.shout.textconv", "tr '[:lower:]' '[:upper:]'")
	writePatchImportFile(t, dir, ".gitattributes", []byte("a.txt diff=shout\n"))
	canonicalAgain, err := session.CommitFilePatch(context.Background(), commit, CommitFile{Path: "a.txt", Status: "M"})
	if err != nil {
		t.Fatal(err)
	}
	if canonicalAgain != patch {
		t.Fatalf("textconv affected canonical patch\nbefore:\n%s\nafter:\n%s", patch, canonicalAgain)
	}

	// Selection line identities come from the viewer and are applied to the
	// canonical patch. A filter that changes line counts must therefore be
	// disabled in both views, not just in the exported patch.
	runGit(t, dir, "config", "diff.shout.textconv", "printf 'converted\\n'")
	displayed, err := session.CommitFileDiff(context.Background(), commit, CommitFile{Path: "a.txt", Status: "M"}, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(displayed.Patch, "-four\n+FOUR\n") || strings.Contains(displayed.Patch, "converted") {
		t.Fatalf("viewer and canonical patch use different content coordinates:\n%s", displayed.Patch)
	}
}
