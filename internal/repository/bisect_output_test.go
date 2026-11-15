package repository

import "testing"

func TestFirstBadCommitSupportsGitOutputFormats(t *testing.T) {
	const hash = "1154a1b3a837397316409bea0314ab8f175cf1a2"
	for _, tc := range []struct {
		name string
		term string
		text string
	}{
		{name: "legacy unquoted default", term: "bad", text: hash + " is the first bad commit\n"},
		{name: "Git 2.56 quoted default", term: "bad", text: hash + " is the first 'bad' commit\n"},
		{name: "custom term", term: "broken", text: hash + " is the first 'broken' commit\r\n"},
		{name: "reversed custom term", term: "good", text: hash + " is the first 'good' commit\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstBadCommit(tc.text, tc.term); got != hash {
				t.Fatalf("parse %q = %q, want %s", tc.text, got, hash)
			}
		})
	}
	if got := firstBadCommit("bisect is still running", "bad"); got != "" {
		t.Fatalf("incomplete bisect returned %q", got)
	}
}
