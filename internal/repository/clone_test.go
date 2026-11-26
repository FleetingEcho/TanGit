package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestValidateCloneURLsAndDeriveDestinationName(t *testing.T) {
	valid := map[string]string{
		"https://github.com/owner/project.git":        "project",
		"https://github.com/owner/project/":           "project",
		"ssh://git@github.com/owner/project.git":      "project",
		"git@github.com:owner/project.git":            "project",
		"example.com:owner/nested/project.git":        "project",
		"https://github.com/owner/project.git?ref=ok": "project",
	}
	for remote, want := range valid {
		t.Run(remote, func(t *testing.T) {
			if err := ValidateCloneURL(remote); err != nil {
				t.Fatalf("valid remote rejected: %v", err)
			}
			got, err := CloneRepositoryName(remote)
			if err != nil || got != want {
				t.Fatalf("repository name = %q, %v; want %q", got, err, want)
			}
		})
	}

	invalid := []string{
		"", "http://example.com/project.git", "git://example.com/project.git",
		"file:///tmp/project", "/tmp/project", "../project", "C:\\project",
		"https://", "https://example.com", "git@example.com:",
		"https://user@example.com/project.git",
		"https://user:secret@example.com/project.git",
		"https://example.com/project.git?access_token=secret",
		"https://example.com/project.git?AWSAccessKeyId=key-id&Signature=signed-secret",
		"https://example.com/project.git?X-Amz-Signature=amz-secret",
		"https://example.com/project.git?X-Goog-Signature=goog-secret",
		"https://example.com/project.git?sig=azure-secret",
		"https://example.com/project.git?unrecognized=not-safe-to-persist",
		"ssh://git:secret@example.com/project.git",
		"git@example.com:owner/project.git?token=secret",
		"https://example.com/project.git\n-credential.helper=bad",
	}
	for _, remote := range invalid {
		t.Run(remote, func(t *testing.T) {
			if err := ValidateCloneURL(remote); err == nil {
				t.Fatalf("unsafe remote %q was accepted", remote)
			}
		})
	}
	if err := ValidateCloneURL("https://user:secret@example.com/project.git"); !errors.Is(err, ErrCloneCredentials) {
		t.Fatalf("credential error = %v, want ErrCloneCredentials", err)
	}
}

func TestParseCloneProgressAcrossCarriageReturns(t *testing.T) {
	var got []CloneProgress
	writer := &cloneProgressWriter{report: func(progress CloneProgress) {
		got = append(got, progress)
	}}
	input := "Enumerating objects: 12, done.\rReceiving objects:  25% (3/12)\r" +
		"Receiving objects: 100% (12/12), 1.25 MiB | 2 MiB/s, done.\r" +
		"remote: Resolving deltas: 50% (2/4), completed with 1 local object.\n"
	if _, err := writer.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("progress records = %#v", got)
	}
	if got[0].Phase != "Enumerating objects" || got[0].Current != 12 || got[0].Known {
		t.Fatalf("enumerating progress = %+v", got[0])
	}
	if got[1].Phase != "Receiving objects" || got[1].Percent != 25 || got[1].Fraction != 0.25 ||
		got[1].Current != 3 || got[1].Total != 12 || !got[1].Known {
		t.Fatalf("receiving progress = %+v", got[1])
	}
	if got[3].Phase != "Resolving deltas" || got[3].Percent != 50 || got[3].Current != 2 || got[3].Total != 4 {
		t.Fatalf("remote-prefixed progress = %+v", got[3])
	}
}

func TestCloneDiagnosticsAreBoundedSanitizedAndRedacted(t *testing.T) {
	diagnostics := &boundedDiagnostic{limit: 1024}
	chunk := []byte(strings.Repeat("x", 2000))
	if _, err := diagnostics.Write(chunk); err != nil {
		t.Fatal(err)
	}
	if len(diagnostics.String()) != 1024 {
		t.Fatalf("diagnostic length = %d, want 1024", len(diagnostics.String()))
	}
	raw := "\x1b[31mReceiving objects:\x1b[0m 50%\x07\x00\r" +
		"fatal: https://user:secret@example.com/project.git failed\n"
	got := sanitizeCloneDiagnostic(raw)
	if strings.ContainsAny(got, "\x00\x1b\x07") || strings.Contains(got, "secret") ||
		!strings.Contains(got, "[redacted]") {
		t.Fatalf("unsanitized diagnostic %q", got)
	}
}

func TestCloneRepositoryRealLocalTransportWithBranchAndProgress(t *testing.T) {
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "init", "--initial-branch=main")
	runGit(t, source, "config", "user.name", "Clone Test")
	runGit(t, source, "config", "user.email", "clone@example.com")
	if err := os.WriteFile(filepath.Join(source, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "main.txt")
	runGit(t, source, "commit", "-m", "main")
	runGit(t, source, "checkout", "-b", "topic")
	if err := os.WriteFile(filepath.Join(source, "topic.txt"), []byte("topic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "add", "topic.txt")
	runGit(t, source, "commit", "-m", "topic")

	destination := filepath.Join(t.TempDir(), "nested", "project")
	remote := "file://" + filepath.ToSlash(source)
	var mu sync.Mutex
	var progress []CloneProgress
	err := cloneRepository(ctx, CloneOptions{URL: remote, Destination: destination, Branch: "topic"}, func(update CloneProgress) {
		mu.Lock()
		progress = append(progress, update)
		mu.Unlock()
	}, true)
	if err != nil {
		t.Fatalf("clone local remote: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(destination, "topic.txt"))
	if err != nil || string(contents) != "topic\n" {
		t.Fatalf("selected branch was not cloned: %q, %v", contents, err)
	}
	if got := strings.TrimSpace(gitOutput(t, destination, "branch", "--show-current")); got != "topic" {
		t.Fatalf("checked out branch = %q, want topic", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(progress) == 0 {
		t.Fatal("git clone --progress emitted no parsed records")
	}
	foundFraction := false
	for _, update := range progress {
		if update.Known && update.Fraction >= 0 && update.Fraction <= 1 && update.Phase != "" {
			foundFraction = true
		}
	}
	if !foundFraction {
		t.Fatalf("real clone produced no fractional progress: %+v", progress)
	}
}

func TestCloneRepositoryRejectsCredentialsBeforeCreatingDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "must-not-exist")
	err := CloneRepository(context.Background(), CloneOptions{
		URL: "https://user:token@example.com/project.git", Destination: destination,
	}, nil)
	if !errors.Is(err, ErrCloneCredentials) {
		t.Fatalf("clone error = %v, want ErrCloneCredentials", err)
	}
	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("rejected clone created its destination: %v", statErr)
	}
}

func TestCloneArgumentsForEachMode(t *testing.T) {
	cases := []struct {
		options CloneOptions
		want    string
	}{
		{CloneOptions{}, "clone --progress"},
		{CloneOptions{Branch: "main", Mode: CloneBlobless}, "clone --progress --branch main --filter=blob:none"},
		{CloneOptions{Mode: CloneTreeless, SingleBranch: true}, "clone --progress --filter=tree:0 --single-branch"},
		{CloneOptions{Mode: CloneShallow}, "clone --progress --depth 1 --no-single-branch"},
		{CloneOptions{Mode: CloneShallow, Depth: 50, SingleBranch: true}, "clone --progress --depth 50 --single-branch"},
		{CloneOptions{Mode: CloneBlobless, SparsePaths: []string{"cmd"}}, "clone --progress --filter=blob:none --sparse"},
	}
	for _, tc := range cases {
		if got := strings.Join(cloneArguments(tc.options), " "); got != tc.want {
			t.Errorf("cloneArguments(%+v) = %q, want %q", tc.options, got, tc.want)
		}
	}
}

func TestParseSparsePathsValidatesFolders(t *testing.T) {
	got, err := ParseSparsePaths(" cmd, internal/app\n/docs/ cmd ")
	if err != nil || strings.Join(got, "|") != "cmd|internal/app|docs" {
		t.Fatalf("ParseSparsePaths = %q, %v", got, err)
	}
	for _, bad := range []string{"../outside", "a/./b", "-x", "src/*.go", "a\\b", "!keep"} {
		if _, err := ParseSparsePaths(bad); err == nil {
			t.Errorf("unsafe sparse folder %q was accepted", bad)
		}
	}
	if _, err := NormalizeCloneOptions(CloneOptions{
		URL: "https://example.com/o/p.git", Destination: t.TempDir() + "/p", Mode: CloneShallow, Depth: -1,
	}); err == nil {
		t.Fatal("negative shallow depth was accepted")
	}
}

func TestCloneRepositoryRealPartialShallowAndSparseModes(t *testing.T) {
	ctx := context.Background()
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(source, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(source, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, source, "init", "--initial-branch=main")
	runGit(t, source, "config", "user.name", "Clone Test")
	runGit(t, source, "config", "user.email", "clone@example.com")
	runGit(t, source, "config", "uploadpack.allowFilter", "true")
	for index, name := range []string{"app/main.go", "docs/guide.md", "README.md"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(strings.Repeat("x", index+1)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, source, "add", name)
		runGit(t, source, "commit", "-m", "add "+name)
	}
	runGit(t, source, "branch", "topic")
	remote := "file://" + filepath.ToSlash(source)

	blobless := filepath.Join(t.TempDir(), "blobless")
	if err := cloneRepository(ctx, CloneOptions{URL: remote, Destination: blobless, Mode: CloneBlobless}, nil, true); err != nil {
		t.Fatalf("blobless clone: %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, blobless, "config", "remote.origin.partialclonefilter")); got != "blob:none" {
		t.Fatalf("partial clone filter = %q, want blob:none", got)
	}

	shallow := filepath.Join(t.TempDir(), "shallow")
	if err := cloneRepository(ctx, CloneOptions{URL: remote, Destination: shallow, Mode: CloneShallow, Depth: 1}, nil, true); err != nil {
		t.Fatalf("shallow clone: %v", err)
	}
	if got := strings.TrimSpace(gitOutput(t, shallow, "rev-list", "--count", "HEAD")); got != "1" {
		t.Fatalf("shallow history length = %s, want 1", got)
	}
	if got := strings.TrimSpace(gitOutput(t, shallow, "branch", "-r")); !strings.Contains(got, "origin/topic") {
		t.Fatalf("shallow clone dropped other branches: %q", got)
	}

	sparse := filepath.Join(t.TempDir(), "sparse")
	if err := cloneRepository(ctx, CloneOptions{
		URL: remote, Destination: sparse, Mode: CloneBlobless, SparsePaths: []string{"app"},
	}, nil, true); err != nil {
		t.Fatalf("sparse clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sparse, "app", "main.go")); err != nil {
		t.Fatalf("sparse folder missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sparse, "docs", "guide.md")); !os.IsNotExist(err) {
		t.Fatalf("folder outside the sparse set was checked out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sparse, "README.md")); err != nil {
		t.Fatalf("cone mode keeps top-level files: %v", err)
	}
}
