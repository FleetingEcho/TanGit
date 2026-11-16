package repository

import (
	"errors"
	"fmt"
	"strings"
)

// RemoteRefEffect records a server-side outcome, never merely a local
// remote-tracking ref guess. Empty OID means that the ref did not exist.
// Remote names, rather than credential-bearing URLs, are retained in journals.
type RemoteRefEffect struct {
	Remote    string `json:"remote"`
	RefName   string `json:"ref_name"`
	BeforeOID string `json:"before_oid,omitempty"`
	AfterOID  string `json:"after_oid,omitempty"`
}

// RemoteCompensation is a review plan, not an authorization and not a claim
// that Undo can reverse a server. A caller must obtain explicit confirmation,
// re-read the remote ref, and use expected-old-value protection when acting.
type RemoteCompensation struct {
	Remote               string
	RefName              string
	ExpectedCurrentOID   string
	TargetOID            string
	Delete               bool
	RequiresConfirmation bool
}

var ErrRemoteCompensationRequired = errors.New("remote effects require an explicitly confirmed compensating action")

func remoteEffectOID(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if (len(value) == 40 || len(value) == 64) && strings.Trim(value, "0") == "" {
		return "", nil
	}
	if !fileRecoveryOID(value) {
		return "", fmt.Errorf("remote effect contains an invalid object id")
	}
	return value, nil
}

func (r OperationRecord) RemoteCompensations() ([]RemoteCompensation, error) {
	plans := make([]RemoteCompensation, 0, len(r.RemoteEffects))
	seen := make(map[string]bool)
	for _, effect := range r.RemoteEffects {
		before, err := remoteEffectOID(effect.BeforeOID)
		if err != nil {
			return nil, err
		}
		after, err := remoteEffectOID(effect.AfterOID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(effect.Remote) == "" || effect.Remote != strings.TrimSpace(effect.Remote) || strings.ContainsAny(effect.Remote, "\x00\r\n") ||
			!strings.HasPrefix(effect.RefName, "refs/") || strings.ContainsAny(effect.RefName, "\x00\r\n") || before == after || before == "" && after == "" {
			return nil, fmt.Errorf("remote effect is incomplete or malformed")
		}
		key := effect.Remote + "\x00" + effect.RefName
		if seen[key] {
			return nil, fmt.Errorf("remote effect for %s on %s is duplicated", effect.RefName, effect.Remote)
		}
		seen[key] = true
		plans = append(plans, RemoteCompensation{
			Remote: effect.Remote, RefName: effect.RefName,
			ExpectedCurrentOID: after, TargetOID: before, Delete: before == "", RequiresConfirmation: true,
		})
	}
	return plans, nil
}
