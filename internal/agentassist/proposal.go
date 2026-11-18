package agentassist

import "time"

const (
	// ProtocolVersion is emitted in requests and required in responses.
	ProtocolVersion = 1
	// DefaultHelperTimeout bounds both helper start and response generation.
	DefaultHelperTimeout = 2 * time.Minute
	// MaxHelperTimeout prevents an accidental effectively-unbounded helper.
	MaxHelperTimeout = 15 * time.Minute
)

// Kind determines both the source shape and the only permitted suggestion shape.
type Kind string

const (
	KindConflict    Kind = "conflict"
	KindChangeSplit Kind = "change_split"
)

// Substitution selects text for one complete conflict region. Custom is exact
// UTF-8 replacement text, not a patch and not a shell or Git expression.
type Substitution string

const (
	SubstituteCurrent  Substitution = "current"
	SubstituteBase     Substitution = "base"
	SubstituteIncoming Substitution = "incoming"
	SubstituteBoth     Substitution = "both"
	SubstituteCustom   Substitution = "custom"
)

// LineKind is the original role of a unified-diff line.
type LineKind string

const (
	LineContext  LineKind = "context"
	LineAdd      LineKind = "add"
	LineDelete   LineKind = "delete"
	LineMetadata LineKind = "metadata"
)

// Limits bounds source parsing, external JSON and result cardinality. A zero
// field selects its default. Callers using custom limits must pass the same
// request to Propose and BuildPreview.
type Limits struct {
	MaxArtifacts       int `json:"maxArtifacts"`
	MaxArtifactBytes   int `json:"maxArtifactBytes"`
	MaxTotalBytes      int `json:"maxTotalBytes"`
	MaxConflictRegions int `json:"maxConflictRegions"`
	MaxDiffFiles       int `json:"maxDiffFiles"`
	MaxDiffHunks       int `json:"maxDiffHunks"`
	MaxDiffLines       int `json:"maxDiffLines"`
	MaxSuggestions     int `json:"maxSuggestions"`
	MaxRequestBytes    int `json:"maxRequestBytes"`
	MaxResponseBytes   int `json:"maxResponseBytes"`
	MaxIdentityBytes   int `json:"maxIdentityBytes"`
	MaxSummaryBytes    int `json:"maxSummaryBytes"`
}

// DefaultLimits intentionally accepts practical text files while bounding
// duplicate structured representations in a helper request.
func DefaultLimits() Limits {
	return Limits{
		MaxArtifacts:       64,
		MaxArtifactBytes:   1 << 20,
		MaxTotalBytes:      4 << 20,
		MaxConflictRegions: 512,
		MaxDiffFiles:       512,
		MaxDiffHunks:       2048,
		MaxDiffLines:       20000,
		MaxSuggestions:     2048,
		MaxRequestBytes:    16 << 20,
		MaxResponseBytes:   8 << 20,
		MaxIdentityBytes:   512,
		MaxSummaryBytes:    4096,
	}
}

// Artifact is an immutable source snapshot after PrepareRequest returns. Path
// is a slash-separated repository-relative display path, never a helper path.
// Regions is derived for KindConflict and Files for KindChangeSplit.
type Artifact struct {
	ID         string           `json:"id"`
	Path       string           `json:"path,omitempty"`
	Digest     string           `json:"digest"`
	MarkerSize int              `json:"markerSize,omitempty"`
	Text       string           `json:"text"`
	Regions    []ConflictRegion `json:"regions,omitempty"`
	Files      []DiffFile       `json:"files,omitempty"`
}

// ConflictRegion identifies one marker-inclusive region. StartLine and EndLine
// are one-based and inclusive. Side text retains its exact line endings.
type ConflictRegion struct {
	ID            string `json:"id"`
	StartLine     int    `json:"startLine"`
	EndLine       int    `json:"endLine"`
	CurrentLabel  string `json:"currentLabel,omitempty"`
	BaseLabel     string `json:"baseLabel,omitempty"`
	IncomingLabel string `json:"incomingLabel,omitempty"`
	HasBase       bool   `json:"hasBase"`
	Current       string `json:"current"`
	Base          string `json:"base"`
	Incoming      string `json:"incoming"`
}

// DiffFile contains only identities and structure parsed from original text.
type DiffFile struct {
	ID      string     `json:"id"`
	Path    string     `json:"path"`
	OldPath string     `json:"oldPath,omitempty"`
	Hunks   []DiffHunk `json:"hunks"`
}

// DiffHunk identifies a complete original hunk, including its context.
type DiffHunk struct {
	ID       string     `json:"id"`
	Header   string     `json:"header"`
	OldStart int        `json:"oldStart"`
	OldLines int        `json:"oldLines"`
	NewStart int        `json:"newStart"`
	NewLines int        `json:"newLines"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine identifies one original line. Text excludes its diff prefix.
type DiffLine struct {
	ID      string   `json:"id"`
	Kind    LineKind `json:"kind"`
	Text    string   `json:"text"`
	OldLine int      `json:"oldLine,omitempty"`
	NewLine int      `json:"newLine,omitempty"`
}

// AdviceRequest is a versioned, self-contained and content-addressed source
// envelope. It contains no provider, model, credential or repository location.
type AdviceRequest struct {
	Version      int        `json:"version"`
	Kind         Kind       `json:"kind"`
	SourceDigest string     `json:"sourceDigest"`
	Limits       Limits     `json:"limits"`
	Artifacts    []Artifact `json:"artifacts"`
}

// ArtifactIdentity binds a response to the exact source bytes of one artifact.
type ArtifactIdentity struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// Advice is a versioned proposal bound to all request artifacts. Unknown and
// missing identities are errors rather than silently ignored advice.
type Advice struct {
	Version      int                `json:"version"`
	Kind         Kind               `json:"kind"`
	SourceDigest string             `json:"sourceDigest"`
	Artifacts    []ArtifactIdentity `json:"artifacts"`
	Suggestions  []Suggestion       `json:"suggestions"`
	Exclusions   []Exclusion        `json:"exclusions,omitempty"`
}

// Suggestion has exactly one kind-specific payload. For a change split, one
// suggestion is one named group, not an independently applicable patch.
type Suggestion struct {
	ID       string              `json:"id"`
	Summary  string              `json:"summary"`
	Conflict *ConflictSuggestion `json:"conflict,omitempty"`
	Split    *ChangeSplit        `json:"split,omitempty"`
}

// ConflictSuggestion resolves one and only one original conflict region.
type ConflictSuggestion struct {
	ArtifactID   string       `json:"artifactId"`
	RegionID     string       `json:"regionId"`
	Substitution Substitution `json:"substitution"`
	Custom       string       `json:"custom,omitempty"`
}

// ChangeSplit groups original diff identities. It includes no patch, content
// replacement, destination revision, or repository operation.
type ChangeSplit struct {
	Changes []ChangeRef `json:"changes"`
}

// ChangeRef selects a whole file (no HunkID), a whole hunk (no LineIDs), or
// explicitly named added/deleted lines from a hunk.
type ChangeRef struct {
	ArtifactID string   `json:"artifactId"`
	FileID     string   `json:"fileId"`
	HunkID     string   `json:"hunkId,omitempty"`
	LineIDs    []string `json:"lineIds,omitempty"`
}

// Exclusion explicitly accounts for an artifact or a nested identity that the
// advisor does not group or resolve. Artifact-only form excludes everything in
// that artifact. Reason is required and intended for later display in a UI.
type Exclusion struct {
	ArtifactID string `json:"artifactId"`
	RegionID   string `json:"regionId,omitempty"`
	FileID     string `json:"fileId,omitempty"`
	HunkID     string `json:"hunkId,omitempty"`
	LineID     string `json:"lineId,omitempty"`
	Reason     string `json:"reason"`
}

// NewConflictArtifact creates an unsealed source artifact with standard Git
// conflict markers. PrepareRequest derives its content digest and regions.
func NewConflictArtifact(path, text string) Artifact {
	return Artifact{Path: path, Text: text, MarkerSize: 7}
}

// NewDiffArtifact creates an unsealed unified-diff artifact. PrepareRequest
// rejects binary patches and derives every file, hunk and line identity.
func NewDiffArtifact(diff string) Artifact {
	return Artifact{Text: diff}
}

// Clone returns a request whose slices share no mutable state with r.
func (r AdviceRequest) Clone() AdviceRequest {
	clone := r
	clone.Artifacts = make([]Artifact, len(r.Artifacts))
	for i := range r.Artifacts {
		clone.Artifacts[i] = cloneArtifact(r.Artifacts[i])
	}
	return clone
}

func cloneArtifact(artifact Artifact) Artifact {
	clone := artifact
	clone.Regions = append([]ConflictRegion(nil), artifact.Regions...)
	if artifact.Files != nil {
		clone.Files = make([]DiffFile, len(artifact.Files))
		for i := range artifact.Files {
			clone.Files[i] = cloneDiffFile(artifact.Files[i])
		}
	}
	return clone
}

func cloneDiffFile(file DiffFile) DiffFile {
	clone := file
	if file.Hunks != nil {
		clone.Hunks = make([]DiffHunk, len(file.Hunks))
		for i := range file.Hunks {
			clone.Hunks[i] = cloneDiffHunk(file.Hunks[i])
		}
	}
	return clone
}

func cloneDiffHunk(hunk DiffHunk) DiffHunk {
	clone := hunk
	clone.Lines = append([]DiffLine(nil), hunk.Lines...)
	return clone
}

// Clone returns advice whose slices and pointers share no mutable state with a.
func (a Advice) Clone() Advice {
	clone := a
	clone.Artifacts = append([]ArtifactIdentity(nil), a.Artifacts...)
	clone.Suggestions = make([]Suggestion, len(a.Suggestions))
	for i, suggestion := range a.Suggestions {
		clone.Suggestions[i] = suggestion
		if suggestion.Conflict != nil {
			conflict := *suggestion.Conflict
			clone.Suggestions[i].Conflict = &conflict
		}
		if suggestion.Split != nil {
			split := ChangeSplit{Changes: make([]ChangeRef, len(suggestion.Split.Changes))}
			for j, change := range suggestion.Split.Changes {
				split.Changes[j] = change
				split.Changes[j].LineIDs = append([]string(nil), change.LineIDs...)
			}
			clone.Suggestions[i].Split = &split
		}
	}
	clone.Exclusions = append([]Exclusion(nil), a.Exclusions...)
	return clone
}
