package app

import (
	"strings"
	"testing"
)

func TestWorkspaceSearchReplacementLiteralPreserveCaseAndMultipleMatches(t *testing.T) {
	content := "Needle needle NEEDLE\r\nΣ ς\n"
	options := workspaceSearchOptions{Query: "needle"}
	plan, err := planWorkspaceSearchReplacement(content, options, "other $1", true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.text, "Other $1 other $1 OTHER $1\r\nΣ ς\n"; got != want {
		t.Fatalf("literal replacement = %q, want %q", got, want)
	}
	if plan.count != 3 || len(plan.edits) != 3 {
		t.Fatalf("literal replacements = %d edits = %d, want 3", plan.count, len(plan.edits))
	}

	plan, err = planWorkspaceSearchReplacement(content, workspaceSearchOptions{Query: "σ"}, "δelta", true)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.text, "Needle needle NEEDLE\r\nΔELTA δelta\n"; got != want {
		t.Fatalf("Unicode preserved-case replacement = %q, want %q", got, want)
	}
}

func TestWorkspaceSearchReplacementRegexCapturesWholeWordAndZeroWidth(t *testing.T) {
	plan, err := planWorkspaceSearchReplacement(
		"cat écat category cat\n",
		workspaceSearchOptions{Query: `cat`, RegularExpr: true, WholeWord: true, CaseSensitive: true},
		"dog", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.text, "dog écat category dog\n"; got != want {
		t.Fatalf("whole-word replacement = %q, want %q", got, want)
	}

	plan, err = planWorkspaceSearchReplacement(
		"Alpha beta\n",
		workspaceSearchOptions{Query: `(?P<first>[A-Z])([a-z]+)`, RegularExpr: true, CaseSensitive: true},
		"${first}${2}-$1", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := plan.text, "Alpha-A beta\n"; got != want {
		t.Fatalf("named/numbered capture replacement = %q, want %q", got, want)
	}

	plan, err = planWorkspaceSearchReplacement("ab\n", workspaceSearchOptions{Query: "$", RegularExpr: true, CaseSensitive: true}, "X", false)
	if err != nil || plan.text != "abX\n" || plan.count != 1 {
		t.Fatalf("zero-width end replacement = %q, count %d, err %v", plan.text, plan.count, err)
	}
	plan, err = planWorkspaceSearchReplacement("ab\n", workspaceSearchOptions{Query: "x*", RegularExpr: true, CaseSensitive: true}, "-", false)
	if err != nil || plan.text != "-a-b-\n" || plan.count != 3 {
		t.Fatalf("zero-width repetition replacement = %q, count %d, err %v; want every non-overlapping match", plan.text, plan.count, err)
	}
}

func TestWorkspaceSearchReplacementIsNotLimitedByVisibleResults(t *testing.T) {
	content := strings.Repeat("needle needle ", 600)
	plan, err := planWorkspaceSearchReplacement(content, workspaceSearchOptions{Query: "needle", CaseSensitive: true}, "x", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.count != 1200 || len(plan.edits) != 1200 {
		t.Fatalf("bulk replacement count = %d, edits = %d; want 1200", plan.count, len(plan.edits))
	}
	if got := strings.Count(plan.text, "x"); got != 1200 {
		t.Fatalf("replacement text contains %d replacements, want 1200", got)
	}
	if strings.Contains(plan.text, "needle") {
		t.Fatal("lazy result limit left matches unreplaced")
	}
}

func TestWorkspaceSearchReplacementRejectsInvalidRegexBeforeEditing(t *testing.T) {
	if _, err := planWorkspaceSearchReplacement("content", workspaceSearchOptions{Query: "(", RegularExpr: true}, "x", false); err == nil {
		t.Fatal("invalid regular expression produced a replacement plan")
	}
}
