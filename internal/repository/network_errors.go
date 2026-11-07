package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Network failures have stable categories so the UI can offer the right next
// step instead of asking the user to interpret Git's wording.
var (
	ErrAuthentication      = errors.New("authentication failed; check your Git credentials or SSH agent")
	ErrNoUpstream          = errors.New("branch has no upstream")
	ErrPushRejected        = errors.New("push rejected; fetch and integrate the remote changes first")
	ErrPushLeaseRejected   = errors.New("force push with lease rejected; the remote changed since your last fetch")
	ErrPromptCancelled     = errors.New("credential prompt cancelled")
	ErrHostKeyVerification = errors.New("SSH host key verification failed")
)

// safeNetworkError retains errors.Is categories while ensuring an underlying
// Git error cannot carry a credential-bearing URL into the UI. The original
// error is intentionally not wrapped: its text can contain the remote secret.
func safeNetworkError(err error, forceWithLease bool) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	for _, kind := range []error{
		ErrAuthentication, ErrNoUpstream, ErrPushRejected, ErrPushLeaseRejected,
		ErrPromptCancelled, ErrHostKeyVerification,
	} {
		if errors.Is(err, kind) {
			return err
		}
	}

	message := RedactSecrets(err.Error())
	lower := strings.ToLower(message)
	kind := error(nil)
	pushRejected := containsAny(lower,
		"failed to push some refs", "non-fast-forward", "fetch first",
		"[remote rejected]", "[rejected]", "stale info",
	)
	switch {
	case containsAny(lower,
		"host key verification failed", "remote host identification has changed",
		"the authenticity of host", "no matching host key type",
		"host key algorithm", "changed host key",
	):
		kind = ErrHostKeyVerification
	case errors.Is(err, ErrPromptCancelled) || containsAny(lower,
		"credential prompt cancelled", "credential prompt canceled",
		"authentication prompt cancelled", "authentication prompt canceled",
		"prompt was cancelled", "prompt was canceled",
	):
		kind = ErrPromptCancelled
	case containsAny(lower,
		"authentication failed", "authentication required", "could not read username",
		"could not read password", "invalid username or password", "invalid username or token",
		"permission denied (publickey", "permission denied, please try again",
		"terminal prompts disabled", "http basic: access denied",
	):
		kind = ErrAuthentication
	case forceWithLease && pushRejected && containsAny(lower, "stale info", "non-fast-forward", "fetch first", "failed to push some refs"):
		kind = ErrPushLeaseRejected
	case pushRejected:
		kind = ErrPushRejected
	}
	if kind == nil {
		return errors.New(message)
	}
	return fmt.Errorf("%w: %s", kind, message)
}

func containsAny(text string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(text, fragment) {
			return true
		}
	}
	return false
}
