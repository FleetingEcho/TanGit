package sshworkspace

import (
	"strings"
	"testing"
)

func TestShellCommandQuotesEachWord(t *testing.T) {
	t.Parallel()
	got, err := ShellCommand("git", "commit", "-m", "it's $HOME; printf bad")
	if err != nil {
		t.Fatal(err)
	}
	want := `'git' 'commit' '-m' 'it'"'"'s $HOME; printf bad'`
	if got != want {
		t.Fatalf("ShellCommand() = %q, want %q", got, want)
	}
	if got := ShellQuote(""); got != "''" {
		t.Fatalf("ShellQuote(empty) = %q", got)
	}
	if got := ShellQuote("line 1\nline 2"); got != "'line 1\nline 2'" {
		t.Fatalf("ShellQuote(multiline) = %q", got)
	}
}

func TestCommandInDirectoryQuotesDirectory(t *testing.T) {
	t.Parallel()
	got, err := commandInDirectory("/tmp/a b/'odd'", "printf", "%s", "$(touch /tmp/not-run)")
	if err != nil {
		t.Fatal(err)
	}
	want := `cd -- '/tmp/a b/'"'"'odd'"'"'' && 'printf' '%s' '$(touch /tmp/not-run)'`
	if got != want {
		t.Fatalf("commandInDirectory() = %q, want %q", got, want)
	}
	if _, err := ShellCommand("printf", "bad\x00argument"); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("NUL argument error = %v", err)
	}
	if _, err := commandInDirectory("bad\x00directory", "pwd"); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("NUL directory error = %v", err)
	}
}
