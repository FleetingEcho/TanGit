package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHistoryChangeSearchFindsChangedLinesAndOccurrences(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	name := "literal [file].txt"
	write := func(name, text, message string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "--literal-pathspecs", "add", "--", name)
		runGit(t, dir, "commit", "-m", message)
	}
	write(name, "timeout = 30\n", "added")
	runGit(t, dir, "tag", "v1")
	write(name, "timeout = 60\n", "changed")
	write("other", "timeout = 60\n", "unrelated file")
	runGit(t, dir, "tag", "v2")
	runGit(t, dir, "checkout", "-b", "other")
	write(name, "fallback\n", "removed")
	runGit(t, dir, "checkout", "main")
	ctx := context.Background()
	session, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	search := session.(HistoryChangeSession)
	tests := []struct {
		name string
		in   HistoryChangeQuery
		want []string
	}{
		{"changed lines", HistoryChangeQuery{Text: "timeout"}, []string{"unrelated file", "changed", "added"}},
		{"literal path", HistoryChangeQuery{Text: "timeout", Path: name}, []string{"changed", "added"}},
		{"occurrences", HistoryChangeQuery{Text: "timeout", Path: name, Occurrences: true}, []string{"added"}},
		{"all branches", HistoryChangeQuery{Text: "timeout", Path: name, AllBranches: true}, []string{"removed", "changed", "added"}},
		{"revision range", HistoryChangeQuery{Text: "timeout", Path: name, Ref: "v1..v2"}, []string{"changed"}},
		{"literal pattern", HistoryChangeQuery{Text: "time.out"}, nil},
		{"regex", HistoryChangeQuery{Text: "timeout = (30|60)", Regex: true, Path: name}, []string{"changed", "added"}},
		{"regex occurrences", HistoryChangeQuery{Text: "timeout = (30|60)", Occurrences: true, Regex: true, Path: name}, []string{"added"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			page, err := search.SearchHistoryChanges(ctx, test.in, HistoryChangeCursor{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var subjects []string
			for _, hit := range page.Hits {
				subjects = append(subjects, hit.Commit.Subject)
				if hit.Path == "" || test.in.Path != "" && hit.Path != test.in.Path {
					t.Errorf("unexpected file %q", hit.Path)
				}
			}
			if page.More || !slices.Equal(subjects, test.want) {
				t.Errorf("got %v, more=%v; want %v", subjects, page.More, test.want)
			}
		})
	}
}

func TestHistoryChangeSearchBoundsWorkAndResumes(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	var input strings.Builder
	const count = historyChangeWindow + 8
	for i := 1; i <= count; i++ {
		body := fmt.Sprintf("frequent %d\n", i)
		if i == 1 {
			body += "rare marker\n"
		}
		fmt.Fprintf(&input, "commit refs/heads/main\ncommitter Test <test@example.com> %d +0000\ndata 0\nM 100644 inline file\ndata %d\n%s\n",
			1_700_000_000+i, len(body), body)
	}
	cmd := exec.Command("git", "-C", dir, "fast-import", "--quiet")
	cmd.Stdin = strings.NewReader(input.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fast-import: %v: %s", err, output)
	}
	ctx := context.Background()
	session, err := (Opener{}).Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	search := session.(HistoryChangeSession)
	read := func(text string, cursor HistoryChangeCursor) HistoryChangePage {
		t.Helper()
		page, err := search.SearchHistoryChanges(ctx, HistoryChangeQuery{Text: text}, cursor, nil)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	popular := read("frequent", HistoryChangeCursor{})
	if !popular.More || popular.Scanned != historyChangeLimit || len(popular.Hits) != historyChangeLimit {
		t.Fatalf("popular result: checked %d, hits %d, more %v", popular.Scanned, len(popular.Hits), popular.More)
	}
	older := read("frequent", popular.Next)
	for _, hit := range older.Hits {
		if slices.ContainsFunc(popular.Hits, func(first HistoryChangeHit) bool { return first.Commit.Hash == hit.Commit.Hash }) {
			t.Fatalf("page repeated %s", hit.Commit.Hash)
		}
	}
	rare := read("rare marker", HistoryChangeCursor{})
	if rare.Scanned != historyChangeWindow || len(rare.Hits) != 0 || !rare.More {
		t.Fatalf("rare result: checked %d, hits %d, more %v", rare.Scanned, len(rare.Hits), rare.More)
	}
	last := read("rare marker", rare.Next)
	if last.Scanned != count-historyChangeWindow || len(last.Hits) != 2 || last.More {
		t.Fatalf("last result: checked %d, hits %d, more %v", last.Scanned, len(last.Hits), last.More)
	}
	absent := read("no such text", HistoryChangeCursor{})
	if absent.Scanned != historyChangeWindow || len(absent.Hits) != 0 || !absent.More {
		t.Fatalf("absent result: checked %d, hits %d, more %v", absent.Scanned, len(absent.Hits), absent.More)
	}
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	stopped, err := search.SearchHistoryChanges(cancelled, HistoryChangeQuery{Text: "frequent"}, HistoryChangeCursor{}, func(HistoryChangePage) {
		cancel()
	})
	if !errors.Is(err, context.Canceled) || stopped.Scanned != historyChangeBatch || !stopped.More {
		t.Fatalf("stopped: checked %d, hits %d, more %v, err %v", stopped.Scanned, len(stopped.Hits), stopped.More, err)
	}
	resume := read("frequent", stopped.Next)
	if resume.Hits[0].Commit.Hash != popular.Hits[historyChangeBatch].Commit.Hash {
		t.Fatal("resuming did not continue after the last examined commit")
	}
}

func TestHistoryChangeHitListsEveryMatchingFile(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		text := "needle\n"
		if name == "c.txt" {
			text = "other\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "three files")
	session, err := (Opener{}).Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := session.(HistoryChangeSession).SearchHistoryChanges(context.Background(), HistoryChangeQuery{Text: "needle"}, HistoryChangeCursor{}, nil)
	if err != nil || len(page.Hits) != 1 {
		t.Fatalf("hits = %+v, %v", page.Hits, err)
	}
	hit := page.Hits[0]
	if !slices.Equal(hit.Files, []string{"a.txt", "b.txt"}) || hit.Path != "a.txt" || hit.Commit.AuthorName != "Test" {
		t.Fatalf("hit = %+v", hit)
	}
}
