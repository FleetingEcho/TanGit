package repository

import (
	"errors"
	"regexp"
	"strings"
)

const redactedSecret = "[redacted]"

var (
	urlPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s<>"']+`)
	// Credentials in URL queries are less common than userinfo, but CI and
	// forge URLs often carry tokens this way. Keep the surrounding URL useful
	// while removing every value whose key says that it is a credential.
	urlQuerySecretPattern       = regexp.MustCompile(`(?i)([?&](?:access[_-]?token|auth|api[_-]?key|awsaccesskeyid|client[_-]?secret|password|passwd|private[_-]?token|sig|signature|token|x-amz-credential|x-amz-security-token|x-amz-signature|x-goog-credential|x-goog-signature)=)[^&#\s]*`)
	credentialFlagPattern       = regexp.MustCompile(`(?i)(--(?:access[_-]?token|api[_-]?key|auth|client[_-]?secret|password|passphrase|private[_-]?token|token)\s+)(?:"[^"]*"|'[^']*'|[^\s]+)`)
	credentialAssignmentPattern = regexp.MustCompile(`(?i)\b((?:access[_-]?token|api[_-]?key|client[_-]?secret|password|passwd|passphrase|private[_-]?key|private[_-]?token|secret|token)\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)
	authorizationPattern        = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+(?:\s+[^\s,;]+)?)`)
	authSchemePattern           = regexp.MustCompile(`(?i)\b(basic|bearer)\s+[a-z0-9._~+/=-]+`)
)

// ErrCredentialsInURL rejects newly supplied repository URLs that would put a
// password or token in .git/config or .gitmodules. Existing credential-bearing
// URLs remain usable, but are redacted everywhere TanGit presents them.
var ErrCredentialsInURL = errors.New("repository URL contains credentials; use a Git credential helper or SSH agent instead")

// RedactSecrets removes credentials from commands, Git output, and errors.
// Redaction is deliberately conservative for fields named as credentials: a
// little lost diagnostic context is preferable to retaining a usable secret.
func RedactSecrets(text string) string {
	if text == "" {
		return ""
	}
	text = urlPattern.ReplaceAllStringFunc(text, redactURL)
	text = credentialFlagPattern.ReplaceAllString(text, `${1}`+redactedSecret)
	text = credentialAssignmentPattern.ReplaceAllString(text, `${1}`+redactedSecret)
	text = authorizationPattern.ReplaceAllString(text, `${1}`+redactedSecret)
	text = authSchemePattern.ReplaceAllString(text, `${1} `+redactedSecret)
	return text
}

// redactURL masks userinfo without parsing and re-serializing the URL. Git
// accepts URLs that net/url rejects, and diagnostics should retain their
// original spelling wherever no secret is present.
func redactURL(raw string) string {
	schemeEnd := strings.Index(raw, "://")
	if schemeEnd >= 0 {
		authorityStart := schemeEnd + 3
		authorityEnd := len(raw)
		if end := strings.IndexAny(raw[authorityStart:], "/?#"); end >= 0 {
			authorityEnd = authorityStart + end
		}
		authority := raw[authorityStart:authorityEnd]
		if at := strings.LastIndex(authority, "@"); at >= 0 {
			raw = raw[:authorityStart] + redactedSecret + raw[authorityStart+at:]
		}
	}
	return urlQuerySecretPattern.ReplaceAllString(raw, `${1}`+redactedSecret)
}

func redactURLs(urls []string) []string {
	if urls == nil {
		return nil
	}
	redacted := make([]string, len(urls))
	for i, raw := range urls {
		redacted[i] = RedactSecrets(raw)
	}
	return redacted
}

// urlContainsCredentials reports whether accepting raw would newly persist a
// secret. It recognizes URL userinfo textually so malformed but still
// dangerous input cannot bypass validation through a parser error.
func urlContainsCredentials(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw != RedactSecrets(raw) {
		return true
	}
	return false
}
