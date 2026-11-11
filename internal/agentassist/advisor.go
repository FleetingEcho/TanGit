// Package agentassist obtains and validates provider-neutral suggestions for
// conflict resolution and change splitting. It never opens a repository,
// invokes Git, stages changes, or applies a suggestion to a source file.
package agentassist

import (
	"context"
	"errors"
)

// Advisor is the only dependency a future UI needs for obtaining suggestions.
// Implementations must not mutate request or retain its mutable slices after
// Propose returns. Advice is only a proposal; it carries no Git write capability.
type Advisor interface {
	Propose(ctx context.Context, request AdviceRequest) (Advice, error)
}

var (
	// ErrInvalidProtocol identifies malformed or internally inconsistent input.
	ErrInvalidProtocol = errors.New("invalid agent advice protocol")
	// ErrSourceChanged identifies a compare-and-swap source digest mismatch.
	ErrSourceChanged = errors.New("agent advice source changed")
	// ErrHashMismatch identifies an artifact or response hash mismatch.
	ErrHashMismatch = errors.New("agent advice hash mismatch")
	// ErrDuplicateIdentity identifies an identity used more than once.
	ErrDuplicateIdentity = errors.New("duplicate agent advice identity")
	// ErrMissingIdentity identifies a required identity omitted from a response.
	ErrMissingIdentity = errors.New("missing agent advice identity")
	// ErrUnknownIdentity identifies a reference to an identity absent from the source.
	ErrUnknownIdentity = errors.New("unknown agent advice identity")
	// ErrBinaryArtifact identifies binary or non-UTF-8 source content.
	ErrBinaryArtifact = errors.New("binary agent advice artifacts are unsupported")
	// ErrLimitExceeded identifies a configured protocol or source size limit.
	ErrLimitExceeded = errors.New("agent advice limit exceeded")
	// ErrIncompletePreview identifies an attempt to copy a partially excluded artifact.
	ErrIncompletePreview = errors.New("agent advice preview is incomplete")
	// ErrUnsafeCommand identifies an invalid external helper command template.
	ErrUnsafeCommand = errors.New("unsafe agent advice command")
)
