package sshworkspace

import "testing"

func TestResolveRemotePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		home  string
		value string
		want  string
	}{
		{"empty uses home", "/home/alice", "", "/home/alice"},
		{"home", "/home/alice", "~", "/home/alice"},
		{"home child", "/home/alice", "~/work/project", "/home/alice/work/project"},
		{"absolute cleaned", "/home/alice", "/var/tmp/../log/", "/var/log"},
		{"relative made absolute", "/home/alice", "work/project", "/home/alice/work/project"},
		{"root home", "/", "~/work", "/work"},
		{"POSIX backslash", "/home/alice", `work\project`, `/home/alice/work\project`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveRemotePath(test.home, test.value)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("ResolveRemotePath() = %q, want %q", got, test.want)
			}
		})
	}
	for _, test := range []struct{ home, value string }{
		{"relative", "work"}, {"/home/alice", "~bob/work"}, {"/home/alice", "bad\x00path"},
	} {
		if _, err := ResolveRemotePath(test.home, test.value); err == nil {
			t.Fatalf("ResolveRemotePath(%q, %q) unexpectedly succeeded", test.home, test.value)
		}
	}
}
