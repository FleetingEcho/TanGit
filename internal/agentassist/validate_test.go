package agentassist

import (
	"errors"
	"strings"
	"testing"
)

func TestRejectedSourceArtifacts(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		text string
		path string
		want error
	}{
		{"NUL conflict", KindConflict, "before\x00after\n", "file.txt", ErrBinaryArtifact},
		{"invalid UTF-8 conflict", KindConflict, string([]byte{0xff}), "file.txt", ErrBinaryArtifact},
		{"absolute path", KindConflict, "<<<<<<<\n=======\n>>>>>>>\n", "/outside", ErrInvalidProtocol},
		{"parent path", KindConflict, "<<<<<<<\n=======\n>>>>>>>\n", "../outside", ErrInvalidProtocol},
		{"backslash path", KindConflict, "<<<<<<<\n=======\n>>>>>>>\n", `..\outside`, ErrInvalidProtocol},
		{"binary Git patch", KindChangeSplit, "diff --git a/image.png b/image.png\nGIT binary patch\n", "", ErrBinaryArtifact},
		{"binary files differ", KindChangeSplit, "diff --git a/image.png b/image.png\nBinary files a/image.png and b/image.png differ\n", "", ErrBinaryArtifact},
		{"malformed conflict", KindConflict, "<<<<<<< HEAD\ncurrent\n", "file.txt", ErrInvalidProtocol},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			artifact := Artifact{Path: test.path, Text: test.text}
			if test.kind == KindConflict {
				artifact.MarkerSize = 7
			}
			_, err := NewRequest(test.kind, []Artifact{artifact})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSourceAndResponseLimits(t *testing.T) {
	request := conflictRequest(t)
	tooSmall := request
	tooSmall.Limits.MaxArtifactBytes = 32
	if _, err := ValidateAdvice(tooSmall, completeConflictAdvice(request, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("large artifact error = %v, want ErrLimitExceeded", err)
	}
	tooMany := request
	tooMany.Limits.MaxSuggestions = 1
	if _, err := ValidateAdvice(tooMany, completeConflictAdvice(request, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("suggestion limit error = %v, want ErrLimitExceeded", err)
	}
	negative := request
	negative.Limits.MaxResponseBytes = -1
	if _, err := ValidateAdvice(negative, Advice{}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("negative limit error = %v, want ErrLimitExceeded", err)
	}
}

func TestConflictResponseIdentityFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AdviceRequest, *Advice)
		want   error
	}{
		{
			"request artifact hash mismatch",
			func(request *AdviceRequest, _ *Advice) {
				request.Artifacts[0].Digest = "sha256:" + strings.Repeat("0", 64)
			},
			ErrHashMismatch,
		},
		{
			"request source hash mismatch",
			func(request *AdviceRequest, _ *Advice) { request.SourceDigest = "sha256:" + strings.Repeat("1", 64) },
			ErrHashMismatch,
		},
		{
			"response source hash mismatch",
			func(_ *AdviceRequest, advice *Advice) { advice.SourceDigest = "sha256:" + strings.Repeat("2", 64) },
			ErrHashMismatch,
		},
		{
			"response artifact hash mismatch",
			func(_ *AdviceRequest, advice *Advice) {
				advice.Artifacts[0].Digest = "sha256:" + strings.Repeat("3", 64)
			},
			ErrHashMismatch,
		},
		{
			"duplicate response artifact",
			func(_ *AdviceRequest, advice *Advice) {
				advice.Artifacts = append(advice.Artifacts, advice.Artifacts[0])
			},
			ErrDuplicateIdentity,
		},
		{
			"missing response artifact identities",
			func(_ *AdviceRequest, advice *Advice) { advice.Artifacts = nil },
			ErrMissingIdentity,
		},
		{
			"unknown response artifact",
			func(_ *AdviceRequest, advice *Advice) { advice.Artifacts[0].ID = "different-artifact" },
			ErrUnknownIdentity,
		},
		{
			"missing region identities",
			func(_ *AdviceRequest, advice *Advice) { advice.Suggestions = advice.Suggestions[:3] },
			ErrMissingIdentity,
		},
		{
			"missing nested region identity",
			func(_ *AdviceRequest, advice *Advice) { advice.Suggestions[0].Conflict.RegionID = "" },
			ErrMissingIdentity,
		},
		{
			"unknown region identity",
			func(_ *AdviceRequest, advice *Advice) { advice.Suggestions[0].Conflict.RegionID = "old-region" },
			ErrUnknownIdentity,
		},
		{
			"duplicate suggested region",
			func(_ *AdviceRequest, advice *Advice) {
				duplicate := advice.Suggestions[0]
				duplicate.ID = "duplicate"
				copied := *duplicate.Conflict
				duplicate.Conflict = &copied
				advice.Suggestions = append(advice.Suggestions, duplicate)
			},
			ErrDuplicateIdentity,
		},
		{
			"suggested and excluded region",
			func(_ *AdviceRequest, advice *Advice) {
				advice.Exclusions = []Exclusion{{ArtifactID: advice.Suggestions[0].Conflict.ArtifactID, RegionID: advice.Suggestions[0].Conflict.RegionID, Reason: "contradiction"}}
			},
			ErrDuplicateIdentity,
		},
		{
			"unknown exclusion artifact",
			func(_ *AdviceRequest, advice *Advice) { advice.Artifacts[0].ID = "other" },
			ErrUnknownIdentity,
		},
		{
			"unsupported version",
			func(_ *AdviceRequest, advice *Advice) { advice.Version = 99 },
			ErrInvalidProtocol,
		},
		{
			"non custom replacement text",
			func(_ *AdviceRequest, advice *Advice) { advice.Suggestions[0].Conflict.Custom = "not allowed" },
			ErrInvalidProtocol,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := conflictRequest(t)
			advice := completeConflictAdvice(request,
				SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCustom,
			)
			test.mutate(&request, &advice)
			_, err := ValidateAdvice(request, advice)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCrossSourceIdentitiesCannotBeReused(t *testing.T) {
	original := conflictRequest(t)
	originalAdvice := completeConflictAdvice(original,
		SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent,
	)
	adviceFromOriginal, err := ValidateAdvice(original, originalAdvice)
	if err != nil {
		t.Fatal(err)
	}
	changed := conflictRequest(t)
	if _, err := ValidateAdvice(changed, adviceFromOriginal); err != nil {
		t.Fatalf("fixture should initially match: %v", err)
	}
	changed.Artifacts[0].Text = strings.Replace(changed.Artifacts[0].Text, "current one", "edited current one", 1)
	changed.Artifacts[0].Digest = ""
	changed.Artifacts[0].Regions = nil
	changed.SourceDigest = ""
	preparedChanged, err := PrepareRequest(changed, changed.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateAdvice(preparedChanged, adviceFromOriginal); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("response from old source error = %v, want ErrHashMismatch", err)
	}

	// Even if an attacker copies the new envelope and artifact digest, an old
	// content-derived region identity cannot authorize a region in the new source.
	forged := completeConflictAdvice(preparedChanged,
		SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent, SubstituteCurrent,
	)
	for i := range forged.Suggestions {
		forged.Suggestions[i].Conflict.RegionID = original.Artifacts[0].Regions[i].ID
	}
	if _, err := ValidateAdvice(preparedChanged, forged); !errors.Is(err, ErrUnknownIdentity) {
		t.Fatalf("stale nested identity error = %v, want ErrUnknownIdentity", err)
	}
	forged.Artifacts[0].Digest = original.Artifacts[0].Digest
	if _, err := ValidateAdvice(preparedChanged, forged); !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("old artifact digest error = %v, want ErrHashMismatch", err)
	}
}

func TestDuplicateSourceArtifactIdentity(t *testing.T) {
	request := conflictRequest(t)
	duplicate := cloneArtifact(request.Artifacts[0])
	request.Artifacts = append(request.Artifacts, duplicate)
	request.SourceDigest = ""
	_, err := PrepareRequest(request, request.Limits)
	if !errors.Is(err, ErrDuplicateIdentity) {
		t.Fatalf("error = %v, want ErrDuplicateIdentity", err)
	}
}

func TestChangeSplitDuplicateAndMissingReferences(t *testing.T) {
	request := splitRequest(t)
	artifact := request.Artifacts[0]
	file := artifact.Files[0]
	valid := responseEnvelope(request)
	// Exclude the other complete file and assign the first file as a whole.
	valid.Suggestions = []Suggestion{{ID: "all", Summary: "all implementation", Split: &ChangeSplit{Changes: []ChangeRef{{ArtifactID: artifact.ID, FileID: file.ID}}}}}
	valid.Exclusions = []Exclusion{{ArtifactID: artifact.ID, FileID: artifact.Files[1].ID, Reason: "deliberate exclusion"}}
	if _, err := ValidateAdvice(request, valid); err != nil {
		t.Fatalf("valid split: %v", err)
	}

	duplicate := valid.Clone()
	duplicate.Suggestions = append(duplicate.Suggestions, Suggestion{ID: "repeat", Summary: "repeat", Split: &ChangeSplit{Changes: []ChangeRef{{ArtifactID: artifact.ID, FileID: file.ID}}}})
	if _, err := ValidateAdvice(request, duplicate); !errors.Is(err, ErrDuplicateIdentity) {
		t.Fatalf("duplicate file grouping error = %v, want ErrDuplicateIdentity", err)
	}

	missing := valid.Clone()
	missing.Exclusions = nil
	// Cover only the first hunk; the second alpha hunk and beta must be explicit.
	missing.Suggestions[0].Split.Changes = []ChangeRef{{ArtifactID: artifact.ID, FileID: file.ID, HunkID: file.Hunks[0].ID}}
	if _, err := ValidateAdvice(request, missing); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("partial split error = %v, want ErrMissingIdentity", err)
	}

	contextLine := valid.Clone()
	for _, line := range file.Hunks[0].Lines {
		if line.Kind == LineContext {
			contextLine.Suggestions[0].Split.Changes = []ChangeRef{{ArtifactID: artifact.ID, FileID: file.ID, HunkID: file.Hunks[0].ID, LineIDs: []string{line.ID}}}
			break
		}
	}
	if _, err := ValidateAdvice(request, contextLine); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("individual context line error = %v, want ErrInvalidProtocol", err)
	}

	missingFile := valid.Clone()
	missingFile.Suggestions[0].Split.Changes[0].FileID = ""
	if _, err := ValidateAdvice(request, missingFile); !errors.Is(err, ErrMissingIdentity) {
		t.Fatalf("empty file identity error = %v, want ErrMissingIdentity", err)
	}
}
