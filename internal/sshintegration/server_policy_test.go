package sshintegration

import (
	"fmt"
	"strings"
	"testing"
)

type openSSHUnavailableRecorder struct {
	message string
	skipped bool
	failed  bool
}

func (r *openSSHUnavailableRecorder) Helper() {}

func (r *openSSHUnavailableRecorder) Skip(args ...any) {
	r.skipped = true
	r.message = fmt.Sprint(args...)
}

func (r *openSSHUnavailableRecorder) Fatalf(format string, args ...any) {
	r.failed = true
	r.message = fmt.Sprintf(format, args...)
}

func TestOpenSSHUnavailableHonorsRequiredMode(t *testing.T) {
	for _, test := range []struct {
		name     string
		value    string
		required bool
	}{
		{name: "default skips"},
		{name: "zero skips", value: "0"},
		{name: "required fails", value: "1", required: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(requireOpenSSHEnvironment, test.value)
			reporter := &openSSHUnavailableRecorder{}
			openSSHUnavailable(reporter, "missing executable %s", "/usr/sbin/sshd")

			if reporter.failed != test.required {
				t.Fatalf("failed = %v, want %v (message %q)", reporter.failed, test.required, reporter.message)
			}
			if reporter.skipped == test.required {
				t.Fatalf("skipped = %v, want %v (message %q)", reporter.skipped, !test.required, reporter.message)
			}
			if !strings.Contains(reporter.message, "missing executable /usr/sbin/sshd") {
				t.Fatalf("message %q lost the environment reason", reporter.message)
			}
			if test.required && !strings.Contains(reporter.message, requireOpenSSHEnvironment+"=1") {
				t.Fatalf("message %q does not identify the required-mode variable", reporter.message)
			}
		})
	}
}
