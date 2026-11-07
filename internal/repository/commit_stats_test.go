package repository

import "testing"

func TestParseCommitLineStats(t *testing.T) {
	output := "100\t10\tmain.go\x00" +
		"-\t-\tassets/logo.png\x00" +
		"5\t2\t\x00old/name.go\x00new/name.go\x00" +
		"3\t0\tpath with spaces.txt\x00"
	stats := parseCommitLineStats(output)
	tests := []struct {
		path           string
		added, deleted int
	}{
		{path: "main.go", added: 100, deleted: 10},
		{path: "assets/logo.png"},
		{path: "new/name.go", added: 5, deleted: 2},
		{path: "path with spaces.txt", added: 3},
	}
	for _, test := range tests {
		got, ok := stats[test.path]
		if !ok {
			t.Errorf("numstat lost %q", test.path)
			continue
		}
		if got.added != test.added || got.deleted != test.deleted {
			t.Errorf("%q = +%d -%d, want +%d -%d",
				test.path, got.added, got.deleted, test.added, test.deleted)
		}
	}
	if _, ok := stats["old/name.go"]; ok {
		t.Error("a rename was keyed by its old path rather than its new path")
	}
}
