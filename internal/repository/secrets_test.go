package repository

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		secrets []string
	}{
		{
			name:    "URL userinfo",
			input:   "fatal: cannot fetch https://alice:s3cr3t@example.com/team/repo.git",
			secrets: []string{"alice", "s3cr3t"},
		},
		{
			name:    "URL token query",
			input:   "https://example.com/repo.git?access_token=abc123&ref=main",
			secrets: []string{"abc123"},
		},
		{
			name:    "signed URL queries",
			input:   "https://example.com/repo.git?AWSAccessKeyId=key-id&Signature=signed-secret&X-Amz-Signature=amz-secret&X-Goog-Signature=goog-secret&sig=azure-secret",
			secrets: []string{"key-id", "signed-secret", "amz-secret", "goog-secret", "azure-secret"},
		},
		{
			name:    "command options and assignments",
			input:   "git -c password=hunter2 push --token gho_secret origin",
			secrets: []string{"hunter2", "gho_secret"},
		},
		{
			name:    "authorization headers",
			input:   "Authorization: Basic dXNlcjpwYXNz\nproxy-authorization=Bearer bearer-secret",
			secrets: []string{"dXNlcjpwYXNz", "bearer-secret"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := RedactSecrets(test.input)
			for _, secret := range test.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("redacted text %q still contains %q", got, secret)
				}
			}
			if !strings.Contains(got, redactedSecret) {
				t.Fatalf("redacted text %q has no marker", got)
			}
		})
	}

	plain := "token is required; see https://example.com/docs#authentication"
	if got := RedactSecrets(plain); got != plain {
		t.Fatalf("non-secret diagnostic changed: %q", got)
	}
}

func TestCommandLogBoundsAndOmitsOverlongUnterminatedLines(t *testing.T) {
	log := NewCommandLog()
	log.logCommand("git custom-command", true)
	writer := log.newWriter()
	if _, err := writer.Write([]byte(strings.Repeat("x", commandOutputLimit) + " https://user:split-")); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("secret@example.com\nsafe output\n")); err != nil {
		t.Fatal(err)
	}
	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1", len(entries))
	}
	output := entries[0].Output
	if strings.Contains(output, "split-secret") || strings.Contains(output, "user:") {
		t.Fatalf("overlong line leaked a split credential: %q", output)
	}
	if !strings.Contains(output, "unterminated output line omitted") || !strings.Contains(output, "safe output") {
		t.Fatalf("overlong line was not bounded cleanly: %q", output)
	}
	if len(log.pendingOutput) > commandOutputLimit {
		t.Fatalf("pending output grew to %d bytes", len(log.pendingOutput))
	}
}

func TestCommandLogRedactsSecretsSplitAcrossWrites(t *testing.T) {
	log := NewCommandLog()
	log.logCommand("git fetch https://alice:command-secret@example.com/repo.git", true)
	writer := log.newWriter()
	for i, chunk := range []string{
		"fatal: authentication failed for 'https://alice:output-",
		"secret@example.com/",
		"repo.git'\nAuthorization: Bearer split-",
		"token",
	} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			// Rendering the log while Git is running must not publish an
			// unredactable prefix merely to make partial output visible early.
			entries := log.Entries()
			if len(entries) != 1 || entries[0].Output != "" {
				t.Fatalf("a partial credential line was published: %+v", entries)
			}
		}
	}
	log.Note("push failed for https://note-user:note-secret@example.com/repo.git")

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("%d entries, want 1", len(entries))
	}
	all := entries[0].Command + "\n" + entries[0].Output
	for _, secret := range []string{
		"alice", "command-secret", "output-secret", "split-token", "note-user", "note-secret",
	} {
		if strings.Contains(all, secret) {
			t.Fatalf("command log contains %q: %q", secret, all)
		}
	}
	if !strings.Contains(all, "example.com/repo.git") || !strings.Contains(all, redactedSecret) {
		t.Fatalf("redaction removed the useful diagnostic: %q", all)
	}
}
