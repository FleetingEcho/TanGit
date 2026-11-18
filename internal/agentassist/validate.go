package agentassist

import (
	"fmt"
	"strings"
)

type indexedSource struct {
	request   AdviceRequest
	artifacts map[string]*indexedArtifact
}

type indexedArtifact struct {
	artifact *Artifact
	regions  map[string]*ConflictRegion
	files    map[string]*indexedFile
}

type indexedFile struct {
	artifactID string
	file       *DiffFile
	hunks      map[string]*indexedHunk
}

type indexedHunk struct {
	file  *indexedFile
	hunk  *DiffHunk
	lines map[string]*DiffLine
}

func indexSource(request AdviceRequest) (*indexedSource, error) {
	index := &indexedSource{request: request, artifacts: make(map[string]*indexedArtifact, len(request.Artifacts))}
	for i := range request.Artifacts {
		artifact := &request.Artifacts[i]
		if _, found := index.artifacts[artifact.ID]; found {
			return nil, duplicateError("source artifact %q", artifact.ID)
		}
		entry := &indexedArtifact{artifact: artifact, regions: map[string]*ConflictRegion{}, files: map[string]*indexedFile{}}
		index.artifacts[artifact.ID] = entry
		for j := range artifact.Regions {
			region := &artifact.Regions[j]
			if _, found := entry.regions[region.ID]; found {
				return nil, duplicateError("source region %q", region.ID)
			}
			entry.regions[region.ID] = region
		}
		for j := range artifact.Files {
			file := &artifact.Files[j]
			if _, found := entry.files[file.ID]; found {
				return nil, duplicateError("source file %q", file.ID)
			}
			fileEntry := &indexedFile{artifactID: artifact.ID, file: file, hunks: map[string]*indexedHunk{}}
			entry.files[file.ID] = fileEntry
			for k := range file.Hunks {
				hunk := &file.Hunks[k]
				if _, found := fileEntry.hunks[hunk.ID]; found {
					return nil, duplicateError("source hunk %q", hunk.ID)
				}
				hunkEntry := &indexedHunk{file: fileEntry, hunk: hunk, lines: map[string]*DiffLine{}}
				fileEntry.hunks[hunk.ID] = hunkEntry
				for l := range hunk.Lines {
					line := &hunk.Lines[l]
					if _, found := hunkEntry.lines[line.ID]; found {
						return nil, duplicateError("source line %q", line.ID)
					}
					hunkEntry.lines[line.ID] = line
				}
			}
		}
	}
	return index, nil
}

// ValidateAdvice re-seals request and returns an independent validated copy of
// advice. Both kinds must account for every source identity through either a
// suggestion or an explicit exclusion.
func ValidateAdvice(request AdviceRequest, advice Advice) (Advice, error) {
	prepared, err := PrepareRequest(request, request.Limits)
	if err != nil {
		return Advice{}, err
	}
	return validatePreparedAdvice(prepared, advice)
}

func validatePreparedAdvice(request AdviceRequest, advice Advice) (Advice, error) {
	limits := request.Limits
	if advice.Version != ProtocolVersion {
		return Advice{}, protocolError("unsupported response version %d", advice.Version)
	}
	if advice.Kind != request.Kind {
		return Advice{}, protocolError("response kind %q does not match request kind %q", advice.Kind, request.Kind)
	}
	if advice.SourceDigest != request.SourceDigest {
		return Advice{}, hashError("response source digest %q does not match request %q", advice.SourceDigest, request.SourceDigest)
	}
	if len(advice.Suggestions) > limits.MaxSuggestions {
		return Advice{}, limitError("response has %d suggestions, maximum is %d", len(advice.Suggestions), limits.MaxSuggestions)
	}
	if len(advice.Exclusions) > limits.MaxSuggestions {
		return Advice{}, limitError("response has %d exclusions, maximum is %d", len(advice.Exclusions), limits.MaxSuggestions)
	}
	if len(advice.Artifacts) > limits.MaxArtifacts {
		return Advice{}, limitError("response has %d artifact identities, maximum is %d", len(advice.Artifacts), limits.MaxArtifacts)
	}
	index, err := indexSource(request)
	if err != nil {
		return Advice{}, err
	}
	if err := validateResponseArtifacts(index, advice.Artifacts, limits); err != nil {
		return Advice{}, err
	}
	advice = advice.Clone()
	switch request.Kind {
	case KindConflict:
		err = validateConflictAdvice(index, advice)
	case KindChangeSplit:
		err = validateChangeSplitAdvice(index, advice)
	}
	if err != nil {
		return Advice{}, err
	}
	return advice, nil
}

func validateResponseArtifacts(index *indexedSource, identities []ArtifactIdentity, limits Limits) error {
	seen := make(map[string]struct{}, len(identities))
	for i, identity := range identities {
		if err := validateIdentity(identity.ID, fmt.Sprintf("response artifact %d identity", i), limits); err != nil {
			return err
		}
		if _, found := seen[identity.ID]; found {
			return duplicateError("response artifact %q", identity.ID)
		}
		seen[identity.ID] = struct{}{}
		artifact, found := index.artifacts[identity.ID]
		if !found {
			return unknownError("response artifact %q is not in the request", identity.ID)
		}
		if identity.Digest != artifact.artifact.Digest {
			return hashError("response artifact %q digest %q does not match source %q", identity.ID, identity.Digest, artifact.artifact.Digest)
		}
	}
	for id := range index.artifacts {
		if _, found := seen[id]; !found {
			return missingError("response does not identify source artifact %q", id)
		}
	}
	return nil
}

type coverageOwner int

const (
	coveredBySuggestion coverageOwner = iota + 1
	coveredByExclusion
)

type coverageUnit struct {
	key      string
	identity string
}

type coverage struct {
	units  map[string]coverageUnit
	owners map[string]coverageOwner
}

func newCoverage() *coverage {
	return &coverage{units: map[string]coverageUnit{}, owners: map[string]coverageOwner{}}
}

func (c *coverage) addAvailable(units []coverageUnit) {
	for _, unit := range units {
		c.units[unit.key] = unit
	}
}

func (c *coverage) cover(units []coverageUnit, owner coverageOwner) error {
	for _, unit := range units {
		if previous, found := c.owners[unit.key]; found {
			if previous == owner {
				return duplicateError("%s is covered more than once", unit.identity)
			}
			return duplicateError("%s is both suggested and excluded", unit.identity)
		}
		c.owners[unit.key] = owner
	}
	return nil
}

func (c *coverage) requireComplete() error {
	for key, unit := range c.units {
		if _, found := c.owners[key]; !found {
			return missingError("%s has neither a suggestion nor an explicit exclusion", unit.identity)
		}
	}
	return nil
}

func conflictUnit(artifactID, regionID string) coverageUnit {
	key := artifactID + "\x00region\x00" + regionID
	return coverageUnit{key: key, identity: fmt.Sprintf("conflict region %q in artifact %q", regionID, artifactID)}
}

func validateConflictAdvice(index *indexedSource, advice Advice) error {
	coverage := newCoverage()
	for _, artifact := range index.artifacts {
		units := make([]coverageUnit, 0, len(artifact.regions))
		for id := range artifact.regions {
			units = append(units, conflictUnit(artifact.artifact.ID, id))
		}
		coverage.addAvailable(units)
	}
	suggestionIDs := map[string]struct{}{}
	for i, suggestion := range advice.Suggestions {
		if err := validateSuggestionHeader(index.request.Limits, suggestion, false, suggestionIDs); err != nil {
			return fmt.Errorf("suggestion %d: %w", i, err)
		}
		if suggestion.Conflict == nil || suggestion.Split != nil {
			return protocolError("conflict suggestion %q must contain only a conflict payload", suggestion.ID)
		}
		proposal := suggestion.Conflict
		if err := validateIdentity(proposal.ArtifactID, "suggested artifact identity", index.request.Limits); err != nil {
			return err
		}
		if err := validateIdentity(proposal.RegionID, "suggested region identity", index.request.Limits); err != nil {
			return err
		}
		artifact, region, err := index.lookupRegion(proposal.ArtifactID, proposal.RegionID)
		if err != nil {
			return err
		}
		if len(proposal.Custom) > 0 && proposal.Substitution != SubstituteCustom {
			return protocolError("suggestion %q supplies custom text for %q substitution", suggestion.ID, proposal.Substitution)
		}
		switch proposal.Substitution {
		case SubstituteCurrent, SubstituteIncoming, SubstituteBoth:
		case SubstituteBase:
			if !region.HasBase {
				return protocolError("suggestion %q selects absent base text from region %q", suggestion.ID, proposal.RegionID)
			}
		case SubstituteCustom:
			if err := validateText(proposal.Custom, "custom substitution", index.request.Limits.MaxArtifactBytes); err != nil {
				return fmt.Errorf("suggestion %q: %w", suggestion.ID, err)
			}
		default:
			return protocolError("suggestion %q has unsupported substitution %q", suggestion.ID, proposal.Substitution)
		}
		if err := coverage.cover([]coverageUnit{conflictUnit(artifact.artifact.ID, region.ID)}, coveredBySuggestion); err != nil {
			return err
		}
	}
	for i, exclusion := range advice.Exclusions {
		units, err := conflictExclusionUnits(index, exclusion)
		if err != nil {
			return fmt.Errorf("exclusion %d: %w", i, err)
		}
		if err := coverage.cover(units, coveredByExclusion); err != nil {
			return err
		}
	}
	return coverage.requireComplete()
}

func validateSuggestionHeader(limits Limits, suggestion Suggestion, split bool, seen map[string]struct{}) error {
	if err := validateIdentity(suggestion.ID, "suggestion identity", limits); err != nil {
		return err
	}
	if _, found := seen[suggestion.ID]; found {
		return duplicateError("suggestion %q", suggestion.ID)
	}
	seen[suggestion.ID] = struct{}{}
	if split && strings.TrimSpace(suggestion.Summary) == "" {
		return protocolError("change split %q requires a summary", suggestion.ID)
	}
	return validateText(suggestion.Summary, "suggestion summary", limits.MaxSummaryBytes)
}

func conflictExclusionUnits(index *indexedSource, exclusion Exclusion) ([]coverageUnit, error) {
	if err := validateExclusion(index.request.Limits, exclusion); err != nil {
		return nil, err
	}
	if exclusion.FileID != "" || exclusion.HunkID != "" || exclusion.LineID != "" {
		return nil, protocolError("conflict exclusion cannot contain diff identities")
	}
	artifact, found := index.artifacts[exclusion.ArtifactID]
	if !found {
		return nil, unknownError("excluded artifact %q", exclusion.ArtifactID)
	}
	if exclusion.RegionID == "" {
		units := make([]coverageUnit, 0, len(artifact.regions))
		for id := range artifact.regions {
			units = append(units, conflictUnit(artifact.artifact.ID, id))
		}
		return units, nil
	}
	if _, found := artifact.regions[exclusion.RegionID]; !found {
		return nil, unknownError("excluded region %q in artifact %q", exclusion.RegionID, exclusion.ArtifactID)
	}
	return []coverageUnit{conflictUnit(artifact.artifact.ID, exclusion.RegionID)}, nil
}

func validateExclusion(limits Limits, exclusion Exclusion) error {
	if err := validateIdentity(exclusion.ArtifactID, "excluded artifact identity", limits); err != nil {
		return err
	}
	identities := []struct{ role, value string }{
		{"region", exclusion.RegionID}, {"file", exclusion.FileID}, {"hunk", exclusion.HunkID}, {"line", exclusion.LineID},
	}
	for _, identity := range identities {
		if identity.value != "" {
			if err := validateIdentity(identity.value, "excluded "+identity.role+" identity", limits); err != nil {
				return err
			}
		}
	}
	if strings.TrimSpace(exclusion.Reason) == "" {
		return protocolError("exclusion of artifact %q requires a reason", exclusion.ArtifactID)
	}
	return validateText(exclusion.Reason, "exclusion reason", limits.MaxSummaryBytes)
}

func validateChangeSplitAdvice(index *indexedSource, advice Advice) error {
	coverage := newCoverage()
	for artifactID, artifact := range index.artifacts {
		if len(artifact.files) == 0 {
			unit := coverageUnit{key: artifactID + "\x00artifact", identity: fmt.Sprintf("empty diff artifact %q", artifactID)}
			coverage.addAvailable([]coverageUnit{unit})
			continue
		}
		for _, file := range artifact.files {
			coverage.addAvailable(fileCoverageUnits(file))
		}
	}
	seenSuggestions := map[string]struct{}{}
	for i, suggestion := range advice.Suggestions {
		if err := validateSuggestionHeader(index.request.Limits, suggestion, true, seenSuggestions); err != nil {
			return fmt.Errorf("suggestion %d: %w", i, err)
		}
		if suggestion.Split == nil || suggestion.Conflict != nil {
			return protocolError("change split %q must contain only a split payload", suggestion.ID)
		}
		if len(suggestion.Split.Changes) == 0 {
			return protocolError("change split %q has no original diff references", suggestion.ID)
		}
		for _, change := range suggestion.Split.Changes {
			units, err := index.unitsForChange(change)
			if err != nil {
				return fmt.Errorf("change split %q: %w", suggestion.ID, err)
			}
			if err := coverage.cover(units, coveredBySuggestion); err != nil {
				return err
			}
		}
	}
	for i, exclusion := range advice.Exclusions {
		units, err := index.changeExclusionUnits(exclusion)
		if err != nil {
			return fmt.Errorf("exclusion %d: %w", i, err)
		}
		if err := coverage.cover(units, coveredByExclusion); err != nil {
			return err
		}
	}
	return coverage.requireComplete()
}

func lineUnit(artifactID string, line *DiffLine) coverageUnit {
	return coverageUnit{key: artifactID + "\x00line\x00" + line.ID, identity: fmt.Sprintf("diff line %q in artifact %q", line.ID, artifactID)}
}

func fileUnit(file *indexedFile) coverageUnit {
	return coverageUnit{key: file.artifactID + "\x00file\x00" + file.file.ID, identity: fmt.Sprintf("diff file %q (%s)", file.file.ID, file.file.Path)}
}

func fileCoverageUnits(file *indexedFile) []coverageUnit {
	units := make([]coverageUnit, 0)
	for _, hunk := range file.hunks {
		for _, line := range hunk.lines {
			if line.Kind == LineAdd || line.Kind == LineDelete {
				units = append(units, lineUnit(file.artifactID, line))
			}
		}
	}
	if len(units) == 0 {
		return []coverageUnit{fileUnit(file)}
	}
	return units
}

func (index *indexedSource) lookupRegion(artifactID, regionID string) (*indexedArtifact, *ConflictRegion, error) {
	artifact, found := index.artifacts[artifactID]
	if !found {
		return nil, nil, unknownError("suggested artifact %q", artifactID)
	}
	region, found := artifact.regions[regionID]
	if !found {
		return nil, nil, unknownError("suggested region %q in artifact %q", regionID, artifactID)
	}
	return artifact, region, nil
}

func (index *indexedSource) lookupFile(artifactID, fileID string) (*indexedFile, error) {
	artifact, found := index.artifacts[artifactID]
	if !found {
		return nil, unknownError("referenced artifact %q", artifactID)
	}
	file, found := artifact.files[fileID]
	if !found {
		return nil, unknownError("referenced file %q in artifact %q", fileID, artifactID)
	}
	return file, nil
}

func (index *indexedSource) unitsForChange(change ChangeRef) ([]coverageUnit, error) {
	if err := validateIdentity(change.ArtifactID, "referenced artifact identity", index.request.Limits); err != nil {
		return nil, err
	}
	if err := validateIdentity(change.FileID, "referenced file identity", index.request.Limits); err != nil {
		return nil, err
	}
	if change.HunkID != "" {
		if err := validateIdentity(change.HunkID, "referenced hunk identity", index.request.Limits); err != nil {
			return nil, err
		}
	}
	for _, lineID := range change.LineIDs {
		if err := validateIdentity(lineID, "referenced line identity", index.request.Limits); err != nil {
			return nil, err
		}
	}
	file, err := index.lookupFile(change.ArtifactID, change.FileID)
	if err != nil {
		return nil, err
	}
	if change.HunkID == "" {
		if len(change.LineIDs) != 0 {
			return nil, protocolError("file reference %q cannot contain line identities without a hunk", change.FileID)
		}
		return fileCoverageUnits(file), nil
	}
	hunk, found := file.hunks[change.HunkID]
	if !found {
		return nil, unknownError("referenced hunk %q in file %q", change.HunkID, change.FileID)
	}
	if len(change.LineIDs) == 0 {
		units := make([]coverageUnit, 0)
		for _, line := range hunk.lines {
			if line.Kind == LineAdd || line.Kind == LineDelete {
				units = append(units, lineUnit(file.artifactID, line))
			}
		}
		if len(units) == 0 {
			return nil, protocolError("hunk %q has no changed lines to group independently", change.HunkID)
		}
		return units, nil
	}
	units := make([]coverageUnit, 0, len(change.LineIDs))
	for _, lineID := range change.LineIDs {
		line, found := hunk.lines[lineID]
		if !found {
			return nil, unknownError("referenced line %q in hunk %q", lineID, change.HunkID)
		}
		if line.Kind != LineAdd && line.Kind != LineDelete {
			return nil, protocolError("line %q is %s; only added or deleted lines can be selected individually", lineID, line.Kind)
		}
		units = append(units, lineUnit(file.artifactID, line))
	}
	return units, nil
}

func (index *indexedSource) changeExclusionUnits(exclusion Exclusion) ([]coverageUnit, error) {
	if err := validateExclusion(index.request.Limits, exclusion); err != nil {
		return nil, err
	}
	if exclusion.RegionID != "" {
		return nil, protocolError("change exclusion cannot contain a conflict region")
	}
	artifact, found := index.artifacts[exclusion.ArtifactID]
	if !found {
		return nil, unknownError("excluded artifact %q", exclusion.ArtifactID)
	}
	if exclusion.FileID == "" {
		if exclusion.HunkID != "" || exclusion.LineID != "" {
			return nil, protocolError("artifact exclusion cannot contain a hunk or line without a file")
		}
		if len(artifact.files) == 0 {
			return []coverageUnit{{key: artifact.artifact.ID + "\x00artifact", identity: fmt.Sprintf("empty diff artifact %q", artifact.artifact.ID)}}, nil
		}
		units := make([]coverageUnit, 0)
		for _, file := range artifact.files {
			units = append(units, fileCoverageUnits(file)...)
		}
		return units, nil
	}
	change := ChangeRef{ArtifactID: exclusion.ArtifactID, FileID: exclusion.FileID, HunkID: exclusion.HunkID}
	if exclusion.LineID != "" {
		if exclusion.HunkID == "" {
			return nil, protocolError("line exclusion %q requires a hunk", exclusion.LineID)
		}
		change.LineIDs = []string{exclusion.LineID}
	}
	return index.unitsForChange(change)
}
