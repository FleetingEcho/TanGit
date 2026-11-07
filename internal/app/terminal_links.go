package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// terminalLineSuffix recognizes the source positions MyGo includes in a
// printed path. Columns are useful to the terminal's link detection, while the
// configured editor interface takes only the line.
var terminalLineSuffix = regexp.MustCompile(`(?i)(?::([0-9]+)(?::[0-9]+)?|\(([0-9]+)(?:,[0-9]+)?\)|#L([0-9]+)(?:C[0-9]+)?)$`)

// terminalFileLink resolves a printed path against the directory in which its
// shell started. Only an existing regular file inside that repository is
// handled here; URLs and other links return false so MyGo retains its normal
// system handling.
func terminalFileLink(root, link string) (string, int, bool) {
	link = strings.TrimSpace(link)
	if root == "" || link == "" {
		return "", 0, false
	}
	path, line := splitTerminalLine(link)
	if terminalLinkHasScheme(path) {
		return "", 0, false
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", 0, false
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	rootAbsolute, err := filepath.Abs(root)
	if err != nil {
		return "", 0, false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootAbsolute, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", 0, false
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, false
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbsolute)
	if err != nil {
		return "", 0, false
	}
	resolvedPath, err := filepath.EvalSymlinks(absolute)
	if err != nil || !pathWithin(resolvedRoot, resolvedPath) {
		return "", 0, false
	}
	return filepath.Clean(absolute), line, true
}

func splitTerminalLine(link string) (string, int) {
	match := terminalLineSuffix.FindStringSubmatchIndex(link)
	if match == nil {
		return link, 0
	}
	line := 0
	for group := 2; group < len(match); group += 2 {
		if match[group] < 0 {
			continue
		}
		parsed, err := strconv.Atoi(link[match[group]:match[group+1]])
		if err == nil {
			line = parsed
		}
		break
	}
	return link[:match[0]], line
}

// terminalLinkHasScheme distinguishes URLs from a source suffix such as
// main.go:12 (already removed above) and from an absolute Windows drive path.
func terminalLinkHasScheme(path string) bool {
	if len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) &&
		path[1] == ':' && (path[2] == '/' || path[2] == '\\') {
		return false
	}
	colon := strings.IndexByte(path, ':')
	if colon <= 0 {
		return false
	}
	for index, r := range path[:colon] {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || index > 0 &&
			(r >= '0' && r <= '9' || r == '+' || r == '-' || r == '.')) {
			return false
		}
	}
	return true
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
