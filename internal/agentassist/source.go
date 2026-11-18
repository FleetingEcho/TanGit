package agentassist

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// NewRequest seals artifacts with the default limits and versioned protocol.
func NewRequest(kind Kind, artifacts []Artifact) (AdviceRequest, error) {
	return PrepareRequest(AdviceRequest{Kind: kind, Artifacts: artifacts}, DefaultLimits())
}

// PrepareRequest validates and owns a deep copy of request. Missing digests and
// identities are derived from source bytes; supplied ones must match exactly.
// Parsed source structure is always regenerated rather than trusted.
func PrepareRequest(request AdviceRequest, limits Limits) (AdviceRequest, error) {
	limits, err := normalizeLimits(limits)
	if err != nil {
		return AdviceRequest{}, err
	}
	request = request.Clone()
	if request.Version == 0 {
		request.Version = ProtocolVersion
	}
	if request.Version != ProtocolVersion {
		return AdviceRequest{}, protocolError("unsupported request version %d", request.Version)
	}
	if request.Kind != KindConflict && request.Kind != KindChangeSplit {
		return AdviceRequest{}, protocolError("unsupported advice kind %q", request.Kind)
	}
	request.Limits = limits
	if len(request.Artifacts) == 0 {
		return AdviceRequest{}, protocolError("at least one source artifact is required")
	}
	if len(request.Artifacts) > limits.MaxArtifacts {
		return AdviceRequest{}, limitError("%d artifacts exceed maximum %d", len(request.Artifacts), limits.MaxArtifacts)
	}

	seenArtifacts := make(map[string]int, len(request.Artifacts))
	totalBytes, totalRegions, totalFiles, totalHunks, totalLines := 0, 0, 0, 0, 0
	for i := range request.Artifacts {
		artifact := &request.Artifacts[i]
		if request.Kind == KindConflict && artifact.MarkerSize == 0 {
			artifact.MarkerSize = 7
		}
		if err := validateSourceArtifact(*artifact, request.Kind, limits); err != nil {
			return AdviceRequest{}, fmt.Errorf("artifact %d: %w", i, err)
		}
		if artifact.ID == "" {
			artifact.ID = deriveArtifactID(request.Kind, *artifact)
		}
		if err := validateIdentity(artifact.ID, "artifact identity", limits); err != nil {
			return AdviceRequest{}, fmt.Errorf("artifact %d: %w", i, err)
		}
		if previous, found := seenArtifacts[artifact.ID]; found {
			return AdviceRequest{}, duplicateError("artifact %q at indexes %d and %d", artifact.ID, previous, i)
		}
		seenArtifacts[artifact.ID] = i
		totalBytes += len(artifact.Text)
		if totalBytes > limits.MaxTotalBytes {
			return AdviceRequest{}, limitError("source exceeds %d bytes", limits.MaxTotalBytes)
		}
		digest := contentDigest(artifact.Text)
		if artifact.Digest != "" && artifact.Digest != digest {
			return AdviceRequest{}, hashError("artifact %q declares %q, actual digest is %q", artifact.ID, artifact.Digest, digest)
		}
		artifact.Digest = digest

		switch request.Kind {
		case KindConflict:
			regions, parseErr := parseConflictRegions(artifact.Text, artifact.MarkerSize, *artifact)
			if parseErr != nil {
				return AdviceRequest{}, fmt.Errorf("artifact %q: %w", artifact.ID, parseErr)
			}
			if len(regions) == 0 {
				return AdviceRequest{}, protocolError("conflict artifact %q has no complete regions", artifact.ID)
			}
			if err := checkSuppliedRegionIDs(artifact.ID, artifact.Regions, regions); err != nil {
				return AdviceRequest{}, err
			}
			artifact.Regions = regions
			totalRegions += len(regions)
			if totalRegions > limits.MaxConflictRegions {
				return AdviceRequest{}, limitError("conflict regions exceed maximum %d", limits.MaxConflictRegions)
			}
		case KindChangeSplit:
			files, parseErr := parseUnifiedDiff(artifact.Text, *artifact)
			if parseErr != nil {
				return AdviceRequest{}, fmt.Errorf("artifact %q: %w", artifact.ID, parseErr)
			}
			if err := checkSuppliedFileIDs(artifact.ID, artifact.Files, files); err != nil {
				return AdviceRequest{}, err
			}
			artifact.Files = files
			totalFiles += len(files)
			if totalFiles > limits.MaxDiffFiles {
				return AdviceRequest{}, limitError("diff files exceed maximum %d", limits.MaxDiffFiles)
			}
			for _, file := range files {
				totalHunks += len(file.Hunks)
				for _, hunk := range file.Hunks {
					totalLines += len(hunk.Lines)
				}
			}
			if totalHunks > limits.MaxDiffHunks {
				return AdviceRequest{}, limitError("diff hunks exceed maximum %d", limits.MaxDiffHunks)
			}
			if totalLines > limits.MaxDiffLines {
				return AdviceRequest{}, limitError("diff lines exceed maximum %d", limits.MaxDiffLines)
			}
		}
	}

	digest := sourceDigest(request.Kind, request.Artifacts)
	if request.SourceDigest != "" && request.SourceDigest != digest {
		return AdviceRequest{}, hashError("request declares source digest %q, actual digest is %q", request.SourceDigest, digest)
	}
	request.SourceDigest = digest
	return request, nil
}

func validateSourceArtifact(artifact Artifact, kind Kind, limits Limits) error {
	if len(artifact.Text) > limits.MaxArtifactBytes {
		return limitError("artifact has %d bytes, maximum is %d", len(artifact.Text), limits.MaxArtifactBytes)
	}
	if !utf8.ValidString(artifact.Text) || strings.IndexByte(artifact.Text, 0) >= 0 {
		return binaryError("artifact contains non-UTF-8 or NUL bytes")
	}
	if kind == KindConflict {
		if err := validateSourcePath(artifact.Path); err != nil {
			return err
		}
		if len(artifact.Files) != 0 {
			return protocolError("conflict artifact unexpectedly contains diff files")
		}
		if artifact.MarkerSize == 0 {
			return nil
		}
		if artifact.MarkerSize < 1 || artifact.MarkerSize > 64 {
			return protocolError("conflict marker size %d is outside 1..64", artifact.MarkerSize)
		}
		return nil
	}
	if artifact.Path != "" {
		return protocolError("diff artifact path must be empty; paths belong to original diff files")
	}
	if artifact.MarkerSize != 0 || len(artifact.Regions) != 0 {
		return protocolError("diff artifact unexpectedly contains conflict metadata")
	}
	return nil
}

// checkSuppliedRegionIDs rejects forged parsed metadata while allowing the
// convenient unsealed NewConflictArtifact form. Re-preparing a sealed request
// is idempotent.
func checkSuppliedRegionIDs(artifactID string, supplied, derived []ConflictRegion) error {
	if supplied == nil {
		return nil
	}
	if len(supplied) != len(derived) {
		return protocolError("artifact %q region metadata has %d entries, source has %d", artifactID, len(supplied), len(derived))
	}
	seen := make(map[string]struct{}, len(supplied))
	for i := range supplied {
		if _, found := seen[supplied[i].ID]; found {
			return duplicateError("region %q in artifact %q", supplied[i].ID, artifactID)
		}
		seen[supplied[i].ID] = struct{}{}
		if supplied[i].ID != derived[i].ID {
			return hashError("artifact %q region %d identity does not match source", artifactID, i)
		}
	}
	return nil
}

func checkSuppliedFileIDs(artifactID string, supplied, derived []DiffFile) error {
	if supplied == nil {
		return nil
	}
	if len(supplied) != len(derived) {
		return protocolError("artifact %q file metadata has %d entries, source has %d", artifactID, len(supplied), len(derived))
	}
	for i := range supplied {
		if supplied[i].ID != derived[i].ID {
			return hashError("artifact %q file %d identity does not match source", artifactID, i)
		}
		suppliedHunks := supplied[i].Hunks
		derivedHunks := derived[i].Hunks
		if suppliedHunks == nil && len(derivedHunks) == 0 {
			continue
		}
		if len(suppliedHunks) != len(derivedHunks) {
			return protocolError("file %q hunk metadata does not match source", supplied[i].ID)
		}
		seen := make(map[string]struct{}, len(suppliedHunks))
		for j := range suppliedHunks {
			if _, found := seen[suppliedHunks[j].ID]; found {
				return duplicateError("hunk %q in file %q", suppliedHunks[j].ID, supplied[i].ID)
			}
			seen[suppliedHunks[j].ID] = struct{}{}
			if suppliedHunks[j].ID != derivedHunks[j].ID {
				return hashError("file %q hunk %d identity does not match source", supplied[i].ID, j)
			}
			if suppliedHunks[j].Lines == nil && len(derivedHunks[j].Lines) == 0 {
				continue
			}
			if len(suppliedHunks[j].Lines) != len(derivedHunks[j].Lines) {
				return protocolError("hunk %q line metadata does not match source", suppliedHunks[j].ID)
			}
			seenLines := make(map[string]struct{}, len(suppliedHunks[j].Lines))
			for k := range suppliedHunks[j].Lines {
				lineID := suppliedHunks[j].Lines[k].ID
				if _, found := seenLines[lineID]; found {
					return duplicateError("line %q in hunk %q", lineID, suppliedHunks[j].ID)
				}
				seenLines[lineID] = struct{}{}
				if lineID != derivedHunks[j].Lines[k].ID {
					return hashError("hunk %q line %d identity does not match source", suppliedHunks[j].ID, k)
				}
			}
		}
	}
	return nil
}

func normalizeLimits(limits Limits) (Limits, error) {
	defaults := DefaultLimits()
	fields := []struct {
		name     string
		value    *int
		fallback int
		maximum  int
	}{
		{"maxArtifacts", &limits.MaxArtifacts, defaults.MaxArtifacts, 1024},
		{"maxArtifactBytes", &limits.MaxArtifactBytes, defaults.MaxArtifactBytes, 16 << 20},
		{"maxTotalBytes", &limits.MaxTotalBytes, defaults.MaxTotalBytes, 64 << 20},
		{"maxConflictRegions", &limits.MaxConflictRegions, defaults.MaxConflictRegions, 16384},
		{"maxDiffFiles", &limits.MaxDiffFiles, defaults.MaxDiffFiles, 8192},
		{"maxDiffHunks", &limits.MaxDiffHunks, defaults.MaxDiffHunks, 32768},
		{"maxDiffLines", &limits.MaxDiffLines, defaults.MaxDiffLines, 262144},
		{"maxSuggestions", &limits.MaxSuggestions, defaults.MaxSuggestions, 32768},
		{"maxRequestBytes", &limits.MaxRequestBytes, defaults.MaxRequestBytes, 64 << 20},
		{"maxResponseBytes", &limits.MaxResponseBytes, defaults.MaxResponseBytes, 64 << 20},
		{"maxIdentityBytes", &limits.MaxIdentityBytes, defaults.MaxIdentityBytes, 4096},
		{"maxSummaryBytes", &limits.MaxSummaryBytes, defaults.MaxSummaryBytes, 65536},
	}
	for _, field := range fields {
		if *field.value == 0 {
			*field.value = field.fallback
		}
		if *field.value < 0 || *field.value > field.maximum {
			return Limits{}, limitError("%s value %d is outside 1..%d", field.name, *field.value, field.maximum)
		}
	}
	return limits, nil
}

func validateSourcePath(value string) error {
	if value == "" || strings.TrimSpace(value) == "" {
		return protocolError("source path is required")
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "\\") || strings.Contains(value, "\\") {
		return protocolError("source path %q must be a portable relative slash path", value)
	}
	if clean := path.Clean(value); clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return protocolError("source path %q escapes its source root", value)
	}
	if err := validateText(value, "source path", 4096); err != nil {
		return err
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return protocolError("source path contains a control character")
		}
	}
	return nil
}

func validateIdentity(value, role string, limits Limits) error {
	if value == "" {
		return missingError("%s is required", role)
	}
	if err := validateText(value, role, limits.MaxIdentityBytes); err != nil {
		return err
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return protocolError("%s contains a control character", role)
		}
	}
	return nil
}

func validateText(value, role string, maximum int) error {
	if len(value) > maximum {
		return limitError("%s exceeds %d bytes", role, maximum)
	}
	if !utf8.ValidString(value) {
		return protocolError("%s is not UTF-8", role)
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return protocolError("%s contains a control character", role)
		}
	}
	return nil
}

func contentDigest(text string) string {
	digest := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func deriveArtifactID(kind Kind, artifact Artifact) string {
	return identityDigest("artifact", string(kind), artifact.Path, fmt.Sprint(artifact.MarkerSize), artifact.Text)
}

func identityDigest(prefix string, parts ...string) string {
	hash := sha256.New()
	hashString(hash, prefix)
	for _, part := range parts {
		hashString(hash, part)
	}
	return prefix + ":" + hex.EncodeToString(hash.Sum(nil))
}

func sourceDigest(kind Kind, artifacts []Artifact) string {
	hash := sha256.New()
	hashString(hash, "agentassist-source-v1")
	hashString(hash, string(kind))
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(artifacts)))
	_, _ = hash.Write(count[:])
	for _, artifact := range artifacts {
		hashString(hash, artifact.ID)
		hashString(hash, artifact.Path)
		hashString(hash, fmt.Sprint(artifact.MarkerSize))
		hashString(hash, contentDigest(artifact.Text))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func hashString(hash hash.Hash, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = hash.Write(size[:])
	_, _ = hash.Write([]byte(value))
}

func protocolError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidProtocol, fmt.Sprintf(format, args...))
}

func hashError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrHashMismatch, fmt.Sprintf(format, args...))
}

func duplicateError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrDuplicateIdentity, fmt.Sprintf(format, args...))
}

func missingError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMissingIdentity, fmt.Sprintf(format, args...))
}

func unknownError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnknownIdentity, fmt.Sprintf(format, args...))
}

func binaryError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBinaryArtifact, fmt.Sprintf(format, args...))
}

func limitError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrLimitExceeded, fmt.Sprintf(format, args...))
}

func sourceChangedError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrSourceChanged, fmt.Sprintf(format, args...))
}

func unsafeCommandError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsafeCommand, fmt.Sprintf(format, args...))
}
