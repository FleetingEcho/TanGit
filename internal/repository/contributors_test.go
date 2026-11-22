package repository

import "testing"

func TestParseContributorsGroupsByEmailAndSortsByCommits(t *testing.T) {
	output := "Alice\x1falice@example.com\x1f300\n" +
		"Bob\x1fbob@example.com\x1f200\n" +
		"alice smith\x1fALICE@example.com\x1f100\n" +
		"Alice S\x1falice@example.com\x1f400\n" +
		"\x1f\x1f0\n" + "garbage\n"
	got := parseContributors(output)
	if len(got) != 2 {
		t.Fatalf("contributors = %+v", got)
	}
	alice, bob := got[0], got[1]
	if alice.Email != "alice@example.com" || alice.Commits != 3 || alice.Name != "Alice S" {
		t.Fatalf("alice = %+v (want 3 commits under her newest name)", alice)
	}
	if alice.FirstCommit != 100 || alice.LastCommit != 400 {
		t.Fatalf("alice span = %d..%d", alice.FirstCommit, alice.LastCommit)
	}
	if bob.Commits != 1 || bob.Name != "Bob" {
		t.Fatalf("bob = %+v", bob)
	}
}
