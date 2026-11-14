package app

import "testing"

// Dynamic confirmation titles are not statically discoverable by tr analysis,
// so check the rendered source catalog rather than relying on a fallback pass.
func TestNewConfirmationTitlesAreLocalized(t *testing.T) {
	keys := []string{
		"Discard Saved File", "Mixed Reset", "Edit Commit", "Checkout Commit", "Push All Tags", "Delete Remote Tag",
		"Mark Bisect Commit", "Mark commit", "Reset Bisect",
		"Apply Mailbox Patch", "Apply patch", "Open Pull Request",
		"Delete Stale Branches", "Move Unpushed Commits",
		"Drop Commit", "Drop commit", "Move Commit", "Move commit",
		"Squash Commit", "Squash commit", "Amend Commit with Staged Changes",
		"Amend commit", "Create Fixup Commit", "Create fixup",
		"Apply Fixup Commits", "Apply fixups", "Merge Branch",
		"Delete Branch", "Rebase Branch", "Force Checkout", "Force checkout",
	}
	for _, language := range []string{"zh-Hans", "zh-Hant", "ja", "es", "fr"} {
		for _, key := range keys {
			value, ok := translations[language][key]
			if !ok || value == "" {
				t.Errorf("%s has no translation for confirmation label %q", language, key)
			}
		}
	}
}
