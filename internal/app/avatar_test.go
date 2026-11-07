package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAuthorInitial(t *testing.T) {
	cases := []struct {
		name, email, want string
	}{
		{"Ada Lovelace", "ada@example.com", "A"},
		{"alan turing", "alan@example.com", "A"},
		{"  Grace Hopper  ", "grace@example.com", "G"},
		{"张三", "zhang@example.com", "张"},
		{"", "linus@example.com", "L"},
		{"", "", "?"},
		{"   ", "   ", "?"},
		{"1Password", "one@example.com", "1"},
	}
	for _, tt := range cases {
		if got := authorInitial(tt.name, tt.email); got != tt.want {
			t.Errorf("authorInitial(%q, %q) = %q, want %q", tt.name, tt.email, got, tt.want)
		}
	}
}

func TestAuthorColorFollowsTheAuthor(t *testing.T) {
	// One author keeps one color: across commits, runs, and the case and
	// spacing a name or email was typed in.
	base := avatarColor("Ada Lovelace", "ada@example.com")
	if got := avatarColor("Ada Lovelace", "ada@example.com"); got != base {
		t.Fatalf("the same author got %v then %v", base, got)
	}
	if got := avatarColor("Ada Lovelace", "  ADA@Example.COM "); got != base {
		t.Fatalf("the email's case and spacing changed the color: %v, want %v", got, base)
	}

	// Different people spread over the palette rather than sharing one
	// color.
	distinct := map[ui.Color]bool{}
	for _, email := range []string{
		"ada@example.com", "alan@example.com", "grace@example.com",
		"linus@example.com", "octocat@github.com", "torvalds@linux-foundation.org",
	} {
		distinct[avatarColor("", email)] = true
	}
	if len(distinct) < 3 {
		t.Fatalf("six authors share only %d colors", len(distinct))
	}
}

func TestAuthorColorWithoutAnAuthor(t *testing.T) {
	// An author with neither a name nor an email still gets a legible
	// avatar, from the first palette color.
	if got := avatarColor("", ""); got != avatarPalette[0] {
		t.Fatalf("no author got %v, want the first palette color %v", got, avatarPalette[0])
	}
	if got := avatarColor("  ", "  "); got != avatarPalette[0] {
		t.Fatalf("blank author got %v, want the first palette color %v", got, avatarPalette[0])
	}
}

func TestHistoryRowsShowAuthorAvatars(t *testing.T) {
	h, _ := historyHarness(t)
	// The fixture's commits are Ada's, so every row's avatar holds an A.
	if !containsText(h.texts(), "A") {
		t.Fatalf("History rows show no author initial: %q", h.texts())
	}
	// The detail pane names the author beside their avatar.
	if !containsText(h.texts(), "Ada <ada@example.com>") {
		t.Fatalf("the commit detail misses the author line: %q", h.texts())
	}
}
