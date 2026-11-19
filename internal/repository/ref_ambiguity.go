package repository

import (
	"fmt"
	"strings"
)

// ambiguousRefBase returns the plain ref whose namespaces need checking for a
// collision. Explicit refs, object IDs and revisions rooted at HEAD do not
// need a guess. Revision suffixes are removed so main~2 and main^{commit} are
// checked just like main; ranges are left to Git because they are not one ref.
func ambiguousRefBase(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "HEAD" || strings.HasPrefix(ref, "refs/") || strings.Contains(ref, "..") || fullRevisionObjectID(ref) {
		return ""
	}
	if suffix := strings.IndexAny(ref, "~^:@"); suffix >= 0 {
		ref = ref[:suffix]
	}
	if ref == "" || ref == "HEAD" {
		return ""
	}
	return ref
}

func fullRevisionObjectID(ref string) bool {
	if len(ref) != 40 && len(ref) != 64 {
		return false
	}
	for _, char := range ref {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

// rejectAmbiguousRef fails closed when one revision names multiple refs in
// the namespaces the app offers. Git otherwise prefers tags before local or
// remote branches, which can turn a branch action into a tag action. Requiring
// an explicit ref preserves generic tag/hash/HEAD revision support without
// inventing an intent for an ambiguous user or persisted value.
func (s *gitSession) rejectAmbiguousRef(revision string) error {
	base := ambiguousRefBase(revision)
	if base == "" {
		return nil
	}
	candidates := []string{
		"refs/heads/" + base,
		"refs/tags/" + base,
		"refs/remotes/" + base,
	}
	args := []string{"git", "for-each-ref", "--format=%(refname)"}
	args = append(args, candidates...)
	output, err := s.cmd.New(args).DontLog().RunWithOutput()
	if err != nil {
		return fmt.Errorf("validate ref %s: %w", revision, err)
	}
	seen := make(map[string]bool, len(candidates))
	for ref := range strings.SplitSeq(output, "\n") {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			seen[ref] = true
		}
	}
	matches := make([]string, 0, len(candidates))
	for _, ref := range candidates {
		if seen[ref] {
			matches = append(matches, ref)
		}
	}
	if len(matches) > 1 {
		return fmt.Errorf("ambiguous ref %q; choose an explicit ref: %s", revision, strings.Join(matches, " or "))
	}
	return nil
}
