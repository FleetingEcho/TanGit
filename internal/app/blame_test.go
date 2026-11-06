package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	"lazygitapp/internal/repository"
)

func TestFormatBlameCompactsAndGroupsCommitMetadata(t *testing.T) {
	first := strings.Repeat("a", 40)
	second := strings.Repeat("b", 40)
	lines := []repository.BlameLine{
		{Hash: first, Author: "Ada", Date: "2025-11-02", Text: "first"},
		{Hash: first, Author: "Ada", Date: "2025-11-02", Text: "\tsecond"},
		{Hash: second, Author: "A very long author name", Date: "2026-04-30", Text: ""},
		{Hash: first, Author: "Ada", Date: "2025-11-02", Text: "after another commit"},
	}
	got := formatBlame(lines)
	rows := strings.Split(got, "\n")
	if len(rows) != len(lines) {
		t.Fatalf("formatted blame has %d rows for %d source lines: %q", len(rows), len(lines), got)
	}
	if !strings.HasPrefix(rows[0], "aaaaaaa  Ada") || !strings.Contains(rows[0], " · 2025-11-02  first") {
		t.Errorf("first metadata row = %q", rows[0])
	}
	if strings.Contains(rows[1], "aaaaaaa") || strings.Contains(rows[1], "Ada") || !strings.HasSuffix(rows[1], "\tsecond") {
		t.Errorf("continuation row = %q", rows[1])
	}
	if !strings.HasPrefix(rows[2], "bbbbbbb  A very long autho… · 2026-04-30") || strings.TrimSpace(rows[2]) != "bbbbbbb  A very long autho… · 2026-04-30" {
		t.Errorf("truncated author or empty source row = %q", rows[2])
	}
	if !strings.HasPrefix(rows[3], "aaaaaaa") || !strings.HasSuffix(rows[3], "after another commit") {
		t.Errorf("a non-contiguous commit must be labelled again: %q", rows[3])
	}
	if strings.Contains(got, "│") {
		t.Errorf("grid marks survived formatting: %q", got)
	}
	if strings.Contains(got, first) || strings.Contains(got, "00:00:00") {
		t.Errorf("verbose metadata survived formatting: %q", got)
	}

	sourceColumn := utf8.RuneCountInString(rows[0][:strings.Index(rows[0], "first")])
	for index, row := range rows[1:] {
		line := lines[index+1]
		if at := strings.Index(row, line.Text); line.Text != "" {
			got := utf8.RuneCountInString(row[:at])
			if got != sourceColumn {
				t.Errorf("source on row %d starts at column %d, want %d: %q", index+1, got, sourceColumn, row)
			}
		}
	}
}
