package app

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/app/i18n"
)

// paletteStringSources are the files whose command labels, details and reasons
// reach the palette through tr at run time. The source-coverage test only sees
// literals passed to tr, so these are checked here by reading them.
var paletteStringSources = []string{
	"command_catalog.go", "command_catalog_actions.go", "command_dynamic.go", "command_commit_search.go",
	"command_palette.go", "hunk_actions.go", "action_controls.go", "keyboard_commands.go", "keyboard_diff.go",
	"conflict_actions.go", "agent_actions.go", "activity_center.go",
}

var paletteStringPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:DisabledReason|disabledReason|FixLabel|Detail|detail|Label|label|description|visibleLabel|hint|reason)\s*(?::=|=|:)\s*"((?:[^"\\]|\\.)+)"`),
	regexp.MustCompile(`state\.FixID, state\.FixLabel = [^,]+, "((?:[^"\\]|\\.)+)"`),
	regexp.MustCompile(`return "((?:[^"\\]|\\.)+)"`),
}

// userTextSources are files whose multi-word string literals are all text the
// user can see, so each one is checked, not only those a pattern recognizes.
var userTextSources = []string{"cockpit_summary.go"}

var stringLiteral = regexp.MustCompile(`"((?:[^"\\\n]|\\.)+)"`)

// paletteStrings gathers every English string the palette can show.
func paletteStrings(t *testing.T) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	add := func(text string) {
		text = strings.TrimSpace(text)
		if text == "" || strings.ContainsAny(text, "\n") {
			return
		}
		found[text] = true
	}
	for _, command := range keyboardCommands() {
		add(command.label)
	}
	for _, command := range diffKeyboardCommands() {
		add(command.label)
	}
	for _, spec := range staticCommandSpecs() {
		if spec.Format != "" {
			add(spec.Format)
		} else {
			add(spec.Label)
		}
		add(spec.Detail)
	}
	// Templates of the commands built from live state.
	for _, format := range []string{"Switch to tab: %s", "Apply stash: %s", "Pop stash: %s", "Fetch remote: %s", "Open remote in the browser: %s"} {
		add(format)
	}
	for _, label := range commandGroupLabels {
		add(label)
	}
	for _, name := range paletteStringSources {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		for _, pattern := range paletteStringPatterns {
			for _, loc := range pattern.FindAllStringSubmatchIndex(string(data), -1) {
				// A literal joined to a value with + is a template fragment.
				if rest := strings.TrimLeft(string(data)[loc[1]:], " \t"); strings.HasPrefix(rest, "+") {
					continue
				}
				match := []string{"", string(data)[loc[2]:loc[3]]}
				text := strings.ReplaceAll(match[1], `\"`, `"`)
				// Skip identifiers, IDs and fragments that are not user text.
				if !strings.ContainsAny(text, "abcdefghijklmnopqrstuvwxyz") || !strings.Contains(text, " ") || strings.ContainsAny(text, "%{}") && !strings.Contains(text, "%s") && !strings.Contains(text, "%d") {
					continue
				}
				if strings.HasPrefix(text, "git/") || strings.Contains(text, "/") && !strings.Contains(text, " ") {
					continue
				}
				add(text)
			}
		}
	}
	for _, name := range userTextSources {
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		for _, match := range stringLiteral.FindAllStringSubmatch(string(data), -1) {
			text := strings.ReplaceAll(match[1], `\"`, `"`)
			if strings.Contains(text, " ") && strings.ContainsAny(text[:1], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
				add(text)
			}
		}
	}
	return found
}

// translationSameInLanguage lists words that are the same in a language.
var translationSameInLanguage = map[string]bool{
	"Navigation|fr": true, "Stash|es": true, "Stash|fr": true, "Application|fr": true,
}

// Every string the palette shows has a translation in each language, so a
// non-English interface never falls back to English in the command list.
func TestPaletteStringsAreTranslated(t *testing.T) {
	missing := map[string][]string{}
	for text := range paletteStrings(t) {
		for _, language := range []string{"zh-Hans", "zh-Hant", "ja", "es", "fr"} {
			if i18n.Tr(text, language) == text && !translationSameInLanguage[text+"|"+language] {
				missing[text] = append(missing[text], language)
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	texts := make([]string, 0, len(missing))
	for text := range missing {
		texts = append(texts, text)
	}
	sort.Strings(texts)
	var report strings.Builder
	for _, text := range texts {
		report.WriteString("\n  " + text + "  [" + strings.Join(missing[text], ",") + "]")
	}
	t.Fatalf("%d palette strings have no translation in some languages:%s", len(texts), report.String())
}
