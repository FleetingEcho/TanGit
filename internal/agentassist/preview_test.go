package agentassist

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeAdvisor struct {
	requests []AdviceRequest
	advice   Advice
	err      error
}

func (s *fakeAdvisor) Propose(_ context.Context, request AdviceRequest) (Advice, error) {
	s.requests = append(s.requests, request.Clone())
	return s.advice.Clone(), s.err
}

func conflictSource() string {
	return "before\n" +
		"<<<<<<< HEAD\ncurrent one\n||||||| original\nbase one\n=======\nincoming one\n>>>>>>> feature\n" +
		"<<<<<<< HEAD\ncurrent two\n||||||| original\nbase two\n=======\nincoming two\n>>>>>>> feature\n" +
		"<<<<<<< HEAD\ncurrent three\n||||||| original\nbase three\n=======\nincoming three\n>>>>>>> feature\n" +
		"<<<<<<< HEAD\ncurrent four\n||||||| original\nbase four\n=======\nincoming four\n>>>>>>> feature\n" +
		"<<<<<<< HEAD\ncustom current\n||||||| original\ncustom base\n=======\ncustom incoming\n>>>>>>> feature\n" +
		"after\n"
}

func conflictRequest(t *testing.T) AdviceRequest {
	t.Helper()
	request, err := NewRequest(KindConflict, []Artifact{NewConflictArtifact("dir/conflicted.txt", conflictSource())})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return request
}

func responseEnvelope(request AdviceRequest) Advice {
	advice := Advice{Version: ProtocolVersion, Kind: request.Kind, SourceDigest: request.SourceDigest, Suggestions: []Suggestion{}}
	for _, artifact := range request.Artifacts {
		advice.Artifacts = append(advice.Artifacts, ArtifactIdentity{ID: artifact.ID, Digest: artifact.Digest})
	}
	return advice
}

func completeConflictAdvice(request AdviceRequest, substitutions ...Substitution) Advice {
	advice := responseEnvelope(request)
	artifact := request.Artifacts[0]
	for i, substitution := range substitutions {
		proposal := &ConflictSuggestion{ArtifactID: artifact.ID, RegionID: artifact.Regions[i].ID, Substitution: substitution}
		if substitution == SubstituteCustom {
			proposal.Custom = "custom five\nsecond line\n"
		}
		advice.Suggestions = append(advice.Suggestions, Suggestion{
			ID: "resolution-" + string(rune('a'+i)), Summary: "apply reviewed choice", Conflict: proposal,
		})
	}
	return advice
}

func TestAdvisorSeamConflictPreviewAndCopyCAS(t *testing.T) {
	request := conflictRequest(t)
	spy := &fakeAdvisor{advice: completeConflictAdvice(request,
		SubstituteCurrent, SubstituteBase, SubstituteIncoming, SubstituteBoth, SubstituteCustom,
	)}
	var advisor Advisor = spy
	advice, err := advisor.Propose(context.Background(), request)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if len(spy.requests) != 1 {
		t.Fatalf("advisor calls = %d, want one", len(spy.requests))
	}
	spy.requests[0].Artifacts[0].Text = "spy mutation"
	if request.Artifacts[0].Text != conflictSource() {
		t.Fatal("advisor or spy aliases the caller's source")
	}

	preview, err := BuildPreview(request, advice)
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}
	want := "before\ncurrent one\nbase two\nincoming three\ncurrent four\nincoming four\ncustom five\nsecond line\nafter\n"
	if len(preview.Artifacts) != 1 || preview.Artifacts[0].Text != want {
		t.Fatalf("preview text =\n%s\nwant:\n%s", preview.Artifacts[0].Text, want)
	}
	if !preview.Artifacts[0].Complete {
		t.Fatal("fully suggested preview is incomplete")
	}
	artifactID := request.Artifacts[0].ID
	first, err := preview.ResolvedCopy(artifactID, request)
	if err != nil {
		t.Fatalf("ResolvedCopy: %v", err)
	}
	first[0] = 'X'
	preview.Artifacts[0].Text = "public display mutation"
	preview.Artifacts[0].Regions[0].RegionID = "forged"
	second, err := preview.ResolvedCopy(artifactID, request)
	if err != nil {
		t.Fatalf("ResolvedCopy after public mutations: %v", err)
	}
	if string(second) != want {
		t.Fatalf("private copy changed through public data or returned slice: %q", second)
	}

	current := request.Clone()
	current.Artifacts[0].Text += "new source edit\n"
	if _, err := preview.ResolvedCopy(artifactID, current); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("stale source error = %v, want ErrSourceChanged", err)
	}
	if request.Artifacts[0].Text != conflictSource() {
		t.Fatal("preview changed original source")
	}
}

func TestConflictExclusionsAreVisibleAndIncomplete(t *testing.T) {
	request, err := NewRequest(KindConflict, []Artifact{NewConflictArtifact("conflicted.txt",
		"<<<<<<< HEAD\ncurrent\n=======\nincoming\n>>>>>>> other\n")})
	if err != nil {
		t.Fatal(err)
	}
	advice := responseEnvelope(request)
	if len(advice.Exclusions) != 0 {
		t.Fatal("unexpected fixture exclusions")
	}
	advice.Exclusions = []Exclusion{{ArtifactID: request.Artifacts[0].ID, RegionID: request.Artifacts[0].Regions[0].ID, Reason: "leave for human review"}}
	preview, err := BuildPreview(request, advice)
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}
	artifact := preview.Artifacts[0]
	if artifact.Complete || !artifact.Regions[0].Excluded || artifact.Regions[0].Reason != "leave for human review" {
		t.Fatalf("exclusion was not explicit: %+v", artifact)
	}
	if _, err := preview.ResolvedCopy(request.Artifacts[0].ID, request); !errors.Is(err, ErrIncompletePreview) {
		t.Fatalf("partial copy error = %v, want ErrIncompletePreview", err)
	}
}

func TestConflictBaseRequiresDiff3Base(t *testing.T) {
	request, err := NewRequest(KindConflict, []Artifact{NewConflictArtifact("conflicted.txt",
		"<<<<<<< HEAD\ncurrent\n=======\nincoming\n>>>>>>> other\n")})
	if err != nil {
		t.Fatal(err)
	}
	advice := completeConflictAdvice(request, SubstituteBase)
	if _, err := BuildPreview(request, advice); !errors.Is(err, ErrInvalidProtocol) || !strings.Contains(err.Error(), "absent base") {
		t.Fatalf("two-way base error = %v", err)
	}
}

const splitDiff = `diff --git a/alpha.txt b/alpha.txt
index 1111111..2222222 100644
--- a/alpha.txt
+++ b/alpha.txt
@@ -1,4 +1,4 @@
 keep
-old one
+new one
 keep2
-old two
+new two
@@ -10,3 +10,3 @@
 before
-old three
+new three
 after
diff --git a/beta.txt b/beta.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/beta.txt
@@ -0,0 +1,2 @@
+beta1
+beta2
\ No newline at end of file
`

func splitRequest(t *testing.T) AdviceRequest {
	t.Helper()
	request, err := NewRequest(KindChangeSplit, []Artifact{NewDiffArtifact(splitDiff)})
	if err != nil {
		t.Fatalf("NewRequest diff: %v", err)
	}
	if len(request.Artifacts[0].Files) != 2 || len(request.Artifacts[0].Files[0].Hunks) != 2 {
		t.Fatalf("unexpected parsed source: %+v", request.Artifacts[0].Files)
	}
	return request
}

func changedLineIDs(hunk DiffHunk, from, to int) []string {
	ids := make([]string, 0)
	for _, line := range hunk.Lines {
		if (line.Kind == LineAdd || line.Kind == LineDelete) && line.OldLine+line.NewLine >= from && line.OldLine+line.NewLine <= to {
			ids = append(ids, line.ID)
		}
	}
	return ids
}

func TestChangeSplitUsesOnlyOriginalIdentityGroups(t *testing.T) {
	request := splitRequest(t)
	artifact := request.Artifacts[0]
	alpha, beta := artifact.Files[0], artifact.Files[1]
	firstPair := []string{}
	for _, line := range alpha.Hunks[0].Lines {
		if line.Text == "old one" || line.Text == "new one" {
			firstPair = append(firstPair, line.ID)
		}
	}
	secondPair := []string{}
	for _, line := range alpha.Hunks[0].Lines {
		if line.Text == "old two" || line.Text == "new two" {
			secondPair = append(secondPair, line.ID)
		}
	}
	advice := responseEnvelope(request)
	advice.Suggestions = []Suggestion{
		{ID: "logic", Summary: "First logical change", Split: &ChangeSplit{Changes: []ChangeRef{
			{ArtifactID: artifact.ID, FileID: alpha.ID, HunkID: alpha.Hunks[0].ID, LineIDs: firstPair},
			{ArtifactID: artifact.ID, FileID: alpha.ID, HunkID: alpha.Hunks[1].ID},
		}}},
		{ID: "tests", Summary: "Remaining change and test", Split: &ChangeSplit{Changes: []ChangeRef{
			{ArtifactID: artifact.ID, FileID: alpha.ID, HunkID: alpha.Hunks[0].ID, LineIDs: secondPair},
			{ArtifactID: artifact.ID, FileID: beta.ID},
		}}},
	}
	preview, err := BuildPreview(request, advice)
	if err != nil {
		t.Fatalf("BuildPreview: %v", err)
	}
	if len(preview.Splits) != 2 || preview.Splits[0].Summary != "First logical change" {
		t.Fatalf("split preview = %+v", preview.Splits)
	}
	first := preview.Splits[0]
	if len(first.Files) != 1 || len(first.Files[0].Hunks) != 2 {
		t.Fatalf("first split should contain two original alpha hunks: %+v", first.Files)
	}
	if len(first.Files[0].Hunks[0].Lines) != 2 || len(first.Files[0].Hunks[1].Lines) != 4 {
		t.Fatalf("line and whole-hunk selection lost source identities: %+v", first.Files[0].Hunks)
	}
	second := preview.Splits[1]
	if len(second.Files) != 2 || second.Files[0].Path != "alpha.txt" || second.Files[1].Path != "beta.txt" {
		t.Fatalf("split output is not in deterministic original source order: %+v", second.Files)
	}
	if got := second.Files[1].Hunks[0].Lines[len(second.Files[1].Hunks[0].Lines)-1].Kind; got != LineMetadata {
		t.Fatalf("whole-file original hunk omitted newline metadata: %q", got)
	}
}

func TestChangeSplitExplicitFileExclusion(t *testing.T) {
	request := splitRequest(t)
	artifact := request.Artifacts[0]
	advice := responseEnvelope(request)
	advice.Suggestions = []Suggestion{{ID: "implementation", Summary: "Implementation", Split: &ChangeSplit{Changes: []ChangeRef{
		{ArtifactID: artifact.ID, FileID: artifact.Files[0].ID},
	}}}}
	advice.Exclusions = []Exclusion{{ArtifactID: artifact.ID, FileID: artifact.Files[1].ID, Reason: "review generated fixture separately"}}
	preview, err := BuildPreview(request, advice)
	if err != nil {
		t.Fatalf("explicitly excluded file must not be implicitly missing: %v", err)
	}
	if len(preview.Splits) != 1 || len(preview.Exclusions) != 1 || preview.Exclusions[0].Reason == "" {
		t.Fatalf("missing explicit exclusion in preview: %+v", preview)
	}
}
