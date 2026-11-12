package workspace

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

// Kind identifies whether a workspace is on the local machine or reached over SSH.
type Kind string

const (
	Local Kind = "local"
	SSH   Kind = "ssh"
)

// Location identifies a workspace root. SSH locations use a stable profile ID,
// never credentials, to identify the remote machine.
type Location struct {
	Kind      Kind
	ProfileID string
	Root      string
}

// Parse creates a location from a local path or an ssh://profile/root URI.
func Parse(value string) (Location, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Location{}, errors.New("workspace location is empty")
	}
	if !strings.HasPrefix(value, "ssh://") {
		return Location{Kind: Local, Root: value}, nil
	}

	u, err := url.Parse(value)
	if err != nil {
		return Location{}, fmt.Errorf("parse SSH workspace location: %w", err)
	}
	if u.User != nil {
		return Location{}, errors.New("SSH workspace location must not contain user info")
	}
	if u.ForceQuery || u.RawQuery != "" {
		return Location{}, errors.New("SSH workspace location must not contain a query")
	}
	// url.Parse does not retain whether an empty fragment was present.
	if strings.Contains(value, "#") {
		return Location{}, errors.New("SSH workspace location must not contain a fragment")
	}
	if !validProfileID(u.Host) {
		return Location{}, errors.New("SSH workspace location has an invalid profile ID")
	}
	if u.Path == "" || !path.IsAbs(u.Path) {
		return Location{}, errors.New("SSH workspace root must be an absolute path")
	}
	if hasControl(u.Path) {
		return Location{}, errors.New("SSH workspace root contains a control character")
	}

	return Location{Kind: SSH, ProfileID: u.Host, Root: path.Clean(u.Path)}, nil
}

// Valid reports whether the location has the invariants required by its kind.
func (l Location) Valid() bool {
	switch l.Kind {
	case Local:
		return l.Root != ""
	case SSH:
		return validProfileID(l.ProfileID) && l.Root != "" && path.IsAbs(l.Root) && !hasControl(l.Root)
	default:
		return false
	}
}

// String returns a local root or a canonical SSH URI. Invalid locations have no
// string representation.
func (l Location) String() string {
	if !l.Valid() {
		return ""
	}
	if l.Kind == Local {
		return l.Root
	}
	return (&url.URL{
		Scheme: "ssh",
		Host:   l.ProfileID,
		Path:   path.Clean(l.Root),
	}).String()
}

// IsRemote reports whether the location is an SSH location.
func (l Location) IsRemote() bool {
	return l.Kind == SSH
}

// Base returns the final path element of a valid workspace root.
func (l Location) Base() string {
	if !l.Valid() {
		return ""
	}
	if l.Kind == SSH {
		return path.Base(l.Root)
	}
	return filepath.Base(l.Root)
}

func validProfileID(profile string) bool {
	if profile == "" || !isASCIIAlphaNumeric(profile[0]) {
		return false
	}
	for i := 0; i < len(profile); i++ {
		c := profile[i]
		if isASCIIAlphaNumeric(c) || c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

func isASCIIAlphaNumeric(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
