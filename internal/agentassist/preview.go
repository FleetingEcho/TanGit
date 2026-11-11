package agentassist

import (
	"bytes"
	"fmt"
	"strings"
)

// Preview is a display-ready proposal plus private, digest-checked copy bytes.
// Mutating its public display slices cannot change what ResolvedCopy returns.
type Preview struct {
	Version      int
	Kind         Kind
	SourceDigest string
	Artifacts    []ConflictPreview
	Splits       []SplitPreview
	Exclusions   []Exclusion

	limits       Limits
	sourceDigest string
	copies       map[string]previewCopy
}

// ConflictPreview preserves exclusions and records which original regions the
// accepted suggestion would replace. Complete means no region was excluded.
type ConflictPreview struct {
	ArtifactID string
	Path       string
	Text       string
	Complete   bool
	Regions    []ConflictRegionPreview
}

// ConflictRegionPreview describes one original region's disposition.
type ConflictRegionPreview struct {
	RegionID     string
	StartLine    int
	EndLine      int
	SuggestionID string
	Summary      string
	Substitution Substitution
	Custom       string
	Excluded     bool
	Reason       string
}

// SplitPreview is one summary-bearing change group in deterministic source order.
type SplitPreview struct {
	SuggestionID string
	Summary      string
	Files        []SplitPreviewFile
}

// SplitPreviewFile contains only original file and hunk identities and text.
type SplitPreviewFile struct {
	ArtifactID string
	FileID     string
	Path       string
	OldPath    string
	Hunks      []SplitPreviewHunk
}

// SplitPreviewHunk contains all original lines for a whole-hunk selection, or
// exactly the selected added/deleted lines for a line-level selection.
type SplitPreviewHunk struct {
	HunkID string
	Header string
	Lines  []DiffLine
}

type previewCopy struct {
	artifactDigest string
	resultDigest   string
	complete       bool
	data           []byte
}

// BuildPreview validates request and advice before doing any substitution. It
// performs no filesystem or Git operations. Conflict text is assembled from
// exact source slices; split previews contain identities, never arbitrary patch.
func BuildPreview(request AdviceRequest, advice Advice) (Preview, error) {
	prepared, err := PrepareRequest(request, request.Limits)
	if err != nil {
		return Preview{}, err
	}
	validated, err := validatePreparedAdvice(prepared, advice)
	if err != nil {
		return Preview{}, err
	}
	preview := Preview{
		Version: ProtocolVersion, Kind: prepared.Kind, SourceDigest: prepared.SourceDigest,
		Exclusions: append([]Exclusion(nil), validated.Exclusions...),
		limits:     prepared.Limits, sourceDigest: prepared.SourceDigest, copies: map[string]previewCopy{},
	}
	index, err := indexSource(prepared)
	if err != nil {
		return Preview{}, err
	}
	switch prepared.Kind {
	case KindConflict:
		err = buildConflictPreview(index, validated, &preview)
	case KindChangeSplit:
		err = buildSplitPreview(prepared, validated, &preview)
	}
	if err != nil {
		return Preview{}, err
	}
	return preview, nil
}

type regionDisposition struct {
	suggestion *Suggestion
	exclusion  *Exclusion
}

func buildConflictPreview(index *indexedSource, advice Advice, preview *Preview) error {
	byArtifact := make(map[string]map[string]regionDisposition, len(index.artifacts))
	for artifactID, artifact := range index.artifacts {
		for regionID := range artifact.regions {
			if byArtifact[artifactID] == nil {
				byArtifact[artifactID] = map[string]regionDisposition{}
			}
			byArtifact[artifactID][regionID] = regionDisposition{}
		}
	}
	for i := range advice.Suggestions {
		suggestion := &advice.Suggestions[i]
		proposal := suggestion.Conflict
		disposition := byArtifact[proposal.ArtifactID][proposal.RegionID]
		disposition.suggestion = suggestion
		byArtifact[proposal.ArtifactID][proposal.RegionID] = disposition
	}
	for i := range advice.Exclusions {
		exclusion := &advice.Exclusions[i]
		if exclusion.RegionID != "" {
			disposition := byArtifact[exclusion.ArtifactID][exclusion.RegionID]
			disposition.exclusion = exclusion
			byArtifact[exclusion.ArtifactID][exclusion.RegionID] = disposition
			continue
		}
		for regionID, disposition := range byArtifact[exclusion.ArtifactID] {
			disposition.exclusion = exclusion
			byArtifact[exclusion.ArtifactID][regionID] = disposition
		}
	}

	preview.Artifacts = make([]ConflictPreview, 0, len(index.request.Artifacts))
	for artifactIndex := range index.request.Artifacts {
		artifact := &index.request.Artifacts[artifactIndex]
		lines := splitSourceLines(artifact.Text)
		regions := byArtifact[artifact.ID]
		result := ConflictPreview{ArtifactID: artifact.ID, Path: artifact.Path, Complete: true, Regions: make([]ConflictRegionPreview, 0, len(artifact.Regions))}
		var output strings.Builder
		output.Grow(len(artifact.Text))
		position := 0
		for _, region := range artifact.Regions {
			disposition := regions[region.ID]
			item := ConflictRegionPreview{RegionID: region.ID, StartLine: region.StartLine, EndLine: region.EndLine}
			start := lines[region.StartLine-1].start
			end := lines[region.EndLine-1].end
			output.WriteString(artifact.Text[position:start])
			if disposition.suggestion != nil {
				suggestion := disposition.suggestion
				proposal := suggestion.Conflict
				item.SuggestionID, item.Summary = suggestion.ID, suggestion.Summary
				item.Substitution, item.Custom = proposal.Substitution, proposal.Custom
				replacement, err := conflictReplacement(&region, *proposal)
				if err != nil {
					return err
				}
				output.WriteString(replacement)
			} else {
				output.WriteString(artifact.Text[start:end])
				result.Complete = false
				item.Excluded = true
				if disposition.exclusion != nil {
					item.Reason = disposition.exclusion.Reason
				}
			}
			result.Regions = append(result.Regions, item)
			position = end
		}
		output.WriteString(artifact.Text[position:])
		if output.Len() > index.request.Limits.MaxArtifactBytes {
			return limitError("conflict preview for artifact %q exceeds %d bytes", artifact.ID, index.request.Limits.MaxArtifactBytes)
		}
		result.Text = output.String()
		data := []byte(result.Text)
		preview.copies[artifact.ID] = previewCopy{
			artifactDigest: artifact.Digest, resultDigest: contentDigest(result.Text), complete: result.Complete, data: bytes.Clone(data),
		}
		preview.Artifacts = append(preview.Artifacts, result)
	}
	return nil
}

func conflictReplacement(region *ConflictRegion, proposal ConflictSuggestion) (string, error) {
	switch proposal.Substitution {
	case SubstituteCurrent:
		return region.Current, nil
	case SubstituteBase:
		if !region.HasBase {
			return "", protocolError("region %q has no base substitution", region.ID)
		}
		return region.Base, nil
	case SubstituteIncoming:
		return region.Incoming, nil
	case SubstituteBoth:
		return region.Current + region.Incoming, nil
	case SubstituteCustom:
		return proposal.Custom, nil
	default:
		return "", protocolError("region %q has unsupported substitution %q", region.ID, proposal.Substitution)
	}
}

// ResolvedCopy performs a source-digest compare-and-swap check against current
// and returns an independent copy of the private approved result. The caller
// remains responsible for any explicit, non-Git destination it chooses to write.
// Partially excluded artifacts can be inspected but cannot be copied as complete.
func (p Preview) ResolvedCopy(artifactID string, current AdviceRequest) ([]byte, error) {
	if p.Kind != KindConflict {
		return nil, protocolError("resolved copies are available only for conflict previews")
	}
	if p.sourceDigest == "" || p.SourceDigest != p.sourceDigest {
		return nil, hashError("preview source identity was modified")
	}
	current = current.Clone()
	current.SourceDigest = ""
	for i := range current.Artifacts {
		current.Artifacts[i].Digest = ""
		current.Artifacts[i].Regions = nil
		current.Artifacts[i].Files = nil
	}
	preparedCurrent, err := PrepareRequest(current, p.limits)
	if err != nil {
		return nil, fmt.Errorf("compare current advice source: %w", err)
	}
	actual := preparedCurrent.SourceDigest
	if actual != p.sourceDigest {
		return nil, sourceChangedError("expected %s, current source is %s; request advice again", p.sourceDigest, actual)
	}
	copy, found := p.copies[artifactID]
	if !found {
		return nil, unknownError("preview artifact %q", artifactID)
	}
	artifact, found := indexArtifact(preparedCurrent, artifactID)
	if !found {
		return nil, unknownError("current artifact %q", artifactID)
	}
	if contentDigest(artifact.Text) != copy.artifactDigest {
		return nil, hashError("current artifact %q digest differs from the preview", artifactID)
	}
	if !copy.complete {
		return nil, fmt.Errorf("%w: artifact %q contains explicit exclusions", ErrIncompletePreview, artifactID)
	}
	if contentDigest(string(copy.data)) != copy.resultDigest {
		return nil, hashError("private preview result for artifact %q failed its integrity check", artifactID)
	}
	return bytes.Clone(copy.data), nil
}

func indexArtifact(request AdviceRequest, artifactID string) (Artifact, bool) {
	for _, artifact := range request.Artifacts {
		if artifact.ID == artifactID {
			return artifact, true
		}
	}
	return Artifact{}, false
}

type splitHunkSelection struct {
	whole   bool
	lineIDs map[string]struct{}
}

type splitFileSelection struct {
	whole bool
	hunks map[string]splitHunkSelection
}

func buildSplitPreview(request AdviceRequest, advice Advice, preview *Preview) error {
	preview.Splits = make([]SplitPreview, 0, len(advice.Suggestions))
	for _, suggestion := range advice.Suggestions {
		selection := map[string]map[string]splitFileSelection{}
		for _, change := range suggestion.Split.Changes {
			if selection[change.ArtifactID] == nil {
				selection[change.ArtifactID] = map[string]splitFileSelection{}
			}
			fileSelection, found := selection[change.ArtifactID][change.FileID]
			if !found {
				fileSelection = splitFileSelection{hunks: map[string]splitHunkSelection{}}
			}
			if change.HunkID == "" {
				fileSelection.whole = true
				selection[change.ArtifactID][change.FileID] = fileSelection
				continue
			}
			hunkSelection := fileSelection.hunks[change.HunkID]
			if len(change.LineIDs) == 0 {
				hunkSelection.whole = true
			} else {
				if hunkSelection.lineIDs == nil {
					hunkSelection.lineIDs = map[string]struct{}{}
				}
				for _, lineID := range change.LineIDs {
					hunkSelection.lineIDs[lineID] = struct{}{}
				}
			}
			fileSelection.hunks[change.HunkID] = hunkSelection
			selection[change.ArtifactID][change.FileID] = fileSelection
		}

		group := SplitPreview{SuggestionID: suggestion.ID, Summary: suggestion.Summary, Files: make([]SplitPreviewFile, 0)}
		for _, artifact := range request.Artifacts {
			artifactSelection := selection[artifact.ID]
			for _, file := range artifact.Files {
				fileSelection, found := artifactSelection[file.ID]
				if !found {
					continue
				}
				filePreview := SplitPreviewFile{
					ArtifactID: artifact.ID, FileID: file.ID, Path: file.Path, OldPath: file.OldPath,
					Hunks: make([]SplitPreviewHunk, 0),
				}
				for _, hunk := range file.Hunks {
					hunkSelection, selected := fileSelection.hunks[hunk.ID]
					if !fileSelection.whole && !selected {
						continue
					}
					hunkPreview := SplitPreviewHunk{HunkID: hunk.ID, Header: hunk.Header, Lines: make([]DiffLine, 0, len(hunk.Lines))}
					for _, line := range hunk.Lines {
						if fileSelection.whole || hunkSelection.whole {
							hunkPreview.Lines = append(hunkPreview.Lines, line)
							continue
						}
						if _, selected := hunkSelection.lineIDs[line.ID]; selected {
							hunkPreview.Lines = append(hunkPreview.Lines, line)
						}
					}
					filePreview.Hunks = append(filePreview.Hunks, hunkPreview)
				}
				group.Files = append(group.Files, filePreview)
			}
		}
		preview.Splits = append(preview.Splits, group)
	}
	return nil
}
