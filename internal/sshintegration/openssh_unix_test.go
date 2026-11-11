//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package sshintegration

import "testing"

func TestLockedAccountDiagnostics(t *testing.T) {
	t.Parallel()
	if !passwdStatusLocked("example L 01/01/2024 0 99999 7 -1") || !passwdStatusLocked("example LK") {
		t.Fatal("locked passwd status was not recognized")
	}
	if passwdStatusLocked("example P 01/01/2024 0 99999 7 -1") {
		t.Fatal("usable password status was reported as locked")
	}
	if !shadowEntryLocked("example:!:20000:0:99999:7:::") || !shadowEntryLocked("example:*:20000:0:99999:7:::") {
		t.Fatal("locked shadow entry was not recognized")
	}
	if shadowEntryLocked("example:$6$salt$hash:20000:0:99999:7:::") || shadowEntryLocked("not a shadow entry") {
		t.Fatal("usable shadow entry was reported as locked")
	}
}

func TestEnvironmentConstraintDiagnostics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "missing directory", output: "Missing privilege separation directory: /run/sshd", want: true},
		{name: "missing account", output: "Privilege separation user sshd does not exist", want: true},
		{name: "denied groups", output: "setgroups: Operation not permitted", want: true},
		{name: "configuration error", output: "Bad configuration option: Example", want: false},
		{name: "authentication error", output: "Permission denied (publickey).", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := environmentConstraint(test.output)
			if (got != "") != test.want {
				t.Fatalf("environmentConstraint(%q) = %q, want constraint=%v", test.output, got, test.want)
			}
		})
	}
}
