package agentassist

import "testing"

func TestConflictCustomMarkersPreserveCRLF(t *testing.T) {
	text := "before\r\n<<<<<<<<< HEAD\r\ncurrent\r\n||||||||| base\r\noriginal\r\n=========\r\nincoming\r\n>>>>>>>>> feature\r\nafter"
	artifact := NewConflictArtifact("crlf.txt", text)
	artifact.MarkerSize = 9
	request, err := NewRequest(KindConflict, []Artifact{artifact})
	if err != nil {
		t.Fatal(err)
	}
	region := request.Artifacts[0].Regions[0]
	if region.Current != "current\r\n" || region.Base != "original\r\n" || region.Incoming != "incoming\r\n" {
		t.Fatalf("side text did not preserve CRLF: %+v", region)
	}
	advice := responseEnvelope(request)
	advice.Suggestions = []Suggestion{{ID: "both", Conflict: &ConflictSuggestion{
		ArtifactID: request.Artifacts[0].ID, RegionID: region.ID, Substitution: SubstituteBoth,
	}}}
	preview, err := BuildPreview(request, advice)
	if err != nil {
		t.Fatal(err)
	}
	want := "before\r\ncurrent\r\nincoming\r\nafter"
	if preview.Artifacts[0].Text != want {
		t.Fatalf("preview = %q, want %q", preview.Artifacts[0].Text, want)
	}
}

func TestQuotedRenameDiffKeepsOriginalPaths(t *testing.T) {
	diff := "diff --git \"a/old name.txt\" \"b/new name.txt\"\n" +
		"similarity index 80%\n" +
		"rename from old name.txt\n" +
		"rename to new name.txt\n" +
		"--- \"a/old name.txt\"\n" +
		"+++ \"b/new name.txt\"\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"+new\n"
	request, err := NewRequest(KindChangeSplit, []Artifact{NewDiffArtifact(diff)})
	if err != nil {
		t.Fatal(err)
	}
	file := request.Artifacts[0].Files[0]
	if file.Path != "new name.txt" || file.OldPath != "old name.txt" {
		t.Fatalf("quoted paths = old %q new %q", file.OldPath, file.Path)
	}
	advice := responseEnvelope(request)
	advice.Suggestions = []Suggestion{{ID: "rename", Summary: "Rename with content update", Split: &ChangeSplit{Changes: []ChangeRef{{
		ArtifactID: request.Artifacts[0].ID, FileID: file.ID,
	}}}}}
	preview, err := BuildPreview(request, advice)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Splits) != 1 || preview.Splits[0].Files[0].OldPath != "old name.txt" {
		t.Fatalf("rename preview lost old identity: %+v", preview.Splits)
	}
}

func TestUnsealedRequestCanUseResolvedCopy(t *testing.T) {
	sealed := singleConflictRequest(t, "file.txt")
	advice := singleConflictAdvice(sealed)
	unsealed := AdviceRequest{Kind: KindConflict, Artifacts: []Artifact{NewConflictArtifact("file.txt", sealed.Artifacts[0].Text)}}
	preview, err := BuildPreview(unsealed, advice)
	if err != nil {
		t.Fatal(err)
	}
	data, err := preview.ResolvedCopy(sealed.Artifacts[0].ID, unsealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != preview.Artifacts[0].Text {
		t.Fatalf("copy = %q, preview = %q", data, preview.Artifacts[0].Text)
	}
}
