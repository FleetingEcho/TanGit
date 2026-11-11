package agentassist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func singleConflictRequest(t *testing.T, sourcePath string) AdviceRequest {
	t.Helper()
	request, err := NewRequest(KindConflict, []Artifact{NewConflictArtifact(sourcePath,
		"prefix\n<<<<<<< HEAD\ncurrent\n||||||| base\noriginal\n=======\nincoming\n>>>>>>> feature\nsuffix\n")})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func singleConflictAdvice(request AdviceRequest) Advice {
	advice := responseEnvelope(request)
	advice.Suggestions = []Suggestion{{ID: "one", Summary: "reviewed current side", Conflict: &ConflictSuggestion{
		ArtifactID: request.Artifacts[0].ID, RegionID: request.Artifacts[0].Regions[0].ID, Substitution: SubstituteCurrent,
	}}}
	return advice
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func writeResponseHelper(t *testing.T, path, before string, response any) {
	t.Helper()
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, path, before+"\ncat > \"$2\" <<'TANGIT_RESPONSE'\n"+string(data)+"\nTANGIT_RESPONSE\n")
}

func TestCommandAdvisorRealHelperIsolatedProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temporary POSIX helper")
	}
	base := t.TempDir()
	root := filepath.Join(base, "helper root 'quoted' ; touch PWNED ; {responseFile}")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(base, "advice helper.sh")
	capturedArgs := filepath.Join(root, "captured arguments")
	capturedRequest := filepath.Join(root, "captured request")
	request := singleConflictRequest(t, "dir with spaces/conflicted.txt")
	before := fmt.Sprintf(`[ -f "$1" ]
[ "$1" != "$2" ]
[ "$PWD" = "$3" ]
printf '%%s\n' "$1" "$2" "$3" "$PWD" > %s
cp "$1" %s
`, shellQuoteHelper(capturedArgs), shellQuoteHelper(capturedRequest))
	writeResponseHelper(t, helper, before, singleConflictAdvice(request))

	advisor, err := NewCommandAdvisor(CommandConfig{
		Template: shellQuoteHelper(helper) + " {requestFile} {responseFile} {artifactDir}",
		Timeout:  5 * time.Second,
		TempDir:  root,
	})
	if err != nil {
		t.Fatal(err)
	}
	advice, err := advisor.Propose(context.Background(), request)
	if err != nil {
		t.Fatalf("real helper Propose: %v", err)
	}
	if len(advice.Suggestions) != 1 || advice.Suggestions[0].Conflict.Substitution != SubstituteCurrent {
		t.Fatalf("unexpected advice: %+v", advice)
	}

	argumentBytes, err := os.ReadFile(capturedArgs)
	if err != nil {
		t.Fatal(err)
	}
	arguments := strings.Split(strings.TrimSpace(string(argumentBytes)), "\n")
	if len(arguments) != 4 || arguments[2] != arguments[3] {
		t.Fatalf("helper did not run in its artifact directory: %q", arguments)
	}
	if filepath.Base(arguments[0]) != "request.json" || filepath.Base(arguments[1]) != "response.json" || filepath.Base(arguments[2]) != "artifacts" {
		t.Fatalf("unexpected exposed paths: %q", arguments)
	}
	if _, err := os.Stat(arguments[0]); !os.IsNotExist(err) {
		t.Fatalf("run request was not cleaned up: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(arguments[0])); !os.IsNotExist(err) {
		t.Fatalf("private run directory was not cleaned up: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "PWNED")); !os.IsNotExist(err) {
		t.Fatalf("quoted temp path became shell syntax: %v", err)
	}

	requestJSON, err := os.ReadFile(capturedRequest)
	if err != nil {
		t.Fatal(err)
	}
	var wireFields map[string]json.RawMessage
	if err := json.Unmarshal(requestJSON, &wireFields); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"provider", "model", "apiKey", "key", "worktree", "repository"} {
		if _, found := wireFields[forbidden]; found {
			t.Fatalf("provider/repository field %q leaked into request: %s", forbidden, requestJSON)
		}
	}
	var sealed AdviceRequest
	if err := decodeStrictJSON(requestJSON, &sealed); err != nil {
		t.Fatalf("request is not strict versioned JSON: %v", err)
	}
	if sealed.Version != ProtocolVersion || sealed.Kind != KindConflict || sealed.SourceDigest != request.SourceDigest {
		t.Fatalf("incorrect request envelope: %+v", sealed)
	}
	if sealed.Artifacts[0].Regions[0].ID != request.Artifacts[0].Regions[0].ID {
		t.Fatal("helper request did not carry original region identity")
	}
}

func TestHelperCommandTemplateAndSinglePassQuoting(t *testing.T) {
	values := map[string]string{
		"requestFile":  "/tmp/{responseFile}' ; touch PWNED ; #/request.json",
		"responseFile": "/tmp/response.json",
		"artifactDir":  "/tmp/artifacts with spaces",
	}
	expanded, err := expandHelperCommand("helper {requestFile} {responseFile} {artifactDir}", values)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(expanded, shellQuoteHelper(values["requestFile"])) {
		t.Fatalf("request path was not quoted exactly: %s", expanded)
	}
	if strings.Count(expanded, shellQuoteHelper(values["responseFile"])) != 1 {
		t.Fatalf("placeholder-looking request data expanded again: %s", expanded)
	}

	invalid := []string{
		"",
		"helper {responseFile}",
		"helper {requestFile}",
		"helper {requestFile} {responseFile} {worktree}",
		"helper {{requestFile}} {responseFile}",
		"helper {requestFile} {requestFile} {responseFile}",
	}
	for _, template := range invalid {
		if _, err := NewCommandAdvisor(CommandConfig{Template: template}); !errors.Is(err, ErrUnsafeCommand) {
			t.Errorf("template %q error = %v, want ErrUnsafeCommand", template, err)
		}
	}
}

func TestCommandAdvisorRejectsInvalidResponses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temporary POSIX helper")
	}
	tests := []struct {
		name      string
		mutate    func(*AdviceRequest, any) any
		want      error
		rawSuffix string
	}{
		{
			name: "artifact digest mismatch",
			mutate: func(_ *AdviceRequest, response any) any {
				advice := response.(Advice)
				advice.Artifacts[0].Digest = "sha256:" + strings.Repeat("0", 64)
				return advice
			},
			want: ErrHashMismatch,
		},
		{
			name: "duplicate artifacts",
			mutate: func(_ *AdviceRequest, response any) any {
				advice := response.(Advice)
				advice.Artifacts = append(advice.Artifacts, advice.Artifacts[0])
				return advice
			},
			want: ErrDuplicateIdentity,
		},
		{
			name: "cross source envelope",
			mutate: func(_ *AdviceRequest, response any) any {
				advice := response.(Advice)
				advice.SourceDigest = "sha256:" + strings.Repeat("1", 64)
				return advice
			},
			want: ErrHashMismatch,
		},
		{
			name: "arbitrary patch cannot enter protocol",
			mutate: func(_ *AdviceRequest, response any) any {
				advice := response.(Advice)
				data, _ := json.Marshal(advice)
				var object map[string]any
				_ = json.Unmarshal(data, &object)
				object["patch"] = "diff --git a/etc/passwd b/etc/passwd"
				return object
			},
			want: ErrInvalidProtocol,
		},
		{
			name: "provider and key cannot enter protocol",
			mutate: func(_ *AdviceRequest, response any) any {
				advice := response.(Advice)
				data, _ := json.Marshal(advice)
				var object map[string]any
				_ = json.Unmarshal(data, &object)
				object["model"] = "untrusted-model"
				object["apiKey"] = "do-not-send"
				return object
			},
			want: ErrInvalidProtocol,
		},
		{
			name:      "trailing JSON",
			mutate:    func(_ *AdviceRequest, response any) any { return response },
			want:      ErrInvalidProtocol,
			rawSuffix: "{}",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			helper := filepath.Join(root, "helper.sh")
			request := singleConflictRequest(t, "file.txt")
			response := test.mutate(&request, singleConflictAdvice(request))
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			writeExecutable(t, helper, "cat > \"$2\" <<'TANGIT_RESPONSE'\n"+string(data)+test.rawSuffix+"\nTANGIT_RESPONSE\n")
			advisor, err := NewCommandAdvisor(CommandConfig{Template: shellQuoteHelper(helper) + " {requestFile} {responseFile}", Timeout: 5 * time.Second, TempDir: root})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := advisor.Propose(context.Background(), request); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestCommandAdvisorRejectsSymlinkResponseAndBadSourcePath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temporary POSIX helper")
	}
	root := t.TempDir()
	request := singleConflictRequest(t, "file.txt")
	response, err := json.Marshal(singleConflictAdvice(request))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside response")
	if err := os.WriteFile(outside, response, 0o600); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "symlink helper.sh")
	writeExecutable(t, helper, "ln -s "+shellQuoteHelper(outside)+" \"$2\"\n")
	advisor, err := NewCommandAdvisor(CommandConfig{Template: shellQuoteHelper(helper) + " {requestFile} {responseFile}", TempDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := advisor.Propose(context.Background(), request); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("symlink response error = %v, want ErrInvalidProtocol", err)
	}

	marker := filepath.Join(root, "must-not-run")
	writeExecutable(t, helper, "touch "+shellQuoteHelper(marker)+"\n")
	advisor, err = NewCommandAdvisor(CommandConfig{Template: shellQuoteHelper(helper) + " {requestFile} {responseFile}", TempDir: root})
	if err != nil {
		t.Fatal(err)
	}
	// Construct a valid request first, then retain sealed metadata to ensure the
	// command adapter itself rejects a traversal before helper launch.
	malicious := singleConflictRequest(t, "file.txt")
	malicious.Artifacts[0].Path = "../../outside"
	if _, err := advisor.Propose(context.Background(), malicious); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("path traversal error = %v, want ErrInvalidProtocol", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("helper ran for a malicious source path: %v", err)
	}
}

func TestCommandAdvisorResponseSizeLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temporary POSIX helper")
	}
	root := t.TempDir()
	helper := filepath.Join(root, "large response.sh")
	request := singleConflictRequest(t, "file.txt")
	request.Limits.MaxResponseBytes = 64
	response := singleConflictAdvice(request)
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	data = append(bytes.Repeat([]byte{' '}, 128), data...)
	writeExecutable(t, helper, "cat > \"$2\" <<'TANGIT_RESPONSE'\n"+string(data)+"\nTANGIT_RESPONSE\n")
	advisor, err := NewCommandAdvisor(CommandConfig{Template: shellQuoteHelper(helper) + " {requestFile} {responseFile}", TempDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := advisor.Propose(context.Background(), request); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("oversized response error = %v, want ErrLimitExceeded", err)
	}
}

func TestCommandAdvisorTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temporary POSIX helper")
	}
	root := t.TempDir()
	helper := filepath.Join(root, "timeout helper.sh")
	writeExecutable(t, helper, "while :; do sleep 30; done\n")
	advisor, err := NewCommandAdvisor(CommandConfig{
		Template: shellQuoteHelper(helper) + " {requestFile} {responseFile}",
		Timeout:  75 * time.Millisecond,
		TempDir:  root,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = advisor.Propose(context.Background(), singleConflictRequest(t, "file.txt"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("helper children held the command open for %s", elapsed)
	}
}

func TestCommandAdvisorCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("temporary POSIX helper")
	}
	root := t.TempDir()
	helper := filepath.Join(root, "cancel helper.sh")
	startedFile := filepath.Join(root, "started")
	writeExecutable(t, helper, "printf started > "+shellQuoteHelper(startedFile)+"\nwhile :; do sleep 30; done\n")
	advisor, err := NewCommandAdvisor(CommandConfig{
		Template: shellQuoteHelper(helper) + " {requestFile} {responseFile}",
		TempDir:  root,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := singleConflictRequest(t, "file.txt")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := advisor.Propose(ctx, request)
		result <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(startedFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("helper did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("canceled helper did not stop")
	}

	preCanceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := advisor.Propose(preCanceled, singleConflictRequest(t, "file.txt")); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled error = %v, want context.Canceled", err)
	}
}
