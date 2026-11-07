package sshworkspace

import (
	"errors"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
	"unicode"
)

const DefaultPort = 22

// AuthMode selects the single authentication method used for a profile.
type AuthMode string

const (
	AuthAgent    AuthMode = "agent"
	AuthKey      AuthMode = "key"
	AuthPassword AuthMode = "password"
)

// Profile describes a remote workspace without embedding any secrets.
// PrivateKeyPath is a path on the local machine. DefaultRoot is a POSIX path on
// the remote machine and may be empty, "~", "~/...", or an absolute path.
type Profile struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Host           string   `json:"host"`
	Username       string   `json:"username"`
	Port           int      `json:"port"`
	AuthMode       AuthMode `json:"authMode"`
	PrivateKeyPath string   `json:"privateKeyPath,omitempty"`
	DefaultRoot    string   `json:"defaultRoot,omitempty"`
}

// Validate checks the stable workspace identity and connection fields. Port
// zero means DefaultPort. Display Name is optional.
func (p Profile) Validate() error {
	if !ValidProfileID(p.ID) {
		return fmt.Errorf("invalid SSH profile ID %q", p.ID)
	}
	if err := validateHost(p.Host); err != nil {
		return err
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("invalid SSH port %d", p.Port)
	}
	if p.Username == "" || strings.IndexFunc(p.Username, unicode.IsControl) >= 0 || strings.ContainsAny(p.Username, " \t\r\n") {
		return errors.New("SSH username must be nonempty and contain no whitespace or control characters")
	}
	switch p.AuthMode {
	case AuthAgent, AuthPassword:
	case AuthKey:
		if p.PrivateKeyPath == "" {
			return errors.New("private-key authentication requires a private key path")
		}
	default:
		return fmt.Errorf("unsupported SSH auth mode %q", p.AuthMode)
	}
	if p.AuthMode != AuthKey && p.PrivateKeyPath != "" {
		return errors.New("private key path is only valid with private-key authentication")
	}
	if err := validateDefaultRoot(p.DefaultRoot); err != nil {
		return err
	}
	return nil
}

// Address returns host:port suitable for a network dialer.
func (p Profile) Address() string {
	port := p.Port
	if port == 0 {
		port = DefaultPort
	}
	host := p.Host
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// ValidProfileID reports whether id is suitable for workspace identity and
// secret references. IDs begin with an ASCII letter or digit and otherwise
// contain only ASCII letters, digits, '.', '_' or '-'.
func ValidProfileID(id string) bool {
	if id == "" || !isASCIIAlphaNumeric(id[0]) {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		if !isASCIIAlphaNumeric(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func validateHost(host string) error {
	if host == "" || host != strings.TrimSpace(host) {
		return errors.New("SSH host must be nonempty and have no surrounding whitespace")
	}
	if strings.IndexFunc(host, unicode.IsControl) >= 0 || strings.ContainsAny(host, " \t\r\n/@") {
		return fmt.Errorf("invalid SSH host %q", host)
	}
	unbracketed := host
	if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
		if len(host) < 2 || !strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]") {
			return fmt.Errorf("invalid bracketed SSH host %q", host)
		}
		unbracketed = host[1 : len(host)-1]
	}
	if unbracketed == "" || strings.ContainsAny(unbracketed, "[]") {
		return fmt.Errorf("invalid SSH host %q", host)
	}
	if strings.Contains(unbracketed, ":") && net.ParseIP(unbracketed) == nil {
		return fmt.Errorf("SSH host must not include a port: %q", host)
	}
	return nil
}

func validateDefaultRoot(root string) error {
	if root == "" {
		return nil
	}
	if strings.ContainsRune(root, '\x00') || strings.IndexFunc(root, unicode.IsControl) >= 0 {
		return errors.New("SSH default root contains a control character")
	}
	if root == "~" || strings.HasPrefix(root, "~/") || path.IsAbs(root) {
		return nil
	}
	return errors.New("SSH default root must be absolute or home-relative")
}
